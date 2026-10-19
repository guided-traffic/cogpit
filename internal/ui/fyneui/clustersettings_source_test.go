package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
)

// Cluster settings › Metrics › Sources (D-87, the owner's WF22; before, Lens's Metrics source
// dropdown): the two integrations Metrics reads, each with its dot, its name linking to its own
// section and its status; switching both off on their sections empties the Pods list's CPU and
// agents read the state; the window's metricsSource (Automatic; an agent's is refused since H-126)
// brings the numbers back.
func TestMetricsSourceFromTheSettings(t *testing.T) {
	s, sh, _, until := demoSettingsShell(t)
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": engine.DemoContext, "section": "metrics"})
	status := func(name engine.Integration) string {
		row, ok := sh.settings.controls["metricsSource:"+string(name)].(*fyne.Container)
		if !ok {
			return ""
		}
		return row.Objects[2].(*textLine).text
	}
	until("the sources", func() bool {
		return sh.settings.view != nil && status(engine.IntegrationMetricsServer) == "Found automatically" &&
			status(engine.IntegrationPrometheus) == "Not in this cluster: the demo cluster's metrics are its own"
	})
	row := sh.settings.controls["metricsSource:"+string(engine.IntegrationMetricsServer)].(*fyne.Container)
	if d := row.Objects[0].(*statusDot); d.look != dotAnswering {
		t.Errorf("the Metrics Server's dot %+v", d.look)
	}
	if l := row.Objects[1].(*linkText); l.text != metrics.ServerLabel {
		t.Errorf("the Metrics Server's link %q", l.text)
	}
	test.Tap(row.Objects[1].(*linkText))
	until("the Metrics Server's section", func() bool {
		st := s.UIState().Settings
		return st != nil && st.Section == "metrics-server" && len(sh.settings.integrationRows) == 1
	})

	// Both switched off on their sections: no metrics.
	test.Tap(sh.settings.integrationRows[0].check)
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": engine.DemoContext, "section": "prometheus"})
	until("Prometheus's section", func() bool { return len(sh.settings.integrationRows) == 1 && sh.settings.shown.Section == "prometheus" })
	test.Tap(sh.settings.integrationRows[0].check)
	until("metrics off", func() bool {
		cs, _ := s.ClusterSettingsOf(engine.DemoContext)
		return cs.IntegrationsOff != nil && cs.IntegrationsOff.Has(engine.IntMetrics)
	})
	mustDispatch(t, s, "settings.close", struct{}{})
	until("the CPU column empty", func() bool { return sh.view.snap != nil && usageRow(sh.view.snap) < 0 })
	// No request with metrics off is counted at the hub (TestMetricsSourceNone); here the window
	// and agents see the state.
	if m, _ := s.MetricsGet(t.Context(), "", "pods", "", "", 1); m.Status != metrics.StatusNotServed {
		t.Errorf("metrics.get with metrics off: %+v", m.Status)
	}

	// The window's command sets Automatic again: the numbers come back, the sources say so.
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": engine.DemoContext, "metricsSource": "auto"})
	until("the CPU column back", func() bool { return sh.view.snap != nil && usageRow(sh.view.snap) >= 0 })
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": engine.DemoContext, "section": "metrics"})
	until("the Metrics Server found again", func() bool { return status(engine.IntegrationMetricsServer) == "Found automatically" })
	if _, err := s.Dispatch(t.Context(), engine.Invocation{ID: "cluster.settings.set", Params: []byte(`{"metricsSource":"thanos"}`), Origin: engine.OriginExternalAgent}); err == nil {
		t.Error("an unknown metrics source was accepted")
	}
}
