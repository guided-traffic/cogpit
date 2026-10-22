package engine

import "errors"

// The welcome page (ui-parity › Layout, G5; Help › Welcome, G13). Lens Desktop has a welcome page
// behind Help › Welcome; what it shows is not captured. Cogpit's page and when it shows are Cogpit's
// defaults (inferred, 2026-10-13, not the owner's decision):
//   - the workspace shows it whenever it has nothing else to show: no cluster is connected and
//     none is connecting (at start, after a disconnect or a connection Cogpit closed), or a cluster
//     is connected and no tab is active (the last tab closed);
//   - never after the shown cluster's connect failed, whether or not a tab is open: its error page
//     shows instead (UIState.ConnectFailure; the owner's AW17 of 2026-10-22, his choice (b): "the
//     welcome page only when no cluster is chosen", D-114); a failed connect without a context to
//     name (none given, the kubeconfig sets no current one) shows the page with its error;
//   - Help › Welcome (ui.welcome.open) shows it over a connected cluster by leaving the active tab,
//     which keeps its own view, filter, selection and drawer and shows them again when it is
//     selected; opening a view or selecting a tab ends the page. Over a failed connect it leaves the
//     failed cluster disconnected, as Disconnect leaves a cluster (its context named, its log and
//     the navigator's red dot kept): Connect Again on its row shows the error page again if it fails;
//   - while a connect runs the workspace says so instead ("Connecting to …"), so that switching to a
//     tab of another cluster does not flash the page.
//
// UIState.Welcome says whether the page shows, derived by syncWelcome after every change, so that
// agents read it with ui_state and the window draws the page from it.

// errWelcomeConnecting refuses ui.welcome.open while a connect runs: the connect decides what the
// workspace shows.
var errWelcomeConnecting = errors.New("a connect is under way: the workspace shows its cluster once it is done")

// syncWelcome derives UIState.Welcome from the connection and the active tab. UpdateUI runs it after
// syncTabs, so the tab a connect activates is known.
func (u *UIState) syncWelcome() {
	switch u.Connection.State {
	case Connecting:
		u.Welcome = false
	case Connected, Interrupted, LoginExpired:
		u.Welcome = u.ActiveTab == "" // syncTabs leaves no tab active only while no view is open
	case ConnectError:
		u.Welcome = u.Context == "" // a chosen cluster's failed connect shows its error page (D-114)
	default:
		u.Welcome = true
	}
}

// SyncWelcome derives Welcome and ConnectFailure from the connection and the active tab, as
// UpdateUI does after every change: for an engine of the window's tests that changes its UI state
// itself (fyneui's fake engine), so that it shows the page the engine would.
func (u *UIState) SyncWelcome() {
	u.syncConnectFailure()
	u.syncWelcome()
}

// CheckOpenWelcome returns why ui.welcome.open cannot run now, or nil.
func (s *Session) CheckOpenWelcome() error {
	if s.UIState().Connection.State == Connecting {
		return errWelcomeConnecting
	}
	return nil
}

// OpenWelcome shows the welcome page (ui.welcome.open, Help › Welcome) and returns the new state.
// Over a connected cluster it leaves the active tab, which keeps its state (syncTabs saved it), and
// closes the open view as closing the last tab does: the view's watches are released. A settings
// page and the command palette close, so that the page is what the window shows. Without a
// connected cluster the page shows already.
func (s *Session) OpenWelcome() (UIState, error) {
	var err error
	st := s.UpdateUI(func(u *UIState) {
		if u.Connection.State == Connecting {
			err = errWelcomeConnecting
			return
		}
		u.Settings, u.Palette = nil, false
		if u.Connection.State == ConnectError && u.Context != "" {
			u.Connection = Connection{State: Disconnected} // the error page gives way (D-114)
		}
		if u.Connection.State.Live() {
			u.ActiveTab = ""
			u.View, u.Filter = "", ""
			u.Selected, u.Drawer = nil, nil
		}
	})
	return st, err
}
