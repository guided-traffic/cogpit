package commands_test

import (
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestTabCloseCommands (D-144): an agent closes the tab bar's other tabs and all of them, and the
// dock's, without the user's acceptance (UI commands, D-2); the dock's Close Others needs the tab it
// keeps.
func TestTabCloseCommands(t *testing.T) {
	h, _ := auditHarness(t)
	agent := engine.OriginExternalAgent
	h.must(t, agent, "ui.tab.open", map[string]any{"view": "services", "fixed": true}, nil)
	h.must(t, agent, "ui.tab.open", map[string]any{"view": "deployments", "fixed": true}, nil)
	h.must(t, agent, "ui.tab.closeOthers", map[string]any{"id": "tab-2"}, nil)
	h.tabs(t, "Services - test *")
	h.must(t, agent, "ui.tab.closeAll", nil, nil)
	h.tabs(t)

	h.must(t, agent, "audit.open", struct{}{}, nil)
	h.must(t, agent, "terminal.open", struct{}{}, nil)
	h.must(t, agent, "audit.open", struct{}{}, nil) // the Audit Log active again
	audit := h.s.UIState().ActiveDock
	if err := h.run(t, agent, "dock.closeOthers", map[string]any{"id": ""}, nil); err == nil {
		t.Error("dock.closeOthers without the tab to keep")
	}
	h.must(t, agent, "dock.closeOthers", map[string]any{"id": audit}, nil)
	ids := func() []string {
		var out []string
		for _, d := range h.s.UIState().Dock {
			out = append(out, d.ID)
		}
		return out
	}
	if got := ids(); !slices.Equal(got, []string{audit}) {
		t.Errorf("dock.closeOthers left %q", got)
	}
	h.must(t, agent, "dock.closeAll", nil, nil)
	if got := ids(); len(got) != 0 {
		t.Errorf("dock.closeAll left %q", got)
	}
}
