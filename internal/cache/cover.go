package cache

import (
	"fmt"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/resourceversion"
	toolscache "k8s.io/client-go/tools/cache"
)

// Covering scopes (architecture §5). A label-selected scope is a subset of the unselected scope of
// the same resource, namespace and metadata flag, its cover. While the cover runs (a handle holds
// it, or it is warm), the selected scope reads from the cover's informer, filtered by its selector,
// and starts no watch of its own; otherwise it runs an informer of its own, which sends the
// selector to the server.
//
// A selected scope's handles read its view: the objects its subscribers have been told about,
// kept by an event handler on whichever informer serves it (its source). Switching sources is a
// handover: the new source's handler first fills a staging copy, and once it has synced the view
// moves onto it, sending its subscribers the difference as events; the old source's events are
// ignored from then on. Both directions:
//
//   - A cover starts while the selected scope runs its own informer: the scope moves onto the
//     cover once the cover has synced, and its own informer stops.
//   - A cover's warm period ends while selected scopes read from it: a covered scope never keeps
//     its cover alive. A scope with open handles starts its own informer, the cover's informer
//     keeps running until that one has synced (at most HandoverTimeout), then stops; a scope that
//     is only warm ends with its cover.
//
// Events stay exact across a handover where resourceVersions are comparable (KEP-5504: well-formed
// integers, as every Kubernetes API server's built-in resources have): for each object the view
// never sends an event for a version older than one it sent, never the same version twice, and
// never brings back an object it has sent as deleted unless a newer version of it arrives. Where
// they are not comparable, a version that differs counts as newer. An object whose labels leave
// the selection arrives as a delete, with the state that left it.

// tombstones bounds how many deleted keys a view remembers with the version that removed them, so
// that a source still behind the removal cannot bring them back.
const tombstones = 1024

// handoverPoll is how often a handover checks whether its new source has synced.
const handoverPoll = 10 * time.Millisecond

// selectorOf returns a scope's label selector if it has one that parses: such a scope can be
// covered. A selector that does not parse is left to the server, which refuses it.
func selectorOf(scope Scope) (labels.Selector, bool) {
	if scope.LabelSelector == "" {
		return nil, false
	}
	sel, err := labels.Parse(scope.LabelSelector)
	if err != nil {
		return nil, false
	}
	return sel, true
}

// coverOf returns the scope that covers a label-selected scope: the same resource, namespace and
// metadata flag, without the selector.
func coverOf(scope Scope) Scope {
	scope.LabelSelector = ""
	return scope
}

// view is what a label-selected scope's handles read: the objects its subscribers have been told
// about, by store key, kept current by the handler on its source.
type view struct {
	e     *entry
	sel   labels.Selector
	cover Scope

	// dmu serialises the deliveries to the entry's subscribers, so that they arrive one at a time
	// and in order, whichever informer's goroutine delivers them. Lock order: dmu, then
	// Cache.mu, then mu; mu is never held while a subscriber runs.
	dmu sync.Mutex
	mu  sync.Mutex

	seen  map[string]*unstructured.Unstructured            // guarded by mu
	byNS  map[string]map[string]*unstructured.Unstructured // guarded by mu: seen by namespace
	tombs map[string]tomb                                  // guarded by mu: removed keys and the version that removed them
	order []tomb                                           // guarded by mu: tombs, oldest first (some since replaced)
	seq   uint64                                           // guarded by mu: numbers the tombs
	cur   *source                                          // guarded by mu: the source the view follows
	next  *source                                          // guarded by mu: the source a handover moves onto
	fail  error                                            // guarded by mu: why the first source could not be read
	done  bool                                             // guarded by mu: the scope ended

	own *runner // guarded by Cache.mu: the scope's own informer while it has one
}

// tomb remembers a key the view removed and the version that removed it.
type tomb struct {
	key string
	rv  string
	seq uint64
}

// source is one informer a view reads from, through its own event handler.
type source struct {
	v   *view
	run *runner

	reg     toolscache.ResourceEventHandlerRegistration // guarded by v.mu
	staging map[string]*unstructured.Unstructured       // guarded by v.mu: what the source holds while a handover waits for it
	pos     string                                      // guarded by v.mu: the newest resourceVersion its handler delivered while staging

	quit chan struct{} // closed when the view drops the source
	once sync.Once
}

func (s *source) close() { s.once.Do(func() { close(s.quit) }) }

func newView(e *entry, sel labels.Selector) *view {
	return &view{
		e: e, sel: sel, cover: coverOf(e.scope),
		seen: map[string]*unstructured.Unstructured{}, byNS: map[string]map[string]*unstructured.Unstructured{},
		tombs: map[string]tomb{},
	}
}

// attach makes r the view's source: at once if the view has none yet, otherwise through a
// handover, which a pending handover to another informer gives way to. bounded limits how long
// the handover waits for r (HandoverTimeout): set when the view's current source is an informer
// that is about to stop. Called with c.mu held.
func (c *Cache) attach(e *entry, r *runner, bounded bool) {
	v := e.view
	v.mu.Lock()
	switch {
	case v.done:
		v.mu.Unlock()
		return
	case v.next != nil && v.next.run == r:
		v.mu.Unlock()
		return
	case v.cur != nil && v.cur.run == r:
		// Back onto the source it follows: the pending handover is not needed.
		pending := v.next
		v.next = nil
		v.mu.Unlock()
		c.dropSource(pending)
		return
	}
	src := &source{v: v, run: r, quit: make(chan struct{})}
	pending := v.next
	live := v.cur == nil
	if live {
		v.cur, v.next, v.fail = src, nil, nil
	} else {
		src.staging = map[string]*unstructured.Unstructured{}
		v.next = src
	}
	v.mu.Unlock()
	r.users++
	c.dropSource(pending)

	reg, err := r.inf.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { src.on(Added, obj) },
		UpdateFunc: func(_, obj any) { src.on(Updated, obj) },
		DeleteFunc: func(obj any) { src.on(Deleted, obj) },
	})
	v.mu.Lock()
	if err != nil {
		if v.cur == src {
			v.cur, v.fail = nil, fmt.Errorf("cannot read %s: %w", e.scope, err)
		}
		if v.next == src {
			v.next = nil
		}
		v.mu.Unlock()
		e.log.Warn("cache: cannot read from an informer", "from", r.scope.String(), "err", err)
		src.close()
		c.unuse(r)
		return
	}
	src.reg = reg
	v.mu.Unlock()
	if !live {
		timeout := time.Duration(0)
		if bounded {
			timeout = c.opts.HandoverTimeout
		}
		go c.awaitHandover(src, timeout)
	}
}

// attachOwn makes the scope's own informer its source, starting it if needed. Called with c.mu
// held.
func (c *Cache) attachOwn(e *entry, bounded bool) {
	v := e.view
	r := v.own
	if r == nil {
		r = c.startRunner(e.scope, nil)
		v.own = r
	}
	c.attach(e, r, bounded)
	if r.users == 0 { // the view ended meanwhile
		c.stopOwn(v, r)
	}
}

// stopOwn stops a view's own informer that nothing reads from. Called with c.mu held.
func (c *Cache) stopOwn(v *view, r *runner) {
	r.cancel()
	delete(c.runners, r)
	if v.own == r {
		v.own = nil
	}
}

// dropSource ends a view's use of a source: its handler goes, and its informer stops if nothing
// else uses it. Called with c.mu held.
func (c *Cache) dropSource(src *source) {
	if src == nil {
		return
	}
	src.close()
	src.v.mu.Lock()
	reg := src.reg
	src.staging = nil
	src.v.mu.Unlock()
	if reg != nil {
		if err := src.run.inf.RemoveEventHandler(reg); err != nil {
			src.v.e.log.Debug("cache: removing a handler", "err", err)
		}
	}
	c.unuse(src.run)
	if src.run.users == 0 && src.v.own == src.run {
		src.v.own = nil
	}
}

// coverStarted moves the label-selected scopes an unselected scope covers onto its informer. A
// scope whose current source is an informer about to stop waits for the new one only as long as
// HandoverTimeout. Called with c.mu held.
func (c *Cache) coverStarted(ce *entry) {
	for _, e := range c.informers {
		if e.view == nil || e.view.cover != ce.scope {
			continue
		}
		e.view.mu.Lock()
		bounded := e.view.cur != nil && e.view.cur.run != e.view.own
		e.view.mu.Unlock()
		c.attach(e, ce.run, bounded)
	}
}

// coverStopping hands the label-selected scopes that read from an unselected scope's informer
// over before it stops: a scope with open handles to an informer of its own, a scope that is only
// warm ends with its cover; a handover onto the stopping informer is abandoned. Called with c.mu
// held.
func (c *Cache) coverStopping(ce *entry) {
	for _, e := range c.informers {
		if e.view == nil || e.view.cover != ce.scope {
			continue
		}
		v := e.view
		v.mu.Lock()
		onCur := v.cur != nil && v.cur.run == ce.run
		onNext := v.next != nil && v.next.run == ce.run
		var pending *source
		if onNext {
			pending, v.next = v.next, nil
		}
		v.mu.Unlock()
		c.dropSource(pending)
		switch {
		case onCur && e.refs == 0:
			c.retire(e)
		case onCur:
			c.attachOwn(e, true)
		}
	}
}

// stopView ends a label-selected scope: its sources go, and so does its own informer. Handles keep
// what they read. Called with c.mu held.
func (c *Cache) stopView(e *entry) {
	v := e.view
	v.mu.Lock()
	v.done = true
	cur, next := v.cur, v.next
	v.next = nil
	v.mu.Unlock()
	c.dropSource(next)
	c.dropSource(cur)
	if v.own != nil && v.own.users == 0 {
		c.stopOwn(v, v.own)
	}
}

// close ends the view when the cache closes; the informers stop with the cache.
func (v *view) close() {
	v.mu.Lock()
	v.done = true
	cur, next := v.cur, v.next
	v.mu.Unlock()
	for _, s := range []*source{cur, next} {
		if s != nil {
			s.close()
		}
	}
}

// awaitHandover waits until a pending source has caught up with the view, or the timeout (if
// any) has passed, and moves the view onto it; then the old source goes. It ends early when the
// source is dropped or the cache closes.
func (c *Cache) awaitHandover(src *source, timeout time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			src.v.e.log.Error("cache: handover panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	t := time.NewTicker(handoverPoll)
	defer t.Stop()
	var deadline <-chan time.Time
	if timeout > 0 {
		dt := time.NewTimer(timeout)
		defer dt.Stop()
		deadline = dt.C
	}
	var (
		old *source
		ok  bool
	)
wait:
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-src.quit:
			return
		case <-t.C:
			// The check repeats under the view's locks: an event the old source delivers in
			// between can move the view ahead of the new source again.
			if !src.v.caughtUp(src) {
				continue
			}
			var retry bool
			if old, ok, retry = src.v.cutover(src, true); !retry {
				break wait
			}
		case <-deadline:
			src.v.e.log.Warn("cache: switching to an informer that has not caught up", "to", src.run.scope.String(), "timeout", timeout)
			old, ok, _ = src.v.cutover(src, false)
			break wait
		}
	}
	if !ok {
		return
	}
	func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.closed {
			c.dropSource(old)
		}
	}()
	// A source that took over before its first list (the wait timed out) left the view as it was;
	// once it has listed, what it does not hold is gone.
	for !src.v.listed(src) {
		select {
		case <-c.ctx.Done():
			return
		case <-src.quit:
			return
		case <-t.C:
		}
	}
	src.v.settle(src)
}

// listed reports whether a source's handler has received its informer's initial list.
func (v *view) listed(src *source) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return src.reg != nil && src.reg.HasSynced()
}

// settle tells the subscribers which objects the view's source does not hold, once it has
// listed: after a handover that timed out before the source's first list, the view kept what it
// had told them instead of reporting every object deleted and adding it again (R-7 of the fourth
// run's review). An object the source holds but its handler has not delivered yet stays; its
// event follows. Idempotent: after an exact handover it finds nothing.
func (v *view) settle(src *source) {
	v.dmu.Lock()
	defer v.dmu.Unlock()
	var evs []Event
	func() {
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.done || v.cur != src {
			return
		}
		store := src.run.inf.GetStore()
		keys := make([]string, 0, len(v.seen))
		for k := range v.seen {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			o, ok, err := store.GetByKey(k)
			if u, _ := o.(*unstructured.Unstructured); err == nil && ok && u != nil && v.matches(u) {
				continue
			}
			was := v.seen[k]
			v.remove(k, "")
			evs = append(evs, Event{Type: Deleted, Object: was})
		}
	}()
	for _, ev := range evs {
		v.e.deliver(ev)
	}
}

// caughtUp reports whether a pending source can take over exactly: it has synced, and its
// handler has delivered a version at least as new as every object the view holds, so that it has
// seen everything the subscribers were told (a watch's events arrive in resourceVersion order; a
// relist's notifications do not, which diff allows for by asking the informer's store). Where
// resourceVersions are not comparable, having synced is enough.
func (v *view) caughtUp(src *source) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.caughtUpLocked(src)
}

// caughtUpLocked is caughtUp with v.mu held.
func (v *view) caughtUpLocked(src *source) bool {
	if src.reg == nil || !src.reg.HasSynced() || src.pos == "" && len(v.seen) > 0 {
		return false
	}
	for _, o := range v.seen {
		n, err := resourceversion.CompareResourceVersion(src.pos, o.GetResourceVersion())
		if err != nil {
			return true
		}
		if n < 0 {
			return false
		}
	}
	return true
}

// on handles one event of a source's informer: a pending source's go to its staging copy, the
// current source's to the view and its subscribers; a dropped source's are ignored.
func (s *source) on(typ EventType, obj any) {
	v := s.v
	defer func() {
		if r := recover(); r != nil {
			v.e.log.Error("cache: covered scope's event handling panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	_, unknown := obj.(toolscache.DeletedFinalStateUnknown)
	u := asObject(obj)
	if u == nil {
		return
	}
	v.dmu.Lock()
	defer v.dmu.Unlock()
	evs := func() []Event {
		v.mu.Lock()
		defer v.mu.Unlock()
		switch {
		case v.done:
		case v.next == s:
			s.stage(typ, u)
		case v.cur == s:
			return v.apply(typ, u, unknown)
		}
		return nil
	}()
	for _, ev := range evs {
		v.e.deliver(ev)
	}
}

// stage keeps a pending source's matching objects, and how far its events have come. Called with
// v.mu held.
func (s *source) stage(typ EventType, u *unstructured.Unstructured) {
	if rv := u.GetResourceVersion(); s.pos == "" || newerRV(rv, s.pos) {
		s.pos = rv
	}
	k := objectKey(u.GetNamespace(), u.GetName())
	if typ == Deleted || !s.v.matches(u) {
		delete(s.staging, k)
		return
	}
	s.staging[k] = u
}

// cutover moves the view onto a pending source: the subscribers get the difference between what
// they were told and what the source holds, and the old source's events are ignored from now on.
// exact asks for a source that has caught up (caughtUp), and retry says it has not; without exact
// the wait timed out and the source takes over as it is. It returns the old source, and false if
// the source is no longer pending.
func (v *view) cutover(src *source, exact bool) (old *source, ok, retry bool) {
	v.dmu.Lock()
	defer v.dmu.Unlock()
	var evs []Event
	func() {
		v.mu.Lock()
		defer v.mu.Unlock()
		switch {
		case v.done || v.next != src:
			return
		case exact && !v.caughtUpLocked(src):
			retry = true
			return
		}
		evs = v.diff(src, exact)
		old, ok = v.cur, true
		v.cur, v.next, src.staging = src, nil, nil
	}()
	for _, ev := range evs {
		v.e.deliver(ev)
	}
	return old, ok, retry
}

// diff brings the view to what a pending source holds and returns the events that tell the
// subscribers. When the source has caught up, an object neither its handler nor its informer's
// store holds is gone, and the version it has come to buries it. When its wait timed out instead, an object its informer holds but its
// handler has not delivered yet is left as the view has it, and a removal buries nothing; before
// the source's first list nothing is removed at all (settle does it once it has listed). Called
// with v.mu held.
func (v *view) diff(src *source, exact bool) []Event {
	store := src.run.inf.GetStore()
	listed := src.reg != nil && src.reg.HasSynced()
	inStore := func(k string) bool {
		o, ok, err := store.GetByKey(k)
		u, _ := o.(*unstructured.Unstructured)
		return err == nil && ok && u != nil && v.matches(u)
	}
	keys := make([]string, 0, len(v.seen)+len(src.staging))
	for k := range v.seen {
		keys = append(keys, k)
	}
	for k := range src.staging {
		if _, ok := v.seen[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var evs []Event
	for _, k := range keys {
		now, was := src.staging[k], v.seen[k]
		switch {
		case now == nil && exact:
			if inStore(k) {
				// Its informer holds it and its handler has not delivered it yet: a relist notifies
				// in key order, not resourceVersion order, so pos says nothing about it (R-8).
				continue
			}
			v.remove(k, src.pos)
			evs = append(evs, Event{Type: Deleted, Object: was})
		case now == nil:
			if listed && !inStore(k) {
				v.remove(k, "")
				evs = append(evs, Event{Type: Deleted, Object: was})
			}
		case was == nil:
			if !v.buried(k, now) {
				v.put(k, now)
				evs = append(evs, Event{Type: Added, Object: now})
			}
		case now.GetResourceVersion() == was.GetResourceVersion():
			v.put(k, now) // the same version, from the source the view follows from now on
		case older(now, was):
			// The source has not come to the version the view holds; it will deliver it.
		case now.GetUID() != "" && was.GetUID() != "" && now.GetUID() != was.GetUID():
			v.remove(k, "")
			v.put(k, now)
			evs = append(evs, Event{Type: Deleted, Object: was}, Event{Type: Added, Object: now})
		default:
			v.put(k, now)
			evs = append(evs, Event{Type: Updated, Object: now})
		}
	}
	return evs
}

// apply brings one event of the current source into the view and returns what the subscribers
// are told: nothing for a version the view holds or has passed, a delete for an object whose
// labels left the selection. unknown marks a delete the informer found by listing again rather
// than watching, whose object is the last state it knew. Called with v.mu held.
func (v *view) apply(typ EventType, u *unstructured.Unstructured, unknown bool) []Event {
	k := objectKey(u.GetNamespace(), u.GetName())
	was := v.seen[k]
	switch {
	case typ == Deleted:
		if was == nil || (u.GetUID() != "" && was.GetUID() != "" && u.GetUID() != was.GetUID()) || (!unknown && older(u, was)) {
			return nil // not held, or the delete of an older object of that name
		}
		v.remove(k, u.GetResourceVersion())
		return []Event{{Type: Deleted, Object: u}}
	case !v.matches(u):
		if was == nil || older(u, was) {
			return nil
		}
		v.remove(k, u.GetResourceVersion())
		return []Event{{Type: Deleted, Object: u}}
	case was == nil:
		if v.buried(k, u) {
			return nil
		}
		v.put(k, u)
		return []Event{{Type: Added, Object: u}}
	case u.GetResourceVersion() == was.GetResourceVersion():
		v.put(k, u) // the same version: keep the current source's object, so the old one's store can go
		return nil
	case older(u, was):
		return nil
	}
	v.put(k, u)
	return []Event{{Type: Updated, Object: u}}
}

func (v *view) matches(u *unstructured.Unstructured) bool {
	return v.sel.Matches(labels.Set(u.GetLabels()))
}

// older reports whether a is an older version than b by comparable resourceVersions; false when
// they are not comparable.
func older(a, b *unstructured.Unstructured) bool {
	return newerRV(b.GetResourceVersion(), a.GetResourceVersion())
}

// newerRV reports whether resourceVersion a is newer than b; false when they are not comparable.
func newerRV(a, b string) bool {
	n, err := resourceversion.CompareResourceVersion(a, b)
	return err == nil && n > 0
}

// buried reports whether an object the view removed comes back in a version not newer than the
// one that removed it. Called with v.mu held.
func (v *view) buried(k string, u *unstructured.Unstructured) bool {
	t, ok := v.tombs[k]
	if !ok {
		return false
	}
	n, err := resourceversion.CompareResourceVersion(u.GetResourceVersion(), t.rv)
	return err == nil && n <= 0
}

// put keeps an object in the view. Called with v.mu held.
func (v *view) put(k string, u *unstructured.Unstructured) {
	v.seen[k] = u
	ns := v.byNS[u.GetNamespace()]
	if ns == nil {
		ns = map[string]*unstructured.Unstructured{}
		v.byNS[u.GetNamespace()] = ns
	}
	ns[k] = u
	delete(v.tombs, k)
}

// remove drops an object from the view and remembers the version that removed it, unless rv is
// empty. Called with v.mu held.
func (v *view) remove(k, rv string) {
	if was := v.seen[k]; was != nil {
		if ns := v.byNS[was.GetNamespace()]; ns != nil {
			delete(ns, k)
			if len(ns) == 0 {
				delete(v.byNS, was.GetNamespace())
			}
		}
	}
	delete(v.seen, k)
	if rv == "" {
		return
	}
	v.seq++
	t := tomb{key: k, rv: rv, seq: v.seq}
	v.tombs[k] = t
	v.order = append(v.order, t)
	for len(v.order) > tombstones {
		if old := v.order[0]; v.tombs[old.key].seq == old.seq {
			delete(v.tombs, old.key)
		}
		v.order = v.order[1:]
	}
}

func (v *view) list() []*unstructured.Unstructured {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]*unstructured.Unstructured, 0, len(v.seen))
	for _, u := range v.seen {
		out = append(out, u)
	}
	return out
}

func (v *view) listNamespace(namespace string) []*unstructured.Unstructured {
	v.mu.Lock()
	defer v.mu.Unlock()
	ns := v.byNS[namespace]
	out := make([]*unstructured.Unstructured, 0, len(ns))
	for _, u := range ns {
		out = append(out, u)
	}
	return out
}

func (v *view) get(k string) (*unstructured.Unstructured, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	u, ok := v.seen[k]
	return u, ok
}

// synced reports whether the view's source has delivered its initial list to the subscribers.
func (v *view) synced() bool {
	v.mu.Lock()
	cur := v.cur
	var reg toolscache.ResourceEventHandlerRegistration
	if cur != nil {
		reg = cur.reg
	}
	v.mu.Unlock()
	return reg != nil && reg.HasSynced()
}

// err returns the error of the informer the view reads from (or waits for, before its first
// source has synced).
func (v *view) err() error {
	v.mu.Lock()
	src, fail := v.cur, v.fail
	if src == nil {
		src = v.next
	}
	v.mu.Unlock()
	switch {
	case fail != nil:
		return fail
	case src == nil:
		return nil
	}
	return src.run.err()
}

// stats returns the view's object count, whether it synced, the cover that serves it ("" when
// its own informer does), and its error.
func (v *view) stats() (objects int, synced bool, coveredBy string, err error) {
	v.mu.Lock()
	objects = len(v.seen)
	if v.cur != nil && v.cur.run.scope != v.e.scope {
		coveredBy = v.cur.run.scope.String()
	}
	v.mu.Unlock()
	return objects, v.synced(), coveredBy, v.err()
}
