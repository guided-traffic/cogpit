package commands

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The commands of a cluster's connection: Reconnect, a new connection from the kubeconfig file as
// it is now with the tabs kept (D-81); Log in again of the connection banner (D-58); and a shell
// tab's Reconnect.

// ConnectionResult is cluster.reconnect's and cluster.login's result: the connection as the UI
// state holds it.
type ConnectionResult struct {
	Context    string            `json:"context"`
	Connection engine.Connection `json:"connection"`
}

func connectionResult(s *engine.Session, contextID string) ConnectionResult {
	u := s.UIState()
	if contextID != "" && contextID != u.Context {
		c, _ := u.ConnectionOf(contextID)
		return ConnectionResult{Context: contextID, Connection: c.Connection}
	}
	return ConnectionResult{Context: u.Context, Connection: u.Connection}
}

// subresourceExec is the pods subresource a shell opens.
const subresourceExec = "exec"

// errLoginNotExpired is cluster.login's Enabled reason.
var errLoginNotExpired = errors.New("the login has not expired")

func registerConnection(g *registrar) {
	add(g, engine.Spec[engine.OnCluster]{
		ID: "cluster.reconnect", Label: "Reconnect", MenuPath: "Cluster/Reconnect", Danger: engine.DangerUI,
		Description: "Ends a connected cluster's connection (context, else the shown one; connected, interrupted or with its " +
			"login expired) and connects it anew from its kubeconfig file as it is now: a client certificate or token renewed " +
			"in the file is used, and a credential plugin's expired token is renewed by the plugin, as at any connect (a plugin " +
			"or a connection without the certificate check that the user has not allowed is refused with the reason). The " +
			"cluster's tabs, view, filter, selection, drawer and dock stay; the views read the new connection's cache once it " +
			"listed. The old connection ends only once the new one stands, so a reconnect that fails leaves the cluster as it " +
			"was (the error says why; cluster.connectLog has the attempt). Logs tabs open their streams again on the new " +
			"connection, port forwards listen on the same local ports again, and shells, attach and node shells end, their tabs " +
			"offering dock.reconnect. Refused while a job or a Helm action runs on the cluster (it would stop it). Only reads " +
			"from the cluster. A connection whose API server was upgraded to another minor since the connect " +
			"(connection.upgraded: from, to) still knows the kinds and API versions of the old one; a reconnect loads the new " +
			"one's. An agent or the CLI may not reconnect a cluster whose login expired while its context logs in " +
			"through a credential plugin (connection.credentialPlugin): that runs the login program, which may open a " +
			"browser, and logging in again is the user's in the window. Returns the connection.",
		Enabled: func(s *engine.Session, p engine.OnCluster) error { return s.ReconnectBlocked(p.Context) },
		Refuse: func(ctx context.Context, s *engine.Session, p engine.OnCluster) error {
			return s.ReconnectRefused(engine.OriginOf(ctx), p.Context)
		},
		Run: func(ctx context.Context, s *engine.Session, p engine.OnCluster) (any, error) {
			if err := s.Reconnect(ctx, p.Context); err != nil {
				return nil, err
			}
			return connectionResult(s, p.Context), nil
		},
	})

	add(g, engine.Spec[engine.OnCluster]{
		ID: "cluster.login", Label: "Log In Again", MenuPath: "Cluster/Log In Again", Danger: engine.DangerUI,
		GUIOnly: engine.ErrGUIOnly,
		Description: "Renews a connected cluster's login (context, else the shown one) after it expired (ui_state's " +
			"connection.state, or its entry of connections, is loginExpired) or lapsed while the cluster was not shown " +
			"(its entry's loginLapsed: its setting renewLoginInBackground is off): the context's credential plugin runs once, " +
			"and may open a browser for the user to log in; the cache, the tabs and the views stay. A context without a " +
			"credential plugin (a client certificate or a token in the kubeconfig: connection.credentialPlugin unset), or one " +
			"whose login in the kubeconfig file changed since the connect, is reconnected instead, as cluster.reconnect does: " +
			"a new connection from the file as it is now, the tabs kept. A plugin whose program changed since the user allowed " +
			"it is not run: then the context is connected anew, which asks the user first. Returns the connection." + guiOnly,
		Enabled: func(s *engine.Session, p engine.OnCluster) error {
			u := s.UIState()
			c, lapsed := u.Connection, false
			if p.Context != "" && p.Context != u.Context {
				e, ok := u.ConnectionOf(p.Context)
				if !ok {
					return &engine.NotConnectedError{Context: p.Context}
				}
				c, lapsed = e.Connection, e.LoginLapsed
			} else if e, ok := u.ConnectionOf(u.Context); ok {
				lapsed = e.LoginLapsed
			}
			switch {
			case c.State != engine.LoginExpired && !lapsed:
				return errLoginNotExpired
			case c.LoggingIn:
				return errors.New("a login is under way")
			}
			return nil
		},
		Run: func(ctx context.Context, s *engine.Session, p engine.OnCluster) (any, error) {
			if err := requireGUI(ctx); err != nil {
				return nil, err
			}
			if err := s.LogInAgain(ctx, p.Context); err != nil {
				return nil, err
			}
			return connectionResult(s, p.Context), nil
		},
	})

	add(g, engine.Spec[dockParams]{
		ID: "dock.reconnect", Label: "Reconnect", Danger: engine.DangerMutating,
		Verbs: []engine.Verb{
			{Verb: nwVerbCreate, Resource: kindPods, Subresource: subresourceExec},
			{Verb: nwVerbCreate, Resource: kindPods, Subresource: "attach"},
		},
		Description: "Opens a new session in a shell, attach or node shell tab whose session ended because the connection " +
			"was interrupted (its dock tab's interrupted is set): a new shell in the same container, attached again, or a new " +
			"root shell in the node shell's pod while it still runs. A process cannot be resumed: what ran in the old session " +
			"is gone. Anything typed there runs with the container's or the node's privileges, so an agent's request waits for " +
			"the user's acceptance; a node shell's always does. Returns the tab.",
		Enabled: func(s *engine.Session, p dockParams) error {
			if _, err := streamableOn(s, tabContext(s, p.ID), "shells"); err != nil {
				return err
			}
			tab, ok := s.UIState().DockTab(p.ID)
			if !ok {
				return fmt.Errorf("no dock tab %q", p.ID)
			}
			if !reconnectable(tab) {
				return fmt.Errorf("%w: the tab's session was not ended by an interruption", engine.ErrNotApplicable)
			}
			return nil
		},
		Escalate: func(s *engine.Session, p dockParams) bool {
			tab, ok := s.UIState().DockTab(p.ID)
			return ok && tab.Type == engine.DockNodeShell // a root shell on a node (D-37)
		},
		Targets: func(s *engine.Session, p dockParams) ([]model.ObjectRef, error) {
			tab, ok := s.UIState().DockTab(p.ID)
			if !ok || tab.Ref == nil {
				return nil, fmt.Errorf("no dock tab %q", p.ID)
			}
			if tab.NodeShell != nil { // the node shell's pod
				return []model.ObjectRef{{Context: tab.Context, Kind: kindPods, Namespace: tab.NodeShell.Namespace, Name: tab.NodeShell.Pod}}, nil
			}
			return []model.ObjectRef{*tab.Ref}, nil
		},
		Run: func(ctx context.Context, s *engine.Session, p dockParams) (any, error) {
			if cur, ok := s.UIState().DockTab(p.ID); ok {
				if err := engine.CheckConfirmedContext(ctx, cur.Context); err != nil {
					return nil, err
				}
			}
			var tab engine.DockTab
			found := false
			s.UpdateUI(func(u *engine.UIState) {
				i := slices.IndexFunc(u.Dock, func(t engine.DockTab) bool { return t.ID == p.ID })
				if i < 0 || !reconnectable(u.Dock[i]) {
					return
				}
				u.Dock[i].Interrupted, u.Dock[i].Reconnected = time.Time{}, false
				u.Dock[i].Session++ // the window opens the new session in the tab
				u.ShowDockTab(p.ID)
				tab, found = u.Dock[i], true
			})
			if !found {
				return nil, fmt.Errorf("dock tab %q has no session to reconnect", p.ID)
			}
			return DockResult{Tab: tab}, nil
		},
	})
}

// reconnectable reports whether a dock tab's session ended with the connection.
func reconnectable(t engine.DockTab) bool {
	switch t.Type {
	case engine.DockShell, engine.DockAttach, engine.DockNodeShell:
		return !t.Interrupted.IsZero()
	}
	return false
}
