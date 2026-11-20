package integration_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The integrations' pages (Image Security, Velero) for a user who may not list their kinds, as
// refused_readers_test.go measures the other readers (D-177): the refusal is the page's state,
// never a list client-go's backoff keeps sending while the page is open, and cluster.refreshAccess
// reads it again.

const (
	vulnReports   = "vulnerabilityreports.aquasecurity.github.io"
	veleroBackups = "backups.velero.io"
	veleroScheds  = "schedules.velero.io"
)

// TestBudgetImageSecurityRefusedReports: the Image Security page and the Trivy integration under
// it. The access reviews that say no cost no list and the page's error line names the reports; the
// API server's refusal costs each reader (the integration's and the page's trimmed scopes) a list in
// all namespaces and one in the user's namespace, and nothing after it.
func TestBudgetImageSecurityRefusedReports(t *testing.T) {
	for _, u := range refusedUsers() {
		t.Run(u.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			s, srv := refusedSession(ctx, t, u, engine.IntTrivy)
			s.OpenTab(engine.ImageSecurity, false)
			text, want := mayNotListText(vulnReports), int64(0)
			if u.incomplete {
				text, want = "is forbidden", 4
			}
			shown, what := pageErrorShows(s, engine.ImageSecurity, text)
			first := settledRefusal(ctx, t, srv, shown, what, vulnReports)
			if first[vulnReports] > want {
				t.Errorf("the reports were listed %d times, want at most %d (reads %v)", first[vulnReports], want, srv.RequestsIn())
			}
			if ts := s.Table(engine.ImageSecurity); ts.Total != 0 || len(ts.Rows) != 0 {
				t.Errorf("the refused page counts %d findings", ts.Total)
			}
			if !u.incomplete {
				return
			}
			refreshAccess(ctx, t, s)
			if !testkit.WaitFor(ctx, func() bool { return refusedLists(srv, vulnReports)[vulnReports] > first[vulnReports] }) {
				t.Fatalf("cluster.refreshAccess did not read the reports again: %v", srv.RequestsIn())
			}
			settledRefusal(ctx, t, srv, shown, what, vulnReports)
		})
	}
}

// TestBudgetVeleroRefusedKinds: the Velero page for a user who may not list Velero's kinds. Each
// tile names its kind's refusal and counts nothing, the model names the kinds it could not read;
// nothing refused is listed again while the page is open; cluster.refreshAccess reads them again.
func TestBudgetVeleroRefusedKinds(t *testing.T) {
	for _, u := range refusedUsers() {
		t.Run(u.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			s, srv := veleroRefusedSession(ctx, t, u)
			s.OpenTab(engine.VeleroDashboard, false)
			text, want := mayNotListText(veleroBackups), int64(0)
			if u.incomplete {
				text, want = "is forbidden", 2 // in all namespaces, then in the user's namespace
			}
			var tile engine.VeleroTile
			shown := func() bool {
				ts := s.Table(engine.VeleroDashboard)
				if ts == nil || !ts.Synced {
					return false // the page's own build first: an agent's read of a page not built reads for itself
				}
				m, err := s.VeleroDashboard(ctx, "", nil)
				if err != nil || len(m.Tiles) < 2 {
					return false
				}
				tile = m.Tiles[1] // Backups
				return tile.Kind == veleroBackups && strings.Contains(tile.Error, text) && tile.Count == 0 && slices.Contains(m.Unread, veleroBackups)
			}
			what := func() string { return "Backups tile " + tile.Error }
			first := settledRefusal(ctx, t, srv, shown, what, veleroBackups, veleroScheds)
			for _, r := range []string{veleroBackups, veleroScheds} {
				if first[r] > want {
					t.Errorf("%s was listed %d times, want at most %d (reads %v)", r, first[r], want, srv.RequestsIn())
				}
			}
			if ts := s.Table(engine.VeleroDashboard); strings.Contains(ts.Error, veleroBackups) || strings.Contains(ts.Error, "Backup") {
				t.Errorf("the page's error line repeats the Backups tile's refusal: %q", ts.Error)
			}
			if !u.incomplete {
				return
			}
			refreshAccess(ctx, t, s)
			if !testkit.WaitFor(ctx, func() bool { return refusedLists(srv, veleroBackups)[veleroBackups] > first[veleroBackups] }) {
				t.Fatalf("cluster.refreshAccess did not read Velero's Backups again: %v", srv.RequestsIn())
			}
			settledRefusal(ctx, t, srv, shown, what, veleroBackups, veleroScheds)
		})
	}
}

// mayNotListText is what a reader says of a kind the access reviews deny (D-177).
func mayNotListText(resource string) string { return "Cogpit may not list " + resource }

// pageErrorShows reports whether an open page's latest snapshot (the window's: no read of its own,
// as an agent's would be) is synced with text in its error line, and what that line says.
func pageErrorShows(s *engine.Session, view, text string) (shown func() bool, what func() string) {
	shown = func() bool {
		ts := s.Table(view)
		return ts != nil && ts.Synced && strings.Contains(ts.Error, text)
	}
	what = func() string {
		if ts := s.Table(view); ts != nil {
			return "error line " + ts.Error
		}
		return "no snapshot"
	}
	return shown, what
}

// veleroRefusedSession connects u (refusedUser: pods, namespaces and Deployments, in team-a alone or
// everywhere) to a fake API server that serves Velero's Backups and Schedules with their
// definitions, which u may not list.
func veleroRefusedSession(ctx context.Context, t *testing.T, u refusedUser) (*engine.Session, *testkit.FakeAPIServer) {
	t.Helper()
	resources := append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
		testkit.FakeResource{GVR: testkit.CRDsGVR, Kind: "CustomResourceDefinition"},
		testkit.FakeResource{GVR: testkit.VeleroBackupsGVR, Kind: "Backup", Namespaced: true},
		testkit.FakeResource{GVR: testkit.VeleroSchedulesGVR, Kind: "Schedule", Namespaced: true})
	crds := testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{"apiextensions.k8s.io"}, Resources: []string{"customresourcedefinitions"}}
	reads := testkit.FakeRule{Verbs: []string{"get", "list", "watch"}, APIGroups: []string{"", "apps"}, Resources: []string{"pods", "namespaces", "deployments"}}
	rules := map[string][]testkit.FakeRule{"": {crds, reads}}
	if u.ns != "" {
		rules = map[string][]testkit.FakeRule{"": {crds}, u.ns: {reads}}
	}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "velero-token", Resources: resources,
		Access: &testkit.FakeAccess{Enforce: true, Incomplete: u.incomplete, Rules: rules}})
	t.Cleanup(srv.Close)
	srv.Set(testkit.CRDsGVR, testkit.CRD("velero.io", "backups", "Backup", true))
	srv.Set(testkit.CRDsGVR, testkit.CRD("velero.io", "schedules", "Schedule", true))
	s := engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", u.ns)}},
		ClusterSettings:  engine.NewClusterSettingsStore(t.TempDir()),
		SnapshotInterval: 10 * time.Millisecond,
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
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
