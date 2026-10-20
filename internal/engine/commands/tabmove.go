package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Moving tabs (the owner's gate walk, GW4; engine/tabmove.go): the tab bar's and the dock's. UI
// commands: they change only the order the window shows, never a cluster.

type tabMoveParams struct {
	ID    string `json:"id,omitempty" jsonschema:"tab id as ui_state lists it in tabs (tab-1); empty means the active tab"`
	Index int    `json:"index" jsonschema:"the place to move it to among the tabs the tab bar shows (the navigator's scope's), from 1 (the first)"`
}

type dockMoveParams struct {
	ID    string `json:"id" jsonschema:"dock tab id as ui_state lists it in dock (logs-1)"`
	Index int    `json:"index" jsonschema:"the place to move it to among the tabs the dock shows (the shown cluster's and those of no cluster), from 1 (the first)"`
}

func registerTabMoves(g *registrar) {
	add(g, engine.Spec[tabMoveParams]{
		ID: "ui.tab.move", Label: "Move Tab", Danger: engine.DangerUI,
		Description: "Moves a tab of the tab bar (the active one without id) to another place among the tabs the bar shows, " +
			"as dragging it there does: index 1 is the first. The tabs of other clusters, which the bar hides while the " +
			"navigator shows one cluster, keep their places among the others. The order survives a restart with the tabs. " +
			"Returns the new UI state.",
		Enabled: func(s *engine.Session, _ tabMoveParams) error { return tabsOpen(s) },
		Run: func(_ context.Context, s *engine.Session, p tabMoveParams) (any, error) {
			return s.MoveTab(p.ID, p.Index)
		},
	})

	add(g, engine.Spec[struct{}]{
		ID: "ui.tab.moveLeft", Label: "Move Tab Left", MenuPath: "View/Move Tab Left", Shortcut: "Cmd+Alt+Shift+Left", Danger: engine.DangerUI,
		Description: "Moves the active tab one place to the left in the tab bar, as ui_tab_move does. Returns the new UI state.",
		Enabled:     func(s *engine.Session, _ struct{}) error { return s.CheckTabStep(-1) },
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			return s.StepTab(-1)
		},
	})

	add(g, engine.Spec[struct{}]{
		ID: "ui.tab.moveRight", Label: "Move Tab Right", MenuPath: "View/Move Tab Right", Shortcut: "Cmd+Alt+Shift+Right", Danger: engine.DangerUI,
		Description: "Moves the active tab one place to the right in the tab bar, as ui_tab_move does. Returns the new UI state.",
		Enabled:     func(s *engine.Session, _ struct{}) error { return s.CheckTabStep(1) },
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			return s.StepTab(1)
		},
	})

	add(g, engine.Spec[dockMoveParams]{
		ID: "dock.move", Label: "Move Tab", Danger: engine.DangerUI,
		Description: "Moves a dock tab to another place among the tabs the dock shows, as dragging it there does: index 1 is " +
			"the first. Tabs of other clusters, which the dock hides, keep their places among the others. Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, p dockMoveParams) (any, error) {
			return s.MoveDockTab(p.ID, p.Index)
		},
	})
}
