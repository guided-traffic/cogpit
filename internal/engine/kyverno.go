package engine

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Kyverno (D-79; beyond-lens › Integrations; the owner, 2026-10-17: "ich will an den K8S-Objekten
// sehen wenn sie gegen eine Policy verstoßen die auf Audit läuft"; his answer: fails of Enforce
// policies on objects that existed before count too). While the integration is on for a connected
// cluster, Cogpit reads the policy reports Kyverno writes (wgpolicyk8s.io/v1alpha2 PolicyReports
// and ClusterPolicyReports, kinds/policyreports.go) through the cache and marks every object a
// report names with a violation: the lists' warning column ("Kyverno: 2 policy violations
// (require-team-label, disallow-latest-tag)"), added after Name to a kind that has none while one of
// its objects violates a policy, and a "Policy violations" group in the drawer. Off: nothing is
// read, nothing shows (overlay.go).
//
//   - Source: the reports in all namespaces, or in each namespace the user names when they may not
//     list them everywhere (the kind's namespace plan, accessiblens_plan.go), and the cluster
//     reports; one watch each, trimmed (cache.Scope.Trim): the cache keeps a report's violations
//     only, not its passes and skips (trimPolicyReport), and nothing of another engine's reports.
//   - What counts: a result fail, or warn (a failed rule of a policy its author marked as not
//     scored), whatever the policy's mode: by the owner's answer an Enforce policy's fail (an object
//     from before the policy, found by a background scan) counts as an Audit one's does, so the
//     policies themselves are not read. An error (the engine could not apply the rule) is not the
//     object's and marks nothing; it shows in the report's own drawer.
//   - Index: by the object's UID (the report's scope.uid, a result's resources[].uid), else by its
//     group, Kind, namespace and name; a list row and a drawer find theirs with one lookup.

// kyvernoTrim names the trim of the reports' scopes (cache.RegisterTrim).
const kyvernoTrim = "kyverno-violations"

// kyvernoMaxNames bounds the policies a signal's text names.
const kyvernoMaxNames = 3

// The section group, the signal text's prefix, an item's name, and the column an added Signals
// column follows.
const (
	kyvernoGroup   = "Policy violations"
	kyvernoPrefix  = "Kyverno: "
	kyvernoItemMsg = "Message"
	nameColumnID   = "name"
)

// signalsColumn is the Signals column an overlay gives a kind (D-150), after Name; merged into the
// kind's own where it has one.
var signalsColumn = overlayColumn{Column: model.Column{ID: model.SignalsColumnID, Title: model.SignalsColumnTitle,
	Type: model.ColumnSignals}, After: nameColumnID}

func init() {
	cache.RegisterTrim(kyvernoTrim, trimPolicyReport)
	integrationProbes[IntegrationKyverno] = probeKyverno
	integrationFollowers = append(integrationFollowers, followKyverno)
}

// trimPolicyReport keeps what the Kyverno integration reads of a report: its violations (fail and
// warn results), and no result of a report another engine wrote. Metadata, scope and summary stay.
func trimPolicyReport(u *unstructured.Unstructured) {
	results, ok := u.Object["results"].([]any)
	if !ok {
		return
	}
	if !kinds.KyvernoReport(u.Object) {
		delete(u.Object, "results")
		return
	}
	kept := make([]any, 0, 1)
	for _, r := range results {
		if kinds.IsPolicyViolation(model.NestedString(r, "result")) {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		delete(u.Object, "results")
		return
	}
	u.Object["results"] = kept
}

// kyvernoReportKinds are the report kinds the integration reads, in this order.
var kyvernoReportKinds = []string{kinds.PolicyReportsKey, kinds.ClusterPolicyReportsKey}

// followKyverno starts the integration's reads while it is on for a connected cluster and the
// cluster serves the reports, reads them again when what it reads changes (a kind came or went, a
// namespace plan changed), and stops them when it is switched off.
func followKyverno(st *state, b *Backend, now UIState) {
	cur, _ := overlayOf[*kyvernoSource](b)
	// Read only while the cluster is the one shown: its marks show only in its views, and a cluster
	// connected in the background goes cold (D-69); shown again within the cache's warm period, the
	// reports are there at once (the lead's decision, 2026-10-17, D-79).
	if !now.IntegrationOn(b.Context, IntegrationKyverno) || now.Context != b.Context {
		if cur != nil {
			b.overlays.put(IntegrationKyverno, nil)
			st.s.goSafe("kyverno off", func() { st.s.integrationsChanged(b.Context) })
		}
		return
	}
	key := kyvernoReadKey(b)
	switch {
	case cur != nil && cur.key == key:
		return
	case key == "":
		if cur != nil { // the cluster no longer serves the reports
			b.overlays.put(IntegrationKyverno, nil)
		}
		return
	}
	b.overlays.put(IntegrationKyverno, startKyverno(st.s, b, key))
}

// kyvernoReadKey says what the integration reads on a cluster: each report kind served, its
// version and its namespace plan; "" when the cluster serves neither.
func kyvernoReadKey(b *Backend) string {
	var parts []string
	for _, key := range kyvernoReportKinds {
		if p, ok := b.Profile(key); ok {
			parts = append(parts, key+"@"+p.Version+"="+b.namespacePlan(p).key())
		}
	}
	return strings.Join(parts, "|")
}

// kyvernoSource is the integration's reads on one cluster and its index of violations; it is the
// backend's Kyverno overlay while the integration is on.
type kyvernoSource struct {
	s     *Session
	b     *Backend
	key   string
	reads []kyvernoRead
	idx   *violationIndex

	quit    chan struct{}
	stopped atomic.Bool
}

// kyvernoRead is one report kind's read.
type kyvernoRead struct {
	profile *model.KindProfile
	plan    nsPlan
	handle  *cache.Handle
	unsub   func()
}

// startKyverno acquires the report kinds' trimmed scopes as their plans say and fills the index.
func startKyverno(s *Session, b *Backend, key string) *kyvernoSource {
	k := &kyvernoSource{s: s, b: b, key: key, quit: make(chan struct{})}
	k.idx = newViolationIndex(b.overlays.changed)
	for _, kk := range kyvernoReportKinds {
		p, ok := b.Profile(kk)
		if !ok {
			continue
		}
		r := kyvernoRead{profile: p, plan: b.namespacePlan(p)}
		sc := b.Scope(p)
		sc.Trim = kyvernoTrim
		switch {
		case r.plan.pending: // the access reviews have not said whether the user may list them everywhere
			r.handle = cache.Pending(sc)
		case r.plan.perNamespace:
			r.handle = b.Cache.AcquireNamespaces(sc, r.plan.watch)
		default:
			r.handle = b.Cache.Acquire(sc)
		}
		h, report := r.handle, kk
		r.unsub = h.Subscribe(func(ev cache.Event) { k.idx.sync(h, report, ev.Object.GetNamespace(), ev.Object.GetName()) })
		for _, o := range h.List() { // subscribed first: nothing is missed
			k.idx.sync(h, report, o.GetNamespace(), o.GetName())
		}
		k.reads = append(k.reads, r)
	}
	s.goSafe("kyverno "+b.Context, k.settle)
	return k
}

// stop releases the reads; the index stays as it was for a reader still holding the overlay.
func (k *kyvernoSource) stop() {
	if !k.stopped.CompareAndSwap(false, true) {
		return
	}
	close(k.quit)
	for _, r := range k.reads {
		r.unsub()
		r.handle.Release()
	}
}

// settle tells the settings page and agents when what the integration knows changes (its reads
// listed, a refusal), which no watch event says: it looks at its reads' state, in memory, until it
// stops.
func (k *kyvernoSource) settle() {
	last, replanned := "", false
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		state, detail := k.status()
		if now := string(state) + "|" + detail; now != last {
			last = now
			k.s.integrationsChanged(k.b.Context)
		}
		if !replanned && k.planSettled() {
			// A read that waited for the access reviews: the follower reads again, now as the
			// plan says, as a view waiting for its plan is started again (replanPending).
			replanned = true
			k.b.ns.wakeReplan()
		}
		select {
		case <-k.quit:
			return
		case <-t.C:
		}
	}
}

// planSettled reports whether a read that started pending (the access reviews had not said whether
// the user may list the reports in all namespaces) could now be read as its plan says.
func (k *kyvernoSource) planSettled() bool {
	for _, r := range k.reads {
		if r.plan.pending && !k.b.namespacePlan(r.profile).pending {
			return true
		}
	}
	return false
}

// refusedAll reports whether the API server refused a read wholly: its one scope, or every
// namespace of a read namespace by namespace.
func refusedAll(h *cache.Handle) bool {
	err := h.Err()
	if !cache.IsRefused(err) {
		return false
	}
	ns := h.Namespaces()
	if ns == nil {
		return true
	}
	multi, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return false
	}
	n := 0
	for _, e := range multi.Unwrap() {
		if cache.IsRefused(e) {
			n++
		}
	}
	return n >= len(ns)
}

// status is what the integration knows on the cluster: checking while a read lists, forbidden when
// every read is refused, found otherwise (a refused cluster read named in the detail).
func (k *kyvernoSource) status() (IntegrationState, string) {
	var refused []string
	listing := false
	for _, r := range k.reads {
		switch {
		case r.plan.pending:
			listing = true
		case refusedAll(r.handle):
			refused = append(refused, r.profile.Key)
		case !r.handle.HasSynced():
			listing = true
		}
	}
	switch {
	case listing:
		return IntegrationChecking, ""
	case len(refused) == len(k.reads):
		return IntegrationForbidden, "may not list " + strings.Join(refused, " or ")
	case len(refused) > 0:
		return IntegrationFound, "may not list " + strings.Join(refused, ", ") + ": their objects show no violations"
	}
	return IntegrationFound, ""
}

// waitSynced waits, bounded by ctx, until the reads listed (or were refused): an agent's read of a
// drawer sees the violations a connect just started to read.
func (k *kyvernoSource) waitSynced(ctx context.Context) {
	for _, r := range k.reads {
		if r.plan.pending {
			continue
		}
		for !r.handle.HasSynced() && !cache.IsRefused(r.handle.Err()) && ctx.Err() == nil && !k.stopped.Load() {
			select {
			case <-ctx.Done():
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
}

// probeKyverno is the integration's probe: whether the cluster serves the reports (the custom
// kinds' watch, or API discovery for a user who may not list the definitions), and while it is on,
// what its reads met. It sends no request.
func probeKyverno(_ *Session, b *Backend, on bool) (IntegrationState, string, string) {
	p, served := b.Profile(kinds.PolicyReportsKey)
	if !served {
		p, served = b.Profile(kinds.ClusterPolicyReportsKey)
	}
	if !served {
		set := b.customSet()
		switch {
		case set != nil && !set.synced && set.err == "":
			if !on {
				return IntegrationOff, "", ""
			}
			return IntegrationChecking, "", ""
		case set != nil && set.err != "" && !set.discovered:
			return IntegrationUnavailable, "", "cannot tell: the custom resource definitions cannot be listed (" + set.err + ")"
		}
		return IntegrationMissing, "", "Kyverno's policy reports (" + kinds.PolicyReportGroup + ") are not served"
	}
	where := p.Group + "/" + p.Version
	if !on {
		return IntegrationFound, where, ""
	}
	k, _ := overlayOf[*kyvernoSource](b)
	if k == nil {
		return IntegrationChecking, where, ""
	}
	state, detail := k.status()
	return state, where, detail
}

// columns: a slot in the Signals column, in a kind's own Signals column always, added after Name
// to a kind that has none while one of its objects violates a policy (an empty column on every
// other view would change sixty views for nothing). Not the reports themselves, not a virtual or
// local kind.
func (k *kyvernoSource) columns(p *model.KindProfile) []overlayColumn {
	if !p.APIObject() || p.Group == kinds.PolicyReportGroup {
		return nil
	}
	if p.ColumnIndex(model.SignalsColumnID) < 0 && !k.idx.hasKind(p.Group, p.Kind) {
		return nil
	}
	return []overlayColumn{signalsColumn}
}

// cell is an object's signal: "Kyverno: 2 policy violations (require-team-label, …)", in the
// warning colour; a click opens the drawer at the first policy's section.
func (k *kyvernoSource) cell(p *model.KindProfile, _ string, obj *unstructured.Unstructured) (model.Cell, bool) {
	vs := k.idx.of(p, obj)
	if len(vs) == 0 {
		return model.Cell{}, false
	}
	return model.SignalsCell([]model.Signal{{Source: model.SignalKyverno, Text: violationsText(vs), Status: model.StatusWarning,
		Section: vs[0].Policy, Rank: model.SignalRank(model.StatusWarning, model.GradeNone, int64(len(vs)))}}), true
}

// violationsText says how many violations an object has, and of which policies.
func violationsText(vs []PolicyViolation) string {
	var names []string
	for _, v := range vs {
		names = appendIfMissing(names, v.Policy)
	}
	n := "1 policy violation"
	if len(vs) != 1 {
		n = strconv.Itoa(len(vs)) + " policy violations"
	}
	if len(names) > kyvernoMaxNames {
		names = append(names[:kyvernoMaxNames], "…")
	}
	return kyvernoPrefix + n + " (" + strings.Join(names, ", ") + ")"
}

func appendIfMissing(out []string, s string) []string {
	if s == "" || slices.Contains(out, s) {
		return out
	}
	return append(out, s)
}

// sections: one per violation under the group "Policy violations", titled by its policy.
func (k *kyvernoSource) sections(p *model.KindProfile, obj *unstructured.Unstructured, f model.TimeFormat) []model.DetailSection {
	if !p.APIObject() || p.Group == kinds.PolicyReportGroup {
		return nil
	}
	vs := k.idx.of(p, obj)
	if len(vs) == 0 {
		return nil
	}
	out := make([]model.DetailSection, 0, len(vs))
	for _, v := range vs {
		var policy model.DetailItem // a link to the policy while the cluster serves its kind; the title names it
		if ref := v.PolicyRef(); ref != nil {
			if _, served := k.b.Profile(ref.Kind); served {
				policy = model.DetailItem{Name: "Policy", Value: v.Policy, Link: ref}
			}
		}
		items := []model.DetailItem{
			policy,
			{Name: "Rule", Value: v.Rule},
			{Name: "Result", Value: resultText(v.Result), Status: model.StatusWarning},
			{Name: kyvernoItemMsg, Value: messageLines(v.Message)},
			{Name: "Severity", Value: v.Severity},
			{Name: "Category", Value: v.Category},
			{Name: "Found by", Value: foundBy(v.Process)},
			{Name: "Reported", Value: model.MarkTime(v.Time)},
		}
		items = slices.DeleteFunc(items, func(it model.DetailItem) bool { return it.Value == "" })
		out = append(out, model.DetailSection{Title: v.Policy, Group: kyvernoGroup, Items: items})
	}
	return f.WriteSections(out)
}

// messageLines puts each sentence of a policy's message on a line of its own: Kyverno's read
// "validation error: <the policy's message>. rule <rule> failed at path <path>", which the drawer
// would cut at its width as one line.
func messageLines(msg string) string {
	parts := strings.Split(strings.TrimSpace(msg), ". ")
	var b strings.Builder
	for i, p := range parts {
		b.WriteString(p)
		if i == len(parts)-1 {
			break
		}
		if low := strings.ToLower(p); strings.HasSuffix(low, "e.g") || strings.HasSuffix(low, "i.e") || strings.HasSuffix(low, "etc") {
			b.WriteString(". ") // an abbreviation, not a sentence's end
			continue
		}
		b.WriteString(".\n")
	}
	return b.String()
}

// resultText names a violation's result as the drawer shows it.
func resultText(result string) string {
	if result == kinds.PolicyWarn {
		return "Warn (the policy is not scored)"
	}
	return "Fail"
}

// foundBy names how Kyverno found a violation: "Background scan" (an object that existed before
// the policy, or changed since without an admission check), "Admission review".
func foundBy(process string) string {
	if process == "" {
		return ""
	}
	return strings.ToUpper(process[:1]) + process[1:]
}

// PolicyViolation is one violation of a Kyverno policy by an object (D-79): what the drawer's
// "Policy violations" and kyverno.violations show. Object is the object's reference in Cogpit (its
// kind key, namespace and name; empty Kind when Cogpit has no view of its kind).
type PolicyViolation struct {
	Object model.ObjectRef `json:"object"`
	kinds.PolicyResult
}

// violationIndex holds the violations of a cluster's objects, by object, from the reports.
type violationIndex struct {
	mu       sync.RWMutex
	reports  map[string][]indexedViolation           // a report's violations, by "<report kind>/<namespace>/<name>"
	byObject map[string]map[string][]PolicyViolation // an object's violations, by report, by object key
	kindOf   map[string]string                       // an object key's "<group>/<Kind>"
	kinds    map[string]int                          // the objects with violations, by "<group>/<Kind>"
	changed  func()
}

type indexedViolation struct {
	object string // the object key
	kind   string // "<group>/<Kind>"
	v      PolicyViolation
}

func newViolationIndex(changed func()) *violationIndex {
	return &violationIndex{
		reports: map[string][]indexedViolation{}, byObject: map[string]map[string][]PolicyViolation{},
		kindOf: map[string]string{}, kinds: map[string]int{}, changed: changed,
	}
}

// subjectKey is the index's key of an object a report names: its UID, else its group, Kind,
// namespace and name.
func subjectKey(s kinds.PolicySubject) string {
	if s.UID != "" {
		return "uid:" + s.UID
	}
	return "ref:" + s.Group() + "/" + s.Kind + "/" + s.Namespace + "/" + s.Name
}

// sync reads one report as the cache holds it now and replaces its violations in the index; a
// report the cache no longer holds leaves it. It reads the cache under the index's lock, so that
// the last event's sync always reads the newest report, whatever order the events' goroutines run
// in.
func (x *violationIndex) sync(h *cache.Handle, reportKind, namespace, name string) {
	rk := reportKind + "/" + namespace + "/" + name
	x.mu.Lock()
	var next []indexedViolation
	if obj, ok := h.Get(namespace, name); ok {
		for _, r := range kinds.PolicyReportResults(obj.Object, kinds.IsPolicyViolation) {
			for _, sub := range r.Subjects {
				next = append(next, indexedViolation{object: subjectKey(sub), kind: sub.Group() + "/" + sub.Kind,
					v: PolicyViolation{Object: model.ObjectRef{Namespace: sub.Namespace, Name: sub.Name}, PolicyResult: withoutSubjects(r)}})
			}
		}
	}
	old := x.reports[rk]
	if sameViolations(old, next) {
		x.mu.Unlock()
		return
	}
	kindOf := map[string]string{} // the objects the report named before or names now, and their kinds
	for _, iv := range old {
		kindOf[iv.object] = iv.kind
		if m := x.byObject[iv.object]; m != nil {
			delete(m, rk)
		}
	}
	if len(next) == 0 {
		delete(x.reports, rk)
	} else {
		x.reports[rk] = next
	}
	for _, iv := range next {
		kindOf[iv.object] = iv.kind
		m := x.byObject[iv.object]
		if m == nil {
			m = map[string][]PolicyViolation{}
			x.byObject[iv.object] = m
		}
		m[rk] = append(m[rk], iv.v)
	}
	for key, kind := range kindOf {
		x.recount(key, kind)
	}
	x.mu.Unlock()
	if x.changed != nil {
		x.changed()
	}
}

// recount follows whether an object has violations in the per-kind counts: an object counts once,
// under the kind it was counted with. x.mu is held.
func (x *violationIndex) recount(key, kind string) {
	counted, was := x.kindOf[key]
	has := len(x.byObject[key]) > 0
	switch {
	case has && !was:
		x.kindOf[key] = kind
		x.kinds[kind]++
	case !has && was:
		delete(x.kindOf, key)
		if x.kinds[counted]--; x.kinds[counted] <= 0 {
			delete(x.kinds, counted)
		}
	}
	if !has {
		delete(x.byObject, key)
	}
}

func withoutSubjects(r kinds.PolicyResult) kinds.PolicyResult {
	r.Subjects = nil
	return r
}

func sameViolations(a, b []indexedViolation) bool {
	return slices.EqualFunc(a, b, func(x, y indexedViolation) bool {
		return x.object == y.object && x.kind == y.kind && x.v.Object == y.v.Object &&
			samePolicyResult(x.v.PolicyResult, y.v.PolicyResult)
	})
}

func samePolicyResult(a, b kinds.PolicyResult) bool {
	return a.Policy == b.Policy && a.Rule == b.Rule && a.Result == b.Result && a.Message == b.Message &&
		a.Severity == b.Severity && a.Category == b.Category && a.Source == b.Source && a.Process == b.Process &&
		a.Time.Equal(b.Time)
}

// hasKind reports whether an object of a kind has a violation.
func (x *violationIndex) hasKind(group, kind string) bool {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.kinds[group+"/"+kind] > 0
}

// of returns an object's violations, by policy and rule; nil for none.
func (x *violationIndex) of(p *model.KindProfile, obj *unstructured.Unstructured) []PolicyViolation {
	x.mu.RLock()
	m := x.byObject["uid:"+string(obj.GetUID())]
	if len(m) == 0 {
		m = x.byObject["ref:"+p.Group+"/"+p.Kind+"/"+obj.GetNamespace()+"/"+obj.GetName()]
	}
	var out []PolicyViolation
	for _, vs := range m {
		out = append(out, vs...)
	}
	x.mu.RUnlock()
	sortViolations(out)
	return out
}

func sortViolations(vs []PolicyViolation) {
	slices.SortFunc(vs, func(a, b PolicyViolation) int {
		if c := strings.Compare(a.Object.Namespace, b.Object.Namespace); c != 0 {
			return c
		}
		if c := strings.Compare(a.Object.Name, b.Object.Name); c != 0 {
			return c
		}
		if c := strings.Compare(a.Policy, b.Policy); c != 0 {
			return c
		}
		return strings.Compare(a.Rule, b.Rule)
	})
}

// all returns every violation with its object's group and Kind.
func (x *violationIndex) all() []struct {
	kind string
	v    PolicyViolation
} {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []struct {
		kind string
		v    PolicyViolation
	}
	for _, ivs := range x.reports {
		for _, iv := range ivs {
			out = append(out, struct {
				kind string
				v    PolicyViolation
			}{iv.kind, iv.v})
		}
	}
	return out
}

// ErrKyvernoOff says that the Kyverno integration is switched off for a cluster.
var ErrKyvernoOff = errors.New("the Kyverno integration is switched off for this cluster (Cluster Settings › Integrations)")

// ErrKyvernoNotShown says that a cluster's Kyverno reports are read only while it is the one shown.
var ErrKyvernoNotShown = errors.New("the Kyverno integration reads the policy reports only while the cluster is the one shown in Cogpit's window; show it first")

// KyvernoReady says why a connected cluster's Kyverno violations cannot be read: the integration
// is off, or the cluster serves no policy reports; nil when they can.
func (s *Session) KyvernoReady(b *Backend) error {
	if b == nil {
		return errors.New("no cluster connected")
	}
	u := s.UIState()
	if !u.IntegrationOn(b.Context, IntegrationKyverno) {
		return ErrKyvernoOff
	}
	if u.Context != b.Context {
		return ErrKyvernoNotShown
	}
	if _, ok := overlayOf[*kyvernoSource](b); !ok {
		_, _, detail := probeKyverno(s, b, true)
		if detail == "" {
			detail = "the policy reports are still being looked for; try again shortly"
		}
		return errors.New("Kyverno: " + detail)
	}
	return nil
}

// PolicyViolations returns the Kyverno policy violations of a connected cluster's objects in the
// given namespaces (none: all; a cluster-scoped object's always), sorted by namespace, name, policy
// and rule, from the cache. It waits, bounded by ctx and syncTimeout, for the reports to list.
func (s *Session) PolicyViolations(ctx context.Context, b *Backend, namespaces []string) ([]PolicyViolation, error) {
	if err := s.KyvernoReady(b); err != nil {
		return nil, err
	}
	k, _ := overlayOf[*kyvernoSource](b)
	if k == nil {
		return nil, ErrKyvernoOff
	}
	wctx, cancel := context.WithTimeout(ctx, syncTimeout)
	k.waitSynced(wctx)
	cancel()
	keys := map[string]string{} // "<group>/<Kind>" → the kind key Cogpit shows it under
	for _, p := range b.AllProfiles() {
		if p.APIObject() {
			if _, taken := keys[p.Group+"/"+p.Kind]; !taken {
				keys[p.Group+"/"+p.Kind] = p.Key
			}
		}
	}
	out := []PolicyViolation{}
	for _, e := range k.idx.all() {
		v := e.v
		if v.Object.Namespace != "" && len(namespaces) > 0 && !slices.Contains(namespaces, v.Object.Namespace) {
			continue
		}
		v.Object.Context, v.Object.Kind = b.Context, keys[e.kind]
		out = append(out, v)
	}
	sortViolations(out)
	return out, nil
}
