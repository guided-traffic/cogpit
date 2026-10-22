package engine

import (
	"context"
	"slices"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

func overviewPod(namespace, name, phase string) *unstructured.Unstructured {
	p := testkit.Pod(namespace, name)
	_ = unstructured.SetNestedField(p.Object, phase, "status", "phase")
	if phase != "Running" {
		unstructured.RemoveNestedField(p.Object, "status", "containerStatuses")
	}
	return p
}

// overviewSession is a session over the real profiles of the overview's kinds, without Daemon
// Sets and Cron Jobs (a cluster that does not serve them).
func overviewSession(t *testing.T, src *testkit.MemorySource) *Session {
	t.Helper()
	var profiles []*model.KindProfile
	for _, key := range []string{"pods", "deployments", "statefulsets", "replicasets", "jobs", "events", "namespaces"} {
		p, ok := kinds.ByKey(key)
		if !ok {
			t.Fatalf("no profile %s", key)
		}
		profiles = append(profiles, p)
	}
	s := New(Options{Connector: &fakeConnector{src: src, profiles: profiles}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	return s
}

func handlesOn(s *Session, resource string) int {
	n := 0
	for _, st := range s.Backend().Cache.Stats() {
		if st.Scope.GVR.Resource == resource {
			n += st.Handles
		}
	}
	return n
}

// TestWorkloadsOverviewPage: the page counts the served workload kinds in the selected namespaces
// with their health from the profiles' status tokens, lists those namespaces' events as captured
// (Type, Source, Namespace, Involved Object, Message, Count, Age, Last Seen as an age, newest
// first), follows the cache, and holds its handles only while it is open.
func TestWorkloadsOverviewPage(t *testing.T) {
	now := time.Now()
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, overviewPod("shop", "web-0", "Running"))
	src.Set(testkit.PodsGVR, overviewPod("shop", "web-1", "Pending"))
	src.Set(testkit.PodsGVR, overviewPod("infra", "db-0", "Failed"))
	src.Set(testkit.EventsGVR, event("shop", "e1", "Pod", "web-1", "", "FailedScheduling", "Warning", now.Add(-2*time.Minute)))
	src.Set(testkit.EventsGVR, event("shop", "e2", "Pod", "web-0", "", "Started", "Normal", now.Add(-time.Minute)))
	src.Set(testkit.EventsGVR, event("infra", "e3", "Pod", "db-0", "", "BackOff", "Warning", now.Add(-3*time.Minute)))
	s := overviewSession(t, src)

	if title, ok := PageTitle(WorkloadsOverview); !ok || title != "Overview" || !IsPage(WorkloadsOverview) || IsPage("pods") {
		t.Fatal("the overview is not a page")
	}
	s.UpdateUI(func(u *UIState) { u.View, u.Sort = WorkloadsOverview, s.ViewSort(WorkloadsOverview) })
	if sort := s.UIState().Sort; sort.Column != lastSeenColumn || !sort.Desc {
		t.Fatalf("the page's events sort by %+v, want last seen, newest first", sort)
	}
	snap := waitTable(t, s, WorkloadsOverview, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 3 })
	var ids []string
	for _, c := range snap.Columns {
		if !c.Hidden {
			ids = append(ids, c.ID)
		}
	}
	if want := []string{"type", "source", "namespace", "involvedObject", "message", "count", "age", "lastSeen"}; !slices.Equal(ids, want) {
		t.Fatalf("event columns %v, want %v", ids, want)
	}
	if last := snap.Columns[len(snap.Columns)-1]; last.Type != model.ColumnAge {
		t.Fatalf("Last Seen is %s, want an age", last.Type)
	}
	if snap.View != WorkloadsOverview || snap.Title != "Overview" || snap.Rows[0].Ref.Name != "e2" || snap.Rows[0].Ref.Kind != "events" {
		t.Fatalf("page table %s (view %s, title %s)", summary(snap), snap.View, snap.Title)
	}
	if got := model.CellText(snap.Columns[len(snap.Columns)-1], snap.Rows[0].Cells[len(snap.Columns)-1], now); got != "1m" {
		t.Fatalf("Last Seen reads %q, want 1m", got)
	}
	// The page's kinds sync on their own informers; a slow machine may not have them all yet.
	var m *WorkloadsOverviewModel
	wctx, wcancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer wcancel()
	testkit.WaitFor(wctx, func() bool {
		var err error
		m, _, err = s.WorkloadsOverview(context.Background(), "", nil, false)
		if err != nil {
			t.Fatal(err)
		}
		return m.Synced
	})
	var keys []string
	for _, k := range m.Kinds {
		keys = append(keys, k.Kind)
	}
	if want := []string{"pods", "deployments", "statefulsets", "replicasets", "jobs"}; !slices.Equal(keys, want) || !m.Synced {
		t.Fatalf("kinds %v (synced %v), want the served ones in the page's order %v", keys, m.Synced, want)
	}
	pods := m.Kinds[0]
	if pods.Count != 3 || pods.Column != 0 || m.Kinds[3].Column != 1 ||
		!slices.Equal(pods.Statuses, []StatusCount{{model.StatusOK, 1}, {model.StatusPending, 1}, {model.StatusError, 1}}) {
		t.Fatalf("pods %+v", pods)
	}
	if handlesOn(s, "pods") != 1 || handlesOn(s, "jobs") != 1 || handlesOn(s, "events") != 1 {
		t.Fatalf("handles while open: pods %d, jobs %d, events %d", handlesOn(s, "pods"), handlesOn(s, "jobs"), handlesOn(s, "events"))
	}

	// The namespace filter narrows the counts and the events; the cache's changes follow.
	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"shop"} })
	waitNamespacesWatched(t, src) // the filter has the engine watch the namespaces (nswatch.go)
	src.Set(testkit.PodsGVR, overviewPod("shop", "web-1", "Running"))
	waitTable(t, s, WorkloadsOverview, func(t *TableSnapshot) bool { return len(t.Rows) == 2 })
	waitFor(t, "the pending pod to run", func() bool {
		m, _, _ := s.WorkloadsOverview(context.Background(), "", nil, false)
		return m.Kinds[0].Count == 2 && slices.Equal(m.Kinds[0].Statuses, []StatusCount{{model.StatusOK, 2}})
	})

	// Another view releases the page's handles; the informers stay warm, so opening the page again
	// costs the API server nothing.
	s.UpdateUI(func(u *UIState) { u.View, u.Sort = "pods", s.ViewSort("pods") })
	waitFor(t, "the page's handles to go", func() bool { return handlesOn(s, "jobs") == 0 && handlesOn(s, "events") == 0 })
	if s.Table(WorkloadsOverview) != nil {
		t.Fatal("a closed page still has a table")
	}
	lists, watches := src.Lists(), src.Watches()
	s.UpdateUI(func(u *UIState) { u.View, u.Sort = WorkloadsOverview, s.ViewSort(WorkloadsOverview) })
	waitTable(t, s, WorkloadsOverview, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 2 })
	if src.Lists() != lists || src.Watches() != watches {
		t.Fatalf("reopening the page within the warm period cost %d lists and %d watches", src.Lists()-lists, src.Watches()-watches)
	}
	s.UpdateUI(func(u *UIState) { u.View, u.Sort = "pods", s.ViewSort("pods") })

	// Built on demand when the page is closed, for other namespaces, with its events.
	m, events, err := s.WorkloadsOverview(context.Background(), "", []string{"infra"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kinds[0].Count != 1 || events == nil || len(events.Rows) != 1 || events.Rows[0].Ref.Name != "e3" {
		t.Fatalf("on demand: pods %+v, events %s", m.Kinds[0], summary(events))
	}
	if handlesOn(s, "jobs") != 0 {
		t.Fatal("the on-demand read kept its handles")
	}
}

// TestWorkloadsOverviewWithoutEventsStartsNoEventsWatch: an agent's workloads.overview without
// events reads the workload kinds only: it starts no cluster-wide events watch, which would stay
// warm for minutes after the read.
func TestWorkloadsOverviewWithoutEventsStartsNoEventsWatch(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, overviewPod("shop", "web-0", "Running"))
	s := overviewSession(t, src)
	if _, events, err := s.WorkloadsOverview(context.Background(), "", nil, false); err != nil || events != nil {
		t.Fatalf("overview without events: %v, events %v", err, events)
	}
	evp, _ := s.Backend().Profile("events")
	if s.Backend().Cached(evp) {
		t.Error("the overview without events started the events watch")
	}
	if _, events, err := s.WorkloadsOverview(context.Background(), "", nil, true); err != nil || events == nil {
		t.Fatalf("overview with events: %v, events %v", err, events)
	}
	if !s.Backend().Cached(evp) {
		t.Error("the overview with events did not read the events")
	}
}
