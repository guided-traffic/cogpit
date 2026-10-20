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

// instrumentTile is the 28 point rounded square at the left of an integration's row (Dark Cockpit,
// design prototype): the integration's initial on a plate tinted by its state, the same tones as
// the bearing's tokens (instrument_bearing.go). A live integration glows in the cluster's cyan, one
// that needs the user is outlined in amber, a failing one is coral, an absent or switched off one is
// dim. The tint is never the only signal: the row's status line says it in words.
type instrumentTile struct {
	widget.BaseWidget
	letter string
	state  bearingState
}

const instrumentTileSide = 28

func newInstrumentTile(name engine.Integration) *instrumentTile {
	t := &instrumentTile{letter: instrumentLetter(name)}
	t.ExtendBaseWidget(t)
	return t
}

// instrumentLetter is the monogram of an integration: M for Metrics Server, P, A, T, K.
func instrumentLetter(name engine.Integration) string {
	if l := integrationNavLabels[name]; l != "" {
		return l[:1]
	}
	return "?"
}

// setState tints the tile.
func (t *instrumentTile) setState(s bearingState) {
	if s == t.state {
		return
	}
	t.state = s
	t.Refresh()
}

// CreateRenderer implements fyne.Widget.
func (t *instrumentTile) CreateRenderer() fyne.WidgetRenderer {
	r := &instrumentTileRenderer{t: t, plate: canvas.NewRectangle(color.Transparent), text: canvas.NewText("", color.Transparent)}
	r.text.TextStyle = fyne.TextStyle{Bold: true}
	r.Refresh()
	return r
}

type instrumentTileRenderer struct {
	t     *instrumentTile
	plate *canvas.Rectangle
	text  *canvas.Text
}

func (r *instrumentTileRenderer) MinSize() fyne.Size {
	return fyne.NewSize(instrumentTileSide, instrumentTileSide)
}

func (r *instrumentTileRenderer) Layout(size fyne.Size) {
	r.plate.Resize(size)
	ts := fyne.MeasureText(r.text.Text, r.text.TextSize, r.text.TextStyle)
	r.text.Resize(ts)
	r.text.Move(fyne.NewPos((size.Width-ts.Width)/2, (size.Height-ts.Height)/2))
}

func (r *instrumentTileRenderer) Refresh() {
	th := r.t.Theme()
	tone := themeColor(th, bearingTone(r.t.state))
	r.plate.CornerRadius = 8
	r.text.Text, r.text.TextSize = r.t.letter, th.Size(theme.SizeNameText)
	switch r.t.state {
	case bearingOff:
		r.plate.FillColor, r.plate.StrokeColor, r.plate.StrokeWidth = themeColor(th, colorTokenDim), color.Transparent, 0
		r.text.Color = themeColor(th, colorMuted)
	case bearingNeeds:
		amber := themeColor(th, colorStatusWarning)
		r.plate.FillColor, r.plate.StrokeColor, r.plate.StrokeWidth = withAlpha(amber, 0x1a), withAlpha(amber, 0xb0), 1.5
		r.text.Color = amber
	default:
		r.plate.FillColor, r.plate.StrokeColor, r.plate.StrokeWidth = withAlpha(tone, 0x24), withAlpha(tone, 0x70), 1
		r.text.Color = tone
	}
	r.Layout(r.t.Size())
	canvas.Refresh(r.t)
}

func (r *instrumentTileRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.plate, r.text}
}
func (r *instrumentTileRenderer) Destroy() {}

// integrationGroups are the two groups of Settings › Integrations: the integrations switched on, in
// the cluster's cyan, and the ones off, muted; each under a bracket head with its count ("on · 3").
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
	g := &integrationGroups{on: &integrationGroup{word: "on"}, off: &integrationGroup{word: "off"}}
	g.on.head, g.off.head = newBracketLabel("on", colorAccent), newBracketLabel("off", "")
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
