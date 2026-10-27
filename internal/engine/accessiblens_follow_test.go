package engine

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// contextOnly is a cluster whose user has a RoleBinding in the context's own namespace team-a and
// nothing else: pods, services, deployments and replica sets there, refused in all namespaces and
// in team-b. The rules review of team-a allows them and no accessible namespaces are named, so a
// plan reads them in all namespaces first, and the API server refuses that. It holds the
// Deployment web with its replica set and its ready pod web-1-a (label app=web, port http 8080),
// and the Service web in front of it.
func contextOnly(t *testing.T) (*testkit.NamespacedOnlySource, *fakeReviewer) {
	t.Helper()
	mem := testkit.NewMemorySource()
	mem.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", "team-a", "web", nil))
	mem.Set(replicaSetsGVR, owned(testkit.Object("apps/v1", "ReplicaSet", "team-a", "web-1", nil), "apps/v1", "Deployment", "web", "uid-team-a-web"))
	mem.Set(testkit.PodsGVR, owned(readyWebPod("team-a", "web-1-a"), "apps/v1", "ReplicaSet", "web-1", "uid-team-a-web-1"))
	mem.Set(testkit.ServicesGVR, testkit.Object("v1", "Service", "team-a", "web", map[string]any{
		"spec": map[string]any{"selector": map[string]any{"app": "web"}, "ports": []any{map[string]any{"port": int64(80), "targetPort": "http"}}},
	}))
	src := &testkit.NamespacedOnlySource{Inner: mem, Denied: []string{"team-b"}, GVRs: []schema.GroupVersionResource{
		testkit.PodsGVR, testkit.ServicesGVR, testkit.DeploymentsGVR, replicaSetsGVR,
	}}
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"team-a": {
		rule("get,list,watch", "", "pods,services"), rule("get,list,watch", "apps", "deployments,replicasets"),
	}})
	return src, rev
}

// readyWebPod is a running, ready pod labelled app=web with the container app and its port http
// 8080: a target of the Service web.
func readyWebPod(namespace, name string) *unstructured.Unstructured {
	return testkit.Object("v1", "Pod", namespace, name, map[string]any{
		"metadata": map[string]any{"labels": map[string]any{"app": "web"}},
		"spec": map[string]any{"containers": []any{map[string]any{
			"name": "app", "image": "registry.example/app:1",
			"ports": []any{map[string]any{"name": "http", "containerPort": int64(8080)}},
		}}},
		"status": map[string]any{
			"phase":             "Running",
			"conditions":        []any{map[string]any{"type": "Ready", "status": "True"}},
			"containerStatuses": []any{map[string]any{"name": "app", "restartCount": int64(0), "state": map[string]any{"running": map[string]any{}}}},
		},
	})
}

func contextOnlySession(t *testing.T, src *testkit.NamespacedOnlySource, rev *fakeReviewer, actions Actions) *Session {
	t.Helper()
	rs, ok := kinds.ByKey("replicasets")
	if !ok {
		t.Fatal("no replica sets profile")
	}
	s := New(Options{Connector: dockConnector{src: src, rev: rev, profiles: append(testProfiles(), rs), actions: actions, namespace: "team-a"},
		SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	return s
}

// callsOf are the source's list and watch calls of one resource, by scope ("list services",
// "watch services in team-a").
func callsOf(src *testkit.NamespacedOnlySource, resource string) map[string]int {
	out := map[string]int{}
	for k, v := range src.Calls() {
		if strings.Contains(k, " "+resource) {
			out[k] = v
		}
	}
	return out
}

// A user whose RoleBinding is in the context's own namespace only: the plan reads a kind in all
// namespaces first (the rules allow it in team-a, nothing else is reviewed), and the API server
// refuses. The reads that wait for their target (one object, a Deployment's logs, a Service's port
// forward) follow that refusal as the views do: they read the target's namespace alone at once,
// instead of waiting 15 seconds for a list that never comes and failing. The refusal is kept, so
// the next read and the kind's view go to the namespace without trying all namespaces again.
func TestTargetReadsFollowARefusedClusterWideList(t *testing.T) {
	src, rev := contextOnly(t)
	actions := &dockActions{}
	s := contextOnlySession(t, src, rev, actions)

	// One object: the Deployment, through the read that agents and the CLI use.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	obj, err := s.Object(ctx, model.ObjectRef{Kind: "deployments", Namespace: "team-a", Name: "web"})
	if err != nil || obj.GetName() != "web" {
		t.Fatalf("Object: %v, %v (calls %v)", obj, err, callsOf(src, "deployments"))
	}
	if calls := callsOf(src, "deployments"); calls["list deployments.apps"] != 1 || calls["list deployments.apps in team-a"] != 1 {
		t.Errorf("deployment calls %v: want the refused list in all namespaces once, then team-a's", calls)
	}

	// The Deployment's logs: its replica sets are read in team-a after one refusal.
	f := newFakeLogs()
	f.content["web-1-a/app"] = "hello\n"
	tab, _, err := s.OpenWorkloadLogsTab(ctx, model.ObjectRef{Kind: "deployments", Namespace: "team-a", Name: "web"})
	if err != nil {
		t.Fatalf("open the tab: %v", err)
	}
	type stream struct {
		rc  io.ReadCloser
		err error
	}
	opened := make(chan stream, 1)
	go func() {
		dt, b, err := s.streamTab(tab.ID, DockLogs)
		if err != nil {
			opened <- stream{err: err}
			return
		}
		rc, err := s.openLogs(ctx, dt, b, f)
		opened <- stream{rc, err}
	}()
	select {
	case o := <-opened:
		if o.err != nil {
			t.Fatalf("the logs: %v (calls %v)", o.err, callsOf(src, "replicasets"))
		}
		t.Cleanup(func() { _ = o.rc.Close() })
		readLines(o.rc).wait(t, "web-1-a/app hello")
	case <-time.After(10 * time.Second):
		t.Fatalf("the logs never opened; replica set calls %v", callsOf(src, "replicasets"))
	}
	if calls := callsOf(src, "replicasets"); calls["list replicasets.apps"] != 1 || calls["list replicasets.apps in team-a"] != 1 {
		t.Errorf("replica set calls %v: want one refused list in all namespaces, then team-a's", calls)
	}

	// A Service's port forward: the services are read in team-a after one refusal.
	fw, err := s.StartForward(ctx, ForwardSpec{Target: model.ObjectRef{Kind: "services", Namespace: "team-a", Name: "web"}, Port: 80})
	if err != nil || fw.Phase != ForwardActive || !slices.Equal(actions.dialed(), []string{"team-a/web-1-a"}) {
		t.Fatalf("forward %+v, %v; dials %v; service calls %v", fw, err, actions.dialed(), callsOf(src, "services"))
	}
	if calls := callsOf(src, "services"); calls["list services"] != 1 || calls["list services in team-a"] != 1 {
		t.Errorf("service calls %v: want one refused list in all namespaces, then team-a's", calls)
	}
	if _, err := s.StopForward(fw.Ref); err != nil {
		t.Error(err)
	}

	// The refusal is kept: the Deployments view reads team-a at once, without a second try in all
	// namespaces.
	s.UpdateUI(func(u *UIState) { u.View = "deployments" })
	ts := waitTable(t, s, "deployments", func(ts *TableSnapshot) bool { return ts.Synced && len(ts.Rows) == 1 })
	if !slices.Equal(ts.Watched, []string{"team-a"}) || ts.Error != "" {
		t.Errorf("the Deployments view: watched %q, error %q", ts.Watched, ts.Error)
	}
	if calls := callsOf(src, "deployments"); calls["list deployments.apps"] != 1 {
		t.Errorf("deployment calls %v: the view tried all namespaces again", calls)
	}
}

// An agent's read of one object (object_get, cogpit-cli get: DetailWait) reads the object's own
// namespace, and its events there, as Session.Object does. The user may read pods, deployments and
// events in shop alone, and the namespaces the plan names (the context's own, default) miss it:
// before, the read took the plan's namespaces and said the object was missing. Nothing is read in
// all namespaces, and no other namespace.
func TestDetailWaitReadsTheObjectsNamespace(t *testing.T) {
	src, rev := shopOnly(t)
	src.GVRs = append(src.GVRs, testkit.EventsGVR)
	src.Inner.Set(testkit.EventsGVR, event("shop", "e1", "Pod", "web-1-a", "", "Started", "Normal", time.Now()))
	rev.set(func(f *fakeReviewer) { f.rules["shop"] = append(f.rules["shop"], rule("get,list,watch", "", "events")) })
	s := shopSession(t, src, rev, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	m, err := s.DetailWait(ctx, model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-1-a"})
	if err != nil || m.Missing || m.Loading {
		t.Fatalf("DetailWait: %+v, %v (pod calls %v)", m, err, podCalls(src))
	}
	if len(m.Events) != 1 || m.Events[0].Reason != "Started" {
		t.Errorf("events %+v", m.Events)
	}
	m, err = s.DetailWait(ctx, model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"})
	if err != nil || m.Missing || m.Loading {
		t.Fatalf("DetailWait of the Deployment: %+v, %v", m, err)
	}
	for _, res := range []string{"pods", "deployments", "events"} {
		for k, n := range callsOf(src, res) {
			if n > 0 && !strings.HasSuffix(k, " in shop") {
				t.Errorf("%s read outside shop: %v", res, callsOf(src, res))
			}
		}
	}
	if calls := callsOf(src, "events"); calls["list events in shop"] != 1 {
		t.Errorf("event calls %v: want shop's list once", calls)
	}
}

// The GUI's drawer reads its object's namespace too: the user may read pods in shop alone and the
// plan names default, so the drawer, which read the plan's namespaces, showed the pod as missing.
// A change of the namespace filter that leaves shop read as it was does not start it again.
func TestDrawerReadsTheObjectsNamespace(t *testing.T) {
	src, rev := shopOnly(t)
	s := shopSession(t, src, rev, nil)
	ref := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-1-a"}
	waitTable(t, s, "pods", func(ts *TableSnapshot) bool { return ts.Synced && slices.Equal(ts.Denied, []string{"default"}) })
	s.UpdateUI(func(u *UIState) { u.Drawer = &ref })
	var m *DetailModel
	waitCond(t, "the drawer's pod", func() bool {
		var err error
		m, err = s.Detail(ref)
		return err == nil && !m.Loading && !m.Missing
	})
	if m.Title != "Pod: web-1-a" {
		t.Errorf("title %q", m.Title)
	}
	d := s.state.drawer
	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"shop"} })
	waitTable(t, s, "pods", func(ts *TableSnapshot) bool { return ts.Synced && slices.Equal(ts.Watched, []string{"shop"}) })
	s.state.mu.Lock()
	same := s.state.drawer == d
	s.state.mu.Unlock()
	if !same {
		t.Error("a filter change started the drawer again")
	}
	for k, n := range podCalls(src) {
		if n > 0 && !strings.HasSuffix(k, " in shop") {
			t.Errorf("pods read outside shop: %v", podCalls(src))
		}
	}
}

// A view's related-objects index follows a refusal too: the Services view reads the endpoint
// slices for its Service drawers' sections, and a user whose RoleBinding is in the context's
// namespace alone is refused both kinds in all namespaces. The view, which waits for its indexes
// before it says it has synced, read the slices again only when the Services' own plan changed:
// a slice read refused afterwards kept the view loading for as long as it was open.
func TestViewIndexFollowsARefusal(t *testing.T) {
	sliceGVR := schema.GroupVersionResource{Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices"}
	mem := testkit.NewMemorySource()
	mem.Set(testkit.ServicesGVR, testkit.Object("v1", "Service", "team-a", "web", nil))
	mem.Set(sliceGVR, testkit.Object("discovery.k8s.io/v1", "EndpointSlice", "team-a", "web-abc", map[string]any{
		"metadata":    map[string]any{"labels": map[string]any{"kubernetes.io/service-name": "web"}},
		"addressType": "IPv4",
	}))
	src := &testkit.NamespacedOnlySource{Inner: mem, GVRs: []schema.GroupVersionResource{testkit.ServicesGVR, sliceGVR}}
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"team-a": {
		rule("get,list,watch", "", "services"), rule("get,list,watch", "discovery.k8s.io", "endpointslices"),
	}})
	var profiles []*model.KindProfile
	for _, key := range []string{"services", "endpointslices"} {
		profiles = append(profiles, builtinProfile(t, key))
	}
	// The slices' answers come later than the Services': the view has followed its own refusal
	// before the slices' refusal arrives.
	slow := testkit.SlowLists{Inner: src, GVR: sliceGVR, Delay: 300 * time.Millisecond}
	s := New(Options{Connector: dockConnector{src: slow, rev: rev, profiles: profiles, namespace: "team-a"}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	s.UpdateUI(func(u *UIState) { u.View = "services" })
	ts := waitTable(t, s, "services", func(ts *TableSnapshot) bool { return ts.Synced && len(ts.Rows) == 1 })
	if !slices.Equal(ts.Watched, []string{"team-a"}) || ts.Error != "" {
		t.Errorf("watched %q, error %q", ts.Watched, ts.Error)
	}
	waitCond(t, "the refused scopes stopped", func() bool {
		for _, st := range s.Backend().Cache.Stats() {
			if st.Scope.Namespace == "" {
				return false
			}
		}
		return true
	})
	if calls := callsOf(src, "endpointslices"); calls["list endpointslices.discovery.k8s.io in team-a"] != 1 {
		t.Errorf("slice calls %v: want team-a's after the refusal", calls)
	}
}

// The reads behind the menus' checks (Lookup) and behind Edit, Revert and Delete (the write's
// target, cachedObject) read the target's namespace too: the user may read pods and deployments in
// shop alone, and the plan names default. Before, both said the object did not exist. For a user
// whose RoleBinding is in the context's namespace alone, the write's target follows the refusal in
// all namespaces at once instead of waiting 15 seconds.
func TestLookupsAndWriteTargetsReadTheTargetsNamespace(t *testing.T) {
	src, rev := shopOnly(t)
	s := shopSession(t, src, rev, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	waitCond(t, "Lookup of the pod in shop", func() bool {
		obj, err := s.Lookup(model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-1-a"})
		return err == nil && obj.GetName() == "web-1-a"
	})
	tg, b, err := s.ResolveTarget(ctx, model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if obj, err := cachedObject(ctx, b, tg); err != nil || obj.GetName() != "web" {
		t.Fatalf("the write's target: %v, %v", obj, err)
	}
	for _, res := range []string{"pods", "deployments"} {
		for k, n := range callsOf(src, res) {
			if n > 0 && !strings.HasSuffix(k, " in shop") {
				t.Errorf("%s read outside shop: %v", res, callsOf(src, res))
			}
		}
	}

	src2, rev2 := contextOnly(t)
	s2 := contextOnlySession(t, src2, rev2, nil)
	tg, b, err = s2.ResolveTarget(ctx, model.ObjectRef{Kind: "replicasets", Namespace: "team-a", Name: "web-1"})
	if err != nil {
		t.Fatal(err)
	}
	if obj, err := cachedObject(ctx, b, tg); err != nil || obj.GetName() != "web-1" {
		t.Fatalf("the write's target after a refusal: %v, %v (calls %v)", obj, err, callsOf(src2, "replicasets"))
	}
	if calls := callsOf(src2, "replicasets"); calls["list replicasets.apps"] != 1 || calls["list replicasets.apps in team-a"] != 1 {
		t.Errorf("replica set calls %v: want one refused list in all namespaces, then team-a's", calls)
	}
}

// The Applications view reads its workloads through a cache of its own; for a user whose
// RoleBinding is in the context's namespace (shop) alone, its reads of the workloads in all
// namespaces are refused, and before (2026-10-13) it kept them for as long as it ran and listed no
// application. Its run now takes a workload kind again when how the kind is read changes, so the
// view lists shop's application.
func TestApplicationsFollowARefusal(t *testing.T) {
	mem := testkit.NewMemorySource()
	mem.Set(testkit.DeploymentsGVR, appDeployment("shop", "web", "storefront", "1.4", 1))
	mem.Set(appsStatefulSetsGVR, appStatefulSet(2))
	mem.Set(testkit.DeploymentsGVR, appDeployment("payments", "api", "storefront", "2.0", 1))
	gvrs := []schema.GroupVersionResource{testkit.PodsGVR, testkit.DeploymentsGVR, appsStatefulSetsGVR, appsJobsGVR, appsCronJobsGVR}
	src := &testkit.NamespacedOnlySource{Inner: mem, GVRs: gvrs, Denied: []string{"payments"}}
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"shop": {
		rule("get,list,watch", "", "pods"), rule("get,list,watch", "apps", "deployments,statefulsets"), rule("get,list,watch", "batch", "jobs,cronjobs"),
	}})
	var profiles []*model.KindProfile
	for _, key := range []string{"pods", "deployments", "statefulsets", "jobs", "cronjobs"} {
		profiles = append(profiles, builtinProfile(t, key))
	}
	s := New(Options{Connector: dockConnector{src: src, rev: rev, profiles: withApplications(profiles), namespace: "shop"}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	s.UpdateUI(func(u *UIState) { u.View = kinds.ApplicationsKey })
	ts := waitTable(t, s, kinds.ApplicationsKey, func(ts *TableSnapshot) bool { return ts.Synced && len(ts.Rows) == 1 && ts.Error == "" })
	if r := ts.Rows[0].Ref; r.Namespace != "shop" {
		t.Errorf("row %v", r)
	}
	for _, res := range []string{"deployments", "statefulsets"} {
		if calls := callsOf(src, res); calls["list "+res+".apps in shop"] != 1 || calls["list "+res+".apps"] > 2 {
			t.Errorf("%s calls %v: want the refused read in all namespaces, then shop's", res, calls)
		}
	}
}

// A user who may not list the custom resource definitions (a namespace-limited user, measured on
// kind: five refused lists and five refused streaming watches in 20 seconds) is not asked again
// for them on every backoff step for as long as the cluster is connected: the refused list ends
// the watch, the custom kinds say why, and a refresh of the access reviews asks once more.
func TestCustomKindsStopAfterARefusal(t *testing.T) {
	src := &testkit.NamespacedOnlySource{Inner: testkit.NewMemorySource(), GVRs: []schema.GroupVersionResource{crdsGVR}}
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"team-a": {rule("get,list,watch", "", "pods")}})
	s := New(Options{Connector: dockConnector{src: src, rev: rev, profiles: testProfiles(), namespace: "team-a", custom: true},
		SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	b := s.Backend()
	refused := func() bool {
		set := b.customSet()
		return b.CustomKindsSettled() && set != nil && strings.Contains(set.err, "forbidden")
	}
	// 30 s, and what was asked so far. Its timeout on CI (2026-10-14, no diagnostics then) matches
	// a refusal that landed while the custom kinds read the handle's error twice: the watch was
	// dropped with an empty reason, one list sent and no error ever shown (customKinds.state,
	// 2026-10-20).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, refused) {
		t.Fatalf("timed out waiting for the custom kinds' refusal: definition calls %v, set %+v", callsOf(src, "customresourcedefinitions"), b.customSet())
	}
	// The refused scope is not kept warm: once the custom kinds let it go, its informer has stopped,
	// before client-go's first backoff step (0.8 s) could list again.
	waitCond(t, "the definitions' informer stopped", func() bool {
		for _, st := range b.Cache.Stats() {
			if st.Scope.GVR == crdsGVR {
				return false
			}
		}
		return true
	})
	if calls := callsOf(src, "customresourcedefinitions"); calls["list customresourcedefinitions.apiextensions.k8s.io"] != 1 {
		t.Errorf("definition calls %v: want the refused list once", calls)
	}
	if !refused() {
		t.Errorf("the custom kinds forgot why there are none: %+v", b.customSet())
	}
	if _, ok := b.RefreshAccess(); !ok {
		t.Fatal("no access reviews")
	}
	waitCond(t, "the definitions asked again after a refresh", func() bool {
		return callsOf(src, "customresourcedefinitions")["list customresourcedefinitions.apiextensions.k8s.io"] == 2
	})
	waitCond(t, "the refusal again", refused)
}

// The same read for a user whose RoleBinding is in the context's namespace alone: the read in all
// namespaces that the plan tries is refused once, and the read takes the object's namespace at
// once instead of waiting 15 seconds and saying the kind is still loading.
func TestDetailWaitFollowsARefusal(t *testing.T) {
	src, rev := contextOnly(t)
	s := contextOnlySession(t, src, rev, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	m, err := s.DetailWait(ctx, model.ObjectRef{Kind: "deployments", Namespace: "team-a", Name: "web"})
	if err != nil || m.Missing || m.Loading {
		t.Fatalf("DetailWait: %+v, %v (calls %v)", m, err, callsOf(src, "deployments"))
	}
	if d := time.Since(start); d > 8*time.Second {
		t.Errorf("DetailWait took %s", d)
	}
	if calls := callsOf(src, "deployments"); calls["list deployments.apps"] != 1 || calls["list deployments.apps in team-a"] != 1 {
		t.Errorf("deployment calls %v: want one refused list in all namespaces, then team-a's", calls)
	}
}
