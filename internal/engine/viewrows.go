package engine

import (
	"fmt"
	"slices"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The rows the user sees (G17, view.rows): a page of the open view's table as the window draws it,
// read from the view's snapshot, never from the API server. The snapshot is what the table shows:
// the namespace filter, the search with its toggles, the status filter and the sort applied, its columns hidden and
// arranged as the user left them (TableSnapshot.Columns, DisplayOrder), each cell's text as the
// table renders it at now (model.CellText). A kind that holds Secret values shows in its cells what
// its view shows (key names, types and sizes), never a value.

// Bounds of a page of view rows.
const (
	DefaultViewRows = 100
	MaxViewRows     = 500
)

// ViewColumn is one column the table shows, in the order it shows them.
type ViewColumn struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ViewRow is one row as the table shows it: its object, its status and its cells' texts, one per
// column of ViewRows.Columns.
type ViewRow struct {
	Ref    model.ObjectRef `json:"ref"`
	Status model.Status    `json:"status,omitempty"`
	Cells  []string        `json:"cells"`
	// Tips are the tooltips of the cells that have one, by column id: a kubelet's upstream support
	// and version skew in the Nodes list's Version column (D-91).
	Tips map[string]string `json:"tips,omitempty"`
	// Selected marks a row of the view's selection (UIState.Selected).
	Selected bool `json:"selected,omitempty"`
}

// ViewRows is a page of the rows the open view shows.
type ViewRows struct {
	View    string `json:"view"`
	Title   string `json:"title"`
	Context string `json:"context"`
	// Columns are the visible columns in the order the table shows them.
	Columns []ViewColumn `json:"columns"`
	// Shown is the number of rows the view shows (after the namespace filter and the search); Total
	// the number of objects before filtering.
	Shown int `json:"shown"`
	Total int `json:"total"`
	// Offset is the index of the first row returned; Returned the rows in this page.
	Offset   int       `json:"offset"`
	Returned int       `json:"returned"`
	Rows     []ViewRow `json:"rows"`
	// What the rows were filtered and sorted with: the snapshot's, which follows the UI state within
	// a tenth of a second.
	Namespaces    []string      `json:"namespaces,omitempty"`
	Filter        string        `json:"filter,omitempty"`
	FilterOptions FilterOptions `json:"filterOptions,omitzero"`
	FilterError   string        `json:"filterError,omitempty"`
	Sort          SortSpec      `json:"sort"`
	// States is the status filter the rows were filtered with; StateChoices the states the rows the
	// namespace filter and the search keep carry, with their counts (what the window's status filter
	// offers; ui.setStatusFilter takes these names).
	States       []string     `json:"states,omitempty"`
	StateChoices []StateCount `json:"stateChoices,omitempty"`
	// Synced is false while the view's kind is still listing; Error is the informer's last error.
	Synced   bool   `json:"synced"`
	Error    string `json:"error,omitempty"`
	Revision uint64 `json:"revision"`
}

// CheckViewPage checks a page's offset and limit and returns the limit to use: DefaultViewRows for
// 0, at most MaxViewRows.
func CheckViewPage(offset, limit int) (int, error) {
	switch {
	case offset < 0:
		return 0, fmt.Errorf("offset %d is negative", offset)
	case limit < 0:
		return 0, fmt.Errorf("limit %d is negative", limit)
	case limit == 0:
		return DefaultViewRows, nil
	}
	return min(limit, MaxViewRows), nil
}

// RowsOf returns rows offset to offset+limit of a snapshot as the table shows them at now, the rows
// of selected marked, absolute times in ISO 8601 in now's zone. limit must be positive
// (CheckViewPage).
func RowsOf(t *TableSnapshot, selected []model.ObjectRef, offset, limit int, now time.Time) ViewRows {
	return RowsIn(t, selected, offset, limit, now, model.TimeLayout{}.In(now.Location()))
}

// RowsIn is RowsOf with absolute times written in f, as the window writes them (D-31).
func RowsIn(t *TableSnapshot, selected []model.ObjectRef, offset, limit int, now time.Time, f model.TimeFormat) ViewRows {
	out := ViewRows{
		View: t.View, Title: t.Title, Context: t.Context, Shown: len(t.Rows), Total: t.Total, Offset: offset,
		Namespaces: slices.Clone(t.Namespaces), Filter: t.Filter, FilterOptions: t.FilterOptions,
		FilterError: t.FilterError, Sort: t.Sort, Synced: t.Synced, Error: t.Error, Revision: t.Revision,
		States: slices.Clone(t.States), StateChoices: slices.Clone(t.StateChoices),
		Rows: []ViewRow{},
	}
	var visible []int
	for _, i := range t.DisplayOrder() { // the order the user arranged, hidden columns left out (as WriteCSV)
		if c := t.Columns[i]; !c.Hidden {
			visible = append(visible, i)
			out.Columns = append(out.Columns, ViewColumn{ID: c.ID, Title: c.Title})
		}
	}
	if offset >= len(t.Rows) {
		return out
	}
	end := min(len(t.Rows), offset+limit)
	out.Rows = make([]ViewRow, 0, end-offset)
	for ri := offset; ri < end; ri++ {
		row := &t.Rows[ri]
		cells := make([]string, len(visible))
		var tips map[string]string
		for i, ci := range visible {
			c := cellAt(row, ci)
			cells[i] = f.CellText(t.Columns[ci], c, now)
			if c.Tip != "" {
				if tips == nil {
					tips = map[string]string{}
				}
				tips[t.Columns[ci].ID] = c.Tip
			}
		}
		out.Rows = append(out.Rows, ViewRow{Ref: row.Ref, Status: row.Status, Cells: cells, Tips: tips,
			Selected: slices.ContainsFunc(selected, row.Ref.Same)})
	}
	out.Returned = len(out.Rows)
	return out
}

// CaughtUp reports whether a view's snapshot shows what the UI state asks of the open view: its
// namespace filter, search, toggles, status filter and sort, and its columns in the arranged order with the
// hidden ones hidden. Snapshots follow the UI state within a tenth of a second, so a caller that
// just changed one waits for it.
func (t *TableSnapshot) CaughtUp(u UIState) bool {
	return t != nil && t.Filter == u.Filter && t.FilterOptions == u.FilterOptions && statesCaughtUp(t, u) &&
		slices.Equal(t.Namespaces, u.Namespaces) && (u.Sort.Column == "" || t.Sort == u.Sort) &&
		slices.Equal(t.Order, ColumnOrder(t.Columns, u.ColumnOrder[u.View])) && t.hidesAsAsked(u)
}

// hidesAsAsked reports whether the snapshot hides the columns the view's entry in
// UIState.HiddenColumns lists, and those the cluster's settings hide (MetricsHidden), and no
// other; a view without an entry shows its kind's defaults, which the snapshot always has.
func (t *TableSnapshot) hidesAsAsked(u UIState) bool {
	hidden, set := u.HiddenColumns[u.View]
	if !set {
		return true
	}
	for _, c := range t.Columns {
		if c.Hidden != (slices.Contains(hidden, c.ID) || slices.Contains(t.MetricsHidden, c.ID)) {
			return false
		}
	}
	return true
}
