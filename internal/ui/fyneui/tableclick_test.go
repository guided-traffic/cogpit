package fyneui

import (
	"runtime"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A click takes ⌘ and ⇧ from its own mouse event, never from the desktop driver's memory of the
// last key event, which stays "held" after a ⌘ that went down outside the window (G16, the owner,
// 2026-10-19: every click on a pod only toggled its checkbox until he pressed ⌘ once). heldKeys
// stands for that memory, clickWith for a click as Fyne's GLFW driver delivers it.

// TestClickTakesItsOwnKeys: with ⌘ or ⇧ stale in the driver, a click without them opens its row's
// drawer and checks nothing; with ⌘ or ⇧ in the click and none in the driver, the click toggles its
// row or adds a range, the drawer staying where it is; a press's keys belong to its own click.
func TestClickTakesItsOwnKeys(t *testing.T) {
	f := connectedFake(t, 50)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	settle(w)
	rows := f.tables["pods"].Rows
	drawerOn := func(row int) func() bool {
		return func() bool { return sh.ui.Drawer != nil && *sh.ui.Drawer == rows[row].Ref }
	}

	// The driver believes ⌘ is held; the click carries no key: the drawer, nothing checked.
	heldKeys(t, fyne.KeyModifierShortcutDefault)
	clickWith(t, cellAt(t, sh, 3, firstDataColumn+1), 0)
	q.until(t, "the drawer on row 3 despite a stale ⌘", drawerOn(3))
	if p := lastSelect(t, f); p.Mode != "open" || p.NoDrawer || len(p.Refs) != 1 || p.Refs[0] != rows[3].Ref {
		t.Fatalf("a plain click under a stale ⌘ sent %+v", p)
	}
	if len(sh.ui.Selected) != 0 {
		t.Fatalf("a plain click under a stale ⌘ checked %v", sh.ui.Selected)
	}

	// A stale ⇧ likewise: the drawer moves, no range is added.
	heldKeys(t, fyne.KeyModifierShift)
	clickWith(t, cellAt(t, sh, 6, firstDataColumn+1), 0)
	q.until(t, "the drawer on row 6 despite a stale ⇧", drawerOn(6))
	if p := lastSelect(t, f); p.Mode != "open" || len(p.Refs) != 1 || p.Refs[0] != rows[6].Ref {
		t.Fatalf("a plain click under a stale ⇧ sent %+v", p)
	}
	if len(sh.ui.Selected) != 0 {
		t.Fatalf("a plain click under a stale ⇧ checked %v", sh.ui.Selected)
	}

	// The inverse: no key in the driver, ⌘ in the click: row 8 toggled, the drawer stays on row 6.
	heldKeys(t, 0)
	clickWith(t, cellAt(t, sh, 8, firstDataColumn+1), mouseShortcut)
	q.until(t, "row 8 checked by ⌘ in the click", func() bool { return len(sh.ui.Selected) == 1 })
	if p := lastSelect(t, f); p.Mode != "toggle" || !p.NoDrawer || len(p.Refs) != 1 || p.Refs[0] != rows[8].Ref {
		t.Fatalf("⌘ in the click sent %+v", p)
	}
	if !drawerOn(6)() {
		t.Fatalf("⌘-click moved the drawer to %v", sh.ui.Drawer)
	}

	// ⇧ in the click: the rows from the anchor (row 8) to row 10 added.
	clickWith(t, cellAt(t, sh, 10, firstDataColumn+1), fyne.KeyModifierShift)
	q.until(t, "rows 8 to 10 checked by ⇧ in the click", func() bool { return len(sh.ui.Selected) == 3 })
	want := []model.ObjectRef{rows[8].Ref, rows[9].Ref, rows[10].Ref}
	if p := lastSelect(t, f); p.Mode != "add" || !slices.Equal(refKeys(p.Refs), refKeys(want)) {
		t.Fatalf("⇧ in the click sent %+v", p)
	}

	// A press's keys are its own click's: a tap after a ⌘-click, without a press of its own, is a
	// plain click.
	clickWith(t, cellAt(t, sh, 12, firstDataColumn+1), mouseShortcut)
	q.until(t, "row 12 checked", func() bool { return len(sh.ui.Selected) == 4 })
	test.Tap(cellAt(t, sh, 13, firstDataColumn+1))
	q.until(t, "the drawer on row 13", drawerOn(13))
	if len(sh.ui.Selected) != 4 {
		t.Fatalf("a tap after a ⌘-click changed the checked rows to %v", sh.ui.Selected)
	}
}

// TestCheckboxClickTakesItsOwnShift: a click on a checkbox adds a range only with ⇧ in the click,
// whatever the driver believes; Space on a checkbox with the keyboard takes ⇧ from its key event,
// which the driver's modifier keys are while the key is handled.
func TestCheckboxClickTakesItsOwnShift(t *testing.T) {
	f := connectedFake(t, 50)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	settle(w)
	rows := f.tables["pods"].Rows
	refs := func(from, to int) []string {
		var out []model.ObjectRef
		for i := from; i <= to; i++ {
			out = append(out, rows[i].Ref)
		}
		return refKeys(out)
	}

	clickWith(t, cellAt(t, sh, 2, checkColumn), 0)
	q.until(t, "row 2 checked", func() bool { return len(sh.ui.Selected) == 1 })

	// A stale ⇧ in the driver, none in the click: row 5 alone.
	heldKeys(t, fyne.KeyModifierShift)
	clickWith(t, cellAt(t, sh, 5, checkColumn), 0)
	q.until(t, "row 5 checked", func() bool { return len(sh.ui.Selected) == 2 })
	if p := lastSelect(t, f); p.Mode != "toggle" || !slices.Equal(refKeys(p.Refs), refs(5, 5)) {
		t.Fatalf("a checkbox clicked under a stale ⇧ sent %+v", p)
	}

	// ⇧ in the click, none in the driver: the rows from the anchor (row 5) to row 8.
	heldKeys(t, 0)
	clickWith(t, cellAt(t, sh, 8, checkColumn), fyne.KeyModifierShift)
	q.until(t, "rows 5 to 8 checked", func() bool { return len(sh.ui.Selected) == 5 })
	if p := lastSelect(t, f); p.Mode != "add" || !slices.Equal(refKeys(p.Refs), refs(5, 8)) {
		t.Fatalf("⇧ in the checkbox's click sent %+v", p)
	}

	// ⇧-Space with the keyboard on row 10's checkbox: the key's ⇧, the rows from the anchor (row 5,
	// which a range leaves where it is) to row 10.
	heldKeys(t, fyne.KeyModifierShift)
	sh.view.table.Select(widget.TableCellID{Row: 10, Col: checkColumn}) // what Space does
	q.until(t, "rows 5 to 10 checked", func() bool { return len(sh.ui.Selected) == 7 })
	if p := lastSelect(t, f); p.Mode != "add" || !slices.Equal(refKeys(p.Refs), refs(5, 10)) {
		t.Fatalf("⇧-Space on a checkbox sent %+v", p)
	}
}

// TestClickModifiers: ⌘ arrives in Fyne's mouse events on macOS as Control and is the shortcut key
// there, Ctrl elsewhere is; only the primary button's press keeps its keys.
func TestClickModifiers(t *testing.T) {
	for _, m := range []struct{ in, want fyne.KeyModifier }{
		{0, 0},
		{fyne.KeyModifierShift, fyne.KeyModifierShift},
		{mouseShortcut, fyne.KeyModifierShortcutDefault},
		{mouseShortcut | fyne.KeyModifierShift, fyne.KeyModifierShortcutDefault | fyne.KeyModifierShift},
		{fyne.KeyModifierAlt, fyne.KeyModifierAlt},
	} {
		if got := clickModifiers(m.in); got != m.want {
			t.Errorf("clickModifiers(%v) = %v, want %v", m.in, got, m.want)
		}
	}
	if runtime.GOOS != goosMac && clickModifiers(fyne.KeyModifierSuper) != fyne.KeyModifierSuper {
		t.Error("off macOS the Super key is no shortcut key in a click")
	}

	c := &tableCell{}
	c.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonSecondary, Modifier: fyne.KeyModifierShift})
	if c.pressMods != 0 {
		t.Errorf("a secondary press kept %v", c.pressMods)
	}
	c.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary, Modifier: mouseShortcut})
	if c.pressMods != fyne.KeyModifierShortcutDefault {
		t.Errorf("a ⌘ press kept %v, want %v", c.pressMods, fyne.KeyModifierShortcutDefault)
	}
}
