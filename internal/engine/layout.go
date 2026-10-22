package engine

import (
	"errors"
	"slices"
)

// The window's layout (F7, the owner's finding of 2026-10-15; decisions.md, F7): three toggles at
// the top bar's right end, as VS Code's title bar has them, for the navigator (VS Code's primary
// side bar), the dock (its panel) and the agent chat's column (its secondary side bar). Each is UI
// state, so that agents see and drive it as the user does: UIState.NavigatorHidden
// (ui.toggleNavigator, ⌘B), UIState.DockOpen (ui.toggleDock, ⌘J) and UIState.Chat (chat.toggle,
// ⌘I; chat.go). The navigator's and the chat column's survive a restart in workspace.json
// (persist.go). The dock's does not need to: its tabs are not restored (D-41), and a dock without
// tabs is closed.

// ErrNoDockTabs disables ui.toggleDock while the dock has no tabs: there is nothing to open, only
// its tab strip with the "+" menu.
var ErrNoDockTabs = errors.New("the dock has no tabs: open a terminal (⌘⇧T), a pod's logs or a Create resource tab first")

// SyncDock keeps the dock to the shown cluster's tabs (syncDockScope, D-69) and DockOpen to what
// the dock can show, whoever changed the state: closed while it shows no tabs; opened when a tab it
// shows is added, or when another tab is made active while the one active before is still there (a
// tab clicked, an object's logs opened again, an agent's dock.activate), but not when the active
// tab moves because the one before it closed, nor when another cluster is shown (its dock opens as
// it was). A command that shows a tab already open, the active one too, opens the dock itself
// (ShowDockTab): SyncDock sees no change there. UpdateUI runs it; exported for the window's test
// fake of the engine, which applies the same rule.
func (u *UIState) SyncDock(old UIState) {
	u.syncDockScope(old)
	shown := u.DockInScope()
	switch {
	case len(shown) == 0:
		u.DockOpen = false
	case dockTabAdded(old.Dock, shown):
		u.DockOpen = true
	case u.Context == old.Context && u.ActiveDock != old.ActiveDock && u.ActiveDock != "" && hasDockTab(u.Dock, old.ActiveDock):
		u.DockOpen = true
	}
}

// ShowDockTab makes the dock tab id the active one and opens the dock on it: a command that shows a
// dock tab opens the dock, also when the tab is open already and the active one (the owner's alpha
// walk, 2026-10-22, AW9: Edit on an object whose Edit tab was the active one left the minimised
// dock collapsed, since SyncDock opens it only for a tab added or another tab made active). A tab
// of a cluster the window does not show opens that cluster's dock for when it is shown
// (syncDockScope), never the shown one. Exported for the window's test fake of the engine.
func (u *UIState) ShowDockTab(id string) {
	u.ActiveDock = id
	if u.inDockScope(id) {
		u.DockOpen = true
	}
}

// dockTabAdded reports whether next has a tab that old has not.
func dockTabAdded(old, next []DockTab) bool {
	for _, t := range next {
		if !hasDockTab(old, t.ID) {
			return true
		}
	}
	return false
}

// hasDockTab reports whether tabs has the tab id; never for "".
func hasDockTab(tabs []DockTab, id string) bool {
	return id != "" && slices.ContainsFunc(tabs, func(t DockTab) bool { return t.ID == id })
}

// SetLayout shows or hides a part of the window's layout: the navigator ("navigator") or the dock
// ("dock"); open nil turns it the other way. The dock refuses while it has no tabs (ErrNoDockTabs).
func (s *Session) SetLayout(part string, open *bool) (UIState, error) {
	switch part {
	case LayoutNavigator:
		return s.UpdateUI(func(u *UIState) { u.NavigatorHidden = !toggled(!u.NavigatorHidden, open) }), nil
	case LayoutDock:
		if err := s.DockToggleUnavailable(); err != nil {
			return s.UIState(), err
		}
		// A tab closed since the check leaves the dock closed: SyncDock.
		return s.UpdateUI(func(u *UIState) { u.DockOpen = toggled(u.DockOpen, open) }), nil
	}
	return s.UIState(), errors.New("unknown part of the layout: " + part)
}

// DockToggleUnavailable returns why the dock cannot be opened or collapsed now (ErrNoDockTabs: it
// shows no tab of the shown cluster nor of no cluster), nil when it can: the enabled state of
// ui.toggleDock.
func (s *Session) DockToggleUnavailable() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ui.DockInScope()) == 0 {
		return ErrNoDockTabs
	}
	return nil
}

// The parts of the window's layout SetLayout shows and hides; the chat column is chat.toggle's.
const (
	LayoutNavigator = "navigator"
	LayoutDock      = "dock"
)

// toggled is the new state of a part that is shown now: open's value, else the other way round.
func toggled(shown bool, open *bool) bool {
	if open != nil {
		return *open
	}
	return !shown
}
