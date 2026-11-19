package commands_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// Drain as a background job (D-63). The fake writer's drain hook reports the steps
// cluster.DrainNode reports against client-go's fake API server (TestDrainNodeReportsEachPod in
// package cluster): the cordon, the list, then each pod's eviction asked for, refused for now by
// a budget, accepted and gone.

// nodePod is a pod on node; owner "Kind/name" makes it controlled, mirror a static pod's mirror.
func nodePod(namespace, name, node, owner string, mirror bool) *unstructured.Unstructured {
	p := testkit.Pod(namespace, name)
	_ = unstructured.SetNestedField(p.Object, node, "spec", "nodeName")
	if owner != "" {
		kind, ownerName, _ := strings.Cut(owner, "/")
		yes := true
		p.SetOwnerReferences([]metav1.OwnerReference{{APIVersion: "apps/v1", Kind: kind, Name: ownerName, UID: types.UID("uid-" + ownerName), Controller: &yes}})
	}
	if mirror {
		p.SetAnnotations(map[string]string{"kubernetes.io/config.mirror": "hash"})
	}
	return p
}

// drainSource is seedActions with pods on worker-1: two the drain evicts, a DaemonSet's and a
// mirror pod it leaves, and one on worker-2.
func drainSource() *testkit.MemorySource {
	src := seedActions()
	for _, p := range []*unstructured.Unstructured{
		nodePod("shop", "db-0", "worker-1", "StatefulSet/db", false),
		nodePod("shop", "web-1", "worker-1", "ReplicaSet/web-5d8", false),
		nodePod("kube-system", "proxy-x1", "worker-1", "DaemonSet/proxy", false),
		nodePod("kube-system", "etcd-worker-1", "worker-1", "", true),
		nodePod("shop", "web-2", "worker-2", "ReplicaSet/web-5d8", false),
	} {
		src.Set(testkit.PodsGVR, p)
	}
	return src
}

// gate is a drain hook's pause: the hook waits at a point the test releases.
type gate struct {
	once    sync.Once
	reached chan struct{}
	release chan struct{}
}

func newGate() *gate { return &gate{reached: make(chan struct{}), release: make(chan struct{})} }

// wait marks the point reached and waits until the test releases it or ctx ends.
func (g *gate) wait(ctx context.Context) error {
	g.once.Do(func() { close(g.reached) })
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startDrain dispatches node.drain from origin with ctx and returns its result.
func startDrain(ctx context.Context, t *testing.T, h *harness, origin engine.Origin, params any) commands.DrainResult {
	t.Helper()
	res, err := h.s.Dispatch(ctx, engine.Invocation{ID: "node.drain", Params: mustJSON(t, params), Origin: origin})
	if err != nil {
		t.Fatalf("node.drain: %v", err)
	}
	dr, ok := res.(commands.DrainResult)
	if !ok {
		t.Fatalf("node.drain returned %T", res)
	}
	return dr
}

// waitStep waits until the job's steps hold want ("state text: detail").
func waitStep(t *testing.T, h *harness, id, want string) {
	t.Helper()
	waitUntil(t, want, func() bool {
		j := jobOf(h, id)
		return j != nil && slices.Contains(stepStates(j), want)
	})
}

const budgetWait = "the PodDisruptionBudget db-pdb allows no disruption now (The disruption budget db-pdb needs 1 healthy pods and has 1 currently)"

// TestDrainJobBudgetBlocksThenAllows: the job starts with the cordon and the cache's pods on the
// node that the drain evicts (not the DaemonSet's, not the mirror pod, not another node's); the
// drain's own list replaces them (a pod gone meanwhile leaves, one the cache did not have comes);
// a budget's refusal shows in the pod's step while the drain retries, then the eviction is
// accepted and the pod goes; the job ends done, the journal entry goes, and the call's result was
// the job's tab at once.
func TestDrainJobBudgetBlocksThenAllows(t *testing.T) {
	journal := engine.NewMemoryDrainJournal()
	h, w := newActionsHarness(t, drainSource(), engine.Options{Drains: engine.NewDrains(journal), Confirmer: &confirmations{answer: true}})
	listed, blocked, accepted := newGate(), newGate(), newGate()
	w.setDrainHook(func(ctx context.Context, node string, progress func(cluster.DrainProgress)) error {
		if err := listed.wait(ctx); err != nil {
			return err
		}
		progress(cluster.DrainProgress{Step: cluster.DrainListed, Node: node, Total: 2, Pods: []string{"shop/late-0", "shop/db-0"},
			Message: "ignoring DaemonSet-managed Pods: kube-system/proxy-x1"})
		for _, s := range []cluster.DrainStep{cluster.DrainEvicting, cluster.DrainAccepted, cluster.DrainEvicted} {
			progress(cluster.DrainProgress{Step: s, Node: node, Pod: "shop/late-0"})
		}
		progress(cluster.DrainProgress{Step: cluster.DrainEvicting, Node: node, Pod: "shop/db-0"})
		progress(cluster.DrainProgress{Step: cluster.DrainBlocked, Node: node, Pod: "shop/db-0", Message: budgetWait})
		if err := blocked.wait(ctx); err != nil {
			return err
		}
		progress(cluster.DrainProgress{Step: cluster.DrainAccepted, Node: node, Pod: "shop/db-0"})
		if err := accepted.wait(ctx); err != nil {
			return err
		}
		progress(cluster.DrainProgress{Step: cluster.DrainEvicted, Node: node, Pod: "shop/db-0"})
		return nil
	})

	res := startDrain(context.Background(), t, h, engine.OriginGUI, actionNodeRef("worker-1"))
	if res.Action != "job started" || res.Job.Job == nil || res.Job.Title != "Drain: node worker-1" || h.s.UIState().ActiveDock != res.Job.ID {
		t.Fatalf("result %+v", res)
	}
	var texts []string
	for _, st := range res.Job.Job.Steps {
		texts = append(texts, st.Text)
	}
	if want := []string{"Node worker-1", "Pod shop/db-0", "Pod shop/web-1"}; !slices.Equal(texts, want) {
		t.Errorf("the steps it started with %v, want %v (the cache's pods the drain evicts)", texts, want)
	}
	<-listed.reached
	waitStep(t, h, res.Job.ID, "done Node worker-1: Cordoned")
	close(listed.release)

	waitStep(t, h, res.Job.ID, "running Pod shop/db-0: Waiting: "+budgetWait+"; retrying")
	if got := stepStates(jobOf(h, res.Job.ID)); !slices.Equal(got, []string{
		"done Node worker-1: Cordoned; ignoring DaemonSet-managed Pods: kube-system/proxy-x1",
		"running Pod shop/db-0: Waiting: " + budgetWait + "; retrying",
		"done Pod shop/late-0: Evicted",
	}) {
		t.Errorf("steps while the budget blocks %v", got)
	}
	var pending []engine.PendingDrainInfo
	h.must(t, engine.OriginExternalAgent, "node.pendingDrains", nil, &pending)
	if len(pending) != 1 || pending[0].Status != engine.DrainStatusRunning || pending[0].Job != res.Job.ID ||
		pending[0].Progress == nil || pending[0].Progress.Step != cluster.DrainBlocked {
		t.Errorf("pending while blocked %+v", pending)
	}
	close(blocked.release)
	waitStep(t, h, res.Job.ID, "running Pod shop/db-0: Evicted; waiting for the pod to end")
	close(accepted.release)

	job := waitJob(t, h, res.Job.ID)
	if job.State != engine.JobDone || job.Outcome != "Drained node worker-1: 2 pods evicted. Ignoring DaemonSet-managed Pods: kube-system/proxy-x1." ||
		job.Counts.Done != 3 || job.Counts.Total != 3 {
		t.Errorf("job %+v, steps %v", job, stepStates(job))
	}
	if entries, _ := journal.List(); len(entries) != 0 {
		t.Errorf("journal after the drain %+v", entries)
	}
}

// TestDrainJobOutlivesItsCall: an MCP client that gives up on its call cancels the call's context
// (mcp.Server.callContext); the drain the user confirmed goes on, its context never cancelled,
// and the agent's audit log gets the job's end.
func TestDrainJobOutlivesItsCall(t *testing.T) {
	audit := &syncBuffer{}
	journal := engine.NewMemoryDrainJournal()
	h, w := newActionsHarness(t, nil, engine.Options{Drains: engine.NewDrains(journal), Confirmer: &confirmations{answer: true}, Audit: audit})
	g := newGate()
	drainErr := make(chan error, 1)
	w.setDrainHook(func(ctx context.Context, node string, progress func(cluster.DrainProgress)) error {
		progress(cluster.DrainProgress{Step: cluster.DrainListed, Node: node, Total: 1, Pods: []string{"shop/web-0"}})
		err := g.wait(ctx)
		drainErr <- err
		if err != nil {
			return err
		}
		progress(cluster.DrainProgress{Step: cluster.DrainEvicted, Node: node, Pod: "shop/web-0"})
		return nil
	})
	call, giveUp := context.WithCancel(context.Background())
	res := startDrain(call, t, h, engine.OriginExternalAgent, actionNodeRef("worker-1"))
	<-g.reached
	giveUp() // the client gives up; the handler's own cancel does the same when it returns
	close(g.release)
	if err := <-drainErr; err != nil {
		t.Fatalf("the drain's context ended with the call: %v", err)
	}
	if job := waitJob(t, h, res.Job.ID); job.State != engine.JobDone {
		t.Errorf("job %+v", job)
	}
	if entries, _ := journal.List(); len(entries) != 0 {
		t.Errorf("journal %+v", entries)
	}
	waitUntil(t, "the audit's ended line", func() bool {
		return slices.ContainsFunc(audit.Lines(), func(l engine.AuditEntry) bool {
			return l.Command == "node.drain" && l.Phase == "ended" && l.OK && l.Origin == engine.OriginExternalAgent
		})
	})
}

// blockedDrain is a drain hook that evicts web-0, is refused db-0's eviction by a budget, and
// waits until its context ends, as cluster.DrainNode does when it is stopped.
func blockedDrain(g *gate) func(ctx context.Context, node string, progress func(cluster.DrainProgress)) error {
	return func(ctx context.Context, node string, progress func(cluster.DrainProgress)) error {
		progress(cluster.DrainProgress{Step: cluster.DrainListed, Node: node, Total: 3, Pods: []string{"shop/web-0", "shop/db-0", "shop/web-1"}})
		progress(cluster.DrainProgress{Step: cluster.DrainEvicting, Node: node, Pod: "shop/web-0"})
		progress(cluster.DrainProgress{Step: cluster.DrainAccepted, Node: node, Pod: "shop/web-0"})
		progress(cluster.DrainProgress{Step: cluster.DrainEvicted, Node: node, Pod: "shop/web-0"})
		progress(cluster.DrainProgress{Step: cluster.DrainEvicting, Node: node, Pod: "shop/db-0"})
		progress(cluster.DrainProgress{Step: cluster.DrainBlocked, Node: node, Pod: "shop/db-0", Message: budgetWait})
		_ = g.wait(ctx)
		<-ctx.Done()
		return fmt.Errorf("drain node %s: cancelled: %w", node, ctx.Err())
	}
}

// TestDrainJobCancelledLeavesAPendingDrain: job.cancel mid-way (an agent's here) stops the drain:
// the pod evicted stays done, the one a budget held is stopped while waiting, the one never asked
// for not attempted; the outcome says who stopped it, what was done and that the node stays
// cordoned; the journal keeps the drain as stopped, which node.pendingDrains and the banner offer
// as after a crash (Finish, Uncordon, Dismiss); nothing is uncordoned, and no toast says more.
func TestDrainJobCancelledLeavesAPendingDrain(t *testing.T) {
	journal := engine.NewMemoryDrainJournal()
	h, w := newActionsHarness(t, nil, engine.Options{Drains: engine.NewDrains(journal), Confirmer: &confirmations{answer: true}})
	events := recordEvents(h.s)
	g := newGate()
	w.setDrainHook(blockedDrain(g))
	res := startDrain(context.Background(), t, h, engine.OriginGUI, actionNodeRef("worker-1"))
	<-g.reached

	var tab engine.DockTab
	h.must(t, engine.OriginExternalAgent, "job.cancel", map[string]any{"id": res.Job.ID}, &tab)
	job := tab.Job
	if job == nil || job.State != engine.JobCancelled {
		t.Fatalf("job.cancel returned %+v", tab)
	}
	if got := stepStates(job); !slices.Equal(got, []string{
		"done Node worker-1: Cordoned",
		"cancelled Pod shop/db-0: Stopped while waiting: " + budgetWait,
		"done Pod shop/web-0: Evicted",
		"cancelled Pod shop/web-1: Not attempted: cancelled by an agent",
	}) {
		t.Errorf("steps %v", got)
	}
	want := "Stopped: cancelled by an agent. Evicted 1 of 3 pods; 2 not attempted. Node worker-1 stays cordoned and partly drained"
	if !strings.HasPrefix(job.Outcome, want) {
		t.Errorf("outcome %q, want it to start %q", job.Outcome, want)
	}
	entries, _ := journal.List()
	if len(entries) != 1 || entries[0].State != engine.DrainStopped || entries[0].Error != "cancelled by an agent" {
		t.Fatalf("journal %+v", entries)
	}
	var pending []engine.PendingDrainInfo
	h.must(t, engine.OriginGUI, "node.pendingDrains", nil, &pending)
	if len(pending) != 1 || pending[0].Status != engine.DrainStatusStopped || pending[0].Job != "" {
		t.Errorf("pending %+v", pending)
	}
	waitUntil(t, "the cordon in the cache", func() bool {
		obj, err := h.s.Lookup(model.ObjectRef{Kind: "nodes", Name: "worker-1"})
		cordoned, _ := model.NestedBool(obj.Object, "spec", "unschedulable")
		return err == nil && cordoned
	})
	if slices.Contains(w.Calls(), "uncordon worker-1") {
		t.Error("the stop uncordoned the node")
	}
	uncordon, _ := h.s.Registry().Get("node.uncordon")
	if err := uncordon.Enabled(h.s, mustJSON(t, actionNodeRef("worker-1"))); err != nil {
		t.Errorf("Uncordon after the stop: %v", err)
	}
	for _, ev := range events.of(engine.EventToast) {
		if strings.Contains(ev.Message, "worker-1") {
			t.Errorf("a toast for a stopped drain: %q", ev.Message)
		}
	}

	// Finish drain: node.drain again with the settings it was started with.
	w.setDrainHook(nil)
	res = startDrain(context.Background(), t, h, engine.OriginGUI, map[string]any{"ref": model.ObjectRef{Kind: "nodes", Name: "worker-1"},
		"ignoreDaemonSets": true, "timeoutSeconds": pending[0].Settings.TimeoutSeconds})
	if job := waitJob(t, h, res.Job.ID); job.State != engine.JobDone {
		t.Errorf("finish %+v", job)
	}
	if entries, _ := journal.List(); len(entries) != 0 {
		t.Errorf("journal after finishing %+v", entries)
	}
}

// TestDrainJobStopsWhenInterrupted (D-58, S8): an interrupted connection stops the drain's job;
// the drain stays as stopped with the reason, to be finished or dismissed after the reconnect.
// Closing the tab stops it the same way.
func TestDrainJobStopsWhenInterrupted(t *testing.T) {
	journal := engine.NewMemoryDrainJournal()
	h, w := newActionsHarness(t, nil, engine.Options{Drains: engine.NewDrains(journal), Confirmer: &confirmations{answer: true}})
	g := newGate()
	w.setDrainHook(blockedDrain(g))
	res := startDrain(context.Background(), t, h, engine.OriginGUI, actionNodeRef("worker-1"))
	<-g.reached
	h.s.UpdateUI(func(u *engine.UIState) { u.Connection.State = engine.Interrupted })
	job := waitJob(t, h, res.Job.ID)
	if job.State != engine.JobCancelled || !strings.HasPrefix(job.Outcome, "Stopped: the connection was interrupted") ||
		!strings.Contains(job.Outcome, "stays cordoned and partly drained") {
		t.Errorf("job %+v", job)
	}
	entries, _ := journal.List()
	if len(entries) != 1 || entries[0].State != engine.DrainStopped || !strings.Contains(entries[0].Error, "the connection was interrupted") {
		t.Fatalf("journal %+v", entries)
	}
	h.s.UpdateUI(func(u *engine.UIState) { u.Connection.State = engine.Connected })

	// Its tab closed.
	g = newGate()
	w.setDrainHook(blockedDrain(g))
	res = startDrain(context.Background(), t, h, engine.OriginGUI, actionNodeRef("worker-1"))
	<-g.reached
	h.must(t, engine.OriginGUI, "dock.close", map[string]any{"id": res.Job.ID}, nil)
	waitUntil(t, "the stopped drain", func() bool { return !h.s.Drains().Running("test", "worker-1") })
	entries, _ = journal.List()
	if len(entries) != 1 || entries[0].State != engine.DrainStopped || entries[0].Error != "its tab was closed" {
		t.Errorf("journal after closing the tab %+v", entries)
	}
}

// TestDrainJobPodFailures: a pod gone before its eviction is done ("Gone already"), a pod whose
// eviction is refused for good fails with the API server's error, and a pod whose eviction was
// accepted counts as evicted although it still shut down when the drain timed out; the drain
// fails and the journal keeps it as failed.
func TestDrainJobPodFailures(t *testing.T) {
	journal := engine.NewMemoryDrainJournal()
	h, w := newActionsHarness(t, nil, engine.Options{Drains: engine.NewDrains(journal), Confirmer: &confirmations{answer: true}})
	w.setDrainHook(func(_ context.Context, node string, progress func(cluster.DrainProgress)) error {
		progress(cluster.DrainProgress{Step: cluster.DrainListed, Node: node, Total: 4, Pods: []string{"shop/a", "shop/b", "shop/c", "shop/d"}})
		progress(cluster.DrainProgress{Step: cluster.DrainEvicted, Node: node, Pod: "shop/a", Message: "gone already"})
		progress(cluster.DrainProgress{Step: cluster.DrainPodFailed, Node: node, Pod: "shop/b", Message: `pods "b" is forbidden: no evictions here`})
		progress(cluster.DrainProgress{Step: cluster.DrainAccepted, Node: node, Pod: "shop/c"})
		return errors.New(`drain node worker-1: timed out after 1m0s: error when waiting for pod "c" in namespace "shop" to terminate: global timeout reached`)
	})
	res := startDrain(context.Background(), t, h, engine.OriginGUI, actionNodeRef("worker-1"))
	job := waitJob(t, h, res.Job.ID)
	if got := stepStates(job); !slices.Equal(got, []string{
		"done Node worker-1: Cordoned",
		"done Pod shop/a: Gone already",
		`failed Pod shop/b: Pods "b" is forbidden: no evictions here`,
		"done Pod shop/c: Evicted; the pod was still shutting down when the drain ended",
		"cancelled Pod shop/d: Not attempted: the drain ended first",
	}) {
		t.Errorf("steps %v", got)
	}
	if job.State != engine.JobFailed || !strings.HasPrefix(job.Outcome, "Evicted 2 of 4 pods; 1 failed, 1 not attempted. Timed out after 1m0s") {
		t.Errorf("job %+v", job)
	}
	if entries, _ := journal.List(); len(entries) != 1 || entries[0].State != engine.DrainFailed {
		t.Errorf("journal %+v", entries)
	}
}
