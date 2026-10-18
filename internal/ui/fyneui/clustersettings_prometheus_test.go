package fyneui

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
)

// promSettingsFake is a fake connected to a real cluster's context with the Prometheus commands
// answered as the engine does: cluster.prometheus returns the setting, cluster.setPrometheus
// records its parameters and takes them as the new setting (a refused address is an error).
type promSettingsFake struct {
	*fakeEngine
	mu  sync.Mutex
	set engine.PrometheusSetting
	got []prometheusSetParams
}

func newPromSettingsFake(t *testing.T, set engine.PrometheusSetting) *promSettingsFake {
	t.Helper()
	f := connectedFake(t, 20)
	f.ui.Context, f.ui.Connection = "kind-dev", engine.Connection{State: engine.Connected}
	pf := &promSettingsFake{fakeEngine: f, set: set}
	f.handlers["cluster.settings.get"] = func(context.Context, engine.Invocation) (any, error) {
		pf.mu.Lock()
		defer pf.mu.Unlock()
		st := engine.IntegrationStatus{Name: engine.IntegrationPrometheus, On: true, State: engine.IntegrationChecking}
		switch {
		case pf.set.InUseStatus == metrics.StatusAvailable && pf.set.Auto:
			st.State, st.Where = engine.IntegrationFound, pf.set.InUse
		case pf.set.InUseStatus == metrics.StatusAvailable:
			st.State, st.Where = engine.IntegrationConfigured, pf.set.Address+pf.set.Prefix
		}
		return engine.ClusterSettingsView{Context: "kind-dev", Name: "kind-dev", ContextName: "kind-dev", Connected: true,
			Integrations: []engine.IntegrationStatus{st}}, nil
	}
	f.handlers["cluster.prometheus"] = func(context.Context, engine.Invocation) (any, error) {
		pf.mu.Lock()
		defer pf.mu.Unlock()
		return pf.set, nil
	}
	f.handlers["cluster.setPrometheus"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p prometheusSetParams
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		pf.mu.Lock()
		defer pf.mu.Unlock()
		pf.got = append(pf.got, p)
		if p.Address == "bad" {
			return nil, errors.New(`"bad": write the address as namespace/service:port`)
		}
		pf.set.Auto, pf.set.Address, pf.set.Prefix = p.Address == autoSetting, "", ""
		if !pf.set.Auto {
			pf.set.Address, pf.set.Prefix = p.Address, p.Prefix
		}
		pf.set.Format, pf.set.FilterEmptyContainers, pf.set.HTTPS = p.Format, p.FilterEmptyContainers, p.HTTPS
		return pf.set, nil
	}
	return pf
}

// calls are the cluster.setPrometheus parameters so far.
func (pf *promSettingsFake) calls() []prometheusSetParams {
	pf.mu.Lock()
	defer pf.mu.Unlock()
	return append([]prometheusSetParams(nil), pf.got...)
}

// openPromSettings shows kind-dev's Settings › Metrics and waits for the engine's setting in it.
func openPromSettings(t *testing.T, mode ThemeMode, set engine.PrometheusSetting) (*shell, *promSettingsFake, *mainQueue, *promFields) {
	t.Helper()
	pf := newPromSettingsFake(t, set)
	sh, _, q := newTestShell(t, pf.fakeEngine, Options{Theme: mode})
	sh.settings.typing = time.Millisecond
	pf.update(func(u *engine.UIState) {
		u.Settings = &engine.SettingsPage{Page: engine.SettingsCluster, Context: "kind-dev", Section: "prometheus"}
	})
	want := prometheusInUseLine(set)
	q.until(t, "the Prometheus fields", func() bool {
		f := sh.settings.prom
		return f != nil && f.line.text == want && f.enabled
	})
	return sh, pf, q, sh.settings.prom
}

// Cluster Settings › Metrics shows the Prometheus's fields on the page (G12, 2026-10-16 night): the
// query format and the empty-container filter, the service address (empty: found by name) with the
// Prometheus in use under it and HTTPS, the path prefix for an address only. Each change sets the
// whole setting at once (a text field after the typing pause); a refusal shows under the address;
// an agent's change shows in the fields.
func TestPrometheusSettingsOnThePage(t *testing.T) {
	sh, pf, q, f := openPromSettings(t, ThemeDark, engine.PrometheusSetting{Context: "kind-dev", Auto: true, Format: autoSetting,
		FilterEmptyContainers: true, Kept: true, InUse: "Prometheus monitoring/prometheus-k8s:web", InUseStatus: metrics.StatusAvailable})
	if f.line.text != "Auto detect: in use Prometheus monitoring/prometheus-k8s:web." {
		t.Errorf("line %q", f.line.text)
	}
	if f.format.Selected != metrics.QueryFormats[0].Label || !f.filter.Checked || f.https.Checked || f.address.Text != "" ||
		f.address.Disabled() || !f.prefix.Disabled() {
		t.Fatalf("fields: format %q, filter %v, https %v, address %q (disabled %v), prefix disabled %v",
			f.format.Selected, f.filter.Checked, f.https.Checked, f.address.Text, f.address.Disabled(), f.prefix.Disabled())
	}
	if n := len(pf.calls()); n != 0 {
		t.Fatalf("%d sets before any change", n)
	}

	// A service address, typed: set after the pause, with the query settings as they are; the
	// prefix takes input now.
	sh.win.Canvas().Focus(f.address)
	test.Type(f.address, "observability/thanos-query:9090")
	q.until(t, "the address set", func() bool {
		c := pf.calls()
		return len(c) > 0 && c[len(c)-1].Address == "observability/thanos-query:9090"
	})
	if c := pf.calls(); c[len(c)-1] != (prometheusSetParams{Context: "kind-dev", Address: "observability/thanos-query:9090", Format: autoSetting, FilterEmptyContainers: true}) {
		t.Errorf("set %+v", c[len(c)-1])
	}
	if f.prefix.Disabled() {
		t.Fatal("the path prefix is disabled with an address")
	}
	sh.win.Canvas().Focus(f.prefix)
	test.Type(f.prefix, "/prometheus")
	q.until(t, "the prefix set", func() bool {
		c := pf.calls()
		return len(c) > 0 && c[len(c)-1].Prefix == "/prometheus"
	})
	sh.win.Canvas().Unfocus()

	// The selects and checks at once.
	f.format.SetSelected(metrics.QueryOperator.Label())
	q.until(t, "the format set", func() bool {
		c := pf.calls()
		return c[len(c)-1].Format == "prometheus-operator" && c[len(c)-1].Address == "observability/thanos-query:9090"
	})
	test.Tap(f.filter)
	test.Tap(f.https)
	q.until(t, "the checks set", func() bool {
		c := pf.calls()
		return !c[len(c)-1].FilterEmptyContainers && c[len(c)-1].HTTPS && c[len(c)-1].Prefix == "/prometheus"
	})

	// A refusal shows under the address.
	sh.win.Canvas().Focus(f.address)
	f.address.SetText("")
	test.Type(f.address, "bad")
	q.until(t, "the refusal", func() bool {
		l := sh.settings.errs[promAddressSetting]
		return l != nil && l.Visible() && l.text == `"bad": write the address as namespace/service:port`
	})

	// Cleared: auto detection again, without the prefix, which is disabled.
	f.address.SetText("")
	q.until(t, "auto again", func() bool {
		c := pf.calls()
		return c[len(c)-1].Address == autoSetting && c[len(c)-1].Prefix == "" && !sh.settings.errs[promAddressSetting].Visible()
	})
	if !f.prefix.Disabled() {
		t.Error("the path prefix takes input without an address")
	}
	sh.win.Canvas().Unfocus()

	// An agent sets another service: the fields follow at the next metrics event.
	pf.mu.Lock()
	pf.set = engine.PrometheusSetting{Context: "kind-dev", Address: "vm/vmselect-main:http", Prefix: "/select/0/prometheus", Format: "cadvisor",
		FormatLabel: metrics.QueryCAdvisor.Label(), FilterEmptyContainers: true, Kept: true,
		InUse: "VictoriaMetrics vm/vmselect-main:http/select/0/prometheus", InUseStatus: metrics.StatusUnavailable}
	pf.mu.Unlock()
	n := len(pf.calls())
	pf.emit(engine.Event{Type: engine.EventMetrics})
	q.until(t, "the agent's setting shown", func() bool {
		return f.address.Text == "vm/vmselect-main:http" && f.prefix.Text == "/select/0/prometheus" &&
			f.format.Selected == metrics.QueryCAdvisor.Label() && !f.https.Checked &&
			f.line.text == "Set: VictoriaMetrics vm/vmselect-main:http/select/0/prometheus does not answer."
	})
	if len(pf.calls()) != n {
		t.Errorf("showing the agent's setting set it again: %+v", pf.calls()[n:])
	}
}

// The fields of a cluster that is not connected, and of the demo cluster, are disabled with the
// reason.
func TestPrometheusSettingsNeedAConnection(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "prometheus"})
	q.until(t, "the fields disabled", func() bool {
		f := sh.settings.prom
		return f != nil && sh.settings.view != nil && f.line.text == "Connect to this cluster to set its Prometheus." &&
			f.address.Disabled() && f.format.Disabled() && f.prefix.Disabled() && f.https.Disabled()
	})
}

func TestPrometheusInUseLine(t *testing.T) {
	for _, c := range []struct {
		set  engine.PrometheusSetting
		want string
	}{
		{engine.PrometheusSetting{Auto: true}, "Auto detect: Cogpit looks for it among the cluster's services when a chart needs it."},
		{engine.PrometheusSetting{Auto: true, InUseStatus: metrics.StatusNotServed}, "Auto detect: no Prometheus found among the cluster's services."},
		{engine.PrometheusSetting{Auto: true, InUseStatus: metrics.StatusForbidden}, "Auto detect: the current user may not read the Prometheus found (get services/proxy)."},
		{engine.PrometheusSetting{Address: "m/p:9090", InUseStatus: metrics.StatusNotServed}, "Set: m/p:9090 is not found or not a Prometheus."},
		{engine.PrometheusSetting{Address: "m/p:9090", Prefix: "/x", InUseStatus: metrics.StatusUnknown}, "Set: m/p:9090/x; asked when a chart needs it."},
		{engine.PrometheusSetting{Auto: true, InUse: "Thanos monitoring/thanos-query:http", InUseStatus: metrics.StatusAvailable}, "Auto detect: in use Thanos monitoring/thanos-query:http."},
	} {
		if got := prometheusInUseLine(c.set); got != c.want {
			t.Errorf("%+v: %q, want %q", c.set, got, c.want)
		}
	}
}

// TestRenderPrometheusSettings draws Settings › Metrics with the Prometheus's fields of a connected
// cluster, a service address set, in both themes, to COGPIT_PNG_DIR or a temporary directory.
func TestRenderPrometheusSettings(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		name := "metrics_prometheus_settings_" + string(mode) + ".png"
		t.Run(name, func(t *testing.T) {
			sh, _, _, _ := openPromSettings(t, mode, engine.PrometheusSetting{Context: "kind-dev", Address: "observability/thanos-query:9090",
				Prefix: "/prometheus", Format: "prometheus-operator", FormatLabel: metrics.QueryOperator.Label(), FilterEmptyContainers: true, Kept: true,
				InUse: "Thanos observability/thanos-query:9090/prometheus", InUseStatus: metrics.StatusAvailable})
			savePNG(t, sh.win, name)
		})
	}
}
