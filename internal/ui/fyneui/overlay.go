package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// dismissOverlay shows content centred over the window on a shaded background, as a modal pop-up
// does, but a click on the shade outside the content closes it (owner, 2026-10-12, of the context
// picker's mask, which went with D-57; the command palette has it now); the shell's Escape closes
// it too (escape.go). Fyne's pop-ups do one or the other: a modal one
// shades the window and ignores clicks outside, a plain one closes on them without a shade.
type dismissOverlay struct {
	widget.BaseWidget
	canvas  fyne.Canvas
	content fyne.CanvasObject
	// onDismiss runs when the user closed the overlay by a click outside or Escape.
	onDismiss func()

	shade *canvas.Rectangle
	panel *canvas.Rectangle
	shown bool
}

var (
	_ fyne.Tappable          = (*dismissOverlay)(nil)
	_ fyne.SecondaryTappable = (*dismissOverlay)(nil)
)

func newDismissOverlay(c fyne.Canvas, content fyne.CanvasObject) *dismissOverlay {
	o := &dismissOverlay{canvas: c, content: content,
		shade: canvas.NewRectangle(color.Transparent), panel: canvas.NewRectangle(color.Transparent)}
	o.ExtendBaseWidget(o)
	return o
}

// Show adds the overlay to the canvas, over everything else.
func (o *dismissOverlay) Show() {
	if !o.shown {
		o.shown = true
		o.canvas.Overlays().Add(o)
	}
	o.Resize(o.canvas.Size())
	o.BaseWidget.Show()
	o.Refresh()
}

// Hide removes the overlay from the canvas.
func (o *dismissOverlay) Hide() {
	if o.shown {
		o.shown = false
		o.canvas.Overlays().Remove(o)
	}
	o.BaseWidget.Hide()
}

// Visible reports whether the overlay is shown.
func (o *dismissOverlay) Visible() bool { return o.shown && o.BaseWidget.Visible() }

// dismiss closes the overlay as the user asked.
func (o *dismissOverlay) dismiss() {
	if !o.shown {
		return
	}
	o.Hide()
	if o.onDismiss != nil {
		o.onDismiss()
	}
}

// Tapped implements fyne.Tappable: a tap reaches the overlay itself only where no widget of the
// content takes it, so one outside the content's panel closes it and one inside does nothing.
func (o *dismissOverlay) Tapped(ev *fyne.PointEvent) {
	if !o.onPanel(ev.Position) {
		o.dismiss()
	}
}

// TappedSecondary implements fyne.SecondaryTappable: a right-click outside closes it too.
func (o *dismissOverlay) TappedSecondary(ev *fyne.PointEvent) { o.Tapped(ev) }

// onPanel reports whether a point (in the overlay's coordinates) lies on the content's panel.
func (o *dismissOverlay) onPanel(p fyne.Position) bool {
	pos, size := o.panel.Position(), o.panel.Size()
	return p.X >= pos.X && p.X <= pos.X+size.Width && p.Y >= pos.Y && p.Y <= pos.Y+size.Height
}

// CreateRenderer implements fyne.Widget.
func (o *dismissOverlay) CreateRenderer() fyne.WidgetRenderer {
	return &dismissOverlayRenderer{o: o}
}

type dismissOverlayRenderer struct{ o *dismissOverlay }

func (r *dismissOverlayRenderer) Layout(size fyne.Size) {
	o := r.o
	th := o.Theme()
	pad := th.Size(theme.SizeNameInnerPadding)
	o.shade.Move(fyne.Position{})
	o.shade.Resize(size)
	cs := o.content.MinSize()
	cs = fyne.NewSize(min(cs.Width, size.Width-2*pad), min(cs.Height, size.Height-2*pad))
	pos := fyne.NewPos((size.Width-cs.Width)/2, (size.Height-cs.Height)/2)
	o.panel.Move(pos.SubtractXY(pad, pad))
	o.panel.Resize(cs.AddWidthHeight(2*pad, 2*pad))
	o.content.Move(pos)
	o.content.Resize(cs)
}

func (r *dismissOverlayRenderer) MinSize() fyne.Size { return r.o.content.MinSize() }

func (r *dismissOverlayRenderer) Refresh() {
	o := r.o
	th := o.Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	o.shade.FillColor = th.Color(theme.ColorNameShadow, v)
	o.panel.FillColor = th.Color(theme.ColorNameOverlayBackground, v)
	o.panel.CornerRadius = th.Size(theme.SizeNamePopupRadius)
	// The drop shadow of Fyne's pop-ups (its internal ApplyShadowForLevel at PopUpLevel).
	o.panel.Shadow = canvas.Shadow{Color: th.Color(theme.ColorNameShadow, v), BlurRadius: 14,
		Offset: fyne.NewPos(0, 4), Variant: canvas.DropShadow}
	o.shade.Refresh()
	o.panel.Refresh()
	r.Layout(o.Size())
	o.content.Refresh()
}

func (r *dismissOverlayRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.o.shade, r.o.panel, r.o.content}
}

func (r *dismissOverlayRenderer) Destroy() {}
