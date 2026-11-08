package fyneui

import (
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// The window's side of the single keys (D-158). Fyne hands a key to the window's handlers
// only while no widget has the keyboard, or while one passes it on (a drawer's selectable text,
// selection.go): then a key acts on the drawer's object while the drawer is open, else on the
// table's keyboard row, the table taking the keyboard. A dialog, a pop-up, the palette or a
// settings page over the window takes the keys from both.

// windowKeyHints are the window's single keys as the key overview lists them.
var windowKeyHints = []keyHint{
	{":", "Go to a view: :po, :deploy kube-system"},
	{"/", "Search the view"},
	{"0", "Every namespace"},
	{"?", "This overview"},
}

// windowRune runs the window's single keys, which act wherever the keyboard is but a text field or
// a terminal: / the view's search, : the jump, ? the key overview, 0 every namespace. It reports
// whether r is one of them.
func (sh *shell) windowRune(r rune) bool {
	switch r {
	case '/':
		sh.win.Canvas().Focus(sh.view.search)
	case ':':
		sh.openJump() // the palette with ":" typed (jump.go)
	case '?':
		sh.toggleKeyOverview()
	case '0':
		sh.dispatch("ui.setNamespace", namespacesParams{Namespaces: []string{}}, nil)
	default:
		return false
	}
	return true
}

// keysBlocked reports whether something over the window takes the single keys: a settings page,
// a dialog, a pop-up menu, the palette, the key overview.
func (sh *shell) keysBlocked() bool {
	return sh.settings.visible() || sh.win.Canvas().Overlays().Top() != nil
}

// windowTypedRune is the window's rune handler: "/" focuses the active logs tab's search, as before;
// the other single keys act on the drawer's object or the table's row.
func (sh *shell) windowTypedRune(r rune) {
	if sh.settings.visible() {
		return
	}
	if sh.win.Canvas().Overlays().Top() != nil {
		if r == '?' && sh.ui.KeyOverview {
			sh.toggleKeyOverview()
		}
		return
	}
	if !sh.rowKeysHere() { // a place with keys of its own handed it on (a logs tab's lines)
		if r == '?' {
			sh.toggleKeyOverview()
		}
		return
	}
	if r == '/' {
		sh.dock.focusLogSearch() // "/" focuses the active logs tab's search
		if sh.win.Canvas().Focused() != nil {
			return
		}
	}
	if r == ' ' || sh.windowRune(r) {
		return
	}
	tv := sh.view
	if sh.ui.Drawer != nil && !tableOnlyRune(r) {
		sh.rowKey(string(r), keyTarget{ref: *sh.ui.Drawer, row: -1, drawer: true})
		return
	}
	if tv.rowCount() > 0 && tv.table.Visible() {
		sh.win.Canvas().Focus(tv.table)
		tv.table.keyed()
		tv.tableRune(r)
	}
}

// tableOnlyRune reports whether a single key is the table's own (moving, sorting), which acts on the
// table even while the drawer is open.
func tableOnlyRune(r rune) bool {
	switch r {
	case 'j', 'k', 'g', 'G', 'N', 'A', 'S', 'P', 'O':
		return true
	}
	return false
}

// windowTypedKey runs the named keys the window hears: Delete deletes the drawer's object or the
// table's row; a key with Control or ⌘ that a drawer's selectable text passed on (Fyne offers such
// a text only ⌘C) is the shortcut it was typed as.
func (sh *shell) windowTypedKey(ev *fyne.KeyEvent) {
	if sh.keysBlocked() || !sh.rowKeysHere() {
		return
	}
	if d, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok {
		mods := d.CurrentKeyModifiers() &^ fyne.KeyModifierShift
		if mods != 0 {
			sh.windowShortcut(&desktop.CustomShortcut{KeyName: ev.Name, Modifier: d.CurrentKeyModifiers()})
			return
		}
	}
	if ev.Name != fyne.KeyDelete {
		return
	}
	if sh.ui.Drawer != nil {
		sh.rowKey("Delete", keyTarget{ref: *sh.ui.Drawer, row: -1, drawer: true})
		return
	}
	if t, ok := sh.view.ringTarget(); ok {
		sh.rowKey("Delete", t)
	}
}

// windowShortcut runs a row key with modifiers typed while nothing has the keyboard: on the
// drawer's object while the drawer is open, else the table's (⌃Z, ⌃\, ⌘S and the row keys on its
// keyboard row).
func (sh *shell) windowShortcut(sc *desktop.CustomShortcut) {
	if sh.keysBlocked() || !sh.rowKeysHere() {
		return
	}
	if sh.ui.Drawer != nil && sh.rowKeyShortcut(sc, keyTarget{ref: *sh.ui.Drawer, row: -1, drawer: true}) {
		return
	}
	if sh.view.rowCount() > 0 {
		sh.view.tableShortcut(sc)
	}
}

// rowKeyWindowShortcuts are the keys with modifiers the window takes while nothing has the keyboard
// (installRowKeyShortcuts): Delete's, the status filter's, clearing the marks, the CSV.
func rowKeyWindowShortcuts(goos string) []*desktop.CustomShortcut {
	cmd := fyne.KeyModifierControl
	if goos == goosMac {
		cmd = fyne.KeyModifierSuper
	}
	return []*desktop.CustomShortcut{
		{KeyName: fyne.KeyD, Modifier: fyne.KeyModifierControl},
		{KeyName: fyne.KeyBackspace, Modifier: cmd},
		{KeyName: fyne.KeyZ, Modifier: fyne.KeyModifierControl},
		{KeyName: fyne.KeyBackslash, Modifier: fyne.KeyModifierControl},
		{KeyName: fyne.KeyS, Modifier: cmd},
	}
}

// installRowKeyShortcuts adds the row keys with modifiers to the window's shortcuts, which Fyne runs
// while no widget that takes shortcuts has the keyboard: a text field and a terminal keep them
// (Ctrl+D in a shell).
func (sh *shell) installRowKeyShortcuts(c fyne.Canvas) {
	for _, sc := range rowKeyWindowShortcuts(runtime.GOOS) {
		c.AddShortcut(sc, func(typed fyne.Shortcut) {
			if cs, ok := typed.(*desktop.CustomShortcut); ok {
				sh.windowShortcut(cs)
			}
		})
	}
}

// toggleKeyOverview shows or hides the key overview (ui.keyOverview), for the place the keyboard is
// now.
func (sh *shell) toggleKeyOverview() {
	if !sh.ui.KeyOverview {
		sh.keyPlace = sh.currentKeyPlace()
	}
	sh.dispatch("ui.keyOverview", struct{}{}, nil)
}

// passWindowShortcut runs a shortcut a focused widget that takes shortcuts (the table) does not use
// as the window would without it: its own keys (windowKeys: ⌘F, ⌘⇧T), else the row keys'
// (windowShortcut).
func (sh *shell) passWindowShortcut(sc fyne.Shortcut) {
	name := sc.ShortcutName()
	if shown, ok := sc.(*shownShortcut); ok {
		name = shown.CustomShortcut.ShortcutName()
	}
	for _, k := range windowKeys {
		if rs := sh.registryShortcut(k.id); rs != nil && rs.ShortcutName() == name {
			heldUnderSettings(k.run)(sh)
			return
		}
	}
}

// rowKeysHere reports whether a key the window hears may act on rows: nothing has the keyboard, or
// the table, or a selectable text in the drawer passed it on. A place with keys of its own that
// passes the rest on (a logs tab's lines) keeps the row keys out.
func (sh *shell) rowKeysHere() bool {
	focused := sh.win.Canvas().Focused()
	if focused == nil || focused == fyne.Focusable(sh.view.table) {
		return true
	}
	obj, ok := focused.(fyne.CanvasObject)
	return ok && sh.drawer.root.Visible() && liesWithin(obj, sh.drawer.root)
}

// liesWithin reports whether obj lies inside area on the window: its top-left corner in area's bounds.
func liesWithin(obj, area fyne.CanvasObject) bool {
	drv := fyne.CurrentApp().Driver()
	p, a := drv.AbsolutePositionForObject(obj), drv.AbsolutePositionForObject(area)
	sz := area.Size()
	return p.X >= a.X && p.Y >= a.Y && p.X < a.X+sz.Width && p.Y < a.Y+sz.Height
}
