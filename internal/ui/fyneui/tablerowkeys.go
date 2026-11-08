package fyneui

import (
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The table's own single keys (D-new-rowkeys, k9s's): moving the keyboard's row, marking rows,
// sorting and the status filter. The object keys (l, s, r, ...) are rowkeys.go's; the window's
// (/ : ? 0) shell.go's windowRune.

var (
	_ fyne.Shortcutable = (*resourceTable)(nil)
)

// tableKeyHints are the table's keys as the key overview lists them.
var tableKeyHints = []keyHint{
	{"↑ ↓  j k", "Previous or next row"},
	{"g G  Home End", "First or last row"},
	{"PageUp PageDown", "A page up or down"},
	{"Space", "Mark the row, or unmark it"},
	{"⇧↑ ⇧↓", "Mark the rows on the way"},
	{"↵", "Details"},
	{"Esc", "Close the details, then the marks"},
	{"⇧N ⇧A ⇧S ⇧P", "Sort: name, age, status, namespace"},
	{"⇧O", "Sort by the keyboard's column"},
	{"Tab ⇧Tab", "Next or previous column"},
	{"⌃Z", "Only rows with problems, or all again"},
	{"⌃\\", "Clear the marks"},
}

// TypedRune implements fyne.Focusable: the table's single keys, then the window's, then the object
// keys on the keyboard's row (with rows marked, on them where the key acts on a selection).
func (t *resourceTable) TypedRune(r rune) {
	if r == ' ' { // Space comes as a key too (TypedKey)
		return
	}
	t.keyed()
	t.tv.tableRune(r)
}

// TypedShortcut implements fyne.Shortcutable: ⌃Z, ⌃\ and ⌘S of the table, then the row keys with
// modifiers (Ctrl+D Delete).
// A shortcut it does not take goes on to the window's own (⌘F the search, ⌘⇧T a terminal), which
// Fyne offers the window only while the focused widget takes no shortcuts.
func (t *resourceTable) TypedShortcut(sc fyne.Shortcut) {
	cs, ok := sc.(*desktop.CustomShortcut)
	if shown, isShown := sc.(*shownShortcut); isShown {
		cs, ok = &shown.CustomShortcut, true
	}
	if ok && t.tv.tableShortcut(cs) {
		t.keyed()
		return
	}
	t.tv.sh.passWindowShortcut(sc)
}

// tableRune runs a single key typed while the table has the keyboard, or handed to it by the
// window while nothing has it. It reports whether a key took it.
func (tv *tableView) tableRune(r rune) bool {
	switch r {
	case 'j':
		tv.moveRing(tv.kbRow + 1)
	case 'k':
		tv.moveRing(tv.kbRow - 1)
	case 'g':
		tv.moveRing(0)
	case 'G':
		tv.moveRing(tv.rowCount() - 1)
	case 'N':
		tv.sortKey("name")
	case 'A':
		tv.sortKey("age")
	case 'S':
		tv.sortKey("status")
	case 'P':
		tv.sortKey("namespace")
	case 'O':
		tv.sortRingColumn()
	default:
		if tv.sh.windowRune(r) {
			return true
		}
		t, ok := tv.ringTarget()
		return ok && tv.sh.rowKey(string(r), t)
	}
	return true
}

// tableShortcut runs a key with modifiers typed while the table has the keyboard.
func (tv *tableView) tableShortcut(sc *desktop.CustomShortcut) bool {
	ctrl := sc.Modifier == fyne.KeyModifierControl
	switch {
	case ctrl && sc.KeyName == fyne.KeyZ:
		tv.toggleFaults()
		return true
	case ctrl && sc.KeyName == fyne.KeyBackslash:
		tv.clearSelection()
		return true
	case sc.Modifier == fyne.KeyModifierShortcutDefault && sc.KeyName == fyne.KeyS:
		if tv.csvButton != nil && tv.csvButton.Visible() {
			tv.exportCSV()
		}
		return true
	}
	t, ok := tv.ringTarget()
	return ok && tv.sh.rowKeyShortcut(sc, t)
}

// tableKey runs the named keys the table adds to Fyne's (TypedKey): Space marks, ⇧↑ and ⇧↓ mark
// on the way, Home, End, PageUp and PageDown move, Delete deletes. It reports whether it took the
// key.
func (tv *tableView) tableKey(name fyne.KeyName, shift bool) bool {
	switch name {
	case fyne.KeySpace:
		if tv.ringValid() {
			tv.checkTapped(tv.kbRow, 0)
		}
	case fyne.KeyUp, fyne.KeyDown:
		if !shift {
			return false
		}
		step := 1
		if name == fyne.KeyUp {
			step = -1
		}
		tv.markOnTheWay(step)
	case fyne.KeyHome:
		tv.moveRing(0)
	case fyne.KeyEnd:
		tv.moveRing(tv.rowCount() - 1)
	case fyne.KeyPageUp:
		tv.moveRing(tv.kbRow - tv.pageRows())
	case fyne.KeyPageDown:
		tv.moveRing(tv.kbRow + tv.pageRows())
	case fyne.KeyDelete:
		if t, ok := tv.ringTarget(); ok {
			tv.sh.rowKey("Delete", t)
		}
	default:
		return false
	}
	return true
}

// rowCount is the number of rows the table shows.
func (tv *tableView) rowCount() int {
	if tv.snap == nil {
		return 0
	}
	return len(tv.snap.Rows)
}

// ringValid reports whether the keyboard's row is a row of the table.
func (tv *tableView) ringValid() bool { return tv.kbRow >= 0 && tv.kbRow < tv.rowCount() }

// ringTarget is the target of a row key typed in the table: the keyboard's row, and the marked rows.
func (tv *tableView) ringTarget() (keyTarget, bool) {
	if !tv.ringValid() {
		return keyTarget{}, false
	}
	return keyTarget{ref: tv.snap.Rows[tv.kbRow].Ref, sel: slices.Clone(tv.selected), row: tv.kbRow}, true
}

// moveRing moves the keyboard to a row (kept within the rows), its column kept, and scrolls it into
// view.
func (tv *tableView) moveRing(row int) {
	n := tv.rowCount()
	if n == 0 {
		return
	}
	row = max(0, min(row, n-1))
	col := max(tv.kbCol, firstDataColumn)
	tv.table.moveHighlight(row, col)
	tv.kbRow, tv.kbCol, tv.kbSync = row, col, false
	tv.updateKeyboardCell()
	tv.table.ScrollTo(widget.TableCellID{Row: row, Col: col})
}

// pageRows is how many rows a page key moves: the rows the table shows at once, at least one.
func (tv *tableView) pageRows() int {
	h := tv.table.Size().Height
	for _, c := range tv.cells {
		if rh := c.Size().Height; rh > 0 {
			return max(1, int(h/rh)-1)
		}
	}
	return 10
}

// markOnTheWay moves the keyboard a row up or down and marks the rows from where marking started
// (the anchor; the row the keyboard left, when none) to it, as ⇧-click marks a range.
func (tv *tableView) markOnTheWay(step int) {
	if !tv.ringValid() {
		return
	}
	if tv.check.anchor == nil || tv.snap.IndexOf(*tv.check.anchor) < 0 {
		ref := tv.snap.Rows[tv.kbRow].Ref
		tv.check.anchor = &ref
	}
	tv.moveRing(tv.kbRow + step)
	tv.selectRange(tv.kbRow)
}

// sortKey sorts the view by a column, as a click on its header does: descending when it is sorted
// by it ascending already. A view without the column says so.
func (tv *tableView) sortKey(id string) {
	if tv.snap == nil {
		return
	}
	if !slices.ContainsFunc(tv.snap.Columns, func(c model.Column) bool { return c.ID == id }) {
		tv.sh.showToast(engine.LevelInfo, tv.sh.kindTitle(tv.snap.View)+" has no "+id+" column", 0)
		return
	}
	cur := tv.sh.ui.Sort
	tv.sh.dispatch("ui.sort", sortParams{Column: id, Desc: cur.Column == id && !cur.Desc}, nil)
}

// sortRingColumn sorts by the column the keyboard is on (k9s's ⇧O), as a click on its header does.
func (tv *tableView) sortRingColumn() {
	vis, ok := tv.dataColumn(tv.kbCol)
	if tv.snap == nil || !ok || tv.snap.Columns[tv.cols[vis]].Type == model.ColumnIcon {
		return
	}
	tv.sortKey(tv.snap.Columns[tv.cols[vis]].ID)
}

// toggleFaults shows only the rows with problems, or all of them again (k9s's ⌃Z): the status
// filter set to the states of the view's first state column whose status is a warning or an error
// (ui.setStatusFilter); a filter set already is cleared.
func (tv *tableView) toggleFaults() {
	if tv.snap == nil {
		return
	}
	if len(tv.sh.ui.States) > 0 {
		tv.sh.dispatch("ui.setStatusFilter", statusFilterParams{States: []string{}}, nil)
		return
	}
	var states []string
	column := ""
	for _, c := range tv.snap.StateChoices {
		if c.Status != model.StatusWarning && c.Status != model.StatusError {
			continue
		}
		if len(states) == 0 {
			column = c.Column
		}
		if c.Column != column {
			continue
		}
		s := c.State
		if c.Column != "" {
			s = c.Column + ": " + c.State
		}
		states = append(states, s)
	}
	if len(states) == 0 {
		tv.sh.showToast(engine.LevelInfo, "No rows with problems", 0)
		return
	}
	tv.sh.dispatch("ui.setStatusFilter", statusFilterParams{States: states}, nil)
}

// ringCell is the cell the keyboard is on, for a menu a key opens there; nil when the table has
// not made it.
func (tv *tableView) ringCell() fyne.CanvasObject {
	for _, c := range tv.cells {
		if c.row == tv.kbRow && c.col == tv.kbCol && c.Visible() {
			return c
		}
	}
	return nil
}
