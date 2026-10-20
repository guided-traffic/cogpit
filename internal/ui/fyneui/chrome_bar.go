package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// capsule is the title bar's cluster pill in the Dark Cockpit design: the connection's dot and the
// cluster's name on a small plate (a faint 6 % fill and a hairline) with fully rounded ends. It
// reads as one object, "the cluster you are looking at", in a bar that otherwise has only text.
type capsule struct {
	widget.BaseWidget
	content fyne.CanvasObject
}

func newCapsule(content fyne.CanvasObject) *capsule {
	c := &capsule{content: content}
	c.ExtendBaseWidget(c)
	return c
}

// CreateRenderer implements fyne.Widget.
func (c *capsule) CreateRenderer() fyne.WidgetRenderer {
	r := &capsuleRenderer{c: c, bg: canvas.NewRectangle(color.Transparent)}
	r.Refresh()
	return r
}

type capsuleRenderer struct {
	c  *capsule
	bg *canvas.Rectangle
}

func (r *capsuleRenderer) MinSize() fyne.Size {
	m := r.c.content.MinSize()
	return fyne.NewSize(m.Width, max(m.Height, capsuleHeight))
}

// capsuleHeight is the capsule's least height: a line of text with 5 points above and below.
const capsuleHeight = 28

func (r *capsuleRenderer) Layout(size fyne.Size) {
	r.bg.Move(fyne.NewPos(0, 0))
	r.bg.Resize(size)
	r.bg.CornerRadius = size.Height / 2
	r.c.content.Move(fyne.NewPos(0, 0))
	r.c.content.Resize(size)
}

func (r *capsuleRenderer) Refresh() {
	th := r.c.Theme()
	r.bg.FillColor = withAlpha(themeColor(th, theme.ColorNameForeground), 0x0f)
	r.bg.StrokeColor, r.bg.StrokeWidth = themeColor(th, colorBracket), 1
	r.Layout(r.c.Size())
	r.bg.Refresh()
	r.c.content.Refresh()
}

func (r *capsuleRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.c.content}
}
func (r *capsuleRenderer) Destroy() {}

// pulseSamples is how many recent event rates the status bar's pulse draws.
const pulseSamples = 24

// pulse is the status bar's live line: a small cyan wave beside the event rate, drawn from the
// recent rates the bar has seen, or as a gentle sine until there are two. It is one raster that is
// redrawn when a sample arrives, not an animation, so it also stands still under Reduce motion.
type pulse struct {
	w       *wave
	samples []float64
}

func newPulse() *pulse {
	p := &pulse{w: newWave(colorAccent, 1.5, 0.45)}
	p.w.min = fyne.NewSize(80, 14)
	return p
}

// add records a sample and redraws the wave.
func (p *pulse) add(v float64) {
	p.samples = append(p.samples, v)
	if n := len(p.samples); n > pulseSamples {
		p.samples = append(p.samples[:0], p.samples[n-pulseSamples:]...)
	}
	lo, hi := p.samples[0], p.samples[0]
	for _, v := range p.samples {
		lo, hi = min(lo, v), max(hi, v)
	}
	if len(p.samples) < 2 || lo == hi { // nothing moved: a gentle sine, not a dead flat line
		p.w.SetValues(nil)
		return
	}
	p.w.SetValues(p.samples)
}
