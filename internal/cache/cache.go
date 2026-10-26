// Package cache is the only place Cogpit lists and watches. It keeps one shared client-go
// informer per (kind, namespace scope) of a cluster, reference-counted by handles and kept warm for
// a grace period after the last handle is released; it drops managedFields before an object
// enters the store; views, commands and MCP tools read only from it (architecture §5, D-13).
//
// A label-selected scope is served from the running unselected scope of the same resource,
// namespace and metadata flag when there is one (its cover), so that it costs no watch of its own
// (cover.go).
//
// Layer L1.
//
// # Errors are states
//
// A list or watch that fails (a forbidden kind, a lost connection) is not a failure of the
// cache: the informer retries with client-go's backoff, and Handle.Err reports the last error
// until a list or a watch succeeds again.
//
// # Panics: what is and is not recovered
//
// A client-go informer runs on several goroutines: the one that calls Run (it also processes the
// queue and updates the store), the reflector (list, watch, decoding, the transform), and one
// pair per event handler. client-go guards its goroutines with utilruntime.HandleCrash, which logs
// and then re-panics while utilruntime.ReallyCrash is true, the default. In this package:
//
//   - Code the cache runs on client-go's goroutines recovers on its own: the Source's List and
//     Watch (a panic becomes a list or watch error), the managedFields transform (the object is
//     kept unchanged), the event dispatch to subscribers (a panicking subscriber is logged and
//     skipped), and a covered scope's event handling and handover (the event is skipped).
//   - The goroutine that runs the informer recovers a panic and reports it through Handle.Err,
//     which covers the queue processing and store updates on that goroutine; so does the
//     goroutine that waits for a handover between a covered scope's informers.
//   - Not recovered: a panic inside client-go's own reflector or event-handler goroutines, or in
//     a Source's watch.Interface producer goroutine. With ReallyCrash true they end the process.
//     Whether the app sets utilruntime.ReallyCrash = false at start-up (then client-go logs and
//     carries on; an informer that stops that way is reported through Handle.Err) is a process-wide
//     decision for cmd/, not for this package.
package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	toolscache "k8s.io/client-go/tools/cache"
)

// DefaultWarmPeriod is how long an informer keeps running after its last handle is released.
const DefaultWarmPeriod = 5 * time.Minute

// DefaultHandoverTimeout is how long an informer that is about to stop keeps serving the
// label-selected scopes it covers while their own informers list (cover.go).
const DefaultHandoverTimeout = time.Minute

// closeTimeout bounds how long Close waits for the informers' goroutines to end.
const closeTimeout = 5 * time.Second

// Source creates the list-watchers informers run on: client-go's dynamic client for a real
// cluster (DynamicSource), a synthetic generator in demo mode, an in-memory fake in tests.
//
// A ListerWatcher that does not support WatchList semantics (streaming initial lists) must
// implement `IsWatchListSemanticsUnSupported() bool` returning true; client-go 0.37's reflector
// otherwise expects a WatchList-capable server.
//
// A Source must honour the whole scope: the namespace, the label selector, and MetadataOnly. A
// metadata-only scope must not transfer the objects' content at all (the real cluster's source asks
// the API server for PartialObjectMetadata), so that a kind that needs only metadata never holds
// more. In-memory sources may apply FilterScope to what they hold.
type Source interface {
	ListerWatcher(scope Scope) toolscache.ListerWatcher
}

// Scope is what one informer watches: a resource, in one namespace or (empty) in all, optionally
// narrowed by a label selector, and either the full objects or their metadata only. Each distinct
// scope has its own handles; Acquire normalises the label selector, so "a=1,b=2" and "b=2,a=1"
// share one. An unselected scope is one informer and one watch; a label-selected scope is one
// too, unless the unselected scope of the same resource, namespace and metadata flag runs, which
// then serves it (cover.go).
type Scope struct {
	GVR       schema.GroupVersionResource `json:"gvr"`
	Namespace string                      `json:"namespace,omitempty"`
	// LabelSelector narrows the list and watch on the server: "owner=helm".
	LabelSelector string `json:"labelSelector,omitempty"`
	// MetadataOnly holds only apiVersion, kind and metadata of each object, as the API server's
	// PartialObjectMetadata: no spec, status or data. See MetadataObject for the shape.
	MetadataOnly bool `json:"metadataOnly,omitempty"`
	// Trim names a trim (RegisterTrim, trim.go) that keeps only the parts of each object an
	// integration reads; the scope is an informer of its own beside the kind's whole objects.
	Trim string `json:"trim,omitempty"`
}

// String names the scope the way kubectl names resources: "pods", "deployments.apps",
// "pods in team-a", "secrets (metadata)", "secrets with owner=helm".
func (s Scope) String() string {
	name := s.GVR.Resource
	if s.GVR.Group != "" {
		name += "." + s.GVR.Group
	}
	if s.MetadataOnly {
		name += " (metadata)"
	}
	if s.Trim != "" {
		name += " (" + s.Trim + ")"
	}
	if s.LabelSelector != "" {
		name += " with " + s.LabelSelector
	}
	if s.Namespace != "" {
		name += " in " + s.Namespace
	}
	return name
}

// normalize returns the scope with its label selector in canonical form; a selector that does not
// parse is kept as it is, and the source's list fails with the server's error.
func (s Scope) normalize() Scope {
	if s.LabelSelector == "" {
		return s
	}
	if sel, err := labels.Parse(s.LabelSelector); err == nil {
		s.LabelSelector = sel.String()
	}
	return s
}

// Options configure a Cache.
type Options struct {
	// WarmPeriod is how long an informer stays running after its last handle is released.
	// Zero means DefaultWarmPeriod.
	WarmPeriod time.Duration
	// HandoverTimeout bounds how long an informer whose warm period ended keeps serving the
	// label-selected scopes it covers while their own informers list; after it they switch
	// anyway. Zero means DefaultHandoverTimeout.
	HandoverTimeout time.Duration
	// Logger receives informer errors; nil means slog.Default().
	Logger *slog.Logger
}

// InformerStats describe one scope for the diagnostics view: one per scope that a handle holds or
// that is warm.
type InformerStats struct {
	Scope     Scope     `json:"scope"`
	Objects   int       `json:"objects"`
	Handles   int       `json:"handles"`
	Synced    bool      `json:"synced"`
	Events    int64     `json:"events"`              // events delivered since start, initial list included
	WarmUntil time.Time `json:"warmUntil,omitempty"` // set while no handle holds the informer
	Error     string    `json:"error,omitempty"`
	// CoveredBy names the unselected scope whose informer serves this label-selected scope; empty
	// when the scope has its own informer (and watch).
	CoveredBy string `json:"coveredBy,omitempty"`
	// LoadedIn is how long the scope's initial list took, from its informer's start (0 while it
	// loads, and for a covered scope); Initial how the list came: InitialStream, InitialList or
	// InitialFallback (diag.go).
	LoadedIn time.Duration `json:"loadedIn,omitempty"`
	Initial  string        `json:"initial,omitempty"`
	// Bytes estimates the memory the scope's objects take in the cache (size.go), filled by
	// StatsWithMemory only; a covered scope's objects are its cover's, 0 here. Sized is how many
	// objects' sizes the estimate knows; the others (beyond one read's walk) count at their mean.
	Bytes int64 `json:"bytes,omitempty"`
	Sized int   `json:"sized,omitempty"`
}

// Cache holds the informers of one cluster. It is safe for concurrent use.
type Cache struct {
	src  Source
	opts Options
	log  *slog.Logger

	ctx  context.Context // ends with Close: handovers stop waiting
	stop context.CancelFunc

	mu        sync.Mutex
	informers map[Scope]*entry     // every scope a handle holds or that is warm
	runners   map[*runner]struct{} // every informer running, those of entries and those still serving a handover
	closed    bool
	entries   uint64 // entries made so far: each entry's seq (peek.go)

	refused   atomic.Pointer[func(Scope)]                                  // told of every refused list or watch (OnRefused, nsscope.go)
	streamErr atomic.Pointer[func(Scope, error)]                           // told of every list or watch that broke (OnStreamError, streamerr.go)
	diag      atomic.Pointer[func(Diagnostic)]                             // told of loads and fallbacks (OnDiagnostic, diag.go)
	label     atomic.Pointer[func(context.Context, Scope) context.Context] // names the requests (LabelRequests, diag.go)
}

// New returns a cache over src. No informer runs until the first Acquire.
func New(src Source, opts Options) *Cache {
	if opts.WarmPeriod <= 0 {
		opts.WarmPeriod = DefaultWarmPeriod
	}
	if opts.HandoverTimeout <= 0 {
		opts.HandoverTimeout = DefaultHandoverTimeout
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	ctx, stop := context.WithCancel(context.Background())
	return &Cache{
		src: src, opts: opts, log: opts.Logger, ctx: ctx, stop: stop,
		informers: map[Scope]*entry{}, runners: map[*runner]struct{}{},
	}
}

// entry is one scope that handles hold, or that is warm: its handles' bookkeeping and their
// subscriptions. An unselected scope (or one whose selector does not parse) reads from its own
// informer, run; a label-selected scope reads from its view, which an informer of its own or its
// cover's feeds (cover.go).
type entry struct {
	scope Scope
	log   *slog.Logger
	run   *runner // the informer the handles read from; nil for a label-selected scope
	view  *view   // a label-selected scope's objects; nil otherwise
	seq   uint64  // unique among the cache's entries: a scope started again is another entry (peek.go)

	refs      int         // guarded by Cache.mu
	warmTimer *time.Timer // guarded by Cache.mu
	warmUntil time.Time   // guarded by Cache.mu
	warmGen   uint64      // guarded by Cache.mu; tells a stale timer from the current one

	events atomic.Int64
	sizes  sizeMemo // the objects' sizes for StatsWithMemory (size.go)

	// subs is replaced, never mutated (copy on write): delivery reads it without a lock, so a
	// subscriber may subscribe or cancel from inside its callback.
	subMu sync.Mutex // serialises writers of subs
	subs  atomic.Pointer[[]*subscription]
}

type subscription struct {
	fn     func(Event)
	active atomic.Bool
}

// runner is one client-go informer and its list and watch: an unselected scope's, or a
// label-selected scope's own while no cover serves it.
type runner struct {
	scope  Scope
	inf    toolscache.SharedIndexInformer
	reg    toolscache.ResourceEventHandlerRegistration // the entry's handler; nil without one, or if it failed
	cancel context.CancelFunc
	done   chan struct{} // closed when the informer's goroutine has returned
	log    *slog.Logger
	c      *Cache

	// owner is the unselected scope's entry the runner serves; nil for a label-selected scope's own
	// informer. A refused list or watch ends it while it is only warm (nsscope.go).
	owner atomic.Pointer[entry]

	// users counts what keeps the informer running: the unselected scope's entry, and every view
	// reading from it (or waiting to). Guarded by Cache.mu; the informer stops at zero.
	users int

	errMu   sync.Mutex
	lastErr error

	// Connection Diagnostics (diag.go): when the informer started, how long its initial list took
	// (0 while it loads), and how the list came.
	started   time.Time
	loadedIn  atomic.Int64
	diagMu    sync.Mutex
	initial   string // InitialStream, InitialList or InitialFallback; "" before the first
	streaming bool   // a streamed initial list was asked for and no list followed yet
	streamErr error  // that stream's error
}

// Acquire returns a handle on the objects of scope, starting an informer if none serves it. The
// caller must Release the handle when it stops showing or using the data. After Close, Acquire
// returns a handle on an empty, never-syncing store.
func (c *Cache) Acquire(scope Scope) *Handle {
	scope = scope.normalize()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return &Handle{scope: scope}
	}
	e := c.informers[scope]
	if e == nil {
		e = c.newEntry(scope)
	}
	e.refs++
	if e.warmTimer != nil {
		e.warmTimer.Stop()
		e.warmTimer = nil
		e.warmUntil = time.Time{}
	}
	e.warmGen++ // a timer that already fired and waits for c.mu must not expire the entry
	return &Handle{scope: scope, e: e, c: c}
}

// newEntry makes and registers the entry of a scope nobody holds: a label-selected scope reads
// through its cover when that runs and through an informer of its own otherwise; an unselected
// scope starts its informer, which then serves the label-selected scopes it covers. Called with
// c.mu held.
func (c *Cache) newEntry(scope Scope) *entry {
	c.entries++
	e := &entry{scope: scope, log: c.log.With("scope", scope.String()), seq: c.entries}
	e.subs.Store(&[]*subscription{})
	c.informers[scope] = e
	if sel, ok := selectorOf(scope); ok {
		e.view = newView(e, sel)
		if cover := c.informers[coverOf(scope)]; cover != nil && cover.run != nil {
			c.attach(e, cover.run, false)
		} else {
			c.attachOwn(e, false)
		}
		return e
	}
	e.run = c.startRunner(scope, e.dispatch)
	e.run.owner.Store(e)
	e.run.users++
	c.coverStarted(e)
	return e
}

// startRunner creates and runs an informer for scope; dispatch, when given, receives its events.
// Called with c.mu held.
func (c *Cache) startRunner(scope Scope, dispatch func(EventType, any)) *runner {
	r := &runner{scope: scope, done: make(chan struct{}), log: c.log.With("scope", scope.String()), c: c, started: time.Now()}
	lw := &trackedLW{r: r, inner: c.listerWatcher(r, scope)}
	inf := toolscache.NewSharedIndexInformerWithOptions(lw, &unstructured.Unstructured{}, toolscache.SharedIndexInformerOptions{
		ResyncPeriod:      0, // no periodic resyncs: only watch events after the initial list
		Indexers:          toolscache.Indexers{toolscache.NamespaceIndex: toolscache.MetaNamespaceIndexFunc},
		ObjectDescription: scope.String(),
	})
	r.inf = inf
	if err := inf.SetTransform(r.stripManagedFields); err != nil {
		r.log.Warn("cache: transform not installed", "err", err)
	}
	if err := inf.SetWatchErrorHandlerWithContext(func(_ context.Context, _ *toolscache.Reflector, err error) {
		r.watchError(err)
	}); err != nil {
		r.log.Warn("cache: watch error handler not installed", "err", err)
	}
	if dispatch != nil {
		reg, err := inf.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj any) { dispatch(Added, obj) },
			UpdateFunc: func(_, obj any) { dispatch(Updated, obj) },
			DeleteFunc: func(obj any) { dispatch(Deleted, obj) },
		})
		if err != nil {
			r.log.Warn("cache: event handler not installed", "err", err)
		}
		r.reg = reg
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go func() {
		defer close(r.done)
		defer func() {
			if rec := recover(); rec != nil {
				r.setErr(fmt.Errorf("the informer for %s stopped: internal error: %v", scope, rec))
				r.log.Error("cache: informer panicked", "panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
			}
		}()
		inf.RunWithContext(ctx)
		if ctx.Err() == nil {
			// Run returned on its own: client-go swallowed a panic (utilruntime.ReallyCrash is
			// false) or refused to run twice. The store is frozen from here on.
			r.setErr(fmt.Errorf("the informer for %s stopped unexpectedly", scope))
			r.log.Error("cache: informer stopped unexpectedly")
		}
	}()
	go r.timeLoad(ctx)
	c.runners[r] = struct{}{}
	return r
}

// unuse drops one use of a runner; the last one stops its informer. Called with c.mu held.
func (c *Cache) unuse(r *runner) {
	r.users--
	if r.users > 0 {
		return
	}
	r.cancel()
	delete(c.runners, r)
}

// listerWatcher asks the source for a list-watcher, turning a panic or a nil result into a
// list-watcher that fails, so the error shows on the handle instead of ending the process.
func (c *Cache) listerWatcher(r *runner, scope Scope) (lw toolscache.ListerWatcher) {
	defer func() {
		if rec := recover(); rec != nil {
			r.log.Error("cache: source panicked", "panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
			lw = failingLW{err: fmt.Errorf("cannot watch %s: internal error: %v", scope, rec)}
		}
	}()
	lw = c.src.ListerWatcher(scope)
	if lw == nil {
		lw = failingLW{err: fmt.Errorf("cannot watch %s: the source has no list-watcher for it", scope)}
	}
	return lw
}

// release drops one reference; the last one starts the warm period.
func (c *Cache) release(e *entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e.refs--
	if e.refs > 0 || c.closed {
		return
	}
	if e.refusedLocked() { // a refused scope is not kept warm (nsscope.go)
		c.retire(e)
		return
	}
	e.warmGen++
	gen := e.warmGen
	e.warmUntil = time.Now().Add(c.opts.WarmPeriod)
	e.warmTimer = time.AfterFunc(c.opts.WarmPeriod, func() { c.expire(e, gen) })
}

// expire ends a scope whose warm period ended without a new handle.
func (c *Cache) expire(e *entry, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e.refs > 0 || e.warmGen != gen || c.informers[e.scope] != e {
		return
	}
	c.retire(e)
}

// retire removes an entry: an unselected scope hands the scopes it covers over to informers of
// their own and stops its informer once none needs it; a label-selected scope stops reading. Called
// with c.mu held.
func (c *Cache) retire(e *entry) {
	delete(c.informers, e.scope)
	if e.warmTimer != nil {
		e.warmTimer.Stop()
		e.warmTimer = nil
	}
	if e.view != nil {
		c.stopView(e)
		return
	}
	c.coverStopping(e)
	c.unuse(e.run)
}

// Stats returns one entry per scope that a handle holds or that is warm.
func (c *Cache) Stats() []InformerStats { return c.stats(false) }

// StatsWithMemory is Stats with each scope's memory estimated (size.go): the sizes of its objects,
// remembered by version, walking at most 1,000 that changed, outside every lock of the cache (the
// store's read lock only while it copies its list of objects).
func (c *Cache) StatsWithMemory() []InformerStats { return c.stats(true) }

func (c *Cache) stats(memory bool) []InformerStats {
	type row struct {
		e         *entry
		refs      int
		warmUntil time.Time
	}
	c.mu.Lock()
	rows := make([]row, 0, len(c.informers))
	for _, e := range c.informers {
		rows = append(rows, row{e: e, refs: e.refs, warmUntil: e.warmUntil})
	}
	c.mu.Unlock()

	// Counting the store's keys walks the store; do it without holding c.mu.
	out := make([]InformerStats, 0, len(rows))
	for _, r := range rows {
		st := InformerStats{
			Scope:     r.e.scope,
			Handles:   r.refs,
			Events:    r.e.events.Load(),
			WarmUntil: r.warmUntil,
		}
		var err error
		if v := r.e.view; v != nil {
			st.Objects, st.Synced, st.CoveredBy, err = v.stats()
			if memory && st.CoveredBy == "" {
				st.Bytes, st.Sized = r.e.sizes.estimate(v.list())
			}
		} else {
			st.Objects, st.Synced, err = len(r.e.run.inf.GetStore().ListKeys()), r.e.run.synced(), r.e.run.err()
			st.LoadedIn, st.Initial = r.e.run.loadStats()
			if memory {
				st.Bytes, st.Sized = r.e.sizes.estimate(storeObjects(r.e.run.inf.GetStore().List()))
			}
		}
		if err != nil {
			st.Error = err.Error()
		}
		out = append(out, st)
	}
	return out
}

// Close stops every informer and waits (up to a few seconds) for their goroutines to end, so no
// request is in flight when the caller closes the connection. Handles keep the data they had.
func (c *Cache) Close() {
	c.mu.Lock()
	c.closed = true
	c.stop()
	for scope, e := range c.informers {
		if e.warmTimer != nil {
			e.warmTimer.Stop()
			e.warmTimer = nil
		}
		if e.view != nil {
			e.view.close()
		}
		delete(c.informers, scope)
	}
	stopped := make([]*runner, 0, len(c.runners))
	for r := range c.runners {
		r.cancel()
		stopped = append(stopped, r)
		delete(c.runners, r)
	}
	c.mu.Unlock()

	// Wait without c.mu: a subscriber still running on an informer goroutine may call into the
	// cache.
	deadline := time.NewTimer(closeTimeout)
	defer deadline.Stop()
	for _, r := range stopped {
		select {
		case <-r.done:
		case <-deadline.C:
			c.log.Warn("cache: informers did not stop in time", "timeout", closeTimeout)
			return
		}
	}
}

// EventType says what happened to an object.
type EventType string

// Event types.
const (
	Added   EventType = "added"
	Updated EventType = "updated"
	Deleted EventType = "deleted"
)

// Event is one change seen by an informer. Object is the new state (for Deleted, the last known
// state; for an object whose labels left a label-selected scope, the state that left it). Objects
// are shared with the store and must not be modified.
type Event struct {
	Type   EventType
	Object *unstructured.Unstructured
}

// dispatch runs on the informer's event-handler goroutine, one event at a time and in order.
func (e *entry) dispatch(typ EventType, obj any) {
	defer func() {
		// Nothing in here should panic; if it does, it must not reach client-go's HandleCrash.
		if r := recover(); r != nil {
			e.log.Error("cache: event dispatch panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	u := asObject(obj)
	if u == nil {
		e.events.Add(1)
		return
	}
	e.deliver(Event{Type: typ, Object: u})
}

// asObject returns the unstructured object an informer handed over, unwrapping the final state of
// a delete the informer missed; nil for anything else.
func asObject(obj any) *unstructured.Unstructured {
	if d, ok := obj.(toolscache.DeletedFinalStateUnknown); ok {
		obj = d.Obj
	}
	u, _ := obj.(*unstructured.Unstructured)
	return u
}

// deliver hands one event to every active subscriber and counts it.
func (e *entry) deliver(ev Event) {
	e.events.Add(1)
	for _, s := range *e.subs.Load() {
		if s.active.Load() {
			e.callSafely(s.fn, ev)
		}
	}
}

// callSafely runs a subscriber; a panicking subscriber must not stop the informer.
func (e *entry) callSafely(fn func(Event), ev Event) {
	defer func() {
		if r := recover(); r != nil {
			e.log.Error("cache: subscriber panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	fn(ev)
}

func (e *entry) subscribe(fn func(Event)) func() {
	s := &subscription{fn: fn}
	s.active.Store(true)
	e.subMu.Lock()
	old := *e.subs.Load()
	next := make([]*subscription, 0, len(old)+1)
	next = append(next, old...)
	next = append(next, s)
	e.subs.Store(&next)
	e.subMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.active.Store(false)
			e.subMu.Lock()
			old := *e.subs.Load()
			next := make([]*subscription, 0, len(old))
			for _, x := range old {
				if x != s {
					next = append(next, x)
				}
			}
			e.subs.Store(&next)
			e.subMu.Unlock()
		})
	}
}

func (r *runner) synced() bool {
	return r.inf.HasSynced() && (r.reg == nil || r.reg.HasSynced())
}

func (r *runner) setErr(err error) {
	r.errMu.Lock()
	r.lastErr = err
	r.errMu.Unlock()
}

func (r *runner) clearErr() {
	r.errMu.Lock()
	r.lastErr = nil
	r.errMu.Unlock()
}

func (r *runner) err() error {
	r.errMu.Lock()
	defer r.errMu.Unlock()
	return r.lastErr
}

// watchError receives the errors that end a list-and-watch cycle (client-go retries after it).
// The routine ones (a watch that expired, a closed stream) are not errors to show.
func (r *runner) watchError(err error) {
	if err == nil || apierrors.IsResourceExpired(err) || apierrors.IsGone(err) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return
	}
	r.failed(err)
	r.log.Warn("cache: list or watch failed; retrying", "err", err)
	if !isRefused(err) {
		r.tellStreamError(err) // the engine checks the connection at once (D-58)
	}
}

// stripManagedFields is the informer transform: managedFields are large and only the editor's
// "managed fields" toggle needs them, which fetches them on demand. For core Secrets in every
// scope it also drops the last-applied-configuration annotation, which kubectl apply fills with
// the Secret's whole manifest, data included: it would repeat every value in memory a second time,
// and in the drawer's annotations, where agents read it (H-8); no scope needs it (Cogpit never
// writes Secrets). It runs on the reflector's goroutine; if it panicked, the object is kept
// unchanged rather than lost.
func (r *runner) stripManagedFields(obj any) (out any, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			r.log.Error("cache: transform panicked", "panic", fmt.Sprint(rec))
			out, err = obj, nil
		}
	}()
	if u, ok := obj.(*unstructured.Unstructured); ok {
		unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
		if r.scope.GVR.Group == "" && r.scope.GVR.Resource == "secrets" {
			unstructured.RemoveNestedField(u.Object, "metadata", "annotations", LastAppliedAnnotation)
		}
		if trim := trimOf(r.scope.Trim); trim != nil { // a trimmed scope keeps what its reader needs (trim.go)
			trim(u)
		}
	}
	return obj, nil
}

// LastAppliedAnnotation is where kubectl apply keeps the manifest it applied; on a Secret it holds
// the Secret's values, so the cache drops it from Secrets.
const LastAppliedAnnotation = "kubectl.kubernetes.io/last-applied-configuration"

// trackedLW wraps a Source's list-watcher: it records list and watch errors on the runner and
// clears them when a list or a watch succeeds, turns a panic in the source into an error, and
// passes the WatchList capability through to the reflector.
type trackedLW struct {
	r     *runner
	inner toolscache.ListerWatcher
}

var _ toolscache.ListerWatcherWithContext = (*trackedLW)(nil)

// IsWatchListSemanticsUnSupported reports what the wrapped list-watcher reports (false when it
// does not say), so the reflector streams initial lists exactly when the source supports it.
func (t *trackedLW) IsWatchListSemanticsUnSupported() bool {
	u, ok := t.inner.(interface{ IsWatchListSemanticsUnSupported() bool })
	return ok && u.IsWatchListSemanticsUnSupported()
}

func (t *trackedLW) List(opts metav1.ListOptions) (runtime.Object, error) {
	return t.ListWithContext(context.Background(), opts)
}

func (t *trackedLW) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	return t.WatchWithContext(context.Background(), opts)
}

func (t *trackedLW) ListWithContext(ctx context.Context, opts metav1.ListOptions) (obj runtime.Object, err error) {
	defer t.recoverInto(&err, "list")
	ctx = t.r.labelled(ctx)
	initial := t.r.listAsked(ctx, opts)
	obj, err = toolscache.ToListerWithContext(t.inner).ListWithContext(ctx, opts)
	t.observe(ctx, err)
	if err == nil {
		t.r.listed(initial)
	}
	return obj, err
}

func (t *trackedLW) WatchWithContext(ctx context.Context, opts metav1.ListOptions) (w watch.Interface, err error) {
	defer t.recoverInto(&err, "watch")
	ctx = t.r.labelled(ctx)
	w, err = toolscache.ToWatcherWithContext(t.inner).WatchWithContext(ctx, opts)
	if err == nil && w == nil {
		err = errors.New("the source returned no watch")
	}
	t.r.watchAsked(ctx, opts, err)
	t.observe(ctx, err)
	return w, err
}

// observe records the outcome of a list or a watch request. A request cut short because the
// informer is stopping says nothing about the cluster.
func (t *trackedLW) observe(ctx context.Context, err error) {
	switch {
	case ctx.Err() != nil:
	case err != nil:
		t.r.failed(err)
	default:
		t.r.clearErr()
	}
}

func (t *trackedLW) recoverInto(err *error, op string) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("cannot %s %s: internal error: %v", op, t.r.scope, r)
		t.r.setErr(*err)
		t.r.log.Error("cache: source panicked", "op", op, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
	}
}

// failingLW stands in for a list-watcher the source could not provide.
type failingLW struct{ err error }

func (f failingLW) List(metav1.ListOptions) (runtime.Object, error)   { return nil, f.err }
func (f failingLW) Watch(metav1.ListOptions) (watch.Interface, error) { return nil, f.err }
func (f failingLW) IsWatchListSemanticsUnSupported() bool             { return true }

// Handle is one consumer's claim on a scope. Its reads stay valid after Release, but the data
// stops changing once the scope has expired.
type Handle struct {
	scope    Scope
	e        *entry
	c        *Cache
	parts    []*Handle // a joined handle's parts (join.go); nil for a handle on one scope
	released atomic.Bool
	set      *nsSet // a scope read in several namespaces, one informer each (nsscope.go); nil otherwise
}

// Scope returns the handle's scope.
func (h *Handle) Scope() Scope { return h.scope }

// HasSynced reports whether the informer has finished its initial list and delivered it to
// subscribers: a subscriber that subscribes after HasSynced sees changes only, never the initial
// list's late additions. (The store is filled before client-go hands the initial list to event
// handlers, so the store's own sync comes first.)
func (h *Handle) HasSynced() bool {
	switch {
	case h.parts != nil:
		return h.joinedSynced()
	case h.set != nil:
		return h.set.synced()
	case h.e == nil:
		return false
	case h.e.view != nil:
		return h.e.view.synced()
	}
	return h.e.run.synced()
}

// WaitForSync blocks until the informer has synced (HasSynced) or ctx ends; it reports whether
// it synced.
func (h *Handle) WaitForSync(ctx context.Context) bool {
	if h.e == nil && h.parts == nil && h.set == nil {
		<-ctx.Done()
		return false
	}
	if h.HasSynced() {
		return true
	}
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return h.HasSynced()
		case <-t.C:
			if h.HasSynced() {
				return true
			}
		}
	}
}

// List returns every object of the scope. The objects are shared and must not be modified.
func (h *Handle) List() []*unstructured.Unstructured {
	switch {
	case h.parts != nil:
		return h.joinedList("", true)
	case h.set != nil:
		return h.set.list()
	case h.e == nil:
		return nil
	case h.e.view != nil:
		return h.e.view.list()
	}
	return toUnstructured(h.e.run.inf.GetStore().List())
}

// ListNamespace returns the objects in one namespace, from the store's namespace index; for an
// informer scoped to one namespace it equals List. The objects are shared and must not be
// modified.
func (h *Handle) ListNamespace(namespace string) []*unstructured.Unstructured {
	switch {
	case h.parts != nil:
		return h.joinedList(namespace, false)
	case h.set != nil:
		return h.set.listNamespace(namespace)
	case h.e == nil:
		return nil
	case h.e.view != nil:
		return h.e.view.listNamespace(namespace)
	}
	items, err := h.e.run.inf.GetIndexer().ByIndex(toolscache.NamespaceIndex, namespace)
	if err != nil {
		return nil
	}
	return toUnstructured(items)
}

func toUnstructured(items []any) []*unstructured.Unstructured {
	out := make([]*unstructured.Unstructured, 0, len(items))
	for _, it := range items {
		if u, ok := it.(*unstructured.Unstructured); ok {
			out = append(out, u)
		}
	}
	return out
}

// Get returns one object by namespace and name.
func (h *Handle) Get(namespace, name string) (*unstructured.Unstructured, bool) {
	if h.parts != nil {
		return h.joinedGet(namespace, name)
	}
	if h.set != nil {
		return h.set.get(namespace, name)
	}
	if h.e == nil {
		return nil, false
	}
	key := objectKey(namespace, name)
	if h.e.view != nil {
		return h.e.view.get(key)
	}
	it, ok, err := h.e.run.inf.GetStore().GetByKey(key)
	if err != nil || !ok {
		return nil, false
	}
	u, ok := it.(*unstructured.Unstructured)
	return u, ok
}

// objectKey is the store key of an object: "namespace/name", or the name of a cluster-scoped one.
func objectKey(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

// Subscribe calls fn for every change after it returns; subscribe before reading List to miss
// nothing. fn runs on an informer's event goroutine, one event at a time in order, and must not
// block. The returned function removes the subscription; it is idempotent and may be called from
// inside fn. A delivery already running on the event goroutine may finish after it returns.
func (h *Handle) Subscribe(fn func(Event)) (cancel func()) {
	if h.parts != nil && fn != nil {
		return h.joinedSubscribe(fn)
	}
	if h.set != nil && fn != nil {
		return h.set.subscribe(fn)
	}
	if h.e == nil || fn == nil {
		return func() {}
	}
	return h.e.subscribe(fn)
}

// Err returns the informer's last list or watch error, or nil (a forbidden kind, a lost
// connection). It clears when the next list or watch succeeds. Errors are states the UI shows,
// not failures.
func (h *Handle) Err() error {
	switch {
	case h.parts != nil:
		return h.joinedErr()
	case h.set != nil:
		return h.set.err()
	case h.e == nil:
		return nil
	case h.e.view != nil:
		return h.e.view.err()
	}
	return h.e.run.err()
}

// Release gives the handle back. It is idempotent. After the last release the scope stays warm
// for the warm period, then stops.
func (h *Handle) Release() {
	if h.parts != nil {
		h.joinedRelease()
		return
	}
	if (h.e == nil && h.set == nil) || !h.released.CompareAndSwap(false, true) {
		return
	}
	if h.set != nil {
		h.set.release()
		return
	}
	h.c.release(h.e)
}
