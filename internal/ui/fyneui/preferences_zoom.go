package fyneui

import (
	"runtime"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Preferences › App › Zoom (D-134, engine.Preferences.ZoomLevel): the window's zoom, the same value
// the status bar's magnifiers and VS Code's keys step (the owner's answer of 2026-10-26: one value
// that stays, which Preferences shows and changes too). Offered as the levels' percentages.

const zoomTitle = "Zoom"

// zoomHelp names the keys as the platform writes them.
func zoomHelp(sh *shell) string {
	keys := func(id string) string {
		if c, ok := sh.eng.Registry().Get(id); ok {
			return shortcutText(c.Shortcut, runtime.GOOS == goosMac)
		}
		return ""
	}
	return "The whole window, as VS Code zooms: " + keys("ui.zoom.in") + " larger, " + keys("ui.zoom.out") +
		" smaller, " + keys("ui.zoom.reset") + " back to 100%, or the magnifiers in the status bar."
}

// zoomSetting returns the row and the function that shows the preference in it.
func (p *settingsPage) zoomSetting() (fyne.CanvasObject, func()) {
	var labels []string
	for l := engine.MinZoomLevel; l <= engine.MaxZoomLevel; l++ {
		labels = append(labels, engine.ZoomPercent(l))
	}
	sel := newSelectField(labels, func(label string) {
		for l := engine.MinZoomLevel; l <= engine.MaxZoomLevel; l++ {
			if engine.ZoomPercent(l) == label {
				p.setPreference("zoomLevel", l)
			}
		}
	})
	sync := func() { sel.SetSelected(engine.ZoomPercent(p.prefs().ZoomLevel)) }
	return p.setting("zoomLevel", zoomTitle, zoomHelp(p.sh), sel), sync
}
