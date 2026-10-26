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

// Cluster Settings › General › Debug (D-new): "Use initial streaming" on by default, a tap
// switches it off and on, the box under the switch describes the way chosen, and a switch Cogpit
// makes shows at once.
func TestClusterSettingsStreams(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "general"})
	q.until(t, "the row", func() bool {
		c, ok := sh.settings.controls["streamInitialLists"].(*widget.Check)
		return ok && c.Checked
	})
	check := sh.settings.controls["streamInitialLists"].(*widget.Check)
	desc := sh.settings.controls["streamsText"].(*paragraph)
	if check.Text != "Use initial streaming" || desc.text != streamingOnText {
		t.Errorf("label %q, box %q", check.Text, desc.text)
	}
	test.Tap(check)
	q.until(t, "off", func() bool {
		off := s.UIState().ClusterSettings["kind-dev"].StreamsOff
		return off != nil && off.By == engine.StreamsOffByUser && !check.Checked && desc.text == streamingOffText
	})
	test.Tap(check)
	q.until(t, "on", func() bool {
		return s.UIState().ClusterSettings["kind-dev"].StreamsOff == nil && check.Checked && desc.text == streamingOnText
	})
	if _, err := s.ChangeClusterSettings("kind-dev", func(cs *engine.ClusterSettings) error {
		cs.StreamsOff = cogpitStreamsOff()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "off by Cogpit", func() bool { return !check.Checked && desc.text == streamingOffText })
}

// The setting on and switched off by Cogpit, dark and light: the PNGs of the pull request (AGENTS.md
// rule 7).
func TestRenderClusterSettingsStreams(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		t.Run(string(mode), func(t *testing.T) {
			s, ctx, _ := settingsEngineWithFile(t)
			sh, w, q := openGeneral(t, s, ctx, mode)
			bottom := func() {
				settle(w)
				sh.settings.scroll.ScrollToBottom() // the setting is the section's last
			}
			q.until(t, "the switch", func() bool {
				c, ok := sh.settings.controls["streamInitialLists"].(*widget.Check)
				return ok && c.Checked
			})
			bottom()
			savePNG(t, w, "cluster_settings_streams_on_"+string(mode)+".png")
			if _, err := s.ChangeClusterSettings(ctx, func(cs *engine.ClusterSettings) error {
				cs.StreamsOff = cogpitStreamsOff()
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			q.until(t, "off", func() bool { return sh.settings.controls["streamsText"].(*paragraph).text == streamingOffText })
			bottom()
			savePNG(t, w, "cluster_settings_streams_off_"+string(mode)+".png")
		})
	}
}

// The texts name no other product, and their numbers are Cogpit's (the owner, 2026-10-26).
func TestStreamingTexts(t *testing.T) {
	for _, text := range []string{streamingOnText, streamingOffText} {
		if strings.Contains(text, "Lens") || strings.Contains(text, "kubectl") {
			t.Errorf("a text names another tool: %q", text)
		}
		if !strings.Contains(text, "5 requests a second") {
			t.Errorf("the rate limit is not named: %q", text)
		}
	}
	if !strings.Contains(streamingOnText, "silent for 10 s") {
		t.Errorf("the bound is not named: %q", streamingOnText)
	}
}
