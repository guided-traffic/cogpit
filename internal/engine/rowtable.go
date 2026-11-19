package engine

import (
	"slices"
	"strconv"

	"github.com/guided-traffic/cogpit/internal/model"
)

// RowTable is a list's rows as agents, the CLI and the window's own reads get them (D-168): the
// columns named once, each row its cells in the columns' order, and the cells every row shares
// once in Same, left out of Columns and Rows. A row written as an object of its own repeats every
// column's title and the object's reference: three to four times the tokens for a model that reads
// the list.
//
// The first columns are the object's: RowNamespace (left out of a list without namespaces),
// RowName and RowLevel, the row's status token. With the list's kind and context they are
// the object's reference (Ref); a list whose rows are of another kind or context than its own has
// RowKind or RowContext too. The other columns are the view's by title, without the ones
// that only repeat the name and the namespace; RowSelected where a row is selected; a column's
// tooltips as "<title> tip" where a row has one.
//
// Decode a table into a fresh value: encoding/json merges into a map that is there, so a value
// used again keeps the cells the earlier result's rows shared.
type RowTable struct {
	// Same holds the cells every row shares, by column; absent for fewer than two rows.
	Same    map[string]string `json:"same,omitempty"`
	Columns []string          `json:"columns"`
	Rows    [][]string        `json:"rows"`
}

// The columns a RowTable adds to the view's.
const (
	RowContext   = "context"
	RowKind      = "kind"
	RowNamespace = "namespace"
	RowName      = "name"
	RowLevel     = "level"
	RowSelected  = "selected"
	// TipSuffix follows a column's title in the column of its tooltips.
	TipSuffix = " tip"
)

// The titles of the view's columns that repeat the reference.
const (
	rowTitleName      = "Name"
	rowTitleNamespace = "Namespace"
)

// TableRow is one row a RowTable is built from: Cells and Tips run parallel to the titles.
type TableRow struct {
	Ref      model.ObjectRef
	Status   model.Status
	Cells    []string
	Tips     []string // "" where the cell has no tooltip; may be shorter than Cells
	Selected bool
}

// NewRowTable builds the table of a list of kind on context: rows under the view's column titles.
func NewRowTable(context, kind string, titles []string, rows []TableRow) RowTable {
	type column struct {
		name string
		cell func(r *TableRow) string
	}
	at := func(s []string, i int) string {
		if i < len(s) {
			return s[i]
		}
		return ""
	}
	some := func(f func(r *TableRow) bool) bool {
		return slices.ContainsFunc(rows, func(r TableRow) bool { return f(&r) })
	}
	var cols []column
	if some(func(r *TableRow) bool { return r.Ref.Context != "" && r.Ref.Context != context }) {
		cols = append(cols, column{RowContext, func(r *TableRow) string { return r.Ref.Context }})
	}
	if some(func(r *TableRow) bool { return r.Ref.Kind != kind }) {
		cols = append(cols, column{RowKind, func(r *TableRow) string { return r.Ref.Kind }})
	}
	if some(func(r *TableRow) bool { return r.Ref.Namespace != "" }) {
		cols = append(cols, column{RowNamespace, func(r *TableRow) string { return r.Ref.Namespace }})
	}
	cols = append(cols,
		column{RowName, func(r *TableRow) string { return r.Ref.Name }},
		column{RowLevel, func(r *TableRow) string { return string(r.Status) }})
	taken := map[string]int{RowContext: 1, RowKind: 1, RowNamespace: 1, RowName: 1, RowLevel: 1, RowSelected: 1}
	unique := func(name string) string { // two columns of one title: the later ones numbered
		taken[name]++
		if n := taken[name]; n > 1 {
			return name + " (" + strconv.Itoa(n) + ")"
		}
		return name
	}
	var tips []column
	for i, title := range titles {
		repeats := func(of func(r *TableRow) string) bool {
			return !some(func(r *TableRow) bool { return at(r.Cells, i) != of(r) })
		}
		if title == rowTitleName && repeats(func(r *TableRow) string { return r.Ref.Name }) ||
			title == rowTitleNamespace && repeats(func(r *TableRow) string { return r.Ref.Namespace }) {
			continue
		}
		name := unique(title)
		cols = append(cols, column{name, func(r *TableRow) string { return at(r.Cells, i) }})
		if some(func(r *TableRow) bool { return at(r.Tips, i) != "" }) {
			tips = append(tips, column{name + TipSuffix, func(r *TableRow) string { return at(r.Tips, i) }})
		}
	}
	if some(func(r *TableRow) bool { return r.Selected }) {
		cols = append(cols, column{RowSelected, func(r *TableRow) string {
			if r.Selected {
				return "true"
			}
			return ""
		}})
	}
	cols = append(cols, tips...)

	t := RowTable{Columns: make([]string, 0, len(cols)), Rows: make([][]string, len(rows))}
	var kept []column
	for _, c := range cols {
		if len(rows) > 1 && c.name != RowName {
			first := c.cell(&rows[0])
			if !some(func(r *TableRow) bool { return c.cell(r) != first }) {
				if t.Same == nil {
					t.Same = map[string]string{}
				}
				t.Same[c.name] = first
				continue
			}
		}
		kept = append(kept, c)
		t.Columns = append(t.Columns, c.name)
	}
	for i := range rows {
		line := make([]string, len(kept))
		for j, c := range kept {
			line[j] = c.cell(&rows[i])
		}
		t.Rows[i] = line
	}
	return t
}

// Cell returns row i's cell of a column: its own, else the one every row shares; "" for a column
// the table does not have or a row it does not have.
func (t RowTable) Cell(i int, column string) string {
	if i < 0 || i >= len(t.Rows) {
		return ""
	}
	if j := slices.Index(t.Columns, column); j >= 0 && j < len(t.Rows[i]) {
		return t.Rows[i][j]
	}
	return t.Same[column]
}

// ColumnOf returns the table's column of a view's column, given by id and title: its title, or, for
// the view's Name and Namespace columns that the table leaves out because they repeat the
// reference, the reference's column.
func (t RowTable) ColumnOf(id, title string) string {
	if _, shared := t.Same[title]; shared || slices.Contains(t.Columns, title) {
		return title
	}
	if id == RowName || id == RowNamespace { // the ids of the view's Name and Namespace columns
		return id
	}
	return title
}

// Record returns row i's cells by column, the shared ones included.
func (t RowTable) Record(i int) map[string]string {
	out := make(map[string]string, len(t.Columns)+len(t.Same))
	if i < 0 || i >= len(t.Rows) {
		return out
	}
	for k, v := range t.Same {
		out[k] = v
	}
	for j, c := range t.Columns {
		if j < len(t.Rows[i]) {
			out[c] = t.Rows[i][j]
		}
	}
	return out
}

// Ref returns the reference of row i's object in a list of kind on context.
func (t RowTable) Ref(i int, context, kind string) model.ObjectRef {
	if c := t.Cell(i, RowContext); c != "" {
		context = c
	}
	if k := t.Cell(i, RowKind); k != "" {
		kind = k
	}
	return model.ObjectRef{Context: context, Kind: kind, Namespace: t.Cell(i, RowNamespace), Name: t.Cell(i, RowName)}
}

// Level returns row i's status token.
func (t RowTable) Level(i int) model.Status { return model.Status(t.Cell(i, RowLevel)) }

// Find returns the index of the row of an object by namespace and name; -1 without one.
func (t RowTable) Find(namespace, name string) int {
	for i := range t.Rows {
		if t.Cell(i, RowName) == name && t.Cell(i, RowNamespace) == namespace {
			return i
		}
	}
	return -1
}
