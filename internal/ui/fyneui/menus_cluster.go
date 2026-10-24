package fyneui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// A cluster's Disconnect and Remove from Cogpit (ui-parity › Clusters, "Cluster menu: connect,
// disconnect, settings, add to hotbar, remove"; G13): in the window's Cluster menu, the navigator's
// section rows and root, a hotbar tile's menu and the welcome page's rows.
// Disconnect (cluster.disconnect) closes Cogpit's connection; Remove from Cogpit (cluster.remove)
// removes Cogpit's link to the kubeconfig file the user added for the cluster, after a confirmation
// that names the path and every context that goes with it, and never edits or deletes the file. A
// context of a synced folder (the default ~/.kube too) or of a file $KUBECONFIG names cannot be
// removed that way: its item is greyed with the reason (cluster.links). The labels are Cogpit's
// (Lens's are not captured): "Remove from Cogpit" says what goes, and stays apart from the hotbar's
// "Remove from Hotbar" in the same menu. The demo cluster has no Remove: it comes from no file.

// linksFresh is how long the clusters' links are taken as current before a menu reads them again.
const linksFresh = 2 * time.Second

// contextLinkView is one entry of cluster.links' result.
type contextLinkView struct {
	Context   string       `json:"context"`
	Source    sourceStatus `json:"source"`
	File      string       `json:"file"`
	Contexts  []string     `json:"contexts"`
	Removable bool         `json:"removable"`
	Reason    string       `json:"reason"`
	Folder    string       `json:"folder"`
}

// clusterRemoveParams mirrors cluster.remove's and cluster.links' parameters.
type clusterRemoveParams struct {
	Context  string   `json:"context"`
	Contexts []string `json:"contexts,omitempty"`
}

// clusterLinks holds the clusters' links for the menus, read off the main goroutine (they read the
// kubeconfig files). A menu built before they are known offers Remove, and Remove asks the engine
// again before its confirmation. Main goroutine.
type clusterLinks struct {
	sh       *shell
	byCtx    map[string]contextLinkView
	none     bool // the engine keeps no kubeconfig sources: Remove does not apply
	at       time.Time
	loading  bool
	menuFor  string                // the window menu's Remove item as last built, to rebuild it on a change
	removing *dialog.ConfirmDialog // the open confirmation, for tests
}

func newClusterLinks(sh *shell) *clusterLinks { return &clusterLinks{sh: sh} }

// linksRead is a read of cluster.links.
type linksRead struct {
	byCtx map[string]contextLinkView
	none  bool
}

// read asks the engine for every context's link. Off the main goroutine.
func (l *clusterLinks) read() linksRead {
	res, err := l.sh.eng.Dispatch(l.sh.ctx, engine.Invocation{ID: "cluster.links", Params: json.RawMessage(`{}`), Origin: engine.OriginGUI})
	if err != nil {
		return linksRead{none: true} // no sources (or an engine without the command): nothing to remove
	}
	var out struct {
		Links []contextLinkView `json:"links"`
	}
	if decode(res, &out) != nil {
		return linksRead{none: true}
	}
	m := make(map[string]contextLinkView, len(out.Links))
	for _, link := range out.Links {
		m[link.Context] = link
	}
	return linksRead{byCtx: m}
}

// set takes a read and rebuilds the window's menu if its Remove item changes.
func (l *clusterLinks) set(r linksRead) {
	l.byCtx, l.none, l.at = r.byCtx, r.none, time.Now()
	state := ""
	if item := l.sh.removeItem(l.sh.ui.Context); item != nil {
		state = item.Label + fmt.Sprint(item.Disabled)
	}
	if state != l.menuFor {
		l.menuFor = state
		l.sh.setMainMenu()
	}
}

// refresh reads the links again unless a read is under way or the last one is fresh.
func (l *clusterLinks) refresh() {
	if l.loading || time.Since(l.at) < linksFresh {
		return
	}
	l.loading = true
	go func() {
		defer l.sh.recoverTo("the clusters' links")
		r := l.read()
		l.sh.do(func() {
			l.loading = false
			l.set(r)
		})
	}()
}

// copyPathLabel is a cluster's menu item that copies the path of the kubeconfig file the context
// comes from (the owner's VW13 of 2026-10-24: "den Pfad zur kube-config in den Zwischenspeicher
// kopieren", in every menu of a cluster, connected or not). Cogpit's own: Lens's cluster menu is not
// captured.
const copyPathLabel = "Copy Kubeconfig Path"

// contextInfo is a context as the window last read the contexts: the navigator's list, else the
// welcome page's (both cluster.contexts, read off the main goroutine and again on EventContexts).
func (sh *shell) contextInfo(contextID string) (engine.ContextInfo, bool) {
	if sh.side != nil && sh.side.sec != nil {
		if c, ok := sh.side.sec.context(contextID); ok {
			return c, true
		}
	}
	if sh.tabs != nil && sh.tabs.welcome != nil {
		for _, c := range sh.tabs.welcome.contexts {
			if c.Name == contextID {
				return c, true
			}
		}
	}
	return engine.ContextInfo{}, false
}

// copyPathItem is a cluster's "Copy Kubeconfig Path" in its menus (the hotbar's tile, the
// navigator's root and rows, the welcome page's rows): the path of the file the context comes from
// (ContextInfo.File) on the clipboard and a toast naming it, a path being no secret. A GUI behaviour,
// no command (copying never is one, D-25, TestCopyHasNoCommand); an agent reads the path in
// cluster_contexts' file and cluster_settings_get's kubeconfig. Nil where the context comes from no
// file of its own: the demo cluster, the contexts kubectl's way merges from the default files
// (without a configuration directory there are no kubeconfig sources), and a context the window does
// not know (a hotbar tile whose context went, the list not read yet).
func (sh *shell) copyPathItem(contextID string) *fyne.MenuItem {
	c, ok := sh.contextInfo(contextID)
	if !ok || c.File == "" {
		return nil
	}
	path := c.File
	return fyne.NewMenuItem(copyPathLabel, func() {
		sh.copyText(path, false)
		sh.copyToast("info", "Copied the kubeconfig path "+kubeconfig.DisplayText(path, maxShownPath))
	})
}

// cancelConnectingLabel is cluster.disconnect's item while the cluster connects (D-73): it ends the
// connect and the login it waits for. Lens's item while connecting is not captured (inferred).
const cancelConnectingLabel = "Cancel Connecting"

// connectingInBackground reports whether a cluster the window does not show connects (the restore
// of the last session, D-69).
func connectingInBackground(ui engine.UIState, contextID string) bool {
	c, ok := ui.ConnectionOf(contextID)
	return ok && contextID != ui.Context && c.Connection.State == engine.Connecting
}

// clusterItems are a cluster's Reconnect and Disconnect (while it is the shown one connected, or
// another connected one, D-69, D-81), or Cancel Connecting while it connects (D-73), and Remove from
// Cogpit, for its menus.
func (sh *shell) clusterItems(contextID string) []*fyne.MenuItem {
	var items []*fyne.MenuItem
	if item := sh.reconnectItem(contextID); item != nil { // reconnect.go
		items = append(items, item)
	}
	st := sh.ui.Connection.State
	c, ok := sh.eng.Registry().Get("cluster.disconnect")
	switch {
	case !ok || contextID == "":
	case contextID == sh.ui.Context && st == engine.Connecting:
		items = append(items, fyne.NewMenuItem(cancelConnectingLabel, sh.disconnect))
	case contextID == sh.ui.Context && st.Live():
		items = append(items, fyne.NewMenuItem(c.Label, sh.disconnect))
	case clusterConnected(sh.ui, contextID): // another connected cluster: it alone, the shown one stays
		items = append(items, fyne.NewMenuItem(c.Label, func() { sh.disconnectOther(contextID) }))
	case connectingInBackground(sh.ui, contextID):
		items = append(items, fyne.NewMenuItem(cancelConnectingLabel, func() {
			sh.dispatch("cluster.disconnect", contextParams{Context: contextID}, nil)
		}))
	}
	if item := sh.removeItem(contextID); item != nil {
		items = append(items, item)
	}
	if len(items) > 0 {
		items = append([]*fyne.MenuItem{fyne.NewMenuItemSeparator()}, items...)
	}
	return items
}

// clusterMenuItems are the window's Cluster menu's Reconnect and Disconnect, greyed while no cluster
// is connected, and Remove from Cogpit for the window's cluster.
func (sh *shell) clusterMenuItems() []*fyne.MenuItem {
	var items []*fyne.MenuItem
	if _, ok := sh.eng.Registry().Get("cluster.reconnect"); ok {
		item := sh.reconnectItem(sh.ui.Context) // reconnect.go
		if item == nil {
			item = fyne.NewMenuItem(reconnectLabel, nil)
			item.Disabled = true
		}
		items = append(items, item)
	}
	if c, ok := sh.eng.Registry().Get("cluster.disconnect"); ok {
		item := fyne.NewMenuItem(c.Label, sh.disconnect)
		st := sh.ui.Connection.State
		item.Disabled = !st.Live() && st != engine.Connecting
		if st == engine.Connecting {
			item.Label = cancelConnectingLabel
		}
		items = append(items, item)
	}
	if item := sh.removeItem(sh.ui.Context); item != nil {
		items = append(items, item)
	}
	return items
}

// disconnect closes the connection (cluster.disconnect); the workspace shows the welcome page.
func (sh *shell) disconnect() {
	if st := sh.ui.Connection.State; st.Live() || st == engine.Connecting {
		sh.dispatch("cluster.disconnect", struct{}{}, nil)
	}
}

// disconnectOther closes a connected cluster the window does not show (cluster.disconnect with its
// context, D-69): its tabs close and its streams end; the shown cluster and the window stay.
func (sh *shell) disconnectOther(contextID string) {
	if switchable(sh.ui, contextID) {
		sh.dispatch("cluster.disconnect", contextParams{Context: contextID}, nil)
	}
}

// removeItem is a context's "Remove from Cogpit…", greyed with the reason when its link cannot be
// removed; nil where Remove does not apply (no context, the demo cluster, an engine without
// kubeconfig sources).
func (sh *shell) removeItem(contextID string) *fyne.MenuItem {
	c, ok := sh.eng.Registry().Get("cluster.remove")
	links := sh.tabs.welcome.links
	if !ok || contextID == "" || engine.IsDemoContext(contextID) {
		return nil
	}
	links.refresh()
	if links.none {
		return nil
	}
	item := fyne.NewMenuItem(c.Label+"…", func() { sh.confirmRemoveCluster(contextID) })
	if link, known := links.byCtx[contextID]; known && !link.Removable {
		item.Label = c.Label + " — " + shorten(link.Reason, maxReasonRunes)
		item.Disabled = true
	}
	return item
}

// confirmRemoveCluster asks the engine for the context's link as it is now, then asks the user,
// naming the file's path and every context that leaves with it; only then does cluster.remove run.
func (sh *shell) confirmRemoveCluster(contextID string) {
	links := sh.tabs.welcome.links
	raw, _ := json.Marshal(clusterRemoveParams{Context: contextID})
	go func() {
		defer sh.recoverTo("cluster.links")
		res, err := sh.eng.Dispatch(sh.ctx, engine.Invocation{ID: "cluster.links", Params: raw, Origin: engine.OriginGUI})
		sh.do(func() {
			var out struct {
				Links []contextLinkView `json:"links"`
			}
			if err == nil {
				err = decode(res, &out)
			}
			if err == nil && len(out.Links) != 1 {
				err = fmt.Errorf("no link for %s", kubeconfig.DisplayText(contextID, maxShownPath))
			}
			if err != nil {
				sh.showError(err)
				return
			}
			link := out.Links[0]
			name := sh.identity(contextID).name
			if !link.Removable {
				sh.toast("warning", "Cannot remove "+name+" from Cogpit: "+link.Reason)
				return
			}
			title, msg := removeClusterText(link, name)
			width := sh.app.Settings().Theme().Size(sizePickerWidth)
			button, done := "Remove", "Removed "+name+" from Cogpit; "+kubeconfig.DisplayText(link.File, maxShownPath)+" stays on disk."
			if link.Folder != "" {
				button = "Remove Folder"
				done = "Removed the folder " + kubeconfig.DisplayText(link.Folder, maxShownPath) + " from Cogpit; its files stay on disk."
			}
			d := dialog.NewCustomConfirm(title, button, "Cancel", newParagraph(msg, theme.ColorNameForeground, width), func(ok bool) {
				links.removing = nil
				if !ok {
					return
				}
				// The removal is bound to the contexts the dialog named (A97): a folder that gained one since is refused.
				sh.dispatch("cluster.remove", clusterRemoveParams{Context: contextID, Contexts: link.Contexts}, func(_ any, err error) {
					if err != nil {
						sh.showError(err)
					} else {
						sh.toast("info", done)
					}
					links.at = time.Time{}
					links.refresh()
					sh.kc.contextsChanged()
				})
			}, sh.win)
			links.removing = d
			sh.showDialog(d)
		})
	}()
}

// removeClusterText is the Remove from Cogpit confirmation: the Kubeconfig Sources' text for the
// file (its path, that only Cogpit's link goes and the file stays), and every context that leaves
// Cogpit with the link.
func removeClusterText(link contextLinkView, name string) (title, msg string) {
	_, msg = removeText(link.Source)
	title = "Remove " + kubeconfig.DisplayText(name, maxShownArg) + " from Cogpit?"
	names := make([]string, 0, len(link.Contexts))
	for _, id := range link.Contexts {
		names = append(names, kubeconfig.DisplayText(engine.ContextLabel(id), maxShownArg))
	}
	switch {
	case link.Folder != "":
		// A97: a synced folder's context leaves with the whole folder, every context of it named.
		title = "Remove " + kubeconfig.DisplayText(name, maxShownArg) + " and its folder from Cogpit?"
		msg = kubeconfig.DisplayText(name, maxShownArg) + " comes from a synced folder, and Cogpit can only remove it with the " +
			"whole folder.\n\n" + msg
		msg += fmt.Sprintf("\n\nThe folder's files define %s, and all of them leave Cogpit: %s.", pluralOf(len(names), "context"),
			strings.Join(names, ", "))
	case len(names) > 1:
		msg += fmt.Sprintf("\n\nThe file defines %d contexts, and all of them leave Cogpit: %s.", len(names), strings.Join(names, ", "))
	}
	return title, msg
}
