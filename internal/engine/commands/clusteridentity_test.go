package commands_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestClusterIdentityIsTheUsers (H-125, the owner's decision of 2026-10-19): a cluster's display
// name and icon colour are set and cleared only in the window, and its icon reset there too. Agents
// and the CLI are refused either way, also where the user allowed agent writes, before anything is
// written or anyone asked, a change that names other fields too as a whole; the other fields stay
// theirs. Agents read the name and the colour, and the tools say who sets them.
func TestClusterIdentityIsTheUsers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	h := newHarness(t, seed(), engine.Options{ClusterSettings: engine.NewClusterSettingsStore(dir)})
	c := &confirmations{answer: true}
	h.s.SetConfirmer(c)
	file := filepath.Join(dir, engine.ClusterSettingsFile)
	agents := []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI}

	refused := func(when string) {
		t.Helper()
		for _, o := range agents {
			for _, p := range []map[string]any{
				{"name": "dev"}, {"name": ""}, {"iconColor": "green"}, {"iconColor": ""},
				{"name": "dev", "terminalDir": "$HOME/agent"},
			} {
				if err := h.run(t, o, "cluster.settings.set", p, nil); !errors.Is(err, engine.ErrClusterIdentityGUIOnly) {
					t.Errorf("%s: %s set %v: %v, want ErrClusterIdentityGUIOnly", when, o, p, err)
				}
			}
			err := h.run(t, o, "cluster.settings.resetIcon", map[string]any{}, nil)
			var ge *engine.GUIOnlyError
			if !errors.As(err, &ge) || !errors.Is(err, engine.ErrClusterIdentityGUIOnly) {
				t.Errorf("%s: %s reset the icon: %v, want a GUI-only refusal", when, o, err)
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

	// The user's own, in the window.
	var view engine.ClusterSettingsView
	h.must(t, engine.OriginGUI, "cluster.settings.set", map[string]any{"name": "Production", "iconColor": "red"}, &view)
	if view.Name != "Production" || view.IconColor != "red" {
		t.Fatalf("the user's name and colour: %+v", view)
	}

	// Agents read them, cannot change them, even with agent writes allowed; the rest is theirs.
	h.must(t, engine.OriginGUI, "cluster.settings.setAgentWrites", map[string]any{"allowed": true}, nil)
	refused("agent writes allowed")
	h.must(t, engine.OriginExternalAgent, "cluster.settings.get", map[string]any{}, &view)
	if view.Name != "Production" || view.IconColor != "red" || view.TerminalDir != "" {
		t.Errorf("an agent reads %+v (a refused change kept nothing)", view)
	}
	h.must(t, engine.OriginExternalAgent, "cluster.settings.set", map[string]any{"terminalDir": "$HOME/src"}, &view)
	if view.TerminalDir != "$HOME/src" || view.Name != "Production" || view.IconColor != "red" {
		t.Errorf("an agent's terminal directory: %+v", view)
	}
	if got := h.s.UIState().ClusterSettings["test"]; got.Name != "Production" || got.IconColor != "red" {
		t.Errorf("ui_state's identity: %+v", got)
	}

	// The user resets the icon in the window.
	var reset engine.ClusterSettingsView // iconColor is left out when none: a fresh value
	h.must(t, engine.OriginGUI, "cluster.settings.resetIcon", map[string]any{}, &reset)
	if reset.IconColor != "" || reset.Name != "Production" {
		t.Errorf("after the user's reset: %+v", reset)
	}

	// What an agent learns from the tools: the fields and the reset say they are the user's.
	set, _ := h.s.Registry().Get("cluster.settings.set")
	for _, field := range []string{"name", "iconColor"} {
		if d := set.Schema.Properties[field].Description; !strings.Contains(d, "refused for agents") {
			t.Errorf("cluster_settings_set's %s does not say it is refused for agents: %q", field, d)
		}
	}
	if reset, _ := h.s.Registry().Get("cluster.settings.resetIcon"); !reset.GUIOnly || !strings.Contains(reset.ToolDescription(), "GUI only") {
		t.Errorf("cluster_settings_resetIcon is not described as GUI only: %q", reset.ToolDescription())
	}
}
