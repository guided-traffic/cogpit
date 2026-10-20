package fyneui

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// longContext is a cluster with a long name, whose tab's title is cut.
const longContext = "prod-eu-central-1-payments-platform-cluster [prod.yaml]"

// tabProbes are the real tab commands the tab bar's tests register, by parameter type.
var tabProbes = map[string]func(t testing.TB, f *fakeEngine, cmd *engine.Command){
	"ui.tab.open": probe[tabOpenParams], "ui.tab.pin": probe[tabIDParams], "ui.tab.close": probe[tabIDParams],
	"ui.tab.select": probe[tabSelectParams], "ui.tab.next": probe[struct{}], "ui.tab.previous": probe[struct{}],
	"ui.tab.back": probe[struct{}], "ui.tab.forward": probe[struct{}],
	"ui.tab.move": probe[tabMoveParams], "ui.tab.moveLeft": probe[struct{}], "ui.tab.moveRight": probe[struct{}],
}

// fakeTabCommands gives the fake engine the tab commands: stand-ins of the real ones in its
// registry (their labels and shortcuts) and handlers that change its UI state as the engine's do in
// the cases the tests use (engine/tabs_test.go tests the engine's own).
func fakeTabCommands(t testing.TB, f *fakeEngine) {
	t.Helper()
	reg := realCommands(t)
	for id, register := range tabProbes {
		c, ok := reg.Get(id)
		if !ok {
			t.Fatalf("the registry has no %s", id)
		}
		register(t, f, c)
	}
	decodeInto := func(inv engine.Invocation, p any) error { return json.Unmarshal(normalize(inv.Params), p) }
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers["ui.tab.open"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p tabOpenParams
		if err := decodeInto(inv, &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			id := fmt.Sprintf("tab-%d", len(u.Tabs)+1)
			u.Tabs = append(u.Tabs, engine.ViewTab{ID: id, Context: u.Context, View: p.View, Title: p.View + " - " + u.Context, Preview: !p.Fixed})
			u.ActiveTab, u.View = id, p.View
		})
		return nil, nil
	}
	f.handlers["ui.tab.select"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p tabSelectParams
		if err := decodeInto(inv, &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			for i, tab := range u.Tabs {
				if tab.ID == p.ID || i+1 == p.Index || (p.Last && i == len(u.Tabs)-1) {
					u.ActiveTab = tab.ID
					if tab.Context == u.Context {
						u.View = tab.View
					}
				}
			}
		})
		return nil, nil
	}
	f.handlers["ui.tab.close"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p tabIDParams
		if err := decodeInto(inv, &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			if p.ID == "" {
				p.ID = u.ActiveTab
			}
			u.Tabs = slices.DeleteFunc(u.Tabs, func(tab engine.ViewTab) bool { return tab.ID == p.ID })
			if u.ActiveTab == p.ID {
				u.ActiveTab = ""
			}
		})
		return nil, nil
	}
	f.handlers["ui.tab.move"] = func(_ context.Context, inv engine.Invocation) (any, error) { // every tab in the bar (the tests' scope)
		var p tabMoveParams
		if err := decodeInto(inv, &p); err != nil {
			return nil, err
		}
		var err error
		f.update(func(u *engine.UIState) {
			id := cmp.Or(p.ID, u.ActiveTab)
			i := slices.IndexFunc(u.Tabs, func(tab engine.ViewTab) bool { return tab.ID == id })
			if i < 0 || p.Index < 1 || p.Index > len(u.Tabs) {
				err = fmt.Errorf("no place %d for %q", p.Index, id)
				return
			}
			tab := u.Tabs[i]
			u.Tabs = slices.Insert(slices.Delete(slices.Clone(u.Tabs), i, i+1), p.Index-1, tab)
		})
		return nil, err
	}
	step := func(d int) func(context.Context, engine.Invocation) (any, error) {
		return func(_ context.Context, _ engine.Invocation) (any, error) {
			f.update(func(u *engine.UIState) {
				i := slices.IndexFunc(u.Tabs, func(tab engine.ViewTab) bool { return tab.ID == u.ActiveTab })
				if i >= 0 && i+d >= 0 && i+d < len(u.Tabs) {
					tab := u.Tabs[i]
					u.Tabs = slices.Insert(slices.Delete(slices.Clone(u.Tabs), i, i+1), i+d, tab)
				}
			})
			return nil, nil
		}
	}
	f.handlers["ui.tab.moveLeft"], f.handlers["ui.tab.moveRight"] = step(-1), step(1)
	f.handlers["ui.tab.pin"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p tabIDParams
		if err := decodeInto(inv, &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			for i := range u.Tabs {
				if u.Tabs[i].ID == p.ID || (p.ID == "" && u.Tabs[i].ID == u.ActiveTab) {
					u.Tabs[i].Preview = false
				}
			}
		})
		return nil, nil
	}
}

// tabsFake is the demo with three tabs: Services (fixed), Pods (the preview tab, shown, with a view
// before it in its history) and a tab of a cluster with a long name, which is not connected; the
// navigator at the section level and mixed environments on, so that the bar shows them all (GW11:
// off, it shows the shown cluster's only).
func tabsFake(t testing.TB) *fakeEngine {
	f := connectedFake(t, 200)
	f.ui.Tabs = []engine.ViewTab{
		{ID: "tab-1", Context: "demo", View: "services", Title: "Services - demo", Sort: engine.DefaultSort},
		{ID: "tab-2", Context: "demo", View: "pods", Title: "Pods - demo", Preview: true, Sort: engine.DefaultSort, Back: []string{"deployments"}},
		{ID: "tab-3", Context: longContext, View: "validatingadmissionpolicybindings.admissionregistration.k8s.io",
			Title: "Validating Admission Policy Bindings - " + engine.ContextLabel(longContext), Sort: engine.DefaultSort},
	}
	f.ui.ActiveTab = "tab-2"
	f.ui.NavigatorScope = engine.NavScopeLocal
	mixed := engine.DefaultPreferences()
	mixed.MixedEnvironments = true // every cluster's tabs in the bar (navscope.go TabsShown)
	f.ui.Preferences = &mixed
	fakeTabCommands(t, f)
	return f
}

func tabsShell(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := tabsFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: mode, MCPCommand: "claude mcp add cogpit -- cogpit-cli mcp"})
	q.until(t, "the shell", func() bool { return len(sh.kinds) > 0 && sh.diag.total > 0 && len(sh.tabs.shown) == 3 })
	return sh, w, q, f
}

// tabRenderer returns a tab's renderer.
func tabRenderer(tab *viewTab) *viewTabRenderer { return test.WidgetRenderer(tab).(*viewTabRenderer) }

// The bar shows every tab: the cluster's badge, "<view> - <context>" (italic for the preview tab),
// a close button; the active tab raised, a tab of a cluster that is not connected with a muted
// badge, a long title cut and shown whole as the tooltip. Without tabs the bar is hidden.
func TestTabBarShowsTheTabs(t *testing.T) {
	sh, w, q, f := tabsShell(t, ThemeDark)
	bar := sh.tabs
	if !bar.root.Visible() {
		t.Fatal("the tab bar is hidden")
	}
	var titles []string
	for _, tab := range bar.shown {
		titles = append(titles, tab.tab.Title)
	}
	if !slices.Equal(titles, []string{"Services - demo", "Pods - demo", "Validating Admission Policy Bindings - prod-eu-central-1-payments-platform-cluster"}) {
		t.Errorf("titles %q", titles)
	}
	services, pods, long := bar.shown[0], bar.shown[1], bar.shown[2]
	if !pods.active || services.active || long.active {
		t.Error("the active tab is not the preview tab alone")
	}
	if !tabRenderer(pods).title.TextStyle.Italic || tabRenderer(services).title.TextStyle.Italic {
		t.Error("only the preview tab's title is italic")
	}
	if r := tabRenderer(long); long.live || !strings.HasSuffix(r.title.Text, "…") || r.initials.Text != "PE" {
		t.Errorf("the long tab: live %v, title %q, badge %q", long.live, r.title.Text, r.initials.Text)
	}
	if r := tabRenderer(services); !services.live || r.initials.Text != "DE" {
		t.Errorf("a connected tab: live %v, badge %q", services.live, r.initials.Text)
	}

	sh.tip.delay = time.Millisecond
	settle(w)
	test.MoveMouse(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(long).AddXY(40, 8))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	if got := sh.tip.visibleText(); got != long.tab.Title+"\n"+long.tab.Context { // the cluster's full context id under the title (D-68)
		t.Errorf("tooltip %q", got)
	}

	f.update(func(u *engine.UIState) { u.Tabs, u.ActiveTab = nil, "" })
	q.until(t, "the bar to hide", func() bool { return !bar.root.Visible() })
}

// A click activates a tab; a click on the active one does nothing; a double click on the preview
// tab fixes it, two clicks apart do not; the close button closes its tab.
func TestTabBarClicks(t *testing.T) {
	sh, _, q, f := tabsShell(t, ThemeDark)
	bar := sh.tabs
	clock := fixedNow
	bar.now = func() time.Time { return clock }
	test.Tap(bar.shown[1]) // the active tab
	clock = clock.Add(time.Second)
	test.Tap(bar.shown[0])
	q.until(t, "Services active", func() bool { return sh.ui.ActiveTab == "tab-1" })
	if calls := f.callsOf("ui.tab.select"); len(calls) != 1 || decodeParams[tabSelectParams](t, calls[0]).ID != "tab-1" {
		t.Errorf("ui.tab.select %v", calls)
	}
	clock = clock.Add(time.Second)
	test.Tap(bar.shown[1])
	clock = clock.Add(100 * time.Millisecond)
	test.Tap(bar.shown[1]) // the second click of a double click: fixed
	q.until(t, "the preview tab fixed", func() bool { return len(sh.ui.Tabs) == 3 && !sh.ui.Tabs[1].Preview })
	if calls := f.callsOf("ui.tab.pin"); len(calls) != 1 || decodeParams[tabIDParams](t, calls[0]).ID != "tab-2" {
		t.Errorf("ui.tab.pin %v", calls)
	}
	if !strings.Contains(tabRenderer(bar.shown[1]).title.Text, "Pods") || tabRenderer(bar.shown[1]).title.TextStyle.Italic {
		t.Error("the fixed tab's title is still italic")
	}
	test.Tap(bar.shown[2].close)
	q.until(t, "the long tab closed", func() bool { return len(sh.ui.Tabs) == 2 })
	if calls := f.callsOf("ui.tab.close"); len(calls) != 1 || decodeParams[tabIDParams](t, calls[0]).ID != "tab-3" {
		t.Errorf("ui.tab.close %v", calls)
	}
	if len(f.callsOf("ui.tab.select")) != 2 {
		t.Errorf("selects %v", f.callsOf("ui.tab.select"))
	}
}

// menuItemWithin finds a main-menu item with a shortcut, in submenus too.
func menuItemWithin(m *fyne.MainMenu, sc fyne.Shortcut) *fyne.MenuItem {
	var find func(items []*fyne.MenuItem) *fyne.MenuItem
	find = func(items []*fyne.MenuItem) *fyne.MenuItem {
		for _, item := range items {
			if item.Shortcut != nil && item.Shortcut.ShortcutName() == sc.ShortcutName() {
				return item
			}
			if item.ChildMenu != nil {
				if got := find(item.ChildMenu.Items); got != nil {
					return got
				}
			}
		}
		return nil
	}
	for _, menu := range m.Items {
		if got := find(menu.Items); got != nil {
			return got
		}
	}
	return nil
}

// The tab keys are main-menu items, so they work whatever has the focus: ⌘W, ⌘⌥→, ⌘⌥←, ⌘1–⌘8,
// ⌘9 (the last tab), ⌃- and ⌃⇧-; a key with nothing to do dispatches nothing.
func TestTabShortcuts(t *testing.T) {
	sh, _, q, f := tabsShell(t, ThemeDark)
	sd := fyne.KeyModifierShortcutDefault
	press := func(sc *desktop.CustomShortcut) {
		t.Helper()
		item := menuItemWithin(sh.mainMenu(), sc)
		if item == nil {
			t.Fatalf("no menu item has %s", sc.ShortcutName())
		}
		item.Action()
		q.drain()
	}
	press(&desktop.CustomShortcut{KeyName: fyne.KeyRight, Modifier: sd | fyne.KeyModifierAlt})
	press(&desktop.CustomShortcut{KeyName: fyne.KeyLeft, Modifier: sd | fyne.KeyModifierAlt})
	press(&desktop.CustomShortcut{KeyName: fyne.KeyMinus, Modifier: fyne.KeyModifierControl})                         // tab-2 has a view before
	press(&desktop.CustomShortcut{KeyName: fyne.KeyMinus, Modifier: fyne.KeyModifierControl | fyne.KeyModifierShift}) // and none after
	press(&desktop.CustomShortcut{KeyName: fyne.Key1, Modifier: sd})
	q.until(t, "tab 1", func() bool { return sh.ui.ActiveTab == "tab-1" })
	press(&desktop.CustomShortcut{KeyName: fyne.Key9, Modifier: sd})
	q.until(t, "the last tab", func() bool { return sh.ui.ActiveTab == "tab-3" })
	press(&desktop.CustomShortcut{KeyName: fyne.Key5, Modifier: sd}) // no fifth tab
	press(&desktop.CustomShortcut{KeyName: fyne.KeyW, Modifier: sd})
	q.until(t, "the tab closed", func() bool { return len(sh.ui.Tabs) == 2 })
	for id, n := range map[string]int{"ui.tab.next": 1, "ui.tab.previous": 1, "ui.tab.back": 1, "ui.tab.forward": 0, "ui.tab.close": 1, "ui.tab.select": 2} {
		if got := len(f.callsOf(id)); got != n {
			t.Errorf("%s dispatched %d times, want %d", id, got, n)
		}
	}
	if p := decodeParams[tabSelectParams](t, f.callsOf("ui.tab.select")[1]); !p.Last {
		t.Errorf("⌘9 selected %+v, want the last tab", p)
	}
	f.update(func(u *engine.UIState) { u.Tabs, u.ActiveTab = nil, "" })
	q.until(t, "no tabs", func() bool { return len(sh.ui.Tabs) == 0 })
	press(&desktop.CustomShortcut{KeyName: fyne.KeyW, Modifier: sd})
	press(&desktop.CustomShortcut{KeyName: fyne.Key9, Modifier: sd})
	if len(f.callsOf("ui.tab.close")) != 1 || len(f.callsOf("ui.tab.select")) != 2 {
		t.Error("a tab key dispatched without tabs")
	}
}

// A click on a navigator item opens its view in the preview tab (ui.openView), a double click in a
// fixed tab (ui.tab.open); clicks apart are two single clicks; a group has no double click.
func TestNavigatorClickAndDoubleClick(t *testing.T) {
	sh, _, q, f := navScene(t, ThemeDark)
	fakeTabCommands(t, f)
	tr := sh.side.tree
	clock := fixedNow
	sh.tabs.now = func() time.Time { return clock }
	test.Tap(tr.rows["nodes"])
	q.until(t, "the Nodes view", func() bool { return sh.ui.View == "nodes" })
	clock = clock.Add(200 * time.Millisecond)
	test.Tap(tr.rows["nodes"])
	q.until(t, "a fixed tab", func() bool { return len(sh.ui.Tabs) == 1 })
	if calls := f.callsOf("ui.tab.open"); len(calls) != 1 || decodeParams[tabOpenParams](t, calls[0]) != (tabOpenParams{View: "nodes", Fixed: true}) {
		t.Errorf("ui.tab.open %v", calls)
	}
	clock = clock.Add(time.Second)
	test.Tap(tr.rows["deployments"])
	q.until(t, "the Deployments view", func() bool { return sh.ui.View == "deployments" })
	clock = clock.Add(time.Second)
	test.Tap(tr.rows["deployments"]) // a second click, not a double one: the view is open already
	group := groupPrefix + "network"
	test.Tap(tr.rows[group])
	test.Tap(tr.rows[group])
	q.drain()
	if got := openViews(t, f); !slices.Equal(got, []string{"nodes", "deployments"}) {
		t.Errorf("ui.openView %v", got)
	}
	if len(f.callsOf("ui.tab.open")) != 1 || tr.IsBranchOpen(group) {
		t.Errorf("ui.tab.open %d times, %s open %v", len(f.callsOf("ui.tab.open")), group, tr.IsBranchOpen(group))
	}
}

// The tab parameters the window sends match the engine's schemas.
func TestTabParamsMatchTheRegistry(t *testing.T) {
	f := newFakeEngine(t)
	fakeTabCommands(t, f)
	reg := realCommands(t)
	for id := range tabProbes {
		p, _ := f.reg.Get(id)
		r, _ := reg.Get(id)
		pr, rr := slices.Sorted(slices.Values(p.Schema.Required)), slices.Sorted(slices.Values(r.Schema.Required))
		if !slices.Equal(pr, rr) || len(p.Schema.Properties) != len(r.Schema.Properties) {
			t.Errorf("%s: the window's params require %v of %d, the command %v of %d", id, pr, len(p.Schema.Properties), rr, len(r.Schema.Properties))
		}
	}
}

// Over the real engine and a small demo cluster: the connect shows "Pods - demo" in the preview
// tab; a click in the navigator replaces it, a double click opens a fixed tab, ⌘W closes the active
// tab and the nearest of the cluster's shows.
func TestTabBarOverTheEngine(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	conn := &engine.ClusterConnector{IncludeDemo: true}
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
	sh := newShell(s, a, w, Options{AutoConnect: engine.DemoContext}, q.do)
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
				t.Fatalf("timed out waiting for %s: tabs %+v", what, sh.ui.Tabs)
			}
			q.drain()
			time.Sleep(5 * time.Millisecond)
		}
		q.drain()
	}
	titles := func() []string {
		var out []string
		for _, tab := range sh.tabs.shown {
			s := tab.tab.Title
			if tab.tab.Preview {
				s += " (preview)"
			}
			if tab.active {
				s += " *"
			}
			out = append(out, s)
		}
		return out
	}
	until("the preview tab", func() bool {
		return slices.Equal(titles(), []string{"Pods - demo (preview) *"}) && len(sh.kinds) > 0 && sh.side.tree.rows["deployments"] != nil
	})
	clock := time.Now()
	sh.tabs.now = func() time.Time { return clock }
	tr := sh.side.tree
	test.Tap(tr.rows["deployments"])
	until("Deployments in the preview tab", func() bool { return slices.Equal(titles(), []string{"Deployments - demo (preview) *"}) })
	clock = clock.Add(time.Second)
	tr.OpenBranch(groupPrefix + "network")
	until("the Network group", func() bool { return tr.rows["services"] != nil })
	test.Tap(tr.rows["services"])
	until("Services in the preview tab", func() bool { return slices.Equal(titles(), []string{"Services - demo (preview) *"}) })
	clock = clock.Add(100 * time.Millisecond)
	test.Tap(tr.rows["services"]) // the double click's second: the preview tab showing it becomes fixed
	until("a fixed Services tab", func() bool { return slices.Equal(titles(), []string{"Services - demo *"}) })
	clock = clock.Add(time.Second)
	test.Tap(tr.rows["pods"])
	until("a new preview tab", func() bool { return slices.Equal(titles(), []string{"Services - demo", "Pods - demo (preview) *"}) })
	test.Tap(sh.tabs.shown[0])
	until("Services shown", func() bool { return sh.ui.View == "services" && sh.tabs.shown[0].active })
	item := menuItemWithin(sh.mainMenu(), &desktop.CustomShortcut{KeyName: fyne.KeyW, Modifier: fyne.KeyModifierShortcutDefault})
	if item == nil {
		t.Fatal("no ⌘W")
	}
	item.Action()
	until("the Services tab closed", func() bool {
		return slices.Equal(titles(), []string{"Pods - demo (preview) *"}) && sh.ui.View == "pods"
	})
}

func TestRenderTabBarDark(t *testing.T)  { renderTabBar(t, ThemeDark, "tabbar_dark.png") }
func TestRenderTabBarLight(t *testing.T) { renderTabBar(t, ThemeLight, "tabbar_light.png") }

// renderTabBar: three tabs, the preview tab shown (italic), a fixed one, and a tab of another
// cluster with a long title, cut, hovered so that its tooltip shows the whole title.
func renderTabBar(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _ := tabsShell(t, mode)
	settle(w)
	sh.tip.delay = time.Millisecond
	long := sh.tabs.shown[2]
	test.MoveMouse(w.Canvas(), fyne.CurrentApp().Driver().AbsolutePositionForObject(long).AddXY(60, 10))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	settle(w)
	savePNG(t, w, name)
}

// A tab's badge is its cluster's identity: the display name's initials in the icon colour set in
// the cluster's settings, as the navigator and the hotbar show it (2026-10-13).
func TestTabBadgeTakesTheClustersIdentity(t *testing.T) {
	sh, _, q, f := tabsShell(t, ThemeDark)
	services := sh.tabs.shown[0]
	before := tabRenderer(services).initials.Color
	f.update(func(u *engine.UIState) {
		u.ClusterSettings = map[string]engine.ClusterSettings{"demo": {Name: "Shop prod", IconColor: "magenta"}}
	})
	q.until(t, "the tab's badge", func() bool { return tabRenderer(sh.tabs.shown[0]).initials.Text == "SP" })
	r := tabRenderer(sh.tabs.shown[0])
	if want := themeColor(sh.tabs.shown[0].Theme(), identityColorName("magenta")); r.initials.Color != want || r.initials.Color == before {
		t.Errorf("badge colour %v, want the cluster's magenta %v", r.initials.Color, want)
	}
}
