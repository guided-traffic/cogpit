package engine

import (
	"fmt"
	"maps"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// A namespace the filter names that is deleted (the owner's alpha walk of 2026-10-22, AW11): his
// Pods view was filtered to walk-gw9, a namespace without pods that a ConfigMap's finalizer held
// Terminating, and the filter's button turned red only "sehr viel später": the window read the
// namespaces again only when the view's rows changed, and no row of it did (D-106's gap). The
// namespaces' own changes now tell it. The lead's details, open to revision (D-106):
//   - the engine watches the namespaces while the shown cluster's namespace filter names some:
//     then a namespace's state shows (the filter's button, nsgone.go); one handle, shared through
//     the cache with the filter's own reads and the access reviews';
//   - only while the user may list namespaces, as Backend.Listable answers: nothing is listed
//     while the access reviews are out (the answer is polled from memory, never asked of the API
//     server) or for a user who may not; a refused list ends the watch until the access is
//     refreshed, as the custom kinds' watch's does (customkinds.go);
//   - EventNamespaces (View the context) only when the names or the terminating ones changed, a
//     label or a condition the namespace controller writes sends none; coalesced to one per
//     nsWatchCoalesce snapshot intervals (500 ms by default), so a burst of namespaces costs the
//     window a few reads, not one per watch event.

// nsWatchCoalesce stretches the snapshot interval for the namespaces' events: each one has the
// window read every namespace.
const nsWatchCoalesce = 5

// nsWatch follows a backend's namespaces for EventNamespaces.
type nsWatch struct {
	s *Session
	b *Backend

	wake     chan struct{}
	quit     chan struct{}
	stopped  atomic.Bool
	filtered atomic.Bool // the shown cluster is b and its namespace filter names namespaces
	retaken  atomic.Bool // a refresh of the access asks again after a refusal
	unsubUI  func()

	// mu guards h, unsub, synced and last: stop releases them from the backend's close.
	mu    sync.Mutex
	h     *cache.Handle
	unsub func()

	synced bool
	last   map[string]bool // the namespaces of the last event, true for a terminating one; nil before

	refused bool // the loop's own: the API server refused the list

	steps atomic.Int64 // steps of the loop so far (tests wait for one)
	seen  atomic.Int64 // namespaces the last read held (tests)
}

// attachNamespaceWatch starts the namespaces watch of a backend with a cache. It runs before the
// backend is published (st.mu held), like attachCustomKinds; it holds nothing until a namespace
// filter asks for it.
func (s *Session) attachNamespaceWatch(b *Backend) {
	if b.Cache == nil || b.nsWatch != nil {
		return
	}
	w := &nsWatch{s: s, b: b, wake: make(chan struct{}, 1), quit: make(chan struct{})}
	w.follow(s.UIState())
	w.unsubUI = s.Subscribe(func(ev Event) {
		if ev.Type == EventUIState && ev.UIState != nil {
			w.follow(*ev.UIState)
		}
	})
	b.nsWatch = w
	interval := s.opts.SnapshotInterval * nsWatchCoalesce
	s.goSafe("namespaces watch", func() { runCoalesced(w.quit, w.wake, interval, w.unsettled, w.step) })
}

// follow notes whether ui's namespace filter names namespaces of b, and wakes the loop when that
// changed. It runs on the emitting goroutine and does not block.
func (w *nsWatch) follow(ui UIState) {
	on := ui.Context == w.b.Context && len(ui.Namespaces) > 0
	if w.filtered.Swap(on) != on {
		signal(w.wake)
	}
}

// poke wakes the loop: the access reviews answered (accessChanged).
func (w *nsWatch) poke() {
	if w != nil {
		signal(w.wake)
	}
}

// retake lets the watch list the namespaces again after a refusal (Backend.RefreshAccess).
func (w *nsWatch) retake() {
	if w != nil {
		w.retaken.Store(true)
		signal(w.wake)
	}
}

// stop ends the loop and releases the handle. The backend's close calls it; nil-safe.
func (w *nsWatch) stop() {
	if w == nil || !w.stopped.CompareAndSwap(false, true) {
		return
	}
	close(w.quit)
	w.unsubUI()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.releaseLocked()
}

// wanted reports whether the watch should hold the namespaces: a filter names some, no refusal
// stands, and the user may list them.
func (w *nsWatch) wanted() bool {
	if !w.filtered.Load() || (w.refused && !w.retaken.Load()) {
		return false
	}
	p, ok := w.b.Profile(namespacesKind)
	if !ok || p.Local {
		return false
	}
	listable, known := w.b.Listable(p, nil)
	return listable && known
}

// unsettled reports whether the loop has to step without a watch event: the access reviews
// answered, the first list ended, or the API server refused it; none comes with an event.
func (w *nsWatch) unsettled() bool {
	w.mu.Lock()
	h, synced := w.h, w.synced
	w.mu.Unlock()
	if h == nil {
		return w.wanted()
	}
	return !w.wanted() || h.HasSynced() != synced || cache.IsRefused(h.Err())
}

// step takes or gives back the handle as wanted, and emits EventNamespaces when the namespaces
// changed. A panic is logged and leaves the last state in place.
func (w *nsWatch) step() {
	defer w.steps.Add(1)
	defer func() {
		if r := recover(); r != nil {
			w.s.log.Error("engine: following the namespaces failed", "context", w.b.Context,
				"panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	if w.retaken.CompareAndSwap(true, false) {
		w.refused = false
	}
	if w.read(w.wanted()) && !w.stopped.Load() {
		w.s.emit(Event{Type: EventNamespaces, View: w.b.Context})
	}
}

// read takes or gives back the handle and, once the namespaces are listed, reports whether they
// differ from the last event's (the first read after a take always does).
func (w *nsWatch) read(want bool) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case w.stopped.Load():
		return false
	case want && w.h == nil:
		p, _ := w.b.Profile(namespacesKind)
		w.h = w.b.Acquire(p)
		wake := w.wake
		w.unsub = w.h.Subscribe(func(cache.Event) { signal(wake) })
	case !want && w.h != nil:
		w.releaseLocked()
	}
	if w.h == nil {
		return false
	}
	if cache.IsRefused(w.h.Err()) {
		w.refused = true
		w.releaseLocked()
		return false
	}
	if w.synced = w.h.HasSynced(); !w.synced {
		return false // a partial list is no state of the namespaces
	}
	objs := w.h.List()
	next := make(map[string]bool, len(objs))
	for _, o := range objs {
		next[o.GetName()] = kinds.NamespaceDeletionOf(o).Terminating
	}
	w.seen.Store(int64(len(next)))
	if w.last != nil && maps.Equal(next, w.last) {
		return false
	}
	w.last = next
	return true
}

// releaseLocked gives the handle back; the next take sends an event once listed.
func (w *nsWatch) releaseLocked() {
	if w.h == nil {
		return
	}
	w.unsub()
	w.h.Release()
	w.h, w.unsub, w.synced, w.last = nil, nil, false, nil
}
