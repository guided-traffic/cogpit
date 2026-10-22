package fyneui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/model"
)

// The chart's hover (G12, "Interactive charts"): crosshair on the nearest point, a dot per series, a
// panel with the exact time and values, and the event near a marker.

var hoverNow = time.Date(2026, 10, 12, 14, 30, 0, 0, time.UTC)

// hoverChart is a 300-wide chart over a minute with four points 20 s apart, a request line and an
// event marker 40 s in.
func hoverChart(t *testing.T) *chart {
	t.Helper()
	test.NewTempApp(t).Settings().SetTheme(newTheme(ThemeDark))
	c := newChart()
	c.Resize(fyne.NewSize(300, c.MinSize().Height))
	c.Set(hoverData(hoverNow, 0))
	return c
}

// hoverData has points at 20 s steps ending at to, shifted by extra points' worth of 0.
func hoverData(to time.Time, shift float64) chartData {
	from := to.Add(-time.Minute)
	pts := make([]chartPoint, 4)
	for i := range pts {
		pts[i] = chartPoint{At: from.Add(time.Duration(i) * 20 * time.Second), Value: 0.1*float64(i+1) + shift}
	}
	return chartData{
		From: from, To: to, Time: model.TimeFormat{Zone: time.UTC}, Format: formatCores, Detail: formatCoresExact,
		Series: []chartSeries{{Name: "CPU usage", Color: colorAccent, Points: pts}},
		Lines: []chartLine{
			{Name: "Requests", Color: colorStatusWarning, Value: 0.25},
			{Name: "Limits", Color: colorStatusError, Value: 0.5},
		},
		Markers: []chartMarker{{
			At: from.Add(40 * time.Second), Name: "Events", Color: colorStatusPending,
			Reason: "BackOff", Count: 3, Message: "Back-off restarting failed container app in pod demo-7d9f_default(1234)",
		}},
	}
}

// at moves the pointer to a time on the axis, at a height inside the plot.
func (c *chart) hoverAtTime(t time.Time, dx float32) {
	c.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(c.geom.x(t)+dx, c.geom.top+c.geom.h/2)}})
}

func TestHoverSnapsToTheNearestPoint(t *testing.T) {
	c := hoverChart(t)
	d := c.data
	// Between the 2nd (20 s) and 3rd (40 s) point, nearer to the 3rd.
	c.hoverAtTime(d.From.Add(34*time.Second), 0)
	h := c.Hover()
	if h == nil {
		t.Fatal("no hover over the plot")
	}
	want := d.From.Add(40 * time.Second)
	if !h.At.Equal(want) || h.X != c.geom.x(want) {
		t.Fatalf("snapped to %v at x %v, want %v at %v", h.At, h.X, want, c.geom.x(want))
	}
	text := h.Text()
	for _, s := range []string{"14:29:40 · 20 s ago", "CPU usage: 300m", "Requests: 250m", "Limits: 500m"} {
		if !strings.Contains(text, s) {
			t.Errorf("panel lacks %q:\n%s", s, text)
		}
	}
	// And nearer to the 2nd point.
	c.hoverAtTime(d.From.Add(26*time.Second), 0)
	if h := c.Hover(); h == nil || !h.At.Equal(d.From.Add(20*time.Second)) || !strings.Contains(h.Text(), "CPU usage: 200m") {
		t.Errorf("26 s must snap to the 20 s point: %v", h)
	}
	// The crosshair, a dot per series and the panel are drawn above the chart's own objects.
	var circles, lines int
	for _, o := range c.rend.overlay {
		switch o.(type) {
		case *canvas.Circle:
			circles++
		case *canvas.Line:
			lines++
		}
	}
	if circles != 1 || lines == 0 {
		t.Errorf("overlay has %d dots and %d lines; want one dot per series and a crosshair", circles, lines)
	}
}

func TestHoverNamesTheEventNearAMarker(t *testing.T) {
	c := hoverChart(t)
	at := c.data.Markers[0].At
	c.hoverAtTime(at, 3)
	h := c.Hover()
	if h == nil || len(h.Events) != 1 {
		t.Fatalf("hover = %+v, want one event", h)
	}
	e := h.Events[0]
	if !strings.Contains(e.Title, "BackOff ×3") || !strings.Contains(e.Title, "14:29:40") {
		t.Errorf("event title = %q", e.Title)
	}
	if !strings.HasSuffix(e.Message, "…") || len([]rune(e.Message)) > chartMessageRunes || !strings.HasPrefix(e.Message, "Back-off restarting") {
		t.Errorf("message must be shortened: %q", e.Message)
	}
	c.hoverAtTime(at, 40) // far from the marker
	if h := c.Hover(); h == nil || len(h.Events) != 0 {
		t.Errorf("an event far from the pointer must not be named: %+v", h)
	}
}

func TestHoverOutHidesAndDataRefreshKeepsTheCrosshair(t *testing.T) {
	c := hoverChart(t)
	c.hoverAtTime(c.data.From.Add(30*time.Second), 0)
	before, px := c.Hover().Text(), c.pointer.X
	// A new poll: the axis moves 15 s on and a point is added; the pointer has not moved.
	c.Set(hoverData(hoverNow.Add(15*time.Second), 0.05))
	h := c.Hover()
	if h == nil || c.pointer.X != px {
		t.Fatal("the crosshair must survive a data refresh")
	}
	if h.Text() == before || !strings.Contains(h.Text(), "CPU usage: 250m") {
		t.Errorf("the panel must show the new data under the same pointer x:\n%s\nwas:\n%s", h.Text(), before)
	}
	if !strings.Contains(h.Text(), "CPU usage: ") || len(c.rend.overlay) == 0 {
		t.Errorf("panel after the refresh: %q", h.Text())
	}
	c.MouseOut()
	if c.Hover() != nil || len(c.rend.overlay) != 0 {
		t.Error("MouseOut must hide the crosshair and the panel")
	}
	c.Set(hoverData(hoverNow.Add(30*time.Second), 0.1))
	if c.Hover() != nil {
		t.Error("a refresh after MouseOut must not bring the hover back")
	}
}

func TestHoverNoDataNoPanelAndClicksPassThrough(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(newTheme(ThemeDark))
	c := newChart()
	c.Resize(fyne.NewSize(300, c.MinSize().Height))
	c.Set(chartData{From: hoverNow.Add(-time.Hour), To: hoverNow, Format: formatCores})
	c.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(150, 60)}})
	if c.Hover() != nil {
		t.Error("a chart without data must show no panel")
	}
	// Outside the plot (the value labels' column, the time labels' row) there is no panel either.
	d := hoverData(hoverNow, 0)
	c.Set(d)
	c.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(2, 40)}})
	if c.Hover() != nil {
		t.Error("the value labels' column is not the plot")
	}
	c.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(150, c.geom.top+c.geom.h+4)}})
	if c.Hover() != nil {
		t.Error("below the plot there is no panel")
	}
	// Only hovering: the chart takes neither taps nor scrolls, so the drawer keeps both.
	var w fyne.Widget = c
	if _, ok := w.(fyne.Tappable); ok {
		t.Error("the chart must not take taps")
	}
	if _, ok := w.(fyne.Scrollable); ok {
		t.Error("the chart must not take scrolls")
	}
	if _, ok := w.(desktop.Mouseable); ok {
		t.Error("the chart must not take mouse buttons")
	}
}

func TestHoverPanelStaysInsideTheChart(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(newTheme(ThemeDark))
	c := newChart()
	c.Resize(fyne.NewSize(chartMinWidth+200, c.MinSize().Height))
	c.Set(hoverData(hoverNow, 0))
	for _, f := range []time.Duration{0, 30 * time.Second, 60 * time.Second} {
		c.hoverAtTime(c.data.From.Add(f), 0)
		for _, o := range c.rend.overlay {
			if r, ok := o.(*canvas.Rectangle); ok && r.StrokeWidth > 0 { // the panel's background
				if r.Position().X < 0 || r.Position().X+r.Size().Width > c.Size().Width+0.5 {
					t.Errorf("at %v the panel lies at %v..%v of %v", f, r.Position().X, r.Position().X+r.Size().Width, c.Size().Width)
				}
			}
		}
	}
}

func TestAgoText(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{{0, "now"}, {30 * time.Second, "30 s ago"}, {60 * time.Second, "1 min ago"}, {125 * time.Second, "2 min 5 s ago"}, {3720 * time.Second, "1 h 2 min ago"}} {
		if got := agoText(tc.d); got != tc.want {
			t.Errorf("agoText(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
	for _, tc := range []struct {
		v    float64
		want string
	}{{0.25, "250m"}, {0.0123, "12m"}, {1, "1"}, {1.2504, "1.25"}} {
		if got := formatCoresExact(tc.v); got != tc.want {
			t.Errorf("formatCoresExact(%v) = %q, want %q", tc.v, got, tc.want)
		}
	}
	// Bytes in the hover panel: binary units with one decimal (D-32).
	_, detail := variantFormats(variantMemory, "")
	for _, tc := range []struct {
		v    float64
		want string
	}{{512, "512 B"}, {192 << 20, "192.0 MiB"}, {200_000_000, "190.7 MiB"}, {1.5 * (1 << 30), "1.5 GiB"}} {
		if got := detail(tc.v); got != tc.want {
			t.Errorf("memory hover(%v) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

// In the drawer, every chart hovers with its own data and the variant's units: the pod's, a
// container's compact one, CPU and Memory.
func TestDrawerChartsHoverPerChartAndVariant(t *testing.T) {
	sh, _, _ := metricsDrawerFor(t, twoContainerPod(t), ThemeDark, withEnvoy)
	p := sh.drawer.kept[metricsKey].(*metricsPanel)
	middle := func(c *chart) *chartHover {
		t.Helper()
		g := c.geom
		c.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(g.left+g.w/2, g.top+g.h/2)}})
		return c.Hover()
	}
	h := middle(p.plot)
	if h == nil {
		t.Fatal("no hover on the pod's chart")
	}
	for _, s := range []string{"CPU usage: ", "Requests: 250m", "Limits: 500m", " s ago"} {
		if !strings.Contains(h.Text(), s) && !strings.Contains(h.Text(), strings.Replace(s, " s ago", " min", 1)) {
			t.Errorf("pod chart panel lacks %q:\n%s", s, h.Text())
		}
	}
	p.variants[variantMemory].OnTapped()
	if h := middle(p.plot); h == nil || !strings.Contains(h.Text(), "Memory usage: ") || !strings.Contains(h.Text(), "Requests: 192.0 MiB") || !strings.Contains(h.Text(), "Limits: 384.0 MiB") {
		t.Errorf("memory panel: %v", h.Text())
	}
	app := p.perBox["app"]
	if h := middle(app.plot); h == nil || !strings.Contains(h.Text(), "Requests: 200m") {
		t.Errorf("container chart panel: %v", h.Text())
	}
	// A new poll keeps the hovering pointer's panel.
	p.apply(p.data, nil)
	if p.plot.Hover() == nil {
		t.Error("a poll must keep the pod chart's panel")
	}
	// Hidden (no metrics) clears it.
	p.plot.Hide()
	if p.plot.Hover() != nil {
		t.Error("a hidden chart keeps no panel")
	}
}

// TestRenderChartHover draws the pod drawer's charts with the crosshair and the panel, once over a
// point and once next to an event marker, for a look: to COGPIT_PNG_DIR when that is set (else it
// is only captured), never to testdata.
func TestRenderChartHover(t *testing.T) {
	for _, c := range []struct {
		mode   ThemeMode
		name   string
		width  float32
		event  bool
		memory bool // the Memory variant: bytes on the axis and in the panel (D-32)
	}{
		{ThemeDark, "chart_hover_dark.png", 0, false, false}, {ThemeLight, "chart_hover_light.png", 0, false, false},
		{ThemeDark, "chart_hover_event_dark.png", 0, true, false}, {ThemeLight, "chart_hover_event_light.png", 0, true, false},
		{ThemeDark, "chart_hover_narrow_dark.png", minDrawerWidth, false, false}, {ThemeLight, "chart_hover_narrow_event_light.png", minDrawerWidth, true, false},
		{ThemeDark, "chart_hover_memory_dark.png", 0, false, true}, {ThemeLight, "chart_hover_memory_light.png", 0, false, true},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			sh, _, q := metricsDrawerFor(t, twoContainerPod(t), c.mode, withEnvoy)
			if c.width > 0 {
				sh.setDrawerWidth(c.width)
				q.until(t, "the drawer to take the width", func() bool { return sh.drawer.width == c.width })
				settle(sh.win)
			}
			p := sh.drawer.kept[metricsKey].(*metricsPanel)
			p.flag.OnTapped()
			if c.memory {
				p.variants[variantMemory].OnTapped()
			}
			settle(sh.win)
			g := p.plot.geom
			pos := fyne.NewPos(g.left+g.w*0.62, g.top+g.h*0.5)
			if c.event {
				if len(p.plot.data.Markers) == 0 {
					t.Fatal("the fixture has no events")
				}
				pos = fyne.NewPos(g.x(p.plot.data.Markers[0].At)+1, g.top+g.h*0.5)
			}
			p.plot.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: pos}})
			if p.plot.Hover() == nil || (c.event && len(p.plot.Hover().Events) == 0) {
				t.Fatalf("hover = %v", p.plot.Hover())
			}
			if c.memory && !strings.Contains(p.plot.Hover().Text(), "Requests: 192.0 MiB") {
				t.Errorf("memory hover: %s", p.plot.Hover().Text())
			}
			settle(sh.win)
			lookPNG(t, sh.win, c.name)
		})
	}
}
