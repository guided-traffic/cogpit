package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A workload's Pods table in its drawer (D-40, G6): the pods it controls, from the cache through
// the drawer's own related-objects indexes (DetailNeeds), which live only while the drawer is open,
// so that the workload lists do not rebuild on every pod change; CPU and Memory from the metrics
// hub as in a node's drawer, sorted by the same engine state, the rest of a long table counted.

var (
	wpDaemonSetsGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}
	wpDeployments   = testkit.DeploymentsGVR
)

// wpOwned is a testkit object controlled by the testkit object of kind and name in its namespace
// (testkit's UIDs are "uid-<namespace>-<name>").
func wpOwned(obj *unstructured.Unstructured, apiVersion, kind, name string) *unstructured.Unstructured {
	return owned(obj, apiVersion, kind, name, "uid-"+obj.GetNamespace()+"-"+name)
}

func wpPod(ns, name, ownerKind, owner string) *unstructured.Unstructured {
	apiVersion := map[string]string{"ReplicaSet": "apps/v1", "StatefulSet": "apps/v1", "DaemonSet": "apps/v1", "Job": "batch/v1"}[ownerKind]
	return wpOwned(testkit.Pod(ns, name), apiVersion, ownerKind, owner)
}

// wpUsage is what the fake Metrics Server measures: some of the Deployment web's pods and the
// stateful set's first pod.
var wpUsage = map[string]model.Usage{
	"shop/web-new-a": {CPUMilli: 40, MemoryBytes: 300 << 20},
	"shop/web-new-b": {CPUMilli: 900, MemoryBytes: 20 << 20},
	"shop/web-old-a": {CPUMilli: 5, MemoryBytes: 700 << 20},
	"shop/db-0":      {CPUMilli: 120, MemoryBytes: 1 << 30},
	"shop/db-1":      {CPUMilli: 500, MemoryBytes: 1 << 30},
}

// workloadPodsSession is a session over every workload kind with pods: in shop, the Deployment web
// in a rollout (replica sets web-old and web-new) and api (api-1), the stateful set db, the daemon
// set agent, the Cron Job report with its job report-1, and a pod without an owner; the Events view
// open (it polls no metrics), a metrics hub over a fake Metrics Server.
func workloadPodsSession(t *testing.T) (*Session, *testkit.MemorySource, *podTableProvider) {
	t.Helper()
	src := testkit.NewMemorySource()
	src.Set(wpDeployments, testkit.Object("apps/v1", "Deployment", "shop", "web", nil))
	src.Set(wpDeployments, testkit.Object("apps/v1", "Deployment", "shop", "api", nil))
	src.Set(replicaSetsGVR, wpOwned(testkit.Object("apps/v1", "ReplicaSet", "shop", "web-old", nil), "apps/v1", "Deployment", "web"))
	src.Set(replicaSetsGVR, wpOwned(testkit.Object("apps/v1", "ReplicaSet", "shop", "web-new", nil), "apps/v1", "Deployment", "web"))
	src.Set(replicaSetsGVR, wpOwned(testkit.Object("apps/v1", "ReplicaSet", "shop", "api-1", nil), "apps/v1", "Deployment", "api"))
	src.Set(statefulSetsGVR, testkit.Object("apps/v1", "StatefulSet", "shop", "db", nil))
	src.Set(wpDaemonSetsGVR, testkit.Object("apps/v1", "DaemonSet", "shop", "agent", nil))
	src.Set(appsCronJobsGVR, testkit.Object("batch/v1", "CronJob", "shop", "report", nil))
	src.Set(appsJobsGVR, wpOwned(testkit.Object("batch/v1", "Job", "shop", "report-1", nil), "batch/v1", "CronJob", "report"))
	for _, p := range []*unstructured.Unstructured{
		wpPod("shop", "web-old-a", "ReplicaSet", "web-old"),
		wpPod("shop", "web-new-b", "ReplicaSet", "web-new"),
		wpPod("shop", "web-new-a", "ReplicaSet", "web-new"),
		wpPod("shop", "api-1-a", "ReplicaSet", "api-1"),
		wpPod("shop", "db-0", "StatefulSet", "db"),
		wpPod("shop", "db-1", "StatefulSet", "db"),
		wpPod("shop", "agent-x", "DaemonSet", "agent"),
		wpPod("shop", "report-1-x", "Job", "report-1"),
		testkit.Pod("shop", "lonely"),
	} {
		src.Set(testkit.PodsGVR, p)
	}
	prov := &podTableProvider{usage: wpUsage}
	var profiles []*model.KindProfile
	for _, k := range []string{"pods", "deployments", "replicasets", "statefulsets", "daemonsets", "jobs", "cronjobs"} {
		profiles = append(profiles, builtinProfile(t, k))
	}
	fc := &fakeConnector{src: src, profiles: append(profiles, testEvents)}
	s := New(Options{
		Connector:        &metricsConnector{fakeConnector: fc, prov: prov},
		ClusterSettings:  NewClusterSettingsStore(filepath.Join(t.TempDir(), "Cogpit")),
		SnapshotInterval: 5 * time.Millisecond,
	})
	t.Cleanup(s.Close)
	decideIntegrations(t, s, "test") // its metrics read (D-82)
	connectTest(t, s)
	openView(s, testEvents.Key)
	return s, src, prov
}

func wpRef(kind, name string) model.ObjectRef {
	return model.ObjectRef{Context: "test", Kind: kind, Namespace: "shop", Name: name}
}

// openDrawer opens the drawer on ref over the open view.
func openDrawer(s *Session, ref model.ObjectRef) {
	s.UpdateUI(func(u *UIState) { u.Drawer = &ref })
}

// waitDrawerPods waits until the drawer model of ref lists exactly want in its Pods table (nil: no
// table).
func waitDrawerPods(t *testing.T, s *Session, ref model.ObjectRef, want ...string) {
	t.Helper()
	var got []string
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		m, err := s.Detail(ref)
		if err != nil {
			t.Fatal(err)
		}
		got = nodePods(m)
		return slices.Equal(got, want)
	}) {
		t.Fatalf("%s's Pods %q, want %q", ref.Name, got, want)
	}
}

// scopeHandles returns how many handles the cache holds on a kind's scope.
func scopeHandles(s *Session, kind string) int {
	b := s.Backend()
	p, _ := b.Profile(kind)
	for _, st := range b.Cache.Stats() {
		if st.Scope == b.Scope(p) {
			return st.Handles
		}
	}
	return 0
}

// Each workload kind's drawer lists the pods it controls, an agent's read (DetailWait, object_get)
// as well, without the drawer open: a Deployment both replica sets' pods during a rollout, not
// api's; a Cron Job its job's; never the pod without an owner.
func TestWorkloadDrawerPodsPerKind(t *testing.T) {
	s, _, _ := workloadPodsSession(t)
	for _, c := range []struct {
		kind, name string
		want       []string
	}{
		{"deployments", "web", []string{"web-new-a", "web-new-b", "web-old-a"}},
		{"deployments", "api", []string{"api-1-a"}},
		{"replicasets", "web-new", []string{"web-new-a", "web-new-b"}},
		{"statefulsets", "db", []string{"db-0", "db-1"}},
		{"daemonsets", "agent", []string{"agent-x"}},
		{"jobs", "report-1", []string{"report-1-x"}},
		{"cronjobs", "report", []string{"report-1-x"}},
	} {
		m, err := s.DetailWait(context.Background(), wpRef(c.kind, c.name))
		if err != nil {
			t.Fatal(err)
		}
		if got := nodePods(m); !slices.Equal(got, c.want) {
			t.Errorf("%s %s: Pods %q, want %q", c.kind, c.name, got, c.want)
		}
	}
}

// The open drawer of a Deployment follows its pods: a pod of the new replica set arrives, the old
// replica set's last pod goes (the rollout ends), a pod that loses its controller reference or
// moves to another owner leaves, and a replica set created by the next rollout brings its pods,
// each announced by EventDetail.
func TestWorkloadDrawerPodsFollowPods(t *testing.T) {
	s, src, _ := workloadPodsSession(t)
	ref := wpRef("deployments", "web")
	openDrawer(s, ref)
	rec := record(s)
	waitDrawerPods(t, s, ref, "web-new-a", "web-new-b", "web-old-a")

	before := rec.count(EventDetail, "deployments")
	src.Set(testkit.PodsGVR, wpPod("shop", "web-new-c", "ReplicaSet", "web-new"))
	waitFor(t, "a detail event for the new pod", func() bool { return rec.count(EventDetail, "deployments") > before })
	waitDrawerPods(t, s, ref, "web-new-a", "web-new-b", "web-new-c", "web-old-a")

	src.Delete(testkit.PodsGVR, "shop", "web-old-a")
	waitDrawerPods(t, s, ref, "web-new-a", "web-new-b", "web-new-c")

	orphan := testkit.Pod("shop", "web-new-b") // its controller reference removed
	src.Set(testkit.PodsGVR, orphan)
	waitDrawerPods(t, s, ref, "web-new-a", "web-new-c")
	src.Set(testkit.PodsGVR, wpPod("shop", "web-new-a", "ReplicaSet", "api-1")) // adopted by api's replica set
	waitDrawerPods(t, s, ref, "web-new-c")

	src.Set(replicaSetsGVR, wpOwned(testkit.Object("apps/v1", "ReplicaSet", "shop", "web-next", nil), "apps/v1", "Deployment", "web"))
	src.Set(testkit.PodsGVR, wpPod("shop", "web-next-a", "ReplicaSet", "web-next"))
	waitDrawerPods(t, s, ref, "web-new-c", "web-next-a")

	// The api Deployment's drawer took web-new-a over.
	m, err := s.DetailWait(context.Background(), wpRef("deployments", "api"))
	if err != nil {
		t.Fatal(err)
	}
	if got := nodePods(m); !slices.Equal(got, []string{"api-1-a", "web-new-a"}) {
		t.Errorf("api's Pods %q", got)
	}
}

// The pods' indexes are the drawer's alone: the Deployments list holds no handle on the pods or the
// replica sets and is not rebuilt when a pod changes; the drawer holds both while it is open and
// gives them back when it closes.
func TestWorkloadPodsIndexOnlyWithTheDrawer(t *testing.T) {
	s, src, _ := workloadPodsSession(t)
	openView(s, "deployments")
	waitTable(t, s, "deployments", func(ts *TableSnapshot) bool { return ts.Synced && len(ts.Rows) == 2 })
	if n, m := scopeHandles(s, "pods"), scopeHandles(s, "replicasets"); n != 0 || m != 0 {
		t.Fatalf("the Deployments list holds %d handles on the pods and %d on the replica sets, want none", n, m)
	}

	// Someone else watches the pods (another agent's read, a page): their events flow.
	pp, _ := s.Backend().Profile("pods")
	pods := s.Backend().Acquire(pp)
	defer pods.Release()
	waitFor(t, "the pods", pods.HasSynced)
	rec := record(s)
	tables := func() int { return rec.count(EventTable, "deployments") }

	// changePods changes a pod of web n times, each seen by the pods' informer before the next.
	changePods := func(round string, n int) {
		t.Helper()
		for i := range n {
			p := wpPod("shop", "web-new-a", "ReplicaSet", "web-new")
			p.SetLabels(map[string]string{"round": fmt.Sprintf("%s-%d", round, i)})
			src.Set(testkit.PodsGVR, p)
			want := p.GetLabels()["round"]
			waitFor(t, "the pod's change in the cache", func() bool {
				o, ok := KindGet(pods, pp, "shop", "web-new-a")
				return ok && o.GetLabels()["round"] == want
			})
		}
	}
	// sentinel changes api and waits for the list to show it and for that snapshot's event: every
	// wake before it has been built and announced by then. Each count opens and closes with one, so
	// no event of the set-up lands in it (api: web's drawer does not wake for it).
	sentinel := func(n int) {
		t.Helper()
		d := testkit.Object("apps/v1", "Deployment", "shop", "api", map[string]any{"spec": map[string]any{"replicas": int64(n)}})
		src.Set(wpDeployments, d)
		col := builtinProfile(t, "deployments").ColumnIndex("replicas")
		snap := waitTable(t, s, "deployments", func(ts *TableSnapshot) bool {
			return slices.ContainsFunc(ts.Rows, func(r model.Row) bool { return r.Ref.Name == "api" && r.Cells[col].Num == int64(n) })
		})
		waitFor(t, "the snapshot's table event", func() bool { return rec.announced("deployments", snap.Revision) })
	}

	sentinel(6)
	before := tables()
	changePods("closed", 10)
	sentinel(7)
	if got := tables() - before; got != 1 {
		t.Errorf("%d Deployments snapshots for ten pod changes and one change of api, want 1 (api's)", got)
	}

	// The drawer open: it holds the pods and the replica sets and wakes for web's pods; the list
	// still does not rebuild.
	ref := wpRef("deployments", "web")
	openDrawer(s, ref)
	waitDrawerPods(t, s, ref, "web-new-a", "web-new-b", "web-old-a")
	if n, m := scopeHandles(s, "pods"), scopeHandles(s, "replicasets"); n != 2 || m != 1 {
		t.Errorf("with the drawer open: %d handles on the pods (want 2: the drawer's and the test's), %d on the replica sets (want 1)", n, m)
	}
	sentinel(8)
	before, details := tables(), rec.count(EventDetail, "deployments")
	changePods("open", 5)
	waitFor(t, "the drawer to wake for its pod", func() bool { return rec.count(EventDetail, "deployments") > details })
	sentinel(9)
	if got := tables() - before; got != 1 {
		t.Errorf("%d Deployments snapshots with the drawer open, want 1 (api's)", got)
	}

	s.UpdateUI(func(u *UIState) { u.Drawer = nil })
	waitFor(t, "the drawer's handles to go back", func() bool {
		return scopeHandles(s, "pods") == 1 && scopeHandles(s, "replicasets") == 0
	})
}

// A Deployment's drawer wakes for its own pods and replica sets only: the pods of another
// Deployment change ten times without an EventDetail, and one pod of its own makes one. (No
// metrics hub: its polls would announce the drawer too.)
func TestWorkloadDrawerWakesOnlyForItsPods(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(wpDeployments, testkit.Object("apps/v1", "Deployment", "shop", "web", nil))
	src.Set(wpDeployments, testkit.Object("apps/v1", "Deployment", "shop", "api", nil))
	src.Set(replicaSetsGVR, wpOwned(testkit.Object("apps/v1", "ReplicaSet", "shop", "web-new", nil), "apps/v1", "Deployment", "web"))
	src.Set(replicaSetsGVR, wpOwned(testkit.Object("apps/v1", "ReplicaSet", "shop", "api-1", nil), "apps/v1", "Deployment", "api"))
	src.Set(testkit.PodsGVR, wpPod("shop", "web-new-a", "ReplicaSet", "web-new"))
	src.Set(testkit.PodsGVR, wpPod("shop", "api-1-a", "ReplicaSet", "api-1"))
	s, fc := newTestSession(t, src, 5*time.Millisecond)
	fc.profiles = append(testProfiles(), builtinProfile(t, "replicasets"))
	for i, p := range fc.profiles {
		if p.Key == "deployments" {
			fc.profiles[i] = builtinProfile(t, "deployments")
		}
	}
	connectTest(t, s)
	openView(s, testEvents.Key)
	ref := wpRef("deployments", "web")
	openDrawer(s, ref)
	waitDrawerPods(t, s, ref, "web-new-a")
	pp, _ := s.Backend().Profile("pods")
	pods := s.Backend().Acquire(pp)
	defer pods.Release()
	waitFor(t, "the pods", pods.HasSynced)
	rec := record(s)
	// A barrier: a change of its own pod, waited for, so that no event of the drawer's set-up is
	// still on its way when the count starts (seen on CI's slow runner under -race).
	own := wpPod("shop", "web-new-a", "ReplicaSet", "web-new")
	own.SetLabels(map[string]string{"barrier": "1"})
	settled := rec.count(EventDetail, "deployments")
	src.Set(testkit.PodsGVR, own)
	waitFor(t, "the barrier's detail event", func() bool { return rec.count(EventDetail, "deployments") > settled })
	waitFor(t, "the barrier in the drawer's pods", func() bool {
		o, ok := KindGet(pods, pp, "shop", "web-new-a")
		return ok && o.GetLabels()["barrier"] == "1"
	})
	before := rec.count(EventDetail, "deployments")

	for i := range 10 {
		p := wpPod("shop", "api-1-a", "ReplicaSet", "api-1")
		p.SetLabels(map[string]string{"round": fmt.Sprint(i)})
		src.Set(testkit.PodsGVR, p)
		want := fmt.Sprint(i)
		waitFor(t, "the other Deployment's pod in the cache", func() bool {
			o, ok := KindGet(pods, pp, "shop", "api-1-a")
			return ok && o.GetLabels()["round"] == want
		})
	}
	src.Set(testkit.PodsGVR, wpPod("shop", "web-new-b", "ReplicaSet", "web-new"))
	waitFor(t, "a detail event for its own pod", func() bool { return rec.count(EventDetail, "deployments") > before })
	waitDrawerPods(t, s, ref, "web-new-a", "web-new-b")
	// One change of its own pod is one event, or two when a slow runner splits its rebuild across
	// the coalescing interval; ten changes of another Deployment's pod are none (without the key
	// tracking they are ten more).
	if got := rec.count(EventDetail, "deployments") - before; got < 1 || got > 2 {
		t.Errorf("%d detail events for ten changes of another Deployment's pod and one pod of its own, want 1 (2 at most)", got)
	}
}

// wpNSConnector is nsConnector with the built-in Stateful Sets.
type wpNSConnector struct{ nsConnector }

func (c wpNSConnector) Connect(ctx context.Context, name string) (*Backend, error) {
	b, err := c.nsConnector.Connect(ctx, name)
	if err == nil {
		p, _ := kinds.ByKey("statefulsets")
		b.Profiles = append(b.Profiles, p)
	}
	return b, err
}

// A user who may list pods and stateful sets only in team-a and team-b opens a stateful set's
// drawer while the access reviews are still out: the drawer reads nothing until the plan is
// decided (a read that does not wait holds no handle while it is pending); once the reviews
// answer, it reads the pods namespace by namespace, never in all namespaces, and the table lists
// the pod.
func TestWorkloadDrawerPodsForANamespaceLimitedUser(t *testing.T) {
	mem := testkit.NewMemorySource()
	mem.Set(statefulSetsGVR, testkit.Object("apps/v1", "StatefulSet", "team-a", "db", nil))
	mem.Set(testkit.PodsGVR, wpPod("team-a", "db-0", "StatefulSet", "db"))
	mem.Set(testkit.PodsGVR, testkit.Pod("team-b", "web-team-b"))
	src := &testkit.NamespacedOnlySource{Inner: mem, GVRs: []schema.GroupVersionResource{testkit.PodsGVR, statefulSetsGVR}, Denied: []string{"team-c", "default"}}
	allowed := []cluster.AccessRule{rule("get,list,watch", "", "pods"), rule("get,list,watch", "apps", "statefulsets")}
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"team-a": allowed, "team-b": allowed})
	rev.set(func(f *fakeReviewer) { f.gate = make(chan struct{}) })
	s := New(Options{Connector: wpNSConnector{nsConnector{src: src, rev: rev, namespace: "team-a"}}, SnapshotInterval: 5 * time.Millisecond,
		ClusterSettings: NewClusterSettingsStore(t.TempDir())})
	t.Cleanup(s.Close)
	if _, err := s.ChangeClusterSettings("test", func(c *ClusterSettings) error { c.AccessibleNamespaces = []string{"team-a", "team-b"}; return nil }); err != nil {
		t.Fatal(err)
	}
	connectTest(t, s)
	openView(s, testEvents.Key)
	ref := model.ObjectRef{Context: "test", Kind: "statefulsets", Namespace: "team-a", Name: "db"}
	openDrawer(s, ref)
	// While the reviews are out the drawer holds nothing: a read that does not wait holds no
	// handle until the plan is decided (accessiblens_follow.go), so it never asks for the pods in
	// all namespaces.
	if all, _ := podsHandles(s); all != 0 {
		t.Errorf("the drawer reads pods in all namespaces while the reviews are out (%d handles)", all)
	}

	rev.set(func(f *fakeReviewer) { close(f.gate); f.gate = nil })
	waitFor(t, "the drawer reading pods namespace by namespace", func() bool {
		all, ns := podsHandles(s)
		return all == 0 && ns > 0
	})
	waitDrawerPods(t, s, ref, "db-0")
	for call := range src.Calls() {
		if call == "list pods" || call == "watch pods" {
			t.Errorf("%s: the pods were read in all namespaces, which the reviews refuse", call)
		}
	}
}

// The table's CPU and Memory come from the hub as in a node's drawer, polled for the workload's
// namespace while the drawer shows the table, and not after it closes; the cluster's settings hide
// them and their poll.
func TestWorkloadDrawerPodsMetrics(t *testing.T) {
	s, _, _ := workloadPodsSession(t)
	hub := s.Backend().Metrics
	ref := wpRef("deployments", "web")
	openDrawer(s, ref)
	pods := builtinProfile(t, "pods")
	var tbl *model.DetailTable
	waitFor(t, "the Pods table's CPU", func() bool {
		tbl = podsTable(t, s, ref)
		if tbl == nil || len(tbl.Rows) != 3 {
			return false
		}
		cells, _ := rowOfPod(t, tbl, "web-new-b")
		return cells[tbl.ColumnIndex(model.ColumnCPU)].Text != ""
	})
	for _, name := range []string{"web-new-a", "web-new-b", "web-old-a"} {
		wantCPU, wantMem := pods.UsageCells(nil, wpUsage["shop/"+name])
		cells, _ := rowOfPod(t, tbl, name)
		if cells[tbl.ColumnIndex(model.ColumnCPU)] != wantCPU || cells[tbl.ColumnIndex(model.ColumnMemory)] != wantMem {
			t.Errorf("%s: %+v, want %+v and %+v", name, cells, wantCPU, wantMem)
		}
	}
	// A Deployment has no chart of its own: the one poll is its pods'.
	if n := hub.Subscribed(); n != 1 {
		t.Errorf("%d scopes polled with a Deployment's drawer open, want 1 (its pods)", n)
	}

	if _, err := s.ChangeClusterSettings("test", func(cs *ClusterSettings) error { cs.HiddenMetrics = HidePods; return nil }); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the table's poll released", func() bool { return hub.Subscribed() == 0 })
	if tbl = podsTable(t, s, ref); tbl.ColumnIndex(model.ColumnCPU) >= 0 || tbl.ColumnIndex(model.ColumnMemory) >= 0 {
		t.Errorf("hidden metrics: columns %q", tbl.Columns)
	}
	if _, err := s.ChangeClusterSettings("test", func(cs *ClusterSettings) error { cs.HiddenMetrics = 0; return nil }); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the poll back", func() bool { podsTable(t, s, ref); return hub.Subscribed() == 1 })

	s.UpdateUI(func(u *UIState) { u.Drawer = nil })
	waitFor(t, "the drawer's poll released", func() bool { return hub.Subscribed() == 0 })
}

// The table sorts by the drawer's order (UIState.DrawerSort, by the section's title: the same as a
// node's Pods), metrics by value; the order stays when the drawer shows another workload.
func TestWorkloadDrawerPodsSort(t *testing.T) {
	s, _, _ := workloadPodsSession(t)
	ref := wpRef("deployments", "web")
	openDrawer(s, ref)
	s.UpdateUI(func(u *UIState) { u.DrawerSort = map[string]SortSpec{"Pods": {Column: model.ColumnMemory, Desc: true}} })
	waitFor(t, "the table sorted by Memory", func() bool {
		tbl := podsTable(t, s, ref)
		return tbl != nil && tbl.Sort != nil && tbl.Sort.Column == model.ColumnMemory &&
			slices.Equal(nodePods(&DetailModel{Sections: []model.DetailSection{{Title: "Pods", Table: tbl}}}), []string{"web-old-a", "web-new-a", "web-new-b"})
	})
	s.UpdateUI(func(u *UIState) { u.DrawerSort = map[string]SortSpec{"Pods": {Column: model.ColumnCPU, Desc: true}} })
	ref = wpRef("statefulsets", "db") // db-1 uses more CPU than db-0
	openDrawer(s, ref)
	waitFor(t, "the stateful set's table sorted by CPU, descending", func() bool {
		tbl := podsTable(t, s, ref)
		return tbl != nil && tbl.Sort != nil && tbl.Sort.Column == model.ColumnCPU && tbl.Sort.Desc && len(tbl.Rows) == 2 &&
			tbl.Rows[0][0] == "db-1" && tbl.Rows[1][0] == "db-0"
	})
}

// A workload with more pods than the bound shows the first MaxDrawerTableRows in the table's
// order and counts the rest; agents read the same model.
func TestWorkloadDrawerPodsBounded(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(statefulSetsGVR, testkit.Object("apps/v1", "StatefulSet", "big", "web", nil))
	extra := 3
	for i := range MaxDrawerTableRows + extra {
		src.Set(testkit.PodsGVR, wpPod("big", fmt.Sprintf("web-%04d", i), "StatefulSet", "web"))
	}
	s, fc := newTestSession(t, src, 5*time.Millisecond)
	fc.profiles = append(testProfiles(), builtinProfile(t, "statefulsets"))
	connectTest(t, s)
	m, err := s.DetailWait(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "big", Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	var tbl *model.DetailTable
	for _, sec := range m.Sections {
		if sec.Title == "Pods" {
			tbl = sec.Table
		}
	}
	if tbl == nil || len(tbl.Cells) != MaxDrawerTableRows || len(tbl.Rows) != MaxDrawerTableRows || tbl.Omitted != extra {
		t.Fatalf("table of %d rows, %d omitted; want %d and %d", len(tbl.Cells), tbl.Omitted, MaxDrawerTableRows, extra)
	}
	if tbl.Rows[0][0] != "web-0000" || tbl.Rows[MaxDrawerTableRows-1][0] != fmt.Sprintf("web-%04d", MaxDrawerTableRows-1) {
		t.Errorf("not the first rows by name: %s … %s", tbl.Rows[0][0], tbl.Rows[MaxDrawerTableRows-1][0])
	}
}

func TestLimitDrawerTables(t *testing.T) {
	cols := []model.Column{{ID: "name", Title: "Name"}}
	rows := func(n int) [][]model.Cell {
		out := make([][]model.Cell, n)
		for i := range out {
			out[i] = []model.Cell{{Text: fmt.Sprint(i)}}
		}
		return out
	}
	long, short, plain := model.SortableCellTable(cols, rows(7)), model.SortableCellTable(cols, rows(3)), model.CellTable(cols, rows(7))
	m := &DetailModel{Sections: []model.DetailSection{{Title: "A", Table: long}, {Title: "B", Table: short}, {Title: "C", Table: plain}, {Title: "D"}}}
	LimitDrawerTables(m, 3)
	if got := m.Sections[0].Table; len(got.Cells) != 3 || len(got.Rows) != 3 || got.Omitted != 4 || got.Rows[2][0] != "2" {
		t.Errorf("the long table: %d cells, %d rows, %d omitted", len(got.Cells), len(got.Rows), got.Omitted)
	}
	if len(long.Cells) != 7 || long.Omitted != 0 {
		t.Error("the table given was changed")
	}
	if m.Sections[1].Table != short || m.Sections[2].Table != plain {
		t.Error("a table within the bound, or not sortable, was replaced")
	}
}
