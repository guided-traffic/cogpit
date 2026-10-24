//go:build darwin

package fyneui

import "testing"

// TestPowerChangeReachesItsHandlerOnce: IOKit's call reaches the handler the window installed when
// the computer went from its adapter to its battery or back, and not when only a battery's charge
// changed (the notification itself needs AppKit's run loop and a cable pulled: not tested here).
func TestPowerChangeReachesItsHandlerOnce(t *testing.T) {
	now := systemOnBattery() // the computer's own, read through IOKit
	t.Logf("on battery: %v", now)
	calls := 0
	powerMu.Lock()
	oldFn, oldLast := powerFn, powerLast
	powerFn, powerLast = func() { calls++ }, !now // as if the last state the window followed was the other
	powerMu.Unlock()
	t.Cleanup(func() {
		powerMu.Lock()
		powerFn, powerLast = oldFn, oldLast
		powerMu.Unlock()
	})
	cogpitPowerChanged()
	cogpitPowerChanged() // the same state again: a charge that changed
	if calls != 1 {
		t.Fatalf("%d calls, want 1", calls)
	}
}
