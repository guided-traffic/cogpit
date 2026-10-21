package fyneui

import (
	"image/color"
	"math"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The top bar's right end (F7, the owner's finding of 2026-10-15, after VS Code's title bar): an
// Update button while the app's updater holds a newer release for the user (UIState.Update), then
// three layout toggles in VS Code's order: the navigator (its primary side bar), the dock (its
// panel) and the agent chat's column (its secondary side bar). Each toggle dispatches its engine
// command (ui.toggleNavigator ⌘B, ui.toggleDock ⌘J, chat.toggle ⌘I), so agents see and drive the
// same state, and its icon shows whether its part is open. Since 2026-10-16 night (the owner's)
// the toggles stand at the bar's right end, after the bell and a divider (bars.go).
// The View menu has the three with their shortcuts (layoutMenuItems).

// layoutIcon is one of the toggles' icons, Cogpit's own drawing (no VS Code asset or codicon,
// D-12): a window's outline and, filled while it is open or as a line where it sits while it is
// closed, the part the toggle shows and hides. Drawn on the inline icon's 16-point grid, so that
// its one-point lines stay sharp on a 1× screen; themed: in the foreground colour, greyed when the
// button is disabled.
func layoutIcon(name, part string) fyne.Resource {
	return theme.NewThemedResource(fyne.NewStaticResource("cogpit-layout-"+name+".svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">`+
			`<path d="M2.5 2h11A1.5 1.5 0 0 1 15 3.5v9a1.5 1.5 0 0 1-1.5 1.5h-11A1.5 1.5 0 0 1 1 12.5v-9A1.5 1.5 0 0 1 2.5 2z`+
			`M2.5 3a.5.5 0 0 0-.5.5v9a.5.5 0 0 0 .5.5h11a.5.5 0 0 0 .5-.5v-9a.5.5 0 0 0-.5-.5z"/>`+
			`<path d="`+part+`"/></svg>`)))
}

// The toggles' icons: the navigator's column at the left, the dock's band at the bottom, the chat's
// column at the right; open filled, closed as the line of the part's inner edge.
var (
	iconNavigatorShown  = layoutIcon("navigator-shown", "M2 3h4v10H2z")
	iconNavigatorHidden = layoutIcon("navigator-hidden", "M5 3h1v10H5z")
	iconDockOpen        = layoutIcon("dock-open", "M2 9h12v4H2z")
	iconDockClosed      = layoutIcon("dock-closed", "M2 9h12v1H2z")
	iconChatShown       = layoutIcon("chat-shown", "M10 3h4v10h-4z")
	iconChatHidden      = layoutIcon("chat-hidden", "M10 3h1v10h-1z")
)

// layoutToggleParams mirrors ui.toggleNavigator's and ui.toggleDock's parameters
// (commands/layout.go).
type layoutToggleParams struct {
	Open *bool `json:"open,omitempty"`
}

// layoutBar is the Update button and the three toggles.
type layoutBar struct {
	sh                    *shell
	update                *tipButton
	updateBox             *fyne.Container // the Update button as a pill in the bar's middle (compactLayout)
	navigator, dock, chat *tipButton
	root                  fyne.CanvasObject // the toggles in their compact theme
	toggles               *fyne.Container   // the three toggles
	label                 string            // the Update button's label with the version (updateLabel), shown where it fits
	room                  float32           // the width the bar's row left for the Update button at its last layout
}

// updateShort is the Update button's label without a version, and where the bar has no room for
// the version.
const updateShort = "Update"

func newLayoutBar(sh *shell) *layoutBar {
	l := &layoutBar{sh: sh, label: updateShort}
	l.update = newTipButton(sh, nil, "", func() { sh.dispatch("app.installUpdate", struct{}{}, nil) })
	l.update.SetText(updateShort)
	l.update.Importance = widget.HighImportance
	l.updateBox = box(compactLayout{}, l.update)
	l.showUpdate(false)
	l.navigator = newTipButton(sh, iconNavigatorShown, "", func() {
		sh.dispatch("ui.toggleNavigator", layoutToggleParams{}, nil)
	})
	l.dock = newTipButton(sh, iconDockClosed, "", func() { sh.dispatch("ui.toggleDock", layoutToggleParams{}, nil) })
	// ⌘I's: the input takes the focus (chat.go). The column is made after the top bar.
	l.chat = newTipButton(sh, iconChatHidden, "", func() { sh.chat.toggle() })
	for _, b := range []struct {
		b  *tipButton
		id string
	}{{l.navigator, "ui.toggleNavigator"}, {l.dock, "ui.toggleDock"}, {l.chat, "chat.toggle"}} {
		if _, ok := sh.eng.Registry().Get(b.id); !ok {
			b.b.Hide() // a build without the command (a test fake)
		}
	}
	// At the bar's right end (bars.go), close together: half their inner padding (the owner,
	// 2026-10-16 night: "können näher zusammen rücken"), and a padding between the buttons (the
	// owner, 2026-10-18, WF17: "zu eng beieinander. Ein bisschen mehr Platz").
	gap := sh.app.Settings().Theme().Size(theme.SizeNamePadding)
	l.toggles = container.New(layout.NewCustomPaddedHBoxLayout(gap), l.navigator, l.dock, l.chat)
	l.root = container.NewThemeOverride(l.toggles, &compactButtonTheme{app: sh.app})
	return l
}

// stateChanged follows the UI state: each toggle's icon and tooltip, the dock's enabled state, and
// the Update button.
func (l *layoutBar) stateChanged() {
	ui := l.sh.ui
	set := func(b *tipButton, open bool, on, off fyne.Resource, verbOpen, verbClosed, label, id string) {
		icon, verb := off, verbClosed
		if open {
			icon, verb = on, verbOpen
		}
		if b.Icon != icon {
			b.SetIcon(icon)
		}
		b.tip = verb + " " + label + l.shortcut(id)
	}
	set(l.navigator, !ui.NavigatorHidden, iconNavigatorShown, iconNavigatorHidden, "Hide", "Show", "Navigator", "ui.toggleNavigator")
	set(l.dock, l.sh.dock.open(), iconDockOpen, iconDockClosed, "Collapse", "Open", "Dock", "ui.toggleDock")
	set(l.chat, ui.Chat, iconChatShown, iconChatHidden, "Hide", "Show", "Agent Chat", "chat.toggle")
	switch {
	case len(ui.DockInScope()) > 0:
		l.dock.Enable()
	case ui.Connection.State.Live() && !ui.Connection.Demo: // an empty dock opens a terminal (WF18)
		l.dock.tip = "Open a Terminal" + l.shortcut("ui.toggleDock")
		l.dock.Enable()
	default: // as ui.toggleDock's Enabled: a terminal needs a connected cluster, not the demo
		why := "a terminal needs a connected cluster"
		if ui.Connection.Demo {
			why = "the demo cluster has no terminals"
		}
		l.dock.tip = "Dock: no tabs; " + why + l.shortcut("ui.toggleDock")
		l.dock.Disable()
	}
	if !l.sh.chat.available() {
		l.chat.Hide() // a window without the agent's chat
	}
	l.updateChanged(ui.Update)
}

// updateChanged shows the Update button while the updater holds a newer release, its version in
// the label (where the bar has the room) and the tooltip. On Linux (D-108) the click installs the
// release through apt after polkit's password prompt, and once apt installed it under the running
// app the button reads Restart to Update and restarts Cogpit into it. On Windows (D-111) the click
// installs it at once through WinSparkle, which shows the download, and Cogpit restarts into it.
func (l *layoutBar) updateChanged(u engine.UpdateState) {
	l.update.tip = updateTip(u, runtime.GOOS)
	label := updateLabel(u.Version)
	if u.Restart {
		label = restartLabel
	}
	if label != l.label {
		l.label = label
		l.fit(l.room)
	}
	l.showUpdate(u.Available)
}

// updateLabel is the Update button's label for the version Sparkle names (the appcast item's
// display version): "Update to v0.16.0", a leading v of the version's own not doubled; "Update"
// without a version.
func updateLabel(version string) string {
	if len(version) > 1 && (version[0] == 'v' || version[0] == 'V') && isDigit(version[1]) {
		version = version[1:]
	}
	switch {
	case version == "":
		return updateShort
	case isDigit(version[0]):
		return "Update to v" + version
	}
	return "Update to " + version // not a number ("beta"): no v before it
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// restartLabel is the Update button's label once the release is installed under the running app
// (Linux); where the bar has no room it says "Update" as ever, and the tooltip says what it does.
const restartLabel = "Restart to Update"

// updateTip is the Update button's tooltip: what its click does on goos.
func updateTip(u engine.UpdateState, goos string) string {
	cogpit := "A newer Cogpit"
	if u.Version != "" {
		cogpit = "Cogpit " + u.Version
	}
	switch {
	case u.Restart:
		return cogpit + " is installed: restart Cogpit to use it"
	case goos == "linux":
		return cogpit + " is available: install it through apt (asks for an administrator's password)"
	case goos == "windows":
		return cogpit + " is available: install it now; Cogpit restarts into it"
	}
	return cogpit + " is available: show it in the update window"
}

// fit gives the Update button its label with the version while that fits in room, the width the
// bar's row leaves for the button, and "Update" otherwise: on a narrow window, or beside a long
// context name, the version gives way first, and the window's least width stays the short label's.
func (l *layoutBar) fit(room float32) {
	l.room = room
	text := updateShort
	if l.label != updateShort && l.updateWidth(l.label) <= room {
		text = l.label
	}
	if l.update.Text != text {
		l.update.SetText(text)
	}
}

// updateWidth is the Update button's least width with text as its label: its width now, changed
// by the difference of the two texts' widths (the button's label is one line of bold text).
func (l *layoutBar) updateWidth(text string) float32 {
	return l.update.MinSize().Width - labelWidth(l.update.Text) + labelWidth(text)
}

// updateExtra is how much wider the Update button is now than it would be with "Update": 0 while
// it is hidden or says "Update".
func (l *layoutBar) updateExtra() float32 {
	if !l.updateBox.Visible() || l.update.Text == updateShort {
		return 0
	}
	return labelWidth(l.update.Text) - labelWidth(updateShort)
}

// labelWidth is the width of a button's label text: the theme's text size, bold.
func labelWidth(text string) float32 {
	th := fyne.CurrentApp().Settings().Theme()
	return fyne.MeasureText(text, th.Size(theme.SizeNameText), fyne.TextStyle{Bold: true}).Width
}

// barRowLayout is the top bar's row: the left part (the logo, the wordmark, the history arrows) at
// the left and the right part (the Update button, the toggles, the bell) at the right end, each at
// its least width and the row's height, where a border layout without a centre puts them; and it
// tells the Update button the room it has, which picks its label (layoutBar.fit). The row's least
// width counts the button's short label, so the version never widens the window's minimum.
type barRowLayout struct{ t *topBar }

func (r barRowLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	lm, rm := r.t.left.MinSize(), r.t.right.MinSize()
	pad := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNamePadding)
	return fyne.NewSize(lm.Width+pad+rm.Width-r.t.layout.updateExtra(), max(lm.Height, rm.Height))
}

func (r barRowLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	left, right, l := r.t.left, r.t.right, r.t.layout
	pad := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNamePadding)
	lw := left.MinSize().Width
	if l.updateBox.Visible() {
		// The room beside the left part that the right part, but for the button, leaves.
		l.fit(size.Width - lw - pad - (right.MinSize().Width - l.update.MinSize().Width))
	}
	rw := right.MinSize().Width
	left.Move(fyne.Position{})
	left.Resize(fyne.NewSize(lw, size.Height))
	right.Move(fyne.NewPos(size.Width-rw, 0))
	right.Resize(fyne.NewSize(rw, size.Height))
}

// showUpdate shows or hides the Update button with its place, so that a hidden one leaves no gap.
func (l *layoutBar) showUpdate(on bool) {
	if on == l.updateBox.Visible() && on == l.update.Visible() {
		return
	}
	if on {
		l.update.Show()
		l.updateBox.Show()
	} else {
		l.update.Hide()
		l.updateBox.Hide()
	}
}

// compactLayout gives its object its least width and the height of a line of bold text with the
// theme's padding above and below, centred: the Update button as a pill, not a block as tall as
// the bar (VS Code's is compact too). The height is rounded up to an even number of units, so that
// in the bar's even height the pill starts on a whole unit and every renderer puts its edges on the
// same rows (at 25.7 units, 7.15 from the top, the offscreen renderer drew it a row lower).
type compactLayout struct{}

func (compactLayout) height() float32 {
	th := fyne.CurrentApp().Settings().Theme()
	h := fyne.MeasureText("Mg", th.Size(theme.SizeNameText), fyne.TextStyle{Bold: true}).Height + 2*th.Size(theme.SizeNamePadding)
	return 2 * float32(math.Ceil(float64(h)/2))
}

func (l compactLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w float32
	for _, o := range objs {
		if o.Visible() {
			w = max(w, o.MinSize().Width)
		}
	}
	return fyne.NewSize(w, l.height())
}

func (l compactLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	h := min(size.Height, l.height())
	for _, o := range objs {
		o.Resize(fyne.NewSize(size.Width, h))
		o.Move(fyne.NewPos(0, (size.Height-h)/2))
	}
}

// shortcut is " (⌘B)" for a command's shortcut, "" without one.
func (l *layoutBar) shortcut(id string) string {
	if c, ok := l.sh.eng.Registry().Get(id); ok {
		if sc := shortcutText(c.Shortcut, runtime.GOOS == goosMac); sc != "" {
			return " (" + sc + ")"
		}
	}
	return ""
}

// layoutChanged follows the layout's state: the navigator shown or hidden, the dock open or
// collapsed, and the top bar's toggles. applyUIState runs it after the dock followed its tabs.
func (sh *shell) layoutChanged(old engine.UIState, first bool) {
	st := sh.ui
	if first || st.NavigatorHidden != old.NavigatorHidden {
		sh.showNavigator(!st.NavigatorHidden)
	}
	if !first && st.DockOpen != old.DockOpen {
		sh.layoutBody()
		sh.dock.rebuildStrip() // the active tab's mark and the strip's arrow
	}
	sh.top.layout.stateChanged()
}

// showNavigator shows or hides the navigator's column with its edge: the views take its room
// (panelLayout), the hotbar stays. Hidden, it gives the keyboard back if it had it.
func (sh *shell) showNavigator(on bool) {
	panel, handle, _ := sh.sideLay.parts(sh.mainRow.Objects)
	if on {
		panel.Show()
		handle.Show()
	} else {
		if sh.navigatorFocused() {
			sh.win.Canvas().Unfocus()
		}
		panel.Hide()
		handle.Hide()
	}
	sh.mainRow.Refresh()
}

// navigatorFocused reports whether the keyboard is in the navigator: its tree, its section level
// or its search.
func (sh *shell) navigatorFocused() bool {
	f := sh.win.Canvas().Focused()
	if f == nil {
		return false
	}
	s := sh.side
	return f == fyne.Focusable(s.tree) || f == fyne.Focusable(s.sec) || (s.head != nil && f == fyne.Focusable(s.head.entry))
}

// layoutMenuChanged reports whether the View menu's layout items must follow the state: their
// checkmarks, and the Dock item's enabled state with the dock's tabs.
func layoutMenuChanged(st, old engine.UIState) bool {
	return st.Chat != old.Chat || st.NavigatorHidden != old.NavigatorHidden || st.DockOpen != old.DockOpen ||
		(len(st.Dock) == 0) != (len(old.Dock) == 0)
}

// layoutMenuItems are the View menu's Navigator (⌘B) and Dock (⌘J), checked while their part is
// open, before Agent Chat (⌘I, chatMenuItems); main-menu items, so their shortcuts work whatever
// has the focus. None in a build without the commands.
func (sh *shell) layoutMenuItems() []*fyne.MenuItem {
	var items []*fyne.MenuItem
	if c, ok := sh.eng.Registry().Get("ui.toggleNavigator"); ok {
		item := sh.withShortcut(fyne.NewMenuItem(c.Label, func() {
			sh.dispatch(c.ID, layoutToggleParams{}, nil)
		}), c.ID)
		item.Checked = !sh.ui.NavigatorHidden
		items = append(items, item)
	}
	if c, ok := sh.eng.Registry().Get("ui.toggleDock"); ok {
		item := sh.withShortcut(fyne.NewMenuItem(c.Label, func() {
			sh.dispatch(c.ID, layoutToggleParams{}, nil)
		}), c.ID)
		item.Checked = sh.dock.open()
		item.Disabled = len(sh.ui.Dock) == 0
		items = append(items, item)
	}
	return items
}

// compactButtonTheme is the app's current theme with half the inner padding: the layout toggles'
// buttons, close together.
type compactButtonTheme struct{ app fyne.App }

func (t *compactButtonTheme) cur() fyne.Theme { return t.app.Settings().Theme() }

func (t *compactButtonTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	return t.cur().Color(n, v)
}
func (t *compactButtonTheme) Font(s fyne.TextStyle) fyne.Resource     { return t.cur().Font(s) }
func (t *compactButtonTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return t.cur().Icon(n) }
func (t *compactButtonTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameInnerPadding {
		return t.cur().Size(n) / 2
	}
	return t.cur().Size(n)
}
