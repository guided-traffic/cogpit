package engine

import (
	"fmt"
	"runtime/debug"
	"sync"
	"time"
)

// A terminal tab agents may read (H-83, owner; D-44): while its "Agents may read" switch is on, the
// tab carries a marker, which flashes at each read by an agent (EventDockRead, the window's
// reaction); and the switch turns itself off once AgentReadIdle passed without an agent's read, or
// at the latest AgentReadMax after it went on whatever the reads (A3, owner), which the tab then
// says (DockTab.AgentReadExpired, AgentReadCapped) until the user turns it on again. The switch,
// the last read, the time it turns off and that it did are UI state, so agents see them in ui_state
// as the window does. The time comes from the session's clock (Options.Now) and the expiry from
// Options.AfterFunc, so tests move the clock instead of waiting.
//
// What counts as a read is one the switch allowed: a dock.read of the tab by anyone but the user's
// window, an agent or the CLI forwarding to the window. The user's own reads change nothing.

// AgentReadIdle is how long a terminal tab's switch stays on without an agent's read;
// AgentReadMax how long it stays on at most, whatever the reads (A3).
const (
	AgentReadIdle = 15 * time.Minute
	AgentReadMax  = time.Hour
)

// EventDockRead: an agent read a terminal tab whose switch is on (dock.read); Event.View holds the
// tab id. The window flashes the tab's marker. Re-read nothing: the UI state has the read's time.
const EventDockRead EventType = "dockRead"

// setAgentRead turns a terminal tab's switch on or off at now: on, it starts the idle period and
// clears a note that it had turned itself off; off, it forgets the reads.
func setAgentRead(t *DockTab, on bool, now time.Time) {
	t.AgentRead, t.AgentReadExpired, t.AgentReadCapped, t.AgentLastRead = on, false, false, time.Time{}
	t.AgentReadOn, t.AgentReadUntil = time.Time{}, time.Time{}
	if on {
		t.AgentReadOn, t.AgentReadUntil = now, agentReadEnd(now, now)
	}
}

// agentReadEnd is when a switch that went on at on, last read (or turned on) at last, turns
// itself off: AgentReadIdle after last, at the latest AgentReadMax after on.
func agentReadEnd(on, last time.Time) time.Time {
	end := last.Add(AgentReadIdle)
	if limit := on.Add(AgentReadMax); limit.Before(end) {
		return limit
	}
	return end
}

// noteAgentRead records a read of tab that its switch allowed: for anyone but the user's window
// reading a terminal tab, the tab's last read and the switch's end move, and the window is told to
// flash the tab. The switch is checked again in the same change: a read that its turning off
// overtook, by the user or by its idle end, returns ErrTerminalOutputWithheld. It returns the tab
// as it is now.
func (s *Session) noteAgentRead(tab DockTab, gui bool) (DockTab, error) {
	if gui || !IsTerminalTab(tab.Type) {
		return tab, nil
	}
	now := s.Now()
	out, err := DockTab{}, ErrTerminalOutputWithheld
	s.UpdateUI(func(u *UIState) {
		for i := range u.Dock {
			t := &u.Dock[i]
			if t.ID != tab.ID || !t.AgentRead {
				continue
			}
			t.AgentLastRead, t.AgentReadUntil = now, agentReadEnd(t.AgentReadOn, now)
			out, err = *t, nil
		}
	})
	if err != nil {
		return DockTab{}, err
	}
	s.armAgentReadExpiry()
	s.emit(Event{Type: EventDockRead, View: tab.ID})
	return out, nil
}

// agentReadExpiry is the one timer that turns the switches off: armed for the earliest end of a
// switch that is on, re-armed after every change.
type agentReadExpiry struct {
	mu     sync.Mutex
	cancel func() bool
	closed bool
}

// stop ends the timer for good (Session.Close).
func (e *agentReadExpiry) stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
}

// armAgentReadExpiry sets the timer for the earliest end of a switch that is on, or stops it when
// none is. Every change of a switch or its end calls it afterwards; it reads the UI state under the
// timer's lock, so that the last call, which saw the last change, sets the timer.
func (s *Session) armAgentReadExpiry() {
	e := &s.agentReads
	e.mu.Lock()
	defer e.mu.Unlock()
	var next time.Time
	for _, t := range s.UIState().Dock {
		if t.AgentRead && !t.AgentReadUntil.IsZero() && (next.IsZero() || t.AgentReadUntil.Before(next)) {
			next = t.AgentReadUntil
		}
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if e.closed || next.IsZero() {
		return
	}
	e.cancel = s.opts.AfterFunc(max(next.Sub(s.Now()), 0), s.expireAgentReads)
}

// expireAgentReads turns off every switch whose end has come, marks the tab, and arms the timer
// for the next. It runs on the timer's goroutine and recovers from a panic there.
func (s *Session) expireAgentReads() {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("agent-read expiry panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	now := s.Now()
	due := func(t DockTab) bool {
		return t.AgentRead && !t.AgentReadUntil.IsZero() && !now.Before(t.AgentReadUntil)
	}
	expired := false
	for _, t := range s.UIState().Dock {
		expired = expired || due(t)
	}
	if expired {
		s.UpdateUI(func(u *UIState) {
			for i := range u.Dock {
				if t := &u.Dock[i]; due(*t) {
					capped := !t.AgentReadOn.IsZero() && !now.Before(t.AgentReadOn.Add(AgentReadMax))
					setAgentRead(t, false, now)
					t.AgentReadExpired, t.AgentReadCapped = true, capped
				}
			}
		})
	}
	s.armAgentReadExpiry()
}
