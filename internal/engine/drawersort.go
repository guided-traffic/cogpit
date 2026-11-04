package engine

import (
	"maps"
	"slices"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Sorting the drawer's tables (owner, 2026-10-13: a node's Pods sort by Name, Namespace, CPU,
// Memory and Status). A sortable table (model.SortableCellTable) is sorted by the engine, as the
// lists are: the order is UI state (rule 2), UIState.DrawerSort, set with ui.sortDrawerTable by a
// click on a column's title or by an agent, and every model of the drawer comes sorted, with the
// order in DetailTable.Sort for the header's arrow. The rules are the lists' (compareRows): text
// case-insensitively, number, age, warning and metric columns by Cell.Num, empty cells (a pod
// without a measurement) last in both directions, ties by namespace and name.
//
// Cogpit's defaults (Lens's drawer sorting is not captured): every column sorts, not only the five
// the owner named; the order is kept by the table's section title ("Pods") for the session, so the
// next node's Pods come in the same order, and is the same in every tab; a table nobody sorted is
// sorted by name, ascending, as a list is, unless the profile gave it an order of its own
// (DetailTable.Sort: Referenced By by Kind).

// drawerSorts returns the drawer tables' sort orders from the UI state.
func (s *Session) drawerSorts() map[string]SortSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.ui.DrawerSort)
}

// SortDrawerTables sorts the model's sortable tables by sorts, by section title; a table without an
// order there (or with one naming a column it does not have) is sorted in the order its profile
// gave it (DetailTable.Sort), else by name. Every drawer model of the session goes through it; the
// window's tests use it for their own models.
func SortDrawerTables(m *DetailModel, sorts map[string]SortSpec) {
	for i := range m.Sections {
		t := m.Sections[i].Table
		if t == nil || !t.Sortable {
			continue
		}
		spec, ok := sorts[m.Sections[i].Title]
		switch {
		case ok && t.ColumnIndex(spec.Column) >= 0:
		case t.Sort != nil && t.ColumnIndex(t.Sort.Column) >= 0:
			spec = SortSpec{Column: t.Sort.Column, Desc: t.Sort.Desc}
		default:
			spec = DefaultSort
		}
		m.Sections[i].Table = SortDetailTable(t, spec)
	}
}

// SortDetailTable returns a sortable table with its rows in spec's order and Sort set, the way the
// drawer's model has them; a table without the column comes back as it is. The table is not
// modified.
func SortDetailTable(t *model.DetailTable, spec SortSpec) *model.DetailTable {
	ci := t.ColumnIndex(spec.Column)
	if ci < 0 {
		return t
	}
	typ := model.ColumnText
	if ci < len(t.Types) {
		typ = t.Types[ci]
	}
	rows := make([]model.Row, len(t.Cells))
	for r, cells := range t.Cells {
		rows[r] = model.Row{Ref: tableRowRef(cells), Cells: cells}
	}
	order := make([]int, len(t.Cells))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return compareRows(&rows[a], &rows[b], ci, typ, spec.Desc) })

	nt := *t
	nt.Cells = make([][]model.Cell, len(order))
	nt.Rows = make([][]string, 0, len(t.Rows))
	for i, r := range order {
		nt.Cells[i] = t.Cells[r]
		if r < len(t.Rows) {
			nt.Rows = append(nt.Rows, t.Rows[r])
		}
	}
	nt.Sort = &model.TableSort{Column: spec.Column, Desc: spec.Desc}
	return &nt
}

// tableRowRef is what ties of a table's rows fall back to: the object its first link names (a pod:
// its namespace and name), else its first cell's text as a name.
func tableRowRef(cells []model.Cell) model.ObjectRef {
	for _, c := range cells {
		if c.Link != nil && c.Link.Name != "" {
			return *c.Link
		}
	}
	if len(cells) > 0 {
		return model.ObjectRef{Name: cells[0].Text}
	}
	return model.ObjectRef{}
}
