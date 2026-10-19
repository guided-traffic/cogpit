package commands_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestAccentColorIsTheUsers (D-80): a cluster's accent colour is set and cleared only in the
// window. Agents and the CLI are refused either way, also where the user allowed agent writes,
// before anything is written or anyone asked; cluster.settings.set has no field for it, and
// resetting the icon leaves it. The user's colour is kept in cluster-settings.json and read back by
// agents in cluster_settings_get and ui_state.
func TestAccentColorIsTheUsers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	h := newHarness(t, seed(), engine.Options{ClusterSettings: engine.NewClusterSettingsStore(dir)})
	c := &confirmations{answer: true}
	h.s.SetConfirmer(c)
	file := filepath.Join(dir, engine.ClusterSettingsFile)

	refused := func(when string) {
		t.Helper()
		for _, o := range []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI} {
			for _, color := range []string{"green", ""} {
				err := h.run(t, o, "cluster.settings.setAccentColor", map[string]any{"color": color}, nil)
				var ge *engine.GUIOnlyError
				if !errors.As(err, &ge) || !errors.Is(err, engine.ErrAccentColorGUIOnly) {
					t.Errorf("%s: %s setting %q: %v, want ErrAccentColorGUIOnly", when, o, color, err)
				}
			}
			if err := h.run(t, o, "cluster.settings.set", map[string]any{"accentColor": "green"}, nil); err == nil {
				t.Errorf("%s: %s set accentColor through cluster.settings.set", when, o)
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

	var view engine.ClusterSettingsView
	h.must(t, engine.OriginGUI, "cluster.settings.setAccentColor", map[string]any{"color": " RED "}, &view)
	if view.AccentColor != "red" || view.Context != "test" {
		t.Errorf("the settings after the user's colour: %+v", view)
	}
	if b, err := os.ReadFile(file); err != nil || !strings.Contains(string(b), `"accentColor": "red"`) {
		t.Fatalf("cluster-settings.json: %s, %v", b, err)
	}
	if got := h.s.UIState().ClusterSettings["test"].AccentColor; got != "red" {
		t.Errorf("ui_state's accent colour %q", got)
	}

	// Agents read it, cannot change it, even with agent writes allowed, and an icon reset leaves it
	// (the icon too is the user's since H-125: TestClusterIdentityIsTheUsers).
	h.must(t, engine.OriginGUI, "cluster.settings.setAgentWrites", map[string]any{"allowed": true}, nil)
	refused("agent writes allowed")
	h.must(t, engine.OriginExternalAgent, "cluster.settings.get", map[string]any{}, &view)
	if view.AccentColor != "red" {
		t.Errorf("an agent reads %+v", view)
	}
	h.must(t, engine.OriginGUI, "cluster.settings.set", map[string]any{"iconColor": "green", "name": "Shop"}, nil)
	h.must(t, engine.OriginGUI, "cluster.settings.resetIcon", map[string]any{}, &view)
	if view.AccentColor != "red" {
		t.Errorf("the accent colour after the user's icon changes: %q", view.AccentColor)
	}

	// The user's own: a free colour, a colour the window does not know, and none again.
	h.must(t, engine.OriginGUI, "cluster.settings.setAccentColor", map[string]any{"color": "#FF8800"}, &view)
	if view.AccentColor != "#ff8800" {
		t.Errorf("a free colour: %q", view.AccentColor)
	}
	if err := h.run(t, engine.OriginGUI, "cluster.settings.setAccentColor", map[string]any{"color": "pink"}, nil); err == nil ||
		!strings.Contains(err.Error(), "accent colour") {
		t.Errorf("pink: %v", err)
	}
	var cleared engine.ClusterSettingsView // accentColor is left out when none: a fresh value
	h.must(t, engine.OriginGUI, "cluster.settings.setAccentColor", map[string]any{"color": ""}, &cleared)
	if cleared.Context != "test" || cleared.AccentColor != "" || h.s.UIState().ClusterSettings["test"].AccentColor != "" {
		t.Errorf("cleared: %+v", cleared)
	}
}
