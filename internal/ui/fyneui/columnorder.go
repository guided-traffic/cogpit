package fyneui

import (
	"image/color"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Reordering columns by dragging a header (ui-parity › Tables, G5). The order is engine state per
// view (UIState.ColumnOrder, set with ui.setColumnOrder), kept for the session like the hidden
// columns, so agents see and set it; the view's snapshot carries it (TableSnapshot.Order) and the
// table shows its columns in that order (tableView.cols), as do the ⋮'s column list and the CSV.
//
// Cogpit's look and rules (inferred, not the owner's decision, 2026-10-13; Lens's is not captured):
// once a header is dragged further than minColumnDrag, a chip with the column's title follows the
// pointer along the header row and an accent line marks the gap it drops into; dropping it there
// sends the view's new order; a drop on its own place, a shorter drag and a drop after the view
// changed do nothing; the checkboxes and the ⋮ stay where they are; Reset Column Order at the end
// of the ⋮'s column list returns the view to its kind's order. A header that is dragged does not
// sort: Fyne taps only what was not dragged.

// minColumnDrag is how far a header must move before it is dragged: less is a click that slipped.
const minColumnDrag = 8

// orderParams are ui.setColumnOrder's parameters (engine/commands/tables.go).
type orderParams struct {
	View  string   `json:"view"`
	Order []string `json:"order,omitempty"`
	Reset bool     `json:"reset,omitempty"`
}

// columnDrag is a header being dragged to another place, and what shows where it would land.
type columnDrag struct {
	tv     *tableView
	active bool
	from   int     // the visible column dragged
	id     string  // its column id
	view   string  // the view it was dragged in
	moved  float32 // how far the pointer moved
	slot   int     // the gap it drops into: before visible column slot, len(cols) after the last; -1 none

	layer     *fyne.Container   // over the table, without a layout: the line and the chip
	mark      *canvas.Rectangle // the accent line in the gap
	chip      *canvas.Rectangle // the chip's background
	chipTitle *canvas.Text
}

func newColumnDrag(tv *tableView) *columnDrag {
	d := &columnDrag{tv: tv, slot: -1}
	d.mark = canvas.NewRectangle(color.Transparent)
	d.chip = canvas.NewRectangle(color.Transparent)
	d.chipTitle = canvas.NewText("", color.Transparent)
	d.chipTitle.TextStyle = fyne.TextStyle{Bold: true}
	d.layer = container.NewWithoutLayout(d.mark, d.chip, d.chipTitle)
	d.hide()
	return d
}

// Dragged implements fyne.Draggable: dragging a column's header moves the column.
func (h *headerCell) Dragged(ev *fyne.DragEvent) {
	if h.onDrag == nil {
		return
	}
	if ev.AbsolutePosition.IsZero() { // Fyne's test driver sends the position only
		e := *ev
		e.AbsolutePosition = fyne.CurrentApp().Driver().AbsolutePositionForObject(h).Add(ev.Position)
		ev = &e
	}
	h.onDrag(h.col, ev)
}

// DragEnd implements fyne.Draggable.
func (h *headerCell) DragEnd() {
	if h.onDragEnd != nil {
		h.onDragEnd(h.col)
	}
}

// dragged follows a header's drag: which column, where it would drop.
func (d *columnDrag) dragged(col int, ev *fyne.DragEvent) {
	tv := d.tv
	if !d.active {
		vis, ok := tv.dataColumn(col)
		if tv.snap == nil || !ok {
			return // the checkboxes and the ⋮ stay where they are
		}
		d.active, d.from, d.id, d.view, d.moved, d.slot = true, vis, tv.snap.Columns[tv.cols[vis]].ID, tv.snap.View, 0, -1
	}
	d.moved += max(ev.Dragged.DX, -ev.Dragged.DX) + max(ev.Dragged.DY, -ev.Dragged.DY)
	if d.moved < minColumnDrag || tv.snap == nil || tv.snap.View != d.view {
		return
	}
	tv.sh.tip.hide()
	x := ev.AbsolutePosition.X - fyne.CurrentApp().Driver().AbsolutePositionForObject(tv.table).X
	d.slot = tv.dropSlot(x)
	d.show(x)
}

// ended drops the dragged column into its gap: the view's new order goes to the engine, and the
// snapshot that follows shows it.
func (d *columnDrag) ended(int) {
	defer d.reset()
	tv := d.tv
	if !d.active || d.moved < minColumnDrag || d.slot < 0 || tv.snap == nil || tv.snap.View != d.view {
		return
	}
	order, ok := tv.movedOrder(d.id, d.slot)
	if !ok {
		return
	}
	tv.sh.dispatch("ui.setColumnOrder", orderParams{View: d.view, Order: order}, nil)
}

func (d *columnDrag) reset() {
	d.active, d.slot, d.moved = false, -1, 0
	d.hide()
}

func (d *columnDrag) hide() {
	d.mark.Hide()
	d.chip.Hide()
	d.chipTitle.Hide()
}

// show draws the line in the drop gap and the chip at the pointer, x from the table's left edge.
func (d *columnDrag) show(x float32) {
	tv := d.tv
	th := tv.table.Theme()
	header := newHeaderCell(nil).MinSize().Height
	line := th.Size(theme.SizeNameInputBorder) * 2
	d.mark.FillColor = themeColor(th, colorAccent)
	d.mark.Move(fyne.NewPos(tv.slotX(d.slot)-line/2, 0))
	d.mark.Resize(fyne.NewSize(line, tv.table.Size().Height))

	d.chipTitle.Text = tv.snap.Columns[tv.cols[min(d.from, len(tv.cols)-1)]].Title
	if d.chipTitle.Text == "" {
		d.chipTitle.Text = "⚠" // the warning column has no title
	}
	d.chipTitle.TextSize = th.Size(theme.SizeNameText)
	d.chipTitle.Color = themeColor(th, colorRowSelectedText)
	ts := d.chipTitle.MinSize()
	pad := th.Size(sizeCellPadding)
	w, h := ts.Width+2*pad, header-2*th.Size(theme.SizeNameInputBorder)
	left := min(max(x-w/2, 0), max(tv.table.Size().Width-w, 0))
	d.chip.FillColor = themeColor(th, colorRowSelected)
	d.chip.StrokeColor = themeColor(th, colorAccent)
	d.chip.StrokeWidth = th.Size(theme.SizeNameInputBorder)
	d.chip.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
	d.chip.Move(fyne.NewPos(left, (header-h)/2))
	d.chip.Resize(fyne.NewSize(w, h))
	d.chipTitle.Move(fyne.NewPos(left+pad, (header-ts.Height)/2))
	d.chipTitle.Resize(ts)
	for _, o := range []fyne.CanvasObject{d.mark, d.chip, d.chipTitle} {
		o.Show()
		o.Refresh()
	}
}

// columnStarts returns where each visible column starts in the table's content, and the gap
// between two columns. setWidths are by table column (the checkboxes first).
func (tv *tableView) columnStarts() (starts []float32, gap float32) {
	gap = tv.table.Theme().Size(theme.SizeNamePadding)
	x := float32(0)
	for col := 0; col < len(tv.setWidths) && col < firstDataColumn+len(tv.cols); col++ {
		if _, ok := tv.dataColumn(col); ok {
			starts = append(starts, x)
		}
		x += tv.setWidths[col] + gap
	}
	return starts, gap
}

// dropSlot returns the gap a column dropped at x (from the table's left edge) goes into: before
// the visible column whose left half x is in, after the last one right of them all.
func (tv *tableView) dropSlot(x float32) int {
	starts, _ := tv.columnStarts()
	cx := x + tv.table.offset.X
	for i, s := range starts {
		if cx < s+tv.setWidths[firstDataColumn+i]/2 {
			return i
		}
	}
	return len(starts)
}

// slotX returns the middle of a drop gap, from the table's left edge.
func (tv *tableView) slotX(slot int) float32 {
	starts, gap := tv.columnStarts()
	if len(starts) == 0 {
		return 0
	}
	var x float32
	if slot < len(starts) {
		x = starts[slot] - gap/2
	} else {
		last := len(starts) - 1
		x = starts[last] + tv.setWidths[firstDataColumn+last] + gap/2
	}
	return x - tv.table.offset.X
}

// movedOrder returns the view's column ids with id moved into the gap before the visible column
// slot (after the last for len(cols)); hidden columns keep their places. ok is false when the
// column would stay where it is.
func (tv *tableView) movedOrder(id string, slot int) ([]string, bool) {
	order := tv.snap.DisplayOrder()
	all, visible := make([]string, 0, len(order)), make([]string, 0, len(tv.cols))
	for _, i := range order {
		all = append(all, tv.snap.Columns[i].ID)
	}
	for _, ci := range tv.cols {
		visible = append(visible, tv.snap.Columns[ci].ID)
	}
	from := slices.Index(visible, id)
	if from < 0 || slot == from || slot == from+1 {
		return nil, false
	}
	out := slices.DeleteFunc(slices.Clone(all), func(c string) bool { return c == id })
	if slot < len(visible) {
		at := slices.Index(out, visible[slot])
		return slices.Insert(out, at, id), true
	}
	last := visible[len(visible)-1]
	if last == id {
		return nil, false
	}
	return slices.Insert(out, slices.Index(out, last)+1, id), true
}

// resetButton is Reset Column Order at the end of the ⋮'s column list, while the user has
// arranged the columns.
func (d *columnDrag) resetButton(view string) fyne.CanvasObject {
	b := newTextButton("Reset Column Order", theme.ViewRestoreIcon(), func() {
		d.tv.sh.dispatch("ui.setColumnOrder", orderParams{View: view, Reset: true}, nil)
		if p := d.tv.colPopup; p != nil {
			p.Hide()
		}
	})
	b.Alignment = widget.ButtonAlignLeading
	return b
}
