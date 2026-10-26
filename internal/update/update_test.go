package update

import (
	"os"
	"strings"
	"testing"
	"time"
)

// The app bundle's Info.plist gets the settings this package relies on from hack/macos/sparkle.sh:
// Sparkle's own scheduled checks off (Cogpit's run instead: the owner's report of 2026-10-26) and
// no interval of theirs, no automatic download and install, and the update verified before
// extraction against a signed feed.
func TestSparkleScriptWritesTheSettings(t *testing.T) {
	b, err := os.ReadFile("../../hack/macos/sparkle.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(b)
	for _, line := range []string{
		`plutil -replace SUEnableAutomaticChecks -bool NO "$plist"`,
		`plutil -replace SUAllowsAutomaticUpdates -bool NO "$plist"`,
		`plutil -replace SUVerifyUpdateBeforeExtraction -bool YES "$plist"`,
		`plutil -replace SURequireSignedFeed -bool YES "$plist"`,
	} {
		if !strings.Contains(script, line) {
			t.Errorf("sparkle.sh does not run %s", line)
		}
	}
	if strings.Contains(script, "plutil -replace SUScheduledCheckInterval") {
		t.Error("sparkle.sh sets SUScheduledCheckInterval: Sparkle's own scheduled checks are off, Cogpit's interval is checkInterval")
	}
}

// What Sparkle's delegate reports is kept and tells the listeners on goroutines of their own: an
// update held with its version, a repeat that changes nothing tells nobody, the session's end
// drops the version. An updater that cannot update this Cogpit never reports one and registers
// nobody.
func TestUpdateNoticeTellsTheListeners(t *testing.T) {
	notice.mu.Lock()
	available, version, listeners := notice.available, notice.version, notice.listeners
	notice.mu.Unlock()
	t.Cleanup(func() {
		notice.mu.Lock()
		notice.available, notice.version, notice.listeners = available, version, listeners
		notice.mu.Unlock()
	})
	calls := make(chan struct{}, 8)
	notice.listen(func() { calls <- struct{}{} })
	wait := func(what string) {
		t.Helper()
		select {
		case <-calls:
		case <-time.After(5 * time.Second):
			t.Fatalf("no call after %s", what)
		}
	}
	notice.set(true, "0.12.0")
	wait("the update")
	if v, ok := notice.get(); !ok || v != "0.12.0" {
		t.Fatalf("held %q %v, want 0.12.0", v, ok)
	}
	notice.set(true, "0.12.0")
	notice.set(false, "0.12.0")
	wait("the session's end")
	if v, ok := notice.get(); ok || v != "" {
		t.Fatalf("held %q %v after the session's end", v, ok)
	}
	select {
	case <-calls:
		t.Fatal("a report that changed nothing told the listeners")
	case <-time.After(50 * time.Millisecond):
	}

	none := &Updater{err: ErrNoKey}
	notice.set(true, "0.13.0")
	wait("the next update")
	if v, ok := none.AvailableUpdate(); ok || v != "" {
		t.Errorf("an updater without the key reports %q %v", v, ok)
	}
	n := len(notice.listeners)
	none.OnUpdateChange(func() {})
	var nilUpdater *Updater
	nilUpdater.OnUpdateChange(func() {})
	if len(notice.listeners) != n {
		t.Error("an updater that cannot update registered a listener")
	}
}
