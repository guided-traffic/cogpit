package engine

import (
	"errors"
	"fmt"
	"slices"
)

// Moving tabs (the owner's gate walk, 2026-10-20, GW4: "Man muss Tab beliebig anordnen können").
// A tab of the tab bar, or of the dock, moves to another place among the tabs its bar shows: the
// tab bar's are the shown cluster's (TabsShown), the dock's the shown cluster's and those of
// no cluster (DockInScope). The tabs the bar does not show keep their places among the others. The
// window drags a tab there (tabbar.go, dock.go, stripdrag.go); ui.tab.moveLeft and ui.tab.moveRight
// move the active tab one place (View › Move Tab Left and Right). The order is UI state: agents see
// it in ui_state, and the tab bar's survives a restart with the tabs (persist.go keeps their order).
// The lead's decisions of 2026-10-20, open to revision (D-106).

// errNotInBar reports a tab its bar does not show.
var errNotInBar = errors.New("the bar does not show it")

// moveAmong moves the item id to the 1-based place index among the items shown (in the order of
// all), and returns all in its new order: it goes right before the shown item that is to follow it,
// or right after the last shown one, so that the hidden ones keep their places among the others.
func moveAmong[T any](all []T, id string, index int, idOf func(T) string, shown func(T) bool) ([]T, error) {
	i := slices.IndexFunc(all, func(t T) bool { return idOf(t) == id })
	if i < 0 {
		return all, fmt.Errorf("%w %q", ErrUnknownTab, id)
	}
	if !shown(all[i]) {
		return all, fmt.Errorf("tab %q: %w", id, errNotInBar)
	}
	var rest []string // the shown items' ids but id, in order
	for _, t := range all {
		if shown(t) && idOf(t) != id {
			rest = append(rest, idOf(t))
		}
	}
	if index < 1 || index > len(rest)+1 {
		return all, fmt.Errorf("place %d: the bar shows %d tabs", index, len(rest)+1)
	}
	item := all[i]
	out := slices.Delete(slices.Clone(all), i, i+1)
	at := len(out)
	if index-1 < len(rest) {
		at = slices.IndexFunc(out, func(t T) bool { return idOf(t) == rest[index-1] })
	} else if len(rest) > 0 {
		at = slices.IndexFunc(out, func(t T) bool { return idOf(t) == rest[len(rest)-1] }) + 1
	}
	return slices.Insert(out, at, item), nil
}

// placeAmong returns an item's 1-based place among the items shown, and how many are shown; 0 when
// it is not shown or gone.
func placeAmong[T any](all []T, id string, idOf func(T) string, shown func(T) bool) (place, n int) {
	for _, t := range all {
		if !shown(t) {
			continue
		}
		n++
		if idOf(t) == id {
			place = n
		}
	}
	return place, n
}

func viewTabID(t ViewTab) string { return t.ID }
func dockTabID(t DockTab) string { return t.ID }

// tabShown reports whether the tab bar shows a tab (TabsShown): with mixed environments on every
// tab, else the shown cluster's (UIState.Context, whatever its connection: a restored session's
// other clusters keep their tabs hidden until they are shown), any while no cluster is.
func (u UIState) tabShown(t ViewTab) bool {
	if u.Preferences != nil && u.Preferences.MixedEnvironments {
		return true
	}
	return u.Context == "" || t.Context == u.Context
}

// moveTab moves a tab ("" is the active one) to the 1-based place index among the tabs the tab bar
// shows.
func (u *UIState) moveTab(id string, index int) error {
	if id == "" {
		if id = u.ActiveTab; id == "" {
			return ErrNoTab
		}
	}
	tabs, err := moveAmong(u.Tabs, id, index, viewTabID, u.tabShown)
	if err != nil {
		return err
	}
	u.Tabs = tabs
	return nil
}

// tabStep returns the place the active tab moves to one place left (d < 0) or right, or why it
// cannot.
func (u UIState) tabStep(d int) (int, error) {
	if u.ActiveTab == "" {
		return 0, ErrNoTab
	}
	place, n := placeAmong(u.Tabs, u.ActiveTab, viewTabID, u.tabShown)
	switch {
	case place == 0:
		return 0, fmt.Errorf("the active tab: %w", errNotInBar)
	case d < 0 && place == 1:
		return 0, errors.New("the tab is the first in the tab bar")
	case d > 0 && place == n:
		return 0, errors.New("the tab is the last in the tab bar")
	}
	return place + d, nil
}

// moveDockTab moves a dock tab to the 1-based place index among the tabs the dock shows.
func (u *UIState) moveDockTab(id string, index int) error {
	tabs, err := moveAmong(u.Dock, id, index, dockTabID, u.InDockScope)
	if err != nil {
		return err
	}
	u.Dock = tabs
	return nil
}

// MoveTab moves a tab of the tab bar ("" is the active one) to the 1-based place index among the
// tabs the bar shows, and returns the new state.
func (s *Session) MoveTab(id string, index int) (UIState, error) {
	var err error
	st := s.UpdateUI(func(u *UIState) { err = u.moveTab(id, index) })
	return st, err
}

// CheckTabStep returns why the active tab cannot move one place left (d < 0) or right, or nil.
func (s *Session) CheckTabStep(d int) error {
	_, err := s.UIState().tabStep(d)
	return err
}

// StepTab moves the active tab one place left (d < 0) or right among the tabs the bar shows.
func (s *Session) StepTab(d int) (UIState, error) {
	var err error
	st := s.UpdateUI(func(u *UIState) {
		var to int
		if to, err = u.tabStep(d); err == nil {
			err = u.moveTab(u.ActiveTab, to)
		}
	})
	return st, err
}

// MoveDockTab moves a dock tab to the 1-based place index among the tabs the dock shows, and
// returns the new state.
func (s *Session) MoveDockTab(id string, index int) (UIState, error) {
	var err error
	st := s.UpdateUI(func(u *UIState) { err = u.moveDockTab(id, index) })
	return st, err
}
