package fyneui

import (
	"fyne.io/fyne/v2"
)

// Preferences › App › Back references (the owner's request, D-152; engine.Preferences.BackReferences).
// Off by default; on, a drawer of a kind something can point at ends with Referenced By
// (kinds.BackRefSection), which the drawer draws as any table of links.

const (
	backRefsTitle = "Back references"
	backRefsCheck = "Show what points at an object in its drawer"
	backRefsHelp  = "On: a drawer ends with Referenced By, the objects that select it by label or name it: a pod's Services, a ConfigMap's pods, a role's bindings."
)

// backRefsSetting returns the row and the function that shows the preference in it.
func (p *settingsPage) backRefsSetting() (fyne.CanvasObject, func()) {
	check := newLabelledSwitch(backRefsCheck, func(on bool) { p.setPreference("backReferences", on) })
	sync := func() { check.SetChecked(p.prefs().BackReferences) }
	return p.setting("backReferences", backRefsTitle, backRefsHelp, check), sync
}
