package fyneui

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Copying the text of a list's cells (D-25, the owner's addition of 2026-10-13): names in Pods,
// Deployments and every other view are copyable with the drawer's copy icon on hover, with the same
// checkmark after a copy. As in the drawer it is a GUI behaviour and no command (TestCopyHasNoCommand).
//
// The owner's feedback after seeing it (2026-10-13): not every cell ("a name, an IP, a node name,
// a message in the Events is great; but no Age, no CPU, no Memory, no Restarts"), by a blacklist,
// so that a custom resource's columns need no marking and keep the icon unless blacklisted
// (copyColumn); and the icon right after the text, not at the column's end.
//
// The owner's decision (A69, 2026-10-14 evening; built 2026-10-15): when the text leaves no room
// after it, the icon lies over the cell's end and the text is not shortened for it: the text keeps
// the cell's whole width (cut off with "…" only where the cell itself ends, its tooltip showing the
// whole text), and the icon's opaque plate covers what lies under it.
//
// Cogpit's defaults (the lead's proposals, not the owner's decision, 2026-10-13):
//   - a cell that shows no text has none: the checkbox, the ⋮, the warning triangle, a pod's
//     container squares, an empty cell and the placeholders "-", "–", "—" and "<none>";
//   - the icon sits a padding after the text, centred on the row, and at the cell's right end
//     when the text reaches it, always inside the cell, so that it never covers the next column;
//   - a click on it copies the cell's full text, not the text shortened with "…", shows the
//     checkmark and the drawer's toast, and neither selects the row, toggles its checkbox nor
//     opens the drawer: the icon is a widget above the table, so the click never reaches the cell;
//   - a Secret's list holds no values (Name, Namespace, Labels, the names of its Keys, Type, Age:
//     kinds/secrets.go, TestSecretDetailShowsNoValue), so a copy from a list is always a plain one
//     (shell.copyText with secret false) and writes no audit line, as the drawer's copy.
//
// One icon per table, moved to the cell under the pointer (a list has 10,000 rows; a widget per
// cell would cost), in a layer above the table as the column drag's chip (columnorder.go). The
// table follows the pointer by cells: setHoverCell hands the icon the cell under the pointer, and
// a pointer move inside the table shows it while the pointer is inside that cell and hides it in
// the gaps and over the header, where Fyne reports no cell.

// The columns without the copy icon: a blacklist (the owner, 2026-10-13), so that every other
// column, a custom resource's printer columns included, keeps it. The owner's principle, with his
// four examples Age, CPU, Memory and Restarts: identifiers and free text keep the icon (names,
// namespaces, IPs, hosts, ports, images, node names, labels, selectors, messages, reasons, URLs,
// versions, types); numbers, times, durations, counts, ratios, booleans and short state words do
// not. The lists below are Cogpit's reading of it for the built-in kinds (D-25 names every id),
// one place to edit.

// noCopyTypes are the column types without the icon, whatever the column. A custom resource's
// printer columns map to them by their type: integer and number to ColumnNumber, date to
// ColumnAge; string and boolean are text (kinds/customresources.go, compilePrinterColumn).
var noCopyTypes = map[model.ColumnType]bool{
	model.ColumnNumber:  true, // Restarts, Count, Replicas, a workload's Pods "3/3", Size, Revision
	model.ColumnAge:     true, // Age, Last Schedule, Updated
	model.ColumnUntil:   true, // a Cron Job's Next Execution
	model.ColumnTime:    true, // an event's Last Seen (D-31)
	model.ColumnMetric:  true, // CPU, Memory
	model.ColumnWarning: true, // the warning triangle
	model.ColumnIcon:    true, // a chart's icon
}

// noCopyColumns are the text columns without the icon, by column id, in every view (a custom
// resource's printer column of the same id too: its id is its name in lowerCamelCase).
var noCopyColumns = map[string]bool{
	// Short state words.
	columnStatus: true, // Pods, Services, PVCs, PVs, Namespaces, HPAs, Port Forwards, Helm releases, VPAs, Applications
	"conditions": true, // Nodes, Deployments, Jobs
	"state":      true, // Resource Claims
	"phase":      true, // custom resources
	"qos":        true, // Pods
	"mode":       true, // VPAs: the update mode
	// Booleans.
	"suspend":       true, // Cron Jobs
	"default":       true, // Ingress Classes
	"globalDefault": true, // Priority Classes
	"privileged":    true, // Pod Security Policies
	// Counts, ratios and durations in text form.
	"ready":          true, // "1/1"
	"desired":        true,
	"current":        true,
	"upToDate":       true,
	"available":      true,
	"completions":    true,
	"duration":       true,
	"minAvailable":   true, // Pod Disruption Budgets: a count or a percentage
	"maxUnavailable": true, // Pod Disruption Budgets
	"metrics":        true, // HPAs: current against target, "cpu 12% / 80%"
	// Policy and mode words of a fixed vocabulary.
	"failurePolicy":      true, // admission policies: Fail, Ignore
	"reinvocationPolicy": true, // mutating admission policies: Never, IfNeeded
	"reclaimPolicy":      true, // Storage Classes: Delete, Retain
	"volumeBindingMode":  true, // Storage Classes: Immediate, WaitForFirstConsumer
	"validationActions":  true, // validating admission policy bindings: Deny, Warn, Audit
	"policyTypes":        true, // Network Policies: Ingress, Egress
	"scope":              true, // Definitions, Ingress Classes: Namespaced, Cluster
	"protocol":           true, // Port Forwards: TCP
}

// Column ids that code outside the profiles names: the Status column of many kinds and the Message
// column of Events.
const (
	columnStatus  = "status"
	columnMessage = "message"
)

// copyColumn reports whether a column's cells have the copy icon.
func copyColumn(c model.Column) bool { return !noCopyTypes[c.Type] && !noCopyColumns[c.ID] }

// tableCopy is a list's copy icon and the cell it is for.
type tableCopy struct {
	tv    *tableView
	btn   *copyButton
	layer *fyne.Container // above the table, without a layout: the icon

	cell     *tableCell    // the cell the icon is for; kept while the pointer is on the icon (entered)
	row, col int           // the cell's row and column when it got the icon
	on       bool          // the icon is placed for cell: the pointer inside lo–hi shows it
	lo, hi   fyne.Position // the cell's visible part, in window coordinates
	text     string        // the cell's text when the icon was placed after it
	stale    bool          // the cells moved under a pointer that did not: the next move looks again
	done     *time.Timer   // the checkmark's
}

func newTableCopy(tv *tableView) *tableCopy {
	tc := &tableCopy{tv: tv}
	tc.btn = newCopyIcon(tv.sh, tc.copyCell, tc.entered, tc.left)
	tc.btn.alt = tc.rowMenu
	tc.layer = container.NewWithoutLayout(tc.btn)
	return tc
}

// rowMenu opens the actions menu of the icon's row on a right-click, as a right-click anywhere
// else on the row does (ui-parity › Tables).
func (tc *tableCopy) rowMenu(e *fyne.PointEvent) {
	if c := tc.cell; c != nil && c.row == tc.row && tc.over(c) {
		tc.tv.openRowMenu(c.row, e.AbsolutePosition)
	}
}

// textOf is what the copy icon copies from a cell: its full text, or "" for a cell without the
// icon (a blacklisted column, no text).
func (tc *tableCopy) textOf(c *tableCell) string {
	tv := tc.tv
	vis, ok := tv.dataColumn(c.col)
	if !ok || tv.snap == nil || !copyColumn(tv.snap.Columns[tv.cols[vis]]) {
		return ""
	}
	return c.copyText()
}

// copyText is a cell's full text, or "" for a cell that shows no text.
func (c *tableCell) copyText() string {
	if c.content.kind != cellText || c.content.marks != nil {
		return ""
	}
	switch strings.TrimSpace(c.content.text) {
	case "", "-", "–", "—", "<none>":
		return ""
	}
	return c.content.text
}

// follow gives the icon to the cell under the pointer, or hides it for nil (setHoverCell). A
// checkmark stays only on the cell it was shown for.
func (tc *tableCopy) follow(c *tableCell) {
	if c == nil {
		tc.hide()
		return
	}
	if c != tc.cell || c.row != tc.row || c.col != tc.col {
		tc.btn.resetConfirm(&tc.done)
	}
	tc.cell, tc.row, tc.col = c, c.row, c.col
	tc.place()
}

// hide hides the icon. The cell is kept: the pointer may have moved onto the icon (entered).
func (tc *tableCopy) hide() {
	tc.on, tc.stale = false, false
	tc.show(false)
}

func (tc *tableCopy) show(on bool) {
	if on != tc.btn.Visible() {
		if on {
			tc.btn.Show()
		} else {
			tc.btn.Hide()
		}
	}
}

// place puts the icon a padding after its cell's text, centred on the row, or over the right end
// of the cell's visible part when the text reaches it (A69: the text is not shortened for it), and
// shows it while the pointer is inside that part; a cell without the icon, or with too little of
// it in view, hides it. It touches only the icon: the cells are never re-laid out for it.
func (tc *tableCopy) place() {
	c, tv := tc.cell, tc.tv
	if c == nil || !tv.pointerIn || !c.Visible() || tc.textOf(c) == "" {
		tc.hide()
		return
	}
	drv := fyne.CurrentApp().Driver()
	at, sz := drv.AbsolutePositionForObject(c), c.Size()
	tp, ts := drv.AbsolutePositionForObject(tv.table), tv.table.Size()
	// The part of the cell in the table's body: the header row, as high as a row (both are a line
	// of text and the row padding), covers the top of a cell scrolled under it.
	lo := fyne.NewPos(max(at.X, tp.X), max(at.Y, tp.Y+sz.Height))
	hi := fyne.NewPos(min(at.X+sz.Width, tp.X+ts.Width), min(at.Y+sz.Height, tp.Y+ts.Height))
	side := min(tc.btn.MinSize().Width, sz.Height)
	if hi.X-lo.X < side || hi.Y-lo.Y < side {
		tc.hide()
		return
	}
	tc.lo, tc.hi, tc.on, tc.stale, tc.text = lo, hi, true, false, c.full
	// Right after the whole text, a padding away. A text that leaves no room for the icon puts it
	// over the cell's end, the text as it is under it (A69).
	th := c.Theme()
	pad, gap := th.Size(sizeCellPadding), th.Size(theme.SizeNamePadding)
	x := max(at.X+pad+fyne.MeasureText(c.full, c.text.TextSize, c.text.TextStyle).Width+gap, lo.X)
	if x+side > hi.X {
		x = hi.X - side
	}
	y := min(max(at.Y+(sz.Height-side)/2, lo.Y), hi.Y-side)
	tc.btn.Move(fyne.NewPos(x, y).Subtract(drv.AbsolutePositionForObject(tc.layer)))
	tc.btn.Resize(fyne.NewSquareSize(side))
	tc.show(tc.inside(tv.pointer))
}

// over reports whether the icon still lies on the cell c: a scroll the table did not report (Fyne
// scrolls on its own for the arrow keys) moves the cells, not the icon.
func (tc *tableCopy) over(c *tableCell) bool {
	if !c.Visible() {
		return false
	}
	drv := fyne.CurrentApp().Driver()
	at := drv.AbsolutePositionForObject(c)
	mid := drv.AbsolutePositionForObject(tc.btn).AddXY(tc.btn.Size().Width/2, tc.btn.Size().Height/2)
	return mid.X >= at.X && mid.X < at.X+c.Size().Width && mid.Y >= at.Y && mid.Y < at.Y+c.Size().Height
}

// inside reports whether a point in window coordinates lies in the icon's cell.
func (tc *tableCopy) inside(p fyne.Position) bool {
	return p.X >= tc.lo.X && p.X < tc.hi.X && p.Y >= tc.lo.Y && p.Y < tc.hi.Y
}

// moved follows the pointer over the table (resourceTable.MouseMoved), after the table moved its
// row band: the icon shows while the pointer is inside its cell and hides in the gaps between
// cells and over the header, where the table keeps its band; once the cells moved under the
// pointer (a scroll, new column widths) it looks for the cell under the pointer again. O(1) per
// move unless the cells moved.
func (tc *tableCopy) moved() {
	tv := tc.tv
	if tc.stale {
		tc.stale = false
		tc.follow(tv.cellUnderPointer(tv.hoverRow, tv.hoverCol))
		return
	}
	if tc.on {
		tc.show(tc.inside(tv.pointer))
	}
}

// moving hides the icon while the cells move under a pointer that does not: a scroll, new column
// widths. The next pointer move puts it on the cell under the pointer again.
func (tc *tableCopy) moving() {
	if !tc.on && !tc.btn.Visible() {
		return
	}
	tc.hide()
	tc.stale = tc.tv.pointerIn
}

// updated follows a cell the table filled (updateCell). The icon's cell keeps it with new text (a
// watch event: the icon moves to the new text's end and copies the text the cell shows when
// clicked) and loses it without text; a cell the table gave to another row or column moved away
// from the pointer. O(1) for every other cell: a comparison.
func (tc *tableCopy) updated(c *tableCell) {
	if c != tc.cell {
		return
	}
	switch {
	case c.row != tc.row || c.col != tc.col:
		if tc.on || tc.btn.Visible() {
			tc.hide()
			tc.stale = tc.tv.pointerIn
		}
	case tc.on && c.full != tc.text:
		tc.place() // another text, or none: after its end, or hidden
	case !tc.on && tc.tv.pointerIn && tc.tv.hoverRow == c.row && tc.tv.hoverCol == c.col && tc.textOf(c) != "":
		tc.stale = true // the cell under the pointer got text: the next move shows the icon
	}
}

// entered keeps the row band and the icon when the pointer moves from the cell onto the icon: the
// table has just seen the pointer leave (resourceTable.MouseOut), which cleared both.
func (tc *tableCopy) entered(ev *desktop.MouseEvent) {
	c, tv := tc.cell, tc.tv
	if c == nil || c.row != tc.row || c.col != tc.col {
		return
	}
	tv.pointer, tv.pointerIn = ev.AbsolutePosition, true
	tv.setHoverCell(tc.row, tc.col)
}

// left ends what entered kept, as the table's MouseOut does; when the pointer moves back onto the
// table, its MouseIn brings the band and the icon back.
func (tc *tableCopy) left() {
	tc.tv.pointerIn = false
	tc.tv.setHover(-1)
}

// copyCell puts the full text of the icon's cell on the clipboard, as the drawer's icon does: the
// checkmark and the toast, a plain copy (no Secret's value is in a list), no audit line.
func (tc *tableCopy) copyCell() {
	c := tc.cell
	if c == nil || c.row != tc.row || c.col != tc.col || !tc.over(c) {
		tc.hide() // the cells moved under the icon (Fyne's own scroll, as its keys do): not its cell
		return
	}
	text := tc.textOf(c)
	if text == "" {
		return
	}
	tc.tv.sh.copyText(text, false)
	tc.btn.confirm(&tc.done)
	tc.tv.sh.copyToast("info", copiedToast)
}
