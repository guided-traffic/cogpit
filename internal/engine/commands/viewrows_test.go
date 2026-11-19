package commands_test

import (
	"encoding/csv"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// view.rows returns what the open view shows: over a view filtered by namespace and by a regular
// expression, sorted descending, its columns reordered and one hidden, the rows come in the table's
// order with their cells in the shown columns' order, the selection marked, exactly as the table's
// CSV export (which writes the table as shown) has them; agents read it; a page is a slice of it.
func TestViewRowsMatchTheTable(t *testing.T) {
	src := seed()
	api := testkit.Pod("shop", "api-0")
	if err := unstructured.SetNestedField(api.Object, "node-2", "spec", "nodeName"); err != nil {
		t.Fatal(err)
	}
	src.Set(testkit.PodsGVR, api)
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-2"))
	h := newHarness(t, src, engine.Options{})
	gui, agent := engine.OriginGUI, engine.OriginExternalAgent

	h.must(t, gui, "ui.openView", map[string]any{"view": "pods"}, nil)
	h.must(t, gui, "ui.setNamespace", map[string]any{"namespaces": []string{"shop"}}, nil)
	h.must(t, gui, "ui.setFilter", map[string]any{"filter": "^web-[01]$", "regex": true}, nil)
	h.must(t, gui, "ui.sort", map[string]any{"column": "name", "desc": true}, nil)
	h.must(t, gui, "ui.setColumnOrder", map[string]any{"order": []string{"node", "name"}}, nil)
	h.must(t, gui, "ui.setColumns", map[string]any{"hidden": []string{"namespace"}}, nil)
	web1 := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "shop", Name: "web-1"}
	h.must(t, gui, "ui.select", map[string]any{"refs": []model.ObjectRef{web1}, "noDrawer": true}, nil)

	var got engine.ViewRows
	h.must(t, agent, "view.rows", struct{}{}, &got) // waits for the snapshot to follow the filters
	cols := make([]string, len(got.ViewColumns))
	for i, c := range got.ViewColumns {
		cols[i] = c.ID
	}
	if !slices.Equal(cols, []string{"node", "name"}) {
		t.Fatalf("columns %v, want node, name (namespace hidden, the order arranged)", cols)
	}
	if got.View != "pods" || got.Shown != 2 || got.Total != 6 || got.Returned != 2 || got.Offset != 0 || !got.Synced {
		t.Errorf("view %s shown %d total %d returned %d offset %d synced %v", got.View, got.Shown, got.Total, got.Returned, got.Offset, got.Synced)
	}
	if got.Filter != "^web-[01]$" || !got.FilterOptions.Regex || !slices.Equal(got.Namespaces, []string{"shop"}) || got.Sort != (engine.SortSpec{Column: "name", Desc: true}) {
		t.Errorf("filtered with %q %+v in %v sorted %+v", got.Filter, got.FilterOptions, got.Namespaces, got.Sort)
	}
	want := [][]string{{"node-1", "web-1"}, {"node-1", "web-0"}}
	// The table as it goes out (D-168): what both rows share once, the rest in the rows.
	if !slices.Equal(got.Columns, []string{engine.RowName, engine.RowSelected}) || got.Same["Node"] != "node-1" ||
		got.Same[engine.RowNamespace] != "shop" || !slices.Equal(got.Rows[0], []string{"web-1", "true"}) {
		t.Errorf("the table: same %v, columns %v, rows %v", got.Same, got.Columns, got.Rows)
	}
	if len(got.Rows) != len(want) {
		t.Fatalf("%d rows, want %d: %+v", len(got.Rows), len(want), got.RowTable)
	}
	for i := range got.Rows {
		if cells := got.ViewCells(i); !slices.Equal(cells, want[i]) {
			t.Errorf("row %d: %v, want %v", i, cells, want[i])
		}
		ref := got.RefAt(i)
		if ref.Namespace != "shop" || ref.Name != want[i][1] || ref.Kind != "pods" {
			t.Errorf("row %d: ref %+v", i, ref)
		}
		wantSelected := ""
		if ref.Name == "web-1" {
			wantSelected = "true"
		}
		if selected := got.Cell(i, engine.RowSelected); selected != wantSelected {
			t.Errorf("row %d (%s): selected %q, want %q", i, ref.Name, selected, wantSelected)
		}
	}

	// The table as the window shows it, written by the CSV export: the same header and cells.
	var exp struct {
		CSV string `json:"csv"`
	}
	h.must(t, agent, "table.exportCsv", struct{}{}, &exp)
	records, err := csv.NewReader(strings.NewReader(exp.CSV)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	titles := make([]string, len(got.ViewColumns))
	for i, c := range got.ViewColumns {
		titles[i] = c.Title
	}
	if !slices.Equal(records[0], titles) || len(records) != len(got.Rows)+1 {
		t.Fatalf("the CSV has %v and %d rows, view.rows %v and %d", records[0], len(records)-1, titles, len(got.Rows))
	}
	for i := range got.Rows {
		if cells := got.ViewCells(i); !slices.Equal(records[i+1], cells) {
			t.Errorf("row %d: the CSV has %v, view.rows %v", i, records[i+1], cells)
		}
	}

	// A page.
	var page engine.ViewRows
	h.must(t, agent, "view.rows", map[string]any{"offset": 1, "limit": 1}, &page)
	if page.Returned != 1 || page.RefAt(0).Name != "web-0" || page.Shown != 2 || page.Offset != 1 {
		t.Errorf("page offset 1 limit 1: %+v", page)
	}
	h.must(t, agent, "view.rows", map[string]any{"offset": 5}, &page)
	if page.Returned != 0 || len(page.Rows) != 0 || page.Shown != 2 || len(page.ViewColumns) != 2 {
		t.Errorf("a page past the end: %+v", page)
	}
	if err := h.run(t, agent, "view.rows", map[string]any{"offset": -1}, nil); err == nil {
		t.Error("a negative offset was accepted")
	}

	// A filter set just before is applied: view.rows waits for the snapshot.
	h.must(t, gui, "ui.setFilter", map[string]any{"filter": "api", "regex": false}, nil)
	got = engine.ViewRows{} // a fresh value: a decode keeps the cells an earlier result's rows shared
	h.must(t, agent, "view.rows", struct{}{}, &got)
	if got.Shown != 1 || got.RefAt(0).Name != "api-0" || !slices.Equal(got.ViewCells(0), []string{"node-2", "api-0"}) {
		t.Errorf("after the search api: %+v", got)
	}
}

// The page's bounds: 100 rows by default, at most 500.
func TestViewRowsPageBounds(t *testing.T) {
	for _, c := range []struct{ offset, limit, want int }{{0, 0, 100}, {0, 1, 1}, {3, 500, 500}, {0, 501, 500}, {0, 100000, 500}} {
		got, err := engine.CheckViewPage(c.offset, c.limit)
		if err != nil || got != c.want {
			t.Errorf("CheckViewPage(%d, %d) = %d, %v; want %d", c.offset, c.limit, got, err, c.want)
		}
	}
	for _, c := range [][2]int{{-1, 0}, {0, -1}} {
		if _, err := engine.CheckViewPage(c[0], c[1]); err == nil {
			t.Errorf("CheckViewPage(%d, %d) accepted", c[0], c[1])
		}
	}
}

// Without an open view there are no rows to read: the command is disabled and says why.
func TestViewRowsNeedsAView(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	h.must(t, engine.OriginGUI, "cluster.disconnect", struct{}{}, nil)
	err := h.run(t, engine.OriginExternalAgent, "view.rows", struct{}{}, nil)
	if err == nil || !strings.Contains(err.Error(), "no cluster connected") {
		t.Errorf("view.rows without a cluster: %v", err)
	}
}
