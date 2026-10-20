package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// pillSwitch is a switch for the Dark Cockpit: a 36 by 20 rounded track with a round knob, in the
// cluster's cyan when on, a dark plate with a hairline and a muted knob when off. It is a
// widget.Check underneath (the value, OnChanged, keyboard focus, Space to toggle and the tests'
// taps are the check's), drawn differently, and it draws no label: its row writes the name, so
// the name, the tile and the switch sit on one line.
type pillSwitch struct {
	widget.Check
	focused bool
	hovered bool
}

const (
	switchWidth  = 36
	switchHeight = 20
	switchKnob   = 14
)

func newPillSwitch(label string, changed func(bool)) *pillSwitch {
	s := &pillSwitch{}
	s.Text, s.OnChanged = label, changed
	s.ExtendBaseWidget(s)
	return s
}

// Tapped implements fyne.Tappable. The whole widget is the target, not the check's own active
// area (a label's height beside the box): the switch sits in a row taller than itself.
func (s *pillSwitch) Tapped(*fyne.PointEvent) {
	if s.Disabled() {
		return
	}
	if !s.focused {
		if c := fyne.CurrentApp().Driver().CanvasForObject(s); c != nil {
			c.Focus(s)
		}
	}
	s.SetChecked(!s.Checked)
}

// FocusGained implements fyne.Focusable.
func (s *pillSwitch) FocusGained() { s.focused = true; s.Check.FocusGained(); s.Refresh() }

// FocusLost implements fyne.Focusable.
func (s *pillSwitch) FocusLost() { s.focused = false; s.Check.FocusLost(); s.Refresh() }

// MouseIn implements desktop.Hoverable.
func (s *pillSwitch) MouseIn(*desktop.MouseEvent) { s.hovered = true; s.Refresh() }

// MouseMoved implements desktop.Hoverable.
func (s *pillSwitch) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (s *pillSwitch) MouseOut() { s.hovered = false; s.Refresh() }

// Cursor implements desktop.Cursorable.
func (s *pillSwitch) Cursor() desktop.Cursor { return desktop.PointerCursor }

// CreateRenderer implements fyne.Widget.
func (s *pillSwitch) CreateRenderer() fyne.WidgetRenderer {
	r := &pillSwitchRenderer{s: s, track: canvas.NewRectangle(color.Transparent), knob: canvas.NewCircle(color.Transparent),
		ring: canvas.NewRectangle(color.Transparent)}
	r.Refresh()
	return r
}

type pillSwitchRenderer struct {
	s     *pillSwitch
	track *canvas.Rectangle
	knob  *canvas.Circle
	ring  *canvas.Rectangle // the keyboard focus
}

func (r *pillSwitchRenderer) MinSize() fyne.Size { return fyne.NewSize(switchWidth+4, switchHeight+4) }

func (r *pillSwitchRenderer) Layout(size fyne.Size) {
	x, y := (size.Width-switchWidth)/2, (size.Height-switchHeight)/2
	r.track.Move(fyne.NewPos(x, y))
	r.track.Resize(fyne.NewSize(switchWidth, switchHeight))
	r.ring.Move(fyne.NewPos(x-2, y-2))
	r.ring.Resize(fyne.NewSize(switchWidth+4, switchHeight+4))
	kx := x + (switchHeight-switchKnob)/2
	if r.s.Checked {
		kx = x + switchWidth - switchKnob - (switchHeight-switchKnob)/2
	}
	r.knob.Move(fyne.NewPos(kx, y+(switchHeight-switchKnob)/2))
	r.knob.Resize(fyne.NewSize(switchKnob, switchKnob))
}

func (r *pillSwitchRenderer) Refresh() {
	th := r.s.Theme()
	cyan := themeColor(th, colorAccent)
	r.track.CornerRadius = switchHeight / 2
	r.ring.CornerRadius = switchHeight/2 + 2
	r.ring.FillColor, r.ring.StrokeWidth = color.Transparent, 1.5
	r.ring.StrokeColor = color.Transparent
	if r.s.focused {
		r.ring.StrokeColor = themeColor(th, theme.ColorNameFocus)
	}
	if r.s.Checked {
		r.track.FillColor = withAlpha(cyan, 0x40)
		r.track.StrokeColor, r.track.StrokeWidth = withAlpha(cyan, 0xa0), 1
		r.knob.FillColor = cyan
	} else {
		r.track.FillColor = themeColor(th, theme.ColorNameInputBackground)
		r.track.StrokeColor, r.track.StrokeWidth = themeColor(th, theme.ColorNameInputBorder), 1
		r.knob.FillColor = themeColor(th, colorMuted)
		if r.s.hovered {
			r.knob.FillColor = themeColor(th, theme.ColorNameForeground)
		}
	}
	if r.s.Disabled() {
		r.track.FillColor = withAlpha(r.track.FillColor, 0x60)
		r.knob.FillColor = withAlpha(r.knob.FillColor, 0x60)
	}
	r.Layout(r.s.Size())
	canvas.Refresh(r.s)
}

func (r *pillSwitchRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.ring, r.track, r.knob}
}
func (r *pillSwitchRenderer) Destroy() {}
