package engine

import (
	"context"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The cluster Overview (ui-parity › Cluster overview and charts, G12, milestone M7): the
// navigator's first item, a page as the Workloads Overview is (overview.go). Its upper part is the
// cluster's CPU, Memory and Pods charts for its master or its worker nodes over a time range
// (clusteroverview_charts.go); its table is the Issues: what is wrong in the cluster now, read from
// the cache only (clusteroverview_issues.go). The page holds its cache handles, and the metrics
// subscription of the cluster's totals, only while it is open.

// ClusterOverview is the page key of the cluster Overview.
const ClusterOverview = "cluster/overview"

// The node groups of the Overview's charts (Lens: "master and worker nodes").
const (
	NodesMaster = "master"
	NodesWorker = "worker"
)

// Kind keys the page reads.
const (
	kindNodes      = "nodes"
	kindNamespaces = "namespaces"
)

// The Issues' column ids besides the name.
const (
	columnKind      = "kind"
	columnNamespace = "namespace"
	columnAge       = "age"
)

// MixedKinds reports whether a view's rows are objects of several kinds: the cluster Overview's
// Issues name pods, nodes, workloads and the objects of events, the Flux dashboard's table objects
// of every Flux kind. ui.select then takes a row of any kind the cluster serves, where a kind's
// view takes its own kind only.
func MixedKinds(view string) bool { return view == ClusterOverview || view == FluxDashboard }

// OverviewChoice is what the cluster Overview's charts show, held by the engine (rule 2: UI state is
// engine state): their time range and their node group. ui.setOverviewRange and ui.setOverviewNodes
// set it; it stays while the session lives, whichever view is open.
type OverviewChoice struct {
	// Range is the charts' time range as ui.setOverviewRange writes it ("1h", "7d"); empty: the
	// default hour.
	Range string `json:"range,omitempty"`
	// Nodes is the node group the charts show, "master" or "worker"; empty: the workers when the
	// cluster has any, else the masters (Cogpit's default).
	Nodes string `json:"nodes,omitempty"`
}

// issuesProfile is the profile of the Overview's table: one row per issue, rows of several kinds
// (MixedKinds). Its key is the page's, so that no kind is mistaken for it.
var issuesProfile = &model.KindProfile{
	Key: ClusterOverview, Title: "Issues", Namespaced: true,
	Columns: []model.Column{
		{ID: metadataName, Title: "Name", Type: model.ColumnText},
		{ID: columnKind, Title: "Kind", Type: model.ColumnText, Width: 120},
		{ID: columnNamespace, Title: "Namespace", Type: model.ColumnText},
		{ID: "message", Title: "Message", Type: model.ColumnText},
		{ID: columnAge, Title: "Age", Type: model.ColumnAge},
	},
}

// issuesSort is the Issues' order until the user sorts: the newest first (Cogpit's default).
var issuesSort = SortSpec{Column: columnAge, Desc: true}

// ClusterOverviewModel is the cluster Overview: its node groups and charts, and its issues.
type ClusterOverviewModel struct {
	// Nodes is the node group the charts show: "master" or "worker".
	Nodes string `json:"nodes"`
	// Groups are the cluster's node groups with their node counts: master (control-plane nodes)
	// and worker (every other node).
	Groups []OverviewNodeGroup `json:"groups"`
	// Range is the charts' time range ("1h"); Window the same as a duration.
	Range  string        `json:"range"`
	Window time.Duration `json:"-"`
	// Charts are CPU, Memory and Pods for the node group, in that order.
	Charts []OverviewChart `json:"charts"`
	// Status is the state of the cluster's metrics, as a drawer's: "available", "notServed",
	// "forbidden", "unavailable", or "unknown" before the first poll answered. Source names the
	// providers the CPU and Memory numbers come from ("Prometheus (monitoring/prometheus-server) and
	// Kubernetes Metrics Server"); Message says why there are none. History is true when a provider
	// with history serves the charts (ranges beyond an hour).
	Status  metrics.Status `json:"status"`
	Source  string         `json:"source,omitempty"`
	Message string         `json:"message,omitempty"`
	History bool           `json:"history,omitempty"`
	// PodsHistory says where the Pods chart's history before the page opened comes from:
	// "kube-state-metrics" (through Prometheus), "none" (Prometheus has no such series: the chart
	// is the cache's counts since the page opened), or empty while not known.
	PodsHistory string `json:"podsHistory,omitempty"`
	// UsageHistory says where the CPU and Memory charts' history comes from once Prometheus
	// answered (A43, F1): "node-exporter" (where the cluster's Prometheus has its series),
	// "cadvisor" (the nodes' root cgroups), "none" (it has neither: the charts hold what Cogpit
	// measured since the page opened), or empty while not known or without Prometheus.
	UsageHistory string `json:"usageHistory,omitempty"`
	// NodeStates and PodStates count the cluster's nodes and pods by the state their views show
	// (all namespaces, whichever group the charts show), in StatusOrder; the health header
	// above the charts draws them.
	NodeStates []StatusCount `json:"nodeStates,omitempty"`
	PodStates  []StatusCount `json:"podStates,omitempty"`
	// Providers is the status line under the charts: each of the cluster's metrics providers, whether
	// it answers, what it serves (as metrics.get and a drawer's status line have it).
	Providers []metrics.ProviderStatus `json:"providers,omitempty"`
	// Issues are what is wrong in the cluster now, in the selected namespaces (a cluster-scoped
	// object's issue, a node's, is always listed), as the page's table lists them.
	Issues []ClusterIssue `json:"issues"`
	// Synced is false while a kind the page reads is still being listed.
	Synced bool `json:"synced"`
	// Error names the kinds the page cannot read ("events: forbidden"); empty when it reads all.
	Error string `json:"error,omitempty"`
}

// OverviewNodeGroup is one node group of the Overview.
type OverviewNodeGroup struct {
	Name  string `json:"name"`  // "master", "worker"
	Title string `json:"title"` // "Master", "Worker"
	Nodes int    `json:"nodes"`
}

// ClusterIssue is one row of the Overview's Issues: an object that is not as it should be, or a
// warning event of the last hour about one.
type ClusterIssue struct {
	// Ref is the object the issue is about (an event's involved object when Cogpit has a view for
	// its kind, else the event): ui_select on the open Overview, or ui_navigate, shows it.
	Ref model.ObjectRef `json:"ref"`
	// Name is the object's name as the row shows it: the involved object's for an event, also when
	// Ref is the event.
	Name string `json:"name"`
	// Kind is the object's kind as Kubernetes names it: "Pod", "Node", "Deployment".
	Kind string `json:"kind"`
	// Source says what found it: "status" (the object's own state, as its view's warning icon or
	// status colour shows it) or "event" (a warning event of the last hour).
	Source string `json:"source"`
	// Severity is "warning" or "error".
	Severity model.Status `json:"severity"`
	Message  string       `json:"message"`
	// Since is when it began, as far as the object says: a condition's last transition, an event's
	// last time; else when the object was created.
	Since time.Time `json:"since,omitzero"`
}

// The sources of an issue.
const (
	IssueStatus = "status"
	IssueEvent  = "event"
)

// clusterOverviewWorker keeps the cluster Overview current while it is open: cache handles on the
// kinds the page reads, its Issues table rebuilt at most once per SnapshotInterval after a change.
type clusterOverviewWorker struct {
	s       *Session
	backend *Backend
	src     *overviewSource
	planKey string // which kinds the open page reads and how (Backend.pageReadKey); the state's lock

	mu     sync.Mutex
	params viewParams
	choice OverviewChoice

	snap     atomic.Pointer[TableSnapshot]
	build    atomic.Pointer[overviewBuild]
	wake     chan struct{}
	quit     chan struct{}
	stopped  atomic.Bool
	panicked atomic.Bool

	charts *overviewCharts // the charts' metrics while the page is open (clusteroverview_charts.go)
}

// startClusterOverview starts the page's worker; the first build runs at once.
func (s *Session) startClusterOverview(b *Backend, params viewParams, choice OverviewChoice) *clusterOverviewWorker {
	w := &clusterOverviewWorker{s: s, backend: b, params: params, choice: choice, wake: make(chan struct{}, 1), quit: make(chan struct{})}
	wake := w.wake
	w.src = newOverviewSource(b, b.acquireForPage, func() { signal(wake) }) // nothing while a plan is pending (A98)
	w.charts = newOverviewCharts(s, b, choice)
	w.charts.setHidden(params.metrics) // the charts the cluster's settings hide are not polled for
	signal(w.wake)
	s.goSafe("cluster overview", func() {
		runCoalesced(w.quit, w.wake, s.opts.SnapshotInterval, w.unsettled, w.rebuild)
	})
	return w
}

func (w *clusterOverviewWorker) setParams(p viewParams, choice OverviewChoice) {
	w.mu.Lock()
	changed := !w.params.equal(p)
	metricsChanged := w.params.metrics != p.metrics
	w.params = p
	choiceChanged := w.choice != choice
	w.choice = choice
	w.mu.Unlock()
	if metricsChanged {
		w.charts.setHidden(p.metrics)
	}
	if choiceChanged {
		w.charts.setChoice(choice)
	}
	if changed || choiceChanged {
		signal(w.wake)
	}
}

// stop ends the loop and releases the handles and the metrics subscription; the informers stay
// warm.
func (w *clusterOverviewWorker) stop() {
	if !w.stopped.CompareAndSwap(false, true) {
		return
	}
	close(w.quit)
	w.src.release()
	w.charts.release()
}

// unsettled reports whether a sync state or an error changed since the last build (neither comes
// with a watch event), or an event of the Issues left the last hour.
func (w *clusterOverviewWorker) unsettled() bool {
	last := w.build.Load()
	if last == nil {
		return true
	}
	if last.synced != w.src.synced() || last.errText != w.src.errText() {
		return true
	}
	return !last.expires.IsZero() && !time.Now().Before(last.expires)
}

// rebuild builds, stores and announces the page; a panic is reported once and does not end the loop.
func (w *clusterOverviewWorker) rebuild() {
	defer func() {
		if r := recover(); r != nil {
			w.s.log.Error("engine: building the cluster overview failed", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			if w.panicked.CompareAndSwap(false, true) {
				w.s.emit(Event{Type: EventToast, Level: LevelError, Message: "the cluster overview failed to update: internal error"})
			}
		}
	}()
	w.mu.Lock()
	p := w.params
	w.mu.Unlock()
	ob := w.src.build(time.Now())
	snap := issuesTable(w.backend.Context, ob, p)
	snap.Revision = w.s.state.snapshots.Add(1)
	if w.stopped.Load() {
		return
	}
	w.charts.follow(ob) // before the store: a read that finds a synced build finds its pods count
	w.build.Store(ob)
	w.snap.Store(snap)
	w.s.emit(Event{Type: EventTable, View: ClusterOverview, Revision: snap.Revision})
}

// overviewSource is what the page reads from the cache: a handle on each kind its Issues and
// totals come from, and per kind what each object adds, rebuilt only when the object changed.
type overviewSource struct {
	b         *Backend
	nodes     *overviewKind
	pods      *overviewKind
	events    *overviewKind
	workloads []*overviewKind
	unsubs    []func()

	mu sync.Mutex // one build at a time: the entries are the build's
}

// overviewKind is one kind of the page: its handle and each object's contribution by UID.
type overviewKind struct {
	profile *model.KindProfile
	handle  *cache.Handle
	entries map[string]*overviewEntry
	gen     uint32
}

// overviewWorkloads are the workload kinds whose shortfall of ready pods is an issue. Replica Sets
// are left out: a Deployment's would repeat it (Cogpit's choice).
var overviewWorkloads = []string{logKindDeployments, logKindStatefulSets, logKindDaemonSets}

// clusterOverviewKinds are every kind newOverviewSource opens, for the open page's plan key.
var clusterOverviewKinds = append([]string{kindNodes, kindPods, eventsKind}, overviewWorkloads...)

// newOverviewSource acquires the page's handles on the kinds the cluster serves and the user may
// list (as far as the access reviews know), through acquire (the open page's acquireForPage, an
// agent's read's Acquire once the plans are decided); onChange runs after every change of one of
// them.
func newOverviewSource(b *Backend, acquire func(*model.KindProfile) *cache.Handle, onChange func()) *overviewSource {
	src := &overviewSource{b: b}
	open := func(key string) *overviewKind {
		p, ok := b.Profile(key)
		if !ok {
			return nil
		}
		if listable, known := b.Listable(p, nil); known && !listable {
			return nil // a watch the API server refuses would only retry
		}
		k := &overviewKind{profile: p, handle: acquire(p), entries: map[string]*overviewEntry{}}
		src.unsubs = append(src.unsubs, k.handle.Subscribe(func(cache.Event) { onChange() }))
		return k
	}
	src.nodes, src.pods, src.events = open(kindNodes), open(kindPods), open(eventsKind)
	for _, key := range overviewWorkloads {
		if k := open(key); k != nil {
			src.workloads = append(src.workloads, k)
		}
	}
	return src
}

// kinds lists the page's kinds that are open.
func (src *overviewSource) kinds() []*overviewKind {
	out := make([]*overviewKind, 0, 3+len(src.workloads))
	for _, k := range []*overviewKind{src.nodes, src.pods, src.events} {
		if k != nil {
			out = append(out, k)
		}
	}
	return append(out, src.workloads...)
}

func (src *overviewSource) release() {
	for _, u := range src.unsubs {
		u()
	}
	for _, k := range src.kinds() {
		k.handle.Release()
	}
}

// synced reports whether every kind of the page finished its initial list.
func (src *overviewSource) synced() bool {
	for _, k := range src.kinds() {
		if !k.handle.HasSynced() {
			return false
		}
	}
	return true
}

// waitSynced waits, bounded by ctx, for every kind's initial list.
func (src *overviewSource) waitSynced(ctx context.Context) {
	for _, k := range src.kinds() {
		k.handle.WaitForSync(ctx)
	}
}

// errText names the kinds the page cannot read: "Events: forbidden ...".
func (src *overviewSource) errText() string {
	var parts []string
	for _, k := range src.kinds() {
		if err := k.handle.Err(); err != nil {
			parts = append(parts, k.profile.Title+": "+err.Error())
		}
	}
	return strings.Join(parts, "; ")
}

// issuesTable is the page's table: the issues in the selected namespaces (a cluster-scoped
// object's always) that match the search and the status filter, sorted.
func issuesTable(context string, ob *overviewBuild, p viewParams) *TableSnapshot {
	var nsSet map[string]struct{}
	if len(p.namespaces) > 0 {
		nsSet = setOf(p.namespaces)
	}
	match, filterErr := p.matcher()
	states := newStateFilter(issuesProfile, p.states)
	rows := make([]model.Row, 0, len(ob.issues))
	for _, is := range ob.issues {
		if !inNamespaces(is, nsSet) {
			continue
		}
		row := issueRow(context, is)
		if !match.All() && !match.Match(row) {
			continue
		}
		if !states.keep(states.spec.Of(&row)) {
			continue
		}
		rows = append(rows, row)
	}
	SortRows(rows, issuesProfile.Columns, p.sort)
	return &TableSnapshot{
		View: ClusterOverview, Context: context, Title: pageTitles[ClusterOverview], Namespaced: true,
		Columns: ColumnsShown(issuesProfile.Columns, p.hidden, p.hiddenSet), Order: ColumnOrder(issuesProfile.Columns, p.order),
		Rows: rows, Sort: p.sort,
		Namespaces: slices.Clone(p.namespaces), Filter: p.filter, FilterOptions: p.filterOpts, FilterError: filterErr, Total: len(ob.issues),
		States: slices.Clone(p.states), StateChoices: states.choices(),
		Synced: ob.synced, Error: ob.errText,
	}
}

// inNamespaces reports whether an issue is in the selected namespaces; one of a cluster-scoped
// object always is (Cogpit's choice: a node's issue concerns every namespace).
func inNamespaces(is ClusterIssue, nsSet map[string]struct{}) bool {
	if nsSet == nil || is.Ref.Namespace == "" {
		return true
	}
	_, ok := nsSet[is.Ref.Namespace]
	return ok
}

// issueRow is an issue as a row of the page's table: Name (a link to the row's own object, which
// opens its details), Kind, Namespace (a link that narrows the namespace filter), Message in the
// issue's severity colour, Age (since the issue began).
func issueRow(context string, is ClusterIssue) model.Row {
	ref := is.Ref
	ref.Context = context
	self := ref
	ns := model.Cell{Text: ref.Namespace}
	if ref.Namespace != "" {
		ns.Link = &model.ObjectRef{Kind: kindNamespaces, Name: ref.Namespace}
	}
	return model.Row{
		Ref: ref,
		Cells: []model.Cell{
			{Text: is.Name, Link: &self},
			{Text: is.Kind},
			ns,
			{Text: is.Message, Status: is.Severity},
			model.AgeCell(is.Since),
		},
		Status: is.Severity,
	}
}

// ClusterOverview returns the cluster Overview for the given namespaces (nil: the namespace filter
// the user has set). While the page is open it is the page's latest build and its charts; otherwise
// it is built once from the cache: the page's handles are held for the call, which waits (bounded by
// ctx and 15 seconds) for their initial lists, and released after. Without the page the charts
// carry the current values only (one Metrics Server list at most, cached for 15 seconds): the
// cluster's history is asked of Prometheus only while the page is shown. With series, the charts
// carry their points.
func (s *Session) ClusterOverview(ctx context.Context, contextID string, namespaces []string, series bool) (*ClusterOverviewModel, error) {
	b, err := s.TargetBackend(contextID) // the context's cluster, "" the shown one (D-69)
	if err != nil {
		return nil, err
	}
	u := s.UIState()
	if namespaces == nil {
		namespaces = s.windowNamespaces(b)
	}
	params := viewParams{namespaces: slices.Clone(namespaces), sort: issuesSort}
	s.state.mu.Lock()
	page := s.state.cluster
	s.state.mu.Unlock()
	if page != nil && page.backend == b {
		if ob := page.build.Load(); ob != nil {
			m := overviewModel(ob, params, u.Overview)
			page.charts.fill(m, ob, series)
			return m, nil
		}
	}
	wctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	// Once the kinds' plans are decided, following a refusal in all namespaces (accessiblens_follow.go).
	src := openFollowing(wctx, b, clusterOverviewKinds, func() (*overviewSource, []kindHandle) {
		src := newOverviewSource(b, b.Acquire, func() {})
		open := src.kinds()
		reads := make([]kindHandle, 0, len(open))
		for _, k := range open {
			reads = append(reads, kindHandle{k.profile, k.handle})
		}
		return src, reads
	}, (*overviewSource).release)
	defer src.release()
	src.waitSynced(wctx)
	ob := src.build(time.Now())
	m := overviewModel(ob, params, u.Overview)
	currentCharts(ctx, b, m, ob, s.hiddenMetrics(b.Context))
	return m, nil
}

// overviewModel is the model of a build for namespaces and a choice, without its charts' metrics.
func overviewModel(ob *overviewBuild, p viewParams, choice OverviewChoice) *ClusterOverviewModel {
	m := &ClusterOverviewModel{Synced: ob.synced, Error: ob.errText, Issues: []ClusterIssue{}}
	var nsSet map[string]struct{}
	if len(p.namespaces) > 0 {
		nsSet = setOf(p.namespaces)
	}
	for _, is := range ob.issues {
		if inNamespaces(is, nsSet) {
			m.Issues = append(m.Issues, is)
		}
	}
	slices.SortStableFunc(m.Issues, func(a, b ClusterIssue) int { return b.Since.Compare(a.Since) })
	m.Groups = []OverviewNodeGroup{
		{Name: NodesMaster, Title: "Master", Nodes: ob.groups[NodesMaster].nodes},
		{Name: NodesWorker, Title: "Worker", Nodes: ob.groups[NodesWorker].nodes},
	}
	m.NodeStates, m.PodStates = statusCounts(ob.nodeStates), statusCounts(ob.podStates)
	m.Nodes = nodeGroupOf(choice, ob)
	m.Window = chartWindowOf(choice)
	m.Range = FormatChartRange(m.Window)
	return m
}

// statusCounts lists counts by state in StatusOrder (healthy first), zero counts left out.
func statusCounts(by map[model.Status]int) []StatusCount {
	out := make([]StatusCount, 0, len(by))
	for _, st := range StatusOrder {
		if n := by[st]; n > 0 {
			out = append(out, StatusCount{Status: st, Count: n})
		}
	}
	return out
}

func setOf(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}

// nodeGroupOf is the group the charts show: the chosen one, else the workers when the cluster has
// any, else the masters.
func nodeGroupOf(choice OverviewChoice, ob *overviewBuild) string {
	switch choice.Nodes {
	case NodesMaster, NodesWorker:
		return choice.Nodes
	}
	if ob != nil && ob.groups[NodesWorker].nodes == 0 && ob.groups[NodesMaster].nodes > 0 {
		return NodesMaster
	}
	return NodesWorker
}

// chartWindowOf is the charts' time range: the chosen one, else the default hour.
func chartWindowOf(choice OverviewChoice) time.Duration {
	if d, err := ParseChartRange(choice.Range); err == nil && d > 0 {
		return d
	}
	return DefaultOverviewWindow
}

// DefaultOverviewWindow is the Overview charts' time range until one is chosen: Lens's shortest,
// an hour.
const DefaultOverviewWindow = time.Hour

// reconcileClusterPageLocked runs the cluster Overview's worker: started when the page opens, given
// the UI's namespaces, search, sort, columns and the charts' choice, stopped when another view opens
// or the backend changes, and started again when the access reviews change which kinds it reads or
// how (pageReadKey, A98). The page's sort is remembered as a view's is.
func (st *state) reconcileClusterPageLocked(b *Backend, now UIState) {
	want := b != nil && now.View == ClusterOverview
	var key string
	if want {
		key = b.pageReadKey(clusterOverviewKinds, true)
	}
	if st.cluster != nil && (!want || st.cluster.backend != b || st.cluster.planKey != key) {
		st.cluster.stop()
		st.cluster = nil
	}
	switch {
	case want && st.cluster == nil:
		st.cluster = st.s.startClusterOverview(b, paramsOf(now), now.Overview)
		st.cluster.planKey = key
	case st.cluster != nil:
		st.cluster.setParams(paramsOf(now), now.Overview)
	}
	if st.cluster != nil && issuesProfile.ColumnIndex(now.Sort.Column) >= 0 {
		st.sorts[now.View] = now.Sort
	}
}
