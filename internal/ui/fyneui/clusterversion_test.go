package fyneui

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/k8seol"
)

// versionTable is the release table of the window's tests: upstream's of 2026-10-18 from 1.31 on,
// so that a refresh of the table built in moves no expectation.
func versionTable() *k8seol.Table {
	return &k8seol.Table{Source: "test", Fetched: "2026-10-18", Minors: []k8seol.Minor{
		{Minor: "1.31", EndOfLife: "2025-11-11"},
		{Minor: "1.32", EndOfLife: "2026-02-28"},
		{Minor: "1.33", EndOfLife: "2026-06-28"},
		{Minor: "1.34", Released: "2025-08-27", MaintenanceFrom: "2026-08-27", EndOfLife: "2026-10-27"},
		{Minor: "1.35", Released: "2025-12-17", MaintenanceFrom: "2026-12-28", EndOfLife: "2027-02-28"},
		{Minor: "1.36", Released: "2026-04-22", MaintenanceFrom: "2027-04-28", EndOfLife: "2027-06-28"},
		{Minor: "1.37", Released: "2026-08-26", MaintenanceFrom: "2027-08-28", EndOfLife: "2027-10-28"},
	}}
}

// clusterVersionOf is the engine's cluster version of userCtx on fixedNow's day.
func clusterVersionOf(version string) *engine.ClusterVersion {
	sup := versionTable().SupportOf(version)
	return &engine.ClusterVersion{Context: userCtx, Version: version, Support: sup, State: sup.StateAt(fixedNow), DaysLeft: sup.DaysLeft(fixedNow)}
}

// The root row shows the API server's version at the right end of the user's line: muted while
// supported, pending in maintenance mode, the warning colour with a ⚠ past its end of life; its
// tooltip says until when (the days left) or since when, "about … estimated" beyond the table, and
// the table's day. Hidden while not live and for another context's version; a version that
// changes alone (a reconnect, a day) is shown at once.
func TestRootRowShowsTheClusterVersion(t *testing.T) {
	const asOf = "\nUpstream dates as of 2026-10-18"
	for _, c := range []struct {
		version, text string
		color         fyne.ThemeColorName
		tip           string
	}{
		{"v1.37.1", "v1.37.1", colorMuted, "Upstream support until 2027-10-28 (382 days)" + asOf},
		{"v1.34.6", "v1.34.6", statusColorName(model.StatusPending), "Upstream support until 2026-10-27 (16 days), maintenance mode since 2026-08-27" + asOf},
		{"v1.33.9", "v1.33.9 ⚠", colorStatusWarning, "Upstream end of life since 2026-06-28" + asOf},
		{"v1.39.2-gke.100", "v1.39.2-gke.100", colorMuted, "Upstream support until about 2028-06-28 (626 days), estimated: this Cogpit's table ends at 1.37" + asOf},
		{"v2.0.0", "v2.0.0", colorMuted, "No upstream support dates for this version" + asOf},
	} {
		f := userFake(t, confirmedUser, engine.Connected)
		f.ui.ClusterVersion = clusterVersionOf(c.version)
		sh, _, _ := userScene(t, f, ThemeDark)
		s := sh.side
		if !s.version.Visible() || s.version.text != c.text || s.version.color != c.color {
			t.Errorf("%s: shown %v %q in %s", c.version, s.version.Visible(), s.version.text, s.version.color)
		}
		if got := s.rootRow.tipAt(middleOf(s.version)); got != c.tip {
			t.Errorf("%s: tooltip %q\nwant %q", c.version, got, c.tip)
		}
		// A line of its own under the user's, as wide as the name's: the name and the user keep
		// their room.
		nameAt, userAt, at := absPos(s.name), absPos(s.subtitle), absPos(s.version)
		if at.X != nameAt.X || at.Y <= userAt.Y || s.version.Size().Width != s.name.Size().Width {
			t.Errorf("%s: the version at %v %v wide, the name at %v %v wide, the user at %v", c.version, at, s.version.Size().Width,
				nameAt, s.name.Size().Width, userAt)
		}
		if got := s.rootRow.tipAt(middleOf(s.groups)); got != "team-a\nsystem:authenticated" {
			t.Errorf("%s: the box's tooltip %q", c.version, got)
		}
	}

	f := userFake(t, confirmedUser, engine.Connected)
	f.ui.ClusterVersion = clusterVersionOf("v1.37.1")
	sh, _, q := userScene(t, f, ThemeDark)
	f.update(func(u *engine.UIState) { u.ClusterVersion = clusterVersionOf("v1.33.9") }) // the context and connection as they were
	q.until(t, "the new version", func() bool { return sh.side.version.text == "v1.33.9 ⚠" })
	f.update(func(u *engine.UIState) {
		cv := *clusterVersionOf("v1.37.1")
		cv.Context = "kind-cogpit"
		u.ClusterVersion = &cv
	})
	q.until(t, "another context's version hidden", func() bool { return !sh.side.version.Visible() && sh.side.versionTip == "" })
	f.update(func(u *engine.UIState) { u.ClusterVersion = nil })
	q.until(t, "the version hidden", func() bool { return !sh.side.version.Visible() })
}

// withSettingsCommand registers cluster.settings.open as the real registry declares it, its
// dispatches recorded.
func withSettingsCommand(t *testing.T, f *fakeEngine) func() []clusterSettingsOpenParams {
	t.Helper()
	c, _ := realCommands(t).Get("cluster.settings.open")
	probe[clusterSettingsOpenParams](t, f, c)
	var mu sync.Mutex // the handler runs on dispatch goroutines
	var opened []clusterSettingsOpenParams
	f.handlers["cluster.settings.open"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		mu.Lock()
		opened = append(opened, decodeParams[clusterSettingsOpenParams](t, inv))
		mu.Unlock()
		return nil, nil
	}
	return func() []clusterSettingsOpenParams {
		mu.Lock()
		defer mu.Unlock()
		return append([]clusterSettingsOpenParams(nil), opened...)
	}
}

// The gear before the root's dot opens the shown cluster's Settings, its tooltip naming ⌘⇧,; it
// goes with the cluster, and a window without the command has none.
func TestRootRowGearOpensClusterSettings(t *testing.T) {
	f := userFake(t, confirmedUser, engine.Connected)
	opened := withSettingsCommand(t, f)
	sh, w, q := userScene(t, f, ThemeDark)
	gear := sh.side.rootRow.settings
	if gear == nil || !gear.Visible() {
		t.Fatal("no gear in the root row")
	}
	want := "Cluster Settings (Ctrl+Shift+,)"
	if runtime.GOOS == goosMac {
		want = "Cluster Settings (⌘⇧,)"
	}
	if gear.tip != want {
		t.Errorf("the gear's tooltip %q, want %q", gear.tip, want)
	}
	if gx, dx := gear.Position().X, sh.side.dot.Position().X; absPos(gear).X >= absPos(sh.side.dot).X {
		t.Errorf("the gear at %v (%v) is not before the dot at %v (%v)", absPos(gear), gx, absPos(sh.side.dot), dx)
	}
	// The navigator's default width cuts the username beside the gear: its tooltip has it whole.
	if natural := fyne.MeasureText("alice@example.com", sh.side.subtitle.Theme().Size(sh.side.subtitle.size), sh.side.subtitle.style).Width; sh.side.subtitle.Size().Width < natural {
		if got := sh.side.rootRow.tipAt(middleOf(sh.side.subtitle)); got != "alice@example.com" {
			t.Errorf("the cut username's tooltip %q", got)
		}
	}
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(gear))
	q.until(t, "the gear's tooltip", func() bool { return sh.tip.visibleText() == want })
	test.Tap(gear)
	q.until(t, "Cluster Settings", func() bool { o := opened(); return len(o) == 1 && o[0].Context == userCtx && o[0].Section == "" })

	f.update(func(u *engine.UIState) { u.Context, u.Connection = "", engine.Connection{State: engine.Disconnected} })
	q.until(t, "no cluster, no gear", func() bool { return !gear.Visible() })

	plain, _, _ := userScene(t, userFake(t, confirmedUser, engine.Connected), ThemeDark)
	if plain.side.rootRow.settings != nil {
		t.Error("a gear without the command")
	}
}

func absPos(o fyne.CanvasObject) fyne.Position {
	return fyne.CurrentApp().Driver().AbsolutePositionForObject(o)
}

// A cell with its own tooltip shows it on hover: a kubelet's support and skew in the Nodes list.
func TestTableCellShowsItsTip(t *testing.T) {
	f := versionsFake(t, "nodes")
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the nodes view", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == "nodes" })
	settle(w)
	c := cellAt(t, sh, rowOf(t, f, "nodes", "worker-b-01"), columnOf(t, sh, "version"))
	if c.full != "v1.33.9 ⚠" {
		t.Fatalf("the cell shows %q", c.full)
	}
	sh.tip.delay = time.Millisecond
	hoverOver(w, q, middleOf(c))
	want := "Upstream end of life since 2026-06-28\nKubelet 1.33 is 4 minors behind the API server 1.37: the version skew policy allows 3"
	q.until(t, "the cell's tooltip", func() bool { return sh.tip.visibleText() == want })
}

// versionsServer is the API server's version of the versions scenes.
const versionsServer = "v1.37.1"

// versionKubelets are the kubelets of the versions scenes that are not the API server's.
var versionKubelets = map[string]string{
	"worker-a-02": "v1.34.6", // maintenance mode
	"worker-b-01": "v1.33.9", // past its end of life, and four minors behind
	"worker-c-01": "v1.38.0", // newer than the API server
}

// versionsFake is kindsFake with a few kubelets of other versions, the Nodes list's Version cells
// and the nodes' drawers as the engine's version check makes them (KubeletVersionCell,
// MarkKubeletVersion) against the API server's version.
func versionsFake(t testing.TB, view string) *fakeEngine {
	f := kindsFake(t, view)
	tf := model.TimeLayout{}.In(time.UTC)
	snap := *f.tables["nodes"]
	vi := -1
	for i, c := range snap.Columns {
		if c.ID == "version" {
			vi = i
		}
	}
	rows := make([]model.Row, len(snap.Rows))
	for i, r := range snap.Rows {
		kubelet := r.Cells[vi].Text
		if v, ok := versionKubelets[r.Ref.Name]; ok {
			kubelet = v
		}
		cells := append([]model.Cell(nil), r.Cells...)
		cells[vi] = engine.KubeletVersionCell(versionTable(), model.Cell{Text: kubelet}, kubelet, versionsServer, fixedNow, tf)
		r.Cells = cells
		rows[i] = r
		ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: r.Ref.Name}
		if m := f.details[ref.String()]; m != nil {
			for si := range m.Sections {
				for ii := range m.Sections[si].Items {
					if m.Sections[si].Items[ii].Name == "Kubelet version" {
						m.Sections[si].Items[ii].Value = kubelet
					}
				}
			}
			engine.MarkKubeletVersion(versionTable(), m, kubelet, versionsServer, fixedNow, tf)
		}
	}
	snap.Rows = rows
	f.tables["nodes"] = &snap
	return f
}

func TestRenderNavigatorVersionDark(t *testing.T) { renderNavigatorVersion(t, ThemeDark, "dark") }

// renderNavigatorVersion: the root row of a cluster whose API server's minor is past its upstream
// end of life, the pointer on the version and its tooltip shown, dates in a German computer's
// format; the gear before the dot.
func renderNavigatorVersion(t *testing.T, mode ThemeMode, suffix string) {
	f := userFake(t, confirmedUser, engine.Connected)
	withSettingsCommand(t, f)
	f.ui.TimeFormat = germanLayout
	f.ui.ClusterVersion = clusterVersionOf("v1.33.9")
	sh, w, q := userScene(t, f, mode)
	q.until(t, "diagnostics", func() bool { return sh.diag.total > 0 })
	settle(w)
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(sh.side.version))
	q.until(t, "the version's tooltip", func() bool { return strings.HasPrefix(sh.tip.visibleText(), "Upstream end of life since 28.06.2026") })
	savePNG(t, w, "navigator_version_"+suffix+".png")
}

func TestRenderNodesVersionsDark(t *testing.T) { renderNodesVersions(t, ThemeDark, "dark") }

// renderNodesVersions: the Nodes list with a kubelet in maintenance mode (pending colour), one past
// its end of life and four minors behind and one newer than the API server (⚠), the pointer on
// the one past its end of life and its tooltip shown.
func renderNodesVersions(t *testing.T, mode ThemeMode, suffix string) {
	f := versionsFake(t, "nodes")
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the nodes view", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == "nodes" && sh.diag.total > 0
	})
	settle(w)
	sh.tip.delay = time.Millisecond
	hoverOver(w, q, middleOf(cellAt(t, sh, rowOf(t, f, "nodes", "worker-b-01"), columnOf(t, sh, "version"))))
	q.until(t, "the cell's tooltip", func() bool { return strings.HasPrefix(sh.tip.visibleText(), "Upstream end of life") })
	savePNG(t, w, "nodes_versions_"+suffix+".png")
}

func TestRenderNodeDrawerVersionsDark(t *testing.T) { renderNodeDrawerVersions(t, ThemeDark, "dark") }

// renderNodeDrawerVersions: the drawer of the node whose kubelet is past its end of life and four
// minors behind: its Kubelet version in the warning colour, Upstream support and Version skew after
// it.
func renderNodeDrawerVersions(t *testing.T, mode ThemeMode, suffix string) {
	f := versionsFake(t, "nodes")
	registerProbes(t, f)
	f.disabled["node.drain"] = "the demo cluster cannot be changed"
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: "worker-b-01"}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer", func() bool {
		return len(sh.kinds) > 0 && sh.drawer.last != nil && len(sh.drawer.actions.Objects) > 0 && sh.diag.total > 0
	})
	settle(w)
	savePNG(t, w, "node_drawer_versions_"+suffix+".png")
}
