package fyneui

import (
	"fyne.io/fyne/v2"
)

// A click on the empty area below a list's rows (the owner's gate walk, 2026-10-20, GW1: "a click
// below the rows should deselect the current pod, so its drawer closes") closes the drawer through
// ui.closeDrawer, so the row it showed is no longer marked and agents see the drawer closed (UI
// state is engine state). What it leaves, the lead's decision of 2026-10-20, open to revision
// (D-106): the checked rows stay checked, as the click on a row keeps them (A73); they are the
// bulk actions' selection, which the selection bar's Clear and the header's checkbox end. Lens's
// behaviour is not captured.
//
// Below the rows is where Fyne's table, not one of its cells, takes the click: the table's own
// area under its last row, in a list shorter than the view, or under the header of an empty one.
// The gaps between rows and columns stay Fyne's, which selects nothing there.

var _ fyne.Tappable = (*resourceTable)(nil)

// Tapped implements fyne.Tappable: a click below the rows closes the drawer; any other click is the
// table's.
func (t *resourceTable) Tapped(e *fyne.PointEvent) {
	if t.belowRows(e.Position) {
		t.tv.sh.tip.hide()
		t.tv.sh.closeDrawer()
		return
	}
	t.Table.Tapped(e)
}

// belowRows reports whether p, in the table's coordinates, lies below its last row: under the
// header, and further down than the rows reach in the body's scroller.
func (t *resourceTable) belowRows(p fyne.Position) bool {
	b := t.body
	if b == nil || b.Content == nil {
		return false
	}
	top := b.Position().Y
	if p.Y < top || p.Y >= top+b.Size().Height || p.X < 0 || p.X >= t.Size().Width {
		return false
	}
	return p.Y-top+b.Offset.Y >= b.Content.MinSize().Height
}
