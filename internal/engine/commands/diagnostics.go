package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Connection Diagnostics (D-13, D-133; engine/diagnostics_view.go, diagnostics_report.go): a
// connected cluster's connection, Cogpit's rate limits and what waited at them, how the reads'
// initial lists came, the context's diagnostics log (refused requests, requests without an answer,
// 429s, server errors, slow answers, waits at the rate limits, initial lists the server did not
// stream, slow loads), its API requests by verb and resource, its watches, the objects and their
// memory per kind, and where discovery came from, from what the connection and the cache keep.
// No command sends a request.

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
			"(requests, open and warm watches, objects, bytes). Connection Diagnostics' part: server and serverVersion, since " +
			"(the context's latest connect), budgets (Cogpit's rate limits: requests, discovery, access reviews, each with " +
			"qps, burst, the requests that passed, those delayed, the time they waited together in nanoseconds, the longest, " +
			"those waiting now; watches never wait), streaming (the open and warm watches whose initial list the server " +
			"streamed, that were listed, or listed after the server did not stream it; each watch's initial and loadedIn too), " +
			"problems (the log's problem lines since the latest connect, as the status bar's button counts them), " +
			"problemCounts (occurrences by kind since then) and log: the context's diagnostics log, the line seen last first, " +
			"at most 500 lines kept in memory only, each with kind (refused, unauthorized, tooManyRequests, serverError, " +
			"timeout, network, slow, throttled, fallback, slowLoad, namespaced, connect), what (a request's verb and resource " +
			"with name and namespace, or a read's scope), text (the API server's message or the error), method and url, " +
			"status, retryAfter, took and longest, first, last and count (the same problem again is counted in its line). " +
			"Real values: the API server's address included; diagnostics_report gives the report with the endpoints replaced. " +
			"From Cogpit's own counters: it sends no request.",
		Run: func(_ context.Context, s *engine.Session, p engine.OnCluster) (any, error) {
			return s.DiagnosticsReportOf(p.Context)
		},
	})

	add(g, engine.Spec[engine.OnCluster]{
		ID: "diagnostics.report", Label: "Connection Diagnostics Report", Danger: engine.DangerRead,
		Description: "Returns Connection Diagnostics' report of a connected cluster (context, else the shown one) as plain " +
			"text (text) and the file name it is offered to be saved under (name): the connection, the problems since " +
			"the latest connect, Cogpit's rate limits, how the initial lists came, the diagnostics log, the watches, the " +
			"API requests and the context's latest connect attempts, with the endpoints replaced: hosts and addresses " +
			"become host-1 and ip-1 throughout (a URL keeps its path), ports and file paths go; names of users, " +
			"namespaces, objects and API groups stay. What the tab's Copy and Save Report put out. The demo cluster has " +
			"no report. It sends no request.",
		Run: func(_ context.Context, s *engine.Session, p engine.OnCluster) (any, error) {
			return s.DiagnosticsReportTextOf(p.Context)
		},
	})

	add(g, engine.Spec[struct{}]{
		ID: "diagnostics.open", Label: "Connection Diagnostics", MenuPath: "Help/Connection Diagnostics", Danger: engine.DangerUI,
		Description: "Opens the Connection Diagnostics dock tab, or activates it: the shown cluster's connection, its " +
			"problems since the latest connect, Cogpit's rate limits and the diagnostics log, its API requests by verb and " +
			"resource, its watches and the objects and memory per kind, and every connected cluster's sums, kept current " +
			"while it is open, as Help › Connection Diagnostics and the status bar's button do. Returns the tab.",
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			tab := engine.DockTab{ID: s.NewDockID(engine.DockDiagnostics), Type: engine.DockDiagnostics, Title: "Connection Diagnostics"}
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
