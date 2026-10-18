package metrics_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit/scorecard"
)

// The cluster's totals (the cluster Overview's charts): a scope polled like a drawer's object,
// through the hub's roles and fallback; its chart has the targets "" (every node), master and
// worker.

// nodesLive is a Metrics Server with a master m1 and two workers, measured at the clock's time.
type nodesLive struct {
	mu    sync.Mutex
	clk   *testClock
	err   error
	lists int
}

func (f *nodesLive) ID() string                                                 { return metrics.ServerID }
func (f *nodesLive) Label() string                                              { return metrics.ServerLabel }
func (f *nodesLive) Capabilities() metrics.Capabilities                         { return metrics.Capabilities{} }
func (f *nodesLive) ListPods(context.Context, string) ([]model.PodUsage, error) { return nil, nil }
func (f *nodesLive) GetPod(context.Context, string, string) (model.PodUsage, error) {
	return model.PodUsage{}, metrics.ErrNoData
}
func (f *nodesLive) GetNode(context.Context, string) (model.NodeUsage, error) {
	return model.NodeUsage{}, metrics.ErrNoData
}

func (f *nodesLive) ListNodes(context.Context) ([]model.NodeUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	if f.err != nil {
		return nil, f.err
	}
	at := f.clk.Now()
	return []model.NodeUsage{
		{Name: "m1", Timestamp: at, Usage: model.Usage{CPUMilli: 500, MemoryBytes: 1 << 30}},
		{Name: "w1", Timestamp: at, Usage: model.Usage{CPUMilli: 1000, MemoryBytes: 2 << 30}},
		{Name: "w2", Timestamp: at.Add(-time.Second), Usage: model.Usage{CPUMilli: 250, MemoryBytes: 512 << 20}},
	}, nil
}

func (f *nodesLive) setErr(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

func (f *nodesLive) listCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists
}

// clusterHist is a Prometheus that answers the cluster's range queries: every node 2 cores, the
// master 0.5.
type clusterHist struct {
	histFake
}

func (f *clusterHist) ObjectRange(_ context.Context, q metrics.RangeQuery) (*metrics.ObjectSeries, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	if f.err != nil {
		return nil, f.err
	}
	out := &metrics.ObjectSeries{Targets: map[string]*metrics.TargetSeries{"": {}, metrics.TargetMaster: {}, metrics.TargetWorker: {}}, From: q.From, To: q.To, Step: q.Step}
	for t := q.From; !t.After(q.To); t = t.Add(q.Step) {
		out.Targets[""].Usage = append(out.Targets[""].Usage, model.UsageSample{At: t, Usage: model.Usage{CPUMilli: 2000, MemoryBytes: 4 << 30}})
		out.Targets[metrics.TargetMaster].Usage = append(out.Targets[metrics.TargetMaster].Usage, model.UsageSample{At: t, Usage: model.Usage{CPUMilli: 500, MemoryBytes: 1 << 30}})
		out.Targets[metrics.TargetWorker].Usage = append(out.Targets[metrics.TargetWorker].Usage, model.UsageSample{At: t, Usage: model.Usage{CPUMilli: 1500, MemoryBytes: 3 << 30}})
	}
	return out, nil
}

func TestClusterScopeNamesItsMasters(t *testing.T) {
	sc := metrics.ClusterScope([]string{"m2", "m1", "m2"})
	if sc.Kind != metrics.KindCluster || sc.Name != "m1,m2" || sc.IsList() || !slices.Equal(metrics.ClusterMasters(sc.Name), []string{"m1", "m2"}) {
		t.Fatalf("scope %+v", sc)
	}
	if none := metrics.ClusterScope(nil); none.IsList() || metrics.ClusterMasters(none.Name) != nil {
		t.Fatalf("a cluster without masters %+v is a list", none)
	}
}

// TestClusterHistoryThenLiveSums: the cluster's chart asks Prometheus once for the range's CPU and
// memory over every node, the master and the workers, then continues with the Metrics Server's
// node list summed per target; a failing Metrics Server hands the live values to Prometheus in the
// same poll.
func TestClusterHistoryThenLiveSums(t *testing.T) {
	clk := &testClock{t: time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)}
	live, hist := &nodesLive{clk: clk}, &clusterHist{histFake{clk: clk}}
	h := metrics.NewHubFor(metrics.Sources{
		Live:    identified{detectorFor(live), metrics.ServerID, metrics.ServerLabel},
		History: identified{detectorFor(hist), metrics.PrometheusID, metrics.PrometheusLabel},
	}, metrics.Options{ObjectInterval: time.Hour, ListInterval: time.Hour, Now: clk.Now})
	t.Cleanup(h.Close)
	sc := metrics.ClusterScope([]string{"m1"})
	polls := make(chan int, 16)
	n := 0
	sub := h.Subscribe(sc, func() { n++; polls <- n })
	t.Cleanup(sub.Release)
	waitPoll(t, polls, 1)

	qs := hist.ranges()
	if len(qs) != 1 || qs[0].Kind != metrics.KindCluster || qs[0].Name != "m1" || qs[0].Want != metrics.WantUsage || qs[0].Step != 30*time.Second {
		t.Fatalf("the first poll asked Prometheus %+v, want the hour once", qs)
	}
	if live.listCount() != 1 {
		t.Fatalf("%d node lists at the first poll", live.listCount())
	}
	for i := 2; i <= 4; i++ {
		clk.Advance(15 * time.Second)
		metrics.Kick(h, sc)
		waitPoll(t, polls, i)
	}
	if len(hist.ranges()) != 1 || live.listCount() != 4 {
		t.Fatalf("after 4 polls: %d range queries, %d node lists", len(hist.ranges()), live.listCount())
	}
	got := h.History(sc)
	whole, master, worker := got.Targets[""].Usage, got.Targets[metrics.TargetMaster].Usage, got.Targets[metrics.TargetWorker].Usage
	last := func(pts []model.UsageSample) model.UsageSample { return pts[len(pts)-1] }
	if l := last(whole); l.CPUMilli != 1750 || l.MemoryBytes != 3<<30+512<<20 || !l.At.Equal(clk.Now()) || whole[len(whole)-4].CPUMilli != 2000 {
		t.Errorf("every node: ends %+v after %+v", l, whole[len(whole)-4])
	}
	if l := last(master); l.CPUMilli != 500 || !l.At.Equal(clk.Now()) {
		t.Errorf("master ends %+v", l)
	}
	if l := last(worker); l.CPUMilli != 1250 || l.MemoryBytes != 2<<30+512<<20 {
		t.Errorf("worker ends %+v", l)
	}
	if st := h.State(sc); !st.Usable() || st.Label != "Prometheus (monitoring/prometheus-server) and Kubernetes Metrics Server" {
		t.Errorf("state %+v", st)
	}

	// The Metrics Server fails: Prometheus's newest points in the same poll.
	live.setErr(unavailable())
	clk.Advance(30 * time.Second)
	metrics.Kick(h, sc)
	waitPoll(t, polls, 5)
	qs = hist.ranges()
	if len(qs) != 2 || qs[1].Want != metrics.WantUsage || !qs[1].To.Equal(clk.Now()) || qs[1].Name != "m1" {
		t.Fatalf("the fallback asked %+v", qs)
	}
	if l := last(h.History(sc).Targets[metrics.TargetWorker].Usage); l.CPUMilli != 1500 {
		t.Errorf("worker after the fallback ends %+v, want Prometheus's", l)
	}
}

// TestClusterLiveOnlyWithoutHistory: with the Metrics Server alone (the demo cluster's kind of
// provider) the chart starts at the first poll with the sums, a cluster without masters has no
// master target, and a list's fresh result seeds the chart without the first poll's point at the
// same time counting twice.
func TestClusterLiveOnlyWithoutHistory(t *testing.T) {
	clk := &testClock{t: time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)}
	live := &nodesLive{clk: clk}
	h := metrics.NewHub(detectorFor(live), metrics.Options{ObjectInterval: time.Hour, ListInterval: time.Hour, Now: clk.Now})
	t.Cleanup(h.Close)
	nodes, nodePolls := pollCounter()
	list := h.Subscribe(metrics.Scope{Kind: metrics.KindNodes}, nodes)
	waitPoll(t, nodePolls, 1)
	sc := metrics.ClusterScope(nil)
	polls := make(chan int, 4)
	sub := h.Subscribe(sc, func() { polls <- 1 })
	t.Cleanup(sub.Release)
	list.Release()
	waitPoll(t, polls, 1)
	got := h.History(sc)
	if got == nil || got.Targets[""] == nil || got.Targets[metrics.TargetMaster] != nil || got.Targets[metrics.TargetWorker] == nil {
		t.Fatalf("targets %+v", got)
	}
	if w := got.Targets[metrics.TargetWorker].Usage; len(w) != 1 || w[0].CPUMilli != 1750 {
		t.Fatalf("workers %+v: every node is a worker without masters", w)
	}
	if live.listCount() != 2 {
		t.Errorf("%d node lists, want the list view's and the cluster's first poll's", live.listCount())
	}
}

// TestClusterHistoryFromNodeExporter (A43, F1): on a Prometheus with node-exporter's series the
// cluster's history is theirs, every exporter for every node and the masters by name or address
// (kube-prometheus-stack's exporter is known by its host-network address only), the workers every
// node less the masters; masters without an exporter leave every exporter's sum to the workers.
func TestClusterHistoryFromNodeExporter(t *testing.T) {
	c := newPromCluster(t, false)
	c.prom.on(`node_cpu_seconds_total{instance=~"(cp|10\\.0\\.0\\.1)(:[0-9]+)?"`, s(nil, 1000, 0.5, 1030, 0.75))
	c.prom.on(`node_memory_MemTotal_bytes{instance=~"(cp|10\\.0\\.0\\.1)(:[0-9]+)?"`, s(nil, 1000, 1<<30, 1030, 1<<30))
	c.prom.on(`node_cpu_seconds_total{mode!~`, s(nil, 1000, 2, 1030, 2.5))
	c.prom.on(`node_memory_MemTotal_bytes - node_memory_MemFree_bytes`, s(nil, 1000, 4<<30, 1030, 5<<30))
	prov := promProvider(t, c, metrics.PrometheusOptions{})
	to := time.Unix(100020, 0)
	q := metrics.RangeQuery{Kind: metrics.KindCluster, Name: metrics.ClusterScope([]string{"cp"}).Name, Addresses: []string{"10.0.0.1"},
		From: to.Add(-time.Hour), To: to, Step: 30 * time.Second, Want: metrics.WantUsage}
	got, err := prov.ObjectRange(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsageSource != metrics.UsageNodeExporter || c.prom.queryCount("container_") != 0 {
		t.Errorf("source %q, %d cAdvisor queries", got.UsageSource, c.prom.queryCount("container_"))
	}
	m, w := got.Targets[metrics.TargetMaster], got.Targets[metrics.TargetWorker]
	if m == nil || w == nil || len(w.Usage) != 2 || w.Usage[1].CPUMilli != 1750 || w.Usage[1].MemoryBytes != 4<<30 || m.Usage[1].CPUMilli != 750 {
		t.Errorf("masters %+v, workers %+v", m, w)
	}

	// The masters without an exporter (the DaemonSet does not tolerate their taint): every
	// exporter is a worker's.
	q.Addresses = nil
	if got, err = prov.ObjectRange(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if got.Targets[metrics.TargetMaster] != nil || len(got.Targets[metrics.TargetWorker].Usage) != 2 || got.Targets[metrics.TargetWorker].Usage[1].CPUMilli != 2500 {
		t.Errorf("masters without an exporter: %+v", got.Targets)
	}
}

// TestClusterRangeQueries: the cluster's history is a sum over the nodes' root cgroups, every node
// without a node matcher and the masters by name (quoted for the regular expression and for
// PromQL), the workers every node less the masters; two queries without masters.
func TestClusterRangeQueries(t *testing.T) {
	c := newPromCluster(t, false)
	c.prom.on(`container_cpu_usage_seconds_total{id="/",node=~`, s(nil, 1000, 0.5, 1030, 0.75))
	c.prom.on(`container_memory_working_set_bytes{id="/",node=~`, s(nil, 1000, 1<<30, 1030, 1<<30))
	c.prom.on(`container_cpu_usage_seconds_total{id="/"}`, s(nil, 1000, 2, 1030, 2.5, 1060, 3))
	c.prom.on(`container_memory_working_set_bytes{id="/"}`, s(nil, 1000, 4<<30, 1030, 5<<30))
	prov := promProvider(t, c, metrics.PrometheusOptions{})
	to := time.Unix(100020, 0)
	q := metrics.RangeQuery{Kind: metrics.KindCluster, Name: metrics.ClusterScope([]string{"cp.1", "m-2"}).Name, From: to.Add(-time.Hour), To: to, Step: 30 * time.Second, Want: metrics.WantUsage}
	before := c.proxyCount()
	got, err := prov.ObjectRange(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if n := c.proxyCount() - before; n != 8 || got.UsageSource != metrics.UsageCAdvisor {
		t.Errorf("%d requests from %q, want 8 from cadvisor (cpu and memory, every node and the masters, from node-exporter, which this one lacks, then from cAdvisor)", n, got.UsageSource)
	}
	var master string
	for _, r := range c.prom.requests() {
		if strings.Contains(r.query, "node=~") && strings.Contains(r.query, "container_cpu") {
			master = r.query
		}
	}
	if !strings.Contains(master, `node=~"(cp\\.1|m-2)"`) || !strings.Contains(master, `instance=~"(cp\\.1|m-2)(:[0-9]+)?"`) || !strings.Contains(master, "[4m]") {
		t.Errorf("the masters' CPU query %s", master)
	}
	whole, m, w := got.Targets[""], got.Targets[metrics.TargetMaster], got.Targets[metrics.TargetWorker]
	if whole == nil || m == nil || w == nil {
		t.Fatalf("targets %v", got.Targets)
	}
	if len(whole.Usage) != 3 || whole.Usage[1].CPUMilli != 2500 || whole.Usage[2].HasMemory() {
		t.Errorf("every node %+v (a time with CPU only is missing its memory)", whole.Usage)
	}
	if len(w.Usage) != 2 || w.Usage[0].CPUMilli != 1500 || w.Usage[1].CPUMilli != 1750 || w.Usage[1].MemoryBytes != 4<<30 {
		t.Errorf("workers %+v, want every node less the masters at the times both have", w.Usage)
	}

	// Without masters: two queries, and the workers are every node.
	before = c.proxyCount()
	q.Name = ""
	got, err = prov.ObjectRange(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if n := c.proxyCount() - before; n != 2 || got.Targets[metrics.TargetMaster] != nil || len(got.Targets[metrics.TargetWorker].Usage) != 3 {
		t.Errorf("without masters: %d requests, targets %v", n, got.Targets)
	}
	// Only the network asked for: the cluster has none, and nothing is sent.
	before = c.proxyCount()
	q.Want = metrics.WantNetwork
	if got, err = prov.ObjectRange(context.Background(), q); err != nil || len(got.Targets) != 0 || c.proxyCount() != before {
		t.Errorf("the network of the cluster: %v, %v, %d requests", got, err, c.proxyCount()-before)
	}
}

// TestClusterBudget (P14): the open Overview on a cluster with the Metrics Server and Prometheus
// costs one probe and one history (four range queries) when it opens, then one node list per poll
// and no Prometheus request; a range change asks once more; nothing after the release.
func TestClusterBudget(t *testing.T) {
	c := newPromCluster(t, true)
	for _, n := range []string{"m1", "w1", "w2"} {
		c.srv.Set(nodeMetricsGVR, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "metrics.k8s.io/v1beta1", "kind": "NodeMetrics", "metadata": map[string]any{"name": n},
			"timestamp": "2026-10-12T10:00:00Z", "window": "15s", "usage": map[string]any{"cpu": "500m", "memory": "1Gi"},
		}})
	}
	c.prom.onRel(`node=~`, s(nil, -60, 0.5, -30, 0.5, 0, 0.5))
	c.prom.onRel(`{id="/"}`, s(nil, -60, 1.5, -30, 1.5, 0, 1.5))
	c.srv.SetProxy("monitoring", "prometheus-server:80", c.prom)
	src, _, err := metrics.SourcesForConn(c.conn, servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h := metrics.NewHubFor(src, metrics.Options{ListInterval: 200 * time.Millisecond, ObjectInterval: 200 * time.Millisecond, MinStep: 5 * time.Millisecond, RangeWindow: 600 * time.Millisecond})
	t.Cleanup(h.Close)
	count := func(key string) int64 { return c.conn.Counter().Snapshot().ByVerbResource[key] }

	onChange, polls := pollCounter()
	sc := metrics.ClusterScope([]string{"m1"})
	sub := h.Subscribe(sc, onChange)
	waitPoll(t, polls, 4)
	how := "the hub over the real client-go stack against the fake API server and a fake Prometheus (no node-exporter) behind its service proxy, the intervals shortened to 0.2 s"
	scorecard.Record(t, scorecard.Measurement{Budget: "P14", Metric: "Prometheus requests of the open Overview in 4 polls: 1 probe, 4 range queries and the root cgroups' 4",
		Value: float64(c.proxyCount()), Unit: "requests", Limit: 1 + 4 + 4, How: how})
	if n := c.proxyCount(); n != 1+4+4 {
		t.Errorf("%d Prometheus requests after 4 polls, want 9 (1 probe + cpu and memory of every node and the master, once from node-exporter, which this one lacks, once from cAdvisor)", n)
	}
	if n := count("list nodes.metrics.k8s.io"); n < 4 {
		t.Errorf("%d node lists in 4 polls", n)
	}
	hist := h.History(sc)
	if hist == nil || hist.Targets[""] == nil || hist.Targets[metrics.TargetMaster] == nil || hist.Targets[metrics.TargetWorker] == nil {
		t.Fatalf("history %+v", hist)
	}
	if w := hist.Targets[metrics.TargetWorker].Usage; len(w) == 0 || w[0].CPUMilli != 1000 {
		t.Errorf("workers %+v", w)
	}

	// A range change: one query set more, from cAdvisor only (this Prometheus's nodes come from it).
	sub.SetWindows(1200 * time.Millisecond)
	waitPoll(t, polls, 6)
	scorecard.Record(t, scorecard.Measurement{Budget: "P14", Metric: "Prometheus requests of a range change", Value: float64(c.proxyCount() - 9), Unit: "requests", Limit: 4, How: how})
	if n := c.proxyCount(); n != 1+8+4 {
		t.Errorf("%d Prometheus requests after a range change, want 13", n)
	}
	sub.Release()
	settled := c.conn.Counter().Snapshot().Total
	time.Sleep(450 * time.Millisecond)
	scorecard.Record(t, scorecard.Measurement{Budget: "P14", Metric: "requests in 450 ms after the Overview closed", Value: float64(c.conn.Counter().Snapshot().Total - settled), Unit: "requests", How: how})
	if got := c.conn.Counter().Snapshot().Total; got != settled {
		t.Errorf("%d requests after the release", got-settled)
	}
}
