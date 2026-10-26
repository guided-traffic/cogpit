package fyneui

import (
	"fmt"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// diagButton is the status bar's button for Connection Diagnostics (D-133, the owner's answer of
// 2026-10-26: "Es gibt in der Status-Leiste dann nur noch ein Button, um die Connection Diagnostics
// zu öffnen"): a muted pill "Diagnostics" while the shown cluster's diagnostics log has no problem
// since its latest connect, the warning colour with their count once it has (capped "99+"). A click
// opens the tab (diagnostics.open); the tooltip says the counts the bar showed before (the requests,
// the watches) and the problems.
type diagButton struct {
	widget.BaseWidget
	sh       *shell
	pill     *pill
	problems int
	tip      string
}

// diagButtonText is the button's word.
const diagButtonText = "Diagnostics"

func newDiagButton(sh *shell) *diagButton {
	b := &diagButton{sh: sh, pill: newPill(diagButtonText, colorMuted, false)}
	b.ExtendBaseWidget(b)
	return b
}

// set shows the shown cluster's problem count and the counters for the tooltip.
func (b *diagButton) set(d diagView) {
	b.problems = d.problems
	text, fg := diagButtonText, colorMuted
	if d.problems > 0 {
		text, fg = diagButtonText+" "+problemCount(d.problems), colorStatusWarning
	}
	b.pill.tinted = d.problems > 0
	b.pill.Set(text, fg)
	b.tip = diagButtonTip(d)
	if b.sh.tip != nil && b.sh.tip.owner == b { // a shown tooltip follows the counts
		b.sh.tip.hover(b, b.tip, b.sh.tip.pos)
	}
}

// problemCount writes a problem count as the button shows it, "99+" beyond 99.
func problemCount(n int) string {
	if n > 99 {
		return "99+"
	}
	return strconv.Itoa(n)
}

// diagButtonTip is the button's tooltip: what a click opens, the problems since the latest
// connect, and the counters the status bar showed before (the API requests, the informers).
func diagButtonTip(d diagView) string {
	tip := "Connection Diagnostics"
	switch {
	case d.problems == 1:
		tip += ": 1 problem since the latest connect"
	case d.problems > 1:
		tip += fmt.Sprintf(": %d problems since the latest connect", d.problems)
	case d.total > 0:
		tip += ": no problem since the latest connect"
	}
	return tip + fmt.Sprintf("\n%s API requests (list %s, watch %s) · %d informers",
		formatCount(d.total), formatCount(d.list), formatCount(d.watch), d.informers)
}

// Tapped implements fyne.Tappable.
func (b *diagButton) Tapped(*fyne.PointEvent) {
	b.sh.tip.hideFor(b)
	b.sh.dispatch("diagnostics.open", struct{}{}, nil)
}

// Cursor implements desktop.Cursorable.
func (b *diagButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

// MouseIn implements desktop.Hoverable.
func (b *diagButton) MouseIn(ev *desktop.MouseEvent) { b.sh.tip.hover(b, b.tip, ev.AbsolutePosition) }

// MouseMoved implements desktop.Hoverable.
func (b *diagButton) MouseMoved(ev *desktop.MouseEvent) { b.sh.tip.moved(b, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (b *diagButton) MouseOut() { b.sh.tip.hideFor(b) }

// CreateRenderer implements fyne.Widget: the pill alone, centred in the bar's height.
func (b *diagButton) CreateRenderer() fyne.WidgetRenderer { return &diagButtonRenderer{b: b} }

type diagButtonRenderer struct{ b *diagButton }

func (r *diagButtonRenderer) MinSize() fyne.Size { return r.b.pill.MinSize() }

func (r *diagButtonRenderer) Layout(size fyne.Size) {
	m := r.b.pill.MinSize()
	r.b.pill.Move(fyne.NewPos(0, (size.Height-m.Height)/2))
	r.b.pill.Resize(fyne.NewSize(size.Width, m.Height))
}

func (r *diagButtonRenderer) Refresh() {
	r.Layout(r.b.Size())
	r.b.pill.Refresh()
}

func (r *diagButtonRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.b.pill} }
func (r *diagButtonRenderer) Destroy()                     {}
