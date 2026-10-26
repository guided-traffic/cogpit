//go:build !nodemo

// The demo cluster's tests: a development build only, as connector_demo.go (D-67).

package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/demo"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// TestDemoProfilesFollowTheDemo checks that the demo cluster offers exactly the built-in kinds it
// serves, as discovery decides for a real cluster: a profile the demo has no objects for (Ingresses,
// Stateful Sets) must not become a view that only shows a 404.
func TestDemoProfilesFollowTheDemo(t *testing.T) {
	served := demo.Resources()
	offered := demoProfiles()
	if len(offered) == 0 {
		t.Fatal("the demo offers no kinds")
	}
	for _, p := range kinds.Builtin() {
		if want, got := slices.Contains(served, p.GVR()), slices.Contains(offered, p); got != want {
			t.Errorf("%s: offered %v, served by the demo %v", p.Key, got, want)
		}
	}
}

// TestDemoOffersApplicationsOnce: the demo cluster serves workload kinds, so it offers
// Applications, once (TestApplicationsOfferedWithWorkloads).
func TestDemoOffersApplicationsOnce(t *testing.T) {
	n := 0
	for _, p := range demoProfiles() {
		if p.Key == kinds.ApplicationsKey {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the demo cluster offers Applications %d times", n)
	}
}

func TestClusterConnectorContextsAndDemo(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-kubeconfig")
	c := &ClusterConnector{KubeconfigPaths: []string{missing}, IncludeDemo: true, DemoOptions: demo.Options{Pods: 20, Services: 3, Namespaces: 2, Nodes: 2, NoChurn: true}}
	list, err := c.Contexts()
	if len(list) == 0 || !list[len(list)-1].Demo || list[len(list)-1].Name != DemoContext {
		t.Errorf("demo entry missing: %+v (err %v)", list, err)
	}
	if _, statErr := os.Stat(missing); statErr == nil {
		t.Error("the connector created a kubeconfig file")
	}
	if !IsDemoContext(DemoContext) {
		t.Error("a development build does not know its demo cluster's context")
	}

	b, err := c.Connect(context.Background(), DemoContext)
	if errors.Is(err, errDemoUnavailable) {
		t.Skip("the demo package is a stub in this tree")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if !b.Demo || b.Context != DemoContext || b.Conn != nil || b.CanWrite() || len(b.Profiles) == 0 || b.DemoStats == nil {
		t.Errorf("demo backend %+v", b)
	}
	// Its API server's version, the one its kube-apiserver pods name (D-91).
	if b.ServerVersion() != demo.ServerVersion || demo.ServerVersion == "" {
		t.Errorf("the demo's API server version %q, want %q", b.ServerVersion(), demo.ServerVersion)
	}
	// Only the kinds the demo serves get a view, as discovery decides on a real cluster, and Helm's
	// releases from its Secrets and ConfigMaps and the Applications made of its deployments
	// (applications.go).
	if len(b.Profiles) != len(demo.Resources())+2 {
		t.Errorf("demo offers %d views for %d served kinds, Releases and Applications", len(b.Profiles), len(demo.Resources()))
	}
}

// TestDemoOffersHelmReleases: the demo cluster serves Secrets, so it offers Helm's releases, once,
// with a memo of its own per call (one per backend).
func TestDemoOffersHelmReleases(t *testing.T) {
	var found []*model.KindProfile
	for _, p := range demoProfiles() {
		if isHelmReleases(p) {
			found = append(found, p)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the demo cluster offers Releases %d times", len(found))
	}
	for _, p := range demoProfiles() {
		if isHelmReleases(p) && p == found[0] {
			t.Error("two demo backends share one releases profile and its memo")
		}
	}
}

// TestDemoHelmReleases: the demo's Releases view lists its three releases, the one kept in
// ConfigMaps joined with those kept in Secrets as for a real cluster (Backend.Acquire), each at its
// newest revision.
func TestDemoHelmReleases(t *testing.T) {
	c := &ClusterConnector{KubeconfigPaths: []string{filepath.Join(t.TempDir(), "none")}, IncludeDemo: true,
		DemoOptions: demo.Options{Pods: 50, Deployments: 10, Services: 3, Namespaces: 8, Nodes: 3, Events: 10, NoChurn: true}}
	s := New(Options{Connector: c, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), DemoContext); err != nil {
		if errors.Is(err, errDemoUnavailable) {
			t.Skip("the demo package is a stub in this tree")
		}
		t.Fatal(err)
	}
	p, ok := s.Backend().Profile(helm.ReleaseKind)
	if !ok {
		t.Fatal("the demo backend has no Releases")
	}
	openView(s, helm.ReleaseKind)
	snap := waitTable(t, s, helm.ReleaseKind, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 3 })
	want := map[string]struct {
		revision int64
		driver   helm.Driver
	}{
		"demo-shop/shop-web": {6, helm.DriverSecret}, "demo-shop/cache": {2, helm.DriverSecret},
		"demo-search/search-api": {3, helm.DriverConfigMap},
	}
	for _, r := range snap.Rows {
		w, ok := want[r.Ref.Namespace+"/"+r.Ref.Name]
		obj, err := s.Lookup(r.Ref)
		if !ok || err != nil || r.Cells[p.ColumnIndex("revision")].Num != w.revision ||
			r.Cells[p.ColumnIndex("status")].Text != "Deployed" || helm.ReleaseStorage(obj) != w.driver {
			t.Errorf("row %s: %+v (lookup %v)", r.Ref.Key(), r.Cells, err)
		}
	}
}

// TestDemoApplications: the demo cluster's deployments carry the recommended labels, so its
// Applications view groups them: every row an instance of its namespace, with a manager and a
// version, and its drawer's workloads all of that namespace.
func TestDemoApplications(t *testing.T) {
	c := &ClusterConnector{KubeconfigPaths: []string{filepath.Join(t.TempDir(), "none")}, IncludeDemo: true,
		DemoOptions: demo.Options{Pods: 300, Deployments: 60, Services: 3, Namespaces: 6, Nodes: 3, NoChurn: true}}
	s := New(Options{Connector: c, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	if err := s.Connect(context.Background(), DemoContext); err != nil {
		if errors.Is(err, errDemoUnavailable) {
			t.Skip("the demo package is a stub in this tree")
		}
		t.Fatal(err)
	}
	apps, _ := s.Backend().Profile(kinds.ApplicationsKey)
	openView(s, kinds.ApplicationsKey)
	snap := waitTable(t, s, kinds.ApplicationsKey, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) > 0 })
	for _, r := range snap.Rows {
		if r.Ref.Namespace == "kube-system" || r.Cells[apps.ColumnIndex("managedBy")].Text == "" ||
			r.Cells[apps.ColumnIndex("version")].Text == "" || r.Cells[apps.ColumnIndex("workloads")].Num == 0 {
			t.Errorf("row %s: %+v", r.Ref.Key(), r.Cells)
		}
	}
	t.Logf("%d applications of 60 deployments", len(snap.Rows))
}
