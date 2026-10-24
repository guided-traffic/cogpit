package commands_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestLightStripIsTheUsers (D-new): a cluster's light strip is chosen only in the window. Agents
// and the CLI are refused for every animation, also where the user allowed agent writes, before
// anything is written or anyone asked; cluster.settings.set has no field for it, and resetting the
// icon or clearing the accent colour leaves it. Without an accent colour the window's choice is
// disabled with its reason. The user's choice is kept in cluster-settings.json and read back by
// agents in cluster_settings_get and ui_state.
func TestLightStripIsTheUsers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	h := newHarness(t, seed(), engine.Options{ClusterSettings: engine.NewClusterSettingsStore(dir)})
	c := &confirmations{answer: true}
	h.s.SetConfirmer(c)
	file := filepath.Join(dir, engine.ClusterSettingsFile)

	refused := func(when string) {
		t.Helper()
		for _, o := range []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI} {
			for _, anim := range []string{"static", "flow", ""} {
				err := h.run(t, o, "cluster.settings.setLightStrip", map[string]any{"animation": anim}, nil)
				var ge *engine.GUIOnlyError
				if !errors.As(err, &ge) || !errors.Is(err, engine.ErrLightStripGUIOnly) {
					t.Errorf("%s: %s choosing %q: %v, want ErrLightStripGUIOnly", when, o, anim, err)
				}
			}
			if err := h.run(t, o, "cluster.settings.set", map[string]any{"lightStrip": "static"}, nil); err == nil {
				t.Errorf("%s: %s set lightStrip through cluster.settings.set", when, o)
			}
		}
		if len(c.all()) != 0 {
			t.Errorf("%s: the user was asked: %+v", when, c.all())
		}
	}
	refused("before any setting")
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("a refused change wrote the settings file: %v", err)
	}

	// Without an accent colour the window's choice is disabled, with the reason it shows.
	err := h.run(t, engine.OriginGUI, "cluster.settings.setLightStrip", map[string]any{"animation": "scanner"}, nil)
	if !errors.Is(err, engine.ErrLightStripNoAccent) {
		t.Errorf("without an accent colour: %v, want ErrLightStripNoAccent", err)
	}

	var view engine.ClusterSettingsView
	h.must(t, engine.OriginGUI, "cluster.settings.setAccentColor", map[string]any{"color": "red"}, &view)
	if view.LightStrip != engine.LightStripStatic {
		t.Errorf("an accent colour's strip by default: %q, want static", view.LightStrip)
	}
	h.must(t, engine.OriginGUI, "cluster.settings.setLightStrip", map[string]any{"animation": " Scanner "}, &view)
	if view.LightStrip != "scanner" || view.Context != "test" {
		t.Errorf("the settings after the user's choice: %+v", view)
	}
	if b, err := os.ReadFile(file); err != nil || !strings.Contains(string(b), `"lightStrip": "scanner"`) {
		t.Fatalf("cluster-settings.json: %s, %v", b, err)
	}
	if got := h.s.UIState().ClusterSettings["test"].LightStrip; got != "scanner" {
		t.Errorf("ui_state's light strip %q", got)
	}

	// Agents read it, cannot change it, even with agent writes allowed; an icon reset and a cleared
	// accent colour leave it, so that the user's choice is there again with a colour.
	h.must(t, engine.OriginGUI, "cluster.settings.setAgentWrites", map[string]any{"allowed": true}, nil)
	refused("agent writes allowed")
	h.must(t, engine.OriginExternalAgent, "cluster.settings.get", map[string]any{}, &view)
	if view.LightStrip != "scanner" {
		t.Errorf("an agent reads %+v", view)
	}
	h.must(t, engine.OriginGUI, "cluster.settings.resetIcon", map[string]any{}, &view)
	h.must(t, engine.OriginGUI, "cluster.settings.setAccentColor", map[string]any{"color": ""}, &view)
	if view.LightStrip != "scanner" {
		t.Errorf("the light strip after the icon's reset and the colour cleared: %q", view.LightStrip)
	}

	// The user's own: static again (kept as none), and an animation the window does not know.
	h.must(t, engine.OriginGUI, "cluster.settings.setAccentColor", map[string]any{"color": "#ff8800"}, nil)
	h.must(t, engine.OriginGUI, "cluster.settings.setLightStrip", map[string]any{"animation": "static"}, &view)
	if view.LightStrip != "static" || h.s.UIState().ClusterSettings["test"].LightStrip != "" {
		t.Errorf("static: %+v", view)
	}
	if err := h.run(t, engine.OriginGUI, "cluster.settings.setLightStrip", map[string]any{"animation": "blink"}, nil); err == nil ||
		!strings.Contains(err.Error(), "light strip") {
		t.Errorf("blink: %v", err)
	}
}
