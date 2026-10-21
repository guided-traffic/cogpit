package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// The navigator's scope (ui-parity › Layout and Clusters, G5, G13). Captured from Lens Desktop
// (2026-10-11): the navigator's header has a dropdown of the scope its tree shows, one cluster or a
// section such as "Local Kubeconfigs"; a cluster's root has a › and a ‹ to go back. Lens's
// documentation: the navigator's sections are Kubernetes Clusters (Local Kubeconfigs and the cloud
// sections), Teamwork and Extensions, and drilling into a section or a cluster with its chevron
// filters the tabs to that scope.
//
// Cogpit's defaults (inferred, not the owner's decision, 2026-10-13):
//   - one section, Local Kubeconfigs under Kubernetes Clusters: every context the kubeconfig
//     sources offer, the one list that chooses a cluster (D-57). The cloud sections (G18, after the
//     beta), Teamwork (D-9) and Extensions (D-5) are not built, so the section level has nothing
//     above it;
//   - the scope is NavScopeLocal or NavScopeCluster(<context id>): at start, and whenever no
//     cluster is connected or connecting (after a disconnect, a failed connect, a connection Cogpit
//     closed), the section;
//   - every connect drills into its cluster as it starts, whoever started it (the section, the
//     hotbar, the welcome page, a tab of another cluster, an agent), a connect again to the
//     connected cluster too: the navigator's tree is the connected cluster's, since Cogpit holds
//     one connection (architecture §6);
//   - ui.setNavigatorScope goes up to the section and back into the connected cluster without
//     touching the connection; into another cluster it connects, as cluster.connect does;
//   - the tab bar shows the shown cluster's tabs, at the section too (the owner, 2026-10-20, GW11:
//     the status bar and the navigator name one cluster, as the title bar did until 2026-10-21
//     (D-54), and the bar shows its tabs; until then it
//     showed every cluster's at the section); every cluster's with the preference mixed
//     environments on (off by default), and every tab while no cluster is shown; never a tab of a
//     cluster without a connection (GW13); ⌘1–⌘9, next and previous count the tabs it shows
//     (TabsShown).
//
// UpdateUI keeps the scope in step with the connection (syncNavScope) after every change, so
// agents read it with ui_state as navigatorScope and the window draws the navigator from it.

// NavScopeLocal is the navigator's section of the local kubeconfigs' clusters.
const NavScopeLocal = "section:local"

// navScopeClusterPrefix starts the scope of one cluster: "cluster:<context id>".
const navScopeClusterPrefix = "cluster:"

// ErrNavScope refuses a scope that is neither the section nor a cluster.
var ErrNavScope = errors.New(`the navigator's scope is "section:local" or "cluster:<context id>"`)

// NavScopeCluster is the navigator's scope of one cluster.
func NavScopeCluster(contextID string) string { return navScopeClusterPrefix + contextID }

// ParseNavScope checks a scope and returns the cluster it names, "" for the section.
func ParseNavScope(scope string) (string, error) {
	if scope == NavScopeLocal {
		return "", nil
	}
	if id, ok := strings.CutPrefix(scope, navScopeClusterPrefix); ok && strings.TrimSpace(id) != "" {
		return id, nil
	}
	return "", fmt.Errorf("%w, not %q", ErrNavScope, scope)
}

// activeConnection reports whether a connection state holds a cluster: connected (interrupted or
// with an expired login included, D-58), or connecting.
func activeConnection(c ConnectionState) bool { return c.Live() || c == Connecting }

// Scope returns the navigator's scope. A state that holds none (the zero UIState) reads as the
// engine would set it: the connected or connecting cluster, else the section.
func (u UIState) Scope() string {
	if u.NavigatorScope != "" {
		return u.NavigatorScope
	}
	if u.Context != "" && activeConnection(u.Connection.State) {
		return NavScopeCluster(u.Context)
	}
	return NavScopeLocal
}

// ScopeCluster returns the cluster the navigator shows, "" at the section level.
func (u UIState) ScopeCluster() string {
	if id, ok := strings.CutPrefix(u.Scope(), navScopeClusterPrefix); ok {
		return id
	}
	return ""
}

// TabsShown returns the tabs the tab bar shows: the shown cluster's, wherever the navigator stands
// (GW11); every connected cluster's with the preference mixed environments on, and while no cluster
// is shown; never a tab of a cluster without a connection (GW13: connecting, failed, disconnected).
func (u UIState) TabsShown() []ViewTab {
	return slices.DeleteFunc(slices.Clone(u.Tabs), func(t ViewTab) bool { return !u.tabShown(t) })
}

// syncNavScope keeps the scope in step with a change of the UI state from old to u, whichever
// command made it. UpdateUI runs it after every change:
//   - without a connected or connecting cluster, the scope is the section;
//   - a connect starting (to another cluster, after none, or again to the same one) shows its
//     cluster, and so does a connection that appears without one (another cluster's, after none);
//   - a cluster scope always names the connected cluster.
func (u *UIState) syncNavScope(old UIState) {
	starting := u.Connection.State == Connecting && (old.Connection.State != Connecting || old.Context != u.Context)
	switch {
	case u.Context == "" || !activeConnection(u.Connection.State):
		u.NavigatorScope = NavScopeLocal
	case starting || !activeConnection(old.Connection.State) || old.Context != u.Context:
		u.NavigatorScope = NavScopeCluster(u.Context)
	case u.NavigatorScope != NavScopeLocal:
		u.NavigatorScope = NavScopeCluster(u.Context)
	}
}

// SetNavigatorScope shows a scope in the navigator (ui.setNavigatorScope) and returns the new state.
// The section and the connected (or connecting) cluster are set at once. Another cluster is
// connected first, as cluster.connect connects it (a credential plugin runs only once the user
// allowed it); the connect drills into it as it starts (syncNavScope). Like cluster.connect it
// returns the state, with no error, when the connect failed: Connection says why, and the scope is
// the section again.
func (s *Session) SetNavigatorScope(ctx context.Context, scope string) (UIState, error) {
	id, err := ParseNavScope(scope)
	if err != nil {
		return s.UIState(), err
	}
	if id == "" {
		return s.UpdateUI(func(u *UIState) { u.NavigatorScope = NavScopeLocal }), nil
	}
	if resolved, rErr := s.ResolveContext(id); rErr == nil {
		id = resolved // a bare context name names the one source context that has it (D-28)
	}
	var shown bool
	st := s.UpdateUI(func(u *UIState) {
		if u.Context == id && activeConnection(u.Connection.State) {
			u.NavigatorScope, shown = NavScopeCluster(id), true
		}
	})
	if shown {
		return st, nil
	}
	if err := s.Connect(ctx, id); err != nil {
		if st := s.UIState(); st.Connection.State == ConnectError && st.Context == id {
			return st, nil
		}
		return s.UIState(), err // refused before connecting: no connector, closed, or superseded
	}
	return s.UIState(), nil
}
