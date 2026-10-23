package fyneui

import (
	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A tab comes back as it was left (the owner, 2026-10-23: "Tabs speichern beim Umschalten zwischen
// Tabs immer ihren letzten Zustand. Wenn ich von einem Cluster wieder auf einen anderen Cluster
// wechsel, und zurück komme in meinem floating tab, dann ist er noch exakt so wie als ich ihn
// verlassen habe. Gleich Scroll-position, gleiche filter."). The engine keeps a tab's filters,
// search, sort, status filter, selection and drawer (engine/tabs.go); the window keeps where the
// tab scrolled its list, which the window alone knows.
//
// The lead's decisions (D-74, open to revision):
//   - per tab: leaving a tab (another tab, another cluster's, the welcome page) keeps its list's
//     offset with its cluster and view; showing that tab again with the same view scrolls back
//     there. A view the tab opens anew, back and forward in its history included, starts at its
//     top: the filters it comes with are new too;
//   - the rows of a tab shown again may come after it (a cluster shown again from its cache, a list
//     not synced yet): it scrolls back once they are in, as far as they reach, and again with each
//     snapshot until its list is synced; a scroll of the user's own ends the wait;
//   - in this run only: the workspace file keeps no offsets, as it keeps no selection.

// shownList names the list the table shows: the active tab's, its cluster and view.
type shownList struct{ tab, context, view string }

// tabScroll is where a tab left its list.
type tabScroll struct {
	context, view string
	offset        fyne.Position
}

// shownOf names the list a snapshot shows in the active tab; none for no snapshot.
func (tv *tableView) shownOf(snap *engine.TableSnapshot) shownList {
	if snap == nil {
		return shownList{}
	}
	return shownList{tab: tv.sh.ui.ActiveTab, context: snap.Context, view: snap.View}
}

// own reports whether a list is its tab's own, the tab's cluster and view as the engine has them:
// between a tab switch and its rows the table can show a moment's mix, a tab with the list of the
// one before it, which is neither kept nor brought back.
func (tv *tableView) own(s shownList) bool {
	t, ok := tv.sh.ui.Tab(s.tab)
	return ok && s.tab != "" && t.Context == s.context && t.View == s.view
}

// leaveTab keeps where the tab shown so far left its list: its offset, or where it was still
// scrolling back to. The tabs closed since are forgotten.
func (tv *tableView) leaveTab() {
	left := tv.shows
	if !tv.own(left) || tv.table.body == nil {
		return
	}
	at := tv.table.body.Offset
	if tv.back != nil {
		at = *tv.back
	}
	if tv.scrolls == nil {
		tv.scrolls = make(map[string]tabScroll)
	}
	tv.scrolls[left.tab] = tabScroll{context: left.context, view: left.view, offset: at}
	for id := range tv.scrolls {
		if _, ok := tv.sh.ui.Tab(id); !ok {
			delete(tv.scrolls, id)
		}
	}
}

// enterTab notes where the tab shown now left its list, when it shows the same view of the same
// cluster again; scrollBack goes there.
func (tv *tableView) enterTab() {
	tv.back = nil
	s, ok := tv.scrolls[tv.shows.tab]
	if ok && tv.own(tv.shows) && s.context == tv.shows.context && s.view == tv.shows.view && s.offset != (fyne.Position{}) {
		at := s.offset
		tv.back = &at
	}
}

// scrollBack scrolls a tab shown again to where it left its list, as far as its rows reach, and
// stops waiting once it got there or its list is synced.
func (tv *tableView) scrollBack() {
	b := tv.table.body
	if tv.back == nil || tv.snap == nil || len(tv.snap.Rows) == 0 || b == nil || b.Content == nil || b.Size().Height <= 0 {
		return
	}
	want := *tv.back
	last := max(b.Content.MinSize().Height-b.Size().Height, 0)
	tv.backing = true
	tv.table.ScrollToOffset(fyne.NewPos(want.X, min(want.Y, last)))
	tv.backing = false
	tv.table.offset = b.Offset
	if want.Y <= last || tv.snap.Synced {
		tv.back = nil
	}
}
