package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/scorecard"
)

// The Nodes list's Disk column against a fake API server with the Metrics Server's node metrics
// and a Prometheus behind the service proxy that has node-exporter's filesystems: the whole
// path through the engine, and the request budget a Nodes list costs with Prometheus (P12, P13).

var nodeMetricsGVR = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "nodes"}

// diskProm answers the disk query: node-1 by its node label, node-2 by its address. It counts the
// queries by kind.
type diskProm struct {
	mu      sync.Mutex
	probes  int
	disks   int
	another []string
}

func (p *diskProm) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	p.mu.Lock()
	defer p.mu.Unlock()
	if r.URL.Path == "/api/v1/status/buildinfo" {
		p.probes++
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"3.15.0"}}`))
		return
	}
	q := r.URL.Query().Get("query")
	if !strings.Contains(q, "node_filesystem_size_bytes") {
		p.another = append(p.another, q)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
		return
	}
	p.disks++
	now := float64(time.Now().UnixMilli()) / 1000
	item := func(labels map[string]string, v string) map[string]any {
		labels["mountpoint"], labels["device"], labels["fstype"] = "/", "/dev/sda1", "ext4" // the root filesystem (D-90)
		return map[string]any{"metric": labels, "value": []any{now, v}}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": []any{
		item(map[string]string{"node": "node-1", "instance": "10.0.0.1:9100", "part": "size"}, "107374182400"),
		item(map[string]string{"node": "node-1", "instance": "10.0.0.1:9100", "part": "avail"}, "21474836480"),
		item(map[string]string{"instance": "10.0.0.2:9100", "part": "size"}, "53687091200"),
		item(map[string]string{"instance": "10.0.0.2:9100", "part": "avail"}, "48318382080"),
	}}})
}

func (p *diskProm) counts() (probes, disks int, another []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.probes, p.disks, append([]string(nil), p.another...)
}

func diskNode(name, ip string) *unstructured.Unstructured {
	return testkit.Object("v1", "Node", "", name, map[string]any{"status": map[string]any{
		"addresses":   []any{map[string]any{"type": "InternalIP", "address": ip}, map[string]any{"type": "Hostname", "address": name}},
		"allocatable": map[string]any{"cpu": "4", "memory": "8Gi", "pods": "110"},
		"capacity":    map[string]any{"cpu": "4", "memory": "8Gi", "pods": "110"},
		"conditions":  []any{map[string]any{"type": "Ready", "status": "True"}},
	}})
}

func TestNodesListDiskBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Resources: append(append([]testkit.FakeResource(nil), testkit.DefaultFakeResources...),
		testkit.FakeResource{GVR: testkit.NodesGVR, Kind: "Node"},
		testkit.FakeResource{GVR: nodeMetricsGVR, Kind: "NodeMetrics"})})
	defer srv.Close()
	for i, ip := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"} {
		name := "node-" + string(rune('1'+i))
		srv.Set(testkit.NodesGVR, diskNode(name, ip))
		srv.Set(nodeMetricsGVR, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "metrics.k8s.io/v1beta1", "kind": "NodeMetrics", "metadata": map[string]any{"name": name},
			"timestamp": time.Now().UTC().Format(time.RFC3339), "window": "15s", "usage": map[string]any{"cpu": "1", "memory": "2Gi"},
		}})
	}
	srv.Set(testkit.ServicesGVR, testkit.Service("monitoring", "prometheus-server", "10.0.0.5"))
	prom := &diskProm{}
	srv.SetProxy("monitoring", "prometheus-server:80", prom)

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{
		KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "default")},
		MetricsOptions:  metrics.Options{ListInterval: 700 * time.Millisecond},
	}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	s.UpdateUI(func(u *engine.UIState) { u.View = "nodes" })
	var tbl *engine.TableSnapshot
	if !testkit.WaitFor(ctx, func() bool {
		tbl = s.Table("nodes")
		return tbl != nil && tbl.Synced && len(tbl.Rows) == 3 && cellOf(tbl, 0, model.ColumnDisk).Text != "" && cellOf(tbl, 0, model.ColumnCPU).Text != ""
	}) {
		t.Fatalf("the Nodes list never showed the disk: %+v", tbl)
	}
	// node-1 by its node label, node-2 by its address, node-3 has no exporter.
	for i, want := range []string{"80%", "10%", ""} {
		if got := cellOf(tbl, i, model.ColumnDisk); got.Text != want || (want != "") != (got.Bar != nil) {
			t.Errorf("%s: disk %+v, want %q", tbl.Rows[i].Ref.Name, got, want)
		}
	}
	if tip := tbl.ColumnTips[model.ColumnDisk]; !strings.HasSuffix(tip, ", read through Prometheus (monitoring/prometheus-server).") {
		t.Errorf("the Disk header's tooltip = %q", tip)
	}

	// The budget over a few list polls: the Metrics Server's node list and one disk query per poll,
	// the detection's probe once, the Services read once by the cache (list and watch), and nothing
	// else of Prometheus: CPU and Memory come from the Metrics Server, which answers.
	reqs0 := srv.Requests()
	_, disks0, _ := prom.counts()
	time.Sleep(2200 * time.Millisecond) // about three list polls
	reqs := srv.Requests()
	probes, disks, another := prom.counts()
	polls := disks - disks0
	if probes != 1 || len(another) != 0 {
		t.Errorf("Prometheus: %d probes and other queries %q, want 1 probe and the disk query alone", probes, another)
	}
	lists := reqs["list nodes.metrics.k8s.io"] - reqs0["list nodes.metrics.k8s.io"]
	if polls < 2 || polls > 4 || lists < int64(polls)-1 || lists > int64(polls)+1 {
		t.Errorf("in about three list polls: %d disk queries and %d node lists, want one of each per poll", polls, lists)
	}
	if n := reqs["list services"] + reqs["watchlist services"]; n != 1 {
		t.Errorf("services read %d times (%v): the detection reads the cache's one list", n, reqs)
	}
	for key := range reqs {
		if strings.Contains(key, "nodes/proxy") {
			t.Errorf("the kubelet's proxy was asked: %s", key)
		}
	}
	t.Logf("P12/P13 for the Nodes list with Prometheus: %d disk queries, %d node lists in %d polls; 1 probe; requests %v", polls, lists, polls, reqs)
	how := "the engine against the fake API server with the Metrics Server's node metrics and a fake Prometheus behind its service proxy, the list interval shortened to 0.7 s"
	scorecard.Record(t, scorecard.Measurement{Budget: "P13", Metric: "Prometheus probes of one detection, the Nodes list's Disk shown",
		Value: float64(probes), Unit: "requests", Limit: 1, How: how})
	scorecard.Record(t, scorecard.Measurement{Budget: "P13", Metric: fmt.Sprintf("Prometheus queries beside the disk query in %d list polls", polls),
		Value: float64(len(another)), Unit: "requests", How: how})

	// Another view: the disks are not polled any more.
	s.UpdateUI(func(u *engine.UIState) { u.View = "pods" })
	if !testkit.WaitFor(ctx, func() bool { return s.Backend().Metrics.Subscribed() <= 1 }) {
		t.Fatalf("%d scopes still polled after the Nodes list closed", s.Backend().Metrics.Subscribed())
	}
	time.Sleep(200 * time.Millisecond)
	_, settled, _ := prom.counts()
	time.Sleep(1500 * time.Millisecond)
	if _, now, _ := prom.counts(); now != settled {
		t.Errorf("%d disk queries after the Nodes list closed", now-settled)
	}
}
