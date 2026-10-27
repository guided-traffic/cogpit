package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The node drawer's Pods table with its pods' CPU and Memory and sortable columns (owner,
// 2026-10-13). The fake's drawer models go through the engine's own fill and sort
// (engine.FillPodTable, engine.SortDrawerTables), and its ui.sortDrawerTable does what the
// engine's does: it sets the UI state's order and announces the drawer.

// fixturePodUsage is what the fake Metrics Server measures of a fixture pod: running pods, and
// those that crash or are not ready, use some CPU and memory; completed and never-started ones
// have no measurement, as with a real Metrics Server.
func fixturePodUsage() map[string]model.Usage {
	out := map[string]model.Usage{}
	for i, p := range fixturePodSpecs() {
		switch p.state {
		case podOK, podCrashLoop, podNotReady:
			out[p.ns+"/"+p.name] = model.Usage{CPUMilli: int64(3 + i*37%900), MemoryBytes: int64(20+i*53%700) << 20}
		}
	}
	return out
}

// nodePodsSortFake is kindsFake with the drawer open on the busy node, its Pods table filled from
// fixturePodUsage and sorted as sorts says, and ui.sortDrawerTable answered as the engine does.
func nodePodsSortFake(t testing.TB, sorts map[string]engine.SortSpec) (*fakeEngine, model.ObjectRef) {
	t.Helper()
	f := kindsFake(t, "nodes")
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: busyNode}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	f.ui.DrawerSort = sorts
	usage := fixturePodUsage()
	pods, _ := kinds.ByKey("pods")
	base := f.details[ref.String()]
	build := func(sorts map[string]engine.SortSpec) *engine.DetailModel {
		m := *base
		m.Sections = slices.Clone(base.Sections)
		for i, sec := range m.Sections {
			if sec.Table == nil || !sec.Table.Sortable {
				continue
			}
			tbl := *sec.Table
			tbl.Rows, tbl.Cells = cloneRows(tbl.Rows), cloneRows(tbl.Cells)
			engine.FillPodTable(&tbl, func(ns, name string) (model.Usage, bool) {
				u, ok := usage[ns+"/"+name]
				return u, ok
			}, func(u model.Usage) (model.Cell, model.Cell) { return pods.UsageCells(nil, u) })
			m.Sections[i].Table = &tbl
		}
		engine.SortDrawerTables(&m, sorts)
		return &m
	}
	f.details[ref.String()] = build(sorts)
	f.handlers["ui.sortDrawerTable"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p drawerSortParams
		if err := json.Unmarshal(inv.Params, &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			s := map[string]engine.SortSpec{}
			for k, v := range u.DrawerSort {
				s[k] = v
			}
			s[p.Section] = engine.SortSpec{Column: p.Column, Desc: p.Desc}
			u.DrawerSort = s
		})
		f.mu.Lock()
		f.details[ref.String()] = build(f.ui.DrawerSort)
		f.mu.Unlock()
		f.emit(engine.Event{Type: engine.EventDetail, View: ref.Kind})
		return f.UIState(), nil
	}
	return f, ref
}

func cloneRows[T any](rows [][]T) [][]T {
	out := make([][]T, len(rows))
	for i, r := range rows {
		out[i] = slices.Clone(r)
	}
	return out
}

// drawerHeaders returns the drawer's sortable column titles by title.
func drawerHeaders(sh *shell) map[string]*tableSortHeader {
	out := map[string]*tableSortHeader{}
	for _, h := range findAll[*tableSortHeader](sh.drawer.details) {
		out[h.title] = h
	}
	return out
}

// podsColumn returns the texts of one column of the drawer's Pods table, row by row: the column's
// title, then each row's text in the order the drawer shows them.
func podsColumn(t *testing.T, sh *shell, title string) []string {
	t.Helper()
	texts := drawerTexts(sh)
	start := -1
	for i, s := range texts {
		if s == "Pods" && i+1 < len(texts) && texts[i+1] == "Name" {
			start = i + 1
		}
	}
	if start < 0 {
		t.Fatalf("no Pods table in %q", texts)
	}
	m := sh.drawer.last
	var tbl *model.DetailTable
	for _, sec := range m.Sections {
		if sec.Title == "Pods" {
			tbl = sec.Table
		}
	}
	col := slices.Index(tbl.Columns, title)
	if col < 0 {
		t.Fatalf("no column %s in %q", title, tbl.Columns)
	}
	var out []string
	for r := range tbl.Rows {
		out = append(out, tbl.Rows[r][col])
	}
	return out
}

// The node's Pods table shows its pods' CPU and Memory as the Pods list writes them; its column
// titles look like the lists' and sort it: a click on Memory sorts ascending with the arrow up,
// a second click descending with the arrow down, a pod without a measurement last both times.
func TestNodeDrawerPodsSortByHeader(t *testing.T) {
	f, _ := nodePodsSortFake(t, nil)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil && len(drawerHeaders(sh)) > 0 })
	settle(w)

	// The values, as the Pods list writes them (the Pods profile's cells).
	usage := fixturePodUsage()
	pods, _ := kinds.ByKey("pods")
	texts := drawerTexts(sh)
	shown := 0
	for _, p := range fixturePodSpecs() {
		u, ok := usage[p.ns+"/"+p.name]
		if p.node != busyNode || !ok {
			continue
		}
		cpu, mem := pods.UsageCells(nil, u)
		if !slices.Contains(texts, cpu.Text) || !slices.Contains(texts, mem.Text) {
			t.Errorf("%s: %s and %s not in the drawer", p.name, cpu.Text, mem.Text)
		}
		shown++
	}
	if shown < 3 {
		t.Fatalf("only %d measured pods on %s: the fixture shows too little", shown, busyNode)
	}

	headers := drawerHeaders(sh)
	for _, title := range []string{"Name", "Node", "Namespace", "Ready", "CPU", "Memory", "Status"} {
		if headers[title] == nil {
			t.Errorf("no sortable title %s among %v", title, keys(headers))
		}
	}
	if h := headers["Name"]; h == nil || h.sort != 1 || headers["Memory"].sort != 0 {
		t.Errorf("unsorted: the arrow is not on Name alone")
	}

	sortBy := func(title string, wantDesc bool) {
		t.Helper()
		before := len(f.callsOf("ui.sortDrawerTable"))
		test.Tap(drawerHeaders(sh)[title])
		q.until(t, "ui.sortDrawerTable", func() bool { return len(f.callsOf("ui.sortDrawerTable")) == before+1 })
		p := decodeParams[drawerSortParams](t, f.callsOf("ui.sortDrawerTable")[before])
		if p.Section != "Pods" || p.Column != model.ColumnMemory || p.Desc != wantDesc {
			t.Fatalf("click on %s sent %+v", title, p)
		}
		want := 1
		if wantDesc {
			want = -1
		}
		q.until(t, "the sorted table", func() bool {
			h := drawerHeaders(sh)[title]
			return h != nil && h.sort == want
		})
		if h := drawerHeaders(sh)["Name"]; h.sort != 0 {
			t.Errorf("the arrow stays on Name after sorting by %s", title)
		}
	}
	checkOrder := func(desc bool) {
		t.Helper()
		var nums []int64
		var empty []int
		col := podsColumn(t, sh, "Memory")
		m := sh.drawer.last
		for _, sec := range m.Sections {
			if sec.Title != "Pods" {
				continue
			}
			ci := sec.Table.ColumnIndex(model.ColumnMemory)
			for r, cells := range sec.Table.Cells {
				if cells[ci].Text == "" {
					empty = append(empty, r)
					continue
				}
				nums = append(nums, cells[ci].Num)
			}
		}
		if len(empty) == 0 || empty[0] != len(col)-len(empty) {
			t.Errorf("pods without a measurement not last: rows %v of %d", empty, len(col))
		}
		sorted := slices.IsSorted(nums)
		if desc {
			slices.Reverse(nums)
			sorted = slices.IsSorted(nums)
		}
		if !sorted {
			t.Errorf("memory not in order (desc %v): %q", desc, col)
		}
	}
	sortBy("Memory", false)
	checkOrder(false)
	sortBy("Memory", true)
	checkOrder(true)
	if got := f.UIState().DrawerSort["Pods"]; got != (engine.SortSpec{Column: model.ColumnMemory, Desc: true}) {
		t.Errorf("ui state %+v", got)
	}
}

// A narrow drawer shortens the sortable titles rather than overflowing: the header asks for its
// title and arrow, and lays them out in what it gets.
func TestTableSortHeaderShortens(t *testing.T) {
	test.NewTempApp(t)
	h := newTableSortHeader("Namespace", -1, nil)
	full := h.MinSize()
	plain := newTableSortHeader("Namespace", 0, nil).MinSize()
	if full.Width <= plain.Width {
		t.Errorf("the sorted header asks %v, the plain one %v: no room for the arrow", full, plain)
	}
	h.Resize(fyne.NewSize(full.Width/2, full.Height))
	r := test.WidgetRenderer(h).(*tableSortHeaderRenderer)
	if r.text.Text == "Namespace" || r.arrow.img.Position().X+r.arrow.img.Size().Width > full.Width/2+1 {
		t.Errorf("narrow: text %q, arrow at %v", r.text.Text, r.arrow.img.Position())
	}
}

// renderNodePodsSorted opens the busy node's drawer, its Pods with CPU and Memory and sorted by
// Memory, descending, and scrolls to the table.
func renderNodePodsSorted(t *testing.T, mode ThemeMode, name string) {
	f, _ := nodePodsSortFake(t, map[string]engine.SortSpec{"Pods": {Column: model.ColumnMemory, Desc: true}})
	registerProbes(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer", func() bool {
		return len(sh.kinds) > 0 && sh.drawer.last != nil && len(findAll[*tipIcon](sh.drawer.details)) > 0 && sh.diag.total > 0
	})
	settle(w)
	scrollDrawerTo(t, sh, "Pods")
	settle(w)
	savePNG(t, w, name)
}

func TestRenderNodePodsSortedDark(t *testing.T) {
	renderNodePodsSorted(t, ThemeDark, "node_pods_metrics_dark.png")
}
