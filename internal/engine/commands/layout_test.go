package commands_test

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The layout toggles are UI commands: the user and agents run them alike (an agent without a
// confirmation, D-2), ui.state reports what they set, and ui.toggleDock opens a terminal while the
// dock has no tabs.
func TestLayoutToggles(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	state := func(origin engine.Origin, id string, params any) engine.UIState {
		t.Helper()
		var u engine.UIState // fresh: the flags are omitted when false
		h.must(t, origin, id, params, &u)
		return u
	}
	if !state(engine.OriginGUI, "ui.toggleNavigator", struct{}{}).NavigatorHidden {
		t.Fatal("ui.toggleNavigator did not hide the navigator")
	}
	if state(engine.OriginExternalAgent, "ui.toggleNavigator", struct{}{}).NavigatorHidden {
		t.Fatal("an agent's ui.toggleNavigator did not show the navigator")
	}
	state(engine.OriginBuiltInAgent, "ui.toggleNavigator", map[string]any{"open": false})
	if !state(engine.OriginBuiltInAgent, "ui.toggleNavigator", map[string]any{"open": false}).NavigatorHidden {
		t.Fatal("ui.toggleNavigator {open: false} twice did not leave the navigator hidden")
	}
	if st := state(engine.OriginGUI, "ui.state", struct{}{}); !st.NavigatorHidden {
		t.Fatal("ui.state does not report the hidden navigator")
	}

	// An empty dock: the toggle opens a terminal tab and the dock with it, for every origin, as
	// terminal.open (a UI command too) does (the owner's WF18).
	if u := state(engine.OriginExternalAgent, "ui.toggleDock", struct{}{}); len(u.Dock) != 1 || u.Dock[0].Type != engine.DockTerminal || !u.DockOpen {
		t.Fatalf("ui.toggleDock without tabs: dock %+v open %v, want a terminal tab in the open dock", u.Dock, u.DockOpen)
	}
	if state(engine.OriginExternalAgent, "ui.toggleDock", struct{}{}).DockOpen {
		t.Fatal("an agent's ui.toggleDock did not collapse the dock")
	}
	if !state(engine.OriginGUI, "ui.toggleDock", map[string]any{"open": true}).DockOpen {
		t.Fatal("ui.toggleDock {open: true} did not open the dock")
	}
	if st := state(engine.OriginGUI, "ui.state", struct{}{}); !st.DockOpen {
		t.Fatal("ui.state does not report the open dock")
	}
}
