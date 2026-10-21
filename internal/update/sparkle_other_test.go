//go:build (!darwin || !cgo) && !linux && !windows

package update

import (
	"errors"
	"testing"
)

// Outside macOS (and without cgo) there is no updater: New says so, and nothing it offers runs.
func TestNoUpdaterOutsideMacOS(t *testing.T) {
	u := New()
	if err := u.Unavailable(); !errors.Is(err, ErrNotMacOS) {
		t.Fatalf("Unavailable() = %v, want ErrNotMacOS", err)
	}
	u.Start(true)
	u.SetAutomaticChecks(false)
	if err := u.CheckForUpdates(); !errors.Is(err, ErrNotMacOS) {
		t.Errorf("CheckForUpdates() = %v", err)
	}
	var none *Updater
	if err := none.Unavailable(); !errors.Is(err, ErrNotMacOS) {
		t.Errorf("a nil updater: %v", err)
	}
}
