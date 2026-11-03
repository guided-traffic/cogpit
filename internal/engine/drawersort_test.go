package engine

import (
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

// sortFixture is a node's Pods table of four pods: names, namespaces, ready counts, CPU and Memory
// (one pod without a measurement), statuses.
func sortFixture() *model.DetailTable {
	cols := []model.Column{
		{ID: "name", Title: "Name", Type: model.ColumnText},
		{ID: "signals", Title: "Signals", Type: model.ColumnSignals},
		{ID: "namespace", Title: "Namespace", Type: model.ColumnText},
		{ID: "ready", Title: "Ready", Type: model.ColumnNumber},
		{ID: model.ColumnCPU, Title: "CPU", Type: model.ColumnMetric},
		{ID: model.ColumnMemory, Title: "Memory", Type: model.ColumnMetric},
		{ID: "status", Title: "Status", Type: model.ColumnText},
	}
	pod := func(ns, name string, ready int64, cpu, mem int64, status string, warn bool) []model.Cell {
		cells := []model.Cell{
			{Text: name, Link: &model.ObjectRef{Kind: "pods", Namespace: ns, Name: name}},
			{},
			{Text: ns, Link: &model.ObjectRef{Kind: "namespaces", Name: ns}},
			{Text: "x/2", Num: ready},
			{}, {},
			{Text: status},
		}
		if warn {
			cells[1] = model.Cell{Text: "not ready", Num: 1, Status: model.StatusWarning}
		}
		if cpu >= 0 {
			cells[4] = model.Cell{Text: model.FormatCPU(cpu), Num: cpu}
			cells[5] = model.Cell{Text: model.FormatBytes(mem), Num: mem}
		}
		return cells
	}
	return model.SortableCellTable(cols, [][]model.Cell{
		pod("shop", "web-0", 2, 250, 64<<20, "Running", false),
		pod("argo", "api-0", 1, -1, 0, "Pending", true), // no measurement
		pod("jobs", "Batch-0", 0, 5, 1536<<20, "Completed", false),
		pod("argo", "web-0", 2, 1200, 8<<20, "Running", false),
	})
}

// namesOf returns a table's rows as namespace/name.
func namesOf(t *model.DetailTable) []string {
	out := make([]string, len(t.Cells))
	for i, c := range t.Cells {
		out[i] = c[2].Text + "/" + c[0].Text
	}
	return out
}

// Every column sorts, both ways: text case-insensitively, numbers and metrics by value, a pod
// without a measurement last in both directions, ties by namespace and name. Rows stays row for
// row with Cells, and Sort says the order.
func TestSortDetailTable(t *testing.T) {
	cases := []struct {
		column string
		desc   bool
		want   []string
	}{
		{"name", false, []string{"argo/api-0", "jobs/Batch-0", "argo/web-0", "shop/web-0"}},
		{"name", true, []string{"argo/web-0", "shop/web-0", "jobs/Batch-0", "argo/api-0"}},
		{"namespace", false, []string{"argo/api-0", "argo/web-0", "jobs/Batch-0", "shop/web-0"}},
		{"namespace", true, []string{"shop/web-0", "jobs/Batch-0", "argo/api-0", "argo/web-0"}},
		{model.ColumnCPU, false, []string{"jobs/Batch-0", "shop/web-0", "argo/web-0", "argo/api-0"}},
		{model.ColumnCPU, true, []string{"argo/web-0", "shop/web-0", "jobs/Batch-0", "argo/api-0"}},
		{model.ColumnMemory, false, []string{"argo/web-0", "shop/web-0", "jobs/Batch-0", "argo/api-0"}},
		{model.ColumnMemory, true, []string{"jobs/Batch-0", "shop/web-0", "argo/web-0", "argo/api-0"}},
		{"status", false, []string{"jobs/Batch-0", "argo/api-0", "argo/web-0", "shop/web-0"}},
		{"status", true, []string{"argo/web-0", "shop/web-0", "argo/api-0", "jobs/Batch-0"}},
		{"ready", false, []string{"jobs/Batch-0", "argo/api-0", "argo/web-0", "shop/web-0"}},
		{"signals", true, []string{"argo/api-0", "argo/web-0", "jobs/Batch-0", "shop/web-0"}},
	}
	for _, c := range cases {
		in := sortFixture()
		before := slices.Clone(in.Cells)
		got := SortDetailTable(in, SortSpec{Column: c.column, Desc: c.desc})
		if names := namesOf(got); !slices.Equal(names, c.want) {
			t.Errorf("%s desc=%v: %q, want %q", c.column, c.desc, names, c.want)
		}
		if got.Sort == nil || *got.Sort != (model.TableSort{Column: c.column, Desc: c.desc}) {
			t.Errorf("%s desc=%v: sort %+v", c.column, c.desc, got.Sort)
		}
		for r := range got.Cells {
			if got.Rows[r][0] != got.Cells[r][0].Text || got.Rows[r][4] != got.Cells[r][4].Text {
				t.Errorf("%s: row %d's texts %q do not follow its cells", c.column, r, got.Rows[r])
			}
		}
		if !slices.EqualFunc(in.Cells, before, func(a, b []model.Cell) bool { return &a[0] == &b[0] }) || in.Sort != nil {
			t.Errorf("%s: the table given was modified", c.column)
		}
	}
	if in := sortFixture(); SortDetailTable(in, SortSpec{Column: "nope"}) != in {
		t.Error("an unknown column must leave the table as it is")
	}
}

// The drawer's order is UI state: a table nobody sorted comes by name with Sort saying so; an order
// set for its title is applied to every model of that title (another node's too), and an order
// naming a column the table lacks falls back to the name.
func TestSortDrawerTables(t *testing.T) {
	m := &DetailModel{Sections: []model.DetailSection{
		{Title: "Properties", Items: []model.DetailItem{{Name: "Name", Value: "n"}}},
		{Title: "Pods", Table: sortFixture()},
		{Title: "Plain", Table: model.CellTable([]model.Column{{ID: "name", Title: "Name"}}, [][]model.Cell{{{Text: "b"}}, {{Text: "a"}}})},
	}}
	SortDrawerTables(m, nil)
	if got := m.Sections[1].Table; got.Sort == nil || *got.Sort != (model.TableSort{Column: "name"}) || namesOf(got)[0] != "argo/api-0" {
		t.Errorf("unsorted: sort %+v, rows %q", got.Sort, namesOf(got))
	}
	if plain := m.Sections[2].Table; plain.Sort != nil || plain.Rows[0][0] != "b" {
		t.Errorf("a table that is not sortable was sorted: %+v", plain)
	}

	m.Sections[1].Table = sortFixture()
	SortDrawerTables(m, map[string]SortSpec{"Pods": {Column: model.ColumnMemory, Desc: true}})
	if got := m.Sections[1].Table; namesOf(got)[0] != "jobs/Batch-0" || !got.Sort.Desc || got.Sort.Column != model.ColumnMemory {
		t.Errorf("by memory: %q, %+v", namesOf(got), got.Sort)
	}

	m.Sections[1].Table = sortFixture()
	SortDrawerTables(m, map[string]SortSpec{"Pods": {Column: "nope", Desc: true}})
	if got := m.Sections[1].Table; got.Sort.Column != "name" || got.Sort.Desc {
		t.Errorf("an unknown column: %+v", got.Sort)
	}
}

// A sort set in the UI state reaches the open drawer's model and announces it (EventDetail); it
// stays when the drawer shows another node, and the hidden CPU column's order falls back to the
// name.
func TestDrawerSortIsEngineState(t *testing.T) {
	s, _, ref := drawerPodsSession(t)
	rec := record(s)
	waitPodsUsage(t, s, ref)
	if tbl := podsTable(t, s, ref); tbl.Sort == nil || tbl.Sort.Column != "name" {
		t.Fatalf("default order %+v", tbl.Sort)
	}

	before := rec.count(EventDetail, "nodes")
	s.UpdateUI(func(u *UIState) { u.DrawerSort = map[string]SortSpec{"Pods": {Column: model.ColumnMemory, Desc: true}} })
	waitFor(t, "a detail event for the new order", func() bool { return rec.count(EventDetail, "nodes") > before })
	tbl := podsTable(t, s, ref)
	var names []string
	for _, r := range tbl.Rows {
		names = append(names, r[0])
	}
	if !slices.Equal(names, []string{"batch-0", "web-0", "api-0"}) { // 1.5GiB, 64MiB, none
		t.Errorf("by memory, descending: %q", names)
	}
	if got := s.UIState().DrawerSort["Pods"]; got != (SortSpec{Column: model.ColumnMemory, Desc: true}) {
		t.Errorf("ui state %+v", got)
	}

	other := model.ObjectRef{Context: "test", Kind: "nodes", Name: "node-2"}
	s.UpdateUI(func(u *UIState) { u.Drawer = &other })
	waitFor(t, "node-2's Pods", func() bool { tbl := podsTable(t, s, other); return tbl != nil && len(tbl.Rows) == 1 })
	if tbl := podsTable(t, s, other); tbl.Sort == nil || tbl.Sort.Column != model.ColumnMemory || !tbl.Sort.Desc {
		t.Errorf("another node's Pods: %+v", tbl.Sort)
	}

	if _, err := s.ChangeClusterSettings("test", func(cs *ClusterSettings) error { cs.HiddenMetrics = HideMemory; return nil }); err != nil {
		t.Fatal(err)
	}
	if tbl := podsTable(t, s, other); tbl.Sort == nil || tbl.Sort.Column != "name" {
		t.Errorf("sorted by a hidden column: %+v", tbl.Sort)
	}
}
