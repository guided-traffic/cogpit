package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// kpsProm is a Prometheus shaped like kube-prometheus-stack's (F1): its kubelet ServiceMonitor
// dropped cAdvisor's root cgroup, and its node-exporter is scraped under the node's host-network
// address, 10.0.0.7:9100, with no node label. It answers node-exporter's queries that name that
// address with points 50 minutes, 60 s and 30 s before the query's end, and nothing else.
type kpsProm struct {
	mu      sync.Mutex
	queries []string
}

func (p *kpsProm) count(text string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, q := range p.queries {
		if strings.Contains(q, text) {
			n++
		}
	}
	return n
}

func (p *kpsProm) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/v1/status/buildinfo" {
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"3.5.0"}}`))
		return
	}
	q := r.URL.Query().Get("query")
	p.mu.Lock()
	p.queries = append(p.queries, q)
	p.mu.Unlock()
	end, _ := strconv.ParseFloat(r.URL.Query().Get("end"), 64)
	var base float64
	switch {
	case strings.HasPrefix(q, "sum(max by (node, instance) (sum by (node, instance, job) (rate(node_cpu_seconds_total{mode!~"): // every exporter
		base = 0.5
	case strings.Contains(q, `node_cpu_seconds_total{instance=~"(cp-1|10\\.0\\.0\\.7)`):
		base = 0.5
	case strings.HasPrefix(q, "sum(max by (node, instance) (node_memory_MemTotal_bytes - "): // every exporter
		base = 1 << 30
	case strings.Contains(q, `node_memory_MemTotal_bytes{instance=~"(cp-1|10\\.0\\.0\\.7)`):
		base = 1 << 30
	default:
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
		return
	}
	values := [][]any{
		{end - 3000, strconv.FormatFloat(base, 'f', -1, 64)},
		{end - 60, strconv.FormatFloat(base*2, 'f', -1, 64)},
		{end - 30, strconv.FormatFloat(base*3, 'f', -1, 64)},
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{
		"resultType": "matrix", "result": []any{map[string]any{"metric": map[string]string{}, "values": values}},
	}})
}

// TestNodeHistoryOnKubePrometheusStack is F1's path through the engine (owner, 2026-10-14
// evening: "no past data" for nodes): a node's drawer and the cluster Overview get the past hour
// from node-exporter, found by the node's InternalIP from the cache, on a Prometheus that has no
// root cgroup, and the Metrics Server's live values after it; the drawer says where the history
// comes from (HistorySource), as does the Overview (UsageHistory).
func TestNodeHistoryOnKubePrometheusStack(t *testing.T) {
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
		"metadata": map[string]any{"labels": map[string]any{"node-role.kubernetes.io/control-plane": ""}},
		"status": map[string]any{
			"addresses":   []any{map[string]any{"type": "InternalIP", "address": "10.0.0.7"}, map[string]any{"type": "Hostname", "address": "cp-1"}},
			"allocatable": map[string]any{"cpu": "4", "memory": "8Gi", "pods": "110"},
			"capacity":    map[string]any{"cpu": "4", "memory": "8Gi", "pods": "110"},
		},
	}))
	srv.Set(testkit.ServicesGVR, testkit.Service("monitoring", "kps-kube-prometheus-stack-prometheus", "10.0.0.5"))
	prom := &kpsProm{}
	srv.SetProxy("monitoring", "kps-kube-prometheus-stack-prometheus:80", prom)

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{
		KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "default")},
		MetricsOptions:  metrics.Options{ObjectInterval: 50 * time.Millisecond},
	}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	dispatch := func(id string, params any) any {
		t.Helper()
		raw, _ := json.Marshal(params)
		res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: engine.OriginExternalAgent})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return res
	}

	// The node's drawer, opened from the Nodes list (the cache holds the node and its addresses).
	dispatch("ui.openView", map[string]any{"view": "nodes"})
	if !testkit.WaitFor(ctx, func() bool { tbl := s.Table("nodes"); return tbl != nil && tbl.Synced && len(tbl.Rows) == 1 }) {
		t.Fatal("the node was never listed")
	}
	ref := model.ObjectRef{Kind: "nodes", Name: "cp-1"}
	dispatch("ui.navigate", map[string]any{"ref": ref})
	var m *engine.ObjectMetrics
	if !testkit.WaitFor(ctx, func() bool {
		m, _ = s.ObjectMetrics(ref)
		// The source line too: it follows the providers' states, which may settle after the samples.
		return m != nil && m.HistorySource == metrics.UsageNodeExporter && len(m.Targets) == 1 && len(m.Targets[0].Samples) >= 3 &&
			m.Source == "Prometheus (monitoring/kps-kube-prometheus-stack-prometheus) and Kubernetes Metrics Server"
	}) {
		t.Fatalf("the node's drawer never got node-exporter's history: %+v", m)
	}
	pts := m.Targets[0].Samples
	if age := time.Since(pts[0].At); age < 45*time.Minute || pts[0].CPUMilli != 500 || pts[len(pts)-1].CPUMilli != 1000 || pts[len(pts)-1].MemoryBytes != 2<<30 {
		t.Errorf("the chart starts %s back with %+v and ends %+v: want node-exporter's hour, then the Metrics Server's value", age.Round(time.Second), pts[0], pts[len(pts)-1])
	}
	if m.Source != "Prometheus (monitoring/kps-kube-prometheus-stack-prometheus) and Kubernetes Metrics Server" {
		t.Errorf("source %q", m.Source)
	}
	if n := prom.count("container_cpu_usage_seconds_total"); n != 0 {
		t.Errorf("cAdvisor's root cgroup asked %d times although node-exporter answered", n)
	}

	// The cluster Overview: the masters' history from node-exporter by the same address.
	dispatch("ui.openView", map[string]any{"view": engine.ClusterOverview})
	var ov engine.ClusterOverviewModel
	if !testkit.WaitFor(ctx, func() bool {
		raw, _ := json.Marshal(dispatch("cluster.overview", map[string]any{"series": true}))
		ov = engine.ClusterOverviewModel{}
		_ = json.Unmarshal(raw, &ov)
		return ov.UsageHistory == metrics.UsageNodeExporter && len(ov.Charts) == 3 && len(ov.Charts[0].Samples) >= 3
	}) {
		t.Fatalf("the Overview never got node-exporter's history: history %q, nodes %q, %d charts", ov.UsageHistory, ov.Nodes, len(ov.Charts))
	}
	if ov.Nodes != engine.NodesMaster || time.Since(ov.Charts[0].Samples[0].At) < 45*time.Minute {
		t.Errorf("the Overview shows %s from %s", ov.Nodes, ov.Charts[0].Samples[0].At)
	}
}
