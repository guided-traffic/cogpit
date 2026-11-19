package fyneui

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A pod whose charts show the network alone has no current CPU and memory (the hub asks for none):
// the drawer draws the network and says nothing of missing measurements; at CPU the note is back.
func TestNetworkOnlyChartNeedsNoCurrentUsage(t *testing.T) {
	networkOnly := func(m *engine.ObjectMetrics) {
		for i := range m.Targets {
			m.Targets[i].Current, m.Targets[i].Samples, m.Targets[i].Variant = nil, nil, engine.VariantNetwork
		}
	}
	_, _, _, p := promDrawer(t, ThemeDark, networkOnly)
	if p.note.Visible() {
		t.Errorf("the network alone: note %q", p.note.text)
	}
	if len(p.plot.data.Series) != 2 {
		t.Errorf("the network chart has %d series, want receive and transmit", len(p.plot.data.Series))
	}

	_, _, _, p = promDrawer(t, ThemeDark, func(m *engine.ObjectMetrics) {
		networkOnly(m)
		m.Targets[0].Variant = engine.VariantCPU
	})
	if !p.note.Visible() || p.note.text != "No metrics for this Pod yet." {
		t.Errorf("CPU without a measurement: note %q (shown %v)", p.note.text, p.note.Visible())
	}
}
