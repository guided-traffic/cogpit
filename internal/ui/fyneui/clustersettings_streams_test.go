package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Cluster Settings › General › Debug (D-135): "Use initial streaming (BETA)" off by default, a tap
// switches it on and off, a change from elsewhere shows at once, and the box under it stays as it is.
func TestClusterSettingsStreams(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "general"})
	q.until(t, "the row", func() bool {
		_, ok := sh.settings.controls["streamInitialLists"].(*widget.Check)
		return ok
	})
	check := sh.settings.controls["streamInitialLists"].(*widget.Check)
	desc := sh.settings.controls["streamsText"].(*paragraph)
	if check.Text != "Use initial streaming (BETA)" || check.Checked || desc.text != streamsText {
		t.Errorf("label %q, checked %v, box %q", check.Text, check.Checked, desc.text)
	}
	test.Tap(check)
	q.until(t, "on", func() bool { return s.UIState().ClusterSettings["kind-dev"].Streaming && check.Checked })
	test.Tap(check)
	q.until(t, "off", func() bool { return !s.UIState().ClusterSettings["kind-dev"].Streaming && !check.Checked })
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "streamInitialLists": true})
	q.until(t, "on from elsewhere", func() bool { return check.Checked })
	if desc.text != streamsText {
		t.Errorf("the box changed: %q", desc.text)
	}
}

// The setting off and on, dark and light: the PNGs of the pull request (AGENTS.md rule 7).
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
				return ok && !c.Checked
			})
			bottom()
			savePNG(t, w, "cluster_settings_streams_off_"+string(mode)+".png")
			if _, err := s.ChangeClusterSettings(ctx, func(cs *engine.ClusterSettings) error {
				cs.Streaming = true
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			q.until(t, "on", func() bool { return sh.settings.controls["streamInitialLists"].(*widget.Check).Checked })
			bottom()
			savePNG(t, w, "cluster_settings_streams_on_"+string(mode)+".png")
		})
	}
}

// The text names no other product and points at Kubernetes' feature (the owner, 2026-10-26).
func TestStreamingText(t *testing.T) {
	if strings.Contains(streamsText, "Lens") || strings.Contains(streamsText, "kubectl") || !strings.Contains(streamsText, "WatchListClient") {
		t.Errorf("text %q", streamsText)
	}
}
