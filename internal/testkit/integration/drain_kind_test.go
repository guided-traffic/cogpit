//go:build kind

package integration_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// drainNamespace is the drain cluster's namespace of the drain test.
const drainNamespace = "cogpit-drain-test"

// crashingJournal stands for the disk of a process that dies: once dead, nothing the process
// writes reaches the journal any more.
type crashingJournal struct {
	engine.DrainJournal
	dead atomic.Bool
}

func (c *crashingJournal) Put(p engine.PendingDrain) error {
	if c.dead.Load() {
		return errors.New("the process is gone")
	}
	return c.DrainJournal.Put(p)
}

func (c *crashingJournal) Remove(context, node string) error {
	if c.dead.Load() {
		return errors.New("the process is gone")
	}
	return c.DrainJournal.Remove(context, node)
}

// drainEvents records a session's toasts and drain events.
type drainEvents struct {
	mu     sync.Mutex
	events []engine.Event
}

func (e *drainEvents) add(ev engine.Event) {
	if ev.Type != engine.EventToast && ev.Type != engine.EventDrains {
		return
	}
	e.mu.Lock()
	e.events = append(e.events, ev)
	e.mu.Unlock()
}

func (e *drainEvents) has(typ engine.EventType, sub string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.ContainsFunc(e.events, func(ev engine.Event) bool { return ev.Type == typ && strings.Contains(ev.Message, sub) })
}

// drainSession starts an engine on the drain cluster with a journal, subscribed to events before
// it connects.
func drainSession(ctx context.Context, t *testing.T, kubeconfig, contextName string, j engine.DrainJournal) (*engine.Session, *drainEvents) {
	t.Helper()
	s := engine.New(engine.Options{
		Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}},
		Confirmer: engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return true, nil }),
		Drains:    engine.NewDrains(j),
	})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	ev := &drainEvents{}
	s.Subscribe(ev.add)
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Objects(ctx, "nodes", nil); err != nil {
		t.Fatal(err)
	}
	return s, ev
}

// TestKindDrainCrashFinishUncordon drains a worker of the multi-node drain cluster through the
// engine: a Deployment's pods and a pod guarded by a PodDisruptionBudget that allows no
// disruption, next to a DaemonSet's pod. Each drain runs as a background job (D-63). Cogpit
// "crashes" while the budget blocks the eviction (the job's step says so); the next start offers
// the unfinished drain; finishing it fails while the budget stands; cancelling it while the budget
// holds the pod leaves it stopped, the node cordoned; finishing it succeeds once the budget is gone,
// although its caller gives up at once; uncordoning makes the node schedulable again. It runs only when
// COGPIT_KIND_DRAIN_KUBECONFIG and COGPIT_KIND_DRAIN_CONTEXT name the drain cluster (make
// test-kind-drain), never against the shared single-node cluster.
func TestKindDrainCrashFinishUncordon(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_DRAIN_KUBECONFIG"), os.Getenv("COGPIT_KIND_DRAIN_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_DRAIN_KUBECONFIG and COGPIT_KIND_DRAIN_CONTEXT are not set; run make kind-drain-up and make test-kind-drain")
	}
	if contextName == "kind-cogpit" {
		t.Fatal("the drain test never runs against the shared cluster kind-cogpit")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	kube := conn.Kube()

	target := pickWorker(ctx, t, kube)
	t.Logf("draining worker %s of %s", target, contextName)
	t.Cleanup(func() { // leave the cluster usable whatever happened
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		if err := conn.SetNodeUnschedulable(cctx, target, false); err != nil {
			t.Errorf("uncordon %s at cleanup: %v", target, err)
		}
	})
	ns := drainNamespace
	freshNamespace(ctx, t, kube, ns)
	onTarget := map[string]string{"kubernetes.io/hostname": target}
	for _, dep := range []*appsv1.Deployment{
		{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "web"}, Spec: appsv1.DeploymentSpec{Replicas: int32p(2),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}, Template: podTemplate("web", onTarget)}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "guarded"}, Spec: appsv1.DeploymentSpec{Replicas: int32p(1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "guarded"}}, Template: podTemplate("guarded", onTarget)}},
	} {
		if _, err := kube.AppsV1().Deployments(ns).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := kube.AppsV1().DaemonSets(ns).Create(ctx, &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: "agent"},
		Spec:       appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "agent"}}, Template: podTemplate("agent", nil)},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := kube.PolicyV1().PodDisruptionBudgets(ns).Create(ctx, guardingBudget(ns, "guarded-pdb", "guarded"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor(ctx, t, "web, guarded and the agent on "+target+", and the budget settled", func() bool {
		return len(readyPods(ctx, kube, ns, "web")) == 2 && len(readyPods(ctx, kube, ns, "guarded")) == 1 &&
			slices.ContainsFunc(readyPods(ctx, kube, ns, "agent"), func(p corev1.Pod) bool { return p.Spec.NodeName == target }) &&
			budgetSettled(ctx, kube, ns, "guarded-pdb", 0)
	})
	// Every drain here sets deleteEmptyDirData: kind's own pods on the worker (CoreDNS, the
	// local-path provisioner) may use emptyDir volumes, and a refused drain would test nothing.
	node := model.ObjectRef{Kind: "nodes", Name: target}
	dir := t.TempDir()

	// The first run of Cogpit: the drain starts as a job (D-63), the budget blocks the guarded
	// pod's eviction, which the job's step shows, and Cogpit dies: the journal's last write is the
	// drain's start.
	crash := &crashingJournal{DrainJournal: engine.NewFileDrainJournal(dir)}
	first, _ := drainSession(ctx, t, kubeconfig, contextName, crash)
	fd := dispatcher{t: t, ctx: ctx, s: first}
	var res commands.DrainResult
	fd.must("node.drain", map[string]any{"ref": node, "timeoutSeconds": 180, "deleteEmptyDirData": true}, &res)
	waitFor(ctx, t, "the guarded pod's step waiting for the budget", func() bool { return budgetWaitShown(first, res.Job.ID) })
	crash.dead.Store(true)
	first.Close() // the job's end is not recorded: the journal is gone with the process
	if !nodeUnschedulable(ctx, t, kube, target) {
		t.Fatal("the interrupted drain left the node schedulable")
	}
	onDisk, err := engine.NewFileDrainJournal(dir).List()
	if err != nil || len(onDisk) != 1 || onDisk[0].State != engine.DrainRunning || onDisk[0].Node != target || onDisk[0].Context != contextName {
		t.Fatalf("journal after the crash: %+v, %v", onDisk, err)
	}

	// The next start: the unfinished drain is offered on connect.
	second, secondEvents := drainSession(ctx, t, kubeconfig, contextName, engine.NewFileDrainJournal(dir))
	defer second.Close()
	d := dispatcher{t: t, ctx: ctx, s: second}
	waitFor(ctx, t, "the offer of the unfinished drain", func() bool {
		return secondEvents.has(engine.EventDrains, "The drain of node "+target+" in context "+contextName+" did not finish")
	})
	var pending []engine.PendingDrainInfo
	d.must("node.pendingDrains", nil, &pending)
	if len(pending) != 1 || pending[0].Status != engine.DrainStatusInterrupted || pending[0].NodeState != engine.DrainNodeCordoned {
		t.Fatalf("pending drains after the restart: %+v", pending)
	}

	// Finishing while the budget stands times out, keeps the entry and names the budget: the
	// eviction's first attempt meets the budget's refusal.
	d.must("node.drain", map[string]any{"ref": node, "timeoutSeconds": 5, "deleteEmptyDirData": true}, &res)
	job := drainJobEnd(ctx, t, second, res.Job.ID)
	if job.State != engine.JobFailed || !strings.Contains(job.Outcome, "guarded-pdb") || !strings.Contains(job.Outcome, "Timed out") {
		t.Fatalf("finish under the budget: %+v", job)
	}
	t.Logf("finish under the budget: %s", job.Outcome)
	var failed []engine.PendingDrainInfo
	d.must("node.pendingDrains", nil, &failed)
	if len(failed) != 1 || failed[0].Status != engine.DrainStatusFailed {
		t.Fatalf("pending after the failed finish: %+v", failed)
	}

	// Finishing again and cancelling it while the budget holds the guarded pod: the job stops, the
	// drain stays as stopped, the node cordoned and the guarded pod in place.
	d.must("node.drain", map[string]any{"ref": node, "timeoutSeconds": 180, "deleteEmptyDirData": true}, &res)
	waitFor(ctx, t, "the guarded pod's step waiting for the budget", func() bool { return budgetWaitShown(second, res.Job.ID) })
	var tab engine.DockTab
	d.must("job.cancel", map[string]any{"id": res.Job.ID}, &tab)
	if tab.Job == nil || tab.Job.State != engine.JobCancelled || !strings.HasPrefix(tab.Job.Outcome, "Stopped: cancelled.") ||
		!strings.Contains(tab.Job.Outcome, "stays cordoned") {
		t.Fatalf("cancelled: %+v", tab.Job)
	}
	t.Logf("cancelled: %s", tab.Job.Outcome)
	var stopped []engine.PendingDrainInfo
	d.must("node.pendingDrains", nil, &stopped)
	if len(stopped) != 1 || stopped[0].Status != engine.DrainStatusStopped || stopped[0].Error != "cancelled" {
		t.Fatalf("pending after the cancel: %+v", stopped)
	}
	if !nodeUnschedulable(ctx, t, kube, target) || len(readyPods(ctx, kube, ns, "guarded")) != 1 {
		t.Error("the cancel uncordoned the node or the guarded pod went")
	}

	// Without the budget, the drain finishes and its entry goes, although its caller gives up at
	// once (as an MCP client's call ends): the job does not end with the call.
	if err := kube.PolicyV1().PodDisruptionBudgets(ns).Delete(ctx, "guarded-pdb", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	call, giveUp := context.WithCancel(ctx)
	cd := dispatcher{t: t, ctx: call, s: second}
	cd.must("node.drain", map[string]any{"ref": node, "timeoutSeconds": 180, "deleteEmptyDirData": true}, &res)
	giveUp()
	job = drainJobEnd(ctx, t, second, res.Job.ID)
	if job.State != engine.JobDone || !strings.HasPrefix(job.Outcome, "Drained node "+target+": ") {
		t.Fatalf("the drain without the budget: %+v", job)
	}
	t.Logf("drained: %s", job.Outcome)
	var none []engine.PendingDrainInfo
	d.must("node.pendingDrains", nil, &none)
	if len(none) != 0 {
		t.Errorf("pending after the drain finished: %+v", none)
	}
	assertOnlyDaemonAndMirrorPods(ctx, t, kube, target)

	// Uncordon: schedulable again.
	d.must("node.uncordon", map[string]any{"ref": node}, nil)
	if nodeUnschedulable(ctx, t, kube, target) {
		t.Error("the node is still cordoned")
	}
}

// budgetWaitShown reports whether a drain job's step for the guarded pod says that the budget
// guarded-pdb holds its eviction.
func budgetWaitShown(s *engine.Session, id string) bool {
	tab, ok := s.UIState().DockTab(id)
	if !ok || tab.Job == nil {
		return false
	}
	return slices.ContainsFunc(tab.Job.Steps, func(st engine.JobStep) bool {
		return strings.HasPrefix(st.Text, "Pod "+drainNamespace+"/guarded-") && strings.HasPrefix(st.Detail, "Waiting: ") &&
			strings.Contains(st.Detail, "guarded-pdb")
	})
}

// drainJobEnd waits until a drain's job ended and returns it, its steps logged.
func drainJobEnd(ctx context.Context, t *testing.T, s *engine.Session, id string) *engine.JobTab {
	t.Helper()
	var job *engine.JobTab
	waitFor(ctx, t, "the drain's job to end", func() bool {
		tab, ok := s.UIState().DockTab(id)
		job = tab.Job
		return ok && job != nil && job.State.Ended()
	})
	for _, st := range job.Steps {
		t.Logf("  %s %s: %s", st.State, st.Text, st.Detail)
	}
	return job
}

// pickWorker returns the first worker by name; the drain cluster has two, so that the other one
// keeps running pods.
func pickWorker(ctx context.Context, t *testing.T, kube kubernetes.Interface) string {
	t.Helper()
	nodes, err := kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var workers []string
	for _, n := range nodes.Items {
		if _, cp := n.Labels["node-role.kubernetes.io/control-plane"]; !cp {
			workers = append(workers, n.Name)
		}
	}
	if len(workers) < 2 {
		t.Fatalf("%d workers: this is not the drain cluster (hack/kind/drain-cluster.yaml has two)", len(workers))
	}
	slices.Sort(workers)
	return workers[0]
}

func nodeUnschedulable(ctx context.Context, t *testing.T, kube kubernetes.Interface, name string) bool {
	t.Helper()
	n, err := kube.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return n.Spec.Unschedulable
}

// assertOnlyDaemonAndMirrorPods checks what a drain leaves on its node: DaemonSet pods, mirror
// pods, and pods already terminating.
func assertOnlyDaemonAndMirrorPods(ctx context.Context, t *testing.T, kube kubernetes.Interface, node string) {
	t.Helper()
	pods, err := kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + node})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pods.Items {
		if p.DeletionTimestamp != nil || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		if _, mirror := p.Annotations[corev1.MirrorPodAnnotationKey]; mirror {
			continue
		}
		if c := metav1.GetControllerOf(&p); c != nil && c.Kind == "DaemonSet" {
			continue
		}
		t.Errorf("pod %s/%s is still on the drained node %s", p.Namespace, p.Name, node)
	}
}
