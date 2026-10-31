package engine

import (
	"context"
	"fmt"
	"slices"
)

// Closing several tabs at once (the owner's request of 2026-10-31: "Bei einem Rechtsklick auf einen
// Tab, sowohl im Hauptbereich als auch im Dock, soll der Nutzer die Wahl bekommen: close, close
// other, close all"). The details (D-144):
//   - they close the tabs the bar shows (TabsShown, DockInScope): the tabs of a cluster it does not
//     show stay, as they stay hidden;
//   - Close Others makes the tab it keeps the active one, as a click on it does;
//   - the dock keeps a tab whose job still runs, which closing would stop (the owner's decision):
//     its own close button and Cancel stop it.

// CloseTabs closes every tab the tab bar shows but keep ("" closes them all); keep becomes the
// active tab (SelectTab).
func (s *Session) CloseTabs(ctx context.Context, keep string) (UIState, error) {
	if u := s.UIState(); keep != "" && !slices.ContainsFunc(u.TabsShown(), func(t ViewTab) bool { return t.ID == keep }) {
		return u, fmt.Errorf("%w %q", ErrUnknownTab, keep)
	}
	var err error
	st := s.UpdateUI(func(u *UIState) {
		for _, t := range u.TabsShown() {
			if t.ID != keep && err == nil {
				err = u.closeTab(t.ID)
			}
		}
	})
	if err != nil {
		return st, err
	}
	st = s.DropGoneSelected(ctx, st)
	if keep != "" && st.ActiveTab != keep {
		return s.SelectTab(ctx, keep)
	}
	return st, nil
}

// CloseDockTabs closes every tab the dock shows but keep ("" closes them all) and those whose job
// still runs. keep becomes the active tab; else the active one stays if it is left, or the last
// the dock shows takes its place.
func (s *Session) CloseDockTabs(keep string) (UIState, error) {
	found := keep == ""
	st := s.UpdateUI(func(u *UIState) {
		if keep != "" && !u.inDockScope(keep) {
			return
		}
		found = true
		u.Dock = slices.DeleteFunc(u.Dock, func(t DockTab) bool {
			return t.ID != keep && u.InDockScope(t) && (t.Job == nil || t.Job.State.Ended())
		})
		switch {
		case keep != "":
			u.ShowDockTab(keep)
		case !hasDockTab(u.Dock, u.ActiveDock):
			u.ActiveDock = u.lastInScope()
		}
	})
	if !found {
		return st, fmt.Errorf("no dock tab %q", keep)
	}
	return st, nil
}
