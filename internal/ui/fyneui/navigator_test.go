package fyneui

import (
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// navScene is the shell on kind-cogpit with cert-manager, the Gateway API and a Vertical Pod
// Autoscaler installed, the Pods view open: Workloads is open, every other group closed.
func navScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := crdFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.side.marked == "pods" })
	return sh, w, q, f
}

// shownTitles is what the tree shows below its root, by title.
func shownTitles(s *sidebar) []string {
	var out []string
	for _, id := range s.tree.visible[1:] {
		out = append(out, s.titles[id])
	}
	return out
}

// openViews are the views ui.openView was asked for, in order.
func openViews(t *testing.T, f *fakeEngine) []string {
	t.Helper()
	var out []string
	for _, c := range f.callsOf("ui.openView") {
		out = append(out, decodeParams[openViewParams](t, c).View)
	}
	return out
}

// The tree's root is the cluster (captured): its badge as the hotbar draws it, its name and
// kubeconfig, a connection dot and a ›; the header's ‹ All Clusters goes up to the section level
// (ui.setNavigatorScope; navigator_sections.go), the root has no ‹ of its own (D-57). Without a
// cluster the navigator shows the section; a connecting cluster's root says so.
func TestNavigatorRootIsTheCluster(t *testing.T) {
	sh, _, q, f := navScene(t, ThemeDark)
	fakeNavScope(t, f)
	s := sh.side
	if s.tree.visible[0] != rootNode || s.name.text != "kind-cogpit" || s.subtitle.text != "Kubernetes cluster" {
		t.Errorf("root %q %q / %q", s.tree.visible[0], s.name.text, s.subtitle.text)
	}
	if s.badge.text != "KC" || s.badge.muted || s.dot.color != connectionColor(engine.Connection{State: engine.Connected}) {
		t.Errorf("badge %q (muted %v), dot %q", s.badge.text, s.badge.muted, s.dot.color)
	}
	if got := s.rootRow.tipText(); got != "kind-cogpit - Connected" {
		t.Errorf("root tooltip %q", got)
	}
	if got := shownTitles(s); !slices.Equal(got, []string{"Overview", "Nodes", "Workloads", "Overview", "Pods", "Deployments", "Config",
		"Network", "Namespaces", "Events", "Custom Resources"}) {
		t.Errorf("tree %v", got)
	}
	if !s.head.back.Visible() || s.head.title.Visible() {
		t.Errorf("header: back %v, title %v", s.head.back.Visible(), s.head.title.Visible())
	}

	test.Tap(s.head.back)
	q.until(t, "the section", func() bool { return s.sec.Visible() && !s.tree.Visible() })
	if got := scopeCalls(t, f); !slices.Equal(got, []string{engine.NavScopeLocal}) || s.head.searching() {
		t.Errorf("‹ All Clusters: scopes %q, search open %v", got, s.head.searching())
	}

	f.update(func(u *engine.UIState) {
		u.Context, u.Connection, u.View, u.NavigatorScope = "", engine.Connection{State: engine.Disconnected}, "", ""
	})
	q.until(t, "the disconnect", func() bool { return s.name.text == "Not connected" })
	if s.badge.text != "–" || !s.badge.muted || len(s.tree.visible) != 1 || s.emptyHint.text != "Connect a cluster to browse it." ||
		!s.head.title.Visible() || s.head.back.Visible() || s.rootRow.tipText() != "" || !s.sec.Visible() || s.tree.Visible() {
		t.Errorf("without a cluster: badge %q, %d rows, hint %q, title %v, section %v", s.badge.text, len(s.tree.visible),
			s.emptyHint.text, s.head.title.Visible(), s.sec.Visible())
	}
	if s.dot.Visible() || s.rootRow.next.Visible() || s.subLine.Visible() {
		t.Error("without a cluster the root shows a dot, a › or a subtitle")
	}
	f.update(func(u *engine.UIState) {
		u.Context, u.Connection = "kind-cogpit", engine.Connection{State: engine.Connecting}
	})
	q.until(t, "the connecting cluster", func() bool { return s.name.text == "kind-cogpit" && s.tree.Visible() })
	// The state is the dot's (F13); the subtitle says what the cluster is: no user known yet (D-68).
	if !s.dot.Visible() || !s.rootRow.next.Visible() || s.subtitle.text != "Kubernetes cluster" ||
		s.dot.color != connectionColor(engine.Connection{State: engine.Connecting}) {
		t.Errorf("connecting: dot %v %q, › %v, subtitle %q", s.dot.Visible(), s.dot.color, s.rootRow.next.Visible(), s.subtitle.text)
	}
}

// The root's menu (right-click) has the hotbar's item for the cluster; without a hotbar it has none
// and does not open.
func TestNavigatorRootMenu(t *testing.T) {
	sh, _, _, _ := navScene(t, ThemeDark)
	sh.side.rootRow.TappedSecondary(&fyne.PointEvent{})
	if sh.side.rootRow.menuModel != nil {
		t.Error("a menu without a hotbar")
	}

	f := crdFake(t)
	withHotbar(f)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the hotbar", func() bool { return sh.hot.root.Visible() && len(sh.kinds) > 0 })
	sh.side.rootRow.TappedSecondary(&fyne.PointEvent{})
	m := sh.side.rootRow.menuModel
	if m == nil || len(m.Items) != 1 || m.Items[0].Label != "Add to Hotbar" {
		t.Fatalf("root menu %+v", m)
	}
	m.Items[0].Action()
	q.until(t, "the cluster pinned", func() bool { return len(f.callsOf("hotbar.addCluster")) == 1 })
	if p := decodeParams[hotbarClusterParams](t, f.callsOf("hotbar.addCluster")[0]); p.Context != "kind-cogpit" {
		t.Errorf("hotbar.addCluster %+v", p)
	}
}

// Hovering an item shows "<item> - <context>" (captured), the context by its name without its
// kubeconfig file; the root shows the context and its state; leaving the row hides it.
func TestNavigatorHoverText(t *testing.T) {
	sh, w, q, _ := navScene(t, ThemeDark)
	s := sh.side
	sh.tip.delay = time.Millisecond
	settle(w)
	centre := func(o fyne.CanvasObject) fyne.Position {
		return fyne.CurrentApp().Driver().AbsolutePositionForObject(o).AddXY(o.Size().Width/2, o.Size().Height/2)
	}
	for id, want := range map[string]string{
		"nodes":                 "Nodes - kind-cogpit",
		groupPrefix + "network": "Network - kind-cogpit",
		rootNode:                "kind-cogpit - Connected",
	} {
		test.MoveMouse(w.Canvas(), centre(s.tree.rowOf(id)))
		q.until(t, "the tooltip of "+id, func() bool { return sh.tip.visibleText() == want })
	}
	test.MoveMouse(w.Canvas(), fyne.NewPos(800, 400)) // the table
	q.until(t, "the tooltip to close", func() bool { return sh.tip.visibleText() == "" })

	if got := navTip("Pods", "kind-dev [.kube/config]"); got != "Pods - kind-dev" {
		t.Errorf("a source context's tooltip %q", got)
	}
}

// The search button opens a field that filters the tree by name: matching items with the groups
// above them, groups shown open without changing what the user opened; Escape clears and closes it,
// ↓ moves into the tree, ↵ opens the first match, and a letter typed in the tree starts a search.
func TestNavigatorSearch(t *testing.T) {
	sh, w, q, f := navScene(t, ThemeDark)
	s, h, c := sh.side, sh.side.head, w.Canvas()
	before := shownTitles(s)

	test.Tap(h.searchB)
	if !h.searching() || c.Focused() != h.entry {
		t.Fatalf("search open %v, focused %T", h.searching(), c.Focused())
	}
	test.Type(h.entry, "POD")
	want := []string{"Workloads", "Pods", "Config", "Vertical Pod Autoscalers", "Custom Resources", "autoscaling.k8s.io", "VerticalPodAutoscaler"}
	if got := shownTitles(s); !slices.Equal(got, want) {
		t.Errorf("filtered %v, want %v", got, want)
	}
	if s.tree.IsBranchOpen(groupPrefix+"config") || !s.shownOpen(groupPrefix+"config") {
		t.Error("the search changed the open groups, or shows Config closed")
	}
	// A group that matches shows everything in it.
	h.entry.SetText("network")
	if got := shownTitles(s); !slices.Equal(got, []string{"Network", "Services", "Gateway API", "Gateway Classes", "Gateways",
		"HTTP Routes", "Reference Grants", "TCP Routes", // and the API group whose name holds the text, with its kinds
		"Custom Resources", "gateway.networking.k8s.io", "Gateway", "GatewayClass", "HTTPRoute", "ReferenceGrant", "TCPRoute"}) {
		t.Errorf("a matching group shows %v", got)
	}
	h.entry.SetText("zzz")
	if len(s.tree.visible) != 1 || s.emptyHint.text != "No items match “zzz”." {
		t.Errorf("no match: %d rows, hint %q", len(s.tree.visible), s.emptyHint.text)
	}

	h.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if h.searching() || s.filter != "" || !slices.Equal(shownTitles(s), before) || c.Focused() != s.tree {
		t.Errorf("after Escape: open %v, filter %q, tree %v, focused %T", h.searching(), s.filter, shownTitles(s), c.Focused())
	}

	// A letter typed in the tree starts a search with it; ↓ moves into the matches.
	s.tree.TypedRune('d')
	s.tree.TypedRune(' ') // no search for a space: it is ↵'s twin
	test.Type(h.entry, "eploy")
	if !h.searching() || s.filter != "deploy" || !slices.Equal(shownTitles(s), []string{"Workloads", "Deployments"}) {
		t.Errorf("typed into the tree: open %v, filter %q, tree %v", h.searching(), s.filter, shownTitles(s))
	}
	h.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
	if c.Focused() != s.tree || s.tree.focus != groupPrefix+"workloads" {
		t.Errorf("↓ from the field: focused %T on %q", c.Focused(), s.tree.focus)
	}

	// ↵ in the field opens the first match through ui.openView.
	c.Focus(h.entry)
	h.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	q.until(t, "the Deployments view", func() bool { return sh.ui.View == "deployments" })
	if got := openViews(t, f); !slices.Equal(got, []string{"deployments"}) {
		t.Errorf("ui.openView %v", got)
	}
	test.Tap(h.searchB) // the button closes it too
	if h.searching() || s.filter != "" {
		t.Error("the search button did not close the search")
	}
}

// The keyboard (Lens's documentation): ↑↓ move through the shown rows, → opens a group and then
// moves into it, ← moves to the group above and closes an open one, ↵ and ⇧↵ open the item's view
// through ui.openView (Cogpit has no tabs: both open the view), on a group they open or close it;
// the keyboard's row has a ring while the tree has the focus; Tab and ⇧Tab leave and enter the tree
// in the window's focus order.
func TestNavigatorKeyboard(t *testing.T) {
	sh, w, q, f := navScene(t, ThemeDark)
	s, tr, c := sh.side, sh.side.tree, w.Canvas()
	key := func(k fyne.KeyName) { tr.TypedKey(&fyne.KeyEvent{Name: k}) }
	at := func(want string) {
		t.Helper()
		if tr.focus != want {
			t.Fatalf("the keyboard is on %q, want %q", tr.focus, want)
		}
	}

	c.Focus(tr)
	at("pods") // the open view's row
	ring := func(id string) bool {
		return test.WidgetRenderer(tr.rows[id]).(*navRowRenderer).ring.Visible()
	}
	if !ring("pods") || ring("deployments") {
		t.Error("the ring is not on the keyboard's row only")
	}
	key(fyne.KeyDown)
	at("deployments")
	if !ring("deployments") || ring("pods") {
		t.Error("the ring did not follow the keyboard")
	}
	key(fyne.KeyDown)
	at(groupPrefix + "config")
	key(fyne.KeyRight) // opens Config
	if !tr.IsBranchOpen(groupPrefix + "config") {
		t.Fatal("→ did not open Config")
	}
	at(groupPrefix + "config")
	key(fyne.KeyRight) // moves into it
	at("verticalpodautoscalers.autoscaling.k8s.io")
	key(fyne.KeyRight) // an item has nothing to open
	at("verticalpodautoscalers.autoscaling.k8s.io")
	key(fyne.KeyLeft) // to its group
	at(groupPrefix + "config")
	key(fyne.KeyLeft) // closes it
	if tr.IsBranchOpen(groupPrefix + "config") {
		t.Fatal("← did not close Config")
	}
	key(fyne.KeyLeft) // a top-level item goes to the root
	at(rootNode)
	key(fyne.KeyLeft) // the root stays
	key(fyne.KeyUp)
	at(rootNode)
	key(fyne.KeyRight) // from the root to the first item
	at(engine.ClusterOverview)
	key(fyne.KeyEnd)
	at(groupPrefix + "custom")
	key(fyne.KeyReturn) // ↵ on a group opens it
	if !tr.IsBranchOpen(groupPrefix + "custom") {
		t.Error("↵ did not open Custom Resources")
	}
	key(fyne.KeyReturn) // and closes it
	if tr.IsBranchOpen(groupPrefix + "custom") {
		t.Error("↵ did not close Custom Resources")
	}

	// ↵ opens the view of the keyboard's row in the preview tab (ui.openView); ⇧↵ in a fixed tab
	// (ui.tab.open, tabbar.go); Space does what ↵ does.
	fakeTabCommands(t, f)
	key(fyne.KeyHome)
	key(fyne.KeyDown)
	at(engine.ClusterOverview)
	key(fyne.KeyDown)
	at("nodes")
	key(fyne.KeyReturn)
	q.until(t, "the Nodes view", func() bool { return sh.ui.View == "nodes" })
	tr.KeyDown(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
	key(fyne.KeyDown)
	key(fyne.KeyDown)
	at("workloads/overview")
	key(fyne.KeyReturn)
	tr.KeyUp(&fyne.KeyEvent{Name: desktop.KeyShiftLeft})
	q.until(t, "the Workloads Overview", func() bool { return sh.ui.View == "workloads/overview" })
	if calls := f.callsOf("ui.tab.open"); len(calls) != 1 ||
		decodeParams[tabOpenParams](t, calls[0]) != (tabOpenParams{View: "workloads/overview", Fixed: true}) {
		t.Errorf("⇧↵: ui.tab.open %v", calls)
	}
	key(fyne.KeyDown)
	key(fyne.KeySpace)
	q.until(t, "the Pods view", func() bool { return sh.ui.View == "pods" })
	if got := openViews(t, f); !slices.Equal(got, []string{"nodes", "pods"}) {
		t.Errorf("ui.openView %v", got)
	}
	if s.marked != "pods" || tr.focus != "pods" {
		t.Errorf("marked %q, keyboard on %q", s.marked, tr.focus)
	}

	// Escape closes the drawer, as it does in the table.
	ref := model.ObjectRef{Context: "kind-cogpit", Kind: "pods", Namespace: "default", Name: "x"}
	f.update(func(u *engine.UIState) { u.Drawer = &ref })
	q.until(t, "the drawer", func() bool { return sh.ui.Drawer != nil })
	key(fyne.KeyEscape)
	q.until(t, "the drawer to close", func() bool { return sh.ui.Drawer == nil })

	// Tab leaves the tree and ⇧Tab comes back; the search field's Tab leads into it.
	c.FocusNext()
	if c.Focused() == tr || c.Focused() == nil {
		t.Fatalf("Tab: focused %T", c.Focused())
	}
	if ring("pods") {
		t.Error("the ring stays without the focus")
	}
	c.FocusPrevious()
	if c.Focused() != tr {
		t.Fatalf("⇧Tab: focused %T", c.Focused())
	}
	s.head.openSearch("")
	c.FocusNext()
	if c.Focused() != tr {
		t.Errorf("Tab from the search field: focused %T", c.Focused())
	}
}

// A click opens the item's view and gives the tree the keyboard there; a click on a group opens or
// closes it; F-keys in the tree belong to the hotbar.
func TestNavigatorClick(t *testing.T) {
	sh, w, q, f := navScene(t, ThemeDark)
	s, tr := sh.side, sh.side.tree
	test.Tap(tr.rows["nodes"])
	q.until(t, "the Nodes view", func() bool { return sh.ui.View == "nodes" })
	if w.Canvas().Focused() != tr || tr.focus != "nodes" || s.marked != "nodes" {
		t.Errorf("focused %T on %q, marked %q", w.Canvas().Focused(), tr.focus, s.marked)
	}
	test.Tap(tr.rows[groupPrefix+"network"])
	if !tr.IsBranchOpen(groupPrefix + "network") {
		t.Error("a click did not open Network")
	}
	test.Tap(tr.rows[groupPrefix+"network"])
	if tr.IsBranchOpen(groupPrefix + "network") {
		t.Error("a second click did not close Network")
	}
	test.Tap(s.rootRow)
	if tr.focus != rootNode {
		t.Errorf("a click on the root: keyboard on %q", tr.focus)
	}
	if got := openViews(t, f); !slices.Equal(got, []string{"nodes"}) {
		t.Errorf("ui.openView %v", got)
	}
}

func TestRenderNavigatorBackDark(t *testing.T) {
	renderNavigatorBack(t, ThemeDark, "navigator_back_dark.png")
}
func TestRenderNavigatorBackLight(t *testing.T) {
	renderNavigatorBack(t, ThemeLight, "navigator_back_light.png")
}

// renderNavigatorBack: at a cluster the header's ‹ All Clusters with the pointer on it and its
// tooltip, the tree's root below it without a ‹ (D-57).
func renderNavigatorBack(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _ := navScene(t, mode)
	q.until(t, "diagnostics", func() bool { return sh.diag.total > 0 })
	settle(w)
	sh.tip.delay = time.Millisecond
	b := sh.side.head.back
	// The button takes the row up to the search button (WF27): its hover box ends right before it.
	drv := fyne.CurrentApp().Driver()
	if end, search := drv.AbsolutePositionForObject(b).X+b.Size().Width, drv.AbsolutePositionForObject(sh.side.head.searchB).X; search-end > 16 {
		t.Errorf("‹ All Clusters ends at %v, %v before the search button at %v", end, search-end, search)
	}
	test.MoveMouse(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(b).AddXY(b.Size().Width/2, b.Size().Height/2))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() == navBackTip })
	savePNG(t, w, name)
}

func TestRenderNavigatorSearchDark(t *testing.T) {
	renderNavigatorSearch(t, ThemeDark, "navigator_search_dark.png")
}
func TestRenderNavigatorSearchLight(t *testing.T) {
	renderNavigatorSearch(t, ThemeLight, "navigator_search_light.png")
}

// renderNavigatorSearch: the search open with "pod", the groups above the matches shown open.
func renderNavigatorSearch(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _ := navScene(t, mode)
	q.until(t, "diagnostics", func() bool { return sh.diag.total > 0 })
	sh.side.head.openSearch("pod")
	savePNG(t, w, name)
}

func TestRenderNavigatorFocusDark(t *testing.T) {
	renderNavigatorFocus(t, ThemeDark, "navigator_focus_dark.png")
}
func TestRenderNavigatorFocusLight(t *testing.T) {
	renderNavigatorFocus(t, ThemeLight, "navigator_focus_light.png")
}

// renderNavigatorFocus: the keyboard on Deployments (a ring) below the open Pods view, Config
// opened with →, and the pointer on Nodes showing "Nodes - kind-cogpit".
func renderNavigatorFocus(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _ := navScene(t, mode)
	q.until(t, "diagnostics", func() bool { return sh.diag.total > 0 })
	tr := sh.side.tree
	w.Canvas().Focus(tr)
	for _, k := range []fyne.KeyName{fyne.KeyDown, fyne.KeyDown, fyne.KeyRight, fyne.KeyUp} {
		tr.TypedKey(&fyne.KeyEvent{Name: k})
	}
	settle(w)
	sh.tip.delay = time.Millisecond
	nodes := tr.rows["nodes"]
	test.MoveMouse(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(nodes).AddXY(60, nodes.Size().Height/2))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() == "Nodes - kind-cogpit" })
	savePNG(t, w, name)
}
