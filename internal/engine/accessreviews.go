package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Bounds of the access reviews (gaps G2 and G4; architecture §5).
const (
	// accessConcurrency bounds the reviews in flight per cluster.
	accessConcurrency = 4
	// accessNamespaceLimit bounds the namespaces the sidebar reviews when the user may list
	// namespaces: on a cluster with more, a kind no reviewed namespace lets the user list stays
	// shown, because its answer is unknown.
	accessNamespaceLimit = 32
	// accessRulesLimit and accessChecksLimit bound the namespaces and the questions the reviews
	// keep answers for until a refresh: commands ask in whatever namespace their parameters name.
	// Past them an answer stays unknown and the API server decides.
	accessRulesLimit  = 512
	accessChecksLimit = 2048
	// accessReviewTimeout bounds one review.
	accessReviewTimeout = 30 * time.Second
	// accessCoalesce stretches the snapshot interval for the events that follow arriving answers:
	// connecting may answer dozens of reviews within a second, and each event redraws the sidebar.
	accessCoalesce = 2
	// namespacesKind is the kind whose objects are the namespaces a user may list.
	namespacesKind = "namespaces"
	// defaultNamespace is a context's default namespace when its kubeconfig names none.
	defaultNamespace = "default"
	// verbList is the verb the sidebar asks about.
	verbList = "list"
)

// Errors of reviews that ended without an answer.
var (
	// errAccessReviewAborted: a panic in the reviewer.
	errAccessReviewAborted = errors.New("the access review ended without an answer")
	// errAccessReviewsFailing: another access review failed while this one waited for its turn.
	errAccessReviewsFailing = errors.New("access reviews are failing; not asked until refreshed")
)

// accessReviewer runs the access reviews of one connection. *cluster.Conn is one; tests
// substitute fakes.
type accessReviewer interface {
	ReviewRules(ctx context.Context, namespace string) (cluster.AccessRules, error)
	ReviewAccess(ctx context.Context, a cluster.AccessAttributes) (allowed, known bool, err error)
}

// accessReviews is the Access of a real cluster: the connected user's access reviews, cached for
// the backend's context per namespace (SelfSubjectRulesReview) and per question
// (SelfSubjectAccessReview), and kept until the user refreshes them (cluster.refreshAccess) or
// connects again. Nothing is polled.
//
// Allowed and listable never block and never make a request themselves: an answer they lack is
// unknown, and the review that would give it starts in the background, once per namespace or
// question (deduplicated), at most accessConcurrency at a time, on goroutines that recover from a
// panic. When answers arrive, the session emits EventKinds (the sidebar reads kinds.list again)
// and EventDetail (the drawer's toolbar asks Enabled again), coalesced.
//
// Which review answers what:
//
//   - A verb in a namespace: that namespace's rules. A rule naming it allows it; no rule naming it
//     denies it, unless the rules are incomplete (an authorizer that cannot list its rules, such
//     as a cloud provider's IAM webhook), and then one access review asks.
//   - A verb on a cluster-scoped resource, or in every namespace: the default namespace's rules
//     deny it when they are complete and name it nowhere, because every namespace's rules carry the
//     cluster-wide bindings' rules. When they name it, a binding in that namespace may be what does
//     (Kubernetes' view role names namespaces), so one access review asks.
//   - Rules limited to named objects answer nothing (the question names no object), and wildcard
//     verbs, groups or resources go to an access review.
type accessReviews struct {
	rev       accessReviewer
	namespace string // the context's default namespace

	ctx    context.Context // cancelled by stop: reviews in flight end
	cancel context.CancelFunc
	sem    chan struct{}
	wake   chan struct{}
	quit   chan struct{}

	mu       sync.Mutex
	s        *Session // set by attach, when the backend is published
	b        *Backend
	stopped  bool
	rules    map[string]*rulesReview
	checks   map[cluster.AccessAttributes]*accessCheck
	filter   []string      // the namespace filter last seen
	named    []string      // the cluster's accessible namespaces last seen (accessiblens_plan.go)
	failing  bool          // an access review failed: ask no more until refreshed (checkLocked)
	nsHandle *cache.Handle // the namespaces, held once the sidebar needs them (visibleLocked)
	nsUnsub  func()
	uiUnsub  func()

	// The connection log's line for the rules reviews a connect starts (noteFirst, D-66).
	firstNote func(level, text string, took time.Duration)
	first     *firstRules
}

// firstRules are the rules reviews attach started, until each answered.
type firstRules struct {
	pending map[string]bool
	total   int
	failed  []string // their errors
	start   time.Time
}

// noteFirst has the reviews tell note how the rules reviews of the connect went, once they all
// answered: their number and time, or which failed. Call it before attach.
func (a *accessReviews) noteFirst(note func(level, text string, took time.Duration)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.firstNote = note
}

// firstDoneLocked takes the answer of namespace ns's rules review for the connect's line and
// returns the line's note once every first review answered; nil until then.
func (a *accessReviews) firstDoneLocked(ns string, err error) func() {
	f, note := a.first, a.firstNote
	if f == nil || note == nil || !f.pending[ns] {
		return nil
	}
	delete(f.pending, ns)
	if err != nil {
		f.failed = append(f.failed, oneLine(err.Error()))
	}
	if len(f.pending) > 0 {
		return nil
	}
	a.first = nil
	took := time.Since(f.start)
	if len(f.failed) == 0 {
		text := fmt.Sprintf("Access reviews: the rules of %d namespace(s) answered", f.total)
		return func() { note(LogInfo, text, took) }
	}
	text := fmt.Sprintf("Access reviews: %d of %d rules reviews failed (the commands' access is then asked per verb): %s",
		len(f.failed), f.total, f.failed[0])
	return func() { note(LogInfo, text, took) } // the connection works and is safe: not a WARN (connectlog.go)
}

// rulesReview is one namespace's rules review: pending until done.
type rulesReview struct {
	done  bool
	err   error
	rules cluster.AccessRules
}

// accessCheck is one access review: pending until done; a failed or uncertain one is done and
// not known.
type accessCheck struct {
	done, known, allowed bool
}

// newAccessReviews returns the access reviews of a connection whose context's default namespace
// is namespace ("" means default). They start when the session publishes the backend (attach).
func newAccessReviews(rev accessReviewer, namespace string) *accessReviews {
	if namespace == "" {
		namespace = defaultNamespace
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &accessReviews{
		rev: rev, namespace: namespace, ctx: ctx, cancel: cancel,
		sem:    make(chan struct{}, accessConcurrency),
		wake:   make(chan struct{}, 1),
		quit:   make(chan struct{}),
		rules:  map[string]*rulesReview{},
		checks: map[cluster.AccessAttributes]*accessCheck{},
	}
}

// attachAccess starts the access reviews of a backend that has them. It runs before the backend
// is published (st.mu held), like attachCustomKinds.
func (s *Session) attachAccess(b *Backend) {
	if a, ok := b.Access.(*accessReviews); ok {
		a.attach(s, b)
	}
}

// attach binds the reviews to the session and prefetches what connecting needs: the default
// namespace's rules, which answer the sidebar for most users and the commands in that namespace.
// The namespace filter's namespaces follow with the UI state; the cluster's accessible namespaces
// are prefetched too (accessiblens_plan.go reads them).
func (a *accessReviews) attach(s *Session, b *Backend) {
	named := s.UIState().ClusterSettings[b.Context].AccessibleNamespaces // before a.mu: emit takes no lock, UIState s.mu
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped || a.s != nil {
		return
	}
	a.s, a.b = s, b
	a.named = slices.Clone(named)
	interval := s.opts.SnapshotInterval * accessCoalesce
	s.goSafe("access reviews", func() { runCoalesced(a.quit, a.wake, interval, nil, a.changed) })
	a.uiUnsub = s.Subscribe(a.onEvent)
	if a.firstNote != nil { // the connection log's line once these answered (noteFirst)
		f := &firstRules{pending: map[string]bool{}, start: time.Now()}
		for _, ns := range a.prefetchLocked() {
			if r, ok := a.rules[ns]; ok && !r.done {
				f.pending[ns] = true
			}
		}
		if f.total = len(f.pending); f.total > 0 {
			a.first = f
		}
		return
	}
	a.prefetchLocked()
}

// stop ends the reviews in flight and releases what the reviews hold. The backend's Close calls it.
func (a *accessReviews) stop() {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return
	}
	a.stopped = true
	h, nsUnsub, uiUnsub := a.nsHandle, a.nsUnsub, a.uiUnsub
	a.nsHandle, a.nsUnsub, a.uiUnsub = nil, nil, nil
	a.mu.Unlock()
	a.cancel()
	close(a.quit)
	for _, fn := range []func(){nsUnsub, uiUnsub} {
		if fn != nil {
			fn()
		}
	}
	if h != nil {
		h.Release()
	}
}

// refresh drops every answer and asks again, starting with the default namespace's and the
// namespace filter's rules; it returns those namespaces. Menus and the sidebar re-evaluate at
// once (the answers are unknown until the reviews land) and again when they land.
func (a *accessReviews) refresh() []string {
	a.mu.Lock()
	if a.stopped || a.s == nil {
		a.mu.Unlock()
		return nil
	}
	a.rules = map[string]*rulesReview{}
	a.checks = map[cluster.AccessAttributes]*accessCheck{}
	a.failing = false
	h, unsub := a.nsHandle, a.nsUnsub
	a.nsHandle, a.nsUnsub = nil, nil
	namespaces := a.prefetchLocked()
	a.mu.Unlock()
	if unsub != nil {
		unsub()
	}
	if h != nil {
		h.Release()
	}
	signal(a.wake)
	return namespaces
}

// forgetFailures drops the answers that failed and lifts "no more until refreshed": the reviews
// sent while the connection was interrupted failed for that reason alone (D-58). The answers that
// came are kept; the rules of the namespaces Cogpit was told about are asked again at once, the
// other questions when they come up.
func (a *accessReviews) forgetFailures() {
	a.mu.Lock()
	if a.stopped || a.s == nil {
		a.mu.Unlock()
		return
	}
	changed := a.failing
	a.failing = false
	for ns, r := range a.rules {
		if r.done && r.err != nil {
			delete(a.rules, ns)
			changed = true
		}
	}
	for q, c := range a.checks {
		if c.done && !c.known {
			delete(a.checks, q)
			changed = true
		}
	}
	if changed {
		a.prefetchLocked()
	}
	a.mu.Unlock()
	if changed {
		signal(a.wake)
	}
}

// prefetchLocked starts the rules reviews of the default namespace and the namespace filter.
func (a *accessReviews) prefetchLocked() []string {
	namespaces := a.seenLocked(a.filter)
	for _, ns := range namespaces {
		a.rulesLocked(ns)
	}
	return namespaces
}

// seenLocked returns the namespaces Cogpit was told about: the context's default namespace, the
// cluster's accessible namespaces and the namespace filter's.
func (a *accessReviews) seenLocked(filter []string) []string {
	out := []string{a.namespace}
	for _, ns := range slices.Concat(a.named, filter) {
		if ns != "" && !slices.Contains(out, ns) {
			out = append(out, ns)
		}
	}
	return out
}

// onEvent follows the namespace filter and the cluster's accessible namespaces: their rules are
// reviewed when they change.
func (a *accessReviews) onEvent(ev Event) {
	if ev.Type != EventUIState || ev.UIState == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped || a.b == nil || ev.UIState.Context != a.b.Context {
		return
	}
	named := ev.UIState.ClusterSettings[a.b.Context].AccessibleNamespaces
	if slices.Equal(ev.UIState.Namespaces, a.filter) && slices.Equal(named, a.named) {
		return
	}
	a.filter, a.named = slices.Clone(ev.UIState.Namespaces), slices.Clone(named)
	a.prefetchLocked()
}

// changed tells the session that answers arrived (the coalescing loop calls it).
func (a *accessReviews) changed() {
	a.mu.Lock()
	s, b, stopped := a.s, a.b, a.stopped
	a.mu.Unlock()
	if !stopped && s != nil {
		s.accessChanged(b)
	}
}

// accessChanged follows new answers of the connected backend's access reviews: the sidebar reads
// kinds.list again (EventKinds) and the drawer's toolbar asks its commands' Enabled again
// (EventDetail). Row menus ask Enabled whenever they open. The open view and drawer read their kind
// again when the answers change how (accessiblens_plan.go).
func (s *Session) accessChanged(b *Backend) {
	b.nsWatch.poke() // may the user list the namespaces (nswatch.go); nil-safe
	if s.Backend() != b {
		return
	}
	s.state.replan(b)
	s.emit(Event{Type: EventKinds})
	s.emit(Event{Type: EventDetail})
}

// Allowed implements Access.
func (a *accessReviews) Allowed(namespace string, v Verb) (allowed, known bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.allowedLocked(cluster.AccessAttributes{
		Namespace: namespace, Verb: v.Verb, Group: v.Group, Resource: v.Resource, Subresource: v.Subresource,
	})
}

func (a *accessReviews) allowedLocked(q cluster.AccessAttributes) (allowed, known bool) {
	if a.stopped {
		return false, false
	}
	if c, ok := a.checks[q]; ok {
		return c.allowed && c.known, c.done && c.known
	}
	if q.Verb == "*" || q.Group == "*" || q.Resource == "*" || q.Resource == "" {
		a.checkLocked(q)
		return false, false
	}
	ns := q.Namespace
	if ns == "" {
		ns = a.namespace // any namespace's rules carry the cluster-wide ones
	}
	r := a.rulesLocked(ns)
	switch {
	case r == nil:
		return false, false
	case r.err != nil:
		a.checkLocked(q)
		return false, false
	}
	switch r.rules.Match(q) {
	case cluster.RuleAllows:
		if q.Namespace != "" {
			return true, true
		}
		a.checkLocked(q)
		return false, false
	case cluster.RuleNamesOnly:
		return false, false
	}
	if r.rules.Incomplete {
		a.checkLocked(q)
		return false, false
	}
	return false, true
}

// listable answers kinds.list for the sidebar: whether the user may list a kind (resource-parity
// › Every kind, "hidden otherwise"). It reads only answers that are cached or cheap: a
// cluster-scoped kind is listable when the default namespace's rules name it (one RoleBinding
// naming a cluster-scoped kind is told apart only where an access review was asked anyway, for
// namespaces); a namespaced kind is listable when the rules of any namespace reviewed so far
// name it, and not listable only when the rules of every namespace the user can see are complete
// and name it nowhere. The namespaces the user can see are the default namespace and the
// namespace filter's; when the user may list namespaces (one access review asks), also the
// namespaces the cache lists, up to accessNamespaceLimit (more, and the answer stays unknown).
// The reviews this needs start in the background.
func (a *accessReviews) listable(p *model.KindProfile, filter []string) (listable, known bool) {
	q := cluster.AccessAttributes{Verb: verbList, Group: p.Group, Resource: p.Resource}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return false, false
	}
	if c, ok := a.checks[q]; ok && c.done && c.known && (c.allowed || !p.Namespaced) {
		return c.allowed, true
	}
	if !p.Namespaced {
		r := a.rulesLocked(a.namespace)
		if r == nil || r.err != nil {
			return false, false
		}
		switch r.rules.Match(q) {
		case cluster.RuleAllows:
			return true, true
		case cluster.RuleNone:
			return false, !r.rules.Incomplete
		}
		return false, false
	}
	for _, r := range a.rules {
		if r.done && r.err == nil && r.rules.Match(q) == cluster.RuleAllows {
			return true, true
		}
	}
	visible, all := a.visibleLocked(filter)
	unknown := !all
	for _, ns := range visible {
		r := a.rulesLocked(ns)
		if r == nil || r.err != nil || r.rules.Incomplete || r.rules.Match(q) != cluster.RuleNone {
			unknown = true
		}
	}
	return false, !unknown
}

// visibleLocked returns the namespaces the user can see, and whether that is all of them: the
// namespaces Cogpit was told about, and, if the user may list namespaces, those the cache lists.
// all is false while that is unknown, or when there are more than accessNamespaceLimit.
func (a *accessReviews) visibleLocked(filter []string) (namespaces []string, all bool) {
	out := a.seenLocked(filter)
	canList, known := a.allowedLocked(cluster.AccessAttributes{Verb: verbList, Resource: namespacesKind})
	switch {
	case !known:
		return out, false
	case !canList:
		return out, true
	}
	names, ok := a.namespaceNamesLocked()
	if !ok || len(names) > accessNamespaceLimit {
		return out, false
	}
	for _, ns := range names {
		if !slices.Contains(out, ns) {
			out = append(out, ns)
		}
	}
	return out, true
}

// namespaceNamesLocked returns the namespaces the cache lists, sorted, once it has listed them.
// The first call acquires a handle on them for as long as the backend is connected (one watch,
// shared with the namespace filter's reads): a namespace created later is reviewed when the
// sidebar asks again, which the handle's events make it do.
func (a *accessReviews) namespaceNamesLocked() ([]string, bool) {
	if a.nsHandle == nil {
		if a.b == nil || a.s == nil {
			return nil, false
		}
		p, ok := a.b.Profile(namespacesKind)
		if !ok || p.Local {
			return nil, false
		}
		h := a.b.Acquire(p)
		a.nsHandle = h
		a.nsUnsub = h.Subscribe(func(cache.Event) { signal(a.wake) })
		ctx := a.ctx
		a.s.goSafe("access reviews: namespaces", func() {
			if h.WaitForSync(ctx) {
				signal(a.wake)
			}
		})
	}
	if !a.nsHandle.HasSynced() {
		return nil, false
	}
	objs := a.nsHandle.List()
	names := make([]string, 0, len(objs))
	for _, o := range objs {
		names = append(names, o.GetName())
	}
	slices.Sort(names)
	return names, true
}

// rulesLocked returns a namespace's finished rules review, or nil while it is pending; a missing
// one is started.
func (a *accessReviews) rulesLocked(ns string) *rulesReview {
	r, ok := a.rules[ns]
	if !ok {
		if a.stopped || a.s == nil || len(a.rules) >= accessRulesLimit {
			return nil
		}
		r = &rulesReview{}
		a.rules[ns] = r
		a.s.goSafe("access review", func() { a.reviewRules(ns, r) })
		return nil
	}
	if !r.done {
		return nil
	}
	return r
}

// checkLocked starts an access review for q unless one ran or runs, or an access review failed
// (a server that does not answer them, or forbids them): then every question would cost a failing
// request, so the answer stays unknown until the reviews are refreshed.
func (a *accessReviews) checkLocked(q cluster.AccessAttributes) {
	if _, ok := a.checks[q]; ok || a.failing || a.stopped || a.s == nil || len(a.checks) >= accessChecksLimit {
		return
	}
	c := &accessCheck{}
	a.checks[q] = c
	a.s.goSafe("access review", func() { a.reviewAccess(q, c) })
}

// acquire waits for a free review slot; false when the reviews were stopped.
func (a *accessReviews) acquire() bool {
	select {
	case a.sem <- struct{}{}:
		return true
	case <-a.ctx.Done():
		return false
	}
}

func (a *accessReviews) release() { <-a.sem }

// reviewRules runs one namespace's rules review. The deferred finish records an answer even when
// the reviewer panics (goSafe then logs the panic), so the namespace never stays pending.
func (a *accessReviews) reviewRules(ns string, r *rulesReview) {
	var (
		rules cluster.AccessRules
		err   = errAccessReviewAborted
	)
	defer func() { a.finishRules(ns, r, rules, err) }()
	if !a.acquire() {
		err = a.ctx.Err()
		return
	}
	defer a.release()
	ctx, cancel := context.WithTimeout(a.ctx, accessReviewTimeout)
	defer cancel()
	rules, err = a.rev.ReviewRules(ctx, ns)
}

func (a *accessReviews) finishRules(ns string, r *rulesReview, rules cluster.AccessRules, err error) {
	a.mu.Lock()
	if a.stopped || a.rules[ns] != r {
		a.mu.Unlock()
		return // stopped or refreshed meanwhile
	}
	r.done, r.rules, r.err = true, rules, err
	s, b := a.s, a.b
	note := a.firstDoneLocked(ns, err)
	a.mu.Unlock()
	if note != nil {
		note() // the connection log's line for the connect's reviews (noteFirst)
	}
	if err != nil {
		s.log.Warn("engine: rules review failed; access reviews ask per verb instead", "context", b.Context, "namespace", ns, "err", err)
	}
	signal(a.wake)
	s.state.replanPending(b) // a view waiting for these rules reads its kind at once (accessiblens_plan.go)
}

// reviewAccess runs one access review, recording an answer even when the reviewer panics.
func (a *accessReviews) reviewAccess(q cluster.AccessAttributes, c *accessCheck) {
	var (
		allowed, known bool
		err            = errAccessReviewAborted
	)
	acquired := false
	defer func() {
		a.finishAccess(q, c, allowed, known, err)
		if acquired {
			// After the answer is recorded: a review waiting for this slot sees a failure and is
			// not sent (released first, one more slipped through: TestBudgetWithoutAccessReviews).
			a.release()
		}
	}()
	if acquired = a.acquire(); !acquired {
		err = a.ctx.Err()
		return
	}
	a.mu.Lock()
	failing := a.failing
	a.mu.Unlock()
	if failing {
		err = errAccessReviewsFailing // queued before another failed: do not send it
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, accessReviewTimeout)
	defer cancel()
	allowed, known, err = a.rev.ReviewAccess(ctx, q)
}

func (a *accessReviews) finishAccess(q cluster.AccessAttributes, c *accessCheck, allowed, known bool, err error) {
	a.mu.Lock()
	if a.stopped || a.checks[q] != c {
		a.mu.Unlock()
		return
	}
	c.done, c.allowed, c.known = true, allowed, known && err == nil
	s, b := a.s, a.b
	first := err != nil && !a.failing
	a.failing = a.failing || err != nil
	a.mu.Unlock()
	if first {
		s.log.Warn("engine: access review failed; no more until refreshed, the API server decides", "context", b.Context, "review", q.String(), "err", err)
	}
	signal(a.wake)
	s.state.replanPending(b) // a view waiting for this answer reads its kind at once (accessiblens_plan.go)
}

// Listable reports whether the connected user may list a kind, for the sidebar and kinds.list
// (resource-parity › Every kind: a kind the user may not list is hidden). filter is the namespace
// filter. known is false while the access reviews that would answer it are pending or cannot
// answer it, and those start in the background; an unknown kind is shown. Local kinds and
// backends without access reviews (the demo cluster) list everything.
func (b *Backend) Listable(p *model.KindProfile, filter []string) (listable, known bool) {
	if p.Local {
		return true, true
	}
	if isApplications(p) { // the workloads it is made of decide (applications.go)
		return b.applicationsListable(filter)
	}
	if isHelmReleases(p) { // the release Secrets or ConfigMaps decide (helmdriver.go)
		return b.releasesListable(p, filter)
	}
	switch a := b.Access.(type) {
	case nil:
		return true, true
	case *accessReviews:
		return a.listable(p, filter)
	default:
		// Another Access: ask about every namespace (or the cluster-scoped kind). A namespaced kind
		// the user may not list everywhere may still be listable somewhere.
		allowed, known := a.Allowed("", Verb{Verb: verbList, Group: p.Group, Resource: p.Resource})
		if p.Namespaced && !allowed {
			return false, false
		}
		return allowed, known
	}
}

// RefreshAccess drops the connected user's access-review answers and asks again
// (cluster.refreshAccess): after an administrator changed the user's roles. It returns the
// namespaces whose rules are reviewed at once, and false for a backend without access reviews
// (the demo cluster).
func (b *Backend) RefreshAccess() (namespaces []string, ok bool) {
	b.ns.clearRefused() // the server's refusals are asked again too (accessiblens_plan.go)
	if b.custom != nil {
		b.custom.retake() // the custom resource definitions, after a refused list (customkinds.go)
	}
	b.nsWatch.retake() // the namespaces, after a refused list (nswatch.go); nil-safe
	a, ok := b.Access.(*accessReviews)
	if !ok {
		return nil, false
	}
	return a.refresh(), true
}

// HasAccessReviews reports whether the backend's Access is the cluster's access reviews, which
// RefreshAccess can refresh.
func (b *Backend) HasAccessReviews() bool {
	_, ok := b.Access.(*accessReviews)
	return ok
}
