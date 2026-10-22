package fyneui

import (
	"testing"

	"fyne.io/fyne/v2/test"
)

// TestTerminalChipOpensATerminal: the chip after the dock's tabs opens a terminal tab with one tap,
// without a menu. Where terminal.open is disabled, the engine's Dispatch refuses it with a
// DisabledError that shell.showError toasts, as for ⌘⇧T; the fake engine does not check Enabled.
func TestTerminalChipOpensATerminal(t *testing.T) {
	f := connectedFake(t, 20)
	f.mu.Lock()
	f.terminalCallsOnly = true
	f.mu.Unlock()
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the terminal chip", func() bool { return sh.dock.terminal.Visible() })
	settle(w)
	lookPNG(t, w, "dock_strip_chips.png") // the strip without tabs: the terminal chip left, the create chip right

	overlays := len(w.Canvas().Overlays().List())
	test.Tap(sh.dock.terminal)
	q.until(t, "a terminal", func() bool { return len(f.callsOf("terminal.open")) == 1 })
	if n := len(w.Canvas().Overlays().List()); n != overlays {
		t.Errorf("the chip opened %d overlays, want none", n-overlays)
	}
}

// TestTerminalChipHiddenWithoutCluster: without a connected cluster there is nothing for a
// terminal's kubectl to talk to, so the strip shows no chip, as it shows no create chip.
func TestTerminalChipHiddenWithoutCluster(t *testing.T) {
	f := newFakeEngine(t)
	f.contexts = fixtureContexts()
	sh, _, q := newTestShell(t, f, Options{})
	q.drain()
	if sh.dock.terminal.Visible() {
		t.Error("the terminal chip shows without a cluster")
	}
}
