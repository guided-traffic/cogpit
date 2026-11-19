package metrics_test

import (
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// onGrid is t on the hub's grid: a multiple of step since the Unix epoch.
func onGrid(t time.Time, step time.Duration) time.Time {
	s := int64(step / time.Second)
	return time.Unix(t.Unix()/s*s, 0)
}

// A46 (owner): the charts of one object whose ranges lie far apart (the pod's chart at a week, a
// container's at the default hour) get query sets of their own, each at its own step. Before, one
// set asked for the week at ten minutes, and the hour's chart had six points.
func TestFarApartRangesGetTheirOwnQuerySets(t *testing.T) {
	const week = 7 * 24 * time.Hour
	d := newDual(t)
	d.open(t) // the default hour: one set
	d.sub.SetWindows(week, time.Hour)
	d.wait(t)
	now := d.clk.Now()
	qs := d.hist.ranges()
	if len(qs) != 2 {
		t.Fatalf("the week beside the hour asked %+v, want one query more", qs)
	}
	coarse := qs[1]
	if coarse.Step != 605*time.Second || coarse.Want != metrics.WantUsage || !coarse.From.Equal(onGrid(now.Add(-week), 605*time.Second)) {
		t.Errorf("the week's set %+v, want CPU and memory over the week at 605 s", coarse)
	}

	// The chart: the week at its step up to where the hour begins, the hour at 30 s, the live
	// values after it; one point per time, in order.
	pts := d.chart(t)
	hourFrom := now.Add(-time.Hour).Truncate(30 * time.Second)
	var older, recent int
	for i, p := range pts {
		if p.At.Before(hourFrom) {
			older++
			if i > 0 && p.At.Sub(pts[i-1].At) != 605*time.Second {
				t.Fatalf("a point of the week %s after the one before: %v", p.At.Sub(pts[i-1].At), p.At)
			}
		} else {
			recent++
		}
	}
	if older < 990 || recent < 120 {
		t.Errorf("%d points of the week before the hour, %d in it: want about 994 and 121", older, recent)
	}
	if h := d.h.History(podA); len(h.Targets["app"].Usage) != len(pts) {
		t.Errorf("the container's series has %d points, the pod's %d: one answer serves both", len(h.Targets["app"].Usage), len(pts))
	}

	// The network shown: its hour at 30 s and its week at 605 s, then only the hour's set follows
	// at its step.
	d.sub.SetWants(metrics.WantNetwork)
	d.wait(t)
	qs = d.hist.ranges()
	if len(qs) != 4 {
		t.Fatalf("showing the network asked %+v", qs[2:])
	}
	for _, q := range qs[2:] {
		if q.Want != metrics.WantNetwork || q.Step != 30*time.Second && q.Step != 605*time.Second {
			t.Errorf("the network's sets: %+v", q)
		}
	}
	net := d.h.History(podA).Targets[""].Network
	d.next(t, 30*time.Second)
	qs = d.hist.ranges()
	if len(qs) != 5 || qs[4].Step != 30*time.Second || qs[4].Want != metrics.WantNetwork || !qs[4].From.Equal(net[len(net)-1].At) {
		t.Errorf("after a step: %+v, want the network from its last point (%v) at 30 s alone", qs[4:], net[len(net)-1].At)
	}

	// The week becomes two: only the coarse set is asked again (CPU, memory and the network),
	// the hour's stands.
	d.sub.SetWindows(2*week, time.Hour)
	d.wait(t)
	qs = d.hist.ranges()
	if len(qs) != 6 || qs[5].Step != 1210*time.Second || qs[5].Want != metrics.WantUsage|metrics.WantNetwork {
		t.Errorf("two weeks asked %+v, want one set at 1210 s", qs[5:])
	}
	pts = d.chart(t)
	if first := pts[0].At; first.After(d.clk.Now().Add(-2*week + 1210*time.Second)) {
		t.Errorf("the chart begins %v, want two weeks back", first)
	}

	// Every chart at two weeks: the coarse set becomes the only one and is not asked again; the
	// network, shown, follows from its last point at the new step.
	d.sub.SetWindows(2 * week)
	d.wait(t)
	qs = d.hist.ranges()
	if len(qs) != 7 || qs[6].Want != metrics.WantNetwork || qs[6].Step != 1210*time.Second || qs[6].From.Before(d.clk.Now().Add(-time.Hour)) {
		t.Errorf("two weeks alone asked %+v, want the network's newest points alone", qs[6:])
	}

	// Back to the hour: a set no range showed any more is forgotten, so the hour is asked for once
	// (CPU, memory and the network at 30 s), then the same range again asks nothing.
	d.sub.SetWindows(time.Hour)
	d.wait(t)
	qs = d.hist.ranges()
	if len(qs) != 8 || qs[7].Step != 30*time.Second || qs[7].Want != metrics.WantUsage|metrics.WantNetwork {
		t.Errorf("back to the hour asked %+v", qs[7:])
	}
	d.sub.SetWindows(time.Hour)
	d.next(t, 15*time.Second)
	if n := len(d.hist.ranges()); n != 8 {
		t.Errorf("an unchanged range asked again: %d queries", n)
	}
	d.chart(t)
}
