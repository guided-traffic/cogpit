// Package testkit holds test fixtures: an in-memory cache.Source, object builders, a fake API
// server over HTTPS (RBAC, label selectors, a service proxy, logs) and a proxy in front of it that
// can go silent, and fakes of Helm repositories and kubectl releases. A real control plane (envtest's
// etcd and kube-apiserver) is in testkit/envtest; the kind tests connect to the cluster make kind-up
// made. It is imported by tests only.
package testkit

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/guided-traffic/cogpit/internal/cache"
)

// MemorySource is an in-memory cache.Source: a fake API server for informers, without HTTP. Tests
// add, update and delete objects; informers see list results and watch events as from a real
// server. It counts List and Watch calls.
type MemorySource struct {
	mu       sync.Mutex
	rv       int64
	objs     map[schema.GroupVersionResource]map[string]*unstructured.Unstructured
	history  map[schema.GroupVersionResource][]memEvent
	watchers map[schema.GroupVersionResource]map[*memWatcher]struct{}

	lists   atomic.Int64
	watches atomic.Int64
}

type memEvent struct {
	rv  int64
	typ watch.EventType
	obj *unstructured.Unstructured
}

// NewMemorySource returns an empty source.
func NewMemorySource() *MemorySource {
	return &MemorySource{
		objs:     map[schema.GroupVersionResource]map[string]*unstructured.Unstructured{},
		history:  map[schema.GroupVersionResource][]memEvent{},
		watchers: map[schema.GroupVersionResource]map[*memWatcher]struct{}{},
	}
}

// Lists returns the number of List calls so far.
func (m *MemorySource) Lists() int64 { return m.lists.Load() }

// Watches returns the number of Watch calls so far.
func (m *MemorySource) Watches() int64 { return m.watches.Load() }

// Watching reports whether a watch on scope is open right now: started and not stopped.
func (m *MemorySource) Watching(scope cache.Scope) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for w := range m.watchers[scope.GVR] {
		if w.scope != scope {
			continue
		}
		select {
		case <-w.stop: // stopped; its removal is on its way
		default:
			return true
		}
	}
	return false
}

// Settled reports whether every informer of c has synced and holds its watch on src open. An
// informer that lists, then watches, reports synced after its list and opens its watch only after
// that, and a view or drawer that opened acquires its informers without waiting for either: a
// request count read before Settled holds can still miss a list or a watch the scenario already
// caused. Tests that assert request budgets on a MemorySource take their baseline once it holds.
// A label-selected scope served by a cover (cache.InformerStats.CoveredBy) needs only to be synced.
func Settled(c *cache.Cache, src *MemorySource) bool {
	for _, st := range c.Stats() {
		if !st.Synced {
			return false
		}
		// A covered scope has no watch of its own: its cover's watch serves it.
		if st.CoveredBy == "" && !src.Watching(st.Scope) {
			return false
		}
	}
	return true
}

// OpenWatches returns the number of watches open right now on gvr.
func (m *MemorySource) OpenWatches(gvr schema.GroupVersionResource) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.watchers[gvr])
}

// Set adds or replaces an object and sends Added or Modified to the watchers. It assigns the
// object's resourceVersion.
func (m *MemorySource) Set(gvr schema.GroupVersionResource, obj *unstructured.Unstructured) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rv++
	obj = obj.DeepCopy()
	obj.SetResourceVersion(strconv.FormatInt(m.rv, 10))
	byKey := m.objs[gvr]
	if byKey == nil {
		byKey = map[string]*unstructured.Unstructured{}
		m.objs[gvr] = byKey
	}
	key := objKey(obj.GetNamespace(), obj.GetName())
	typ := watch.Added
	if _, ok := byKey[key]; ok {
		typ = watch.Modified
	}
	byKey[key] = obj
	m.publish(gvr, memEvent{rv: m.rv, typ: typ, obj: obj})
}

// Delete removes an object and sends Deleted to the watchers.
func (m *MemorySource) Delete(gvr schema.GroupVersionResource, namespace, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := objKey(namespace, name)
	obj, ok := m.objs[gvr][key]
	if !ok {
		return
	}
	delete(m.objs[gvr], key)
	m.rv++
	obj = obj.DeepCopy()
	obj.SetResourceVersion(strconv.FormatInt(m.rv, 10))
	m.publish(gvr, memEvent{rv: m.rv, typ: watch.Deleted, obj: obj})
}

func (m *MemorySource) publish(gvr schema.GroupVersionResource, ev memEvent) {
	m.history[gvr] = append(m.history[gvr], ev)
	for w := range m.watchers[gvr] {
		w.send(ev)
	}
}

// ListerWatcher implements cache.Source. It honours the scope's namespace, label selector and
// MetadataOnly as the API server does (cache.FilterListerWatcher): a watch with a label selector
// turns an object whose labels leave the selection into a delete, and one whose labels enter it
// into an add.
func (m *MemorySource) ListerWatcher(scope cache.Scope) toolscache.ListerWatcher {
	plain := cache.Scope{GVR: scope.GVR, Namespace: scope.Namespace}
	return cache.FilterListerWatcher(scope, &memLW{m: m, gvr: scope.GVR, namespace: scope.Namespace, scope: plain})
}

func objKey(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

type memLW struct {
	m         *MemorySource
	gvr       schema.GroupVersionResource
	namespace string
	scope     cache.Scope
}

// IsWatchListSemanticsUnSupported tells client-go's reflector to list, then watch.
func (lw *memLW) IsWatchListSemanticsUnSupported() bool { return true }

func (lw *memLW) List(_ metav1.ListOptions) (runtime.Object, error) {
	lw.m.lists.Add(1)
	lw.m.mu.Lock()
	defer lw.m.mu.Unlock()
	list := &unstructured.UnstructuredList{Object: map[string]any{"apiVersion": "v1", "kind": "List"}}
	for _, o := range lw.m.objs[lw.gvr] {
		if lw.namespace != "" && o.GetNamespace() != lw.namespace {
			continue
		}
		if f := cache.FilterScope(lw.scope, o); f != nil {
			list.Items = append(list.Items, *f.DeepCopy())
		}
	}
	list.SetResourceVersion(strconv.FormatInt(lw.m.rv, 10))
	return list, nil
}

func (lw *memLW) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	lw.m.watches.Add(1)
	from, _ := strconv.ParseInt(opts.ResourceVersion, 10, 64)
	lw.m.mu.Lock()
	defer lw.m.mu.Unlock()
	var replay []memEvent
	for _, ev := range lw.m.history[lw.gvr] {
		if ev.rv > from {
			replay = append(replay, ev)
		}
	}
	// The channel holds the whole replay besides its buffer for live events: the replay is sent
	// under the source's lock before anyone reads, and a longer one would block Set and Delete
	// for good (a slow runner, many changes between a list and its watch).
	w := &memWatcher{ch: make(chan watch.Event, len(replay)+4096), stop: make(chan struct{}), namespace: lw.namespace, scope: lw.scope}
	for _, ev := range replay {
		w.send(ev)
	}
	set := lw.m.watchers[lw.gvr]
	if set == nil {
		set = map[*memWatcher]struct{}{}
		lw.m.watchers[lw.gvr] = set
	}
	set[w] = struct{}{}
	w.remove = func() {
		lw.m.mu.Lock()
		delete(lw.m.watchers[lw.gvr], w)
		lw.m.mu.Unlock()
	}
	return w, nil
}

type memWatcher struct {
	ch        chan watch.Event
	stop      chan struct{}
	once      sync.Once
	namespace string
	scope     cache.Scope
	remove    func()
}

func (w *memWatcher) send(ev memEvent) {
	if w.namespace != "" && ev.obj.GetNamespace() != w.namespace {
		return
	}
	obj := cache.FilterScope(w.scope, ev.obj)
	if obj == nil {
		return
	}
	select {
	case w.ch <- watch.Event{Type: ev.typ, Object: obj.DeepCopy()}:
	case <-w.stop:
	}
}

func (w *memWatcher) ResultChan() <-chan watch.Event { return w.ch }

func (w *memWatcher) Stop() {
	w.once.Do(func() {
		close(w.stop)
		if w.remove != nil {
			go w.remove() // Stop may be called while the source holds its lock
		}
	})
}

// WaitFor polls cond until it is true or ctx ends; it reports whether cond became true. Use it
// instead of sleeps in tests of asynchronous code.
func WaitFor(ctx context.Context, cond func() bool) bool {
	for {
		if cond() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		default:
		}
		select {
		case <-ctx.Done():
			return cond()
		case <-afterTick():
		}
	}
}
