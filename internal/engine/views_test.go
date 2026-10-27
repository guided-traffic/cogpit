package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

func TestConnectLifecycle(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("default", "web-0"))
	s, _ := newTestSession(t, src, 10*time.Millisecond)
	rec := record(s)

	if s.Backend() != nil {
		t.Fatal("backend before connecting")
	}
	connectTest(t, s)
	u := s.UIState()
	if u.Context != "test" || u.Connection.State != Connected || u.View != "pods" || u.Sort != DefaultSort {
		t.Fatalf("after connect: %+v", u)
	}
	// The states the connection went through, in order: other UI state events while connected (the
	// integrations of a new connection, the Alertmanager's first answer among them) repeat a state.
	var states []ConnectionState
	for _, ev := range rec.all() {
		if ev.Type == EventUIState && (len(states) == 0 || states[len(states)-1] != ev.UIState.Connection.State) {
			states = append(states, ev.UIState.Connection.State)
		}
	}
	if !slices.Equal(states, []ConnectionState{Connecting, Connected}) {
		t.Errorf("connection states %v, want connecting, connected", states)
	}
	snap := waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 1 })
	if snap.Context != "test" || snap.Title != "Pods" || snap.Rows[0].Ref.Context != "test" {
		t.Errorf("snapshot %+v", snap)
	}
	waitFor(t, "the snapshot's table event", func() bool { return rec.announced("pods", snap.Revision) })

	// A failing connect leaves no backend and the error in the UI state.
	err := s.Connect(context.Background(), "broken")
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("connect broken: %v", err)
	}
	u = s.UIState()
	if u.Context != "broken" || u.Connection.State != ConnectError || !strings.Contains(u.Connection.Error, "refused") || u.View != "" {
		t.Errorf("after failed connect: %+v", u)
	}
	if s.Backend() != nil || s.Table("pods") != nil {
		t.Error("the old backend survived a failed connect")
	}

	connectTest(t, s)
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 1 })

	s.Close()
	if s.Backend() != nil {
		t.Error("backend after close")
	}
	if err := s.Connect(context.Background(), "test"); !errors.Is(err, ErrClosed) {
		t.Errorf("connect after close: %v", err)
	}
}

func TestConnectWithoutConnector(t *testing.T) {
	s := New(Options{})
	defer s.Close()
	if err := s.Connect(context.Background(), "x"); err == nil {
		t.Error("connect without a connector succeeded")
	}
	if _, err := s.Contexts(); err == nil {
		t.Error("contexts without a connector succeeded")
	}
}

func TestConnectSupersedesPendingConnect(t *testing.T) {
	s, _ := newTestSession(t, testkit.NewMemorySource(), 10*time.Millisecond)
	slow := make(chan error, 1)
	go func() { slow <- s.Connect(context.Background(), "slow") }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { return s.UIState().Context == "slow" }) {
		t.Fatal("slow connect did not start")
	}
	connectTest(t, s)
	if err := <-slow; err == nil {
		t.Error("superseded connect reported success")
	}
	u := s.UIState()
	if u.Context != "test" || u.Connection.State != Connected {
		t.Errorf("after supersede: %+v", u.Connection)
	}
}

func TestViewSnapshotNamespaceAndSearchFilter(t *testing.T) {
	src := testkit.NewMemorySource()
	for _, ns := range []string{"shop", "infra"} {
		for _, n := range []string{"web-0", "db-0"} {
			src.Set(testkit.PodsGVR, testkit.Pod(ns, n))
		}
	}
	src.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", "shop", nil))
	src.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", "infra", nil))
	s, _ := newTestSession(t, src, 10*time.Millisecond)
	connectTest(t, s)
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 4 })

	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"shop"} })
	snap := waitTable(t, s, "pods", func(t *TableSnapshot) bool { return len(t.Namespaces) == 1 })
	if got := rowKeys(snap); !slices.Equal(got, []string{"shop/db-0", "shop/web-0"}) || snap.Total != 4 {
		t.Errorf("namespace filter: rows %v total %d", got, snap.Total)
	}

	s.UpdateUI(func(u *UIState) { u.Filter = "WEB" })
	snap = waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Filter == "WEB" })
	if got := rowKeys(snap); !slices.Equal(got, []string{"shop/web-0"}) {
		t.Errorf("search filter: rows %v", got)
	}

	// Watch events reach the snapshot; the filters still apply.
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-1"))
	src.Set(testkit.PodsGVR, testkit.Pod("infra", "web-2"))
	snap = waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Total == 6 })
	if got := rowKeys(snap); !slices.Equal(got, []string{"shop/web-0", "shop/web-1"}) {
		t.Errorf("after events: rows %v", got)
	}
	src.Delete(testkit.PodsGVR, "shop", "web-0")
	snap = waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Total == 5 })
	if got := rowKeys(snap); !slices.Equal(got, []string{"shop/web-1"}) {
		t.Errorf("after delete: rows %v", got)
	}

	// Cluster-scoped views ignore the namespace filter.
	s.UpdateUI(func(u *UIState) { u.View, u.Filter = "namespaces", "" })
	snap = waitTable(t, s, "namespaces", func(t *TableSnapshot) bool { return t.Synced })
	if len(snap.Rows) != 2 {
		t.Errorf("namespaces view: rows %v", rowKeys(snap))
	}
	if s.Table("pods") != nil {
		t.Error("closed view still has a snapshot")
	}
}

func TestSortEmptyLastAndTies(t *testing.T) {
	cols := []model.Column{
		{ID: "name", Type: model.ColumnText}, {ID: "node", Type: model.ColumnText},
		{ID: "restarts", Type: model.ColumnNumber}, {ID: "age", Type: model.ColumnAge},
	}
	row := func(ns, name, node string, restarts, created int64) model.Row {
		return model.Row{
			Ref:   model.ObjectRef{Kind: "pods", Namespace: ns, Name: name},
			Cells: []model.Cell{{Text: name}, {Text: node}, {Text: fmt.Sprint(restarts), Num: restarts}, {Num: created}},
		}
	}
	rows := []model.Row{
		row("b", "web", "Node-b", 3, 300),
		row("a", "web", "node-a", 0, 0),
		row("a", "api", "", 1, 100),
		row("c", "Zed", "node-C", 3, 200),
		row("a", "db", "", 0, 0),
	}
	keys := func(rs []model.Row) []string {
		out := make([]string, 0, len(rs))
		for _, r := range rs {
			out = append(out, r.Ref.Key())
		}
		return out
	}
	cases := []struct {
		spec SortSpec
		want []string
	}{
		{SortSpec{Column: "name"}, []string{"a/api", "a/db", "a/web", "b/web", "c/Zed"}},
		{SortSpec{Column: "name", Desc: true}, []string{"c/Zed", "a/web", "b/web", "a/db", "a/api"}},
		{SortSpec{Column: "node"}, []string{"a/web", "b/web", "c/Zed", "a/api", "a/db"}},
		{SortSpec{Column: "node", Desc: true}, []string{"c/Zed", "b/web", "a/web", "a/api", "a/db"}},
		{SortSpec{Column: "restarts"}, []string{"a/db", "a/web", "a/api", "b/web", "c/Zed"}},
		{SortSpec{Column: "restarts", Desc: true}, []string{"b/web", "c/Zed", "a/api", "a/db", "a/web"}},
		{SortSpec{Column: "age"}, []string{"a/api", "c/Zed", "b/web", "a/db", "a/web"}},
		{SortSpec{Column: "age", Desc: true}, []string{"b/web", "c/Zed", "a/api", "a/db", "a/web"}},
		{SortSpec{Column: "nope"}, []string{"a/api", "a/db", "a/web", "b/web", "c/Zed"}},
	}
	for _, c := range cases {
		got := slices.Clone(rows)
		SortRows(got, cols, c.spec)
		if !slices.Equal(keys(got), c.want) {
			t.Errorf("%+v: %v, want %v", c.spec, keys(got), c.want)
		}
		// The snapshot path sorts the same way.
		es := make([]*rowEntry, len(rows))
		for i := range rows {
			es[i] = &rowEntry{row: rows[i]}
		}
		sortEntries(es, cols, c.spec)
		var viaEntries []model.Row
		for _, e := range es {
			viaEntries = append(viaEntries, e.row)
		}
		if !slices.Equal(keys(viaEntries), c.want) {
			t.Errorf("entries %+v: %v, want %v", c.spec, keys(viaEntries), c.want)
		}
	}
	if compareFold("Ärger", "ärger") != 0 || compareFold("abc", "ABD") >= 0 || compareFold("ab", "abc") >= 0 {
		t.Error("compareFold")
	}
}

func TestSortRememberedPerView(t *testing.T) {
	src := testkit.NewMemorySource()
	for i := range 3 {
		p := testkit.Pod("default", fmt.Sprintf("web-%d", i))
		p.Object["status"].(map[string]any)["containerStatuses"].([]any)[0].(map[string]any)["restartCount"] = int64(i)
		src.Set(testkit.PodsGVR, p)
	}
	s, _ := newTestSession(t, src, 10*time.Millisecond)
	connectTest(t, s)
	restarts := SortSpec{Column: "restarts", Desc: true}
	s.UpdateUI(func(u *UIState) { u.Sort = restarts })
	snap := waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Sort == restarts && len(t.Rows) == 3 })
	if got := rowKeys(snap); !slices.Equal(got, []string{"default/web-2", "default/web-1", "default/web-0"}) {
		t.Errorf("sorted by restarts: %v", got)
	}
	if got := s.ViewSort("services"); got != DefaultSort {
		t.Errorf("services default sort %+v", got)
	}
	if got := s.ViewSort("events"); got != (SortSpec{Column: "lastSeen", Desc: true}) {
		t.Errorf("events default sort %+v", got)
	}
	svcSort := s.ViewSort("services")
	s.UpdateUI(func(u *UIState) { u.View, u.Sort = "services", svcSort })
	if got := s.ViewSort("pods"); got != restarts {
		t.Errorf("pods sort not remembered: %+v", got)
	}
}

func TestSnapshotsCoalesce(t *testing.T) {
	src := testkit.NewMemorySource()
	for i := range 100 {
		src.Set(testkit.PodsGVR, testkit.Pod("default", fmt.Sprintf("web-%03d", i)))
	}
	const interval = 30 * time.Millisecond
	s, _ := newTestSession(t, src, interval)
	rec := record(s)
	connectTest(t, s)
	settled := waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 100 })
	waitFor(t, "the synced snapshot's event", func() bool { return rec.announced("pods", settled.Revision) })
	before := rec.count(EventTable, "pods")

	const updates = 300
	start := time.Now()
	for i := range updates {
		time.Sleep(time.Until(start.Add(time.Duration(i) * time.Millisecond)))
		p := testkit.Pod("default", fmt.Sprintf("web-%03d", i%100))
		p.SetLabels(map[string]string{"round": fmt.Sprint(i)})
		src.Set(testkit.PodsGVR, p)
	}
	elapsed := time.Since(start)
	// The source numbers resource versions 1, 2, ...: the last update is version 400.
	lastName, lastRV := fmt.Sprintf("web-%03d", (updates-1)%100), fmt.Sprint(100+updates)
	last := waitTable(t, s, "pods", func(snap *TableSnapshot) bool {
		i := snap.IndexOf(model.ObjectRef{Kind: "pods", Namespace: "default", Name: lastName})
		return snap.Revision > settled.Revision && i >= 0 && snap.Rows[i].ResourceVersion == lastRV
	})
	waitFor(t, "the last snapshot's event", func() bool { return rec.announced("pods", last.Revision) })
	n := rec.count(EventTable, "pods") - before
	// One snapshot when the burst starts, then at most one per interval (runCoalesced never builds
	// sooner), and a trailing one with the last update: 11 for a burst of 300 ms.
	bound := int(elapsed/interval) + 2
	t.Logf("%d updates over %v: %d snapshots (bound %d)", updates, elapsed.Round(time.Millisecond), n, bound)
	if n > bound {
		t.Errorf("%d snapshots for %d updates over %v, want at most %d", n, updates, elapsed, bound)
	}
	if n < 2 {
		t.Errorf("only %d snapshots: updates were not shown while they arrived", n)
	}
}

func TestViewReportsInformerErrorsAndSync(t *testing.T) {
	src := testkit.NewMemorySource()
	s, _ := newTestSession(t, src, 10*time.Millisecond)
	connectTest(t, s)
	// An empty kind sends no event; the snapshot must still turn synced.
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced && t.Total == 0 })
}

func TestUIChangedIgnoresOlderRevisions(t *testing.T) {
	src := testkit.NewMemorySource()
	s, _ := newTestSession(t, src, 10*time.Millisecond)
	connectTest(t, s)
	// The newest revision applied first: a new connection's integrations change the UI state on
	// their own (the Alertmanager's first answer among them), and a change that is only on its way is
	// not stale yet.
	var now UIState
	waitCond(t, "the newest UI state applied", func() bool {
		now = s.UIState()
		s.state.mu.Lock()
		defer s.state.mu.Unlock()
		return s.state.appliedRev == now.Revision
	})
	old := now
	old.Revision--
	old.View = "services"
	s.state.uiChanged(old, old) // arrives late: must not reopen services
	s.state.mu.Lock()
	view := s.state.view.profile.Key
	s.state.mu.Unlock()
	if view != "pods" {
		t.Errorf("a stale UI change opened %s", view)
	}
}

// The session runs in a testing/synctest bubble: the diagnostics' ticker of a second passes on the
// bubble's clock.
func TestDiagnosticsCountSnapshotsAndInformers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := testkit.NewMemorySource()
		src.Set(testkit.PodsGVR, testkit.Pod("default", "web-0"))
		s, _ := newTestSession(t, src, 10*time.Millisecond)
		if d := s.Diagnostics(); d.Snapshots != 0 || len(d.Informers) != 0 {
			t.Errorf("diagnostics before connect: %+v", d)
		}
		rec := record(s)
		connectTest(t, s)
		waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced })
		d := s.Diagnostics()
		if d.Context != "test" || d.Snapshots == 0 || len(d.Informers) != 1 || d.Informers[0].Objects != 1 {
			t.Errorf("diagnostics %+v", d)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if !testkit.WaitFor(ctx, func() bool { return rec.count(EventDiagnostics, "") > 0 }) {
			t.Error("no diagnostics event")
		}
	})
}
