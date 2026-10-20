package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The bearing's token of an integration says its state by shape and tone: live filled in the
// cluster's cyan, found but waiting for the user an outline, failing in the error colour, off or
// absent dim; a switch off wins over what was found.
func TestBearingStates(t *testing.T) {
	for _, c := range []struct {
		name string
		st   engine.IntegrationStatus
		want bearingState
	}{
		{"found", engine.IntegrationStatus{Name: engine.IntegrationMetricsServer, On: true, State: engine.IntegrationFound}, bearingLive},
		{"configured", engine.IntegrationStatus{Name: engine.IntegrationPrometheus, On: true, State: engine.IntegrationConfigured}, bearingLive},
		{"manual", engine.IntegrationStatus{Name: engine.IntegrationPrometheus, On: true, State: engine.IntegrationManual}, bearingNeeds},
		{"forbidden", engine.IntegrationStatus{Name: engine.IntegrationKyverno, On: true, State: engine.IntegrationForbidden}, bearingFailing},
		{"unavailable", engine.IntegrationStatus{Name: engine.IntegrationAlertmanager, On: true, State: engine.IntegrationUnavailable}, bearingFailing},
		{"missing", engine.IntegrationStatus{Name: engine.IntegrationTrivy, On: true, State: engine.IntegrationMissing}, bearingOff},
		{"switched off", engine.IntegrationStatus{Name: engine.IntegrationMetricsServer, On: false, State: engine.IntegrationFound}, bearingOff},
	} {
		if got := bearingOf(c.st); got.state != c.want {
			t.Errorf("%s: state %v, want %v", c.name, got.state, c.want)
		}
	}
	items := bearingItems([]engine.IntegrationStatus{{Name: engine.IntegrationKyverno, On: true, State: engine.IntegrationFound}})
	if len(items) != len(engine.Integrations) || items[len(items)-1].name != engine.IntegrationKyverno || items[len(items)-1].state != bearingLive ||
		items[0].state != bearingOff {
		t.Errorf("items %+v: want the fixed order, an unreported integration off", items)
	}
	if got := bearingHead(items); got != "integrations · 1 of 5 live" {
		t.Errorf("head %q", got)
	}
}

// The Overview's header draws the nodes as one square each, the pods as bars by state, and a
// legend of the states that are not healthy for nodes; a large cluster's nodes coarsen to bars.
func TestOverviewHealthHeader(t *testing.T) {
	test.NewTempApp(t)
	h := newOverviewHealth(nil)
	h.setStates(
		[]engine.StatusCount{{Status: model.StatusOK, Count: 7}, {Status: model.StatusError, Count: 1}},
		[]engine.StatusCount{{Status: model.StatusOK, Count: 38}, {Status: model.StatusPending, Count: 2}})
	if len(h.nodes.tokens) != 8 || h.nodes.bars || h.nodesHead.text != "nodes · 7 of 8 ready" {
		t.Errorf("nodes: %d tokens, bars %v, head %q", len(h.nodes.tokens), h.nodes.bars, h.nodesHead.text)
	}
	if len(h.pods.tokens) != 2 || !h.pods.bars || h.podsHead.text != "pods · 40" {
		t.Errorf("pods: %d tokens, bars %v, head %q", len(h.pods.tokens), h.pods.bars, h.podsHead.text)
	}
	if n := len(h.nodesLegend.row.Objects); n != 1 {
		t.Errorf("the nodes' legend names %d states, want only the one that is not Ready", n)
	}
	h.setStates([]engine.StatusCount{{Status: model.StatusOK, Count: 120}}, nil)
	if !h.nodes.bars || len(h.nodes.tokens) != 1 || h.nodesHead.text != "nodes · 120 ready" {
		t.Errorf("120 nodes: %d tokens, bars %v, head %q", len(h.nodes.tokens), h.nodes.bars, h.nodesHead.text)
	}
	if h.podsCol.Visible() {
		t.Error("the pods group shows without pods")
	}
}

// The alerts pill shows a token and a number per severity that has alerts, in order, and the
// single number otherwise.
func TestAlertsPillSegments(t *testing.T) {
	f := connectedFake(t, 10)
	sh, w, _ := newTestShell(t, f, Options{})
	settle(w)
	a := sh.top.alerts
	a.set(&engine.AlertsSummary{State: engine.AlertsFound, Answered: true, Total: 24, Critical: 3, Warning: 18, Info: 3, Worst: "critical"})
	settle(w)
	r := test.WidgetRenderer(a).(*alertsRenderer)
	if r.count.Visible() {
		t.Error("the single number shows beside the severities")
	}
	for i, want := range []string{"3", "18", "3"} {
		if !r.segs[i].tok.Visible() || r.segs[i].num.Text != want {
			t.Errorf("severity %d: token visible %v, number %q, want %q", i, r.segs[i].tok.Visible(), r.segs[i].num.Text, want)
		}
	}
	if r.segs[1].tok.Size().Width <= r.segs[0].tok.Size().Width {
		t.Errorf("the 18 warnings' token (%v) is not wider than the 3 criticals' (%v)", r.segs[1].tok.Size().Width, r.segs[0].tok.Size().Width)
	}
	a.set(&engine.AlertsSummary{State: engine.AlertsFound, Answered: true, Total: 2, Info: 2, Worst: "info"})
	settle(w)
	if r.segs[0].tok.Visible() || r.segs[1].tok.Visible() || !r.segs[2].tok.Visible() {
		t.Error("a severity without alerts has a token")
	}
	a.set(&engine.AlertsSummary{State: engine.AlertsFound, Answered: true})
	settle(w)
	if !r.count.Visible() || r.segs[2].tok.Visible() {
		t.Error("no alerts: want the single zero")
	}
}

// Requests and limits are ticks at the plot's right edge at rest; the pointer on the legend's
// "Reference lines" entry runs them full width, dashed, and leaving it puts them back. The
// ceilings are single hairlines either way.
func TestChartReferenceLinesAreRevealedByTheLegend(t *testing.T) {
	c := hoverChart(t)
	if c.refRect.x1 <= c.refRect.x0 {
		t.Fatalf("no legend entry for the reference lines: %+v", c.refRect)
	}
	count := func() int { return len(test.WidgetRenderer(c).Objects()) }
	rest := count()
	in := fyne.NewPos((c.refRect.x0+c.refRect.x1)/2, (c.refRect.y0+c.refRect.y1)/2)
	c.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: in}})
	if !c.refShown {
		t.Fatal("the pointer on the entry did not reveal the lines")
	}
	if shown := count(); shown <= rest {
		t.Errorf("revealed: %d objects, at rest %d: the dashes are missing", shown, rest)
	}
	c.MouseOut()
	if c.refShown || count() != rest {
		t.Errorf("after the pointer left: shown %v, %d objects, want rest %d", c.refShown, count(), rest)
	}
}

// The segmented Master | Worker control marks the chosen segment with its underline and none
// with none.
func TestSegmentedControlMarksTheChosenSegment(t *testing.T) {
	test.NewTempApp(t)
	a, b := widget.NewButton("Master", nil), widget.NewButton("Worker", nil)
	s := newSegmented(&shell{app: fyne.CurrentApp()}, a, b)
	s.choose(1)
	if s.under.Hidden || !s.st[1].chosen || s.st[0].chosen {
		t.Errorf("worker chosen: underline hidden %v, themes %v %v", s.under.Hidden, s.st[0].chosen, s.st[1].chosen)
	}
	s.root.Resize(s.root.MinSize())
	if s.under.Position().X < a.MinSize().Width {
		t.Errorf("the underline at %v is not under the second segment", s.under.Position())
	}
	s.choose(-1)
	if !s.under.Hidden {
		t.Error("an underline without a chosen segment")
	}
}

// The alerts pill's tokens are bounded: five widths, and "99+" from a hundred alerts.
func TestAlertTokenWidthIsBounded(t *testing.T) {
	if w := alertTokenWidth(1000); w != 24 || alertTokenWidth(1) != 8 || alertTokenWidth(18) != 16 {
		t.Errorf("widths: 1 → %v, 18 → %v, 1000 → %v", alertTokenWidth(1), alertTokenWidth(18), w)
	}
	if alertCountText(100) != "99+" || alertCountText(99) != "99" {
		t.Errorf("counts: %q %q", alertCountText(99), alertCountText(100))
	}
}

// A found integration's status is muted words and a mint dot; a failing one is coral words and
// dot; the pill switch toggles from anywhere in its row.
func TestIntegrationRowStaysQuietWhenHealthy(t *testing.T) {
	test.NewTempApp(t)
	in, _ := engine.IntegrationOf(engine.IntegrationMetricsServer)
	var got []bool
	r := newIntegrationRow(in, func(on bool) { got = append(got, on) })
	r.paintStatus(colorStatusOK)
	if r.status.color != colorMuted || r.dot.fill != colorStatusOK || !r.dot.Visible() {
		t.Errorf("healthy: words %s, dot %s", r.status.color, r.dot.fill)
	}
	r.paintStatus(colorStatusError)
	if r.status.color != colorStatusError || r.dot.fill != colorStatusError {
		t.Errorf("failing: words %s, dot %s", r.status.color, r.dot.fill)
	}
	r.paintStatus(colorMuted)
	if r.dot.Visible() {
		t.Error("a plain state has a dot")
	}
	r.check.Tapped(&fyne.PointEvent{Position: fyne.NewPos(1, 1)})
	if !r.check.Checked || len(got) != 1 || !got[0] {
		t.Errorf("tap: checked %v, calls %v", r.check.Checked, got)
	}
}
