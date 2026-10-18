package fyneui

import (
	"image"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
)

// The area under a series is filled (Lens's charts, captured: "the chart's fill"): a path per run
// of points, from the run's first to its last point and down to zero, in the series' colour at
// chartFillAlpha, in the fill layer beneath the lines' layer; a gap splits it as it splits the
// line, a point alone has none.
func TestChartFillsUnderTheSeries(t *testing.T) {
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(ThemeDark))
	at := time.Date(2026, 10, 16, 21, 0, 0, 0, time.UTC)
	var pts []chartPoint
	for i := range 20 {
		if i >= 8 && i < 14 {
			continue // a gap: two runs
		}
		pts = append(pts, chartPoint{At: at.Add(time.Duration(i) * 30 * time.Second), Value: float64(1 + i%3)})
	}
	pts = append(pts, chartPoint{At: at.Add(30 * time.Minute), Value: 2}) // alone after a long gap: a dot, no fill
	c := newChart()
	c.Set(chartData{From: at, To: at.Add(31 * time.Minute), Series: []chartSeries{{Name: "CPU usage", Color: colorAccent, Points: pts}}})
	w := test.NewTempWindow(t, c)
	w.Resize(fyne.NewSize(400, 200))
	r := c.rend
	if len(r.fill.paths) != 2 || len(r.line.paths) != 2 {
		t.Fatalf("%d fills and %d lines, want one of each per run of points (2)", len(r.fill.paths), len(r.line.paths))
	}
	objs := test.WidgetRenderer(c).Objects()
	fillAt, lineAt, circles := -1, -1, 0
	for i, o := range objs {
		switch o {
		case r.fill.raster:
			fillAt = i
		case r.line.raster:
			lineAt = i
		}
		if _, ok := o.(*canvas.Circle); ok {
			circles++
		}
	}
	if fillAt < 0 || lineAt < 0 || fillAt > lineAt {
		t.Errorf("the fill layer at %d, the lines' at %d: the fill must lie beneath the lines", fillAt, lineAt)
	}
	if circles != 1 {
		t.Errorf("%d dots, want the point alone as one", circles)
	}

	// The fill's image at the layer's size: filled under the line, transparent above it and in the
	// gap, the series' colour at the fill's alpha.
	g, l := c.geom, r.fill
	img := l.draw(int(l.size.Width), int(l.size.Height)).(*image.RGBA)
	px := func(x, y float32) uint8 {
		return img.RGBAAt(int(x-l.origin.X), int(y-l.origin.Y)).A
	}
	mid := pts[4] // 2, between a 1 and a 3; the plot's top is 3
	if a := px(g.x(mid.At), g.y(0)-1); a != chartFillAlpha {
		t.Errorf("alpha %#x under the line, want %#x", a, chartFillAlpha)
	}
	if a := px(g.x(mid.At), g.top+1); a != 0 {
		t.Errorf("filled above the line (alpha %#x)", a)
	}
	gap := at.Add(10*30*time.Second + 15*time.Second)
	if a := px(g.x(gap), g.y(0)-1); a != 0 {
		t.Errorf("filled in the gap (alpha %#x)", a)
	}
}
