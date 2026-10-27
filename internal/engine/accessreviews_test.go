package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// fakeReviewer answers access reviews from RBAC-like rules per namespace: "" holds the
// cluster-wide bindings' rules, which every namespace's rules review includes and which alone
// grant cluster-scoped resources and all namespaces, as on a real API server. gate, when set,
// holds every review until it is closed.
type fakeReviewer struct {
	mu          sync.Mutex
	rules       map[string][]cluster.AccessRule
	incomplete  bool
	failRules   bool
	failAccess  bool
	panicOn     string // the namespace whose rules review panics
	gate        chan struct{}
	inFlight    int
	maxInFlight int
	rulesCalls  map[string]int
	accessCalls map[cluster.AccessAttributes]int
}

func newFakeReviewer(rules map[string][]cluster.AccessRule) *fakeReviewer {
	return &fakeReviewer{rules: rules, rulesCalls: map[string]int{}, accessCalls: map[cluster.AccessAttributes]int{}}
}

func (f *fakeReviewer) enter(ctx context.Context) error {
	f.mu.Lock()
	f.inFlight++
	f.maxInFlight = max(f.maxInFlight, f.inFlight)
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (f *fakeReviewer) leave() {
	f.mu.Lock()
	f.inFlight--
	f.mu.Unlock()
}

func (f *fakeReviewer) ReviewRules(ctx context.Context, ns string) (cluster.AccessRules, error) {
	defer f.leave()
	if err := f.enter(ctx); err != nil {
		return cluster.AccessRules{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rulesCalls[ns]++
	switch {
	case ns == f.panicOn:
		panic("reviewer bug")
	case f.failRules:
		return cluster.AccessRules{}, errors.New(`selfsubjectrulesreviews.authorization.k8s.io is forbidden`)
	}
	return cluster.AccessRules{Rules: append(slices.Clone(f.rules[""]), f.rules[ns]...), Incomplete: f.incomplete}, nil
}

func (f *fakeReviewer) ReviewAccess(ctx context.Context, q cluster.AccessAttributes) (bool, bool, error) {
	defer f.leave()
	if err := f.enter(ctx); err != nil {
		return false, false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accessCalls[q]++
	if f.failAccess {
		return false, false, errors.New(`the server could not find the requested resource`)
	}
	rules := slices.Clone(f.rules[""])
	if q.Namespace != "" {
		rules = append(rules, f.rules[q.Namespace]...)
	}
	return cluster.AccessRules{Rules: rules}.Match(q) == cluster.RuleAllows, true, nil
}

func (f *fakeReviewer) set(fn func(f *fakeReviewer)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeReviewer) calls() (rules map[string]int, access map[cluster.AccessAttributes]int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return copyMap(f.rulesCalls), copyMap(f.accessCalls)
}

func copyMap[K comparable](in map[K]int) map[K]int {
	out := make(map[K]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func rule(verbs, groups, resources string) cluster.AccessRule {
	return cluster.AccessRule{Verbs: strings.Split(verbs, ","), APIGroups: strings.Split(groups, ","), Resources: strings.Split(resources, ",")}
}

// accessConnector is the test connector with access reviews over a fake reviewer, as the
// cluster connector sets them up.
type accessConnector struct {
	*fakeConnector
	rev       *fakeReviewer
	namespace string
}

func (c accessConnector) Connect(ctx context.Context, name string) (*Backend, error) {
	b, err := c.fakeConnector.Connect(ctx, name)
	if err != nil {
		return nil, err
	}
	b.Namespace = c.namespace
	a := newAccessReviews(c.rev, c.namespace)
	b.Access = a
	closeCache := b.Close
	b.Close = func() { a.stop(); closeCache() }
	return b, nil
}

func newAccessSession(t *testing.T, src *testkit.MemorySource, rev *fakeReviewer, namespace string) *Session {
	t.Helper()
	fc := &fakeConnector{src: src, profiles: testProfiles()}
	s := New(Options{Connector: accessConnector{fakeConnector: fc, rev: rev, namespace: namespace}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	return s
}

func waitCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, cond) {
		t.Fatalf("timed out waiting for %s", what)
	}
}

// waitAllowed waits until Allowed knows the answer and returns it.
func waitAllowed(t *testing.T, a Access, ns string, v Verb) bool {
	t.Helper()
	var allowed, known bool
	waitCond(t, fmt.Sprintf("an answer for %s in %q", v.Verb+" "+v.Resource, ns), func() bool {
		allowed, known = a.Allowed(ns, v)
		return known
	})
	return allowed
}

var (
	listPods      = Verb{Verb: "list", Resource: "pods"}
	deletePods    = Verb{Verb: "delete", Resource: "pods"}
	listNodes     = Verb{Verb: "list", Resource: "nodes"}
	listNamespace = Verb{Verb: "list", Resource: "namespaces"}
)

// Allowed never blocks and never makes a request: while the review is pending the answer is
// unknown; when it lands the answer is known and the session tells menus, the drawer and the
// sidebar to look again (EventKinds and EventDetail).
func TestAccessReviewsAnswerWithoutBlocking(t *testing.T) {
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"apps": {rule("get,list,watch", "", "pods")}})
	rev.gate = make(chan struct{})
	s := newAccessSession(t, testkit.NewMemorySource(), rev, "apps")
	rec := record(s)
	a := s.Backend().Access

	// The review is held at its gate until after these: an Allowed that waited for it would not
	// return.
	for range 100 {
		if _, known := a.Allowed("apps", listPods); known {
			t.Fatal("an answer before the review landed")
		}
	}
	kindsBefore, detailBefore := rec.count(EventKinds, ""), rec.count(EventDetail, "")

	close(rev.gate)
	if !waitAllowed(t, a, "apps", listPods) {
		t.Error("list pods in apps denied, the rules allow it")
	}
	if waitAllowed(t, a, "apps", deletePods) {
		t.Error("delete pods in apps allowed, no rule names it")
	}
	waitCond(t, "the events that follow the answers", func() bool {
		return rec.count(EventKinds, "") > kindsBefore && rec.count(EventDetail, "") > detailBefore
	})
	if rules, access := rev.calls(); rules["apps"] != 1 || len(access) != 0 {
		t.Errorf("reviews: rules %v, access %v; want one rules review of apps and no access review", rules, access)
	}
}

// Concurrent questions about the same namespace start one review; reviews run at most
// accessConcurrency at a time; once answered, asking again costs nothing.
func TestAccessReviewsDeduplicateAndBound(t *testing.T) {
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"": {rule("list", "", "pods")}})
	rev.gate = make(chan struct{})
	s := newAccessSession(t, testkit.NewMemorySource(), rev, "default")
	a := s.Backend().Access

	var wg sync.WaitGroup
	for i := range 20 {
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				a.Allowed(fmt.Sprintf("ns-%d", i), listPods)
			}()
		}
	}
	wg.Wait()
	waitCond(t, "the first reviews to start", func() bool {
		rev.mu.Lock()
		defer rev.mu.Unlock()
		return rev.inFlight == accessConcurrency
	})
	time.Sleep(50 * time.Millisecond) // more would start now if the bound did not hold
	rev.mu.Lock()
	inFlight := rev.inFlight
	rev.mu.Unlock()
	if inFlight != accessConcurrency {
		t.Errorf("%d reviews in flight, want %d", inFlight, accessConcurrency)
	}
	close(rev.gate)
	for i := range 20 {
		waitAllowed(t, a, fmt.Sprintf("ns-%d", i), listPods)
	}
	rules, _ := rev.calls()
	for i := range 20 {
		if n := rules[fmt.Sprintf("ns-%d", i)]; n != 1 {
			t.Errorf("ns-%d reviewed %d times, want once", i, n)
		}
	}
	if rev.maxInFlight > accessConcurrency {
		t.Errorf("%d reviews ran at once, the bound is %d", rev.maxInFlight, accessConcurrency)
	}
	before, _ := rev.calls()
	for i := range 20 {
		a.Allowed(fmt.Sprintf("ns-%d", i), listPods)
		a.Allowed(fmt.Sprintf("ns-%d", i), deletePods)
	}
	if after, _ := rev.calls(); len(after) != len(before) || after["default"] != before["default"] {
		t.Errorf("asking again cost reviews: %v, then %v", before, after)
	}
}

// What rules answer and when an access review asks instead: rules in a namespace answer that
// namespace; complete rules deny a cluster-scoped verb they do not name; a cluster-scoped verb
// they name, incomplete rules, a failed rules review and wildcards go to an access review; rules
// limited to named objects answer nothing.
func TestAccessReviewsRulesAndAccessReviews(t *testing.T) {
	type ask struct {
		ns          string
		v           Verb
		want        bool
		wantUnknown bool // stays unknown
		review      bool // answered by an access review
	}
	for _, tt := range []struct {
		name      string
		namespace string // the context's default namespace; "" is default
		rules     map[string][]cluster.AccessRule
		setup     func(*fakeReviewer)
		asks      []ask
	}{
		{
			name: "namespaced and cluster-scoped from rules",
			rules: map[string][]cluster.AccessRule{
				"":     {rule("get,list,watch", "", "pods")},
				"apps": {rule("delete", "", "pods")},
			},
			asks: []ask{
				{ns: "apps", v: deletePods, want: true},
				{ns: "default", v: deletePods, want: false},
				{ns: "", v: listNodes, want: false},             // complete rules name nodes nowhere
				{ns: "", v: listPods, want: true, review: true}, // named: a binding in default could be why
				{ns: "", v: deletePods, want: false},            // named in apps only, not in default
				// pods does not cover pods/log
				{ns: "apps", v: Verb{Verb: "get", Resource: "pods", Subresource: "log"}, want: false},
			},
		},
		{
			name: "a RoleBinding to a role naming namespaces", namespace: "team-a",
			rules: map[string][]cluster.AccessRule{
				"team-a": {rule("get,list,watch", "", "pods,namespaces")},
			},
			asks: []ask{
				{ns: "", v: listNamespace, want: false, review: true},
				{ns: "team-a", v: listPods, want: true},
				{ns: "", v: listNodes, want: false},
			},
		},
		{
			name:  "incomplete rules",
			rules: map[string][]cluster.AccessRule{"apps": {rule("list", "", "pods")}},
			setup: func(f *fakeReviewer) { f.incomplete = true },
			asks: []ask{
				{ns: "apps", v: listPods, want: true},
				{ns: "apps", v: deletePods, want: false, review: true},
			},
		},
		{
			name:  "failed rules review",
			rules: map[string][]cluster.AccessRule{"apps": {rule("list", "", "pods")}},
			setup: func(f *fakeReviewer) { f.failRules = true },
			asks: []ask{
				{ns: "apps", v: listPods, want: true, review: true},
				{ns: "apps", v: deletePods, want: false, review: true},
			},
		},
		{
			name: "rules limited to named objects, wildcards and subresources",
			rules: map[string][]cluster.AccessRule{
				"apps": {
					{Verbs: []string{"delete"}, APIGroups: []string{""}, Resources: []string{"pods"}, ResourceNames: []string{"web"}},
					rule("*", "apps", "*"),
					rule("create", "", "*/eviction"),
				},
			},
			asks: []ask{
				{ns: "apps", v: deletePods, wantUnknown: true},
				{ns: "apps", v: Verb{Verb: "patch", Group: "apps", Resource: "deployments", Subresource: "scale"}, want: true},
				{ns: "apps", v: Verb{Verb: "create", Resource: "pods", Subresource: "eviction"}, want: true},
				{ns: "apps", v: Verb{Verb: "*", Resource: "pods"}, want: false, review: true},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rev := newFakeReviewer(tt.rules)
			if tt.setup != nil {
				tt.setup(rev)
			}
			ns := tt.namespace
			if ns == "" {
				ns = "default"
			}
			s := newAccessSession(t, testkit.NewMemorySource(), rev, ns)
			a := s.Backend().Access
			for _, q := range tt.asks {
				label := fmt.Sprintf("%s %s/%s in %q", q.v.Verb, q.v.Resource, q.v.Subresource, q.ns)
				if q.wantUnknown {
					waitCond(t, "the rules of "+q.ns, func() bool {
						a.Allowed(q.ns, q.v)
						rules, _ := rev.calls()
						return rules[q.ns] == 1
					})
					time.Sleep(20 * time.Millisecond)
					if _, known := a.Allowed(q.ns, q.v); known {
						t.Errorf("%s: known, want unknown", label)
					}
					continue
				}
				if got := waitAllowed(t, a, q.ns, q.v); got != q.want {
					t.Errorf("%s: allowed %v, want %v", label, got, q.want)
				}
				attrs := cluster.AccessAttributes{Namespace: q.ns, Verb: q.v.Verb, Group: q.v.Group, Resource: q.v.Resource, Subresource: q.v.Subresource}
				if _, access := rev.calls(); (access[attrs] == 1) != q.review {
					t.Errorf("%s: access reviews %v, want one: %v", label, access, q.review)
				}
			}
		})
	}
}

// The registry greys a command the reviews deny, with the reason, once they answered; before,
// the command is offered.
func TestAccessReviewsGreyCommands(t *testing.T) {
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"apps": {rule("get,list,watch", "", "pods")}})
	rev.gate = make(chan struct{})
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("apps", "web"))
	fc := &fakeConnector{src: src, profiles: testProfiles()}
	s := New(Options{Connector: accessConnector{fakeConnector: fc, rev: rev, namespace: "default"}, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	if err := Register(s.Registry(), Spec[accessParams]{ID: "test.del", Danger: DangerRead, Verbs: []Verb{{Verb: "delete", Resource: "*"}},
		Run: func(context.Context, *Session, accessParams) (any, error) { return "ran", nil }}); err != nil {
		t.Fatal(err)
	}
	connectTest(t, s)
	cmd, _ := s.Registry().Get("test.del")
	params, _ := json.Marshal(accessParams{Ref: model.ObjectRef{Kind: "pods", Namespace: "apps", Name: "web"}})
	if err := cmd.Enabled(s, params); err != nil {
		t.Fatalf("before the reviews answered: %v, want offered", err)
	}
	close(rev.gate)
	var err error
	waitCond(t, "the denial", func() bool { err = cmd.Enabled(s, params); return err != nil })
	if !errors.Is(err, ErrNotPermitted) || err.Error() != "not permitted: you may not delete pods in apps" {
		t.Errorf("reason %q", err)
	}
	if _, derr := s.Dispatch(context.Background(), Invocation{ID: "test.del", Params: params, Origin: OriginBuiltInAgent}); !errors.As(derr, new(*DisabledError)) {
		t.Errorf("Dispatch of a denied command: %v", derr)
	}
}

// The sidebar's question: a kind is listable if the rules of any namespace the user has seen name
// it, and not listable only when every namespace the user can see was reviewed and none names it.
func TestAccessListable(t *testing.T) {
	listable := func(t *testing.T, b *Backend, p *model.KindProfile, filter []string) bool {
		t.Helper()
		var l, known bool
		waitCond(t, "an answer for "+p.Key, func() bool { l, known = b.Listable(p, filter); return known })
		return l
	}

	t.Run("a user limited to their namespace", func(t *testing.T) {
		// The edit role through a RoleBinding in team-a, which names namespaces too: an access
		// review tells that the user may not list namespaces, so team-a and the namespace filter
		// are all the user can see.
		rev := newFakeReviewer(map[string][]cluster.AccessRule{
			"team-a": {rule("get,list,watch", "", "pods,namespaces"), rule("get,list,watch", "apps", "deployments")},
			"team-b": {rule("get,list,watch", "", "secrets")},
		})
		s := newAccessSession(t, testkit.NewMemorySource(), rev, "team-a")
		b := s.Backend()
		for _, tt := range []struct {
			p    *model.KindProfile
			want bool
		}{{testPods, true}, {testDeployments, true}, {testSecrets, false}, {testServices, false}, {testNamespaces, false}} {
			if got := listable(t, b, tt.p, nil); got != tt.want {
				t.Errorf("%s: listable %v, want %v", tt.p.Key, got, tt.want)
			}
		}
		// team-b in the namespace filter: its rules are reviewed, Secrets become listable.
		if got := listable(t, b, testSecrets, []string{"team-b"}); !got {
			t.Error("secrets not listable with team-b in the filter")
		}
		rules, access := rev.calls()
		if rules["team-a"] != 1 || rules["team-b"] != 1 || len(rules) != 2 {
			t.Errorf("rules reviews %v, want team-a and team-b once", rules)
		}
		if len(access) != 1 || access[cluster.AccessAttributes{Verb: "list", Resource: "namespaces"}] != 1 {
			t.Errorf("access reviews %v, want one for list namespaces", access)
		}
	})

	t.Run("a user who may list namespaces", func(t *testing.T) {
		src := testkit.NewMemorySource()
		for i := range 5 {
			src.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", fmt.Sprintf("ns-%d", i), nil))
		}
		rev := newFakeReviewer(map[string][]cluster.AccessRule{
			"":     {rule("get,list,watch", "", "pods,namespaces")},
			"ns-3": {rule("list", "", "secrets")},
		})
		s := newAccessSession(t, src, rev, "default")
		b := s.Backend()
		if !listable(t, b, testPods, nil) || !listable(t, b, testSecrets, nil) || listable(t, b, testDeployments, nil) {
			t.Error("want pods and secrets listable, deployments not")
		}
		rules, _ := rev.calls()
		if len(rules) != 6 {
			t.Errorf("rules reviews %v, want default and ns-0 … ns-4 once each", rules)
		}
		// A namespace created later is reviewed when the sidebar asks again, which its event makes
		// it do. Only an EventKinds sent once the cache holds the namespace counts: one still on
		// its way from the answers above let the sidebar ask before the namespace arrived.
		nsHandle := b.Acquire(testNamespaces)
		defer nsHandle.Release()
		var asked atomic.Bool
		unsub := s.Subscribe(func(ev Event) {
			if _, arrived := nsHandle.Get("", "ns-new"); arrived && ev.Type == EventKinds {
				asked.Store(true)
			}
		})
		defer unsub()
		src.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", "ns-new", nil))
		waitCond(t, "EventKinds after the new namespace", asked.Load)
		listable(t, b, testDeployments, nil)
		if rules, _ := rev.calls(); rules["ns-new"] != 1 {
			t.Errorf("the new namespace was not reviewed: %v", rules)
		}
	})

	t.Run("more namespaces than the limit", func(t *testing.T) {
		src := testkit.NewMemorySource()
		for i := range accessNamespaceLimit + 1 {
			src.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", fmt.Sprintf("ns-%02d", i), nil))
		}
		rev := newFakeReviewer(map[string][]cluster.AccessRule{"": {rule("get,list,watch", "", "pods,namespaces")}})
		s := newAccessSession(t, src, rev, "default")
		b := s.Backend()
		if !listable(t, b, testPods, nil) {
			t.Error("pods not listable")
		}
		waitCond(t, "the namespaces in the cache", func() bool {
			b.Listable(testDeployments, nil)
			return b.Cached(testNamespaces) && b.Cache.Acquire(b.Scope(testNamespaces)).HasSynced()
		})
		time.Sleep(50 * time.Millisecond)
		if _, known := b.Listable(testDeployments, nil); known {
			t.Error("deployments known not listable although not every namespace was reviewed")
		}
		if rules, _ := rev.calls(); len(rules) != 1 {
			t.Errorf("rules reviews %v, want only the default namespace's", rules)
		}
	})
}

// cluster.refreshAccess's engine side: every answer is dropped, menus look again at once, and the
// reviews run again.
func TestAccessRefresh(t *testing.T) {
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"default": {rule("list", "", "pods")}})
	s := newAccessSession(t, testkit.NewMemorySource(), rev, "default")
	b := s.Backend()
	if !waitAllowed(t, b.Access, "default", listPods) || waitAllowed(t, b.Access, "default", deletePods) {
		t.Fatal("first answers wrong")
	}
	rev.set(func(f *fakeReviewer) { f.rules["default"] = append(f.rules["default"], rule("delete", "", "pods")) })
	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"apps"} })
	waitCond(t, "the filter's namespace reviewed", func() bool { rules, _ := rev.calls(); return rules["apps"] == 1 })
	waitAllowed(t, b.Access, "apps", listPods) // its answer kept, not only asked: refresh names the namespaces it holds

	rec := record(s)
	namespaces, ok := b.RefreshAccess()
	if !ok || !slices.Equal(namespaces, []string{"default", "apps"}) {
		t.Errorf("RefreshAccess = %v, %v", namespaces, ok)
	}
	if !waitAllowed(t, b.Access, "default", deletePods) {
		t.Error("the refreshed rules were not asked")
	}
	// The reviews run on their own goroutines: the apps one may land after default's answer.
	waitCond(t, "both namespaces reviewed again", func() bool { rules, _ := rev.calls(); return rules["apps"] >= 2 })
	if rules, _ := rev.calls(); rules["default"] != 2 || rules["apps"] != 2 {
		t.Errorf("rules reviews %v, want default and apps twice", rules)
	}
	waitCond(t, "the events of the refresh", func() bool { return rec.count(EventKinds, "") > 0 && rec.count(EventDetail, "") > 0 })
	if _, ok := (&Backend{}).RefreshAccess(); ok || (&Backend{}).HasAccessReviews() {
		t.Error("a backend without access reviews refreshed")
	}
}

// A server that answers no reviews costs one failing rules review per namespace and one failing
// access review, not one per question: the answers stay unknown, the commands offered, until a
// refresh asks again.
func TestAccessReviewsStopAskingAfterAFailure(t *testing.T) {
	rev := newFakeReviewer(nil)
	rev.failRules, rev.failAccess = true, true
	s := newAccessSession(t, testkit.NewMemorySource(), rev, "default")
	b := s.Backend()
	waitCond(t, "the first access review", func() bool {
		b.Access.Allowed("default", listPods)
		_, access := rev.calls()
		return len(access) == 1
	})
	a := b.Access.(*accessReviews)
	waitCond(t, "the failure recorded", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.failing
	})
	for _, v := range []Verb{listPods, deletePods, listNodes, listNamespace} {
		for _, ns := range []string{"default", "", "apps"} {
			if _, known := b.Access.Allowed(ns, v); known {
				t.Errorf("%s %s in %q known from failing reviews", v.Verb, v.Resource, ns)
			}
		}
		b.Listable(testDeployments, nil)
	}
	waitCond(t, "the rules review of apps answered", func() bool {
		rules, _ := rev.calls()
		rev.mu.Lock()
		defer rev.mu.Unlock()
		return rules["apps"] == 1 && rev.inFlight == 0
	})
	rules, access := rev.calls()
	if len(access) != 1 || rules["default"] != 1 || rules["apps"] != 1 || len(rules) != 2 {
		t.Errorf("reviews after the failure: rules %v, access %v", rules, access)
	}
	rev.set(func(f *fakeReviewer) { f.failRules, f.failAccess = false, false })
	b.RefreshAccess()
	if waitAllowed(t, b.Access, "default", listPods) {
		t.Error("allowed by an empty rule set")
	}
}

// A panicking reviewer is recovered at the goroutine's boundary: the namespace's rules count as
// failed, and an access review answers instead.
func TestAccessReviewerPanicIsRecovered(t *testing.T) {
	rev := newFakeReviewer(map[string][]cluster.AccessRule{"apps": {rule("list", "", "pods")}})
	rev.panicOn = "apps"
	s := newAccessSession(t, testkit.NewMemorySource(), rev, "default")
	rec := record(s)
	if !waitAllowed(t, s.Backend().Access, "apps", listPods) {
		t.Error("the access review did not answer after the rules review panicked")
	}
	waitCond(t, "the error toast", func() bool { return rec.count(EventToast, "") > 0 })
}

// Disconnecting stops the reviews: those in flight end, and the old backend's Access answers
// nothing and starts nothing.
func TestAccessReviewsStopWithTheBackend(t *testing.T) {
	rev := newFakeReviewer(map[string][]cluster.AccessRule{})
	rev.gate = make(chan struct{}) // never opened: the reviews end by cancellation
	s := newAccessSession(t, testkit.NewMemorySource(), rev, "default")
	old := s.Backend()
	old.Access.Allowed("apps", listPods)
	waitCond(t, "the reviews to start", func() bool { rev.mu.Lock(); defer rev.mu.Unlock(); return rev.inFlight == 2 })
	if err := s.Disconnect(); err != nil { // the old backend closes (D-69: a connect again only shows it)
		t.Fatal(err)
	}
	connectTest(t, s) // a new backend
	waitCond(t, "the old reviews to end", func() bool {
		rev.mu.Lock()
		defer rev.mu.Unlock()
		return rev.inFlight == 1 // the new backend's default namespace waits at the gate
	})
	before, _ := rev.calls()
	if _, known := old.Access.Allowed("other", listPods); known {
		t.Error("a stopped Access answered")
	}
	time.Sleep(20 * time.Millisecond)
	rev.mu.Lock()
	inFlight := rev.inFlight
	rev.mu.Unlock()
	if after, _ := rev.calls(); inFlight != 1 || len(after) != len(before) {
		t.Errorf("a stopped Access started a review: %d in flight, %v", inFlight, after)
	}
}
