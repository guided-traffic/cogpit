package commands_test

import (
	"errors"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// ui.setNavigatorScope as agents and the GUI dispatch it: up to the section and back without
// touching the connection, into another cluster through a connect; ui.state reports the scope, and
// the tab bar's index counts the scope's tabs. An agent runs it without the user's acceptance (D-2:
// a UI command; connecting only reads).
func TestNavigatorScopeCommand(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{Preferences: engine.NewPreferencesStore(t.TempDir())})
	agent, gui := engine.OriginExternalAgent, engine.OriginGUI
	var st commands.StateResult
	h.must(t, agent, "ui.state", nil, &st)
	if st.NavigatorScope != "cluster:test" {
		t.Fatalf("connected: scope %q", st.NavigatorScope)
	}
	h.must(t, gui, "ui.tab.open", map[string]any{"view": "services", "fixed": true}, nil)

	h.must(t, agent, "ui.setNavigatorScope", map[string]any{"scope": "section:local"}, &st)
	if st.NavigatorScope != engine.NavScopeLocal || st.Context != "test" || st.Connection.State != engine.Connected {
		t.Errorf("the section: scope %q, %s %q", st.NavigatorScope, st.Connection.State, st.Context)
	}
	h.must(t, agent, "ui.setNavigatorScope", map[string]any{"scope": "cluster:other"}, &st)
	if st.NavigatorScope != "cluster:other" || st.Context != "other" || st.Connection.State != engine.Connected {
		t.Fatalf("into other: scope %q, %s %q", st.NavigatorScope, st.Connection.State, st.Context)
	}
	// other's tabs only: the preview tab it took over; test's fixed Services tab is not counted.
	if err := h.run(t, agent, "ui.tab.select", map[string]any{"index": 2}, nil); !errors.Is(err, engine.ErrUnknownTab) {
		t.Errorf("tab 2 of other's one: %v", err)
	}
	// At the section the bar still shows the shown cluster's tabs (the owner's GW11); with mixed
	// environments on, which an agent may set, every cluster's, and tab 2 is test's.
	h.must(t, gui, "ui.setNavigatorScope", map[string]any{"scope": "section:local"}, nil)
	if err := h.run(t, agent, "ui.tab.select", map[string]any{"index": 2}, nil); !errors.Is(err, engine.ErrUnknownTab) {
		t.Errorf("tab 2 at the section with other's one shown: %v", err)
	}
	h.must(t, agent, "preferences.set", map[string]any{"mixedEnvironments": true}, nil)
	h.must(t, agent, "ui.tab.select", map[string]any{"index": 2}, &st)
	if st.Context != "test" || st.View != "services" || st.NavigatorScope != "cluster:test" {
		t.Errorf("tab 2 at the section with mixed environments: %q view %q scope %q", st.Context, st.View, st.NavigatorScope)
	}

	h.must(t, agent, "ui.setNavigatorScope", map[string]any{"scope": "cluster:broken"}, &st)
	if st.NavigatorScope != engine.NavScopeLocal || st.Connection.State != engine.ConnectError {
		t.Errorf("a failed connect: scope %q, %s", st.NavigatorScope, st.Connection.State)
	}
	if err := h.run(t, agent, "ui.setNavigatorScope", map[string]any{"scope": "section:teamwork"}, nil); !errors.Is(err, engine.ErrNavScope) {
		t.Errorf("an unknown section: %v", err)
	}
}

// The navigator's own state as agents and the window set it (D-51): the search, the keyboard's row
// and the collapsed source groups through their commands, all run by an agent without the user's
// acceptance (UI commands) and reported by ui.state; the palette's text with who typed it.
func TestNavigatorAndPaletteStateCommands(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	agent, gui := engine.OriginExternalAgent, engine.OriginGUI
	var st commands.StateResult
	h.must(t, agent, "ui.setNavigatorSearch", map[string]any{"search": "dep"}, &st)
	if st.Navigator.Search != "dep" {
		t.Errorf("search %q", st.Navigator.Search)
	}
	h.must(t, agent, "ui.setNavigatorRow", map[string]any{"row": "group:workloads"}, &st)
	if st.Navigator.Row != "group:workloads" {
		t.Errorf("row %q", st.Navigator.Row)
	}
	if err := h.run(t, agent, "ui.setNavigatorRow", map[string]any{"row": "context:test"}, nil); !errors.Is(err, engine.ErrNavRow) {
		t.Errorf("a section's row at a cluster: %v", err)
	}
	h.must(t, gui, "ui.setNavigatorGroup", map[string]any{"group": ".kube/", "collapsed": true}, nil)
	h.must(t, agent, "ui.state", nil, &st)
	if len(st.Navigator.Collapsed) != 1 || st.Navigator.Collapsed[0] != ".kube/" || st.Navigator.Search != "dep" {
		t.Errorf("navigator %+v", st.Navigator)
	}

	h.must(t, agent, "ui.palette.setText", map[string]any{"text": "delete"}, &st)
	if !st.Palette || st.PaletteText != "delete" || st.PaletteTextOrigin != agent {
		t.Errorf("an agent's text: open %v, %q by %q", st.Palette, st.PaletteText, st.PaletteTextOrigin)
	}
	h.must(t, gui, "ui.palette.setText", map[string]any{"text": "delete pod"}, &st)
	if st.PaletteTextOrigin != gui {
		t.Errorf("the user's text by %q", st.PaletteTextOrigin)
	}
	var closed commands.StateResult
	h.must(t, gui, "ui.palette.close", nil, &closed)
	if closed.Palette || closed.PaletteText != "" {
		t.Errorf("closed: %v %q", closed.Palette, closed.PaletteText)
	}
}
