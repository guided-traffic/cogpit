package fyneui

import (
	"errors"
	"fmt"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A double click on the separator at a column's right edge in the header row fits the column to its
// content (ui-parity › Tables), where the resize cursor shows. The header cells do not cover the
// separators, so a click there reaches the table, as one below the rows and in the gaps between
// body cells does.
//
// The table tells the double click itself, as the tab bar and the navigator do (doubleClickTime),
// and is no fyne.DoubleTappable: Fyne's driver holds every tap on a DoubleTappable for the
// double-click interval before it calls Tapped or DoubleTapped (internal/driver/glfw/window.go,
// mouseClickedHandleTapDoubleTap; on macOS NSEvent.doubleClickInterval, 0.5 s by default), so the
// click below the rows closed the drawer only then (the owner's alpha walk, AW12; D-106). The first
// click acts at once and the second fits the column.

// separatorTapped takes a click at pos, in the table's coordinates, when it is on a header
// separator: the second click on the same column's separator within doubleClickTime fits that
// column, unless it keeps its width. Any other click on the table ends a double click begun there.
func (t *resourceTable) separatorTapped(pos fyne.Position) bool {
	col := t.tv.separatorAt(pos)
	if col < 0 {
		t.sepClick = click{}
		return false
	}
	if t.tv.fixedAt(col) {
		t.sepClick = click{}
		return true
	}
	if t.tv.sh.tabs.double(&t.sepClick, t.tv.snap.View+"/"+t.tv.snap.Columns[t.tv.cols[col]].ID) {
		t.tv.fitToContent(col)
	}
	return true
}

// separatorAt returns the visible column whose right-edge separator in the header row is at pos
// (relative to the table), or -1; the checkbox's separator is none. setWidths are by table column.
func (tv *tableView) separatorAt(pos fyne.Position) int {
	if tv.snap == nil || pos.Y < 0 || pos.Y > newHeaderCell(nil).MinSize().Height {
		return -1
	}
	gap := tv.table.Theme().Size(theme.SizeNamePadding)
	x := pos.X + tv.table.offset.X
	start := float32(0)
	for col := 0; col < tv.menuColumn() && col < len(tv.setWidths); col++ {
		end := start + tv.setWidths[col]
		if x >= end && x < end+gap {
			if vis, ok := tv.dataColumn(col); ok {
				return vis
			}
			return -1
		}
		start = end + gap
	}
	return -1
}

// fitToContent sizes a visible column to its widest content: its title (with the sort arrow) and
// the text of every row as the table shows it, not only the rows on screen. The width counts as
// the user's, as a dragged one does.
func (tv *tableView) fitToContent(col int) {
	if tv.snap == nil || col < 0 || col >= len(tv.cols) {
		return
	}
	th := tv.table.Theme()
	pad := th.Size(sizeCellPadding)
	ci := tv.cols[col]
	c := tv.snap.Columns[ci]
	size := th.Size(theme.SizeNameText)
	w := fyne.MeasureText(c.Title, size, fyne.TextStyle{Bold: true}).Width
	if tv.snap.Sort.Column == c.ID {
		w += th.Size(theme.SizeNamePadding)/2 + th.Size(sizeSortIcon)
	}
	switch c.Type {
	case model.ColumnWarning:
		w = max(w, th.Size(sizeCellIcon))
	case model.ColumnIcon:
		w = chartIconMaxSide // a chart's icon (helmcharticons.go); the column has no title
	default:
		rows := tv.snap.Rows
		w = max(w, widestText(len(rows), func(i int) string {
			var cell model.Cell
			if ci < len(rows[i].Cells) {
				cell = rows[i].Cells[ci]
			}
			return tv.tf.CellText(c, cell, tv.now)
		}, size))
	}
	if c.ID == columnStatus {
		w += pillPadX*2 + pillDotSide + pillDotGap // the pill's padding and dot (list_rows.go)
	}
	tv.userWidths[tv.snap.View+"/"+c.ID] = w + 2*pad + 1 // a point of slack against rounding
	tv.fitWidth = 0
	tv.fitColumns(tv.table.Size().Width)
}

// fitCandidates is how many of the widest texts by estimate widestText measures exactly.
const fitCandidates = 16

// widestText returns the width of the widest of n texts. Measuring 10,000 distinct names with
// fyne.MeasureText (text shaping) costs about 150 ms on the main goroutine; widestText estimates
// each text from the advance widths of its runes, measured once per rune, and measures only the
// widest candidates exactly. Kerning moves a text by a fraction of a point, far less than what
// separates a candidate from the texts it beat.
func widestText(n int, text func(int) string, size float32) float32 {
	style := fyne.TextStyle{}
	adv := map[rune]float32{}
	type cand struct {
		s string
		w float32
	}
	var top []cand // the widest by estimate, widest first
	for i := range n {
		s := text(i)
		est := float32(0)
		for _, r := range s {
			a, ok := adv[r]
			if !ok {
				a = fyne.MeasureText(string(r), size, style).Width
				adv[r] = a
			}
			est += a
		}
		if len(top) == fitCandidates && est <= top[len(top)-1].w {
			continue
		}
		at := len(top)
		for at > 0 && top[at-1].w < est {
			at--
		}
		top = slices.Insert(top, at, cand{s, est})
		if len(top) > fitCandidates {
			top = top[:fitCandidates]
		}
	}
	w := float32(0)
	for _, c := range top {
		w = max(w, fyne.MeasureText(c.s, size, style).Width)
	}
	return w
}

// itemCount is the header's count of a view's rows: "18 items" as captured (2026-10-11); with a
// filter that hides some, "18 of 10,000 items" (Cogpit's).
func itemCount(shown, total int) string {
	noun := " items"
	if total == 1 {
		noun = " item"
	}
	if shown != total {
		return formatCount(int64(shown)) + " of " + formatCount(int64(total)) + noun
	}
	return formatCount(int64(shown)) + noun
}

// csvExport is what table.exportCsv returns.
type csvExport struct {
	Filename string `json:"filename"`
	Rows     int    `json:"rows"`
	CSV      string `json:"csv"`
}

// exportCSV is the download button beside the item count: the engine writes the open view as CSV
// (table.exportCsv, so agents export the same text), and the system's save panel stores it.
func (tv *tableView) exportCSV() {
	tv.sh.dispatch("table.exportCsv", struct{}{}, func(res any, err error) {
		if err != nil {
			tv.sh.showError(err)
			return
		}
		var out csvExport
		if derr := decode(res, &out); derr != nil {
			tv.sh.toast("error", "Unexpected table.exportCsv result: "+derr.Error())
			return
		}
		tv.sh.saveFile(out.Filename, out.Rows, []byte(out.CSV))
	})
}

// saveWithDialog asks where to save data in Fyne's dialog, suggesting name, and writes it there:
// the shell's saveFile (saveTable) where the system has no save panel. start, if set, is the
// folder the dialog opens in.
func (sh *shell) saveWithDialog(name string, rows int, data []byte, start fyne.ListableURI) *dialog.FileDialog {
	d := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err == nil && w == nil {
			return // cancelled
		}
		if err == nil {
			err = writeAndClose(w, data)
		}
		if err != nil {
			sh.toast("error", "Could not save the table: "+err.Error())
			return
		}
		sh.toast("info", fmt.Sprintf("Saved %s to %s", itemCount(rows, rows), w.URI().Name()))
	}, sh.win)
	d.SetFileName(name)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".csv"}))
	if start != nil {
		d.SetLocation(start)
	}
	sh.showDialog(d)
	return d
}

// writeAndClose writes data and closes w; a failing close (the file system did not take the
// last bytes) is an error too.
func writeAndClose(w fyne.URIWriteCloser, data []byte) error {
	_, err := w.Write(data)
	return errors.Join(err, w.Close())
}

// The column list behind the ⋮ at the end of the header row (ui-parity › Tables): one check per
// column of the view, checked when the column shows. Hidden columns are engine state
// (UIState.HiddenColumns, set with ui.setColumns), so the list shows what the snapshot says and a
// toggle only dispatches; the snapshot that follows hides or shows the column.

// openColumns shows the open view's columns below the header's ⋮, at the table's right edge.
// Checks toggle without closing the list, as the namespace filter's do.
func (tv *tableView) openColumns() {
	if tv.snap == nil {
		return
	}
	tv.colView = tv.snap.View
	tv.colChecks, tv.colIDs = tv.colChecks[:0], tv.colIDs[:0]
	list := container.NewVBox()
	for _, i := range tv.snap.DisplayOrder() { // the columns as the table shows them (columnorder.go)
		c := tv.snap.Columns[i]
		id := c.ID
		ch := widget.NewCheck(c.Title, func(on bool) { tv.toggleColumn(id, on) })
		tv.colChecks = append(tv.colChecks, ch)
		tv.colIDs = append(tv.colIDs, id)
		list.Add(ch)
	}
	tv.syncColumnChecks()
	if tv.snap.Order != nil { // the user arranged the columns: the way back (columnorder.go)
		list.Add(widget.NewSeparator())
		list.Add(tv.order.resetButton(tv.colView))
	}
	th := tv.table.Theme()
	scroll := container.NewVScroll(list)
	header := newHeaderCell(nil).MinSize().Height
	// The list may reach the table's bottom: a kind has a dozen columns or so, and seeing them all
	// at once is the point of the list.
	h := min(list.MinSize().Height, max(tv.table.Size().Height-2*header, th.Size(sizePopupMaxList)))
	w := max(list.MinSize().Width+th.Size(sizeCellPadding)*2, th.Size(sizeFilterWidth))
	scroll.SetMinSize(fyne.NewSize(w, h))
	c := fyne.CurrentApp().Driver().CanvasForObject(tv.table)
	if c == nil {
		return
	}
	tv.colPopup = widget.NewPopUp(scroll, c)
	pos := fyne.NewPos(tv.table.Size().Width-tv.colPopup.MinSize().Width, header)
	p := tv.colPopup
	onShown(c, func() { p.ShowAtRelativePosition(pos, tv.table) }, p.Hide) // Escape closes it (escape.go)
}

// columnsOpen reports whether the column list is showing.
func (tv *tableView) columnsOpen() bool { return tv.colPopup != nil && tv.colPopup.Visible() }

// syncColumnChecks makes the list's checks show the snapshot's columns. A list opened for another
// view closes.
func (tv *tableView) syncColumnChecks() {
	if len(tv.colChecks) == 0 || tv.snap == nil {
		return
	}
	if tv.snap.View != tv.colView {
		if tv.colPopup != nil {
			tv.colPopup.Hide()
		}
		tv.colChecks, tv.colIDs = nil, nil
		return
	}
	tv.colSync = true
	defer func() { tv.colSync = false }()
	for i, ch := range tv.colChecks {
		shown := false
		for _, c := range tv.snap.Columns {
			if c.ID == tv.colIDs[i] {
				shown = !c.Hidden
				break
			}
		}
		if ch.Checked != shown {
			ch.SetChecked(shown)
		}
	}
}

// toggleColumn shows or hides a column: it sends the view's new list of hidden columns. The
// columns the cluster's settings hide (TableSnapshot.MetricsHidden) are not the view's own and stay
// out of it. The engine refuses to hide the last visible column; the check then goes back.
func (tv *tableView) toggleColumn(id string, show bool) {
	if tv.colSync || tv.snap == nil || tv.snap.View != tv.colView {
		return
	}
	hidden := []string{}
	for _, c := range tv.snap.Columns {
		if c.Hidden && c.ID != id && !slices.Contains(tv.snap.MetricsHidden, c.ID) {
			hidden = append(hidden, c.ID)
		}
	}
	if !show {
		hidden = append(hidden, id)
	}
	if len(hidden)+len(tv.snap.MetricsHidden) >= len(tv.snap.Columns) {
		tv.sh.toast("warning", "At least one column stays visible.")
		tv.syncColumnChecks()
		return
	}
	tv.sh.dispatch("ui.setColumns", columnsParams{View: tv.colView, Hidden: hidden}, func(_ any, err error) {
		tv.sh.showError(err)
		tv.syncColumnChecks()
	})
}
