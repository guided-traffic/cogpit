package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// Restore the last session (D-69 item 8; the owner: a global preference, off by default).
// Preferences › App › "Restore the last session":
//
//   - On: at start Cogpit restores the tab bar's tabs of the last run (D-41) and connects again to
//     the clusters that were connected when it quit, the one shown then first (Connect: the window
//     shows it as on any connect, its most recently used tab), then the others one after another
//     in the background (ConnectBackground), so that the window is usable at once and never two
//     credential plugins ask at the same time. Each connect is a connect of the user's: an
//     approved credential plugin runs as on any connect, one not allowed yet (or whose program
//     changed) is refused with its reason (D-26), and nothing else is sent. A context no
//     kubeconfig source offers any more is not connected (a notice says so). --context, or a
//     cogpit:// link, connects its own cluster and shows it; the restore then connects the others
//     behind it.
//   - Off: Cogpit starts without tabs and without connections. The window's arrangement that is no
//     session (the tables' column layout, the navigator's collapsed groups, the navigator hidden,
//     the chat column) is restored either way.
//
// workspace.json keeps what the restore needs whatever the preference says (persist.go: connected,
// shown, beside the tabs), so that turning it on restores the session of the last quit. The file
// keeps context ids only, never a credential, as it kept them for the tabs.

// ErrRestoreSessionGUIOnly refuses an agent's or the CLI's "Restore the last session" turned on:
// on, the next start connects clusters and runs their credential plugins without a click, so
// turning it on is the user's, in Cogpit's window. Turning it off is open to them.
var ErrRestoreSessionGUIOnly = errors.New("only the user, in Cogpit's own window, can turn on restoring the last session " +
	"(the next start then connects the clusters connected at quit, their credential plugins included); agents and the CLI may only turn it off")

// RefuseRestoreSession refuses turning on the restore of the last session from anywhere but the
// user's own action in Cogpit's window.
func RefuseRestoreSession(ctx context.Context, on *bool) error {
	if on != nil && *on && OriginOf(ctx) != OriginGUI {
		return ErrRestoreSessionGUIOnly
	}
	return nil
}

// restorePlan is what the start connects again: the cluster shown at quit, and every cluster
// connected then, in the file's order.
type restorePlan struct {
	shown     string
	connected []string
}

// restoreSession reports whether the preference asks to restore the last session.
func (s *Session) restoreSession() bool {
	if s.opts.Preferences == nil {
		return false
	}
	p, err := s.Preferences()
	return err == nil && p.RestoreSession
}

// planRestore turns a workspace file's connections into the restore's plan, without the contexts
// that are certainly gone (goneContexts); it returns them for the notice.
func (s *Session) planRestore(doc workspaceDoc) (*restorePlan, []string) {
	if len(doc.Connected) == 0 {
		return nil, nil
	}
	gone := s.goneContexts(doc.Connected, false)
	plan := &restorePlan{}
	var dropped []string
	for _, id := range doc.Connected {
		if gone[id] {
			dropped = append(dropped, id)
			continue
		}
		plan.connected = append(plan.connected, id)
	}
	if slices.Contains(plan.connected, doc.Shown) {
		plan.shown = doc.Shown
	}
	if len(plan.connected) == 0 {
		return nil, dropped
	}
	return plan, dropped
}

// notReconnectedNotice says which clusters of the last session are not connected again; "" for
// none.
func notReconnectedNotice(dropped []string) string {
	if len(dropped) == 0 {
		return ""
	}
	list := make([]goneTabs, 0, len(dropped))
	for _, id := range dropped {
		list = append(list, goneTabs{Context: id})
	}
	return fmt.Sprintf("Not connected again from the last session: no kubeconfig source offers %s any more.", clusterNames(list))
}

// RestoreLastSession connects again to the clusters of the last session when the preference
// "Restore the last session" was on at start (D-69 item 8): the one shown at quit first, unless
// explicit names the cluster the start connects itself (--context, the demo cluster), then the
// others one after another in the background. It returns at once; the connects run on a goroutine
// of their own, which the session's close ends. Without a plan (the preference off, nothing
// connected at the last quit, a session without a workspace) it does nothing. Only the first call
// connects.
func (s *Session) RestoreLastSession(explicit string) {
	w := s.opts.Workspace
	if w == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	plan := w.takePlan(cancel)
	if plan == nil {
		cancel()
		return
	}
	s.goSafe("restoring the last session", func() {
		defer cancel()
		s.restoreConnections(ctx, *plan, explicit)
	})
}

// restoreConnections runs the plan: the shown cluster's connect while the window has not been
// asked for another one, then the others in the background, one after another. A cluster shown at
// quit whose connect another one superseded (a cogpit:// link, the user's click) connects in the
// background with them.
func (s *Session) restoreConnections(ctx context.Context, plan restorePlan, explicit string) {
	shown := ""
	if explicit == "" && plan.shown != "" {
		if u := s.UIState(); u.Connection.State == Disconnected && len(u.Connections) == 0 {
			shown = plan.shown
			if err := s.Connect(ctx, shown); err != nil && ctx.Err() == nil {
				// A failed connect shows its error as any connect does; the log says it came from here.
				s.log.Info("engine: restoring the last session: the cluster shown at quit did not connect", "context", shown, "err", err)
				if s.UIState().Context != shown {
					shown = "" // superseded, not failed: it connects behind the one the window shows now
				}
			}
		}
	}
	for _, id := range plan.connected {
		if ctx.Err() != nil {
			return
		}
		if id == shown || id == explicit {
			continue
		}
		err := s.ConnectBackground(ctx, id)
		if err == nil || errors.Is(err, errConnectedAlready) || errors.Is(err, errConnectCancelled) || ctx.Err() != nil {
			continue
		}
		s.emit(Event{Type: EventToast, Level: LevelWarning, Message: fmt.Sprintf("Could not connect to %s again (restoring the last session): %s",
			kubeconfig.DisplayText(id, 200), oneLine(err.Error()))})
	}
}
