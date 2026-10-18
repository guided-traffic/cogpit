package fyneui

import (
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A chart's line breaks at a gap (G12: a drawer's chart kept after it closed, reopened ten minutes
// later): no line across the time nobody measured; a point alone is a dot.

func pointsEvery(start time.Time, n int, step time.Duration) []chartPoint {
	out := make([]chartPoint, n)
	for i := range out {
		out[i] = chartPoint{At: start.Add(time.Duration(i) * step), Value: float64(i)}
	}
	return out
}

func TestSeriesRunsBreakAtAGap(t *testing.T) {
	t0 := fixedNow.Add(-time.Hour)
	// 20 points at 15 s, a drawer closed for ten minutes, then 8 points, then one after a 3-minute
	// hole, then a missed measurement (45 s) that still joins.
	pts := pointsEvery(t0, 20, 15*time.Second)
	pts = append(pts, pointsEvery(pts[19].At.Add(10*time.Minute), 8, 15*time.Second)...)
	pts = append(pts, chartPoint{At: pts[27].At.Add(3 * time.Minute)})
	pts = append(pts, chartPoint{At: pts[28].At.Add(15 * time.Second)}, chartPoint{At: pts[28].At.Add(60 * time.Second)})
	if got := gapLimit(pts); got != 2*time.Minute {
		t.Errorf("gap limit %v, want two minutes (4 × 15 s is less)", got)
	}
	runs := seriesRuns(pts, t0, fixedNow)
	want := [][2]int{{0, 20}, {20, 28}, {28, 31}}
	if len(runs) != len(want) {
		t.Fatalf("runs %v, want %v", runs, want)
	}
	for i := range want {
		if runs[i] != want[i] {
			t.Errorf("run %d = %v, want %v", i, runs[i], want[i])
		}
	}
	// A week's history at ten minutes: one line.
	week := pointsEvery(fixedNow.Add(-7*24*time.Hour), 1000, 10*time.Minute)
	if runs := seriesRuns(week, week[0].At, week[999].At); len(runs) != 1 {
		t.Errorf("a week at ten minutes breaks into %d runs", len(runs))
	}
	// Points outside the axis end a run; a series of two never breaks (its step is unknown).
	if runs := seriesRuns(pts, pts[5].At, pts[10].At); len(runs) != 1 || runs[0] != [2]int{5, 11} {
		t.Errorf("runs within the axis = %v", runs)
	}
	two := []chartPoint{{At: t0}, {At: t0.Add(time.Hour)}}
	if runs := seriesRuns(two, t0, fixedNow); len(runs) != 1 {
		t.Errorf("two points far apart: %v", runs)
	}
}

// reopenedPod is the fixture pod's chart as a reopened drawer has it with the Metrics Server alone:
// the minutes measured while the drawer was open before, a gap of twenty minutes, then the
// measurements since it opened again.
func reopenedPod(m *engine.ObjectMetrics) {
	for i := range m.Targets {
		s := m.Targets[i].Samples
		var kept []model.UsageSample
		for j, p := range s {
			if j < 80 || j >= 160 { // drop twenty minutes in the middle
				kept = append(kept, p)
			}
		}
		m.Targets[i].Samples = kept
	}
}

func TestReopenedDrawerChartBreaksAtTheGap(t *testing.T) {
	sh, _, _ := metricsDrawer(t, ThemeDark, reopenedPod)
	p := sh.drawer.kept[metricsKey].(*metricsPanel)
	test.WidgetRenderer(p.plot) // laid out
	var runs []int
	for _, l := range p.plot.rend.line.paths {
		if l.color == themeColor(p.plot.Theme(), colorAccent) {
			runs = append(runs, len(l.pts))
		}
	}
	// 160 points in two runs of 80: two paths, none across the gap.
	if !slices.Equal(runs, []int{80, 80}) {
		t.Errorf("usage paths of %v points, want two runs of 80 (no line across the gap)", runs)
	}
	if !p.note.Visible() || p.note.text == "" {
		t.Errorf("the note on the Metrics Server's history: %q", p.note.text)
	}
}

func TestRenderReopenedDrawer(t *testing.T) {
	for _, c := range []struct {
		mode ThemeMode
		name string
	}{{ThemeDark, "pod_metrics_reopened_dark.png"}, {ThemeLight, "pod_metrics_reopened_light.png"}} {
		t.Run(c.name, func(t *testing.T) {
			sh, _, _ := metricsDrawer(t, c.mode, reopenedPod)
			savePNG(t, sh.win, c.name)
		})
	}
}
