package engine

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
)

// How the engine reads a kind (architecture §5, "Scopes"). Every read of a kind's objects goes
// through Backend.Acquire and the kind* helpers below, never through cache.Scope{GVR: …} by hand,
// so that a kind's profile alone decides what the cache holds for it:
//
//   - its scope: the GVR with the profile's LabelSelector and MetadataOnly (Secrets watch every
//     Secret in full; Helm's releases the Secrets labelled owner=helm, which the full Secrets watch
//     serves while it runs), in all namespaces, or in each namespace the user named when they may
//     not list the kind in all of them (accessiblens_plan.go);
//   - where it lives: the cluster's cache, or the backend's local store for a local kind (port
//     forwards), which never reaches an API server;
//   - what its objects are: the cached objects, or what the profile's Derive makes of them for a
//     virtual kind (Helm releases from their release Secrets).

// localStore holds the objects of a backend's local kinds: an in-memory source and a cache over
// it, made on first use.
type localStore struct {
	mu    sync.Mutex
	src   *cache.Memory
	cache *cache.Cache
	done  bool
}

func (l *localStore) get(log *slog.Logger) (*cache.Memory, *cache.Cache) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cache == nil {
		l.src = cache.NewMemory()
		l.cache = cache.New(l.src, cache.Options{Logger: log})
		if l.done {
			l.cache.Close() // a closed backend hands out never-syncing handles
		}
	}
	return l.src, l.cache
}

func (l *localStore) close() {
	l.mu.Lock()
	c := l.cache
	l.done = true
	l.mu.Unlock()
	if c != nil {
		c.Close()
	}
}

// HoldsSecretValues reports whether a kind's scope holds core Secrets with their values: the
// Secrets view's kind, and Helm's releases. Reading such a kind starts (or keeps warm) a watch of
// every value in its scope, so nothing reads it as a side effect, such as a search across kinds.
func HoldsSecretValues(p *model.KindProfile) bool {
	return p != nil && !p.Local && p.Group == "" && p.Resource == secretsKind && !p.MetadataOnly
}

// Scope returns the cache scope a kind's objects are watched in: its GVR in all namespaces, with
// the profile's label selector and metadata-only flag. A label-selected scope is served from the
// unselected scope of the same resource while that runs (cache.Scope, "covering scopes"): Helm's
// releases read from the Secrets view's watch while it is open.
func (b *Backend) Scope(p *model.KindProfile) cache.Scope {
	return cache.Scope{GVR: p.GVR(), LabelSelector: p.LabelSelector, MetadataOnly: p.MetadataOnly}
}

// Acquire returns a handle on the objects of a kind: on the backend's local store for a local
// kind, on the cluster's cache otherwise. The caller releases it. For a virtual kind the handle
// holds the objects Derive works on; read the kind's objects through KindList and KindGet.
func (b *Backend) Acquire(p *model.KindProfile) *cache.Handle {
	if isHelmReleases(p) { // the release Secrets joined with the release ConfigMaps (helmdriver.go)
		return b.acquireReleases(p)
	}
	if p.Local {
		_, c := b.local.get(b.logger())
		return c.Acquire(b.Scope(p))
	}
	// In all namespaces, or in each namespace the user named when they may not list the kind in
	// all of them (accessiblens_plan.go).
	return b.acquirePlanned(p, b.namespacePlan(p))
}

func (b *Backend) logger() *slog.Logger { return slog.Default() }

// errNotLocal refuses a local-store write for a kind that is not local.
var errNotLocal = errors.New("not a local kind")

// PutLocal adds or replaces an object of a local kind in the backend's local store: views, the
// drawer and agents see it like a cluster object. The object needs a name (and a namespace when
// the kind is namespaced); apiVersion and kind are set from the profile. The store keeps a copy.
func (b *Backend) PutLocal(p *model.KindProfile, obj *unstructured.Unstructured) error {
	if !p.Local {
		return fmt.Errorf("%s: %w", p.Key, errNotLocal)
	}
	if obj.GetName() == "" {
		return errors.New("a local object needs a name")
	}
	if p.Namespaced != (obj.GetNamespace() != "") {
		return fmt.Errorf("%s: namespace %q does not fit the kind's scope", p.Key, obj.GetNamespace())
	}
	obj = obj.DeepCopy()
	obj.SetAPIVersion(p.Group + "/" + p.Version)
	obj.SetKind(p.Kind)
	src, _ := b.local.get(b.logger())
	src.Set(p.GVR(), obj)
	return nil
}

// DeleteLocal removes an object of a local kind from the backend's local store; a missing one is
// a no-op.
func (b *Backend) DeleteLocal(p *model.KindProfile, namespace, name string) error {
	if !p.Local {
		return fmt.Errorf("%s: %w", p.Key, errNotLocal)
	}
	src, _ := b.local.get(b.logger())
	src.Delete(p.GVR(), namespace, name)
	return nil
}

// KindList returns the objects of a kind from its handle: the cached objects, or what Derive makes
// of them. Objects are shared and must not be modified.
func KindList(h *cache.Handle, p *model.KindProfile) []*unstructured.Unstructured {
	return derive(p, h.List())
}

// KindListNamespace is KindList for one namespace.
func KindListNamespace(h *cache.Handle, p *model.KindProfile, namespace string) []*unstructured.Unstructured {
	return derive(p, h.ListNamespace(namespace))
}

// KindGet returns one object of a kind by namespace and name ("" for cluster-scoped kinds).
func KindGet(h *cache.Handle, p *model.KindProfile, namespace, name string) (*unstructured.Unstructured, bool) {
	if p.Derive == nil {
		return h.Get(namespace, name)
	}
	var objs []*unstructured.Unstructured
	if p.Namespaced {
		objs = KindListNamespace(h, p, namespace)
	} else {
		objs = KindList(h, p)
	}
	for _, o := range objs {
		if o.GetName() == name && o.GetNamespace() == namespace {
			return o, true
		}
	}
	return nil, false
}

// derive applies a virtual kind's Derive; a panicking Derive yields no objects.
func derive(p *model.KindProfile, objs []*unstructured.Unstructured) (out []*unstructured.Unstructured) {
	if p.Derive == nil {
		return objs
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Default().Error("engine: deriving a virtual kind's objects failed", "kind", p.Key, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			out = nil
		}
	}()
	return p.Derive(objs)
}
