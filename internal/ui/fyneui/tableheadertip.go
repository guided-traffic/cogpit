package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
)

// A column header's tooltip: the snapshot's ColumnTips by column id (the Nodes list's Disk says
// where its numbers come from, or why it is empty: G12), after the title whole when the column is
// too narrow for it (headerCell.cut). A header without either shows none. The
// table follows the pointer itself (resourceTable.MouseMoved, which also drives the resize cursor
// over the header's gaps), so a header cell is not hoverable: the table asks which header is under
// the pointer when it asks for a body cell's tooltip.

// headerTipShown asks for the tooltip of the header under the pointer and reports whether it did.
func (tv *tableView) headerTipShown() bool {
	if !tv.pointerIn {
		return false
	}
	h := tv.headerUnderPointer()
	if h == nil {
		return false
	}
	tip := tv.headerTip(h.col)
	if h.cut && h.title != "" {
		tip = strings.TrimSuffix(h.title+"\n"+tip, "\n")
	}
	if tip == "" {
		return false
	}
	tv.sh.tip.hover(h, tip, tv.pointer)
	return true
}

// headerUnderPointer returns the visible header cell the pointer is on; nil off the header row.
func (tv *tableView) headerUnderPointer() *headerCell {
	drv := fyne.CurrentApp().Driver()
	for _, h := range tv.headers {
		if !h.Visible() || h.col < 0 {
			continue
		}
		p, s := drv.AbsolutePositionForObject(h), h.Size()
		if tv.pointer.X >= p.X && tv.pointer.X < p.X+s.Width && tv.pointer.Y >= p.Y && tv.pointer.Y < p.Y+s.Height {
			return h
		}
	}
	return nil
}

// headerTip is a table column's tooltip from the snapshot; "" without.
func (tv *tableView) headerTip(col int) string {
	vis, ok := tv.dataColumn(col)
	if tv.snap == nil || !ok || len(tv.snap.ColumnTips) == 0 {
		return ""
	}
	return tv.snap.ColumnTips[tv.snap.Columns[tv.cols[vis]].ID]
}
