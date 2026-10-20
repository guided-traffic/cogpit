package fyneui

import (
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
	bg.stroke = theme.ColorNameSeparator
	return container.NewStack(bg, inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, content))
}

// clippedHorizon is the horizon cut to its own bounds: the grid's rays run past the page's edges
// to converge, and the navigator beside the page must not show them.
func clippedHorizon() fyne.CanvasObject {
	c := container.NewClip(newHorizon())
	c.MinSize() // NewClip extends its base widget at the first MinSize (navigator_slide.go)
	return c
}
