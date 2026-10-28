package fyneui

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// demoSettingsShell runs the shell over the real engine and the demo cluster with short polls and
// cluster settings kept in a temporary folder, connected and showing the Pods list with usage.
func demoSettingsShell(t *testing.T) (*engine.Session, *shell, fyne.Window, func(string, func() bool)) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	conn := &engine.ClusterConnector{
		IncludeDemo: true,
		MetricsOptions: metrics.Options{
			ListInterval: 200 * time.Millisecond, ObjectInterval: 50 * time.Millisecond, TTL: 40 * time.Millisecond,
		},
	}
	conn.DemoOptions.Pods, conn.DemoOptions.NoChurn, conn.DemoOptions.Seed = 200, true, 5
	conn.DemoOptions.MetricsResolution = 40 * time.Millisecond
	s := newTestEngine(engine.Options{Connector: conn, ClusterSettings: engine.NewClusterSettingsStore(filepath.Join(t.TempDir(), "Cogpit"))})
	t.Cleanup(s.Close)
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
	t.Cleanup(func() {
		sh.stop()
		q.drain()
		s.SetConfirmer(nil)
	})
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			q.drain()
			time.Sleep(5 * time.Millisecond)
		}
		q.drain()
	}
	until("the Pods list with usage", func() bool {
		return sh.view.snap != nil && sh.view.snap.Synced && usageRow(sh.view.snap) >= 0
	})
	return s, sh, w, until
}

// usageRow is the first row of a snapshot with a CPU value; -1 for none.
func usageRow(snap *engine.TableSnapshot) int {
	ci := slices.IndexFunc(snap.Columns, func(c model.Column) bool { return c.ID == model.ColumnCPU })
	if ci < 0 {
		return -1
	}
	return slices.IndexFunc(snap.Rows, func(r model.Row) bool { return ci < len(r.Cells) && r.Cells[ci].Text != "" })
}

// shownColumn reports whether the table shows the column with the id.
func shownColumn(sh *shell, id string) bool {
	for _, i := range sh.view.cols {
		if sh.view.snap.Columns[i].ID == id {
			return true
		}
	}
	return false
}

// The Metrics section's Hide metrics setting, clicked: each check sets the engine's state at once,
// and the window follows it: the Pods list's CPU and Memory columns go, the pod's drawer loses its
// Metrics section, then its hidden variants; the Overview shows only the charts left. Hide all
// metrics and Reset set everything and nothing; the line under them says what is hidden.
func TestHideMetricsFromTheSettings(t *testing.T) {
	s, sh, _, until := demoSettingsShell(t)
	ref := sh.view.snap.Rows[usageRow(sh.view.snap)].Ref
	s.UpdateUI(func(u *engine.UIState) { u.Drawer = &ref; u.Selected = []model.ObjectRef{ref} })
	var panel *metricsPanel
	until("the drawer's charts", func() bool {
		p, ok := sh.drawer.kept[metricsKey].(*metricsPanel)
		panel = p
		return ok && p.data != nil && p.data.Status == metrics.StatusAvailable && len(p.data.Targets) > 1
	})
	if !shownColumn(sh, model.ColumnCPU) || !slices.Contains(findAll[*fyne.Container](sh.drawer.root), panel.root) {
		t.Fatal("before hiding: the CPU column or the Metrics section is missing")
	}

	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": engine.DemoContext, "section": "metrics"})
	until("the Metrics section", func() bool { return sh.settings.controls["hiddenPlaces"] != nil })
	places := sh.settings.controls["hiddenPlaces"].(*widget.CheckGroup)
	measures := sh.settings.controls["hiddenMeasures"].(*widget.CheckGroup)
	line := sh.settings.controls["hiddenMetricsLine"].(*textLine)
	if line.text != "All metrics are visible." || !slices.Equal(places.Options, []string{"Cluster", "Nodes", "Pods", "Containers", "Volumes"}) ||
		!slices.Equal(measures.Options, []string{"CPU", "Memory", "Network", "Filesystem"}) {
		t.Fatalf("line %q, places %q, measures %q", line.text, places.Options, measures.Options)
	}
	if !sh.settings.controls["resetMetrics"].(*textButton).Disabled() {
		t.Error("Reset is enabled with nothing hidden")
	}

	places.SetSelected([]string{"Pods"})
	until("pods hidden", func() bool {
		cs, _ := s.ClusterSettingsOf(engine.DemoContext)
		return cs.HiddenMetrics == engine.HidePods && line.text == "Hidden: Pods."
	})
	mustDispatch(t, s, "settings.close", struct{}{})
	until("the columns and the section gone", func() bool {
		return sh.view.snap != nil && !shownColumn(sh, model.ColumnCPU) && !shownColumn(sh, model.ColumnMemory) &&
			!slices.Contains(findAll[*fyne.Container](sh.drawer.root), panel.root)
	})
	if n := s.Backend().Metrics.Subscribed(); n != 0 {
		t.Errorf("%d scopes polled with the pods' metrics hidden", n)
	}

	// The CPU and the network hidden instead: the section is back without those two buttons, and
	// the chart shows Memory.
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": engine.DemoContext, "section": "metrics"})
	until("the Metrics section again", func() bool { return sh.settings.controls["hiddenPlaces"] != nil })
	places = sh.settings.controls["hiddenPlaces"].(*widget.CheckGroup)
	measures = sh.settings.controls["hiddenMeasures"].(*widget.CheckGroup)
	places.SetSelected(nil)
	measures.SetSelected([]string{"CPU", "Network"})
	until("cpu and network hidden", func() bool {
		cs, _ := s.ClusterSettingsOf(engine.DemoContext)
		return cs.HiddenMetrics == engine.HideCPU|engine.HideNetwork
	})
	mustDispatch(t, s, "settings.close", struct{}{})
	// The table follows its own snapshots: wait for the one without CPU too, not only the drawer
	// (one with the pods' metrics shown again came first on CI, 2026-10-13).
	until("the section with Memory and Filesystem", func() bool {
		p, ok := sh.drawer.kept[metricsKey].(*metricsPanel)
		return ok && slices.Contains(findAll[*fyne.Container](sh.drawer.root), p.root) && p.data != nil &&
			slices.Equal(p.data.Variants, []string{"memory", "filesystem"}) && p.variant == variantMemory &&
			shownColumn(sh, model.ColumnMemory) && !shownColumn(sh, model.ColumnCPU)
	})
	panel = sh.drawer.kept[metricsKey].(*metricsPanel)
	for id, b := range panel.variants {
		if want := id == variantMemory || id == variantFilesystem; b.Visible() != want {
			t.Errorf("the %s button shows: %v", id, b.Visible())
		}
	}
	if shownColumn(sh, model.ColumnCPU) || !shownColumn(sh, model.ColumnMemory) {
		t.Errorf("columns: cpu %v, memory %v", shownColumn(sh, model.ColumnCPU), shownColumn(sh, model.ColumnMemory))
	}

	// The Overview: CPU hidden leaves Memory and Pods.
	s.UpdateUI(func(u *engine.UIState) { u.Drawer, u.View = nil, engine.ClusterOverview })
	until("the Overview without CPU", func() bool {
		p := sh.view.cluster
		return p.data != nil && len(p.data.Charts) == 3 && p.data.Charts[0].Hidden && !p.cards[0].root.Visible() &&
			p.cards[1].root.Visible() && p.cards[2].root.Visible()
	})

	// Hide all metrics, then Reset, from the page's buttons.
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": engine.DemoContext, "section": "metrics"})
	until("the buttons", func() bool { return sh.settings.controls["hideAllMetrics"] != nil })
	test.Tap(sh.settings.controls["hideAllMetrics"].(*textButton))
	until("all hidden", func() bool {
		cs, _ := s.ClusterSettingsOf(engine.DemoContext)
		l := sh.settings.controls["hiddenMetricsLine"].(*textLine)
		return cs.HiddenMetrics == engine.HideAll && l.text == "All metrics are hidden." &&
			len(sh.settings.controls["hiddenPlaces"].(*widget.CheckGroup).Selected) == 5
	})
	if !sh.settings.controls["hideAllMetrics"].(*textButton).Disabled() {
		t.Error("Hide all metrics is enabled with everything hidden")
	}
	mustDispatch(t, s, "settings.close", struct{}{})
	until("the Overview without charts", func() bool {
		p := sh.view.cluster
		return p.data != nil && p.data.Status == engine.StatusHidden && !p.controls.Visible() &&
			!p.cards[0].root.Visible() && !p.cards[1].root.Visible() && !p.cards[2].root.Visible()
	})
	if n := s.Backend().Metrics.Subscribed(); n != 0 {
		t.Errorf("%d scopes polled with every metric hidden", n)
	}
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": engine.DemoContext, "section": "metrics"})
	until("the buttons again", func() bool { return sh.settings.controls["resetMetrics"] != nil })
	test.Tap(sh.settings.controls["resetMetrics"].(*textButton))
	until("all visible", func() bool {
		cs, _ := s.ClusterSettingsOf(engine.DemoContext)
		return cs.HiddenMetrics == 0 && sh.settings.controls["hiddenMetricsLine"].(*textLine).text == "All metrics are visible."
	})
	mustDispatch(t, s, "settings.close", struct{}{})
	until("the Overview's charts back", func() bool {
		p := sh.view.cluster
		return p.data != nil && p.controls.Visible() && p.cards[0].root.Visible() && s.Backend().Metrics.Subscribed() == 1
	})
}

// The Metrics section with hidden metrics, dark and light: the PNGs of the pull request
// (AGENTS.md rule 7).
func TestRenderClusterSettingsMetricsDark(t *testing.T) {
	renderClusterSettingsMetrics(t, ThemeDark, "cluster_settings_metrics_dark.png")
}

func renderClusterSettingsMetrics(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "name": "Dev cluster", "iconColor": "violet",
		"hiddenMetrics": []string{"containers", "network", "filesystem"},
		"integrations":  map[string]bool{"metrics-server": true, "prometheus": true}}) // its metrics chosen (D-82)
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "metrics"})
	q.until(t, "the Metrics section", func() bool {
		l, ok := sh.settings.controls["hiddenMetricsLine"].(*textLine)
		return sh.settings.visible() && sh.settings.view != nil && ok && l.text == "Hidden: Containers, Network, Filesystem."
	})
	q.drain()
	savePNG(t, w, name)
}
