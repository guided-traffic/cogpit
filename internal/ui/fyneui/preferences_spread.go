package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// Preferences › Tables › Free width (engine.Preferences.TableSpreadWidth, D-145): on by default, a
// table that opens gives the width its columns leave free to the columns whose content is cut
// before Name takes the rest (tablespread.go); off, Name takes all of it. Its exceptions
// (TableSpreadExceptLabels, TableSpreadExceptSelectors, D-153), on by default: a column of labels
// or of a selector keeps its own width when shown; they wait while Free width is off.

const (
	spreadWidthTitle = "Free width"
	spreadWidthCheck = "Give free width to the columns that are cut first"
	spreadWidthHelp  = "When a table opens, cut columns widen to show their text before Name takes the rest; not again while it stays open."

	spreadExceptTitle     = "Except"
	spreadExceptLabels    = "Labels"
	spreadExceptSelectors = "Selectors"
	spreadExceptHelp      = "These columns keep their own width when you show them: Labels in Namespaces and Secrets, " +
		"Selector in Services and Replication Controllers, Node Selector in Daemon Sets. They are hidden by default; " +
		"the drawer lists them."
)

// spreadWidthSetting returns the rows of Free width and its exceptions, and the function that
// shows the preferences in them.
func (p *settingsPage) spreadWidthSetting() (fyne.CanvasObject, func()) {
	check := newLabelledSwitch(spreadWidthCheck, func(on bool) { p.setPreference("tableSpreadWidth", on) })
	labels := newLabelledSwitch(spreadExceptLabels, func(on bool) { p.setPreference("tableSpreadExceptLabels", on) })
	selectors := newLabelledSwitch(spreadExceptSelectors, func(on bool) { p.setPreference("tableSpreadExceptSelectors", on) })
	sync := func() {
		pr := p.prefs()
		check.SetChecked(pr.TableSpreadWidth)
		labels.SetChecked(pr.TableSpreadExceptLabels)
		selectors.SetChecked(pr.TableSpreadExceptSelectors)
		for _, s := range []*pillSwitch{labels, selectors} {
			if pr.TableSpreadWidth {
				s.Enable()
			} else {
				s.Disable() // without free width there is nothing to except
			}
		}
	}
	return container.NewVBox(
		p.setting("tableSpreadWidth", spreadWidthTitle, spreadWidthHelp, check),
		p.setting("tableSpreadExcept", spreadExceptTitle, spreadExceptHelp, container.NewVBox(labels, selectors)),
	), sync
}
