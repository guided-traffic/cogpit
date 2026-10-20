package fyneui

import (
	"image/color"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// alertsButton is the top bar's alerts pill, left of the notifications' bell (the owner, 2026-10-16
// night: a warning sign and a number on a rounded background, as the status bar's MCP pill, its
// corners the same). Since D-77 its source is the shown cluster's Alertmanager (UIState.Alerts,
// engine/alerts.go): the owner's colours (2026-10-17) by the worst active alert, Solarized's
// status tokens: red (the error token) from a critical alert, orange (the warning token) from a
// warning, major or unlabelled one, yellow (the pending token, Solarized yellow) from an info one,
// green (the ok token) with "0" while Alertmanager answers with none (the owner, 2026-10-20); its
// number is every alert shown, "99+" beyond 99. A click opens the alert board (alerts.open). Hidden
// while the Alertmanager integration is off for the shown cluster, while no cluster is shown and for
// the demo cluster; grey (the unknown token) with "warn" while the integration is on and nothing
// answers (none found, not allowed, not answering; the owner, 2026-10-20), with a dash while the
// first look runs (the lead's, open to revision); the tooltip says which. The triangle is the bell's
// size, the number in the views' text size (first as tall as the triangle, too heavy: countSize), a
// padding between them, and both inside the pill (the owner, 2026-10-20). Cogpit's own; Lens's title bar has no such pill.
type alertsButton struct {
	widget.BaseWidget
	sh        *shell
	sum       engine.AlertsSummary // what the pill shows; the zero value while hidden
	shown     bool
	concealed bool                // the README pictures: never shown (conceal)
	divider   []fyne.CanvasObject // the divider right of the bell and its gaps, hidden with the pill when concealed
	hovered   bool
}

// conceal hides the pill and the divider after it for good: the README's showcase pictures show
// neither (showcase_test.go), as the bell is not in them either.
func (b *alertsButton) conceal() {
	b.concealed = true
	b.Hide()
	for _, o := range b.divider {
		o.Hide()
	}
}

var (
	_ desktop.Hoverable  = (*alertsButton)(nil)
	_ fyne.Tappable      = (*alertsButton)(nil)
	_ desktop.Cursorable = (*alertsButton)(nil)
)

func newAlertsButton(sh *shell) *alertsButton {
	b := &alertsButton{sh: sh}
	b.ExtendBaseWidget(b)
	b.Hide() // until the UI state says the shown cluster's integration is on
	return b
}

// update follows the UI state's pill (UIState.Alerts); unchanged state costs nothing.
func (b *alertsButton) update() {
	var sum engine.AlertsSummary
	shown := b.sh.ui.Alerts != nil && !b.concealed
	if shown {
		sum = *b.sh.ui.Alerts
	}
	if shown == b.shown && sum == b.sum {
		return
	}
	b.sum, b.shown = sum, shown
	if !shown {
		b.Hide()
		b.sh.tip.hideFor(b)
		return
	}
	b.Show()
	b.Refresh()
}

// set shows a summary as the UI state would: for tests and renders.
func (b *alertsButton) set(sum *engine.AlertsSummary) {
	b.sh.ui.Alerts = sum
	b.update()
}

// colour is the pill's colour token: the worst alert's, green without alerts, grey without an
// answer.
func (b *alertsButton) colour() fyne.ThemeColorName {
	if !b.sum.Answered {
		return colorStatusUnknown
	}
	if s := engine.AlertSeverityStatus(b.sum.Worst); s != "" {
		return statusColorName(s)
	}
	return colorStatusOK
}

// countText is the number in the pill: every alert shown, "99+" beyond 99; without an answer "warn",
// a dash while the first look runs.
func (b *alertsButton) countText() string {
	switch n := b.sum.Total; {
	case !b.sum.Answered && b.sum.State == engine.AlertsChecking:
		return "–"
	case !b.sum.Answered:
		return "warn"
	case n > 99:
		return "99+"
	default:
		return strconv.Itoa(n)
	}
}

// tipText is the pill's tooltip: the counts by severity, or why there are none.
func (b *alertsButton) tipText() string {
	s := b.sum
	switch {
	case !s.Answered && s.State == engine.AlertsChecking:
		return "Alerts: looking for the cluster's Alertmanager…"
	case !s.Answered && s.State == engine.AlertsNotFound:
		return "Alerts: no Alertmanager found in this cluster.\nThe alert board says how to set its address."
	case !s.Answered:
		return "Alerts: " + s.Detail
	}
	var parts []string
	for _, c := range []struct {
		n    int
		what string
	}{{s.Critical, "critical"}, {s.Warning, "warning"}, {s.Info, "info"}} {
		if c.n > 0 {
			parts = append(parts, strconv.Itoa(c.n)+" "+c.what)
		}
	}
	tip := "No active alerts"
	if len(parts) > 0 {
		tip = "Alerts: " + strings.Join(parts, ", ")
	}
	switch {
	case !s.FailingSince.IsZero():
		tip += "\nAlertmanager does not answer since " + b.sh.timeFormat().FormatClock(s.FailingSince) + ": its last answer."
	case b.sh.ui.Connection.State == engine.Interrupted:
		tip += "\nThe connection is interrupted: the alerts as of then."
	}
	return tip
}

// Tapped implements fyne.Tappable: the alert board.
func (b *alertsButton) Tapped(*fyne.PointEvent) {
	b.sh.tip.hideFor(b)
	b.sh.dispatch("alerts.open", struct{}{}, nil)
}

// Cursor implements desktop.Cursorable.
func (b *alertsButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

// MouseIn implements desktop.Hoverable.
func (b *alertsButton) MouseIn(ev *desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
	b.sh.tip.hover(b, b.tipText(), ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (b *alertsButton) MouseMoved(ev *desktop.MouseEvent) { b.sh.tip.moved(b, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (b *alertsButton) MouseOut() {
	b.hovered = false
	b.Refresh()
	b.sh.tip.hideFor(b)
}

// CreateRenderer implements fyne.Widget.
func (b *alertsButton) CreateRenderer() fyne.WidgetRenderer {
	r := &alertsRenderer{b: b, bg: canvas.NewRectangle(color.Transparent), img: newIconImage(),
		count: canvas.NewText("", color.Transparent)}
	r.count.TextStyle = fyne.TextStyle{Bold: true}
	for i := range r.segs {
		r.segs[i] = alertSegment{tok: canvas.NewRectangle(color.Transparent), num: canvas.NewText("", color.Transparent)}
		r.segs[i].num.TextStyle = fyne.TextStyle{Bold: true}
	}
	r.Refresh()
	return r
}

type alertsRenderer struct {
	b     *alertsButton
	bg    *canvas.Rectangle
	img   *iconImage
	count *canvas.Text
	segs  [3]alertSegment // critical, warning, info: a token sized by the count and the count (Dark Cockpit)
}

// alertSegment is one severity's token and number in the segmented pill.
type alertSegment struct {
	tok *canvas.Rectangle
	num *canvas.Text
}

// alertSegments are the pill's severities in order, with the token colour of each: the error colour
// for critical, amber for warning, blue for info (the Succeeded token's blue; the pill's triangle
// keeps the worst severity's own status colour, colour()).
var alertSegments = [3]struct {
	name  string
	tone  fyne.ThemeColorName
	count func(engine.AlertsSummary) int
}{
	{"critical", colorStatusError, func(s engine.AlertsSummary) int { return s.Critical }},
	{"warning", colorStatusWarning, func(s engine.AlertsSummary) int { return s.Warning }},
	{"info", colorStatusSucceeded, func(s engine.AlertsSummary) int { return s.Info }},
}

// segmented reports whether the pill shows the severities' tokens: while an Alertmanager answered
// with alerts. Otherwise it shows its single number (zero, a dash, "warn").
func (b *alertsButton) segmented() bool { return b.sum.Answered && b.sum.Total > 0 }

// alertTokenWidth is a severity token's width: a short bar for one alert, growing with the count to
// a long one at thirty, so that the pill reads as a proportion before it is read as numbers.
func alertTokenWidth(n int) float32 { return 8 + 0.9*float32(min(n, 30)) }

// segmentSizes are the shown segments' token widths and number sizes, in order, with the severity's
// index.
func (r *alertsRenderer) shownSegments() (idx []int, widths []float32, nums []fyne.Size) {
	if !r.b.segmented() {
		return nil, nil, nil
	}
	for i, sg := range alertSegments {
		if n := sg.count(r.b.sum); n > 0 {
			r.segs[i].num.Text = strconv.Itoa(n)
			idx = append(idx, i)
			widths = append(widths, alertTokenWidth(n))
			nums = append(nums, r.segs[i].num.MinSize())
		}
	}
	return idx, widths, nums
}

// segmentsWidth is the room the severities take, with a gap of the padding between neighbours and
// between a token and its number.
func (r *alertsRenderer) segmentsWidth() float32 {
	_, pad, _ := r.metrics()
	idx, widths, nums := r.shownSegments()
	var w float32
	for i := range idx {
		w += widths[i] + pad + nums[i].Width
		if i > 0 {
			w += 2 * pad
		}
	}
	return w
}

// metrics are the pill's inner sizes: the icon's side (the bell's), the padding at its ends
// (alertsPillEnd beyond it) and between the icon and the number, and the pill's height (the icon
// with the padding).
func (r *alertsRenderer) metrics() (icon, pad, height float32) {
	th := r.b.Theme()
	pad = th.Size(theme.SizeNamePadding)
	icon = bellIcon(th)
	return icon, pad, icon + pad
}

// countSize is the number's text size: the views' text size, its digits about two thirds of the
// warning sign's height and centred on it (10 and 14 pixels at scale 1, measured in the renders).
// The bell's size (13-pixel digits, the sign's own height) read too heavy in the owner's window
// (2026-10-20: "Die Zahl doch etwas kleiner").
func countSize(th fyne.Theme) float32 { return th.Size(theme.SizeNameText) }

// alertsPillEnd is the room added at the pill's two ends beyond the padding: the owner's look of
// 2026-10-20 ("die Pille ist mir zu eng", "5px rechts und links dazu").
const alertsPillEnd = 5

// countRise lifts the number by this share of its text size: a text's box holds the line's
// descent below the digits, so a box centred in the pill puts the digits low (measured).
const countRise = 0.08

func (r *alertsRenderer) MinSize() fyne.Size {
	icon, pad, h := r.metrics()
	inner := r.count.MinSize().Width
	if r.b.segmented() {
		inner = r.segmentsWidth()
	}
	return fyne.NewSize(2*(pad+alertsPillEnd)+icon+pad+inner, h)
}

func (r *alertsRenderer) Layout(size fyne.Size) {
	icon, pad, h := r.metrics()
	ms := r.MinSize()
	y := (size.Height - h) / 2 // the pill keeps its height and sits centred in the bar's row
	x := (size.Width - ms.Width) / 2
	r.bg.Move(fyne.NewPos(x, y))
	r.bg.Resize(fyne.NewSize(ms.Width, h))
	r.bg.CornerRadius = r.b.Theme().Size(sizeChipRadius) // the status bar's MCP pill's
	// The icon and the number start after the end's room.
	x += alertsPillEnd
	r.img.img.Move(fyne.NewPos(x+pad, y+(h-icon)/2))
	r.img.img.Resize(fyne.NewSize(icon, icon))
	ts := r.count.MinSize()
	r.count.Move(fyne.NewPos(x+pad+icon+pad, y+(h-ts.Height)/2-countRise*r.count.TextSize))
	r.count.Resize(ts)
	// The severities: a token of the count's width, 6 points high, and its number after it.
	idx, widths, nums := r.shownSegments()
	sx := x + pad + icon + pad
	for k, i := range idx {
		if k > 0 {
			sx += 2 * pad
		}
		sg := r.segs[i]
		sg.tok.Move(fyne.NewPos(sx, y+(h-6)/2))
		sg.tok.Resize(fyne.NewSize(widths[k], 6))
		sg.tok.CornerRadius = 3
		sx += widths[k] + pad
		sg.num.Move(fyne.NewPos(sx, y+(h-nums[k].Height)/2-countRise*sg.num.TextSize))
		sg.num.Resize(nums[k])
		sx += nums[k].Width
	}
}

func (r *alertsRenderer) Refresh() {
	th := r.b.Theme()
	c := r.b.colour()
	fg := themeColor(th, c)
	r.img.set(th, theme.WarningIcon(), c)
	r.count.Text = r.b.countText()
	r.count.TextSize = countSize(th)
	r.count.Color = fg
	alpha := uint8(0x2c) // as a tinted pill's (widgets.go)
	if r.b.hovered {
		alpha = 0x48
	}
	r.bg.FillColor = withAlpha(fg, alpha)
	// With severities to show the plate is neutral, so that the tokens carry the colours and the
	// triangle the worst one's.
	seg := r.b.segmented()
	r.count.Hidden = seg
	if seg {
		r.bg.FillColor = themeColor(th, colorChip)
		if r.b.hovered {
			r.bg.FillColor = themeColor(th, colorRowHover)
		}
	}
	for i, sg := range r.segs {
		tone := themeColor(th, alertSegments[i].tone)
		sg.tok.FillColor = tone
		sg.num.TextSize, sg.num.Color = countSize(th), themeColor(th, theme.ColorNameForeground)
		on := seg && alertSegments[i].count(r.b.sum) > 0
		sg.tok.Hidden, sg.num.Hidden = !on, !on
	}
	r.Layout(r.b.Size())
	r.bg.Refresh()
	r.count.Refresh()
}

func (r *alertsRenderer) Objects() []fyne.CanvasObject {
	objs := []fyne.CanvasObject{r.bg, r.img.img, r.count}
	for _, sg := range r.segs {
		objs = append(objs, sg.tok, sg.num)
	}
	return objs
}

func (r *alertsRenderer) Destroy() {}
