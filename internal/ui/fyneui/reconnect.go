package fyneui

import (
	"errors"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Reconnect in the window (D-81, the owner's F29: "ein Button in der Clusterübersicht, für einen
// Reconnect"): cluster.reconnect ends a connected cluster's connection and connects it anew from
// its kubeconfig file as it is now, its tabs kept. The window offers it
//
//   - as a ↻ button on every connected cluster's row in the navigator's section, the list of all
//     clusters (navigator_sections.go), and above the cluster Overview's charts (clusteroverview.go):
//     the owner's "Clusterübersicht" read both ways;
//   - in the cluster's menus beside Disconnect: the window's Cluster menu, a section row's and the
//     tree root's right-click menu, a hotbar tile's and a welcome-page row's menu (menus_cluster.go);
//   - in the connection banner: interrupted, and for a login expired without a credential plugin
//     (connbanner.go); in the Connection Updates dialog (connectlog.go); and in the palette, from the
//     registry's menu path Cluster/Reconnect.
//
// While it runs the cluster's dots blink as at a connect (clustermarks.go) and its items say
// "Reconnecting…", greyed. Lens has no Reconnect of its own (its cluster menu connects and
// disconnects, captured): Cogpit's own.

const (
	reconnectLabel   = "Reconnect"
	reconnectingText = "Reconnecting…"
	reconnectTip     = "Reconnect: a new connection from the kubeconfig file as it is now; the tabs stay"
)

// reconnectIcon is Reconnect's icon, the banner's and the shell tabs' too.
func reconnectIcon() fyne.Resource { return theme.ViewRefreshIcon() }

// reconnecting reports whether a cluster's connection is being made anew (Connection.Reconnecting).
func reconnecting(ui engine.UIState, ctx string) bool {
	if ctx == "" {
		return false
	}
	if ctx == ui.Context && ui.Connection.State.Live() {
		return ui.Connection.Reconnecting
	}
	c, ok := ui.ConnectionOf(ctx)
	return ok && c.Connection.Reconnecting
}

// reconnectable reports whether the window offers Reconnect for a cluster: connected (shown or
// not, interrupted or its login expired included), not the demo cluster.
func reconnectable(ui engine.UIState, ctx string) bool {
	if !clusterConnected(ui, ctx) || engine.IsDemoContext(ctx) {
		return false
	}
	if ctx == ui.Context {
		return !ui.Connection.Demo
	}
	c, _ := ui.ConnectionOf(ctx)
	return !c.Connection.Demo
}

// reconnect dispatches cluster.reconnect for a connected cluster. A credential plugin or an
// unchecked certificate the user has not allowed (as it is now) is shown for approval first, then
// the reconnect runs again; a reconnect that fails says why in the engine's toast and the
// connection log.
func (sh *shell) reconnect(contextID string) {
	if reconnecting(sh.ui, contextID) {
		return // the engine refuses it too; a second click says nothing
	}
	sh.dispatch("cluster.reconnect", contextParams{Context: contextID}, func(_ any, err error) {
		var dis *engine.DisabledError
		switch {
		case err == nil:
		case errors.Is(err, engine.ErrExecNotApproved):
			sh.kc.reviewPlugin(contextID, func(allowed bool) {
				if allowed {
					sh.dispatch("cluster.reconnect", contextParams{Context: contextID}, nil)
				}
			})
		case errors.As(err, &dis):
			sh.toast("warning", dis.Reason)
		}
	})
}

// reconnectItem is a cluster's Reconnect for its menus: nil where it does not apply (a cluster not
// connected, the demo cluster), greyed with "Reconnecting…" while it runs.
func (sh *shell) reconnectItem(contextID string) *fyne.MenuItem {
	if _, ok := sh.eng.Registry().Get("cluster.reconnect"); !ok || !reconnectable(sh.ui, contextID) {
		return nil
	}
	item := fyne.NewMenuItem(reconnectLabel, func() { sh.reconnect(contextID) })
	if reconnecting(sh.ui, contextID) {
		item.Label, item.Disabled = reconnectingText, true
	}
	return item
}
