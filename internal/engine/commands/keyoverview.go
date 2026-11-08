package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The key overview (D-158): a card over Cogpit's window that lists the single keys acting
// where the keyboard is (the resource table, the drawer, a logs tab: k9s's keys) and the window's.
// Whether it shows is UI state (rule 2), so that agents see it in ui_state and can close it.

type keyOverviewParams struct {
	Open *bool `json:"open,omitempty" jsonschema:"true shows the key overview, false hides it; omitted: switches it"`
}

func registerKeyOverview(g *registrar) {
	add(g, engine.Spec[keyOverviewParams]{
		ID: "ui.keyOverview", Label: "Key Overview", MenuPath: "Help/Key Overview", Danger: engine.DangerUI,
		Description: "Shows or hides the key overview over Cogpit's window, as ? does while the resource table, the " +
			"drawer or a logs tab has the keyboard: the single keys that act there now, k9s's (l Logs, s Shell, " +
			"r Restart, ...), and the window's. ui_state reports keyOverview: true while it shows. Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, p keyOverviewParams) (any, error) {
			return s.UpdateUI(func(u *engine.UIState) {
				if p.Open != nil {
					u.KeyOverview = *p.Open
					return
				}
				u.KeyOverview = !u.KeyOverview
			}), nil
		},
	})
}
