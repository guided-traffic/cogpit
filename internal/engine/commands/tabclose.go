package commands

import (
	"context"
	"errors"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Closing several tabs at once, a tab's right-click menu in the tab bar and in the dock
// (engine/tabclose.go). UI commands: they change what the window shows, never a cluster; the dock
// keeps a tab whose job still runs.

func registerTabClose(g *registrar) {
	add(g, engine.Spec[tabIDParams]{
		ID: "ui.tab.closeOthers", Label: "Close Other Tabs", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Closes every tab the tab bar shows (the shown cluster's; every connected cluster's with Mixed " +
			"environments) but the active one (or the one named by id), which becomes the active tab as ui_tab_select " +
			"makes it. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ tabIDParams) error { return tabsOpen(s) },
		Run: func(ctx context.Context, s *engine.Session, p tabIDParams) (any, error) {
			if p.ID == "" {
				if p.ID = s.UIState().ActiveTab; p.ID == "" {
					return nil, engine.ErrNoTab
				}
			}
			return s.CloseTabs(ctx, p.ID)
		},
	})

	add(g, engine.Spec[struct{}]{
		ID: "ui.tab.closeAll", Label: "Close All Tabs", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Closes every tab the tab bar shows (the shown cluster's; every connected cluster's with Mixed " +
			"environments); the tabs of a cluster it does not show stay. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ struct{}) error { return tabsOpen(s) },
		Run: func(ctx context.Context, s *engine.Session, _ struct{}) (any, error) {
			return s.CloseTabs(ctx, "")
		},
	})

	add(g, engine.Spec[dockParams]{
		ID: "dock.closeOthers", Label: "Close Other Tabs", Danger: engine.DangerUI,
		Description: "Closes every tab the dock shows (the shown cluster's and those of no cluster) but the one named by id, " +
			"which becomes the active tab, and ends their streams and shells, as dock_close does; a tab whose job still " +
			"runs stays, since closing it would stop the job (dock_close and job_cancel do that). Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, p dockParams) (any, error) {
			if p.ID == "" {
				return nil, errors.New("name the dock tab to keep: id")
			}
			return s.CloseDockTabs(p.ID)
		},
	})

	add(g, engine.Spec[struct{}]{
		ID: "dock.closeAll", Label: "Close All Tabs", Danger: engine.DangerUI,
		Description: "Closes every tab the dock shows (the shown cluster's and those of no cluster) and ends their streams " +
			"and shells, as dock_close does; a tab whose job still runs stays, since closing it would stop the job " +
			"(dock_close and job_cancel do that). Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			return s.CloseDockTabs("")
		},
	})
}
