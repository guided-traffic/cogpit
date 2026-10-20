package fyneui

import (
	"bytes"
	"log"
	"log/slog"
	"strings"
	"testing"
)

// Fyne's warning reaches the window through the app log: the standard log package's lines, routed
// by slog.SetDefault as cmd/cogpit does, are paired with their "From:" line, and each place is
// handed on once (D-103). Every record still reaches the log itself.
func TestFyneThreadWarningsReachTheWindow(t *testing.T) {
	var out bytes.Buffer
	calls := &strayCallLog{seen: map[string]bool{}}
	h := threadCheckHandler{Handler: slog.NewTextHandler(&out, nil), log: calls}

	prev := slog.Default()
	slog.SetDefault(slog.New(h.WithAttrs(nil)))
	defer slog.SetDefault(prev)

	var got []string
	stop := calls.watch(func(site string) { got = append(got, site) })
	fyneWarns := func(from string) { // as fyne.io/fyne/v2/internal/async.EnsureMain logs it
		log.Println("*** Error in Fyne call thread, this should have been called in fyne.Do[AndWait] ***")
		log.Println("*** This error needs to be addressed as it could cause concurrent errors soon ***")
		log.Println("*** The next major Fyne release will disable this safety by default! ***")
		log.Printf("  From: %s:%d", from, 42)
	}
	fyneWarns("/src/cogpit/internal/ui/fyneui/shell.go")
	fyneWarns("/src/cogpit/internal/ui/fyneui/shell.go") // the same place again: no second toast
	log.Println("  From: /elsewhere/not-after-a-warning.go:1")
	fyneWarns("/src/cogpit/internal/ui/fyneui/termview/logs.go")
	stop()
	fyneWarns("/src/cogpit/internal/ui/fyneui/drawer.go") // after stop: kept, not handed on

	want := []string{"/src/cogpit/internal/ui/fyneui/shell.go:42", "/src/cogpit/internal/ui/fyneui/termview/logs.go:42"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("places handed on: %q, want %q", got, want)
	}
	if n := strings.Count(out.String(), "Error in Fyne call thread"); n != 4 {
		t.Errorf("the log has %d of Fyne's warnings, want all 4:\n%s", n, out.String())
	}
}

func TestStrayCallMessage(t *testing.T) {
	msg := strayCallMessage("/Users/x/cogpit/internal/ui/fyneui/termview/logs.go:484")
	if !strings.Contains(msg, "termview/logs.go:484") || strings.Contains(msg, "/Users/x") {
		t.Errorf("message %q", msg)
	}
}

// A development build's window shows a place Fyne reported as a warning toast (D-103); a test
// binary is one, with the check on.
func TestStrayCallShowsAToast(t *testing.T) {
	sh, _, q := newTestShell(t, newFakeEngine(t), Options{})
	q.drain()
	site := "/src/cogpit/internal/ui/fyneui/teststray.go:7"
	t.Cleanup(func() { // the place stays seen in this process; the next test's shell must not care
		strayCalls.mu.Lock()
		delete(strayCalls.seen, site)
		strayCalls.mu.Unlock()
	})
	strayCalls.note("*** Error in Fyne call thread, this should have been called in fyne.Do[AndWait] ***")
	strayCalls.note("  From: " + site)
	q.until(t, "the toast", func() bool { return strings.Contains(toastTexts(sh), "fyneui/teststray.go:7") })
	if !strings.Contains(toastTexts(sh), "a release build, with the thread check off, would race") {
		t.Errorf("toasts %q", toastTexts(sh))
	}
}

// The metadata a build with the check off sets: the fyneDo migration, Cogpit's ID and name, so
// that Fyne looks for no FyneApp.toml; none with the check on (D-103).
func TestAppMetadataDeclaresTheMigration(t *testing.T) {
	if _, ok := appMetadata(true); ok {
		t.Error("a build with the check on sets metadata")
	}
	m, ok := appMetadata(false)
	if !ok || !m.Migrations["fyneDo"] || m.ID != appID || m.Name != appName {
		t.Errorf("metadata %+v, %v", m, ok)
	}
}
