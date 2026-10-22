package fyneui

import (
	"bytes"
	"context"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// welcomeEngine is a real engine over a kubeconfig file it never connects with (three contexts,
// kind-dev the current one) and the demo cluster, with a hotbar in a temporary folder.
func welcomeEngine(t *testing.T) *engine.Session {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "Cogpit")
	kc := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(kc, []byte(settingsKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := &engine.ClusterConnector{KubeconfigPaths: []string{kc}, IncludeDemo: true}
	conn.DemoOptions.Pods, conn.DemoOptions.NoChurn, conn.DemoOptions.Seed = 200, true, 3
	s := engine.New(engine.Options{Connector: conn, Hotbar: engine.NewHotbar(cfg),
		Preferences: engine.NewPreferencesStore(cfg), ClusterSettings: engine.NewClusterSettingsStore(cfg)})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// welcomeShown waits for the welcome page with its clusters listed.
func welcomeShown(t *testing.T, sh *shell, q *mainQueue) *welcomePage {
	t.Helper()
	w := sh.tabs.welcome
	q.until(t, "the welcome page", func() bool { return w.shown && w.root.Visible() && w.loaded && len(w.rows) > 0 })
	return w
}

// menuNamed returns the main menu's menu with this label, or nil.
func menuNamed(m *fyne.MainMenu, label string) *fyne.Menu {
	for _, menu := range m.Items {
		if menu.Label == label {
			return menu
		}
	}
	return nil
}

// Without a cluster the workspace shows the welcome page: the logo, the clusters the kubeconfigs
// offer (the current one first), Add Kubeconfigs and the documentation. A click on a cluster
// connects it through the engine and its tab replaces the page; Help › Welcome brings the page
// back over the connected cluster, its row back to the cluster's tab; Disconnect in the Cluster
// menu leaves the page with the cluster listed, and closes its tab (A61).
func TestWelcomePageOverTheEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	s := welcomeEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	w := welcomeShown(t, sh, q)
	if got := w.shownClusters(); len(got) != 4 || got[0] != "kind-dev" || !slices.Contains(got, demoLabel) {
		t.Fatalf("clusters on a first start %q, want kind-dev first and the demo", got)
	}
	if w.status.Visible() || !strings.Contains(w.hint.text, "finds a cluster by name") {
		t.Errorf("status %q (visible %v), hint %q", w.status.text, w.status.Visible(), w.hint.text)
	}
	if got := welcomeButtons(w); !slices.Equal(got, []string{"Add Kubeconfigs…"}) { // N122
		t.Errorf("the welcome page's buttons %q, want Add Kubeconfigs… alone", got)
	}

	// Pinned to the hotbar, the demo is listed under the hotbar, alone: no "Other clusters" (AW13,
	// D-114), the navigator's section lists them.
	mustDispatch(t, s, "hotbar.addCluster", map[string]string{"context": engine.DemoContext})
	q.until(t, "the hotbar's group", func() bool {
		return slices.Equal(w.shownClusters(), []string{demoLabel})
	})

	// One click connects through the engine; the cluster's tab takes the workspace.
	test.Tap(w.rows[0])
	q.until(t, "the demo connected", func() bool {
		return sh.ui.Connection.State == engine.Connected && sh.ui.Context == engine.DemoContext && len(sh.ui.Tabs) == 1
	})
	q.until(t, "the page gone", func() bool { return !w.shown && !w.root.Visible() })
	if u := s.UIState(); u.Welcome || u.ActiveTab == "" {
		t.Fatalf("connected: %+v", u)
	}
	// The connected cluster's menus offer Disconnect: the navigator's root and its hotbar tile; the
	// demo cluster has no Remove from Cogpit (it comes from no kubeconfig file).
	sh.side.rootRow.TappedSecondary(&fyne.PointEvent{})
	q.until(t, "the hotbar's tile", func() bool { return len(tilesOf(sh.hot)) == 1 })
	tilesOf(sh.hot)[0].TappedSecondary(&fyne.PointEvent{AbsolutePosition: fyne.NewPos(30, 100)})
	for name, m := range map[string]*fyne.Menu{"navigator root": sh.side.rootRow.menuModel, "hotbar tile": sh.hot.menuModel} {
		labels := menuLabels(m)
		if !slices.Contains(labels, "Disconnect") || slices.ContainsFunc(labels, func(l string) bool { return strings.HasPrefix(l, "Remove from Cogpit") }) {
			t.Errorf("%s menu %q", name, labels)
		}
	}

	// Help › Welcome over the connected cluster; its row goes back to its tab.
	help := menuNamed(sh.mainMenu(), "Help")
	if help == nil || help.Items[0].Label != "Welcome" {
		t.Fatalf("Help menu %v", menuLabels(help))
	}
	help.Items[0].Action()
	q.until(t, "the page over the cluster", func() bool { return w.shown && s.UIState().ActiveTab == "" })
	if !strings.HasPrefix(w.status.text, "Connected to demo") || sh.ui.Connection.State != engine.Connected {
		t.Errorf("status %q", w.status.text)
	}
	// ⌘W has no tab to close while the page shows: nothing happens, no error.
	toasts := len(sh.toasts.Objects)
	menuItemWithin(sh.mainMenu(), &desktop.CustomShortcut{KeyName: fyne.KeyW, Modifier: fyne.KeyModifierShortcutDefault}).Action()
	q.drain()
	if len(sh.toasts.Objects) != toasts || len(sh.ui.Tabs) != 1 {
		t.Errorf("⌘W on the welcome page: %d toasts (was %d), tabs %v", len(sh.toasts.Objects), toasts, sh.ui.Tabs)
	}
	q.until(t, "the connected row", func() bool { return len(w.rows) == 1 && w.rows[0].title.text == demoLabel })
	test.Tap(w.rows[0])
	q.until(t, "the tab back", func() bool { return !w.shown && sh.ui.ActiveTab != "" && sh.ui.View != "" })

	// Disconnect from the Cluster menu: the page, the demo under the hotbar.
	var disconnect *fyne.MenuItem
	for _, it := range sh.mainMenu().Items[0].Items {
		if it.Label == "Disconnect" {
			disconnect = it
		}
	}
	if disconnect == nil || disconnect.Disabled {
		t.Fatalf("Cluster menu %v", menuLabels(sh.mainMenu().Items[0]))
	}
	disconnect.Action()
	q.until(t, "disconnected", func() bool { return sh.ui.Connection.State == engine.Disconnected && w.shown })
	if s.Backend() != nil || len(sh.ui.Tabs) != 0 {
		t.Errorf("after the disconnect: backend %v, tabs %v (Disconnect closes the cluster's tabs, A61)", s.Backend(), sh.ui.Tabs)
	}
	q.until(t, "the menu follows", func() bool {
		for _, it := range sh.mainMenu().Items[0].Items {
			if it.Label == "Disconnect" {
				return it.Disabled
			}
		}
		return false
	})
}

// The welcome page's PNGs: a first start, no cluster connected.
func TestRenderWelcomeDark(t *testing.T)  { renderWelcome(t, ThemeDark) }
func TestRenderWelcomeLight(t *testing.T) { renderWelcome(t, ThemeLight) }

func renderWelcome(t *testing.T, mode ThemeMode) {
	s := welcomeEngine(t)
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	welcomeShown(t, sh, q)
	q.drain()
	savePNG(t, w, "welcome_"+string(mode)+".png")
}

// sourcesWelcomeEngine is a real engine whose kubeconfig sources are a default folder (a fake
// ~/.kube with config) and an added file with two contexts, all in temporary folders.
func sourcesWelcomeEngine(t *testing.T) (s *engine.Session, added string) {
	t.Helper()
	root := t.TempDir()
	kube := filepath.Join(root, "home", ".kube")
	added = filepath.Join(root, "work", "team.yaml")
	for path, content := range map[string]string{
		filepath.Join(kube, "config"): settingsKubeconfig,
		added:                         strings.ReplaceAll(strings.ReplaceAll(settingsKubeconfig, "prod-eu-central", "team-a"), "staging", "team-b"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	k := engine.NewKubeconfigSources(filepath.Join(t.TempDir(), "Cogpit"), engine.WithDefaultFolder(kube))
	if _, _, err := k.AddFile(added); err != nil {
		t.Fatal(err)
	}
	s = engine.New(engine.Options{Connector: &engine.ClusterConnector{Sources: k}})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, added
}

// Remove from Cogpit in a context's menu: for an added file it asks first, naming the file's path
// and every context that leaves with it, and only then unlinks the file, which stays on disk
// unchanged; a context of the default ~/.kube offers it too, with its folder (A97:
// TestRemoveFromCogpitUnlinksTheFolder).
func TestRemoveFromCogpitAsksFirst(t *testing.T) {
	s, added := sourcesWelcomeEngine(t)
	before, err := os.ReadFile(added)
	if err != nil {
		t.Fatal(err)
	}
	sh, win, q := newEngineShell(t, s, Options{})
	links := sh.tabs.welcome.links
	q.until(t, "the links", func() bool { return len(links.byCtx) == 6 })
	removeOf := func(id string) *fyne.MenuItem {
		for _, it := range sh.clusterItems(id) {
			if strings.HasPrefix(it.Label, "Remove from Cogpit") {
				return it
			}
		}
		return nil
	}
	if it := removeOf("kind-dev [.kube/config]"); it == nil || it.Disabled || it.Label != "Remove from Cogpit…" {
		t.Fatalf("Remove of a ~/.kube context: %+v", it)
	}
	if it := removeOf(engine.DemoContext); it != nil {
		t.Errorf("Remove of the demo cluster: %+v", it)
	}
	if items := sh.clusterItems("team-a [team.yaml]"); len(items) != 2 || items[1].Label != "Remove from Cogpit…" || items[1].Disabled {
		t.Fatalf("an added file's context: %v", menuLabels(fyne.NewMenu("", items...)))
	}

	// The navigator's section row menu has it too.
	n := sh.side.sec
	q.until(t, "the section", func() bool { return n.loaded && len(n.contexts) == 6 })
	n.rowMenu("team-a [team.yaml]", fyne.NewPos(300, 300))
	if labels := menuLabels(n.menuModel); !slices.Contains(labels, "Remove from Cogpit…") {
		t.Fatalf("the section's row menu %v", labels)
	}
	item := removeOf("team-a [team.yaml]")
	item.Action()
	q.until(t, "the confirmation", func() bool { return links.removing != nil })
	texts := strings.Join(dialogTexts(win), " ")
	for _, want := range []string{"Remove team-a from Cogpit?", added, "Only Cogpit's link to the file is removed", "team-a, team-b"} {
		if !strings.Contains(texts, want) {
			t.Errorf("the confirmation lacks %q: %s", want, texts)
		}
	}
	ctxs, _ := s.Contexts()
	if len(ctxs) != 6 {
		t.Fatalf("removed before the confirmation: %d contexts", len(ctxs))
	}
	confirm := findButton(win, "Remove")
	if confirm == nil {
		t.Fatal("no Remove button")
	}
	test.Tap(confirm)
	q.until(t, "the file unlinked", func() bool { c, _ := s.Contexts(); return len(c) == 3 })
	ctxs, _ = s.Contexts()
	for _, c := range ctxs {
		if strings.HasSuffix(c.Name, "[team.yaml]") {
			t.Errorf("%s still offered", c.Name)
		}
	}
	if after, err := os.ReadFile(added); err != nil || !bytes.Equal(after, before) {
		t.Errorf("the kubeconfig file changed: %v", err)
	}
}

// A97: Remove from Cogpit on a context of a synced folder (here the default ~/.kube) asks to
// remove the whole folder, naming its path and every context of its files, and on "Remove Folder"
// unlinks it: those contexts leave Cogpit, the added file's stay, and the folder's files are not
// touched.
func TestRemoveFromCogpitUnlinksTheFolder(t *testing.T) {
	s, _ := sourcesWelcomeEngine(t)
	link, err := s.ContextLink("kind-dev [.kube/config]")
	if err != nil || link.Folder == "" {
		t.Fatalf("the ~/.kube context's link: %+v, %v", link, err)
	}
	config := filepath.Join(link.Folder, "config")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	sh, win, q := newEngineShell(t, s, Options{})
	links := sh.tabs.welcome.links
	q.until(t, "the links", func() bool { return len(links.byCtx) == 6 })
	sh.confirmRemoveCluster("kind-dev [.kube/config]")
	q.until(t, "the confirmation", func() bool { return links.removing != nil })
	texts := strings.Join(dialogTexts(win), " ")
	for _, want := range []string{"Remove kind-dev and its folder from Cogpit?", "comes from a synced folder", link.Folder,
		"Only Cogpit's link to the folder is removed", "define 3 contexts", "kind-dev, prod-eu-central, staging"} {
		if !strings.Contains(texts, want) {
			t.Errorf("the confirmation lacks %q: %s", want, texts)
		}
	}
	confirm := findButton(win, "Remove Folder")
	if confirm == nil {
		t.Fatal("no Remove Folder button")
	}
	test.Tap(confirm)
	q.until(t, "the folder unlinked", func() bool { c, _ := s.Contexts(); return len(c) == 3 })
	ctxs, _ := s.Contexts()
	for _, c := range ctxs {
		if strings.HasSuffix(c.Name, "[.kube/config]") {
			t.Errorf("%s still offered", c.Name)
		}
	}
	if after, err := os.ReadFile(config); err != nil || !bytes.Equal(after, before) {
		t.Errorf("the folder's kubeconfig changed: %v", err)
	}
}

// The confirmation of a folder's removal, the PNGs of the pull request.
func TestRenderRemoveFolderConfirmationDark(t *testing.T) {
	renderRemoveFolderConfirmation(t, ThemeDark, "remove_folder_confirm_dark.png")
}

func TestRenderRemoveFolderConfirmationLight(t *testing.T) {
	renderRemoveFolderConfirmation(t, ThemeLight, "remove_folder_confirm_light.png")
}

// renderRemoveFolderConfirmation draws it over the fake engine, whose paths are fixed (a session's
// would name the test's temporary folder).
func renderRemoveFolderConfirmation(t *testing.T, mode ThemeMode, name string) {
	f, _ := kubeconfigFake(t, engine.ExecRequired)
	ids := []string{"dev [.kube/.dev.kubeconfig]", "team [.kube/team.yaml]"}
	f.handlers["cluster.links"] = func(context.Context, engine.Invocation) (any, error) {
		return map[string]any{"links": []map[string]any{{"context": ids[1], "file": "/Users/me/.kube/team.yaml", "contexts": ids,
			"removable": true, "folder": "/Users/me/.kube",
			"source": map[string]any{"id": ".kube/", "kind": "folder", "default": true, "path": "/Users/me/.kube"}}}}, nil
	}
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	links := sh.tabs.welcome.links
	sh.confirmRemoveCluster(ids[1])
	q.until(t, "the confirmation", func() bool { return links.removing != nil })
	q.drain()
	savePNG(t, w, name)
}

// The Help menu: Welcome, Documentation (Cogpit's README in the browser), and on Windows and Linux
// About Cogpit at its end (macOS's application menu has it there: TestAppMenuAbout); Copy MCP
// command only with an MCP server. Cogpit's Window menu exists on Windows and Linux only: macOS
// keeps GLFW's.
func TestHelpAndWindowMenus(t *testing.T) {
	browser := recordBrowser(t)
	f := newFakeEngine(t)
	reg := realCommands(t)
	for _, id := range []string{"ui.welcome.open", "cluster.disconnect"} {
		c, ok := reg.Get(id)
		if !ok {
			t.Fatalf("the registry has no %s", id)
		}
		probe[struct{}](t, f, c)
	}
	sh, win, q := newTestShell(t, f, Options{})
	if got := menuLabels(sh.helpMenuOn(goosMac)); !slices.Equal(got, []string{"Welcome", "Documentation"}) {
		t.Errorf("Help menu on macOS %q", got)
	}
	for _, goos := range []string{"linux", "windows"} {
		if got := menuLabels(sh.helpMenuOn(goos)); !slices.Equal(got, []string{"Welcome", "Documentation", "", "About Cogpit"}) {
			t.Errorf("Help menu on %s %q", goos, got)
		}
	}
	if got, want := menuLabels(menuNamed(sh.mainMenu(), "Help")), menuLabels(sh.helpMenuOn(runtime.GOOS)); !slices.Equal(got, want) {
		t.Errorf("the main menu's Help %q, want this system's %q", got, want)
	}
	sh.helpMenuOn("linux").Items[3].Action() // About Cogpit: Cogpit's dialog
	if texts := strings.Join(dialogTexts(win), "|"); !strings.Contains(texts, "Cogpit|") || !strings.Contains(texts, aboutLicence) {
		t.Errorf("About: %s", texts)
	}
	help := menuNamed(sh.mainMenu(), "Help")
	help.Items[0].Action()
	q.until(t, "ui.welcome.open", func() bool { return len(f.callsOf("ui.welcome.open")) == 1 })
	help.Items[1].Action()
	if got := browser.opened(); !slices.Equal(got, []string{readmeURL}) {
		t.Errorf("Documentation opened %q", got)
	}
	sh.mcp = mcpState{on: true, command: "claude mcp add cogpit http://127.0.0.1:1/mcp"}
	sh.opts.MCPCommand = sh.mcp.command
	if got := menuLabels(menuNamed(sh.mainMenu(), "Help")); !slices.Contains(got, "Copy MCP command") {
		t.Errorf("Help menu with MCP %q", got)
	}

	if m := sh.windowMenu("darwin"); m != nil {
		t.Errorf("a Window menu on macOS: %q", menuLabels(m))
	}
	for _, goos := range []string{"linux", "windows"} {
		if got := menuLabels(sh.windowMenu(goos)); !slices.Equal(got, []string{"Toggle Full Screen", "", "Close Window"}) {
			t.Errorf("Window menu on %s: %q", goos, got)
		}
	}
	if sh.windowMenu("linux").Items[0].Action(); !sh.win.FullScreen() {
		t.Error("Toggle Full Screen")
	}
	// The Cluster menu: Disconnect greyed without a cluster.
	var disconnect *fyne.MenuItem
	for _, it := range sh.mainMenu().Items[0].Items {
		if it.Label == "Disconnect" {
			disconnect = it
		}
	}
	if disconnect == nil || !disconnect.Disabled {
		t.Errorf("Disconnect without a cluster: %+v", disconnect)
	}
}

// A click on a cluster that cannot be reached: the connect fails and the window shows its error
// page, not the welcome page (D-114): the cause, the connect stage by stage from the engine (TCP
// refused at 127.0.0.1:1), Connect Again. Help › Welcome then shows the welcome page, the failed
// cluster under Recent, alone (AW13).
func TestWelcomeShowsAFailedConnect(t *testing.T) {
	s := welcomeEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	w := welcomeShown(t, sh, q)
	test.Tap(w.rows[0]) // kind-dev, at https://127.0.0.1:1
	p := sh.tabs.errPage
	q.until(t, "the error page", func() bool { return sh.ui.Connection.State == engine.ConnectError && p.shown && len(p.rows) > 0 })
	if w.shown || p.title.text != "Could not connect to kind-dev" || !strings.Contains(p.cause.text, "127.0.0.1:1 refused the connection") {
		t.Errorf("after a failed connect: welcome %v, title %q, cause %q", w.shown, p.title.text, p.cause.text)
	}
	var results []string
	for _, r := range p.rows {
		results = append(results, r.stage.Name+"="+r.stage.Result)
	}
	if got := strings.Join(results, " "); got != "kubeconfig=ok dns=skipped tcp=failed tls=notReached api=notReached" {
		t.Errorf("the stages %s", got)
	}
	help := menuNamed(sh.mainMenu(), "Help")
	help.Items[0].Action() // Help › Welcome
	q.until(t, "the welcome page", func() bool { return w.shown && !p.shown })
	q.until(t, "this session's group", func() bool { return slices.Equal(w.shownClusters(), []string{"kind-dev"}) })
	if g := w.groups(); len(g) != 1 || g[0].title != "Recent" { // A67: was "This session"
		t.Errorf("groups %+v, want Recent alone", groupTitles(g))
	}
}

// The page links the repository on GitHub, in the browser (A113): the README for what Cogpit does,
// and the command list for the commands.
func TestWelcomeLinksTheReadmeAndTheCommands(t *testing.T) {
	s := welcomeEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	w := welcomeShown(t, sh, q)
	if w.docs.URL.String() != readmeURL || w.cmds.URL.String() != commandsURL ||
		commandsURL != "https://github.com/guided-traffic/cogpit/blob/main/docs/developer/commands.md" {
		t.Fatalf("links %s, %s", w.docs.URL, w.cmds.URL)
	}
	var opened []string
	old := openBrowser
	openBrowser = func(_ fyne.App, u *url.URL) error { opened = append(opened, u.String()); return nil }
	t.Cleanup(func() { openBrowser = old })
	w.docs.OnTapped() // a tap lands on the link's text, which the test cannot aim at
	w.cmds.OnTapped()
	if !slices.Equal(opened, []string{readmeURL, commandsURL}) {
		t.Errorf("opened %q", opened)
	}
}
