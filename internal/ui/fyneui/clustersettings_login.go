package fyneui

import (
	"fyne.io/fyne/v2"
)

// Cluster Settings › General › Login while not shown (D-69 item 4; engine/connwatch_lapse.go).
// Cogpit's own: Lens's captured Cluster Settings page has no such setting. On by default: a cluster
// connected but not shown renews its login as the shown one does, its credential plugin running
// when the login expires (a browser login included). Off: while the cluster is not shown its
// plugin does not run; an expired login lapses, grey rather than red, until the cluster is shown or
// logged in again.

const (
	renewLoginTitle = "Login while not shown"
	renewLoginCheck = "Renew the login while the cluster is not shown"
	renewLoginHelp  = "Off: no login program runs for this cluster while another one is shown; an expired login waits until you show it."
)

// clusterLoginSetting returns the row and the function that shows the setting in it, from the UI
// state's cluster settings, so that it follows a change at once.
func (p *settingsPage) clusterLoginSetting() (fyne.CanvasObject, func()) {
	check := newLabelledSwitch(renewLoginCheck, func(on bool) { p.setCluster("renewLoginInBackground", on) })
	ctx := p.shown.Context
	sync := func() { check.SetChecked(!p.sh.ui.ClusterSettings[ctx].HoldBackgroundLogin) }
	return p.setting("renewLoginInBackground", renewLoginTitle, renewLoginHelp, check), sync
}
