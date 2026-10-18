package fyneui

import (
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The tests of the Metrics section and the CPU and Memory columns (G12, architecture §9). The
// numbers are canned: the fake engine answers ObjectMetrics, the table snapshots carry the usage
// cells the engine would have patched in.

// usageOf makes up a pod's or node's usage from its position in the list.
func usageOf(i int) model.Usage {
	return model.Usage{CPUMilli: int64(5 + (i*37)%900), MemoryBytes: int64(16+(i*53)%900) << 20}
}

// withUsageCells fills the CPU and Memory cells of a fixture view the way the engine's row cache
// does, for every row but each seventh (a pod without a measurement shows nothing).
func withUsageCells(t testing.TB, f *fakeEngine, key string) {
	t.Helper()
	p, ok := kinds.ByKey(key)
	if !ok || p.UsageCells == nil {
		t.Fatalf("%s has no usage cells", key)
	}
	objs := map[string]*unstructured.Unstructured{}
	switch key {
	case "pods":
		for _, o := range fixturePodObjects() {
			objs[o.GetName()] = o
		}
	case "nodes":
		for _, o := range fixtureNodeObjects() {
			objs[o.GetName()] = o
		}
	}
	snap := *f.tables[key]
	snap.Rows = slices.Clone(snap.Rows)
	cpu, mem := slices.IndexFunc(snap.Columns, func(c model.Column) bool { return c.ID == model.ColumnCPU }),
		slices.IndexFunc(snap.Columns, func(c model.Column) bool { return c.ID == model.ColumnMemory })
	if cpu < 0 || mem < 0 {
		t.Fatalf("%s has no CPU and Memory columns: %v", key, snap.Columns)
	}
	for i := range snap.Rows {
		if i%7 == 6 {
			continue
		}
		c, m := p.UsageCells(objs[snap.Rows[i].Ref.Name], usageOf(i))
		cells := slices.Clone(snap.Rows[i].Cells)
		cells[cpu], cells[mem] = c, m
		snap.Rows[i].Cells = cells
	}
	f.tables[key] = &snap
}

func TestPodsListShowsCPUAndMemory(t *testing.T) {
	f := kindsFake(t, "pods")
	withUsageCells(t, f, "pods")
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the pods view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "pods" && sh.diag.total > 0 })
	settle(w)

	var ids []string
	for _, c := range sh.view.snap.Columns[:5] {
		ids = append(ids, c.ID)
	}
	if want := []string{"name", "warning", "namespace", "cpu", "memory"}; !slices.Equal(ids, want) {
		t.Fatalf("first columns = %v, want %v (Lens: Name, warning, Namespace, CPU, Memory)", ids, want)
	}
	cpuCol, memCol := firstDataColumn+3, firstDataColumn+4 // table columns: the checkboxes first
	shown := 0
	for r := range min(len(sh.view.snap.Rows), 12) { // the table draws the rows on screen only
		c := sh.view.cellAt(t, r, cpuCol)
		m := sh.view.cellAt(t, r, memCol)
		if r%7 == 6 {
			if c.full != "" || m.full != "" {
				t.Errorf("row %d has no measurement but shows %q, %q", r, c.full, m.full)
			}
			continue
		}
		if !strings.Contains(c.full, ".") || !strings.HasSuffix(m.full, "iB") {
			t.Errorf("row %d shows cpu %q memory %q, want cores and binary units", r, c.full, m.full)
		}
		shown++
	}
	if shown == 0 {
		t.Fatal("no rows on screen")
	}
}

func TestNodesListShowsCPUAndMemoryAsShares(t *testing.T) {
	f := kindsFake(t, "nodes")
	withUsageCells(t, f, "nodes")
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "the nodes view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "nodes" && sh.diag.total > 0 })
	settle(w)
	if got := sh.view.snap.Columns[2].ID + "," + sh.view.snap.Columns[3].ID; got != "cpu,memory" {
		t.Fatalf("columns after the warning icon = %s, want cpu,memory (Lens)", got)
	}
	if c := sh.view.cellAt(t, 0, firstDataColumn+2); !strings.HasSuffix(c.full, "%") {
		t.Errorf("node cpu = %q, want a share of allocatable", c.full)
	}
}

// metricsSeries is an hour of a pod's CPU or memory at the Metrics Server's resolution, ending at
// the fixture's clock.
func metricsSeries(n int, f func(i int) model.Usage) []model.UsageSample {
	out := make([]model.UsageSample, n)
	for i := range out {
		out[i] = model.UsageSample{At: fixedNow.Add(-time.Duration(n-1-i) * 15 * time.Second), Usage: f(i)}
	}
	return out
}

func podMetricsFixture(ref model.ObjectRef) *engine.ObjectMetrics {
	n := 240
	pod := metricsSeries(n, func(i int) model.Usage {
		wave := 0.5 + 0.5*math.Sin(float64(i)/14)
		spike := 0.0
		if i > 150 && i < 165 {
			spike = 0.35
		}
		return model.Usage{
			CPUMilli:    int64(110 + 120*wave + 400*spike),
			MemoryBytes: int64(150+float64(i)*0.45+40*wave) << 20,
		}
	})
	app := metricsSeries(n, func(i int) model.Usage {
		u := pod[i].Usage
		return model.Usage{CPUMilli: u.CPUMilli * 8 / 10, MemoryBytes: u.MemoryBytes * 9 / 10}
	})
	cur := func(s []model.UsageSample) *model.Usage { u := s[len(s)-1].Usage; return &u }
	return &engine.ObjectMetrics{
		Ref: ref, Status: metrics.StatusAvailable, Source: "Kubernetes Metrics Server",
		Targets: []engine.MetricsTarget{
			{
				Current: cur(pod), At: fixedNow, Samples: pod,
				Requests: model.Usage{CPUMilli: 250, MemoryBytes: 192 << 20}, Limits: model.Usage{CPUMilli: 500, MemoryBytes: 384 << 20},
			},
			{
				Name: "app", Current: cur(app), At: fixedNow, Samples: app,
				Requests: model.Usage{CPUMilli: 200, MemoryBytes: 128 << 20}, Limits: model.Usage{CPUMilli: 400, MemoryBytes: 256 << 20},
			},
		},
	}
}

// metricsDrawer opens the rich fixture pod's drawer with canned metrics.
func metricsDrawer(t *testing.T, mode ThemeMode, mutate func(*engine.ObjectMetrics)) (*shell, *fakeEngine, *mainQueue) {
	t.Helper()
	return metricsDrawerFor(t, richPod(t), mode, mutate)
}

// twoContainerPod is a fixture pod with the containers app and envoy.
func twoContainerPod(t testing.TB) podSpec {
	t.Helper()
	for _, p := range fixturePodSpecs() {
		if slices.Equal(p.containers, []string{"app", "envoy"}) && p.state == podOK {
			return p
		}
	}
	t.Fatal("no fixture pod with the containers app and envoy")
	return podSpec{}
}

// withEnvoy adds the numbers of a second container, envoy, with other requests and limits.
func withEnvoy(m *engine.ObjectMetrics) {
	app := m.Targets[1]
	samples := metricsSeries(len(app.Samples), func(i int) model.Usage {
		u := app.Samples[i].Usage
		return model.Usage{CPUMilli: u.CPUMilli / 4, MemoryBytes: u.MemoryBytes / 3}
	})
	cur := samples[len(samples)-1].Usage
	m.Targets = append(m.Targets, engine.MetricsTarget{
		Name: "envoy", Current: &cur, At: fixedNow, Samples: samples,
		Requests: model.Usage{CPUMilli: 50, MemoryBytes: 64 << 20}, Limits: model.Usage{CPUMilli: 100, MemoryBytes: 96 << 20},
	})
}

// metricsDrawerFor opens a fixture pod's drawer with canned metrics.
func metricsDrawerFor(t *testing.T, p podSpec, mode ThemeMode, mutate func(*engine.ObjectMetrics)) (*shell, *fakeEngine, *mainQueue) {
	t.Helper()
	f := kindsFake(t, "pods")
	withUsageCells(t, f, "pods")
	ref := model.ObjectRef{Context: "demo", Kind: "pods", Namespace: p.ns, Name: p.name}
	m := podMetricsFixture(ref)
	if mutate != nil {
		mutate(m)
	}
	f.metrics = map[string]*engine.ObjectMetrics{ref.String(): m}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	withRangeCommand(f)
	withVariantCommand(f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the metrics", func() bool {
		pm, ok := sh.drawer.kept[metricsKey].(*metricsPanel)
		return ok && pm.data != nil && sh.drawer.last != nil && sh.diag.total > 0
	})
	settle(w)
	return sh, f, q
}

func TestPodDrawerMetricsSection(t *testing.T) {
	sh, _, _ := metricsDrawer(t, ThemeDark, nil)
	texts := drawerTexts(sh)
	if texts[0] != "Metrics" {
		t.Fatalf("the Metrics section must come first (Lens), got %q", texts[:3])
	}
	if i, j := slices.Index(texts, "Displaying metrics from Kubernetes Metrics Server"), slices.Index(texts, "Properties"); i < 0 || j < i {
		t.Errorf("the source line must be in the Metrics section, before Properties: %q", texts[:6])
	}
	p := sh.drawer.kept[metricsKey].(*metricsPanel)
	if p.rangeSel.Selected != "1h" || p.window != time.Hour {
		t.Errorf("range = %q (%v), want Lens's default 1h", p.rangeSel.Selected, p.window)
	}
	// The variants: CPU selected, Memory enabled, Network and Filesystem disabled with the reason.
	if p.variants[variantCPU].Importance != widget.HighImportance || p.variants[variantMemory].Disabled() {
		t.Error("CPU must be selected and Memory enabled")
	}
	for _, id := range []string{variantNetwork, variantFilesystem} {
		b := p.variants[id]
		if !b.Disabled() || !strings.Contains(b.tip, "needs Prometheus") {
			t.Errorf("%s: disabled %v, tip %q; want disabled with the reason", id, b.Disabled(), b.tip)
		}
	}
	// The chart: the usage series, and requests and limits as lines (CPU in cores).
	d := p.plot.data
	if len(d.Series) != 1 || d.Series[0].Name != "CPU usage" || len(d.Series[0].Points) != 240 {
		t.Fatalf("series = %+v", d.Series)
	}
	if len(d.Lines) != 2 || d.Lines[0].Name != "Requests" || d.Lines[0].Value != 0.25 || d.Lines[1].Name != "Limits" || d.Lines[1].Value != 0.5 {
		t.Errorf("lines = %+v, want requests 0.25 and limits 0.5 cores", d.Lines)
	}
	if !d.To.Equal(fixedNow) || d.To.Sub(d.From) != time.Hour {
		t.Errorf("axis %v to %v", d.From, d.To)
	}
	// Each container has a chart of its own under its section title.
	if len(p.perBox) != 1 || p.perBox["app"] == nil || len(p.perBox["app"].plot.data.Series) != 1 {
		t.Fatalf("container charts = %v", p.perBox)
	}
	if !p.perBox["app"].plot.data.Compact || p.perBox["app"].plot.data.Lines[0].Value != 0.2 {
		t.Errorf("app's chart: %+v", p.perBox["app"].plot.data)
	}
}

func TestMetricsVariantsRangeAndEvents(t *testing.T) {
	sh, _, _ := metricsDrawer(t, ThemeDark, nil)
	p := sh.drawer.kept[metricsKey].(*metricsPanel)

	// Memory: bytes, with the memory requests and limits.
	p.variants[variantMemory].OnTapped()
	d := p.plot.data
	if d.Series[0].Name != "Memory usage" || d.Lines[0].Value != float64(192<<20) || d.Lines[1].Value != float64(384<<20) {
		t.Errorf("memory chart: %+v", d)
	}
	if got := d.Format(float64(192 << 20)); got != "192.0 MiB" {
		t.Errorf("memory axis label = %q", got)
	}
	if p.variants[variantMemory].Importance != widget.HighImportance || p.variants[variantCPU].Importance == widget.HighImportance {
		t.Error("Memory must be the selected variant")
	}
	p.variants[variantCPU].OnTapped()

	// A shorter range narrows the axis.
	p.rangeSel.SetSelected("15m")
	if p.plot.data.To.Sub(p.plot.data.From) != 15*time.Minute {
		t.Errorf("axis = %v", p.plot.data.To.Sub(p.plot.data.From))
	}
	p.rangeSel.SetSelected("1h")

	// The flag toggles the events as markers; the fixture's events are minutes old.
	if len(p.plot.data.Markers) != 0 {
		t.Error("events are marked before the flag is pressed")
	}
	p.flag.OnTapped()
	var inRange int
	for _, e := range sh.drawer.last.Events {
		if e.LastSeen > 0 && time.Unix(e.LastSeen, 0).After(fixedNow.Add(-time.Hour)) {
			inRange++
		}
	}
	if got := len(p.plot.data.Markers); got != inRange || inRange == 0 {
		t.Errorf("%d markers for %d events (%d in the last hour)", got, len(sh.drawer.last.Events), inRange)
	}
	p.flag.OnTapped()
	if len(p.plot.data.Markers) != 0 {
		t.Error("the markers stay after the flag is pressed again")
	}
}

// Without a metrics-server the section says so, in place of the chart, and no button invites a click.
func TestMetricsSectionStates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status metrics.Status
		msg    string
		want   string
	}{
		{"not served", metrics.StatusNotServed, "the cluster serves no metrics.k8s.io API (no metrics-server installed)", "No metrics: the cluster serves no metrics.k8s.io API (no metrics-server installed)"},
		{"forbidden", metrics.StatusForbidden, "the current user may not read the Metrics API", "Metrics are not available: the current user may not read the Metrics API"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sh, _, _ := metricsDrawer(t, ThemeDark, func(m *engine.ObjectMetrics) {
				m.Status, m.Message, m.Source, m.Targets = tc.status, tc.msg, "", nil
			})
			p := sh.drawer.kept[metricsKey].(*metricsPanel)
			if !slices.Contains(drawerTexts(sh), tc.want) {
				t.Errorf("texts = %q, want %q", drawerTexts(sh)[:4], tc.want)
			}
			if p.plot.Visible() || p.controls.Visible() {
				t.Error("the chart and its controls show without metrics")
			}
			// No provider is a state, said quietly; a refusal is a problem, in the warning colour.
			if quiet := tc.status == metrics.StatusNotServed; (p.caption.color == colorMuted) != quiet {
				t.Errorf("caption colour %v for %s", p.caption.color, tc.status)
			}
			if len(p.perBox) != 0 {
				t.Error("a container chart without metrics")
			}
		})
	}
}

// A new measurement redraws the chart and leaves the rest of the drawer alone.
func TestMetricsEventUpdatesInPlace(t *testing.T) {
	sh, f, q := metricsDrawer(t, ThemeDark, nil)
	p := sh.drawer.kept[metricsKey].(*metricsPanel)
	before := len(sh.drawer.details.Objects)
	first := sh.drawer.details.Objects[0]

	f.mu.Lock()
	m := f.metrics[p.ref.String()]
	m.Targets[0].Samples = m.Targets[0].Samples[:120]
	f.mu.Unlock()
	f.emit(engine.Event{Type: engine.EventMetrics, View: "pods"})
	q.until(t, "the chart to follow", func() bool { return len(p.plot.data.Series[0].Points) == 120 })
	if len(sh.drawer.details.Objects) != before || sh.drawer.details.Objects[0] != first {
		t.Error("a metrics event rebuilt the drawer")
	}
}

func TestNodeDrawerMetricsLines(t *testing.T) {
	f := kindsFake(t, "nodes")
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: fixtureNodeSpecs()[0].name}
	f.metrics = map[string]*engine.ObjectMetrics{ref.String(): {
		Ref: ref, Status: metrics.StatusAvailable, Source: "Kubernetes Metrics Server",
		Targets: []engine.MetricsTarget{{
			Current: &model.Usage{CPUMilli: 1200, MemoryBytes: 3 << 30}, At: fixedNow,
			Samples:     metricsSeries(40, func(i int) model.Usage { return model.Usage{CPUMilli: int64(900 + 8*i), MemoryBytes: 3 << 30} }),
			Allocatable: model.Usage{CPUMilli: 3920, MemoryBytes: 7 << 30}, Capacity: model.Usage{CPUMilli: 4000, MemoryBytes: 8 << 30},
		}},
	}}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the node metrics", func() bool {
		p, ok := sh.drawer.kept[metricsKey].(*metricsPanel)
		return ok && p.data != nil
	})
	p := sh.drawer.kept[metricsKey].(*metricsPanel)
	names := []string{}
	for _, l := range p.plot.data.Lines {
		names = append(names, l.Name)
	}
	if !slices.Equal(names, []string{"Allocatable capacity", "Capacity"}) {
		t.Errorf("node lines = %v (Lens: allocatable capacity and capacity)", names)
	}
	if len(p.perBox) != 0 {
		t.Error("a node has no container charts")
	}
}

// The chart draws a point per sample, a segment between them, the dashed lines and the markers, in
// theme colours, and keeps to the width it is given.
func TestChartDrawsWhatItIsGiven(t *testing.T) {
	sh, _, _ := metricsDrawer(t, ThemeDark, nil)
	p := sh.drawer.kept[metricsKey].(*metricsPanel)
	p.flag.OnTapped()
	var lines, texts int
	colors := map[color.Color]int{}
	for _, o := range test.WidgetRenderer(p.plot).Objects() {
		switch x := o.(type) {
		case *canvas.Line:
			lines++
			colors[x.StrokeColor]++
		case *canvas.Text:
			texts++
		}
	}
	// The usage line, one path of 240 points (chartpath.go); the requests and limits as dashes;
	// grid; marker.
	for _, l := range p.plot.rend.line.paths {
		colors[l.color]++
	}
	if paths := p.plot.rend.line.paths; len(paths) != 1 || len(paths[0].pts) != 240 {
		t.Errorf("usage paths %d, want one of 240 points", len(paths))
	}
	if lines < 10 || texts < 5 {
		t.Errorf("%d lines and %d texts drawn", lines, texts)
	}
	if len(colors) < 4 {
		t.Errorf("only %d colours used: usage, requests, limits and events must differ", len(colors))
	}
	if w := p.plot.MinSize().Width; w > chartMinWidth {
		t.Errorf("the chart asks for %v of width; the drawer's width is the user's", w)
	}
}

func TestChartAxisAndNiceSteps(t *testing.T) {
	for _, tc := range []struct{ top, want float64 }{{1, 0.5}, {0.5, 0.2}, {7, 2.5}, {1200, 500}, {3, 1}} {
		if got := niceStep(tc.top, 3); got != tc.want && !(tc.top == 0.5 && got == 0.2) {
			t.Errorf("niceStep(%v, 3) = %v, want %v", tc.top, got, tc.want)
		}
	}
	axis, _ := variantFormats(variantMemory, "")
	for _, tc := range []struct {
		v    float64
		want string
	}{{0, "0 B"}, {512, "512 B"}, {1024, "1.0 KiB"}, {192 << 20, "192.0 MiB"}, {1.5 * (1 << 30), "1.5 GiB"}} {
		if got := axis(tc.v); got != tc.want {
			t.Errorf("memory axis(%v) = %q, want %q", tc.v, got, tc.want)
		}
	}
	if got := formatCores(0.30000000000000004); got != "0.3" {
		t.Errorf("formatCores = %q", got)
	}
}

// TestRenderMetricsDrawer draws the pod drawer with its Metrics section for a look: to
// COGPIT_PNG_DIR or a temporary directory, never to testdata.
func TestRenderMetricsDrawer(t *testing.T) {
	for _, c := range []struct {
		mode   ThemeMode
		name   string
		width  float32 // the drawer's width; 0 keeps the default
		scroll float32 // how far the drawer is scrolled: to the containers' charts
	}{
		{ThemeDark, "pod_metrics_dark.png", 0, 0}, {ThemeLight, "pod_metrics_light.png", 0, 0},
		{ThemeDark, "pod_metrics_containers_dark.png", 0, 400}, {ThemeLight, "pod_metrics_containers_light.png", 0, 400},
		{ThemeDark, "pod_metrics_narrow_dark.png", minDrawerWidth, 0}, {ThemeLight, "pod_metrics_narrow_containers_light.png", minDrawerWidth, 600},
	} {
		c := c
		t.Run(c.name, func(t *testing.T) {
			sh, _, q := metricsDrawerFor(t, twoContainerPod(t), c.mode, withEnvoy)
			if c.width > 0 {
				sh.setDrawerWidth(c.width)
				q.until(t, "the drawer to take the width", func() bool { return sh.drawer.width == c.width })
				settle(sh.win)
			}
			p := sh.drawer.kept[metricsKey].(*metricsPanel)
			p.flag.OnTapped() // events on, as the capture shows them
			p.perBox["envoy"].variants[variantMemory].OnTapped()
			sh.drawer.scroll.Offset.Y = c.scroll
			sh.drawer.scroll.Refresh()
			settle(sh.win)
			dir := os.Getenv("COGPIT_PNG_DIR")
			if dir == "" {
				dir = t.TempDir()
			}
			f, err := os.Create(filepath.Join(dir, c.name))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := png.Encode(f, sh.win.Canvas().Capture()); err != nil {
				t.Fatal(err)
			}
			t.Log("rendered", f.Name())
		})
	}
}
