package commands

import (
	"context"
	"errors"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The status filter of every view (D-34, owner; engine/statusfilter.go): ui.setStatusFilter sets
// the active tab's, as the window's filter beside the namespace filter does; ui.openView with
// states opens a view filtered (the dashboards' state links); view.rows and ui.state show it.

type statusFilterParams struct {
	States []string `json:"states,omitempty" jsonschema:"states the rows must carry, as view_rows lists them in stateChoices: the texts of the view's state columns (a pod's Status Running, a Flux object's Not Ready, a deployment's condition Available), qualified by the column's title in a view with several (an Argo CD Application's Sync: OutOfSync or Health: Degraded; an unqualified text matches any of them), or for a kind without one its rows' status tokens (OK, Pending, Error); a row is shown when it carries one of the states selected for each column named; empty or omitted clears the filter"`
}

func registerStatusFilter(g *registrar) {
	add(g, engine.Spec[statusFilterParams]{
		ID: "ui.setStatusFilter", Label: "Filter by Status", MenuPath: "View/Status Filter", Danger: engine.DangerUI,
		Description: "Sets the status filter of the open view, as the window's states menu beside the namespace filter does " +
			"(Cogpit's addition; Lens has none): only the rows that carry the given states are shown, in the table, view_rows, " +
			"select all and the CSV export. view_rows lists the states the rows carry (stateChoices, with counts). With one " +
			"state column (Status) a row carrying any selected state is shown; with several (an Argo CD Application's Sync " +
			"and Health) the states are qualified by the column's title and a row must carry a selected state of each column " +
			"named. The filter belongs to the active tab's view: opening another view clears it (ui_openView with states " +
			"opens a view filtered). An empty list clears it. Returns the new UI state.",
		Enabled: func(s *engine.Session, _ statusFilterParams) error { _, err := openView(s); return err },
		Run: func(_ context.Context, s *engine.Session, p statusFilterParams) (any, error) {
			view, _, err := openViewKey(s)
			if err != nil {
				return nil, err
			}
			states := engine.CleanStates(p.States)
			applied := false
			st := s.UpdateUI(func(u *engine.UIState) {
				if u.View == view {
					u.SetStates(states)
					applied = true
				}
			})
			if !applied {
				return nil, errors.New("the open view changed; set the status filter again")
			}
			return st, nil
		},
	})
}
