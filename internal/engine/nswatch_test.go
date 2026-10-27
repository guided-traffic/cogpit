package engine

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// nsObject is a namespace as the API server keeps it: Active, or Terminating once deleted while a
// finalizer of an object in it holds it (kubectl delete ns walk-gw9 --wait=false).
func nsObject(name string, terminating bool, labels map[string]any) *unstructured.Unstructured {
	meta := map[string]any{}
	if labels != nil {
		meta["labels"] = labels
	}
	phase := "Active"
	if terminating {
		meta["deletionTimestamp"] = "2026-10-22T10:00:00Z"
		phase = "Terminating"
	}
	return testkit.Object("v1", "Namespace", "", name, map[string]any{
		"metadata": meta,
		"spec":     map[string]any{"finalizers": []any{"kubernetes"}},
		"status":   map[string]any{"phase": phase},
	})
}

// nsWatchSession connects a session over src with access (nil allows everything) and records its
// events.
func nsWatchSession(t *testing.T, src *testkit.MemorySource, access Access) (*Session, *recorder) {
	t.Helper()
	fc := &fakeConnector{src: src, profiles: testProfiles()}
	var conn Connector = fc
	if access != nil {
		conn = &blockingConnector{fakeConnector: fc, access: access}
	}
	s := New(Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	r := record(s)
	connectTest(t, s)
	return s, r
}

// waitNamespacesWatched waits until the watch a namespace filter starts (nswatch.go) has listed
// the namespaces and holds its watch open: a test that counts requests takes its baseline after.
func waitNamespacesWatched(t *testing.T, src *testkit.MemorySource) {
	t.Helper()
	waitFor(t, "the namespaces watched", func() bool { return src.Watching(cache.Scope{GVR: testkit.NamespacesGVR}) })
}

// nsHandles returns how many handles hold the cluster-wide namespaces scope of b's cache, -1
// while the cache has no such informer.
func nsHandles(b *Backend) int {
	for _, st := range b.Cache.Stats() {
		if st.Scope.GVR == testkit.NamespacesGVR && st.Scope.Namespace == "" {
			return st.Handles
		}
	}
	return -1
}

// TestNamespaceEventsWhileFiltered (the owner's alpha walk, AW11): the Pods view filtered to
// walk-gw9, a namespace without pods, which is deleted and held Terminating by a finalizer, then
// gone. The window learned of it only when the view's rows changed, which they never did. Now the
// namespaces' own changes from the cache emit EventNamespaces while the filter names namespaces:
// when it begins terminating, and when it is gone; a change that leaves the names and the
// terminating ones as they were (a label) emits none.
func TestNamespaceEventsWhileFiltered(t *testing.T) {
	src := testkit.NewMemorySource()
	for _, n := range []string{"orders", "payments", "walk-gw9"} {
		src.Set(testkit.NamespacesGVR, nsObject(n, false, nil))
	}
	src.Set(testkit.PodsGVR, testkit.Pod("orders", "web-0"))
	s, r := nsWatchSession(t, src, nil)
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced })

	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"walk-gw9"} })
	waitFor(t, "the first namespaces event", func() bool { return r.count(EventNamespaces, "test") == 1 })

	src.Set(testkit.NamespacesGVR, nsObject("walk-gw9", true, nil)) // kubectl delete ns walk-gw9 --wait=false
	waitFor(t, "the event of walk-gw9 terminating", func() bool { return r.count(EventNamespaces, "test") == 2 })

	src.Delete(testkit.NamespacesGVR, "", "walk-gw9") // its finalizer removed
	waitFor(t, "the event of walk-gw9 gone", func() bool { return r.count(EventNamespaces, "test") == 3 })

	w := s.Backend().nsWatch
	before := w.steps.Load()
	src.Set(testkit.NamespacesGVR, nsObject("orders", false, map[string]any{"team": "shop"}))
	waitFor(t, "the label's change read", func() bool { return w.steps.Load() > before })
	if n := r.count(EventNamespaces, ""); n != 3 {
		t.Fatalf("%d namespaces events after a label changed, want still 3", n)
	}
	if n := r.count(EventNamespaces, "test"); n != 3 {
		t.Fatalf("events for another context: %v", r.all())
	}
}

// TestNamespaceEventsCoalesced: 300 namespaces created at once (a test suite's) send a few events,
// not one per watch event (1,000 events per second is the budget, AGENTS.md rule 8).
func TestNamespaceEventsCoalesced(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.NamespacesGVR, nsObject("orders", false, nil))
	s, r := nsWatchSession(t, src, nil)
	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"orders"} })
	waitFor(t, "the first namespaces event", func() bool { return r.count(EventNamespaces, "test") == 1 })
	w := s.Backend().nsWatch
	start := time.Now()
	for i := range 300 {
		src.Set(testkit.NamespacesGVR, nsObject(fmt.Sprintf("ns-%03d", i), false, nil))
	}
	waitFor(t, "the 301 namespaces read and announced", func() bool { return w.seen.Load() == 301 && r.count(EventNamespaces, "test") > 1 })
	elapsed := time.Since(start)
	// At most one event per coalescing interval (runCoalesced), however long the informer took to
	// deliver the burst: a few, not one per watch event.
	interval := 5 * time.Millisecond * nsWatchCoalesce
	if n, bound := r.count(EventNamespaces, "test")-1, int(elapsed/interval)+2; n > bound {
		t.Fatalf("%d events for 300 new namespaces over %v, want at most %d", n, elapsed, bound)
	}
}

// TestNamespaceWatchOnlyWhileFiltered: without a namespace filter the engine holds nothing on the
// namespaces; a filter takes one handle, clearing it gives the handle back.
func TestNamespaceWatchOnlyWhileFiltered(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.NamespacesGVR, nsObject("orders", false, nil))
	src.Set(testkit.PodsGVR, testkit.Pod("orders", "web-0"))
	s, r := nsWatchSession(t, src, nil)
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced })
	b := s.Backend()
	if n := nsHandles(b); n > 0 {
		t.Fatalf("no filter: %d handles on the namespaces", n)
	}
	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"orders"} })
	waitFor(t, "the namespaces held and read", func() bool { return nsHandles(b) == 1 && r.count(EventNamespaces, "test") == 1 })
	s.UpdateUI(func(u *UIState) { u.Namespaces = nil })
	waitFor(t, "the handle given back", func() bool { return nsHandles(b) == 0 })
}

// TestNamespaceWatchOfANamespaceLimitedUser: a user whose RoleBinding grants pods in team-a only
// may not list namespaces. With the filter on team-a the view loads, the engine never lists or
// watches the namespaces and sends no event (a related-objects index once left such users' views
// waiting for good).
func TestNamespaceWatchOfANamespaceLimitedUser(t *testing.T) {
	inner := testkit.NewMemorySource()
	inner.Set(testkit.NamespacesGVR, nsObject("team-a", false, nil))
	inner.Set(testkit.PodsGVR, testkit.Pod("team-a", "web-0"))
	src := &testkit.NamespacedOnlySource{Inner: inner, GVRs: []schema.GroupVersionResource{testkit.PodsGVR, testkit.NamespacesGVR}}
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"team-a": {rule("get,list,watch", "", "pods")}})
	s := New(Options{Connector: dockConnector{src: src, rev: rev, profiles: testProfiles(), namespace: "team-a"},
		SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	r := record(s)
	connectTest(t, s)
	b, w := s.Backend(), s.Backend().nsWatch
	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"team-a"} })
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 1 })
	waitFor(t, "the access reviews' answer on namespaces", func() bool {
		p, _ := b.Profile(namespacesKind)
		_, known := b.Listable(p, nil)
		return known
	})
	before := w.steps.Load()
	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"team-a", "team-b"} })
	waitFor(t, "the watch's step on the new filter", func() bool { return w.steps.Load() > before })
	if calls := callsOf(src, "namespaces"); len(calls) > 0 {
		t.Fatalf("the namespaces were read for a user who may not list them: %v", calls)
	}
	if n := r.count(EventNamespaces, ""); n != 0 || nsHandles(b) > 0 {
		t.Fatalf("%d namespaces events, %d handles", n, nsHandles(b))
	}
}

// switchedAccess answers "unknown" until on, then allows everything.
type switchedAccess struct{ on atomic.Bool }

func (a *switchedAccess) Allowed(string, Verb) (allowed, known bool) {
	on := a.on.Load()
	return on, on
}

// TestNamespaceWatchWaitsForTheAccessAnswer: while the access reviews have not said whether the
// user may list namespaces nothing is read; once they allow it the watch starts on its own.
func TestNamespaceWatchWaitsForTheAccessAnswer(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.NamespacesGVR, nsObject("orders", false, nil))
	access := &switchedAccess{}
	s, r := nsWatchSession(t, src, access)
	b, w := s.Backend(), s.Backend().nsWatch
	before := w.steps.Load()
	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"orders"} })
	waitFor(t, "the watch's step on the filter", func() bool { return w.steps.Load() > before })
	if n := nsHandles(b); n > 0 || src.Watching(cache.Scope{GVR: testkit.NamespacesGVR}) {
		t.Fatalf("read while the access is not known: %d handles", n)
	}
	access.on.Store(true)
	waitFor(t, "the watch after the answer", func() bool { return nsHandles(b) == 1 && r.count(EventNamespaces, "test") == 1 })
}

// TestNamespaceWatchEndsAtARefusal: access that says yes where the API server says no (an
// authorizer the access reviews cannot see): the refused list ends the watch, which is not retried
// until the access is refreshed, as the custom kinds' watch (customkinds.go).
func TestNamespaceWatchEndsAtARefusal(t *testing.T) {
	inner := testkit.NewMemorySource()
	inner.Set(testkit.NamespacesGVR, nsObject("orders", false, nil))
	src := &testkit.NamespacedOnlySource{Inner: inner, GVRs: []schema.GroupVersionResource{testkit.NamespacesGVR}}
	s := New(Options{Connector: refusingConnector{src: src}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	b := s.Backend()
	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"orders"} })
	// The informer ends with the refusal once no handle holds it: nothing retries the list.
	waitFor(t, "the refused watch ended", func() bool { return callsOf(src, "namespaces")["list namespaces"] == 1 && nsHandles(b) == -1 })
	b.RefreshAccess()
	waitFor(t, "the list asked again after the refresh", func() bool { return callsOf(src, "namespaces")["list namespaces"] == 2 })
}

// refusingConnector connects to a cache over src with access that allows everything.
type refusingConnector struct{ src cache.Source }

func (c refusingConnector) Contexts() ([]ContextInfo, error) {
	return []ContextInfo{{Name: "test", Current: true}}, nil
}

func (c refusingConnector) Connect(context.Context, string) (*Backend, error) {
	ch := cache.New(c.src, cache.Options{WarmPeriod: time.Minute})
	return &Backend{Context: "test", Namespace: "default", Cache: ch, Profiles: testProfiles(), Close: ch.Close}, nil
}
