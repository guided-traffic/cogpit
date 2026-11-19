package fyneui

import (
	"encoding/json"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestWelcomeRestoreBoxIsThePreference is the owner's WF1: the welcome page has a checkbox for
// "Restore the last session"; ticking it turns the preference on (the Preferences show it), and the
// preference turned off elsewhere unticks it.
func TestWelcomeRestoreBoxIsThePreference(t *testing.T) {
	s := welcomeEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	w := welcomeShown(t, sh, q)
	if w.restore.Checked || w.restore.Text != welcomeRestoreLabel {
		t.Fatalf("box %q checked %v, want off by default", w.restore.Text, w.restore.Checked)
	}
	test.Tap(w.restore)
	q.until(t, "the preference on", func() bool {
		pr := s.UIState().Preferences
		return pr != nil && pr.RestoreSession
	})
	if _, err := s.Dispatch(sh.ctx, engine.Invocation{ID: "preferences.set", Params: json.RawMessage(`{"restoreSession":false}`), Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "the box to follow", func() bool {
		w.render()
		return !w.restore.Checked
	})
	if pr := s.UIState().Preferences; pr == nil || pr.RestoreSession {
		t.Error("following the preference wrote it back on")
	}
}
