package integration_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestNetworkOnlyDrawerAsksNoLiveUsage: a node's drawer whose one chart shows the network asks the
// Metrics Server nothing and Prometheus no CPU or memory, poll after poll (2026-10-16 night; before,
// every object poll asked for the live CPU and memory); back to CPU, the live values go on. The
// same with CPU and memory hidden by the cluster's settings: the chart shows the network by
// default, and nothing asks for them. Counted at the fake API server.
func TestNetworkOnlyDrawerAsksNoLiveUsage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Access: testkit.AdminAccess(), Resources: append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
		testkit.FakeResource{GVR: testkit.NodesGVR, Kind: "Node"},
		testkit.FakeResource{GVR: nodeMetricsGVR, Kind: "NodeMetrics"})})
	defer srv.Close()
	srv.Set(nodeMetricsGVR, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "metrics.k8s.io/v1beta1", "kind": "NodeMetrics", "metadata": map[string]any{"name": "cp-1"},
		"timestamp": time.Now().UTC().Format(time.RFC3339), "window": "15s", "usage": map[string]any{"cpu": "1", "memory": "2Gi"},
	}})
	srv.Set(testkit.NodesGVR, testkit.Object("v1", "Node", "", "cp-1", map[string]any{
		"status": map[string]any{
			"addresses":   []any{map[string]any{"type": "InternalIP", "address": "10.0.0.7"}},
			"allocatable": map[string]any{"cpu": "4", "memory": "8Gi", "pods": "110"},
			"capacity":    map[string]any{"cpu": "4", "memory": "8Gi", "pods": "110"},
		},
	}))
	srv.Set(testkit.ServicesGVR, testkit.Service("monitoring", "kps-kube-prometheus-stack-prometheus", "10.0.0.5"))
	prom := &kpsProm{} // node-exporter's CPU and memory by the node's address; the network empty
	srv.SetProxy("monitoring", "kps-kube-prometheus-stack-prometheus:80", prom)

	s := engine.New(engine.Options{
		Connector: &engine.ClusterConnector{
			KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "default")},
			MetricsOptions:  metrics.Options{ObjectInterval: 50 * time.Millisecond},
		},
		ClusterSettings: engine.NewClusterSettingsStore(filepath.Join(t.TempDir(), "Cogpit")),
	})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	decideIntegrations(t, s, "fake") // its metrics read (D-82)
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	var polls atomic.Int64 // the drawer's polls: the engine emits EventMetrics after each
	stop := s.Subscribe(func(e engine.Event) {
		if e.Type == engine.EventMetrics {
			polls.Add(1)
		}
	})
	defer stop()
	dispatch := func(id string, params any) {
		t.Helper()
		raw, _ := json.Marshal(params)
		if _, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: engine.OriginGUI}); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	gets := func() int64 { return s.Diagnostics().Requests.ByVerbResource["get nodes.metrics.k8s.io"] }
	// quiet waits for n polls of the drawer and reports the Metrics Server's gets and Prometheus's
	// CPU queries meanwhile.
	quiet := func(n int64) (live int64, cpu int) {
		t.Helper()
		g0, c0, p0 := gets(), prom.count("node_cpu_seconds_total"), polls.Load()
		if !testkit.WaitFor(ctx, func() bool { return polls.Load() >= p0+n }) {
			t.Fatalf("%d polls never came", n)
		}
		return gets() - g0, prom.count("node_cpu_seconds_total") - c0
	}

	dispatch("ui.openView", map[string]any{"view": "nodes"})
	if !testkit.WaitFor(ctx, func() bool { tbl := s.Table("nodes"); return tbl != nil && tbl.Synced && len(tbl.Rows) == 1 }) {
		t.Fatal("the node was never listed")
	}
	ref := model.ObjectRef{Kind: "nodes", Name: "cp-1"}
	dispatch("ui.navigate", map[string]any{"ref": ref})
	if !testkit.WaitFor(ctx, func() bool {
		m, _ := s.ObjectMetrics(ref)
		return m != nil && m.Capabilities.Network && m.HistorySource == metrics.UsageNodeExporter && gets() > 0
	}) {
		t.Fatal("the node's drawer never had its history and live values")
	}
	if live, _ := quiet(10); live == 0 {
		t.Fatal("CPU shown: no live value asked in 10 polls")
	}

	// The network alone: the Metrics Server and Prometheus's CPU are not asked.
	dispatch("ui.setMetricsVariant", map[string]any{"ref": ref, "variant": "network"})
	if !testkit.WaitFor(ctx, func() bool { return prom.count("node_network_receive_bytes_total") > 0 }) {
		t.Fatal("the network was never asked")
	}
	quiet(3) // the poll that was under way when the variant changed
	if live, cpu := quiet(8); live != 0 || cpu != 0 {
		t.Errorf("the network shown: %d live gets and %d CPU queries in 8 polls, want none", live, cpu)
	}
	m, _ := s.ObjectMetrics(ref)
	if m == nil || m.Source != "Prometheus (monitoring/kps-kube-prometheus-stack-prometheus) and Kubernetes Metrics Server" {
		t.Errorf("source while the network shows: %+v", m) // the chart's providers as before
	}

	// CPU again: the live values go on.
	dispatch("ui.setMetricsVariant", map[string]any{"ref": ref, "variant": "cpu"})
	if !testkit.WaitFor(ctx, func() bool { live, _ := quiet(5); return live > 0 }) {
		t.Fatal("CPU shown again: no live value")
	}

	// CPU and memory hidden by the cluster's settings: the chart shows the network, nothing asks
	// for them.
	dispatch("cluster.settings.set", map[string]any{"context": "fake", "hiddenMetrics": []string{"cpu", "memory"}})
	if !testkit.WaitFor(ctx, func() bool {
		m, _ := s.ObjectMetrics(ref)
		return m != nil && len(m.Targets) == 1 && m.Targets[0].Variant == engine.VariantNetwork
	}) {
		t.Fatal("the chart never showed the network with CPU and memory hidden")
	}
	quiet(3) // the poll that was under way when the setting changed
	if live, cpu := quiet(8); live != 0 || cpu != 0 {
		t.Errorf("CPU and memory hidden: %d live gets and %d CPU queries in 8 polls, want none", live, cpu)
	}
}
