package fyneui

import (
	"image/color"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// settingsOverlay shows a settings page as a card over the whole window, Cogpit dimmed behind it
// (the owner's VW6 of 2026-10-23: "Cluster-Settings selbst sollte eine Maske bekommen, die über
// Cogpit drüber liegt", Preferences the same; D-118). It is an overlay of the window's canvas, as
// the command palette's dismissOverlay is, because Fyne gives an overlay two things a layer of the
// window's content does not have: only the top overlay receives the pointer, so nothing behind the
// shade is clicked, scrolled or hovered; and every overlay has a focus manager of its own, so Tab
// goes round the card's fields and never on to a field behind it.
//
// The window's tooltip layer and toasts lie in its content, under every overlay: while the card
// shows they are lifted into it, above the card (lift), so that a button's tooltip and a toast show
// and a toast keeps its hover, its copy icon and its ×, as a dialog keeps a tooltip layer of its own
// (dialogframe.go). A dialog, a pop-up or a menu opened from the page is an overlay above this one.
//
// A click or a right-click on the shade closes the page, as the palette's does; one on the card's
// own background does nothing. Escape closes it too (escape.go: the top overlay first).
type settingsOverlay struct {
	widget.BaseWidget
	sh   *shell
	card fyne.CanvasObject // the settings page (settingsPage.root)
	// onDismiss is the user's close by the shade or Escape (settingsPage.close).
	onDismiss func()

	shade  *canvas.Rectangle
	shadow *canvas.Rectangle   // the card's drop shadow, under it
	layers []fyne.CanvasObject // the window's tooltip layer and toasts while the card shows
	shown  bool
}

var (
	_ fyne.Tappable          = (*settingsOverlay)(nil)
	_ fyne.SecondaryTappable = (*settingsOverlay)(nil)
)

// The card's size follows the window's alone, never the shown section's, so that it does not jump
// between sections: inset from each edge by 5 % of the window and at least settingsMargin, at most
// settingsCardMaxWidth wide (a dialog's look on a wide screen; a section grows downwards, so the
// height is the window's), never smaller than the page needs and never larger than the window
// (D-118).
const (
	settingsMargin       = 24
	settingsMarginShare  = 0.05
	settingsCardMaxWidth = 1120
)

func newSettingsOverlay(sh *shell, card fyne.CanvasObject, onDismiss func()) *settingsOverlay {
	o := &settingsOverlay{sh: sh, card: card, onDismiss: onDismiss,
		shade: canvas.NewRectangle(color.Transparent), shadow: canvas.NewRectangle(color.Transparent)}
	o.ExtendBaseWidget(o)
	return o
}

// Show adds the overlay to the window's canvas, over everything shown so far, and lifts the
// tooltips and toasts into it.
func (o *settingsOverlay) Show() {
	c := o.sh.win.Canvas()
	if !o.shown {
		o.shown = true
		o.lift(true)
		c.Overlays().Add(o)
		onEscape(c, o, o.onDismiss)
	}
	o.Resize(c.Size())
	o.BaseWidget.Show()
	o.Refresh()
}

// Hide removes the overlay and gives the tooltips and toasts back to the window's content.
func (o *settingsOverlay) Hide() {
	if o.shown {
		o.shown = false
		o.sh.win.Canvas().Overlays().Remove(o)
		o.lift(false)
	}
	o.BaseWidget.Hide()
}

// Visible reports whether the overlay is shown.
func (o *settingsOverlay) Visible() bool { return o.shown && o.BaseWidget.Visible() }

// lift moves the window's tooltip layer and toasts from its content into the overlay, or back.
func (o *settingsOverlay) lift(up bool) {
	sh := o.sh
	layers := []fyne.CanvasObject{sh.tip.layer, sh.toasts}
	sh.root.Objects = slices.DeleteFunc(sh.root.Objects, func(c fyne.CanvasObject) bool { return slices.Contains(layers, c) })
	if up {
		o.layers = layers
	} else {
		o.layers = nil
		sh.root.Objects = append(sh.root.Objects, layers...)
	}
	sh.root.Refresh()
}

// Tapped implements fyne.Tappable: a tap reaches the overlay itself only where no widget of the
// card takes it, so one on the shade closes the page and one on the card does nothing.
func (o *settingsOverlay) Tapped(ev *fyne.PointEvent) {
	pos, size := o.card.Position(), o.card.Size()
	p := ev.Position
	if p.X >= pos.X && p.X <= pos.X+size.Width && p.Y >= pos.Y && p.Y <= pos.Y+size.Height {
		return
	}
	if o.onDismiss != nil {
		o.onDismiss()
	}
}

// TappedSecondary implements fyne.SecondaryTappable: a right-click on the shade closes it too.
func (o *settingsOverlay) TappedSecondary(ev *fyne.PointEvent) { o.Tapped(ev) }

// settingsCardRect is where the card stands in a window of size area, for a page that needs least.
func settingsCardRect(area, least fyne.Size) (fyne.Position, fyne.Size) {
	mx := max(settingsMargin, area.Width*settingsMarginShare)
	my := max(settingsMargin, area.Height*settingsMarginShare)
	w := min(area.Width-2*mx, settingsCardMaxWidth)
	h := area.Height - 2*my
	w = min(max(w, least.Width), area.Width)
	h = min(max(h, least.Height), area.Height)
	return fyne.NewPos((area.Width-w)/2, (area.Height-h)/2), fyne.NewSize(w, h)
}

// CreateRenderer implements fyne.Widget.
func (o *settingsOverlay) CreateRenderer() fyne.WidgetRenderer {
	return &settingsOverlayRenderer{o: o}
}

type settingsOverlayRenderer struct{ o *settingsOverlay }

func (r *settingsOverlayRenderer) Layout(size fyne.Size) {
	o := r.o
	o.shade.Move(fyne.Position{})
	o.shade.Resize(size)
	pos, cs := settingsCardRect(size, o.card.MinSize())
	o.shadow.Move(pos)
	o.shadow.Resize(cs)
	o.card.Move(pos)
	o.card.Resize(cs)
	for _, l := range o.layers {
		l.Move(fyne.Position{})
		l.Resize(size)
	}
}

func (r *settingsOverlayRenderer) MinSize() fyne.Size { return r.o.card.MinSize() }

func (r *settingsOverlayRenderer) Refresh() {
	o := r.o
	th := o.Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	o.shade.FillColor = th.Color(theme.ColorNameShadow, v)
	o.shadow.FillColor = th.Color(theme.ColorNameBackground, v)
	o.shadow.CornerRadius = th.Size(theme.SizeNameDialogRadius)
	// The drop shadow of Fyne's pop-ups (its internal ApplyShadowForLevel at PopUpLevel), as the
	// palette's (overlay.go).
	o.shadow.Shadow = canvas.Shadow{Color: th.Color(theme.ColorNameShadow, v), BlurRadius: 14,
		Offset: fyne.NewPos(0, 4), Variant: canvas.DropShadow}
	o.shade.Refresh()
	o.shadow.Refresh()
	r.Layout(o.Size())
	o.card.Refresh()
}

func (r *settingsOverlayRenderer) Objects() []fyne.CanvasObject {
	return append([]fyne.CanvasObject{r.o.shade, r.o.shadow, r.o.card}, r.o.layers...)
}

func (r *settingsOverlayRenderer) Destroy() {}
