package fyneui

import (
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The title bar's history arrows and context menu (ui-parity › Layout, "Title bar with history
// back and forward, notifications and a context menu with Preferences"; captured 2026-10-11: back
// and forward arrows at the bar's left). The history is the active tab's (UIState.Tabs, Back and
// Forward; engine/tabs.go): an arrow is enabled while the tab has somewhere to go, and a click
// dispatches ui.tab.back or ui.tab.forward, as ⌃- and ⌃⇧- do. Cogpit's logo stays beside the
// window buttons (the owner's requirement), so the arrows follow it, where Lens has them after the
// window buttons. A right-click on the bar where no button is opens its context menu: Preferences…
// (the row's text; which other items Lens's menu has is not captured).

// The arrows, drawn for Cogpit: a shaft and a head pointing left, and right.
var (
	iconHistoryBack = theme.NewThemedResource(fyne.NewStaticResource("cogpit-history-back.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
			`<path d="M10.6 5.3 12 6.7 7.7 11H20v2H7.7l4.3 4.3-1.4 1.4L3.9 12z"/></svg>`)))
	iconHistoryForward = theme.NewThemedResource(fyne.NewStaticResource("cogpit-history-forward.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
			`<path d="M13.4 5.3 12 6.7l4.3 4.3H4v2h12.3L12 17.3l1.4 1.4 6.7-6.7z"/></svg>`)))
)

// titleHistory is the arrows and the bar's context menu.
type titleHistory struct {
	sh            *shell
	back, forward *tipButton
	root          *fyne.Container
	menu          *fyne.Menu // the context menu last shown (tests)
}

func newTitleHistory(sh *shell) *titleHistory {
	h := &titleHistory{sh: sh}
	h.back = h.arrow("ui.tab.back", iconHistoryBack, true)
	h.forward = h.arrow("ui.tab.forward", iconHistoryForward, false)
	h.root = container.NewHBox(h.back, h.forward)
	return h
}

// shown reports whether an arrow shows: false in a build without the commands.
func (h *titleHistory) shown() bool { return h.back.Visible() || h.forward.Visible() }

// arrow is one arrow, named by its command's label and shortcut; hidden in a build without the
// command.
func (h *titleHistory) arrow(id string, icon fyne.Resource, back bool) *tipButton {
	b := newBareTipButton(h.sh, icon, "", func() { h.step(back) })
	if c, ok := h.sh.eng.Registry().Get(id); ok {
		b.tip = c.Label
		if sc := shortcutText(c.Shortcut, runtime.GOOS == "darwin"); sc != "" {
			b.tip += " (" + sc + ")"
		}
	} else {
		b.Hide()
	}
	b.Disable()
	return b
}

// can reports whether the active tab has a view to go back (or forward) to, as the engine's
// ui.tab.back and ui.tab.forward check it: a tab of the connected cluster with history that way.
func (h *titleHistory) can(back bool) bool {
	ui := h.sh.ui
	t, ok := ui.Tab(ui.ActiveTab)
	if !ok || !ui.Connection.State.Live() || t.Context != ui.Context {
		return false
	}
	if back {
		return len(t.Back) > 0
	}
	return len(t.Forward) > 0
}

// step goes back (or forward) in the active tab's history.
func (h *titleHistory) step(back bool) {
	if !h.can(back) {
		return
	}
	id := "ui.tab.forward"
	if back {
		id = "ui.tab.back"
	}
	h.sh.dispatch(id, struct{}{}, nil)
}

// update enables the arrows from the active tab's history.
func (h *titleHistory) update() {
	for _, a := range []struct {
		b  *tipButton
		on bool
	}{{h.back, h.can(true)}, {h.forward, h.can(false)}} {
		if a.on && a.b.Disabled() {
			a.b.Enable()
		} else if !a.on && !a.b.Disabled() {
			a.b.Disable()
		}
	}
}

// showMenu opens the title bar's context menu at pos, in window coordinates.
func (h *titleHistory) showMenu(pos fyne.Position) {
	sh := h.sh
	var items []*fyne.MenuItem
	if _, ok := sh.eng.Registry().Get("preferences.open"); ok {
		items = append(items, sh.withShortcut(fyne.NewMenuItem("Preferences…", func() {
			sh.dispatch("preferences.open", preferencesOpenParams{}, nil)
		}), "preferences.open"))
	}
	if len(items) == 0 {
		return
	}
	h.menu = fyne.NewMenu("", items...)
	sh.tip.hide()
	widget.NewPopUpMenu(h.menu, sh.win.Canvas()).ShowAtPosition(pos)
}

// stateChanged follows the UI state in the bar: the arrows and the bell.
func (t *topBar) stateChanged() {
	t.history.update()
	t.notes.stateChanged()
	t.alerts.update() // the shown cluster's alerts (alerts.go, D-77)
}
