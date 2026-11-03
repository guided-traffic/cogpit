package fyneui

import (
	"fyne.io/fyne/v2"
)

// Preferences › App › Restore the last session (D-69 item 8; engine/restore.go). Cogpit's own: no
// such switch is known of Lens's preferences (inferred, not captured). Off by default (the
// owner's decision of 2026-10-16): Cogpit then starts without tabs and without connections. On,
// the next start restores the tabs and connects the clusters connected at quit, the one shown then
// first; the help line says that their login programs run as on a connect.

const (
	restoreSessionTitle = "Restore the last session"
	restoreSessionCheck = "Reopen the tabs and connect again to the clusters connected when Cogpit quit"
	restoreSessionHelp  = "Their login programs run as on any connect; the cluster shown at quit comes first. Off: no tabs, nothing connected."
)

// restoreSessionSetting returns the row and the function that shows the preference in it.
func (p *settingsPage) restoreSessionSetting() (fyne.CanvasObject, func()) {
	check := newLabelledSwitch(restoreSessionCheck, func(on bool) { p.setPreference("restoreSession", on) })
	sync := func() { check.SetChecked(p.prefs().RestoreSession) }
	return p.setting("restoreSession", restoreSessionTitle, restoreSessionHelp, check), sync
}
