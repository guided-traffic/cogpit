package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// What holds a Terminating namespace (WF43, D-98). The namespace controller names, on the
// namespace itself, the kinds that still have objects in it and why it could not discover every
// kind (kinds.NamespaceDeletionOf); anyone who may read the namespace reads that. The drawer of
// such a namespace lists, per kind it names, the objects there that hold their deletion with
// finalizers, read through the cache: one metadata-only watch of the kind in that namespace, held
// while the drawer shows the namespace (drawerWatch.blockers) and released when it closes or the
// kind is no longer named, so an object whose finalizers go vanishes by itself. A kind the user may
// not list there (the namespace's rules review) is a counted row from the condition alone, and a
// failed discovery names the API services that are not available (one watch of them, cluster-wide,
// while it lasts). Agents read the same section through object_get.

// BlockingTitle is the title of a Terminating namespace's drawer section of what holds it.
const BlockingTitle = "Blocking Deletion"

// maxBlockingRows bounds the objects the section lists; the rest are counted.
const maxBlockingRows = 100

// apiServicesGVR is the aggregated APIs' registration, served by every API server since 1.10.
var apiServicesGVR = schema.GroupVersionResource{Group: "apiregistration.k8s.io", Version: "v1", Resource: "apiservices"}

// isNamespaces reports whether a profile is the core Namespaces kind.
func isNamespaces(p *model.KindProfile) bool {
	return p != nil && p.Group == "" && p.Resource == namespacesKind && p.APIObject()
}

// blockerState is how far a kind of the list is read.
type blockerState int

const (
	blockerLoading blockerState = iota // the access review or the first list is still out
	blockerListed                      // the cache holds the kind's objects in the namespace
	blockerDenied                      // the user may not list it there (the review, or a refusal)
	blockerUnknown                     // Cogpit has no profile of the kind (a custom kind without its definition)
)

// blockerRead is how the list reads one kind the controller names, or the API services.
type blockerRead struct {
	gr      schema.GroupResource
	profile *model.KindProfile // nil: no profile (blockerUnknown); unused for the API services
	h       *cache.Handle      // nil while not read
	unsub   func()
	refused bool // the API server refused the read; it is not taken again while the set lives
}

// drop releases the read's handle.
func (r *blockerRead) drop() {
	if r.unsub != nil {
		r.unsub()
		r.unsub = nil
	}
	if r.h != nil {
		r.h.Release()
		r.h = nil
	}
}

// nsBlockers are the reads of one Terminating namespace's list. The drawer's set lives as long as
// the drawer shows the namespace and follows its conditions; a read without the drawer takes a set
// of its own for the call.
type nsBlockers struct {
	b        *Backend
	ns       string
	onChange func() // wakes the drawer; nil for a set of one call

	mu    sync.Mutex
	holds int
	reads map[schema.GroupResource]*blockerRead
	apis  *blockerRead // the API services, while discovery fails
}

func newNSBlockers(b *Backend, ns string, onChange func()) *nsBlockers {
	return &nsBlockers{b: b, ns: ns, onChange: onChange, holds: 1, reads: map[schema.GroupResource]*blockerRead{}}
}

// hold adds an owner, unless the set was released; a held set must be released once more.
func (n *nsBlockers) hold() bool {
	if n == nil {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.holds == 0 {
		return false
	}
	n.holds++
	return true
}

// release gives up one hold; with the last, every handle goes back (the informers stay warm).
func (n *nsBlockers) release() {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.holds == 0 {
		return
	}
	n.holds--
	if n.holds > 0 {
		return
	}
	for _, r := range n.reads {
		r.drop()
	}
	n.reads = nil
	if n.apis != nil {
		n.apis.drop()
		n.apis = nil
	}
}

// follow reads what d names and nothing else: a kind no longer named is released, a named one is
// taken once the access review allows it (or cannot tell), a refused one is not taken again.
func (n *nsBlockers) follow(d kinds.NamespaceDeletion) {
	if n == nil {
		return
	}
	want := map[schema.GroupResource]bool{}
	if d.Terminating {
		for _, r := range d.Remaining {
			want[r.GroupResource] = true
		}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.holds == 0 {
		return
	}
	for gr, r := range n.reads {
		if !want[gr] {
			r.drop()
			delete(n.reads, gr)
		}
	}
	for gr := range want {
		r := n.reads[gr]
		if r == nil {
			r = &blockerRead{gr: gr, profile: n.b.namespacedProfile(gr)}
			n.reads[gr] = r
		}
		n.takeLocked(r)
	}
	switch {
	case d.Terminating && d.DiscoveryFailure != "":
		if n.apis == nil {
			n.apis = &blockerRead{gr: apiServicesGVR.GroupResource()}
		}
		n.takeLocked(n.apis)
	case n.apis != nil:
		n.apis.drop()
		n.apis = nil
	}
}

// takeLocked starts a read when it may: a kind in the namespace, metadata only; the API services
// cluster-wide, whole (their status says which is not available).
func (n *nsBlockers) takeLocked(r *blockerRead) {
	if r.h != nil {
		if cache.IsRefused(r.h.Err()) { // a refused scope is not kept retrying (cache: not kept warm)
			r.drop()
			r.refused = true
		}
		return
	}
	if r.refused || n.stateLocked(r) != blockerLoading {
		return
	}
	var scope cache.Scope
	if r == n.apis {
		scope = cache.Scope{GVR: apiServicesGVR}
	} else {
		if n.b.listIn(r.gr, n.ns) == nsPending {
			return // the namespace's rules review is out: nothing a refusal would answer
		}
		scope = cache.Scope{GVR: r.profile.GVR(), Namespace: n.ns, MetadataOnly: true}
	}
	r.h = n.b.Cache.Acquire(scope)
	if n.onChange != nil {
		wake := n.onChange
		r.unsub = r.h.Subscribe(func(cache.Event) { wake() })
	}
}

// stateLocked says how far a read is.
func (n *nsBlockers) stateLocked(r *blockerRead) blockerState {
	if r == nil {
		return blockerLoading
	}
	apis := r == n.apis
	switch {
	case !apis && r.profile == nil:
		return blockerUnknown
	case r.refused, r.h != nil && cache.IsRefused(r.h.Err()):
		return blockerDenied
	case r.h != nil && r.h.HasSynced():
		return blockerListed
	case r.h != nil:
		return blockerLoading
	case apis:
		if n.b.apiServicesDenied() {
			return blockerDenied
		}
	case n.b.listIn(r.gr, n.ns) == nsDenied:
		return blockerDenied
	}
	return blockerLoading
}

// key changes whenever a read's state does: the drawer's poll wakes the drawer on it (an access
// review that answered, a first list that finished, a refusal).
func (n *nsBlockers) key() string {
	if n == nil {
		return ""
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	var b strings.Builder
	grs := make([]schema.GroupResource, 0, len(n.reads))
	for gr := range n.reads {
		grs = append(grs, gr)
	}
	slices.SortFunc(grs, func(a, b schema.GroupResource) int { return strings.Compare(a.String(), b.String()) })
	for _, gr := range grs {
		fmt.Fprintf(&b, "%s=%d;", gr, n.stateLocked(n.reads[gr]))
	}
	if n.apis != nil {
		fmt.Fprintf(&b, "apis=%d", n.stateLocked(n.apis))
	}
	return b.String()
}

// settled reports whether every read is listed, denied or unknown: nothing more will come by
// waiting.
func (n *nsBlockers) settled() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, r := range n.reads {
		if n.stateLocked(r) == blockerLoading {
			return false
		}
	}
	return n.apis == nil || n.stateLocked(n.apis) != blockerLoading
}

// waitSettled follows d until every read settled, bounded by ctx and syncTimeout: an agent's
// read (object_get) gets the list, not "loading".
func (n *nsBlockers) waitSettled(ctx context.Context, d kinds.NamespaceDeletion) {
	wctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	for {
		n.follow(d)
		if n.settled() {
			return
		}
		select {
		case <-wctx.Done():
			return
		case <-t.C:
		}
	}
}

// namespacedProfile returns the profile of a namespaced kind of the cluster's API by its resource
// and group, or nil.
func (b *Backend) namespacedProfile(gr schema.GroupResource) *model.KindProfile {
	for _, p := range b.AllProfiles() {
		if p.APIObject() && p.Namespaced && p.Group == gr.Group && p.Resource == gr.Resource {
			return p
		}
	}
	return nil
}

// listIn asks the access reviews whether the user may list a kind in one namespace (its rules
// review, started if needed).
func (b *Backend) listIn(gr schema.GroupResource, ns string) namespaceAnswer {
	return b.namespaceAccess(&model.KindProfile{Group: gr.Group, Resource: gr.Resource, Namespaced: true}, ns)
}

// apiServicesDenied reports whether the access reviews say the user may not list API services.
func (b *Backend) apiServicesDenied() bool {
	if b.Access == nil {
		return false
	}
	allowed, known := b.Access.Allowed("", Verb{Verb: verbList, Group: apiServicesGVR.Group, Resource: apiServicesGVR.Resource})
	return known && !allowed
}

// drawerBlockers returns the open drawer's list reads, held, when the drawer shows ref through p on
// b, else nil.
func (s *Session) drawerBlockers(b *Backend, p *model.KindProfile, ref model.ObjectRef) *nsBlockers {
	st := s.state
	st.mu.Lock()
	defer st.mu.Unlock()
	d := st.drawer
	if d == nil || d.backend != b || d.profile != p || !d.ref.Same(ref) || !d.blockers.hold() {
		return nil
	}
	return d.blockers
}

// namespaceBlocking adds a Terminating namespace's Blocking Deletion section to its drawer model,
// right after Properties. The open drawer's reads follow the namespace's conditions; a read that
// waits (object_get) waits, bounded, for its kinds' first lists.
func (s *Session) namespaceBlocking(ctx context.Context, b *Backend, p *model.KindProfile, ref model.ObjectRef, obj *unstructured.Unstructured, m *DetailModel, wait bool) {
	if !isNamespaces(p) || b.Cache == nil {
		return
	}
	d := kinds.NamespaceDeletionOf(obj)
	set := s.drawerBlockers(b, p, ref)
	if !d.Terminating {
		set.follow(d) // the drawer's reads go back (a namespace is never Active again, but be exact)
		set.release()
		return
	}
	if set == nil {
		set = newNSBlockers(b, ref.Name, nil)
	}
	defer set.release()
	set.follow(d)
	if wait {
		set.waitSettled(ctx, d)
	}
	// Right after Properties, before the Status of raw conditions: what the user acts on first.
	at := 0
	if len(m.Sections) > 0 && m.Sections[0].Title == model.TitleProperties {
		at = 1
	}
	m.Sections = slices.Insert(m.Sections, at, set.section(ref, d))
}

// section builds the Blocking Deletion section from the reads.
func (n *nsBlockers) section(nsRef model.ObjectRef, d kinds.NamespaceDeletion) model.DetailSection {
	n.mu.Lock()
	defer n.mu.Unlock()
	sec := model.DetailSection{Title: BlockingTitle}
	blocked, rows := false, 0
	for _, rem := range d.Remaining {
		r := n.reads[rem.GroupResource]
		var items []model.DetailItem
		var holds bool
		items, holds, rows = n.kindItemsLocked(nsRef, rem, r, rows)
		blocked = blocked || holds
		sec.Items = append(sec.Items, items...)
	}
	if d.DiscoveryFailure != "" {
		blocked = true
		sec.Items = append(sec.Items, n.apiItemsLocked(nsRef.Context)...)
	}
	if len(d.Failures) > 0 && !blocked {
		blocked = true
		types := make([]string, len(d.Failures))
		for i, f := range d.Failures {
			types[i] = f.Type
		}
		sec.Items = append(sec.Items, model.DetailItem{Name: "Controller", Value: "reports " + strings.Join(types, ", ") + " (Status above)"})
	}
	if !blocked {
		text := "Nothing blocks the deletion any more: the namespace controller removes the namespace by itself; no second Delete is needed."
		if !d.Reported {
			text = "The namespace controller has not reported what remains yet; it removes the namespace by itself once nothing holds it."
		}
		sec.Items = append(sec.Items, model.DetailItem{Value: text})
	}
	return sec
}

// kindItemsLocked lists one named kind's objects that hold the deletion, each with its finalizers
// and Remove finalizers, then a count of the kind's other objects still there; or a counted row
// from the condition when the kind is not listed. holds says whether anything of the kind is left.
func (n *nsBlockers) kindItemsLocked(nsRef model.ObjectRef, rem kinds.RemainingResource, r *blockerRead, rows int) (items []model.DetailItem, holds bool, _ int) {
	plural := rem.String()
	singular := plural
	if r != nil && r.profile != nil {
		plural = r.profile.Title
		singular = singularTitle(r.profile.Title)
		if r.profile.DetailTitle != "" {
			singular = r.profile.DetailTitle
		}
	}
	count := strconv.FormatInt(rem.Count, 10) + " remaining"
	switch n.stateLocked(r) {
	case blockerUnknown:
		return []model.DetailItem{{Name: plural, Value: count + "; Cogpit has no view of this kind (its definition is not listed)"}}, true, rows
	case blockerDenied:
		return []model.DetailItem{{Name: plural, Value: count + "; you may not list " + rem.String() + " in " + n.ns}}, true, rows
	case blockerLoading:
		return []model.DetailItem{{Name: plural, Value: count + "; loading…"}}, true, rows
	}
	objs := slices.Clone(r.h.ListNamespace(n.ns))
	slices.SortFunc(objs, func(a, b *unstructured.Unstructured) int { return strings.Compare(a.GetName(), b.GetName()) })
	others, omitted := 0, 0
	for _, o := range objs {
		fins := o.GetFinalizers()
		if o.GetDeletionTimestamp() == nil || len(fins) == 0 {
			others++
			continue
		}
		if rows >= maxBlockingRows {
			omitted++
			continue
		}
		rows++
		ref := model.ObjectRef{Context: nsRef.Context, Kind: r.profile.Key, Namespace: n.ns, Name: o.GetName()}
		params, _ := json.Marshal(RemoveFinalizersParams{Ref: ref, Finalizers: fins})
		chips := make([]model.Chip, len(fins))
		for i, f := range fins {
			chips[i] = model.Chip{Text: f}
		}
		items = append(items,
			model.DetailItem{Name: singular, Value: o.GetName(), Link: &ref, Actions: []model.ItemAction{
				{Label: "Remove Finalizers", Command: RemoveFinalizersCommand, Params: params},
			}},
			model.ChipsItem("Finalizers", chips))
	}
	if omitted > 0 {
		items = append(items, model.DetailItem{Name: plural, Value: strconv.Itoa(omitted) + " more with finalizers, not listed"})
	}
	if others > 0 {
		items = append(items, model.DetailItem{Name: plural, Value: strconv.Itoa(others) + " more without finalizers, not gone yet"})
	}
	return items, len(objs) > 0, rows
}

// apiItemsLocked names the API services that are not available, each with the Service that serves
// it, linked; or says why it cannot.
func (n *nsBlockers) apiItemsLocked(context string) []model.DetailItem {
	const name = "Discovery"
	switch n.stateLocked(n.apis) {
	case blockerDenied:
		return []model.DetailItem{{Name: name, Value: "failed for some API groups, so the namespace controller cannot delete their objects; you may not list API services, which would name the one not available"}}
	case blockerLoading:
		return []model.DetailItem{{Name: name, Value: "failed for some API groups; loading the API services…"}}
	}
	all := slices.Clone(n.apis.h.List())
	slices.SortFunc(all, func(a, b *unstructured.Unstructured) int { return strings.Compare(a.GetName(), b.GetName()) })
	var out []model.DetailItem
	for _, a := range all {
		reason, msg, ok := apiServiceDown(a)
		if !ok {
			continue
		}
		text := a.GetName() + " not available"
		if reason != "" {
			text += ": " + reason
		}
		if msg != "" {
			text += ": " + msg
		}
		out = append(out, model.DetailItem{Name: "API Service", Value: text})
		svcNS := model.NestedString(a.Object, "spec", "service", "namespace")
		svc := model.NestedString(a.Object, "spec", "service", "name")
		if svc != "" && svcNS != "" {
			out = append(out, model.DetailItem{Name: "Served by", Value: svcNS + "/" + svc,
				Link: &model.ObjectRef{Context: context, Kind: "services", Namespace: svcNS, Name: svc}})
		}
	}
	if len(out) == 0 {
		return []model.DetailItem{{Name: name, Value: "failed for some API groups (the namespace's conditions); every API service is available now, the controller tries again"}}
	}
	return out
}

// apiServiceDown reads an API service's Available condition: not True is down, with its reason and
// message.
func apiServiceDown(obj *unstructured.Unstructured) (reason, message string, down bool) {
	for _, c := range model.NestedSlice(obj.Object, "status", "conditions") {
		if model.NestedString(c, "type") != "Available" {
			continue
		}
		if model.NestedString(c, "status") == condTrue {
			return "", "", false
		}
		return model.NestedString(c, "reason"), model.NestedString(c, "message"), true
	}
	return "", "", false // no condition yet: the aggregator has not judged it
}
