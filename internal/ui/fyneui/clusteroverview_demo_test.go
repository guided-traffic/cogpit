package fyneui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
)

// renderClusterOverviewDemo runs the shell over the real engine and the demo cluster, as `cogpit
// --demo` does, with the polls shortened: the cluster Overview's charts gather the demo Metrics
// Server's node measurements summed for the workers (the demo has no Prometheus, so no history),
// the Pods chart counts the cache's pods as the churn moves them, and the Issues list what the
// demo's pods, nodes, workloads and warning events show. The polls are fast, so the series is
// seconds long: the charts are drawn over a window that shows its shape.
func renderClusterOverviewDemo(t *testing.T, mode ThemeMode, name string) {
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	conn := &engine.ClusterConnector{
		IncludeDemo: true,
		MetricsOptions: metrics.Options{
			ListInterval: 300 * time.Millisecond, ObjectInterval: 60 * time.Millisecond, TTL: 40 * time.Millisecond,
		},
	}
	conn.DemoOptions.Pods, conn.DemoOptions.Seed, conn.DemoOptions.EventsPerSecond = 1500, 11, 200
	conn.DemoOptions.MetricsResolution = 40 * time.Millisecond
	s := newTestEngine(engine.Options{Connector: conn})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(mode))
	w := test.NewTempWindow(t, nil)
	w.SetPadded(false)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{AutoConnect: engine.DemoContext, Theme: mode}, q.do)
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
				t.Fatalf("timed out waiting for %s", what)
			}
			q.drain()
			time.Sleep(5 * time.Millisecond)
		}
		q.drain()
	}
	until("the demo cluster", func() bool { return sh.ui.Connection.State == engine.Connected && len(sh.kinds) > 0 })
	s.UpdateUI(func(u *engine.UIState) { u.View, u.Sort = engine.ClusterOverview, s.ViewSort(engine.ClusterOverview) })
	p := sh.view.cluster
	until("the charts and the issues", func() bool {
		tbl := s.Table(engine.ClusterOverview)
		return tbl != nil && tbl.Synced && len(tbl.Rows) > 0 && p.data != nil && len(p.data.Charts) == 3 &&
			len(p.data.Charts[0].Samples) >= 40 && len(p.data.Charts[2].Samples) >= 3
	})
	m := *p.data
	cpu := m.Charts[0].Samples
	first, last := cpu[0].At, cpu[len(cpu)-1].At
	span := last.Sub(first)
	sh.now = func() time.Time { return last.Add(span / 20) }
	p.show(&m)
	for i, c := range p.cards {
		c.set(m.Charts[i], span*5/4, sh.now(), sh.timeFormat())
	}
	sh.view.refreshAges()
	t.Logf("%s: %d CPU points, %d pods points, %d issues, nodes %s, source %q", m.Nodes, len(cpu), len(m.Charts[2].Samples),
		len(s.Table(engine.ClusterOverview).Rows), m.Nodes, m.Source)
	if m.Status != metrics.StatusAvailable || m.Nodes != engine.NodesWorker || m.Charts[0].Allocatable <= 0 || *m.Charts[0].Requests <= 0 {
		t.Errorf("demo overview: status %s, nodes %s, cpu %+v", m.Status, m.Nodes, m.Charts[0])
	}
	savePNG(t, w, name)
}

func TestRenderClusterOverviewDemoDark(t *testing.T) {
	renderClusterOverviewDemo(t, ThemeDark, "cluster_overview_demo_dark.png")
}
