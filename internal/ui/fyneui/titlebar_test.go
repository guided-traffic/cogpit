package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
)

// The title bar's logic, without a window: the AppKit side (titlebar_darwin.m) cannot run
// offscreen, so what it feeds is tested here, and the rest needs a look at the running app.

func TestDoubleClickAction(t *testing.T) {
	for _, c := range []struct {
		action                    string
		legacySet, legacyMinimize bool
		want                      titlebarAction
	}{
		{"Maximize", false, false, titlebarZoom},
		{"Minimize", false, false, titlebarMinimize},
		{"None", false, false, titlebarNone},
		{"Fill", false, false, titlebarZoom},            // tiling systems; no public AppKit call fills
		{"Minimize", true, false, titlebarMinimize},     // the newer key wins
		{"", true, true, titlebarMinimize},              // only the older key, on
		{"", true, false, titlebarZoom},                 // only the older key, off
		{"", false, false, titlebarZoom},                // unset: a new Mac zooms
		{"Something new", false, false, titlebarZoom},   // a value Cogpit does not know
		{"Something new", true, true, titlebarMinimize}, // falls back to the older key
	} {
		if got := doubleClickAction(c.action, c.legacySet, c.legacyMinimize); got != c.want {
			t.Errorf("doubleClickAction(%q, %v, %v) = %v, want %v", c.action, c.legacySet, c.legacyMinimize, got, c.want)
		}
	}
}

func TestTitlebarLayout(t *testing.T) {
	// A standard window on macOS: close at 7.5 from the edge, zoom ending at 66.5, 28 high.
	g := titlebarGeometry{ButtonsMinX: 7.5, ButtonsMaxX: 66.5, Height: 28}
	lead, height := titlebarLayout(g, 1, 8, 4)
	if lead != 62 || height != 28 { // 66.5 + 7.5 room, less the bar's padding 8 and the box's gap 4
		t.Fatalf("lead %v, height %v", lead, height)
	}
	if lead, height := titlebarLayout(g, 2, 8, 4); lead != 25 || height != 14 { // FYNE_SCALE=2: half the units
		t.Fatalf("at scale 2: lead %v, height %v", lead, height)
	}
	g.FullScreen = true
	if lead, height := titlebarLayout(g, 1, 8, 4); lead != 0 || height != 0 {
		t.Fatalf("full screen: lead %v, height %v", lead, height)
	}
	if lead, height := titlebarLayout(titlebarGeometry{}, 1, 8, 4); lead != 0 || height != 0 {
		t.Fatalf("no buttons: lead %v, height %v", lead, height)
	}
	if lead, _ := titlebarLayout(titlebarGeometry{ButtonsMaxX: 5, Height: 28}, 1, 8, 4); lead != 0 {
		t.Fatalf("buttons narrower than the padding: lead %v", lead)
	}
}

// TestTopBarMakesRoomForWindowButtons: with the title bar's geometry the logo moves right of the
// window buttons' room and the bar grows to a taller title bar; full screen gives the room back.
func TestTopBarMakesRoomForWindowButtons(t *testing.T) {
	f := connectedFake(t, 1)
	sh, w, q := newTestShell(t, f, Options{menus: menusNative}) // macOS's bar: its menus are the system's
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	logoX := func() float32 {
		settle(w)
		for _, o := range sh.top.left.Objects {
			if _, ok := o.(*logo); ok {
				return fyne.CurrentApp().Driver().AbsolutePositionForObject(o).X
			}
		}
		t.Fatal("no logo in the top bar")
		return 0
	}
	before, barH := logoX(), sh.top.root.Size().Height

	sh.applyTitlebar(titlebarGeometry{ButtonsMinX: 7.5, ButtonsMaxX: 66.5, Height: 28})
	if got := logoX(); got < 74 || got > 75 {
		t.Fatalf("logo at %v, want right of the buttons' room (74: 66.5 + 7.5)", got)
	}
	if h := sh.top.root.Size().Height; h != barH {
		t.Fatalf("a 28-point title bar changed the %v-high bar to %v", barH, h)
	}
	sh.applyTitlebar(titlebarGeometry{ButtonsMinX: 7.5, ButtonsMaxX: 66.5, Height: 52})
	if h := sh.top.root.Size().Height; h != 52 {
		t.Fatalf("bar %v high under a 52-point title bar", h)
	}
	sh.applyTitlebar(titlebarGeometry{ButtonsMinX: 7.5, ButtonsMaxX: 66.5, Height: 28, FullScreen: true})
	if got := logoX(); got != before || sh.top.lead.Visible() {
		t.Fatalf("full screen: logo at %v, want %v", got, before)
	}
	if h := sh.top.root.Size().Height; h != barH {
		t.Fatalf("full screen: bar %v high, want %v", h, barH)
	}
}

// AppKit reports every step of a live resize; the top bar refreshes the window's content only
// when the buttons' room or the title bar's height changed, not for each step (the text flickered
// while the window was dragged, 2026-10-13).
func TestTopBarRefreshesOnlyOnChange(t *testing.T) {
	f := connectedFake(t, 1)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	if !sh.top.setTitlebar(62, 28) {
		t.Fatal("the first room was not applied")
	}
	for i := range 3 {
		if sh.top.setTitlebar(62, 28) {
			t.Fatalf("step %d of a resize with the same room refreshed the window", i)
		}
	}
	if !sh.top.setTitlebar(62, 52) || !sh.top.setTitlebar(0, 0) {
		t.Fatal("a changed height or the room given back was not applied")
	}
	if sh.top.setTitlebar(0, 0) {
		t.Fatal("full screen, reported again, refreshed the window")
	}
}

// TestBarDragArea: the top bar has a drag area under its contents, as large as the bar; a
// primary mouse-down on it is the title bar's, other buttons are not.
func TestBarDragArea(t *testing.T) {
	f := connectedFake(t, 1)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	settle(w)
	areas := findAll[*barDragArea](sh.top.root)
	if len(areas) != 1 || areas[0].Size() != sh.top.root.Size() {
		t.Fatalf("drag areas %d, size %v of the bar's %v", len(areas), areas[0].Size(), sh.top.root.Size())
	}
	downs := 0
	a := newBarDragArea(func() { downs++ })
	a.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonSecondary})
	a.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	a.MouseUp(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	if downs != 1 {
		t.Fatalf("%d title bar mouse-downs, want the primary one", downs)
	}
	// Without the AppKit bridge (the test window has no NSWindow) a mouse-down does nothing.
	areas[0].MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	if th := sh.app.Settings().Theme(); sh.top.root.MinSize().Height != th.Size(sizeBarHeight) || th.Size(theme.SizeNamePadding) == 0 {
		t.Fatalf("bar min height %v", sh.top.root.MinSize().Height)
	}
}

// renderTitlebarRoom shows the top bar as on macOS with Cogpit's title bar: the room the window
// buttons take (AppKit draws them; offscreen it stays empty) before the logo.
func renderTitlebarRoom(t *testing.T, mode ThemeMode, name string) {
	f := connectedFake(t, 200)
	sh, w, q := newTestShell(t, f, Options{Theme: mode, MCPCommand: "claude mcp add cogpit -- cogpit-cli mcp"})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.diag.total > 0 })
	sh.applyTitlebar(titlebarGeometry{ButtonsMinX: 7.5, ButtonsMaxX: 66.5, Height: 28})
	savePNG(t, w, name)
}

func TestRenderTitlebarRoomDark(t *testing.T) {
	renderTitlebarRoom(t, ThemeDark, "titlebar_room_dark.png")
}
func TestRenderTitlebarRoomLight(t *testing.T) {
	renderTitlebarRoom(t, ThemeLight, "titlebar_room_light.png")
}
