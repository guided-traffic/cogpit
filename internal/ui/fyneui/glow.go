package fyneui

import (
	"image"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The design prototype's signature shapes (docs/developer/design-language.md, "Dark Cockpit"): the
// bracket under a group's label, the token row, the glow frame, the wave and the horizon. Each is
// drawn from vector canvas objects (rectangles, lines, gradients), so that it costs the GPU and not
// a raster per frame, and reads its colours from the theme in Refresh as the widgets in widgets.go do.

// bracketLabel is a group's head as the reference image draws it: a lowercase label with a middle
// dot ("properties · 14"), in the group's light, above a hairline with an end tick at each side.
// It replaces bold section titles and full-width rules.
type bracketLabel struct {
	widget.BaseWidget
	text  string
	tone  fyne.ThemeColorName // the label's colour: the accent (cluster), colorAgent, or colorMuted
	ticks bool                // end ticks under the line's ends
}

// newBracketLabel returns a bracket head in tone; an empty tone is the muted text colour.
func newBracketLabel(text string, tone fyne.ThemeColorName) *bracketLabel {
	if tone == "" {
		tone = colorMuted
	}
	b := &bracketLabel{text: text, tone: tone, ticks: true}
	b.ExtendBaseWidget(b)
	return b
}

// SetText changes the label.
func (b *bracketLabel) SetText(s string) {
	if s == b.text {
		return
	}
	b.text = s
	b.Refresh()
}

// CreateRenderer implements fyne.Widget.
func (b *bracketLabel) CreateRenderer() fyne.WidgetRenderer {
	r := &bracketRenderer{b: b, text: canvas.NewText("", color.Transparent),
		line: canvas.NewLine(color.Transparent), left: canvas.NewLine(color.Transparent), right: canvas.NewLine(color.Transparent)}
	r.text.TextStyle = fyne.TextStyle{Bold: true}
	r.Refresh()
	return r
}

type bracketRenderer struct {
	b                 *bracketLabel
	text              *canvas.Text
	line, left, right *canvas.Line
}

const (
	bracketTick  = 6  // an end tick's length below the line
	bracketGap   = 8  // between the label and the line
	bracketBelow = 12 // between the line and what the head introduces
)

func (r *bracketRenderer) textSize() float32 { return r.b.Theme().Size(theme.SizeNameCaptionText) + 1 }

func (r *bracketRenderer) MinSize() fyne.Size {
	ts := fyne.MeasureText(r.b.text, r.textSize(), r.text.TextStyle)
	return fyne.NewSize(ts.Width+2*bracketTick, ts.Height+bracketGap+bracketBelow)
}

func (r *bracketRenderer) Layout(size fyne.Size) {
	ts := fyne.MeasureText(r.b.text, r.textSize(), r.text.TextStyle)
	r.text.Move(fyne.NewPos(0, 0))
	r.text.Resize(ts)
	y := ts.Height + bracketGap
	r.line.Position1, r.line.Position2 = fyne.NewPos(0, y), fyne.NewPos(size.Width, y)
	r.left.Position1, r.left.Position2 = fyne.NewPos(0.5, y), fyne.NewPos(0.5, y+bracketTick)
	r.right.Position1, r.right.Position2 = fyne.NewPos(size.Width-0.5, y), fyne.NewPos(size.Width-0.5, y+bracketTick)
}

func (r *bracketRenderer) Refresh() {
	th := r.b.Theme()
	r.text.Text = r.b.text
	r.text.TextSize = r.textSize()
	r.text.Color = themeColor(th, r.b.tone)
	// The group's light at about 55 %: a line that does its job beside its label, not a stray rule.
	line := withAlpha(themeColor(th, r.b.tone), 0x8c)
	for _, l := range []*canvas.Line{r.line, r.left, r.right} {
		l.StrokeColor, l.StrokeWidth = line, 1
		l.Hidden = l != r.line && !r.b.ticks
	}
	r.Layout(r.b.Size())
	canvas.Refresh(r.b)
}

func (r *bracketRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.text, r.line, r.left, r.right}
}
func (r *bracketRenderer) Destroy() {}

// tok is one rounded square of a token row: a container, a pod, a replica, a finding. Weight
// widens it in a bar row (the reference's 6 bars and 3 long bars); in a square row it is ignored.
type tok struct {
	color  fyne.ThemeColorName
	weight float32
	hollow bool // drawn as an outline: an empty slot, a stale value
	quiet  bool // a healthy token, drawn at about a third of its light, so that only what needs a look is bright
}

// tokenRow draws tokens left to right: squares of the row's height, or, with bars set, bars
// sharing the row's width by weight. Fade dims the tokens towards the row's end as the reference's
// cyan row does, so a long row reads as a sequence and not a wall.
type tokenRow struct {
	widget.BaseWidget
	tokens []tok
	side   float32 // a square's side; the row's height
	gap    float32
	bars   bool
	fade   bool
	// fit, when set, spreads a square row over the width it is given: each square is as wide as the
	// width allows, at most fit points (a row of nine nodes fills its bracket instead of huddling at
	// its left), and the row is fit points high.
	fit float32
}

func newTokenRow(side float32, tokens ...tok) *tokenRow {
	t := &tokenRow{tokens: tokens, side: side, gap: max(2, side/4)}
	t.ExtendBaseWidget(t)
	return t
}

// SetTokens replaces the row's tokens.
func (t *tokenRow) SetTokens(tokens []tok) {
	t.tokens = tokens
	t.Refresh()
}

// CreateRenderer implements fyne.Widget.
func (t *tokenRow) CreateRenderer() fyne.WidgetRenderer {
	r := &tokenRowRenderer{t: t}
	r.Refresh()
	return r
}

type tokenRowRenderer struct {
	t     *tokenRow
	rects []*canvas.Rectangle
}

func (r *tokenRowRenderer) MinSize() fyne.Size {
	n := float32(len(r.t.tokens))
	if r.t.fit > 0 {
		return fyne.NewSize(r.t.side, r.t.fit)
	}
	if r.t.bars || n == 0 {
		return fyne.NewSize(r.t.side, r.t.side)
	}
	return fyne.NewSize(n*r.t.side+(n-1)*r.t.gap, r.t.side)
}

func (r *tokenRowRenderer) Layout(size fyne.Size) {
	n := len(r.rects)
	if n == 0 {
		return
	}
	y := (size.Height - r.t.side) / 2
	if !r.t.bars && r.t.fit > 0 {
		side := min(r.t.fit, (size.Width-float32(n-1)*r.t.gap)/float32(n))
		y = (size.Height - side) / 2
		for i, rect := range r.rects {
			rect.CornerRadius = max(1.5, side/4)
			rect.Move(fyne.NewPos(float32(i)*(side+r.t.gap), y))
			rect.Resize(fyne.NewSize(side, side))
		}
		return
	}
	if !r.t.bars {
		for i, rect := range r.rects {
			rect.Move(fyne.NewPos(float32(i)*(r.t.side+r.t.gap), y))
			rect.Resize(fyne.NewSize(r.t.side, r.t.side))
		}
		return
	}
	var total float32
	for _, tk := range r.t.tokens {
		total += max(tk.weight, 0.0001)
	}
	free := size.Width - float32(n-1)*r.t.gap
	x := float32(0)
	for i, rect := range r.rects {
		w := free * max(r.t.tokens[i].weight, 0.0001) / total
		rect.Move(fyne.NewPos(x, y))
		rect.Resize(fyne.NewSize(max(w, 1), r.t.side))
		x += w + r.t.gap
	}
}

func (r *tokenRowRenderer) Refresh() {
	th := r.t.Theme()
	for len(r.rects) < len(r.t.tokens) {
		r.rects = append(r.rects, canvas.NewRectangle(color.Transparent))
	}
	r.rects = r.rects[:len(r.t.tokens)]
	radius := max(1.5, r.t.side/4)
	for i, rect := range r.rects {
		tk := r.t.tokens[i]
		c := themeColor(th, tk.color)
		if r.t.fade && len(r.t.tokens) > 1 {
			// From full to about 60 % along the row.
			a := uint8(255 - 100*float32(i)/float32(len(r.t.tokens)-1))
			c = withAlpha(c, a)
		}
		rect.CornerRadius = radius
		if tk.quiet {
			c = withAlpha(c, 0x6a)
		}
		if tk.hollow {
			rect.FillColor, rect.StrokeColor, rect.StrokeWidth = color.Transparent, c, 1
		} else {
			rect.FillColor, rect.StrokeWidth = c, 0
		}
	}
	r.Layout(r.t.Size())
	canvas.Refresh(r.t)
}

func (r *tokenRowRenderer) Objects() []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, len(r.rects))
	for i, rect := range r.rects {
		objs[i] = rect
	}
	return objs
}
func (r *tokenRowRenderer) Destroy() {}

// glowFrame lays a glow around its content: the logo's glow without blur, as four rounded
// outlines of falling alpha, 0, 1.5, 3.5 and 6.5 points outside the content. It marks what
// matters now (the active hotbar tile, a destructive dialog, the agent's running reply): one per
// region.
type glowFrame struct {
	widget.BaseWidget
	content fyne.CanvasObject
	tone    fyne.ThemeColorName
	radius  float32
	fill    fyne.ThemeColorName // optional plate behind the content
	on      bool
}

func newGlowFrame(content fyne.CanvasObject, tone fyne.ThemeColorName, radius float32) *glowFrame {
	g := &glowFrame{content: content, tone: tone, radius: radius, on: true}
	g.ExtendBaseWidget(g)
	return g
}

// SetGlow switches the glow on or off and changes its light.
func (g *glowFrame) SetGlow(on bool, tone fyne.ThemeColorName) {
	if on == g.on && tone == g.tone {
		return
	}
	g.on, g.tone = on, tone
	g.Refresh()
}

// CreateRenderer implements fyne.Widget.
func (g *glowFrame) CreateRenderer() fyne.WidgetRenderer {
	r := &glowFrameRenderer{g: g, plate: canvas.NewRectangle(color.Transparent)}
	for i := range r.rings {
		r.rings[i] = canvas.NewRectangle(color.Transparent)
	}
	r.Refresh()
	return r
}

type glowFrameRenderer struct {
	g     *glowFrame
	plate *canvas.Rectangle
	rings [4]*canvas.Rectangle
}

// The rings from the inside out: a crisp edge in the full light, then a halo that falls off over
// about seven points, the logo's glow drawn without blur.
var (
	glowAlphas  = [4]uint8{0xe6, 0x5c, 0x2a, 0x10}
	glowOffsets = [4]float32{0, 1.5, 3.5, 6.5}
	glowWidths  = [4]float32{1, 2, 2.5, 3.5}
)

func (r *glowFrameRenderer) MinSize() fyne.Size { return r.g.content.MinSize() }

func (r *glowFrameRenderer) Layout(size fyne.Size) {
	r.g.content.Resize(size)
	r.g.content.Move(fyne.NewPos(0, 0))
	r.plate.Resize(size)
	for i, ring := range r.rings {
		o := glowOffsets[i]
		ring.Move(fyne.NewPos(-o, -o))
		ring.Resize(fyne.NewSize(size.Width+2*o, size.Height+2*o))
		ring.CornerRadius = r.g.radius + o
	}
	r.plate.CornerRadius = r.g.radius
}

func (r *glowFrameRenderer) Refresh() {
	th := r.g.Theme()
	c := themeColor(th, r.g.tone)
	for i, ring := range r.rings {
		ring.FillColor = color.Transparent
		ring.StrokeColor = withAlpha(c, glowAlphas[i])
		ring.StrokeWidth = glowWidths[i]
		ring.Hidden = !r.g.on
	}
	r.plate.FillColor = color.Transparent
	if r.g.fill != "" {
		r.plate.FillColor = themeColor(th, r.g.fill)
	}
	r.Layout(r.g.Size())
	canvas.Refresh(r.g)
	r.g.content.Refresh()
}

func (r *glowFrameRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.rings[3], r.rings[2], r.rings[1], r.plate, r.g.content, r.rings[0]}
}
func (r *glowFrameRenderer) Destroy() {}

// wave is a glowing sine (the agent's thinking). It is drawn twice, a wide faint stroke under a
// crisp one, as the reference's violet wave. Phase moves it; an animation that sets it honours
// Reduce motion at its caller.
type wave struct {
	widget.BaseWidget
	tone      fyne.ThemeColorName
	cycles    float64 // a sine's periods across the width
	amplitude float32 // a sine's height as a share of the widget's, 0 to 1
	phase     float64
	min       fyne.Size
}

func newWave(tone fyne.ThemeColorName, cycles float64, amplitude float32) *wave {
	w := &wave{tone: tone, cycles: cycles, amplitude: amplitude, min: fyne.NewSize(60, 14)}
	w.ExtendBaseWidget(w)
	return w
}

// SetPhase moves the sine; 2π is one period.
func (w *wave) SetPhase(p float64) {
	w.phase = p
	w.Refresh()
}

// CreateRenderer implements fyne.Widget.
func (w *wave) CreateRenderer() fyne.WidgetRenderer {
	r := &waveRenderer{w: w}
	r.img = canvas.NewRaster(r.draw)
	r.img.ScaleMode = canvas.ImageScaleSmooth
	r.Refresh()
	return r
}

// waveRenderer draws the line into a raster at the display's pixel size: one pass over the
// pixels, each lit by its distance to the curve (a crisp core and a wide, faint glow), so that the
// line has no joints. Fyne keeps the raster until the next Refresh.
type waveRenderer struct {
	w   *wave
	img *canvas.Raster
}

func (r *waveRenderer) MinSize() fyne.Size { return r.w.min }

// point is the line's y (0 at the top) at x in [0, 1], for a height h.
func (r *waveRenderer) point(x float64, h float64) float64 {
	mid := h / 2
	return mid - math.Sin(2*math.Pi*r.w.cycles*x+r.w.phase)*mid*float64(r.w.amplitude)
}

func (r *waveRenderer) draw(pw, ph int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, pw, ph))
	if pw < 2 || ph < 2 {
		return img
	}
	c := color.NRGBAModel.Convert(themeColor(r.w.Theme(), r.w.tone)).(color.NRGBA)
	scale := float64(pw) / math.Max(1, float64(r.w.Size().Width))
	core, glow := 0.8*scale, 4.5*scale // the core's half width and the glow's reach, in pixels
	h := float64(ph)
	for x := range pw {
		fx := (float64(x) + 0.5) / float64(pw)
		y := r.point(fx, h)
		dy := (r.point(math.Min(1, fx+1/float64(pw)), h) - r.point(math.Max(0, fx-1/float64(pw)), h)) / 2
		norm := math.Sqrt(1 + dy*dy)
		edge := math.Min(1, math.Min(fx, 1-fx)*10) // the ends fade in and out
		for py := range ph {
			d := math.Abs(float64(py)+0.5-y) / norm
			a := 0.0
			if d < core+1 {
				a = math.Min(1, core+1-d)
			}
			a = math.Max(a, 0.30*math.Exp(-(d*d)/(glow*glow)))
			if a <= 0.004 {
				continue
			}
			img.SetNRGBA(x, py, color.NRGBA{R: c.R, G: c.G, B: c.B, A: uint8(255 * a * edge * float64(c.A) / 255)})
		}
	}
	return img
}

func (r *waveRenderer) Layout(size fyne.Size) { r.img.Resize(size) }

func (r *waveRenderer) Refresh() {
	r.Layout(r.w.Size())
	r.img.Refresh()
}

func (r *waveRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.img} }
func (r *waveRenderer) Destroy()                     {}

// horizon is the stage the reference image stands on: a top light over the stage colour and a
// perspective grid floor that fades towards the horizon. It goes only where there is no data (the
// welcome page, empty states, About, a connect's wait), never behind a list, a drawer or a dialog.
type horizon struct {
	widget.BaseWidget
	floor float32 // the floor's share of the height, from the bottom
}

func newHorizon() *horizon {
	h := &horizon{floor: 0.36}
	h.ExtendBaseWidget(h)
	return h
}

// CreateRenderer implements fyne.Widget.
func (h *horizon) CreateRenderer() fyne.WidgetRenderer {
	r := &horizonRenderer{h: h, bg: canvas.NewRectangle(color.Transparent),
		light: canvas.NewRadialGradient(color.Transparent, color.Transparent),
		fade:  canvas.NewLinearGradient(color.Transparent, color.Transparent, 0)}
	for i := range r.rows {
		r.rows[i] = canvas.NewLine(color.Transparent)
	}
	for i := range r.rays {
		r.rays[i] = canvas.NewLine(color.Transparent)
	}
	r.Refresh()
	return r
}

const (
	horizonRows = 12
	horizonRays = 25
)

type horizonRenderer struct {
	h     *horizon
	bg    *canvas.Rectangle
	light *canvas.RadialGradient
	fade  *canvas.LinearGradient // the floor's far end dissolves into the stage
	rows  [horizonRows]*canvas.Line
	rays  [horizonRays]*canvas.Line
}

func (r *horizonRenderer) MinSize() fyne.Size { return fyne.NewSize(0, 0) }

func (r *horizonRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.light.Move(fyne.NewPos(size.Width*0.1, -size.Height*0.35))
	r.light.Resize(fyne.NewSize(size.Width*0.8, size.Height*1.0))
	top := size.Height * (1 - r.h.floor)
	depth := size.Height - top
	// Rows close in towards the horizon: a row at distance d sits at 1/d.
	for i, l := range r.rows {
		y := top + depth/(1+float32(i)*0.55)
		l.Position1, l.Position2 = fyne.NewPos(0, y), fyne.NewPos(size.Width, y)
	}
	// Rays meet at a vanishing point above the horizon's centre.
	vx, vy := size.Width/2, top-depth*0.6
	for i, l := range r.rays {
		f := float32(i)/float32(horizonRays-1)*2 - 1 // -1 … 1
		bx := vx + f*size.Width*1.6
		// From where the ray crosses the horizon down to the bottom edge.
		t := (top - vy) / (size.Height - vy)
		l.Position1 = fyne.NewPos(vx+(bx-vx)*t, top)
		l.Position2 = fyne.NewPos(bx, size.Height)
	}
	r.fade.Move(fyne.NewPos(0, top-1))
	r.fade.Resize(fyne.NewSize(size.Width, depth*0.55))
}

func (r *horizonRenderer) Refresh() {
	th := r.h.Theme()
	stage := themeColor(th, theme.ColorNameBackground)
	r.bg.FillColor = stage
	r.light.StartColor = themeColor(th, colorStageLight)
	r.light.EndColor = withAlpha(stage, 0)
	grid := themeColor(th, colorGrid)
	for i, l := range r.rows {
		// Nearer rows are brighter.
		a := uint8(float32(color.NRGBAModel.Convert(grid).(color.NRGBA).A) * (1 - float32(i)/horizonRows*0.7))
		l.StrokeColor, l.StrokeWidth = withAlpha(grid, a), 1
	}
	for _, l := range r.rays {
		l.StrokeColor, l.StrokeWidth = grid, 1
	}
	r.fade.StartColor, r.fade.EndColor = stage, withAlpha(stage, 0)
	r.Layout(r.h.Size())
	canvas.Refresh(r.h)
}

func (r *horizonRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{r.bg, r.light}
	for _, l := range r.rays {
		objs = append(objs, l)
	}
	for _, l := range r.rows {
		objs = append(objs, l)
	}
	return append(objs, r.fade)
}
func (r *horizonRenderer) Destroy() {}
