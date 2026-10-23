package fyneui

import (
	"image/color"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// textButton is a button with a word on it and no plate of its own, every one in the window (the
// owner's RW2 of 2026-10-23, D-104): Fyne's LowImportance button draws neither a plate nor an
// outline until the pointer is on it, so Show, Hide, Add Key or Dismiss read as text, in the dark
// theme and the light one. This one draws the field's outline round it (theme.ColorNameInputBorder,
// the dropdown's since AW1), the primary colour while it has the keyboard, the disabled colour while
// it is disabled, as a field does. A button with an icon only (copy, remove, close) stays bare, and
// so do the find bar's toggles (Aa, ab, .*), switches whose on state has its plate.
//
// tone, when set, draws the word, the icon and the outline in that colour: a Secret's Show, which
// exposes a value (RW3).
type textButton struct {
	widget.Button
	tone    fyne.ThemeColorName
	focused bool // the button has the keyboard: its outline in the primary colour
}

func newTextButton(label string, icon fyne.Resource, tapped func()) *textButton {
	b := &textButton{}
	b.Text, b.Icon, b.OnTapped, b.Importance = label, icon, tapped, widget.LowImportance
	b.ExtendBaseWidget(b)
	return b
}

// setTone draws the button in c ("" for the plain look): its word, its icon and its outline.
func (b *textButton) setTone(c fyne.ThemeColorName) {
	if b.tone == c {
		return
	}
	b.tone = c
	b.Refresh()
}

// FocusGained implements fyne.Focusable.
func (b *textButton) FocusGained() {
	b.focused = true
	b.Button.FocusGained()
}

// FocusLost implements fyne.Focusable.
func (b *textButton) FocusLost() {
	b.focused = false
	b.Button.FocusLost()
}

// CreateRenderer implements fyne.Widget: Fyne's button with the outline over it.
func (b *textButton) CreateRenderer() fyne.WidgetRenderer {
	r := &textButtonRenderer{WidgetRenderer: b.Button.CreateRenderer(), b: b, outline: canvas.NewRectangle(color.Transparent)}
	r.style()
	return r
}

// textButtonRenderer is Fyne's button renderer and the outline.
type textButtonRenderer struct {
	fyne.WidgetRenderer
	b       *textButton
	outline *canvas.Rectangle
	objects []fyne.CanvasObject
}

func (r *textButtonRenderer) style() {
	b := r.b
	th := b.Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	o := r.outline
	o.StrokeWidth = th.Size(theme.SizeNameInputBorder)
	o.CornerRadius = th.Size(theme.SizeNameButtonRadius)
	switch {
	case b.Disabled():
		o.StrokeColor = th.Color(theme.ColorNameDisabled, v)
	case b.focused:
		o.StrokeColor = th.Color(theme.ColorNamePrimary, v)
	case b.tone != "":
		o.StrokeColor = th.Color(b.tone, v)
	default:
		o.StrokeColor = th.Color(theme.ColorNameInputBorder, v)
	}
	if b.tone == "" || b.Disabled() {
		return
	}
	for _, obj := range r.WidgetRenderer.Objects() { // Fyne's renderer set the foreground: the tone instead
		switch x := obj.(type) {
		case *widget.RichText:
			if seg, ok := x.Segments[0].(*widget.TextSegment); ok && seg.Style.ColorName != b.tone {
				seg.Style.ColorName = b.tone
				x.Refresh()
			}
		case *canvas.Image:
			if res, ok := x.Resource.(fyne.ThemedResource); ok && res.ThemeColorName() != b.tone {
				x.Resource = theme.NewColoredResource(x.Resource, b.tone)
				x.Refresh()
			}
		}
	}
}

func (r *textButtonRenderer) Refresh() {
	r.WidgetRenderer.Refresh()
	r.style()
	r.Layout(r.b.Size())
	r.outline.Refresh()
}

// Layout places the outline as a field's: inside the button by half its width.
func (r *textButtonRenderer) Layout(size fyne.Size) {
	r.WidgetRenderer.Layout(size)
	w := r.b.Theme().Size(theme.SizeNameInputBorder)
	r.outline.Move(fyne.NewSquareOffsetPos(w / 2))
	r.outline.Resize(fyne.NewSize(size.Width-w-.5, size.Height-w-.5)) // .5: a low DPI rounds down at the trailing edge
}

// Objects are Fyne's, the outline over them; Fyne's renderer adds its icon's image when the
// button first gets one.
func (r *textButtonRenderer) Objects() []fyne.CanvasObject {
	inner := r.WidgetRenderer.Objects()
	if len(r.objects) != len(inner)+1 {
		r.objects = append(slices.Clone(inner), r.outline)
	}
	return r.objects
}
