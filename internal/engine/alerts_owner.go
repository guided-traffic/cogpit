package engine

import (
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The object that controls an alert's pod (the alert board's cards, alerts_card.go): CloudNativePG's
// alerts name a pod, and what an admin runs is the Cluster that owns it; a crash loop names a pod,
// and what is rolled out is its Deployment. The board finds it in the pods' ownerReferences, a
// ReplicaSet's own controller after it (its Deployment), from what the cache holds already: it
// peeks (cache.Peek, as the secret scrub does, H-82) and starts no watch, so a pod the cache does not
// hold has no owner on the board. The index is built again only when the cached pods or ReplicaSets
// changed (PeekVersion).

// podOwnerIndex is a backend's index of its cached pods' and ReplicaSets' controllers.
type podOwnerIndex struct {
	mu      sync.Mutex
	version string
	by      map[string]ownerRef // "pods/<ns>/<name>", "replicasets/<ns>/<name>"
}

// ownerRef is an object's controller: its API version, kind and name, in the object's namespace.
type ownerRef struct{ apiVersion, kind, name string }

// cachedOwners selects the cache's scopes of pods and ReplicaSets.
func cachedOwners(s cache.Scope) bool {
	return s.GVR.Group == "" && s.GVR.Resource == resourcePods || s.GVR.Group == groupApps && s.GVR.Resource == "replicasets"
}

// podOwners returns the lookup of a pod's controller ("" kind: none known), from the index of what
// b's cache holds now; nil without a cache.
func (b *Backend) podOwners() func(namespace, pod string) ownerRef {
	if b == nil || b.Cache == nil {
		return nil
	}
	version := b.Cache.PeekVersion(cachedOwners) // before the objects: they are at least this new
	ix := &b.owners
	ix.mu.Lock()
	if ix.by == nil || ix.version != version {
		ix.by, ix.version = map[string]ownerRef{}, version
		for _, p := range b.Cache.Peek(cachedOwners) {
			for _, o := range p.Objects {
				if ref, ok := controllerOf(o); ok {
					ix.by[p.Scope.GVR.Resource+"/"+o.GetNamespace()+"/"+o.GetName()] = ref
				}
			}
		}
	}
	by := ix.by
	ix.mu.Unlock()
	return func(namespace, pod string) ownerRef {
		ref := by["pods/"+namespace+"/"+pod]
		if group, _, _ := strings.Cut(ref.apiVersion, "/"); group == groupApps && ref.kind == apiKindReplicaSet {
			if dep, ok := by["replicasets/"+namespace+"/"+ref.name]; ok {
				return dep
			}
		}
		return ref
	}
}

// controllerOf is an object's controller among its ownerReferences.
func controllerOf(o *unstructured.Unstructured) (ownerRef, bool) {
	for _, r := range o.GetOwnerReferences() {
		if r.Controller != nil && *r.Controller && r.Name != "" {
			return ownerRef{r.APIVersion, r.Kind, r.Name}, true
		}
	}
	return ownerRef{}, false
}

// ownerObject is a pod's controller as an object of the board: linked when the cluster serves its
// kind, its name as text otherwise.
func (ix alertKindIndex) ownerObject(namespace string, r ownerRef) AlertObject {
	o := AlertObject{Label: "owner", Ref: model.ObjectRef{Namespace: namespace, Name: r.name}, Title: r.kind}
	group, _, grouped := strings.Cut(r.apiVersion, "/")
	if !grouped {
		group = ""
	}
	if p := ix.byGroup[group+"/"+r.kind]; p != nil {
		o.Ref.Kind = p.Key
	}
	return o
}
