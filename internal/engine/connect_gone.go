package engine

import (
	"errors"
	"io/fs"
	"os"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// A connect still running when its context leaves Cogpit (G5, G13; FIXUP 2026-10-14): its
// kubeconfig file is gone, or its source was removed, while the connect waited for the cluster.
// The watcher (closeTabsOfGoneFiles) and the removal (closeTabsOfSource) close the context's tabs
// but leave UIState.Context alone while a connect holds it, so that the connect's end has the say.
// Before, a connect that failed then left Context naming a context that is gone (ConnectError),
// and one that succeeded left a connection made from a gone file until the sources changed again
// (or, closed by the watcher between its publishing and the UI's Connected, a UI that said
// Connected without a connection). Now the connect's end forgets the context as the watcher would
// have, had the connect been over: a connection that succeeded is closed with the reason
// (CloseGoneSourceConnection), and the context, its tabs and the dock's are dropped
// (UIState.dropContexts), the reason kept in Connection.Error for the welcome page. Only a plain
// fact counts, as for the watcher: the file does not exist, or no source covers the context and
// the connector no longer offers it.

// settleGoneConnect follows the end of the latest connect to id: b is the backend it connected,
// nil when it failed. Connect calls it holding the connect lock, so that no other connect or
// disconnect changes the backend meanwhile; only the watcher's close of a gone file's connection
// (disconnectBackend) can. A connect whose context is still offered costs a look at the sources'
// list and a stat of its file; a context that is not a source's costs nothing.
func (s *Session) settleGoneConnect(id string, b *Backend) {
	reason := s.goneReason(id)
	if reason == "" {
		return
	}
	if b == nil {
		text := "Could not connect to " + kubeconfig.DisplayText(id, 200) + ": " + reason + "."
		s.state.logs.add(id, LogError, text)
		s.forgetGoneContext(id, text, false, false)
		return
	}
	if s.state.holds(b) {
		s.CloseGoneSourceConnection() // or the watcher's close of it, under way, wins
	}
	if s.state.holds(b) {
		return // still connected: the sources say the file is there after all
	}
	// Closed here or by the watcher, whose change of the UI may still be on its way: the reason
	// the UI already shows stays, else this one is said.
	s.forgetGoneContext(id, "Disconnected from "+kubeconfig.DisplayText(id, 200)+": "+reason+".", true, false)
}

// settleClosedConnect follows the window's connect to id whose backend was closed between its
// publishing and the window's Connected (awaitRun). The close of a gone file's or a removed
// source's connection (disconnectBackend) takes the backend from the pool before it changes the
// UI; when it ran in the kubeconfig watcher meanwhile, settleGoneConnect, at the run's end, found
// the window still Connecting, left the context alone, and the connect returned with the window
// "connecting" for a context that is gone (found in a race-detector run on CI, 2026-10-23). That
// Connecting is the window's own connect's, which is over: the context is forgotten here.
func (s *Session) settleClosedConnect(id string) {
	if reason := s.goneReason(id); reason != "" {
		s.forgetGoneContext(id, "Disconnected from "+kubeconfig.DisplayText(id, 200)+": "+reason+".", true, true)
	}
}

// goneReason says why a context left Cogpit ("its kubeconfig file … is gone", "its kubeconfig
// source was removed"), or "" when it did not, or that cannot be told.
func (s *Session) goneReason(id string) string {
	k := s.KubeconfigSources()
	if k == nil || !looksLikeSourceID(id) {
		return ""
	}
	srcs, err := k.List()
	if err != nil {
		return ""
	}
	if path, _, ok := sourceFileOf(srcs, id); ok {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return "its kubeconfig file " + kubeconfig.DisplayText(path, 400) + " is gone"
		}
		return ""
	}
	list, err := s.Contexts() // no source covers it: gone unless the connector still offers it
	if err != nil {
		return ""
	}
	for _, c := range list {
		if c.Name == id {
			return ""
		}
	}
	return "its kubeconfig source was removed"
}

// forgetGoneContext drops a context that left Cogpit from the UI state: its tabs, the dock's
// (their unsaved editor text kept as drafts, dock_gone.go) and Context, the connection left
// Disconnected with errText. closed says the connect's backend was closed (Connect holds the
// connect lock: a Connected UI is then stale), and that a reason the UI shows already stays.
// A window Connecting to the context is left alone, a connect that started meanwhile, unless
// ended says the window's connect is the one that ended (settleClosedConnect).
func (s *Session) forgetGoneContext(id, errText string, closed, ended bool) {
	stays := func(u UIState) bool {
		return u.Context != id || (u.Connection.State == Connecting && !ended) || (u.Connection.State.Live() && !closed)
	}
	if stays(s.UIState()) {
		return
	}
	gone := func(c string) bool { return c == id }
	kept := s.keepDraftsOf(gone)
	s.UpdateUI(func(u *UIState) {
		if stays(*u) {
			return
		}
		if closed && u.Connection.State == Disconnected && u.Connection.Error != "" {
			errText = u.Connection.Error // disconnectBackend's, which names the file
		}
		u.Connection = Connection{State: Disconnected} // not active: dropContexts forgets Context
		u.dropContexts(gone)
		u.Connection = Connection{State: Disconnected, Error: errText}
		u.View, u.Filter = "", ""
		u.Selected, u.Drawer = nil, nil
	})
	if notice := keptDraftsText(kept, true); notice != "" {
		s.emit(Event{Type: EventToast, Level: LevelInfo, Message: notice})
	}
}
