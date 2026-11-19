package engine

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

func podRow(ns, name string, status model.Status, cells ...string) TableRow {
	return TableRow{Ref: model.ObjectRef{Context: "prod", Kind: "pods", Namespace: ns, Name: name}, Status: status,
		Cells: append([]string{name, ns}, cells...)}
}

var podTitles = []string{"Name", "Namespace", "Status", "Age", "Node"}

// TestRowTableSharesWhatEveryRowHas (D-168): the columns are named once and each row is its cells
// in their order; a cell every row has is in Same and in no row; the view's Name and Namespace
// columns, which repeat the reference, are left out; the name is never shared. Cell, Record, Ref,
// Level and Find read a shared cell as a row's own.
func TestRowTableSharesWhatEveryRowHas(t *testing.T) {
	tb := NewRowTable("prod", "pods", podTitles, []TableRow{
		podRow("shop", "web-0", model.StatusOK, "Running", "3d", "node-a"),
		podRow("shop", "web-1", model.StatusOK, "Running", "3d", "node-b"),
		podRow("shop", "web-2", model.StatusOK, "Running", "5h", "node-a"),
	})
	if want := map[string]string{RowNamespace: "shop", RowLevel: "ok", "Status": "Running"}; !reflect.DeepEqual(tb.Same, want) {
		t.Errorf("same %v, want %v", tb.Same, want)
	}
	if want := []string{RowName, "Age", "Node"}; !slices.Equal(tb.Columns, want) {
		t.Errorf("columns %v, want %v", tb.Columns, want)
	}
	if want := [][]string{{"web-0", "3d", "node-a"}, {"web-1", "3d", "node-b"}, {"web-2", "5h", "node-a"}}; !reflect.DeepEqual(tb.Rows, want) {
		t.Errorf("rows %v, want %v", tb.Rows, want)
	}
	if tb.Cell(1, "Node") != "node-b" || tb.Cell(1, "Status") != "Running" || tb.Cell(1, RowNamespace) != "shop" ||
		tb.Cell(1, "No such column") != "" || tb.Cell(3, "Node") != "" || tb.Cell(-1, "Status") != "" {
		t.Error("Cell does not read a row's own and its shared cells")
	}
	want := map[string]string{RowNamespace: "shop", RowName: "web-2", RowLevel: "ok", "Status": "Running", "Age": "5h", "Node": "node-a"}
	if got := tb.Record(2); !reflect.DeepEqual(got, want) {
		t.Errorf("record %v, want %v", got, want)
	}
	if len(tb.Record(9)) != 0 {
		t.Error("a record of a row the table does not have")
	}
	if got, want := tb.Ref(2, "prod", "pods"), (model.ObjectRef{Context: "prod", Kind: "pods", Namespace: "shop", Name: "web-2"}); got != want {
		t.Errorf("ref %+v, want %+v", got, want)
	}
	if tb.Level(0) != model.StatusOK || tb.Find("shop", "web-1") != 1 || tb.Find("other", "web-1") != -1 || tb.Find("shop", "web-9") != -1 {
		t.Error("Level or Find")
	}

	// The same name in two namespaces: the name stays a column, the namespace too.
	two := NewRowTable("prod", "pods", podTitles, []TableRow{
		podRow("a", "web", model.StatusOK, "Running", "1d", "n"), podRow("b", "web", model.StatusError, "Failed", "1d", "n")})
	if want := []string{RowNamespace, RowName, RowLevel, "Status"}; !slices.Equal(two.Columns, want) {
		t.Errorf("columns %v, want %v", two.Columns, want)
	}
	if two.Find("b", "web") != 1 || two.Level(1) != model.StatusError || two.Same["Age"] != "1d" {
		t.Errorf("two namespaces: %+v", two)
	}
}

// TestRowTableOfOneRowAndNone: one row shares nothing with anyone, so it has every column and no
// Same; no row at all still names the columns, and rows is an empty list, never null.
func TestRowTableOfOneRowAndNone(t *testing.T) {
	one := NewRowTable("prod", "pods", podTitles, []TableRow{podRow("shop", "web-0", model.StatusOK, "Running", "3d", "node-a")})
	if one.Same != nil || !slices.Equal(one.Columns, []string{RowNamespace, RowName, RowLevel, "Status", "Age", "Node"}) ||
		!reflect.DeepEqual(one.Rows, [][]string{{"shop", "web-0", "ok", "Running", "3d", "node-a"}}) {
		t.Errorf("one row: %+v", one)
	}
	none := NewRowTable("prod", "pods", podTitles, nil)
	if none.Same != nil || !slices.Equal(none.Columns, []string{RowName, RowLevel, "Status", "Age", "Node"}) {
		t.Errorf("no row: %+v", none)
	}
	b, err := json.Marshal(struct {
		Kind string `json:"kind"`
		RowTable
	}{"pods", none})
	if err != nil || string(b) != `{"kind":"pods","columns":["name","level","Status","Age","Node"],"rows":[]}` {
		t.Errorf("no row as JSON: %s, %v", b, err)
	}
}

// TestRowTableColumns: a list without namespaces has no namespace column; a column's tooltips are
// a column of their own, only where a row has one; the selection is a column, only where a row is
// selected; a row of another kind or context than the list's brings the kind and context columns,
// which Ref then reads; a Name column that says more than the name stays; two columns of one title
// are told apart.
func TestRowTableColumns(t *testing.T) {
	node := func(name, version, tip string, selected bool) TableRow {
		return TableRow{Ref: model.ObjectRef{Kind: "nodes", Name: name}, Status: model.StatusOK, Cells: []string{name, version},
			Tips: []string{"", tip}, Selected: selected}
	}
	titles := []string{"Name", "Version"}
	plain := NewRowTable("prod", "nodes", titles, []TableRow{node("a", "v1.30", "", false), node("b", "v1.31", "", false)})
	if want := []string{RowName, "Version"}; !slices.Equal(plain.Columns, want) || plain.Same[RowLevel] != "ok" || len(plain.Same) != 1 {
		t.Errorf("nodes without tips or selection: %+v", plain)
	}
	tb := NewRowTable("prod", "nodes", titles, []TableRow{node("a", "v1.30", "end of life", true), node("b", "v1.31", "", false)})
	if want := []string{RowName, "Version", RowSelected, "Version" + TipSuffix}; !slices.Equal(tb.Columns, want) {
		t.Errorf("columns %v, want %v", tb.Columns, want)
	}
	if tb.Cell(0, "Version tip") != "end of life" || tb.Cell(1, "Version tip") != "" || tb.Cell(0, RowSelected) != "true" || tb.Cell(1, RowSelected) != "" {
		t.Errorf("tips and selection: %+v", tb)
	}
	if got := tb.Ref(1, "prod", "nodes"); got != (model.ObjectRef{Context: "prod", Kind: "nodes", Name: "b"}) {
		t.Errorf("a node's ref %+v", got)
	}

	mixed := NewRowTable("prod", "helmreleases", []string{"Name"}, []TableRow{
		{Ref: model.ObjectRef{Context: "prod", Kind: "helmreleases", Namespace: "shop", Name: "web"}, Cells: []string{"web"}},
		{Ref: model.ObjectRef{Context: "stage", Kind: "helmcharts", Namespace: "shop", Name: "web"}, Cells: []string{"web (chart)"}},
	})
	if want := []string{RowContext, RowKind, RowName, "Name"}; !slices.Equal(mixed.Columns, want) {
		t.Errorf("columns %v, want %v", mixed.Columns, want)
	}
	if got := mixed.Ref(1, "prod", "helmreleases"); got != (model.ObjectRef{Context: "stage", Kind: "helmcharts", Namespace: "shop", Name: "web"}) {
		t.Errorf("a row of another kind and context: %+v", got)
	}
	if mixed.Cell(1, "Name") != "web (chart)" || mixed.Cell(0, RowName) != "web" {
		t.Errorf("a Name column that says more than the name: %+v", mixed)
	}

	twice := NewRowTable("", "things", []string{"Size", "Size"}, []TableRow{
		{Ref: model.ObjectRef{Kind: "things", Name: "a"}, Cells: []string{"1", "2"}},
		{Ref: model.ObjectRef{Kind: "things", Name: "b"}, Cells: []string{"3", "4"}},
	})
	if want := []string{RowName, "Size", "Size (2)"}; !slices.Equal(twice.Columns, want) || twice.Cell(1, "Size (2)") != "4" {
		t.Errorf("two columns of one title: %+v", twice)
	}
	// A row with fewer cells than titles: the missing ones are empty, nothing panics.
	short := NewRowTable("", "things", []string{"Size", "Colour"}, []TableRow{{Ref: model.ObjectRef{Kind: "things", Name: "a"}, Cells: []string{"1"}}})
	if short.Cell(0, "Colour") != "" || short.Cell(0, "Size") != "1" {
		t.Errorf("a short row: %+v", short)
	}
}

// TestRowTableColumnOfAViewsColumn: a view's column is in the table under its title, shared or not;
// its Name and Namespace columns, left out where they repeat the reference, are the reference's.
func TestRowTableColumnOfAViewsColumn(t *testing.T) {
	tb := NewRowTable("prod", "pods", podTitles, []TableRow{
		podRow("shop", "web-0", model.StatusOK, "Running", "3d", "node-a"),
		podRow("shop", "web-1", model.StatusOK, "Running", "3d", "node-b"),
	})
	for _, c := range []struct{ id, title, want string }{
		{"name", "Name", RowName}, {"namespace", "Namespace", RowNamespace}, {"status", "Status", "Status"}, {"node", "Node", "Node"},
		{"gone", "Gone", "Gone"},
	} {
		if got := tb.ColumnOf(c.id, c.title); got != c.want {
			t.Errorf("column %s (%s): %q, want %q", c.id, c.title, got, c.want)
		}
	}
	kept := NewRowTable("", "things", []string{"Name"}, []TableRow{{Ref: model.ObjectRef{Kind: "things", Name: "a"}, Cells: []string{"a (default)"}}})
	if got := kept.ColumnOf("name", "Name"); got != "Name" || kept.Cell(0, got) != "a (default)" {
		t.Errorf("a Name column that says more than the name: %q", got)
	}
	v := ViewRows{View: "pods", Context: "prod", ViewColumns: []ViewColumn{{ID: "node", Title: "Node"}, {ID: "name", Title: "Name"}, {ID: "status", Title: "Status"}}, RowTable: tb}
	if got := v.ViewCells(1); !slices.Equal(got, []string{"node-b", "web-1", "Running"}) {
		t.Errorf("a row under the view's columns: %v", got)
	}
}
