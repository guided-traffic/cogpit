//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// realCluster (realcluster_test.go) names the real cluster or skips the test.

// realMetricsSession connects the engine to the real cluster, its metrics tuned by opts.
func realMetricsSession(ctx context.Context, t *testing.T, opts metrics.Options) (*engine.Session, func(id string, params any) any) {
	t.Helper()
	kubeconfig, contextName := realCluster(t)
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}, MetricsOptions: opts}})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	return s, func(id string, params any) any {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: engine.OriginExternalAgent})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return res
	}
}

// TestRealClusterNodeDrawer checks a node's drawer against a real cluster's Prometheus and Metrics
// Server: its CPU and memory history, and its Network and Filesystem history from node-exporter,
// which kube-prometheus-stack needs (its kubelet ServiceMonitor drops cAdvisor's root cgroup); then budget P13 with the drawer open, 90 seconds each with CPU shown and with
// the network shown. It only reads.
func TestRealClusterNodeDrawer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	s, dispatch := realMetricsSession(ctx, t, metrics.Options{})
	proxy := func() int64 { return s.Diagnostics().Requests.ByVerbResource["get services/proxy"] }

	dispatch("ui.openView", map[string]any{"view": "nodes"})
	var node model.ObjectRef
	if !testkit.WaitFor(ctx, func() bool {
		tbl := s.Table("nodes")
		if tbl == nil || !tbl.Synced || len(tbl.Rows) == 0 {
			return false
		}
		node = tbl.Rows[0].Ref
		return true
	}) {
		t.Fatal("no node")
	}
	opened := time.Now()
	dispatch("ui.navigate", map[string]any{"ref": node})
	m := waitHistory(ctx, t, s, node, func(m *engine.ObjectMetrics) bool {
		return len(m.Targets[0].Samples) > 0 && m.HistorySource != ""
	})
	t.Logf("source %q, history from %q", m.Source, m.HistorySource)
	logProviders(t, "node", m.Providers)
	if m.HistorySource != metrics.UsageNone {
		checkPast(t, "node CPU and memory", m.Targets[0].Samples, opened)
	}

	dispatch("ui.setMetricsVariant", map[string]any{"ref": node, "variant": "network"})
	m = waitHistory(ctx, t, s, node, func(m *engine.ObjectMetrics) bool { return len(m.Targets[0].Network) > 0 })
	net := m.Targets[0].Network
	pastNet := 0
	for _, p := range net {
		if p.At.Before(opened.Add(-time.Minute)) {
			pastNet++
		}
	}
	if pastNet == 0 {
		t.Errorf("node network: no point from before the drawer opened (%d points)", len(net))
	}
	dispatch("ui.setMetricsVariant", map[string]any{"ref": node, "variant": "filesystem"})
	m = waitHistory(ctx, t, s, node, func(m *engine.ObjectMetrics) bool { return len(m.Targets[0].Filesystem) > 0 })
	m.Targets[0].Network = net
	t.Logf("node network: %d points, %d from before the drawer opened; filesystem: %d points (%s)", len(net), pastNet, len(m.Targets[0].Filesystem), m.FilesystemSource)
	logTarget(t, "node "+node.Name, m.FilesystemSource, m.Targets[0])

	// Budget P13 with the node's drawer at 1 h: CPU shown, then the network shown, which since
	// 2026-10-16 night asks the Metrics Server nothing (the one chart shows no CPU or memory).
	liveGets := func() int64 { return s.Diagnostics().Requests.ByVerbResource["get nodes.metrics.k8s.io"] }
	measure := func(what string) (prom, live int64) {
		p0, g0, w0 := proxy(), liveGets(), writes(s)
		time.Sleep(90 * time.Second)
		prom, live = proxy()-p0, liveGets()-g0
		if w := writes(s) - w0; w != 0 {
			t.Errorf("%s: %d writes", what, w)
		}
		t.Logf("budget P13 on a real cluster, a node's drawer at 1 h, %s: %d Prometheus requests and %d Metrics Server gets in 90 s; requests %v", what, prom, live, s.Diagnostics().Requests.ByVerbResource)
		return prom, live
	}
	dispatch("ui.setMetricsVariant", map[string]any{"ref": node, "variant": "cpu"})
	time.Sleep(3 * time.Second)
	if _, live := measure("CPU shown"); live == 0 {
		t.Error("CPU shown: no Metrics Server get in 90 s")
	}
	dispatch("ui.setMetricsVariant", map[string]any{"ref": node, "variant": "network"})
	time.Sleep(time.Second) // the poll under way when the variant changed
	if _, live := measure("the network shown"); live != 0 {
		t.Errorf("the network shown: %d Metrics Server gets in 90 s, want none", live)
	}
}
