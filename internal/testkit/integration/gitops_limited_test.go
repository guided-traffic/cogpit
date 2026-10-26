package integration_test

import (
	"context"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The Flux and Argo CD dashboards of a namespace-limited user (G3, G23; FIXUP 2026-10-14), measured
// at the HTTP level against a fake API server that enforces RBAC: the dashboards were built before
// the namespace-limited fixes, with workers of their own. A user who may read the custom resource
// definitions (a ClusterRole, as Flux's and Argo CD's tenants are often given) and the GitOps kinds
// in team-a alone, or in the accessible namespaces team-a and team-b, opens either dashboard, as a
// page and as an agent's read with no page open: the reads end in the user's namespaces, a read in
// all namespaces the access reviews refuse is never sent, and a refusal the API server gives is
// followed as the views and the Overviews follow it. A cluster-admin's counts are one read per
// kind in all namespaces, as before.

var (
	gitopsKustomizationsGVR = schema.GroupVersionResource{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"}
	gitopsGitReposGVR       = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "gitrepositories"}
)

// gitopsResources are what the dashboards read, as the server counts them.
var (
	fluxReads = []string{"kustomizations.kustomize.toolkit.fluxcd.io", "gitrepositories.source.toolkit.fluxcd.io"}
	argoReads = []string{"applications.argoproj.io", "applicationsets.argoproj.io", "appprojects.argoproj.io"}
)

// gitopsObject is a Flux object with a Ready condition, or an Argo CD Application.
func gitopsFlux(apiVersion, kind, namespace, name, ready string) *unstructured.Unstructured {
	obj := testkit.Object(apiVersion, kind, namespace, name, nil)
	obj.Object["status"] = map[string]any{"conditions": []any{map[string]any{
		"type": "Ready", "status": ready, "message": name + " is " + ready, "lastTransitionTime": "2026-10-14T06:00:00Z",
	}}}
	return obj
}

// connectGitOps connects an engine to a fake API server that serves the definitions, Flux's
// Kustomizations and Git repositories, Argo CD's three kinds and Secrets, enforcing access's
// rules, with team-a as the context's namespace and the accessible namespaces set before
// connecting. team-a holds two Kustomizations (one not ready), a Git repository, two Applications
// (one out of sync), a project and a repository's Secret; team-b and team-c one Kustomization and
// one Application each.
func connectGitOps(ctx context.Context, t *testing.T, access *testkit.FakeAccess, accessible []string) (*engine.Session, *testkit.FakeAPIServer) {
	t.Helper()
	resources := append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
		testkit.FakeResource{GVR: testkit.CRDsGVR, Kind: "CustomResourceDefinition"},
		testkit.FakeResource{GVR: testkit.SecretsGVR, Kind: "Secret", Namespaced: true},
		testkit.FakeResource{GVR: gitopsKustomizationsGVR, Kind: "Kustomization", Namespaced: true},
		testkit.FakeResource{GVR: gitopsGitReposGVR, Kind: "GitRepository", Namespaced: true},
		testkit.FakeResource{GVR: testkit.ArgoApplicationsGVR, Kind: "Application", Namespaced: true},
		testkit.FakeResource{GVR: testkit.ArgoAppSetsGVR, Kind: "ApplicationSet", Namespaced: true},
		testkit.FakeResource{GVR: testkit.ArgoProjectsGVR, Kind: "AppProject", Namespaced: true})
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "limited-token", Access: access, Resources: resources})
	t.Cleanup(srv.Close)
	srv.Set(testkit.CRDsGVR, testkit.CRD(gitopsKustomizationsGVR.Group, gitopsKustomizationsGVR.Resource, "Kustomization", true))
	srv.Set(testkit.CRDsGVR, testkit.CRD(gitopsGitReposGVR.Group, gitopsGitReposGVR.Resource, "GitRepository", true))
	for _, crd := range testkit.ArgoCRDs() {
		srv.Set(testkit.CRDsGVR, crd)
	}
	ks := func(ns, name, ready string) {
		srv.Set(gitopsKustomizationsGVR, gitopsFlux("kustomize.toolkit.fluxcd.io/v1", "Kustomization", ns, name, ready))
	}
	ks("team-a", "apps", "True")
	ks("team-a", "broken", "False")
	ks("team-b", "infra", "True")
	ks("team-c", "other", "False")
	srv.Set(gitopsGitReposGVR, gitopsFlux("source.toolkit.fluxcd.io/v1", "GitRepository", "team-a", "repo", "True"))
	srv.Set(testkit.ArgoApplicationsGVR, testkit.ArgoApplication("team-a", "web", "Synced", "Healthy"))
	srv.Set(testkit.ArgoApplicationsGVR, testkit.ArgoApplication("team-a", "api", "OutOfSync", "Degraded"))
	srv.Set(testkit.ArgoApplicationsGVR, testkit.ArgoApplication("team-b", "db", "Synced", "Healthy"))
	srv.Set(testkit.ArgoApplicationsGVR, testkit.ArgoApplication("team-c", "queue", "OutOfSync", "Missing"))
	srv.Set(testkit.ArgoProjectsGVR, testkit.Object("argoproj.io/v1alpha1", "AppProject", "team-a", "default", nil))
	srv.Set(testkit.SecretsGVR, testkit.ArgoRepoSecret("team-a", "apps-repo", "https://repo.example.com/apps.git", "not-a-real-password"))
	s := engine.New(engine.Options{
		Connector:       &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "team-a")}},
		ClusterSettings: engine.NewClusterSettingsStore(t.TempDir()),
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if len(accessible) > 0 {
		if _, err := s.ChangeClusterSettings("fake", func(c *engine.ClusterSettings) error { c.AccessibleNamespaces = accessible; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	s.UpdateUI(func(u *engine.UIState) { u.View = "pods" }) // no GitOps kind open
	if !testkit.WaitFor(ctx, func() bool { return s.Backend().CustomKindsSettled() }) {
		t.Fatal("the definitions were never listed")
	}
	return s, srv
}

// waitGitOpsKinds waits for the Flux and Argo CD kinds of the definitions.
func waitGitOpsKinds(ctx context.Context, t *testing.T, s *engine.Session) {
	t.Helper()
	if !testkit.WaitFor(ctx, func() bool {
		b := s.Backend()
		_, flux := b.Profile(kinds.FluxKinds()[1].Key) // Kustomizations
		_, argo := b.Profile(kinds.ArgoApplicationsKey)
		return flux && argo
	}) {
		t.Fatal("the Flux and Argo CD kinds never came")
	}
}

// fluxCounts is the Flux dashboard as "Title count" per kind, with an error.
func fluxCounts(m *engine.FluxDashboardModel) string {
	var parts []string
	for _, k := range m.Kinds {
		parts = append(parts, k.Title+" "+strconv.Itoa(k.Count))
	}
	return strings.Join(parts, ", ")
}

// argoCounts is the Argo CD dashboard's cards as "Title count".
func argoCounts(m *engine.ArgoCDDashboardModel) string {
	var parts []string
	for _, k := range m.Kinds {
		parts = append(parts, k.Title+" "+strconv.Itoa(k.Count))
	}
	return strings.Join(parts, ", ")
}

// gitopsUser is one user of the test: the rules, the accessible namespaces, and what each
// dashboard counts once it has read everything the user may read.
type gitopsUser struct {
	name       string
	access     *testkit.FakeAccess
	accessible []string
	flux, argo string
	wide       bool // the user may list everything in all namespaces
	// reads checks the server's reads of one resource the dashboards read.
	reads func(t *testing.T, resource string, reads map[string]int64)
}

func gitopsUsers() []gitopsUser {
	crds := testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{"apiextensions.k8s.io"}, Resources: []string{"customresourcedefinitions"}}
	gitops := testkit.FakeRule{Verbs: []string{"get", "list", "watch"},
		APIGroups: []string{"", "kustomize.toolkit.fluxcd.io", "source.toolkit.fluxcd.io", "argoproj.io"},
		Resources: []string{"pods", "secrets", "kustomizations", "gitrepositories", "applications", "applicationsets", "appprojects"}}
	all := testkit.FakeRule{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}
	return []gitopsUser{
		{
			name:   "a RoleBinding in the context's namespace alone",
			access: &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{"": {crds}, "team-a": {gitops}}},
			flux:   "Kustomizations 2, Git Repositories 1", argo: "Applications 2, Application Sets 0, Projects 1, Repositories 1, Clusters 0",
			// The rules of team-a allow the kinds, so the plan reads them in all namespaces first:
			// a refused read (a streaming watch, and the plain list client-go falls back to), then
			// team-a's alone, whoever met the refusal. A scope a handle holds is retried by
			// client-go's backoff (800 ms first) until the engine's new plan lets it go; under the
			// race detector that plan can land after a retry or two (CI, 2026-10-16: three refused
			// reads), so up to three refused attempts pass, never a read beside these three.
			reads: func(t *testing.T, res string, reads map[string]int64) {
				wide := reads["list "+res]
				if wide > 6 || reads["list "+res+" in team-a"] != 1 || reads["watch "+res+" in team-a"] != 1 || len(reads) > 3 {
					t.Errorf("%s: reads %v, want the refused reads in all namespaces (and their lists) to end, then team-a's", res, reads)
				}
			},
		},
		{
			name: "accessible namespaces team-a and team-b",
			access: &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{
				"": {crds}, "team-a": {gitops}, "team-b": {gitops},
			}},
			accessible: []string{"team-a", "team-b"},
			flux:       "Kustomizations 3, Git Repositories 1", argo: "Applications 3, Application Sets 0, Projects 1, Repositories 1, Clusters 0",
			// The access reviews say no before anything is read: never a read in all namespaces.
			reads: func(t *testing.T, res string, reads map[string]int64) {
				want := map[string]int64{"list " + res + " in team-a": 1, "watch " + res + " in team-a": 1,
					"list " + res + " in team-b": 1, "watch " + res + " in team-b": 1}
				if !maps.Equal(reads, want) {
					t.Errorf("%s: reads %v, want %v (one list and one watch per namespace, nothing in all namespaces)", res, reads, want)
				}
			},
		},
		{
			name:   "a cluster-admin",
			access: &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{"": {all}}},
			wide:   true,
			flux:   "Kustomizations 4, Git Repositories 1", argo: "Applications 4, Application Sets 0, Projects 1, Repositories 1, Clusters 0",
			reads: func(t *testing.T, res string, reads map[string]int64) {
				if want := map[string]int64{"list " + res: 1, "watch " + res: 1}; !maps.Equal(reads, want) {
					t.Errorf("%s: reads %v, want %v (as before)", res, reads, want)
				}
			},
		},
	}
}

func TestGitOpsDashboardsOfANamespaceLimitedUser(t *testing.T) {
	for _, u := range gitopsUsers() {
		t.Run(u.name, func(t *testing.T) {
			for _, page := range []bool{true, false} {
				how := "an agent's read"
				if page {
					how = "the page"
				}
				t.Run("Flux, "+how, func(t *testing.T) { checkFluxDashboard(t, u, page) })
				t.Run("Argo CD, "+how, func(t *testing.T) { checkArgoDashboard(t, u, page) })
			}
		})
	}
}

// checkFluxDashboard opens the Flux dashboard (the page, or an agent's read with no page open) and
// checks its counts and the server's reads.
func checkFluxDashboard(t *testing.T, u gitopsUser, page bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	s, srv := connectGitOps(ctx, t, u.access, u.accessible)
	waitGitOpsKinds(ctx, t, s)
	start := time.Now()
	var m *engine.FluxDashboardModel
	if page {
		s.OpenTab(engine.FluxDashboard, false)
		if !testkit.WaitFor(ctx, func() bool {
			var err error
			m, _, err = s.FluxDashboard(ctx, "", []string{}, false)
			return err == nil && m.Synced && m.Error == "" && fluxCounts(m) == u.flux
		}) {
			t.Fatalf("the page never counted %q: %+v (reads %v)", u.flux, m, srv.RequestsIn())
		}
	} else {
		var err error
		m, _, err = s.FluxDashboard(ctx, "", []string{}, true)
		if err != nil || !m.Synced || m.Error != "" || fluxCounts(m) != u.flux {
			t.Fatalf("an agent's read: %+v, %v, want %q (reads %v)", m, err, u.flux, srv.RequestsIn())
		}
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the dashboard took %s", d)
	}
	settle(ctx, t, s, u)
	for _, res := range fluxReads {
		u.reads(t, res, readsOf(srv, res))
	}
}

// checkArgoDashboard is checkFluxDashboard for Argo CD's.
func checkArgoDashboard(t *testing.T, u gitopsUser, page bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	s, srv := connectGitOps(ctx, t, u.access, u.accessible)
	waitGitOpsKinds(ctx, t, s)
	start := time.Now()
	var m *engine.ArgoCDDashboardModel
	if page {
		s.OpenTab(engine.ArgoCDDashboard, false)
		if !testkit.WaitFor(ctx, func() bool {
			var err error
			m, _, err = s.ArgoCDDashboard(ctx, "", []string{}, false)
			return err == nil && m.Synced && m.Error == "" && argoCounts(m) == u.argo && argoKindsClean(m)
		}) {
			t.Fatalf("the page never counted %q: %+v (reads %v)", u.argo, m, srv.RequestsIn())
		}
	} else {
		var err error
		m, _, err = s.ArgoCDDashboard(ctx, "", []string{}, true)
		if err != nil || !m.Synced || m.Error != "" || argoCounts(m) != u.argo || !argoKindsClean(m) {
			t.Fatalf("an agent's read: %+v, %v, want %q (reads %v)", m, err, u.argo, srv.RequestsIn())
		}
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the dashboard took %s", d)
	}
	settle(ctx, t, s, u)
	secrets := argoSecretReads(srv)
	if len(secrets) != 2 {
		t.Errorf("Argo CD's Secrets read as %q, want the Repositories' and the Clusters' scopes", secrets)
	}
	for _, res := range append(argoReads, secrets...) {
		u.reads(t, res, readsOf(srv, res))
	}
}

// argoKindsClean reports whether every card of the Argo CD dashboard read its kind without an
// error.
func argoKindsClean(m *engine.ArgoCDDashboardModel) bool {
	for _, k := range m.Kinds {
		if k.Error != "" || !k.Synced {
			return false
		}
	}
	return true
}

// settle waits until the cache holds no read in all namespaces of a kind the dashboards read,
// for a user who may not list them there (a cluster-admin's are what stays): a refused read that
// were kept would be retried, and counted.
func settle(ctx context.Context, t *testing.T, s *engine.Session, u gitopsUser) {
	t.Helper()
	if u.wide {
		return
	}
	if !testkit.WaitFor(ctx, func() bool {
		for _, st := range s.Backend().Cache.Stats() {
			if g := st.Scope.GVR.Group; st.Scope.Namespace == "" && (g == "argoproj.io" || strings.HasSuffix(g, ".fluxcd.io") || st.Scope.GVR.Resource == "secrets") {
				return false
			}
		}
		return true
	}) {
		t.Errorf("a read in all namespaces is left: %+v", s.Backend().Cache.Stats())
	}
}

// inNamespace is the namespace RequestsIn writes after a resource read in one namespace.
var inNamespace = regexp.MustCompile(` in [a-z0-9-]+$`)

// argoSecretReads are the label-selected Secret reads the server was sent (Argo CD's
// Repositories and Clusters), by resource as RequestsIn names it.
func argoSecretReads(srv *testkit.FakeAPIServer) []string {
	seen := map[string]bool{}
	var out []string
	for k := range srv.RequestsIn() {
		_, rest, _ := strings.Cut(k, " ")
		res := inNamespace.ReplaceAllString(rest, "") // "… in (repo-creds,repository) in team-a"
		if strings.HasPrefix(res, "secrets with argocd.argoproj.io/secret-type") && !seen[res] {
			seen[res] = true
			out = append(out, res)
		}
	}
	return out
}

// A namespace-limited user whose Role names the GitOps kinds but who may not list their
// definitions (the rule rather than the exception: listing definitions takes a ClusterRole). Before
// 2026-10-14 both dashboards said the cluster serves none; then that Cogpit cannot tell, with the
// server's reason. Since A90 (the owner's decision of 2026-10-14 evening) Cogpit recognises Flux
// and Argo CD from API discovery and shows them with their curated profiles: both dashboards, as
// pages and as agents' reads, count team-a's objects as for a user who may list the definitions,
// with the same reads of each kind (one refused read in all namespaces and its list, then team-a's).
// The definitions are refused once.
func TestGitOpsDashboardsWithoutTheDefinitions(t *testing.T) {
	u := gitopsUsers()[0] // a RoleBinding in the context's namespace alone
	u.name += ", without the definitions"
	u.access = &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{"team-a": u.access.Rules["team-a"]}}
	for _, page := range []bool{true, false} {
		how := "an agent's read"
		if page {
			how = "the page"
		}
		t.Run("Flux, "+how, func(t *testing.T) { checkFluxDashboard(t, u, page) })
		t.Run("Argo CD, "+how, func(t *testing.T) { checkArgoDashboard(t, u, page) })
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, srv := connectGitOps(ctx, t, u.access, nil)
	waitGitOpsKinds(ctx, t, s)
	if set := s.Backend().CustomProfiles(); len(set) != len(kinds.ArgoSettings())+5 {
		var keys []string
		for _, p := range set {
			keys = append(keys, p.Key)
		}
		t.Errorf("custom kinds %q, want Kustomizations, Git repositories, Argo CD's three and its settings", keys)
	}
	if crds := readsOf(srv, "customresourcedefinitions.apiextensions.k8s.io"); crds["list customresourcedefinitions.apiextensions.k8s.io"] < 1 || len(crds) > 1 {
		t.Errorf("definitions: reads %v, want the refused list alone", crds)
	}
}
