package fyneui

import (
	"fyne.io/fyne/v2"
)

// ⌘F in the dock's editor tabs (the owner's gate walk, 2026-10-20, GW3: "⌘F does not open the Helm
// Upgrade tab's find bar", only its magnifier did). Every dock tab with a YAML editor has a find bar
// below it (findbar.go): Edit, Create, Helm Upgrade and Helm Install; Edit had none before and has
// one since, opened by ⌘F alone (it has no toolbar for a magnifier, as Lens's Edit tab has none).
// ⌘F opens the shown tab's find bar and puts the keyboard in its field, wherever the keys are within
// that tab: in the editor (yamlview's OnFind), in another of its fields, or nowhere (after a click
// on one of its buttons). Elsewhere ⌘F stays what it was: the list's search, or the focused text
// field's (A55, menukeys.go). The lead's decisions of 2026-10-20, open to revision (D-106):
//   - ⌘F goes by where the keyboard is, not by what is shown: with the keys in the list (its table,
//     its search) the list's search takes ⌘F although the dock shows an editor tab below, as an
//     editor's find takes it only from its own pane in other IDEs;
//   - with the keys nowhere, the open dock's editor tab takes it: a click on its buttons (the
//     magnifier, Save, a version) leaves no focus, and the list's search would surprise there;
//   - ⌘F on an open find bar keeps it open and puts the keys back in its field (the magnifier still
//     toggles it); the editor's selection on one line becomes the search text, as the magnifier
//     does;
//   - an Edit tab showing its review (the diff) has no editor in view: ⌘F is the list's again.

// findTab is a dock tab with a find bar: openFind opens it and reports whether it did.
type findTab interface {
	openFind() bool
}

// findTarget returns the shown dock tab whose find bar ⌘F opens: the dock is open on a tab with a
// find bar, and the keyboard is in that tab or nowhere. Nil: ⌘F is not the dock's.
func (d *dock) findTarget(focused fyne.Focusable) findTab {
	if !d.open() {
		return nil
	}
	c, ok := d.contents[d.active]
	if !ok || c.closed {
		return nil
	}
	f, ok := c.editor.(findTab)
	if !ok {
		return nil
	}
	if focused != nil && !d.holdsFocus(c, focused) {
		return nil
	}
	return f
}

// holdsFocus reports whether the focused widget lies in a tab's content: its place in the window
// within the content's (the editor, a find bar's field, the Install tab's fields).
func (d *dock) holdsFocus(c *dockContent, focused fyne.Focusable) bool {
	o, ok := focused.(fyne.CanvasObject)
	if !ok || !o.Visible() || !c.obj.Visible() {
		return false
	}
	drv := fyne.CurrentApp().Driver()
	at, size := drv.AbsolutePositionForObject(c.obj), c.obj.Size()
	p := drv.AbsolutePositionForObject(o)
	return p.X >= at.X && p.Y >= at.Y && p.X < at.X+size.Width && p.Y < at.Y+size.Height
}

// claimFind opens the shown editor tab's find bar when ⌘F is the dock's (findTarget), or the
// drawer's YAML find bar when it is the drawer's (drawerFinds): true when it did, before any widget
// is offered the key.
func (sh *shell) claimFind() bool {
	focused := sh.win.Canvas().Focused()
	drawer := sh.drawerFinds(focused)
	if f := sh.dock.findTarget(focused); f != nil && (focused != nil || !drawer) && f.openFind() {
		return true
	}
	if drawer {
		sh.drawer.find.open()
		return true
	}
	return false
}

// drawerFinds reports whether ⌘F is the drawer's YAML find bar's (the owner's request of
// 2026-10-25, "Mit cmd+f wird das Feld dann aktiv"): the drawer shows its YAML, and the keyboard
// is in the drawer, in the list's table (a click on a row puts it there, VW17) or nowhere. A text
// field elsewhere keeps its own ⌘F, and with the keys nowhere the drawer's YAML goes before the
// dock's editor (the lead's details, D-106's addition, open to revision).
func (sh *shell) drawerFinds(focused fyne.Focusable) bool {
	if !sh.drawer.yamlShown() {
		return false
	}
	if focused == nil || focused == fyne.Focusable(sh.view.table) {
		return true
	}
	o, ok := focused.(fyne.CanvasObject)
	if !ok {
		return false
	}
	drv := fyne.CurrentApp().Driver()
	at, size, p := drv.AbsolutePositionForObject(sh.drawer.root), sh.drawer.root.Size(), drv.AbsolutePositionForObject(o)
	return p.X >= at.X && p.Y >= at.Y && p.X < at.X+size.Width && p.Y < at.Y+size.Height
}

// searchKey is the window's ⌘F: the shown editor tab's find bar when ⌘F is the dock's, else the
// list's search.
func (sh *shell) searchKey() {
	if sh.claimFind() {
		return
	}
	sh.win.Canvas().Focus(sh.view.search)
}

// openFind opens the find bar of a Create tab.
func (c *createTab) openFind() bool { c.find.open(); return true }

// openFind opens the find bar of a Helm Upgrade tab.
func (u *helmUpgradeTab) openFind() bool { u.find.open(); return true }

// openFind opens the find bar of a Helm Install tab.
func (u *helmInstallTab) openFind() bool { u.find.open(); return true }

// openFind opens the find bar of an Edit tab while it shows its editor; its review has none.
func (e *editorTab) openFind() bool {
	if e.reviewing {
		return false
	}
	e.find.open()
	return true
}
