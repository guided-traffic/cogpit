package fyneui

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
)

// The cluster Overview's Pods chart with its history from Prometheus (G12): kube-state-metrics'
// counts over the range before the page opened, then the cache's; the note says where they come
// from.

// clusterFakeWithPodsHistory is clusterFake whose Pods chart has an hour of kube-state-metrics'
// counts at 30 s before the cache's nine minutes.
func clusterFakeWithPodsHistory(t *testing.T, source string) *fakeEngine {
	f := clusterFake(t)
	f.handlers["cluster.overview"] = func(context.Context, engine.Invocation) (any, error) {
		f.mu.Lock()
		choice := f.ui.Overview
		f.mu.Unlock()
		m := clusterModelFor(choice)
		m.PodsHistory = source
		if source != metrics.PodsFromKubeStateMetrics {
			return m, nil
		}
		pods := &m.Charts[2]
		var hist []engine.OverviewSample
		for t, i := fixedNow.Add(-m.Window), 0; t.Before(pods.Samples[0].At); t, i = t.Add(30*time.Second), i+1 {
			hist = append(hist, engine.OverviewSample{At: t, Value: math.Round(38 + 4*math.Sin(float64(i)/14))})
		}
		pods.Samples = append(hist, pods.Samples...)
		return m, nil
	}
	return f
}

func TestClusterOverviewPodsHistoryNote(t *testing.T) {
	for _, tc := range []struct {
		source, note string
	}{
		{metrics.PodsFromKubeStateMetrics, "Pods before this page opened: kube-state-metrics through Prometheus; since then the cluster's objects."},
		{metrics.PodsFromNowhere, "Pods are counted from the cluster's objects since this page opened: Prometheus has no kube-state-metrics series."},
		{"", "Pods are counted from the cluster's objects since this page opened."},
	} {
		f := clusterFakeWithPodsHistory(t, tc.source)
		sh, w, q := newTestShell(t, f, Options{})
		q.until(t, "the page", func() bool { return sh.view.cluster.data != nil })
		settle(w)
		if got := sh.view.cluster.note.text; got != tc.note {
			t.Errorf("%q: note %q, want %q", tc.source, got, tc.note)
		}
		if tc.source == metrics.PodsFromKubeStateMetrics {
			pods := sh.view.cluster.cards[2].plot.data
			if len(pods.Series) == 0 || len(pods.Series[0].Points) < 100 {
				t.Errorf("the Pods chart draws %+v, want the hour's history", len(pods.Series))
			}
		}
	}
}
