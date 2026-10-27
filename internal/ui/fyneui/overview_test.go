package fyneui

import (
	"context"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// overviewEventsSnapshot is the page's events table as the engine builds it: the fixture events in
// the captured order (Type, Source, Namespace, Involved Object, Message, Count, Age, Last Seen as an
// age), the hidden name first, newest first.
func overviewEventsSnapshot(t *testing.T) *engine.TableSnapshot {
	src := kindSnapshot(t, "events", fixtureEventObjects())
	order := []string{"name", "type", "source", "namespace", "involvedObject", "message", "count", "age", "lastSeen"}
	idx := make([]int, len(order))
	cols := make([]model.Column, len(order))
	for i, id := range order {
		idx[i] = slices.IndexFunc(src.Columns, func(c model.Column) bool { return c.ID == id })
		if idx[i] < 0 {
			t.Fatalf("the events profile has no column %s", id)
		}
		cols[i] = src.Columns[idx[i]]
	}
	cols[len(cols)-1].Type = model.ColumnAge
	rows := make([]model.Row, len(src.Rows))
	for ri, r := range src.Rows {
		r.Cells = make([]model.Cell, len(idx))
		for i, j := range idx {
			r.Cells[i] = src.Rows[ri].Cells[j]
		}
		rows[ri] = r
	}
	last := len(cols) - 1
	slices.SortStableFunc(rows, func(a, b model.Row) int { return int(b.Cells[last].Num - a.Cells[last].Num) })
	return &engine.TableSnapshot{
		View: engine.WorkloadsOverview, Context: "demo", Title: "Overview", Namespaced: true, Columns: cols, Rows: rows,
		Sort: engine.SortSpec{Column: "lastSeen", Desc: true}, Total: len(rows), Revision: 1, Synced: true,
	}
}

// overviewKind counts a fixture view's rows by status, as the engine counts a kind's objects.
func overviewKind(f *fakeEngine, key, title string, column int) engine.OverviewKind {
	k := engine.OverviewKind{Kind: key, Title: title, Column: column, Synced: true}
	counts := map[model.Status]int{}
	if s := f.tables[key]; s != nil {
		for _, r := range s.Rows {
			counts[r.Status]++
			k.Count++
		}
	}
	for _, st := range engine.StatusOrder {
		if counts[st] > 0 {
			k.Statuses = append(k.Statuses, engine.StatusCount{Status: st, Count: counts[st]})
		}
	}
	return k
}

// overviewFake is the demo fake on the Workloads Overview: the fixture's pods and deployments,
// served but empty Stateful Sets, Replica Sets and Jobs, and the fixture's events.
func overviewFake(t *testing.T) *fakeEngine {
	f := kindsFake(t, engine.WorkloadsOverview)
	f.tables[engine.WorkloadsOverview] = overviewEventsSnapshot(t)
	f.ui.Sort = engine.SortSpec{Column: "lastSeen", Desc: true}
	m := engine.WorkloadsOverviewModel{Synced: true, Kinds: []engine.OverviewKind{
		overviewKind(f, "pods", "Pods", 0), overviewKind(f, "deployments", "Deployments", 0),
		overviewKind(f, "statefulsets", "Stateful Sets", 0),
		overviewKind(f, "replicasets", "Replica Sets", 1), overviewKind(f, "jobs", "Jobs", 1),
	}}
	f.handlers["workloads.overview"] = func(context.Context, engine.Invocation) (any, error) { return m, nil }
	return f
}

// TestWorkloadsOverviewPage: Workloads › Overview opens the page; it shows a tile per served kind
// in two columns with its count and health, the events below with their own header (count,
// download, search), and the namespace filter beside the page's title. A tile opens its kind's
// view, an event row its details.
func TestWorkloadsOverviewPage(t *testing.T) {
	f := overviewFake(t)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the page", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && len(sh.view.overview.tiles) == 5
	})
	if kids := sh.side.children[groupPrefix+"workloads"]; len(kids) == 0 || kids[0] != engine.WorkloadsOverview ||
		sh.side.titles[engine.WorkloadsOverview] != "Overview" {
		t.Fatalf("Workloads in the sidebar: %v", kids)
	}
	tv := sh.view
	if !tv.pageMode || !tv.pageBox.Visible() || tv.title.text != "Overview" || tv.count.text != "19 items" {
		t.Fatalf("page layout %v, title %q, count %q", tv.pageMode, tv.title.text, tv.count.text)
	}
	if !holds(tv.eventsHeading.Objects, tv.count) || !holds(tv.eventsTools.Objects, tv.searchBox) ||
		!holds(tv.tools.Objects, tv.nsBox) || holds(tv.tools.Objects, tv.searchBox) {
		t.Fatal("the count and the search are not in the events' header, or the namespace filter not beside the title")
	}
	if tv.search.PlaceHolder != "Search events…" {
		t.Fatalf("search hint %q", tv.search.PlaceHolder)
	}
	var left, right []string
	for _, o := range tv.overview.cols[0].Objects {
		left = append(left, o.(*overviewTile).kind)
	}
	for _, o := range tv.overview.cols[1].Objects {
		right = append(right, o.(*overviewTile).kind)
	}
	if !slices.Equal(left, []string{"pods", "deployments", "statefulsets"}) || !slices.Equal(right, []string{"replicasets", "jobs"}) {
		t.Fatalf("tiles %v | %v", left, right)
	}
	pods := tv.overview.tiles["pods"]
	if pods.link.text != "Pods" || pods.count.text != formatCount(int64(len(f.tables["pods"].Rows))) || pods.bar.tipText() == "none" {
		t.Fatalf("pods tile %q %q, bar %q", pods.link.text, pods.count.text, pods.bar.tipText())
	}
	if got := tv.overview.tiles["jobs"].bar.tipText(); got != "none" {
		t.Fatalf("an empty kind's bar says %q", got)
	}

	test.Tap(tv.cellAt(t, 0, 2))
	q.until(t, "the event's drawer", func() bool { return sh.ui.Drawer != nil })
	if d := *sh.ui.Drawer; d.Kind != "events" || sh.ui.View != engine.WorkloadsOverview {
		t.Fatalf("a row of the page opened %v on view %s", d, sh.ui.View)
	}
	test.Tap(pods.link)
	q.until(t, "the pods view", func() bool { return sh.ui.View == "pods" && tv.snap != nil && tv.snap.View == "pods" })
	if tv.pageMode || tv.pageBox.Visible() || !holds(tv.tools.Objects, tv.searchBox) {
		t.Fatal("a kind's view kept the page's layout")
	}
}

// holds reports whether a container's objects include o.
func holds(objs []fyne.CanvasObject, o fyne.CanvasObject) bool {
	return slices.ContainsFunc(objs, func(x fyne.CanvasObject) bool { return x == o })
}

// cellAt returns the body cell the table shows for a row and visible column.
func (tv *tableView) cellAt(t *testing.T, row, col int) *tableCell {
	t.Helper()
	for _, c := range tv.cells {
		if c.row == row && c.col == col && c.Visible() {
			return c
		}
	}
	t.Fatalf("no cell %d,%d on screen", row, col)
	return nil
}

func renderOverview(t *testing.T, mode ThemeMode, name string) {
	f := overviewFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the page", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && len(sh.view.overview.tiles) == 5 && sh.diag.total > 0
	})
	settle(w)
	savePNG(t, w, name)
}

func TestRenderWorkloadsOverviewDark(t *testing.T) {
	renderOverview(t, ThemeDark, "workloads_overview_dark.png")
}
