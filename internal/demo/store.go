package demo

import (
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

// resourcePods is the pods resource, also the name of a node's pod capacity.
const resourcePods = "pods"

// kind indexes the resources the cluster serves.
type kind int

const (
	kindPods kind = iota
	kindDeployments
	kindServices
	kindNamespaces
	kindNodes
	kindEvents
	kindSecrets    // Helm's release records only (helm.go)
	kindConfigMaps // Helm's release records only (helm.go)
	numKinds
)

// kindInfo describes each served resource.
var kindInfo = [numKinds]struct {
	gvr        schema.GroupVersionResource
	apiVersion string
	kind       string
	namespaced bool
}{
	kindPods:        {schema.GroupVersionResource{Version: "v1", Resource: resourcePods}, "v1", "Pod", true},
	kindDeployments: {schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, "apps/v1", "Deployment", true},
	kindServices:    {schema.GroupVersionResource{Version: "v1", Resource: "services"}, "v1", "Service", true},
	kindNamespaces:  {schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, "v1", "Namespace", false},
	kindNodes:       {schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, "v1", "Node", false},
	kindEvents:      {schema.GroupVersionResource{Version: "v1", Resource: "events"}, "v1", "Event", true},
	kindSecrets:     {schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, "v1", "Secret", true},
	kindConfigMaps:  {schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, "v1", "ConfigMap", true},
}

// kindOf returns the kind serving gvr.
func kindOf(gvr schema.GroupVersionResource) (kind, bool) {
	for k := range numKinds {
		if kindInfo[k].gvr == gvr {
			return k, true
		}
	}
	return 0, false
}

// objKey is the informer store key: namespace/name, or name for cluster-scoped objects.
func objKey(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

// change is one entry of the history and of a watcher's queue. In the history obj is set only
// for deletions (the last state, which the store no longer has); in a queue it is the object
// version the change produced.
type change struct {
	rv        int64
	kind      kind
	typ       watch.EventType
	namespace string
	key       string
	obj       *unstructured.Unstructured
}

// put stores a freshly rendered object during the build: it gets the next resourceVersion but no
// history entry and no watch event. Called with c.mu held.
func (c *Cluster) put(k kind, obj *unstructured.Unstructured) {
	c.rv++
	obj.SetResourceVersion(strconv.FormatInt(c.rv, 10))
	c.store[k][objKey(obj.GetNamespace(), obj.GetName())] = obj
}

// publish applies one change: obj (freshly rendered, not yet shared) gets the next
// resourceVersion, replaces or leaves the store, enters the history and goes to the watchers.
// After publish the object is immutable. Called with c.mu held.
func (c *Cluster) publish(k kind, typ watch.EventType, obj *unstructured.Unstructured) {
	c.rv++
	obj.SetResourceVersion(strconv.FormatInt(c.rv, 10))
	ns := obj.GetNamespace()
	key := objKey(ns, obj.GetName())
	ch := change{rv: c.rv, kind: k, typ: typ, namespace: ns, key: key, obj: obj}
	if typ == watch.Deleted {
		delete(c.store[k], key)
	} else {
		c.store[k][key] = obj
	}
	hist := ch
	if typ != watch.Deleted {
		hist.obj = nil // the store has it; the history keeps no old versions
	}
	if evicted, ok := c.history.push(hist); ok {
		c.compacted = evicted
	}
	for w := range c.watchers[k] {
		w.enqueue(ch)
	}
	c.sent.Add(1)
}

// currentRV returns the resourceVersion of a stored object, for event references.
func (c *Cluster) currentRV(k kind, namespace, name string) string {
	if o := c.store[k][objKey(namespace, name)]; o != nil {
		return o.GetResourceVersion()
	}
	return ""
}

// history is a ring of the most recent changes, oldest first.
type history struct {
	buf   []change
	start int // index of the oldest entry
	n     int
}

func newHistory(size int) history { return history{buf: make([]change, size)} }

// push appends a change; when the ring is full it evicts the oldest and returns its
// resourceVersion.
func (h *history) push(ch change) (evicted int64, ok bool) {
	if h.n < len(h.buf) {
		h.buf[(h.start+h.n)%len(h.buf)] = ch
		h.n++
		return 0, false
	}
	evicted = h.buf[h.start].rv
	h.buf[h.start] = ch
	h.start = (h.start + 1) % len(h.buf)
	return evicted, true
}

// since calls fn for every change with a resourceVersion above rv, oldest first.
func (h *history) since(rv int64, fn func(*change)) {
	// Binary search for the first entry above rv; the ring is sorted by resourceVersion.
	lo, hi := 0, h.n
	for lo < hi {
		mid := (lo + hi) / 2
		if h.buf[(h.start+mid)%len(h.buf)].rv <= rv {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	for i := lo; i < h.n; i++ {
		fn(&h.buf[(h.start+i)%len(h.buf)])
	}
}
