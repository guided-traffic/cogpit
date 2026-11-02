package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// Preferences › Tables › Free width (engine.Preferences.TableSpreadWidth, D-145): on by default, a
// table that opens gives the width its columns leave free to the columns whose content is cut
// before Name takes the rest (tablespread.go); off, Name takes all of it.

const (
	spreadWidthTitle = "Free width"
	spreadWidthCheck = "Give free width to the columns that are cut first"
	spreadWidthHelp  = "When a table opens, cut columns widen to show their text before Name takes the rest; not again while it stays open."
)

// spreadWidthSetting returns the row and the function that shows the preference in it.
func (p *settingsPage) spreadWidthSetting() (fyne.CanvasObject, func()) {
	check := widget.NewCheck(spreadWidthCheck, func(on bool) { p.setPreference("tableSpreadWidth", on) })
	sync := func() { check.SetChecked(p.prefs().TableSpreadWidth) }
	return p.setting("tableSpreadWidth", spreadWidthTitle, spreadWidthHelp, check), sync
}
