package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
)

// ErrClosed is returned by a Session that was closed.
var ErrClosed = errors.New("the session is closed")

// state holds the pool of connected backends (pool.go, D-69) and the window's view state: the
// backend the window shows, the open view with its cache handle and coalesced snapshots, the
// pages, the drawer's watch; and the diagnostics loop. R3's windows (D-52) later get a view state
// each; the pool stays one.
//
// Lock order: state.mu before Session.mu. Session.mu is never held while state.mu is taken
// (UpdateUI releases it before calling uiChanged); state.mu may be held while Session.mu is taken
// briefly to read the UI state (customKindsChanged reconciles against the latest one). state.mu is
// never held while emitting events or building snapshots.
type state struct {
	s *Session

	// connMu serialises Connect calls and Disconnect of the shown cluster, so that the UI state
	// follows one connect at a time.
	connMu sync.Mutex

	mu            sync.Mutex
	closed        bool
	connectSeq    uint64
	connectCancel context.CancelCauseFunc // the window's connect's wait: errConnectSuperseded, errConnectCancelled (Disconnect)
	connectWait   *windowConnect          // the window's latest connect while it runs (connect_join.go)
	pool          map[string]*pooled      // every connected backend, by context id (pool.go)
	// runs are the connects under way, by context id (connect_run.go, D-97): the one the window
	// waits for and those in the background (the restore, or one the window stopped waiting for).
	runs  map[string]*connectRun
	bgGen uint64 // the last connection's number (newGenLocked)
	// reconnects are the reconnects under way, by context id (reconnect.go, D-81): a disconnect
	// ends one.
	reconnects map[string]*reconnectRun

	// The window's view state. shown is the backend the window shows, one of the pool's; nil while
	// none is (connecting, failed, disconnected). The workers below read it only.
	shown      *Backend
	appliedRev uint64
	view       *viewWorker
	page       *overviewWorker        // the open page's worker (overview.go); nil unless a page is open
	cluster    *clusterOverviewWorker // the cluster Overview's worker (clusteroverview.go); nil unless it is open
	flux       *fluxDashboardWorker   // the Flux dashboard's worker (fluxdashboard.go); nil unless it is open
	argo       *argoDashWorker        // the Argo CD Dashboard's worker (argocd_dashboard.go); nil unless it is open
	vanished   *TableSnapshot         // the open view after the cluster stopped serving its kind (customkinds.go)
	drawer     *drawerWatch
	drawerSort map[string]SortSpec  // the drawer tables' order last applied (UIState.DrawerSort)
	diagStop   chan struct{}        // the diagnostics loop's, while the pool holds a backend
	sorts      map[string]SortSpec  // the sort order remembered per view
	rowCaches  map[string]*rowCache // rows per view of the shown backend, reused across visits
	dockSeq    map[DockType]int

	logs connectLogs // the last connect attempt of each context (connectlog.go); has its own lock

	snapshots atomic.Uint64 // table snapshots built since start; also the snapshots' revisions
	wg        sync.WaitGroup
}

func newState(s *Session) *state {
	return &state{
		s:         s,
		sorts:     map[string]SortSpec{},
		rowCaches: map[string]*rowCache{},
		dockSeq:   map[DockType]int{},
	}
}

// uiChanged follows a UI state change: start the view worker of a newly opened view and stop the
// old one, hand namespaces, filter and sort to the worker, and follow the drawer. It reconciles
// against the new state rather than the difference, so calls that arrive out of order (two
// goroutines updating the UI at once) are harmless: an older revision is ignored.
func (st *state) uiChanged(_, now UIState) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed || now.Revision < st.appliedRev {
		return
	}
	st.appliedRev = now.Revision
	st.reconcileLocked(now)
}

func (st *state) reconcileLocked(now UIState) {
	for _, e := range st.pool {
		st.followIntegrations(e.b, now) // each cluster's integrations, the Metrics source first, before anything polls (integrations.go)
	}
	b := st.shown
	if b != nil && now.Context != b.Context {
		// The UI state does not name the shown backend yet (a pooled cluster being shown,
		// showConnected): no view of the other cluster's state starts on it.
		b = nil
	}

	// The view: one worker for the open kind of the current backend, read in all namespaces or in
	// each namespace the user named (accessiblens_plan.go); a new plan reads the kind again.
	var prof *model.KindProfile
	var plan nsPlan
	var relKey string // how its related-objects indexes read their kinds (accessiblens_plan.go)
	if b != nil {
		prof, _ = b.Profile(now.View)
		b.ns.setInputs(now.ClusterSettings[b.Context].AccessibleNamespaces, now.Namespaces)
		plan = b.namespacePlan(prof)
		if prof != nil {
			relKey = b.relatedKey(prof, prof.Needs, "")
		}
	}
	st.noteVanishedLocked(b, prof, now.View)
	if st.view != nil && (st.view.backend != b || st.view.profile != prof || st.view.plan.key() != plan.key() || st.view.relKey != relKey) {
		st.view.stop()
		st.view = nil
	}
	if st.view == nil && prof != nil {
		rc := st.rowCaches[prof.Key]
		if rc == nil {
			rc = &rowCache{}
			st.rowCaches[prof.Key] = rc
		}
		st.view = st.s.startViewWorker(b, prof, rc, paramsOf(now), plan)
		st.view.relKey = relKey
	} else if st.view != nil {
		st.view.setParams(paramsOf(now))
	}
	if prof != nil && b.hasViewColumn(prof, now.Sort.Column) { // an integration's column too (overlay.go)
		st.sorts[prof.Key] = now.Sort
	}
	st.reconcilePageLocked(b, now)
	st.reconcileClusterPageLocked(b, now)
	st.reconcileFluxPageLocked(b, now)
	st.reconcileArgoPageLocked(b, now)

	// The drawer: a watch on its object and on the events. A custom kind's profile changes with its
	// definition (another version, or gone), and the drawer follows it.
	var drawerProf *model.KindProfile
	var drawerPlan string
	if b != nil && now.Drawer != nil {
		drawerProf, _ = b.Profile(now.Drawer.Kind)
		drawerPlan = b.drawerPlanKey(drawerProf, now.Drawer.Namespace)
	}
	if st.drawer != nil && (now.Drawer == nil || st.drawer.backend != b || st.drawer.ref != *now.Drawer || st.drawer.profile != drawerProf ||
		st.drawer.planKey != drawerPlan) {
		st.drawer.stop()
		st.drawer = nil
	}
	if st.drawer == nil && drawerProf != nil {
		st.drawer = st.s.startDrawerWatch(b, drawerProf, *now.Drawer, hiddenMetricsOf(&now, b.Context))
		st.drawer.planKey = drawerPlan
	} else if st.drawer != nil {
		st.drawer.followHidden(st.s, hiddenMetricsOf(&now, b.Context)) // the cluster's settings changed what shows
	}
	// A table of the drawer sorted anew (ui.sortDrawerTable, drawersort.go): the drawer's model
	// changes, and the window and agents hear it as they hear a change of the object.
	if !maps.Equal(st.drawerSort, now.DrawerSort) {
		st.drawerSort = maps.Clone(now.DrawerSort)
		if st.drawer != nil {
			signal(st.drawer.wake)
		}
	}
}

// close disconnects every cluster and stops every loop. It waits a bounded time for the engine's
// goroutines.
func (st *state) close() {
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		return
	}
	st.closed = true
	if st.connectCancel != nil {
		st.connectCancel(ErrClosed)
	}
	for _, r := range st.runs {
		r.cancel(ErrClosed) // its credential plugin's run ends with it (D-73)
	}
	st.detachViewsLocked()
	var all []*Backend
	for id := range st.pool {
		all = append(all, st.removeLocked(id))
	}
	st.mu.Unlock()
	for _, b := range all {
		b.close(st.s.log)
	}

	done := make(chan struct{})
	go func() {
		st.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		st.s.log.Warn("engine: goroutines still running after close")
	}
}

// Backend returns the backend of the cluster the window shows, or nil while none is (D-69: other
// clusters may be connected; BackendOf returns any connected context's). The window's views and
// commands on the window's state use it; a command on an object or with a context parameter uses
// its cluster's (TargetBackend). It is shared: callers must not modify it.
func (s *Session) Backend() *Backend {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	return s.state.shown
}

// Contexts lists the contexts the connector offers. With a partial result (the kubeconfig failed
// to load but the demo cluster is offered), both the list and the error are returned.
func (s *Session) Contexts() ([]ContextInfo, error) {
	if s.opts.Connector == nil {
		return nil, errors.New("no connector configured")
	}
	var (
		out []ContextInfo
		err error
	)
	if pErr := s.safeCall("cluster.contexts", func() error { out, err = s.opts.Connector.Contexts(); return nil }); pErr != nil {
		return nil, pErr
	}
	s.markConnectProblems(out)
	s.markClientCerts(out)
	return out, err
}

// markClientCerts sets each client certificate's state at this moment (D-75): a copy of the
// validity the kubeconfig sources described, so that a list kept elsewhere is not changed.
func (s *Session) markClientCerts(list []ContextInfo) {
	now := s.opts.Now()
	for i := range list {
		if v := list[i].ClientCert; v != nil {
			list[i].ClientCert = v.At(now)
		}
	}
}

// Connect connects a context through the connector and opens the default view: Pods if the
// cluster serves them, else its first kind. The UI state follows the connection's lifecycle:
// Connecting, then Connected or ConnectError with the message.
//
// Several clusters stay connected (D-69, pool.go): a context that is connected already is only
// shown, from its cache, without a request (one whose login program changed since it was allowed
// is connected anew, D-26); the cluster shown before stays connected, and the window's views on it
// stop when the connect starts, so that UIState.Context always names the cluster the window shows.
// The connect runs on its own (connect_run.go, D-97): a later Connect, or a Show, ends only this
// wait, and the connect goes on in the background (ErrConnectInBackground); Disconnect cancels it
// (ErrConnectCancelled); a newer connect or show before it began supersedes it
// (ErrConnectSuperseded); none of the three is a failure (ConnectNotFailed). A failed connect leaves
// the window on its cluster with the error, never on the cluster shown before. The tab a connected
// cluster shows again comes with its selection, without the objects gone meanwhile
// (DropGoneSelected).
func (s *Session) Connect(ctx context.Context, name string) error {
	if err := s.connect(ctx, name); err != nil {
		return err
	}
	s.DropGoneSelected(ctx, s.UIState())
	return nil
}

func (s *Session) connect(ctx context.Context, name string) (err error) {
	st := s.state
	connector := s.opts.Connector
	if connector == nil {
		return errors.New("no connector configured")
	}
	if r, ok := connector.(contextResolver); ok {
		// A bare context name (--context kind-dev) names the one source context that has it (D-28).
		resolved, err := r.ResolveContext(name)
		if err != nil {
			return fmt.Errorf("connect %s: %w", name, err)
		}
		name = resolved
	}
	cctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		return ErrClosed
	}
	if w := st.connectWait; w != nil && w.name == name && w.ctx.Err() == nil {
		// The window's connect waits for this cluster already: this one joins that wait and
		// answers as it does, rather than ending it as a connect of another cluster would.
		w.joined++
		st.mu.Unlock()
		return w.join(ctx)
	}
	if st.connectCancel != nil {
		// The window's connect waiting so far stops waiting; its run goes on in the background
		// (connect_run.go, D-97).
		st.connectCancel(errConnectSuperseded)
	}
	st.connectSeq++
	seq := st.connectSeq
	st.connectCancel = cancel
	w := &windowConnect{name: name, ctx: cctx, done: make(chan struct{})}
	st.connectWait = w
	st.mu.Unlock()
	defer func() { st.endWindowConnect(w, err) }()

	st.connMu.Lock()
	defer st.connMu.Unlock()
	if !st.current(seq) {
		// Replaced before it began: by a newer connect or a show (ErrConnectSuperseded), by
		// Disconnect (ErrConnectCancelled) or by the session's close (ErrClosed).
		cause := context.Cause(cctx)
		if !errors.Is(cause, errConnectCancelled) && !errors.Is(cause, ErrClosed) {
			cause = errConnectSuperseded
		}
		return fmt.Errorf("connect %s: %w", name, cause)
	}

	st.mu.Lock()
	if e := st.pool[name]; e != nil && !e.b.watch.needsNewConnect() {
		// Connected already (D-69): only shown, from its cache; nothing is asked of the cluster.
		st.showLocked(e)
		st.mu.Unlock()
		s.showConnected(e.b, e.gen)
		s.applyLoginHolds() // shown: its login is renewed again, a lapsed one now (connwatch_lapse.go)
		return nil
	}
	stale := st.removeLocked(name) // its login program changed since it was allowed: connected anew (D-26)
	st.leaveLocked()               // the cluster shown so far stays connected (D-69)
	// The connect under way for the context, in the background (the restore, or one the window
	// stopped waiting for), is the one to show; else a new one (connect_run.go).
	run, fresh := st.runs[name], false
	if run == nil {
		run, fresh = st.newRunLocked(ctx, name, false), true
	}
	run.watchSeq = seq
	st.mu.Unlock()
	stale.close(s.log)
	s.applyLoginHolds() // the cluster shown so far may hold its login now
	s.UpdateUI(func(u *UIState) {
		if stale != nil {
			u.dropConnection(name, 0)
		}
		conn := Connection{State: Connecting}
		if c, ok := u.ConnectionOf(name); ok && c.Connection.State == Connecting {
			conn.WaitingForLogin, conn.Reason = c.Connection.WaitingForLogin, c.Connection.Reason // what it waits for already
		}
		if u.Context != name {
			u.leaveCluster() // no filter crosses to another cluster (WF20)
		}
		u.Context = name
		u.Connection = conn
		u.View = ""
		u.Selected = nil
		u.Drawer = nil
		u.Filter = ""
	})
	if fresh {
		s.startConnectRun(run)
	}
	return s.awaitRun(cctx, run, seq)
}

// finishCancelledConnect ends the attempt of a connect that Disconnect cancelled (Cancel
// Connecting, D-73), with how its login ended when it waited for one.
func finishCancelledConnect(logs *connectLogs, attempt *ConnectAttempt, err error) {
	text := "Connecting cancelled (Disconnect)"
	var ended *cluster.LoginEndedError
	if errors.As(err, &ended) {
		text += ": " + ended.Error()
	}
	logs.finish(attempt, OutcomeCancelled, LogInfo, text)
}

// current reports whether the connect with sequence number seq is still the latest.
func (st *state) current(seq uint64) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return !st.closed && st.connectSeq == seq
}

// defaultView is the view a fresh connection opens: Pods if served, else the first kind.
func defaultView(b *Backend) string {
	if _, ok := b.Profile("pods"); ok {
		return "pods"
	}
	if len(b.Profiles) > 0 {
		return b.Profiles[0].Key
	}
	return ""
}

// ViewSort returns the sort order for a view: the one last used for it, else the default (name
// ascending; events by last seen, newest first).
func (s *Session) ViewSort(view string) SortSpec {
	st := s.state
	st.mu.Lock()
	defer st.mu.Unlock()
	p := st.shown.viewProfile(view) // a kind's profile, or a page's table profile
	if sp, ok := st.sorts[view]; ok && (p == nil || st.shown.hasViewColumn(p, sp.Column)) {
		return sp
	}
	return defaultSort(p)
}

// DefaultSort is the sort order of a view nobody sorted yet: by name, ascending.
var DefaultSort = SortSpec{Column: "name"}

// lastSeenColumn is the events column the Events view sorts by by default, newest first.
const lastSeenColumn = "lastSeen"

func defaultSort(p *model.KindProfile) SortSpec {
	if p == issuesProfile { // the cluster Overview's Issues, newest first (clusteroverview.go)
		return issuesSort
	}
	if p == fluxIssuesProfile { // the Flux dashboard's objects, the latest change first (fluxdashboard.go)
		return fluxIssuesSort
	}
	if p != nil && p.Key == eventsKind && p.ColumnIndex(lastSeenColumn) >= 0 {
		return SortSpec{Column: lastSeenColumn, Desc: true}
	}
	return DefaultSort
}

// Table returns the latest snapshot of a view, or nil if the view is not open or has no snapshot
// yet. An open view whose kind the cluster stopped serving returns a snapshot without rows whose
// Error says so. Snapshots are immutable and shared: callers must not modify them.
func (s *Session) Table(view string) *TableSnapshot {
	s.state.mu.Lock()
	w := s.state.view
	page := s.state.page
	cluster := s.state.cluster
	flux := s.state.flux
	gone := s.state.vanished
	s.state.mu.Unlock()
	if page != nil && view == WorkloadsOverview {
		return page.snap.Load()
	}
	if cluster != nil && view == ClusterOverview {
		return cluster.snap.Load()
	}
	if flux != nil && view == FluxDashboard {
		return flux.snap.Load()
	}
	if view == ArgoCDDashboard {
		return s.state.argoTable()
	}
	if w == nil || w.profile.Key != view {
		if gone != nil && gone.View == view {
			return gone
		}
		return nil
	}
	return w.snap.Load()
}

// NewDockID returns a fresh dock tab id for a tab type: "logs-1", "terminal-2".
func (s *Session) NewDockID(t DockType) string {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	s.state.dockSeq[t]++
	return string(t) + "-" + strconv.Itoa(s.state.dockSeq[t])
}

// viewParams are the UI state a view's snapshot depends on.
type viewParams struct {
	namespaces []string
	groups     []string // the Definitions view's API group filter
	hidden     []string // the view's hidden column ids; only when hiddenSet
	hiddenSet  bool     // false: the kind's default columns
	order      []string // the view's column ids in the user's order; nil: the kind's
	filter     string
	filterOpts FilterOptions // the search's toggles (filteroptions.go)
	states     []string      // the status filter (statusfilter.go)
	sort       SortSpec
	metrics    MetricSet // the metrics the cluster's settings hide (hidemetrics.go)
}

func paramsOf(u UIState) viewParams {
	hidden, set := u.HiddenColumns[u.View]
	return viewParams{
		namespaces: slices.Clone(u.Namespaces), groups: slices.Clone(u.Groups),
		hidden: slices.Clone(hidden), hiddenSet: set,
		order:  slices.Clone(u.ColumnOrder[u.View]),
		filter: u.Filter, filterOpts: u.FilterOptions, states: slices.Clone(u.States), sort: u.Sort,
		metrics: hiddenMetricsOf(&u, u.Context),
	}
}

func (p viewParams) equal(o viewParams) bool {
	return p.filter == o.filter && p.filterOpts == o.filterOpts && p.sort == o.sort && slices.Equal(p.namespaces, o.namespaces) &&
		slices.Equal(p.states, o.states) &&
		slices.Equal(p.groups, o.groups) && p.hiddenSet == o.hiddenSet && slices.Equal(p.hidden, o.hidden) &&
		slices.Equal(p.order, o.order) && p.metrics == o.metrics
}
