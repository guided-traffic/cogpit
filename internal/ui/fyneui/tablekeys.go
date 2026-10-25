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
//
// The ring shows where the keyboard is only while the keyboard is used, as the web's :focus-visible
// and the navigator's (A53) do (the owner's VW17 of 2026-10-25: "dass immer der erste Eintrag einer
// Tabelle einen Rahmen hat, wenn irgendein Element in der Tabelle selektiert ist"; the lead's
// detail, D-74): a click on a cell puts the keyboard there without the ring, so that ↑↓ go on from
// the row clicked; a key the table takes shows the ring, and so does the focus that comes by the
// keyboard (Tab from another widget), not the one a click gives. On the drawer's row the bar marks
// the row already and the ring stays off (the owner, 2026-10-25: "Der Balken auf der linken Seite
// über die ganze Zeilenhöhe würde reichen"); ↑↓ to another row show it there.

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
// Every key the table takes shows the ring (VW17).
func (t *resourceTable) TypedKey(ev *fyne.KeyEvent) {
	if t.tv.sh.hot.key(ev.Name, t.shift) { // F1–F12 belong to the hotbar
		return
	}
	t.keyed()
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

// FocusGained shows the keyboard's cell, unless a click gave the focus (VW17); Fyne's table
// announces the cell through OnHighlighted.
func (t *resourceTable) FocusGained() {
	t.tv.kbOn, t.tv.kbKeys = true, !t.pointer
	t.syncHighlight()
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

// keyboardOn reports whether the ring is on a cell: the keyboard is there, the table has the focus,
// the keyboard was used last, not the pointer (VW17), and the row is not the drawer's, which its bar
// marks.
func (tv *tableView) keyboardOn(row, col int) bool {
	return tv.kbOn && tv.kbKeys && row == tv.kbRow && col == tv.kbCol && row >= 0 && !tv.rowDrawer(row)
}

// keyed marks the keyboard as used: the ring shows on its cell, which a click may have moved since
// Fyne's highlight last followed (VW17).
func (t *resourceTable) keyed() {
	t.syncHighlight()
	if !t.tv.kbKeys {
		t.tv.kbKeys = true
		t.tv.updateKeyboardCell()
	}
}

// pointed puts the keyboard on a cell the user clicked and hides the ring (VW17): ↑↓ go on from
// that row, and the next key shows the ring there. Fyne's highlight follows at the next key or
// focus (syncHighlight), so that pointed itself scrolls nothing (a checkbox's click).
func (tv *tableView) pointed(row, col int) {
	tv.kbRow, tv.kbCol, tv.kbSync, tv.kbKeys = row, col, true, false
	tv.updateKeyboardCell()
}

// focusByPointer gives the table the focus for a click: without the ring (VW17).
func (t *resourceTable) focusByPointer() {
	cnv := fyne.CurrentApp().Driver().CanvasForObject(t)
	if cnv == nil {
		return
	}
	t.pointer = true
	defer func() { t.pointer = false }()
	cnv.Focus(t)
}

// syncHighlight moves Fyne's highlight, which its arrows move from, to the keyboard's cell after a
// click moved that (pointed).
func (t *resourceTable) syncHighlight() {
	if t.tv.kbSync {
		t.tv.kbSync = false
		t.moveHighlight(t.tv.kbRow, t.tv.kbCol)
	}
}

// moveHighlight puts Fyne's highlight on a cell. Fyne keeps its highlight private, and Select is the
// only way to move it; the table's OnSelected (which would open the drawer) is off meanwhile, and
// the selection is dropped again.
func (t *resourceTable) moveHighlight(row, col int) {
	on := t.OnSelected
	t.OnSelected = nil
	t.Select(widget.TableCellID{Row: row, Col: col})
	t.UnselectAll()
	t.OnSelected = on
}

// resetKeyboard moves the keyboard to the first row's first column of the view (right of its
// checkbox), as for a view just opened.
func (t *resourceTable) resetKeyboard() {
	t.moveHighlight(0, firstDataColumn)
	t.tv.kbRow, t.tv.kbCol, t.tv.kbSync = 0, firstDataColumn, false
	t.tv.updateKeyboardCell()
}

// ringInset is how far the keyboard's ring lies inside its cell, for a ring of stroke width: a point
// inside the cell's bounds, so that the table's scroller does not cut it off where it ends left of
// the first column and above the first row.
func ringInset(stroke float32) float32 { return 1 + stroke/2 }
