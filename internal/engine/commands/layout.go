package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// layoutToggleParams are ui.toggleNavigator's and ui.toggleDock's parameters, as chat.toggle's.
type layoutToggleParams struct {
	Open *bool `json:"open,omitempty" jsonschema:"true shows it, false hides it; leave it out to turn it the other way round"`
}

// registerLayout registers the window's layout toggles (F7; engine/layout.go): the navigator (⌘B)
// and the dock (⌘J), beside the chat column's chat.toggle (⌘I, chat.go). UI commands: agents run
// them freely (D-2) and read the result in ui_state.
func registerLayout(g *registrar) {
	add(g, engine.Spec[layoutToggleParams]{
		ID: "ui.toggleNavigator", Label: "Navigator", MenuPath: "View/Navigator", Shortcut: "Cmd+B", Danger: engine.DangerUI,
		Description: "Shows or hides the navigator, the column of the clusters and views between the hotbar and the " +
			"views, as ⌘B (Ctrl+B) and the first layout button at the right of Cogpit's top bar do; the hotbar stays. " +
			"ui_state reports navigatorHidden: true while it is hidden; it is kept across restarts. Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, p layoutToggleParams) (any, error) {
			return s.SetLayout(engine.LayoutNavigator, p.Open)
		},
	})
	add(g, engine.Spec[layoutToggleParams]{
		ID: "ui.toggleDock", Label: "Dock", MenuPath: "View/Dock", Shortcut: "Cmd+J", Danger: engine.DangerUI,
		Description: "Opens or collapses the dock, the panel of logs, terminal, shell and editor tabs below the views, " +
			"as ⌘J (Ctrl+J), the dock's arrow and the second layout button at the right of Cogpit's top bar do; " +
			"collapsed, it shows its tab strip only. Opening or activating a tab opens it. While the dock has no " +
			"tab it opens a terminal tab, as terminal.open does (the owner's WF18), and is disabled only when no terminal " +
			"can open (no cluster connected). ui_state reports dockOpen: true while it is open. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ layoutToggleParams) error {
			if s.DockToggleUnavailable() == nil {
				return nil
			}
			_, err := streamableOn(s, "", "terminals") // an empty dock opens a terminal (WF18)
			return err
		},
		Run: func(_ context.Context, s *engine.Session, p layoutToggleParams) (any, error) {
			if s.DockToggleUnavailable() != nil && (p.Open == nil || *p.Open) {
				if _, err := openTerminalTab(s, ""); err != nil { // an empty dock: a terminal, the dock opens with it
					return nil, err
				}
				return s.UIState(), nil
			}
			return s.SetLayout(engine.LayoutDock, p.Open)
		},
	})
}
