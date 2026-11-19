package engine

import (
	"fmt"
	"strconv"
)

// The dock's tabs of a context that leaves Cogpit (G5, G13). A removed kubeconfig source's
// contexts (closeTabsOfSource) and those whose file is gone (closeTabsOfGoneFiles) lose their
// cluster tabs (UIState.dropContexts) and their dock tabs with them: logs, terminals, shells, a node shell, editors and Helm tabs, every
// tab bound to one of those contexts (a tab without a context, the Audit Log, stays). What a
// tab's close does follows, as for dock.close: the window ends its stream or shell, and a node
// shell's pod is deleted (endClosedNodeShells, D-37). One thing differs: closing a tab discards its
// unsaved text, but these tabs close without the user, so an editor's unsaved text is first kept
// as a draft (Drafts.keep), offered when its cluster connects again (D-11). Cogpit's default, not
// the owner's decision (Lens's behaviour is not captured): a terminal or a log stream of a cluster
// Cogpit no longer offers would act on a context that is gone, as a cluster tab would fail to
// connect.

// dropDockOf closes the dock's tabs of the contexts gone reports, counting them in closed. When
// the shown tab closes, the nearest open tab before it is shown, else the first one, else none.
func (u *UIState) dropDockOf(gone func(string) bool, closed []goneTabs) []goneTabs {
	closes := func(t DockTab) bool { return t.Context != "" && gone(t.Context) }
	var open []DockTab
	before, activeGone := "", false
	passed := false // the shown tab is behind
	for _, t := range u.Dock {
		if t.ID == u.ActiveDock {
			passed, activeGone = true, closes(t)
		}
		if closes(t) {
			closed = addGone(closed, t.Context)
			continue
		}
		open = append(open, t)
		if !passed {
			before = t.ID
		}
	}
	if len(open) == len(u.Dock) {
		return closed
	}
	u.Dock = open
	if activeGone {
		switch {
		case before != "":
			u.ActiveDock = before
		case len(open) > 0:
			u.ActiveDock = open[0].ID
		default:
			u.ActiveDock = ""
		}
	}
	return closed
}

// dockOfGone reports whether the dock holds a tab of a context gone reports.
func (u *UIState) dockOfGone(gone func(string) bool) bool {
	for _, t := range u.Dock {
		if t.Context != "" && gone(t.Context) {
			return true
		}
	}
	return false
}

// keepDraftsOf keeps the unsaved text of the editor tabs of the contexts gone reports as drafts,
// before those tabs close (Drafts.keep), and returns how many it kept per context.
func (s *Session) keepDraftsOf(gone func(string) bool) []goneTabs {
	byCtx := map[string][]string{}
	var order []string
	for _, t := range s.UIState().Dock {
		if (t.Type == DockEdit || t.Type == DockCreate) && t.Context != "" && gone(t.Context) {
			if _, seen := byCtx[t.Context]; !seen {
				order = append(order, t.Context)
			}
			byCtx[t.Context] = append(byCtx[t.Context], t.ID)
		}
	}
	var kept []goneTabs
	for _, ctxName := range order {
		if n := s.opts.Drafts.keep(s, byCtx[ctxName]); n > 0 {
			kept = append(kept, goneTabs{Context: ctxName, Tabs: n})
		}
	}
	return kept
}

// keptDraftsText says where the unsaved text of the closed editor tabs went; "" for none. With
// named, it names the clusters (a notice of its own); else it follows a notice that named them.
func keptDraftsText(kept []goneTabs, named bool) string {
	if len(kept) == 0 {
		return ""
	}
	n := 0
	for _, g := range kept {
		n += g.Tabs
	}
	tabs, verb, drafts := "1 editor tab", "is", "a draft"
	if n > 1 {
		tabs, verb, drafts = strconv.Itoa(n)+" editor tabs", "are", "drafts"
	}
	when := "its cluster connects"
	if len(kept) > 1 {
		when = "their clusters connect"
	}
	if named {
		tabs += " of " + clusterNames(kept)
	}
	return fmt.Sprintf("The unsaved text of %s %s kept as %s, offered when %s again.", tabs, verb, drafts, when)
}
