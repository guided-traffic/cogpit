package commands_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// TestShowingAnOpenDockTabOpensTheDock (the owner's alpha walk, 2026-10-22, AW9: with the dock
// minimised, Edit on an object whose Edit tab was open "sometimes" did nothing): a command that
// shows a dock tab opens the collapsed dock, also when it reuses the tab open on the same thing and
// that tab is the active one, which left the dock collapsed. The same for each command that reuses
// a tab, and for an agent's dock.activate of the active tab.
func TestShowingAnOpenDockTabOpensTheDock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	h, _ := editHarness(t, engine.Options{Audit: f, AuditPath: path})
	gui, agent := engine.OriginGUI, engine.OriginExternalAgent
	pod := model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}
	collapse := func() {
		t.Helper()
		var u engine.UIState
		h.must(t, gui, "ui.toggleDock", map[string]any{"open": false}, &u)
		if u.DockOpen {
			t.Fatal("the dock did not collapse")
		}
	}
	for _, c := range []struct {
		id     string
		origin engine.Origin
		params any
	}{
		{"object.edit", gui, map[string]any{"ref": checkout}}, // the owner's case: Edit in the drawer
		{"object.edit", agent, map[string]any{"ref": checkout}},
		{"pod.logs", gui, map[string]any{"ref": pod}},
		{"audit.open", agent, struct{}{}},
		{"diagnostics.open", gui, struct{}{}},
	} {
		h.must(t, c.origin, c.id, c.params, nil) // its tab, new or shown again: the dock opens
		if u := h.s.UIState(); !u.DockOpen {
			t.Errorf("%s: the dock is collapsed after its tab %s was shown", c.id, u.ActiveDock)
		}
		active := h.s.UIState().ActiveDock
		collapse()
		h.must(t, c.origin, c.id, c.params, nil) // the same tab, the active one
		if u := h.s.UIState(); u.ActiveDock != active || !u.DockOpen {
			t.Errorf("%s from %s again: active %s (was %s), dock open %v", c.id, c.origin, u.ActiveDock, active, u.DockOpen)
		}
	}
	collapse()
	active := h.s.UIState().ActiveDock
	var u engine.UIState
	h.must(t, agent, "dock.activate", map[string]any{"id": active}, &u)
	if !u.DockOpen {
		t.Errorf("an agent's dock.activate of the active tab %s left the dock collapsed", active)
	}
}
