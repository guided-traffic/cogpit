package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Moving tabs (the owner's gate walk, GW4; engine/tabmove.go): the tab bar's and the dock's. UI
// commands: they change only the order the window shows, never a cluster.

type tabMoveParams struct {
	ID    string `json:"id,omitempty" jsonschema:"tab id as ui_state lists it in tabs (tab-1); empty means the active tab"`
	Index int    `json:"index" jsonschema:"the place to move it to among the tabs the tab bar shows (the shown cluster's), from 1 (the first)"`
}

// tabStepParams are Move Tab Left's and Right's: which bar's active tab moves.
type tabStepParams struct {
	Dock bool `json:"dock,omitempty" jsonschema:"true moves the dock's active tab among the tabs the dock shows (the shown cluster's and those of no cluster); leave it out for the tab bar's active tab"`
}

// check returns why Move Tab Left (d < 0) or Right cannot move the active tab of the bar p names.
func (p tabStepParams) check(s *engine.Session, d int) error {
	if p.Dock {
		return s.CheckDockStep(d)
	}
	return s.CheckTabStep(d)
}

// step moves the active tab of the bar p names one place left (d < 0) or right.
func (p tabStepParams) step(s *engine.Session, d int) (engine.UIState, error) {
	if p.Dock {
		return s.StepDockTab(d)
	}
	return s.StepTab(d)
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

	add(g, engine.Spec[tabStepParams]{
		ID: "ui.tab.moveLeft", Label: "Move Tab Left", MenuPath: "View/Move Tab Left", Shortcut: "Cmd+Alt+Shift+Left", Danger: engine.DangerUI,
		Description: "Moves the active tab one place to the left in the tab bar, as ui_tab_move does, or with dock the dock's " +
			"active tab in the dock, as dock_move does. Returns the new UI state.",
		Enabled: func(s *engine.Session, p tabStepParams) error { return p.check(s, -1) },
		Run: func(_ context.Context, s *engine.Session, p tabStepParams) (any, error) {
			return p.step(s, -1)
		},
	})

	add(g, engine.Spec[tabStepParams]{
		ID: "ui.tab.moveRight", Label: "Move Tab Right", MenuPath: "View/Move Tab Right", Shortcut: "Cmd+Alt+Shift+Right", Danger: engine.DangerUI,
		Description: "Moves the active tab one place to the right in the tab bar, as ui_tab_move does, or with dock the dock's " +
			"active tab in the dock, as dock_move does. Returns the new UI state.",
		Enabled: func(s *engine.Session, p tabStepParams) error { return p.check(s, 1) },
		Run: func(_ context.Context, s *engine.Session, p tabStepParams) (any, error) {
			return p.step(s, 1)
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
