package fyneui

import (
	"context"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The source row under the drawer's charts and each chart's variant as engine state (G12, owner
// 2026-10-12, evening). The providers' states are canned as the engine reports them.

// bothAnswering is the normal case: Prometheus gives the history (and the network and the
// filesystem), the Metrics Server the live values.
func bothAnswering() []metrics.ProviderStatus {
	return []metrics.ProviderStatus{
		{Provider: metrics.PrometheusID, Label: metrics.PrometheusLabel, Service: "monitoring/prometheus-server:http", Namespace: "monitoring", ServiceName: "prometheus-server", Status: metrics.StatusAvailable,
			LastOK: fixedNow.Add(-40 * time.Second), Serves: []string{metrics.ServesHistory, metrics.ServesNetwork, metrics.ServesFilesystem}},
		{Provider: metrics.ServerID, Label: metrics.ServerLabel, Status: metrics.StatusAvailable,
			LastOK: fixedNow.Add(-5 * time.Second), Serves: []string{metrics.ServesLive}},
	}
}

// serverDown is the fallback: the Metrics Server stopped answering, Prometheus serves the live
// values too.
func serverDown() []metrics.ProviderStatus {
	ps := bothAnswering()
	ps[0].Serves = []string{metrics.ServesHistory, metrics.ServesLive, metrics.ServesNetwork, metrics.ServesFilesystem}
	ps[0].LastOK = fixedNow.Add(-10 * time.Second)
	ps[1] = metrics.ProviderStatus{Provider: metrics.ServerID, Label: metrics.ServerLabel, Status: metrics.StatusUnavailable,
		LastOK: fixedNow.Add(-3 * time.Minute), LastError: "unavailable (HTTP 503)", LastErrorAt: fixedNow.Add(-25 * time.Second)}
	return ps
}

// prometheusGone: Prometheus was not found; the Metrics Server alone, without history.
func prometheusGone() []metrics.ProviderStatus {
	ps := bothAnswering()
	ps[0] = metrics.ProviderStatus{Provider: metrics.PrometheusID, Label: metrics.PrometheusLabel, Status: metrics.StatusNotServed,
		LastError: "not found (HTTP 404)", LastErrorAt: fixedNow.Add(-2 * time.Minute)}
	return ps
}

func withProviders(ps []metrics.ProviderStatus) func(*engine.ObjectMetrics) {
	return func(m *engine.ObjectMetrics) { m.Providers = ps }
}

// withVariantCommand answers ui.setMetricsVariant as the engine does: the chart's variant changes
// in what ObjectMetrics returns, and EventMetrics follows.
func withVariantCommand(f *fakeEngine) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers["ui.setMetricsVariant"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p struct {
			Ref       model.ObjectRef `json:"ref"`
			Container string          `json:"container"`
			Variant   string          `json:"variant"`
		}
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		f.setVariant(p.Ref, p.Container, p.Variant)
		return nil, nil
	}
}

// setVariant changes a chart's variant as the engine holds it (the GUI's command or an agent's) and
// reports it.
func (f *fakeEngine) setVariant(ref model.ObjectRef, container, variant string) {
	f.mu.Lock()
	old := f.metrics[ref.String()]
	if old == nil {
		f.mu.Unlock()
		return
	}
	m := *old
	m.Targets = slices.Clone(old.Targets)
	for i := range m.Targets {
		if m.Targets[i].Name == container {
			m.Targets[i].Variant = variant
		}
	}
	f.metrics[ref.String()] = &m
	f.mu.Unlock()
	f.emit(engine.Event{Type: engine.EventMetrics, View: ref.Kind})
}

// chipTexts are the source row's providers as text, "Prometheus ● 40 s ago", in the row's order.
func chipTexts(l *providerStatusLine) []string {
	var out []string
	for _, c := range l.chips {
		if c.Visible() {
			out = append(out, c.text())
		}
	}
	return out
}

// The source row under the drawer's charts (D-88, the owner's WF24 and WF25): Prometheus at the
// left, the Metrics Server at the right, each with its dot and when it last answered, the details
// in its tooltip, no link while both answer; a provider that fails says so in a word, its dot red,
// with a link to its section; Prometheus not found asks for its address.
func TestSourceRow(t *testing.T) {
	sh, f, q, p := promDrawer(t, ThemeDark, withProviders(bothAnswering()))
	got := chipTexts(p.status)
	if want := []string{"Prometheus ● 40 s ago", "Metrics Server ● 5 s ago"}; !slices.Equal(got, want) {
		t.Fatalf("source row = %q, want %q", got, want)
	}
	if p.caption.Visible() {
		t.Errorf("a source line in prose above the row: %q", p.caption.text)
	}
	prom, server := p.status.chips[0], p.status.chips[1]
	if prom.look != dotAnswering || !strings.Contains(prom.tip, "Prometheus monitoring/prometheus-server:http") ||
		!strings.Contains(prom.tip, "Found and answering.") || !strings.Contains(prom.tip, "Last good answer: 14:29:20 (40 s ago)") {
		t.Errorf("Prometheus: %q, %+v", prom.tip, prom.look)
	}
	// The two at the row's ends.
	settle(sh.win)
	if prom.Position().X != 0 || server.Position().X+server.Size().Width < p.status.root.Size().Width-1 {
		t.Errorf("Prometheus at %v, the Metrics Server at %v (+%v) in %v", prom.Position(), server.Position(), server.Size(), p.status.root.Size())
	}
	// Hovering a provider shows its whole state.
	server.MouseIn(&desktop.MouseEvent{PointEvent: fyne.PointEvent{AbsolutePosition: fyne.NewPos(10, 10)}})
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	if tip := sh.tip.visibleText(); !strings.Contains(tip, "Kubernetes Metrics Server") || !strings.Contains(tip, "Serves: live values.") {
		t.Errorf("tooltip = %q", tip)
	}
	server.MouseOut()

	// The Metrics Server stops answering: a red dot, "not answering" and a link to its settings;
	// the tooltip has the last error's class and time.
	f.mu.Lock()
	m := *f.metrics[p.ref.String()]
	m.Providers = serverDown()
	f.metrics[p.ref.String()] = &m
	f.mu.Unlock()
	f.emit(engine.Event{Type: engine.EventMetrics, View: "pods"})
	q.until(t, "the Metrics Server down on the row", func() bool {
		texts := chipTexts(p.status)
		return len(texts) == 2 && texts[1] == "Metrics Server ● not answering · "+settingsLink
	})
	if server.look != dotFailing || !strings.Contains(server.tip, "Last error: unavailable (HTTP 503) at 14:29:35 (25 s ago)") {
		t.Errorf("the Metrics Server: %q, %+v", server.tip, server.look)
	}
	if got := chipTexts(p.status)[0]; got != "Prometheus ● 10 s ago" {
		t.Errorf("Prometheus with the Metrics Server down: %q", got)
	}

	// Prometheus not found: a grey dot, "not found", and a link to set its address.
	f.mu.Lock()
	m.Providers = prometheusGone()
	f.metrics[p.ref.String()] = &m
	f.mu.Unlock()
	f.emit(engine.Event{Type: engine.EventMetrics, View: "pods"})
	q.until(t, "Prometheus not found", func() bool {
		texts := chipTexts(p.status)
		return len(texts) == 2 && texts[0] == "Prometheus ● not found · "+setAddressLink
	})
	if prom.look != dotAbsent || !strings.Contains(prom.tip, "Not found in this cluster.") {
		t.Errorf("Prometheus: %q, %+v", prom.tip, prom.look)
	}
}

// Every state has its dot and its word; a link only while a provider is not reachable; a tooltip
// never shows more than the class of a failure (the engine hands it nothing else).
func TestSourceChipStates(t *testing.T) {
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(ThemeDark))
	for _, tc := range []struct {
		ps   metrics.ProviderStatus
		text string
		look dotLook
	}{
		{metrics.ProviderStatus{Provider: metrics.PrometheusID, Label: "Prometheus", Status: metrics.StatusForbidden, LastError: "forbidden (HTTP 403)"},
			"Prometheus ● no permission · " + settingsLink, dotFailing},
		{metrics.ProviderStatus{Provider: metrics.PrometheusID, Label: "Prometheus", Status: metrics.StatusUnknown}, "Prometheus ● waiting", dotWaiting},
		{metrics.ProviderStatus{Provider: metrics.PrometheusID, Label: "Thanos", Service: "monitoring/thanos-query:http", Status: metrics.StatusAvailable},
			"Thanos ● answering", dotAnswering},
		{metrics.ProviderStatus{Provider: metrics.ServerID, Label: metrics.ServerLabel, Status: metrics.StatusUnavailable, LastError: "timeout"},
			"Metrics Server ● not answering · " + settingsLink, dotFailing},
		{metrics.ProviderStatus{Provider: metrics.ServerID, Label: metrics.ServerLabel, Status: metrics.StatusNotServed},
			"Metrics Server ● not found · " + settingsLink, dotAbsent},
		{metrics.ProviderStatus{Provider: metrics.ServerID, Label: metrics.ServerLabel, Status: metrics.StatusAvailable, LastOK: fixedNow},
			"Metrics Server ● now", dotAnswering},
	} {
		c := newSourceChip(&shell{app: a})
		c.set(tc.ps, fixedNow, germanTime, nil)
		if c.text() != tc.text || c.look != tc.look || !strings.HasPrefix(c.tip, tc.ps.Label) {
			t.Errorf("%s %s: %q %+v (tip %q), want %q %+v", tc.ps.Label, tc.ps.Status, c.text(), c.look, c.tip, tc.text, tc.look)
		}
		if tc.ps.LastError != "" && !strings.Contains(c.tip, "Last error: "+tc.ps.LastError) {
			t.Errorf("%s: tooltip without the failure's class: %q", tc.ps.Label, c.tip)
		}
	}
	// An integration's state on Cluster Settings has the same colours (D-87).
	for st, want := range map[engine.IntegrationState]dotLook{
		engine.IntegrationFound: dotAnswering, engine.IntegrationConfigured: dotAnswering, engine.IntegrationUnavailable: dotFailing,
		engine.IntegrationForbidden: dotFailing, engine.IntegrationMissing: dotAbsent, engine.IntegrationManual: dotAbsent,
		engine.IntegrationOff: dotAbsent, engine.IntegrationChecking: dotWaiting, engine.IntegrationNotConnected: dotWaiting,
	} {
		if got := integrationDot(st); got != want {
			t.Errorf("%s: %+v, want %+v", st, got, want)
		}
	}
}

// A chart's variant is engine state: the button asks the engine (ui.setMetricsVariant), and the
// chart follows what the engine reports, whoever set it.
func TestMetricsVariantIsEngineState(t *testing.T) {
	sh, f, q, p := promDrawer(t, ThemeDark, withProviders(bothAnswering()))
	p.variants[variantNetwork].OnTapped()
	q.until(t, "the command", func() bool { return len(f.callsOf("ui.setMetricsVariant")) == 1 })
	var params struct {
		Container string `json:"container"`
		Variant   string `json:"variant"`
	}
	if err := json.Unmarshal(normalize(f.callsOf("ui.setMetricsVariant")[0].Params), &params); err != nil || params.Variant != variantNetwork || params.Container != "" {
		t.Fatalf("ui.setMetricsVariant params %+v (%v)", params, err)
	}
	q.until(t, "the engine's answer", func() bool { return !p.variantPending && p.data.Targets[0].Variant == variantNetwork })
	if p.variant != variantNetwork {
		t.Errorf("the pod's chart shows %q", p.variant)
	}
	// An agent switches the app container's chart to Memory: the window follows.
	f.setVariant(p.ref, "app", variantMemory)
	q.until(t, "the agent's variant", func() bool { c := p.perBox["app"]; return c != nil && c.variant == variantMemory })
	if c := p.perBox["app"]; c.plot.data.Series[0].Name != "Memory usage" {
		t.Errorf("the app chart draws %q", c.plot.data.Series[0].Name)
	}
	_ = sh
}

// A sample without the part a chart shows (Prometheus had the memory but no CPU rate yet) is left
// out, not drawn as 0.
func TestMissingPartIsLeftOutOfTheChart(t *testing.T) {
	tg := &engine.MetricsTarget{Samples: []model.UsageSample{
		{At: fixedNow.Add(-time.Minute), Usage: model.Usage{CPUMilli: 120, MemoryBytes: 64 << 20}},
		{At: fixedNow.Add(-30 * time.Second), Usage: model.Usage{MemoryBytes: 65 << 20, Missing: model.UsageCPU}},
		{At: fixedNow, Usage: model.Usage{CPUMilli: 130, MemoryBytes: 66 << 20}},
	}}
	cpu, _, _, _ := usageSeries(tg, true, false)
	mem, _, _, _ := usageSeries(tg, false, false)
	if len(cpu[0].Points) != 2 || len(mem[0].Points) != 3 {
		t.Errorf("cpu %d points, memory %d: the sample without CPU belongs to the memory chart only", len(cpu[0].Points), len(mem[0].Points))
	}
	for _, pt := range cpu[0].Points {
		if pt.Value == 0 {
			t.Errorf("a missing CPU value drawn as 0 at %v", pt.At)
		}
	}
}

// TestRenderMetricsStatusLine draws the source row under a pod's charts in both themes: both
// providers answering, the Metrics Server down (its tooltip shown), Prometheus not found, and the
// narrowest drawer, where the two stack. To COGPIT_PNG_DIR or a temporary directory, never to
// testdata.
func TestRenderMetricsStatusLine(t *testing.T) {
	looks := []struct {
		name   string
		ps     []metrics.ProviderStatus
		narrow bool
		hover  bool
	}{
		{"both", bothAnswering(), false, false},
		{"server_down", serverDown(), false, true},
		{"prometheus_gone", prometheusGone(), false, false},
		{"narrow", serverDown(), true, false},
	}
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		for _, l := range looks {
			name := "metrics_status_" + l.name + "_" + string(mode) + ".png"
			t.Run(name, func(t *testing.T) {
				sh, _, q, p := promDrawer(t, mode, withProviders(l.ps))
				if l.narrow {
					sh.setDrawerWidth(minDrawerWidth)
					q.until(t, "the drawer to take the width", func() bool { return sh.drawer.width == minDrawerWidth })
				}
				settle(sh.win)
				if l.hover {
					c := p.status.chips[1]
					pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(c)
					c.MouseIn(&desktop.MouseEvent{PointEvent: fyne.PointEvent{AbsolutePosition: pos.AddXY(20, 8), Position: fyne.NewPos(20, 8)}})
					q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
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

// The row's links (WF25): none while the providers answer; a provider not reachable links to its
// section of the cluster's settings (D-87), the drawer's cluster's. The Prometheus… button under
// the row is gone.
func TestSourceRowLinksToTheSettings(t *testing.T) {
	sh, f, q, p := promDrawer(t, ThemeDark, withProviders(prometheusGone()))
	settle(sh.win)
	c := p.status.chips[0]
	if c.link != setAddressLink {
		t.Fatalf("Prometheus not found: link %q", c.link)
	}
	if b := findAll[*tipButton](p.root); slices.ContainsFunc(b, func(b *tipButton) bool { return b.Text == "Prometheus…" }) {
		t.Error("the Prometheus… button is still under the charts")
	}
	// A click on the name is no click on the link.
	c.Tapped(&fyne.PointEvent{Position: fyne.NewPos(2, 4)})
	settle(sh.win)
	if n := len(f.callsOf("cluster.settings.open")); n != 0 {
		t.Fatalf("a click on the name opened the settings (%d)", n)
	}
	c.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(c.linkStart()+4, 4)}})
	if !c.hoverLink || c.Cursor() != desktop.PointerCursor {
		t.Error("no hand over the link")
	}
	c.Tapped(&fyne.PointEvent{Position: fyne.NewPos(c.linkStart()+4, 4)})
	q.until(t, "cluster.settings.open", func() bool { return len(f.callsOf("cluster.settings.open")) == 1 })
	var params clusterSettingsOpenParams
	if err := json.Unmarshal(normalize(f.callsOf("cluster.settings.open")[0].Params), &params); err != nil ||
		params.Section != "prometheus" || params.Context != p.ref.Context {
		t.Errorf("cluster.settings.open %+v (%v): want the drawer's cluster's Prometheus section", params, err)
	}
	if sh.win.Canvas().Overlays().Top() != nil {
		t.Error("a dialog opened")
	}

	// Both answering: no link.
	p.status.set(bothAnswering(), fixedNow)
	for _, c := range p.status.chips {
		if c.link != "" {
			t.Errorf("%s links to %q while it answers", c.name, c.link)
		}
	}
}

// A cluster whose integrations are not chosen (D-82) reads no metrics: the caption says why, and
// the row offers the question again instead of the providers; the demo cluster is never asked; a
// cluster with both switched off gives the reason and no question.
func TestSourceRowUndecided(t *testing.T) {
	sh, f, q, p := promDrawer(t, ThemeDark)
	f.update(func(u *engine.UIState) { u.IntegrationsAskFirst = true })
	q.until(t, "the window asks first", func() bool { return sh.ui.IntegrationsAskFirst })
	const off = "the Metrics Server and Prometheus are switched off for this cluster"
	p.status.context = "kind-dev"
	p.status.set(nil, fixedNow)
	if !p.status.choose.Visible() || !p.status.root.Visible() || len(chipTexts(p.status)) != 0 {
		t.Fatalf("the question not offered: choose %v, row %q", p.status.choose.Visible(), chipTexts(p.status))
	}
	if got := noMetricsCaption(sh.ui, "kind-dev", off); got != "No metrics: no integrations are chosen for this cluster yet." {
		t.Errorf("caption %q", got)
	}
	test.Tap(p.status.choose)
	q.until(t, "cluster.integrations.ask", func() bool { return len(f.callsOf("cluster.integrations.ask")) == 1 })
	if c := f.callsOf("cluster.integrations.ask")[0]; string(normalize(c.Params)) != `{"context":"kind-dev"}` {
		t.Errorf("ask %s", c.Params)
	}
	if noIntegrationsChosen(sh.ui, engine.DemoContext) {
		t.Error("the demo cluster is asked")
	}

	// Decided, both switched off: the reason, no question.
	f.update(func(u *engine.UIState) {
		none := engine.IntMetrics
		u.ClusterSettings = map[string]engine.ClusterSettings{"kind-dev": {IntegrationsOff: &none}}
	})
	q.until(t, "the choice in the window's state", func() bool { return sh.ui.IntegrationsDecided("kind-dev") })
	p.status.set(nil, fixedNow)
	if p.status.root.Visible() || p.status.choose.Visible() {
		t.Error("a row without a provider or a question")
	}
	if got := noMetricsCaption(sh.ui, "kind-dev", off); got != "No metrics: "+off {
		t.Errorf("caption %q", got)
	}
}
