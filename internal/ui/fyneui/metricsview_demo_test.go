package fyneui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// TestRenderDemoMetrics runs the shell over the real engine and the demo cluster's synthetic
// Metrics Server, as `cogpit --demo` does, with the polls shortened: the Pods list fills its CPU and
// Memory columns, a pod's drawer gathers a series and draws it with the pod's requests and limits.
// The PNG goes to COGPIT_PNG_DIR when that is set (else it is only captured), never to testdata.
func TestRenderDemoMetrics(t *testing.T) {
	s, sh, w, until := demoShell(t)
	until("the Pods list with usage", func() bool {
		tbl := s.Table("pods")
		if tbl == nil || !tbl.Synced || len(tbl.Rows) == 0 || len(sh.kinds) == 0 {
			return false
		}
		cpu := -1
		for i, c := range tbl.Columns {
			if c.ID == model.ColumnCPU {
				cpu = i
			}
		}
		return cpu >= 0 && tbl.Rows[0].Cells[cpu].Text != ""
	})

	// A pod with requests and limits, so that the chart has its lines.
	var ref model.ObjectRef
	for _, r := range s.Table("pods").Rows {
		obj, err := s.Lookup(r.Ref)
		if err != nil {
			continue
		}
		for _, c := range model.PodResources(obj) {
			if c.Limits.CPUMilli > 0 && c.Requests.CPUMilli > 0 && c.Limits.MemoryBytes > 0 {
				ref = r.Ref
			}
		}
		if ref.Name != "" {
			break
		}
	}
	if ref.Name == "" {
		t.Fatal("no demo pod with requests and limits")
	}
	s.UpdateUI(func(u *engine.UIState) { u.Drawer = &ref; u.Selected = []model.ObjectRef{ref} })
	until("a series in the drawer", func() bool {
		p, ok := sh.drawer.kept[metricsKey].(*metricsPanel)
		return ok && p.data != nil && p.data.Status == metrics.StatusAvailable && len(p.data.Targets) > 0 && len(p.data.Targets[0].Samples) >= 30
	})
	p := sh.drawer.kept[metricsKey].(*metricsPanel)
	// The polls are shortened, so the series is seconds long: a window that shows its shape, with the
	// clock at the newest sample.
	samples := p.data.Targets[0].Samples
	first, last := samples[0].At, samples[len(samples)-1].At
	p.window = last.Sub(first) * 5 / 4
	sh.now = func() time.Time { return last.Add(last.Sub(first) / 20) }
	p.render()
	t.Logf("%s: %d samples", ref.Name, len(p.data.Targets[0].Samples))
	if len(p.plot.data.Lines) != 2 || p.plot.data.Lines[0].Value <= 0 || p.plot.data.Lines[1].Value <= 0 {
		t.Errorf("lines = %+v, want the pod's requests and limits", p.plot.data.Lines)
	}

	demoPNG(t, w, "demo_metrics.png")
}

// demoShell is the shell over the real engine and the demo cluster, as `cogpit --demo` runs, with
// the polls shortened, and a wait that drains the main queue.
func demoShell(t *testing.T) (*engine.Session, *shell, fyne.Window, func(what string, cond func() bool)) {
	t.Helper()
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	conn := &engine.ClusterConnector{
		IncludeDemo: true,
		MetricsOptions: metrics.Options{
			ListInterval: 300 * time.Millisecond, ObjectInterval: 60 * time.Millisecond, TTL: 40 * time.Millisecond,
		},
	}
	// The UI may not import the demo package, but it may size the demo cluster through its field.
	conn.DemoOptions.Pods, conn.DemoOptions.NoChurn, conn.DemoOptions.Seed = 1500, true, 11
	conn.DemoOptions.MetricsResolution = 40 * time.Millisecond
	s := newTestEngine(engine.Options{Connector: conn})
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
	})
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
	return s, sh, w, until
}

// demoPNG settles the window and captures it, written to COGPIT_PNG_DIR when that is set, never
// to testdata (lookPNG).
func demoPNG(t *testing.T, w fyne.Window, name string) {
	t.Helper()
	settle(w)
	lookPNG(t, w, name)
}
