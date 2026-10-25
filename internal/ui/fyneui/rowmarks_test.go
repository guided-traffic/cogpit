package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The row marks of the owner's VW16 (a checked row has the fill, the drawer's row the bar over its
// whole height, a row that is both the two) and the keyboard's ring of his VW17 (a click puts the
// keyboard on the cell clicked without the ring; a key or the keyboard's focus shows it, not on the
// drawer's row), both of his live walk of 2026-10-25.

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
			bar := c.selBar != nil && c.selBar.Visible()
			if want := wantDrawer && col == checkColumn; bar != want {
				t.Errorf("row %d, column %d: bar %v, want %v", row, col, bar, want)
			}
		}
	}
	marks(both, true, true)
	marks(checkedOnly, false, true)

	// The bar lies at the row's left edge as high as the row's plate, the cell and half the gap
	// above and below it; the spine right of it.
	gap := th.Size(theme.SizeNamePadding)
	box := cellAt(t, sh, both, checkColumn)
	if p, sz := box.selBar.Position(), box.selBar.Size(); p.X != 0 || p.Y != -gap/2 || sz.Height != box.Size().Height+gap {
		t.Errorf("the bar at %v, %v; want at the edge from %v, %v high", p, sz, -gap/2, box.Size().Height+gap)
	}
	if box.spine == nil || !box.spine.Visible() || box.spine.Position().X < selBarWidth {
		t.Fatalf("spine %v at %v: want it right of the bar", box.spine != nil && box.spine.Visible(), box.spine.Position())
	}

	// The text takes the selection's colour on the fill only.
	name := cellAt(t, sh, both, firstDataColumn)
	if name.text.Color != themeColor(th, colorRowSelectedText) {
		t.Errorf("text on the filled row in %v", name.text.Color)
	}

	// The drawer moves to a row nobody checked: the bar, no fill.
	drawerOnly := (both + 3) % len(tv.snap.Rows)
	openRow(t, sh, q, drawerOnly, restarts)
	settle(w)
	marks(drawerOnly, true, false)
	marks(both, false, true)
	if name := cellAt(t, sh, drawerOnly, firstDataColumn); name.text.Color == themeColor(th, colorRowSelectedText) {
		t.Error("the drawer's row without the fill has the selection's text colour")
	}
}

// ringed returns the cells on screen that show the keyboard's ring: the table's pool keeps cells
// with the ids they last showed.
func ringed(tv *tableView) []*tableCell {
	var out []*tableCell
	for _, c := range findAll[*tableCell](tv.table) {
		if c.focused && c.Visible() {
			out = append(out, c)
		}
	}
	return out
}

// wantRing fails unless exactly the cell at row, col shows the ring, or none for row -1.
func wantRing(t *testing.T, tv *tableView, what string, row, col int) {
	t.Helper()
	got := ringed(tv)
	switch {
	case row < 0 && len(got) != 0:
		t.Fatalf("%s: a ring on %d,%d", what, got[0].row, got[0].col)
	case row >= 0 && (len(got) != 1 || got[0].row != row || got[0].col != col || !got[0].ring.Visible()):
		cells := [][2]int{}
		for _, c := range got {
			cells = append(cells, [2]int{c.row, c.col})
		}
		t.Fatalf("%s: rings on %v, want one on %d,%d", what, cells, row, col)
	}
}

func TestTableClickMovesTheKeyboardWithoutTheRing(t *testing.T) {
	sh, w, q := rowMarksScene(t, ThemeDark)
	tv := sh.view
	restarts, age := columnOf(t, sh, "restarts"), columnOf(t, sh, "age")

	// A click focuses the table and puts the keyboard on the cell clicked, without the ring: none
	// on the first row, where the keyboard was.
	openRow(t, sh, q, 5, restarts)
	settle(w)
	if w.Canvas().Focused() != tv.table || tv.kbRow != 5 || tv.kbCol != restarts {
		t.Fatalf("after a click: focused %T, keyboard on %d,%d", w.Canvas().Focused(), tv.kbRow, tv.kbCol)
	}
	wantRing(t, tv, "after a click", -1, 0)

	// ↓ goes on from the row clicked and shows the ring there.
	press(t, w, fyne.KeyDown)
	wantRing(t, tv, "after ↓", 6, restarts)

	// Another click hides it again; ↵ then opens the row the keyboard is on, the clicked one.
	openRow(t, sh, q, 9, age)
	wantRing(t, tv, "after the second click", -1, 0)
	press(t, w, fyne.KeyEscape)
	q.until(t, "the drawer closed", func() bool { return sh.ui.Drawer == nil })
	wantRing(t, tv, "after Escape", 9, age)
	press(t, w, fyne.KeyUp)
	press(t, w, fyne.KeyReturn)
	q.until(t, "row 8's drawer", func() bool { return sh.ui.Drawer != nil && *sh.ui.Drawer == tv.snap.Rows[8].Ref })
	wantRing(t, tv, "after ↑↵, on the drawer's row with its bar", -1, 0)
	press(t, w, fyne.KeyDown)
	wantRing(t, tv, "after ↓ from the drawer's row", 9, age)

	// A checkbox click moves the keyboard too, without scrolling: ↓ goes on from its row. (The GL
	// driver keeps the table's focus through a click inside it; the test driver's tap takes it, so
	// the focus comes back as the keyboard brings it, with the ring.)
	checkRow(t, sh, q, 3)
	wantRing(t, tv, "after a checkbox click", -1, 0)
	w.Canvas().Focus(tv.table)
	wantRing(t, tv, "the focus back", 3, checkColumn)
	press(t, w, fyne.KeyDown)
	wantRing(t, tv, "after ↓ from the checkbox", 4, checkColumn)
}

func TestTableFocusByKeyboardShowsTheRing(t *testing.T) {
	sh, w, q := rowMarksScene(t, ThemeDark)
	tv := sh.view
	openRow(t, sh, q, 4, firstDataColumn)
	wantRing(t, tv, "after a click", -1, 0)

	// Tab from the search field to the table: the focus comes by the keyboard, the ring shows on
	// the cell the click left the keyboard on.
	c := w.Canvas()
	c.Focus(tv.search)
	for range 20 {
		if c.Focused() == tv.table {
			break
		}
		c.FocusNext()
	}
	if c.Focused() != tv.table {
		t.Fatalf("Tab did not reach the table: focused %T", c.Focused())
	}
	wantRing(t, tv, "after Tab into the table, on the drawer's row with its bar", -1, 0)
	sh.closeDrawer()
	q.until(t, "the drawer closed", func() bool { return sh.ui.Drawer == nil })
	wantRing(t, tv, "the drawer closed", 4, firstDataColumn)
	c.Unfocus()
	wantRing(t, tv, "after leaving the table", -1, 0)
}

// renderRowMarks shows the marks of VW16 and the ring of VW17 together: two rows checked, the
// first of them the drawer's too and in an abnormal state (fill, bar and spine), and the ring on
// the row below after ↓ from the click.
func renderRowMarks(t *testing.T, mode ThemeMode, name string) {
	sh, w, q := rowMarksScene(t, mode)
	both := firstAbnormalRow(t, sh)
	for _, row := range []int{both, both + 2} {
		checkRow(t, sh, q, row)
	}
	openRow(t, sh, q, both, columnOf(t, sh, "restarts"))
	press(t, w, fyne.KeyDown)
	settle(w)
	savePNG(t, w, name)
}

func TestRenderRowMarksDark(t *testing.T)  { renderRowMarks(t, ThemeDark, "row_marks_dark.png") }
func TestRenderRowMarksLight(t *testing.T) { renderRowMarks(t, ThemeLight, "row_marks_light.png") }
