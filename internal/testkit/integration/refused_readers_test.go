package integration_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// Readers that hold their reads while they are shown, for a user who may not list what they read,
// measured at the HTTP level against a fake API server that enforces RBAC: the refusal is a state
// the reader shows, never a list client-go's backoff keeps sending while it is shown, and
// cluster.refreshAccess reads it again (D-new). The user may list pods, namespaces and Deployments
// in all namespaces, or in team-a alone; the access reviews either tell, or cannot (a webhook
// authorizer: every rules review incomplete), and then the API server's refusal is the first word.

// refusedTrivyGVR is Trivy Operator's VulnerabilityReport resource.
var refusedTrivyGVR = schema.GroupVersionResource{Group: "aquasecurity.github.io", Version: "v1alpha1", Resource: "vulnerabilityreports"}

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
// the events, the nodes, Secrets, and Trivy Operator's, Flux's and Argo CD's kinds with their
// definitions; with trivy, the Trivy integration is on.
func refusedSession(ctx context.Context, t *testing.T, u refusedUser, trivy bool) (*engine.Session, *testkit.FakeAPIServer) {
	t.Helper()
	resources := append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
		testkit.FakeResource{GVR: testkit.CRDsGVR, Kind: "CustomResourceDefinition"},
		testkit.FakeResource{GVR: testkit.NodesGVR, Kind: "Node"},
		testkit.FakeResource{GVR: testkit.EventsGVR, Kind: "Event", Namespaced: true},
		testkit.FakeResource{GVR: testkit.SecretsGVR, Kind: "Secret", Namespaced: true},
		testkit.FakeResource{GVR: refusedTrivyGVR, Kind: "VulnerabilityReport", Namespaced: true},
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
	rules := map[string][]testkit.FakeRule{"": {crds, reads}}
	if u.ns != "" {
		rules = map[string][]testkit.FakeRule{"": {crds}, u.ns: {reads}}
	}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "refused-token", Resources: resources,
		Access: &testkit.FakeAccess{Enforce: true, Incomplete: u.incomplete, Rules: rules}})
	t.Cleanup(srv.Close)
	srv.Set(testkit.CRDsGVR, testkit.CRD(refusedTrivyGVR.Group, refusedTrivyGVR.Resource, "VulnerabilityReport", true,
		testkit.CRDVersion{Name: refusedTrivyGVR.Version, Served: true, Storage: true}))
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
	if trivy {
		if _, err := s.ChangeClusterSettings("fake", func(c *engine.ClusterSettings) error {
			c.SetIntegrations(engine.IntAll, engine.IntTrivy, 0)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
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
			s, srv := refusedSession(ctx, t, u, true)
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
				s, srv := refusedSession(ctx, t, u, false)
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
	s, srv := refusedSession(ctx, t, u, false)
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
