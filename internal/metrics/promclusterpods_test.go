package metrics_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// The cluster Overview's Pods history (G12): kube-state-metrics' counts of the pods that have not
// finished on the cluster's nodes, through Prometheus, once over the chart's range.

func TestClusterPodCountQueries(t *testing.T) {
	c := newPromCluster(t, false)
	// The pods rules first: their queries also contain the masters' `node=~`.
	c.prom.on(`kube_pod_info{node=~`, s(nil, 1000, 3, 1060, 4))
	c.prom.on(`kube_pod_info{node!=""}`, s(nil, 1000, 20, 1030, 21, 1060, 25))
	prov := promProvider(t, c, metrics.PrometheusOptions{})
	to := time.Unix(100020, 0)
	q := metrics.RangeQuery{Kind: metrics.KindCluster, Name: metrics.ClusterScope([]string{"cp.1", "m-2"}).Name, From: to.Add(-time.Hour), To: to, Step: 30 * time.Second, Want: metrics.WantPods}
	before := c.proxyCount()
	got, err := prov.ObjectRange(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if n := c.proxyCount() - before; n != 2 {
		t.Errorf("%d requests, want 2 (every node's pods and the masters'; no CPU or memory asked)", n)
	}
	var all, master string
	for _, r := range c.prom.requests() {
		switch {
		case strings.Contains(r.query, `node!=""`):
			all = r.query
		case strings.Contains(r.query, "kube_pod_info{node=~"):
			master = r.query
		}
	}
	wantAll := `count(max by (namespace, pod) (kube_pod_info{node!=""}) unless on (namespace, pod) (max by (namespace, pod) (kube_pod_status_phase{phase=~"Succeeded|Failed"}) == 1))`
	if all != wantAll {
		t.Errorf("every node's query\n%s\nwant\n%s", all, wantAll)
	}
	if !strings.Contains(master, `kube_pod_info{node=~"(cp\\.1|m-2)"}`) {
		t.Errorf("the masters' query %s", master)
	}
	if got.PodsSource != metrics.PodsFromKubeStateMetrics {
		t.Errorf("source %q", got.PodsSource)
	}
	whole, m, w := got.Targets[""], got.Targets[metrics.TargetMaster], got.Targets[metrics.TargetWorker]
	if whole == nil || m == nil || w == nil || len(whole.Pods) != 3 || len(m.Pods) != 3 || len(w.Pods) != 3 {
		t.Fatalf("targets %+v", got.Targets)
	}
	// At 1030 the masters have no count: no unfinished pod on them, so the workers have all 21.
	if m.Pods[1].Value != 0 || w.Pods[1].Value != 21 || w.Pods[0].Value != 17 || w.Pods[2].Value != 21 {
		t.Errorf("masters %+v workers %+v", m.Pods, w.Pods)
	}
	if len(whole.Usage) != 0 {
		t.Errorf("CPU and memory were not asked for, got %+v", whole.Usage)
	}

	// Without masters: one query, the workers are every node.
	q.Name = ""
	before = c.proxyCount()
	got, err = prov.ObjectRange(context.Background(), q)
	if err != nil || c.proxyCount()-before != 1 || len(got.Targets[metrics.TargetWorker].Pods) != 3 || got.Targets[metrics.TargetMaster] != nil {
		t.Errorf("without masters: %d requests, %+v, %v", c.proxyCount()-before, got, err)
	}
}

// A Prometheus without kube-state-metrics answers nothing: the source says so, the counts stay
// the cache's, and CPU and memory's history is not held up.
func TestClusterPodCountsWithoutKubeStateMetrics(t *testing.T) {
	c := newPromCluster(t, false)
	c.prom.on(`container_cpu_usage_seconds_total{id="/"}`, s(nil, 1000, 2, 1030, 2.5))
	c.prom.on(`container_memory_working_set_bytes{id="/"}`, s(nil, 1000, 4<<30, 1030, 5<<30))
	prov := promProvider(t, c, metrics.PrometheusOptions{})
	to := time.Unix(100020, 0)
	got, err := prov.ObjectRange(context.Background(), metrics.RangeQuery{Kind: metrics.KindCluster, From: to.Add(-time.Hour), To: to, Step: 30 * time.Second, Want: metrics.WantUsage | metrics.WantPods})
	if err != nil {
		t.Fatal(err)
	}
	if got.PodsSource != metrics.PodsFromNowhere || len(got.Targets[""].Pods) != 0 || len(got.Targets[""].Usage) != 2 {
		t.Errorf("source %q, targets %+v", got.PodsSource, got.Targets[""])
	}
}

// P14 with the Pods history: the Overview's first poll asks the pod counts once beside CPU and
// memory's history (two queries more with masters), the next polls ask nothing of Prometheus, a
// range change asks both again; the history serves the counts by target.
func TestClusterPodsHistoryBudget(t *testing.T) {
	c := newPromCluster(t, true)
	for _, n := range []string{"m1", "w1", "w2"} {
		c.srv.Set(nodeMetricsGVR, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "metrics.k8s.io/v1beta1", "kind": "NodeMetrics", "metadata": map[string]any{"name": n},
			"timestamp": "2026-10-12T10:00:00Z", "window": "15s", "usage": map[string]any{"cpu": "500m", "memory": "1Gi"},
		}})
	}
	c.prom.onRel(`kube_pod_info{node=~`, s(nil, -60, 2, 0, 2))
	c.prom.onRel(`kube_pod_info{node!=""}`, s(nil, -60, 30, -30, 31, 0, 32))
	c.prom.onRel(`node=~`, s(nil, -60, 0.5, -30, 0.5, 0, 0.5))
	c.prom.onRel(`{id="/"}`, s(nil, -60, 1.5, -30, 1.5, 0, 1.5))
	c.srv.SetProxy("monitoring", "prometheus-server:80", c.prom)
	src, _, err := metrics.SourcesForConn(c.conn, servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h := metrics.NewHubFor(src, metrics.Options{ListInterval: 200 * time.Millisecond, ObjectInterval: 200 * time.Millisecond, MinStep: 5 * time.Millisecond, RangeWindow: 600 * time.Millisecond})
	t.Cleanup(h.Close)

	onChange, polls := pollCounter()
	sc := metrics.ClusterScope([]string{"m1"})
	sub := h.Subscribe(sc, onChange)
	sub.SetWants(metrics.WantPods)
	waitPoll(t, polls, 4)
	if n := c.prom.queryCount("kube_pod_info"); n != 2 {
		t.Errorf("%d pod-count queries in 4 polls, want 2 (every node and the master, once)", n)
	}
	if n := c.proxyCount(); n != 1+4+4+2 {
		t.Errorf("%d Prometheus requests after 4 polls, want 11 (1 probe + 4 for node-exporter's CPU and memory, which this one lacks, + 4 for cAdvisor's + 2 pod counts)", n)
	}
	hist := h.History(sc)
	if hist == nil || hist.PodsSource != metrics.PodsFromKubeStateMetrics {
		t.Fatalf("history %+v", hist)
	}
	if w := hist.Targets[metrics.TargetWorker]; w == nil || len(w.Pods) == 0 || w.Pods[len(w.Pods)-1].Value != 30 {
		t.Errorf("the workers' pods %+v, want every node's 32 less the master's 2", w)
	}

	sub.SetWindows(1200 * time.Millisecond)
	waitPoll(t, polls, 6)
	if n := c.prom.queryCount("kube_pod_info"); n != 4 {
		t.Errorf("%d pod-count queries after a range change, want 4", n)
	}
	// Without the Pods chart (SetWants without pods): nothing more of them, the counts dropped.
	sub.SetWants(0)
	waitPoll(t, polls, 8)
	if n := c.prom.queryCount("kube_pod_info"); n != 4 {
		t.Errorf("%d pod-count queries once the pods are not wanted, want 4", n)
	}
	if hist := h.History(sc); hist.PodsSource != "" || len(hist.Targets[""].Pods) != 0 {
		t.Errorf("the counts outlived their chart: %q, %d", hist.PodsSource, len(hist.Targets[""].Pods))
	}
	sub.Release()
}
