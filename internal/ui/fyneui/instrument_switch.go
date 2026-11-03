package fyneui

import (
	"image/color"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// pillSwitch is a switch for the Dark Cockpit: a 36 by 20 rounded track with a round knob, in the
// cluster's cyan when on, a dark plate with a hairline and a muted knob when off. It is a
// widget.Check underneath (the value, OnChanged, keyboard focus, Space to toggle and the tests'
// taps are the check's), drawn differently. An integration's switch draws no label: its row writes
// the name, so the name, the tile and the switch sit on one line. A setting's switch
// (newLabelledSwitch) draws its label at its left and the track at its right edge, so the
// switches of a page line up with the integrations' at the right; a click on the label switches
// too, as a checkbox's does. Every on/off setting of Preferences and Cluster Settings is one
// (D-146); filters, column picks and dialogs keep checkboxes.
type pillSwitch struct {
	widget.Check
	focused  bool
	hovered  bool
	labelled bool
}

const (
	switchWidth  = 36
	switchHeight = 20
	switchKnob   = 14
)

// switchLabelGap is the room between a setting's label and its track.
const switchLabelGap = 12

func newPillSwitch(label string, changed func(bool)) *pillSwitch {
	s := &pillSwitch{}
	s.Text, s.OnChanged = label, changed
	s.ExtendBaseWidget(s)
	return s
}

// newLabelledSwitch is a setting's switch: its label, then the track at the right edge.
func newLabelledSwitch(label string, changed func(bool)) *pillSwitch {
	s := newPillSwitch(label, changed)
	s.labelled = true
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
		ring: canvas.NewRectangle(color.Transparent), label: canvas.NewText("", color.Transparent)}
	r.Refresh()
	return r
}

type pillSwitchRenderer struct {
	s     *pillSwitch
	track *canvas.Rectangle
	knob  *canvas.Circle
	ring  *canvas.Rectangle // the keyboard focus
	label *canvas.Text      // a setting's label; empty on an integration's switch
}

func (r *pillSwitchRenderer) MinSize() fyne.Size {
	ms := fyne.NewSize(switchWidth+4, switchHeight+4)
	if r.s.labelled {
		lm := r.label.MinSize()
		ms = fyne.NewSize(lm.Width+switchLabelGap+ms.Width, max(lm.Height, ms.Height))
	}
	return ms
}

func (r *pillSwitchRenderer) Layout(size fyne.Size) {
	x, y := (size.Width-switchWidth)/2, (size.Height-switchHeight)/2
	if r.s.labelled {
		lm := r.label.MinSize()
		r.label.Move(fyne.NewPos(0, (size.Height-lm.Height)/2))
		r.label.Resize(lm)
		x = size.Width - switchWidth - 2
	}
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
		r.track.StrokeColor, r.track.StrokeWidth = themeColor(th, colorHairline), 1
		r.knob.FillColor = themeColor(th, colorMuted)
		if r.s.hovered {
			r.knob.FillColor = themeColor(th, theme.ColorNameForeground)
		}
	}
	r.label.Text, r.label.Color = "", themeColor(th, theme.ColorNameForeground)
	if r.s.labelled {
		r.label.Text = r.s.Text
	}
	r.label.TextSize = th.Size(theme.SizeNameText)
	if r.s.Disabled() {
		r.track.FillColor = withAlpha(r.track.FillColor, 0x60)
		r.knob.FillColor = withAlpha(r.knob.FillColor, 0x60)
		r.label.Color = themeColor(th, theme.ColorNameDisabled)
	}
	r.label.Refresh()
	r.Layout(r.s.Size())
	canvas.Refresh(r.s)
}

func (r *pillSwitchRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.label, r.ring, r.track, r.knob}
}
func (r *pillSwitchRenderer) Destroy() {}

// switchGroup is a row of labelled switches, one per option: a check group's value (the options
// switched on, in their order) on a settings page, where every on/off is a switch.
type switchGroup struct {
	widget.BaseWidget
	Options   []string
	Selected  []string
	OnChanged func([]string)
	switches  []*pillSwitch
	setting   bool // SetSelected is moving the switches: one OnChanged, after all of them
}

func newSwitchGroup(options []string, changed func([]string)) *switchGroup {
	g := &switchGroup{Options: options, OnChanged: changed}
	for _, o := range options {
		g.switches = append(g.switches, newLabelledSwitch(o, func(bool) {
			if !g.setting {
				g.Selected = g.on()
				g.changed()
			}
		}))
	}
	g.ExtendBaseWidget(g)
	return g
}

// on are the options switched on, in their order.
func (g *switchGroup) on() []string {
	var out []string
	for i, s := range g.switches {
		if s.Checked {
			out = append(out, g.Options[i])
		}
	}
	return out
}

func (g *switchGroup) changed() {
	if g.OnChanged != nil {
		g.OnChanged(g.Selected)
	}
}

// SetSelected switches the options named on and the others off, as a check group's does.
func (g *switchGroup) SetSelected(options []string) {
	g.setting = true
	for i, s := range g.switches {
		s.SetChecked(slices.Contains(options, g.Options[i]))
	}
	g.setting = false
	g.Selected = g.on()
	g.changed()
}

// CreateRenderer implements fyne.Widget.
func (g *switchGroup) CreateRenderer() fyne.WidgetRenderer {
	objs := make([]fyne.CanvasObject, len(g.switches))
	for i, s := range g.switches {
		objs[i] = s
	}
	return widget.NewSimpleRenderer(box(layout.NewCustomPaddedHBoxLayout(20), objs...))
}
