package metrics_test

import (
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// The cluster's Metrics source (Cluster Settings › Metrics; Lens's dropdown, captured): Automatic
// asks both providers for their parts; Prometheus or the Metrics Server alone turns the other off,
// No metrics both. An off provider is never detected nor asked and leaves the status line.

func TestMetricsSourcePrometheusAlone(t *testing.T) {
	d := newDual(t)
	if d.h.SetSource(metrics.SourceAuto) {
		t.Error("Automatic changed a hub that has both providers on")
	}
	if !d.h.SetSource(metrics.SourcePrometheus) {
		t.Fatal("Prometheus alone changed nothing")
	}
	d.open(t)
	d.next(t, 30*time.Second)
	if gets, _ := d.live.counts(); gets != 0 || d.liveDet.n.Load() != 0 {
		t.Errorf("the Metrics Server was asked %d times and detected %d times while turned off", gets, d.liveDet.n.Load())
	}
	if len(d.hist.ranges()) == 0 {
		t.Error("Prometheus served nothing")
	}
	if ps := d.h.Providers(); len(ps) != 1 || ps[0].Provider != metrics.PrometheusID || !strings.Contains(strings.Join(ps[0].Serves, ","), metrics.ServesLive) {
		t.Errorf("status line %+v, want Prometheus alone serving the live values", ps)
	}
	if !d.h.DisksPossible() {
		t.Error("no disks with Prometheus on")
	}
}

func TestMetricsSourceMetricsServerAlone(t *testing.T) {
	d := newDual(t)
	d.h.SetSource(metrics.SourceMetricsServer)
	d.open(t)
	d.next(t, 15*time.Second)
	if n := len(d.hist.ranges()); n != 0 || d.histDet.n.Load() != 0 {
		t.Errorf("Prometheus was asked %d range queries and detected %d times while turned off", n, d.histDet.n.Load())
	}
	if gets, _ := d.live.counts(); gets == 0 {
		t.Error("the Metrics Server served nothing")
	}
	if d.h.Capabilities().History || d.h.DisksPossible() {
		t.Error("history or disks without Prometheus")
	}
	if st := d.h.DiskState(); st.Status != metrics.StatusUnknown {
		t.Errorf("disk state %+v before any disk poll", st)
	}
	if ps := d.h.Providers(); len(ps) != 1 || ps[0].Provider != metrics.ServerID {
		t.Errorf("status line %+v, want the Metrics Server alone", ps)
	}
}

func TestMetricsSourceNone(t *testing.T) {
	d := newDual(t)
	d.open(t) // both on: the history and a live value
	if len(d.chart(t)) == 0 {
		t.Fatal("no chart with both providers on")
	}
	d.h.SetSource(metrics.SourceNone)
	gets, _ := d.live.counts()
	ranges := len(d.hist.ranges())
	d.next(t, 15*time.Second)
	if g, _ := d.live.counts(); g != gets || len(d.hist.ranges()) != ranges {
		t.Errorf("metrics turned off still asked: %d gets, %d range queries more", g-gets, len(d.hist.ranges())-ranges)
	}
	st := d.h.State(podA)
	if st.Status != metrics.StatusNotServed || !strings.Contains(st.Message, "switched off for this cluster") {
		t.Errorf("state %+v", st)
	}
	if ps := d.h.Providers(); len(ps) != 0 {
		t.Errorf("status line %+v with metrics off", ps)
	}
	if hist := d.h.History(podA); hist != nil && hist.Targets[""] != nil && len(hist.Targets[""].Usage) != 0 {
		t.Errorf("the chart kept %d points of the providers turned off", len(hist.Targets[""].Usage))
	}

	// Back to Automatic: both are detected and asked again. The switch wakes the drawer's poll
	// itself, so the test waits for its effect, not for a count of polls.
	d.h.SetSource(metrics.SourceAuto)
	deadline := time.Now().Add(10 * time.Second)
	for len(d.hist.ranges()) == ranges || len(d.h.Providers()) != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("after Automatic again: %d range queries more, status line %+v", len(d.hist.ranges())-ranges, d.h.Providers())
		}
		metrics.Kick(d.h, podA)
		time.Sleep(5 * time.Millisecond)
	}
}
