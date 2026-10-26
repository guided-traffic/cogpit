package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Following a refusal (G3; architecture §5, "A refusal counts, whoever met it"). A user whose one
// RoleBinding is in the context's namespace gets a plan that reads a kind in all namespaces first:
// that namespace's rules allow the kind, and nothing else says no. The API server refuses that
// read (403 Forbidden). From then on the kind is read namespace by namespace, whoever met the
// refusal: the cache tells the session of every refused list or watch (cache.OnRefused,
// attachRefusals), and what is open reads the kind again (state.replan). A read that waits for its
// objects (one object, a list for an agent, a workload's logs, a port forward, the drawer's model
// for an agent) follows its own refusal at once: it drops the refused handle and takes the read
// again, now in the target's namespace or the plan's namespaces (settleReads).

// followedRead is one read that waits for a kind's objects: how it acquires them, and its handle.
type followedRead struct {
	p        *model.KindProfile
	acquire  func(context.Context) *cache.Handle
	h        *cache.Handle
	followed bool // its read in all namespaces was refused and taken again (once)
}

// startRead acquires a read's handle.
func startRead(ctx context.Context, p *model.KindProfile, acquire func(context.Context) *cache.Handle) *followedRead {
	return &followedRead{p: p, acquire: acquire, h: acquire(ctx)}
}

// settleReads waits, bounded by ctx, until every read's handle has synced, the reads side by side.
// A read in all namespaces that the API server refuses is followed: the refusal is kept (the kind is
// read namespace by namespace from then on, by every reader) and the read is taken again, once.
func (b *Backend) settleReads(ctx context.Context, reads ...*followedRead) {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		waiting := false
		for _, r := range reads {
			if r.h.HasSynced() {
				continue
			}
			if !r.followed && readsAllNamespaces(r.p, r.h) && cache.IsRefused(r.h.Err()) {
				r.followed = true
				b.followRefused(r.p) // noted before the release ends the refused read (acquirePlanned)
				r.h.Release()
				if r.h = r.acquire(ctx); r.h.HasSynced() {
					continue
				}
			}
			waiting = true
		}
		if !waiting {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// readFollowing acquires a kind's objects for a read that waits for them (acquire: acquireIn for
// one namespace's objects, acquireSettled for a list) and waits, bounded by ctx and syncTimeout,
// for their initial list, following a refusal in all namespaces (settleReads). Before
// (2026-10-13) such a read waited 15 seconds for a list that never came and failed. label names
// the kind in the error of a list that never finished; the handle goes back with an error.
func (b *Backend) readFollowing(ctx context.Context, p *model.KindProfile, label string, acquire func(context.Context) *cache.Handle) (*cache.Handle, error) {
	r := startRead(ctx, p, acquire)
	wctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	b.settleReads(wctx, r)
	if r.h.HasSynced() {
		return r.h, nil
	}
	err := notSynced(ctx, r.h, label)
	r.h.Release()
	return nil, err
}

// kindHandle is one kind's handle of a reader of several kinds.
type kindHandle struct {
	p *model.KindProfile
	h *cache.Handle
}

// openFollowing opens a one-off reader of several kinds (an agent's Overview: open acquires its
// handles) once the kinds' plans are decided, so that it does not take them in all namespaces
// while the access reviews are out, and waits, bounded by ctx, for its handles; when the API server
// refuses a read in all namespaces, the reader is opened again, once, now namespace by namespace
// for those kinds. Before (2026-10-13) such a reader read in all namespaces while the reviews were
// out and waited 15 seconds on a refused read. The caller releases what it returns.
func openFollowing[T any](ctx context.Context, b *Backend, keys []string, open func() (T, []kindHandle), release func(T)) T {
	for _, key := range keys {
		if p, ok := b.Profile(key); ok {
			b.settledPlan(ctx, p)
		}
	}
	r, reads := open()
	if refused := refusedWide(ctx, reads); len(refused) > 0 {
		for _, p := range refused { // noted before the release ends the refused reads (acquirePlanned)
			b.followRefused(p)
		}
		release(r)
		r, _ = open()
	}
	return r
}

// refusedWide waits, bounded by ctx, until every handle has synced or been refused, and returns
// the kinds whose read in all namespaces the API server refused (all of them, so that one reopen
// covers every refusal).
func refusedWide(ctx context.Context, reads []kindHandle) []*model.KindProfile {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		var refused []*model.KindProfile
		waiting := false
		for _, r := range reads {
			switch {
			case r.h.HasSynced():
			case readsAllNamespaces(r.p, r.h) && cache.IsRefused(r.h.Err()):
				refused = append(refused, r.p)
			default:
				waiting = true
			}
		}
		if !waiting {
			return refused
		}
		select {
		case <-ctx.Done():
			return refused
		case <-t.C:
		}
	}
}

// notSynced is waitSynced's error for a handle that did not sync in time: its list's error, the
// caller's ctx ending, or the time that ran out.
func notSynced(ctx context.Context, h *cache.Handle, kind string) error {
	if err := h.Err(); err != nil {
		return fmt.Errorf("%w %s: %v", ErrNotSynced, kind, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%w %s yet", ErrNotSynced, kind)
}

// inNamespace is acquireIn for readFollowing: a kind's objects in one namespace.
func (b *Backend) inNamespace(p *model.KindProfile, namespace string) func(context.Context) *cache.Handle {
	return func(ctx context.Context) *cache.Handle { return b.acquireIn(ctx, p, namespace) }
}

// readsAllNamespaces reports whether a handle on a namespaced kind of the cluster reads it in all
// namespaces: one that a refusal there would leave without objects.
func readsAllNamespaces(p *model.KindProfile, h *cache.Handle) bool {
	return p.Namespaced && !p.Local && !isApplications(p) && !isHelmReleases(p) &&
		h.Namespaces() == nil && h.Scope().Namespace == ""
}

// followRefused keeps that the API server refused to list a kind in all namespaces and, when that
// is news, has what is open (views, pages, the drawer) read it again namespace by namespace.
func (b *Backend) followRefused(p *model.KindProfile) {
	if b.noteRefused(p) {
		b.noteNamespaced(p) // in Connection Diagnostics' log (diaglogs.go, D-133)
		b.ns.wakeReplan()
	}
}

// attachRefusals makes every refused read of a namespaced kind in all namespaces count, whoever
// sent it: a view, a page, the drawer, a related-objects index, an agent's read, the lookup of
// Prometheus's Services. Before (2026-10-13) only a view's own refusal counted, and a page or an
// index whose kind no view had opened kept a cluster-wide watch the server refused, retried for as
// long as it was open. The cache tells of each refused list or watch (cache.OnRefused) on an
// informer's goroutine; the replan runs on a loop of the session's, which the backend's close
// ends. A scope in one namespace and a cluster-scoped or local kind are left alone. Connect calls
// it before the backend is published.
func (s *Session) attachRefusals(b *Backend) {
	if b == nil || b.Cache == nil {
		return
	}
	wake, quit := make(chan struct{}, 1), make(chan struct{})
	b.ns.mu.Lock()
	b.ns.replan, b.ns.quit = wake, quit
	b.ns.mu.Unlock()
	s.goSafe("namespace plan", func() {
		for {
			select {
			case <-quit:
				return
			case <-wake:
				s.state.replan(b)
			}
		}
	})
	b.Cache.OnRefused(func(sc cache.Scope) {
		if sc.Namespace != "" {
			return
		}
		for _, p := range b.AllProfiles() {
			if p.GVR() == sc.GVR && p.Namespaced && !p.Local {
				b.followRefused(p)
				return
			}
		}
	})
}

// acquireNow is Acquire for a read of one object's kind that does not wait (the GUI's drawer and
// its model, their events and related objects): in all namespaces, or, for a user who may not list
// the kind there, in the object's namespace alone, shared with any other reader of that namespace
// (the view's, when the plan names it). A plan still pending, a cluster-scoped object and the kinds
// with caches of their own read as Acquire does. Before (2026-10-13) the drawer read the plan's
// namespaces, and an object in another one the user may read showed as missing.
func (b *Backend) acquireNow(p *model.KindProfile, namespace string) *cache.Handle {
	switch {
	case b.narrowsTo(p, namespace):
		return b.Cache.AcquireNamespaces(b.Scope(p), []string{namespace})
	case b.waitsForPlan(p):
		// Nothing while the access reviews have not said whether the kind may be listed in all
		// namespaces, as the view holds nothing then: a read in all namespaces would be refused a
		// user who may not. The drawer starts again once they answer (drawerPlanKey); a menu's
		// check says the kind is still loading.
		return cache.Pending(b.Scope(p))
	}
	return b.Acquire(p)
}

// waitsForPlan reports whether a namespaced kind of the cluster has a pending plan.
func (b *Backend) waitsForPlan(p *model.KindProfile) bool {
	return p.Namespaced && !p.Local && !isApplications(p) && !isHelmReleases(p) && b.namespacePlan(p).pending
}

// narrowsTo reports whether acquireNow reads a kind in namespace alone: namespace is an object's,
// and the user may not list the kind in all namespaces, as the access reviews or a refusal say.
// The rules of the other namespaces the plan names do not matter here: a plan pending for one
// (a namespace just added to the filter) does not send the drawer back to all namespaces.
func (b *Backend) narrowsTo(p *model.KindProfile, namespace string) bool {
	if b == nil || namespace == "" || !p.Namespaced || p.Local || isApplications(p) || isHelmReleases(p) {
		return false
	}
	denied, settled := b.clusterWideDenied(p)
	return denied && settled
}

// readKeyIn is how acquireNow reads a kind for an object in namespace now: a drawer whose key
// changes reads again.
func (b *Backend) readKeyIn(p *model.KindProfile, namespace string) string {
	if b.narrowsTo(p, namespace) {
		return "in:" + namespace
	}
	return b.namespacePlan(p).key()
}

// detailReads takes what a drawer model is built from: the object's kind, the events (nil when
// the cluster serves none or the kind has none) and the related-objects indexes. A read that waits
// (DetailWait: object_get, cogpit-cli get) reads the object's namespace alone once the plan is
// decided, as Session.Object does, the events and a namespaced object's related objects there too,
// follows a refusal in all namespaces, and waits, bounded by ctx and syncTimeout, for the kind and
// the events side by side, then for the related objects. The GUI's read does not wait and reads as
// the open drawer does (acquireNow). Before (2026-10-13) the waiting read took the plan's
// namespaces, which can miss the object's, and waited 15 seconds for a refused read.
func (s *Session) detailReads(ctx context.Context, b *Backend, p *model.KindProfile, ref model.ObjectRef, wait bool) (h, evh *cache.Handle, rs *relatedSet) {
	ep, events := b.Profile(eventsKind)
	events = events && p.APIObject() // no event involves a virtual or local kind's object
	ns := ref.Namespace              // "" for a cluster-scoped kind (resolve)
	if !wait {
		h = b.acquireNow(p, ns)
		if events {
			evh = b.acquireNow(ep, ns)
		}
		if rs = s.drawerRelated(b, p, ref); rs == nil {
			rs = newRelatedSetWith(b, drawerNeeds(b, p), nil, false, func(rp *model.KindProfile) *cache.Handle { return b.acquireNow(rp, ns) })
		}
		return h, evh, rs
	}
	reads := []*followedRead{startRead(ctx, p, b.inNamespace(p, ns))}
	if events {
		reads = append(reads, startRead(ctx, ep, b.inNamespace(ep, ns)))
	}
	wctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	b.settleReads(wctx, reads...)
	h = reads[0].h
	if events {
		evh = reads[1].h
	}
	if rs = s.drawerRelated(b, p, ref); rs == nil {
		rs = newRelatedSetWith(b, drawerNeeds(b, p), nil, false, func(rp *model.KindProfile) *cache.Handle {
			r := startRead(wctx, rp, b.inNamespace(rp, ns))
			b.settleReads(wctx, r)
			return r.h
		})
	}
	rs.waitSynced(wctx)
	return h, evh, rs
}
