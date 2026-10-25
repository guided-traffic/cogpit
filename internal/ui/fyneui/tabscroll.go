package fyneui

import (
	"slices"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A tab comes back as it was left (the owner, 2026-10-23: "Tabs speichern beim Umschalten zwischen
// Tabs immer ihren letzten Zustand. Wenn ich von einem Cluster wieder auf einen anderen Cluster
// wechsel, und zurück komme in meinem floating tab, dann ist er noch exakt so wie als ich ihn
// verlassen habe. Gleich Scroll-position, gleiche filter."), and so does a place of its history
// that back or forward brings back (the owner's finding of 2026-10-25: "beim zurück navigieren
// möchte ich auch, dass der Drawer, Scrollpunkt und Filter wiederhergestellt werden"). The engine
// keeps a tab's filters, search, sort, status filter, selection and drawer, and each place of its
// history with its own (engine/tabs.go); the window keeps where each place scrolled its list,
// which the window alone knows, by the place's name (engine.ViewTab.Place).
//
// The lead's decisions (D-74, open to revision):
//   - per place: leaving a place (another tab, another cluster's, the welcome page, another view
//     or a step within the view in the same tab) keeps its list's offset with its cluster and
//     view; showing that place again (the tab shown again, back or forward) scrolls back there. A
//     view the tab opens anew is a new place and starts at its top (the owner's VW15 of
//     2026-10-25); a place no tab holds any more, in its history or shown, is forgotten
//     (forgetGone);
//   - the drawer of a place brought back does not scroll its row into view: the place's offset is
//     where the user left the list with that drawer open;
//   - the rows of a place shown again may come after it (a cluster shown again from its cache, a
//     list not synced yet): it scrolls back once they are in, as far as they reach, and again with
//     each snapshot until its list is synced; a scroll of the user's own ends the wait;
//   - the workspace file keeps no offsets, as it keeps no selection and no place's drawer.

// shownList names the list the table shows: the active tab's, its cluster and view, and the place
// of the tab it is (engine.ViewTab.Place).
type shownList struct {
	tab, context, view string
	place              uint64
}

// list is the list without its place: another tab, cluster or view is another list, a view
// entered; another place on the same list is a step within the view.
func (s shownList) list() shownList {
	s.place = 0
	return s
}

// tabScroll is where a place left its list.
type tabScroll struct {
	context, view string
	offset        fyne.Position
}

// shownOf names the list a snapshot shows in the active tab; none for no snapshot.
func (tv *tableView) shownOf(snap *engine.TableSnapshot) shownList {
	if snap == nil {
		return shownList{}
	}
	s := shownList{tab: tv.sh.ui.ActiveTab, context: snap.Context, view: snap.View}
	if t, ok := tv.sh.ui.Tab(s.tab); ok {
		s.place = t.Place
	}
	return s
}

// placeView returns the view of a place of the tab: the one it shows, or one of its history.
func placeView(t engine.ViewTab, place uint64) (string, bool) {
	if place == 0 {
		return "", false
	}
	if t.Place == place {
		return t.View, true
	}
	for _, h := range [][]engine.TabPlace{t.Back, t.Forward} {
		if i := slices.IndexFunc(h, func(p engine.TabPlace) bool { return p.Place == place }); i >= 0 {
			return h[i].View, true
		}
	}
	return "", false
}

// own reports whether a list is a place of its tab, the tab's cluster and the place's view as the
// engine has them: between a tab switch (or a view opened) and its rows the table can show a
// moment's mix, a place with the list of the one before it, which is neither kept nor brought back.
func (tv *tableView) own(s shownList) bool {
	t, ok := tv.sh.ui.Tab(s.tab)
	if !ok || s.tab == "" || t.Context != s.context {
		return false
	}
	view, ok := placeView(t, s.place)
	return ok && view == s.view
}

// leaveTab keeps where the place shown so far left its list: its offset, or where it was still
// scrolling back to. The places no tab holds any more are forgotten first (forgetGone).
func (tv *tableView) leaveTab() {
	tv.forgetGone()
	left := tv.shows
	if !tv.own(left) || tv.table.body == nil {
		return
	}
	at := tv.table.body.Offset
	if tv.back != nil {
		at = *tv.back
	}
	if tv.scrolls == nil {
		tv.scrolls = make(map[uint64]tabScroll)
	}
	tv.scrolls[left.place] = tabScroll{context: left.context, view: left.view, offset: at}
}

// forgetGone forgets where the places no tab holds any more left their lists: their tab closed,
// or its history dropped them (a view opened after back, the oldest beyond the history's bound).
func (tv *tableView) forgetGone() {
	if len(tv.scrolls) == 0 {
		return
	}
	held := make(map[uint64]bool)
	for _, t := range tv.sh.ui.Tabs {
		held[t.Place] = true
		for _, h := range [][]engine.TabPlace{t.Back, t.Forward} {
			for _, p := range h {
				held[p.Place] = true
			}
		}
	}
	for place := range tv.scrolls {
		if !held[place] {
			delete(tv.scrolls, place)
		}
	}
}

// enterTab notes where the place shown now left its list, when it is a place of its tab shown
// again; scrollBack goes there. The drawer the place comes back with stays where the place left
// it: its row is not scrolled into view (keep).
func (tv *tableView) enterTab() {
	tv.back, tv.keep = nil, nil
	s, ok := tv.scrolls[tv.shows.place]
	if !ok || !tv.own(tv.shows) || s.context != tv.shows.context || s.view != tv.shows.view {
		return
	}
	at := s.offset
	tv.back = &at
	if d := tv.sh.ui.Drawer; d != nil {
		ref := *d
		tv.keep = &ref
		tv.pending = nil
	}
}

// kept reports whether the drawer is the one the place brought back came with, whose row stays
// where the place left it; it is asked once.
func (tv *tableView) kept(d *model.ObjectRef) bool {
	k := tv.keep
	tv.keep = nil
	return d != nil && k != nil && *d == *k
}

// scrollBack scrolls a place shown again to where it left its list, as far as its rows reach, and
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

// activePlace is the place the active tab shows; 0 for none.
func activePlace(u engine.UIState) uint64 {
	if t, ok := u.Tab(u.ActiveTab); ok {
		return t.Place
	}
	return 0
}
