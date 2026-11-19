package engine

import (
	"context"
	"maps"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Who controls a pod, for the fold of the Overview's issues (clusteroverview_fold.go), from the
// cache alone. The Overview holds the pods and the Deployments, Stateful Sets and Daemon Sets; a
// Deployment's pods name a Replica Set and a Cron Job's a Job, which it does not. Those kinds, and
// only those (foldAcquires), are read into the cache for the call, as any list of them is (and as
// the Workloads Overview reads them), when the fold asks for one: an issue is about a pod whose
// controller is one, or a warning event is about a Replica Set or a Job. Without them the pods of
// the commonest workload would each stay a subject of their own. A kind that cannot be read, or
// whose list does not arrive in the call's time, is named in the fold (IssueFold.OwnersUnread).
// Every other owner counts only where the cache holds it already (a peek, as the alert board's
// owners are found, alerts_owner.go): an owner reference is written by whoever creates the pod, so
// it must never choose what Cogpit starts to watch.

// kindCronJobs is the Cron Jobs' kind key.
const kindCronJobs = "cronjobs"

// foldAcquires are the kinds the fold reads into the cache when it asks for an object of one.
// Without an entry the fold only peeks, and reads nothing the Overview does not read.
var foldAcquires = map[string]bool{logKindReplicaSets: true, logKindJobs: true, kindCronJobs: true}

// issueControllerOf is an object's controller among its owner references; nil without one.
func issueControllerOf(o *unstructured.Unstructured) *issueController {
	for _, r := range o.GetOwnerReferences() {
		if r.Controller != nil && *r.Controller && r.Name != "" {
			return &issueController{apiVersion: r.APIVersion, kind: r.Kind, name: r.Name, uid: string(r.UID)}
		}
	}
	return nil
}

// foldOwners is the fold's ownerLookup over a backend's cache for one read: the handles the
// Overview holds, the ones it acquires for the read, and what it peeks at.
type foldOwners struct {
	ctx     context.Context
	b       *Backend
	kinds   map[string]*model.KindProfile // by "<group>/<Kind>": the namespaced kinds of the cluster
	handles map[string]*cache.Handle      // by kind key; nil for a kind that cannot be read
	taken   []*cache.Handle               // the handles acquired for the read
	peeked  map[string]map[string]*unstructured.Unstructured
	missed  map[string]struct{} // the kinds of foldAcquires that could not be read
}

// newFoldOwners returns the lookup over b's cache and the Overview's handles in src; waiting for a
// kind it acquires is bounded by ctx. The caller releases it.
func newFoldOwners(ctx context.Context, b *Backend, src *overviewSource) *foldOwners {
	o := &foldOwners{ctx: ctx, b: b, kinds: map[string]*model.KindProfile{}, handles: map[string]*cache.Handle{},
		peeked: map[string]map[string]*unstructured.Unstructured{}, missed: map[string]struct{}{}}
	for _, p := range b.AllProfiles() {
		if !p.Namespaced || p.Local || p.Derive != nil || p.LabelSelector != "" || p.Kind == "" {
			continue
		}
		if key := p.Group + "/" + p.Kind; o.kinds[key] == nil {
			o.kinds[key] = p
		}
	}
	for _, k := range src.kinds() {
		o.handles[k.profile.Key] = k.handle
	}
	return o
}

func (o *foldOwners) release() {
	for _, h := range o.taken {
		h.Release()
	}
}

func (o *foldOwners) unread() []string {
	if len(o.missed) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(o.missed))
}

func (o *foldOwners) kindOf(apiVersion, kind string) (string, bool) {
	group, _, grouped := strings.Cut(apiVersion, "/")
	if !grouped {
		group = ""
	}
	p := o.kinds[group+"/"+kind]
	if p == nil {
		return "", false
	}
	return p.Key, true
}

func (o *foldOwners) object(kind, namespace, name string) (cachedOwner, bool) {
	obj := o.find(kind, namespace, name)
	if obj == nil {
		return cachedOwner{}, false
	}
	return cachedOwner{uid: string(obj.GetUID()), controller: issueControllerOf(obj)}, true
}

// find returns the cached object of a kind: through the kind's handle where the read has one, else
// from the scopes of the kind that run.
func (o *foldOwners) find(kind, namespace, name string) *unstructured.Unstructured {
	h, known := o.handles[kind]
	if !known && foldAcquires[kind] {
		h = o.acquire(kind)
		o.handles[kind] = h
		known = true
	}
	if known {
		if h == nil {
			return nil
		}
		obj, _ := h.Get(namespace, name)
		return obj
	}
	return o.peek(kind)[namespace+"/"+name]
}

// acquire reads a kind into the cache for the read and waits for its list, as a list of the kind
// does (following a refusal in all namespaces); nil for a kind the cluster does not serve or the
// user may not list. A kind the user may not list, or whose list is not there when the wait ends,
// is one the read missed.
func (o *foldOwners) acquire(kind string) *cache.Handle {
	p, ok := o.b.Profile(kind)
	if !ok {
		return nil
	}
	if listable, known := o.b.Listable(p, nil); known && !listable {
		o.missed[kind] = struct{}{}
		return nil
	}
	h := openFollowing(o.ctx, o.b, []string{kind}, func() (*cache.Handle, []kindHandle) {
		h := o.b.Acquire(p)
		return h, []kindHandle{{p, h}}
	}, (*cache.Handle).Release)
	o.taken = append(o.taken, h)
	if !h.HasSynced() || h.Err() != nil {
		o.missed[kind] = struct{}{}
	}
	return h
}

// peek returns the objects of a kind the cache holds without a handle of the read's, by namespace
// and name; it starts no watch.
func (o *foldOwners) peek(kind string) map[string]*unstructured.Unstructured {
	if by, ok := o.peeked[kind]; ok {
		return by
	}
	by := map[string]*unstructured.Unstructured{}
	o.peeked[kind] = by
	p, ok := o.b.Profile(kind)
	if !ok || o.b.Cache == nil {
		return by
	}
	gvr := p.GVR()
	for _, scope := range o.b.Cache.Peek(func(s cache.Scope) bool { return s.GVR == gvr && s.LabelSelector == "" }) {
		for _, obj := range scope.Objects {
			by[obj.GetNamespace()+"/"+obj.GetName()] = obj
		}
	}
	return by
}

// foldModel replaces a model's issues by their fold as q asks; nil leaves them as they are.
func foldModel(ctx context.Context, b *Backend, src *overviewSource, m *ClusterOverviewModel, q *IssueQuery) {
	if q == nil {
		return
	}
	wctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	owners := newFoldOwners(wctx, b, src)
	defer owners.release()
	m.Fold = foldIssues(m.Issues, owners, *q, FoldBytes)
	m.Issues = nil
}
