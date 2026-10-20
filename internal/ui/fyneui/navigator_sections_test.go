package fyneui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The section's clusters: the connected one from ~/.kube's config, one of another file of ~/.kube
// with a display name and a failed connect, one of a file the user added whose login program is not
// allowed yet, and the demo cluster.
const (
	secKind = "kind-cogpit [.kube/config]"
	secProd = "prod-eu-central [.kube/prod.yaml]"
	secEKS  = "eks-payments [eks.yaml]"
)

// fakeNavScope gives the fake engine ui.setNavigatorScope: the real command's metadata in its
// registry and fakeEngine.setNavScope, which changes its UI state as the engine does.
func fakeNavScope(t testing.TB, f *fakeEngine) {
	t.Helper()
	c, ok := realCommands(t).Get("ui.setNavigatorScope")
	if !ok {
		t.Fatal("the registry has no ui.setNavigatorScope")
	}
	probe[navScopeParams](t, f, c)
	setHandler(f, "ui.setNavigatorScope", f.setNavScope)
}

// setHandler replaces a command's handler while the shell runs.
func setHandler(f *fakeEngine, id string, h func(context.Context, engine.Invocation) (any, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[id] = h
}

// sectionFake is kind-cogpit connected with the navigator at the section level, and three tabs: two
// of kind-cogpit, one of prod.
func sectionFake(t testing.TB) *fakeEngine {
	f := connectedFake(t, 200)
	f.contexts = []engine.ContextInfo{ // sorted by id, as the engine lists them
		{Name: "demo", Namespace: "default", Demo: true},
		{Name: secEKS, Cluster: "eks-payments", Server: "https://ABCD.gr7.eu-central-1.eks.amazonaws.com", Source: "eks.yaml",
			KubeContext: "eks-payments", File: "/Users/dev/eks.yaml", Exec: engine.ExecRequired, ExecKind: "program"},
		{Name: secKind, Cluster: "kind-cogpit", Server: "https://127.0.0.1:52341", Source: ".kube/config",
			KubeContext: "kind-cogpit", File: "/Users/dev/.kube/config", Current: true},
		{Name: secProd, Cluster: "prod-eu-central", Server: "https://api.prod-eu-central.example.com:6443", Source: ".kube/prod.yaml",
			KubeContext: "prod-eu-central", File: "/Users/dev/.kube/prod.yaml", ConnectProblem: true},
	}
	f.ui.Context = secKind
	f.ui.Connection = engine.Connection{State: engine.Connected}
	f.ui.NavigatorScope = engine.NavScopeLocal
	f.ui.ClusterSettings = map[string]engine.ClusterSettings{secProd: {Name: "Payments prod", IconColor: "magenta"}}
	f.ui.Tabs = []engine.ViewTab{
		{ID: "tab-1", Context: secKind, View: kindPods, Title: "Pods - kind-cogpit", Preview: true, Sort: engine.DefaultSort},
		{ID: "tab-2", Context: secKind, View: "services", Title: "Services - kind-cogpit", Sort: engine.DefaultSort},
		{ID: "tab-3", Context: secProd, View: "deployments", Title: "Deployments - prod-eu-central", Sort: engine.DefaultSort},
	}
	f.ui.ActiveTab = "tab-1"
	fakeNavScope(t, f)
	return f
}

func sectionScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := sectionFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the section's clusters", func() bool {
		return sh.side.sec.loaded && len(sh.side.sec.visible) == 7 && len(sh.kinds) > 0
	})
	return sh, w, q, f
}

// Keyboard ids of the section's source headings in the scene.
const (
	headKube = navSourcePrefix + ".kube/"
	headEKS  = navSourcePrefix + "eks.yaml"
)

// scopeCalls are the scopes ui.setNavigatorScope was asked for, in order.
func scopeCalls(t *testing.T, f *fakeEngine) []string {
	t.Helper()
	var out []string
	for _, c := range f.callsOf("ui.setNavigatorScope") {
		out = append(out, decodeParams[navScopeParams](t, c).Scope)
	}
	return out
}

// The section level lists every cluster under Local Kubeconfigs (Kubernetes Clusters), grouped by
// the folder or file it comes from, the demo cluster first: each with its identity's badge, its
// display name, a connection dot and a ›, the source as the tooltip with what the context picker
// said beside the context (a failed connect, a login waiting for the user; D-57); the connected one
// marked. The header reads All Clusters with its "+" and search, and the tab bar shows the shown
// cluster's tabs (GW11).
func TestNavigatorSectionLevel(t *testing.T) {
	sh, w, q, _ := sectionScene(t, ThemeDark)
	s, n := sh.side, sh.side.sec
	if !n.Visible() || s.tree.Visible() {
		t.Fatalf("section shown %v, tree shown %v", n.Visible(), s.tree.Visible())
	}
	h := s.head
	if h.title.text != "All Clusters" || !h.title.Visible() || h.back.Visible() || !h.addB.Visible() || !h.searchB.Visible() ||
		n.root.title.text != "Local Kubeconfigs" || n.root.sub.text != "Kubernetes Clusters · 4 clusters" {
		t.Errorf("header %q (shown %v, back %v, + %v), root %q / %q", h.title.text, h.title.Visible(), h.back.Visible(),
			h.addB.Visible(), n.root.title.text, n.root.sub.text)
	}
	if h.addB.tip != "Add Kubeconfigs…" || h.entry.PlaceHolder != "Search clusters…" {
		t.Errorf("+ %q, search %q", h.addB.tip, h.entry.PlaceHolder)
	}
	if got := n.visible[1:]; !slices.Equal(got, []string{"demo", headKube, secKind, secProd, headEKS, secEKS}) {
		t.Errorf("rows %q", got)
	}
	var titles []string
	for _, id := range n.visible[1:] {
		if !isSourceHead(id) {
			titles = append(titles, n.rows[id].title.text)
		}
	}
	if !slices.Equal(titles, []string{"Demo cluster", "kind-cogpit", "Payments prod", "eks-payments"}) {
		t.Errorf("titles %q", titles)
	}
	if len(n.heads) != 2 || n.heads[".kube/"] == nil || n.heads["eks.yaml"] == nil || n.heads[".kube/"].label.text != ".kube/" ||
		!n.heads[".kube/"].open || n.heads[".kube/"].count.text != "" {
		t.Errorf("group headings %v", n.heads)
	}
	kind, prod, eks := n.rows[secKind], n.rows[secProd], n.rows[secEKS]
	if !n.marked(secKind) || !kind.title.style.Bold || prod.title.style.Bold {
		t.Error("the connected cluster is not the one marked")
	}
	if kind.dot.color != connectionColor(engine.Connection{State: engine.Connected}) ||
		prod.dot.color != statusColorName(model.StatusError) || eks.dot.color != connectionColor(engine.Connection{}) {
		t.Errorf("dots %q %q %q", kind.dot.color, prod.dot.color, eks.dot.color)
	}
	if prod.badge.text != "PP" || prod.badge.ink != identityColorName("magenta") || kind.badge.text != "KC" {
		t.Errorf("badges %q (%q), %q", prod.badge.text, prod.badge.ink, kind.badge.text)
	}
	// The tooltip names the context in full (D-68): the row shows its display name.
	if got := prod.tipText(); got != "prod-eu-central - .kube/prod.yaml\nThe last connect failed: Show Connection Updates… in its menu says why" {
		t.Errorf("tooltip %q", got)
	}
	if got := kind.tipText(); got != "kind-cogpit - .kube/config" {
		t.Errorf("the connected one's tooltip %q", got)
	}
	if got := n.rows["demo"].tipText(); got != demoLabel {
		t.Errorf("the demo's tooltip %q", got)
	}
	// The tab bar at the section shows the shown cluster's tabs only (the owner's GW11, mixed
	// environments off).
	want := 0
	for _, tab := range sh.ui.Tabs {
		if tab.Context == sh.ui.Context {
			want++
		}
	}
	if len(sh.tabs.shown) != want || want == len(sh.ui.Tabs) {
		t.Errorf("the tab bar shows %d tabs at the section, want the shown cluster's %d of %d", len(sh.tabs.shown), want, len(sh.ui.Tabs))
	}
	for _, v := range sh.tabs.shown {
		if v.tab.Context != sh.ui.Context {
			t.Errorf("the tab bar at the section shows %s's tab %s", v.tab.Context, v.tab.ID)
		}
	}

	sh.tip.delay = time.Millisecond
	settle(w)
	test.MoveMouse(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(eks).AddXY(60, eks.Size().Height/2))
	q.until(t, "the tooltip", func() bool {
		return sh.tip.visibleText() == "eks-payments - eks.yaml\nLogin program not allowed yet: Cogpit shows it before it connects"
	})
}

// The row's tooltip says what the context picker's row said beside a context (D-57): the error of
// the connect that just failed, a changed token file, and a certificate the kubeconfig does not
// check with the server the credential goes to.
func TestNavigatorSectionTooltipLines(t *testing.T) {
	f := sectionFake(t)
	f.contexts = append(f.contexts, engine.ContextInfo{Name: "lab [lab.yaml]", Source: "lab.yaml", KubeContext: "lab",
		Server: "https://192.168.49.2:8443", Exec: engine.ExecChanged, ExecKind: "tokenFile", Insecure: true})
	f.ui.Context, f.ui.Connection = secProd, engine.Connection{State: engine.ConnectError, Error: "dial tcp 10.0.0.7:6443: i/o timeout"}
	sh, _, q := newTestShell(t, f, Options{})
	n := sh.side.sec
	q.until(t, "the clusters", func() bool { return n.loaded && n.rows["lab [lab.yaml]"] != nil })
	if got := n.rows[secProd].tipText(); got != "prod-eu-central - .kube/prod.yaml\nCould not connect: dial tcp 10.0.0.7:6443: i/o timeout" {
		t.Errorf("the failed one %q", got)
	}
	if n.rows[secProd].dot.color != statusColorName(model.StatusError) {
		t.Errorf("the failed one's dot %q", n.rows[secProd].dot.color)
	}
	want := "lab - lab.yaml\nToken file changed: Cogpit shows it before it connects\n" +
		"insecure-skip-tls-verify: the credential goes to whoever answers at https://192.168.49.2:8443"
	if got := n.rows["lab [lab.yaml]"].tipText(); got != want {
		t.Errorf("lab %q, want %q", got, want)
	}
	// The unchecked certificate shows on the row itself too, as the picker's pill did.
	if !n.rows["lab [lab.yaml]"].insecure.Visible() || n.rows[secProd].insecure.Visible() {
		t.Errorf("insecure icon: lab %v, prod %v", n.rows["lab [lab.yaml]"].insecure.Visible(), n.rows[secProd].insecure.Visible())
	}
}

// A cluster's › (or a click on its row) drills into it: ui.setNavigatorScope, which connects a
// cluster that is not connected; the tree shows its root and the tab bar its tabs only. The header's
// ‹ All Clusters goes back up to the section, where the tab bar still shows the shown cluster's tabs
// only (the owner's GW11); a click on the connected cluster's row drills in without a connect.
func TestNavigatorSectionDrillsInAndBack(t *testing.T) {
	sh, _, q, f := sectionScene(t, ThemeDark)
	s, n := sh.side, sh.side.sec
	test.Tap(n.rows[secProd].next)
	q.until(t, "prod's tree", func() bool { return sh.ui.Context == secProd && s.tree.Visible() && !n.Visible() })
	if h := s.head; !h.back.Visible() || h.back.text.text != "All Clusters" || h.title.Visible() || h.addB.Visible() ||
		!h.searchB.Visible() || s.name.text != "Payments prod" {
		t.Errorf("header: back %v %q, title %v, + %v; root %q", h.back.Visible(), h.back.text.text, h.title.Visible(),
			h.addB.Visible(), s.name.text)
	}
	if len(sh.tabs.shown) != 1 || sh.tabs.shown[0].tab.ID != "tab-3" {
		t.Errorf("the tab bar at prod: %d tabs", len(sh.tabs.shown))
	}

	test.Tap(s.head.back)
	q.until(t, "the section", func() bool { return n.Visible() && !s.tree.Visible() })
	if len(sh.tabs.shown) != 1 || sh.tabs.shown[0].tab.ID != "tab-3" {
		t.Errorf("the tab bar at the section with prod shown: %d tabs", len(sh.tabs.shown))
	}
	if sh.ui.Context != secProd {
		t.Errorf("going up changed the connection: %q", sh.ui.Context)
	}
	test.Tap(n.rows[secProd])
	q.until(t, "prod's tree again", func() bool { return s.tree.Visible() })
	want := []string{"cluster:" + secProd, engine.NavScopeLocal, "cluster:" + secProd}
	if got := scopeCalls(t, f); !slices.Equal(got, want) {
		t.Errorf("scopes %q, want %q", got, want)
	}
	if n := len(f.callsOf("cluster.connect")); n != 0 {
		t.Errorf("cluster.connect dispatched %d times: drilling in connects through ui.setNavigatorScope", n)
	}
}

// A cluster whose login program the user has not allowed shows it first: nothing connects, and the
// scope stays, before the user allowed it.
func TestNavigatorSectionAsksForTheLoginProgramFirst(t *testing.T) {
	sh, _, q, f := sectionScene(t, ThemeDark)
	n := sh.side.sec
	setHandler(f, "kubeconfig.pendingApprovals", func(context.Context, engine.Invocation) (any, error) {
		return nil, errors.New("no plugin in this fake")
	})
	test.Tap(n.rows[secEKS])
	q.until(t, "the approval asked", func() bool { return len(f.callsOf("kubeconfig.pendingApprovals")) == 1 })
	q.drain()
	if got := scopeCalls(t, f); len(got) != 0 {
		t.Errorf("drilled in before the approval: %q", got)
	}
	if p := decodeParams[map[string]string](t, f.callsOf("kubeconfig.pendingApprovals")[0]); p["context"] != secEKS {
		t.Errorf("the approval asked for %q", p["context"])
	}
	setHandler(f, "kubeconfig.pendingApprovals", func(context.Context, engine.Invocation) (any, error) {
		return map[string]any{"groups": []any{}}, nil // allowed meanwhile
	})
	test.Tap(n.rows[secEKS].next)
	q.until(t, "eks's tree", func() bool { return sh.ui.Context == secEKS && sh.side.tree.Visible() })
	if got := scopeCalls(t, f); !slices.Equal(got, []string{"cluster:" + secEKS}) {
		t.Errorf("scopes %q", got)
	}
}

// The section's keyboard: the list starts on the connected cluster; ↑↓, Home and End move, ← goes
// to the cluster's heading and on to the root, → from the root to the first row; on a heading ←
// collapses its group (kept by the engine, A63) and → opens it, then moves into it; ↵ (and →) on a
// cluster drill in, and the keyboard follows into the tree's root; ← on the tree's root goes up,
// back to the cluster's row; a letter starts the search.
func TestNavigatorSectionKeyboard(t *testing.T) {
	sh, w, q, f := sectionScene(t, ThemeDark)
	s, n, c := sh.side, sh.side.sec, w.Canvas()
	c.Focus(n)
	if !n.focused || n.focus != secKind || !n.keyboardOn(secKind) {
		t.Fatalf("focused %v on %q", n.focused, n.focus)
	}
	keys := func(names ...fyne.KeyName) {
		for _, k := range names {
			n.TypedKey(&fyne.KeyEvent{Name: k})
		}
	}
	at := func(what, want string) {
		t.Helper()
		if n.focus != want {
			t.Fatalf("%s: on %q, want %q", what, n.focus, want)
		}
	}
	keys(fyne.KeyDown)
	at("↓", secProd)
	keys(fyne.KeyLeft)
	at("← on a cluster", headKube)
	keys(fyne.KeyLeft) // collapses .kube/
	at("← on an open heading", headKube)
	if got := n.visible[1:]; !slices.Equal(got, []string{"demo", headKube, headEKS, secEKS}) || n.heads[".kube/"].count.text != "2" {
		t.Fatalf("collapsed: rows %q, count %q", got, n.heads[".kube/"].count.text)
	}
	q.until(t, "the engine's collapsed groups", func() bool { return slices.Equal(f.UIState().Navigator.Collapsed, []string{".kube/"}) })
	keys(fyne.KeyLeft)
	at("← on a collapsed heading", navSectionRoot)
	keys(fyne.KeyRight)
	at("→ on the root", "demo")
	keys(fyne.KeyDown, fyne.KeyRight) // opens .kube/
	at("→ on a collapsed heading", headKube)
	if !n.groupOpen(".kube/") {
		t.Fatal("→ did not open .kube/")
	}
	keys(fyne.KeyRight)
	at("→ on an open heading", secKind)
	keys(fyne.KeyEnd)
	at("End", secEKS)
	keys(fyne.KeyHome, fyne.KeyDown, fyne.KeyDown, fyne.KeyDown)
	at("Home ↓↓↓", secKind)
	q.until(t, "the engine's groups open", func() bool { return len(f.UIState().Navigator.Collapsed) == 0 })
	if got := scopeCalls(t, f); len(got) != 0 {
		t.Fatalf("moving drilled in: %q", got)
	}
	keys(fyne.KeyReturn)
	q.until(t, "the tree", func() bool { return s.tree.Visible() && c.Focused() == s.tree })
	if s.tree.focus != rootNode || !s.tree.keyboardOn(rootNode) {
		t.Errorf("the tree's keyboard on %q", s.tree.focus)
	}
	s.tree.TypedKey(&fyne.KeyEvent{Name: fyne.KeyLeft})
	q.until(t, "the section", func() bool { return n.Visible() && c.Focused() == n })
	if n.focus != secKind {
		t.Errorf("back up: keyboard on %q, want the cluster it came from", n.focus)
	}
	if got := scopeCalls(t, f); !slices.Equal(got, []string{"cluster:" + secKind, engine.NavScopeLocal}) {
		t.Errorf("scopes %q", got)
	}
	n.TypedRune('p')
	if !s.head.searching() || s.head.entry.Text != "p" {
		t.Errorf("a letter: search %v %q", s.head.searching(), s.head.entry.Text)
	}
}

// The header's "+" at the section adds kubeconfigs (the Sync Files and Folders dialog), as the
// root's right-click does; at a cluster it is gone (D-57, I8).
func TestNavigatorHeaderAddsKubeconfigs(t *testing.T) {
	sh, _, q, _ := sectionScene(t, ThemeDark)
	h := sh.side.head
	asked := 0
	sh.kc.pick = func(func(string, bool)) { asked++ }
	test.Tap(h.addB)
	if asked != 1 {
		t.Fatalf("the + asked %d times for a kubeconfig", asked)
	}
	test.Tap(sh.side.sec.rows[secKind])
	q.until(t, "kind-cogpit's tree", func() bool { return sh.side.tree.Visible() })
	if h.addB.Visible() || !h.back.Visible() {
		t.Errorf("at a cluster: + %v, back %v", h.addB.Visible(), h.back.Visible())
	}
}

// The header's search filters the section's clusters by name, display name and source; ↵ drills
// into the first match; nothing matching says so.
func TestNavigatorSectionSearch(t *testing.T) {
	sh, _, q, f := sectionScene(t, ThemeDark)
	h, n := sh.side.head, sh.side.sec
	for text, want := range map[string][]string{
		"Payments P": {headKube, secProd}, // the display name, ignoring case
		"eks.yaml":   {headEKS, secEKS},   // the source
		"KIND":       {headKube, secKind}, // the context's name
		"o":          {"demo", headKube, secKind, secProd},
	} {
		h.openSearch(text)
		if got := n.visible[1:]; !slices.Equal(got, want) {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
	h.openSearch("zzz")
	if len(n.visible) != 1 || n.hint.text != "No clusters match “zzz”." {
		t.Errorf("no match: %d rows, hint %q", len(n.visible), n.hint.text)
	}
	h.openSearch("prod")
	h.entry.OnSubmitted(h.entry.Text)
	q.until(t, "prod's tree", func() bool { return sh.side.tree.Visible() })
	if got := scopeCalls(t, f); !slices.Equal(got, []string{"cluster:" + secProd}) {
		t.Errorf("scopes %q", got)
	}
	if h.searching() {
		t.Error("the search stays open in the cluster's tree")
	}
}

// A cluster's row has the cluster's menu: Connect while it is not the active one, Settings… and the
// hotbar's item where they exist, and after a failed connect Show Connection Updates…, its log; the
// connected one's has Disconnect and no Connect.
func TestNavigatorSectionRowMenu(t *testing.T) {
	f := sectionFake(t)
	withHotbar(f)
	f.handlers["cluster.connectLog"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		return map[string]any{"context": secProd, "lines": []map[string]any{
			{"time": fixedNow, "level": "ERROR", "text": "cannot connect to https://api.prod-eu-central.example.com:6443: i/o timeout"},
		}}, nil
	}
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the clusters", func() bool { return sh.side.sec.loaded && len(sh.side.sec.visible) == 7 })
	n := sh.side.sec
	labels := func() []string {
		var out []string
		for _, it := range n.menuModel.Items {
			out = append(out, it.Label)
		}
		return out
	}
	n.rows[secKind].TappedSecondary(&fyne.PointEvent{})
	if got := labels(); slices.Contains(got, connectLogItem) {
		t.Errorf("a cluster without a failed connect offers its log: %q", got)
	}
	n.rows[secProd].TappedSecondary(&fyne.PointEvent{})
	if got := labels(); len(got) < 2 || got[0] != "Connect" || !slices.Contains(got, "Add to Hotbar") || got[len(got)-1] != connectLogItem {
		t.Fatalf("prod's menu %q", got)
	}
	n.menuModel.Items[len(n.menuModel.Items)-1].Action()
	q.until(t, "the connect's log", func() bool { return sh.kc.connectLog != nil })
	if p := decodeParams[map[string]string](t, f.callsOf("cluster.connectLog")[0]); p["context"] != secProd {
		t.Errorf("cluster.connectLog %v", p)
	}
	test.Tap(sh.kc.connectLog.close)
	n.rows[secProd].TappedSecondary(&fyne.PointEvent{})
	n.menuModel.Items[0].Action()
	q.until(t, "prod's tree", func() bool { return sh.side.tree.Visible() && sh.ui.Context == secProd })
	sh.side.goUp()
	q.until(t, "the section", func() bool { return n.Visible() })
	n.rows[secProd].TappedSecondary(&fyne.PointEvent{})
	if got := labels(); slices.Contains(got, "Connect") || !slices.Contains(got, "Add to Hotbar") {
		t.Errorf("the connected cluster's menu %q", got)
	}
	n.root.TappedSecondary(&fyne.PointEvent{})
	if got := labels(); !slices.Equal(got, []string{"Add Kubeconfigs…", sourcesLabel}) {
		t.Errorf("the root's menu %q", got)
	}
}

// The window's parameters match the engine's schema.
func TestNavScopeParamsMatchTheRegistry(t *testing.T) {
	f := newFakeEngine(t)
	fakeNavScope(t, f)
	p, _ := f.reg.Get("ui.setNavigatorScope")
	r, _ := realCommands(t).Get("ui.setNavigatorScope")
	if !slices.Equal(p.Schema.Required, r.Schema.Required) || len(p.Schema.Properties) != len(r.Schema.Properties) ||
		!slices.Equal(r.Schema.Required, []string{"scope"}) {
		t.Errorf("the window requires %v of %d, the command %v of %d", p.Schema.Required, len(p.Schema.Properties),
			r.Schema.Required, len(r.Schema.Properties))
	}
}

// Over the real engine and a small demo cluster, not connected at start: the navigator shows the
// section with the demo cluster; its › connects through the engine and drills in, the tree and the
// preview tab showing; the header's ‹ All Clusters goes back up and keeps the connection and the
// tab.
func TestNavigatorSectionOverTheEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	empty := filepath.Join(t.TempDir(), "config") // a kubeconfig without contexts: never the developer's
	if err := os.WriteFile(empty, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := &engine.ClusterConnector{IncludeDemo: true, KubeconfigPaths: []string{empty}}
	conn.DemoOptions.Pods, conn.DemoOptions.NoChurn, conn.DemoOptions.Seed = 300, true, 3
	s := engine.New(engine.Options{Connector: conn})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(ThemeDark))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{}, q.do)
	s.SetConfirmer(sh.confirmer)
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	defer func() {
		sh.stop()
		q.drain()
	}()
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s: scope %q, %s %q", what, sh.ui.NavigatorScope, sh.ui.Connection.State, sh.ui.Context)
			}
			q.drain()
			time.Sleep(5 * time.Millisecond)
		}
		q.drain()
	}
	n := sh.side.sec
	until("the section", func() bool { return n.Visible() && n.rows[engine.DemoContext] != nil })
	if sh.ui.NavigatorScope != engine.NavScopeLocal || len(sh.tabs.shown) != 0 {
		t.Errorf("at start: scope %q, %d tabs", sh.ui.NavigatorScope, len(sh.tabs.shown))
	}
	test.Tap(n.rows[engine.DemoContext].next)
	until("the demo's tree", func() bool {
		return sh.ui.Connection.State == engine.Connected && sh.side.tree.Visible() && sh.side.tree.rows["deployments"] != nil &&
			len(sh.tabs.shown) == 1 && sh.tabs.shown[0].tab.Title == "Pods - demo"
	})
	if got := s.UIState().NavigatorScope; got != engine.NavScopeCluster(engine.DemoContext) {
		t.Errorf("scope %q", got)
	}
	test.Tap(sh.side.head.back)
	until("the section again", func() bool { return n.Visible() && sh.ui.NavigatorScope == engine.NavScopeLocal })
	if sh.ui.Connection.State != engine.Connected || len(sh.tabs.shown) != 1 || !n.marked(engine.DemoContext) {
		t.Errorf("up: %s, %d tabs, demo marked %v", sh.ui.Connection.State, len(sh.tabs.shown), n.marked(engine.DemoContext))
	}
	if !strings.HasPrefix(n.root.sub.text, "Kubernetes Clusters · 1 cluster") {
		t.Errorf("root %q", n.root.sub.text)
	}
}

func TestRenderNavigatorSectionDark(t *testing.T) {
	renderNavigatorSection(t, ThemeDark, "navigator_section_dark.png")
}
func TestRenderNavigatorSectionLight(t *testing.T) {
	renderNavigatorSection(t, ThemeLight, "navigator_section_light.png")
}

// renderNavigatorSection: the section level with its groups, the keyboard on the connected cluster,
// the pointer on the cluster whose login program waits, showing its source; every tab in the bar.
func renderNavigatorSection(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _ := sectionScene(t, mode)
	q.until(t, "diagnostics", func() bool { return sh.diag.total > 0 })
	w.Canvas().Focus(sh.side.sec)
	settle(w)
	sh.tip.delay = time.Millisecond
	eks := sh.side.sec.rows[secEKS]
	test.MoveMouse(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(eks).AddXY(60, eks.Size().Height/2))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	savePNG(t, w, name)
}

// renderNavigatorReconnecting: the section while the connected cluster reconnects, its row's dot
// in the blink's dimmed phase and the pointer on it, its tooltip shown (F13).
func renderNavigatorReconnecting(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, f := sectionScene(t, mode)
	q.until(t, "diagnostics", func() bool { return sh.diag.total > 0 })
	clk := fakeBlinkClock(sh)
	now := interruptedAt.Add(time.Minute)
	sh.now = func() time.Time { return now }
	interrupt(f, engine.Interrupted, now)
	q.until(t, "the blink", func() bool { return sh.blink.blinking(secKind) })
	clk.after(blinkPhase)
	settle(w)
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(sh.side.sec.rows[secKind].dot))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	savePNG(t, w, name)
}

func TestRenderNavigatorReconnectingDark(t *testing.T) {
	renderNavigatorReconnecting(t, ThemeDark, "navigator_reconnecting_dark.png")
}
func TestRenderNavigatorReconnectingLight(t *testing.T) {
	renderNavigatorReconnecting(t, ThemeLight, "navigator_reconnecting_light.png")
}

func TestRenderNavigatorFindDark(t *testing.T) {
	renderNavigatorFind(t, ThemeDark, "navigator_find_dark.png")
}
func TestRenderNavigatorFindLight(t *testing.T) {
	renderNavigatorFind(t, ThemeLight, "navigator_find_light.png")
}

// renderNavigatorFind: drilled into kind-cogpit, then ⌘P (Connect to Cluster…, D-57): the section
// with its search open and focused, "pro" typed, the header's All Clusters with its "+".
func renderNavigatorFind(t *testing.T, mode ThemeMode, name string) {
	f := sectionFake(t)
	if c, ok := realCommands(t).Get(connectCommand); ok { // ⌘P on the Cluster menu's item
		probe[probeContext](t, f, c)
	}
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the section and diagnostics", func() bool {
		return sh.side.sec.loaded && len(sh.side.sec.visible) == 7 && len(sh.kinds) > 0 && sh.diag.total > 0
	})
	test.Tap(sh.side.sec.rows[secKind])
	q.until(t, "the tree", func() bool { return sh.side.tree.Visible() && sh.side.tree.rows["deployments"] != nil })
	sh.connectMenuItem().Action()
	h := sh.side.head
	q.until(t, "the section's search", func() bool { return sh.side.sec.Visible() && w.Canvas().Focused() == h.entry })
	test.Type(h.entry, "pro")
	q.until(t, "the search reported", func() bool { return sh.ui.Navigator.Search == "pro" })
	settle(w)
	savePNG(t, w, name)
}

// The section root's "−" opens Kubeconfig Sources… (the owner's WF44 of 2026-10-19), as its menu
// does.
func TestSectionRootMinusOpensTheSources(t *testing.T) {
	sh, _, q, _ := sectionScene(t, ThemeDark)
	r := sh.side.sec.root
	if !r.sources.Visible() || r.sources.tip != sourcesTip {
		t.Fatalf("the root row's minus: visible %v, tip %q", r.sources.Visible(), r.sources.tip)
	}
	test.Tap(r.sources)
	q.until(t, "the sources dialog", func() bool { return sh.kc.sources != nil })
}

// The foot of All Clusters (the owner's GW11, 2026-10-20): "Mixed environments", off by default,
// with a "?" whose tooltip says what it does; a click sets the preference through preferences.set
// (agents set the same), and with it on the tab bar shows every cluster's tabs.
func TestMixedEnvironmentsCheck(t *testing.T) {
	sh, w, q, f := sectionScene(t, ThemeDark)
	n := sh.side.sec
	if !n.mixed.Visible() || n.mixed.Checked || n.mixed.Text != "Mixed environments" {
		t.Fatalf("the check: visible %v, checked %v, %q", n.mixed.Visible(), n.mixed.Checked, n.mixed.Text)
	}
	if hints := findAll[*fieldName](n.foot); len(hints) != 1 || !strings.Contains(hints[0].hint, "every connected cluster") {
		t.Errorf("the foot's hint: %+v", hints)
	}
	shownBefore := len(sh.tabs.shown)
	test.Tap(n.mixed)
	q.until(t, "preferences.set", func() bool { return len(f.callsOf("preferences.set")) == 1 })
	if p := decodeParams[map[string]bool](t, f.callsOf("preferences.set")[0]); !p["mixedEnvironments"] {
		t.Errorf("preferences.set %v", p)
	}
	f.update(func(u *engine.UIState) {
		p := engine.DefaultPreferences()
		p.MixedEnvironments = true
		u.Preferences = &p
	})
	// prod's connect failed: its tab stays out of the bar (GW13) until prod is connected.
	q.until(t, "the check on", func() bool { return n.mixed.Checked })
	q.drain()
	if len(sh.tabs.shown) != shownBefore {
		t.Errorf("with mixed environments on, the failed prod's tab shows: %d tabs", len(sh.tabs.shown))
	}
	f.update(func(u *engine.UIState) {
		u.Connections = append(u.Connections, engine.ConnectedCluster{Context: secProd, Connection: engine.Connection{State: engine.Connected}})
	})
	q.until(t, "every connected cluster's tabs", func() bool { return len(sh.tabs.shown) == len(sh.ui.Tabs) })
	if shownBefore >= len(sh.ui.Tabs) {
		t.Errorf("the scene shows every tab with the check off too (%d of %d)", shownBefore, len(sh.ui.Tabs))
	}
	if n := len(f.callsOf("preferences.set")); n != 1 {
		t.Errorf("the UI state's check sent preferences.set again: %d calls", n)
	}
	settle(w)
}

// The foot's "?" shows its explanation on a hover and a click (GW11). Its tooltip is the shell's,
// asked for at the event: the navigator is built before the shell's tooltip, and v0.31.0 kept the
// nil it saw then, so a hover over the "?" ended Cogpit (the owner's report of 2026-10-20).
func TestMixedEnvironmentsHintHover(t *testing.T) {
	sh, w, q, _ := sectionScene(t, ThemeDark)
	hints := findAll[*fieldName](sh.side.sec.foot)
	if len(hints) != 1 {
		t.Fatalf("%d hints at the foot", len(hints))
	}
	hint := hints[0]
	if hint.tip() != sh.tip || sh.tip == nil {
		t.Fatalf("the hint's tooltip %p, the shell's %p", hint.tip(), sh.tip)
	}
	sh.tip.delay = time.Millisecond
	settle(w)
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(hint).AddXY(hint.Size().Width/2, hint.Size().Height/2)
	test.MoveMouse(w.Canvas(), at)
	q.until(t, "the hint's tooltip", func() bool { return strings.Contains(sh.tip.visibleText(), "every connected cluster") })
	test.MoveMouse(w.Canvas(), fyne.NewPos(1, 1))
	q.until(t, "the tooltip gone", func() bool { return sh.tip.visibleText() == "" })
	test.Tap(hint)
	q.until(t, "the tooltip on a click", func() bool { return sh.tip.visibleText() != "" })
}
