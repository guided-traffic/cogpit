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
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Flux CD Dashboard (resource-parity › Paid Lens Desktop views › Flux CD, G23, D-34): the Flux
// section's first item, a page as the Workloads Overview is (overview.go). Its upper part counts,
// per Flux kind the cluster serves, the objects in the selected namespaces by state (Ready, Not
// Ready, Reconciling, Suspended, Unknown: kinds.FluxState); its table lists every Flux object that
// is not ready, of every kind, so that a selection of it takes the Flux actions. Everything comes
// from the cache: the page holds a handle on each Flux kind only while it is open. Lens's Flux
// dashboard is not captured: its look and contents here are Cogpit's (inferred).

// FluxDashboard is the page key of the Flux section's Dashboard.
const FluxDashboard = "flux/dashboard"

// FluxDashboardTitle is the page's title in its header and its tab; the navigator names it
// Dashboard inside the Flux group, as captured.
const FluxDashboardTitle = "Flux Dashboard"

func init() { pageTitles[FluxDashboard] = FluxDashboardTitle }

// The Flux dashboard table's column ids besides the name, and its Namespace column's title.
const (
	fluxColumnStatus   = "status"
	fluxColumnMessage  = "message"
	fluxTitleNamespace = "Namespace"
)

// fluxIssuesProfile is the profile of the dashboard's table: one row per Flux object that is not
// ready, rows of several kinds (MixedKinds). Its key is the page's, so that no kind is mistaken
// for it.
var fluxIssuesProfile = &model.KindProfile{
	Key: FluxDashboard, Title: "Not Ready", Namespaced: true,
	Columns: []model.Column{
		{ID: metadataName, Title: "Name", Type: model.ColumnText},
		{ID: columnKind, Title: "Kind", Type: model.ColumnText, Width: 150},
		{ID: columnNamespace, Title: fluxTitleNamespace, Type: model.ColumnText},
		{ID: fluxColumnStatus, Title: "Status", Type: model.ColumnText, Width: 110},
		{ID: fluxColumnMessage, Title: "Message", Type: model.ColumnText},
		{ID: columnAge, Title: "Age", Type: model.ColumnAge},
	},
}

// fluxIssuesSort is the table's order until the user sorts: the most recent change of the Ready
// condition first (Cogpit's default).
var fluxIssuesSort = SortSpec{Column: columnAge, Desc: true}

// FluxDashboardModel is the dashboard's upper part: one entry per Flux kind the cluster serves, in
// the navigator's order, with its objects counted by state.
type FluxDashboardModel struct {
	Kinds []FluxDashboardKind `json:"kinds"`
	// Namespaces are the namespaces counted; empty: all.
	Namespaces []string `json:"namespaces,omitempty"`
	// Synced is false while a kind is still being listed.
	Synced bool `json:"synced"`
	// Error names the kinds the page cannot read ("Kustomizations: forbidden …"); empty when it
	// reads all.
	Error string `json:"error,omitempty"`
}

// FluxDashboardKind is one Flux kind of the dashboard.
type FluxDashboardKind struct {
	Kind    string `json:"kind"`              // kind key: ui_openView opens its view
	Title   string `json:"title"`             // "Kustomizations"
	Section string `json:"section,omitempty"` // the navigator's sub-group: "Sources"
	Count   int    `json:"count"`
	// States are the counts by state in kinds.FluxStates' order, zero counts left out.
	States []FluxStateCount `json:"states,omitempty"`
	Synced bool             `json:"synced"`
	Error  string           `json:"error,omitempty"`
}

// FluxStateCount is the number of a kind's objects in one state.
type FluxStateCount struct {
	State kinds.FluxState `json:"state"`
	// Title is the state as the views' Status column writes it: "Ready", "Not Ready".
	Title string `json:"title"`
	// Status is the state's status token, the colour the views give it.
	Status model.Status `json:"status"`
	Count  int          `json:"count"`
}

// fluxDashboardWorker keeps the dashboard current while it is open: a cache handle on each Flux
// kind the page reads, its counts and table rebuilt at most once per SnapshotInterval after a
// change.
type fluxDashboardWorker struct {
	s       *Session
	backend *Backend
	src     *fluxSource
	planKey string       // which Flux kinds the open page reads and how (fluxPlanKey); the state's lock
	refused refusedReads // the reads given back as refused (pagerefusals.go)

	mu     sync.Mutex
	params viewParams

	snap     atomic.Pointer[TableSnapshot]
	model    atomic.Pointer[fluxBuild]
	wake     chan struct{}
	quit     chan struct{}
	stopped  atomic.Bool
	panicked atomic.Bool
}

// fluxSource is what the page reads from the cache: a handle on each Flux kind the cluster serves
// and the user may list (as far as the access reviews know).
type fluxSource struct {
	kinds  []fluxSourceKind
	unsubs []func()
}

type fluxSourceKind struct {
	info    kinds.FluxKind
	profile *model.KindProfile
	handle  *cache.Handle
}

// fluxBuild is one reading of the page's kinds: every object's state, for the counts and the
// table of any namespaces.
type fluxBuild struct {
	kinds   []fluxBuiltKind
	synced  bool
	errText string
	// unknown says why the page cannot tell whether the cluster serves Flux kinds, when it reads
	// none (Backend.customKindsUnknown); "" when it can.
	unknown string
}

// fluxKindsWhat names the Flux kinds in the text of a page that cannot tell whether the cluster
// serves them.
const fluxKindsWhat = "Flux kinds"

// fluxUnknown is customKindsUnknown for a build that reads no Flux kind.
func (b *Backend) fluxUnknown(fb *fluxBuild) string {
	if len(fb.kinds) > 0 {
		return ""
	}
	return b.customKindsUnknown(fluxKindsWhat)
}

type fluxBuiltKind struct {
	info    kinds.FluxKind
	objects []fluxObjectState
	synced  bool
	err     string
}

type fluxObjectState struct {
	ref     model.ObjectRef
	state   kinds.FluxState
	message string
	since   time.Time
}

// newFluxSource acquires the page's handles through acquire (the open page's acquireForPage, an
// agent's read's Acquire once the plans are decided); onChange runs after every change of one of
// them.
func newFluxSource(b *Backend, acquire func(*model.KindProfile) *cache.Handle, onChange func()) *fluxSource {
	src := &fluxSource{}
	for _, k := range kinds.FluxKinds() {
		p, ok := b.Profile(k.Key)
		if !ok {
			continue
		}
		if listable, known := b.Listable(p, nil); known && !listable {
			continue // a watch the API server refuses would only retry
		}
		h := acquire(p)
		src.unsubs = append(src.unsubs, h.Subscribe(func(cache.Event) { onChange() }))
		src.kinds = append(src.kinds, fluxSourceKind{info: k, profile: p, handle: h})
	}
	return src
}

// kindHandles are the source's handles, for openFollowing.
func (src *fluxSource) kindHandles() []kindHandle {
	out := make([]kindHandle, 0, len(src.kinds))
	for _, k := range src.kinds {
		out = append(out, kindHandle{k.profile, k.handle})
	}
	return out
}

func (src *fluxSource) release() {
	for _, u := range src.unsubs {
		u()
	}
	for _, k := range src.kinds {
		k.handle.Release()
	}
}

func (src *fluxSource) synced() bool {
	for _, k := range src.kinds {
		if !k.handle.HasSynced() {
			return false
		}
	}
	return true
}

func (src *fluxSource) waitSynced(ctx context.Context) {
	for _, k := range src.kinds {
		k.handle.WaitForSync(ctx)
	}
}

func (src *fluxSource) errText() string {
	var parts []string
	for _, k := range src.kinds {
		if err := k.handle.Err(); err != nil {
			parts = append(parts, k.info.Title+": "+err.Error())
		}
	}
	return strings.Join(parts, "; ")
}

// build reads every object's state from the cache.
func (src *fluxSource) build(context string) *fluxBuild {
	fb := &fluxBuild{synced: true, errText: src.errText()}
	for _, k := range src.kinds {
		synced := k.handle.HasSynced() // before List, as views do
		objs := KindList(k.handle, k.profile)
		bk := fluxBuiltKind{info: k.info, synced: synced, err: errText(k.handle.Err()), objects: make([]fluxObjectState, 0, len(objs))}
		for _, o := range objs {
			state, msg, since := kinds.FluxStatusOf(k.info.Key, o)
			if since.IsZero() {
				since = o.GetCreationTimestamp().Time
			}
			bk.objects = append(bk.objects, fluxObjectState{
				ref:   model.ObjectRef{Context: context, Kind: k.info.Key, Namespace: o.GetNamespace(), Name: o.GetName()},
				state: state, message: msg, since: since,
			})
		}
		fb.synced = fb.synced && synced
		fb.kinds = append(fb.kinds, bk)
	}
	return fb
}

// fluxModel counts a build's objects in the namespaces (empty: all).
func fluxModel(fb *fluxBuild, namespaces []string) *FluxDashboardModel {
	var nsSet map[string]struct{}
	if len(namespaces) > 0 {
		nsSet = setOf(namespaces)
	}
	m := &FluxDashboardModel{Kinds: []FluxDashboardKind{}, Namespaces: slices.Clone(namespaces), Synced: fb.synced, Error: fb.errText}
	if fb.unknown != "" {
		m.Error = fb.unknown // the definitions cannot be listed: whether Flux is there is unknown
	}
	for _, k := range fb.kinds {
		counts := map[kinds.FluxState]int{}
		n := 0
		for _, o := range k.objects {
			if nsSet != nil {
				if _, in := nsSet[o.ref.Namespace]; !in {
					continue
				}
			}
			counts[o.state]++
			n++
		}
		dk := FluxDashboardKind{Kind: k.info.Key, Title: k.info.Title, Section: k.info.Section, Count: n, Synced: k.synced, Error: k.err}
		for _, st := range kinds.FluxStates {
			if c := counts[st]; c > 0 {
				dk.States = append(dk.States, FluxStateCount{State: st, Title: kinds.FluxStateText(st), Status: kinds.FluxStateToken(st), Count: c})
			}
		}
		m.Kinds = append(m.Kinds, dk)
	}
	return m
}

// fluxTable is the page's table: the objects that are not ready, in the namespaces, matching the
// search and the status filter, sorted.
func fluxTable(context string, fb *fluxBuild, p viewParams) *TableSnapshot {
	var nsSet map[string]struct{}
	if len(p.namespaces) > 0 {
		nsSet = setOf(p.namespaces)
	}
	match, filterErr := p.matcher()
	states := newStateFilter(fluxIssuesProfile, p.states)
	rows := []model.Row{}
	total := 0
	for _, k := range fb.kinds {
		for _, o := range k.objects {
			if o.state == kinds.FluxReady {
				continue
			}
			total++
			if nsSet != nil {
				if _, in := nsSet[o.ref.Namespace]; !in {
					continue
				}
			}
			row := fluxRow(context, k.info, o)
			if !match.All() && !match.Match(row) {
				continue
			}
			if !states.keep(states.spec.Of(&row)) {
				continue
			}
			rows = append(rows, row)
		}
	}
	SortRows(rows, fluxIssuesProfile.Columns, p.sort)
	snap := &TableSnapshot{
		View: FluxDashboard, Context: context, Title: FluxDashboardTitle, Namespaced: true,
		Columns: ColumnsShown(fluxIssuesProfile.Columns, p.hidden, p.hiddenSet), Order: ColumnOrder(fluxIssuesProfile.Columns, p.order),
		Rows: rows, Sort: p.sort,
		Namespaces: slices.Clone(p.namespaces), Filter: p.filter, FilterOptions: p.filterOpts, FilterError: filterErr, Total: total,
		States: slices.Clone(p.states), StateChoices: states.choices(),
		Synced: fb.synced, Error: fb.errText,
	}
	switch {
	case fb.unknown != "":
		snap.Error = fb.unknown
	case len(fb.kinds) == 0:
		snap.Error = "The cluster serves no Flux kinds."
	}
	return snap
}

// fluxRow is an object as a row of the table: Name (a link to the object, which opens its
// details), Kind, Namespace (a link that narrows the namespace filter), Status in the state's
// colour, Message, Age (since the Ready condition last changed).
func fluxRow(context string, k kinds.FluxKind, o fluxObjectState) model.Row {
	ref := o.ref
	ref.Context = context
	self := ref
	ns := model.Cell{Text: ref.Namespace}
	if ref.Namespace != "" {
		ns.Link = &model.ObjectRef{Kind: kindNamespaces, Name: ref.Namespace}
	}
	token := kinds.FluxStateToken(o.state)
	return model.Row{
		Ref: ref,
		Cells: []model.Cell{
			{Text: ref.Name, Link: &self},
			{Text: k.APIKind},
			ns,
			{Text: kinds.FluxStateText(o.state), Status: token},
			{Text: o.message},
			model.AgeCell(o.since),
		},
		Status: token,
	}
}

// startFluxDashboard starts the page's worker; the first build runs at once.
func (s *Session) startFluxDashboard(b *Backend, params viewParams) *fluxDashboardWorker {
	w := &fluxDashboardWorker{s: s, backend: b, params: params, wake: make(chan struct{}, 1), quit: make(chan struct{})}
	w.refused.startAt(b.ns.refusalsGen())
	wake := w.wake
	w.src = newFluxSource(b, b.acquireForPage, func() { signal(wake) }) // nothing while a plan is pending
	signal(w.wake)
	s.goSafe("flux dashboard", func() {
		runCoalesced(w.quit, w.wake, s.opts.SnapshotInterval, w.unsettled, w.rebuild)
	})
	return w
}

func (w *fluxDashboardWorker) setParams(p viewParams) {
	w.mu.Lock()
	changed := !w.params.equal(p)
	w.params = p
	w.mu.Unlock()
	if changed {
		signal(w.wake)
	}
}

// stop ends the loop and releases the handles; the informers stay warm.
func (w *fluxDashboardWorker) stop() {
	if !w.stopped.CompareAndSwap(false, true) {
		return
	}
	close(w.quit)
	w.src.release()
}

// unsettled reports whether a sync state or an error changed since the last build: neither comes
// with a watch event, nor does the end of the definitions' list for a page that reads no kind. A
// read the API server refused wholly goes back first, its refusal kept (pagerefusals.go).
func (w *fluxDashboardWorker) unsettled() bool {
	gave := false
	for _, k := range w.src.kinds {
		gave = w.refused.giveBack(k.handle) || gave
	}
	last := w.model.Load()
	return last == nil || gave || last.synced != w.src.synced() || last.errText != w.src.errText() ||
		(len(last.kinds) == 0 && last.unknown != w.backend.customKindsUnknown(fluxKindsWhat))
}

// rebuild builds, stores and announces the page; a panic is reported once and does not end the loop.
func (w *fluxDashboardWorker) rebuild() {
	defer func() {
		if r := recover(); r != nil {
			w.s.log.Error("engine: building the flux dashboard failed", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			if w.panicked.CompareAndSwap(false, true) {
				w.s.emit(Event{Type: EventToast, Level: LevelError, Message: "the Flux dashboard failed to update: internal error"})
			}
		}
	}()
	w.mu.Lock()
	p := w.params
	w.mu.Unlock()
	fb := w.src.build(w.backend.Context)
	fb.unknown = w.backend.fluxUnknown(fb)
	snap := fluxTable(w.backend.Context, fb, p)
	snap.Revision = w.s.state.snapshots.Add(1)
	if w.stopped.Load() {
		return
	}
	w.model.Store(fb)
	w.snap.Store(snap)
	w.s.emit(Event{Type: EventTable, View: FluxDashboard, Revision: snap.Revision})
}

// FluxDashboard returns the Flux dashboard for the given namespaces (nil: the namespace filter the
// user has set) and, with table, the objects that are not ready as the page's table lists them.
// While the page is open it is the page's latest build; otherwise it is built once from the cache:
// the page's handles are held for the call, which waits (bounded by ctx and 15 seconds) for their
// initial lists, and released after.
func (s *Session) FluxDashboard(ctx context.Context, contextID string, namespaces []string, table bool) (*FluxDashboardModel, *TableSnapshot, error) {
	b, err := s.TargetBackend(contextID) // the context's cluster, "" the shown one (D-69)
	if err != nil {
		return nil, nil, err
	}
	if namespaces == nil {
		namespaces = s.windowNamespaces(b)
	}
	params := viewParams{namespaces: slices.Clone(namespaces), sort: fluxIssuesSort}
	s.state.mu.Lock()
	page := s.state.flux
	s.state.mu.Unlock()
	var fb *fluxBuild
	if page != nil && page.backend == b {
		fb = page.model.Load()
	}
	if fb == nil {
		wctx, cancel := context.WithTimeout(ctx, syncTimeout)
		defer cancel()
		// Once the kinds' plans are decided, following a refusal in all namespaces
		// (accessiblens_follow.go), as the Overviews' reads do.
		src := openFollowing(wctx, b, fluxKindKeys(b), func() (*fluxSource, []kindHandle) {
			src := newFluxSource(b, b.Acquire, func() {})
			return src, src.kindHandles()
		}, (*fluxSource).release)
		defer src.release()
		src.waitSynced(wctx)
		fb = src.build(b.Context)
		fb.unknown = b.fluxUnknown(fb)
	}
	var snap *TableSnapshot
	if table {
		snap = fluxTable(b.Context, fb, params)
	}
	return fluxModel(fb, namespaces), snap, nil
}

// fluxKindKeys are the keys of the Flux kinds the cluster serves, in the navigator's order.
func fluxKindKeys(b *Backend) []string {
	var keys []string
	for _, k := range kinds.FluxKinds() {
		if _, ok := b.Profile(k.Key); ok {
			keys = append(keys, k.Key)
		}
	}
	return keys
}

// fluxPlanKey is how the open page reads the Flux kinds: which the cluster serves, at which
// version (a definition that moves to another version opens the page again), and how
// (pageReadKey: a pending plan, all namespaces, or the namespaces).
func (b *Backend) fluxPlanKey() string {
	var keys []string
	var sb strings.Builder
	for _, k := range kinds.FluxKinds() {
		if p, ok := b.Profile(k.Key); ok {
			keys = append(keys, k.Key)
			sb.WriteString(p.Version)
			sb.WriteByte('|')
		}
	}
	return b.pageReadKey(keys, true) + sb.String()
}

// reconcileFluxPageLocked runs the Flux dashboard's worker: started when the page opens, given the
// UI's namespaces, search, sort and columns, stopped when another view opens or the backend
// changes, and started again when the served Flux kinds or the access reviews change how it reads
// them. The page's sort is remembered as a view's is.
func (st *state) reconcileFluxPageLocked(b *Backend, now UIState) {
	want := b != nil && now.View == FluxDashboard
	var key string
	if want {
		key = b.fluxPlanKey()
	}
	if st.flux != nil && (!want || st.flux.backend != b || st.flux.planKey != key || st.flux.refused.retake(b)) {
		st.flux.stop()
		st.flux = nil
	}
	switch {
	case want && st.flux == nil:
		st.flux = st.s.startFluxDashboard(b, paramsOf(now))
		st.flux.planKey = key
	case st.flux != nil:
		st.flux.setParams(paramsOf(now))
	}
	if st.flux != nil && fluxIssuesProfile.ColumnIndex(now.Sort.Column) >= 0 {
		st.sorts[now.View] = now.Sort
	}
}
