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
	return colorSlotDim
}

// bearing is the widget: the integrations' tiles in one row spread over the width, each with its
// short name beneath, as the nodes' squares above it are spread over theirs.
type bearing struct {
	widget.BaseWidget
	sh      *shell
	items   []bearingItem
	hovered int // the token under the pointer, -1 for none
}

const (
	bearingNameGap = 8  // between a tile and its name: the nodes' legend sits as far below its squares
	bearingMinCell = 52 // the narrowest a tile's cell is, for the name beneath it
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
	b      *bearing
	plates []*canvas.Rectangle
	glyphs []*iconImage
	names  []*canvas.Text
}

func (r *bearingRenderer) textSize() float32 { return r.b.Theme().Size(theme.SizeNameCaptionText) }

func (r *bearingRenderer) textHeight() float32 {
	return fyne.MeasureText("Mg", r.textSize(), fyne.TextStyle{}).Height
}

// cell is the width of one tile's cell in a row of the given width.
func (b *bearing) cell(width float32) float32 {
	if len(b.items) == 0 {
		return 0
	}
	return width / float32(len(b.items))
}

func (r *bearingRenderer) MinSize() fyne.Size {
	if len(r.b.items) == 0 {
		return fyne.NewSize(0, instrumentTileSide)
	}
	return fyne.NewSize(bearingMinCell*float32(len(r.b.items)), instrumentTileSide+bearingNameGap+r.textHeight())
}

func (r *bearingRenderer) Layout(size fyne.Size) {
	cell := r.b.cell(size.Width)
	th := r.textHeight()
	g := float32(instrumentTileSide) * 0.58
	for i := range r.plates {
		x := float32(i) * cell
		r.plates[i].Move(fyne.NewPos(x+(cell-instrumentTileSide)/2, 0))
		r.plates[i].Resize(fyne.NewSize(instrumentTileSide, instrumentTileSide))
		r.glyphs[i].img.Move(fyne.NewPos(x+(cell-g)/2, (instrumentTileSide-g)/2))
		r.glyphs[i].img.Resize(fyne.NewSize(g, g))
		w := fyne.MeasureText(r.b.items[i].label, r.textSize(), fyne.TextStyle{}).Width
		r.names[i].Move(fyne.NewPos(x+(cell-w)/2, instrumentTileSide+bearingNameGap))
		r.names[i].Resize(fyne.NewSize(w, th))
	}
}

func (r *bearingRenderer) Refresh() {
	th := r.b.Theme()
	for len(r.plates) < len(r.b.items) {
		r.plates = append(r.plates, canvas.NewRectangle(color.Transparent))
		r.glyphs = append(r.glyphs, newIconImage())
		r.names = append(r.names, canvas.NewText("", color.Transparent))
	}
	n := len(r.b.items)
	r.plates, r.glyphs, r.names = r.plates[:n], r.glyphs[:n], r.names[:n]
	for i, it := range r.b.items {
		paintTile(th, r.plates[i], r.glyphs[i], it.name, it.state)
		name := r.names[i]
		name.Text, name.TextSize = it.label, r.textSize()
		name.Color = themeColor(th, colorMuted)
	}
	r.Layout(r.b.Size())
	canvas.Refresh(r.b)
}

func (r *bearingRenderer) Objects() []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, 0, 3*len(r.plates))
	for i := range r.plates {
		objs = append(objs, r.plates[i], r.glyphs[i].img, r.names[i])
	}
	return objs
}
func (r *bearingRenderer) Destroy() {}

// tokenAt is the token whose cell holds x, or -1.
func (b *bearing) tokenAt(x float32) int {
	cell := b.cell(b.Size().Width)
	if cell <= 0 || x < 0 {
		return -1
	}
	if i := int(x / cell); i < len(b.items) {
		return i
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
