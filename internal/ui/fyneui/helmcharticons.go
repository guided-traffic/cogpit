package fyneui

import (
	"context"
	"crypto/sha256"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// Chart icons in the window (D-46, decided by the owner 2026-10-14; HELM2;
// engine/helmicons.go): the Charts view's first column and a chart drawer's header show the icon
// the chart's repository index names. The window asks the engine for an icon only when a cell or
// the drawer shows it, and not at all while Preferences › Kubernetes › Load chart icons is off;
// until it arrives, and when there is none or it failed, Cogpit's own helm-wheel glyph shows
// (Cogpit's drawing, not Helm's logo). An SVG icon is rasterised to a PNG before the window sees
// it (helmcharticons_svg.go, H-88). Fetched icons are kept for the window's life; turning the
// preference off shows the glyph everywhere at once.

// chartIconSource fetches a chart's icon: the real engine (*engine.Session). A test fake without
// it draws the glyph.
type chartIconSource interface {
	ChartIcon(ctx context.Context, iconURL string) (data []byte, format string, err error)
}

// chartIconCache is what the window holds of the charts' icons.
type chartIconCache struct {
	res     map[string]fyne.Resource // by URL; nil for one that failed
	loading map[string]bool
	on      bool // Load chart icons, as last applied
	glyph   fyne.Resource
}

// chartIconMaxSide is the largest a chart's icon is drawn in a table row, in points; the drawer's
// header draws it at chartIconHeaderSide.
const (
	chartIconMaxSide    = 22
	chartIconHeaderSide = 28
)

// chartIconSide is a chart icon's side in a row of the given height.
func chartIconSide(rowHeight float32) float32 { return max(min(rowHeight-8, chartIconMaxSide), 12) }

// helmWheel is Cogpit's own glyph of a chart without an icon: a ship's wheel (a helm).
var helmWheel = fyne.NewStaticResource("cogpit-helm-wheel.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
	`<path d="M4.6 12a7.4 7.4 0 1 0 14.8 0a7.4 7.4 0 1 0 -14.8 0zM6.3 12a5.7 5.7 0 1 1 11.4 0a5.7 5.7 0 1 1 -11.4 0z"/>`+
	`<path d="M9.9 12a2.1 2.1 0 1 0 4.2 0a2.1 2.1 0 1 0 -4.2 0z"/>`+
	`<path d="M17.9 12.65L17.9 11.35L13.5 11.35L13.5 12.65z"/>`+
	`<path d="M10.5 12.65L10.5 11.35L6.1 11.35L6.1 12.65z"/><path d="M22 12.65L22 11.35L19.2 11.35L19.2 12.65z"/>`+
	`<path d="M4.8 12.65L4.8 11.35L2 11.35L2 12.65z"/><path d="M15.71 16.63L16.63 15.71L13.52 12.6L12.6 13.52z"/>`+
	`<path d="M10.48 11.4L11.4 10.48L8.29 7.37L7.37 8.29z"/>`+
	`<path d="M18.61 19.53L19.53 18.61L17.55 16.63L16.63 17.55z"/>`+
	`<path d="M6.45 7.37L7.37 6.45L5.39 4.47L4.47 5.39z"/><path d="M11.35 17.9L12.65 17.9L12.65 13.5L11.35 13.5z"/>`+
	`<path d="M11.35 10.5L12.65 10.5L12.65 6.1L11.35 6.1z"/><path d="M11.35 22L12.65 22L12.65 19.2L11.35 19.2z"/>`+
	`<path d="M11.35 4.8L12.65 4.8L12.65 2L11.35 2z"/><path d="M7.37 15.71L8.29 16.63L11.4 13.52L10.48 12.6z"/>`+
	`<path d="M12.6 10.48L13.52 11.4L16.63 8.29L15.71 7.37z"/>`+
	`<path d="M4.47 18.61L5.39 19.53L7.37 17.55L6.45 16.63z"/>`+
	`<path d="M16.63 6.45L17.55 7.37L19.53 5.39L18.61 4.47z"/>`+
	`<path d="M20.8 12a1.4 1.4 0 1 0 2.8 0a1.4 1.4 0 1 0 -2.8 0z"/>`+
	`<path d="M17.81 19.21a1.4 1.4 0 1 0 2.8 0a1.4 1.4 0 1 0 -2.8 0z"/>`+
	`<path d="M10.6 22.2a1.4 1.4 0 1 0 2.8 0a1.4 1.4 0 1 0 -2.8 0z"/>`+
	`<path d="M3.39 19.21a1.4 1.4 0 1 0 2.8 0a1.4 1.4 0 1 0 -2.8 0z"/>`+
	`<path d="M0.4 12a1.4 1.4 0 1 0 2.8 0a1.4 1.4 0 1 0 -2.8 0z"/>`+
	`<path d="M3.39 4.79a1.4 1.4 0 1 0 2.8 0a1.4 1.4 0 1 0 -2.8 0z"/>`+
	`<path d="M10.6 1.8a1.4 1.4 0 1 0 2.8 0a1.4 1.4 0 1 0 -2.8 0z"/>`+
	`<path d="M17.81 4.79a1.4 1.4 0 1 0 2.8 0a1.4 1.4 0 1 0 -2.8 0z"/>`+
	`</svg>`))

// chartIconsOn reports whether the Preferences let the window fetch chart icons (on without
// preferences, as by default).
func (sh *shell) chartIconsOn() bool {
	return sh.ui.Preferences == nil || sh.ui.Preferences.LoadChartIcons
}

// chartGlyph is the glyph in the muted colour.
func (sh *shell) chartGlyph() fyne.Resource {
	if sh.charts.glyph == nil {
		sh.charts.glyph = themedIcon(helmWheel, colorMuted)
	}
	return sh.charts.glyph
}

// chartIconResource returns a chart icon to draw: the fetched image, else the glyph. The first
// call for a URL asks the engine off the main goroutine; the cells and the drawer that show it are
// drawn again once it arrived. Main goroutine.
func (sh *shell) chartIconResource(iconURL string) fyne.Resource {
	if iconURL == "" || !sh.chartIconsOn() {
		return sh.chartGlyph()
	}
	if r, ok := sh.charts.res[iconURL]; ok {
		if r == nil {
			return sh.chartGlyph()
		}
		return r
	}
	src, ok := sh.eng.(chartIconSource)
	if !ok || sh.charts.loading[iconURL] {
		return sh.chartGlyph()
	}
	if sh.charts.loading == nil {
		sh.charts.loading = map[string]bool{}
	}
	sh.charts.loading[iconURL] = true
	ctx := sh.ctx
	go func() {
		defer sh.recoverTo("chart icon")
		data, format, err := src.ChartIcon(ctx, iconURL)
		if err == nil && format == "svg" { // drawn here, never by the window's renderer (H-88)
			data, err = rasteriseSVG(data)
			format = "png"
		}
		sh.do(func() {
			delete(sh.charts.loading, iconURL)
			if ctx.Err() != nil {
				return
			}
			if sh.charts.res == nil {
				sh.charts.res = map[string]fyne.Resource{}
			}
			var r fyne.Resource
			if err == nil && len(data) > 0 {
				sum := sha256.Sum256([]byte(iconURL))
				r = fyne.NewStaticResource(fmt.Sprintf("%x.%s", sum[:8], format), data)
			}
			sh.charts.res[iconURL] = r
			sh.chartIconsChanged(iconURL)
		})
	}()
	return sh.chartGlyph()
}

// chartIconsChanged draws again what shows the icon of a URL ("" every icon): the table's cells
// and the drawer's header.
func (sh *shell) chartIconsChanged(iconURL string) {
	if sh.view != nil {
		for _, c := range sh.view.cells {
			if c.content.kind == cellImage && (iconURL == "" || c.content.text == iconURL) {
				c.apply()
			}
		}
	}
	d := sh.drawer
	if d != nil && d.shown != nil && d.last != nil && d.shown.Kind == helmChartsKind {
		d.showHeader(*d.shown, d.last)
	}
}

// chartIconsPrefChanged follows Preferences › Load chart icons: on, the icons fetched before show
// again and the rest are asked for; off, the glyph shows everywhere.
func (sh *shell) chartIconsPrefChanged() {
	if on := sh.chartIconsOn(); on != sh.charts.on {
		sh.charts.on = on
		sh.chartIconsChanged("")
	}
}

// chartIconImage is the drawer header's icon of one chart.
type chartIconImage struct {
	img *canvas.Image
	url string
	res fyne.Resource
}

// chartHeaderIcon returns the drawer's icon of the chart whose icon URL is iconURL, kept across
// rebuilds and drawn again when the icon arrives.
func chartHeaderIcon(d *drawer, iconURL string) fyne.CanvasObject {
	ci, _ := d.kept[chartIconKey].(*chartIconImage)
	if ci == nil {
		ci = &chartIconImage{img: canvas.NewImageFromResource(nil)}
		ci.img.FillMode = canvas.ImageFillContain
		ci.img.SetMinSize(fyne.NewSquareSize(chartIconHeaderSide))
		d.kept[chartIconKey] = ci
	}
	res := d.sh.chartIconResource(iconURL)
	if ci.url != iconURL || ci.res != res {
		ci.url, ci.res = iconURL, res
		ci.img.Resource = res
		ci.img.Refresh()
	}
	return ci.img
}

// chartIconKey is where the drawer keeps a chart's header icon.
const chartIconKey = "helm.chartIcon"

// chartIconsSetting returns Preferences › Kubernetes › Load chart icons and the function that
// shows the preference in it.
func (p *settingsPage) chartIconsSetting() (fyne.CanvasObject, func()) {
	check := newLabelledSwitch("Load the icon a chart repository names, from the host that serves it", func(on bool) {
		p.setPreference("loadChartIcons", on)
	})
	sync := func() { check.SetChecked(p.prefs().LoadChartIcons) }
	return p.setting("loadChartIcons", "Load chart icons",
		"Each icon comes from its own host, which learns that you looked at the chart; off: Cogpit's glyph.", check), sync
}
