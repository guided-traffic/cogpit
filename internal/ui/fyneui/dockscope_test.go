package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The dock per cluster (D-69 item 9; engine/dockscope.go): the dock shows the shown cluster's tabs
// and those of no cluster, the other connected clusters' tabs keep their streams behind it.

// dockClustersFake is kind-cogpit shown with a logs tab of its own and the Audit Log, staging-us-east
// connected behind it; an agent then opens a pod's logs on staging-us-east, which go to that
// cluster's dock.
func dockClustersFake(t *testing.T) *fakeEngine {
	t.Helper()
	f := connectedFake(t, 200)
	f.ui.Context, f.ui.Connection = cShown, engine.Connection{State: engine.Connected}
	f.ui.Connections = []engine.ConnectedCluster{
		{Context: cShown, Connection: engine.Connection{State: engine.Connected}, Shown: true},
		{Context: cCold, Connection: engine.Connection{State: engine.Connected}},
	}
	web := model.ObjectRef{Context: cShown, Kind: kindPods, Namespace: "checkout", Name: "web-7d9f8-x2k4p"}
	f.ui.Dock = []engine.DockTab{
		{ID: "logs-1", Type: engine.DockLogs, Title: "Logs: web-7d9f8-x2k4p/app", Context: cShown, Ref: &web, Container: "app"},
		{ID: "audit-2", Type: engine.DockAudit, Title: "Audit Log"},
	}
	f.ui.ActiveDock = "logs-1"
	return f
}

// openHiddenLogs is an agent's pod.logs on staging-us-east while kind-cogpit is shown.
func openHiddenLogs(f *fakeEngine) {
	api := model.ObjectRef{Context: cCold, Kind: kindPods, Namespace: "orders", Name: "api-5c6b7-q8z2m"}
	f.update(func(u *engine.UIState) {
		u.Dock = append(u.Dock, engine.DockTab{ID: "logs-3", Type: engine.DockLogs, Title: "Logs: api-5c6b7-q8z2m/api",
			Context: cCold, Ref: &api, Container: "api"})
		u.ActiveDock = "logs-3"
	})
}

// showCluster shows a connected cluster, as cluster.connect of a connected context does.
func showCluster(f *fakeEngine, ctx string) {
	f.update(func(u *engine.UIState) {
		u.Context, u.Connection = ctx, engine.Connection{State: engine.Connected}
		for i := range u.Connections {
			u.Connections[i].Shown = u.Connections[i].Context == ctx
		}
	})
}

// stripTitles are the dock strip's tab titles.
func stripTitles(sh *shell) []string {
	var out []string
	for _, t := range sh.dock.tabs {
		out = append(out, t.Title)
	}
	return out
}

// The dock shows the shown cluster's tabs and the Audit Log; a logs tab an agent opens on the
// cluster behind it opens its stream at once but stays out of the strip; showing that cluster shows
// its tab, open, the other cluster's stream still running; showing the first one again brings its
// tab back without opening its stream anew.
func TestDockShowsTheShownClustersTabs(t *testing.T) {
	f := dockClustersFake(t)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the dock", func() bool { return len(sh.dock.contents) == 2 && sh.dock.open() })
	openHiddenLogs(f)
	q.until(t, "the hidden tab's stream", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.opened["logs-3"] == 1
	})
	if got := stripTitles(sh); len(got) != 2 || sh.dock.active != "logs-1" {
		t.Errorf("kind-cogpit's strip %q, active %q", got, sh.dock.active)
	}
	showCluster(f, cCold)
	q.until(t, "staging's dock", func() bool { return sh.ui.Context == cCold && sh.dock.active == "logs-3" })
	if got := stripTitles(sh); len(got) != 2 || got[0] != "Audit Log" || !sh.dock.open() {
		t.Errorf("staging's strip %q, open %v", got, sh.dock.open())
	}
	showCluster(f, cShown)
	q.until(t, "kind-cogpit's dock again", func() bool { return sh.dock.active == "logs-1" })
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range []string{"logs-1", "logs-3"} {
		if f.opened[id] != 1 || f.closed[id] != 0 {
			t.Errorf("%s opened %d times, closed %d times", id, f.opened[id], f.closed[id])
		}
	}
}

// The dock per cluster, dark and light: kind-cogpit shown with its logs tab (before), then
// staging-us-east shown with the logs an agent opened on it meanwhile (after).
func TestRenderDockPerClusterDark(t *testing.T) {
	renderDockPerCluster(t, ThemeDark, "dock_cluster_before_dark.png", "dock_cluster_after_dark.png")
}
func renderDockPerCluster(t *testing.T, mode ThemeMode, before, after string) {
	f := dockClustersFake(t)
	f.ui.Hotbar = nil
	withHotbar(f, cShown, cCold)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the dock", func() bool {
		return len(sh.dock.contents) == 2 && sh.dock.open() && len(tilesOf(sh.hot)) == 2 && sh.diag.total > 0
	})
	openHiddenLogs(f)
	logLines := func(id string) int {
		c := sh.dock.contents[id]
		if c == nil || c.logs == nil || c.logs.view == nil {
			return 0
		}
		return c.logs.view.Output().Len()
	}
	q.until(t, "both logs' lines, the hidden one's too", func() bool { return logLines("logs-1") == 1 && logLines("logs-3") == 1 })
	q.drain()
	savePNG(t, w, before)
	showCluster(f, cCold)
	q.until(t, "staging's dock", func() bool { return sh.dock.active == "logs-3" && sh.dock.open() })
	q.drain()
	w.Resize(fyne.NewSize(1440, 900))
	savePNG(t, w, after)
}
