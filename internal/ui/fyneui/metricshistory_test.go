package fyneui

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// F1 (owner, 2026-10-14 evening): a node's chart had no past and said nothing about it. Its
// history now comes from node-exporter where the cluster's Prometheus has it (A43), and where that
// Prometheus has no CPU or memory series of the object at all, the drawer and the cluster Overview
// say so under the source line instead of leaving a chart that silently starts late.

// nodeHistoryFake is a node's drawer with Prometheus behind it: an hour of node-exporter's
// history, or (source "none") only the five minutes Cogpit measured since the drawer opened.
func nodeHistoryFake(t *testing.T, source string) (*fakeEngine, model.ObjectRef) {
	t.Helper()
	f := kindsFake(t, "nodes")
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: fixtureNodeSpecs()[0].name}
	f.metrics = map[string]*engine.ObjectMetrics{ref.String(): nodeHistoryMetrics(ref, source)}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	return f, ref
}

func nodeHistoryMetrics(ref model.ObjectRef, source string) *engine.ObjectMetrics {
	n := 240 // an hour at 15 s
	if source == metrics.UsageNone {
		n = 20 // five minutes at 15 s
	}
	samples := metricsSeries(n, func(i int) model.Usage {
		return model.Usage{CPUMilli: int64(900 + 6*i + 120*(i%7)), MemoryBytes: 3<<30 + int64(i%11)<<24}
	})
	last := samples[len(samples)-1].Usage
	return &engine.ObjectMetrics{
		Ref: ref, Status: metrics.StatusAvailable, Source: "Prometheus (monitoring/kps-kube-prometheus-stack-prometheus) and Kubernetes Metrics Server",
		Capabilities: metrics.Capabilities{History: true, Network: true, Filesystem: true}, HistorySource: source,
		Providers: []metrics.ProviderStatus{
			{Provider: metrics.PrometheusID, Label: "Prometheus", Service: "monitoring/kps-kube-prometheus-stack-prometheus:http-web", Status: metrics.StatusAvailable,
				LastOK: fixedNow.Add(-40 * time.Second), Serves: []string{metrics.ServesHistory, metrics.ServesNetwork, metrics.ServesFilesystem}},
			{Provider: metrics.ServerID, Label: metrics.ServerLabel, Status: metrics.StatusAvailable, LastOK: fixedNow.Add(-5 * time.Second), Serves: []string{metrics.ServesLive}},
		},
		Targets: []engine.MetricsTarget{{
			Current: &last, At: fixedNow, Samples: samples, Window: time.Hour, Range: "1h", Variant: engine.VariantCPU,
			Allocatable: model.Usage{CPUMilli: 3920, MemoryBytes: 7 << 30}, Capacity: model.Usage{CPUMilli: 4000, MemoryBytes: 8 << 30},
		}},
	}
}

func TestNoPrometheusHistoryNote(t *testing.T) {
	f, ref := nodeHistoryFake(t, metrics.UsageNone)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the node's metrics", func() bool {
		p, ok := sh.drawer.kept[metricsKey].(*metricsPanel)
		return ok && p.data != nil
	})
	p := sh.drawer.kept[metricsKey].(*metricsPanel)
	want := "Prometheus has no history of this Node; Cogpit shows what it measured."
	if !p.note.Visible() || p.note.text != want {
		t.Errorf("note visible %v %q, want %q", p.note.Visible(), p.note.text, want)
	}

	// node-exporter's history (A43): nothing to say.
	f.mu.Lock()
	f.metrics[ref.String()] = nodeHistoryMetrics(ref, metrics.UsageNodeExporter)
	f.mu.Unlock()
	f.emit(engine.Event{Type: engine.EventMetrics, View: "nodes"})
	q.until(t, "the note to go", func() bool { return !p.note.Visible() })
	if len(p.plot.data.Series) != 1 || len(p.plot.data.Series[0].Points) != 240 {
		t.Errorf("the chart draws %+v, want the hour's 240 points", p.plot.data.Series)
	}
}

// clusterFakeWithoutUsageHistory is clusterFake on a Prometheus with no CPU or memory series of
// the nodes: the charts hold the last nine minutes Cogpit measured.
func clusterFakeWithoutUsageHistory(t *testing.T) *fakeEngine {
	f := clusterFake(t)
	f.handlers["cluster.overview"] = func(context.Context, engine.Invocation) (any, error) {
		f.mu.Lock()
		choice := f.ui.Overview
		f.mu.Unlock()
		m := clusterModelFor(choice)
		m.UsageHistory = metrics.UsageNone
		for i := range m.Charts[:2] {
			m.Charts[i].Samples = m.Charts[i].Samples[len(m.Charts[i].Samples)-18:]
		}
		return m, nil
	}
	return f
}

func TestClusterOverviewNoUsageHistoryNote(t *testing.T) {
	f := clusterFakeWithoutUsageHistory(t)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the page", func() bool { return sh.view.cluster.data != nil })
	settle(w)
	if got, want := sh.view.cluster.note.text, "Prometheus has no history of the nodes; Cogpit shows what it measured."; got != want {
		t.Errorf("note %q, want %q", got, want)
	}
}

func TestRenderNoPrometheusHistory(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		f, _ := nodeHistoryFake(t, metrics.UsageNone)
		sh, w, q := newTestShell(t, f, Options{Theme: mode})
		q.until(t, "the node's metrics", func() bool {
			p, ok := sh.drawer.kept[metricsKey].(*metricsPanel)
			return ok && p.data != nil && p.note.Visible()
		})
		savePNG(t, w, "node_no_history_"+string(mode)+".png")

		f, _ = nodeHistoryFake(t, metrics.UsageNodeExporter)
		sh, w, q = newTestShell(t, f, Options{Theme: mode})
		q.until(t, "the node's metrics", func() bool {
			p, ok := sh.drawer.kept[metricsKey].(*metricsPanel)
			return ok && p.data != nil
		})
		savePNG(t, w, "node_history_node_exporter_"+string(mode)+".png")

		f = clusterFakeWithoutUsageHistory(t)
		sh, w, q = newTestShell(t, f, Options{Theme: mode})
		q.until(t, "the page", func() bool {
			return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.cluster.data != nil && sh.diag.total > 0
		})
		savePNG(t, w, "cluster_overview_no_history_"+string(mode)+".png")
	}
}
