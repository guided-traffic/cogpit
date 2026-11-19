package engine

import (
	"context"
	"fmt"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Argo CD's Dashboard (resource-parity › Paid Lens Desktop views › Argo CD, G23): a page built from
// the cache. Lens's documentation (read, not captured) describes "a health strip with counts for
// Applications, Synced, Healthy, Out of sync, Degraded, Missing, and Progressing", "a card per
// resource kind", a "Needs attention" panel and a "Recent activity" timeline, with Sync and Refresh
// of several Applications on the Applications card. Cogpit's page: the strip and the kinds' cards
// in its model (ArgoCDDashboardModel), Needs attention as the page's table (the Applications not
// Healthy or not Synced, with their message: sortable, searchable, selectable, a row opens the
// Application's drawer), the recent activity from the Applications' operation states and histories.
// The layout is Cogpit's own (inferred).

// ArgoCDDashboard is the page key of the Argo CD Dashboard (Argo CD › Dashboard in the navigator).
const ArgoCDDashboard = "argocd/dashboard"

func init() { pageTitles[ArgoCDDashboard] = "Dashboard" }

// argoDashKinds are the kinds the Dashboard reads, in its cards' order.
var argoDashKinds = []string{
	kinds.ArgoApplicationsKey, kinds.ArgoApplicationSetKey, kinds.ArgoProjectsKey, kinds.ArgoRepositoriesKey, kinds.ArgoClustersKey,
}

// argoActivityShown bounds the Dashboard's recent activity.
const argoActivityShown = 20

// argoMessageColumn is the Applications' Message column, shown in Needs attention.
const argoMessageColumn = "message"

// ArgoCDDashboardModel is the Dashboard's upper part; Needs attention is the page's table
// (Session.Table, or argocd.dashboard with attention).
type ArgoCDDashboardModel struct {
	// Served is false when the cluster does not serve Argo CD's Application kind.
	Served bool `json:"served"`
	// Namespaces are the namespaces counted; empty: all.
	Namespaces []string `json:"namespaces,omitempty"`
	// Strip is Lens's health strip: Applications, Synced, Healthy, Out of sync, Degraded, Missing,
	// Progressing, each with its count.
	Strip []ArgoTile `json:"strip"`
	// Health and Sync count the Applications by status.health.status and status.sync.status, in
	// Argo CD's order, zero counts left out; "" counts those not assessed yet.
	Health []ArgoTile `json:"health,omitempty"`
	Sync   []ArgoTile `json:"sync,omitempty"`
	// Kinds are the cards: one per Argo CD kind the cluster serves, its count and health bar.
	Kinds []OverviewKind `json:"kinds"`
	// OutOfSync and All are the Applications the Applications card's Sync and Refresh take (Lens:
	// a sync preselects the out-of-sync Applications, a refresh all of them).
	OutOfSync []model.ObjectRef `json:"outOfSync,omitempty"`
	All       []model.ObjectRef `json:"all,omitempty"`
	// Activity is the recent activity, newest first, at most 20.
	Activity []ArgoActivityItem `json:"activity,omitempty"`
	// Synced is false while a kind is still being listed.
	Synced bool   `json:"synced"`
	Error  string `json:"error,omitempty"`
}

// ArgoTile is one count of the Dashboard: what it counts, how many, its colour, and the state of
// the Applications view's status filter that shows them ("Sync: OutOfSync", "Health: Degraded":
// ui.openView with these states, D-34; "" opens the view unfiltered).
type ArgoTile struct {
	Label  string       `json:"label"`
	Count  int          `json:"count"`
	Status model.Status `json:"status,omitempty"`
	State  string       `json:"state,omitempty"`
}

// ArgoActivityItem is one entry of the recent activity: an Application's operation or sync.
type ArgoActivityItem struct {
	At       time.Time       `json:"at"`
	Ref      model.ObjectRef `json:"ref"`
	Text     string          `json:"text"`
	Status   model.Status    `json:"status,omitempty"`
	Revision string          `json:"revision,omitempty"`
}

// argoAttentionProfiles holds the Needs attention table's profile per Applications profile.
var argoAttentionProfiles sync.Map // *model.KindProfile → *model.KindProfile

// argoAttentionProfile is the Needs attention table's profile: the Applications' own, its key
// unchanged so rows name Applications, with Name, Namespace, Sync, Health, Message and Age shown
// and the rest hidden. Nil when the cluster serves no Applications.
func argoAttentionProfile(b *Backend) *model.KindProfile {
	if b == nil {
		return nil
	}
	src, ok := b.Profile(kinds.ArgoApplicationsKey)
	if !ok {
		return nil
	}
	if p, ok := argoAttentionProfiles.Load(src); ok {
		return p.(*model.KindProfile)
	}
	p := *src
	p.Columns = slices.Clone(src.Columns)
	for i, c := range p.Columns {
		switch c.ID {
		case metadataName, columnNamespace, argoSyncColumn, argoHealthColumn, argoMessageColumn, "age":
			p.Columns[i].Hidden = false
		default:
			p.Columns[i].Hidden = true
		}
	}
	actual, _ := argoAttentionProfiles.LoadOrStore(src, &p)
	return actual.(*model.KindProfile)
}

// argoNeedsAttention reports whether an Application belongs in Needs attention: not Healthy, or
// not Synced (one not assessed yet included), or its last operation failed.
func argoNeedsAttention(app *unstructured.Unstructured) bool {
	if kinds.ArgoHealthStatus(app) != kinds.ArgoHealthy || kinds.ArgoSyncStatus(app) != kinds.ArgoSynced {
		return true
	}
	switch kinds.ArgoOperationPhase(app) {
	case "Failed", "Error":
		return true
	}
	return false
}

// argoDashWorker keeps the Dashboard current while it is open: a handle on each served kind, a
// rebuild at most once per SnapshotInterval after a change.
type argoDashWorker struct {
	s       *Session
	backend *Backend
	table   *model.KindProfile // nil without Applications
	rows    *rowCache
	kinds   []*argoDashKind
	unsubs  []func()
	planKey string // how the page reads its kinds (Backend.pageReadKey); the state's lock

	mu     sync.Mutex
	params viewParams

	snap     atomic.Pointer[TableSnapshot]
	model    atomic.Pointer[ArgoCDDashboardModel]
	wake     chan struct{}
	quit     chan struct{}
	stopped  atomic.Bool
	panicked atomic.Bool
}

type argoDashKind struct {
	profile *model.KindProfile
	handle  *cache.Handle
}

// newArgoDashWorker acquires the page's handles through acquire (the open page's acquireForPage,
// an agent's read's Acquire once the plans are decided); with notify, every change wakes the loop.
func (s *Session) newArgoDashWorker(b *Backend, params viewParams, notify bool, acquire func(*model.KindProfile) *cache.Handle) *argoDashWorker {
	w := &argoDashWorker{s: s, backend: b, params: params, rows: &rowCache{}, table: argoAttentionProfile(b),
		wake: make(chan struct{}, 1), quit: make(chan struct{})}
	onChange := func() {}
	if notify {
		wake := w.wake
		onChange = func() { signal(wake) }
	}
	for _, key := range argoDashKinds {
		p, ok := b.Profile(key)
		if !ok {
			continue
		}
		k := &argoDashKind{profile: p, handle: acquire(p)}
		w.unsubs = append(w.unsubs, k.handle.Subscribe(func(cache.Event) { onChange() }))
		w.kinds = append(w.kinds, k)
	}
	return w
}

func (s *Session) startArgoDashboard(b *Backend, params viewParams) *argoDashWorker {
	w := s.newArgoDashWorker(b, params, true, b.acquireForPage) // nothing while a plan is pending
	signal(w.wake)
	s.goSafe("argo cd dashboard", func() {
		runCoalesced(w.quit, w.wake, s.opts.SnapshotInterval, w.unsettled, w.rebuild)
	})
	return w
}

func (w *argoDashWorker) setParams(p viewParams) {
	w.mu.Lock()
	changed := !w.params.equal(p)
	w.params = p
	w.mu.Unlock()
	if changed {
		signal(w.wake)
	}
}

func (w *argoDashWorker) stop() {
	if !w.stopped.CompareAndSwap(false, true) {
		return
	}
	close(w.quit)
	w.release()
}

// kindHandles are the worker's handles, for openFollowing.
func (w *argoDashWorker) kindHandles() []kindHandle {
	out := make([]kindHandle, 0, len(w.kinds))
	for _, k := range w.kinds {
		out = append(out, kindHandle{k.profile, k.handle})
	}
	return out
}

func (w *argoDashWorker) release() {
	for _, u := range w.unsubs {
		u()
	}
	for _, k := range w.kinds {
		k.handle.Release()
	}
}

// synced reports whether every kind the page reads has listed; a page that reads no Applications
// is not synced either while the definitions are still being listed (build says so too).
func (w *argoDashWorker) synced() bool {
	for _, k := range w.kinds {
		if !k.handle.HasSynced() {
			return false
		}
	}
	return w.table != nil || w.backend.CustomKindsSettled()
}

// unsettled reports whether a sync state changed since the last build: it comes with no event,
// nor does the end of the definitions' list for a page that reads no Applications.
func (w *argoDashWorker) unsettled() bool {
	m := w.model.Load()
	return m == nil || m.Synced != w.synced() || (w.table == nil && m.Error != w.backend.customKindsUnknown(argoKindsWhat))
}

// argoKindsWhat names Argo CD's kinds in the text of a page that cannot tell whether the cluster
// serves them.
const argoKindsWhat = "Argo CD Applications"

func (w *argoDashWorker) rebuild() {
	defer func() {
		if r := recover(); r != nil {
			w.s.log.Error("engine: building the Argo CD dashboard failed", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			if w.panicked.CompareAndSwap(false, true) {
				w.s.emit(Event{Type: EventToast, Level: LevelError, Message: "the Argo CD dashboard failed to update: internal error"})
			}
		}
	}()
	w.mu.Lock()
	p := w.params
	w.mu.Unlock()
	m, snap := w.build(p)
	if w.stopped.Load() {
		return
	}
	w.model.Store(m)
	w.snap.Store(snap)
	w.s.emit(Event{Type: EventTable, View: ArgoCDDashboard, Revision: snap.Revision})
}

// build computes the model and the Needs attention table for the given namespaces, search and sort.
func (w *argoDashWorker) build(p viewParams) (*ArgoCDDashboardModel, *TableSnapshot) {
	nsSet := setOf(p.namespaces)
	in := func(o *unstructured.Unstructured) bool {
		if len(nsSet) == 0 {
			return true
		}
		_, ok := nsSet[o.GetNamespace()]
		return ok
	}
	m := &ArgoCDDashboardModel{Namespaces: slices.Clone(p.namespaces), Synced: true, Kinds: []OverviewKind{}, Strip: []ArgoTile{}}
	var apps []*unstructured.Unstructured
	for _, k := range w.kinds {
		synced := k.handle.HasSynced()
		objs := KindList(k.handle, k.profile)
		counts := map[model.Status]int{}
		n := 0
		for _, o := range objs {
			if !in(o) {
				continue
			}
			counts[model.BuildRow(k.profile, w.backend.Context, o).Status]++
			n++
			if k.profile.Key == kinds.ArgoApplicationsKey {
				apps = append(apps, o)
			}
		}
		ok := OverviewKind{Kind: k.profile.Key, Title: k.profile.Title, Count: n, Synced: synced, Error: errText(k.handle.Err())}
		for _, st := range StatusOrder {
			if c := counts[st]; c > 0 {
				ok.Statuses = append(ok.Statuses, StatusCount{Status: st, Count: c})
			}
		}
		m.Kinds = append(m.Kinds, ok)
		m.Synced = m.Synced && synced
		if k.profile.Key == kinds.ArgoApplicationsKey {
			m.Served, m.Error = true, ok.Error
		}
	}
	argoApplicationsSummary(m, w.backend.Context, apps, argoStateNamer(w.backend))

	var snap *TableSnapshot
	if w.table != nil {
		attention := make([]*unstructured.Unstructured, 0, len(apps))
		for _, a := range apps {
			if argoNeedsAttention(a) {
				attention = append(attention, a)
			}
		}
		snap = w.rows.build(w.table, w.backend.Context, attention, p, model.NoRelated, 0)
		snap.Synced = m.Synced
		snap.Error = m.Error
	} else {
		snap = &TableSnapshot{Rows: []model.Row{}, Synced: true, Namespaced: true, Error: "The cluster serves no Argo CD Applications."}
		// A user who may not list the definitions: whether Argo CD is there is unknown.
		if why := w.backend.customKindsUnknown(argoKindsWhat); why != "" {
			snap.Error, m.Error = why, why
		}
		// While the definitions are still being listed, nothing is known yet: not synced (an agent's
		// read right after connecting saw served false and synced true).
		if !w.backend.CustomKindsSettled() {
			snap.Synced, m.Synced = false, false
		}
	}
	snap.View, snap.Title = ArgoCDDashboard, pageTitles[ArgoCDDashboard] // the table's own heading is the window's
	snap.Revision = w.s.state.snapshots.Add(1)
	return m, snap
}

// argoHealthOrder and argoSyncOrder are Argo CD's states in the order the Dashboard counts them.
var (
	argoHealthOrder = []string{kinds.ArgoHealthy, kinds.ArgoProgressing, kinds.ArgoDegraded, kinds.ArgoSuspended, kinds.ArgoMissing, condUnknown, ""}
	argoSyncOrder   = []string{kinds.ArgoSynced, kinds.ArgoOutOfSync, "Unknown", ""}
)

// argoStateNamer names an Application's sync or health status as the Applications view's status
// filter does: qualified by its column's title ("Health: Degraded"), the view having both columns.
func argoStateNamer(b *Backend) func(column, status string) string {
	p, ok := b.Profile(kinds.ArgoApplicationsKey)
	if !ok {
		return func(_, status string) string { return status }
	}
	spec := model.StatesOf(p)
	return func(column, status string) string { return spec.Qualified(p, column, status) }
}

// argoApplicationsSummary fills the strip, the counts by health and sync, the Applications the
// card's Sync and Refresh take, and the recent activity; state names a status as the Applications
// view's status filter does (argoStateNamer).
func argoApplicationsSummary(m *ArgoCDDashboardModel, context string, apps []*unstructured.Unstructured, state func(column, status string) string) {
	health, sync := map[string]int{}, map[string]int{}
	var activity []ArgoActivityItem
	for _, a := range apps {
		h, s := kinds.ArgoHealthStatus(a), kinds.ArgoSyncStatus(a)
		health[h]++
		sync[s]++
		ref := model.ObjectRef{Context: context, Kind: kinds.ArgoApplicationsKey, Namespace: a.GetNamespace(), Name: a.GetName()}
		m.All = append(m.All, ref)
		if s == kinds.ArgoOutOfSync {
			m.OutOfSync = append(m.OutOfSync, ref)
		}
		for _, act := range kinds.ArgoAppActivity(a) {
			activity = append(activity, ArgoActivityItem{At: act.At.UTC(), Ref: ref, Text: act.Text, Status: act.Status, Revision: act.Revision})
		}
	}
	argoSortRefs(m.All)
	argoSortRefs(m.OutOfSync)
	slices.SortStableFunc(activity, func(x, y ArgoActivityItem) int {
		if c := y.At.Compare(x.At); c != 0 {
			return c
		}
		return argoCompareRefs(x.Ref, y.Ref)
	})
	m.Activity = activity[:min(len(activity), argoActivityShown)]
	healthState := func(h string) string {
		if h == "" {
			return "" // not assessed: no cell to filter by
		}
		return state(argoHealthColumn, h)
	}
	syncState := func(s string) string {
		if s == "" {
			return ""
		}
		return state(argoSyncColumn, s)
	}
	for _, h := range argoHealthOrder {
		if n := health[h]; n > 0 {
			m.Health = append(m.Health, ArgoTile{Label: argoStateLabel(h), Count: n, Status: kinds.ArgoHealthToken(h), State: healthState(h)})
		}
	}
	for _, s := range argoSyncOrder {
		if n := sync[s]; n > 0 {
			m.Sync = append(m.Sync, ArgoTile{Label: argoStateLabel(s), Count: n, Status: kinds.ArgoSyncToken(s), State: syncState(s)})
		}
	}
	m.Strip = []ArgoTile{
		{Label: "Applications", Count: len(apps)},
		{Label: "Synced", Count: sync[kinds.ArgoSynced], Status: model.StatusOK, State: syncState(kinds.ArgoSynced)},
		{Label: "Healthy", Count: health[kinds.ArgoHealthy], Status: model.StatusOK, State: healthState(kinds.ArgoHealthy)},
		{Label: "Out of sync", Count: sync[kinds.ArgoOutOfSync], Status: model.StatusWarning, State: syncState(kinds.ArgoOutOfSync)},
		{Label: "Degraded", Count: health[kinds.ArgoDegraded], Status: model.StatusError, State: healthState(kinds.ArgoDegraded)},
		{Label: "Missing", Count: health[kinds.ArgoMissing], Status: model.StatusWarning, State: healthState(kinds.ArgoMissing)},
		{Label: "Progressing", Count: health[kinds.ArgoProgressing], Status: model.StatusPending, State: healthState(kinds.ArgoProgressing)},
	}
}

// The Applications' Sync and Health columns, whose texts are their states in the status filter.
const (
	argoSyncColumn   = "sync"
	argoHealthColumn = "health"
)

// argoStateLabel names a state; "" is one the controller has not assessed yet.
func argoStateLabel(s string) string {
	if s == "" {
		return "Not assessed"
	}
	return s
}

func argoSortRefs(refs []model.ObjectRef) { slices.SortFunc(refs, argoCompareRefs) }

func argoCompareRefs(a, b model.ObjectRef) int {
	if a.Namespace != b.Namespace {
		if a.Namespace < b.Namespace {
			return -1
		}
		return 1
	}
	switch {
	case a.Name < b.Name:
		return -1
	case a.Name > b.Name:
		return 1
	}
	return 0
}

// ArgoCDDashboard returns the Dashboard for the given namespaces (nil: the UI's filter) and, with
// attention, its Needs attention table. While the page is open with those namespaces it is the
// page's latest build; otherwise it is built once from the cache, waiting (bounded by ctx and 15
// seconds) for the kinds' initial lists.
func (s *Session) ArgoCDDashboard(ctx context.Context, contextID string, namespaces []string, attention bool) (*ArgoCDDashboardModel, *TableSnapshot, error) {
	b, err := s.TargetBackend(contextID) // the context's cluster, "" the shown one (D-69)
	if err != nil {
		return nil, nil, err
	}
	if namespaces == nil {
		namespaces = s.windowNamespaces(b)
	}
	s.state.mu.Lock()
	page := s.state.argo
	s.state.mu.Unlock()
	if page != nil && page.backend == b {
		page.mu.Lock()
		same := slices.Equal(page.params.namespaces, namespaces)
		page.mu.Unlock()
		if m := page.model.Load(); same && m != nil {
			snap := page.snap.Load()
			if !attention {
				snap = nil
			}
			return m, snap, nil
		}
	}
	wctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	// Once the cluster's definitions are listed: before, Argo CD's kinds are not known yet, and a read
	// right after connecting would say the cluster serves none.
	waitCustomKindsSettled(wctx, b)
	// Once the kinds' plans are decided, following a refusal in all namespaces
	// (accessiblens_follow.go), as the Overviews' reads do.
	w := openFollowing(wctx, b, argoDashKinds, func() (*argoDashWorker, []kindHandle) {
		w := s.newArgoDashWorker(b, viewParams{}, false, b.Acquire)
		return w, w.kindHandles()
	}, (*argoDashWorker).release)
	defer w.release()
	for _, k := range w.kinds {
		k.handle.WaitForSync(wctx)
	}
	m, snap := w.build(viewParams{namespaces: slices.Clone(namespaces), sort: defaultSort(w.table)})
	if !attention {
		snap = nil
	}
	return m, snap, nil
}

// reconcileArgoPageLocked runs the Dashboard's worker: started when the page opens, given the UI's
// namespaces, search, sort and columns, stopped when another view opens or the backend changes,
// and started again when the kinds it reads change (Argo CD's definitions came or went) or how
// they are read does (pageReadKey: a pending plan, all namespaces, or the namespaces).
func (st *state) reconcileArgoPageLocked(b *Backend, now UIState) {
	want := b != nil && now.View == ArgoCDDashboard
	var key string
	if want {
		key = b.pageReadKey(argoDashKinds, false)
	}
	if st.argo != nil && (!want || st.argo.backend != b || st.argo.planKey != key) {
		st.argo.stop()
		st.argo = nil
	}
	switch {
	case want && st.argo == nil:
		st.argo = st.s.startArgoDashboard(b, paramsOf(now))
		st.argo.planKey = key
	case st.argo != nil:
		st.argo.setParams(paramsOf(now))
	}
	if st.argo != nil && st.argo.table != nil && st.argo.table.ColumnIndex(now.Sort.Column) >= 0 {
		st.sorts[now.View] = now.Sort
	}
}

// detachArgoLocked stops the Dashboard's worker.
func (st *state) detachArgoLocked() {
	if st.argo != nil {
		st.argo.stop()
		st.argo = nil
	}
}

// argoTable returns the open Dashboard's latest snapshot, or nil.
func (st *state) argoTable() *TableSnapshot {
	st.mu.Lock()
	w := st.argo
	st.mu.Unlock()
	if w == nil {
		return nil
	}
	return w.snap.Load()
}

// waitCustomKindsSettled waits, bounded by ctx, until the backend knows its custom kinds (the
// definitions' first list is done or failed). The set is in memory, so looking again costs no
// request.
func waitCustomKindsSettled(ctx context.Context, b *Backend) {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for !b.CustomKindsSettled() {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
