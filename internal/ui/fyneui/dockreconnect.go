package fyneui

import (
	"context"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui/termview"
)

// A shell, attach or node shell tab whose session the connection's interruption ended (D-58, S7):
// a row above the terminal says so and offers Reconnect (dock.reconnect), which opens a new session
// in the same tab once the connection is back; a process cannot be resumed. The engine marks the tab
// (DockTab.Interrupted) and counts its sessions (DockTab.Session); the window follows them here.

// isTerminal reports whether a dock tab shows a terminal.
func isTerminal(t engine.DockType) bool {
	switch t {
	case engine.DockTerminal, engine.DockShell, engine.DockAttach, engine.DockNodeShell:
		return true
	}
	return false
}

// startTerminal opens a terminal tab's session off the main goroutine and shows it in c; until it
// is open, or if it fails, the tab says so.
func (d *dock) startTerminal(ctx context.Context, c *dockContent, id string) {
	go func() {
		defer d.sh.recoverTo("terminal")
		sess, err := d.sh.eng.OpenTerminal(ctx, id)
		d.sh.do(func() {
			if c.closed || ctx.Err() != nil {
				if sess != nil {
					go func() { _ = sess.Close() }() // the tab is gone or opens another session; nobody to tell
				}
				return
			}
			if err != nil {
				c.set(c.withReconnectRow(dockMessage("Could not start the session: "+err.Error(), colorStatusError)))
				return
			}
			w := termview.NewTerminal(sess)
			w.SetRightClickCopyPaste(d.sh.rightClickCopyPaste()) // the Preferences' right mouse button (termpaste.go)
			c.closer, c.term = w, w
			c.set(c.withReconnectRow(c.withKubectlOffer(d.sh.terminalScope(w)))) // the Preferences' terminal font size (preferences.go)
			d.takeFocus()                                                        // the user's new tab takes the keys now (dockfocus.go)
		})
	}()
}

// followSession follows a terminal tab's state: a new session after dock.reconnect, the row while
// the interruption ended it.
func (d *dock) followSession(c *dockContent, t engine.DockTab) {
	restart := t.Session != c.tab.Session
	c.tab = t
	if restart {
		c.cancel()
		if cl := c.closer; cl != nil {
			go func() { _ = cl.Close() }()
		}
		c.closer, c.term = nil, nil
		ctx, cancel := context.WithCancel(d.sh.ctx)
		c.cancel = cancel
		c.set(dockMessage("Opening "+t.Title+"…", colorMuted))
		d.startTerminal(ctx, c, t.ID)
	}
	c.updateReconnect(d.sh)
}

// connectionChanged updates the terminal tabs' Reconnect rows: their button works once the
// connection is back; and the terminal chip, shown while the shown cluster is connected. Main
// goroutine only.
func (d *dock) connectionChanged() {
	d.terminal.update()
	for _, c := range d.contents {
		c.updateReconnect(d.sh)
	}
}

// reconnectRow is the row above a terminal whose session the interruption ended.
type reconnectRow struct {
	root   *fyne.Container
	text   *textLine
	button *widget.Button
}

// withReconnectRow puts the tab's Reconnect row above content.
func (c *dockContent) withReconnectRow(content fyne.CanvasObject) fyne.CanvasObject {
	if c.tab.Type == engine.DockTerminal {
		return content // a local shell does not end with the connection
	}
	if c.reconnect == nil {
		c.reconnect = &reconnectRow{text: newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})}
		c.reconnect.text.truncate = true
		c.reconnect.button = widget.NewButtonWithIcon("Reconnect", theme.ViewRefreshIcon(), nil)
		c.reconnect.button.Importance = widget.HighImportance
		strip := container.NewBorder(nil, nil, container.NewCenter(widget.NewIcon(themedIcon(theme.WarningIcon(), colorStatusWarning))),
			container.NewCenter(c.reconnect.button), c.reconnect.text)
		c.reconnect.root = container.NewVBox(container.NewStack(newBannerTint(colorStatusWarning),
			inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, strip)), newDivider(false))
		c.reconnect.root.Hide()
	}
	return container.NewBorder(c.reconnect.root, nil, nil, nil, content)
}

// updateReconnect shows or hides the row for the tab's state and the connection's.
func (c *dockContent) updateReconnect(sh *shell) {
	r := c.reconnect
	if r == nil {
		return
	}
	if c.tab.Interrupted.IsZero() {
		if r.root.Visible() {
			r.root.Hide()
			r.root.Refresh()
		}
		return
	}
	what := "This shell"
	switch c.tab.Type {
	case engine.DockAttach:
		what = "This attach"
	case engine.DockNodeShell:
		what = "This node shell"
	}
	why := "the connection to the cluster was interrupted"
	if c.tab.Reconnected {
		why = "Cogpit connected the cluster anew (Reconnect)" // D-81
	}
	text := what + " ended at " + sh.timeFormat().FormatClock(c.tab.Interrupted) +
		": " + why + ", and a process cannot be resumed. Reconnect opens a new session here"
	id := c.tab.ID
	r.button.OnTapped = func() { sh.dispatch("dock.reconnect", dockParams{ID: id}, nil) }
	if sh.ui.Connection.State == engine.Connected {
		r.button.Enable()
	} else {
		r.button.Disable()
		text += " once the connection is back"
	}
	r.text.SetText(text + ".")
	if !r.root.Visible() {
		r.root.Show()
	}
	r.root.Refresh()
}
