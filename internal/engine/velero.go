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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Velero page (D-new): a page under the navigator's Integrations, shown while the cluster
// serves Velero's Backups, read-only and from the cache alone (D-151: no DownloadRequest, no
// ServerStatusRequest, nothing created). Its header names Velero's version, namespace, server and
// node agents and plugins (Velero's Deployment and node-agent DaemonSet); its tiles count the
// Schedules, Backups, Restores and the storage (locations, snapshot locations, repositories) by
// state; Needs attention lists what needs action, worst first, then newest; the Schedules table
// has each schedule's last runs, its next run by Velero's own rule (velero_cron.go); Outside every
// schedule lists the namespaces no enabled schedule's scope takes in full (velero_scope.go), with
// their volumes first. The page holds its handles only while it is shown, as the Flux dashboard
// does (fluxdashboard.go), and agents read the same model through velero.dashboard.

// VeleroDashboard is the page key of the Velero page.
const VeleroDashboard = "velero/dashboard"

// VeleroDashboardTitle is the page's title in its header, its tab and the navigator.
const VeleroDashboardTitle = "Velero"

func init() { pageTitles[VeleroDashboard] = VeleroDashboardTitle }

// Velero's kinds (velero.io/v1), as their kind keys.
const (
	VeleroSchedulesKey         = "schedules.velero.io"
	VeleroBackupsKey           = "backups.velero.io"
	VeleroRestoresKey          = "restores.velero.io"
	VeleroLocationsKey         = "backupstoragelocations.velero.io"
	VeleroSnapshotLocationsKey = "volumesnapshotlocations.velero.io"
	VeleroRepositoriesKey      = "backuprepositories.velero.io"
	// A backup's volumes: the file-system backups of pod volumes (velero.io/v1) and the data
	// mover's uploads of CSI snapshots (velero.io/v2alpha1), for Failed volume backups.
	VeleroPodVolumeBackupsKey = "podvolumebackups.velero.io"
	VeleroDataUploadsKey      = "datauploads.velero.io"
)

// The built-in kinds the page reads besides Velero's: the namespaces and what is in them for
// Outside every schedule, Velero's Deployment and node agents for the header.
const (
	kindPVCs         = "persistentvolumeclaims"
	kindStatefulSets = logKindStatefulSets
)

// veleroKinds are Velero's kinds the page reads, and veleroCoreKinds the built-in ones.
var (
	veleroKinds = []string{
		VeleroSchedulesKey, VeleroBackupsKey, VeleroRestoresKey, VeleroLocationsKey, VeleroSnapshotLocationsKey, VeleroRepositoriesKey,
		VeleroPodVolumeBackupsKey, VeleroDataUploadsKey,
	}
	veleroCoreKinds = []string{kindNamespaces, kindPVCs, logKindDeployments, kindStatefulSets, logKindDaemonSets}
)

// veleroPageKinds are every kind the page reads, for its plan key and an agent's read.
var veleroPageKinds = append(slices.Clone(veleroKinds), veleroCoreKinds...)

// veleroRefresh is how often the open page builds again without a change: a run comes due, an age
// moves.
const veleroRefresh = 30 * time.Second

// veleroPageProfile is the profile of the page's rows for the commands on the open view
// (viewProfile): ui.select opens the drawer of a row's object, of any kind (MixedKinds). The page
// has no table of its own in the window; its rows are velero.dashboard's.
var veleroPageProfile = &model.KindProfile{
	Key: VeleroDashboard, Title: VeleroDashboardTitle, Namespaced: true,
	Columns: fluxIssuesProfile.Columns[:1], // the name, as the Flux dashboard's (fluxdashboard.go)
}

// VeleroDashboardModel is the page: what velero.dashboard answers and the window draws.
type VeleroDashboardModel struct {
	// Served is false when the cluster does not serve Velero's Backups.
	Served bool `json:"served"`
	// Namespaces are the namespaces the page is narrowed to (empty: all): a schedule, backup or
	// restore counts when its scope takes one of them in, a repository when it holds their volumes,
	// a namespace row when it is one; the storage locations and the header always count.
	Namespaces []string     `json:"namespaces,omitempty"`
	Header     VeleroHeader `json:"header"`
	// Tiles are Schedules, Backups, Restores and Storage.
	Tiles []VeleroTile `json:"tiles"`
	// Attention is Needs attention: worst first, then newest.
	Attention []VeleroAttention `json:"attention"`
	// FailedVolumes are the failed volume backups of the backups in Needs attention, grouped: those
	// of one namespace that failed the same way, most volumes first (velero_volumes.go).
	FailedVolumes []VeleroVolumeGroup `json:"failedVolumes"`
	// Schedules are the schedules, those that need action first, then by name.
	Schedules []VeleroSchedule `json:"schedules"`
	// Outside are the namespaces no enabled schedule takes in full (inferred from the schedules'
	// scopes, never proof of a backup): those with volumes first.
	Outside []VeleroOutside `json:"outside"`
	// OutsideError says why Outside could not be read (the namespaces cannot be listed).
	OutsideError string `json:"outsideError,omitempty"`
	// Unread are Velero's kinds the cluster serves that the page could not read: the access reviews
	// say the user may not list them, or the API server refused them (or failed) with nothing
	// listed. What is built from them is unknown, not none (D-177); their tiles name why.
	Unread []string `json:"unread,omitempty"`
	// Synced is false while a kind is still being listed.
	Synced bool `json:"synced"`
	// Error names the kinds the page cannot read; empty when it reads all.
	Error string `json:"error,omitempty"`
}

// VeleroHeader is the page's header line: what the cache tells of Velero itself. A part the user
// may not read is left out.
type VeleroHeader struct {
	Version    string         `json:"version,omitempty"`   // the server image's tag: "1.17.1"
	Namespace  string         `json:"namespace,omitempty"` // where Velero's storage locations and Deployment live
	Server     *VeleroReady   `json:"server,omitempty"`    // the Deployment's ready and desired replicas
	NodeAgents *VeleroReady   `json:"nodeAgents,omitempty"`
	Plugins    []VeleroPlugin `json:"plugins,omitempty"` // the Deployment's init containers
}

// VeleroReady is a workload's ready and desired count.
type VeleroReady struct {
	Ready   int             `json:"ready"`
	Desired int             `json:"desired"`
	Ref     model.ObjectRef `json:"ref"`
}

// VeleroPlugin is one of Velero's plugins: "aws" at "1.13.0".
type VeleroPlugin struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// VeleroTile is one tile: its kind's view opens from its title.
type VeleroTile struct {
	Kind  string        `json:"kind"`
	Title string        `json:"title"`
	Count int           `json:"count"`
	Lines []VeleroCount `json:"lines,omitempty"`
	// Synced is false while its kinds are still being listed.
	Synced bool   `json:"synced"`
	Error  string `json:"error,omitempty"`
}

// VeleroCount is one line of a tile: a state and how many are in it. Status is error for what
// needs action now, warning for what needs a check, pending for what is idle or under way
// (paused, in progress), ok for the rest.
type VeleroCount struct {
	Label  string       `json:"label"`
	Count  int          `json:"count"`
	Status model.Status `json:"status"`
}

// VeleroAttention is one row of Needs attention.
type VeleroAttention struct {
	Problem string       `json:"problem"` // "Location unavailable", "Overdue"
	Status  model.Status `json:"status"`  // error: act now; warning: check
	// Ref is the object: its drawer opens from the row. A row about a kind (no default location)
	// has no name, and its kind's view opens.
	Ref    model.ObjectRef `json:"ref"`
	Object string          `json:"object"`          // "Schedule nightly-all"
	Scope  string          `json:"scope,omitempty"` // the object's namespaces: "all namespaces − 3"
	// Since is when the problem began, as far as Velero's objects tell; zero when they do not.
	Since  time.Time `json:"since,omitzero"`
	Detail string    `json:"detail,omitempty"`
}

// VeleroSchedule is one row of the Schedules table.
type VeleroSchedule struct {
	Ref  model.ObjectRef `json:"ref"`
	Cron string          `json:"cron"` // spec.schedule as written
	When string          `json:"when"` // in words: "daily 02:00", "every 6 h"
	// NextRun is the next run by Velero's rule, or the next time cron names when that one is due
	// and not created; zero while paused or failing validation.
	NextRun time.Time `json:"nextRun,omitzero"`
	// Runs are the last runs, oldest first, at most seven.
	Runs []VeleroRun `json:"runs"`
	// LastCompleted is when the newest Completed backup finished, and Duration how long it ran.
	LastCompleted time.Time `json:"lastCompleted,omitzero"`
	Duration      string    `json:"duration,omitempty"`
	Scope         string    `json:"scope"`
	TTL           string    `json:"ttl,omitempty"`
	Location      string    `json:"location,omitempty"`
	State         string    `json:"state"` // "Enabled", "Paused", "Overdue", "Partially failed"
	// Status is the state's: error and warning need action, the rest is quiet.
	Status model.Status `json:"status"`
}

// VeleroRun is one of a schedule's last runs: completed, partiallyFailed, failed,
// failedValidation, running, deleting, or missed (a run cron expected that was never created).
type VeleroRun struct {
	State  string    `json:"state"`
	At     time.Time `json:"at"`               // when it began, or for a missed run when it was due
	Backup string    `json:"backup,omitempty"` // the Backup's name; "" for a missed run
}

// VeleroOutside is one row of Outside every schedule.
type VeleroOutside struct {
	Ref       model.ObjectRef `json:"ref"` // the Namespace
	PVCs      int             `json:"pvcs"`
	Workloads int             `json:"workloads"` // its Deployments, StatefulSets and DaemonSets
	ManagedBy string          `json:"managedBy,omitempty"`
	// Partly is true when a schedule takes in some of its objects only (a selector, a resource
	// filter, the Namespace labelled out of backups).
	Partly bool   `json:"partly,omitempty"`
	Reason string `json:"reason"`
}

// veleroSource is what the page reads from the cache: a handle on each of its kinds the cluster
// serves and the user may list (as far as the access reviews know).
type veleroSource struct {
	kinds  map[string]*veleroSourceKind
	unsubs []func()
}

type veleroSourceKind struct {
	profile *model.KindProfile
	handle  *cache.Handle
}

// newVeleroSource acquires the page's handles through acquire (the open page's acquireForPage, an
// agent's read's Acquire once the plans are decided); onChange runs after every change of one of
// them. Without Velero's Backups the page reads nothing.
func newVeleroSource(b *Backend, acquire func(*model.KindProfile) *cache.Handle, onChange func()) *veleroSource {
	src := &veleroSource{kinds: map[string]*veleroSourceKind{}}
	if _, served := b.Profile(VeleroBackupsKey); !served {
		return src
	}
	for _, key := range veleroPageKinds {
		p, ok := b.Profile(key)
		if !ok {
			continue
		}
		if b.deniedKind(p) {
			continue // the access reviews say no: a read the API server would refuse (D-177)
		}
		h := acquire(p)
		src.unsubs = append(src.unsubs, h.Subscribe(func(cache.Event) { onChange() }))
		src.kinds[key] = &veleroSourceKind{profile: p, handle: h}
	}
	return src
}

// kindHandles are the source's handles, for openFollowing.
func (src *veleroSource) kindHandles() []kindHandle {
	out := make([]kindHandle, 0, len(src.kinds))
	for _, key := range veleroPageKinds {
		if k := src.kinds[key]; k != nil {
			out = append(out, kindHandle{k.profile, k.handle})
		}
	}
	return out
}

func (src *veleroSource) release() {
	for _, u := range src.unsubs {
		u()
	}
	for _, k := range src.kinds {
		k.handle.Release()
	}
}

func (src *veleroSource) synced() bool {
	for _, k := range src.kinds {
		if !k.handle.HasSynced() {
			return false
		}
	}
	return true
}

func (src *veleroSource) waitSynced(ctx context.Context) {
	for _, k := range src.kinds {
		k.handle.WaitForSync(ctx)
	}
}

// veleroTileKinds are the kinds a Status tile reads: a tile names its own kinds' errors, so the
// page's error line leaves them out and says each refusal once (D-177).
var veleroTileKinds = map[string]bool{
	VeleroSchedulesKey: true, VeleroBackupsKey: true, VeleroRestoresKey: true,
	VeleroLocationsKey: true, VeleroSnapshotLocationsKey: true, VeleroRepositoriesKey: true,
}

// errText is the page's error line: the errors of the kinds no tile reads.
func (src *veleroSource) errText() string {
	var parts []string
	for _, key := range veleroPageKinds {
		k := src.kinds[key]
		if k == nil || veleroTileKinds[key] {
			continue
		}
		if err := k.handle.Err(); err != nil {
			parts = append(parts, k.profile.Title+": "+err.Error())
		}
	}
	return strings.Join(parts, "; ")
}

// veleroBuild is one reading of the page's kinds: their objects (shared with the cache, never
// changed), whether each is listed and readable.
type veleroBuild struct {
	served  bool
	synced  bool
	errText string
	unknown string // why the page cannot tell whether the cluster serves Velero; "" when it can
	objs    map[string][]*unstructured.Unstructured
	read    map[string]bool // the kinds the page reads
	kindErr map[string]string
	kindOK  map[string]bool // listed
}

// build reads every object from the cache.
func (src *veleroSource) build(b *Backend) *veleroBuild {
	_, served := b.Profile(VeleroBackupsKey)
	vb := &veleroBuild{served: served, synced: true, errText: src.errText(),
		objs: map[string][]*unstructured.Unstructured{}, read: map[string]bool{}, kindErr: map[string]string{}, kindOK: map[string]bool{}}
	for key, k := range src.kinds {
		synced := k.handle.HasSynced() // before List, as views do
		vb.objs[key] = KindList(k.handle, k.profile)
		vb.read[key] = true
		vb.kindOK[key] = synced
		vb.kindErr[key] = errText(k.handle.Err())
		vb.synced = vb.synced && synced
	}
	if len(src.kinds) == 0 {
		vb.unknown = b.customKindsUnknown(veleroKindsWhat)
		if !b.CustomKindsSettled() {
			vb.synced = false
		}
	}
	return vb
}

// veleroKindsWhat names Velero's kinds in the text of a page that cannot tell whether the cluster
// serves them.
const veleroKindsWhat = "Velero's kinds"

// veleroWorker keeps the page current while it is open: a cache handle on each kind it reads, its
// model rebuilt at most once per SnapshotInterval after a change and every veleroRefresh.
type veleroWorker struct {
	s       *Session
	backend *Backend
	src     *veleroSource
	planKey string       // which kinds the open page reads and how (veleroPlanKey); the state's lock
	refused refusedReads // the reads given back as refused (pagerefusals.go)

	mu         sync.Mutex
	namespaces []string

	build    atomic.Pointer[veleroBuild]
	model    atomic.Pointer[VeleroDashboardModel]
	snap     atomic.Pointer[TableSnapshot]
	built    atomic.Int64 // when the last build ran, Unix nanoseconds
	wake     chan struct{}
	quit     chan struct{}
	stopped  atomic.Bool
	panicked atomic.Bool
}

// startVeleroPage starts the page's worker; the first build runs at once.
func (s *Session) startVeleroPage(b *Backend, namespaces []string) *veleroWorker {
	w := &veleroWorker{s: s, backend: b, namespaces: slices.Clone(namespaces), wake: make(chan struct{}, 1), quit: make(chan struct{})}
	w.refused.startAt(b.ns.refusalsGen())
	wake := w.wake
	w.src = newVeleroSource(b, b.acquireForPage, func() { signal(wake) }) // nothing while a plan is pending
	signal(w.wake)
	s.goSafe("velero page", func() {
		runCoalesced(w.quit, w.wake, s.opts.SnapshotInterval, w.unsettled, w.rebuild)
	})
	return w
}

func (w *veleroWorker) setNamespaces(ns []string) {
	w.mu.Lock()
	changed := !slices.Equal(w.namespaces, ns)
	w.namespaces = slices.Clone(ns)
	w.mu.Unlock()
	if changed {
		signal(w.wake)
	}
}

// stop ends the loop and releases the handles; the informers stay warm.
func (w *veleroWorker) stop() {
	if !w.stopped.CompareAndSwap(false, true) {
		return
	}
	close(w.quit)
	w.src.release()
}

// unsettled reports whether the page should build again without an event: a sync state or an
// error changed (neither comes with one), the definitions' list ended for a page that reads
// nothing, or veleroRefresh passed. A read the API server refused wholly goes back first, its
// refusal kept (pagerefusals.go).
func (w *veleroWorker) unsettled() bool {
	gave := false
	for _, k := range w.src.kinds {
		gave = w.refused.giveBack(k.handle) || gave
	}
	last := w.build.Load()
	if last == nil || gave || last.synced != w.src.synced() || last.errText != w.src.errText() {
		return true
	}
	if len(w.src.kinds) == 0 && last.unknown != w.backend.customKindsUnknown(veleroKindsWhat) {
		return true
	}
	return time.Since(time.Unix(0, w.built.Load())) >= veleroRefresh
}

// rebuild builds, stores and announces the page; a panic is reported once and does not end the loop.
func (w *veleroWorker) rebuild() {
	defer func() {
		if r := recover(); r != nil {
			w.s.log.Error("engine: building the Velero page failed", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			if w.panicked.CompareAndSwap(false, true) {
				w.s.emit(Event{Type: EventToast, Level: LevelError, Message: "the Velero page failed to update: internal error"})
			}
		}
	}()
	w.mu.Lock()
	ns := slices.Clone(w.namespaces)
	w.mu.Unlock()
	w.built.Store(time.Now().UnixNano())
	vb := w.src.build(w.backend)
	m := w.s.veleroModel(w.backend, vb, ns)
	snap := veleroSnapshot(w.backend.Context, m)
	snap.Revision = w.s.state.snapshots.Add(1)
	if w.stopped.Load() {
		return
	}
	w.build.Store(vb)
	w.model.Store(m)
	w.snap.Store(snap)
	w.s.emit(Event{Type: EventTable, View: VeleroDashboard, Revision: snap.Revision})
}

// veleroSnapshot is the page's table snapshot: no rows (the page draws its own tables from the
// model), its sync state and error, so that the window and Session.Table know the page's state.
func veleroSnapshot(context string, m *VeleroDashboardModel) *TableSnapshot {
	snap := &TableSnapshot{
		View: VeleroDashboard, Context: context, Title: VeleroDashboardTitle, Namespaced: true,
		Columns: veleroPageProfile.Columns, Rows: []model.Row{}, Namespaces: slices.Clone(m.Namespaces),
		Synced: m.Synced, Error: m.Error,
	}
	if !m.Served && m.Error == "" && m.Synced {
		snap.Error = veleroNotServed
	}
	return snap
}

// veleroNotServed is the page's text on a cluster without Velero.
const veleroNotServed = "The cluster serves no Velero kinds."

// VeleroDashboard returns the Velero page for the given namespaces (nil: the namespace filter the
// user has set). While the page is open with those namespaces it is the page's latest build;
// otherwise it is built once from the cache: the page's handles are held for the call, which waits
// (bounded by ctx and 15 seconds) for their initial lists, and released after.
func (s *Session) VeleroDashboard(ctx context.Context, contextID string, namespaces []string) (*VeleroDashboardModel, error) {
	b, err := s.TargetBackend(contextID) // the context's cluster, "" the shown one (D-69)
	if err != nil {
		return nil, err
	}
	if namespaces == nil {
		namespaces = s.windowNamespaces(b)
	}
	s.state.mu.Lock()
	page := s.state.velero
	s.state.mu.Unlock()
	if page != nil && page.backend == b {
		page.mu.Lock()
		same := slices.Equal(page.namespaces, namespaces)
		page.mu.Unlock()
		if m := page.model.Load(); same && m != nil {
			return m, nil
		}
		if vb := page.build.Load(); vb != nil {
			return s.veleroModel(b, vb, namespaces), nil
		}
	}
	wctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	// Once the cluster's definitions are listed: before, Velero's kinds are not known yet.
	waitCustomKindsSettled(wctx, b)
	// Once the kinds' plans are decided, following a refusal in all namespaces
	// (accessiblens_follow.go), as the Overviews' reads do.
	src := openFollowing(wctx, b, veleroPageKinds, func() (*veleroSource, []kindHandle) {
		src := newVeleroSource(b, b.Acquire, func() {})
		return src, src.kindHandles()
	}, (*veleroSource).release)
	defer src.release()
	src.waitSynced(wctx)
	return s.veleroModel(b, src.build(b), namespaces), nil
}

// veleroPlanKey is how the open page reads its kinds: which the cluster serves, at which version
// (a definition that moves to another version opens the page again), and how (pageReadKey: a
// pending plan, all namespaces, or the namespaces).
func (b *Backend) veleroPlanKey() string {
	var sb strings.Builder
	for _, key := range veleroKinds {
		if p, ok := b.Profile(key); ok {
			sb.WriteString(p.Version)
		}
		sb.WriteByte('|')
	}
	return b.pageReadKey(veleroPageKinds) + sb.String()
}

// reconcileVeleroPageLocked runs the page's worker: started when the page opens, given the UI's
// namespaces, stopped when another view opens or the backend changes, and started again when the
// served Velero kinds or the access reviews change how it reads them, or cluster.refreshAccess
// follows a read given back as refused.
func (st *state) reconcileVeleroPageLocked(b *Backend, now UIState) {
	want := b != nil && now.View == VeleroDashboard
	var key string
	if want {
		key = b.veleroPlanKey()
	}
	if st.velero != nil && (!want || st.velero.backend != b || st.velero.planKey != key || st.velero.refused.retake(b)) {
		st.velero.stop()
		st.velero = nil
	}
	switch {
	case want && st.velero == nil:
		st.velero = st.s.startVeleroPage(b, now.Namespaces)
		st.velero.planKey = key
	case st.velero != nil:
		st.velero.setNamespaces(now.Namespaces)
	}
}

// detachVeleroLocked stops the page's worker.
func (st *state) detachVeleroLocked() {
	if st.velero != nil {
		st.velero.stop()
		st.velero = nil
	}
}

// veleroTable returns the open page's latest snapshot, or nil.
func (st *state) veleroTable() *TableSnapshot {
	st.mu.Lock()
	w := st.velero
	st.mu.Unlock()
	if w == nil {
		return nil
	}
	return w.snap.Load()
}
