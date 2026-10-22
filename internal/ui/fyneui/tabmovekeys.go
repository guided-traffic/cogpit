package fyneui

import (
	"slices"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Move Tab Left and Right by their keys (⌘⌥⇧←, ⌘⌥⇧→, View › Move Tab Left and Right; the owner's
// alpha walk, 2026-10-22, AW10: the keys "do nothing in the dock") move the active tab of the bar
// the keyboard works in: ui.tab.moveLeft and ui.tab.moveRight with dock for the dock's
// (engine/tabmove.go), without it for the tab bar's, as before. The lead's decisions, open to
// revision (D-106):
//   - the dock's while the focused widget lies in the dock: its tab's terminal, editor, log or a
//     find bar's field, or the strip's switch. A click on a dock tab gives the tab's widget the keys
//     (D-115, dockfocus.go), so after a click on a tab with one the keys are the dock's this way, and
//     switching the tab bar's tabs by the keys (⌘⌥→) leaves them there: the focus decides first;
//   - with nothing focused, the bar whose tab was clicked last; the tab bar's once its tabs were
//     switched by the keys (⌘⌥→, ⌘⌥←, ⌘1–⌘9). Nothing is focused after a click on a dock tab that
//     takes no keys (the Audit Log, the Diagnostics, a job's progress, an Edit tab's review) or not
//     yet (a session or a stream still opening), on a tab of the tab bar, a button or a list's empty
//     area (a click on what takes no keys takes them away in Fyne);
//   - else, with the keys anywhere outside the dock, the tab bar's.
//
// The keys reach the menu item whatever has the focus, a terminal included: the items are main-menu
// items, which AppKit runs for their key equivalents before the window sees the key on macOS, and
// which Fyne looks a typed shortcut up in before the focused widget is offered it elsewhere (Fyne
// 2.8's glfw window, read in its source; not tried in a real window).

// tabStepParams mirror ui.tab.moveLeft's and ui.tab.moveRight's parameters.
type tabStepParams struct {
	Dock bool `json:"dock,omitempty"`
}

// stepTab is Move Tab Left (d < 0) or Right: the active tab of the bar the keyboard works in
// (keysInDock) moves one place, only where it has a place to go.
func (sh *shell) stepTab(id string, d int) {
	if sh.keysInDock() {
		tabs := sh.ui.DockInScope()
		i := slices.IndexFunc(tabs, func(t engine.DockTab) bool { return t.ID == sh.ui.ActiveDock })
		if i >= 0 && i+d >= 0 && i+d < len(tabs) {
			sh.dispatch(id, tabStepParams{Dock: true}, nil)
		}
		return
	}
	tabs := sh.ui.TabsShown()
	i := slices.IndexFunc(tabs, func(t engine.ViewTab) bool { return t.ID == sh.ui.ActiveTab })
	if i >= 0 && i+d >= 0 && i+d < len(tabs) {
		sh.dispatch(id, tabStepParams{}, nil)
	}
}

// keysInDock reports whether the keyboard works in the dock: the focused widget lies in it, or
// nothing is focused and a dock tab was clicked after the tab bar's last click or key.
func (sh *shell) keysInDock() bool {
	if f := sh.win.Canvas().Focused(); f != nil {
		return sh.dock.holdsKeys(f)
	}
	return sh.dock.keysLast
}

// holdsKeys reports whether a focused widget lies in the dock: in its open panel, or in the strip of
// the collapsed dock.
func (d *dock) holdsKeys(focused fyne.Focusable) bool {
	o, ok := focused.(fyne.CanvasObject)
	if !ok || !o.Visible() {
		return false
	}
	area := fyne.CanvasObject(d.collapsedBar)
	if d.open() {
		area = d.panel
	}
	if !area.Visible() {
		return false
	}
	drv := fyne.CurrentApp().Driver()
	at, size, p := drv.AbsolutePositionForObject(area), area.Size(), drv.AbsolutePositionForObject(o)
	return p.X >= at.X && p.Y >= at.Y && p.X < at.X+size.Width && p.Y < at.Y+size.Height
}
