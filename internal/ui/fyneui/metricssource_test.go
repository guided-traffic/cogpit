package fyneui

import (
	"context"
	"encoding/json"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// tapSeg taps the middle of a source line's piece, as a click there would.
func tapSeg(l *sourceLine, text string) bool {
	for i, sp := range l.spans(l.Size().Width) {
		if l.segs[i].text == text {
			l.Tapped(&fyne.PointEvent{Position: fyne.NewPos((sp[0]+sp[1])/2, 4)})
			return true
		}
	}
	return false
}

// Where a found Prometheus runs is on its section of Cluster Settings (D-87; until 2026-10-18 the
// drawer's source line): its namespace and service are links that close the page and open the
// Namespace or the Service through ui.navigate, in that cluster; the hand pointer and the underline
// show over a link only; a status without a Service's place stays plain.
func TestPrometheusSectionLinksItsService(t *testing.T) {
	inf := newIntegrationsFake(t)
	inf.decide()
	inf.statuses[1] = engine.IntegrationStatus{Name: engine.IntegrationPrometheus, State: engine.IntegrationFound, Where: "monitoring/prometheus-server:http"}
	inf.handlers["cluster.prometheus"] = func(context.Context, engine.Invocation) (any, error) {
		return engine.PrometheusSetting{Context: "kind-dev", Auto: true, Format: autoSetting, Kept: true}, nil
	}
	inf.handlers["settings.close"] = func(context.Context, engine.Invocation) (any, error) {
		inf.update(func(u *engine.UIState) { u.Settings = nil })
		return nil, nil
	}
	sh, _, q := newTestShell(t, inf.fakeEngine, Options{})
	inf.update(func(u *engine.UIState) {
		u.Settings = &engine.SettingsPage{Page: engine.SettingsCluster, Context: "kind-dev", Section: "prometheus"}
	})
	var l *sourceLine
	q.until(t, "the status with its place", func() bool {
		if len(sh.settings.integrationRows) != 1 {
			return false
		}
		l = sh.settings.integrationRows[0].status
		return l.text == "Found automatically: monitoring/prometheus-server:http"
	})
	settle(sh.win)
	links := l.links()
	if len(links) != 2 || links["monitoring"] == nil || links["prometheus-server"] == nil {
		t.Fatalf("links %v", links)
	}
	if !tapSeg(l, "prometheus-server") {
		t.Fatal("no service piece")
	}
	q.until(t, "the page closed, then ui.navigate to the Service", func() bool {
		return len(inf.callsOf("settings.close")) == 1 && len(inf.callsOf("ui.navigate")) == 1
	})
	var nav refParams
	if err := json.Unmarshal(normalize(inf.callsOf("ui.navigate")[0].Params), &nav); err != nil ||
		!nav.Ref.Same(model.ObjectRef{Kind: kindServices, Namespace: "monitoring", Name: "prometheus-server"}) || nav.Ref.Context != "kind-dev" {
		t.Errorf("navigate %+v (%v)", nav, err)
	}
	tapSeg(l, "Found automatically: ")
	settle(sh.win)
	if n := len(inf.callsOf("ui.navigate")); n != 1 {
		t.Errorf("a click on the plain text navigated (%d)", n)
	}

	// The pointer: a hand and an underline over a link only.
	sp := l.spans(l.Size().Width)
	l.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos((sp[3][0]+sp[3][1])/2, 4)}})
	if l.hover != 3 || l.Cursor() != desktop.PointerCursor {
		t.Errorf("over the service: hover %d, cursor %v", l.hover, l.Cursor())
	}
	l.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(2, 4)}})
	if l.hover != -1 || l.Cursor() != desktop.DefaultCursor {
		t.Errorf("over the text: hover %d, cursor %v", l.hover, l.Cursor())
	}
}

// serviceSegments links a Service's place at the end of a line only: an API group (Trivy's,
// Kyverno's) or a line that does not end in the place stays plain.
func TestServiceSegments(t *testing.T) {
	sh := &shell{}
	segs := sh.serviceSegments("Configured: prometheus/prometheus:9090", "prometheus/prometheus:9090", "kind-dev")
	want := []string{"Configured: ", "prometheus", "/", "prometheus", ":9090"}
	if len(segs) != len(want) {
		t.Fatalf("segments %+v", segs)
	}
	for i := range want {
		if segs[i].text != want[i] || (segs[i].onTap != nil) != (i == 1 || i == 3) {
			t.Errorf("segment %d: %q (link %v)", i, segs[i].text, segs[i].onTap != nil)
		}
	}
	for _, tc := range [][2]string{
		{"Found automatically: wgpolicyk8s.io/v1alpha2", "wgpolicyk8s.io/v1alpha2"},
		{"Not answering: monitoring/am:9093 (timeout)", "monitoring/am:9093"},
		{"Found automatically", ""},
	} {
		if segs := sh.serviceSegments(tc[0], tc[1], "kind-dev"); len(segs) != 1 || segs[0].onTap != nil || segs[0].text != tc[0] {
			t.Errorf("%q: %+v", tc[0], segs)
		}
	}
}
