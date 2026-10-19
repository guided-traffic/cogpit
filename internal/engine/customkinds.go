package engine

import (
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// crdsGVR is the resource the engine watches to learn a cluster's custom resource kinds.
var crdsGVR = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}

// customKindsCoalesce stretches the snapshot interval for rebuilding the custom kinds: installing
// a chart creates dozens of definitions within a second, and each rebuild redraws the sidebar.
const customKindsCoalesce = 5

// customKinds keeps a backend's custom resource kinds current: it holds a cache handle on the
// cluster's custom resource definitions (one watch, shared through the cache, released when the
// backend closes), rebuilds the kinds' profiles after changes, coalesced, and publishes them as
// one immutable customSet that views, commands, agents and the UI read concurrently.
type customKinds struct {
	s *Session
	b *Backend

	// hmu guards handle, unsub and dropped: a refused list ends the watch (drop), and a refresh of
	// the access reviews takes it again (retake).
	hmu     sync.Mutex
	handle  *cache.Handle
	unsub   func()
	dropped string // the refusal that ended the watch; handle holds nothing meanwhile

	wake    chan struct{}
	quit    chan struct{}
	stopped atomic.Bool
	set     atomic.Pointer[customSet]

	// entries are the kinds of the last build by kind key. Only the rebuild loop touches them.
	entries map[string]customEntry
}

type customEntry struct {
	kind    kinds.CustomKind
	profile *model.KindProfile
}

// customSet is one state of a backend's custom resource kinds. It is published whole and never
// changed; a definition update that changes nothing a view shows keeps its profile pointer, so the
// open view keeps running.
type customSet struct {
	profiles []*model.KindProfile // sorted by API group, then Kind
	byKey    map[string]*model.KindProfile
	synced   bool   // the definitions' initial list is done
	err      string // the watch's last error: without list and watch on definitions there are no custom kinds
	// discovered: the definitions could not be listed (err), and API discovery answered which
	// curated kinds the cluster serves (A90, customkinds_discovery.go).
	discovered bool
}

// attachCustomKinds starts the custom kinds watch of a backend that asks for it. It runs before
// the backend is published (st.mu held), so every reader of the backend sees b.custom.
func (s *Session) attachCustomKinds(b *Backend) {
	if !b.CustomResources || b.Cache == nil || b.custom != nil {
		return
	}
	ck := &customKinds{
		s: s, b: b,
		wake:    make(chan struct{}, 1),
		quit:    make(chan struct{}),
		entries: map[string]customEntry{},
	}
	ck.set.Store(&customSet{byKey: map[string]*model.KindProfile{}})
	ck.handle = b.Cache.Acquire(cache.Scope{GVR: crdsGVR})
	ck.unsub = ck.handle.Subscribe(func(cache.Event) { signal(ck.wake) })
	signal(ck.wake)
	b.custom = ck
	interval := s.opts.SnapshotInterval * customKindsCoalesce
	s.goSafe("custom resource kinds", func() {
		runCoalesced(ck.quit, ck.wake, interval, ck.unsettled, ck.rebuild)
	})
}

// stop ends the loop and releases the handle on the definitions.
func (ck *customKinds) stop() {
	if !ck.stopped.CompareAndSwap(false, true) {
		return
	}
	close(ck.quit)
	ck.hmu.Lock()
	defer ck.hmu.Unlock()
	ck.unsub()
	ck.handle.Release()
}

// current returns the definitions' handle and the error the custom kinds show: the watch's own,
// or the refusal that ended it.
func (ck *customKinds) current() (*cache.Handle, string) {
	h, msg, _ := ck.state()
	return h, msg
}

// state is current with the handle's error that msg was made from, nil once the watch was dropped.
// A refusal is judged on this one read: read again, a refusal that arrived in between was dropped
// with the empty text of the first read, and the watch ended with no error, no refusal and no
// discovery until the next connect (TestCuratedKindsFromDiscovery, CI, 2026-10-16).
func (ck *customKinds) state() (h *cache.Handle, msg string, err error) {
	ck.hmu.Lock()
	defer ck.hmu.Unlock()
	if ck.dropped != "" {
		return ck.handle, ck.dropped, nil
	}
	err = ck.handle.Err()
	return ck.handle, errText(err), err
}

// drop ends a watch of the definitions that the API server refused (403 Forbidden): retried, it
// would cost a refused list on every backoff step for as long as the cluster is connected (a
// namespace-limited user, measured on kind 2026-10-13: ten refused requests in 20 seconds). The
// custom kinds keep the refusal as their error; a refresh of the access reviews or the next
// connect asks again (retake), as for a kind refused in all namespaces (accessiblens_plan.go).
// The refusal's text is the reason: dropped holds it, and an empty one would end the watch without
// saying so (refused and retake read dropped).
func (ck *customKinds) drop(h *cache.Handle, refusal error) {
	ck.hmu.Lock()
	defer ck.hmu.Unlock()
	if ck.stopped.Load() || ck.handle != h || ck.dropped != "" || refusal == nil {
		return
	}
	ck.unsub()
	ck.handle.Release()
	ck.handle, ck.unsub, ck.dropped = cache.Pending(cache.Scope{GVR: crdsGVR}), func() {}, refusal.Error()
	// The views whose rows look the definitions up (a custom owner, an event's custom object:
	// kinds.CustomKindsByGroupKind) read them no more and stop waiting for them (relatedKey).
	ck.b.ns.wakeReplan()
}

// refused reports whether the API server refused to list the definitions, so that their watch
// ended (drop) until a refresh of the access reviews or the next connect.
func (ck *customKinds) refused() bool {
	if ck == nil {
		return false
	}
	ck.hmu.Lock()
	defer ck.hmu.Unlock()
	return ck.dropped != ""
}

// retake watches the definitions again after drop.
func (ck *customKinds) retake() {
	ck.hmu.Lock()
	if ck.stopped.Load() || ck.dropped == "" {
		ck.hmu.Unlock()
		return
	}
	ck.handle = ck.b.Cache.Acquire(cache.Scope{GVR: crdsGVR})
	ck.unsub = ck.handle.Subscribe(func(cache.Event) { signal(ck.wake) })
	ck.dropped = ""
	ck.hmu.Unlock()
	if ck.b.Conn != nil { // the refresh asks discovery again: without the definitions nothing else confirms it (D-65)
		ck.b.Conn.InvalidateServedResources()
	}
	signal(ck.wake)
}

// unsettled reports whether the watch's sync state or error changed since the last build: neither
// comes with a watch event.
func (ck *customKinds) unsettled() bool {
	last := ck.set.Load()
	h, errMsg := ck.current()
	return last.synced != h.HasSynced() || last.err != errMsg
}

// rebuild turns the cached definitions into profiles and publishes them. Established definitions
// only; a kind a built-in (curated) profile covers keeps the built-in one. A panic is logged and
// leaves the last set in place.
func (ck *customKinds) rebuild() {
	defer func() {
		if r := recover(); r != nil {
			ck.s.log.Error("engine: building the custom resource kinds failed", "context", ck.b.Context,
				"panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	h, errMsg, err := ck.state()
	synced := h.HasSynced() // before List: a set never claims a partial list is complete
	if cache.IsRefused(err) {
		ck.drop(h, err) // not retried on every backoff step; the set below says why
	}
	crds := h.List()
	slices.SortFunc(crds, func(x, y *unstructured.Unstructured) int { return strings.Compare(x.GetName(), y.GetName()) })
	ck.confirmDiscovery(crds, synced, errMsg) // the discovery cache checked against the definitions (customkinds_digest.go)

	next := make(map[string]customEntry, len(crds))
	for _, crd := range crds {
		k, err := kinds.ParseCRD(crd)
		if err != nil {
			continue // not established (yet), or not a kind Cogpit can name
		}
		key := k.Key()
		if _, dup := next[key]; dup || ck.b.builtinProfile(key) != nil {
			continue
		}
		if old, ok := ck.entries[key]; ok && old.kind.Equal(k) {
			next[key] = old
			continue
		}
		next[key] = customEntry{kind: k, profile: kinds.ProfileFor(k)} // a curated profile, else the generic one
	}
	// Without the definitions, the curated kinds from API discovery (A90, customkinds_discovery.go).
	discovered := errMsg != "" && ck.discoverCurated(next)
	addArgoCompanions(ck.b, next) // argocd.go: Argo CD's settings views come with its Application kind
	changed := len(next) != len(ck.entries)
	for key, e := range next {
		if old, ok := ck.entries[key]; !ok || old.profile != e.profile {
			changed = true
		}
	}
	ck.entries = next

	set := &customSet{
		profiles:   make([]*model.KindProfile, 0, len(next)),
		byKey:      make(map[string]*model.KindProfile, len(next)),
		synced:     synced,
		err:        errMsg,
		discovered: discovered,
	}
	for key, e := range next {
		set.profiles = append(set.profiles, e.profile)
		set.byKey[key] = e.profile
	}
	slices.SortFunc(set.profiles, func(x, y *model.KindProfile) int {
		if c := strings.Compare(x.Group, y.Group); c != 0 {
			return c
		}
		if c := strings.Compare(x.Kind, y.Kind); c != 0 {
			return c
		}
		return strings.Compare(x.Key, y.Key)
	})
	if ck.stopped.Load() {
		return
	}
	ck.set.Store(set)
	if changed {
		ck.s.customKindsChanged(ck.b)
	}
}

// customKindsChanged follows a change of the connected backend's custom kinds: the open view and
// the drawer follow their profiles (a kind that is gone leaves a view that says so, a kind whose
// version changed moves its view to the new version), and EventKinds tells the UI to read
// kinds.list again. The discovery answer follows the definitions themselves (confirmDiscovery,
// D-65), not every change of the kinds: the first build after connecting must not drop the answer
// the disk cache just gave.
func (s *Session) customKindsChanged(b *Backend) {
	st := s.state
	st.mu.Lock()
	if st.closed || st.shown != b {
		st.mu.Unlock()
		return
	}
	// The UI state is read under state.mu: read before it, it could be older than one an UpdateUI
	// applied in between, and reconciling against it would close the drawer or view that update
	// opened. Read here, it is the latest; an UpdateUI still on its way to uiChanged reconciles the
	// same state again.
	ui := s.UIState()
	if ui.Revision > st.appliedRev {
		st.appliedRev = ui.Revision
	}
	st.reconcileLocked(ui)
	gone := st.vanished
	st.mu.Unlock()

	s.emit(Event{Type: EventKinds})
	if gone != nil {
		s.emit(Event{Type: EventTable, View: gone.View, Revision: gone.Revision})
	}
}

// noteVanishedLocked keeps what the open view showed when the cluster stops serving its kind (its
// custom resource definition was deleted or is no longer established): Table then returns a
// snapshot that says so, instead of nothing. reconcileLocked calls it before it stops the view.
func (st *state) noteVanishedLocked(b *Backend, prof *model.KindProfile, view string) {
	switch {
	case prof != nil || b == nil || view == "":
		st.vanished = nil
	case st.view != nil && st.view.backend == b && st.view.profile.Key == view:
		p := st.view.profile
		st.view.mu.Lock()
		params := st.view.params
		st.view.mu.Unlock()
		st.vanished = &TableSnapshot{
			View: p.Key, Context: b.Context, Title: p.Title, Namespaced: p.Namespaced,
			Columns: p.Columns, Rows: []model.Row{}, Sort: params.sort,
			Namespaces: slices.Clone(params.namespaces), Filter: params.filter,
			Synced: true, Revision: st.snapshots.Add(1),
			Error: "The cluster no longer serves " + p.Title + " (" + p.Key + "): its custom resource definition was deleted or is not established.",
		}
	case st.vanished != nil && (st.vanished.View != view || st.vanished.Context != b.Context):
		st.vanished = nil
	}
}

// builtinProfile returns the backend's built-in profile for a kind key, or nil.
func (b *Backend) builtinProfile(key string) *model.KindProfile {
	for _, p := range b.Profiles {
		if p.Key == key {
			return p
		}
	}
	return nil
}

// customSet returns the backend's current custom kinds, or nil when it does not watch them.
func (b *Backend) customSet() *customSet {
	if b == nil || b.custom == nil {
		return nil
	}
	return b.custom.set.Load()
}

// CustomProfiles returns the kinds of the cluster's established custom resource definitions,
// sorted by API group and Kind, without the kinds a built-in profile covers. The slice is an
// immutable snapshot: callers must not modify it.
func (b *Backend) CustomProfiles() []*model.KindProfile {
	if set := b.customSet(); set != nil {
		return set.profiles
	}
	return nil
}

// Cached reports whether the backend's cache already watches a kind in all namespaces, in the
// scope its profile gives it (a view, a drawer or a lookup uses it, or used it within the warm
// period), so that reading it costs no request. A local kind is always cached: its store asks no
// API server.
func (b *Backend) Cached(p *model.KindProfile) bool {
	if p == nil {
		return false
	}
	if p.Local {
		return true
	}
	if isApplications(p) {
		return b.apps.running(b.Scope(p))
	}
	if isHelmReleases(p) { // both drivers' records (helmdriver.go)
		return b.releasesCached(p)
	}
	return b.Cache != nil && (b.Cache.Running(b.Scope(p)) || b.namespacesRunning(p))
}

// customKindsNote explains why a custom resource kind key is unknown: the definitions are still
// being listed, or the user may not list them. It is empty for other keys.
func (b *Backend) customKindsNote(key string) string {
	set := b.customSet()
	if set == nil || !strings.Contains(key, ".") {
		return ""
	}
	switch {
	case set.err != "":
		return " (custom resource kinds are unavailable: " + set.err + ")"
	case !set.synced:
		return " (the cluster's custom resource definitions are still being listed; try again shortly)"
	}
	return ""
}
