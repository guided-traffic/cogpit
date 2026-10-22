package fyneui

import (
	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Which dock tab has the keys (the owner's alpha walk of 2026-10-22: AW5, "⋮ › Edit opens the Edit
// tab without the cursor in the editor"; AW8, "⌘⇧T opens a terminal, but the cursor is not put into
// it"; and AW6's first cause, ⌘F routed by where the keys are, D-106, to the list's search; D-115).
// A dock tab the user opens or shows from the window takes the keyboard on its main widget: the
// editor of an Edit, Create, Helm Upgrade or Helm Install tab, the terminal of a terminal, shell,
// attach or node shell tab once its session runs, the lines of a logs tab once its stream opened.
// The window knows a tab is its user's because a command of its own answers with it (the engine's
// DockResult, EditResult and DebugResult, openedTab: the row menu, the drawer's buttons, the menus,
// the palette, ⌘⇧T, the strip's chips, Reconnect, Debug), because the user clicked the tab in the
// strip (dock.rebuildStrip), or restored its draft (drafts.go). The lead's decisions of 2026-10-22, open
// to revision (D-115):
//   - a tab an agent opens or shows (over MCP, the built-in agent) or the CLI leaves the keys where
//     they are: the window sees only the state, never an answer of its own; keys meant for the chat
//     or the list's search would land in a pod's shell otherwise, and the palette and the chat column
//     take the keys for the window's own request only, too (cmdPalette.requested, chatPanel.requested);
//   - a widget not ready yet (a session or a stream still opening) takes the keys once it is, unless
//     the user moved them meanwhile to another widget (a click on a button leaves none: then the tab
//     takes them);
//   - a click on the tab the dock shows gives it the keys back, as a click on a VS Code panel's tab;
//     keys already in the tab (its find bar, a version just picked, which answers with the tab too)
//     stay where they are;
//   - an Edit tab's review and the tabs without a text surface (the Audit Log, the Diagnostics, a
//     job's progress) leave the keys where they are; so do ⌘J, the layout toggle and the strip's
//     arrow, which show or hide the dock and pick no tab.

// openedTab is a command's answer that names the dock tab it opened or showed
// (commands.DockResult, commands.EditResult, commands.DebugResult's Attach tab).
type openedTab interface{ OpenedTab() engine.DockTab }

// focusRequest is a tab the window asked the keys for: what had them then, the tab the dock showed
// and the state's revision at the time.
type focusRequest struct {
	id    string
	from  fyne.Focusable
	shown string
	rev   uint64
}

// focusOpened asks the keys for the tab a command of the window's own answered with. It runs on
// the dispatching goroutine before the command's done is queued, so done finds the request made.
func (sh *shell) focusOpened(res any) {
	t, ok := res.(openedTab)
	if !ok {
		return
	}
	id := t.OpenedTab().ID
	sh.do(func() { sh.dock.requestFocus(id) })
}

// requestFocus asks the keys for tab id: its main widget takes them once the dock shows the tab and
// the widget is ready (takeFocus). The newest request replaces an older one. Main goroutine.
func (d *dock) requestFocus(id string) {
	if id == "" {
		return
	}
	d.focusReq = &focusRequest{id: id, from: d.sh.win.Canvas().Focused(), shown: d.active, rev: d.sh.ui.Revision}
	d.takeFocus()
}

// takeFocus gives the requested tab's main widget the keys once the dock shows the tab and the
// widget is ready. It drops the request when it can no longer be what the user wants: the keys went
// elsewhere meanwhile, the tab closed, a newer state shows a third tab, or the tab has nothing to
// type in. Main goroutine; it costs nothing without a request (applyUIState runs it every time).
func (d *dock) takeFocus() {
	r := d.focusReq
	if r == nil {
		return
	}
	canvas := d.sh.win.Canvas()
	if f := canvas.Focused(); f != nil && f != r.from {
		d.focusReq = nil // the user put the keys elsewhere
		return
	}
	c, ok := d.contents[r.id]
	if ok && c.closed {
		d.focusReq = nil
		return
	}
	if !ok || d.active != r.id || !d.open() {
		// Not shown yet: the state that shows it may be on its way (a click's dock.activate, the
		// dock opening). A newer state without the tab, or showing a third one, ends the request.
		if d.sh.ui.Revision > r.rev && (!ok || d.active != r.shown) {
			d.focusReq = nil
		}
		return
	}
	target, ready := c.focusTarget()
	if !ready {
		return // its session or stream is opening: startTerminal and logTab.open call again
	}
	d.focusReq = nil
	if f := canvas.Focused(); target == nil || (f != nil && d.holdsFocus(c, f)) {
		return // nothing to type in, or the keys are in the tab already (its find bar, a version picked)
	}
	canvas.Focus(target)
}

// focusTarget is the widget of a tab's content that takes the keys, and whether the content is
// ready to say: a terminal's once its session runs, a logs tab's once its stream opened, an editor
// tab's editor; nil for an Edit tab's review and for a tab without a text surface.
func (c *dockContent) focusTarget() (fyne.Focusable, bool) {
	switch {
	case isTerminal(c.tab.Type):
		if c.term == nil {
			return nil, false
		}
		return c.term.FocusTarget(), true
	case c.logs != nil:
		if c.logs.view == nil {
			return nil, false
		}
		return c.logs.view.FocusTarget(), true
	}
	if e, ok := c.editor.(interface{ focusTarget() fyne.Focusable }); ok {
		return e.focusTarget(), true
	}
	return nil, true
}

// focusTarget is the Edit tab's editor; its review has nothing to type in.
func (e *editorTab) focusTarget() fyne.Focusable {
	if e.reviewing {
		return nil
	}
	return e.pane.view
}

// focusTarget is the Create tab's editor.
func (c *createTab) focusTarget() fyne.Focusable { return c.pane.view }

// focusTarget is the Helm Upgrade tab's values editor.
func (u *helmUpgradeTab) focusTarget() fyne.Focusable { return u.pane.view }

// focusTarget is the Helm Install tab's values editor, as in the other editor tabs; its namespace
// and release name come proposed (the namespace filter's, the chart's name with five letters).
func (u *helmInstallTab) focusTarget() fyne.Focusable { return u.pane.view }
