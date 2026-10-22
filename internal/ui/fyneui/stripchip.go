package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// stripChip is a 28 point chip in the dock's strip: a glyph in the accent colour on a plate that
// lights up under the pointer. One stands at each end of the strip: the terminal chip on the left
// opens a terminal tab (terminalbutton.go), the create chip on the right creates an object in the
// open list (createbutton.go). shown decides when it is visible; update asks it.
type stripChip struct {
	widget.BaseWidget
	glyph   func() fyne.Resource
	shown   func() bool
	tap     func()
	hovered bool

	plate *canvas.Rectangle
	icon  *iconImage
}

func newStripChip(glyph func() fyne.Resource, shown func() bool, tap func()) *stripChip {
	b := &stripChip{glyph: glyph, shown: shown, tap: tap, plate: canvas.NewRectangle(color.Transparent), icon: newIconImage()}
	b.ExtendBaseWidget(b)
	b.Hide()
	return b
}

// update shows or hides the chip as shown says. Main goroutine only.
func (b *stripChip) update() {
	if b.shown() {
		b.Show()
	} else {
		b.Hide()
	}
}

// Tapped implements fyne.Tappable.
func (b *stripChip) Tapped(*fyne.PointEvent) { b.tap() }

// Cursor implements desktop.Cursorable.
func (b *stripChip) Cursor() desktop.Cursor { return desktop.PointerCursor }

// MouseIn implements desktop.Hoverable.
func (b *stripChip) MouseIn(*desktop.MouseEvent) { b.hovered = true; b.Refresh() }

// MouseMoved implements desktop.Hoverable.
func (b *stripChip) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (b *stripChip) MouseOut() { b.hovered = false; b.Refresh() }

// CreateRenderer implements fyne.Widget.
func (b *stripChip) CreateRenderer() fyne.WidgetRenderer {
	r := &stripChipRenderer{b: b}
	r.Refresh()
	return r
}

type stripChipRenderer struct{ b *stripChip }

// stripChipSide is the chip's side in points.
const stripChipSide = 28

func (r *stripChipRenderer) MinSize() fyne.Size {
	return fyne.NewSize(stripChipSide, stripChipSide)
}

func (r *stripChipRenderer) Layout(s fyne.Size) {
	b := r.b
	d := fyne.Min(s.Width, s.Height)
	b.plate.Move(fyne.NewPos(0, 0))
	b.plate.Resize(fyne.NewSize(d, d))
	icon := b.Theme().Size(theme.SizeNameInlineIcon)
	b.icon.img.Move(fyne.NewPos((d-icon)/2, (d-icon)/2))
	b.icon.img.Resize(fyne.NewSize(icon, icon))
}

func (r *stripChipRenderer) Refresh() {
	b := r.b
	th := b.Theme()
	b.plate.CornerRadius = th.Size(theme.SizeNameInputRadius)
	b.plate.FillColor = themeColor(th, colorChip)
	b.plate.StrokeColor, b.plate.StrokeWidth = themeColor(th, colorBracket), 1
	if b.hovered {
		b.plate.FillColor = themeColor(th, theme.ColorNameHover)
		b.plate.StrokeColor = withAlpha(themeColor(th, colorAccent), 0xa0)
	}
	b.icon.set(th, b.glyph(), colorAccent)
	r.Layout(b.Size())
	b.plate.Refresh()
}

func (r *stripChipRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.b.plate, r.b.icon.img}
}

func (r *stripChipRenderer) Destroy() {}
