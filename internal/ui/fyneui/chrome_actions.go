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

// The header checkbox's icons: an outlined box with a dash (some rows selected) or a check (every
// row), never Fyne's filled block, which was stronger than the selected row it stands for. The
// box keeps the 24 unit grid of Fyne's own icons so that it lines up with the rows' boxes.
//
// Fyne recolours an icon's filled paths but not its strokes or rect elements, so the box is a
// filled ring (an even-odd path); only the first path of an icon is recoloured, so the mark is a
// second subpath of the same one.
const checkBoxRing = `M6.5 4h11A2.5 2.5 0 0 1 20 6.5v11a2.5 2.5 0 0 1-2.5 2.5h-11A2.5 2.5 0 0 1 4 17.5v-11A2.5 2.5 0 0 1 6.5 4zM6.5 6a.5.5 0 0 0-.5.5v11a.5.5 0 0 0 .5.5h11a.5.5 0 0 0 .5-.5v-11a.5.5 0 0 0-.5-.5z`

var (
	checkSomeOutline = checkIcon("check-some-outline.svg", `M9 11h6a1 1 0 0 1 0 2H9a1 1 0 0 1 0-2z`)
	checkAllOutline  = checkIcon("check-all-outline.svg", `M7.6 12.6L9 11.2l1.8 1.8 4.4-4.4 1.4 1.4-5.8 5.8z`)
)

// checkIcon is the box with a mark inside it as one even-odd path: the mark lies in the box's
// hole, so it is filled again.
func checkIcon(name, mark string) fyne.Resource {
	return fyne.NewStaticResource(name, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill-rule="evenodd" d="`+checkBoxRing+mark+`"/></svg>`))
}
