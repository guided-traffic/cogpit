//go:build kind

package integration_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// kindIdle is how long the kind test leaves its synced views alone.
const kindIdle = 3 * time.Second

// TestKindIdleViewsCostNoLists connects to the kind cluster `make test-kind` names, opens Pods and
// Services the way two views would, and checks the API-load budget against a real API server:
// the initial lists (or streaming watches) and nothing more while the views stay open and idle.
// It only reads.
func TestKindIdleViewsCostNoLists(t *testing.T) {
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()
	t.Logf("connected to %s (%s), Kubernetes %s", conn.Context(), conn.Server(), conn.ServerVersion())

	served, err := conn.ServedResources(ctx)
	if err != nil {
		t.Fatalf("ServedResources: %v", err)
	}
	if !served[testkit.PodsGVR] || !served[testkit.ServicesGVR] || !served[testkit.DeploymentsGVR] {
		t.Fatalf("served resources lack pods, services or deployments: %d resources", len(served))
	}
	t.Logf("discovery: %d resources that support list and watch", len(served))

	c := cache.New(cache.DynamicSource(conn.Dynamic(), conn.Metadata()), cache.Options{})
	defer c.Close()
	pods := c.Acquire(cache.Scope{GVR: testkit.PodsGVR})
	services := c.Acquire(cache.Scope{GVR: testkit.ServicesGVR})
	defer pods.Release()
	defer services.Release()
	for _, h := range []*cache.Handle{pods, services} {
		if !h.WaitForSync(ctx) {
			t.Fatalf("%v not synced: %v", h.Scope(), h.Err())
		}
	}
	// The idle window opens once each scope's watch is open: a stream is its own watch; after a
	// list (a refused stream's fallback) the watch follows the synced list.
	var synced cluster.RequestStats
	if !testkit.WaitFor(ctx, func() bool {
		synced = conn.Counter().Snapshot()
		for _, res := range []string{"pods", "services"} {
			if got := countFor(synced, res); got.watch < 1 && (got.watchlist < 1 || got.list > 0) {
				return false
			}
		}
		return true
	}) {
		t.Fatalf("the watches never opened: %v", synced.ByVerbResource)
	}
	for _, res := range []string{"pods", "services"} {
		got := countFor(synced, res)
		// Either one streaming watch, or one list and one watch (after a refused streaming
		// attempt): never more than one list.
		if got.list > 1 || got.list+got.watchlist < 1 || got.watch > 1 {
			t.Errorf("initial cost for %s: %+v", res, got)
		}
	}
	if len(pods.List()) == 0 {
		t.Error("no pods in the cache; kind runs system pods")
	}

	time.Sleep(kindIdle)
	after := conn.Counter().Snapshot()
	for _, res := range []string{"pods", "services"} {
		if b, a := countFor(synced, res), countFor(after, res); a != b {
			t.Errorf("idle for %s: %s went from %+v to %+v", kindIdle, res, b, a)
		}
	}
	if after.ByVerb[cluster.VerbList] != synced.ByVerb[cluster.VerbList] {
		t.Errorf("idle for %s cost %d list requests", kindIdle, after.ByVerb[cluster.VerbList]-synced.ByVerb[cluster.VerbList])
	}
	t.Logf("requests after %s idle: %v", kindIdle, after.ByVerbResource)
}
