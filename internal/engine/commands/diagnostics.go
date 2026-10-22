package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The Diagnostics view (D-13; engine/diagnostics_view.go): a connected cluster's API requests by
// verb and resource, its watches, the objects and their memory per kind, and where discovery came
// from, from the counters the connection and the cache keep. Neither command sends a request.

func registerDiagnostics(g *registrar) {
	add(g, engine.Spec[engine.OnCluster]{
		ID: "diagnostics.get", Label: "Read Diagnostics", Danger: engine.DangerRead,
		Description: "Returns what Cogpit asked of a connected cluster (context, else the shown one) and holds of it, as the Diagnostics tab shows it: " +
			"the API requests since connecting, by verb and by verb and resource (list, watch, get, ...; watches " +
			"included), each watch (informer) with its scope, state (open: a view or lookup holds it; warm: kept a while " +
			"after its last user; covered: served by another watch), objects, events and errors, the objects per kind " +
			"with the memory they take (bytes: an estimate, a sample of each watch's objects walked as Go lays them " +
			"out on the heap and scaled to its objects, within a few percent of the heap for the same objects; " +
			"client-go's queues and Cogpit's table snapshots not counted) and their sums for the cluster, where the " +
			"connection's discovery answer came from (discovery.source: disk, the cache Cogpit keeps for the next " +
			"connection, or server), and the table snapshots built; clusters sums up every connected cluster, shown or not " +
			"(requests, open and warm watches, objects, bytes). From Cogpit's own counters: it sends no request.",
		Run: func(_ context.Context, s *engine.Session, p engine.OnCluster) (any, error) {
			return s.DiagnosticsReportOf(p.Context)
		},
	})

	add(g, engine.Spec[struct{}]{
		ID: "diagnostics.open", Label: "Diagnostics", MenuPath: "Help/Diagnostics", Danger: engine.DangerUI,
		Description: "Opens the Diagnostics dock tab, or activates it: the shown cluster's API requests by verb and " +
			"resource, its watches and the objects and memory per kind, and every connected cluster's sums, kept current " +
			"while it is open, as Help › Diagnostics does. Returns the tab.",
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			tab := engine.DockTab{ID: s.NewDockID(engine.DockDiagnostics), Type: engine.DockDiagnostics, Title: "Diagnostics"}
			created := true
			s.UpdateUI(func(u *engine.UIState) {
				for _, t := range u.Dock { // one Diagnostics tab
					if t.Type == engine.DockDiagnostics {
						tab, created = t, false
						break
					}
				}
				if created {
					u.Dock = append(u.Dock, tab)
				}
				u.ShowDockTab(tab.ID)
			})
			return DockResult{Tab: tab, Created: created}, nil
		},
	})
}
