package engine

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The cluster Overview's Pods chart with Prometheus's history (G12): kube-state-metrics' counts
// before the page opened, then the cache's.

// podsHistory is a Prometheus with the cluster's history: CPU and memory (counting two machines
// besides the cluster's nodes), and the workers' pod counts 30, 20 and 10 minutes back when it has
// kube-state-metrics.
type podsHistory struct {
	ksm   bool
	mu    sync.Mutex
	asks  []metrics.Want
	nodes [][]string
}

func (*podsHistory) ID() string    { return metrics.PrometheusID }
func (*podsHistory) Label() string { return "Prometheus (monitoring/prometheus-server)" }
func (*podsHistory) Capabilities() metrics.Capabilities {
	return metrics.Capabilities{History: true, Network: true, Filesystem: true}
}
func (*podsHistory) ListPods(context.Context, string) ([]model.PodUsage, error) { return nil, nil }
func (*podsHistory) GetPod(context.Context, string, string) (model.PodUsage, error) {
	return model.PodUsage{}, metrics.ErrNoData
}
func (*podsHistory) ListNodes(context.Context) ([]model.NodeUsage, error) { return nil, nil }
func (*podsHistory) GetNode(context.Context, string) (model.NodeUsage, error) {
	return model.NodeUsage{}, metrics.ErrNoData
}

func (p *podsHistory) ObjectRange(_ context.Context, q metrics.RangeQuery) (*metrics.ObjectSeries, error) {
	p.mu.Lock()
	p.asks = append(p.asks, q.Want)
	p.nodes = append(p.nodes, q.Nodes)
	p.mu.Unlock()
	out := &metrics.ObjectSeries{Targets: map[string]*metrics.TargetSeries{}, From: q.From, To: q.To, Step: q.Step}
	now := time.Now()
	worker := &metrics.TargetSeries{}
	out.Targets[metrics.TargetWorker] = worker
	if q.Want.Has(metrics.WantUsage) {
		worker.Usage = []model.UsageSample{{At: now.Add(-20 * time.Minute), Usage: model.Usage{CPUMilli: 900, MemoryBytes: 2 << 30}}}
		out.UsageSource, out.ForeignMachines = metrics.UsageNodeExporter, 2
	}
	if q.Want.Has(metrics.WantPods) {
		out.PodsSource = metrics.PodsFromNowhere
		if p.ksm {
			out.PodsSource = metrics.PodsFromKubeStateMetrics
			for i, v := range []float64{1, 2, 3} {
				worker.Pods = append(worker.Pods, model.CountSample{At: now.Add(-time.Duration(30-10*i) * time.Minute), Value: v})
			}
		}
	}
	return out, nil
}

func (p *podsHistory) wants() []metrics.Want {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]metrics.Want(nil), p.asks...)
}

func TestClusterOverviewPodsHistory(t *testing.T) {
	for _, ksm := range []bool{true, false} {
		src := overviewFixture(time.Now())
		prom := &podsHistory{ksm: ksm}
		s := clusterOverviewSession(t, src, func() *metrics.Hub {
			return metrics.NewHubFor(metrics.Sources{Live: metrics.Fixed(nodesProvider{}), History: metrics.Fixed(prom)},
				metrics.Options{ObjectInterval: 20 * time.Millisecond, ListInterval: time.Hour, TTL: 10 * time.Millisecond})
		})
		s.UpdateUI(func(u *UIState) { u.View = "nodes" })
		s.UpdateUI(func(u *UIState) { u.View = ClusterOverview })
		var m *ClusterOverviewModel
		waitFor(t, "the pods' history", func() bool {
			var err error
			if m, err = s.ClusterOverview(context.Background(), "", nil, true); err != nil {
				t.Fatal(err)
			}
			// With kube-state-metrics, also the cache's count after the history's three samples: the
			// chart can show the history before the count joins it.
			return m.PodsHistory != "" && m.Charts[2].Current != nil && (!ksm || len(m.Charts[2].Samples) >= 4)
		})
		pods := m.Charts[2]
		if !ksm {
			if m.PodsHistory != metrics.PodsFromNowhere || len(pods.Samples) == 0 || pods.Samples[0].At.Before(time.Now().Add(-time.Minute)) {
				t.Errorf("without kube-state-metrics: %q, samples %+v (want the cache's alone)", m.PodsHistory, pods.Samples)
			}
			continue
		}
		if m.PodsHistory != metrics.PodsFromKubeStateMetrics || len(pods.Samples) < 4 {
			t.Fatalf("with kube-state-metrics: %q, samples %+v", m.PodsHistory, pods.Samples)
		}
		// The history first (1, 2, 3 from 30 minutes back), then the cache's count (2 on the workers).
		if pods.Samples[0].Value != 1 || pods.Samples[2].Value != 3 || pods.Samples[3].Value != 2 || *pods.Current != 2 {
			t.Errorf("pods samples %+v, current %v", pods.Samples, *pods.Current)
		}
		for i := 1; i < len(pods.Samples); i++ {
			if !pods.Samples[i].At.After(pods.Samples[i-1].At) {
				t.Errorf("samples out of order at %d: %+v", i, pods.Samples)
			}
		}
		// The pods were asked for with the history, once.
		n := 0
		for _, w := range prom.wants() {
			if w.Has(metrics.WantPods) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("the pod counts asked for %d times (%v), want once", n, prom.wants())
		}
		// The history was asked for the cluster's nodes, and the machines beyond them reach the page
		// (D-157).
		prom.mu.Lock()
		nodes := prom.nodes[0]
		prom.mu.Unlock()
		if !slices.Equal(nodes, []string{"master-1", "worker-1", "worker-2"}) || m.ForeignMachines != 2 {
			t.Errorf("the history asked for the nodes %v, the page counts %d foreign machines", nodes, m.ForeignMachines)
		}
	}
}
