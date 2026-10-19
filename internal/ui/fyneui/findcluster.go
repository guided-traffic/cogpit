package fyneui

import (
	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// connectCommand is the registry's command whose label and shortcut (⌘P) the window's ways to
// choose a cluster carry; in the window it shows the section, agents run it with a context.
const connectCommand = "cluster.connect"

// connectLabel is cluster.connect's label in the registry (commands/cluster.go), the name the
// Cluster menu, the palette and the workspace's button give it (D-57, I6); a build without the
// command (a test fake) uses it as it is.
const connectLabel = "Connect to Cluster"

// connectAgainLabel is the button after a failed connect that connects its cluster again (D-97).
const connectAgainLabel = "Connect Again"

// Choosing a cluster (D-57, the owner's interview of 2026-10-15 evening): one list, the navigator's
// section (navigator_sections.go), instead of the context picker that listed the same clusters a
// second time. Every way that opened the picker shows the section with its search open and
// focused: ⌘P and the Cluster menu's Connect to Cluster… (cluster.connect's shortcut and label in
// the registry), the palette's Connect to Cluster, the workspace's button while no cluster is
// connected, and a --context that fails at start. The welcome page has no button of its own (the
// owner, 2026-10-16 evening, N122): its cluster rows and the section are the list. The workspace's
// stays: while it shows, the engine's scope is the section (syncNavScope: the shown cluster is not
// connected or connecting), but a navigator ⌘B hid shows nothing, and the button shows it first. ↵ drills into the
// first match, connecting it as the section does (its login program shown for approval first);
// Escape on the empty search goes back to the tree of the cluster ⌘P came from (navHeader.escape).
//
// What is engine state and what is the window's (the lead's night decision, 2026-10-15, for the
// owner to confirm; D-57): the navigator shown (ui.toggleNavigator), the section
// (ui.setNavigatorScope) and the search's text (ui.setNavigatorSearch, D-51) are engine state, set
// through their commands as every other way sets them; the search field's keyboard focus and the
// cluster ⌘P came from are the window's, as the palette's focus is: they belong to the keystroke,
// an agent drives the same state with the three commands, and the cluster to go back to is the
// connected one, which ui_state names.

// connectMenuItem is the Cluster menu's Connect to Cluster… with ⌘P: the registry's label and
// shortcut of cluster.connect, the section in the window (findCluster).
func (sh *shell) connectMenuItem() *fyne.MenuItem {
	label := connectLabel
	if c, ok := sh.eng.Registry().Get(connectCommand); ok {
		label = c.Label
	}
	return sh.withShortcut(fyne.NewMenuItem(label+"…", sh.findCluster), connectCommand)
}

// findCluster shows the navigator's section with its search open and focused.
func (sh *shell) findCluster() {
	s := sh.side
	from := sh.ui.ScopeCluster()
	if from == "" && s.head.searching() {
		from = s.findFrom // ⌘P again on its own search: Escape still goes back where the first came from
	}
	show := func() {
		sh.showSection(func() {
			if !s.section || sh.ui.NavigatorHidden || sh.ui.Settings != nil {
				return // the engine refused, or another change came between
			}
			s.head.openSearch(s.filter)
			s.findFrom = from
		})
	}
	if sh.palette.visible() { // ⌘P over the palette: the section takes its place once it closed,
		sh.palette.dismissThen(show) // so that its closing does not take the search's focus
		return
	}
	show()
}

// showSection shows the navigator's section and runs then once the window shows the engine's
// answer: a settings page closes first (it takes the navigator's place; settings.close, as Escape
// does), the navigator shows if ⌘B hid it (ui.toggleNavigator), the section if a cluster's tree
// shows (ui.setNavigatorScope). Each step is dispatched only while it is needed, one after the
// other: the engine emits its state before a dispatch returns, and the window applies it before the
// dispatch's callback runs, so the next step sees the last one's result.
func (sh *shell) showSection(then func()) {
	open := true
	steps := []struct {
		id     string
		params any
		needed func() bool
	}{
		{"settings.close", struct{}{}, func() bool { return sh.ui.Settings != nil }},
		{"ui.toggleNavigator", layoutToggleParams{Open: &open}, func() bool { return sh.ui.NavigatorHidden }},
		{"ui.setNavigatorScope", navScopeParams{Scope: engine.NavScopeLocal}, func() bool { return sh.ui.ScopeCluster() != "" }},
	}
	var run func(i int)
	run = func(i int) {
		for i < len(steps) && !steps[i].needed() {
			i++
		}
		if i == len(steps) {
			then()
			return
		}
		sh.dispatch(steps[i].id, steps[i].params, func(_ any, err error) {
			sh.showError(err)
			run(i + 1)
		})
	}
	run(0)
}
