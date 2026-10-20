package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Namespaced watches (G3; architecture §5, "Namespaced watches"). The engine reads a namespaced
// kind in all namespaces, one watch, unless the user may not list it there: then it reads the kind
// in each namespace the user named, one watch each, shared and reference-counted like every scope
// (cache.AcquireNamespaces), and the view merges them. Users who may list a kind cluster-wide keep
// the one cluster-wide watch: nothing here asks the API server a question of its own.
//
// Whether the user may list a kind in all namespaces comes, in this order, from:
//   - a refusal the API server already gave: a view's cluster-wide watch answered 403 Forbidden
//     (kept until the access reviews are refreshed or the cluster connected again);
//   - an access review asked about it anyway (cluster-wide list), when one answered;
//   - the context's default namespace's rules review, which connecting asks for: a rule naming the
//     kind allows it (a RoleBinding in that namespace could be what grants it; then the refusal
//     above corrects it once), no rule in complete rules denies it, incomplete rules allow it;
//   - nothing (a backend without access reviews, the demo cluster): allowed.
//
// Until the rules review answers, the kind's view waits (cache.Pending) instead of trying a
// cluster-wide list that may be refused; every other read (an agent's, a drawer's) reads in all
// namespaces, as before. The namespaces read are the cluster's accessible namespaces
// (ClusterSettings.AccessibleNamespaces) and the namespace filter's, else the context's own
// namespace (Cogpit's default, not the owner's decision; inferred to be Lens's). A namespace whose
// complete rules name the kind nowhere is not read, and the view says so (TableSnapshot.Denied);
// one the reviews cannot answer is read, and a refusal there shows as the view's error.

// nsState is what a backend knows for reading namespaced kinds namespace by namespace.
type nsState struct {
	in atomic.Pointer[nsInputs]

	// planMu orders noting a refusal (markRefused, exclusive) against acquiring a kind in all
	// namespaces by a plan made earlier (acquirePlanned, shared).
	planMu  sync.RWMutex
	mu      sync.Mutex
	refused map[schema.GroupVersionResource]bool // listing in all namespaces refused by the server (403)
	replan  chan struct{}                        // wakes the session's replan loop (attachRefusals); nil before
	quit    chan struct{}                        // ends that loop; closed by stop
}

// wakeReplan has the session read what is open again, once its replan loop runs.
func (n *nsState) wakeReplan() {
	n.mu.Lock()
	ch := n.replan
	n.mu.Unlock()
	if ch != nil {
		signal(ch)
	}
}

// stop ends the replan loop; the backend's close calls it. Stopping twice is harmless.
func (n *nsState) stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.quit != nil {
		close(n.quit)
		n.quit, n.replan = nil, nil
	}
}

// nsInputs are the namespaces the user named: the cluster's accessible namespaces and the
// namespace filter's.
type nsInputs struct {
	accessible []string
	filter     []string
}

// setInputs keeps the namespaces the session last showed; the session calls it whenever it
// reconciles the views with the UI state.
func (n *nsState) setInputs(accessible, filter []string) {
	if cur := n.in.Load(); cur != nil && slices.Equal(cur.accessible, accessible) && slices.Equal(cur.filter, filter) {
		return
	}
	n.in.Store(&nsInputs{accessible: slices.Clone(accessible), filter: slices.Clone(filter)})
}

func (n *nsState) markRefused(gvr schema.GroupVersionResource) bool {
	n.planMu.Lock()
	defer n.planMu.Unlock()
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.refused[gvr] {
		return false
	}
	if n.refused == nil {
		n.refused = map[schema.GroupVersionResource]bool{}
	}
	n.refused[gvr] = true
	return true
}

func (n *nsState) isRefused(gvr schema.GroupVersionResource) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.refused[gvr]
}

func (n *nsState) clearRefused() {
	n.mu.Lock()
	n.refused = nil
	n.mu.Unlock()
}

// nsPlan is how a kind is read: in all namespaces (the zero value), namespace by namespace, or
// not yet (pending: the access reviews have not said whether the user may list it everywhere).
type nsPlan struct {
	pending      bool
	perNamespace bool
	watch        []string // the namespaces read, sorted; perNamespace only
	denied       []string // the namespaces whose rules deny the kind, sorted; not read
}

// key tells plans apart: a view whose plan changes reads its kind again.
func (p nsPlan) key() string {
	switch {
	case p.pending:
		return "pending"
	case !p.perNamespace:
		return "all"
	}
	return "in:" + strings.Join(p.watch, ",") + ";denied:" + strings.Join(p.denied, ",")
}

// namespaceAnswer is what the access reviews say about one namespace.
type namespaceAnswer int

const (
	nsUnknown namespaceAnswer = iota // read it; the server decides
	nsAllowed
	nsDenied
	nsPending
)

// namespacePlan returns how the backend reads a kind now. It asks the API server nothing itself;
// a rules review it needs is started in the background, deduplicated, as Allowed does.
func (b *Backend) namespacePlan(p *model.KindProfile) nsPlan {
	// Helm's releases are planned as the Secrets they are read from (the profile's resource), and
	// their ConfigMaps part as the ConfigMaps (acquireReleases).
	if b == nil || p == nil || !p.Namespaced || p.Local || isApplications(p) {
		return nsPlan{}
	}
	denied, settled := b.clusterWideDenied(p)
	switch {
	case !settled:
		return nsPlan{pending: true}
	case !denied:
		return nsPlan{}
	}
	plan := nsPlan{perNamespace: true}
	for _, ns := range b.namedNamespaces() {
		switch b.namespaceAccess(p, ns) {
		case nsPending:
			return nsPlan{pending: true}
		case nsDenied:
			plan.denied = append(plan.denied, ns)
		default:
			plan.watch = append(plan.watch, ns)
		}
	}
	return plan
}

// namedNamespaces are the namespaces a kind the user may not list everywhere is read in: the
// cluster's accessible namespaces and the namespace filter's, else the context's own, sorted.
func (b *Backend) namedNamespaces() []string {
	var out []string
	if in := b.ns.in.Load(); in != nil {
		out = append(slices.Clone(in.accessible), in.filter...)
	}
	out = slices.DeleteFunc(out, func(ns string) bool { return ns == "" })
	if len(out) == 0 {
		ns := b.Namespace
		if ns == "" {
			ns = defaultNamespace
		}
		return []string{ns}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// clusterWideDenied says whether the user may not list a kind in all namespaces; settled is false
// while the access reviews that would answer it are pending.
func (b *Backend) clusterWideDenied(p *model.KindProfile) (denied, settled bool) {
	if b.ns.isRefused(p.GVR()) {
		return true, true
	}
	switch a := b.Access.(type) {
	case nil:
		return false, true
	case *accessReviews:
		return a.clusterWideDenied(p)
	default:
		allowed, known := a.Allowed("", Verb{Verb: verbList, Group: p.Group, Resource: p.Resource})
		return known && !allowed, true
	}
}

// namespaceAccess says whether the user may list a kind in one namespace.
func (b *Backend) namespaceAccess(p *model.KindProfile, ns string) namespaceAnswer {
	switch a := b.Access.(type) {
	case nil:
		return nsAllowed
	case *accessReviews:
		return a.namespaceAccess(p, ns)
	default:
		allowed, known := a.Allowed(ns, Verb{Verb: verbList, Group: p.Group, Resource: p.Resource})
		switch {
		case !known:
			return nsUnknown
		case allowed:
			return nsAllowed
		}
		return nsDenied
	}
}

// clusterWideDenied reads the answers the reviews hold (see the file's comment). It asks an access
// review of its own only for a user who named accessible namespaces, and only when the rules
// cannot tell: for everyone else, a user who may list a kind everywhere included, it costs nothing.
func (a *accessReviews) clusterWideDenied(p *model.KindProfile) (denied, settled bool) {
	q := cluster.AccessAttributes{Verb: verbList, Group: p.Group, Resource: p.Resource}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped || a.s == nil {
		return false, true
	}
	if c, ok := a.checks[q]; ok && c.done {
		return c.known && !c.allowed, true // a failed review cannot tell: read everywhere
	}
	r, pending := a.settledRulesLocked(a.namespace)
	switch {
	case pending:
		return false, false
	case r == nil || r.err != nil:
		return false, true // the reviews cannot tell: read everywhere, and a refusal corrects it
	}
	if m := r.rules.Match(q); m != cluster.RuleAllows {
		return !r.rules.Incomplete, true
	}
	// The context's namespace's rules allow it: a cluster-wide binding, or one in that namespace.
	// A cluster-wide binding's rules are in every namespace's rules: complete rules of another
	// namespace without it say no.
	for _, other := range a.rules {
		if other.done && other.err == nil && !other.rules.Incomplete && other.rules.Match(q) != cluster.RuleAllows {
			return true, true
		}
	}
	if len(a.named) == 0 {
		return false, true // no namespaces named: read everywhere, and a refusal corrects it
	}
	// The user named accessible namespaces: their rules may tell; else one access review does.
	for _, ns := range a.named {
		if _, pending := a.settledRulesLocked(ns); pending {
			return false, false
		}
	}
	a.checkLocked(q)
	if _, asked := a.checks[q]; !asked {
		return false, true // access reviews are failing or bounded: read everywhere
	}
	return false, false
}

// namespaceAccess reads one namespace's rules review, starting it if needed.
func (a *accessReviews) namespaceAccess(p *model.KindProfile, ns string) namespaceAnswer {
	q := cluster.AccessAttributes{Namespace: ns, Verb: verbList, Group: p.Group, Resource: p.Resource}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped || a.s == nil {
		return nsUnknown
	}
	if c, ok := a.checks[q]; ok && c.done && c.known {
		if c.allowed {
			return nsAllowed
		}
		return nsDenied
	}
	r, pending := a.settledRulesLocked(ns)
	switch {
	case pending:
		return nsPending
	case r == nil || r.err != nil:
		return nsUnknown
	}
	switch m := r.rules.Match(q); {
	case m == cluster.RuleAllows:
		return nsAllowed
	case r.rules.Incomplete:
		return nsUnknown
	}
	return nsDenied
}

// settledRulesLocked returns a namespace's finished rules review, or pending while it runs; a
// namespace not reviewed yet is started. Past the reviews' bound a namespace is never reviewed:
// nil and not pending (unknown).
func (a *accessReviews) settledRulesLocked(ns string) (r *rulesReview, pending bool) {
	if _, ok := a.rules[ns]; !ok && len(a.rules) >= accessRulesLimit {
		return nil, false
	}
	if r = a.rulesLocked(ns); r == nil {
		return nil, true
	}
	return r, false
}

// acquirePlanned returns a handle on a kind read as the plan says: in all namespaces, or in each
// of the plan's namespaces; a pending plan reads in all namespaces too (only the view waits).
//
// A plan to read in all namespaces that was made before the API server's refusal of that read was
// noted is stale: the refused read may have ended already (a refused scope is not kept warm), and
// acquiring it would send the refused read again. The check and the acquire hold off noting a
// refusal (planMu), and the cache tells of a refusal before a reader can see it and end the read
// (cache.OnRefused), so a plan found current here either joins the read still running or starts
// the first one. Before (2026-10-20) a reader that had planned while another reader's read was
// refused and ended started it again (TestPrometheusDetectionFollowsARefusal, CI: Prometheus's
// detection after the Alertmanager's lookup of the Services).
func (b *Backend) acquirePlanned(p *model.KindProfile, plan nsPlan) *cache.Handle {
	if plan.perNamespace {
		return b.Cache.AcquireNamespaces(b.Scope(p), plan.watch)
	}
	b.ns.planMu.RLock()
	defer b.ns.planMu.RUnlock()
	if b.ns.isRefused(p.GVR()) {
		if now := b.namespacePlan(p); now.perNamespace {
			return b.Cache.AcquireNamespaces(b.Scope(p), now.watch)
		}
	}
	return b.Cache.Acquire(b.Scope(p))
}

// planWait bounds how long a read that waits anyway waits for a kind's plan.
const planWait = 10 * time.Second

// acquireSettled is Acquire for a read that waits for the cache anyway (an agent's, the CLI's, a
// drawer's for an agent): it first waits, bounded by ctx and planWait, for the access reviews to
// say whether the kind can be listed in all namespaces, so that a user who may not is not sent a
// cluster-wide list the server refuses. Without an answer by then it reads in all namespaces.
func (b *Backend) acquireSettled(ctx context.Context, p *model.KindProfile) *cache.Handle {
	plan := b.settledPlan(ctx, p)
	if plan.pending || p.Local || isApplications(p) || isHelmReleases(p) { // their own caches (Acquire)
		return b.Acquire(p)
	}
	return b.acquirePlanned(p, plan)
}

// settledPlan is the kind's plan once the access reviews decided it, waiting for them bounded by
// ctx and planWait; still pending without an answer by then.
func (b *Backend) settledPlan(ctx context.Context, p *model.KindProfile) nsPlan {
	plan := b.namespacePlan(p)
	if !plan.pending {
		return plan
	}
	wctx, cancel := context.WithTimeout(ctx, planWait)
	defer cancel()
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for plan.pending && wctx.Err() == nil {
		select {
		case <-wctx.Done():
		case <-t.C:
			plan = b.namespacePlan(p)
		}
	}
	return plan
}

// acquireIn is acquireSettled for a read of one namespace's objects (one object, a workload's
// logs, a port forward's pods and service): once the plan is decided, a user who may not list the
// kind in all namespaces gets that namespace alone, shared with every other reader of it, whether
// the namespaces the plan names include it or not. Cogpit's default (2026-10-13, not the owner's
// decision): the target is what the read is about, and the named namespaces (the accessible ones,
// the filter's, else the context's own) can miss it. A user who may list the kind everywhere, a
// cluster-scoped kind (namespace "") and a plan still undecided read as acquireSettled does.
func (b *Backend) acquireIn(ctx context.Context, p *model.KindProfile, namespace string) *cache.Handle {
	plan := b.settledPlan(ctx, p)
	switch {
	case plan.pending || p.Local || isApplications(p) || isHelmReleases(p): // their own caches (Acquire)
		return b.Acquire(p)
	case plan.perNamespace && namespace != "":
		return b.Cache.AcquireNamespaces(b.Scope(p), []string{namespace})
	}
	return b.acquirePlanned(p, plan)
}

// refusedIn is the API server's refusal of a handle's namespace that acquireIn read (403
// Forbidden: the user may not list the kind there either), or nil. A merged handle counts as
// synced once its namespace was refused, so a reader would see no objects instead of the reason.
func refusedIn(h *cache.Handle, kind string) error {
	if err := h.Err(); cache.IsRefused(err) {
		return fmt.Errorf("%s: %w", kind, err)
	}
	return nil
}

// acquireForView is the open view's handle on its kind: as plan says, and nothing while the plan
// is pending; local kinds and the Applications as Acquire serves them.
func (b *Backend) acquireForView(p *model.KindProfile, plan nsPlan) *cache.Handle {
	switch {
	case plan.pending:
		return cache.Pending(b.Scope(p))
	case p.Local || isApplications(p) || isHelmReleases(p): // the release Secrets and ConfigMaps joined (helmdriver.go)
		return b.Acquire(p)
	}
	return b.acquirePlanned(p, plan)
}

// namespacesRunning reports whether a kind read namespace by namespace is cached in every one of
// its namespaces, without starting a watch (Backend.Cached: a search across kinds reads only what
// runs).
func (b *Backend) namespacesRunning(p *model.KindProfile) bool {
	plan := b.namespacePlan(p)
	if !plan.perNamespace || len(plan.watch) == 0 {
		return false
	}
	for _, ns := range plan.watch {
		sc := b.Scope(p)
		sc.Namespace = ns
		if !b.Cache.Running(sc) {
			return false
		}
	}
	return true
}

// drawerPlanKey is how the drawer of an object of kind p in namespace reads its kind, the events
// and its related objects (acquireNow); a drawer whose key changes starts again. A change of the
// plan's namespaces that leaves the object's namespace read as it was (a filter) changes nothing.
func (b *Backend) drawerPlanKey(p *model.KindProfile, namespace string) string {
	if p == nil {
		return ""
	}
	if !p.Namespaced {
		namespace = ""
	}
	key := b.readKeyIn(p, namespace)
	if ep, ok := b.Profile(eventsKind); ok && p.APIObject() {
		key += "|" + b.readKeyIn(ep, namespace)
	}
	return key + b.relatedKey(p, drawerNeeds(b, p), namespace)
}

// relatedKey is how the related-objects indexes of a kind's view (p.Needs, namespace "") or of a
// drawer (p.DrawerNeeds: its own indexes too, a workload's pods) read their kinds; a view or
// drawer whose key changes starts again, so that an index's read the API server refused, or one
// taken while its kind's plan was still pending, is taken again namespace by namespace.
func (b *Backend) relatedKey(p *model.KindProfile, needs []model.RelatedIndex, namespace string) string {
	var sb strings.Builder
	seen := map[string]bool{}
	for _, ix := range needs {
		rp, ok := b.Profile(ix.Kind)
		if !ok || seen[ix.Kind] {
			continue
		}
		seen[ix.Kind] = true
		if ix.Kind == kinds.KeyCustomResourceDefinitions && b.custom.refused() {
			sb.WriteString("|" + ix.Kind + "=refused") // newRelatedSetWith reads them no more
			continue
		}
		sb.WriteString("|" + ix.Kind + "=" + b.readKeyIn(rp, namespace))
	}
	if isHelmReleases(p) && b.servesConfigMaps() { // the release ConfigMaps (acquireReleases)
		sb.WriteString("|" + configMapsKind + "=" + b.releasePartPlan(p, 1).key())
	}
	return sb.String()
}

// readKey is how a handle Acquire takes on a kind reads it now: in all namespaces (a pending plan
// too, which Acquire reads in all namespaces) or namespace by namespace in the plan's namespaces.
func (b *Backend) readKey(p *model.KindProfile) string {
	if plan := b.namespacePlan(p); plan.perNamespace {
		return plan.key()
	}
	return "all"
}

// followRefusal reads the view's kind namespace by namespace once the API server refused its
// cluster-wide watch (403 Forbidden), though the access reviews allowed it or could not tell (a
// RoleBinding in the context's own namespace, an authorizer that cannot list its rules).
func (w *viewWorker) followRefusal(err error) {
	if w.plan.pending || w.plan.perNamespace || !cache.IsRefused(err) {
		return
	}
	w.backend.followRefused(w.profile)
}

// noteRefused records that the API server refused to list a kind in all namespaces (a view's
// watch answered 403 Forbidden): from now on the kind is read namespace by namespace, until the
// access reviews are refreshed. It reports whether that is news.
func (b *Backend) noteRefused(p *model.KindProfile) bool {
	if b == nil || p == nil || !p.Namespaced || p.Local {
		return false
	}
	return b.ns.markRefused(p.GVR())
}

// replan reconciles the views with the latest UI state after something a namespace plan depends
// on changed outside it: answers of the access reviews, a refusal of the API server.
func (st *state) replan(b *Backend) {
	st.mu.Lock()
	if st.closed || b == nil || st.shown != b {
		st.mu.Unlock()
		return
	}
	now := st.s.UIState()
	if now.Revision > st.appliedRev {
		st.appliedRev = now.Revision
	}
	st.reconcileLocked(now)
	st.mu.Unlock()
	b.apps.wake() // the Applications' own reads of the workloads (applications.go followPlans)
}

// replanPending is replan for a view still waiting for its plan: a rules review answered.
func (st *state) replanPending(b *Backend) {
	st.mu.Lock()
	waiting := st.view != nil && st.view.backend == b && st.view.plan.pending
	st.mu.Unlock()
	if waiting {
		st.replan(b)
	}
}
