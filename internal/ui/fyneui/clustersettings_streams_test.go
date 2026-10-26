package fyneui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// cogpitStreamsOff is the switch Cogpit makes after a stall whose list answered at once.
func cogpitStreamsOff() *engine.StreamsOff {
	return &engine.StreamsOff{By: engine.StreamsOffByCogpit, At: time.Date(2026, 10, 26, 16, 31, 4, 0, time.UTC),
		Why: "pods in team-a: 33 objects received, then nothing for 10s before the end of the initial list; a list answered in 80 ms"}
}

// Cluster Settings › General › Streamed initial lists (D-new): on by default, a tap switches it off
// and on, and a switch Cogpit makes shows at once with when and why under it.
func TestClusterSettingsStreams(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "general"})
	q.until(t, "the row", func() bool {
		c, ok := sh.settings.controls["streamInitialLists"].(*widget.Check)
		return ok && c.Checked
	})
	check := sh.settings.controls["streamInitialLists"].(*widget.Check)
	note := sh.settings.controls["streamsOff"].(*paragraph)
	if check.Text != streamsCheck || note.Visible() {
		t.Errorf("label %q, note shown %v", check.Text, note.Visible())
	}
	test.Tap(check)
	q.until(t, "off", func() bool {
		off := s.UIState().ClusterSettings["kind-dev"].StreamsOff
		return off != nil && off.By == engine.StreamsOffByUser && !check.Checked && note.Visible()
	})
	if !strings.HasSuffix(note.text, "switched off by you.") {
		t.Errorf("note %q", note.text)
	}
	test.Tap(check)
	q.until(t, "on", func() bool {
		return s.UIState().ClusterSettings["kind-dev"].StreamsOff == nil && check.Checked && !note.Visible()
	})
	if _, err := s.ChangeClusterSettings("kind-dev", func(cs *engine.ClusterSettings) error {
		cs.StreamsOff = cogpitStreamsOff()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "off by Cogpit", func() bool { return !check.Checked && note.Visible() })
	if !strings.Contains(note.text, "switched off by Cogpit: pods in team-a: 33 objects received") {
		t.Errorf("note %q", note.text)
	}
}

// The setting switched off by Cogpit, dark and light: the PNGs of the pull request (AGENTS.md rule 7).
func TestRenderClusterSettingsStreamsOff(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		t.Run(string(mode), func(t *testing.T) {
			s, ctx, _ := settingsEngineWithFile(t)
			if _, err := s.ChangeClusterSettings(ctx, func(cs *engine.ClusterSettings) error {
				cs.StreamsOff = cogpitStreamsOff()
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			sh, w, q := openGeneral(t, s, ctx, mode)
			q.until(t, "the note", func() bool {
				n, ok := sh.settings.controls["streamsOff"].(*paragraph)
				return ok && n.Visible()
			})
			settle(w)
			sh.settings.scroll.ScrollToBottom() // the setting is the section's last
			savePNG(t, w, "cluster_settings_streams_off_"+string(mode)+".png")
		})
	}
}
