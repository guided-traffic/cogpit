package commands_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestTabMoveCommands (GW4): an agent moves the tab bar's tabs and the dock's as the window's drag
// does, without the user's acceptance (UI commands, D-2); Move Tab Left and Right step the active
// tab and are greyed with the reason at the bar's ends; a place outside the bar is refused.
func TestTabMoveCommands(t *testing.T) {
	h, _ := auditHarness(t)
	agent, gui := engine.OriginExternalAgent, engine.OriginGUI
	h.must(t, gui, "ui.tab.open", map[string]any{"view": "services", "fixed": true}, nil)
	h.must(t, gui, "ui.tab.open", map[string]any{"view": "deployments", "fixed": true}, nil)
	h.tabs(t, "Pods - test (preview)", "Services - test", "Deployments - test *")

	h.must(t, agent, "ui.tab.move", map[string]any{"id": "tab-1", "index": 3}, nil)
	h.tabs(t, "Services - test", "Deployments - test *", "Pods - test (preview)")
	h.must(t, agent, "ui.tab.move", map[string]any{"index": 1}, nil) // the active tab
	h.tabs(t, "Deployments - test *", "Services - test", "Pods - test (preview)")
	if err := h.run(t, agent, "ui.tab.move", map[string]any{"id": "tab-1", "index": 4}, nil); err == nil || !strings.Contains(err.Error(), "shows 3 tabs") {
		t.Errorf("a place past the bar: %v", err)
	}
	if err := h.run(t, agent, "ui.tab.move", map[string]any{"id": "tab-9", "index": 1}, nil); !errors.Is(err, engine.ErrUnknownTab) {
		t.Errorf("an unknown tab: %v", err)
	}

	var dis *engine.DisabledError
	if err := h.run(t, gui, "ui.tab.moveLeft", nil, nil); !errors.As(err, &dis) || !strings.Contains(dis.Reason, "first") {
		t.Errorf("Move Tab Left on the first tab: %v", err)
	}
	h.must(t, agent, "ui.tab.moveRight", nil, nil)
	h.tabs(t, "Services - test", "Deployments - test *", "Pods - test (preview)")
	h.must(t, gui, "ui.tab.moveRight", nil, nil)
	h.tabs(t, "Services - test", "Pods - test (preview)", "Deployments - test *")
	if err := h.run(t, gui, "ui.tab.moveRight", nil, nil); !errors.As(err, &dis) || !strings.Contains(dis.Reason, "last") {
		t.Errorf("Move Tab Right on the last tab: %v", err)
	}
	h.must(t, gui, "ui.tab.moveLeft", nil, nil)
	h.tabs(t, "Services - test", "Deployments - test *", "Pods - test (preview)")

	// The dock: the Audit Log (no cluster's) and a terminal of the shown cluster.
	h.must(t, gui, "audit.open", struct{}{}, nil)
	h.must(t, gui, "terminal.open", struct{}{}, nil)
	dockIDs := func() []string {
		var out []string
		for _, d := range h.s.UIState().Dock {
			out = append(out, d.ID)
		}
		return out
	}
	before := dockIDs()
	if len(before) != 2 {
		t.Fatalf("dock %v", before)
	}
	h.must(t, agent, "dock.move", map[string]any{"id": before[1], "index": 1}, nil)
	if got := dockIDs(); !slices.Equal(got, []string{before[1], before[0]}) {
		t.Fatalf("dock after the move %v", got)
	}
	if err := h.run(t, agent, "dock.move", map[string]any{"id": before[0], "index": 3}, nil); err == nil {
		t.Error("a dock place past the dock accepted")
	}
}
