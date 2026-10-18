package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The drawer's charts with Prometheus as the source (G12, architecture §9): the Network and
// Filesystem variants, the ranges beyond an hour as engine state (ui.setMetricsRange), the hover
// over the new series. The numbers are canned, as the engine reports them from range queries.

const promSource = "Prometheus (monitoring/prometheus-server)"

// withPrometheus turns the canned Metrics Server numbers into Prometheus's: the capabilities, the
// source and its providers, the pod's network (the container carries it as the pod's) and each target's
// filesystem usage, at the same times as the CPU and memory samples.
func withPrometheus(m *engine.ObjectMetrics) {
	m.Source = promSource
	m.Providers = bothAnswering() // the source row (D-88): Prometheus and the Metrics Server answering
	m.Capabilities = metrics.Capabilities{History: true, Network: true, Filesystem: true}
	m.FilesystemSource = "usage"
	var net []model.NetworkSample
	for i, sm := range m.Targets[0].Samples {
		wave := 0.5 + 0.5*math.Sin(float64(i)/9)
		net = append(net, model.NetworkSample{At: sm.At, ReceiveBytesPerSec: 24<<10 + 40<<10*wave, TransmitBytesPerSec: 6<<10 + 18<<10*(1-wave)})
	}
	for ti := range m.Targets {
		t := &m.Targets[ti]
		t.Network, t.NetworkOfPod = net, t.Name != ""
		t.Filesystem = nil
		for i, sm := range t.Samples {
			t.Filesystem = append(t.Filesystem, model.FilesystemSample{At: sm.At, UsageBytes: float64(12<<20 + i*(16<<10)/(ti+1))})
		}
		t.Window, t.Range = time.Hour, "1h"
	}
}

// withFilesystemIO makes the filesystem numbers read and write rates, as a Prometheus without the
// usage metric gives them.
func withFilesystemIO(m *engine.ObjectMetrics) {
	m.FilesystemSource = "io"
	for ti := range m.Targets {
		t := &m.Targets[ti]
		for i := range t.Filesystem {
			t.Filesystem[i] = model.FilesystemSample{At: t.Filesystem[i].At, ReadBytesPerSec: float64(2<<10 + (i%17)*512), WriteBytesPerSec: float64(150<<10 + (i%11)*(20<<10))}
		}
	}
}

// withRangeCommand answers ui.setMetricsRange as the engine does: the chart's range changes in
// what ObjectMetrics returns, and EventMetrics follows; a drawer that shows another object refuses.
func withRangeCommand(f *fakeEngine) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers["ui.setMetricsRange"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p struct {
			Ref       model.ObjectRef `json:"ref"`
			Container string          `json:"container"`
			Range     string          `json:"range"`
		}
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		d, err := engine.ParseChartRange(p.Range)
		if err != nil {
			return nil, err
		}
		if d == 0 {
			d = time.Hour
		}
		f.mu.Lock()
		old := f.metrics[p.Ref.String()]
		if old == nil || f.ui.Drawer == nil || !f.ui.Drawer.Same(p.Ref) {
			f.mu.Unlock()
			return nil, engine.ErrDrawerNotOpen
		}
		m := *old // a copy: the panel holds the old one
		m.Targets = slices.Clone(old.Targets)
		for i := range m.Targets {
			if m.Targets[i].Name == p.Container {
				m.Targets[i].Window, m.Targets[i].Range = d, engine.FormatChartRange(d)
			}
		}
		f.metrics[p.Ref.String()] = &m
		f.mu.Unlock()
		f.emit(engine.Event{Type: engine.EventMetrics, View: p.Ref.Kind})
		return nil, nil
	}
}

// promDrawer opens a two-container pod's drawer with Prometheus's numbers.
func promDrawer(t *testing.T, mode ThemeMode, more ...func(*engine.ObjectMetrics)) (*shell, *fakeEngine, *mainQueue, *metricsPanel) {
	t.Helper()
	sh, f, q := metricsDrawerFor(t, twoContainerPod(t), mode, func(m *engine.ObjectMetrics) {
		withEnvoy(m)
		withPrometheus(m)
		for _, fn := range more {
			fn(m)
		}
	})
	return sh, f, q, sh.drawer.kept[metricsKey].(*metricsPanel)
}

func TestPrometheusSourceAndVariants(t *testing.T) {
	sh, _, _, p := promDrawer(t, ThemeDark)
	texts := drawerTexts(sh)
	if !slices.ContainsFunc(texts, func(s string) bool { return strings.HasPrefix(s, "Prometheus ● ") }) {
		t.Errorf("Prometheus missing on the source row: %q", texts[:8])
	}
	for _, s := range texts {
		if strings.Contains(s, "keeps no history") {
			t.Errorf("Prometheus keeps history, but the drawer says %q", s)
		}
	}
	for name, c := range map[string]*chartCtl{"pod": p.chartCtl, "app": p.perBox["app"], "envoy": p.perBox["envoy"]} {
		for _, v := range metricsVariants {
			b := c.variants[v.id]
			if b.Disabled() || b.tip != v.label || b.Text != "" || b.Icon == nil {
				t.Errorf("%s/%s: disabled %v, tip %q, text %q: want an enabled icon with its name as the tooltip", name, v.id, b.Disabled(), b.tip, b.Text)
			}
		}
	}

	// Network on the pod's chart: receive and transmit in bytes per second, no requests or limits.
	p.variants[variantNetwork].OnTapped()
	d := p.plot.data
	if len(d.Series) != 2 || d.Series[0].Name != "Receive" || d.Series[1].Name != "Transmit" || len(d.Lines) != 0 {
		t.Fatalf("network chart: series %+v lines %+v", d.Series, d.Lines)
	}
	if d.Series[0].Color != colorAccent || d.Series[1].Color != colorChartSecond || len(d.Series[0].Points) != 240 {
		t.Errorf("network series colours %s, %s, %d points", d.Series[0].Color, d.Series[1].Color, len(d.Series[0].Points))
	}
	if got := d.Format(48 << 10); got != "48.0 KiB/s" {
		t.Errorf("network axis label = %q", got)
	}
	if p.variants[variantNetwork].Importance != widget.HighImportance || p.variants[variantCPU].Importance == widget.HighImportance {
		t.Error("Network must show as the selected variant")
	}
	if p.hint.Visible() {
		t.Errorf("the pod's own network needs no hint: %q", p.hint.text)
	}

	// A container's network is the pod's, and its chart says so; with two series it has a legend.
	app := p.perBox["app"]
	app.variants[variantNetwork].OnTapped()
	if !app.hint.Visible() || app.hint.text != networkPerPod {
		t.Errorf("app's network hint: visible %v, %q", app.hint.Visible(), app.hint.text)
	}
	if !app.plot.data.legend() || app.plot.data.Series[1].Name != "Transmit" {
		t.Error("a compact chart with two series needs its legend")
	}
	app.variants[variantCPU].OnTapped()
	if app.hint.Visible() || app.plot.data.legend() {
		t.Error("the hint and the legend stay after leaving Network")
	}

	// Filesystem: the bytes in use.
	p.variants[variantFilesystem].OnTapped()
	d = p.plot.data
	if len(d.Series) != 1 || d.Series[0].Name != "Usage" || len(d.Lines) != 0 || d.Format(12<<20) != "12.0 MiB" {
		t.Errorf("filesystem chart: %+v", d.Series)
	}
}

func TestPrometheusFilesystemIO(t *testing.T) {
	_, _, _, p := promDrawer(t, ThemeDark, withFilesystemIO)
	p.variants[variantFilesystem].OnTapped()
	d := p.plot.data
	if len(d.Series) != 2 || d.Series[0].Name != "Read" || d.Series[1].Name != "Write" || d.Series[1].Color != colorChartSecond {
		t.Fatalf("filesystem io chart: %+v", d.Series)
	}
	if got := d.Detail(150 << 10); got != "150.0 KiB/s" {
		t.Errorf("filesystem io hover value = %q", got)
	}
}

// The Metrics Server has neither variant: the buttons stay disabled with the reason, and its ranges
// end at an hour; a provider change takes a chart that showed Network back to CPU.
func TestVariantsFollowTheProvider(t *testing.T) {
	sh, f, q, p := promDrawer(t, ThemeDark)
	if !slices.Contains(p.rangeSel.Options, "7d") || !slices.Contains(p.rangeSel.Options, "30d") {
		t.Errorf("Prometheus's ranges = %v, want up to 30d", p.rangeSel.Options)
	}
	p.variants[variantNetwork].OnTapped()

	f.mu.Lock()
	m := podMetricsFixture(p.ref)
	withEnvoy(m)
	f.metrics[p.ref.String()] = m
	f.mu.Unlock()
	f.emit(engine.Event{Type: engine.EventMetrics, View: "pods"})
	q.until(t, "the Metrics Server's numbers", func() bool { return p.data != nil && p.data.Source == "Kubernetes Metrics Server" })
	settle(sh.win)
	if p.variant != variantCPU || p.plot.data.Series[0].Name != "CPU usage" {
		t.Errorf("variant %q after the provider lost Network", p.variant)
	}
	for _, id := range []string{variantNetwork, variantFilesystem} {
		if b := p.variants[id]; !b.Disabled() || !strings.HasSuffix(b.tip, needsPrometheus) {
			t.Errorf("%s: disabled %v tip %q", id, b.Disabled(), b.tip)
		}
	}
	if want := []string{"5m", "15m", "30m", "1h"}; !slices.Equal(p.rangeSel.Options, want) {
		t.Errorf("Metrics Server ranges = %v, want %v", p.rangeSel.Options, want)
	}
}

// TestMetricsRangeIsEngineState: the range control asks the engine (ui.setMetricsRange) and
// follows what the engine reports, so an agent's change shows in the window too.
func TestMetricsRangeIsEngineState(t *testing.T) {
	sh, f, q, p := promDrawer(t, ThemeDark)
	p.rangeSel.SetSelected("7d")
	if p.plot.data.To.Sub(p.plot.data.From) != 7*24*time.Hour {
		t.Errorf("axis = %v, want 7d at once", p.plot.data.To.Sub(p.plot.data.From))
	}
	q.until(t, "the engine's answer", func() bool {
		return len(f.callsOf("ui.setMetricsRange")) == 1 && p.data.Targets[0].Range == "7d"
	})
	call := f.callsOf("ui.setMetricsRange")[0]
	if s := string(call.Params); !strings.Contains(s, `"range":"7d"`) || strings.Contains(s, `"container"`) || !strings.Contains(s, p.ref.Name) || call.Origin != engine.OriginGUI {
		t.Errorf("dispatched %s from %s", s, call.Origin)
	}
	if p.window != 7*24*time.Hour || p.rangeSel.Selected != "7d" || p.perBox["app"].window != time.Hour {
		t.Errorf("pod %v (%q), app %v", p.window, p.rangeSel.Selected, p.perBox["app"].window)
	}
	if got := p.plot.data.timeLabel(p.plot.data.From); got != "10-04" { // the day without the year, ISO 8601 by default (D-31)
		t.Errorf("a week's axis starts with %q, want the date", got)
	}

	// An agent sets the envoy chart to 6h: the window follows.
	_, err := f.Dispatch(context.Background(), engine.Invocation{
		ID: "ui.setMetricsRange", Origin: engine.OriginExternalAgent,
		Params: mustJSON(t, map[string]any{"ref": p.ref, "container": "envoy", "range": "6h"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	envoy := p.perBox["envoy"]
	q.until(t, "the window to follow the agent", func() bool { return envoy.window == 6*time.Hour })
	if envoy.rangeBtn.Text != "6h" || envoy.plot.data.To.Sub(envoy.plot.data.From) != 6*time.Hour {
		t.Errorf("envoy shows %q over %v", envoy.rangeBtn.Text, envoy.plot.data.To.Sub(envoy.plot.data.From))
	}
	if n := len(f.callsOf("ui.setMetricsRange")); n != 2 {
		t.Errorf("following the agent dispatched again: %d calls", n)
	}

	// A container's compact chart asks the engine for its own chart.
	envoy.setRange("1d")
	q.until(t, "envoy's range", func() bool { return len(f.callsOf("ui.setMetricsRange")) == 3 })
	if s := string(f.callsOf("ui.setMetricsRange")[2].Params); !strings.Contains(s, `"container":"envoy"`) || !strings.Contains(s, `"range":"1d"`) {
		t.Errorf("envoy dispatched %s", s)
	}
	q.until(t, "envoy at a day", func() bool { return p.data.Targets[2].Range == "1d" && envoy.rangeBtn.Text == "1d" })

	// A drawer that shows another object refuses quietly; the chart follows the engine again.
	toasts := len(sh.toasts.Objects)
	refuse := func(err error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.handlers["ui.setMetricsRange"] = func(context.Context, engine.Invocation) (any, error) { return nil, err }
	}
	refuse(engine.ErrDrawerNotOpen)
	p.rangeSel.SetSelected("12h")
	q.until(t, "the refusal", func() bool { return len(f.callsOf("ui.setMetricsRange")) == 4 && p.window == 7*24*time.Hour })
	if len(sh.toasts.Objects) != toasts || p.rangeSel.Selected != "7d" {
		t.Errorf("a refusal for a closed drawer: %d toasts, select %q", len(sh.toasts.Objects)-toasts, p.rangeSel.Selected)
	}
	// Any other refusal shows.
	refuse(errors.New("no such chart"))
	p.rangeSel.SetSelected("3h")
	q.until(t, "the toast", func() bool { return len(sh.toasts.Objects) > toasts })
	q.until(t, "the range back", func() bool { return p.rangeSel.Selected == "7d" })
}

// Hovering the new series names them and gives their values with units.
func TestHoverNetworkAndFilesystem(t *testing.T) {
	middle := func(c *chart) string {
		t.Helper()
		g := c.geom
		c.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(g.left+g.w/2, g.top+g.h/2)}})
		if c.Hover() == nil {
			t.Fatal("no hover")
		}
		return c.Hover().Text()
	}
	_, _, _, p := promDrawer(t, ThemeDark)
	p.variants[variantNetwork].OnTapped()
	text := middle(p.plot)
	for _, s := range []string{"Receive: ", "Transmit: ", "KiB/s"} {
		if !strings.Contains(text, s) {
			t.Errorf("network panel lacks %q:\n%s", s, text)
		}
	}
	if strings.Contains(text, "Requests") || strings.Contains(text, "Limits") {
		t.Errorf("network has no requests or limits:\n%s", text)
	}
	if len(p.plot.Hover().dots) != 2 {
		t.Errorf("%d dots, want one per series", len(p.plot.Hover().dots))
	}
	p.variants[variantFilesystem].OnTapped()
	if text := middle(p.plot); !strings.Contains(text, "Usage: ") || !strings.Contains(text, "Mi") {
		t.Errorf("filesystem panel:\n%s", text)
	}

	_, _, _, p = promDrawer(t, ThemeDark, withFilesystemIO)
	app := p.perBox["app"]
	app.variants[variantFilesystem].OnTapped()
	if text := middle(app.plot); !strings.Contains(text, "Read: ") || !strings.Contains(text, "Write: ") || !strings.Contains(text, "KiB/s") {
		t.Errorf("filesystem io panel:\n%s", text)
	}
}

func TestRateFormatsAndLongRanges(t *testing.T) {
	// Rates read the same on the axis and in the hover panel: binary units with one decimal, whole
	// bytes below 1 KiB (D-32).
	axis, exact := variantFormats(variantNetwork, "")
	for _, tc := range []struct {
		v    float64
		want string
	}{
		{0, "0 B/s"}, {512, "512 B/s"}, {1536, "1.5 KiB/s"}, {200_000_000, "190.7 MiB/s"}, {0.25, "0 B/s"},
	} {
		if got := axis(tc.v); got != tc.want {
			t.Errorf("rate axis(%v) = %q, want %q", tc.v, got, tc.want)
		}
		if got := exact(tc.v); got != tc.want {
			t.Errorf("rate hover(%v) = %q, want %q", tc.v, got, tc.want)
		}
	}
	// A byte axis steps at round amounts of its unit, and at least at one byte.
	for _, tc := range []struct{ top, want float64 }{{75_000, 25 << 10}, {700, 250}, {1.5 * (1 << 30), 1 << 29}, {200 << 20, 100 << 20}, {1, 1}, {1.2, 1}} {
		if got := binaryStep(tc.top, 3); got != tc.want {
			t.Errorf("binaryStep(%v) = %v, want %v", tc.top, got, tc.want)
		}
	}
	// The axis labels in the user's format (D-31): a German and a US one.
	at := time.Date(2026, 10, 11, 14, 30, 0, 0, time.UTC)
	de := model.TimeLayout{Date: "02.01.2006", Time: "15:04:05"}.In(time.UTC)
	us := model.TimeLayout{Date: "1/2/2006", Time: "3:04:05 PM"}.In(time.UTC)
	for _, tc := range []struct {
		span   time.Duration
		de, us string
	}{
		{5 * time.Minute, "14:30:00", "2:30:00 PM"}, {time.Hour, "14:30", "2:30 PM"},
		{24 * time.Hour, "11.10. 14:30", "10/11 2:30 PM"}, {7 * 24 * time.Hour, "11.10.", "10/11"},
	} {
		for _, f := range []struct {
			tf   model.TimeFormat
			want string
		}{{de, tc.de}, {us, tc.us}} {
			d := chartData{From: at.Add(-tc.span), To: at, Time: f.tf}
			if got := d.timeLabel(at); got != f.want {
				t.Errorf("a %v axis labels %q, want %q", tc.span, got, f.want)
			}
		}
	}
	if got := agoText(50*time.Hour + 20*time.Minute); got != "2 d 2 h ago" {
		t.Errorf("agoText = %q", got)
	}
	if got := rangeLabels(true); got[0] != "5m" || got[len(got)-1] != "30d" || !slices.Contains(got, "1d") {
		t.Errorf("ranges with history = %v", got)
	}
}

// TestRenderPrometheusMetrics draws the drawer's charts with Prometheus's numbers for a look: each
// variant in both themes, a container's network with its hint, a week's range, and the hover over
// the network chart. To COGPIT_PNG_DIR or a temporary directory, never to testdata.
func TestRenderPrometheusMetrics(t *testing.T) {
	type look struct {
		name     string
		variant  string
		io       bool
		hover    bool
		week     bool
		scroll   float32
		narrow   bool
		appNetwk bool
	}
	looks := []look{ // CPU and Memory look as TestRenderMetricsDrawer draws them
		{name: "network", variant: variantNetwork},
		{name: "filesystem", variant: variantFilesystem},
		{name: "filesystem_io", variant: variantFilesystem, io: true},
		{name: "network_hover", variant: variantNetwork, hover: true},
		{name: "network_week", variant: variantNetwork, week: true},
		{name: "containers_network", variant: variantCPU, scroll: 400, appNetwk: true},
		{name: "narrow_network_hover", variant: variantNetwork, hover: true, narrow: true},
	}
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		for _, l := range looks {
			name := "prom_" + l.name + "_" + string(mode) + ".png"
			t.Run(name, func(t *testing.T) {
				var more []func(*engine.ObjectMetrics)
				if l.io {
					more = append(more, withFilesystemIO)
				}
				sh, _, q, p := promDrawer(t, mode, more...)
				if l.narrow {
					sh.setDrawerWidth(minDrawerWidth)
					q.until(t, "the drawer to take the width", func() bool { return sh.drawer.width == minDrawerWidth })
					settle(sh.win)
				}
				p.variants[l.variant].OnTapped()
				if l.week {
					p.rangeSel.SetSelected("7d")
					q.until(t, "the week", func() bool { return p.data.Targets[0].Range == "7d" })
				}
				if l.appNetwk {
					p.perBox["app"].variants[variantNetwork].OnTapped()
					p.perBox["envoy"].variants[variantFilesystem].OnTapped()
				}
				sh.drawer.scroll.Offset.Y = l.scroll
				sh.drawer.scroll.Refresh()
				settle(sh.win)
				if l.hover {
					g := p.plot.geom
					p.plot.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(g.left+g.w*0.62, g.top+g.h*0.5)}})
					if p.plot.Hover() == nil {
						t.Fatal("no hover")
					}
					settle(sh.win)
				}
				dir := os.Getenv("COGPIT_PNG_DIR")
				if dir == "" {
					dir = t.TempDir()
				}
				f, err := os.Create(filepath.Join(dir, name))
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
}
