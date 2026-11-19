package commands_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// fakeClock is a session clock the test moves: Now, and AfterFunc timers that fire when advance
// passes their time, on the test's goroutine.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at          time.Time
	f           func()
	done, fired bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		was := !t.done
		t.done = true
		return was
	}
}

// armed returns the times of the timers that have not fired or been stopped.
func (c *fakeClock) armed() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []time.Time
	for _, t := range c.timers {
		if !t.done {
			out = append(out, t.at)
		}
	}
	return out
}

// advance moves the clock by d and fires the timers due by then, in order.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.done && !t.at.After(c.now) {
			t.done, t.fired = true, true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.f()
	}
}

// dockTab returns the open dock tab id from the session's UI state.
func dockTab(t *testing.T, h *harness, id string) engine.DockTab {
	t.Helper()
	tab, ok := h.s.UIState().DockTab(id)
	if !ok {
		t.Fatalf("dock tab %s is not open", id)
	}
	return tab
}

// A terminal tab's "Agents may read" switch (H-83, owner): on, it ends 15 minutes later unless an
// agent reads; each agent's read moves the end and tells the window to flash the tab
// (EventDockRead); the user's own reads do neither; without a read for 15 minutes it turns itself
// off and the tab says so (agentReadExpired) until the user turns it on again; switching it off
// by hand leaves no such note. Each tab's switch ends on its own.
func TestAgentReadSwitchTurnsItselfOff(t *testing.T) {
	clock := newFakeClock()
	h := newHarness(t, seed(), engine.Options{Now: clock.Now, AfterFunc: clock.AfterFunc})
	w := &fakeWindow{}
	h.s.SetDockOutputReader(w)
	var mu sync.Mutex
	var flashes []string
	cancel := h.s.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventDockRead {
			mu.Lock()
			flashes = append(flashes, ev.View)
			mu.Unlock()
		}
	})
	defer cancel()
	flashed := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), flashes...)
	}
	t0 := clock.Now()
	id := openDockTab(t, h, "terminal.open", struct{}{})
	w.show(id, "$ ls", "$ ")

	h.must(t, engine.OriginGUI, "dock.allowAgentRead", map[string]any{"id": id, "allowed": true}, nil)
	if tab := dockTab(t, h, id); !tab.AgentRead || !tab.AgentReadUntil.Equal(t0.Add(engine.AgentReadIdle)) ||
		!tab.AgentLastRead.IsZero() || tab.AgentReadExpired {
		t.Fatalf("switched on: %+v", tab)
	}
	if got := clock.armed(); len(got) != 1 || !got[0].Equal(t0.Add(engine.AgentReadIdle)) {
		t.Fatalf("timers after switching on: %v", got)
	}

	// An agent reads after ten minutes: the end moves, the window is told.
	clock.advance(10 * time.Minute)
	out, err := readDock(t, h, engine.OriginExternalAgent, map[string]any{"id": id})
	if err != nil || len(out.Lines) != 2 {
		t.Fatalf("an agent's read: %q, %v", out.Lines, err)
	}
	read := t0.Add(10 * time.Minute)
	if tab := dockTab(t, h, id); !tab.AgentLastRead.Equal(read) || !tab.AgentReadUntil.Equal(read.Add(engine.AgentReadIdle)) ||
		!out.Tab.AgentReadUntil.Equal(tab.AgentReadUntil) {
		t.Errorf("after the read: %+v, the result's tab %+v", tab, out.Tab)
	}
	if got := flashed(); len(got) != 1 || got[0] != id {
		t.Errorf("flashes after an agent's read: %v", got)
	}
	// The user's own read moves nothing and flashes nothing.
	clock.advance(time.Minute)
	if _, err := readDock(t, h, engine.OriginGUI, map[string]any{"id": id}); err != nil {
		t.Fatal(err)
	}
	if tab := dockTab(t, h, id); !tab.AgentLastRead.Equal(read) || len(flashed()) != 1 {
		t.Errorf("the user's read: %+v, flashes %v", tab, flashed())
	}

	// Fifteen minutes after switching on, but five after the read: still on.
	clock.advance(4 * time.Minute)
	if tab := dockTab(t, h, id); !tab.AgentRead || tab.AgentReadExpired {
		t.Fatalf("15 minutes after switching on, 5 after a read: %+v", tab)
	}
	// Fifteen minutes after the read: off, and the tab says so.
	clock.advance(10*time.Minute - time.Second)
	if tab := dockTab(t, h, id); !tab.AgentRead {
		t.Fatal("off a second early")
	}
	clock.advance(time.Second)
	tab := dockTab(t, h, id)
	if tab.AgentRead || !tab.AgentReadExpired || !tab.AgentReadUntil.IsZero() || !tab.AgentLastRead.IsZero() {
		t.Fatalf("15 minutes without a read: %+v", tab)
	}
	if _, err := readDock(t, h, engine.OriginExternalAgent, map[string]any{"id": id}); !errors.Is(err, engine.ErrTerminalOutputWithheld) {
		t.Errorf("an agent's read after the switch turned itself off: %v", err)
	}
	if got := clock.armed(); len(got) != 0 {
		t.Errorf("timers with no switch on: %v", got)
	}

	// On again: the note goes. Off by hand: no note.
	h.must(t, engine.OriginGUI, "dock.allowAgentRead", map[string]any{"id": id, "allowed": true}, nil)
	if tab := dockTab(t, h, id); !tab.AgentRead || tab.AgentReadExpired {
		t.Errorf("on again: %+v", tab)
	}
	h.must(t, engine.OriginGUI, "dock.allowAgentRead", map[string]any{"id": id, "allowed": false}, nil)
	if tab := dockTab(t, h, id); tab.AgentRead || tab.AgentReadExpired || !tab.AgentReadUntil.IsZero() {
		t.Errorf("off by hand: %+v", tab)
	}

	// Two tabs: each switch ends on its own.
	other := openDockTab(t, h, "terminal.open", struct{}{})
	h.must(t, engine.OriginGUI, "dock.allowAgentRead", map[string]any{"id": id, "allowed": true}, nil)
	clock.advance(5 * time.Minute)
	h.must(t, engine.OriginGUI, "dock.allowAgentRead", map[string]any{"id": other, "allowed": true}, nil)
	clock.advance(10 * time.Minute)
	if a, b := dockTab(t, h, id), dockTab(t, h, other); a.AgentRead || !a.AgentReadExpired || !b.AgentRead {
		t.Errorf("first tab's end: %+v / %+v", a, b)
	}
	clock.advance(5 * time.Minute)
	if b := dockTab(t, h, other); b.AgentRead || !b.AgentReadExpired {
		t.Errorf("second tab's end: %+v", b)
	}
}

// A switch ends at the latest an hour after it went on, whatever the reads (A3, owner): an agent
// reading every ten minutes keeps it on until then, not longer, and the tab says it ended at its
// hour; turned on again, the hour starts again.
func TestAgentReadSwitchEndsAfterAnHour(t *testing.T) {
	clock := newFakeClock()
	h := newHarness(t, seed(), engine.Options{Now: clock.Now, AfterFunc: clock.AfterFunc})
	w := &fakeWindow{}
	h.s.SetDockOutputReader(w)
	t0 := clock.Now()
	id := openDockTab(t, h, "terminal.open", struct{}{})
	w.show(id, "$ ls", "$ ")
	h.must(t, engine.OriginGUI, "dock.allowAgentRead", map[string]any{"id": id, "allowed": true}, nil)
	if tab := dockTab(t, h, id); !tab.AgentReadOn.Equal(t0) {
		t.Fatalf("switched on: %+v", tab)
	}
	for i := 1; i <= 5; i++ { // reads at 10, 20, 30, 40 and 50 minutes
		clock.advance(10 * time.Minute)
		if _, err := readDock(t, h, engine.OriginExternalAgent, map[string]any{"id": id}); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}
	if tab := dockTab(t, h, id); !tab.AgentReadUntil.Equal(t0.Add(engine.AgentReadMax)) {
		t.Fatalf("after a read at 50 minutes the end is %v, want the hour %v", tab.AgentReadUntil, t0.Add(engine.AgentReadMax))
	}
	clock.advance(10*time.Minute - time.Second)
	if tab := dockTab(t, h, id); !tab.AgentRead {
		t.Fatal("off a second before the hour")
	}
	clock.advance(time.Second)
	tab := dockTab(t, h, id)
	if tab.AgentRead || !tab.AgentReadExpired || !tab.AgentReadCapped || !tab.AgentReadOn.IsZero() {
		t.Fatalf("at the hour: %+v", tab)
	}
	if _, err := readDock(t, h, engine.OriginExternalAgent, map[string]any{"id": id}); !errors.Is(err, engine.ErrTerminalOutputWithheld) {
		t.Errorf("an agent's read after the hour: %v", err)
	}
	// On again: a new hour, and no note; ended for want of a read, it is not capped.
	h.must(t, engine.OriginGUI, "dock.allowAgentRead", map[string]any{"id": id, "allowed": true}, nil)
	if tab := dockTab(t, h, id); !tab.AgentRead || tab.AgentReadExpired || tab.AgentReadCapped || !tab.AgentReadOn.Equal(clock.Now()) {
		t.Fatalf("on again: %+v", tab)
	}
	clock.advance(engine.AgentReadIdle)
	if tab := dockTab(t, h, id); tab.AgentRead || !tab.AgentReadExpired || tab.AgentReadCapped {
		t.Errorf("15 minutes without a read: %+v", tab)
	}
}

// Agents read while the user switches on and off and the clock moves: whatever the interleaving,
// once the last change is made the timer is armed for the switch's end, so a switch left on always
// turns itself off 15 minutes after its last read.
func TestAgentReadExpiryUnderConcurrentReads(t *testing.T) {
	clock := newFakeClock()
	h := newHarness(t, seed(), engine.Options{Now: clock.Now, AfterFunc: clock.AfterFunc})
	w := &fakeWindow{}
	h.s.SetDockOutputReader(w)
	id := openDockTab(t, h, "terminal.open", struct{}{})
	w.show(id, "$ ")
	h.must(t, engine.OriginGUI, "dock.allowAgentRead", map[string]any{"id": id, "allowed": true}, nil)

	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 25 {
				switch {
				case g == 0 && i%5 == 0:
					_, _ = h.s.AllowAgentRead(id, i%10 == 0)
				case g == 1:
					clock.advance(time.Second)
				default:
					_, _ = readDock(t, h, engine.OriginBuiltInAgent, map[string]any{"id": id})
				}
			}
		}()
	}
	wg.Wait()
	h.must(t, engine.OriginGUI, "dock.allowAgentRead", map[string]any{"id": id, "allowed": true}, nil)
	if _, err := readDock(t, h, engine.OriginExternalAgent, map[string]any{"id": id}); err != nil {
		t.Fatal(err)
	}
	end := dockTab(t, h, id).AgentReadUntil
	if got := clock.armed(); len(got) != 1 || !got[0].Equal(end) {
		t.Fatalf("timers %v, want one at the switch's end %v", got, end)
	}
	clock.advance(engine.AgentReadIdle)
	if tab := dockTab(t, h, id); tab.AgentRead || !tab.AgentReadExpired {
		t.Errorf("the switch did not turn itself off: %+v", tab)
	}
}
