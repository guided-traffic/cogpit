package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

var gadgetsGVR = schema.GroupVersionResource{Group: "e2e.cogpit.test", Version: "v1", Resource: "gadgets"}

// TestDiscoveryCacheAcrossConnects follows the discovery cache on disk (D-65) through the engine on
// a real client-go stack against the fake API server: the first connection asks discovery and
// writes it, confirmed by the definitions' watch once it listed them; the next connection reads it
// from the file (its discovery requests fall by what discovery cost) and its first build of the
// custom kinds keeps it; a definition added while connected drops it from memory and disk, so the
// connection after that asks the server again and sees the new kind.
func TestDiscoveryCacheAcrossConnects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Resources: append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
		testkit.FakeResource{GVR: testkit.CRDsGVR, Kind: "CustomResourceDefinition"},
		testkit.FakeResource{GVR: widgetsGVR, Kind: "Widget", Namespaced: true},
		testkit.FakeResource{GVR: gadgetsGVR, Kind: "Gadget", Namespaced: true},
	)})
	defer srv.Close()
	srv.Set(testkit.CRDsGVR, testkit.CRD("e2e.cogpit.test", "widgets", "Widget", true, widgetCRD()...))
	kubeconfig := srv.WriteKubeconfig(t, "fake", "")
	dir := filepath.Join(t.TempDir(), "discovery")

	// connect opens a session on the fake cluster, waits for the custom kinds' first build from
	// the definitions, and returns its connection's discovery requests and answer.
	connect := func(name, key string) (*engine.Session, *cluster.Conn, int64) {
		t.Helper()
		s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}, DiscoveryCacheDir: dir},
			SnapshotInterval: 10 * time.Millisecond}) // the custom kinds' rebuild coalesces five intervals
		if err := s.Connect(ctx, "fake"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !testkit.WaitFor(ctx, func() bool { _, ok := s.Backend().Profile(key); return ok }) {
			t.Fatalf("%s: the kind %s never appeared", name, key)
		}
		conn := s.Backend().Conn
		if !testkit.WaitFor(ctx, func() bool { return conn.DiscoveryInfo().Checked }) {
			t.Fatalf("%s: the definitions never confirmed discovery: %+v", name, conn.DiscoveryInfo())
		}
		return s, conn, conn.Counter().Snapshot().ByVerb[cluster.VerbDiscovery]
	}

	first, conn, asked := connect("first", widgetsKey)
	if info := conn.DiscoveryInfo(); info.Source != "server" {
		t.Errorf("first connection's discovery %+v, want from the server", info)
	}
	first.Close()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("%d cache files after the first connection, want 1", len(entries))
	}

	second, conn, fromDisk := connect("second", widgetsKey)
	if info := conn.DiscoveryInfo(); info.Source != "disk" {
		t.Errorf("second connection's discovery %+v, want from the disk cache", info)
	}
	// The fake server's legacy discovery: /api, /api/v1, /apis and one per group version.
	if saved := asked - fromDisk; saved < 4 {
		t.Errorf("the disk cache saved %d discovery requests (%d → %d), want /api, /apis and the group versions", saved, asked, fromDisk)
	}
	t.Logf("discovery requests at connect: %d from the server, %d with the disk cache", asked, fromDisk)

	srv.Set(testkit.CRDsGVR, testkit.CRD("e2e.cogpit.test", "gadgets", "Gadget", true, widgetCRD()...))
	if !testkit.WaitFor(ctx, func() bool { _, ok := second.Backend().Profile("gadgets.e2e.cogpit.test"); return ok }) {
		t.Fatal("the Gadget kind never appeared in the connected session")
	}
	if !testkit.WaitFor(ctx, func() bool { e, _ := os.ReadDir(dir); return len(e) == 0 }) {
		t.Error("a new definition left the cache file in place")
	}
	if info := conn.DiscoveryInfo(); info.Source != "" {
		t.Errorf("a new definition kept the answer in memory: %+v", info)
	}
	second.Close()

	third, conn, _ := connect("third", "gadgets.e2e.cogpit.test")
	defer third.Close()
	if info := conn.DiscoveryInfo(); info.Source != "server" {
		t.Errorf("the connection after the change read discovery from %q, want the server", info.Source)
	}
	if _, err := conn.MapResource(ctx, "e2e.cogpit.test", "gadgets"); err != nil {
		t.Errorf("the new kind does not map: %v", err)
	}
}
