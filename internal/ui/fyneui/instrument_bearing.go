package fyneui

import (
	"image/color"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The integrations' bearing (Dark Cockpit, design prototype): one small rounded token per
// integration in a fixed order, so that the five (Metrics Server, Prometheus, Alertmanager, Trivy,
// Kyverno) read as one instrument and an operator sees at a glance which of the cluster's tools
// Cogpit is using. It sits in the Overview's header and at the top of Cluster Settings ›
// Integrations. The token says the state by its shape, never by colour alone: filled is live, an
// outline is found but stale or waiting for the user, a dim plate is off or absent, a filled
// error colour is failing; the tooltip of each says it in words, from the same status line the
// settings page shows.

// bearingState is how an integration's token is drawn.
type bearingState int

const (
	bearingOff     bearingState = iota // switched off, not in the cluster, or not asked yet: a dim plate
	bearingLive                        // found and answering: filled
	bearingNeeds                       // found but stale, or it needs the user's address: an outline
	bearingFailing                     // not answering, or no permission: filled in the error colour
)

// bearingItem is one token: an integration, its state and its tooltip.
type bearingItem struct {
	name  engine.Integration
	label string
	state bearingState
	tip   string
}

// bearingShort are the names beside the tokens, short enough for five of them in a third of the
// page; the tooltip has the full name.
var bearingShort = map[engine.Integration]string{
	engine.IntegrationMetricsServer: "Metrics",
	engine.IntegrationAlertmanager:  "Alerts",
	engine.IntegrationTrivy:         "Trivy",
}

// bearingOf maps an integration's status to its token. A switch off wins over what was found: Cogpit
// asks the cluster nothing for an integration that is off.
func bearingOf(st engine.IntegrationStatus) bearingItem {
	full := integrationNavLabels[st.Name]
	if full == "" {
		full = st.Label
	}
	label := bearingShort[st.Name]
	if label == "" {
		label = full
	}
	text, _ := integrationStatusLine(st)
	it := bearingItem{name: st.Name, label: label, tip: full + "\n" + text}
	switch st.State {
	case engine.IntegrationFound, engine.IntegrationConfigured:
		it.state = bearingLive
	case engine.IntegrationManual:
		it.state = bearingNeeds
	case engine.IntegrationForbidden, engine.IntegrationUnavailable:
		it.state = bearingFailing
	}
	if !st.On {
		it.state = bearingOff
	}
	return it
}

// bearingLiveCount counts the live tokens.
func bearingLiveCount(items []bearingItem) int {
	n := 0
	for _, it := range items {
		if it.state == bearingLive {
			n++
		}
	}
	return n
}

// bearingTone is the token's colour: the cluster's cyan for live (its live data), the status colours
// for the rest.
func bearingTone(s bearingState) fyne.ThemeColorName {
	switch s {
	case bearingLive:
		return colorAccent
	case bearingNeeds:
		return colorStatusWarning
	case bearingFailing:
		return colorStatusError
	}
	return colorTokenDim
}

// bearing is the widget: the tokens, each with its short name beside it.
type bearing struct {
	widget.BaseWidget
	sh      *shell
	items   []bearingItem
	hovered int // the token under the pointer, -1 for none
}

const (
	bearingSide = 12 // a token's side
	bearingGap  = 14 // between one token's name and the next token
)

// newBearing returns an empty bearing; sh gives its tooltips (nil: none).
func newBearing(sh *shell) *bearing {
	b := &bearing{sh: sh, hovered: -1}
	b.ExtendBaseWidget(b)
	return b
}

// SetItems shows the tokens.
func (b *bearing) SetItems(items []bearingItem) {
	b.items = items
	b.Refresh()
}

// CreateRenderer implements fyne.Widget.
func (b *bearing) CreateRenderer() fyne.WidgetRenderer {
	r := &bearingRenderer{b: b}
	r.Refresh()
	return r
}

type bearingRenderer struct {
	b     *bearing
	rects []*canvas.Rectangle
	names []*canvas.Text
}

func (r *bearingRenderer) textSize() float32 { return r.b.Theme().Size(theme.SizeNameCaptionText) }

// slot is the x range of the token i with its name.
func (b *bearing) slots() []float32 {
	size := b.Theme().Size(theme.SizeNameCaptionText)
	xs := make([]float32, 0, len(b.items)+1)
	x := float32(0)
	for _, it := range b.items {
		xs = append(xs, x)
		x += bearingSide + 6 + fyne.MeasureText(it.label, size, fyne.TextStyle{}).Width + bearingGap
	}
	return append(xs, x-bearingGap)
}

func (r *bearingRenderer) MinSize() fyne.Size {
	xs := r.b.slots()
	h := max(bearingSide, fyne.MeasureText("Mg", r.textSize(), fyne.TextStyle{}).Height)
	return fyne.NewSize(max(xs[len(xs)-1], 0), h)
}

func (r *bearingRenderer) Layout(size fyne.Size) {
	xs := r.b.slots()
	ts := r.textSize()
	th := fyne.MeasureText("Mg", ts, fyne.TextStyle{}).Height
	for i := range r.rects {
		r.rects[i].Move(fyne.NewPos(xs[i], (size.Height-bearingSide)/2))
		r.rects[i].Resize(fyne.NewSize(bearingSide, bearingSide))
		r.names[i].Move(fyne.NewPos(xs[i]+bearingSide+6, (size.Height-th)/2))
		r.names[i].Resize(fyne.NewSize(xs[i+1]-xs[i]-bearingSide-6-bearingGap, th))
	}
}

func (r *bearingRenderer) Refresh() {
	th := r.b.Theme()
	for len(r.rects) < len(r.b.items) {
		r.rects = append(r.rects, canvas.NewRectangle(color.Transparent))
		r.names = append(r.names, canvas.NewText("", color.Transparent))
	}
	r.rects, r.names = r.rects[:len(r.b.items)], r.names[:len(r.b.items)]
	for i, it := range r.b.items {
		c := themeColor(th, bearingTone(it.state))
		rect := r.rects[i]
		rect.CornerRadius = 3.5
		switch it.state {
		case bearingNeeds:
			rect.FillColor, rect.StrokeColor, rect.StrokeWidth = color.Transparent, c, 1.5
		default:
			rect.FillColor, rect.StrokeColor, rect.StrokeWidth = c, color.Transparent, 0
		}
		name := r.names[i]
		name.Text, name.TextSize = it.label, r.textSize()
		name.Color = themeColor(th, colorMuted)
		if it.state == bearingLive || it.state == bearingFailing {
			name.Color = themeColor(th, theme.ColorNameForeground)
		}
	}
	r.Layout(r.b.Size())
	canvas.Refresh(r.b)
}

func (r *bearingRenderer) Objects() []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, 0, 2*len(r.rects))
	for i := range r.rects {
		objs = append(objs, r.rects[i], r.names[i])
	}
	return objs
}
func (r *bearingRenderer) Destroy() {}

// tokenAt is the token under x, or -1.
func (b *bearing) tokenAt(x float32) int {
	xs := b.slots()
	for i := range b.items {
		if x >= xs[i] && x <= xs[i+1]-bearingGap {
			return i
		}
	}
	return -1
}

// bearingTip names the owner of a token's tooltip.
type bearingTip struct {
	b *bearing
	i int
}

// MouseIn implements desktop.Hoverable.
func (b *bearing) MouseIn(ev *desktop.MouseEvent) { b.MouseMoved(ev) }

// MouseMoved implements desktop.Hoverable.
func (b *bearing) MouseMoved(ev *desktop.MouseEvent) {
	if b.sh == nil {
		return
	}
	i := b.tokenAt(ev.Position.X)
	if i != b.hovered {
		b.sh.tip.hideFor(bearingTip{b, b.hovered})
		b.hovered = i
	}
	if i < 0 {
		return
	}
	b.sh.tip.hover(bearingTip{b, i}, b.items[i].tip, ev.AbsolutePosition)
	b.sh.tip.moved(bearingTip{b, i}, ev.AbsolutePosition)
}

// MouseOut implements desktop.Hoverable.
func (b *bearing) MouseOut() {
	if b.sh != nil {
		b.sh.tip.hideFor(bearingTip{b, b.hovered})
	}
	b.hovered = -1
}

// bearingItems are the tokens of a cluster's integrations in the fixed order of engine.Integrations;
// an integration the engine did not report is off.
func bearingItems(statuses []engine.IntegrationStatus) []bearingItem {
	byName := map[engine.Integration]engine.IntegrationStatus{}
	for _, st := range statuses {
		byName[st.Name] = st
	}
	items := make([]bearingItem, 0, len(engine.Integrations))
	for _, in := range engine.Integrations {
		st, ok := byName[in.Name]
		if !ok {
			st = engine.IntegrationStatus{Name: in.Name, Label: in.Label, State: engine.IntegrationOff}
		}
		items = append(items, bearingOf(st))
	}
	return items
}

// bearingHead is the bracket head over a bearing: "integrations · 3 of 5 live".
func bearingHead(items []bearingItem) string {
	return "integrations · " + strconv.Itoa(bearingLiveCount(items)) + " of " + strconv.Itoa(len(items)) + " live"
}
