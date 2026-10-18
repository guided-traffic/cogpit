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
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// TestDemoProfilesFollowTheDemo checks that the demo cluster offers exactly the built-in kinds it
// serves, as discovery decides for a real cluster: a profile the demo has no objects for (Ingresses,
// Config Maps) must not become a view that only shows a 404.
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
	// Only the kinds the demo serves get a view, as discovery decides on a real cluster, and the
	// Applications made of its deployments (applications.go).
	if len(b.Profiles) != len(demo.Resources())+1 {
		t.Errorf("demo offers %d views for %d served kinds and Applications", len(b.Profiles), len(demo.Resources()))
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
