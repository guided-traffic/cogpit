//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestKindClusterOverview checks the cluster Overview (G12, M7) against the kind cluster's
// metrics-server and the real Prometheus of `make kind-prometheus`: the page's CPU and Memory get the
// cluster's history from Prometheus once (sums over node-exporter's series, A43) and the Metrics
// Server's node list after it; the live sum matches metrics.get's nodes; the lines and the Pods
// come from the cache; the Issues name objects the cluster has; a range change asks Prometheus
// once more; a closed page asks nothing. It only reads. Skipped without that Prometheus.
func TestKindClusterOverview(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	s, dispatch := kindMetricsSession(ctx, t, metrics.Options{})
	count := func(key string) int64 { return s.Diagnostics().Requests.ByVerbResource[key] }
	read := func(series bool) engine.ClusterOverviewModel {
		t.Helper()
		var m engine.ClusterOverviewModel
		raw, _ := json.Marshal(dispatch("cluster.overview", map[string]any{"series": series, "everyIssue": true}))
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	proxy0, lists0 := count("get services/proxy"), count("list nodes.metrics.k8s.io")
	opened := time.Now()
	dispatch("ui.openView", map[string]any{"view": engine.ClusterOverview})
	var m engine.ClusterOverviewModel
	if !testkit.WaitFor(ctx, func() bool {
		m = read(true)
		return m.Synced && m.Status == metrics.StatusAvailable && m.Source == kindBothSource && len(m.Charts) == 3 &&
			len(m.Charts[0].Samples) > 10 && m.Charts[0].At.After(opened.Add(-time.Minute))
	}) {
		t.Fatalf("the Overview never had both providers' numbers: status %q source %q message %q, %d cpu points",
			m.Status, m.Source, m.Message, len(m.Charts[0].Samples))
	}
	logProviders(t, "overview", m.Providers)
	t.Logf("groups %+v, the charts show %s over %s", m.Groups, m.Nodes, m.Range)
	for _, ch := range m.Charts {
		line := ch.Title + ": "
		if n := len(ch.Samples); n > 0 {
			line += ch.Samples[0].At.Format(time.TimeOnly) + "–" + ch.Samples[n-1].At.Format(time.TimeOnly) + ", " + itoa(n) + " points"
		}
		if ch.Current != nil {
			line += ", current " + ftoa(*ch.Current)
		}
		if ch.Requests != nil {
			line += ", requests " + ftoa(*ch.Requests) + ", limits " + ftoa(*ch.Limits)
		}
		t.Logf("%s, allocatable %s, capacity %s %s", line, ftoa(ch.Allocatable), ftoa(ch.Capacity), ch.Unit)
	}
	if m.Charts[0].Allocatable <= 0 || m.Charts[1].Capacity <= 0 || m.Charts[2].Allocatable <= 0 || m.Charts[2].Current == nil || *m.Charts[2].Current <= 0 {
		t.Errorf("lines and pods from the cache: %+v", m.Charts)
	}

	// The history's grid, then the live values: no step longer than the history's plus a poll.
	pts := m.Charts[0].Samples
	var longest time.Duration
	for i := 1; i < len(pts); i++ {
		longest = max(longest, pts[i].At.Sub(pts[i-1].At))
	}
	t.Logf("cpu: %d points from %s, the longest step %s", len(pts), pts[0].At.Format(time.TimeOnly), longest)
	if pts[0].At.After(opened.Add(-30 * time.Minute)) {
		t.Errorf("the cpu chart starts %s: no history from Prometheus", pts[0].At.Format(time.TimeOnly))
	}

	// The live sum is the Metrics Server's nodes' sum (the masters on this single-node cluster).
	var nodes engine.MetricsResult
	raw, _ := json.Marshal(dispatch("metrics.get", map[string]any{"kind": "nodes"}))
	if err := json.Unmarshal(raw, &nodes); err != nil {
		t.Fatal(err)
	}
	var sum model.Usage
	for _, n := range nodes.Nodes {
		sum = sum.Add(n.Usage)
	}
	t.Logf("metrics.get nodes: %d nodes, cpu %dm, memory %s (source %q); the Overview: cpu %s, memory %s",
		len(nodes.Nodes), sum.CPUMilli, model.FormatBytes(sum.MemoryBytes), nodes.Source, ftoa(*m.Charts[0].Current), ftoa(*m.Charts[1].Current))

	// The history's sources (A43, A115): node-exporter's CPU and memory, kube-state-metrics' pods.
	if m.UsageHistory != metrics.UsageNodeExporter || m.PodsHistory != metrics.PodsFromKubeStateMetrics {
		t.Errorf("history from %q and pods from %q, want node-exporter and kube-state-metrics (make kind-prometheus again: its values enable both since A115)", m.UsageHistory, m.PodsHistory)
	}

	// Prometheus's requests: the probe and one history (4 range queries of node-exporter: every
	// node and the masters; 2 of kube-state-metrics: their pods), then none while the Metrics
	// Server answers; a node list per poll.
	time.Sleep(45 * time.Second)
	proxy, lists := count("get services/proxy")-proxy0, count("list nodes.metrics.k8s.io")-lists0
	t.Logf("the page open %s: %d Prometheus requests, %d node lists", time.Since(opened).Round(time.Second), proxy, lists)
	if proxy > 7 {
		t.Errorf("%d Prometheus requests with the page open, want at most 7 (a probe and one history)", proxy)
	}

	// The Issues: objects the cluster has.
	m = read(false)
	t.Logf("%d issues", len(m.Issues))
	for i, is := range m.Issues {
		if i < 8 {
			t.Logf("issue: %s %s/%s (%s): %s, since %s", is.Kind, is.Ref.Namespace, is.Name, is.Source, is.Message, is.Since.Format(time.TimeOnly))
		}
		if _, _, err := s.ResolveRef(is.Ref); err != nil {
			t.Errorf("issue %+v: %v", is, err)
		}
	}

	// A range change asks Prometheus once more.
	before := count("get services/proxy")
	dispatch("ui.setOverviewRange", map[string]any{"range": "3h"})
	if !testkit.WaitFor(ctx, func() bool {
		m = read(true)
		return m.Range == "3h" && len(m.Charts[0].Samples) > 0 && m.Charts[0].Samples[0].At.Before(time.Now().Add(-90*time.Minute))
	}) {
		t.Logf("3h: the chart starts %s (Prometheus may hold less)", m.Charts[0].Samples[0].At.Format(time.TimeOnly))
	}
	time.Sleep(3 * time.Second)
	t.Logf("a range change to 3h: %d Prometheus requests, %d cpu points", count("get services/proxy")-before, len(m.Charts[0].Samples))
	dispatch("ui.setOverviewRange", map[string]any{"range": "default"})

	// Closed: nothing asked for it.
	dispatch("ui.openView", map[string]any{"view": "nodes"})
	time.Sleep(2 * time.Second)
	proxy, lists = count("get services/proxy"), count("list nodes.metrics.k8s.io")
	time.Sleep(20 * time.Second)
	if p := count("get services/proxy") - proxy; p != 0 {
		t.Errorf("%d Prometheus requests after the page closed", p)
	}
	t.Logf("closed (the Nodes view open): %d node lists in 20 s", count("list nodes.metrics.k8s.io")-lists)
	if n := writes(s); n != 0 {
		t.Errorf("%d writes", n)
	}
}

func ftoa(v float64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
