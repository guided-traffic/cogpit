package fyneui

import (
	"os"
	"strings"
	"testing"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// iconFile is a kept icon's file name, as the engine names them.
var iconFile = strings.Repeat("ab", 32) + ".png"

// iconResource is an uploaded icon as the shell holds it once read (clusteridentity.go): the test
// PNG of pngFile.
func iconResource(t *testing.T) fyne.Resource {
	t.Helper()
	b, err := os.ReadFile(pngFile(t))
	if err != nil {
		t.Fatal(err)
	}
	return fyne.NewStaticResource(iconFile, b)
}

// TestTabShowsTheClustersImage (the owner's F25): a tab of a cluster with an uploaded icon shows the
// image in its badge's place, as the navigator and the hotbar do, also when the image arrives after
// the tab drew the initials; faded while its cluster is not connected; the initials again once the
// icon is reset to a colour.
func TestTabShowsTheClustersImage(t *testing.T) {
	sh, _, q, f := tabsShell(t, ThemeDark)
	f.update(func(u *engine.UIState) {
		u.ClusterSettings = map[string]engine.ClusterSettings{"demo": {Name: "wds18-main", Icon: iconFile}, longContext: {Icon: iconFile}}
	})
	q.until(t, "the initials while the image loads", func() bool { return tabRenderer(sh.tabs.shown[0]).initials.Text == "WM" })
	if r := tabRenderer(sh.tabs.shown[0]); r.img.Visible() || !r.initials.Visible() {
		t.Fatal("an image shows before it was read")
	}

	res := iconResource(t)
	sh.icons = map[string]fyne.Resource{iconFile: res} // what Session.ClusterIcon returned
	sh.identitiesChanged()
	q.drain()
	for i, tab := range sh.tabs.shown {
		r := tabRenderer(tab)
		if !r.img.Visible() || r.img.Resource != res || r.initials.Visible() || r.badge.Visible() {
			t.Errorf("tab %d: image shown %v (%v), initials %v, badge %v", i, r.img.Visible(), r.img.Resource, r.initials.Visible(), r.badge.Visible())
		}
		if want := map[bool]float64{true: 0, false: notLiveFade}[tab.live]; r.img.Translucency != want {
			t.Errorf("tab %d (live %v): translucency %v, want %v", i, tab.live, r.img.Translucency, want)
		}
	}
	if long := sh.tabs.shown[2]; long.live {
		t.Fatal("the long context's tab should be of a cluster that is not connected")
	}
	if r := tabRenderer(sh.tabs.shown[0]); r.img.Size().Width <= 0 || r.img.Size() != r.badge.Size() {
		t.Errorf("the image's size %v, the badge's %v", r.img.Size(), r.badge.Size())
	}

	f.update(func(u *engine.UIState) {
		u.ClusterSettings = map[string]engine.ClusterSettings{"demo": {Name: "wds18-main", IconColor: "orange"}}
	})
	q.until(t, "the initials again", func() bool {
		r := tabRenderer(sh.tabs.shown[0])
		return !r.img.Visible() && r.initials.Visible() && r.initials.Color == themeColor(sh.tabs.shown[0].Theme(), identityColorName("orange"))
	})
}

func TestRenderTabBarClusterIconsDark(t *testing.T) {
	renderTabBarClusterIcons(t, ThemeDark, "tabbar_cluster_icons_dark.png")
}

func TestRenderTabBarClusterIconsLight(t *testing.T) {
	renderTabBarClusterIcons(t, ThemeLight, "tabbar_cluster_icons_light.png")
}

// renderTabBarClusterIcons: the shown cluster's tab with its uploaded image (the owner's case), a
// tab of a cluster connected in the background with its icon colour, one of a cluster that is not
// connected with an image, faded; the navigator shows the same identities.
func renderTabBarClusterIcons(t *testing.T, mode ThemeMode, name string) {
	f, _ := marksFake(t, true)
	f.ui.Tabs = []engine.ViewTab{
		{ID: "tab-1", Context: cShown, View: kindPods, Title: "Pods - wds18-main", Sort: engine.DefaultSort},
		{ID: "tab-2", Context: cCold, View: "services", Title: "Services - staging-us-east", Sort: engine.DefaultSort},
		{ID: "tab-3", Context: cFailed, View: "nodes", Title: "Nodes - qa-eu-west", Sort: engine.DefaultSort},
	}
	f.ui.ActiveTab = "tab-1"
	f.ui.ClusterSettings = map[string]engine.ClusterSettings{
		cShown: {Name: "wds18-main", Icon: iconFile}, cCold: {IconColor: "magenta"}, cFailed: {Icon: iconFile},
	}
	mixed := engine.DefaultPreferences()
	mixed.MixedEnvironments = true // every cluster's tabs in the bar (GW11)
	f.ui.Preferences = &mixed
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the tabs", func() bool { return len(sh.tabs.shown) == 3 && len(sh.kinds) > 0 && sh.side.sec.loaded })
	sh.icons = map[string]fyne.Resource{iconFile: iconResource(t)}
	sh.identitiesChanged()
	q.drain()
	if r := tabRenderer(sh.tabs.shown[0]); !r.img.Visible() {
		t.Fatal("the shown cluster's tab has no image")
	}
	savePNG(t, w, name)
}
