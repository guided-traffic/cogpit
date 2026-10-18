package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// The pool of connected clusters (D-69, the owner's F16 of 2026-10-16: several clusters connected
// at once, one shown). The engine holds one backend per connected context, each with its own cache,
// connection watch (D-58), discovery, access reviews, port forwards and the dock's streams; the
// window shows one of them (state.shown, the window's view state; R3's windows, D-52, later get
// one each from the same pool).
//
//   - cluster.connect connects a context that is not in the pool and shows it; one that is in the
//     pool is only shown: no request, no new cache, its views come from its cache at once. The
//     cluster shown before stays connected.
//   - Leaving a cluster stops the window's views on it, which release their cache handles: the
//     cache keeps the informers warm for its warm period (5 minutes, cache.DefaultWarmPeriod), so
//     that coming back within it lists nothing again; then they go, and the cluster is cold
//     (ConnectedCluster.Cold). Its heartbeat, discovery, access reviews, the dock's streams and its
//     jobs go on.
//   - cluster.disconnect closes one context's backend, the shown one by default: its streams end
//     and its tabs close, as a Disconnect did for the one connection.
//   - Every command chooses its cluster: the window's views the shown one (Session.Backend), a
//     command on an object reference the reference's context, a command without one its optional
//     context parameter (OnCluster), the shown cluster by default. A context that is not connected
//     is an error (NotConnectedError); a read never connects one.

// pooled is one connected backend of the pool.
type pooled struct {
	b   *Backend
	gen uint64 // the connect that made it: UIState.connGen and ConnectedCluster.gen hold it

	// cold watches a left cluster's warm informers (armColdLocked); nil while the cluster is
	// shown, once it is cold, and after it left the pool. Guarded by state.mu.
	cold *time.Timer
}

// NotConnectedError is the error of a command whose context is not connected (D-69): reads and
// writes never connect a cluster by themselves. It matches ErrNotConnected.
type NotConnectedError struct {
	Context string
}

func (e *NotConnectedError) Error() string {
	return fmt.Sprintf("context %s is not connected; cluster.connect it first", kubeconfig.DisplayText(e.Context, 200))
}

// Is makes errors.Is(err, ErrNotConnected) hold.
func (e *NotConnectedError) Is(target error) bool { return target == ErrNotConnected }

// BackendOf returns the connected backend of a context, the shown one for "": nil when the context
// is not connected (or, for "", while no cluster is shown). It is shared: callers must not modify it.
func (s *Session) BackendOf(contextID string) *Backend {
	st := s.state
	st.mu.Lock()
	defer st.mu.Unlock()
	if contextID == "" {
		return st.shown
	}
	if e := st.pool[contextID]; e != nil {
		return e.b
	}
	return nil
}

// TargetBackend returns the backend a command reads or acts on: the context's when one is named
// (a NotConnectedError when it is not connected), else the shown cluster's (ErrNotConnected while
// none is shown).
func (s *Session) TargetBackend(contextID string) (*Backend, error) {
	if b := s.BackendOf(contextID); b != nil {
		return b, nil
	}
	if contextID == "" {
		return nil, ErrNotConnected
	}
	return nil, &NotConnectedError{Context: contextID}
}

// Backends returns every connected backend, sorted by context id.
func (s *Session) Backends() []*Backend {
	st := s.state
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]*Backend, 0, len(st.pool))
	for _, e := range st.pool {
		out = append(out, e.b)
	}
	slices.SortFunc(out, func(a, b *Backend) int { return strings.Compare(a.Context, b.Context) })
	return out
}

// filterNamespace is the namespace filter's single namespace while the window shows the context's
// cluster, "" otherwise: a create without a namespace lands there, as in the window (another
// cluster's objects land in their context's default namespace).
func (s *Session) filterNamespace(contextID string) string {
	if u := s.UIState(); u.Context == contextID && len(u.Namespaces) == 1 {
		return u.Namespaces[0]
	}
	return ""
}

// windowNamespaces is the namespace filter the window shows for b: the UI's while the window shows
// b's cluster, every namespace (nil) for another cluster.
func (s *Session) windowNamespaces(b *Backend) []string {
	if u := s.UIState(); b != nil && u.Context == b.Context {
		return u.Namespaces
	}
	return nil
}

// holds reports whether b is still the pool's backend of its context.
func (st *state) holds(b *Backend) bool {
	if b == nil {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	e := st.pool[b.Context]
	return e != nil && e.b == b
}

// pooledLocked returns the pool's entry for b, nil when b left it. state.mu is held.
func (st *state) pooledLocked(b *Backend) *pooled {
	if b == nil {
		return nil
	}
	if e := st.pool[b.Context]; e != nil && e.b == b {
		return e
	}
	return nil
}

// addLocked puts a connected backend into the pool; the first one starts the diagnostics loop.
// state.mu is held.
func (st *state) addLocked(e *pooled) {
	if st.pool == nil {
		st.pool = map[string]*pooled{}
	}
	st.pool[e.b.Context] = e
	if st.diagStop == nil {
		st.diagStop = make(chan struct{})
		stop := st.diagStop
		st.s.goSafe("diagnostics", func() { st.s.diagnosticsLoop(stop) })
	}
}

// removeLocked takes a context's backend out of the pool and returns it for closing outside the
// lock (nil when the context is not connected); the window's views on it stop. The last one stops
// the diagnostics loop. state.mu is held.
func (st *state) removeLocked(contextID string) *Backend {
	e := st.pool[contextID]
	if e == nil {
		return nil
	}
	delete(st.pool, contextID)
	st.cancelReconnectLocked(contextID) // a reconnect under way ends with it (reconnect.go)
	e.stopCold()
	if st.shown == e.b {
		st.detachViewsLocked()
	}
	if len(st.pool) == 0 && st.diagStop != nil {
		close(st.diagStop)
		st.diagStop = nil
	}
	return e.b
}

// leaveLocked stops the window's views on the shown cluster, which stays connected: the views
// release their cache handles and the cache keeps them warm for its warm period, after which the
// cluster is cold (armColdLocked). state.mu is held.
func (st *state) leaveLocked() {
	old := st.shown
	if old == nil {
		return
	}
	st.detachViewsLocked()
	if e := st.pooledLocked(old); e != nil {
		st.armColdLocked(e)
	}
}

// showLocked makes a pooled backend the shown one: the views on the cluster shown before stop,
// and the next reconcile starts the views on this one from its cache. state.mu is held.
func (st *state) showLocked(e *pooled) {
	if st.shown == e.b {
		return
	}
	st.leaveLocked()
	st.shown = e.b
	e.stopCold()
}

// detachViewsLocked stops everything of the window's view state bound to the shown backend: the
// view, the pages, the dashboards and the drawer, which release their cache handles, and the rows
// kept per view. The backend itself is left alone. state.mu is held.
func (st *state) detachViewsLocked() {
	if st.view != nil {
		st.view.stop()
		st.view = nil
	}
	if st.page != nil {
		st.page.stop()
		st.page = nil
	}
	if st.cluster != nil {
		st.cluster.stop()
		st.cluster = nil
	}
	if st.flux != nil {
		st.flux.stop()
		st.flux = nil
	}
	st.detachArgoLocked()
	if st.drawer != nil {
		st.drawer.stop()
		st.drawer = nil
	}
	st.shown = nil
	st.vanished = nil
	clear(st.rowCaches)
}

// coldSlack is how long after the cache's last warm period ends the pool looks again: the cache's
// own timer retires the informers at that moment.
const coldSlack = 50 * time.Millisecond

// armColdLocked watches a left cluster until its views' informers are gone from its cache: a
// timer at the end of the cache's last warm period asks the cache again (a release meanwhile
// moves it), and once nothing is warm the cluster is cold (ConnectedCluster.Cold). No request is
// sent, and nothing runs between the timer's checks. state.mu is held.
func (st *state) armColdLocked(e *pooled) {
	e.stopCold()
	var until time.Time
	if e.b.Cache != nil {
		until = e.b.Cache.WarmUntil()
	}
	wait := time.Until(until) + coldSlack
	if until.IsZero() {
		wait = 0 // nothing warm: the views held nothing of their own
	}
	e.cold = time.AfterFunc(max(wait, 0), func() { st.s.checkCold(e) })
}

// stopCold stops the cold watch's timer. state.mu is held.
func (e *pooled) stopCold() {
	if e.cold != nil {
		e.cold.Stop()
		e.cold = nil
	}
}

// checkCold is the cold watch's timer: the cluster is cold once nothing in its cache is warm any
// more, unless it was shown again or left the pool; a warm period that a release meanwhile
// started waits for its end.
func (s *Session) checkCold(e *pooled) {
	st := s.state
	st.mu.Lock()
	if st.closed || st.pooledLocked(e.b) != e || st.shown == e.b {
		st.mu.Unlock()
		return
	}
	if e.b.Cache != nil {
		if until := e.b.Cache.WarmUntil(); until.After(time.Now()) {
			st.armColdLocked(e)
			st.mu.Unlock()
			return
		}
	}
	e.cold = nil
	st.mu.Unlock()
	s.UpdateUI(func(u *UIState) {
		if i := u.connectionIndex(e.b.Context, e.gen); i >= 0 && !u.Connections[i].Shown {
			u.Connections[i].Cold = true
		}
	})
}

// showConnected shows a connected cluster in the UI state (Connect of a pooled context): its
// connection as its watch last said it, its most recently used tab, else its default view in the
// preview tab (syncTabs), and the navigator drilled into it. The cluster's views start from its
// cache (uiChanged). A connect again to the shown cluster only drills the navigator into it.
func (s *Session) showConnected(b *Backend, gen uint64) {
	view := defaultView(b)
	sort := s.ViewSort(view)
	s.UpdateUI(func(u *UIState) {
		if u.Context == b.Context && u.connGen == gen && u.Connection.State.Live() {
			u.NavigatorScope = NavScopeCluster(b.Context) // every connect drills in (navscope.go)
			return
		}
		i := u.connectionIndex(b.Context, gen)
		if i < 0 {
			return // disconnected meanwhile
		}
		if u.Context != b.Context {
			u.leaveCluster() // no filter crosses to another cluster (WF20)
		}
		u.Context = b.Context
		u.Connection = u.Connections[i].Connection
		u.connGen = gen
		u.View = view
		u.Sort = sort
		u.Selected = nil
		u.Drawer = nil
		u.Filter = ""
	})
}

// Show shows a connected cluster in the window (ui.navigate to an object of another connected
// cluster): as cluster.connect of a context the pool holds, without ever connecting one. A context
// that is not connected is a NotConnectedError. The cluster's tab comes with its selection, without
// the objects gone meanwhile (DropGoneSelected).
func (s *Session) Show(contextID string) error {
	if err := s.show(contextID); err != nil {
		return err
	}
	s.DropGoneSelected(context.Background(), s.UIState())
	return nil
}

func (s *Session) show(contextID string) error {
	st := s.state
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		return ErrClosed
	}
	if st.pool[contextID] == nil {
		st.mu.Unlock()
		return &NotConnectedError{Context: contextID}
	}
	if st.connectCancel != nil {
		st.connectCancel(errConnectSuperseded) // the window shows this one now: a pending connect gives up
		st.connectCancel = nil
	}
	st.connectSeq++
	seq := st.connectSeq
	st.mu.Unlock()

	st.connMu.Lock()
	defer st.connMu.Unlock()
	st.mu.Lock()
	e := st.pool[contextID]
	if st.closed || st.connectSeq != seq || e == nil {
		st.mu.Unlock()
		if e == nil {
			return &NotConnectedError{Context: contextID}
		}
		return errShowSuperseded
	}
	st.showLocked(e)
	st.mu.Unlock()
	s.showConnected(e.b, e.gen)
	s.applyLoginHolds() // shown: its login is renewed again, a lapsed one now (connwatch_lapse.go)
	return nil
}

// errShowSuperseded is Show's error when a newer connect took over.
var errShowSuperseded = errors.New("show: superseded by a newer connect")

// errDisconnectSuperseded is Disconnect's error when a newer connect took over.
var errDisconnectSuperseded = errors.New("disconnect: superseded by a newer connect")
