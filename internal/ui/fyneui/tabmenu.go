package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// A tab's right-click menu, in the tab bar and in the dock (engine/tabclose.go, D-144): Close,
// Close Others (greyed while the bar shows no other tab) and Close All, each its bar's command.

var (
	_ fyne.SecondaryTappable = (*viewTab)(nil)
	_ fyne.SecondaryTappable = (*tabButton)(nil)
)

// showTabMenu pops a tab's menu up at pos (window coordinates) and returns it.
func (sh *shell) showTabMenu(pos fyne.Position, others bool, closeOne, closeOthers, closeAll func()) *fyne.Menu {
	sh.tip.hide()
	other := fyne.NewMenuItem("Close Others", closeOthers)
	other.Disabled = !others
	m := fyne.NewMenu("", fyne.NewMenuItem("Close", closeOne), other, fyne.NewMenuItem("Close All", closeAll))
	widget.NewPopUpMenu(m, sh.win.Canvas()).ShowAtPosition(pos)
	return m
}

// TappedSecondary implements fyne.SecondaryTappable: the tab's menu.
func (t *viewTab) TappedSecondary(ev *fyne.PointEvent) {
	b, id := t.bar, t.tab.ID
	b.menuModel = b.sh.showTabMenu(ev.AbsolutePosition, len(b.shown) > 1,
		func() { b.sh.dispatch("ui.tab.close", tabIDParams{ID: id}, nil) },
		func() { b.sh.dispatch("ui.tab.closeOthers", tabIDParams{ID: id}, nil) },
		func() { b.sh.dispatch("ui.tab.closeAll", struct{}{}, nil) })
}

// TappedSecondary implements fyne.SecondaryTappable: the tab's menu.
func (b *tabButton) TappedSecondary(ev *fyne.PointEvent) {
	if b.onMenu != nil {
		b.onMenu(ev.AbsolutePosition)
	}
}

// tabMenu shows the menu of the dock's tab id at pos.
func (d *dock) tabMenu(id string, pos fyne.Position) {
	sh := d.sh
	d.menuModel = sh.showTabMenu(pos, len(d.tabs) > 1,
		func() { sh.dispatch("dock.close", dockParams{ID: id}, nil) },
		func() { sh.dispatch("dock.closeOthers", dockParams{ID: id}, nil) },
		func() { sh.dispatch("dock.closeAll", struct{}{}, nil) })
}
