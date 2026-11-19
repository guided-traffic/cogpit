package integration_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// Readers that hold their reads while they are shown, for a user who may not list what they read,
// measured at the HTTP level against a fake API server that enforces RBAC: the refusal is a state
// the reader shows, never a list client-go's backoff keeps sending while it is shown, and
// cluster.refreshAccess reads it again (D-177). The user may list pods, namespaces and Deployments
// in all namespaces, or in team-a alone; the access reviews either tell, or cannot (a webhook
// authorizer: every rules review incomplete), and then the API server's refusal is the first word.

// refusedTrivyGVR is Trivy Operator's VulnerabilityReport resource, refusedPolicyReportsGVR
// Kyverno's PolicyReport.
var (
	refusedTrivyGVR          = schema.GroupVersionResource{Group: "aquasecurity.github.io", Version: "v1alpha1", Resource: "vulnerabilityreports"}
	refusedPolicyReportsGVR  = schema.GroupVersionResource{Group: kinds.PolicyReportGroup, Version: "v1alpha2", Resource: "policyreports"}
	refusedEndpointSlicesGVR = schema.GroupVersionResource{Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices"}
)

// refusedWorkloadGVRs are the workload kinds the Overviews read besides the Deployments.
var refusedWorkloadGVRs = map[string]schema.GroupVersionResource{
	"StatefulSet": {Group: "apps", Version: "v1", Resource: "statefulsets"},
	"DaemonSet":   {Group: "apps", Version: "v1", Resource: "daemonsets"},
	"ReplicaSet":  {Group: "apps", Version: "v1", Resource: "replicasets"},
	"Job":         {Group: "batch", Version: "v1", Resource: "jobs"},
	"CronJob":     {Group: "batch", Version: "v1", Resource: "cronjobs"},
}

// refusedUser is one user of these tests.
type refusedUser struct {
	name       string
	ns         string   // the context's namespace and the user's one RoleBinding; "": cluster-wide reads
	incomplete bool     // the access reviews cannot tell
	allow      []string // resources of the core and apps groups the user may read besides pods, namespaces, deployments
	noPods     bool     // the user may not read pods
	teamA      []string // resources of the core and apps groups the user may read in team-a alone
	cluster    []string // resources of the core group the user may read cluster-wide (a ClusterRole), also when namespace-limited
	accessible []string // the cluster's accessible namespaces
}

func refusedUsers() []refusedUser {
	return []refusedUser{
		{name: "cluster-wide, the access reviews tell"},
		{name: "cluster-wide, the access reviews cannot tell", incomplete: true},
		{name: "namespace-limited, the access reviews tell", ns: "team-a"},
		{name: "namespace-limited, the access reviews cannot tell", ns: "team-a", incomplete: true},
	}
}

// refusedSession connects u to a fake API server that serves the core kinds, the workload kinds,
// the events, the nodes, Secrets, and Trivy Operator's, Kyverno's, Flux's and Argo CD's kinds with
// their definitions; on are the integrations switched on.
func refusedSession(ctx context.Context, t *testing.T, u refusedUser, on engine.IntegrationSet) (*engine.Session, *testkit.FakeAPIServer) {
	t.Helper()
	resources := append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
		testkit.FakeResource{GVR: testkit.CRDsGVR, Kind: "CustomResourceDefinition"},
		testkit.FakeResource{GVR: testkit.NodesGVR, Kind: "Node"},
		testkit.FakeResource{GVR: testkit.EventsGVR, Kind: "Event", Namespaced: true},
		testkit.FakeResource{GVR: testkit.SecretsGVR, Kind: "Secret", Namespaced: true},
		testkit.FakeResource{GVR: refusedEndpointSlicesGVR, Kind: "EndpointSlice", Namespaced: true},
		testkit.FakeResource{GVR: refusedTrivyGVR, Kind: "VulnerabilityReport", Namespaced: true},
		testkit.FakeResource{GVR: refusedPolicyReportsGVR, Kind: "PolicyReport", Namespaced: true},
		testkit.FakeResource{GVR: gitopsKustomizationsGVR, Kind: "Kustomization", Namespaced: true},
		testkit.FakeResource{GVR: gitopsGitReposGVR, Kind: "GitRepository", Namespaced: true},
		testkit.FakeResource{GVR: testkit.ArgoApplicationsGVR, Kind: "Application", Namespaced: true},
		testkit.FakeResource{GVR: testkit.ArgoAppSetsGVR, Kind: "ApplicationSet", Namespaced: true},
		testkit.FakeResource{GVR: testkit.ArgoProjectsGVR, Kind: "AppProject", Namespaced: true})
	for kind, gvr := range refusedWorkloadGVRs {
		resources = append(resources, testkit.FakeResource{GVR: gvr, Kind: kind, Namespaced: true})
	}
	crds := testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{"apiextensions.k8s.io"}, Resources: []string{"customresourcedefinitions"}}
	reads := testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{"", "apps"},
		Resources: append([]string{"pods", "namespaces", "deployments"}, u.allow...)}
	if u.noPods {
		reads.Resources = append([]string{"namespaces", "deployments"}, u.allow...)
	}
	rules := map[string][]testkit.FakeRule{"": {crds, reads}}
	if u.ns != "" {
		rules = map[string][]testkit.FakeRule{"": {crds}, u.ns: {reads}}
	}
	if len(u.cluster) > 0 {
		rules[""] = append(rules[""], testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{""}, Resources: u.cluster})
	}
	if len(u.teamA) > 0 {
		rules["team-a"] = append(rules["team-a"], testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{"", "apps"}, Resources: u.teamA})
	}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "refused-token", Resources: resources,
		Access: &testkit.FakeAccess{Enforce: true, Incomplete: u.incomplete, Rules: rules}})
	t.Cleanup(srv.Close)
	srv.Set(testkit.CRDsGVR, testkit.CRD(refusedTrivyGVR.Group, refusedTrivyGVR.Resource, "VulnerabilityReport", true,
		testkit.CRDVersion{Name: refusedTrivyGVR.Version, Served: true, Storage: true}))
	srv.Set(testkit.CRDsGVR, testkit.CRD(refusedPolicyReportsGVR.Group, refusedPolicyReportsGVR.Resource, "PolicyReport", true,
		testkit.CRDVersion{Name: refusedPolicyReportsGVR.Version, Served: true, Storage: true}))
	srv.Set(testkit.CRDsGVR, testkit.CRD(gitopsKustomizationsGVR.Group, gitopsKustomizationsGVR.Resource, "Kustomization", true))
	srv.Set(testkit.CRDsGVR, testkit.CRD(gitopsGitReposGVR.Group, gitopsGitReposGVR.Resource, "GitRepository", true))
	for _, crd := range testkit.ArgoCRDs() {
		srv.Set(testkit.CRDsGVR, crd)
	}
	s := engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", u.ns)}},
		ClusterSettings:  engine.NewClusterSettingsStore(t.TempDir()),
		SnapshotInterval: 10 * time.Millisecond,
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeClusterSettings("fake", func(c *engine.ClusterSettings) error {
		c.SetIntegrations(engine.IntAll, on, 0)
		c.AccessibleNamespaces = u.accessible
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool { return s.Backend().CustomKindsSettled() }) {
		t.Fatal("the definitions were never listed")
	}
	return s, srv
}

// refusedLists counts the server's lists of resources ("vulnerabilityreports.aquasecurity.github.io",
// "nodes"; a label-selected read's key starts so too), in all namespaces and in each, refused ones
// included.
func refusedLists(srv *testkit.FakeAPIServer, resources ...string) map[string]int64 {
	out := map[string]int64{}
	for k, c := range srv.RequestsIn() {
		for _, r := range resources {
			for _, verb := range []string{"list ", "watchlist "} {
				if rest, ok := strings.CutPrefix(k, verb+r); ok && (rest == "" || strings.HasPrefix(rest, " ")) {
					out[r] += c
				}
			}
		}
	}
	return out
}

// refusedWindow is longer than client-go's first backoff steps (0.8 s, then 1.6 s): a refused read
// still held is listed again within it.
const refusedWindow = 3 * time.Second

// settledRefusal waits until shown says the reader shows the refusal, gives the reader's loop a
// moment to give the read back, then checks that resources are not listed within refusedWindow;
// it returns each resource's lists before the window.
func settledRefusal(ctx context.Context, t *testing.T, srv *testkit.FakeAPIServer, shown func() bool, what func() string, resources ...string) map[string]int64 {
	t.Helper()
	if !testkit.WaitFor(ctx, shown) {
		t.Fatalf("the refusal never showed (%s; reads %v)", what(), srv.RequestsIn())
	}
	time.Sleep(time.Second) // the reader gives a refused read back at its next look
	before := refusedLists(srv, resources...)
	time.Sleep(refusedWindow)
	after := refusedLists(srv, resources...)
	for _, r := range resources {
		if n := after[r] - before[r]; n != 0 {
			t.Errorf("%s was listed %d times within %s after the refusal showed, want none (reads %v)", r, n, refusedWindow, srv.RequestsIn())
		}
	}
	if !shown() {
		t.Errorf("the reader lost the refusal: %s", what())
	}
	return before
}

// refreshAccess runs cluster.refreshAccess as the window does.
func refreshAccess(ctx context.Context, t *testing.T, s *engine.Session) {
	t.Helper()
	if _, err := s.Dispatch(ctx, engine.Invocation{ID: "cluster.refreshAccess", Params: []byte(`{}`), Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
}

// TestBudgetTrivyRefusedReports: the Trivy integration for a user who may not list Trivy Operator's
// reports, while the cluster is shown. The access reviews that say no cost no list and make the
// refusal the integration's state; the API server's refusal costs a list in all namespaces and,
// once followed, one in the user's namespace, and nothing after it; cluster.refreshAccess reads
// them once again.
func TestBudgetTrivyRefusedReports(t *testing.T) {
	const reports = "vulnerabilityreports.aquasecurity.github.io"
	for _, u := range refusedUsers() {
		t.Run(u.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			s, srv := refusedSession(ctx, t, u, engine.IntTrivy)
			status := func() engine.IntegrationStatus {
				for _, st := range s.IntegrationStatuses("fake") {
					if st.Name == engine.IntegrationTrivy {
						return st
					}
				}
				return engine.IntegrationStatus{}
			}
			shown := func() bool {
				st := status()
				return st.State == engine.IntegrationForbidden && strings.Contains(st.Detail, reports)
			}
			what := func() string { st := status(); return string(st.State) + " " + st.Detail }
			want := int64(0) // the access reviews said no: nothing is sent
			if u.incomplete {
				want = 2 // in all namespaces, then in the context's namespace
			}
			first := settledRefusal(ctx, t, srv, shown, what, reports)
			if first[reports] > want {
				t.Errorf("the reports were listed %d times, want at most %d (reads %v)", first[reports], want, srv.RequestsIn())
			}
			if !u.incomplete {
				return
			}
			refreshAccess(ctx, t, s)
			if !testkit.WaitFor(ctx, func() bool { return refusedLists(srv, reports)[reports] >= first[reports]+want }) {
				t.Fatalf("cluster.refreshAccess did not read the reports again: %v", srv.RequestsIn())
			}
			again := settledRefusal(ctx, t, srv, shown, what, reports)
			if n := again[reports] - first[reports]; n > want {
				t.Errorf("after cluster.refreshAccess the reports were listed %d more times, want at most %d", n, want)
			}
		})
	}
}

// TestBudgetPagesRefusedKinds: the pages that hold their kinds while they are shown (the Workloads
// and cluster Overviews, the Flux and Argo CD dashboards), for a user the access reviews cannot
// tell about who may not list the page's kinds: the page shows the API server's refusal, and no
// refused kind is listed again while it is shown, before and after cluster.refreshAccess reads them
// once again. How often a refused kind is listed until then depends on the scheduler: each
// refusal in all namespaces that is followed starts the page again.
func TestBudgetPagesRefusedKinds(t *testing.T) {
	const (
		statefulSets = "statefulsets.apps"
		daemonSets   = "daemonsets.apps"
	)
	pages := []struct {
		view      string
		resources []string // the page's kinds the user may not list
	}{
		{engine.WorkloadsOverview, []string{statefulSets, daemonSets, "replicasets.apps", "jobs.batch", "cronjobs.batch", "events"}},
		{engine.ClusterOverview, []string{"nodes", "events", statefulSets, daemonSets}},
		{engine.FluxDashboard, fluxReads},
		{engine.ArgoCDDashboard, append(append([]string(nil), argoReads...), "secrets")},
	}
	for _, page := range pages {
		for _, u := range refusedUsers() {
			if !u.incomplete {
				continue // the access reviews that say no cost no list (TestGitOpsDashboardsOfANamespaceLimitedUser)
			}
			t.Run(page.view+", "+u.name, func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				s, srv := refusedSession(ctx, t, u, 0)
				s.OpenTab(page.view, false)
				shown := func() bool {
					ts := s.Table(page.view)
					return ts != nil && strings.Contains(ts.Error, "is forbidden")
				}
				what := func() string {
					if ts := s.Table(page.view); ts != nil {
						return "error line " + ts.Error
					}
					return "no snapshot"
				}
				first := settledRefusal(ctx, t, srv, shown, what, page.resources...)
				t.Logf("lists until the refusal settled: %v", first)

				refreshAccess(ctx, t, s)
				if !testkit.WaitFor(ctx, func() bool {
					now := refusedLists(srv, page.resources...)
					for _, r := range page.resources {
						if now[r] <= first[r] {
							return false
						}
					}
					return true
				}) {
					t.Fatalf("cluster.refreshAccess did not read the page's kinds again: %v, before %v", refusedLists(srv, page.resources...), first)
				}
				settledRefusal(ctx, t, srv, shown, what, page.resources...)
			})
		}
	}
}

// TestBudgetClusterOverviewRefusedNodes: the cluster Overview of a user who may read everything it
// reads but the Nodes, and whom the access reviews cannot tell about: the refused Nodes, a
// cluster-scoped kind no plan reads namespace by namespace, are listed once, are the page's error
// and are not listed again while it is shown; cluster.refreshAccess reads them again. (The page
// starts again then anyway: its other kinds' plans wait for the reviews' new answers and come back,
// each a start that reads the Nodes once.)
func TestBudgetClusterOverviewRefusedNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	u := refusedUser{name: "all but the nodes", incomplete: true, allow: []string{"events", "statefulsets", "daemonsets"}}
	s, srv := refusedSession(ctx, t, u, 0)
	s.OpenTab(engine.ClusterOverview, false)
	shown := func() bool {
		ts := s.Table(engine.ClusterOverview)
		return ts != nil && strings.HasPrefix(ts.Error, "Nodes: ") && strings.Contains(ts.Error, "is forbidden")
	}
	what := func() string {
		if ts := s.Table(engine.ClusterOverview); ts != nil {
			return "error line " + ts.Error
		}
		return "no snapshot"
	}
	first := settledRefusal(ctx, t, srv, shown, what, "nodes")
	if first["nodes"] != 1 {
		t.Errorf("the nodes were listed %d times, want once (reads %v)", first["nodes"], srv.RequestsIn())
	}
	refreshAccess(ctx, t, s)
	if !testkit.WaitFor(ctx, func() bool { return refusedLists(srv, "nodes")["nodes"] >= 2 }) {
		t.Fatalf("cluster.refreshAccess did not read the nodes again: %v", srv.RequestsIn())
	}
	if again := settledRefusal(ctx, t, srv, shown, what, "nodes"); again["nodes"] > 3 {
		t.Errorf("after cluster.refreshAccess the nodes were listed %d times in all, want at most 3", again["nodes"])
	}
}

// TestReadersNameKindsTheReviewsDeny: a user the access reviews say may not list a reader's kinds,
// cluster-wide and namespace-limited: nothing is listed, and each reader says so, as the
// integrations' states do ("Cogpit may not list <kind>") — the Flux dashboard instead of "The
// cluster serves no Flux kinds.", the Argo CD dashboard and the Workloads Overview instead of
// counts of nothing, the Kyverno integration instead of "found".
func TestReadersNameKindsTheReviewsDeny(t *testing.T) {
	const (
		ks    = "kustomizations.kustomize.toolkit.fluxcd.io"
		repos = "gitrepositories.source.toolkit.fluxcd.io"
		apps  = "applications.argoproj.io"
		polr  = "policyreports.wgpolicyk8s.io"
	)
	for _, u := range refusedUsers() {
		if u.incomplete {
			continue
		}
		t.Run(u.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			s, srv := refusedSession(ctx, t, u, engine.IntKyverno)
			pageError := func(view, want string) {
				t.Helper()
				s.OpenTab(view, false)
				if !testkit.WaitFor(ctx, func() bool { ts := s.Table(view); return ts != nil && ts.Synced && ts.Error == want }) {
					var got string
					if ts := s.Table(view); ts != nil {
						got = ts.Error
					}
					t.Errorf("%s says %q, want %q", view, got, want)
				}
			}

			pageError(engine.FluxDashboard, "Cogpit may not list "+ks+", "+repos)
			if m, _, err := s.FluxDashboard(ctx, "", nil, false); err != nil || m.Error != "Cogpit may not list "+ks+", "+repos {
				t.Errorf("flux.dashboard: %+v, %v", m, err)
			}

			pageError(engine.ArgoCDDashboard, "Cogpit may not list "+apps)
			if m, _, err := s.ArgoCDDashboard(ctx, "", nil, false); err != nil || m.Error != "Cogpit may not list "+apps ||
				len(m.Kinds) == 0 || m.Kinds[0].Error != "Cogpit may not list "+apps {
				t.Errorf("argocd.dashboard: %+v, %v", m, err)
			}

			pageError(engine.WorkloadsOverview, "Cogpit may not list events")
			if m, _, err := s.WorkloadsOverview(ctx, "", nil, false); err != nil {
				t.Error(err)
			} else {
				for _, k := range m.Kinds {
					if k.Kind == "statefulsets" && k.Error != "Cogpit may not list statefulsets.apps" {
						t.Errorf("the Workloads Overview's Stateful Sets: %+v", k)
					}
				}
			}

			kyverno := func() engine.IntegrationStatus {
				for _, st := range s.IntegrationStatuses("fake") {
					if st.Name == engine.IntegrationKyverno {
						return st
					}
				}
				return engine.IntegrationStatus{}
			}
			if !testkit.WaitFor(ctx, func() bool {
				st := kyverno()
				return st.State == engine.IntegrationForbidden && st.Detail == "may not list "+polr
			}) {
				t.Errorf("Kyverno's status: %+v", kyverno())
			}

			if got := refusedLists(srv, ks, repos, apps, polr, "statefulsets.apps", "events"); len(got) != 0 {
				t.Errorf("kinds the reviews deny were listed: %v", got)
			}
		})
	}
}

// TestBudgetViewsRefusedKinds: a kind's list view for a user the access reviews cannot tell about
// who may not list the kind (a namespaced one, the Stateful Sets, and the Nodes): the view's error
// line is the API server's refusal, and the kind is not listed again while the view is open, before
// and after cluster.refreshAccess reads it once again.
func TestBudgetViewsRefusedKinds(t *testing.T) {
	for _, view := range []struct{ key, resource string }{{"statefulsets", "statefulsets.apps"}, {"nodes", "nodes"}} {
		for _, u := range refusedUsers() {
			if !u.incomplete {
				continue
			}
			t.Run(view.key+", "+u.name, func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				s, srv := refusedSession(ctx, t, u, 0)
				s.OpenTab(view.key, false)
				shown := func() bool {
					ts := s.Table(view.key)
					return ts != nil && strings.Contains(ts.Error, "is forbidden")
				}
				what := func() string {
					if ts := s.Table(view.key); ts != nil {
						return "error line " + ts.Error
					}
					return "no snapshot"
				}
				first := settledRefusal(ctx, t, srv, shown, what, view.resource)
				if first[view.resource] > 2 { // in all namespaces, then in the context's namespace
					t.Errorf("%s was listed %d times, want at most 2 (reads %v)", view.resource, first[view.resource], srv.RequestsIn())
				}
				refreshAccess(ctx, t, s)
				if !testkit.WaitFor(ctx, func() bool { return refusedLists(srv, view.resource)[view.resource] > first[view.resource] }) {
					t.Fatalf("cluster.refreshAccess did not read %s again: %v", view.resource, srv.RequestsIn())
				}
				settledRefusal(ctx, t, srv, shown, what, view.resource)
			})
		}
	}
}

// TestBudgetPartiallyRefusedRead: the Stateful Sets view of a user who may list them in team-a but
// not in team-b, both accessible namespaces, whom the access reviews cannot tell about: team-b's
// refusal is the view's error line and team-b is listed once, while team-a's are read and watched;
// cluster.refreshAccess reads team-b again.
func TestBudgetPartiallyRefusedRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	u := refusedUser{name: "team-a's Stateful Sets", ns: "team-a", incomplete: true, teamA: []string{"statefulsets"}, accessible: []string{"team-a", "team-b"}}
	s, srv := refusedSession(ctx, t, u, 0)
	srv.Set(refusedWorkloadGVRs["StatefulSet"], testkit.Object("apps/v1", "StatefulSet", "team-a", "db", nil))
	s.OpenTab("statefulsets", false)
	teamB := func() int64 {
		reads := srv.RequestsIn()
		return reads["list statefulsets.apps in team-b"] + reads["watchlist statefulsets.apps in team-b"]
	}
	if !testkit.WaitFor(ctx, func() bool {
		ts := s.Table("statefulsets")
		return ts != nil && ts.Synced && len(ts.Rows) == 1 && strings.Contains(ts.Error, "in team-b") && strings.Contains(ts.Error, "is forbidden")
	}) {
		t.Fatalf("the view never showed team-a's row and team-b's refusal: %+v (reads %v)", s.Table("statefulsets"), srv.RequestsIn())
	}
	time.Sleep(refusedWindow + time.Second) // client-go's backoff would list team-b twice more by then
	if n := teamB(); n != 1 {
		t.Errorf("team-b's Stateful Sets were listed %d times, want once (reads %v)", n, srv.RequestsIn())
	}
	if ts := s.Table("statefulsets"); !ts.Synced || len(ts.Rows) != 1 || !strings.Contains(ts.Error, "in team-b") {
		t.Errorf("the view lost team-a's row or team-b's refusal: %+v", ts)
	}
	srv.Set(refusedWorkloadGVRs["StatefulSet"], testkit.Object("apps/v1", "StatefulSet", "team-a", "cache", nil))
	if !testkit.WaitFor(ctx, func() bool { ts := s.Table("statefulsets"); return ts != nil && len(ts.Rows) == 2 }) {
		t.Errorf("team-a's watch went with team-b's refusal: %+v", s.Table("statefulsets"))
	}

	refreshAccess(ctx, t, s)
	if !testkit.WaitFor(ctx, func() bool { return teamB() > 1 }) {
		t.Fatalf("cluster.refreshAccess did not read team-b again: %v", srv.RequestsIn())
	}
}

// TestBudgetRelatedReadsRefused: the reads a list view or a drawer makes of its related objects
// (the pods behind the Nodes, the Endpoint Slices behind the Services, a Deployment's drawer's Replica
// Sets, pods and events) for a user the access reviews cannot tell about who may read the view's
// kind but not those: each refused read is listed in all namespaces and then in the context's
// namespace, and not again while the view or drawer is open.
func TestBudgetRelatedReadsRefused(t *testing.T) {
	for _, c := range []struct {
		name      string
		u         refusedUser
		view      string
		drawer    bool
		resources []string
	}{
		{"the Nodes' pods, cluster-wide", refusedUser{incomplete: true, cluster: []string{"nodes"}, noPods: true}, "nodes", false, []string{"pods"}},
		{"the Nodes' pods, namespace-limited", refusedUser{ns: "team-a", incomplete: true, cluster: []string{"nodes"}, noPods: true}, "nodes", false, []string{"pods"}},
		{"the Services' Endpoint Slices", refusedUser{incomplete: true, allow: []string{"services"}}, "services", false, []string{"endpointslices.discovery.k8s.io"}},
		{"a Deployment's drawer, cluster-wide", refusedUser{incomplete: true, noPods: true}, "deployments", true, []string{"replicasets.apps", "pods", "events"}},
		{"a Deployment's drawer, namespace-limited", refusedUser{ns: "team-a", incomplete: true, noPods: true}, "deployments", true, []string{"replicasets.apps", "pods", "events"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			s, srv := refusedSession(ctx, t, c.u, 0)
			ns := c.u.ns
			if ns == "" {
				ns = "default"
			}
			s.OpenTab(c.view, false)
			if c.drawer {
				srv.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", ns, "web", nil))
				if !testkit.WaitFor(ctx, func() bool { ts := s.Table(c.view); return ts != nil && len(ts.Rows) == 1 }) {
					t.Fatalf("the Deployment never showed: %+v", s.Table(c.view))
				}
				s.UpdateUI(func(u *engine.UIState) {
					u.Drawer = &model.ObjectRef{Context: "fake", Kind: c.view, Namespace: ns, Name: "web"}
				})
			}
			// Refused in the context's namespace: the read namespace by namespace was refused too.
			shown := func() bool {
				reads := srv.RequestsIn()
				for _, r := range c.resources {
					if reads["list "+r+" in "+ns]+reads["watchlist "+r+" in "+ns] == 0 {
						return false
					}
				}
				return true
			}
			what := func() string { return fmt.Sprint(refusedLists(srv, c.resources...)) }
			settledRefusal(ctx, t, srv, shown, what, c.resources...)
		})
	}
}

// TestRelatedRefusalsAreNamed: a list view whose related read the user may not list says so on its
// error line ("Cogpit may not list pods") and shows every cell read from it as "—", never 0 or
// empty: the Nodes' pods, the Namespaces' pods behind the requests, whether the access reviews say
// no or the API server refuses.
func TestRelatedRefusalsAreNamed(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		t.Run(fmt.Sprintf("the access reviews cannot tell: %v", incomplete), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			u := refusedUser{ns: "team-a", incomplete: incomplete, cluster: []string{"nodes", "namespaces"}, noPods: true}
			s, srv := refusedSession(ctx, t, u, 0)
			srv.Set(testkit.NodesGVR, testkit.Object("v1", "Node", "", "node-1", nil))
			srv.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", "team-a", nil))
			for _, c := range []struct{ view, column string }{{"nodes", ""}, {"namespaces", "cpuRequests"}} {
				s.OpenTab(c.view, false)
				var ts *engine.TableSnapshot
				if !testkit.WaitFor(ctx, func() bool {
					ts = s.Table(c.view)
					return ts != nil && len(ts.Rows) == 1 && ts.Error == "Cogpit may not list pods"
				}) {
					t.Fatalf("%s: %+v", c.view, ts)
				}
				if c.column == "" {
					continue
				}
				i := slices.IndexFunc(ts.Columns, func(col model.Column) bool { return col.ID == c.column })
				if i < 0 {
					t.Fatalf("%s has no column %s", c.view, c.column)
				}
				if cell := ts.Rows[0].Cells[i]; cell != model.NoCountCell() && (cell.Text != model.NoCount || cell.Parts == nil || !(*cell.Parts)[0].Muted) {
					t.Errorf("%s's %s cell: %+v, want %q muted", c.view, c.column, cell, model.NoCount)
				}
			}
		})
	}
}
