package engine

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// nsConnector connects "test" to a cache over any source, with access reviews over a fake
// reviewer, as the cluster connector sets them up.
type nsConnector struct {
	src       cache.Source
	rev       *fakeReviewer
	namespace string
}

func (c nsConnector) Contexts() ([]ContextInfo, error) {
	return []ContextInfo{{Name: "test", Current: true}}, nil
}

func (c nsConnector) Connect(context.Context, string) (*Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	a := newAccessReviews(c.rev, c.namespace)
	return &Backend{
		Context: "test", Namespace: c.namespace, Cache: ch, Profiles: testProfiles(), Access: a,
		Close: func() { a.stop(); ch.Close() },
	}, nil
}

// namespacedOnly is a cluster whose user may list pods only in team-a and team-b: the source
// refuses pods in all namespaces and in team-c, and the reviewer's rules say the same.
func namespacedOnly(t *testing.T) (*testkit.MemorySource, *testkit.NamespacedOnlySource, *fakeReviewer) {
	t.Helper()
	mem := testkit.NewMemorySource()
	for _, ns := range []string{"team-a", "team-b", "team-c", "default"} {
		mem.Set(testkit.PodsGVR, testkit.Pod(ns, "web-"+ns))
	}
	src := &testkit.NamespacedOnlySource{Inner: mem, GVRs: []schema.GroupVersionResource{testkit.PodsGVR}, Denied: []string{"team-c", "default"}}
	pods := rule("get,list,watch", "", "pods")
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"team-a": {pods}, "team-b": {pods}})
	return mem, src, rev
}

func nsSession(t *testing.T, src cache.Source, rev *fakeReviewer, namespace string, accessible ...string) *Session {
	t.Helper()
	s := New(Options{Connector: nsConnector{src: src, rev: rev, namespace: namespace}, SnapshotInterval: 5 * time.Millisecond,
		ClusterSettings: NewClusterSettingsStore(t.TempDir())})
	t.Cleanup(s.Close)
	if len(accessible) > 0 {
		if _, err := s.ChangeClusterSettings("test", func(c *ClusterSettings) error { c.AccessibleNamespaces = accessible; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func rowNamespaces(ts *TableSnapshot) []string {
	var out []string
	for _, r := range ts.Rows {
		out = append(out, r.Ref.Namespace)
	}
	slices.Sort(out)
	return out
}

// podCalls are the source's list and watch calls of pods, by scope.
func podCalls(src *testkit.NamespacedOnlySource) map[string]int {
	out := map[string]int{}
	for k, v := range src.Calls() {
		if strings.Contains(k, " pods") {
			out[k] = v
		}
	}
	return out
}

// A user the access reviews deny listing pods in all namespaces, with accessible namespaces set:
// the Pods view waits for the reviews instead of trying a cluster-wide list, then reads pods in
// each accessible namespace the rules allow, one informer each, and merges them; a namespace the
// rules deny is not read and is named in the snapshot. No cluster-wide list is ever tried.
func TestNamespacedWatchesWhenClusterWideListIsDenied(t *testing.T) {
	_, src, rev := namespacedOnly(t)
	rev.set(func(f *fakeReviewer) { f.gate = make(chan struct{}) })
	s := nsSession(t, src, rev, "team-a", "team-a", "team-b", "team-c")
	connectTest(t, s)

	// The rules review is held: the view waits, and nothing lists pods.
	waitTable(t, s, "pods", func(ts *TableSnapshot) bool { return !ts.Synced })
	time.Sleep(50 * time.Millisecond)
	if calls := podCalls(src); len(calls) != 0 {
		t.Fatalf("pods read before the access reviews answered: %v", calls)
	}
	rev.set(func(f *fakeReviewer) { close(f.gate); f.gate = nil })

	ts := waitTable(t, s, "pods", func(ts *TableSnapshot) bool { return ts.Synced && len(ts.Rows) == 2 })
	if got := rowNamespaces(ts); !slices.Equal(got, []string{"team-a", "team-b"}) {
		t.Errorf("rows in %q", got)
	}
	if !slices.Equal(ts.Watched, []string{"team-a", "team-b"}) || !slices.Equal(ts.Denied, []string{"team-c"}) || ts.Error != "" {
		t.Errorf("watched %q, denied %q, error %q", ts.Watched, ts.Denied, ts.Error)
	}
	want := map[string]int{"list pods in team-a": 1, "watch pods in team-a": 1, "list pods in team-b": 1, "watch pods in team-b": 1}
	waitCond(t, "the namespaces' watches", func() bool { return maps.Equal(podCalls(src), want) })
	if _, access := rev.calls(); len(access) != 0 {
		t.Errorf("the plan asked access reviews of its own: %v", access)
	}

	// The namespace filter narrows the merged view client-side: no new watch.
	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"team-b"} })
	ts = waitTable(t, s, "pods", func(ts *TableSnapshot) bool { return ts.Synced && len(ts.Rows) == 1 })
	if ts.Rows[0].Ref.Namespace != "team-b" {
		t.Errorf("filtered to %q", ts.Rows[0].Ref.Namespace)
	}

	// An agent's read and the drawer read the same informers.
	objs, err := s.Objects(context.Background(), "pods", nil)
	if err != nil || len(objs) != 2 {
		t.Errorf("Objects: %d, %v", len(objs), err)
	}
	m, err := s.DetailWait(context.Background(), model.ObjectRef{Kind: "pods", Namespace: "team-a", Name: "web-team-a"})
	if err != nil || m.Missing {
		t.Errorf("drawer: %+v, %v", m, err)
	}
	if calls := podCalls(src); !maps.Equal(calls, want) {
		t.Errorf("reads cost requests: %v, want %v", calls, want)
	}
	for _, st := range s.Backend().Cache.Stats() {
		if st.Scope.GVR == testkit.PodsGVR && st.Scope.Namespace == "" {
			t.Errorf("a cluster-wide pods scope: %+v", st)
		}
	}
	if !s.Backend().Cached(testPods) {
		t.Error("pods read namespace by namespace are not cached for a search across kinds")
	}
}

// An agent's read right after connecting (cogpit-cli get) waits for the access reviews as the
// view does, then reads the named namespaces: no cluster-wide list for a user who may not.
func TestAgentReadWaitsForThePlan(t *testing.T) {
	_, src, rev := namespacedOnly(t)
	rev.set(func(f *fakeReviewer) { f.gate = make(chan struct{}) })
	s := nsSession(t, src, rev, "team-a", "team-a", "team-b", "team-c")
	connectTest(t, s)
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() {
		objs, err := s.Objects(context.Background(), "pods", nil)
		done <- result{len(objs), err}
	}()
	time.Sleep(50 * time.Millisecond)
	if calls := podCalls(src); len(calls) != 0 {
		t.Fatalf("pods read before the access reviews answered: %v", calls)
	}
	rev.set(func(f *fakeReviewer) { close(f.gate); f.gate = nil })
	select {
	case r := <-done:
		if r.err != nil || r.n != 2 {
			t.Errorf("Objects: %d, %v", r.n, r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the read never finished")
	}
	if calls := podCalls(src); calls["list pods"] != 0 || calls["list pods in team-a"] != 1 || calls["list pods in team-b"] != 1 {
		t.Errorf("calls %v", calls)
	}
}

// The plan follows the settings and the filter: an accessible namespace added is read at once,
// the list cleared falls back to the namespace filter's namespaces, and without either to the
// context's own namespace.
func TestNamespacePlanFollowsSettingsAndFilter(t *testing.T) {
	_, src, rev := namespacedOnly(t)
	s := nsSession(t, src, rev, "team-a", "team-a")
	connectTest(t, s)
	ts := waitTable(t, s, "pods", func(ts *TableSnapshot) bool { return ts.Synced && len(ts.Rows) == 1 })
	if !slices.Equal(ts.Watched, []string{"team-a"}) {
		t.Fatalf("watched %q", ts.Watched)
	}
	if _, err := s.ChangeClusterSettings("test", func(c *ClusterSettings) error {
		c.AccessibleNamespaces = []string{"team-a", "team-b"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	waitTable(t, s, "pods", func(ts *TableSnapshot) bool {
		return ts.Synced && slices.Equal(rowNamespaces(ts), []string{"team-a", "team-b"})
	})
	if _, err := s.ChangeClusterSettings("test", func(c *ClusterSettings) error { c.AccessibleNamespaces = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	ts = waitTable(t, s, "pods", func(ts *TableSnapshot) bool { return ts.Synced && slices.Equal(ts.Watched, []string{"team-a"}) })
	if got := rowNamespaces(ts); !slices.Equal(got, []string{"team-a"}) {
		t.Errorf("the context's namespace alone: %q", got)
	}
	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"team-b"} })
	ts = waitTable(t, s, "pods", func(ts *TableSnapshot) bool { return ts.Synced && slices.Equal(ts.Watched, []string{"team-b"}) })
	if got := rowNamespaces(ts); !slices.Equal(got, []string{"team-b"}) {
		t.Errorf("the filter's namespace: %q", got)
	}
	if calls := podCalls(src); calls["list pods"] != 0 {
		t.Errorf("a cluster-wide list: %v", calls)
	}
}

// A user who may list pods everywhere keeps the one cluster-wide watch: the plan costs no access
// review of its own and no namespace's watch.
func TestClusterWideWatchForAUserWhoMayList(t *testing.T) {
	mem := testkit.NewMemorySource()
	mem.Set(testkit.PodsGVR, testkit.Pod("team-a", "web"))
	mem.Set(testkit.PodsGVR, testkit.Pod("team-b", "web"))
	src := &testkit.NamespacedOnlySource{Inner: mem}
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"": {rule("*", "*", "*")}})
	s := nsSession(t, src, rev, "")
	connectTest(t, s)
	ts := waitTable(t, s, "pods", func(ts *TableSnapshot) bool { return ts.Synced && len(ts.Rows) == 2 })
	if len(ts.Watched) != 0 || len(ts.Denied) != 0 {
		t.Errorf("watched %q denied %q", ts.Watched, ts.Denied)
	}
	waitCond(t, "the cluster-wide watch", func() bool { return maps.Equal(podCalls(src), map[string]int{"list pods": 1, "watch pods": 1}) })
	rules, access := rev.calls()
	if len(access) != 0 || !maps.Equal(rules, map[string]int{"default": 1}) {
		t.Errorf("reviews: rules %v, access %v; want the default namespace's rules only", rules, access)
	}
}

// Rules that allow pods in the context's namespace (a RoleBinding there) do not tell whether a
// cluster-wide list is allowed: the view tries it, the server refuses it, and the view reads the
// context's namespace instead; the refused scope is not kept warm, and a refresh of the access
// reviews tries the cluster-wide list again.
func TestRefusedClusterWideListFallsBackToNamespaces(t *testing.T) {
	mem := testkit.NewMemorySource()
	mem.Set(testkit.PodsGVR, testkit.Pod("team-a", "web"))
	mem.Set(testkit.PodsGVR, testkit.Pod("team-b", "web"))
	src := &testkit.NamespacedOnlySource{Inner: mem, GVRs: []schema.GroupVersionResource{testkit.PodsGVR}}
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"team-a": {rule("get,list,watch", "", "pods")}})
	s := nsSession(t, src, rev, "team-a")
	connectTest(t, s)
	ts := waitTable(t, s, "pods", func(ts *TableSnapshot) bool {
		return ts.Synced && slices.Equal(ts.Watched, []string{"team-a"}) && len(ts.Rows) == 1
	})
	if ts.Error != "" {
		t.Errorf("error %q", ts.Error)
	}
	waitCond(t, "the refused scope stopped", func() bool {
		for _, st := range s.Backend().Cache.Stats() {
			if st.Scope.GVR == testkit.PodsGVR && st.Scope.Namespace == "" {
				return false
			}
		}
		return true
	})
	calls := podCalls(src)
	if calls["list pods"] == 0 || calls["list pods in team-a"] != 1 {
		t.Errorf("calls %v", calls)
	}
	if !s.Backend().ns.isRefused(testkit.PodsGVR) {
		t.Error("the refusal is not kept")
	}
	// The reviews held while the refresh is checked: once they answer, the view tries the
	// cluster-wide list again and the server refuses it again (CI, 2026-10-22: a loaded runner
	// had the refusal noted again before the check ran).
	rev.set(func(f *fakeReviewer) { f.gate = make(chan struct{}) })
	if _, ok := s.Backend().RefreshAccess(); !ok {
		t.Fatal("no access reviews")
	}
	if s.Backend().ns.isRefused(testkit.PodsGVR) {
		t.Error("a refresh kept the refusal")
	}
	rev.set(func(f *fakeReviewer) { close(f.gate); f.gate = nil })
	waitCond(t, "the cluster-wide list tried again", func() bool {
		return podCalls(src)["list pods"] > calls["list pods"] && s.Backend().ns.isRefused(testkit.PodsGVR)
	})
}

// Backends without access reviews (the demo cluster) and cluster-scoped, local and virtual
// kinds are read in all namespaces; a generic Access that denies the cluster-wide list reads the
// named namespaces it does not deny.
func TestNamespacePlanCases(t *testing.T) {
	b := &Backend{Namespace: "team-a"}
	if p := b.namespacePlan(testPods); p.key() != "all" {
		t.Errorf("no access reviews: %s", p.key())
	}
	for _, p := range []*model.KindProfile{testNamespaces, {Key: "forwards", Namespaced: true, Local: true}} {
		b.Access = denyAll{}
		if plan := b.namespacePlan(p); plan.key() != "all" {
			t.Errorf("%s: %s", p.Key, plan.key())
		}
	}
	b.ns.setInputs([]string{"team-b", "team-c"}, []string{"team-a"})
	b.Access = denyAll{except: map[string]bool{"team-a": true, "team-b": true}}
	if plan := b.namespacePlan(testPods); plan.key() != "in:team-a,team-b;denied:team-c" {
		t.Errorf("generic access: %s", plan.key())
	}
}

// denyAll denies every verb except in the namespaces it lists.
type denyAll struct{ except map[string]bool }

func (d denyAll) Allowed(namespace string, _ Verb) (bool, bool) { return d.except[namespace], true }
