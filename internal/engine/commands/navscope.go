package commands

import (
	"context"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The navigator's scope (engine/navscope.go; ui-parity › Layout and Clusters). Which level the
// navigator shows is UI state (the tab bar shows the shown cluster's tabs at either level, GW11), so agents see and drive
// it as the user does (D-2: a UI command, run freely; into another cluster it connects, as
// cluster.connect does, which only reads).

type navScopeParams struct {
	Scope string `json:"scope" jsonschema:"section:local for the Local Kubeconfigs section (every cluster Cogpit offers), or cluster:<context id> for a cluster's tree (context id as cluster_contexts lists it). The tab bar shows the shown cluster's tabs at either level. A cluster that is not connected is connected first"`
}

func registerNavScope(g *registrar) {
	add(g, engine.Spec[navScopeParams]{
		ID: "ui.setNavigatorScope", Label: "Show in Navigator", Danger: engine.DangerUI, Verbs: cacheReads,
		Description: "Sets what the navigator shows, as its header's ‹ All Clusters, ⌘P (Connect to Cluster) and the › on a " +
			"cluster's row do: section:local, the Local Kubeconfigs section listing every cluster Cogpit offers, or " +
			"cluster:<context id>, that cluster's resource tree. The tab bar shows the shown cluster's tabs at either level " +
			"(ui_tab_select's index, next and previous count those). Going up to " +
			"the section keeps the connection. A cluster that is not the shown one is shown as cluster_connect shows it: " +
			"one that is connected already at once, from the cache, another one connected first (connecting only reads; " +
			"the cluster shown before stays connected); a failed connect leaves the section with connection saying why, one " +
			"that goes on in the background because another cluster was shown while it waited answers the state too, and " +
			"one cancelled meanwhile (cluster_disconnect) a cancelled error. " +
			"Every connect drills into its cluster, and without a shown cluster the scope is the section. Returns the new UI " +
			"state; navigatorScope is the scope.",
		Run: func(ctx context.Context, s *engine.Session, p navScopeParams) (any, error) {
			return s.SetNavigatorScope(ctx, p.Scope)
		},
	})
}

// The navigator's own state (engine/navigator.go, D-51): its search text, the row its keyboard is
// on and the Local Kubeconfigs section's collapsed source groups, which the window reports as the
// user types, moves and clicks, and an agent reads in ui_state (navigator) and sets as the user does
// (D-2: UI commands, run freely). They have no menu path: the window's own fields and keys set them.

type navSearchParams struct {
	Search string `json:"search" jsonschema:"the text to search the navigator for, one line of at most 1000 bytes; empty clears the search"`
}

type navRowParams struct {
	Row string `json:"row" jsonschema:"the row to put the navigator's keyboard on, of the level navigatorScope shows. At a cluster: root (the cluster), a view key as ui_openView takes it (pods, workloads/overview), group:<path> (group:workloads, group:network/gatewayapi) or alias:<kind key> (a kind listed again under Custom Resources). At the section: root (Local Kubeconfigs), context:<context id> (a cluster) or source:<label> (a source group's heading, .kube/ or dev.yaml)"`
}

type navGroupParams struct {
	Group     string `json:"group" jsonschema:"the source group's label as its heading in the Local Kubeconfigs section shows it: the folder (.kube/) or the file (dev.yaml) its clusters come from"`
	Collapsed bool   `json:"collapsed" jsonschema:"true collapses the group (its clusters hidden under its heading), false opens it"`
}

func registerNavigator(g *registrar) {
	add(g, engine.Spec[navSearchParams]{
		ID: "ui.setNavigatorSearch", Label: "Search the Navigator", Danger: engine.DangerUI,
		Description: "Types into the navigator's search field, as the user does after its search button or a letter typed " +
			"in the navigator: at a cluster it filters the tree's items by title (an item with the groups above it, a group " +
			"with everything in it), at the Local Kubeconfigs section the clusters by name, display name and source. An " +
			"empty text clears the search. It does not move the keyboard focus. The search clears when the navigator " +
			"changes level (ui_setNavigatorScope, a connect, a disconnect). ui_state reports it as navigator.search. " +
			"Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, p navSearchParams) (any, error) {
			return s.SetNavigatorSearch(p.Search)
		},
	})

	add(g, engine.Spec[navRowParams]{
		ID: "ui.setNavigatorRow", Label: "Move the Navigator's Keyboard", Danger: engine.DangerUI,
		Description: "Puts the navigator's keyboard on a row of the level it shows, as the arrow keys and a click do: " +
			"where the ring is while the navigator has the keyboard focus, and the row Enter opens (a view, in the preview " +
			"tab) or drills into (a cluster) once the user presses it. It does not move the keyboard focus and opens " +
			"nothing. A row inside a closed group opens the groups above it; a cluster inside a collapsed source group " +
			"puts the keyboard on the group's heading; a row the navigator does not show leaves the keyboard where it is. " +
			"Opening a view at a cluster puts it on the view's row, a change of level on the level's root (or, up at the " +
			"section, on the cluster it came from). ui_state reports it as navigator.row. Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, p navRowParams) (any, error) {
			return s.SetNavigatorRow(p.Row)
		},
	})

	add(g, engine.Spec[navGroupParams]{
		ID: "ui.setNavigatorGroup", Label: "Collapse a Source Group", Danger: engine.DangerUI,
		Description: "Collapses or opens a source group of the navigator's Local Kubeconfigs section, as a click on its " +
			"heading (or ← and →) does: a collapsed group shows its heading only. Which groups are collapsed is kept " +
			"across restarts. ui_state reports them as navigator.collapsed. Returns the new UI state.",
		Run: func(_ context.Context, s *engine.Session, p navGroupParams) (any, error) {
			return s.SetNavigatorGroup(p.Group, p.Collapsed)
		},
	})
}
