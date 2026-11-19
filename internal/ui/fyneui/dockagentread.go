package fyneui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The marker of a terminal tab agents may read (H-83, owner; engine/agentread.go): while the tab's
// "Agents may read" switch is on, its tab carries an eye after the title in the warning colour, and
// at each agent's read (engine.EventDockRead) the tab flashes, tinted in that colour, for
// agentReadFlash. When the switch turned itself off, after 15 minutes without a read or one hour
// after it went on (A3), the strip says which beside the switch while that tab is the active one,
// until the user turns it on again. The switch, its end and that it turned off are the engine's
// state (DockTab.AgentRead, AgentReadUntil, AgentReadExpired, AgentReadCapped); the flash is only
// this window's reaction to the event.

// agentReadExpiredNote is the strip's line for a tab whose switch turned itself off for want of a
// read; agentReadCappedNote for one that reached its hour (A3).
const (
	agentReadExpiredNote = "Turned off after 15 minutes without an agent's read"
	agentReadCappedNote  = "Turned off one hour after it was turned on"
)

// agentReadFlash is how long a tab shows an agent's read.
const agentReadFlash = 700 * time.Millisecond

// agentReadMarks holds the dock's tab buttons by tab id, the tabs flashing now and the strip's
// note. Main goroutine.
type agentReadMarks struct {
	note     *textLine
	buttons  map[string]*tabButton
	flashes  map[string]uint64 // tab id → the flash showing on it
	gen      uint64
	flashFor time.Duration
}

func newAgentReadMarks() agentReadMarks {
	note := newTextLine(agentReadExpiredNote, colorMuted, theme.SizeNameText, fyne.TextStyle{})
	note.Hide()
	return agentReadMarks{note: note, buttons: map[string]*tabButton{}, flashes: map[string]uint64{}, flashFor: agentReadFlash}
}

// mark sets a new tab button's marker from its tab, and keeps the button for a flash: the marker
// while agents may read the terminal tab, tinted while a flash of it shows.
func (m *agentReadMarks) mark(b *tabButton, t engine.DockTab) {
	b.agentRead = engine.IsTerminalTab(t.Type) && t.AgentRead
	_, flashing := m.flashes[t.ID]
	b.flash = b.agentRead && flashing
	m.buttons[t.ID] = b
}

// showNote shows the strip's note when the active tab's switch turned itself off.
func (m *agentReadMarks) showNote(active *engine.DockTab, open bool) {
	if open && active != nil && engine.IsTerminalTab(active.Type) && active.AgentReadExpired {
		text := agentReadExpiredNote
		if active.AgentReadCapped {
			text = agentReadCappedNote
		}
		m.note.SetText(text)
		m.note.Show()
		return
	}
	m.note.Hide()
}

// flashAgentRead flashes the tab an agent read (EventDockRead), if it shows the marker; a read
// during a flash starts it again. Main goroutine.
func (d *dock) flashAgentRead(id string) {
	m := &d.agentReadMarks
	b, ok := m.buttons[id]
	if !ok || !b.agentRead {
		return
	}
	m.gen++
	gen := m.gen
	m.flashes[id] = gen
	b.flash = true
	b.Refresh()
	time.AfterFunc(m.flashFor, func() {
		d.sh.do(func() {
			if m.flashes[id] != gen {
				return // a later read flashes it still, or the dock forgot it
			}
			delete(m.flashes, id)
			if b, ok := m.buttons[id]; ok {
				b.flash = false
				b.Refresh()
			}
		})
	})
}

// refreshAgentReadMark paints a tab button's marker and, during a flash, tints the tab in the
// warning colour (tabButtonRenderer.Refresh).
func refreshAgentReadMark(th fyne.Theme, b *tabButton) {
	if !b.agentRead {
		b.mark.set(th, nil, colorMuted)
		return
	}
	b.mark.set(th, theme.VisibilityIcon(), colorStatusWarning)
	if b.flash {
		b.bg.FillColor = withAlpha(themeColor(th, colorStatusWarning), 0x59)
		b.under.FillColor = themeColor(th, colorStatusWarning)
	}
}
