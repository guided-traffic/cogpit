package engine

import (
	"context"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Pages are views that are not one kind's list (architecture §4.3): their keys contain a slash,
// "<sidebar group>/<page>", which no kind key does (built-in keys are plain plurals, custom kinds'
// keys "<plural>.<group>"). A page that shows a table has a table profile: the kind of its rows,
// with the page's own columns. The first page is the Workloads Overview.

// WorkloadsOverview is the page key of the Workloads Overview (Workloads › Overview in the sidebar,
// resource-parity › Workloads › Overview).
const WorkloadsOverview = "workloads/overview"

// IsPage reports whether a view key names a page rather than a kind.
func IsPage(view string) bool { return strings.Contains(view, "/") }

// pageTitles are the pages and their titles.
var pageTitles = map[string]string{WorkloadsOverview: "Overview", ClusterOverview: "Overview"}

// PageTitle returns a page's title, and whether the key names a page.
func PageTitle(view string) (string, bool) {
	t, ok := pageTitles[view]
	return t, ok
}

// overviewKinds are the Workloads Overview's kinds in its two columns, as captured: Pods,
// Deployments, Daemon Sets, Stateful Sets; Replica Sets, Jobs, Cron Jobs.
var overviewKinds = [2][]string{
	{kindPods, logKindDeployments, logKindDaemonSets, logKindStatefulSets},
	{logKindReplicaSets, logKindJobs, "cronjobs"},
}

// overviewPageKinds are every kind the open page reads, its events included, for its plan key.
var overviewPageKinds = append(append(append([]string{}, overviewKinds[0]...), overviewKinds[1]...), eventsKind)

// overviewEventColumns are the columns of the overview's events table after the events' own first
// column (their hidden name: the first column is the name), as captured: Type, Source, Namespace,
// Involved Object, Message, Count, Age, Last Seen (relative here, where the Events view shows a
// timestamp).
var overviewEventColumns = []string{"type", "source", "namespace", "involvedObject", "message", "count", "age", lastSeenColumn}

// WorkloadsOverviewModel is the Workloads Overview's upper part: one entry per workload kind the
// cluster serves, with its count and health in the selected namespaces. The events below are the
// page's table (Session.Table).
type WorkloadsOverviewModel struct {
	Kinds []OverviewKind `json:"kinds"`
	// Namespaces are the namespaces counted; empty: all.
	Namespaces []string `json:"namespaces,omitempty"`
	// Synced is false while a kind is still being listed.
	Synced bool `json:"synced"`
}

// OverviewKind is one kind of the Workloads Overview: a link to its view, its count and its health
// bar.
type OverviewKind struct {
	Kind   string `json:"kind"` // kind key: ui_openView opens its view
	Title  string `json:"title"`
	Column int    `json:"column"` // 0: the left column, 1: the right
	Count  int    `json:"count"`
	// Statuses is the health bar: how many objects show each status token (the row's status as the
	// kind's view colours it), in StatusOrder, zero counts left out.
	Statuses []StatusCount `json:"statuses,omitempty"`
	Synced   bool          `json:"synced"`
	Error    string        `json:"error,omitempty"`
}

// StatusCount is the number of objects with one status token.
type StatusCount struct {
	Status model.Status `json:"status"`
	Count  int          `json:"count"`
}

// StatusOrder is the order of a health bar's segments: healthy first, then done, waiting, the
// problems, and what Cogpit cannot judge.
var StatusOrder = []model.Status{
	model.StatusOK, model.StatusSucceeded, model.StatusPending, model.StatusWarning, model.StatusError,
	model.StatusTerminating, model.StatusUnknown, model.StatusNone,
}

// overviewProfiles holds the events table profile derived from each events profile, so that a
// page's rows are built against one stable profile (the row cache keys on it).
var overviewProfiles sync.Map // *model.KindProfile (events) → *model.KindProfile

// overviewEvents returns the Workloads Overview's table profile: the events, in the page's column
// order, with Last Seen as an age. Its key stays "events", so its rows name events. Nil when the
// cluster serves no events.
func overviewEvents(b *Backend) *model.KindProfile {
	src, ok := b.Profile(eventsKind)
	if !ok {
		return nil
	}
	if p, ok := overviewProfiles.Load(src); ok {
		return p.(*model.KindProfile)
	}
	p := *src
	if len(src.Columns) == 0 {
		return nil
	}
	idx := []int{0}
	p.Columns = []model.Column{src.Columns[0]}
	for _, id := range overviewEventColumns {
		if i := src.ColumnIndex(id); i >= 0 {
			c := src.Columns[i]
			if id == lastSeenColumn {
				c.Type = model.ColumnAge
			}
			p.Columns = append(p.Columns, c)
			idx = append(idx, i)
		}
	}
	cells := func(in []model.Cell) []model.Cell {
		out := make([]model.Cell, len(idx))
		for i, j := range idx {
			if j < len(in) {
				out[i] = in[j]
			}
		}
		return out
	}
	switch {
	case src.CellsWith != nil:
		inner := src.CellsWith
		p.CellsWith = func(obj *unstructured.Unstructured, rel model.Related) ([]model.Cell, model.Status) {
			c, st := inner(obj, rel)
			return cells(c), st
		}
	case src.Cells != nil:
		inner := src.Cells
		p.Cells = func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
			c, st := inner(obj)
			return cells(c), st
		}
	}
	actual, _ := overviewProfiles.LoadOrStore(src, &p)
	return actual.(*model.KindProfile)
}

// viewProfile returns the profile of the rows a view's table shows: a kind's own, or a page's
// table profile; nil for neither.
func (b *Backend) viewProfile(view string) *model.KindProfile {
	if b == nil {
		return nil
	}
	if p, ok := b.Profile(view); ok {
		return p
	}
	switch view {
	case WorkloadsOverview:
		return overviewEvents(b)
	case ClusterOverview:
		return issuesProfile // clusteroverview.go
	case FluxDashboard:
		return fluxIssuesProfile // fluxdashboard.go
	case ArgoCDDashboard:
		return argoAttentionProfile(b) // argocd_dashboard.go
	case ImageSecurity:
		return imageSecProfile // imagesecurity_model.go
	}
	return nil
}

// ViewProfile returns the profile of the rows the open cluster's view shows: the kind's own for a
// kind's view, the page's table profile for a page (the Workloads Overview's events). Commands on
// the open view's rows (ui.sort, ui.select, ui.setColumns) use it.
func (s *Session) ViewProfile(view string) (*model.KindProfile, bool) {
	p := s.Backend().viewProfile(view)
	return p, p != nil
}

// overviewWorker keeps the Workloads Overview current while it is open: it holds a cache handle on
// each served workload kind and on the events, and rebuilds the kinds' counts and the events table
// at most once per SnapshotInterval after a change.
type overviewWorker struct {
	s          *Session
	backend    *Backend
	events     *model.KindProfile // the page's table profile; nil without events
	evts       *cache.Handle
	evtsDenied bool // the access reviews say the user may not list the events where the page reads them
	rows       *rowCache
	kinds      []*overviewKindState
	unsubs     []func()
	planKey    string       // how the open page reads its kinds (Backend.pageReadKey); the state's lock
	refused    refusedReads // the reads given back as refused (pagerefusals.go)

	mu     sync.Mutex
	params viewParams

	snap     atomic.Pointer[TableSnapshot]
	model    atomic.Pointer[WorkloadsOverviewModel]
	wake     chan struct{}
	quit     chan struct{}
	stopped  atomic.Bool
	panicked atomic.Bool
}

// overviewKindState is one workload kind of the overview: its handle and each object's status,
// rebuilt only when the object's resourceVersion (or a related kind) changed.
type overviewKindState struct {
	profile  *model.KindProfile
	column   int
	handle   *cache.Handle
	denied   bool // the access reviews say the user may not list it where the page reads it
	related  *relatedSet
	relGen   uint64
	gen      uint32
	statuses map[string]*overviewStatus
}

type overviewStatus struct {
	rv     string
	ns     string
	status model.Status
	gen    uint32
}

// newOverviewWorker acquires the page's handles; with notify, every change of its kinds wakes the
// worker's loop (startOverview), which a one-off read does not run. Without events it holds no
// handle on the events (a one-off read that does not ask for them starts no events watch).
func (s *Session) newOverviewWorker(b *Backend, params viewParams, notify, events bool, acquire func(*model.KindProfile) *cache.Handle) *overviewWorker {
	w := &overviewWorker{
		s: s, backend: b, params: params, rows: &rowCache{},
		wake: make(chan struct{}, 1), quit: make(chan struct{}),
	}
	// The callbacks run on informer goroutines as soon as a handle exists: they capture the channel,
	// never w's fields that are still being set.
	onChange := func() {}
	if notify {
		wake := w.wake
		onChange = func() { signal(wake) }
	}
	for col, keys := range overviewKinds {
		for _, key := range keys {
			p, ok := b.Profile(key)
			if !ok {
				continue
			}
			k := &overviewKindState{profile: p, column: col, handle: acquire(p), denied: b.deniedKind(p), statuses: map[string]*overviewStatus{}}
			k.related = newRelatedSet(b, p.Needs, onChange)
			w.unsubs = append(w.unsubs, k.handle.Subscribe(func(cache.Event) { onChange() }))
			w.kinds = append(w.kinds, k)
		}
	}
	if w.events = overviewEvents(b); w.events != nil && events {
		w.evts = acquire(w.events)
		w.evtsDenied = b.deniedKind(w.events)
		w.unsubs = append(w.unsubs, w.evts.Subscribe(func(cache.Event) { onChange() }))
	}
	return w
}

// kindHandles are the worker's handles with their kinds: the workloads' and the events'.
func (w *overviewWorker) kindHandles() []kindHandle {
	out := make([]kindHandle, 0, len(w.kinds)+1)
	for _, k := range w.kinds {
		out = append(out, kindHandle{k.profile, k.handle})
	}
	if w.evts != nil {
		out = append(out, kindHandle{w.events, w.evts})
	}
	return out
}

// startOverview starts the page's worker; the first build runs at once.
func (s *Session) startOverview(b *Backend, params viewParams) *overviewWorker {
	gen := b.ns.refusalsGen()
	w := s.newOverviewWorker(b, params, true, true, b.acquireForPage) // nothing while a plan is pending (A98)
	w.refused.startAt(gen)
	signal(w.wake)
	s.goSafe("workloads overview", func() {
		runCoalesced(w.quit, w.wake, s.opts.SnapshotInterval, w.unsettled, w.rebuild)
	})
	return w
}

func (w *overviewWorker) setParams(p viewParams) {
	w.mu.Lock()
	changed := !w.params.equal(p)
	w.params = p
	w.mu.Unlock()
	if changed {
		signal(w.wake)
	}
}

// stop ends the loop and releases every handle; the informers stay warm.
func (w *overviewWorker) stop() {
	if !w.stopped.CompareAndSwap(false, true) {
		return
	}
	close(w.quit)
	w.release()
}

func (w *overviewWorker) release() {
	for _, u := range w.unsubs {
		u()
	}
	for _, k := range w.kinds {
		k.handle.Release()
		k.related.release()
	}
	if w.evts != nil {
		w.evts.Release()
	}
}

// synced reports whether every kind of the page finished its initial list.
func (w *overviewWorker) synced() bool {
	for _, k := range w.kinds {
		if !k.handle.HasSynced() || !k.related.synced() {
			return false
		}
	}
	return w.evts == nil || w.evts.HasSynced()
}

// unsettled reports whether a sync state or an error changed since the last build: neither comes
// with a watch event. A read the API server refused wholly goes back first, its refusal kept
// (pagerefusals.go).
func (w *overviewWorker) unsettled() bool {
	gave := w.refused.giveBack(w.evts)
	for _, k := range w.kinds {
		gave = w.refused.giveBack(k.handle) || gave
	}
	m := w.model.Load()
	if m == nil || gave {
		return true
	}
	if m.Synced != w.synced() {
		return true
	}
	for i, k := range w.kinds {
		if i < len(m.Kinds) && m.Kinds[i].Error != k.err() {
			return true
		}
	}
	return false
}

// rebuild builds, stores and announces the page; a panic is reported once and does not end the loop.
func (w *overviewWorker) rebuild() {
	defer func() {
		if r := recover(); r != nil {
			w.s.log.Error("engine: building the workloads overview failed", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			if w.panicked.CompareAndSwap(false, true) {
				w.s.emit(Event{Type: EventToast, Level: LevelError, Message: "the workloads overview failed to update: internal error"})
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
	w.s.emit(Event{Type: EventTable, View: WorkloadsOverview, Revision: snap.Revision})
}

// build computes the kinds' counts and health and the events table for the given namespaces,
// search text and sort.
func (w *overviewWorker) build(p viewParams) (*WorkloadsOverviewModel, *TableSnapshot) {
	var nsSet map[string]struct{}
	if len(p.namespaces) > 0 {
		nsSet = make(map[string]struct{}, len(p.namespaces))
		for _, ns := range p.namespaces {
			nsSet[ns] = struct{}{}
		}
	}
	m := &WorkloadsOverviewModel{Namespaces: slices.Clone(p.namespaces), Synced: true}
	for _, k := range w.kinds {
		synced := k.handle.HasSynced() && k.related.synced() // before List, as views do
		ok := k.count(w.backend.Context, nsSet)
		ok.Synced = synced
		ok.Error = k.err()
		m.Synced = m.Synced && synced
		m.Kinds = append(m.Kinds, ok)
	}

	var snap *TableSnapshot
	if w.evts != nil {
		synced := w.evts.HasSynced()
		snap = w.rows.build(w.events, w.backend.Context, KindList(w.evts, w.events), p, model.NoRelated, 0)
		snap.Synced = synced
		snap.Error = errText(w.evts.Err())
		if w.evtsDenied {
			snap.Error = mayNotList(w.events)
		}
		m.Synced = m.Synced && synced
	} else {
		snap = &TableSnapshot{Rows: []model.Row{}, Synced: true, Error: "The cluster serves no events.", Namespaced: true}
	}
	snap.View, snap.Title = WorkloadsOverview, pageTitles[WorkloadsOverview]
	snap.Revision = w.s.state.snapshots.Add(1)
	return m, snap
}

// err is the kind's error: that the user may not list it (mayNotList), else its read's.
func (k *overviewKindState) err() string {
	if k.denied {
		return mayNotList(k.profile)
	}
	return errText(k.handle.Err())
}

// count tallies a kind's objects in the namespaces by their rows' status tokens.
func (k *overviewKindState) count(context string, nsSet map[string]struct{}) OverviewKind {
	rel, relGen := k.related.view()
	if relGen != k.relGen {
		clear(k.statuses) // the related objects changed: every row's status may have
		k.relGen = relGen
	}
	k.gen++
	counts := map[model.Status]int{}
	n := 0
	objs := KindList(k.handle, k.profile)
	for _, o := range objs {
		key := string(o.GetUID())
		if key == "" {
			key = o.GetNamespace() + "/" + o.GetName()
		}
		rv := o.GetResourceVersion()
		e := k.statuses[key]
		if e == nil || e.rv != rv || rv == "" {
			e = &overviewStatus{rv: rv, ns: o.GetNamespace(), status: model.BuildRowWith(k.profile, context, o, rel).Status}
			k.statuses[key] = e
		}
		e.gen = k.gen
		if nsSet != nil {
			if _, in := nsSet[e.ns]; !in {
				continue
			}
		}
		counts[e.status]++
		n++
	}
	if len(k.statuses) > len(objs) { // objects were deleted
		for key, e := range k.statuses {
			if e.gen != k.gen {
				delete(k.statuses, key)
			}
		}
	}
	out := OverviewKind{Kind: k.profile.Key, Title: k.profile.Title, Column: k.column, Count: n}
	for _, st := range StatusOrder {
		if c := counts[st]; c > 0 {
			out.Statuses = append(out.Statuses, StatusCount{Status: st, Count: c})
		}
	}
	return out
}

// WorkloadsOverview returns the Workloads Overview for the given namespaces (empty: all) and, with
// events, its events table. While the page is open with those namespaces it is the page's latest
// build; otherwise it is built once from the cache: the kinds' handles are held for the call, which
// waits (bounded by ctx and 15 seconds) for their initial lists, and released after.
func (s *Session) WorkloadsOverview(ctx context.Context, contextID string, namespaces []string, events bool) (*WorkloadsOverviewModel, *TableSnapshot, error) {
	b, err := s.TargetBackend(contextID) // the context's cluster, "" the shown one (D-69)
	if err != nil {
		return nil, nil, err
	}
	if namespaces == nil {
		namespaces = s.windowNamespaces(b)
	}
	s.state.mu.Lock()
	page := s.state.page
	s.state.mu.Unlock()
	if page != nil && page.backend == b {
		page.mu.Lock()
		same := slices.Equal(page.params.namespaces, namespaces)
		page.mu.Unlock()
		if m := page.model.Load(); same && m != nil {
			return m, page.snap.Load(), nil
		}
	}
	wctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	// Once the kinds' plans are decided, following a refusal in all namespaces (accessiblens_follow.go).
	w := openFollowing(wctx, b, overviewPageKinds, func() (*overviewWorker, []kindHandle) {
		w := s.newOverviewWorker(b, viewParams{}, false, events, b.Acquire) // the plans are decided here
		return w, w.kindHandles()
	}, (*overviewWorker).release)
	defer w.release()
	for _, k := range w.kinds {
		k.handle.WaitForSync(wctx)
		k.related.waitSynced(wctx)
	}
	if w.evts != nil {
		w.evts.WaitForSync(wctx)
	}
	sort := defaultSort(w.events)
	m, snap := w.build(viewParams{namespaces: slices.Clone(namespaces), sort: sort})
	if !events {
		snap = nil
	}
	return m, snap, nil
}

// reconcilePageLocked runs the open page's worker: started when the page opens, given the UI's
// namespaces, search, sort and columns, stopped when another view opens or the backend changes,
// and started again when the access reviews change how its kinds are read (pageReadKey: a pending
// plan is told apart, since the page holds nothing then, A98). The
// page's sort is remembered as a view's is.
func (st *state) reconcilePageLocked(b *Backend, now UIState) {
	want := b != nil && now.View == WorkloadsOverview
	var key string
	if want {
		key = b.pageReadKey(overviewPageKinds) // a kind the reviews come to deny names it (overviewKindState.denied)
	}
	if st.page != nil && (!want || st.page.backend != b || st.page.planKey != key || st.page.refused.retake(b)) {
		st.page.stop()
		st.page = nil
	}
	switch {
	case want && st.page == nil:
		st.page = st.s.startOverview(b, paramsOf(now))
		st.page.planKey = key
	case st.page != nil:
		st.page.setParams(paramsOf(now))
	}
	if st.page != nil && st.page.events != nil && st.page.events.ColumnIndex(now.Sort.Column) >= 0 {
		st.sorts[now.View] = now.Sort
	}
}
