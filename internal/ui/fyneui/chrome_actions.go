package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Quiet actions of the Dark Cockpit look. A solid fill is a loud thing in a calm window: the
// selection bar's Delete wears the destructive colour as a 12 % plate with a hairline and a glyph
// in that colour, the way the destructive dialog's rim does, and the "+" that creates an object is
// a chip with a hairline and a cyan glyph that sits in the dock's strip instead of over the rows.

// Geometry of the plates, in points.
const (
	dangerPlateAlpha = 0x1f // the destructive colour behind the glyph: about 12 %
	plateStroke      = 1
)

// dangerPlate wraps an icon button in a rounded outline in the status error colour. The button
// keeps its own behaviour (tooltip, Disabled for a lapsed connection); the plate follows it.
type dangerPlate struct {
	widget.BaseWidget
	btn  *tipButton
	rect *canvas.Rectangle
}

// newDangerPlate puts btn, which must carry the error-themed glyph, on a plate.
func newDangerPlate(btn *tipButton) *dangerPlate {
	p := &dangerPlate{btn: btn, rect: canvas.NewRectangle(color.Transparent)}
	p.ExtendBaseWidget(p)
	return p
}

// CreateRenderer implements fyne.Widget.
func (p *dangerPlate) CreateRenderer() fyne.WidgetRenderer {
	r := &dangerPlateRenderer{p: p}
	r.Refresh()
	return r
}

type dangerPlateRenderer struct{ p *dangerPlate }

func (r *dangerPlateRenderer) MinSize() fyne.Size { return r.p.btn.MinSize() }

func (r *dangerPlateRenderer) Layout(s fyne.Size) {
	r.p.rect.Move(fyne.NewPos(0, 0))
	r.p.rect.Resize(s)
	r.p.btn.Move(fyne.NewPos(0, 0))
	r.p.btn.Resize(s)
}

func (r *dangerPlateRenderer) Refresh() {
	p := r.p
	th := p.Theme()
	col := themeColor(th, colorStatusError)
	p.rect.CornerRadius = th.Size(theme.SizeNameInputRadius)
	p.rect.FillColor = withAlpha(col, dangerPlateAlpha)
	p.rect.StrokeColor, p.rect.StrokeWidth = withAlpha(col, 0xb0), plateStroke
	if p.btn.Disabled() {
		p.rect.FillColor, p.rect.StrokeColor = color.Transparent, withAlpha(themeColor(th, theme.ColorNameDisabled), 0x80)
	}
	p.rect.Refresh()
	p.btn.Refresh()
}

func (r *dangerPlateRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.p.rect, r.p.btn}
}

func (r *dangerPlateRenderer) Destroy() {}
