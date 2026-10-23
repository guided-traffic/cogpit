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
// it is disabled, as a field does. The find bar's toggles (Aa, ab, .*) stay bare, switches whose on
// state has its plate. Since the owner's answers of 2026-10-23 (D-104) the same outline is on every
// icon button of a content surface (tipButton) and on every neutral plated button (Cancel, Revert,
// Close: Fyne's MediumImportance plate measured 1.1–1.2:1 against its surface); an icon button of
// the window's chrome is bare (newBareTipButton).
//
// tone, when set, draws the word, the icon and the outline in that colour: a Secret's Show, which
// exposes a value (RW3).
type textButton struct {
	widget.Button
	tone    fyne.ThemeColorName
	focused bool    // the button has the keyboard: its outline in the primary colour
	bare    bool    // Fyne's look, no outline and no room added: the chrome's icons, the search's switches
	width   float32 // at least this wide: a toggle as wide as its wider state (revealButton)
}

// textButtonSide is the room a button keeps beside its word, beyond Fyne's inner padding (the
// owner's CW4 of 2026-10-23: "rechts und links bei diesem Button noch mal so 3-5px … weil das Wort
// sonst so in diesen Button gequetscht aussieht … Keine Mindestbreite"; his pick of +3, +4 and +5
// rendered, dark and light). Every button with a word has it, a coloured one too (the lead's
// detail: Save beside an outlined Cancel looked squeezed); a button with an icon only keeps
// Fyne's square.
const textButtonSide float32 = 4

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

// MinSize implements fyne.Widget: Fyne's button, the room beside a word on either side.
func (b *textButton) MinSize() fyne.Size {
	s := b.Button.MinSize()
	if b.Text != "" && !b.bare {
		s.Width += 2 * textButtonSide
	}
	s.Width = max(s.Width, b.width)
	return s
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
	case b.bare, coloured(b.Importance):
		o.StrokeColor = color.Transparent
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

// newPlateButton is a button with Fyne's plate, as widget.NewButton makes one (MediumImportance,
// or the importance its caller sets), with the room beside its word and, while it is neutral, the
// field's outline (the owner's answer of 2026-10-23: a neutral plate measured 1.1–1.2:1 against
// its surface; a coloured one, primary, danger, warning, stays without).
func newPlateButton(label string, icon fyne.Resource, tapped func()) *textButton {
	b := newTextButton(label, icon, tapped)
	b.Importance = widget.MediumImportance
	return b
}

// coloured reports whether an importance draws a coloured plate, which marks the button already.
func coloured(i widget.Importance) bool {
	return i == widget.HighImportance || i == widget.DangerImportance || i == widget.WarningImportance || i == widget.SuccessImportance
}
