package fyneui

import (
	"fmt"
	"image/color"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
)

// Hovering a chart (G12, "Interactive charts"): while the pointer is over the plot, a vertical
// crosshair stands on the data point nearest to it, a dot sits on each series at that point and a
// small panel inside the chart lists the exact time, every series' value, the dashed lines' values
// and, when the pointer is within a few pixels of an event marker, the event. The panel is part of
// the chart (not the shell's delayed tooltip): it follows the pointer without a delay, needs no
// shell, and it flips to the crosshair's other side before it would leave the chart, so it stays
// readable at the drawer's narrowest width.
//
// The chart implements only desktop.Hoverable, so clicks and scrolling pass through it. The pointer's
// position is kept across Set: a new poll recomputes the crosshair at the same x without a flicker,
// because only the overlay's objects are replaced, never the chart's own.

const (
	chartEventHit      = 5  // how near (in units) the pointer must be to a marker to name its event
	chartEventsShown   = 3  // events named in one panel; the rest is counted
	chartMessageRunes  = 44 // an event's message is cut to this many characters
	chartHoverRadius   = 3.5
	chartHoverGap      = 8 // between the crosshair and the panel
	chartHoverSwatch   = 7
	chartHoverRowSpace = 1
)

// chartGeom is where the plot lies in the chart's own coordinates; the layout sets it.
type chartGeom struct {
	left, top, w, h float32
	top2            float64 // the value at the plot's top edge
	from            time.Time
	span            time.Duration
}

func (g chartGeom) x(t time.Time) float32 {
	if g.span <= 0 {
		return g.left
	}
	return g.left + float32(float64(t.Sub(g.from))/float64(g.span))*g.w
}

func (g chartGeom) y(v float64) float32 { return g.top + g.h - float32(v/g.top2)*g.h }

// chartHover is what the panel says; tests read it.
type chartHover struct {
	X     float32   // the crosshair's x, in the chart's coordinates
	At    time.Time // the snapped data point's time
	Title string    // "15:04:05 · 30 s ago"
	Rows  []chartHoverRow
	// Events are the markers within reach of the pointer.
	Events []chartHoverEvent
	// More counts the events beyond chartEventsShown.
	More int
	dots []chartHoverDot
}

type chartHoverRow struct {
	Name, Value string
	Color       fyne.ThemeColorName
	Dashed      bool // a fixed line (request, limit), not a series
}

type chartHoverEvent struct {
	Title, Message string
	Color          fyne.ThemeColorName
}

type chartHoverDot struct {
	pos   fyne.Position
	color fyne.ThemeColorName
}

// Text is the panel's content as lines.
func (h *chartHover) Text() string {
	if h == nil {
		return ""
	}
	lines := []string{h.Title}
	for _, r := range h.Rows {
		lines = append(lines, r.Name+": "+r.Value)
	}
	for _, e := range h.Events {
		lines = append(lines, e.Title)
		if e.Message != "" {
			lines = append(lines, e.Message)
		}
	}
	if h.More > 0 {
		lines = append(lines, fmt.Sprintf("+%d more events", h.More))
	}
	return strings.Join(lines, "\n")
}

// MouseIn implements desktop.Hoverable.
func (c *chart) MouseIn(ev *desktop.MouseEvent) { c.MouseMoved(ev) }

// MouseMoved implements desktop.Hoverable.
func (c *chart) MouseMoved(ev *desktop.MouseEvent) {
	c.hovering, c.pointer = true, ev.Position
	c.updateHover()
}

// MouseOut implements desktop.Hoverable.
func (c *chart) MouseOut() {
	c.hovering = false
	c.updateHover()
}

// Hide clears the hover: a hidden chart gets no MouseOut.
func (c *chart) Hide() {
	c.hovering = false
	c.hover = nil
	c.BaseWidget.Hide()
}

// Hover is the panel now shown, nil when none.
func (c *chart) Hover() *chartHover { return c.hover }

// updateHover recomputes the overlay for the pointer and repaints only if it changed.
func (c *chart) updateHover() {
	h := c.computeHover()
	if sameHover(h, c.hover) {
		return
	}
	c.hover = h
	if r := c.rend; r != nil {
		old := slices.Clone(r.overlay)
		r.buildOverlay()
		refreshOverlay(c, old, r.overlay)
	}
}

// refreshOverlay repaints the chart's window for a changed overlay without refreshing the chart:
// a refresh of the chart would drop the textures of its line and fill layers (chartpath.go) and
// draw them again at every move of the pointer. The old objects are refreshed so their textures
// go; the new ones so the window paints, and walks the chart's objects, again.
func refreshOverlay(c *chart, old, cur []fyne.CanvasObject) {
	cv := fyne.CurrentApp().Driver().CanvasForObject(c)
	if cv == nil {
		return
	}
	for _, o := range old {
		cv.Refresh(o)
	}
	for _, o := range cur {
		cv.Refresh(o)
	}
}

func sameHover(a, b *chartHover) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.X == b.X && a.Text() == b.Text()
}

// computeHover finds the data point nearest to the pointer's x and what to say about it.
func (c *chart) computeHover() *chartHover {
	g := c.geom
	if !c.hovering || g.w <= 0 || g.span <= 0 || g.top2 <= 0 {
		return nil
	}
	p := c.pointer
	if p.X < g.left || p.X > g.left+g.w || p.Y < g.top || p.Y > g.top+g.h {
		return nil
	}
	d := c.data
	inRange := func(t time.Time) bool { return !t.Before(d.From) && !t.After(d.To) }
	var at time.Time
	found := false
	best := float32(math.MaxFloat32)
	for _, s := range d.Series {
		for _, pt := range s.Points {
			if !inRange(pt.At) {
				continue
			}
			if dx := float32(math.Abs(float64(g.x(pt.At) - p.X))); dx < best {
				best, at, found = dx, pt.At, true
			}
		}
	}
	if !found {
		return nil // no data, no panel
	}
	h := &chartHover{X: g.x(at), At: at, Title: d.hoverTime(at) + " · " + agoText(d.To.Sub(at))}
	for _, s := range d.Series {
		var near *chartPoint
		nd := time.Duration(math.MaxInt64)
		for i := range s.Points {
			if !inRange(s.Points[i].At) {
				continue
			}
			if dt := absDur(s.Points[i].At.Sub(at)); dt < nd {
				nd, near = dt, &s.Points[i]
			}
		}
		if near == nil {
			continue
		}
		h.Rows = append(h.Rows, chartHoverRow{Name: s.Name, Value: d.detail(near.Value), Color: s.Color})
		h.dots = append(h.dots, chartHoverDot{pos: fyne.NewPos(g.x(near.At), g.y(near.Value)), color: s.Color})
	}
	for _, l := range d.Lines {
		if l.Value > 0 {
			h.Rows = append(h.Rows, chartHoverRow{Name: l.Name, Value: d.detail(l.Value), Color: l.Color, Dashed: true})
		}
	}
	var near []chartMarker
	for _, mk := range d.Markers {
		if inRange(mk.At) && math.Abs(float64(g.x(mk.At)-p.X)) <= chartEventHit {
			near = append(near, mk)
		}
	}
	for i, mk := range near {
		if i == chartEventsShown {
			h.More = len(near) - i
			break
		}
		title := mk.Reason
		if title == "" {
			title = mk.Name
		}
		if mk.Count > 1 {
			title += fmt.Sprintf(" ×%d", mk.Count)
		}
		title += " · " + d.hoverTime(mk.At)
		h.Events = append(h.Events, chartHoverEvent{Title: title, Message: cutLine(mk.Message, chartMessageRunes), Color: fallback(mk.Color, colorStatusPending)})
	}
	return h
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// cutLine cuts s to n characters with an ellipsis, on one line.
func cutLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// agoText says how long before the chart's end a moment lies: "30 s ago", "2 min 5 s ago",
// "1 h 2 min ago", "3 d 4 h ago".
func agoText(d time.Duration) string {
	s := int(math.Round(d.Seconds()))
	switch {
	case s <= 0:
		return "now"
	case s < 60:
		return fmt.Sprintf("%d s ago", s)
	case s < 3600:
		if s%60 == 0 {
			return fmt.Sprintf("%d min ago", s/60)
		}
		return fmt.Sprintf("%d min %d s ago", s/60, s%60)
	}
	if s < 86400 {
		return fmt.Sprintf("%d h %d min ago", s/3600, s%3600/60)
	}
	return fmt.Sprintf("%d d %d h ago", s/86400, s%86400/3600)
}

// hoverTime formats a moment for the panel in the user's format: the time of day with seconds,
// with the date before it on a range of a day or more.
func (d chartData) hoverTime(t time.Time) string {
	if d.To.Sub(d.From) >= 24*time.Hour {
		return d.Time.Format(t)
	}
	return d.Time.FormatTime(t)
}

// detail formats a value for the panel: more exact than the axis where the chart says so.
func (d chartData) detail(v float64) string {
	if d.Detail != nil {
		return d.Detail(v)
	}
	return d.format(v)
}

// swatch kinds of a panel row.
const (
	swatchNone = iota
	swatchLine
	swatchDashed
	swatchDiamond
)

// buildOverlay replaces the overlay's objects: the crosshair, the dots and the panel.
func (r *chartRenderer) buildOverlay() {
	r.overlay = r.overlay[:0]
	h := r.c.hover
	if h == nil {
		return
	}
	th := r.c.Theme()
	m := r.sizes()
	g := r.c.geom
	col := func(n fyne.ThemeColorName) color.Color { return themeColor(th, fallback(n, colorAccent)) }
	add := func(o fyne.CanvasObject) { r.overlay = append(r.overlay, o) }

	cross := canvas.NewLine(col(colorMuted))
	cross.StrokeWidth = 1
	cross.Position1, cross.Position2 = fyne.NewPos(h.X, g.top), fyne.NewPos(h.X, g.top+g.h)
	add(cross)
	for _, dot := range h.dots {
		c := canvas.NewCircle(col(dot.color))
		c.StrokeColor, c.StrokeWidth = col(theme.ColorNameInputBackground), 1
		c.Move(dot.pos.SubtractXY(chartHoverRadius, chartHoverRadius))
		c.Resize(fyne.NewSquareSize(2 * chartHoverRadius))
		add(c)
	}

	// The panel: rows of an optional swatch and a text, measured first.
	type row struct {
		text   *canvas.Text
		indent bool
		kind   int
		swatch fyne.ThemeColorName
	}
	var rows []row
	mk := func(s string, c fyne.ThemeColorName, bold, indent bool, kind int, sw fyne.ThemeColorName) {
		t := canvas.NewText(s, themeColor(th, c))
		t.TextSize = m.text
		t.TextStyle.Bold = bold
		rows = append(rows, row{t, indent, kind, sw})
	}
	mk(h.Title, theme.ColorNameForeground, true, false, swatchNone, "")
	for _, rw := range h.Rows {
		kind := swatchLine
		if rw.Dashed {
			kind = swatchDashed
		}
		mk(rw.Name+": "+rw.Value, theme.ColorNameForeground, false, true, kind, rw.Color)
	}
	for _, e := range h.Events {
		mk(e.Title, theme.ColorNameForeground, false, true, swatchDiamond, e.Color)
		if e.Message != "" {
			mk(e.Message, colorMuted, false, true, swatchNone, "")
		}
	}
	if h.More > 0 {
		mk(fmt.Sprintf("+%d more events", h.More), colorMuted, false, true, swatchNone, "")
	}
	pad := m.pad * 1.5
	indent := float32(chartHoverSwatch) + m.pad
	var w, hgt float32
	for _, rw := range rows {
		ms := rw.text.MinSize()
		extra := float32(0)
		if rw.indent {
			extra = indent
		}
		w = fyne.Max(w, ms.Width+extra)
		hgt += ms.Height + chartHoverRowSpace
	}
	w, hgt = w+2*pad, hgt+2*pad-chartHoverRowSpace
	size := r.c.Size()
	px := h.X + chartHoverGap
	if px+w > size.Width {
		px = h.X - chartHoverGap - w
	}
	px = fyne.Max(0, fyne.Min(px, size.Width-w))
	py := fyne.Max(0, fyne.Min(g.top+m.pad, size.Height-hgt))

	bg := canvas.NewRectangle(col(colorTooltip))
	bg.StrokeColor, bg.StrokeWidth = col(theme.ColorNameInputBorder), 1
	bg.CornerRadius = th.Size(theme.SizeNameInputRadius)
	bg.Move(fyne.NewPos(px, py))
	bg.Resize(fyne.NewSize(w, hgt))
	add(bg)
	y := py + pad
	for _, rw := range rows {
		ms := rw.text.MinSize()
		x := px + pad
		sy := y + ms.Height/2
		switch rw.kind {
		case swatchLine, swatchDashed:
			l := canvas.NewLine(col(rw.swatch))
			l.StrokeWidth = 2
			l.Position1, l.Position2 = fyne.NewPos(x, sy), fyne.NewPos(x+chartHoverSwatch, sy)
			add(l)
			if rw.kind == swatchDashed { // a gap in the middle says "dashed"
				gap := canvas.NewRectangle(col(colorTooltip))
				gap.Move(fyne.NewPos(x+chartHoverSwatch/2-0.75, sy-1.5))
				gap.Resize(fyne.NewSize(1.5, 3))
				add(gap)
			}
		case swatchDiamond:
			half := float32(chartDiamond)
			cx := x + chartHoverSwatch/2
			pts := [4]fyne.Position{fyne.NewPos(cx, sy-half), fyne.NewPos(cx+half, sy), fyne.NewPos(cx, sy+half), fyne.NewPos(cx-half, sy)}
			for i := range pts {
				e := canvas.NewLine(col(rw.swatch))
				e.StrokeWidth = 1.5
				e.Position1, e.Position2 = pts[i], pts[(i+1)%4]
				add(e)
			}
		}
		if rw.indent {
			x += indent
		}
		rw.text.Move(fyne.NewPos(x, y))
		rw.text.Resize(ms)
		add(rw.text)
		y += ms.Height + chartHoverRowSpace
	}
}
