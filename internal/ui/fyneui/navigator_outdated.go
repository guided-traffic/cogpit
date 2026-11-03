package fyneui

import (
	"slices"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Hide outdated objects (the owner, 2026-11-03; engine.Preferences.HideOutdatedObjects, on by
// default): the navigator leaves out the kinds Kubernetes has a successor for
// (engine.OutdatedKinds: Replication Controllers, Endpoints). Hidden means only the navigator's
// tree: its search still finds them (typing "endpoints" shows the entry), and the palette, the View
// menu's kinds, links, open tabs and ui.openView reach them as before. The View menu's Hide
// Outdated Objects, checked while on, and Preferences › App switch it, both through
// preferences.set.

const (
	outdatedMenuLabel = "Hide Outdated Objects"
	outdatedTitle     = "Outdated objects"
	outdatedCheck     = "Hide outdated objects in the navigator"
	outdatedHelp      = "On: Replication Controllers and Endpoints, which Deployments and EndpointSlices replace, leave the navigator; its search, ⌘K and links still find them."
)

// hideOutdated reports whether the preference is on; before the first preferences, as by default.
func (sh *shell) hideOutdated() bool {
	p := sh.ui.Preferences
	return p == nil || p.HideOutdatedObjects
}

// outdatedHidden reports whether the tree leaves a node out: an outdated kind while the preference
// is on and no search is.
func (s *sidebar) outdatedHidden(id string) bool {
	return s.filter == "" && s.sh.hideOutdated() && slices.Contains(engine.OutdatedKinds, id)
}

// outdatedChanged lays the tree out again when the preference changed (preferencesChanged).
func (s *sidebar) outdatedChanged() {
	if on := s.sh.hideOutdated(); on != s.hidingOutdated {
		s.hidingOutdated = on
		s.selectView(s.sh.ui.View) // the open view's mark, on a row that came or went; it rebuilds
	}
}

// outdatedMenuItem is the View menu's Hide Outdated Objects, checked while on.
func (sh *shell) outdatedMenuItem() *fyne.MenuItem {
	on := sh.hideOutdated()
	item := fyne.NewMenuItem(outdatedMenuLabel, func() {
		sh.dispatch("preferences.set", map[string]any{"hideOutdatedObjects": !on}, nil)
	})
	item.Checked = on
	return item
}

// outdatedSetting returns Preferences › App's row and the function that shows the preference in it.
func (p *settingsPage) outdatedSetting() (fyne.CanvasObject, func()) {
	check := newLabelledSwitch(outdatedCheck, func(on bool) { p.setPreference("hideOutdatedObjects", on) })
	sync := func() { check.SetChecked(p.prefs().HideOutdatedObjects) }
	return p.setting("hideOutdatedObjects", outdatedTitle, outdatedHelp, check), sync
}
