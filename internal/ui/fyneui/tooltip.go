package fyneui

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// tooltipDelay is how long the pointer rests on something before its tooltip shows.
const tooltipDelay = 450 * time.Millisecond

// tooltip shows one short text near the pointer after a hover delay: the full text of a table
// cell that is cut off, the reason of a warning icon, the name of a drawer action. Fyne 2.8 has no
// tooltip widget. The tooltip is drawn in a layer of the window's content above the shell (below
// popups and dialogs); the layer is neither hoverable nor tappable, so the widget under the
// pointer keeps receiving its events and decides when the tooltip goes away.
//
// Every method runs on the main goroutine. The delay's timer posts back with sh.do and shows the
// tooltip only if nothing changed in between (gen).
type tooltip struct {
	sh    *shell
	layer *fyne.Container

	delay time.Duration
	// inDialog marks a dialog's own tooltip (newDialogTooltip): its layer lies in the dialog, over
	// the window's other overlays, where the shell's layer cannot reach, and is placed anywhere in
	// the window, so the tooltip is kept inside the window rather than inside its layer.
	inDialog bool
	owner    any           // who asked for the current or pending tooltip: a table cell, a button
	text     string        // what it shows
	pos      fyne.Position // the pointer, in window coordinates
	shown    bool
	gen      uint64
	timer    *time.Timer
}

func newTooltip(sh *shell) *tooltip {
	t := &tooltip{sh: sh, delay: tooltipDelay}
	t.layer = box(&tooltipLayout{t: t})
	return t
}

// newDialogTooltip is a dialog's own tooltip (dialogFrame): the shell's layer lies under every
// overlay, and only the top overlay receives the pointer, so a tooltip over a dialog lives in it.
func newDialogTooltip(sh *shell) *tooltip {
	t := newTooltip(sh)
	t.inDialog = true
	return t
}

// now shows owner's tooltip at once, without the delay: a click on a hint's "?" (paramdialog.go).
// A second click on the same owner hides it.
func (t *tooltip) now(owner any, text string, pos fyne.Position) {
	if owner == t.owner && t.shown {
		t.hide()
		return
	}
	t.hide()
	if text == "" {
		return
	}
	t.owner, t.text, t.pos = owner, text, pos
	t.show()
}

// hover asks for owner's tooltip text at the pointer position pos. The same owner and text keep
// the pending or shown tooltip; anything else replaces it after the delay. An empty text hides
// owner's tooltip.
func (t *tooltip) hover(owner any, text string, pos fyne.Position) {
	if text == "" {
		t.hideFor(owner)
		return
	}
	if owner == t.owner && text == t.text {
		if !t.shown {
			t.pos = pos
		}
		return
	}
	t.hide()
	t.owner, t.text, t.pos = owner, text, pos
	g := t.gen
	t.timer = time.AfterFunc(t.delay, func() {
		t.sh.do(func() {
			if t.gen == g && t.owner != nil {
				t.show()
			}
		})
	})
}

// moved follows the pointer until the tooltip shows; a shown tooltip stays where it opened.
func (t *tooltip) moved(owner any, pos fyne.Position) {
	if owner == t.owner && !t.shown {
		t.pos = pos
	}
}

// hideFor hides the tooltip if owner asked for it.
func (t *tooltip) hideFor(owner any) {
	if owner == t.owner {
		t.hide()
	}
}

// hide removes the tooltip and cancels a pending one.
func (t *tooltip) hide() {
	t.gen++
	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
	}
	t.owner, t.text = nil, ""
	if t.shown {
		t.shown = false
		t.layer.Objects = nil
		t.layer.Refresh()
	}
}

// visibleText is the text of the shown tooltip, "" when none shows.
func (t *tooltip) visibleText() string {
	if !t.shown {
		return ""
	}
	return t.text
}

// show draws the tooltip: the text wrapped at the theme's tooltip width, on the tooltip colour.
func (t *tooltip) show() {
	th := t.sh.app.Settings().Theme()
	size := th.Size(theme.SizeNameCaptionText)
	width := float32(0)
	for _, line := range strings.Split(t.text, "\n") {
		width = fyne.Max(width, fyne.MeasureText(line, size, fyne.TextStyle{}).Width)
	}
	width = fyne.Min(width+1, th.Size(sizeTooltipWidth))
	text := newParagraph(t.text, theme.ColorNameForeground, width)
	text.size = theme.SizeNameCaptionText
	bg := newSurface(colorTooltip)
	bg.radius, bg.stroke = theme.SizeNameInputRadius, theme.ColorNameInputBorder
	t.layer.Objects = []fyne.CanvasObject{container.NewStack(bg, inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, text))}
	t.shown = true
	t.layer.Refresh()
}

// tooltipLayout places the tooltip below and right of the pointer, inside the window.
type tooltipLayout struct{ t *tooltip }

func (l *tooltipLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.Size{} }

func (l *tooltipLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) == 0 {
		return
	}
	o := objs[0]
	ms := o.MinSize()
	off := fyne.CurrentApp().Settings().Theme().Size(sizeTooltipOffset)
	var origin fyne.Position // the layer's place in the window: the shell's layer covers it
	area := size
	if l.t.inDialog {
		origin = fyne.CurrentApp().Driver().AbsolutePositionForObject(l.t.layer)
		area = l.t.sh.win.Canvas().Size()
	}
	pos := l.t.pos.AddXY(off, off)
	if pos.X+ms.Width > area.Width {
		pos.X = fyne.Max(0, area.Width-ms.Width-off/2)
	}
	if pos.Y+ms.Height > area.Height {
		pos.Y = fyne.Max(0, l.t.pos.Y-ms.Height-off/2)
	}
	o.Move(pos.Subtract(origin))
	o.Resize(ms)
}

// tipButton is a button with a tooltip: the drawer's action icons, named by their tooltip. A
// disabled one still shows its tooltip, which says why it is disabled.
type tipButton struct {
	widget.Button
	sh  *shell
	tip string
}

func newTipButton(sh *shell, icon fyne.Resource, tip string, tapped func()) *tipButton {
	b := &tipButton{sh: sh, tip: tip}
	b.Icon, b.OnTapped, b.Importance = icon, tapped, widget.LowImportance
	b.ExtendBaseWidget(b)
	return b
}

// setTip changes the tooltip's text; a tooltip shown or pending for the button takes the new one.
func (b *tipButton) setTip(text string) {
	if b.tip == text {
		return
	}
	b.tip = text
	if b.sh.tip.owner == b {
		b.sh.tip.hover(b, text, b.sh.tip.pos)
	}
}

// MouseIn implements desktop.Hoverable.
func (b *tipButton) MouseIn(ev *desktop.MouseEvent) {
	b.Button.MouseIn(ev)
	b.sh.tip.hover(b, b.tip, ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (b *tipButton) MouseMoved(ev *desktop.MouseEvent) {
	b.Button.MouseMoved(ev)
	b.sh.tip.moved(b, ev.AbsolutePosition)
}

// MouseOut implements desktop.Hoverable.
func (b *tipButton) MouseOut() {
	b.Button.MouseOut()
	b.sh.tip.hideFor(b)
}

// Tapped implements fyne.Tappable.
func (b *tipButton) Tapped(ev *fyne.PointEvent) {
	b.sh.tip.hideFor(b)
	b.Button.Tapped(ev)
}
