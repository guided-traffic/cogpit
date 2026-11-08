package fyneui

import (
	"context"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// typeRune types a character into what has the keyboard, or into the window when nothing has it.
func typeRune(t *testing.T, w fyne.Window, r rune) {
	t.Helper()
	if f := w.Canvas().Focused(); f != nil {
		f.TypedRune(r)
		return
	}
	w.Canvas().OnTypedRune()(r)
}

// refsOf decodes the ref of every invocation of a command.
func refsOf(t *testing.T, invs []engine.Invocation) []model.ObjectRef {
	t.Helper()
	out := make([]model.ObjectRef, 0, len(invs))
	for _, inv := range invs {
		out = append(out, decodeParams[refParams](t, inv).Ref)
	}
	return out
}

// TestRowKeysRunTheRowsCommand: with the table's keyboard on a row, a row key runs the command the
// row menu runs (l Logs), Ctrl+D and the Delete key Delete (which asks first); a command that cannot
// run says why in a toast instead.
func TestRowKeysRunTheRowsCommand(t *testing.T) {
	f := connectedFake(t, 5)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the pods view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" })
	tv := sh.view
	w.Canvas().Focus(tv.table)
	press(t, w, fyne.KeyDown)
	row1 := f.tables["pods"].Rows[1].Ref

	typeRune(t, w, 'l')
	q.until(t, "Logs on the row", func() bool { return len(f.callsOf("pod.logs")) == 1 })
	if got := refsOf(t, f.callsOf("pod.logs")); got[0] != row1 {
		t.Fatalf("l ran Logs on %v, want %v", got[0], row1)
	}

	f.disabled["object.delete"] = "not permitted: you may not delete pods"
	tv.table.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyD, Modifier: fyne.KeyModifierControl})
	q.until(t, "Delete's reason", func() bool { return toastSays(sh, "Delete — not permitted: you may not delete pods") })
	if n := len(f.callsOf("object.delete")); n != 0 {
		t.Fatalf("a greyed Delete ran %d times", n)
	}
	delete(f.disabled, "object.delete")

	tv.table.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyD, Modifier: fyne.KeyModifierControl})
	q.until(t, "Delete asked for", func() bool { return len(f.callsOf("object.delete")) == 1 })
	q.until(t, "its confirmation", func() bool { return w.Canvas().Overlays().Top() != nil })
	sh.escape() // declines
	q.until(t, "the confirmation gone", func() bool { return w.Canvas().Overlays().Top() == nil })
	w.Canvas().Focus(tv.table)
	press(t, w, fyne.KeyDelete)
	q.until(t, "Delete again", func() bool { return len(f.callsOf("object.delete")) == 2 })
	for _, ref := range refsOf(t, f.callsOf("object.delete")) {
		if ref != row1 {
			t.Fatalf("Delete on %v, want %v", ref, row1)
		}
	}
}

// TestRowKeysOnMarkedRows: Space marks rows, ⇧↓ marks on the way, and a key whose command acts on
// a selection acts on the marked rows (Ctrl+D Delete Selected); one without (l) on the keyboard's row.
func TestRowKeysOnMarkedRows(t *testing.T) {
	f := connectedFake(t, 6)
	mustRegister(t, engine.Register(f.reg, engine.Spec[selectionRefs]{ID: "object.deleteMany", Label: "Delete", Order: engine.OrderDelete,
		RowKeys: []string{"Ctrl+D"}, Danger: engine.DangerDestructive,
		Targets: func(_ *engine.Session, p selectionRefs) ([]model.ObjectRef, error) { return p.Refs, nil },
		Run:     func(context.Context, *engine.Session, selectionRefs) (any, error) { return nil, nil }}))
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the pods view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" })
	tv := sh.view
	w.Canvas().Focus(tv.table)
	rows := f.tables["pods"].Rows

	press(t, w, fyne.KeySpace)
	q.until(t, "row 0 marked", func() bool { return len(sh.ui.Selected) == 1 })
	tv.table.KeyDown(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
	press(t, w, fyne.KeyDown)
	press(t, w, fyne.KeyDown)
	tv.table.KeyUp(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
	q.until(t, "rows 0 to 2 marked", func() bool { return len(sh.ui.Selected) == 3 })
	if tv.kbRow != 2 {
		t.Fatalf("⇧↓⇧↓ left the keyboard on row %d", tv.kbRow)
	}
	if sh.ui.Drawer != nil {
		t.Fatal("marking opened the drawer")
	}

	tv.table.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyD, Modifier: fyne.KeyModifierControl})
	q.until(t, "Delete Selected", func() bool { return len(f.callsOf("object.deleteMany")) == 1 })
	many := decodeParams[selectionRefs](t, f.callsOf("object.deleteMany")[0]).Refs // the calls are the fake's, under its lock
	if len(many) != 3 || many[0] != rows[0].Ref || many[2] != rows[2].Ref {
		t.Fatalf("Delete Selected on %v", many)
	}
	if n := len(f.callsOf("object.delete")); n != 0 {
		t.Fatalf("Delete ran on one object %d times", n)
	}
	typeRune(t, w, 'l')
	q.until(t, "Logs on the keyboard's row", func() bool { return len(f.callsOf("pod.logs")) == 1 })
	if got := refsOf(t, f.callsOf("pod.logs")); got[0] != rows[2].Ref {
		t.Fatalf("l with marks ran on %v, want the keyboard's row %v", got[0], rows[2].Ref)
	}
}

func mustRegister(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestTableMoveKeys: j and k move as ↓ and ↑, g and G, Home and End to the first and last row, the
// page keys a page; the keyboard's row follows and the ring with it.
func TestTableMoveKeys(t *testing.T) {
	f := connectedFake(t, 60)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the pods view", func() bool { return sh.view.snap != nil && len(sh.view.snap.Rows) == 60 })
	tv := sh.view
	w.Canvas().Focus(tv.table)
	for _, step := range []struct {
		do   func()
		want int
	}{
		{func() { typeRune(t, w, 'j') }, 1},
		{func() { typeRune(t, w, 'j') }, 2},
		{func() { typeRune(t, w, 'k') }, 1},
		{func() { typeRune(t, w, 'G') }, 59},
		{func() { typeRune(t, w, 'g') }, 0},
		{func() { press(t, w, fyne.KeyEnd) }, 59},
		{func() { press(t, w, fyne.KeyHome) }, 0},
		{func() { typeRune(t, w, 'k') }, 0},
	} {
		step.do()
		if tv.kbRow != step.want {
			t.Fatalf("keyboard row %d, want %d", tv.kbRow, step.want)
		}
	}
	press(t, w, fyne.KeyPageDown)
	if page := tv.kbRow; page < 2 || page >= 59 {
		t.Fatalf("PageDown moved to row %d", page)
	}
	press(t, w, fyne.KeyPageUp)
	if tv.kbRow != 0 {
		t.Fatalf("PageUp back to row %d", tv.kbRow)
	}
	if sh.ui.Drawer != nil || len(sh.ui.Selected) != 0 {
		t.Fatal("moving opened the drawer or marked a row")
	}
}

// TestSortAndFilterKeys: ⇧N sorts by name (twice: descending), ⇧O by the keyboard's column; a
// view without the column says so.
func TestSortAndFilterKeys(t *testing.T) {
	f := connectedFake(t, 5)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the pods view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" })
	tv := sh.view
	w.Canvas().Focus(tv.table)
	typeRune(t, w, 'N')
	q.until(t, "sorted by name", func() bool { return sh.ui.Sort.Column == "name" && !sh.ui.Sort.Desc })
	typeRune(t, w, 'N')
	q.until(t, "by name, descending", func() bool { return sh.ui.Sort.Column == "name" && sh.ui.Sort.Desc })
	press(t, w, fyne.KeyTab)
	vis, _ := tv.dataColumn(tv.kbCol)
	col := tv.snap.Columns[tv.cols[vis]].ID
	typeRune(t, w, 'O')
	q.until(t, "sorted by the keyboard's column", func() bool { return sh.ui.Sort.Column == col })
	typeRune(t, w, 'P')
	if tv.snap.View == "pods" && !strings.Contains(col, "namespace") {
		q.until(t, "by namespace", func() bool { return sh.ui.Sort.Column == "namespace" })
	}
}

// TestWindowKeysActOnTheDrawer: with nothing holding the keyboard and the drawer open, a row key
// acts on the drawer's object; a dialog or the palette over the window takes the keys.
func TestWindowKeysActOnTheDrawer(t *testing.T) {
	f := connectedFake(t, 5)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the pods view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" })
	ref := f.tables["pods"].Rows[3].Ref
	sh.openDetails(ref)
	q.until(t, "the drawer", func() bool { return sh.ui.Drawer != nil })
	w.Canvas().Unfocus()
	typeRune(t, w, 'l')
	q.until(t, "Logs on the drawer's object", func() bool { return len(f.callsOf("pod.logs")) == 1 })
	if got := refsOf(t, f.callsOf("pod.logs")); got[0] != ref {
		t.Fatalf("l ran on %v, want the drawer's %v", got[0], ref)
	}

	pop := widget.NewPopUp(widget.NewLabel("over the window"), w.Canvas())
	pop.Show()
	w.Canvas().Unfocus()
	typeRune(t, w, 'l')
	if n := len(f.callsOf("pod.logs")); n != 1 {
		t.Fatalf("a key under the palette ran Logs (%d calls)", n)
	}
}

// TestKeyOverview: ? opens the key overview for the place the keyboard is (a pod's keys, the
// table's and the window's), and Escape closes it.
func TestKeyOverview(t *testing.T) {
	f := connectedFake(t, 5)
	keyOverviewFake(t, f)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the pods view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" })
	w.Canvas().Focus(sh.view.table)
	typeRune(t, w, '?')
	q.until(t, "the overview", func() bool { return sh.ui.KeyOverview && sh.keys.popup != nil && sh.keys.popup.Visible() })
	q.until(t, "the pod's keys evaluated", func() bool { return overviewSays(sh, "Logs") && overviewSays(sh, "Shell") })
	for _, s := range []string{"Keys · Pods", "Mark the row", "Go to a view", "YAML", "Copy Namespace"} {
		if !overviewSays(sh, s) {
			t.Errorf("the overview lacks %q", s)
		}
	}
	sh.escape()
	q.until(t, "closed", func() bool { return !sh.ui.KeyOverview && !sh.keys.popup.Visible() })
}

// overviewSays reports whether a line of the key overview holds s.
func overviewSays(sh *shell, s string) bool {
	for _, l := range findAll[*textLine](sh.keys.body) {
		if strings.Contains(l.text, s) {
			return true
		}
	}
	return false
}

// TestRowMenuShowsRowKeys: the row menu shows each action's key after its label; a key another
// command of the kind takes is not shown.
func TestRowMenuShowsRowKeys(t *testing.T) {
	f := connectedFake(t, 3)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the pods view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" })
	ref := f.tables["pods"].Rows[0].Ref
	m := sh.rowMenu(ref, sh.evaluateActions(ref, nil))
	keys := map[string]string{}
	for _, it := range m.Items {
		if sc, ok := it.Shortcut.(*shownShortcut); ok {
			keys[it.Label] = string(sc.KeyName)
		}
	}
	if keys["Logs"] != "l" || keys["Shell"] != "s" || keys["Delete"] != "D" {
		t.Fatalf("row menu keys %v", keys)
	}
	for _, tc := range []struct{ key, want string }{{"l", "l"}, {"J", "⇧J"}} {
		if got := rowKeyText(tc.key); got != tc.want {
			t.Errorf("rowKeyText(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

// TestKeyNotations: a typed shortcut is matched in the registry's notation, Ctrl as Cmd too off
// macOS.
func TestKeyNotations(t *testing.T) {
	ctrlBack := &desktop.CustomShortcut{KeyName: fyne.KeyBackspace, Modifier: fyne.KeyModifierControl}
	if got := keyNotations(ctrlBack, "linux"); strings.Join(got, ",") != "Ctrl+Backspace,Cmd+Backspace" {
		t.Errorf("linux: %v", got)
	}
	superBack := &desktop.CustomShortcut{KeyName: fyne.KeyBackspace, Modifier: fyne.KeyModifierSuper}
	if got := keyNotations(superBack, goosMac); strings.Join(got, ",") != "Cmd+Backspace" {
		t.Errorf("darwin: %v", got)
	}
	ctrlD := &desktop.CustomShortcut{KeyName: fyne.KeyD, Modifier: fyne.KeyModifierControl}
	if got := keyNotations(ctrlD, goosMac); strings.Join(got, ",") != "Ctrl+D" {
		t.Errorf("darwin Ctrl+D: %v", got)
	}
}

// keyOverviewFake answers ui.keyOverview as the engine does.
func keyOverviewFake(t *testing.T, f *fakeEngine) {
	f.handlers["ui.keyOverview"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		p := decodeParams[struct {
			Open *bool `json:"open"`
		}](t, inv)
		f.update(func(u *engine.UIState) {
			if p.Open != nil {
				u.KeyOverview = *p.Open
			} else {
				u.KeyOverview = !u.KeyOverview
			}
		})
		return nil, nil
	}
}

func renderKeyOverview(t *testing.T, mode ThemeMode, name string) {
	f := connectedFake(t, 8)
	keyOverviewFake(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" })
	w.Canvas().Focus(sh.view.table)
	typeRune(t, w, 'j')
	typeRune(t, w, '?')
	q.until(t, "the pod's keys evaluated", func() bool { return sh.keys.popup != nil && overviewSays(sh, "Shell") })
	savePNG(t, w, name)
}

func TestRenderKeyOverviewDark(t *testing.T) { renderKeyOverview(t, ThemeDark, "keyoverview_dark.png") }
func TestRenderKeyOverviewLight(t *testing.T) {
	renderKeyOverview(t, ThemeLight, "keyoverview_light.png")
}

// TestRenderRowMenuKeys: the row menu with each action's key after its label.
func TestRenderRowMenuKeys(t *testing.T) {
	f := connectedFake(t, 8)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the pods view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" })
	tv := sh.view
	tv.openRowMenu(1, fyne.NewPos(500, 220))
	q.until(t, "the row menu", func() bool { return tv.menu != nil && tv.menu.Visible() })
	savePNG(t, w, "rowmenu_keys_dark.png")
}

// TestXShowsAllOfASecret: x on a Secret's row opens its drawer with every value shown (Show all),
// and x again in the drawer hides them.
func TestXShowsAllOfASecret(t *testing.T) {
	f, ref := secretsFake(t)
	f.ui.Selected, f.ui.Drawer = nil, nil
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the Secrets", func() bool { return sh.view.snap != nil && sh.view.snap.View == "secrets" })
	tv := sh.view
	w.Canvas().Focus(tv.table)
	tv.moveRing(tv.snap.IndexOf(ref))
	typeRune(t, w, 'x')
	q.until(t, "the drawer with every value", func() bool {
		return sh.ui.Drawer != nil && sh.ui.Drawer.Same(ref) && allShown(sh)
	})
	w.Canvas().Unfocus()
	typeRune(t, w, 'x')
	q.until(t, "every value hidden", func() bool { return noneShown(sh) })
}

// TestRowKeysStayOutOfTheLogs: with a logs tab's lines holding the keyboard, a row key the lines
// pass on acts on no row; ? opens the overview with the logs tab's keys.
func TestRowKeysStayOutOfTheLogs(t *testing.T) {
	sh, w, q, f := logsScene(t, ThemeDark, true, engine.LogOptions{ResourceNames: true})
	keyOverviewFake(t, f)
	lt := logTabOf(sh)
	c := w.Canvas()
	q.until(t, "every line", func() bool { return len(lt.view.ShownLines()) == len(demoMerged) })
	c.Focus(lt.view.FocusTarget())
	logs := len(f.callsOf("pod.logs"))
	typeFocused(c, "l")
	logBarrier(t, sh, q)
	if n := len(f.callsOf("pod.logs")); n != logs || c.Focused() != lt.view.FocusTarget() {
		t.Fatalf("l in the logs ran Logs on a row (%d calls) or took the keyboard (%T)", n-logs, c.Focused())
	}
	typeFocused(c, "?")
	q.until(t, "the overview", func() bool { return sh.ui.KeyOverview && sh.keys.popup != nil && sh.keys.popup.Visible() })
	if !overviewSays(sh, "Keys · Logs") || !overviewSays(sh, "Timestamps") {
		t.Fatal("the overview does not list the logs tab's keys")
	}
}

// TestColonOpensTheJump: : in the list opens the palette with ":" typed, its field taking the
// keys, as k9s's command line.
func TestColonOpensTheJump(t *testing.T) {
	sh, w, q, _ := jumpShell(t, ThemeDark)
	q.until(t, "the list", func() bool { return sh.view.snap != nil })
	w.Canvas().Focus(sh.view.table)
	typeRune(t, w, ':')
	p := sh.palette
	q.until(t, "the palette with a colon", func() bool { return p.visible() && p.filter.Text == ":" })
	if w.Canvas().Focused() != p.filter {
		t.Fatalf("focus on %T, want the palette's field", w.Canvas().Focused())
	}
}
