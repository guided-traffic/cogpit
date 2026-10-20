package fyneui

import (
	"strconv"

	"fyne.io/fyne/v2/container"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// instrumentTile is the 28 point rounded square of an integration (Dark Cockpit, design prototype):
// its glyph on a plate tinted by its state, the same object in the Overview's bearing and at the
// left of its row in Settings › Integrations. A live integration is tinted in the cluster's cyan, one
// that needs the user is outlined in amber, a failing one is coral, an absent or switched off one is
// dim. The tint is never the only signal: the status line and the tooltip say it in words.
type instrumentTile struct {
	widget.BaseWidget
	name  engine.Integration
	state bearingState
}

const instrumentTileSide = 28

func newInstrumentTile(name engine.Integration) *instrumentTile {
	t := &instrumentTile{name: name}
	t.ExtendBaseWidget(t)
	return t
}

// The integrations' glyphs, Cogpit's own drawings (clean-room, D-12) on a 24-unit grid: the
// Metrics Server's chip, Prometheus' rising line, the Alertmanager's bell, Trivy's shield and
// Kyverno's checked ring.
var (
	glyphPrometheus = metricIcon("cogpit-prometheus.svg", `<path d="M3 18.2 9 11l3.6 3.6L19.6 6.4l1.6 1.4-8.5 10-3.6-3.6-4.7 5.6z"/>`)
	glyphTrivy      = metricIcon("cogpit-trivy.svg", `<path d="M12 2 4 5.5v5.7c0 4.7 3.3 8.7 8 10 4.7-1.3 8-5.3 8-10V5.5z"/>`)
	glyphKyverno    = metricIcon("cogpit-kyverno.svg", `<path fill-rule="evenodd" d="M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20zm0 2.4a7.6 7.6 0 1 1 0 15.2 7.6 7.6 0 0 1 0-15.2z"/>`+
		`<path d="M10.6 15.6 7.4 12.4l1.4-1.4 1.8 1.8 4.6-4.6 1.4 1.4z"/>`)
)

// instrumentGlyph is the glyph of an integration; the first letter's absence is a state nobody
// reaches (the five are fixed), so an unknown one gets the chip.
func instrumentGlyph(name engine.Integration) fyne.Resource {
	switch name {
	case engine.IntegrationPrometheus:
		return glyphPrometheus
	case engine.IntegrationAlertmanager:
		return iconBell
	case engine.IntegrationTrivy:
		return glyphTrivy
	case engine.IntegrationKyverno:
		return glyphKyverno
	}
	return cpuIcon
}

// setState tints the tile.
func (t *instrumentTile) setState(s bearingState) {
	if s == t.state {
		return
	}
	t.state = s
	t.Refresh()
}

// paintTile gives a plate and its glyph the look of a state; side is the plate's side, for the
// radius and the glyph's size.
func paintTile(th fyne.Theme, plate *canvas.Rectangle, glyph *iconImage, name engine.Integration, state bearingState) {
	tone := bearingTone(state)
	c := themeColor(th, tone)
	plate.CornerRadius = 8
	switch state {
	case bearingOff:
		plate.FillColor, plate.StrokeColor, plate.StrokeWidth = themeColor(th, colorTokenDim), themeColor(th, colorBracket), 1
		tone = colorMuted
	case bearingNeeds:
		plate.FillColor, plate.StrokeColor, plate.StrokeWidth = withAlpha(c, 0x1a), withAlpha(c, 0xb0), 1.5
	default:
		plate.FillColor, plate.StrokeColor, plate.StrokeWidth = withAlpha(c, 0x24), withAlpha(c, 0x70), 1
	}
	glyph.set(th, instrumentGlyph(name), tone)
}

// CreateRenderer implements fyne.Widget.
func (t *instrumentTile) CreateRenderer() fyne.WidgetRenderer {
	r := &instrumentTileRenderer{t: t, plate: canvas.NewRectangle(color.Transparent), glyph: newIconImage()}
	r.Refresh()
	return r
}

type instrumentTileRenderer struct {
	t     *instrumentTile
	plate *canvas.Rectangle
	glyph *iconImage
}

func (r *instrumentTileRenderer) MinSize() fyne.Size {
	return fyne.NewSize(instrumentTileSide, instrumentTileSide)
}

func (r *instrumentTileRenderer) Layout(size fyne.Size) {
	r.plate.Resize(size)
	g := size.Width * 0.58
	r.glyph.img.Resize(fyne.NewSize(g, g))
	r.glyph.img.Move(fyne.NewPos((size.Width-g)/2, (size.Height-g)/2))
}

func (r *instrumentTileRenderer) Refresh() {
	paintTile(r.t.Theme(), r.plate, r.glyph, r.t.name, r.t.state)
	r.Layout(r.t.Size())
	canvas.Refresh(r.t)
}

func (r *instrumentTileRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.plate, r.glyph.img}
}
func (r *instrumentTileRenderer) Destroy() {}

// integrationGroups are the two groups of Settings › Integrations: the integrations switched on ("enabled"), in
// the cluster's cyan, and the ones off, muted; each under a bracket head with its count ("enabled · 3").
type integrationGroups struct {
	on, off *integrationGroup
	key     string // which rows are in which group, so that a sync that changes nothing rebuilds nothing
}

type integrationGroup struct {
	word string
	head *bracketLabel
	box  *fyne.Container
}

func newIntegrationGroups() *integrationGroups {
	g := &integrationGroups{on: &integrationGroup{word: "enabled"}, off: &integrationGroup{word: "off"}}
	g.on.head, g.off.head = newBracketLabel("enabled", colorAccent), newBracketLabel("off", "")
	for _, grp := range []*integrationGroup{g.on, g.off} {
		grp.box = container.NewVBox()
	}
	return g
}

// place puts each row into its group by its switch and writes the counts into the heads; an empty
// group is hidden.
func (g *integrationGroups) place(rows []*integrationRow) {
	var on, off []*integrationRow
	key := ""
	for _, r := range rows {
		if r.check.Checked {
			on = append(on, r)
			key += "+" + string(r.name)
		} else {
			off = append(off, r)
			key += "-" + string(r.name)
		}
	}
	if key == g.key {
		return
	}
	g.key = key
	for _, c := range []struct {
		grp  *integrationGroup
		rows []*integrationRow
	}{{g.on, on}, {g.off, off}} {
		c.grp.head.SetText(c.grp.word + " · " + strconv.Itoa(len(c.rows)))
		objs := []fyne.CanvasObject{c.grp.head, newSpace(theme.SizeNameInnerPadding)}
		for _, r := range c.rows {
			objs = append(objs, r.box, newSpace(theme.SizeNameInnerPadding))
		}
		c.grp.box.Objects = objs
		c.grp.box.Refresh()
		if len(c.rows) == 0 {
			c.grp.box.Hide()
		} else {
			c.grp.box.Show()
		}
	}
}
