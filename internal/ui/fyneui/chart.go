package fyneui

import (
	"fmt"
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/model"
)

// chart is a small line chart for the drawer's Metrics section (architecture §9): time on the
// horizontal axis, a value axis from zero, lines for series (usage), dashed horizontal lines for
// fixed values (requests, limits, allocatable) and vertical markers with a diamond for moments
// (events). It draws what it is given and knows nothing of where the numbers came from, so the
// Prometheus provider's series will use it as they are.
//
// The grid, the reference lines and the markers are canvas lines and text in theme colours, which
// the renderer rebuilds on every Refresh and Layout; the series' lines and the areas under them are
// two raster layers the renderer keeps (chartpath.go, D-89), drawn as continuous paths at the
// screen's pixel size.
type chart struct {
	widget.BaseWidget
	data chartData

	// The hover (chart_hover.go): the pointer while it is over the chart, the plot's geometry from
	// the last layout, the panel computed from both, and the renderer that draws it.
	hovering bool
	pointer  fyne.Position
	geom     chartGeom
	hover    *chartHover
	rend     *chartRenderer
}

// chartData is what a chart draws.
type chartData struct {
	// From and To are the time axis. A series' points outside it are not drawn.
	From, To time.Time
	// Time writes the axis labels' and the hover panel's times: the user's zone and format (D-31,
	// shell.timeFormat); the zero value writes ISO 8601 in UTC.
	Time model.TimeFormat
	// Series are the lines of measured values over time.
	Series []chartSeries
	// Lines are horizontal lines at a fixed value; Markers are vertical lines at a moment.
	Lines   []chartLine
	Markers []chartMarker
	// Format formats a value for the axis; nil prints the number.
	Format func(float64) string
	// Detail formats a value for the hover panel, more exactly than the axis; nil uses Format.
	Detail func(float64) string
	// Binary says the values are bytes (or bytes per second): the value axis steps at round
	// amounts of KiB, MiB or GiB (25.0 KiB, 50.0 KiB), not of decimal bytes (24.4 KiB, 48.8 KiB).
	Binary bool
	// Empty is the text shown in place of the plot while there is no point to draw.
	Empty string
	// Compact gives a smaller plot and leaves out the legend unless the chart has several series
	// (receive and transmit), which only the legend tells apart (a container's chart under its
	// section title).
	Compact bool
}

type chartSeries struct {
	Name   string
	Color  fyne.ThemeColorName
	Points []chartPoint
}

type chartPoint struct {
	At    time.Time
	Value float64
}

type chartLine struct {
	Name  string
	Color fyne.ThemeColorName
	Value float64
	Soft  bool // a ceiling that frames the chart (allocatable, capacity): drawn fainter than requests and limits
}

type chartMarker struct {
	At    time.Time
	Name  string // the kind of moment, for the legend ("Events")
	Color fyne.ThemeColorName
	// Reason, Message and Count say what happened; the hover panel names them.
	Reason, Message string
	Count           int64
}

// Chart geometry, in Fyne units.
const (
	chartPlotHeight        = 120   // the plot's height
	chartCompactHeight     = 72    // a compact chart's plot
	chartMinWidth          = 120   // the narrowest the chart asks for; the drawer's width is the user's
	chartLineWidth         = 1.75  // a series' line (a path, chartpath.go)
	chartGlowWidth         = 6     // the faint wide stroke under it
	chartRefWidth          = 1     // requests and limits
	chartDash, chartGap    = 6, 4  // a reference line's dashes
	chartDiamond           = 3.5   // half the diamond of an event marker
	chartGridLines         = 3     // the grid's horizontal lines above the axis
	chartPointRadius       = 2.5   // a single point's dot
	chartLegendSwatch      = 8     // the legend's colour square
	chartMaxSegmentsPerSer = 2_000 // never draw more than this per series
)

func newChart() *chart {
	c := &chart{}
	c.ExtendBaseWidget(c)
	return c
}

// Set replaces the chart's data and redraws it.
func (c *chart) Set(d chartData) {
	c.data = d
	c.Refresh()
}

// CreateRenderer implements fyne.Widget.
func (c *chart) CreateRenderer() fyne.WidgetRenderer {
	r := &chartRenderer{c: c, fill: newChartLayer(false, 0), glow: newChartLayer(true, chartGlowWidth), line: newChartLayer(true, chartLineWidth)}
	r.fill.fade = true
	c.rend = r
	r.Refresh()
	return r
}

type chartRenderer struct {
	c                *chart
	fill, glow, line *chartLayer // the areas under the series, their glow, and the series' lines (chartpath.go)
	objs             []fyne.CanvasObject
	overlay          []fyne.CanvasObject // the hover's crosshair, dots and panel, above the rest
}

func (r *chartRenderer) Objects() []fyne.CanvasObject {
	if len(r.overlay) == 0 {
		return r.objs
	}
	return append(r.objs[:len(r.objs):len(r.objs)], r.overlay...)
}
func (r *chartRenderer) Destroy() {}

func (r *chartRenderer) Refresh() {
	r.Layout(r.c.Size())
	canvas.Refresh(r.c)
}

// metrics are the sizes the layout works from.
type chartMetrics struct {
	pad, text float32
	textH     float32
}

func (r *chartRenderer) sizes() chartMetrics {
	th := r.c.Theme()
	text := th.Size(theme.SizeNameCaptionText)
	return chartMetrics{
		pad: th.Size(theme.SizeNamePadding), text: text,
		textH: fyne.MeasureText("Mg", text, fyne.TextStyle{}).Height,
	}
}

func (r *chartRenderer) plotHeight() float32 {
	if r.c.data.Compact {
		return chartCompactHeight
	}
	return chartPlotHeight
}

// legend reports whether the chart draws a legend: a full chart with anything to name, a compact one
// only to tell several series apart.
func (d chartData) legend() bool {
	if d.Compact {
		return len(d.Series) > 1
	}
	return len(d.Series)+len(d.Lines)+len(d.Markers) > 0
}

func (r *chartRenderer) legendHeight() float32 {
	if !r.c.data.legend() {
		return 0
	}
	m := r.sizes()
	return m.textH + m.pad
}

func (r *chartRenderer) MinSize() fyne.Size {
	m := r.sizes()
	return fyne.NewSize(chartMinWidth, m.pad+r.plotHeight()+m.pad+m.textH+r.legendHeight())
}

// valueMax is the top of the value axis: the largest value drawn, rounded up to a tick.
func (d chartData) valueMax() float64 {
	top := 0.0
	for _, s := range d.Series {
		for _, p := range s.Points {
			if !p.At.Before(d.From) && !p.At.After(d.To) {
				top = math.Max(top, p.Value)
			}
		}
	}
	for _, l := range d.Lines {
		top = math.Max(top, l.Value)
	}
	if top <= 0 {
		return 1
	}
	return top
}

// niceStep returns a round step (1, 2, 2.5, 5 times a power of ten) so that about n steps cover top.
func niceStep(top float64, n int) float64 {
	raw := top / float64(n)
	if raw <= 0 {
		return 1
	}
	exp := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if raw <= m*exp {
			return m * exp
		}
	}
	return 10 * exp
}

// binaryStep is niceStep in the binary unit (B, KiB, MiB, ...) the top of the axis is counted in, so
// that the steps of a byte axis are round in the unit its labels show. A step is at least one
// byte: labels write whole bytes (D-32), and half a byte would label two lines alike.
func binaryStep(top float64, n int) float64 {
	unit := 1.0
	for top/unit >= 1024 {
		unit *= 1024
	}
	return math.Max(niceStep(top/unit, n)*unit, 1)
}

func (d chartData) format(v float64) string {
	if d.Format != nil {
		return d.Format(v)
	}
	return fmt.Sprintf("%g", v)
}

// timeLabel formats an axis time in the user's format: with seconds on a short range, with the day
// (no year) on a range of a day or more, the day alone from a week on.
func (d chartData) timeLabel(t time.Time) string {
	switch span := d.To.Sub(d.From); {
	case span < 10*time.Minute:
		return d.Time.FormatTime(t)
	case span < 24*time.Hour:
		return d.Time.FormatClock(t)
	case span < 7*24*time.Hour:
		return d.Time.FormatDay(t) + " " + d.Time.FormatClock(t)
	}
	return d.Time.FormatDay(t)
}

func (r *chartRenderer) Layout(size fyne.Size) {
	th := r.c.Theme()
	d := r.c.data
	m := r.sizes()
	r.objs = r.objs[:0]
	add := func(o fyne.CanvasObject) { r.objs = append(r.objs, o) }
	col := func(n fyne.ThemeColorName) color.Color {
		if n == "" {
			n = colorAccent
		}
		return themeColor(th, n)
	}
	text := func(s string, c fyne.ThemeColorName) *canvas.Text {
		t := canvas.NewText(s, themeColor(th, c))
		t.TextSize = m.text
		return t
	}

	top := m.pad
	plotH := r.plotHeight()
	legendH := r.legendHeight()
	// The plot's own height grows with the widget when it is given more than it asked for.
	if extra := size.Height - r.MinSize().Height; extra > 0 {
		plotH += extra
	}
	hasData := false
	for _, s := range d.Series {
		hasData = hasData || len(s.Points) > 0
	}
	ymax := d.valueMax()
	step := niceStep(ymax, chartGridLines)
	if d.Binary {
		step = binaryStep(ymax, chartGridLines)
	}
	ticks := int(math.Ceil(ymax / step))
	top2 := step * float64(ticks) // the top of the axis, a whole number of steps
	if top2 <= 0 {
		top2 = 1
	}

	// The value labels' column.
	labelW := float32(0)
	labels := make([]string, 0, ticks+1)
	for i := 0; i <= ticks; i++ {
		l := d.format(step * float64(i))
		labels = append(labels, l)
		labelW = fyne.Max(labelW, fyne.MeasureText(l, m.text, fyne.TextStyle{}).Width)
	}
	left := labelW + m.pad*2
	plotW := fyne.Max(size.Width-left-m.pad, 1)
	span := d.To.Sub(d.From)
	g := chartGeom{left: left, top: top, w: plotW, h: plotH, top2: top2, from: d.From, span: span}
	r.c.geom = g
	y, x := g.y, g.x

	// Plot background and grid.
	bg := canvas.NewRectangle(col(theme.ColorNameInputBackground))
	bg.Move(fyne.NewPos(left, top))
	bg.Resize(fyne.NewSize(plotW, plotH))
	add(bg)
	for i, l := range labels {
		gy := y(step * float64(i))
		line := canvas.NewLine(col(theme.ColorNameSeparator))
		line.StrokeWidth = 1
		line.Position1, line.Position2 = fyne.NewPos(left, gy), fyne.NewPos(left+plotW, gy)
		add(line)
		t := text(l, colorMuted)
		ts := t.MinSize()
		t.Move(fyne.NewPos(left-m.pad-ts.Width, gy-ts.Height/2))
		t.Resize(ts)
		add(t)
	}

	// Time labels: start, middle and end of the axis.
	if span > 0 {
		for i, at := range []time.Time{d.From, d.From.Add(span / 2), d.To} {
			t := text(d.timeLabel(at), colorMuted)
			ts := t.MinSize()
			tx := x(at) - ts.Width/2
			switch i {
			case 0:
				tx = left
			case 2:
				tx = left + plotW - ts.Width
			}
			t.Move(fyne.NewPos(tx, top+plotH+m.pad/2))
			t.Resize(ts)
			add(t)
		}
	}

	if !hasData {
		msg := d.Empty
		if msg == "" {
			msg = "No data yet"
		}
		t := text(msg, colorMuted)
		ts := t.MinSize()
		t.Move(fyne.NewPos(left+(plotW-ts.Width)/2, top+(plotH-ts.Height)/2))
		t.Resize(ts)
		add(t)
	}

	// The series as paths: a line runs through the points within the axis and breaks at a gap
	// (chartgap.go): a drawer closed for a while, both providers away. A point alone is a dot. The
	// area under each run (chartfill.go) lies beneath the markers, the reference lines and the lines.
	lines, glows, fills, dots := r.seriesPaths(g)
	r.fill.place(left, top, left+plotW, top+plotH, 0, y(0), fills)
	r.glow.place(left, top, left+plotW, top+plotH, chartGlowWidth, y(0), glows)
	r.line.place(left, top, left+plotW, top+plotH, 2*chartLineWidth, y(0), lines)
	add(r.fill.raster)
	add(r.glow.raster)

	// Event markers: a vertical line with a diamond at its top.
	for _, mk := range d.Markers {
		if mk.At.Before(d.From) || mk.At.After(d.To) {
			continue
		}
		c := col(fallback(mk.Color, colorStatusPending))
		mx := x(mk.At)
		line := canvas.NewLine(c)
		line.StrokeWidth = 1
		line.Position1, line.Position2 = fyne.NewPos(mx, top), fyne.NewPos(mx, top+plotH)
		add(line)
		cy := top + chartDiamond
		pts := [4]fyne.Position{
			fyne.NewPos(mx, cy-chartDiamond), fyne.NewPos(mx+chartDiamond, cy),
			fyne.NewPos(mx, cy+chartDiamond), fyne.NewPos(mx-chartDiamond, cy),
		}
		for i := range pts {
			e := canvas.NewLine(c)
			e.StrokeWidth = 1.5
			e.Position1, e.Position2 = pts[i], pts[(i+1)%4]
			add(e)
		}
	}

	// Reference lines, dashed.
	for _, l := range d.Lines {
		if l.Value <= 0 {
			continue
		}
		c := col(l.Color)
		if l.Soft {
			c = withAlpha(c, 0x66) // the cluster's ceilings recede behind what is being measured
		} else {
			c = withAlpha(c, 0xb8)
		}
		ly := y(l.Value)
		for dx := float32(0); dx < plotW; dx += chartDash + chartGap {
			seg := canvas.NewLine(c)
			seg.StrokeWidth = chartRefWidth
			seg.Position1 = fyne.NewPos(left+dx, ly)
			seg.Position2 = fyne.NewPos(left+fyne.Min(dx+chartDash, plotW), ly)
			add(seg)
		}
	}

	// Series: the lines' layer, then the dots of points alone.
	add(r.line.raster)
	r.objs = append(r.objs, dots...)

	// Legend: a colour square and a name for each series, line and marker kind.
	if legendH > 0 {
		lx := left
		ly := top + plotH + m.pad/2 + m.textH + m.pad/2
		entry := func(name string, c fyne.ThemeColorName) {
			sw := canvas.NewRectangle(col(c))
			sw.Move(fyne.NewPos(lx, ly+(m.textH-chartLegendSwatch)/2))
			sw.Resize(fyne.NewSize(chartLegendSwatch, chartLegendSwatch))
			add(sw)
			t := text(name, colorMuted)
			ts := t.MinSize()
			t.Move(fyne.NewPos(lx+chartLegendSwatch+m.pad, ly))
			t.Resize(ts)
			add(t)
			lx += chartLegendSwatch + m.pad + ts.Width + 2*m.pad
		}
		for _, s := range d.Series {
			entry(s.Name, s.Color)
		}
		for _, l := range d.Lines {
			if l.Value > 0 && !d.Compact {
				entry(l.Name, l.Color)
			}
		}
		if len(d.Markers) > 0 {
			entry(d.Markers[0].Name, fallback(d.Markers[0].Color, colorStatusPending))
		}
	}

	// A pointer still over the chart after new data or a resize gets its crosshair at the same x.
	r.c.hover = r.c.computeHover()
	r.buildOverlay()
}

func fallback(n, def fyne.ThemeColorName) fyne.ThemeColorName {
	if n == "" {
		return def
	}
	return n
}
