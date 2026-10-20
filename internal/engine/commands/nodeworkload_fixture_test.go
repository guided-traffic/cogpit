package commands_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The fake writer's node and workload writes, for the shared harness (the fuzz target) and as the
// base of actionsWriter. They record each call.

func (w *fakeWriter) SetNodeUnschedulable(_ context.Context, name string, unschedulable bool) error {
	if unschedulable {
		w.record("cordon " + name)
	} else {
		w.record("uncordon " + name)
	}
	return nil
}

func (w *fakeWriter) DrainNode(_ context.Context, name string, _ cluster.DrainOptions, progress func(cluster.DrainProgress)) error {
	w.record("drain " + name)
	if progress != nil {
		progress(cluster.DrainProgress{Step: cluster.DrainCordoned, Node: name})
		progress(cluster.DrainProgress{Step: cluster.DrainListed, Node: name})
	}
	return nil
}

func (w *fakeWriter) ScaleWorkload(_ context.Context, resource, namespace, name, uid string, replicas int32) error {
	w.bound(uid)
	w.record(fmt.Sprintf("scale %s %s/%s %d", resource, namespace, name, replicas))
	return nil
}

func (w *fakeWriter) TriggerCronJob(_ context.Context, cj *unstructured.Unstructured) (string, error) {
	w.record(fmt.Sprintf("trigger %s/%s %s", cj.GetNamespace(), cj.GetName(), cj.GetUID()))
	return cj.GetName() + "-manual-x7k2p", nil
}

func (w *fakeWriter) SetCronJobSuspend(_ context.Context, namespace, name string, suspend bool) error {
	w.record(fmt.Sprintf("suspend %s/%s %t", namespace, name, suspend))
	return nil
}

func (w *fakeWriter) EvictPod(_ context.Context, namespace, name, uid string) error {
	w.record(fmt.Sprintf("evict %s/%s %s", namespace, name, uid))
	return nil
}

// GVRs of the kinds these tests add to testkit's.
var (
	statefulSetsGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}
	cronJobsGVR     = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}
)

func actionNode(name string, unschedulable bool) *unstructured.Unstructured {
	spec := map[string]any{}
	if unschedulable {
		spec["unschedulable"] = true
	}
	return testkit.Object("v1", "Node", "", name, map[string]any{
		"spec":   spec,
		"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
	})
}

// seedActions is the cluster of the node and workload tests: a schedulable and a cordoned node,
// a deployment with and one without spec.replicas, a stateful set, a running and a suspended cron
// job, and pods.
func seedActions() *testkit.MemorySource {
	src := testkit.NewMemorySource()
	src.Set(testkit.NodesGVR, actionNode("worker-1", false))
	src.Set(testkit.NodesGVR, actionNode("worker-2", true))
	for _, ns := range []string{"shop", "ops"} {
		src.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", ns, nil))
	}
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-0"))
	src.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", "shop", "web", map[string]any{"spec": map[string]any{"replicas": int64(3)}}))
	src.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", "shop", "defaulted", nil))
	src.Set(statefulSetsGVR, testkit.Object("apps/v1", "StatefulSet", "shop", "db", map[string]any{"spec": map[string]any{"replicas": int64(2)}}))
	src.Set(cronJobsGVR, testkit.Object("batch/v1", "CronJob", "ops", "backup", map[string]any{"spec": map[string]any{
		"schedule": "0 3 * * *", "jobTemplate": map[string]any{"spec": map[string]any{}},
	}}))
	src.Set(cronJobsGVR, testkit.Object("batch/v1", "CronJob", "ops", "nightly", map[string]any{"spec": map[string]any{
		"schedule": "0 1 * * *", "suspend": true, "jobTemplate": map[string]any{"spec": map[string]any{}},
	}}))
	return src
}

// actionProfiles are the shared test profiles plus Nodes (the built-in profile) and test profiles
// for Stateful Sets and Cron Jobs, whose built-in profiles come with another change.
func actionProfiles() []*model.KindProfile {
	nodes, _ := kinds.ByKey("nodes")
	return append(testProfiles(), nodes,
		profile("statefulsets", "apps", "StatefulSet", "Stateful Sets", true),
		profile("cronjobs", "batch", "CronJob", "Cron Jobs", true))
}

// actionsConnector connects "test" and "other" to fresh caches over one source, with the action
// profiles and the given writer; nothing here dials a cluster.
type actionsConnector struct {
	src    *testkit.MemorySource
	writer engine.Actions
	demo   bool // a demo cluster: no connection, no writer
}

func (c *actionsConnector) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "other"}, {Name: "test", Current: true}}, nil
}

func (c *actionsConnector) Connect(_ context.Context, name string) (*engine.Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	b := &engine.Backend{Context: name, Namespace: "default", Cache: ch, Profiles: actionProfiles(), Close: ch.Close, Demo: c.demo}
	if !c.demo {
		b.Conn, b.Actions = &cluster.Conn{}, c.writer
	}
	return b, nil
}

// actionsWriter is the fake writer of these tests: it applies cordon and uncordon to the source,
// so that the cache sees them, and lets a test take over a drain or fail an eviction.
type actionsWriter struct {
	*fakeWriter
	src *testkit.MemorySource

	mu        sync.Mutex
	drainHook func(ctx context.Context, name string, progress func(cluster.DrainProgress)) error
	drainOpts []cluster.DrainOptions
	evictErr  error
}

func (w *actionsWriter) SetNodeUnschedulable(ctx context.Context, name string, unschedulable bool) error {
	_ = w.fakeWriter.SetNodeUnschedulable(ctx, name, unschedulable)
	w.src.Set(testkit.NodesGVR, actionNode(name, unschedulable))
	return nil
}

// waitCordoned waits until the session's cache shows the node cordoned (or not): the writer applies
// a cordon to the source, which reaches the cache through its watch a moment later, and the next
// node command's Enabled reads the cache, as the window's does (a test that ran Uncordon at once
// after a Cordon failed now and then: "worker-1 is not cordoned").
func waitCordoned(t *testing.T, h *harness, node string, cordoned bool) {
	t.Helper()
	id, state := "node.cordon", "uncordoned"
	if cordoned {
		id, state = "node.uncordon", "cordoned"
	}
	cmd, ok := h.s.Registry().Get(id)
	if !ok {
		t.Fatalf("no command %s", id)
	}
	params := mustJSON(t, actionNodeRef(node))
	waitUntil(t, "the cache to show "+node+" "+state, func() bool { return cmd.Enabled(h.s, params) == nil })
}

// DrainNode cordons the node, then runs the test's hook, or evicts two pods.
func (w *actionsWriter) DrainNode(ctx context.Context, name string, opts cluster.DrainOptions, progress func(cluster.DrainProgress)) error {
	w.mu.Lock()
	hook := w.drainHook
	w.drainOpts = append(w.drainOpts, opts)
	w.mu.Unlock()
	w.record("drain " + name)
	_ = w.SetNodeUnschedulable(ctx, name, true)
	progress(cluster.DrainProgress{Step: cluster.DrainCordoned, Node: name})
	if hook != nil {
		return hook(ctx, name, progress)
	}
	progress(cluster.DrainProgress{Step: cluster.DrainListed, Node: name, Total: 2, Message: "ignoring DaemonSet-managed Pods: kube-system/proxy-1"})
	progress(cluster.DrainProgress{Step: cluster.DrainEvicted, Node: name, Pod: "shop/web-0", Done: 1, Total: 2})
	progress(cluster.DrainProgress{Step: cluster.DrainEvicted, Node: name, Pod: "shop/web-1", Done: 2, Total: 2})
	return nil
}

func (w *actionsWriter) EvictPod(ctx context.Context, namespace, name, uid string) error {
	_ = w.fakeWriter.EvictPod(ctx, namespace, name, uid)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.evictErr
}

func (w *actionsWriter) setDrainHook(fn func(ctx context.Context, name string, progress func(cluster.DrainProgress)) error) {
	w.mu.Lock()
	w.drainHook = fn
	w.mu.Unlock()
}

func (w *actionsWriter) lastDrainOpts() cluster.DrainOptions {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.drainOpts) == 0 {
		return cluster.DrainOptions{}
	}
	return w.drainOpts[len(w.drainOpts)-1]
}

// newActionsHarness connects a session on seedActions (or src) to "test", with every kind the
// actions need already in the cache, so that Enabled checks see them.
func newActionsHarness(t testing.TB, src *testkit.MemorySource, opts engine.Options) (*harness, *actionsWriter) {
	t.Helper()
	if src == nil {
		src = seedActions()
	}
	w := &actionsWriter{fakeWriter: &fakeWriter{}, src: src}
	opts.Connector = &actionsConnector{src: src, writer: w}
	if opts.SnapshotInterval == 0 {
		opts.SnapshotInterval = 10 * time.Millisecond
	}
	s := engine.New(opts)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	warmActionKinds(t, s)
	return &harness{s: s, src: src, writer: w.fakeWriter}, w
}

func warmActionKinds(t testing.TB, s *engine.Session) {
	t.Helper()
	for _, k := range []string{"nodes", "pods", "deployments", "statefulsets", "cronjobs"} {
		if _, err := s.Objects(context.Background(), k, nil); err != nil {
			t.Fatal(err)
		}
	}
}

// confirmations records the confirmation requests and answers them.
type confirmations struct {
	mu     sync.Mutex
	reqs   []engine.ConfirmRequest
	answer bool
	during func(context.Context) // runs while the "dialog" is open
}

func (c *confirmations) Confirm(ctx context.Context, req engine.ConfirmRequest) (bool, error) {
	c.mu.Lock()
	c.reqs = append(c.reqs, req)
	answer, during := c.answer, c.during
	c.mu.Unlock()
	if during != nil {
		during(ctx)
	}
	return answer, nil
}

func (c *confirmations) all() []engine.ConfirmRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]engine.ConfirmRequest(nil), c.reqs...)
}

func (c *confirmations) last(t testing.TB) engine.ConfirmRequest {
	t.Helper()
	all := c.all()
	if len(all) == 0 {
		t.Fatal("no confirmation was asked")
	}
	return all[len(all)-1]
}

// eventLog records the events of a session.
type eventLog struct {
	mu     sync.Mutex
	events []engine.Event
}

func recordEvents(s *engine.Session) *eventLog {
	l := &eventLog{}
	s.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventDrains || ev.Type == engine.EventToast {
			l.mu.Lock()
			l.events = append(l.events, ev)
			l.mu.Unlock()
		}
	})
	return l
}

func (l *eventLog) of(typ engine.EventType) []engine.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []engine.Event
	for _, ev := range l.events {
		if ev.Type == typ {
			out = append(out, ev)
		}
	}
	return out
}

// waitEvent waits until an event of the type whose message contains want arrived.
func (l *eventLog) waitEvent(t testing.TB, typ engine.EventType, want string) engine.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var got engine.Event
	if !testkit.WaitFor(ctx, func() bool {
		for _, ev := range l.of(typ) {
			if strings.Contains(ev.Message, want) {
				got = ev
				return true
			}
		}
		return false
	}) {
		t.Fatalf("no %s event with %q; events: %+v", typ, want, l.of(typ))
	}
	return got
}

func actionNodeRef(name string) map[string]any {
	return map[string]any{"ref": model.ObjectRef{Kind: "nodes", Name: name}}
}
