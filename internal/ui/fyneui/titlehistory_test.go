package fyneui

import (
	"runtime"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The arrows follow the active tab's history: the demo's preview tab has Deployments before it, so
// Back is enabled and Forward not; a click dispatches ui.tab.back, a disabled arrow dispatches
// nothing; the tooltips name the commands and their shortcuts. Another tab's history, a tab of a
// cluster that is not connected, or no tab at all leave both disabled.
func TestTitleHistoryArrows(t *testing.T) {
	sh, w, q, f := tabsShell(t, ThemeDark)
	h := sh.top.history
	if !h.back.Visible() || !h.forward.Visible() {
		t.Fatal("the arrows are hidden")
	}
	if h.back.Disabled() || !h.forward.Disabled() {
		t.Fatalf("back disabled %v, forward disabled %v; the tab has one view behind it", h.back.Disabled(), h.forward.Disabled())
	}
	wantBack, wantForward := "Go Back (Ctrl+-)", "Go Forward (Ctrl+Shift+-)"
	if runtime.GOOS == "darwin" {
		wantBack, wantForward = "Go Back (⌃-)", "Go Forward (⌃⇧-)"
	}
	if h.back.tip != wantBack || h.forward.tip != wantForward {
		t.Errorf("tooltips %q and %q", h.back.tip, h.forward.tip)
	}

	test.Tap(h.forward)
	test.Tap(h.back)
	q.until(t, "ui.tab.back", func() bool { return len(f.callsOf("ui.tab.back")) == 1 })
	if n := len(f.callsOf("ui.tab.forward")); n != 0 {
		t.Errorf("the disabled forward arrow dispatched %d times", n)
	}
	if inv := f.callsOf("ui.tab.back")[0]; inv.Origin != engine.OriginGUI {
		t.Errorf("dispatched by %s", inv.Origin)
	}

	// The engine went back: the tab now has a view after it and none before.
	f.update(func(u *engine.UIState) {
		u.Tabs[1].View, u.Tabs[1].Back, u.Tabs[1].Forward, u.View = "deployments", nil, []string{"pods"}, "deployments"
	})
	q.until(t, "forward only", func() bool { return h.back.Disabled() && !h.forward.Disabled() })
	f.update(func(u *engine.UIState) { u.ActiveTab = "tab-1" }) // Services: no history
	q.until(t, "no history", func() bool { return h.back.Disabled() && h.forward.Disabled() })
	f.update(func(u *engine.UIState) { u.ActiveTab, u.Tabs[2].Back = "tab-3", []string{"pods"} }) // another cluster's tab
	q.until(t, "the other cluster's tab", func() bool { return sh.ui.ActiveTab == "tab-3" })
	if !h.back.Disabled() {
		t.Error("Back is enabled on a tab of a cluster that is not connected")
	}
	f.update(func(u *engine.UIState) { u.Tabs, u.ActiveTab = nil, "" })
	q.until(t, "no tabs", func() bool { return len(sh.ui.Tabs) == 0 })
	if !h.back.Disabled() || !h.forward.Disabled() {
		t.Error("an arrow is enabled without a tab")
	}

	// The tooltip shows on a disabled arrow too.
	sh.tip.delay = time.Millisecond
	settle(w)
	test.MoveMouse(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(h.back).AddXY(6, 6))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() == wantBack })
}

// Without the tab commands (an engine that does not offer them) the arrows are hidden.
func TestTitleHistoryHiddenWithoutCommands(t *testing.T) {
	sh, _, _ := newTestShell(t, connectedFake(t, 3), Options{})
	if sh.top.history.back.Visible() || sh.top.history.forward.Visible() {
		t.Error("arrows shown without ui.tab.back and ui.tab.forward")
	}
}

// clickTarget is the object a click at pos (window coordinates) reaches, found as Fyne's driver
// finds it (internal/driver.FindObjectAtPositionMatching): the last visible object under the
// pointer, in drawing order, that takes a tap, a secondary tap, a double tap, the focus or the
// mouse button.
func clickTarget(root fyne.CanvasObject, pos fyne.Position) fyne.CanvasObject {
	var found fyne.CanvasObject
	var walk func(o fyne.CanvasObject, origin fyne.Position)
	walk = func(o fyne.CanvasObject, origin fyne.Position) {
		if o == nil || !o.Visible() {
			return
		}
		at := origin.Add(o.Position())
		if pos.X >= at.X && pos.Y >= at.Y && pos.X < at.X+o.Size().Width && pos.Y < at.Y+o.Size().Height {
			switch o.(type) {
			case fyne.Tappable, fyne.SecondaryTappable, fyne.DoubleTappable, fyne.Focusable, desktop.Mouseable:
				found = o
			}
		}
		switch x := o.(type) {
		case *fyne.Container:
			for _, c := range x.Objects {
				walk(c, at)
			}
		case fyne.Widget:
			for _, c := range test.WidgetRenderer(x).Objects() {
				walk(c, at)
			}
		}
	}
	walk(root, fyne.Position{})
	return found
}

// centre is the middle of an object, in window coordinates.
func centre(o fyne.CanvasObject) fyne.Position {
	return fyne.CurrentApp().Driver().AbsolutePositionForObject(o).AddXY(o.Size().Width/2, o.Size().Height/2)
}

// TestBarClicksReachTheButtons: on macOS a mouse-down that reaches the bar's drag area drags the
// window, so every button on the bar must take the click itself: the arrows (enabled or not) and
// the bell (Switch context went, D-57; the cluster and its dot went, D-54: the connection log is
// the status bar's dot's, conndot_test.go); the bar's text and its empty stretch reach the drag
// area, whose right-click opens the bar's context menu: Preferences…, which opens the
// Preferences page.
func TestBarClicksReachTheButtons(t *testing.T) {
	f := tabsFake(t)
	probe[preferencesOpenParams](t, f, mustCommand(t, "preferences.open"))
	probe[struct{}](t, f, mustCommand(t, "notifications.list")) // the bell shows with the log's commands
	sh, w, q := newTestShell(t, f, Options{MCPCommand: "claude mcp add cogpit -- cogpit-cli mcp"})
	q.until(t, "the shell", func() bool { return len(sh.kinds) > 0 && len(sh.tabs.shown) == 3 })
	settle(w)
	root := w.Canvas().Content()
	top := sh.top
	if !top.notes.bell.Visible() {
		t.Fatal("no bell")
	}
	for name, o := range map[string]fyne.CanvasObject{
		"back": top.history.back, "forward (disabled)": top.history.forward, "bell": top.notes.bell,
	} {
		if got := clickTarget(root, centre(o)); got != o {
			t.Errorf("a click on %s reaches %T", name, got)
		}
	}
	drag := findAll[*barDragArea](top.root)[0]
	for _, l := range findAll[*textLine](top.left) { // the wordmark
		if got := clickTarget(root, centre(l)); got != drag {
			t.Errorf("a click on the bar's text %q reaches %T, not the drag area", l.text, got)
		}
	}
	empty := fyne.CurrentApp().Driver().AbsolutePositionForObject(top.left).AddXY(top.left.Size().Width+40, 10) // between the left and the right
	if got := clickTarget(root, empty); got != drag {
		t.Errorf("a click on the bar's empty stretch reaches %T", got)
	}

	drag.TappedSecondary(&fyne.PointEvent{AbsolutePosition: empty})
	m := top.history.menu
	if m == nil {
		t.Fatal("no context menu")
	}
	if got := menuLabels(m); !slices.Equal(got, []string{"Preferences…"}) {
		t.Fatalf("context menu %q", got)
	}
	if sc, ok := m.Items[0].Shortcut.(*desktop.CustomShortcut); !ok || sc.KeyName != fyne.KeyComma {
		t.Errorf("Preferences… shows shortcut %v", m.Items[0].Shortcut)
	}
	if len(findAll[*widget.PopUpMenu](w.Canvas().Overlays().Top())) != 1 {
		t.Errorf("the menu is not shown: the top overlay is %T", w.Canvas().Overlays().Top())
	}
	m.Items[0].Action()
	q.until(t, "preferences.open", func() bool { return len(f.callsOf("preferences.open")) == 1 })
}

// mustCommand returns a real command from the registry.
func mustCommand(t testing.TB, id string) *engine.Command {
	t.Helper()
	c, ok := realCommands(t).Get(id)
	if !ok {
		t.Fatalf("the registry has no %s", id)
	}
	return c
}
