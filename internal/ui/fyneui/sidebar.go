package fyneui

import (
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Tree node ids. A kind's node id is its kind key; a group's is groupPrefix and its path
// ("group:network/gatewayapi"); the API-group branches of Custom Resources are
// customGroupPrefix and the API group. A kind that a curated section places (Network › Gateway
// API › Gateways) is listed again under Custom Resources, as Lens does: that second node's id is
// aliasPrefix and the key, so ids stay unique, and selecting either opens the same view.
const (
	groupPrefix       = "group:"
	aliasPrefix       = "alias:"
	customGroup       = groupPrefix + "custom"
	customGroupPrefix = customGroup + "/"
	otherGroup        = groupPrefix + "other"
)

// Kind keys the shell refers to by name.
const (
	kindPods        = "pods"
	kindNamespaces  = "namespaces"
	kindServices    = "services"
	kindEvents      = "events"
	kindDefinitions = "customresourcedefinitions"
)

// navEntry is one node of navLayout: a kind, a page, or a group of entries.
type navEntry struct {
	id    string     // a kind key, a page key, or groupPrefix + the group's path
	title string     // a group's title; for a kind, a title in place of its own (a CRD kind's plural title)
	items []navEntry // a group's entries, in Lens's order
	page  bool       // a page (engine.IsPage), shown while a cluster is connected
	needs string     // a page shown only while the cluster serves this kind (the Argo CD Dashboard)
}

// page is a page of the navigator, titled as the engine titles it.
func page(key string) navEntry {
	title, _ := engine.PageTitle(key)
	return navEntry{id: key, title: title, page: true}
}

// navGroup builds a group; items are kind keys (strings) and navEntry values (titled kinds,
// sub-groups).
func navGroup(path, title string, items ...any) navEntry {
	g := navEntry{id: groupPrefix + path, title: title}
	for _, it := range items {
		switch e := it.(type) {
		case string:
			g.items = append(g.items, navEntry{id: e})
		case navEntry:
			g.items = append(g.items, e)
		}
	}
	return g
}

// titled is a kind the layout titles itself: CRD kinds in a curated section show Lens's plural
// title there ("Gateway Classes"), and their singular Kind name under Custom Resources.
func titled(key, title string) navEntry { return navEntry{id: key, title: title} }

// Gateway API kind keys (CRDs, docs/developer/architecture.md › Kind keys).
const (
	gatewayAPI  = ".gateway.networking.k8s.io"
	gatewayAPIX = ".gateway.networking.x-k8s.io"
)

// navLayout is the navigator's tree in the order of Lens's sidebar as captured from the running
// app (docs/parity/ui-parity.md, Cluster sidebar): top-level kinds and groups of kinds, with
// sub-groups (Config › Admission Policies, Network › Gateway API). Only kinds the cluster serves
// appear, and a group without any is left out: the Gateway API sub-group appears with its CRDs.
// Custom Resources is built from the cluster's kinds: Definitions, then one collapsed group per
// API group with its kinds by singular Kind name, the groups of curated sections included. Kinds
// the layout does not know and that no CRD defines go under Other. Pages (page) show while a
// cluster is connected: the cluster Overview first (as captured), Workloads › Overview, Flux ›
// Dashboard; a group whose kinds the cluster serves none of is left out with its pages (the Flux
// section without Flux). Listener Sets are listed under both groups Gateway API releases used for
// them.
var navLayout = []navEntry{
	page(engine.ClusterOverview),
	{id: "applications"}, // a virtual kind, the workloads grouped by app.kubernetes.io/instance
	fluxNav,              // Flux CD between Applications and Nodes, as captured (fluxnav.go)
	argoNavGroup,         // Argo CD after Flux CD, while the cluster serves its Applications (argocd.go)
	{id: "nodes"},
	navGroup("workloads", "Workloads", page(engine.WorkloadsOverview),
		kindPods, "deployments", "daemonsets", "statefulsets", "replicasets", "replicationcontrollers", "jobs", "cronjobs"),
	navGroup("config", "Config",
		"configmaps", "secrets", "resourcequotas", "limitranges", "horizontalpodautoscalers",
		titled("verticalpodautoscalers.autoscaling.k8s.io", "Vertical Pod Autoscalers"),
		"poddisruptionbudgets", "priorityclasses", "runtimeclasses", "leases",
		"mutatingwebhookconfigurations", "validatingwebhookconfigurations",
		navGroup("config/admissionpolicies", "Admission Policies",
			"mutatingadmissionpolicies", "mutatingadmissionpolicybindings",
			"validatingadmissionpolicies", "validatingadmissionpolicybindings")),
	navGroup("network", "Network",
		"services", "endpointslices", "endpoints", "ingresses", "ingressclasses", "networkpolicies",
		navGroup("network/gatewayapi", "Gateway API",
			titled("gatewayclasses"+gatewayAPI, "Gateway Classes"),
			titled("gateways"+gatewayAPI, "Gateways"),
			titled("httproutes"+gatewayAPI, "HTTP Routes"),
			titled("backendtlspolicies"+gatewayAPI, "Backend TLS Policies"),
			titled("grpcroutes"+gatewayAPI, "gRPC Routes"),
			titled("xlistenersets"+gatewayAPIX, "Listener Sets"),
			titled("listenersets"+gatewayAPI, "Listener Sets"),
			titled("referencegrants"+gatewayAPI, "Reference Grants"),
			titled("tcproutes"+gatewayAPI, "TCP Routes"),
			titled("tlsroutes"+gatewayAPI, "TLS Routes"),
			titled("udproutes"+gatewayAPI, "UDP Routes")),
		kindPortForwards),
	navGroup("storage", "Storage", "persistentvolumeclaims", "persistentvolumes", "storageclasses"),
	navGroup("devices", "Device Resources", "resourceclaims", "resourceclaimtemplates", "deviceclasses", "resourceslices"),
	{id: kindNamespaces},
	{id: kindEvents},
	navGroup("helm", "Helm", "helmcharts", "helmreleases"), // Charts from the repositories of Helm's repositories.yaml (G11, D-29)
	navGroup("access", "Access Control",
		"serviceaccounts", "clusterroles", "roles", "clusterrolebindings", "rolebindings", "podsecuritypolicies"),
	{id: customGroup, title: "Custom Resources", items: []navEntry{{id: kindDefinitions}}},
	{id: otherGroup, title: "Other"},
}

// Cogpit's own navigator icons, drawn for Cogpit (not taken from any other product): a network
// of three nodes, and stacked layers for namespaces.
var (
	iconNetwork = fyne.NewStaticResource("cogpit-network.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
		`<rect x="9" y="2" width="6" height="6"/><rect x="2" y="16" width="6" height="6"/><rect x="16" y="16" width="6" height="6"/>`+
		`<rect x="11" y="8" width="2" height="4"/><rect x="4" y="11" width="16" height="2"/>`+
		`<rect x="4" y="11" width="2" height="5"/><rect x="18" y="11" width="2" height="5"/></svg>`))
	iconLayers = fyne.NewStaticResource("cogpit-layers.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
		`<path d="M12 2 22 7 12 12 2 7Z"/><path d="M4.2 10.9 12 14.8 19.8 10.9 22 12 12 17 2 12Z"/>`+
		`<path d="M4.2 15.9 12 19.8 19.8 15.9 22 17 12 22 2 17Z"/></svg>`))
	// iconOverview is a gauge: an arc with a needle, for the cluster Overview.
	iconOverview = fyne.NewStaticResource("cogpit-overview.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
		`<path d="M12 4a10 10 0 0 0-10 10v3h3v-3a7 7 0 0 1 14 0v3h3v-3A10 10 0 0 0 12 4Z"/>`+
		`<path d="M11 15.5 16.5 9l1.4 1.4-5.6 6.4a1.5 1.5 0 1 1-1.3-1.3Z"/></svg>`))
)

// navIcons gives top-level items and groups an icon; kinds inside a group have none, as in Lens.
// The theme's icons are looked up when an item is drawn, never at package initialisation: no Fyne
// app runs then, and each lookup logged "Attempt to access current Fyne app when none is started"
// (ten lines at every start) and got the fallback theme's icon.
var navIcons = map[string]func() fyne.Resource{
	engine.ClusterOverview:    func() fyne.Resource { return iconOverview },
	"applications":            theme.ViewRestoreIcon,
	"nodes":                   theme.ComputerIcon,
	groupPrefix + "workloads": theme.GridIcon,
	groupPrefix + "config":    theme.SettingsIcon,
	groupPrefix + "network":   func() fyne.Resource { return iconNetwork },
	groupPrefix + "storage":   theme.StorageIcon,
	kindNamespaces:            func() fyne.Resource { return iconLayers },
	kindEvents:                theme.HistoryIcon,
	groupPrefix + "devices":   theme.ComputerIcon,
	groupPrefix + "helm":      theme.FileApplicationIcon,
	groupPrefix + argoGroup:   theme.ViewRefreshIcon, // Argo CD: a sync's circular arrows
	groupPrefix + "access":    theme.AccountIcon,
	groupPrefix + "custom":    theme.ListIcon,
	groupPrefix + "other":     theme.MoreHorizontalIcon,
}

// navIcon returns a navigator node's icon, or nil.
func navIcon(id string) fyne.Resource {
	if icon := navIcons[id]; icon != nil {
		return icon()
	}
	return nil
}

// sidebar is the navigator (navigator.go): the header (navigator_header.go) with the way up to the
// section or the section's title, and the search, and below it the scope it shows (UIState.NavigatorScope): the tree whose root is the connected
// cluster with its views below, or the section level listing the clusters (navigator_sections.go).
// Opening a view dispatches ui.openView; the open view's row follows UIState.View.
type sidebar struct {
	sh       *shell
	head     *navHeader
	tree     *navTree
	sec      *navSections // the section level, shown in the tree's place at the section scope
	section  bool         // the section level shows (the scope applied last)
	children map[string][]string
	titles   map[string]string
	parents  map[string]string // node id → its branch's id; "" for a top-level item
	open     map[string]bool   // the groups the user opened (the window's, not engine state)
	revealed string            // the view whose branches selectView last opened
	marked   string            // the node of the open view
	filter   string            // the search text; "" shows every item
	shown    map[string]bool   // with a search: the nodes it keeps

	// The search text and the keyboard's row reported to the engine (D-51; navigatorstate.go).
	searchRep, rowRep *navReport
	applying          bool // the engine's search is being shown: not a change to report

	// findFrom is the cluster whose tree ⌘P left for the section while the search it opened stays
	// open: Escape on the empty search goes back to it (findcluster.go, D-57); "" otherwise.
	findFrom string

	rootRow   *navRootRow
	badge     *clusterBadge
	name      *textLine
	subtitle  *textLine
	groups    *groupsBox        // the user's groups, beside the subtitle's username (clusteruser.go, D-68)
	userTip   string            // the subtitle's tooltip: a user not confirmed, or last seen when; "" for none
	subLine   fyne.CanvasObject // the subtitle and the groups' box
	dot       *connDot          // the root's connection dot (conndot.go, F13)
	emptyHint *textLine
	hintRow   fyne.CanvasObject // the hint below the root while no item shows
	root      fyne.CanvasObject

	// The cluster's Kubernetes version, the root's third line, and its tooltip, the minor's
	// upstream support (clusterversion.go, D-91).
	version    *textLine
	versionTip string
}

func newSidebar(sh *shell) *sidebar {
	s := &sidebar{sh: sh, children: map[string][]string{}, titles: map[string]string{}, parents: map[string]string{},
		open: map[string]bool{}}
	s.badge = newClusterBadge()
	s.name = newTextLine(notConnected, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	s.subtitle = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	// The sidebar's width is the user's (panelwidths.go): a long context name is cut, not widened.
	s.name.truncate, s.subtitle.truncate = true, true
	s.groups = newGroupsBox()
	s.subLine = box(&userLineLayout{text: s.subtitle, box: s.groups}, s.subtitle, s.groups)
	s.version = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	s.version.truncate = true
	s.version.Hide() // until the cluster's version is known (clusterversion.go)
	s.dot = newConnDot(sh)
	s.emptyHint = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	s.emptyHint.truncate = true
	s.hintRow = inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, s.emptyHint)

	s.tree = newNavTree(s)
	s.sec = newNavSections(s)
	s.sec.Hide() // until the first UI state says which scope shows (scopeChanged)
	s.head = newNavHeader(s)
	s.rootRow = newNavRootRow(s)
	// The header before the tree in the objects, so that Tab goes from the search field into the
	// tree (container.NewBorder would put the tree first).
	s.root = container.New(layout.NewBorderLayout(s.head.root, nil, nil, nil), s.head.root, container.NewStack(s.tree, s.sec))
	s.tree.rebuild()
	s.initNavigatorState()
	return s
}

// setScope dispatches ui.setNavigatorScope: the header's ‹ All Clusters and ← on the tree's root go
// up to the section, a cluster's row drills into a cluster, Escape on ⌘P's empty search goes back.
func (s *sidebar) setScope(scope string) {
	s.sh.dispatch("ui.setNavigatorScope", navScopeParams{Scope: scope}, nil)
}

// goUp shows the section level (the header's ‹ All Clusters, ← on the tree's root).
func (s *sidebar) goUp() { s.setScope(engine.NavScopeLocal) }

// scopeChanged shows the level the navigator's scope names: the section's clusters, or the
// connected cluster's tree. The search closes when the level changes (it filtered the other one;
// inferred), and a keyboard in the navigator follows to the level shown: from a cluster to its row
// in the section, from a row into the cluster's root.
func (s *sidebar) scopeChanged() {
	s.head.update()
	if s.sec.loadSeq == 0 {
		s.sec.load() // at either level: the clusters are read before ⌘P or ‹ All Clusters shows them
	}
	section := s.sh.ui.ScopeCluster() == ""
	if section == s.section && s.sec.Visible() == section {
		return
	}
	from := s.sh.ui.Context
	// The level's search and row are the engine's again (it reset them for the new level): a report
	// for the level shown so far would land at the new one.
	s.searchRep.cancel()
	s.rowRep.cancel()
	c := s.sh.win.Canvas()
	hadKeys := c.Focused() == s.tree || c.Focused() == s.sec
	byKeys := (s.section && s.sec.keys) || (!s.section && s.tree.keys) // the ring follows the keyboard, not a click (A53)
	s.section = section
	if s.head.searching() {
		s.head.closeSearch()
	}
	if section {
		s.tree.Hide()
		s.sec.Show()
		s.sec.shownFrom(from)
	} else {
		s.sec.Hide()
		s.tree.Show()
		s.tree.focus = rootNode
		s.tree.rebuild()
	}
	if hadKeys {
		s.tree.pointer, s.sec.pointer = !byKeys, !byKeys
		c.Focus(s.list())
		s.tree.pointer, s.sec.pointer = false, false
	}
}

// list is the level shown, which takes the keyboard: the section's clusters or the cluster's tree.
func (s *sidebar) list() fyne.Focusable {
	if s.section {
		return s.sec
	}
	return s.tree
}

// updateHeader shows the active context at the tree's root: an initials badge, its name (the one
// short name, D-68), the user Cogpit is logged in as there (else what it is), and the connection's
// dot; the header follows the scope.
func (s *sidebar) updateHeader() {
	ui := s.sh.ui
	s.head.update()
	if s.section {
		s.sec.render() // the clusters' dots, marks and identities
	}
	s.dot.setMark(ui.Context, s.sh.mark(ui.Context, false)) // the shown cluster's mark (clustermarks.go)
	s.rootRow.showCluster(ui.Context != "")
	s.showVersion(ui) // the cluster's Kubernetes version (clusterversion.go, D-91)
	if ui.Context == "" {
		s.badge.set("–", true)
		s.name.SetText(notConnected)
		s.setSubtitle("", colorMuted, "", nil)
		return
	}
	_, file := splitContextID(ui.Context)
	id := s.sh.identity(ui.Context) // the display name and icon from the cluster's settings
	s.badge.setIdentity(id)
	s.name.SetText(id.name)
	switch {
	case ui.Connection.Demo || engine.IsDemoContext(ui.Context):
		s.setSubtitle("Synthetic demo cluster", colorMuted, "", nil)
	case s.showUser(ui): // the user Cogpit is logged in as, or was last (clusteruser.go, D-68)
	case file != "":
		s.setSubtitle(file, colorMuted, "", nil) // the kubeconfig the context comes from (D-28: every context has one)
	default:
		s.setSubtitle("Kubernetes cluster", colorMuted, "", nil)
	}
}

// setKinds rebuilds the tree from kinds.list. It runs on connect and whenever the cluster's
// custom resource kinds or the access reviews' answers change; branches keep the open or closed
// state they had, and new ones start closed (owner, 2026-10-12; Lens opened them, and the API
// groups of Custom Resources closed): the group of the open view is opened by selectView, so the
// selection stays visible. A kind the user may not list is left out; one not known yet is shown.
func (s *sidebar) setKinds(kinds []kindInfo) {
	kinds = listableKinds(kinds) // the View menu offers the same (menuaccess.go)
	s.children = map[string][]string{}
	s.titles = map[string]string{}
	s.parents = map[string]string{}

	have := make(map[string]kindInfo, len(kinds))
	for _, k := range kinds {
		have[k.Kind] = k
	}
	placed := map[string]bool{} // kinds the layout's sections show
	var place func(n navEntry)
	place = func(n navEntry) {
		if _, ok := have[n.id]; ok {
			placed[n.id] = true
		}
		for _, c := range n.items {
			place(c)
		}
	}
	for _, n := range navLayout {
		place(n)
	}

	var roots []string
	for _, n := range navLayout {
		if id := s.build(n, "", have, placed, kinds); id != "" {
			roots = append(roots, id)
		}
	}
	s.children[""] = roots
	for id := range s.open { // a branch that went away starts closed if it comes back
		if _, ok := s.children[id]; !ok {
			delete(s.open, id)
		}
	}
	s.refilter()
	s.tree.rebuild()
}

// build adds a layout entry the cluster serves to the tree and returns its node id, or "" when
// the cluster serves none of it.
func (s *sidebar) build(n navEntry, parent string, have map[string]kindInfo, placed map[string]bool, kinds []kindInfo) string {
	var kids []string
	switch {
	case n.id == customGroup:
		kids = s.buildCustom(have, placed, kinds)
	case n.id == otherGroup:
		for _, k := range kinds {
			if !placed[k.Kind] && !isCustomKind(k.Kind) {
				kids = append(kids, s.leaf(k.Kind, k.Title, n.id))
			}
		}
	case strings.HasPrefix(n.id, groupPrefix):
		served := 0 // kinds and sub-groups: a group of pages alone is left out
		for _, c := range n.items {
			if id := s.build(c, n.id, have, placed, kinds); id != "" {
				kids = append(kids, id)
				if !c.page {
					served++
				}
			}
		}
		if served == 0 {
			for _, id := range kids { // its pages: no node is left for them
				delete(s.titles, id)
				delete(s.parents, id)
			}
			return ""
		}
	case n.page:
		if len(have) == 0 {
			return "" // not connected
		}
		if _, served := have[n.needs]; n.needs != "" && !served {
			return "" // a section's page without its kinds (argocd.go)
		}
		return s.leaf(n.id, n.title, parent)
	default:
		k, ok := have[n.id]
		if !ok {
			return ""
		}
		title := n.title
		if title == "" {
			title = k.Title
		}
		return s.leaf(n.id, title, parent)
	}
	if len(kids) == 0 {
		return ""
	}
	s.children[n.id] = kids
	s.titles[n.id] = n.title
	s.parents[n.id] = parent
	return n.id
}

// buildCustom fills Custom Resources: Definitions, then one branch per API group, sorted by name,
// with its kinds by singular Kind name, sorted. A kind a curated section shows already gets an
// alias node here.
func (s *sidebar) buildCustom(have map[string]kindInfo, placed map[string]bool, kinds []kindInfo) []string {
	var kids []string
	if k, ok := have[kindDefinitions]; ok {
		kids = append(kids, s.leaf(kindDefinitions, k.Title, customGroup))
	}
	byGroup := map[string][]kindInfo{}
	for _, k := range kinds {
		if isCustomKind(k.Kind) {
			g := apiGroupOf(k)
			byGroup[g] = append(byGroup[g], k)
		}
	}
	groups := make([]string, 0, len(byGroup))
	for g := range byGroup {
		groups = append(groups, g)
	}
	slices.Sort(groups)
	for _, g := range groups {
		branch := customGroupPrefix + g
		members := byGroup[g]
		slices.SortFunc(members, func(a, b kindInfo) int {
			if c := strings.Compare(customTitle(a), customTitle(b)); c != 0 {
				return c
			}
			return strings.Compare(a.Kind, b.Kind)
		})
		var nodes []string
		for _, k := range members {
			id := k.Kind
			if placed[id] {
				id = aliasPrefix + id
			}
			nodes = append(nodes, s.leaf(id, customTitle(k), branch))
		}
		s.children[branch] = nodes
		s.titles[branch] = g
		s.parents[branch] = customGroup
		kids = append(kids, branch)
	}
	return kids
}

// leaf records a kind's node and returns its id.
func (s *sidebar) leaf(id, title, parent string) string {
	s.titles[id] = title
	s.parents[id] = parent
	return id
}

// isCustomKind reports whether a kind key names a kind from a custom resource definition: those
// keys are "<plural>.<group>", and built-in keys never contain a dot (architecture §4.5).
func isCustomKind(key string) bool { return strings.Contains(key, ".") }

// apiGroupOf is a custom kind's API group: kinds.list's group, else the key's.
func apiGroupOf(k kindInfo) string {
	if k.Group != "" {
		return k.Group
	}
	_, g, _ := strings.Cut(k.Kind, ".")
	return g
}

// customTitle is a kind's title under Custom Resources: its singular Kind name (captured).
func customTitle(k kindInfo) string {
	switch {
	case k.APIKind != "":
		return k.APIKind
	case k.Title != "":
		return k.Title
	}
	return k.Kind
}

// selectView marks the open view in the tree without dispatching anything: its node in a curated
// section if it has one, else its node under Custom Resources. When the view changes, the
// branches above it open, so that a view an agent opened inside a closed API group is visible, and
// the keyboard moves to it.
func (s *sidebar) selectView(view string) {
	if view == "" || s.titles[view] == "" {
		s.revealed, s.marked = "", ""
		s.tree.rebuild()
		return
	}
	changed := view != s.revealed
	if changed {
		s.revealed = view
		for p := s.parents[view]; p != ""; p = s.parents[p] {
			s.open[p] = true
		}
	}
	s.marked = view
	s.tree.rebuild()
	if changed {
		s.tree.setFocus(view)
	}
}

// activate does what a click or ↵ on a node does: open its view, or open or close a group (not
// while a search shows every group open); the root does nothing. An alias node opens its kind's
// view; the mark then moves to the kind's primary node.
func (s *sidebar) activate(id string) {
	switch {
	case id == "" || id == rootNode:
		return
	case s.isBranch(id):
		if s.filter == "" {
			s.setOpen(id, !s.open[id])
		}
		return
	}
	key := strings.TrimPrefix(id, aliasPrefix)
	if key != s.sh.ui.View {
		s.marked = id
		s.tree.rebuild()
		s.sh.dispatch("ui.openView", openViewParams{View: key}, func(_ any, err error) {
			if err != nil { // the view stays: so does its mark
				s.sh.showError(err)
				s.selectView(s.sh.ui.View)
			}
		})
	} else if key != id {
		s.selectView(key)
	}
}

// setOpen opens or closes a group.
func (s *sidebar) setOpen(id string, open bool) {
	if !s.isBranch(id) || s.open[id] == open {
		return
	}
	if open {
		s.open[id] = true
	} else {
		delete(s.open, id)
	}
	s.tree.rebuild()
}

// isBranch reports whether a node is a group.
func (s *sidebar) isBranch(id string) bool { return strings.HasPrefix(id, groupPrefix) }

// shownOpen reports whether a group shows its items: the user opened it, or a search is on.
func (s *sidebar) shownOpen(id string) bool { return s.filter != "" || s.open[id] }

// parentOf is the node above id in the tree: its group, or the root for a top-level item.
func (s *sidebar) parentOf(id string) string {
	if p := s.parents[id]; p != "" {
		return p
	}
	return rootNode
}

// depth is how many groups hold a node.
func (s *sidebar) depth(id string) int {
	d := 0
	for p := s.parents[id]; p != ""; p = s.parents[p] {
		d++
	}
	return d
}

// shownNodes lists the items the tree shows, in order, below the root: those of open groups, and
// with a search those it keeps.
func (s *sidebar) shownNodes() []string {
	var out []string
	var walk func(parent string)
	walk = func(parent string) {
		for _, id := range s.children[parent] {
			if s.shown != nil && !s.shown[id] {
				continue
			}
			out = append(out, id)
			if s.isBranch(id) && s.shownOpen(id) {
				walk(id)
			}
		}
	}
	walk("")
	return out
}

// searchTyped follows the search field's text: it filters at once and reports the text (A51).
func (s *sidebar) searchTyped(text string) {
	s.setFilter(text)
	s.noteSearch(text)
}

// setFilter applies the search text (the header's field).
func (s *sidebar) setFilter(text string) {
	text = strings.TrimSpace(text)
	if text == s.filter {
		return
	}
	s.filter = text
	s.refilter()
	s.tree.rebuild()
	if s.section {
		s.sec.render()
	}
	if text != "" {
		s.tree.scroll.ScrollToTop() // the first matches
	} else {
		s.tree.reveal(s.tree.focus)
	}
}

// refilter works out which nodes the search keeps: an item whose title holds the text (ignoring
// case) with the groups above it, and a group whose title holds it with everything inside.
func (s *sidebar) refilter() {
	if s.filter == "" {
		s.shown = nil
		return
	}
	f := strings.ToLower(s.filter)
	s.shown = map[string]bool{}
	var visit func(id string, inMatch bool) bool
	visit = func(id string, inMatch bool) bool {
		match := inMatch || strings.Contains(strings.ToLower(s.titles[id]), f)
		keep := match
		for _, c := range s.children[id] {
			if visit(c, match) {
				keep = true
			}
		}
		if keep {
			s.shown[id] = true
		}
		return keep
	}
	for _, id := range s.children[""] {
		visit(id, false)
	}
}

// hint is the line below the root while no item shows: no cluster, or nothing matches the search.
func (s *sidebar) hint() string {
	switch {
	case len(s.children[""]) == 0:
		return "Connect a cluster to browse it."
	case s.filter != "" && len(s.shown) == 0:
		return "No items match “" + s.filter + "”."
	}
	return ""
}

// openFirstMatch opens the first view the search shows (↵ in the search field), and puts the
// keyboard there.
func (s *sidebar) openFirstMatch() {
	if s.section {
		s.sec.openFirstMatch() // the section's first cluster
		return
	}
	for _, id := range s.tree.visible {
		if id != rootNode && !s.isBranch(id) {
			s.tree.setFocus(id)
			s.activate(id)
			return
		}
	}
}

// initials returns up to two letters for a context's badge: "kind-cogpit" → "KC".
func initials(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' || r == '.' || r == '@' || r == ':' || r == '/' })
	if len(parts) == 0 {
		return "?"
	}
	var b []rune
	for _, p := range parts {
		r := []rune(p)
		if len(r) > 0 {
			b = append(b, r[0])
		}
		if len(b) == 2 {
			break
		}
	}
	if len(b) == 1 && len([]rune(parts[0])) > 1 {
		b = append(b, []rune(parts[0])[1])
	}
	return strings.ToUpper(string(b))
}
