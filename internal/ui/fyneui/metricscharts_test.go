package fyneui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Every chart has its own controls, and changing one changes only that chart (owner: "The selection
// buttons must be shown at every container too. There is no main button").
func TestEveryChartHasItsOwnControls(t *testing.T) {
	sh, f, q := metricsDrawerFor(t, twoContainerPod(t), ThemeDark, withEnvoy)
	p := sh.drawer.kept[metricsKey].(*metricsPanel)
	app, envoy := p.perBox["app"], p.perBox["envoy"]
	if app == nil || envoy == nil {
		t.Fatalf("container charts = %v", p.perBox)
	}
	for name, c := range map[string]*chartCtl{"pod": p.chartCtl, "app": app, "envoy": envoy} {
		if (c.rangeSel == nil && c.rangeBtn == nil) || c.flag == nil || len(c.variants) != 4 || !c.controls.Visible() {
			t.Errorf("%s: incomplete controls", name)
		}
		for _, id := range []string{variantNetwork, variantFilesystem} {
			if !c.variants[id].Disabled() || !strings.Contains(c.variants[id].tip, "needs Prometheus") {
				t.Errorf("%s/%s: want disabled with the reason, tip %q", name, id, c.variants[id].tip)
			}
		}
		if c.variant != variantCPU || c.variants[variantCPU].Importance != widget.HighImportance {
			t.Errorf("%s: CPU must be the default", name)
		}
	}
	if app.controls == envoy.controls || app.controls == p.controls {
		t.Fatal("the charts share a row of controls")
	}

	// Memory on envoy: its series and its lines change, nothing else.
	envoy.variants[variantMemory].OnTapped()
	d := envoy.plot.data
	if d.Series[0].Name != "Memory usage" || d.Lines[0].Value != float64(64<<20) || d.Lines[1].Value != float64(96<<20) {
		t.Errorf("envoy after Memory: %+v", d)
	}
	if envoy.variants[variantMemory].Importance != widget.HighImportance || envoy.variants[variantCPU].Importance == widget.HighImportance {
		t.Error("envoy's buttons do not show Memory")
	}
	for name, c := range map[string]*chartCtl{"pod": p.chartCtl, "app": app} {
		if c.plot.data.Series[0].Name != "CPU usage" || c.variant != variantCPU || c.variants[variantCPU].Importance != widget.HighImportance {
			t.Errorf("%s changed with envoy: %+v", name, c.plot.data.Series[0].Name)
		}
	}
	if app.plot.data.Lines[0].Value != 0.2 {
		t.Errorf("app's lines = %+v", app.plot.data.Lines)
	}

	// The range and the flag are per chart as well.
	app.setRange("5m")
	app.flag.OnTapped()
	if app.plot.data.To.Sub(app.plot.data.From) != 5*time.Minute || envoy.plot.data.To.Sub(envoy.plot.data.From) != time.Hour ||
		p.plot.data.To.Sub(p.plot.data.From) != time.Hour {
		t.Error("app's range leaked into another chart")
	}
	if envoy.showEvents || p.showEvents || !app.showEvents {
		t.Error("the events flag is not per chart")
	}

	// A rebuild (a watch event) and a poll keep every selection.
	sh.drawer.rebuild()
	settle(sh.win)
	f.emit(engine.Event{Type: engine.EventMetrics, View: "pods"})
	q.until(t, "the poll", func() bool { return p.loads >= 2 })
	settle(sh.win)
	if p.perBox["app"] != app || p.perBox["envoy"] != envoy {
		t.Fatal("the container charts were made anew")
	}
	if envoy.variant != variantMemory || envoy.plot.data.Series[0].Name != "Memory usage" || app.window != 5*time.Minute || !app.showEvents ||
		p.variant != variantCPU || app.variant != variantCPU {
		t.Error("a selection was lost over a rebuild and a poll")
	}
}

// A container that appears later gets the defaults, whatever the others show.
func TestLateContainerStartsWithCPU(t *testing.T) {
	sh, _, q := metricsDrawerFor(t, twoContainerPod(t), ThemeDark, nil)
	p := sh.drawer.kept[metricsKey].(*metricsPanel)
	p.variants[variantMemory].OnTapped()
	// The tap reaches the engine on another goroutine, which reads the metrics it holds: wait for
	// its answer, then add the container to a copy (mutating the shared one raced).
	q.until(t, "the pod's Memory from the engine", func() bool {
		p = sh.drawer.kept[metricsKey].(*metricsPanel)
		return p.data != nil && len(p.data.Targets) > 0 && p.data.Targets[0].Variant == engine.VariantMemory
	})
	d := *p.data
	d.Targets = slices.Clone(d.Targets)
	withEnvoy(&d)
	p.data = &d
	p.render()
	p.d.rebuild()
	settle(sh.win)
	e := p.perBox["envoy"]
	if e == nil || e.variant != variantCPU || e.plot.data.Series[0].Name != "CPU usage" || e.window != time.Hour || e.showEvents {
		t.Fatalf("envoy = %+v", e)
	}
	if p.perBox["app"].variant != variantCPU {
		t.Error("the pod's Memory leaked into app")
	}
}
