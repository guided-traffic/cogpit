package fyneui

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A cluster's accent colour on Cluster Settings › General (D-80, the owner's F26 of 2026-10-17;
// engine/accentcolor.go): a row of swatches under the icon, as the icon colour has: None first, the
// eight accents, a colour picked freely (its swatch while one is set) and the picker. A tap
// dispatches cluster.settings.setAccentColor, which only the window may run: the colour is the
// user's own mark of which cluster this is. The light strip under the tabs shows it
// (lightstrip.go, D-new), as the row under this one chooses (clustersettings_lightstrip.go).
// Cogpit's own: Lens has no accent colour.

const (
	accentSetting      = "accentColor"
	accentTitle        = "Accent colour"
	accentHelp         = "Your mark for this cluster, red for production, say: the strip under the tabs shows it. Only you set it; agents read it."
	accentPickerTitle  = "Accent colour"
	iconPickerTitle    = "Cluster colour"
	freeColourHelp     = "Any colour: Cogpit draws it in a shade of its hue that stays legible on the light and the dark theme."
	accentNoneSwatchID = "accentNone"
	noneSwatchName     = "none" // the None swatch's colour name: its tooltip reads "None"
)

// accentColorSetting builds the Accent colour setting and the function that shows the cluster's
// colour in it.
func (p *settingsPage) accentColorSetting() (fyne.CanvasObject, func()) {
	ctx := p.shown.Context
	set := func(c string) { p.setAccentColor(ctx, c) }
	none := newNoneSwatch(p.sh, func() { set("") })
	row := container.NewHBox(none)
	swatches := make([]*swatch, 0, len(engine.IconColors))
	for _, c := range engine.IconColors {
		col := c
		s := newSwatch(p.sh, col, func() { set(col) })
		swatches = append(swatches, s)
		row.Add(s)
	}
	custom := newSwatch(p.sh, "#000000", func() { p.pickColor(accentPickerTitle, set) })
	custom.Hide()
	picker := newTipButton(p.sh, theme.ColorPaletteIcon(), "Pick any colour", func() { p.pickColor(accentPickerTitle, set) })
	row.Add(custom)
	row.Add(picker)
	sync := func() {
		c := p.sh.ui.ClusterSettings[ctx].AccentColor // the UI state's, so the swatch follows at once
		none.setSelected(c == "")
		for i, s := range swatches {
			s.setSelected(c == engine.IconColors[i])
		}
		if engine.IsCustomIconColor(c) {
			custom.color = c
			custom.Show()
			custom.setSelected(true)
			custom.Refresh()
		} else {
			custom.Hide()
		}
	}
	setting := p.setting(accentSetting, accentTitle, accentHelp, row)
	p.controls[accentNoneSwatchID] = none
	p.controls["accentCustom"] = custom
	p.controls["accentPicker"] = picker
	return setting, sync
}

// setAccentColor dispatches the user's accent colour for ctx ("" for none); a refusal shows under it.
func (p *settingsPage) setAccentColor(ctx, c string) {
	p.sh.dispatch("cluster.settings.setAccentColor", map[string]any{contextParam: ctx, "color": c},
		p.settled(accentSetting))
}

// pickColor opens the colour picker for a cluster colour picked freely (A93), the icon's or the
// accent; set receives it as "#rrggbb", which the window draws in a shade of its hue legible on
// each theme (identitycolor.go).
func (p *settingsPage) pickColor(title string, set func(hex string)) {
	d := dialog.NewColorPicker(title, freeColourHelp, func(c color.Color) {
		r, g, b, _ := c.RGBA()
		set(fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8))
	}, p.sh.win)
	d.Advanced = true
	p.sh.showDialog(d)
}

// noneSwatch is the accent row's None: an empty square crossed by a line, ringed while the cluster
// has no accent colour. Its tooltip is "None" (the swatch's, from its colour's name).
type noneSwatch struct{ swatch }

func newNoneSwatch(sh *shell, tap func()) *noneSwatch {
	n := &noneSwatch{swatch: swatch{sh: sh, color: noneSwatchName, onTap: tap}}
	n.ExtendBaseWidget(n)
	return n
}

// CreateRenderer implements fyne.Widget.
func (n *noneSwatch) CreateRenderer() fyne.WidgetRenderer {
	r := &noneSwatchRenderer{swatchRenderer: swatchRenderer{s: &n.swatch, ring: canvas.NewRectangle(color.Transparent),
		fill: canvas.NewRectangle(color.Transparent)}, slash: canvas.NewLine(color.Transparent)}
	r.Refresh()
	return r
}

type noneSwatchRenderer struct {
	swatchRenderer
	slash *canvas.Line
}

func (r *noneSwatchRenderer) Layout(size fyne.Size) {
	r.swatchRenderer.Layout(size)
	at, s := r.fill.Position(), r.fill.Size()
	r.slash.Position1 = fyne.NewPos(at.X+2, at.Y+s.Height-2)
	r.slash.Position2 = fyne.NewPos(at.X+s.Width-2, at.Y+2)
}

func (r *noneSwatchRenderer) Refresh() {
	th := r.s.Theme()
	muted := themeColor(th, colorMuted)
	r.fill.FillColor = color.Transparent
	r.fill.StrokeColor, r.fill.StrokeWidth = muted, 1
	r.fill.CornerRadius = th.Size(theme.SizeNameSelectionRadius) / 2
	r.slash.StrokeColor, r.slash.StrokeWidth = muted, 1.5
	r.ring.FillColor = color.Transparent
	r.ring.CornerRadius = th.Size(theme.SizeNameInputRadius)
	r.ring.StrokeWidth = 0
	switch {
	case r.s.selected:
		r.ring.StrokeColor, r.ring.StrokeWidth = themeColor(th, theme.ColorNameForeground), 2
	case r.s.hovered:
		r.ring.StrokeColor, r.ring.StrokeWidth = withAlpha(muted, 0x99), 2
	}
	r.Layout(r.s.Size())
	r.ring.Refresh()
	r.fill.Refresh()
	r.slash.Refresh()
}

func (r *noneSwatchRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.ring, r.fill, r.slash}
}
