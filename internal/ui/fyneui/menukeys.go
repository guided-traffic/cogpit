package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// ⌘F and ⌘⇧T are main-menu items that show their keys, View › Search and View › New Terminal, while
// the keys stay the window's shortcuts (the owner's A55, 2026-10-14 evening): a focused widget that
// takes shortcuts keeps them, so a text field or a terminal swallows them as before; ⌘F with the
// keys in a dock tab's editor, or anywhere in that tab, opens its find bar (GW3, findkeys.go). A
// main-menu shortcut would take them from any focus (Fyne asks the main menu first; on macOS it is
// the native menu's key equivalent), as ⌘⇧P and ⌘P do on purpose.
//
// How (Cogpit's, read in Fyne 2.8.1's source, not tried in a real window):
//   - the item's shortcut is a shownShortcut: it has the key and the modifiers, so the menu draws
//     them (Fyne's menu on Windows and Linux) or sets them as the key equivalent (macOS), and a
//     name of its own, so that Fyne's main-menu lookup (triggerMainMenuShortcut, which compares the
//     names) never matches a typed key: on Windows and Linux the key goes on to the focused widget
//     and the window's shortcut as before, and the item runs only when clicked;
//   - on macOS AppKit runs the item for its key equivalent before the window sees the key, so the
//     item asks AppKit whether a key event is being dispatched (menuKey, menukeys_darwin.m): for a
//     key it does what the window does with it (typeWindowShortcut), for a click its action.

// windowKey is a shortcut the window keeps: the registry command whose shortcut it is, and what the
// window does when neither the main menu nor the focused widget takes it.
type windowKey struct {
	id  string
	run func(sh *shell)
	// claim, when set, may take the key before the focused widget is offered it: true when it did
	// (⌘F in the dock's editor tabs, findkeys.go).
	claim func(sh *shell) bool
}

// setFilterCommand is the view's search, ⌘F.
const setFilterCommand = "ui.setFilter"

// windowKeys are the window's own shortcuts (installShortcuts), shown in the View menu.
var windowKeys = []windowKey{
	{setFilterCommand, (*shell).searchKey, (*shell).claimFind},                                // ⌘F: the editor tab's find bar, else the view's search
	{"terminal.open", func(sh *shell) { sh.dispatch("terminal.open", struct{}{}, nil) }, nil}, // ⌘⇧T: a terminal tab
}

// shownShortcut is a menu item's shortcut that the menu shows and Fyne never triggers from a key.
type shownShortcut struct {
	desktop.CustomShortcut
}

// ShortcutName implements fyne.Shortcut: a name no typed key has.
func (s *shownShortcut) ShortcutName() string { return "Shown:" + s.CustomShortcut.ShortcutName() }

// windowKeyItems are the View menu's items of the window's own shortcuts: of the commands the
// registry gives a menu path.
func (sh *shell) windowKeyItems() []*fyne.MenuItem {
	var out []*fyne.MenuItem
	for _, k := range windowKeys {
		c, ok := sh.eng.Registry().Get(k.id)
		if !ok || c.MenuPath == "" {
			continue
		}
		run, claim := k.run, k.claim
		item := fyne.NewMenuItem(c.Label, func() { run(sh) })
		if sc := parseShortcut(c.Shortcut); sc != nil {
			item.Shortcut = &shownShortcut{*sc}
			item.Action = func() {
				if sh.menuKey() { // macOS's key equivalent: the window's key, not a click
					if claim != nil && claim(sh) {
						return
					}
					sh.typeWindowShortcut(sc, func() { run(sh) })
					return
				}
				run(sh)
			}
		}
		out = append(out, item)
	}
	return out
}

// typeWindowShortcut does with a key the window keeps what Fyne does without a main-menu item for
// it: the focused widget takes it when it takes shortcuts (the editor, a terminal, a text field),
// else the window's own shortcut runs.
func (sh *shell) typeWindowShortcut(sc fyne.Shortcut, run func()) {
	if f, ok := sh.win.Canvas().Focused().(fyne.Shortcutable); ok {
		f.TypedShortcut(sc)
		return
	}
	run()
}
