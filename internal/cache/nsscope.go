package cache

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Namespaced watches (architecture §5, G3). A user who may not list a kind in all namespaces can
// still read it in the namespaces a Role grants: the engine then reads the kind through one scope
// per namespace (Scope.Namespace set), each an informer of its own, shared and reference-counted
// like every other scope, and AcquireNamespaces merges them into one handle. A scope the API server
// refuses (403 Forbidden) is not kept warm: retrying it for the warm period would cost a failing
// request per backoff step and show nothing.

// AcquireNamespaces returns one handle on scope's objects in the given namespaces: a scope per
// namespace, each served (and started if needed) as Acquire serves it, merged. scope.Namespace is
// ignored; duplicate and empty names are dropped. With no namespace the handle holds nothing and
// is synced.
//
// The merged handle:
//   - HasSynced: every namespace finished its initial list, or the server refused it (its Err says
//     so), so that the namespaces the user may read show while another one is refused;
//   - Err: each namespace's error, as "in <namespace>: <error>"; nil when none has one;
//   - Subscribe: the events of every namespace, exact per namespace (their objects are disjoint),
//     delivered to fn one at a time;
//   - Release: releases every namespace's scope, each warm on its own.
func (c *Cache) AcquireNamespaces(scope Scope, namespaces []string) *Handle {
	scope.Namespace = ""
	scope = scope.normalize()
	names := make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		if ns != "" {
			names = append(names, ns)
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)
	set := &nsSet{names: names, parts: make([]*Handle, len(names))}
	for i, ns := range names {
		part := scope
		part.Namespace = ns
		set.parts[i] = c.Acquire(part)
	}
	return &Handle{scope: scope, c: c, set: set}
}

// Pending returns a handle that holds nothing and never syncs, for a consumer whose scope is not
// decided yet (the engine, while the access reviews have not said whether the user may list a kind
// in all namespaces). Releasing it does nothing.
func Pending(scope Scope) *Handle { return &Handle{scope: scope.normalize()} }

// Namespaces returns the namespaces a handle from AcquireNamespaces reads, sorted; nil for any
// other handle (one scope: in its namespace, or in all).
func (h *Handle) Namespaces() []string {
	if h.set == nil {
		return nil
	}
	return slices.Clone(h.set.names)
}

// nsSet is a merged handle's scopes, one per namespace, in the order of names.
type nsSet struct {
	names []string
	parts []*Handle
}

func (s *nsSet) part(namespace string) *Handle {
	if i, ok := slices.BinarySearch(s.names, namespace); ok {
		return s.parts[i]
	}
	return nil
}

func (s *nsSet) synced() bool {
	for _, p := range s.parts {
		if !p.HasSynced() && !isRefused(p.Err()) {
			return false
		}
	}
	return true
}

func (s *nsSet) list() []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for _, p := range s.parts {
		out = append(out, p.List()...)
	}
	return out
}

func (s *nsSet) listNamespace(namespace string) []*unstructured.Unstructured {
	if p := s.part(namespace); p != nil {
		return p.List()
	}
	return nil
}

func (s *nsSet) get(namespace, name string) (*unstructured.Unstructured, bool) {
	if p := s.part(namespace); p != nil {
		return p.Get(namespace, name)
	}
	return nil, false
}

// subscribe hands fn the events of every namespace, one at a time: each namespace's informer
// delivers on its own goroutine.
func (s *nsSet) subscribe(fn func(Event)) func() {
	var mu sync.Mutex
	one := func(ev Event) {
		mu.Lock()
		defer mu.Unlock()
		fn(ev)
	}
	cancels := make([]func(), len(s.parts))
	for i, p := range s.parts {
		cancels[i] = p.Subscribe(one)
	}
	return func() {
		for _, cancel := range cancels {
			cancel()
		}
	}
}

func (s *nsSet) err() error {
	var errs nsErrors
	for i, p := range s.parts {
		if err := p.Err(); err != nil {
			errs = append(errs, fmt.Errorf("in %s: %w", s.names[i], err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func (s *nsSet) release() {
	for _, p := range s.parts {
		p.Release()
	}
}

// nsErrors are the errors of a merged handle's namespaces, on one line.
type nsErrors []error

func (e nsErrors) Error() string {
	texts := make([]string, len(e))
	for i, err := range e {
		texts[i] = err.Error()
	}
	return strings.Join(texts, "; ")
}

func (e nsErrors) Unwrap() []error { return e }

// isRefused reports whether a list or watch error is the API server's refusal of the user (403
// Forbidden): retrying it changes nothing until the user's roles change.
func isRefused(err error) bool { return err != nil && apierrors.IsForbidden(err) }

// IsRefused reports whether a handle's error (Handle.Err) is the API server's refusal of the user
// (403 Forbidden), in its scope or, for a merged handle, in one of its namespaces.
func IsRefused(err error) bool { return isRefused(err) }

// OnRefused registers fn to be told of every list or watch of a scope that the API server refused
// (403 Forbidden), whether a handle holds the scope or it is only warm; nil unregisters. fn runs on
// the informer's goroutine, once per refused request (client-go retries a held scope with its
// backoff), and must not block. The engine reads a namespaced kind namespace by namespace once a
// read of it in all namespaces was refused, whichever consumer's read it was (G3).
func (c *Cache) OnRefused(fn func(Scope)) {
	if fn == nil {
		c.refused.Store(nil)
		return
	}
	c.refused.Store(&fn)
}

// failed records a list or watch error. A refusal is told first (OnRefused), then shown: a reader
// that sees it may release the scope, which ends it (a refused scope is not kept warm), and a
// reader that decides how to read the kind after that must already know of the refusal, or it
// starts the refused read again (2026-10-20). A warm scope that was refused ends (refusedIf); a
// scope a handle holds keeps retrying, and its error shows.
func (r *runner) failed(err error) {
	refused := isRefused(err) && r.c != nil
	if refused {
		r.tellRefused()
	}
	r.setErr(err)
	if refused {
		r.refusedIf(err)
	}
}

// refusedIf ends a warm scope whose list or watch the API server refused; a scope a handle holds
// keeps retrying, and its error shows. The cache's OnRefused heard of it before (failed).
func (r *runner) refusedIf(err error) {
	if !isRefused(err) || r.c == nil {
		return
	}
	c := r.c
	c.mu.Lock()
	defer c.mu.Unlock()
	e := r.owner.Load()
	if e == nil {
		// A label-selected scope's own informer (cover.go): its entry reads it through a view, and
		// ends when the view follows it (not a cover that serves it meanwhile). Before
		// (2026-10-14) such a scope was kept warm and retried for its warm period.
		if se := c.informers[r.scope]; se != nil && se.view != nil && se.view.own == r && isRefused(se.view.err()) {
			e = se
		}
	}
	if e == nil || c.closed || e.refs > 0 || c.informers[e.scope] != e || (e.view == nil && e.run != r) {
		return
	}
	c.retire(e)
}

// refusedLocked reports whether the API server refused the last list or watch of the informer an
// entry reads from: its own, or for a label-selected scope the one its view follows (its own or
// its cover's). Called with Cache.mu held.
func (e *entry) refusedLocked() bool {
	if e.view != nil {
		return isRefused(e.view.err())
	}
	return e.run != nil && isRefused(e.run.err())
}

// tellRefused hands the cache's OnRefused hook the runner's scope; a panic in it is logged, never
// carried into client-go's goroutine.
func (r *runner) tellRefused() {
	fn := r.c.refused.Load()
	if fn == nil {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			r.log.Error("cache: the refusal hook panicked", "scope", r.scope.String(), "panic", fmt.Sprint(p))
		}
	}()
	(*fn)(r.scope)
}
