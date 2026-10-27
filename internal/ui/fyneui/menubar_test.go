package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// menuBarScene is a connected cluster in a window whose menus are in the top bar, as on Windows and
// Linux (menubar.go).
func menuBarScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue) {
	t.Helper()
	f := connectedFake(t, 1)
	sh, w, q := newTestShell(t, f, Options{Theme: mode, menus: menusInBar})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	w.Resize(fyne.NewSize(1440, 900))
	settle(w)
	return sh, w, q
}

// barMenuOpen is the menu open in the window: its layer, the top overlay; nil while none is open.
func barMenuOpen(w fyne.Window) *menuOverlay {
	o, _ := w.Canvas().Overlays().Top().(*menuOverlay)
	return o
}

// TestMenusInTheTopBar: on Windows and Linux the menus' titles are the top bar's first objects,
// before the logo, in the menus' order; the window keeps a main menu with the same menus for their
// keys, Quit at the end of the first, and a rebuild refills that same main menu.
func TestMenusInTheTopBar(t *testing.T) {
	sh, w, _ := menuBarScene(t, ThemeDark)
	b := sh.top.menus
	if b == nil || sh.top.left.Objects[0] != b.root {
		t.Fatal("the menus are not the top bar's first objects")
	}
	want := sh.mainMenu()
	if len(b.titles) != len(want.Items) {
		t.Fatalf("%d titles, want %d", len(b.titles), len(want.Items))
	}
	for i, m := range want.Items {
		if b.titles[i].label != m.Label {
			t.Errorf("title %d %q, want %q", i, b.titles[i].label, m.Label)
		}
	}
	keys := w.MainMenu()
	if keys == nil || keys != sh.menuKeys || len(keys.Items) != len(want.Items) {
		t.Fatalf("the window's main menu %+v, want the menus for their keys", keys)
	}
	first := keys.Items[0].Items
	if last := first[len(first)-1]; last.Label != quitLabel || last.Action == nil {
		t.Errorf("the first menu ends with %q, want %s", last.Label, quitLabel)
	}
	sh.setMainMenu()
	if w.MainMenu() != keys || len(keys.Items) != len(want.Items) {
		t.Error("a rebuild gave the window another main menu, or emptied it")
	}
}

// TestMenuOpensUnderItsTitle: a title's click opens its menu under it, below the bar; the pointer on
// another title opens that one; → and ← go to the neighbouring menus, round; Escape, a click outside
// and a second click on its title close it.
func TestMenuOpensUnderItsTitle(t *testing.T) {
	sh, w, _ := menuBarScene(t, ThemeDark)
	b := sh.top.menus
	n := len(b.titles)
	test.Tap(b.titles[1])
	o := barMenuOpen(w)
	if o == nil || o.index != 1 || !b.titles[1].opened {
		t.Fatalf("after the click on %q: %+v", b.titles[1].label, o)
	}
	d := fyne.CurrentApp().Driver()
	title, bar := d.AbsolutePositionForObject(b.titles[1]), d.AbsolutePositionForObject(sh.top.root)
	if got := o.menu.Position(); got.X != title.X || got.Y != bar.Y+sh.top.root.Size().Height {
		t.Errorf("menu at %v, want under its title at x %v, below the bar at y %v", got, title.X, bar.Y+sh.top.root.Size().Height)
	}

	o.zones[0].MouseIn(nil)
	if o = barMenuOpen(w); o == nil || o.index != 0 || b.titles[1].opened || !b.titles[0].opened {
		t.Fatalf("the pointer on %q: %+v", b.titles[0].label, o)
	}
	o.TypedKey(&fyne.KeyEvent{Name: fyne.KeyLeft})
	if o = barMenuOpen(w); o == nil || o.index != n-1 {
		t.Fatalf("← from the first menu: %+v, want the last", o)
	}
	o.TypedKey(&fyne.KeyEvent{Name: fyne.KeyRight})
	if o = barMenuOpen(w); o == nil || o.index != 0 {
		t.Fatalf("→ from the last menu: %+v, want the first", o)
	}
	o.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if barMenuOpen(w) != nil || b.open != nil || b.titles[0].opened {
		t.Fatal("Escape left the menu open")
	}

	test.Tap(b.titles[2])
	test.Tap(barMenuOpen(w).under)
	if barMenuOpen(w) != nil {
		t.Fatal("a click outside left the menu open")
	}
	test.Tap(b.titles[2])
	test.Tap(barMenuOpen(w).zones[2])
	if barMenuOpen(w) != nil {
		t.Fatal("a second click on its title left the menu open")
	}
}

// TestMenuItemRunsAndCloses: an item chosen with the keys runs and closes the menu.
func TestMenuItemRunsAndCloses(t *testing.T) {
	sh, w, _ := menuBarScene(t, ThemeDark)
	ran := false
	b := sh.top.menus
	b.set(fyne.NewMainMenu(fyne.NewMenu("Test", fyne.NewMenuItem("Run", func() { ran = true }))))
	test.Tap(b.titles[0])
	o := barMenuOpen(w)
	o.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
	o.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if !ran || barMenuOpen(w) != nil {
		t.Errorf("ran %v, menu still open %v", ran, barMenuOpen(w) != nil)
	}
}

// TestMenuKeysWaitForTheWindow: before the window exists (fyneui.Run before ShowAndRun) the menus
// are the bar's alone; the window gets them for their keys once it started.
func TestMenuKeysWaitForTheWindow(t *testing.T) {
	sh, w, _ := menuBarScene(t, ThemeDark)
	sh.menuKeys = nil
	w.SetMainMenu(nil)
	sh.deferMenuKeys = true
	sh.setMainMenu()
	if w.MainMenu() != nil || sh.menuKeys != nil {
		t.Fatal("the menus' keys were installed before the window started")
	}
	sh.installMenuKeys()
	if m := w.MainMenu(); m == nil || m != sh.menuKeys || len(m.Items) != len(sh.top.menus.titles) {
		t.Fatalf("after the start: %+v", m)
	}
}

// The top bar with the View menu open under its title, the menus before the logo (Windows and
// Linux; the system's title bar above it is not Cogpit's).
func TestRenderMenuBarDark(t *testing.T) { renderMenuBar(t, ThemeDark, "menubar_dark.png") }
func renderMenuBar(t *testing.T, mode ThemeMode, name string) {
	sh, w, _ := menuBarScene(t, mode)
	test.Tap(sh.top.menus.titles[1])
	if barMenuOpen(w) == nil {
		t.Fatal("the View menu did not open")
	}
	savePNG(t, w, name)
}
