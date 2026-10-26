package engine

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
)

// The Diagnostics view (D-13 cache transparency; beyond-lens › Cache transparency): what the
// status bar sums up, in full, for the connected cluster: its API requests by verb and by verb and
// resource since connecting, each watch (informer) with its state and objects, the objects and the
// memory they take per kind and for the cluster, and where discovery came from (D-65). It reads the
// counters the connection and the cache keep (Session.Diagnostics); it sends no request. The memory
// is an estimate (cache/size.go): a sample of each watch's objects walked as Go lays them out on
// the heap, scaled to the watch's objects; the model is within a few percent of the heap Go reports
// for the same objects, and what client-go holds besides its stores and the engine's table
// snapshots are not counted.

// DockDiagnostics: the Diagnostics dock tab (diagnostics.open).
const DockDiagnostics DockType = "diagnostics"

// Watch states of the Diagnostics view.
const (
	WatchOpen    = "open"    // a handle holds it: a view, a drawer or a lookup uses it
	WatchWarm    = "warm"    // no handle: kept running until its warm period ends
	WatchCovered = "covered" // a label-selected scope served by another informer's watch
)

// DiagCount is one counter: a verb's requests.
type DiagCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// DiagRequest is the requests of one verb on one resource.
type DiagRequest struct {
	Verb     string `json:"verb"`
	Resource string `json:"resource,omitempty"` // empty for requests without one (discovery)
	Count    int64  `json:"count"`
}

// DiagWatch is one informer, or a label-selected scope another informer serves.
type DiagWatch struct {
	Scope   string `json:"scope"` // "pods", "secrets with owner=helm", "pods in team-a"
	Kind    string `json:"kind"`  // the resource with its group, kubectl's way: "deployments.apps"
	State   string `json:"state"` // WatchOpen, WatchWarm or WatchCovered
	Handles int    `json:"handles"`
	Objects int    `json:"objects"`
	Events  int64  `json:"events"` // delivered since the informer started, its initial list included
	Synced  bool   `json:"synced"`
	// WarmUntil is when a warm informer stops; CoveredBy the scope whose watch serves a covered one.
	WarmUntil time.Time `json:"warmUntil,omitzero"`
	CoveredBy string    `json:"coveredBy,omitempty"`
	Error     string    `json:"error,omitempty"`
	// Bytes estimates the memory of the watch's objects (0 for a covered scope: its cover's).
	Bytes int64 `json:"bytes,omitempty"`
	// LoadedIn is how long its initial list took (0 while it loads, and for a covered scope);
	// Initial how the list came: streamed by the server, listed, or listed after the server did
	// not stream it (cache.InitialStream, InitialList, InitialFallback; D-133).
	LoadedIn time.Duration `json:"loadedIn,omitempty"`
	Initial  string        `json:"initial,omitempty"`
}

// DiagKind is the objects Cogpit holds of one kind: those of its watches (a covered scope's
// objects are its informer's, counted once), and the memory they take, estimated.
type DiagKind struct {
	Kind    string `json:"kind"`
	Objects int    `json:"objects"`
	Watches int    `json:"watches"`
	Bytes   int64  `json:"bytes"`
}

// DiagnosticsReport is the Diagnostics view's content.
type DiagnosticsReport struct {
	Context   string `json:"context"`
	Connected bool   `json:"connected"`
	Demo      bool   `json:"demo,omitempty"`
	// Requests counts the connection's API requests since connecting, watches included; the demo
	// cluster has no connection and counts none.
	Requests         int64         `json:"requests"`
	RequestsByVerb   []DiagCount   `json:"requestsByVerb"`
	RequestsByTarget []DiagRequest `json:"requestsByVerbAndResource"`
	// Watches are the informers, open first, then warm, then covered, each by scope.
	Watches []DiagWatch `json:"watches"`
	Open    int         `json:"open"`
	Warm    int         `json:"warm"`
	Covered int         `json:"covered"`
	// Kinds are the objects per kind, the most memory first (then the most objects); Objects and
	// Bytes their sums: what the cluster's cache holds.
	Kinds   []DiagKind `json:"kinds"`
	Objects int        `json:"objects"`
	Bytes   int64      `json:"bytes"`
	// Discovery says where the connection's discovery answer came from: the disk cache or the
	// server (D-65); nil for the demo cluster and without a connection.
	Discovery *cluster.DiscoveryInfo `json:"discovery,omitempty"`
	// Snapshots counts the table snapshots built since start.
	Snapshots uint64 `json:"snapshots"`
	// Clusters sums up every connected cluster (D-69), shown or not, by context id: what each one
	// costs, its requests, watches, objects and memory; the fields above are the reported one's in
	// full. Empty while nothing is connected.
	Clusters []DiagCluster `json:"clusters,omitempty"`

	// Connection Diagnostics (D-133): the connection, Cogpit's rate limits and what waited at them,
	// how the reads' initial lists came, and the context's diagnostics log. Empty for the demo
	// cluster.
	//
	// Server is the API server's URL and ServerVersion its version; Since when the latest connect
	// of the context began.
	Server        string    `json:"server,omitempty"`
	ServerVersion string    `json:"serverVersion,omitempty"`
	Since         time.Time `json:"since,omitzero"`
	// Budgets are the connection's rate limits: the requests' (lists, gets, writes; watches never
	// wait), discovery's and the access reviews'.
	Budgets   []cluster.BudgetStats `json:"budgets,omitempty"`
	Streaming DiagStreaming         `json:"streaming"`
	// Problems counts the log's problem lines seen since the latest connect (the status bar's
	// button); ProblemCounts how often each kind of problem happened since then, the most first.
	Problems      int         `json:"problems"`
	ProblemCounts []DiagCount `json:"problemCounts,omitempty"`
	// Log is the context's diagnostics log, the line seen last first: at most
	// cluster.DiagLogLimit lines, each a problem with its count (cluster.DiagEntry).
	Log []cluster.DiagEntry `json:"log"`
}

// DiagStreaming counts the open and warm watches by how their initial list came.
type DiagStreaming struct {
	Streamed int `json:"streamed"` // the server streamed it (WatchList)
	Listed   int `json:"listed"`   // listed: the source does not stream
	FellBack int `json:"fellBack"` // listed after the server did not stream it
	// Off says the connection lists every read since a stream stalled (D-new).
	Off bool `json:"off,omitempty"`
}

// DiagCluster is one connected cluster's sums in the Diagnostics view.
type DiagCluster struct {
	Context  string `json:"context"`
	Shown    bool   `json:"shown,omitempty"` // the cluster the window shows
	Requests int64  `json:"requests"`
	Open     int    `json:"open"`
	Warm     int    `json:"warm"`
	Objects  int    `json:"objects"`
	Bytes    int64  `json:"bytes"`
}

// DiagnosticsReport returns the shown cluster's counters as the Diagnostics view shows them, with
// the memory per kind estimated, and every connected cluster's sums.
func (s *Session) DiagnosticsReport() DiagnosticsReport {
	r, _ := s.DiagnosticsReportOf("")
	return r
}

// DiagnosticsReportOf is DiagnosticsReport for a connected context's cluster, "" the shown one
// (D-69); a context that is not connected is a NotConnectedError.
func (s *Session) DiagnosticsReportOf(contextID string) (DiagnosticsReport, error) {
	shown := s.Backend()
	b := shown
	if contextID != "" {
		var err error
		if b, err = s.TargetBackend(contextID); err != nil {
			return DiagnosticsReport{}, err
		}
	}
	r := ReportOf(s.diagnosticsOf(s.UIState().Context, b, true), b != nil)
	r.addConnection(b)
	for _, c := range s.Backends() {
		one := r
		if c != b {
			one = ReportOf(s.diagnosticsOf(c.Context, c, true), true)
		}
		r.Clusters = append(r.Clusters, DiagCluster{Context: c.Context, Shown: c == shown, Requests: one.Requests,
			Open: one.Open, Warm: one.Warm, Objects: one.Objects, Bytes: one.Bytes})
	}
	return r, nil
}

// ReportOf arranges the counters for the Diagnostics view: requests by verb and by verb and
// resource, most first; watches open, warm, then covered; objects and memory per kind.
func ReportOf(d Diagnostics, connected bool) DiagnosticsReport {
	r := DiagnosticsReport{Context: d.Context, Demo: d.Demo, Connected: connected, Snapshots: d.Snapshots,
		Requests: d.Requests.Total, RequestsByVerb: []DiagCount{}, RequestsByTarget: []DiagRequest{},
		Watches: []DiagWatch{}, Kinds: []DiagKind{}, Discovery: d.Discovery}
	for verb, n := range d.Requests.ByVerb {
		r.RequestsByVerb = append(r.RequestsByVerb, DiagCount{Name: verb, Count: n})
	}
	slices.SortFunc(r.RequestsByVerb, func(a, b DiagCount) int { return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Name, b.Name)) })
	for key, n := range d.Requests.ByVerbResource {
		verb, resource, _ := strings.Cut(key, " ")
		r.RequestsByTarget = append(r.RequestsByTarget, DiagRequest{Verb: verb, Resource: resource, Count: n})
	}
	slices.SortFunc(r.RequestsByTarget, func(a, b DiagRequest) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Resource, b.Resource), cmp.Compare(a.Verb, b.Verb))
	})
	kinds := map[string]*DiagKind{}
	for _, inf := range d.Informers {
		w := diagWatch(inf)
		r.Watches = append(r.Watches, w)
		switch w.State {
		case WatchOpen:
			r.Open++
		case WatchWarm:
			r.Warm++
		default:
			r.Covered++
			continue // its objects are its informer's
		}
		k := kinds[w.Kind]
		if k == nil {
			k = &DiagKind{Kind: w.Kind}
			kinds[w.Kind] = k
		}
		k.Objects += w.Objects
		k.Bytes += w.Bytes
		k.Watches++
		r.Objects += w.Objects
		r.Bytes += w.Bytes
	}
	r.Streaming.Off = d.Listing
	for _, w := range r.Watches {
		switch {
		case w.State == WatchCovered:
		case w.Initial == cache.InitialStream:
			r.Streaming.Streamed++
		case w.Initial == cache.InitialFallback:
			r.Streaming.FellBack++
		case w.Initial == cache.InitialList:
			r.Streaming.Listed++
		}
	}
	rank := map[string]int{WatchOpen: 0, WatchWarm: 1, WatchCovered: 2}
	slices.SortFunc(r.Watches, func(a, b DiagWatch) int {
		return cmp.Or(cmp.Compare(rank[a.State], rank[b.State]), cmp.Compare(a.Scope, b.Scope))
	})
	for _, k := range kinds {
		r.Kinds = append(r.Kinds, *k)
	}
	slices.SortFunc(r.Kinds, func(a, b DiagKind) int {
		return cmp.Or(cmp.Compare(b.Bytes, a.Bytes), cmp.Compare(b.Objects, a.Objects), cmp.Compare(a.Kind, b.Kind))
	})
	return r
}

func diagWatch(inf cache.InformerStats) DiagWatch {
	kind := inf.Scope.GVR.Resource
	if g := inf.Scope.GVR.Group; g != "" {
		kind += "." + g
	}
	w := DiagWatch{Scope: inf.Scope.String(), Kind: kind, Handles: inf.Handles, Objects: inf.Objects, Events: inf.Events,
		Synced: inf.Synced, CoveredBy: inf.CoveredBy, Error: inf.Error, Bytes: inf.Bytes, LoadedIn: inf.LoadedIn, Initial: inf.Initial}
	switch {
	case inf.CoveredBy != "":
		w.State = WatchCovered
	case inf.Handles > 0:
		w.State = WatchOpen
	default:
		w.State, w.WarmUntil = WatchWarm, inf.WarmUntil
	}
	return w
}

// addConnection fills Connection Diagnostics' part of the report from the backend's connection:
// nothing for the demo cluster and without a backend.
func (r *DiagnosticsReport) addConnection(b *Backend) {
	r.Log = []cluster.DiagEntry{}
	log := b.diagLog()
	if log == nil {
		return
	}
	r.Server, r.ServerVersion = b.Conn.Server(), b.Conn.ServerVersion()
	r.Since = log.Since()
	r.Budgets = b.Conn.Budgets()
	r.Problems = log.Problems()
	for kind, n := range log.Counts() {
		r.ProblemCounts = append(r.ProblemCounts, DiagCount{Name: string(kind), Count: n})
	}
	slices.SortFunc(r.ProblemCounts, func(a, b DiagCount) int { return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Name, b.Name)) })
	r.Log = log.Entries()
}
