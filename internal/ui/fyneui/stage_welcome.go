package fyneui

import (
	"image"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Pieces of the Dark Cockpit's welcome page and About dialog (docs/developer/design-language.md):
// the logo with its soft glow, centred wrapped text, and the raised plate a cluster's row stands on.

// logoGlow is the logo on a soft radial glow of the glow blue, the glow of the logo's own C carried
// out of its tile: a radial gradient from about 18 % to nothing, wider than the logo and centred
// behind it. The widget is taller than the logo by the glow's reach, so that no text lies on it.
type logoGlow struct {
	widget.BaseWidget
	logo *welcomeLogo
	side float32
}

func newLogoGlow(side float32) *logoGlow {
	g := &logoGlow{logo: newWelcomeLogo(side), side: side}
	g.ExtendBaseWidget(g)
	return g
}

// CreateRenderer implements fyne.Widget.
func (g *logoGlow) CreateRenderer() fyne.WidgetRenderer {
	r := &logoGlowRenderer{g: g, glow: canvas.NewRadialGradient(color.Transparent, color.Transparent)}
	r.Refresh()
	return r
}

type logoGlowRenderer struct {
	g    *logoGlow
	glow *canvas.RadialGradient
}

// logoGlowReach is how far the glow reaches beyond the logo's edge, as a share of the logo's side.
// The widget asks for room for the logo and a margin only; the glow's faint end lies over the empty
// space around it, so that the page keeps its height and no text meets more than a few percent of it.
const logoGlowReach = 0.45

func (r *logoGlowRenderer) MinSize() fyne.Size {
	s := r.g.side
	return fyne.NewSize(s*(1+2*logoGlowReach), s*1.3)
}

func (r *logoGlowRenderer) Layout(size fyne.Size) {
	s := r.g.side
	d := s * (1 + 2*logoGlowReach)
	r.glow.Move(fyne.NewPos((size.Width-d)/2, (size.Height-d)/2)) // overflows above and below the widget
	r.glow.Resize(fyne.NewSize(d, d))
	r.g.logo.Move(fyne.NewPos((size.Width-s)/2, (size.Height-s)/2))
	r.g.logo.Resize(fyne.NewSize(s, s))
}

func (r *logoGlowRenderer) Refresh() {
	c := themeColor(r.g.Theme(), theme.ColorNamePrimary)
	r.glow.StartColor, r.glow.EndColor = withAlpha(c, 0x2e), withAlpha(c, 0) // about 18 % to 0
	r.Layout(r.g.Size())
	r.glow.Refresh()
	r.g.logo.Refresh()
}

func (r *logoGlowRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.glow, r.g.logo}
}
func (r *logoGlowRenderer) Destroy() {}

// centeredText is a wrapped text whose lines are centred on the widget: a tagline under a title.
type centeredText struct {
	widget.BaseWidget
	text  string
	color fyne.ThemeColorName
	size  fyne.ThemeSizeName
	style fyne.TextStyle
	width float32 // the wrap width and the minimum width
}

func newCenteredText(text string, c fyne.ThemeColorName, size fyne.ThemeSizeName, width float32) *centeredText {
	t := &centeredText{text: text, color: c, size: size, width: width}
	t.ExtendBaseWidget(t)
	return t
}

// CreateRenderer implements fyne.Widget.
func (t *centeredText) CreateRenderer() fyne.WidgetRenderer {
	r := &centeredTextRenderer{t: t}
	r.Refresh()
	return r
}

type centeredTextRenderer struct {
	t     *centeredText
	lines []*canvas.Text
}

func (r *centeredTextRenderer) lineHeight() float32 {
	th := r.t.Theme()
	return fyne.MeasureText("Mg", th.Size(r.t.size), r.t.style).Height + th.Size(theme.SizeNameLineSpacing)
}

func (r *centeredTextRenderer) MinSize() fyne.Size {
	return fyne.NewSize(r.t.width, float32(len(r.lines))*r.lineHeight())
}

func (r *centeredTextRenderer) Layout(size fyne.Size) {
	h := r.lineHeight()
	for i, l := range r.lines {
		ms := l.MinSize()
		l.Move(fyne.NewPos((size.Width-ms.Width)/2, float32(i)*h))
		l.Resize(ms)
	}
}

func (r *centeredTextRenderer) Refresh() {
	th := r.t.Theme()
	size, c := th.Size(r.t.size), themeColor(th, r.t.color)
	r.lines = r.lines[:0]
	for _, s := range wrapLines(r.t.text, r.t.width, size, r.t.style) {
		o := canvas.NewText(s, c)
		o.TextSize, o.TextStyle = size, r.t.style
		r.lines = append(r.lines, o)
	}
	r.Layout(r.t.Size())
	canvas.Refresh(r.t)
}

func (r *centeredTextRenderer) Objects() []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, len(r.lines))
	for i, l := range r.lines {
		objs[i] = l
	}
	return objs
}
func (r *centeredTextRenderer) Destroy() {}

// plateOf puts content on a raised plate: the drawer's surface, a step above the stage, with a
// hairline edge and the cards' radius, so that a list of clusters reads as a row of objects and not
// as text on the page. The content keeps its own events.
func plateOf(content fyne.CanvasObject) fyne.CanvasObject {
	bg := newSurface(colorDrawer)
	bg.radius = theme.SizeNameDialogRadius
	bg.stroke = colorPlateEdge
	return container.NewStack(bg, inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, content))
}

// clippedHorizon is the horizon cut to its own bounds: the grid's rays run past the page's edges
// to converge, and the navigator beside the page must not show them. floor is the grid floor's
// share of the height: the welcome page's full-height stage takes the default (0), a small panel
// (About) a low floor, so that no line runs behind its text.
func clippedHorizon(floor float32) fyne.CanvasObject {
	h := newHorizon()
	if floor > 0 {
		h.floor = floor
	}
	c := container.NewClip(h)
	c.MinSize() // NewClip extends its base widget at the first MinSize (navigator_slide.go)
	return c
}

// welcomeStage is the welcome page's backdrop and its content in one: the horizon (the stage light
// at the top, the grid floor at the bottom) behind a scroll holding the page. The floor must never
// run behind text (design-language.md: no line under a word), so a scrim in the stage's own colour
// lies over it behind the page's column only, from just above where the floor starts to just under
// the content's last line, with soft edges all round: the grid shows beside the column all the
// time, and under it in the free band below the content. The scrim follows the scroll offset and
// the page's height (relayout, which the page calls after it renders).
//
// The owner, 2026-10-21, on the first build (a scrim the window's width, worked out from a stale
// height until the first scroll): the grid seen at a fresh start "sehr gut gemacht", but gone after
// the slightest scroll until a restart. The column's scrim keeps that look and the rule.
type welcomeStage struct {
	horizon fyne.CanvasObject
	scrim   *canvas.Raster
	scroll  *container.Scroll
	page    fyne.CanvasObject // the content the scroll holds, whose height says where the last line is
	box     *fyne.Container   // the stage's container, laid out again by relayout

	// What the scrim's image was drawn for, so that a layout that moves nothing draws nothing.
	drawn    fyne.Size
	fadeDown bool // the scrim ends above the window's bottom and fades out there
	stage    color.NRGBA
}

const (
	welcomeScrimEdge   = 28 // the height (and width) of the scrim's soft edges, in points
	welcomeScrimMargin = 12 // the scrim's solid part beyond the column on each side, in points
)

func newWelcomeStage(page fyne.CanvasObject) *fyne.Container {
	s := &welcomeStage{horizon: clippedHorizon(0), page: page, scroll: container.NewVScroll(page)}
	s.scrim = canvas.NewRaster(s.drawScrim)
	s.scrim.ScaleMode = canvas.ImageScaleSmooth
	s.box = container.New(s, s.horizon, s.scrim, s.scroll)
	s.scroll.OnScrolled = func(fyne.Position) { s.relayout() }
	return s.box
}

// relayout places the scrim again: after a scroll, and after the page changed its height.
func (s *welcomeStage) relayout() {
	if s != nil {
		s.Layout(s.box.Objects, s.box.Size())
	}
}

// welcomeStageOf returns the stage a welcome page's root is, or nil.
func welcomeStageOf(root *fyne.Container) *welcomeStage {
	s, _ := root.Layout.(*welcomeStage)
	return s
}

func (s *welcomeStage) MinSize(objs []fyne.CanvasObject) fyne.Size { return s.scroll.MinSize() }

func (s *welcomeStage) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	s.horizon.Move(fyne.Position{})
	s.horizon.Resize(size)
	s.scroll.Move(fyne.Position{})
	s.scroll.Resize(size)
	floorTop := size.Height * (1 - newHorizon().floor)
	from := floorTop - welcomeScrimEdge
	to := max(s.page.MinSize().Height-s.scroll.Offset.Y, 0) + 10 // just under the last line
	if to <= from {
		s.scrim.Hide()
		return
	}
	// The column as welcomeLayout places it, and the scrim's solid margin and soft edge beside it.
	pad := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameInnerPadding)
	col := min(float32(welcomeColumn), size.Width-2*pad)
	x := max((size.Width-col)/2-welcomeScrimMargin-welcomeScrimEdge, 0)
	w := min(col+2*(welcomeScrimMargin+welcomeScrimEdge), size.Width)
	bottom := min(to+welcomeScrimEdge, size.Height)
	stage := color.NRGBAModel.Convert(themeColor(fyne.CurrentApp().Settings().Theme(), theme.ColorNameBackground)).(color.NRGBA)
	box, fadeDown := fyne.NewSize(w, bottom-from), to+welcomeScrimEdge <= size.Height
	s.scrim.Move(fyne.NewPos(x, from))
	s.scrim.Resize(box)
	s.scrim.Show()
	if box != s.drawn || fadeDown != s.fadeDown || stage != s.stage {
		s.drawn, s.fadeDown, s.stage = box, fadeDown, stage
		s.scrim.Refresh()
	}
}

// drawScrim draws the scrim: the stage's colour, opaque inside, fading to nothing over
// welcomeScrimEdge at the left, right and top, and at the bottom when the scrim ends above the
// window's. It draws a quarter of the pixels; the raster's smooth scaling makes up the rest, which
// a fade this soft does not show, and keeps a scroll cheap.
func (s *welcomeStage) drawScrim(pw, ph int) image.Image {
	w, h := max(pw/4, 1), max(ph/4, 1)
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	if s.drawn.Width <= 0 || s.drawn.Height <= 0 {
		return img
	}
	ex := float32(w) * welcomeScrimEdge / s.drawn.Width  // the soft edge's width in this image's pixels
	ey := float32(h) * welcomeScrimEdge / s.drawn.Height // and its height
	ramp := func(d, e float32) float32 {                 // 0 at the edge, 1 a full edge inside it
		if e <= 0 || d >= e {
			return 1
		}
		return max(d/e, 0)
	}
	fx := make([]float32, w)
	for i := range fx {
		c := float32(i) + 0.5
		fx[i] = ramp(min(c, float32(w)-c), ex)
	}
	for j := range h {
		c := float32(j) + 0.5
		fy := ramp(c, ey)
		if s.fadeDown {
			fy = min(fy, ramp(float32(h)-c, ey))
		}
		row := img.Pix[j*img.Stride:]
		for i, f := range fx {
			a := fy * f
			row[4*i], row[4*i+1], row[4*i+2] = s.stage.R, s.stage.G, s.stage.B
			row[4*i+3] = uint8(float32(s.stage.A) * a * a * (3 - 2*a)) // smoothstep: no visible band where the fade begins
		}
	}
	return img
}
