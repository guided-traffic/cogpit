package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The row marks of the owner's VW16 of his live walk of 2026-10-25: a checked row has the fill, the
// drawer's row an outline and the bar, a row that is both all three.

// rowMarksScene opens Pods with seven columns hidden, so that a row shows from its checkbox to its
// ⋮ beside the drawer.
func rowMarksScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	f := kindsFake(t, "pods")
	hidden := []string{"namespace", model.ColumnCPU, model.ColumnMemory, "containers", "controlledBy", "node", "qos"}
	f.ui.HiddenColumns = map[string][]string{"pods": hidden}
	f.tables["pods"].Columns = engine.ColumnsShown(f.tables["pods"].Columns, hidden, true)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	return sh, w, q
}

// checkRow ticks a row's checkbox.
func checkRow(t *testing.T, sh *shell, q *mainQueue, row int) {
	t.Helper()
	test.Tap(cellAt(t, sh, row, checkColumn))
	q.until(t, "the row checked", func() bool { return sh.view.check.has(sh.view.snap.Rows[row].Ref) })
}

// openRow clicks a row's cell, which opens its drawer.
func openRow(t *testing.T, sh *shell, q *mainQueue, row, col int) {
	t.Helper()
	ref := sh.view.snap.Rows[row].Ref
	test.Tap(cellAt(t, sh, row, col))
	q.until(t, "the drawer", func() bool { return sh.drawer.shown != nil && *sh.drawer.shown == ref })
}

// firstAbnormalRow is the first row in an abnormal state: one with a status spine.
func firstAbnormalRow(t *testing.T, sh *shell) int {
	t.Helper()
	for i, r := range sh.view.snap.Rows {
		if abnormalStatus(r.Status) {
			return i
		}
	}
	t.Fatal("no row in an abnormal state")
	return -1
}

func TestRowMarksTellCheckedFromTheDrawer(t *testing.T) {
	sh, w, q := rowMarksScene(t, ThemeDark)
	tv := sh.view
	both := firstAbnormalRow(t, sh) // checked, the drawer's, and its spine
	checkedOnly := (both + 1) % len(tv.snap.Rows)
	for _, row := range []int{both, checkedOnly} {
		checkRow(t, sh, q, row)
	}
	restarts := columnOf(t, sh, "restarts")
	openRow(t, sh, q, both, restarts)
	settle(w)

	th := tv.table.Theme()
	fill := themeColor(th, colorRowSelected)
	last := tv.menuColumn()
	marks := func(row int, wantDrawer, wantChecked bool) {
		t.Helper()
		for _, col := range []int{checkColumn, restarts, last} {
			c := cellAt(t, sh, row, col)
			if got := c.bg.FillColor == fill; got != wantChecked {
				t.Errorf("row %d, column %d: fill %v, want %v", row, col, got, wantChecked)
			}
			if got := c.outline != nil && c.outline.box.Visible(); got != wantDrawer {
				t.Errorf("row %d, column %d: outline %v, want %v", row, col, got, wantDrawer)
			}
			bar := c.selBar != nil && c.selBar.Visible()
			if want := wantDrawer && col == checkColumn; bar != want {
				t.Errorf("row %d, column %d: bar %v, want %v", row, col, bar, want)
			}
		}
	}
	marks(both, true, true)
	marks(checkedOnly, false, true)

	// The outline is one rounded rectangle across the row: the top and bottom edge in every cell,
	// the sides and their corners only at the ends, the edges meeting across the gaps.
	gap := th.Size(theme.SizeNamePadding)
	for _, col := range []int{checkColumn, restarts, last} {
		o := cellAt(t, sh, both, col).outline
		first, end := col == checkColumn, col == last
		if !o.top.Visible() || !o.bottom.Visible() || o.left.Visible() != first || o.right.Visible() != end {
			t.Errorf("column %d: top %v bottom %v left %v right %v", col, o.top.Visible(), o.bottom.Visible(), o.left.Visible(), o.right.Visible())
		}
		for i, a := range o.corners {
			if want := i < 2 && first || i >= 2 && end; a.Visible() != want {
				t.Errorf("column %d: corner %d shown %v, want %v", col, i, a.Visible(), want)
			}
		}
		if !first && o.top.Position().X > -gap/2 {
			t.Errorf("column %d: the top edge begins at %v, not in the gap before the cell", col, o.top.Position().X)
		}
		if !end && o.top.Position().X+o.top.Size().Width < cellAt(t, sh, both, col).Size().Width+gap/2 {
			t.Errorf("column %d: the top edge ends short of the next cell", col)
		}
	}

	// The spine still shows, right of the outline, and the bar right of the spine.
	box := cellAt(t, sh, both, checkColumn)
	if box.spine == nil || !box.spine.Visible() || box.spine.Position().X < outlineWidth ||
		box.selBar.Position().X < box.spine.Position().X+spineWidth {
		t.Fatalf("spine %v at %v, bar at %v: want the spine right of the outline and the bar after it",
			box.spine != nil && box.spine.Visible(), box.spine.Position(), box.selBar.Position())
	}

	// The text takes the selection's colour on the fill only.
	name := cellAt(t, sh, both, firstDataColumn)
	if name.text.Color != themeColor(th, colorRowSelectedText) {
		t.Errorf("text on the filled row in %v", name.text.Color)
	}

	// The drawer moves to a row nobody checked: an outline and the bar, no fill.
	drawerOnly := (both + 3) % len(tv.snap.Rows)
	openRow(t, sh, q, drawerOnly, restarts)
	settle(w)
	marks(drawerOnly, true, false)
	marks(both, false, true)
	if name := cellAt(t, sh, drawerOnly, firstDataColumn); name.text.Color == themeColor(th, colorRowSelectedText) {
		t.Error("the drawer's row without the fill has the selection's text colour")
	}
}

// renderRowMarks shows the marks of VW16 with the keyboard's ring: two rows checked, the first of
// them the drawer's too and in an abnormal state (fill, outline, spine and bar), and the ring on the
// drawer's row after ↓↑ from the click.
func renderRowMarks(t *testing.T, mode ThemeMode, name string) {
	sh, w, q := rowMarksScene(t, mode)
	both := firstAbnormalRow(t, sh)
	for _, row := range []int{both, both + 2} {
		checkRow(t, sh, q, row)
	}
	openRow(t, sh, q, both, columnOf(t, sh, "restarts"))
	press(t, w, fyne.KeyDown)
	press(t, w, fyne.KeyUp)
	settle(w)
	savePNG(t, w, name)
}

func TestRenderRowMarksDark(t *testing.T)  { renderRowMarks(t, ThemeDark, "row_marks_dark.png") }
func TestRenderRowMarksLight(t *testing.T) { renderRowMarks(t, ThemeLight, "row_marks_light.png") }
