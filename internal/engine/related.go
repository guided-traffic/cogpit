package engine

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// relatedSet serves the related-objects indexes a profile declares in Needs (architecture §4.4):
// it holds a cache handle on each index's kind for as long as its owners (a view, the drawer and
// the fetches it serves, one read) hold it, counts the changes of those kinds, and keeps each index
// current. Nothing here lists or watches by itself: the handles share the cache's informers with
// every other consumer.
//
// An index is built from the cache on its first lookup and then kept current by the kind's
// events, each of which moves one object between keys; a lookup copies one key's objects and never
// rebuilds the index. A kind whose objects are derived (Derive) has no such events: its index is
// built again on the first lookup after a change.
type relatedSet struct {
	needs    []model.RelatedIndex
	profiles map[string]*model.KindProfile // by kind key; a kind the cluster does not serve is absent
	handles  map[string]*cache.Handle      // by kind key
	unsubs   []func()

	gen    atomic.Uint64 // bumped by every event of an indexed kind
	builds atomic.Int64  // indexes built from the cache, for tests

	// track: onChange runs only for an event that moves an object into, out of or within a key a
	// lookup asked for (the drawer, which shows a few keys); without it, for every event (a view,
	// whose rows ask for any key).
	track    bool
	onChange func()

	bmu sync.Mutex // serialises index builds; taken before mu, never by an event

	mu      sync.Mutex
	holds   int                      // owners; the handles go back when the last one releases
	indexes map[string]*liveIndex    // by index ID, built on first lookup
	pending map[string]*pendingMoves // by index ID: the events that arrive while it is built
}

// pendingMoves are the events of an index's kind that arrived while the index was built from
// the cache, to apply after the build in their order.
type pendingMoves struct {
	moves []pendingMove
	stale bool // a derived kind changed: the build is good for its lookup only
}

type pendingMove struct {
	id   string
	obj  *unstructured.Unstructured
	keys []string
	sum  model.Totals
}

// liveIndex is one index: every object of the kind under its keys, and for an index with a Sum
// what its objects add up to under each key, moved along with them.
type liveIndex struct {
	byKey   map[string]map[string]*unstructured.Unstructured // key → object id → object
	keysOf  map[string][]string                              // object id → its keys
	watched map[string]bool                                  // keys a lookup asked for (track)
	sums    map[string]model.Totals                          // key → its objects' sum (Sum only)
	sumOf   map[string]model.Totals                          // object id → what it adds (Sum only)
}

// maxWatchedKeys bounds the keys a tracking set remembers; past it every event wakes, as without
// tracking. A drawer asks for a handful.
const maxWatchedKeys = 1024

// newRelatedSet acquires the handles of needs' kinds on b; onChange, if set, runs (on an informer
// goroutine; it must not block) after every change of one of them. It returns nil for no needs.
// The caller holds it once and releases it.
func newRelatedSet(b *Backend, needs []model.RelatedIndex, onChange func()) *relatedSet {
	return newRelatedSetTracking(b, needs, onChange, false)
}

// newRelatedSetTracking is newRelatedSet whose onChange runs, with track, only for the changes of
// the keys looked up through the set.
func newRelatedSetTracking(b *Backend, needs []model.RelatedIndex, onChange func(), track bool) *relatedSet {
	return newRelatedSetWith(b, needs, onChange, track, nil)
}

// newRelatedSetWith is newRelatedSetTracking whose handles come from acquire (nil: Backend.Acquire):
// a drawer reads a namespaced object's related objects in its namespace for a user who may not
// list them everywhere (accessiblens_follow.go).
func newRelatedSetWith(b *Backend, needs []model.RelatedIndex, onChange func(), track bool, acquire func(*model.KindProfile) *cache.Handle) *relatedSet {
	if len(needs) == 0 {
		return nil
	}
	if acquire == nil {
		acquire = b.Acquire
	}
	r := &relatedSet{
		needs: needs, profiles: map[string]*model.KindProfile{}, handles: map[string]*cache.Handle{},
		track: track, onChange: onChange, holds: 1, indexes: map[string]*liveIndex{},
		pending: map[string]*pendingMoves{},
	}
	for _, ix := range needs {
		if _, done := r.handles[ix.Kind]; done {
			continue
		}
		p, ok := b.Profile(ix.Kind)
		if !ok {
			continue
		}
		if ix.Kind == kinds.KeyCustomResourceDefinitions && b.custom.refused() {
			continue // the user may not list them: the lookups stay empty, nothing is retried
		}
		h := acquire(p)
		kind, derived := ix.Kind, p.Derive != nil // the callback reads no map this loop still fills
		r.profiles[kind], r.handles[kind] = p, h
		r.unsubs = append(r.unsubs, h.Subscribe(func(ev cache.Event) {
			r.gen.Add(1)
			if r.apply(kind, derived, ev) || !track {
				if onChange != nil {
					onChange()
				}
			}
		}))
	}
	return r
}

// hold adds an owner, unless the set was released; a held set must be released once more.
func (r *relatedSet) hold() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.holds == 0 {
		return false
	}
	r.holds++
	return true
}

// release gives up one hold; with the last, the handles go back (the informers stay warm).
func (r *relatedSet) release() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.holds == 0 {
		r.mu.Unlock()
		return
	}
	r.holds--
	last := r.holds == 0
	if last {
		r.indexes, r.pending = nil, nil
	}
	r.mu.Unlock()
	if !last {
		return
	}
	for _, u := range r.unsubs {
		u()
	}
	for _, h := range r.handles {
		h.Release()
	}
}

// apply moves an event's object between the keys of every built index on its kind (or keeps it for
// an index being built), and reports whether it touched a key a lookup asked for. It runs on the
// kind's informer goroutine and never waits for a build.
func (r *relatedSet) apply(kind string, derived bool, ev cache.Event) bool {
	if ev.Object == nil {
		return false
	}
	id := ev.Object.GetNamespace() + "/" + ev.Object.GetName()
	relevant := false
	for _, ix := range r.needs {
		if ix.Kind != kind {
			continue
		}
		var keys []string
		var sum model.Totals
		if ev.Type != cache.Deleted && !derived {
			keys, sum = indexKeys(ix, ev.Object), indexSum(ix, ev.Object) // outside the lock: a profile's code
		}
		r.mu.Lock()
		li, pend := r.indexes[ix.ID()], r.pending[ix.ID()]
		switch {
		case derived:
			// The event is about a source object (a Secret of a release): any key may have
			// changed, so the index is built again on the next lookup, and it wakes.
			delete(r.indexes, ix.ID())
			if pend != nil {
				pend.stale = true
			}
			relevant = true
		case li != nil:
			relevant = li.move(id, ev.Object, keys, sum) || relevant
		case pend != nil:
			pend.moves = append(pend.moves, pendingMove{id: id, obj: ev.Object, keys: keys, sum: sum})
		}
		r.mu.Unlock()
	}
	return relevant
}

// move files the object with id under keys (none: it is gone), its sum with it, and reports
// whether a watched key gained or lost it. Called with the set's mu held.
func (li *liveIndex) move(id string, obj *unstructured.Unstructured, keys []string, sum model.Totals) bool {
	relevant := len(li.watched) >= maxWatchedKeys
	old := li.sumOf[id]
	for _, k := range li.keysOf[id] {
		if m := li.byKey[k]; m != nil {
			delete(m, id)
			if len(m) == 0 {
				delete(li.byKey, k)
			}
		}
		if li.sums != nil {
			li.sums[k] = li.sums[k].Sub(old)
		}
		relevant = relevant || li.watched[k]
	}
	if len(keys) == 0 {
		delete(li.keysOf, id)
		if li.sumOf != nil {
			delete(li.sumOf, id)
		}
		return relevant
	}
	li.keysOf[id] = keys
	for _, k := range keys {
		m := li.byKey[k]
		if m == nil {
			m = map[string]*unstructured.Unstructured{}
			li.byKey[k] = m
		}
		m[id] = obj
		if li.sums != nil {
			li.sums[k] = li.sums[k].Add(sum)
		}
		relevant = relevant || li.watched[k]
	}
	if li.sumOf != nil {
		li.sumOf[id] = sum
	}
	return relevant
}

// synced reports whether every served kind of the set finished its initial list.
func (r *relatedSet) synced() bool {
	if r == nil {
		return true
	}
	for kind, h := range r.handles {
		if h.HasSynced() {
			continue
		}
		// A cluster-scoped kind the API server refused (the definitions for a namespace-limited
		// user) has no namespace to read instead: its lookups stay empty, and the rows do not wait
		// for it (TestKindNamespaceLimitedUser, 2026-10-16, after custom owners began to link).
		if p := r.profiles[kind]; p != nil && !p.Namespaced && cache.IsRefused(h.Err()) {
			continue
		}
		return false
	}
	return true
}

// waitSynced waits, bounded by ctx and syncTimeout, for the set's kinds. A kind that does not
// sync leaves its lookups empty, which is a state, not an error.
func (r *relatedSet) waitSynced(ctx context.Context) {
	if r == nil {
		return
	}
	wctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	for _, h := range r.handles {
		h.WaitForSync(wctx)
	}
}

// view returns the set's lookups and its generation: the rows of one snapshot read a key once,
// however many rows ask for it. A nil set yields model.NoRelated.
//
// The generation also says whether every kind had synced: an index is empty until its kind
// syncs, and an informer counts its initial list's last event (bumping gen) before it reports
// synced, with no event after. Rows built in between would otherwise stay cached at a generation
// that never changes, empty related columns and all. The events are counted before synced is
// read, so a view that says synced has every index full.
func (r *relatedSet) view() (model.Related, uint64) {
	if r == nil {
		return model.NoRelated, 0
	}
	g := r.gen.Load() << 1
	if r.synced() {
		g |= 1
	}
	return &relatedView{r: r, seen: map[lookupKey][]*unstructured.Unstructured{}}, g
}

// lookup returns the objects ix files under key, sorted by namespace and name, building the index
// first if it is not built; nil while the kind is listing or not served.
func (r *relatedSet) lookup(ix model.RelatedIndex, key string) []*unstructured.Unstructured {
	h, ok := r.handles[ix.Kind]
	if !ok || !h.HasSynced() || ix.Keys == nil {
		return nil
	}
	r.bmu.Lock()
	defer r.bmu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	li := r.ensure(ix, h)
	if li == nil {
		return nil
	}
	if r.track && len(li.watched) < maxWatchedKeys {
		li.watched[key] = true
	}
	m := li.byKey[key]
	if len(m) == 0 {
		return nil
	}
	out := make([]*unstructured.Unstructured, 0, len(m))
	for _, o := range m {
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b *unstructured.Unstructured) int {
		return cmp.Or(cmp.Compare(a.GetNamespace(), b.GetNamespace()), cmp.Compare(a.GetName(), b.GetName()))
	})
	return out
}

// totals returns what ix's objects under key add up to, building the index first if it is not
// built; false while the kind is listing or not served.
func (r *relatedSet) totals(ix model.RelatedIndex, key string) (model.Totals, bool) {
	h, ok := r.handles[ix.Kind]
	if !ok || !h.HasSynced() || ix.Keys == nil || ix.Sum == nil {
		return model.Totals{}, false
	}
	r.bmu.Lock()
	defer r.bmu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	li := r.ensure(ix, h)
	if li == nil {
		return model.Totals{}, false
	}
	if r.track && len(li.watched) < maxWatchedKeys {
		li.watched[key] = true
	}
	return li.sums[key], true
}

// ensure returns ix's index, building it from h's cache first if it is not built; nil once the
// set is released. Called with bmu and mu held; mu is let go while the index is built.
func (r *relatedSet) ensure(ix model.RelatedIndex, h *cache.Handle) *liveIndex {
	if r.holds == 0 {
		return nil
	}
	id := ix.ID()
	li := r.indexes[id]
	if li == nil {
		// Built from the cache outside the lock, so that the kind's informer never waits for it.
		// The events that arrive meanwhile are applied after the build, in order: one whose
		// object the list holds already moves it to the same keys again, which changes nothing;
		// one the list missed is applied. Objects reach the cache before their event.
		pend := &pendingMoves{}
		r.pending[id] = pend
		r.mu.Unlock()
		built := buildIndex(ix, KindList(h, r.profiles[ix.Kind]))
		r.builds.Add(1)
		r.mu.Lock()
		if r.holds == 0 {
			return nil
		}
		delete(r.pending, id)
		for _, m := range pend.moves {
			built.move(m.id, m.obj, m.keys, m.sum)
		}
		li = built
		if !pend.stale {
			r.indexes[id] = li
		}
	}
	return li
}

// buildIndex files every object under its keys, with its sum for an index that has one; an index
// function that panics on an object skips it.
func buildIndex(ix model.RelatedIndex, objs []*unstructured.Unstructured) *liveIndex {
	li := &liveIndex{
		byKey:  map[string]map[string]*unstructured.Unstructured{},
		keysOf: make(map[string][]string, len(objs)), watched: map[string]bool{},
	}
	if ix.Sum != nil {
		li.sums, li.sumOf = map[string]model.Totals{}, make(map[string]model.Totals, len(objs))
	}
	for _, o := range objs {
		if keys := indexKeys(ix, o); len(keys) > 0 {
			li.move(o.GetNamespace()+"/"+o.GetName(), o, keys, indexSum(ix, o))
		}
	}
	return li
}

// indexSum is what o adds under its keys to an index with a Sum; nothing for one without, or for
// a Sum that panics on o.
func indexSum(ix model.RelatedIndex, o *unstructured.Unstructured) (sum model.Totals) {
	if ix.Sum == nil {
		return sum
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Default().Error("engine: related-objects sum panicked", "index", ix.ID(), "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			sum = model.Totals{}
		}
	}()
	return ix.Sum(o)
}

func indexKeys(ix model.RelatedIndex, o *unstructured.Unstructured) (keys []string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Default().Error("engine: related-objects index panicked", "index", ix.ID(), "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			keys = nil
		}
	}()
	return ix.Keys(o)
}

// relatedView is a relatedSet's lookups for one build of rows or one drawer model.
type relatedView struct {
	r    *relatedSet
	seen map[lookupKey][]*unstructured.Unstructured // not shared between goroutines
}

type lookupKey struct{ index, key string }

// declared returns the declared index with ix's ID.
func (v *relatedView) declared(ix model.RelatedIndex) (model.RelatedIndex, bool) {
	id := ix.ID()
	for _, n := range v.r.needs {
		if n.ID() == id {
			return n, true
		}
	}
	return model.RelatedIndex{}, false
}

// Objects implements model.Related. Only the indexes the profile declared are served.
func (v *relatedView) Objects(ix model.RelatedIndex, key string) []*unstructured.Unstructured {
	lk := lookupKey{ix.ID(), key}
	if objs, ok := v.seen[lk]; ok {
		return objs
	}
	var objs []*unstructured.Unstructured
	if n, ok := v.declared(ix); ok {
		objs = v.r.lookup(n, key)
	}
	v.seen[lk] = objs
	return objs
}

// Totals implements model.RelatedTotals. Only the indexes the profile declared are served.
func (v *relatedView) Totals(ix model.RelatedIndex, key string) (model.Totals, bool) {
	n, ok := v.declared(ix)
	if !ok {
		return model.Totals{}, false
	}
	return v.r.totals(n, key)
}

// Listed implements model.Related: the index is declared, its kind served and listed.
func (v *relatedView) Listed(ix model.RelatedIndex) bool {
	if _, ok := v.declared(ix); !ok {
		return false
	}
	h, ok := v.r.handles[ix.Kind]
	return ok && h.HasSynced()
}

// BuildRows builds table rows for objects of a kind as its view would, related objects included:
// for a profile with Needs it holds the indexed kinds' handles for the call and waits (bounded by
// ctx and 15 seconds) for their initial lists. Commands that list rows outside a view use it.
func (s *Session) BuildRows(ctx context.Context, b *Backend, p *model.KindProfile, objs []*unstructured.Unstructured) []model.Row {
	rs := newRelatedSet(b, p.Needs, nil)
	defer rs.release()
	rs.waitSynced(ctx)
	rel, _ := rs.view()
	rows := make([]model.Row, 0, len(objs))
	for _, o := range objs {
		rows = append(rows, model.BuildRowWith(p, b.Context, o, rel))
	}
	return rows
}
