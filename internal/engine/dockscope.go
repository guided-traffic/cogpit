package engine

import (
	"maps"
	"slices"
)

// The dock per cluster (D-69 item 9, stage 2): the dock shows the tabs of the cluster the window
// shows and the tabs of no cluster (the Audit Log, the Diagnostics), so that a terminal never
// stands beside another cluster's log. The other clusters' tabs stay in UIState.Dock with their
// streams running, hidden, and come back with what ran meanwhile when their cluster is shown again
// (as Lens). A local terminal belongs to the cluster it was opened for (DockTab.Context).
//
// Each cluster keeps its own active tab and whether its dock was open: ActiveDock and DockOpen are
// the shown cluster's, and the engine keeps every other cluster's while it is not shown
// (UIState.dockMemo), so that agents read in ui_state what the window shows: dock lists every tab
// with its context (dock_read finds a hidden one by its id), activeDock and dockOpen are the shown
// cluster's. The dock's height is the window's (a split it keeps for every cluster), as the panels'
// widths are (panelwidths.go).
//
// The rules (syncDockScope, which SyncDock runs after every change):
//   - showing another cluster keeps the left one's active tab and open state, and gives the shown
//     one its own back: the tab it had active (else its last tab), open as it was; a cluster shown
//     the first time keeps a tab of no cluster that was active, else shows its last tab, collapsed;
//   - a tab of a cluster not shown made active (an agent's pod_logs or terminal_open on a cluster
//     the window does not show) becomes that cluster's active tab, its dock open for when it is
//     shown; the shown dock keeps its tab;
//   - the active tab closing gives way to its nearest neighbour the dock shows, never to a hidden
//     cluster's tab;
//   - a hidden cluster's active tab closing gives way to its last tab; a cluster without tabs keeps
//     nothing.
//
// dock.activate of a hidden cluster's tab shows that cluster first (commands/dock.go), as
// ui.navigate shows the cluster of an object it opens.

// dockMemo is a cluster's dock while another one is shown: the tab it had active and whether it
// was open.
type dockMemo struct {
	active string
	open   bool
}

// InDockScope reports whether the dock shows a tab: one of the shown cluster's (Context), or one of
// no cluster; every tab while the window has no cluster (before the first connect).
func (u UIState) InDockScope(t DockTab) bool {
	return t.Context == "" || u.Context == "" || t.Context == u.Context
}

// DockInScope returns the tabs the dock shows, in their order (InDockScope).
func (u UIState) DockInScope() []DockTab {
	out := make([]DockTab, 0, len(u.Dock))
	for _, t := range u.Dock {
		if u.InDockScope(t) {
			out = append(out, t)
		}
	}
	return out
}

// inDockScope reports whether the dock shows the tab id; never for "" or a tab that is gone.
func (u *UIState) inDockScope(id string) bool {
	t, ok := u.DockTab(id)
	return ok && u.InDockScope(t)
}

// syncDockScope keeps the dock to the shown cluster's tabs after a change from old (see the rules
// above). SyncDock runs it first.
func (u *UIState) syncDockScope(old UIState) {
	memo := u.dockMemo
	edited := false
	edit := func() map[string]dockMemo {
		if !edited {
			memo, edited = maps.Clone(memo), true // the old state may share the map
			if memo == nil {
				memo = map[string]dockMemo{}
			}
		}
		return memo
	}
	if u.Context != old.Context {
		m := edit()
		if old.Context != "" {
			m[old.Context] = dockMemo{active: old.ActiveDock, open: old.DockOpen}
		}
		back, ok := m[u.Context]
		delete(m, u.Context)
		if u.ActiveDock == old.ActiveDock || !u.inDockScope(u.ActiveDock) { // unless the change chose the tab itself
			u.ActiveDock, u.DockOpen = u.shownDock(back, ok, old)
		}
	}
	if t, ok := u.DockTab(u.ActiveDock); ok && !u.InDockScope(t) {
		if old.ActiveDock != "" && !hasDockTab(u.Dock, old.ActiveDock) {
			u.ActiveDock = u.neighbourInScope(old) // the active tab closed: a tab the dock shows takes its place
		} else {
			edit()[t.Context] = dockMemo{active: t.ID, open: true} // a hidden cluster's tab, for when it is shown
			u.ActiveDock = old.ActiveDock
			if !u.inDockScope(u.ActiveDock) {
				u.ActiveDock = u.lastInScope()
			}
		}
	}
	for ctx, m := range memo {
		if hasDockTab(u.Dock, m.active) {
			continue
		}
		last := u.lastOf(ctx)
		switch {
		case last != "":
			edit()[ctx] = dockMemo{active: last, open: m.open}
		default:
			delete(edit(), ctx)
		}
	}
	if len(memo) == 0 {
		memo = nil
	}
	u.dockMemo = memo
}

// shownDock is the active tab and open state of the dock when its cluster is shown: the ones it
// had (back, when ok), else a tab of no cluster that stays active, else its last tab, collapsed.
func (u *UIState) shownDock(back dockMemo, ok bool, old UIState) (string, bool) {
	last := u.lastInScope()
	switch {
	case last == "":
		return "", false
	case ok && u.inDockScope(back.active):
		return back.active, back.open
	case ok:
		return last, back.open
	case u.inDockScope(old.ActiveDock): // a tab of no cluster, which every cluster's dock shows
		return old.ActiveDock, old.DockOpen
	}
	return last, false
}

// lastInScope is the id of the last tab the dock shows, "" for none.
func (u *UIState) lastInScope() string {
	for i := len(u.Dock) - 1; i >= 0; i-- {
		if u.InDockScope(u.Dock[i]) {
			return u.Dock[i].ID
		}
	}
	return ""
}

// lastOf is the id of a cluster's last tab, "" for none.
func (u *UIState) lastOf(contextID string) string {
	for i := len(u.Dock) - 1; i >= 0; i-- {
		if u.Dock[i].Context == contextID {
			return u.Dock[i].ID
		}
	}
	return ""
}

// neighbourInScope is the tab the dock shows nearest before the active tab of old, which closed,
// else the nearest after it, else "".
func (u *UIState) neighbourInScope(old UIState) string {
	at := slices.IndexFunc(old.Dock, func(t DockTab) bool { return t.ID == old.ActiveDock })
	if at < 0 {
		return u.lastInScope()
	}
	for i := at - 1; i >= 0; i-- {
		if id := old.Dock[i].ID; u.inDockScope(id) {
			return id
		}
	}
	for i := at + 1; i < len(old.Dock); i++ {
		if id := old.Dock[i].ID; u.inDockScope(id) {
			return id
		}
	}
	return u.lastInScope()
}
