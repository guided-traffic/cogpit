package metrics_test

import (
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// F1 (owner, 2026-10-14 evening, in a real window): a node's drawer showed no past. On
// kube-prometheus-stack the root cgroup the node's history was read from is dropped, and its
// node-exporter carries the node's address, not its name. The drawer's history now comes from
// node-exporter, found by the addresses the engine gives (SetNodeAddresses); the Metrics Server's
// live values continue it. Addresses that come after the drawer opened (the node was not listed
// yet) make the chart ask for its history again, once.
func TestNodeDrawerHistoryFromNodeExporterByAddress(t *testing.T) {
	c := newPromCluster(t, true)
	c.srv.Set(nodeMetricsGVR, &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "metrics.k8s.io/v1beta1", "kind": "NodeMetrics", "metadata": map[string]any{"name": "n1"},
		"timestamp": time.Now().UTC().Format(time.RFC3339), "window": "15s", "usage": map[string]any{"cpu": "500m", "memory": "1Gi"},
	}})
	c.prom.onRel(`node_cpu_seconds_total{instance=~"(n1|10\\.0\\.0\\.7)(:[0-9]+)?"`, s(nil, -120, 0.25, -90, 0.5, -60, 0.75))
	c.prom.onRel(`node_memory_MemTotal_bytes{instance=~"(n1|10\\.0\\.0\\.7)(:[0-9]+)?"`, s(nil, -120, 1<<30, -90, 1<<30, -60, 1<<30))
	c.srv.SetProxy("monitoring", "prometheus-server:80", c.prom)
	src, _, err := metrics.SourcesForConn(c.conn, servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h := metrics.NewHubFor(src, metrics.Options{ObjectInterval: time.Hour, ListInterval: time.Hour})
	t.Cleanup(h.Close)
	sc := metrics.Scope{Kind: metrics.KindNodes, Name: "n1"}
	onChange, polls := pollCounter()

	// Opened before the cache knew the node's addresses: Prometheus has nothing by its name.
	sub := h.Subscribe(sc, onChange)
	t.Cleanup(sub.Release)
	waitPoll(t, polls, 1)
	hist := h.History(sc)
	if hist == nil || hist.UsageSource != metrics.UsageNone || len(hist.Targets[""].Usage) != 1 {
		t.Fatalf("by its name alone: %+v, want no history and the Metrics Server's live value", hist)
	}

	// The addresses: the history is asked for again, now with them.
	h.SetNodeAddresses("n1", []string{"10.0.0.7", "n1", ""})
	waitPoll(t, polls, 2)
	hist = h.History(sc)
	if hist == nil || hist.UsageSource != metrics.UsageNodeExporter {
		t.Fatalf("with the address: %+v", hist)
	}
	pts := hist.Targets[""].Usage
	if len(pts) != 4 || pts[0].CPUMilli != 250 || pts[3].CPUMilli != 500 || !pts[2].At.Before(pts[3].At) {
		t.Errorf("the chart %+v, want node-exporter's three points, then the Metrics Server's", pts)
	}
	var withAddr int
	for _, r := range c.prom.requests() {
		if strings.Contains(r.query, `10\\.0\\.0\\.7`) {
			withAddr++
		}
	}
	if withAddr != 2 {
		t.Errorf("%d queries named the address, want node-exporter's CPU and memory", withAddr)
	}

	// The same addresses again (every read of the node sets them): the history stands, the next
	// poll asks the Metrics Server alone.
	before := c.proxyCount()
	h.SetNodeAddresses("n1", []string{"10.0.0.7"})
	metrics.Kick(h, sc)
	waitPoll(t, polls, 3)
	if n := c.proxyCount() - before; n != 0 {
		t.Errorf("%d Prometheus requests after unchanged addresses", n)
	}
}
