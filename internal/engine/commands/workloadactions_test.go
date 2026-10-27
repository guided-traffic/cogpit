package commands_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// RestartWorkload is the fake writer's restart of a Daemon Set or Stateful Set.
func (w *fakeWriter) RestartWorkload(_ context.Context, resource, namespace, name, uid string) error {
	w.bound(uid)
	w.record(fmt.Sprintf("restart %s %s/%s", resource, namespace, name))
	return nil
}

// GVRs of the workload kinds these tests add.
var (
	daemonSetsGVR  = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}
	replicaSetsGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}
	jobsGVR        = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
)

// controlledBy makes obj's controller the owner of the given kind, name and UID.
func controlledBy(obj *unstructured.Unstructured, apiVersion, kind, name, uid string) *unstructured.Unstructured {
	yes := true
	obj.Object["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{
		"apiVersion": apiVersion, "kind": kind, "name": name, "uid": uid, "controller": yes,
	}}
	return obj
}

// seedWorkloads is seedActions with the workloads of the restart, scale and logs tests: the
// deployment web with its replica set and two pods, a daemon set with the OnDelete strategy and a
// pod, the stateful set db with a pod of two containers, a replica set no deployment controls,
// and a job with a pod.
func seedWorkloads() *testkit.MemorySource {
	src := seedActions()
	src.Set(replicaSetsGVR, controlledBy(testkit.Object("apps/v1", "ReplicaSet", "shop", "web-7d9f", map[string]any{
		"spec": map[string]any{"replicas": int64(3)},
	}), "apps/v1", "Deployment", "web", "uid-shop-web"))
	src.Set(replicaSetsGVR, testkit.Object("apps/v1", "ReplicaSet", "shop", "orphan", map[string]any{"spec": map[string]any{"replicas": int64(2)}}))
	src.Set(daemonSetsGVR, testkit.Object("apps/v1", "DaemonSet", "ops", "agent", map[string]any{
		"spec": map[string]any{"updateStrategy": map[string]any{"type": "OnDelete"}},
	}))
	src.Set(daemonSetsGVR, testkit.Object("apps/v1", "DaemonSet", "ops", "proxy", nil))
	src.Set(jobsGVR, testkit.Object("batch/v1", "Job", "ops", "migrate", nil))
	for _, p := range []struct{ name, kind, apiVersion, owner, ownerUID string }{
		{"web-7d9f-a1", "ReplicaSet", "apps/v1", "web-7d9f", "uid-shop-web-7d9f"},
		{"web-7d9f-b2", "ReplicaSet", "apps/v1", "web-7d9f", "uid-shop-web-7d9f"},
		{"db-0", "StatefulSet", "apps/v1", "db", "uid-shop-db"},
		{"orphan-c3", "ReplicaSet", "apps/v1", "orphan", "uid-shop-orphan"},
	} {
		src.Set(testkit.PodsGVR, controlledBy(testkit.Pod("shop", p.name), p.apiVersion, p.kind, p.owner, p.ownerUID))
	}
	src.Set(testkit.PodsGVR, controlledBy(testkit.Pod("ops", "agent-x9"), "apps/v1", "DaemonSet", "agent", "uid-ops-agent"))
	src.Set(testkit.PodsGVR, controlledBy(testkit.Pod("ops", "migrate-q4"), "batch/v1", "Job", "migrate", "uid-ops-migrate"))
	return src
}

// workloadProfiles are the action profiles plus the built-in Daemon Sets, Replica Sets and Jobs.
func workloadProfiles() []*model.KindProfile {
	out := actionProfiles()
	for _, key := range []string{"daemonsets", "replicasets", "jobs"} {
		p, ok := kinds.ByKey(key)
		if !ok {
			panic("no built-in profile " + key)
		}
		out = append(out, p)
	}
	return out
}

// workloadConnector is actionsConnector with the workload profiles.
type workloadConnector struct{ actionsConnector }

func (c *workloadConnector) Connect(ctx context.Context, name string) (*engine.Backend, error) {
	b, err := c.actionsConnector.Connect(ctx, name)
	if err != nil {
		return nil, err
	}
	b.Profiles = workloadProfiles()
	return b, nil
}

// newWorkloadHarness connects a session on seedWorkloads (or src) with writer w (nil: a fake
// writer of its own), every workload kind already in the cache.
func newWorkloadHarness(t testing.TB, src *testkit.MemorySource, w engine.Actions, opts engine.Options) *harness {
	t.Helper()
	if src == nil {
		src = seedWorkloads()
	}
	h := &harness{src: src, writer: &fakeWriter{}}
	if w == nil {
		w = h.writer
	}
	opts.Connector = &workloadConnector{actionsConnector{src: src, writer: w}}
	if opts.SnapshotInterval == 0 {
		opts.SnapshotInterval = 10 * time.Millisecond
	}
	h.s = engine.New(opts)
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"nodes", "pods", "deployments", "statefulsets", "daemonsets", "replicasets", "jobs", "cronjobs"} {
		if _, err := h.s.Objects(context.Background(), k, nil); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

// TestRestartDaemonSetsAndStatefulSets: Restart of a Daemon Set and a Stateful Set sends
// kubectl's restart for its kind, after a confirmation for every origin (owner, 2026-10-11: it
// replaces every pod), even when agent writes are allowed; a declined one writes nothing; the
// confirmation says when the OnDelete strategy delays the replacement.
func TestRestartDaemonSetsAndStatefulSets(t *testing.T) {
	audit := &syncBuffer{}
	h := newWorkloadHarness(t, nil, nil, engine.Options{Audit: audit})
	agent, db := actionRef("daemonsets", "ops", "agent"), actionRef("statefulsets", "shop", "db")

	for _, id := range []string{"daemonset.restart", "statefulset.restart"} {
		c, _ := h.s.Registry().Get(id)
		if c.Danger != engine.DangerDestructive || c.Order != 100 || c.Label != "Restart" || len(c.Verbs) != 1 || c.Verbs[0].Verb != "patch" {
			t.Errorf("%s: %+v", id, c)
		}
	}
	if err := h.run(t, engine.OriginGUI, "daemonset.restart", map[string]any{"ref": agent}, nil); !errors.Is(err, engine.ErrConfirmationUnavailable) {
		t.Errorf("restart without a GUI to confirm: %v", err)
	}
	c := &confirmations{answer: false}
	h.s.SetConfirmer(c)
	if err := h.run(t, engine.OriginGUI, "statefulset.restart", map[string]any{"ref": db}, nil); !errors.Is(err, engine.ErrDeclined) {
		t.Errorf("declined restart: %v", err)
	}
	if calls := h.writer.Calls(); len(calls) != 0 {
		t.Fatalf("writes without a confirmation: %v", calls)
	}

	c.answer = true
	h.s.SetAgentWrites("test", true)
	for _, origin := range []engine.Origin{engine.OriginGUI, engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
		before := len(c.all())
		var res commands.ActionResult
		h.must(t, origin, "daemonset.restart", map[string]any{"ref": agent}, &res)
		h.must(t, origin, "statefulset.restart", map[string]any{"ref": db}, nil)
		if res.Action != "restarted" || res.Ref.Context != "test" {
			t.Errorf("%s: result %+v", origin, res)
		}
		reqs := c.all()[before:]
		if len(reqs) != 2 || reqs[0].Danger != engine.DangerDestructive || reqs[0].Origin != origin || reqs[0].Command != "daemonset.restart" ||
			reqs[0].Objects[0].Name != "agent" || !slices.Equal(reqs[0].Namespaces, []string{"ops"}) || reqs[1].Command != "statefulset.restart" {
			t.Errorf("%s: confirmations %+v", origin, reqs)
		}
		if !strings.Contains(reqs[0].Detail, "OnDelete") || reqs[1].Detail != "" {
			t.Errorf("%s: details %q, %q", origin, reqs[0].Detail, reqs[1].Detail)
		}
	}
	want := slices.Repeat([]string{"restart daemonsets ops/agent", "restart statefulsets shop/db"}, 3)
	if got := h.writer.Calls(); !slices.Equal(got, want) {
		t.Errorf("writes %v, want %v", got, want)
	}
	var restarts int
	for _, l := range audit.Lines() {
		if strings.HasSuffix(l.Command, ".restart") && l.Phase == "done" && l.Error == "" {
			restarts++
		}
	}
	if restarts != 4 {
		t.Errorf("audited agent restarts: %d, want 4", restarts)
	}
}

// TestRestartRefusesARecreatedObject: a deployment, daemon set or stateful set deleted and created
// again under its name while the confirmation was open is not restarted: the user confirmed the
// old one.
func TestRestartRefusesARecreatedObject(t *testing.T) {
	for _, tc := range []struct {
		id   string
		gvr  schema.GroupVersionResource
		kind string
		ref  model.ObjectRef
	}{
		{"statefulset.restart", statefulSetsGVR, "StatefulSet", actionRef("statefulsets", "shop", "db")},
		{"daemonset.restart", daemonSetsGVR, "DaemonSet", actionRef("daemonsets", "ops", "proxy")},
		{"deployment.restart", testkit.DeploymentsGVR, "Deployment", actionRef("deployments", "shop", "web")},
	} {
		t.Run(tc.id, func(t *testing.T) {
			h := newWorkloadHarness(t, nil, nil, engine.Options{})
			c := &confirmations{answer: true, during: func(context.Context) {
				obj := testkit.Object("apps/v1", tc.kind, tc.ref.Namespace, tc.ref.Name, nil)
				obj.SetUID("uid-again")
				h.src.Set(tc.gvr, obj)
				waitRecreated(t, h, tc.ref)
			}}
			h.s.SetConfirmer(c)
			err := h.run(t, engine.OriginGUI, tc.id, map[string]any{"ref": tc.ref}, nil)
			if !errors.Is(err, engine.ErrTargetsChanged) {
				t.Errorf("restart of a recreated object: %v", err)
			}
			if calls := h.writer.Calls(); len(calls) != 0 {
				t.Errorf("writes %v", calls)
			}
		})
	}
}

// waitRecreated waits, in a confirmation's hook, until the cache shows ref created again with the
// UID uid-again; t.Error, not Fatal: the hook runs inside the command.
func waitRecreated(t *testing.T, h *harness, ref model.ObjectRef) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		o, err := h.s.Lookup(ref)
		return err == nil && o.GetUID() == "uid-again"
	}) {
		t.Errorf("the cache never showed %s created again", ref.Name)
	}
}

// TestScaleToZeroRefusesARecreatedObject: Scale to zero is confirmed as destructive, so it binds
// to the object the user confirmed like Restart does: a replica set, deployment or stateful set
// deleted and created again under its name while the dialog was open is not scaled to zero. A
// scale to a non-zero count is mutating and binds to nothing.
func TestScaleToZeroRefusesARecreatedObject(t *testing.T) {
	for _, tc := range []struct {
		id   string
		gvr  schema.GroupVersionResource
		kind string
		ref  model.ObjectRef
	}{
		{"replicaset.scale", replicaSetsGVR, "ReplicaSet", actionRef("replicasets", "shop", "orphan")},
		{"statefulset.scale", statefulSetsGVR, "StatefulSet", actionRef("statefulsets", "shop", "db")},
	} {
		t.Run(tc.id, func(t *testing.T) {
			h := newWorkloadHarness(t, nil, nil, engine.Options{})
			c := &confirmations{answer: true, during: func(context.Context) {
				obj := testkit.Object("apps/v1", tc.kind, tc.ref.Namespace, tc.ref.Name, map[string]any{"spec": map[string]any{"replicas": int64(2)}})
				obj.SetUID("uid-again")
				h.src.Set(tc.gvr, obj)
				waitRecreated(t, h, tc.ref)
			}}
			h.s.SetConfirmer(c)
			err := h.run(t, engine.OriginGUI, tc.id, scaleTo(tc.ref, 0), nil)
			if !errors.Is(err, engine.ErrTargetsChanged) {
				t.Errorf("scale to zero of a recreated object: %v", err)
			}
			if calls := h.writer.Calls(); len(calls) != 0 {
				t.Errorf("writes %v", calls)
			}
			// A non-zero scale needs no confirmation from the GUI: it acts on the object the cache holds.
			h.must(t, engine.OriginGUI, tc.id, scaleTo(tc.ref, 3), nil)
		})
	}
}

// TestRestartAndScaleDisabled: each command says why it cannot run, and does not apply at all to
// another kind (the menus hide it).
func TestRestartAndScaleDisabled(t *testing.T) {
	h := newWorkloadHarness(t, nil, nil, engine.Options{})
	reason := func(id string, ref model.ObjectRef) error {
		cmd, _ := h.s.Registry().Get(id)
		return cmd.Enabled(h.s, cmd.ObjectParams(ref, ""))
	}
	for id, ref := range map[string]model.ObjectRef{
		"daemonset.restart":   actionRef("daemonsets", "ops", "agent"),
		"statefulset.restart": actionRef("statefulsets", "shop", "db"),
		"replicaset.scale":    actionRef("replicasets", "shop", "orphan"),
	} {
		if err := reason(id, ref); err != nil {
			t.Errorf("%s: %v", id, err)
		}
		if err := reason(id, actionRef("deployments", "shop", "web")); !errors.Is(err, engine.ErrNotApplicable) {
			t.Errorf("%s on a deployment: %v", id, err)
		}
		gone := ref
		gone.Name = "gone"
		if err := reason(id, gone); !errors.Is(err, engine.ErrNotFound) {
			t.Errorf("%s on a missing object: %v", id, err)
		}
	}

	// A connection whose writer cannot restart, and the demo cluster.
	type scaleOnly struct{ engine.WorkloadActions }
	noRestart := newWorkloadHarness(t, nil, struct {
		engine.Actions
		scaleOnly
	}{&fakeWriter{}, scaleOnly{&fakeWriter{}}}, engine.Options{})
	cmd, _ := noRestart.s.Registry().Get("daemonset.restart")
	if err := cmd.Enabled(noRestart.s, cmd.ObjectParams(actionRef("daemonsets", "ops", "agent"), "")); err == nil ||
		!strings.Contains(err.Error(), "cannot restart workloads") {
		t.Errorf("restart without a restarting writer: %v", err)
	}

	src := seedWorkloads()
	s := engine.New(engine.Options{Connector: demoWorkloads{src}})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	ref := actionRef("statefulsets", "shop", "db")
	cmd, _ = s.Registry().Get("statefulset.restart")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { _, err := s.Lookup(ref); return err == nil }) {
		t.Fatalf("the demo cluster's %s never reached the cache", ref.Name)
	}
	if _, err := s.Objects(context.Background(), "statefulsets", nil); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Enabled(s, cmd.ObjectParams(ref, "")); err == nil || !strings.Contains(err.Error(), "demo cluster cannot be changed") {
		t.Errorf("restart on the demo cluster: %v", err)
	}
}

// demoWorkloads connects a demo-like backend: no connection, no writer.
type demoWorkloads struct{ src *testkit.MemorySource }

func (demoWorkloads) Contexts() ([]engine.ContextInfo, error) {
	return []engine.ContextInfo{{Name: "demo", Demo: true}}, nil
}

func (d demoWorkloads) Connect(context.Context, string) (*engine.Backend, error) {
	c := cache.New(d.src, cache.Options{WarmPeriod: time.Minute})
	return &engine.Backend{Context: "demo", Cache: c, Profiles: workloadProfiles(), Close: c.Close, Demo: true}, nil
}

// TestReplicaSetScale: Scale of a replica set patches its scale subresource, prefilled from the
// cache, escalates to destructive at zero for every origin, and warns when a Deployment controls
// the replica set (its controller sets the count back).
func TestReplicaSetScale(t *testing.T) {
	ev := &eventLog{}
	h := newWorkloadHarness(t, nil, nil, engine.Options{})
	h.s.Subscribe(func(e engine.Event) {
		if e.Type == engine.EventToast {
			ev.mu.Lock()
			ev.events = append(ev.events, e)
			ev.mu.Unlock()
		}
	})
	orphan, owned := actionRef("replicasets", "shop", "orphan"), actionRef("replicasets", "shop", "web-7d9f")
	cmd, _ := h.s.Registry().Get("replicaset.scale")
	if cmd.Order != 100 || !cmd.Escalates || cmd.Danger != engine.DangerMutating || cmd.Verbs[0].Subresource != "scale" || cmd.Verbs[0].Resource != "replicasets" {
		t.Errorf("replicaset.scale: %+v", cmd)
	}
	raw, err := cmd.Prefill(h.s, mustJSON(t, map[string]any{"ref": orphan}))
	if err != nil || !strings.Contains(string(raw), `"replicas":2`) {
		t.Errorf("prefill %s (%v)", raw, err)
	}

	h.must(t, engine.OriginGUI, "replicaset.scale", scaleTo(orphan, 4), nil)
	if toasts := ev.of(engine.EventToast); len(toasts) != 1 || toasts[0].Level != engine.LevelInfo {
		t.Errorf("toasts after scaling a replica set of its own: %+v", toasts)
	}
	h.must(t, engine.OriginGUI, "replicaset.scale", scaleTo(owned, 5), nil)
	ev.waitEvent(t, engine.EventToast, "controlled by deployment web")

	if err := h.run(t, engine.OriginGUI, "replicaset.scale", scaleTo(orphan, 0), nil); !errors.Is(err, engine.ErrConfirmationUnavailable) {
		t.Errorf("scale to zero without a confirmation: %v", err)
	}
	c := &confirmations{answer: true}
	h.s.SetConfirmer(c)
	h.s.SetAgentWrites("test", true)
	h.must(t, engine.OriginExternalAgent, "replicaset.scale", scaleTo(orphan, 0), nil)
	if req := c.last(t); req.Danger != engine.DangerDestructive || req.Command != "replicaset.scale" {
		t.Errorf("confirmation %+v", req)
	}
	want := []string{"scale replicasets shop/orphan 4", "scale replicasets shop/web-7d9f 5", "scale replicasets shop/orphan 0"}
	if got := h.writer.Calls(); !slices.Equal(got, want) {
		t.Errorf("writes %v", got)
	}
}

// TestWorkloadActionOrders: the menus as Lens documents them: Daemon Set Restart; Stateful Set
// Restart, Scale; Replica Set Scale; then Edit and Delete (Logs come with workload.logs).
func TestWorkloadActionOrders(t *testing.T) {
	r := engine.NewRegistry()
	if err := commands.RegisterAll(r); err != nil {
		t.Fatal(err)
	}
	for kind, want := range map[string][]string{
		"daemonsets":   {"daemonset.restart"},
		"statefulsets": {"statefulset.restart", "statefulset.scale"},
		"replicasets":  {"replicaset.scale"},
	} {
		var got []string
		for _, c := range r.Actions(kind) {
			if c.Order < engine.OrderEdit && !c.DrawerOnly && c.ID != "workload.logs" {
				got = append(got, c.ID)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", kind, got, want)
		}
	}
}
