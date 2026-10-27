package metrics_test

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

var (
	podMetricsGVR  = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}
	nodeMetricsGVR = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "nodes"}
)

func metricsResources() []testkit.FakeResource {
	return append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
		testkit.FakeResource{GVR: podMetricsGVR, Kind: "PodMetrics", Namespaced: true},
		testkit.FakeResource{GVR: nodeMetricsGVR, Kind: "NodeMetrics"})
}

func connectFake(t *testing.T, opts testkit.FakeAPIOptions) (*testkit.FakeAPIServer, *cluster.Conn) {
	t.Helper()
	srv := testkit.NewFakeAPIServer(opts)
	t.Cleanup(srv.Close)
	// A limiter that never waits: the tests count requests, the cluster's default 5 a second would
	// only delay them.
	conn, err := cluster.Connect(context.Background(), cluster.Options{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "default")}, QPS: 1000, Burst: 1000})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	return srv, conn
}

func podObj(ns, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "metrics.k8s.io/v1beta1", "kind": "PodMetrics",
		"metadata":   map[string]any{"namespace": ns, "name": name},
		"timestamp":  "2026-10-12T10:00:00Z",
		"window":     "15s",
		"containers": []any{map[string]any{"name": "app", "usage": map[string]any{"cpu": "250m", "memory": "64Mi"}}},
	}}
}

// The whole path against a fake API server: discovery, the typed client through the connection's
// counting transport, the hub's polling. The counter's numbers are the request budget (P12).
func TestHubOverTheCountingTransport(t *testing.T) {
	srv, conn := connectFake(t, testkit.FakeAPIOptions{Resources: metricsResources()})
	srv.Set(podMetricsGVR, podObj("web", "a"))
	srv.Set(podMetricsGVR, podObj("web", "b"))
	srv.Set(nodeMetricsGVR, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "metrics.k8s.io/v1beta1", "kind": "NodeMetrics", "metadata": map[string]any{"name": "n1"},
		"timestamp": "2026-10-12T10:00:00Z", "window": "15s", "usage": map[string]any{"cpu": "1", "memory": "2Gi"},
	}})
	prov, err := metrics.ForConn(conn)
	if err != nil {
		t.Fatal(err)
	}
	h := metrics.NewHub(prov, metrics.Options{ListInterval: 40 * time.Millisecond, ObjectInterval: 20 * time.Millisecond})
	defer h.Close()
	count := func() map[string]int64 { return conn.Counter().Snapshot().ByVerbResource }

	// Nothing open: nothing sent.
	before := conn.Counter().Snapshot().Total
	time.Sleep(100 * time.Millisecond)
	if got := conn.Counter().Snapshot().Total; got != before {
		t.Fatalf("an idle hub sent %d requests", got-before)
	}

	list := h.Subscribe(allPods, nil)
	eventually(t, "list polls", func() bool { return count()["list pods.metrics.k8s.io"] >= 3 })
	if !h.State(allPods).Usable() {
		t.Fatalf("state = %+v", h.State(allPods))
	}
	p, ok := h.Pod("web", "a")
	if !ok || p.Total().CPUMilli != 250 || p.Total().MemoryBytes != 64<<20 || p.Containers[0].Name != "app" {
		t.Fatalf("pod a = %+v (found %v)", p, ok)
	}
	drawer := h.Subscribe(metrics.Scope{Kind: metrics.KindPods, Namespace: "web", Name: "a"}, nil)
	nodes := h.Subscribe(allNode, nil)
	eventually(t, "object and node polls", func() bool {
		return count()["get pods.metrics.k8s.io"] >= 2 && count()["list nodes.metrics.k8s.io"] >= 2
	})
	if n, ok := h.Node("n1"); !ok || n.CPUMilli != 1000 || n.MemoryBytes != 2<<30 {
		t.Errorf("node n1 = %+v (found %v)", n, ok)
	}
	list.Release()
	drawer.Release()
	nodes.Release()
	// Close waits for the polls under way, so the counts below are final.
	h.Close()

	// The counting transport sees every request once: the server's own count of the Metrics API
	// requests is the counter's, not twice it (the typed client is built on the connection's client).
	// The counter counts a request when it is sent and the server when it handles it, so wait for
	// the server's side. A poll that Close cancelled in flight is counted by the client and may never
	// reach the server: one more on the client's side, per key, is that poll, not a request counted
	// twice.
	keys := []string{"list pods.metrics.k8s.io", "get pods.metrics.k8s.io", "list nodes.metrics.k8s.io"}
	agree := func() bool {
		snap := conn.Counter().Snapshot()
		for _, key := range keys {
			if got, want := snap.ByVerbResource[key], srv.Requests()[key]; got < want || got > want+1 || want == 0 {
				return false
			}
		}
		return true
	}
	eventually(t, "the counter and the server to agree", agree)
	snap := conn.Counter().Snapshot()
	if snap.ByVerb["watch"] != 0 || snap.ByVerb["watchlist"] != 0 {
		t.Errorf("metrics were watched: %v", snap.ByVerb)
	}
	// One discovery request for the group, however many polls followed.
	if n := snap.ByVerbResource["discovery"]; n > 6 {
		t.Errorf("%d discovery requests (connect and the group's): detection must be kept", n)
	}
}

func TestDetectionAgainstClustersWithoutMetrics(t *testing.T) {
	t.Run("not served", func(t *testing.T) {
		_, conn := connectFake(t, testkit.FakeAPIOptions{})
		prov, err := metrics.ForConn(conn)
		if err != nil {
			t.Fatal(err)
		}
		_, st := prov.Detect(context.Background())
		if st.Status != metrics.StatusNotServed {
			t.Errorf("state = %+v, want notServed", st)
		}
		h := metrics.NewHub(prov, metrics.Options{ListInterval: 20 * time.Millisecond})
		defer h.Close()
		sub := h.Subscribe(allPods, nil)
		defer sub.Release()
		time.Sleep(120 * time.Millisecond)
		if n := conn.Counter().Snapshot().ByVerbResource["list pods.metrics.k8s.io"]; n != 0 {
			t.Errorf("%d list requests against a cluster without the Metrics API", n)
		}
	})
	t.Run("registered but down", func(t *testing.T) {
		_, conn := connectFake(t, testkit.FakeAPIOptions{BrokenGroups: []string{"metrics.k8s.io"}})
		prov, err := metrics.ForConn(conn)
		if err != nil {
			t.Fatal(err)
		}
		_, st := prov.Detect(context.Background())
		if st.Status != metrics.StatusUnavailable || st.Message == "" {
			t.Errorf("state = %+v, want unavailable with a message", st)
		}
	})
}
