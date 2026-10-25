package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// Keyboard navigation in a table (ui-parity › Tables, Lens's shortcuts page): ↑↓ move between rows,
// Tab and ⇧Tab between columns, ↵ or Space opens the details of the row, through ui.select as a
// click does. Fyne's table moves its highlight on the arrows and selects on Space; the resource
// table adds Tab, ⇧Tab and ↵, and draws the highlighted cell itself, because Cogpit hides Fyne's
// cell markers (HideSeparators) and draws whole-row bands instead.

var (
	_ fyne.Tabbable   = (*resourceTable)(nil)
	_ desktop.Keyable = (*resourceTable)(nil)
)

// AcceptsTab implements fyne.Tabbable: Tab moves between the table's columns instead of to the next
// widget. The search field is ⌘F away; a click leaves the table too.
func (t *resourceTable) AcceptsTab() bool { return true }

// KeyDown implements desktop.Keyable: it tracks Shift for ⇧Tab, which reaches TypedKey without its
// modifier.
func (t *resourceTable) KeyDown(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		t.shift = true
	}
}

// KeyUp implements desktop.Keyable.
func (t *resourceTable) KeyUp(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		t.shift = false
	}
}

// TypedKey adds Escape (close the drawer), Tab, ⇧Tab and ↵ to Fyne's table keys (arrows, Space).
func (t *resourceTable) TypedKey(ev *fyne.KeyEvent) {
	if t.tv.sh.hot.key(ev.Name, t.shift) { // F1–F12 belong to the hotbar
		return
	}
	switch ev.Name {
	case fyne.KeyEscape:
		t.tv.sh.closeDrawer()
		return
	case fyne.KeyTab:
		if t.shift {
			ev = &fyne.KeyEvent{Name: fyne.KeyLeft}
		} else {
			ev = &fyne.KeyEvent{Name: fyne.KeyRight}
		}
	case fyne.KeyReturn, fyne.KeyEnter:
		ev = &fyne.KeyEvent{Name: fyne.KeySpace} // Space selects the highlighted row
	}
	t.keying = true
	defer func() { t.keying = false }()
	t.Table.TypedKey(ev)
}

// FocusGained shows the keyboard's cell; Fyne's table announces it through OnHighlighted.
func (t *resourceTable) FocusGained() {
	t.tv.kbOn = true
	t.keying = true
	defer func() { t.keying = false }()
	t.Table.FocusGained()
	t.tv.updateKeyboardCell()
}

// FocusLost hides the keyboard's cell.
func (t *resourceTable) FocusLost() {
	t.Table.FocusLost()
	t.tv.kbOn = false
	t.shift = false
	t.tv.updateKeyboardCell()
}

// highlighted follows Fyne's highlight: the pointer moving over a cell, or the keyboard. Both move
// the row band; only the keyboard moves the focused cell.
func (tv *tableView) highlighted(id widget.TableCellID) {
	if tv.table.keying {
		tv.kbRow, tv.kbCol = id.Row, id.Col
		tv.updateKeyboardCell()
	}
	tv.setHoverCell(id.Row, id.Col)
}

// updateKeyboardCell rings the cell the keyboard is on while the table has the focus. It visits the
// cells the table has made, never the rows.
func (tv *tableView) updateKeyboardCell() {
	for _, c := range tv.cells {
		c.setFocused(tv.keyboardOn(c.row, c.col))
	}
}

// keyboardOn reports whether the keyboard is on a cell.
func (tv *tableView) keyboardOn(row, col int) bool {
	return tv.kbOn && row == tv.kbRow && col == tv.kbCol && row >= 0
}

// resetKeyboard moves the keyboard to the first row's first column of the view (right of its
// checkbox), as for a view just opened. Fyne keeps its highlight private, and Select is the only way
// to move it; the table's OnSelected (which would open the drawer) is off meanwhile, and the
// selection is dropped again.
func (t *resourceTable) resetKeyboard() {
	on := t.OnSelected
	t.OnSelected = nil
	t.Select(widget.TableCellID{Col: firstDataColumn})
	t.UnselectAll()
	t.OnSelected = on
	t.tv.kbRow, t.tv.kbCol = 0, firstDataColumn
	t.tv.updateKeyboardCell()
}

// ringInset is how far the keyboard's ring lies inside its cell, for a ring of stroke width: inside
// the drawer's outline, which lies on the bounds of the row's cells (VW16, rowoutline.go), with a gap
// between, so that the ring marks a cell and the outline a row, and the two read apart where they
// meet. Inside the cell, the ring is not cut off either where the table's scroller ends left of the
// first column and above the first row.
func ringInset(stroke float32) float32 { return outlineWidth + markGap + stroke/2 }
