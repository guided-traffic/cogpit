package fyneui

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// integrationsFixture is what Cogpit found of kind-dev's integrations, a mix of every kind of
// answer: the Metrics Server found, Prometheus not found by name (its address to be set),
// Alertmanager found, no Trivy Operator, Kyverno's reports not readable. Synthetic, shaped like a
// real cluster's.
var integrationsFixture = []engine.IntegrationStatus{
	{Name: engine.IntegrationMetricsServer, State: engine.IntegrationFound},
	{Name: engine.IntegrationPrometheus, State: engine.IntegrationManual, Detail: "none found by name"},
	{Name: engine.IntegrationAlertmanager, State: engine.IntegrationFound, Where: "monitoring/kube-prometheus-alertmanager:9093"},
	{Name: engine.IntegrationTrivy, State: engine.IntegrationMissing, Detail: "aquasecurity.github.io is not served (no Trivy Operator)"},
	{Name: engine.IntegrationKyverno, State: engine.IntegrationForbidden, Detail: "the current user may not list policyreports"},
}

// integrationsFake is a fake connected to kind-dev that answers the integration commands as the
// engine does: cluster.settings.get with the fixture's statuses (a switch off says "off"), the
// switches applied to the UI state, the question's steps and ticks (D-82), an answer and Later
// ending the question, Ask again forgetting the choice. A session that asks before it looks: a
// cluster without a choice reads nothing but what its question ticks.
type integrationsFake struct {
	*fakeEngine
	mu       sync.Mutex
	statuses []engine.IntegrationStatus
}

func newIntegrationsFake(t *testing.T) *integrationsFake {
	t.Helper()
	f := connectedFake(t, 20)
	f.ui.Context, f.ui.Connection = "kind-dev", engine.Connection{State: engine.Connected}
	f.ui.Preferences = &engine.Preferences{}
	f.ui.IntegrationsAskFirst = true
	f.contexts = append(f.contexts, engine.ContextInfo{Name: "kind-dev"})
	inf := &integrationsFake{fakeEngine: f, statuses: slices.Clone(integrationsFixture)}
	f.handlers["cluster.settings.get"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p clusterSettingsContext
		_ = json.Unmarshal(normalize(inv.Params), &p)
		u := f.UIState()
		inf.mu.Lock()
		defer inf.mu.Unlock()
		v := engine.ClusterSettingsView{Context: p.Context, Name: p.Context, ContextName: p.Context, Connected: true,
			IntegrationsDecided: u.IntegrationsDecided(p.Context), MetricsSource: u.MetricsSourceOf(p.Context)}
		for _, st := range inf.statuses {
			in, _ := engine.IntegrationOf(st.Name)
			st.Label, st.Adds, st.On = in.Label, in.Adds, u.IntegrationOn(p.Context, st.Name)
			if !st.On {
				st.State, st.Where, st.Detail = engine.IntegrationOff, "", ""
			}
			v.Integrations = append(v.Integrations, st)
		}
		return v, nil
	}
	f.handlers["cluster.settings.set"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p struct {
			Context      string          `json:"context"`
			Integrations map[string]bool `json:"integrations"`
		}
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		inf.setIntegrations(p.Context, p.Integrations)
		return nil, nil
	}
	f.handlers["cluster.integrations.answer"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p struct {
			Context       string          `json:"context"`
			Integrations  map[string]bool `json:"integrations"`
			SaveAsDefault bool            `json:"saveAsDefault"`
		}
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		inf.setIntegrations(p.Context, p.Integrations)
		return nil, nil
	}
	f.handlers["cluster.integrations.later"] = func(context.Context, engine.Invocation) (any, error) {
		f.update(endQuestion)
		return nil, nil
	}
	f.handlers["cluster.integrations.step"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p struct {
			Step string `json:"step"`
		}
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			u.IntegrationsStep, u.IntegrationsLookFor = engine.IntegrationsStep(p.Step), 0
			if u.IntegrationsStep == engine.IntegrationsCheckAll {
				u.IntegrationsLookFor = engine.IntAll &^ u.Preferences.IntegrationsOff
			}
		})
		return nil, nil
	}
	f.handlers["cluster.integrations.lookFor"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p struct {
			Integrations map[string]bool `json:"integrations"`
		}
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		on, off, err := engine.ParseIntegrationSwitches(p.Integrations)
		if err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) { u.IntegrationsLookFor = (u.IntegrationsLookFor | on) &^ off })
		return nil, nil
	}
	f.handlers["cluster.integrations.ask"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p clusterSettingsContext
		_ = json.Unmarshal(normalize(inv.Params), &p)
		f.update(func(u *engine.UIState) {
			cs := u.ClusterSettings[p.Context]
			cs.IntegrationsOff = nil
			u.ClusterSettings = maps.Clone(u.ClusterSettings)
			u.ClusterSettings[p.Context] = cs
			if p.Context == u.Context {
				u.IntegrationsQuestion, u.IntegrationsStep, u.IntegrationsLookFor = p.Context, engine.IntegrationsPreChoice, 0
			}
		})
		return nil, nil
	}
	f.handlers["cluster.settings.open"] = func(context.Context, engine.Invocation) (any, error) { return nil, nil }
	// Alertmanager's address fields under its row: found by name, no address set.
	f.handlers["cluster.alertmanager"] = func(context.Context, engine.Invocation) (any, error) {
		return engine.AlertmanagerSetting{Context: "kind-dev", Auto: true, Kept: true, State: engine.AlertsFound,
			InUse: "monitoring/kube-prometheus-alertmanager:http-web"}, nil
	}
	return inf
}

// endQuestion drops the question, as the engine does once it is answered or put off.
func endQuestion(u *engine.UIState) {
	u.IntegrationsQuestion, u.IntegrationsStep, u.IntegrationsLookFor = "", "", 0
}

// offNow is what applies to a context now, as the engine's IntegrationsOffNow says.
func offNow(u *engine.UIState, ctx string) engine.IntegrationSet {
	var off engine.IntegrationSet
	for _, in := range engine.Integrations {
		if !u.IntegrationOn(ctx, in.Name) {
			off |= in.Set
		}
	}
	return off
}

// setIntegrations applies switches to a context's settings as the engine does, which ends the
// question about it.
func (inf *integrationsFake) setIntegrations(ctx string, switches map[string]bool) {
	on, off, _ := engine.ParseIntegrationSwitches(switches)
	inf.update(func(u *engine.UIState) {
		base := offNow(u, ctx)
		u.ClusterSettings = maps.Clone(u.ClusterSettings)
		if u.ClusterSettings == nil {
			u.ClusterSettings = map[string]engine.ClusterSettings{}
		}
		cs := u.ClusterSettings[ctx]
		cs.SetIntegrations(base, on, off)
		u.ClusterSettings[ctx] = cs
		if u.IntegrationsQuestion == ctx {
			endQuestion(u)
		}
	})
}

// decide makes kind-dev's choice every integration on, as answered.
func (inf *integrationsFake) decide() {
	inf.update(func(u *engine.UIState) {
		none := engine.IntegrationSet(0)
		u.ClusterSettings = map[string]engine.ClusterSettings{"kind-dev": {IntegrationsOff: &none}}
	})
}

// integrationRowOf is the section's row of an integration.
func integrationRowOf(t *testing.T, rows []*integrationRow, name engine.Integration) *integrationRow {
	t.Helper()
	for _, r := range rows {
		if r.name == name {
			return r
		}
	}
	t.Fatalf("no row for %s", name)
	return nil
}

// openIntegrationsSection shows kind-dev's Settings › Integrations, every integration on as the
// user answered, and waits for the statuses.
func openIntegrationsSection(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *integrationsFake) {
	t.Helper()
	inf := newIntegrationsFake(t)
	inf.decide()
	sh, w, q := newTestShell(t, inf.fakeEngine, Options{Theme: mode})
	inf.update(func(u *engine.UIState) {
		u.Settings = &engine.SettingsPage{Page: engine.SettingsCluster, Context: "kind-dev", Section: engine.SectionIntegrations}
	})
	q.until(t, "the integrations' statuses", func() bool {
		rows := sh.settings.integrationRows
		return len(rows) == len(engine.Integrations) && rows[len(rows)-1].state == engine.IntegrationForbidden
	})
	return sh, w, q, inf
}

// Cluster Settings › Integrations (D-76, I1): a row per integration, its switch, whether Cogpit
// found it automatically (and where), needs a manual configuration, or it is not in the cluster, in
// the status colours; Prometheus's link to where its address is set; a switch sets the cluster's
// own choice at once; a detection that settled reads the statuses again; Ask again (D-82, WF4)
// forgets the choice: the note says nothing is looked for, every row off.
func TestIntegrationsSection(t *testing.T) {
	sh, _, q, inf := openIntegrationsSection(t, ThemeDark)
	p := sh.settings
	want := map[engine.Integration]struct {
		text string
		c    fyne.ThemeColorName
	}{
		engine.IntegrationMetricsServer: {"Found automatically", colorMuted},
		engine.IntegrationPrometheus:    {"Needs manual configuration: none found by name", colorStatusWarning},
		engine.IntegrationAlertmanager:  {"Found automatically: monitoring/kube-prometheus-alertmanager:9093", colorMuted},
		engine.IntegrationTrivy:         {"Not in this cluster: aquasecurity.github.io is not served (no Trivy Operator)", colorMuted},
		engine.IntegrationKyverno:       {"No permission: the current user may not list policyreports", colorStatusError},
	}
	for name, w := range want {
		r := integrationRowOf(t, p.integrationRows, name)
		if r.status.text != w.text || r.status.color != w.c || !r.check.Checked {
			t.Errorf("%s: %q in %s, on %v", name, r.status.text, w.c, r.check.Checked)
		}
		// Quiet when healthy (Dark Cockpit): a found integration is muted words and a mint dot.
		if name == engine.IntegrationMetricsServer && (r.dot.fill != colorStatusOK || !r.dot.Visible()) {
			t.Errorf("%s: the status dot is %s, visible %v", name, r.dot.fill, r.dot.Visible())
		}
		if in, _ := engine.IntegrationOf(name); r.check.Text != in.Label || r.adds.text != in.Adds {
			t.Errorf("%s: label %q, adds %q", name, r.check.Text, r.adds.text)
		}
	}
	prom := integrationRowOf(t, p.integrationRows, engine.IntegrationPrometheus)
	if prom.link == nil || !prom.link.Visible() || prom.link.text != "Set its address…" {
		t.Fatal("no link to the Prometheus address on a manual Prometheus")
	}
	if l := integrationRowOf(t, p.integrationRows, engine.IntegrationKyverno).link; l == nil || l.text != integrationDetailsLink {
		t.Error("no link to Kyverno's section")
	}
	if note := p.controls["integrationsNote"]; note.Visible() {
		t.Error("the note of an undecided cluster shows on a decided one")
	}
	if ask := p.controls["integrationsAsk"].(*textButton); !ask.Visible() || ask.Text != integrationsAskAgain {
		t.Errorf("Ask again: %q, shown %v", ask.Text, ask.Visible())
	}

	// The user switches Alertmanager off: cluster.settings.set at once.
	test.Tap(integrationRowOf(t, p.integrationRows, engine.IntegrationAlertmanager).check)
	q.until(t, "Alertmanager off", func() bool {
		r := integrationRowOf(t, p.integrationRows, engine.IntegrationAlertmanager)
		return !r.check.Checked && r.state == engine.IntegrationOff
	})
	calls := inf.callsOf("cluster.settings.set")
	if len(calls) != 1 || string(normalize(calls[0].Params)) != `{"context":"kind-dev","integrations":{"alertmanager":false}}` {
		t.Fatalf("set: %d calls, %s", len(calls), calls[0].Params)
	}

	// The link opens Prometheus's own section, where its address is set (D-87).
	test.Tap(prom.link)
	q.until(t, "the Prometheus section asked for", func() bool { return len(inf.callsOf("cluster.settings.open")) == 1 })
	if c := inf.callsOf("cluster.settings.open")[0]; string(normalize(c.Params)) != `{"context":"kind-dev","section":"prometheus"}` {
		t.Errorf("open: %s", c.Params)
	}

	// A detection settled (EventIntegrations): read again, the new status shows.
	inf.mu.Lock()
	inf.statuses[1] = engine.IntegrationStatus{Name: engine.IntegrationPrometheus, State: engine.IntegrationFound, Where: "monitoring/prometheus-operated:9090"}
	inf.mu.Unlock()
	inf.emit(engine.Event{Type: engine.EventIntegrations, View: "kind-dev"})
	q.until(t, "Prometheus found", func() bool {
		r := integrationRowOf(t, p.integrationRows, engine.IntegrationPrometheus)
		return r.status.text == "Found automatically: monitoring/prometheus-operated:9090" && r.link.text == integrationDetailsLink
	})

	// Ask again: cluster.integrations.ask; the choice forgotten, nothing on, the note, the question.
	test.Tap(p.controls["integrationsAsk"].(*textButton))
	q.until(t, "asked again", func() bool {
		for _, r := range p.integrationRows {
			if r.check.Checked {
				return false
			}
		}
		return p.controls["integrationsNote"].Visible() && sh.intq.d != nil
	})
	if calls := inf.callsOf("cluster.integrations.ask"); len(calls) != 1 || string(normalize(calls[0].Params)) != `{"context":"kind-dev"}` {
		t.Errorf("ask: %d calls", len(calls))
	}
	if text := p.controls["integrationsNote"].(*textLine).text; text != integrationsUndecided {
		t.Errorf("the note: %q", text)
	}
}

// Each integration has a section of its own (D-87, the owner's WF22): under a divider and the
// Integrations heading in the page's list; its switch, its status (a found Alertmanager's Service as
// links), and the details its own file adds: Prometheus's fields, Alertmanager's address, none for
// Kyverno. A switch there sets the cluster's choice at once.
func TestIntegrationSections(t *testing.T) {
	inf := newIntegrationsFake(t)
	inf.decide()
	inf.handlers["cluster.prometheus"] = func(context.Context, engine.Invocation) (any, error) {
		return engine.PrometheusSetting{Context: "kind-dev", Auto: true, Format: autoSetting, Kept: true}, nil
	}
	sh, _, q := newTestShell(t, inf.fakeEngine, Options{})
	p := sh.settings
	open := func(section string, status string) *integrationRow {
		t.Helper()
		inf.update(func(u *engine.UIState) {
			u.Settings = &engine.SettingsPage{Page: engine.SettingsCluster, Context: "kind-dev", Section: section}
		})
		q.until(t, section+"'s section", func() bool {
			return p.shown.Section == section && len(p.integrationRows) == 1 && p.integrationRows[0].status.text == status
		})
		return p.integrationRows[0]
	}

	r := open("kyverno", "No permission: the current user may not list policyreports")
	if r.check.Text != integrationSwitchLabel || !r.check.Checked || r.link != nil {
		t.Errorf("Kyverno's switch %q, on %v, link %v", r.check.Text, r.check.Checked, r.link != nil)
	}
	if p.prom != nil || p.controls[amFieldsControl] != nil {
		t.Error("another integration's details on Kyverno's section")
	}
	// The list: the integrations after Agents, under the heading, each by its name.
	secs := engine.ClusterSettingsSections
	if ia, ii := slices.Index(secs, engine.SectionAgents), slices.Index(secs, engine.SectionIntegrations); ia < 0 || ii != ia+1 ||
		!slices.Equal(secs[ii+1:], []string{"metrics-server", "prometheus", "alertmanager", "trivy", "kyverno"}) {
		t.Errorf("sections %q", secs)
	}
	for in, label := range integrationNavLabels {
		if b := p.navItems[string(in)]; b == nil || b.Text != label {
			t.Errorf("%s: no list item %q", in, label)
		}
	}
	if b := p.navItems[engine.SectionIntegrations]; b == nil || b.Text != "" || !within(p.nav, b) {
		t.Error("the Integrations heading is no item of the list")
	} else if !slices.ContainsFunc(findAll[*bracketLabel](p.nav), func(l *bracketLabel) bool { return l.text == "integrations" }) {
		t.Error("the heading's label")
	}
	if b := p.navItems["kyverno"]; b.Importance != widget.HighImportance {
		t.Error("the shown section is not marked")
	}
	test.Tap(r.check)
	q.until(t, "Kyverno off", func() bool { return len(inf.callsOf("cluster.settings.set")) == 1 })
	if c := inf.callsOf("cluster.settings.set")[0]; string(normalize(c.Params)) != `{"context":"kind-dev","integrations":{"kyverno":false}}` {
		t.Errorf("set %s", c.Params)
	}

	open("prometheus", "Needs manual configuration: none found by name")
	q.until(t, "the Prometheus fields", func() bool {
		return p.prom != nil && p.prom.enabled &&
			p.prom.line.text == "Auto detect: Cogpit looks for it among the cluster's services when a chart needs it."
	})

	r = open("alertmanager", "Found automatically: monitoring/kube-prometheus-alertmanager:9093")
	if _, ok := p.controls[amFieldsControl].(*amFields); !ok {
		t.Error("no Alertmanager address on its section")
	}
	if links := r.status.links(); len(links) != 2 || links["monitoring"] == nil || links["kube-prometheus-alertmanager"] == nil {
		t.Errorf("Alertmanager's place links %v", links)
	}
}

// navLabels are the settings page's list as text: each item's label, "—" for the divider, the
// integrations' heading as it reads.
func navLabels(p *settingsPage) []string {
	var out []string
	for _, o := range p.nav.Objects {
		switch x := o.(type) {
		case *navItem:
			out = append(out, x.Text)
		case *fyne.Container:
			if lines := findAll[*bracketLabel](x); len(lines) > 0 {
				out = append(out, lines[0].text)
			} else {
				out = append(out, "—")
			}
		}
	}
	return out
}

// within reports whether o is in the tree of containers under root.
func within(root, o fyne.CanvasObject) bool {
	if root == o {
		return true
	}
	if c, ok := root.(*fyne.Container); ok {
		return slices.ContainsFunc(c.Objects, func(x fyne.CanvasObject) bool { return within(x, o) })
	}
	return false
}

func TestRenderIntegrationsSection(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		t.Run(string(mode), func(t *testing.T) {
			_, w, _, _ := openIntegrationsSection(t, mode)
			savePNG(t, w, "cluster_settings_integrations_"+string(mode)+".png")
		})
	}
}

// Preferences › Kubernetes › Integrations of new clusters: the default, switched by the user and
// shown as an agent sets it.
func TestNewClusterIntegrationsPreference(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "kubernetes"})
	q.until(t, "the Kubernetes section", func() bool { return sh.settings.controls[integrationDefaultsSetting] != nil })
	checks := sh.settings.controls[integrationDefaultsSetting].(*fyne.Container).Objects
	if len(checks) != len(engine.Integrations) {
		t.Fatalf("%d checks", len(checks))
	}
	for _, o := range checks {
		if !o.(*widget.Check).Checked {
			t.Errorf("%s off by default", o.(*widget.Check).Text)
		}
	}
	test.Tap(checks[2].(*widget.Check)) // Alertmanager
	q.until(t, "Alertmanager off by default", func() bool {
		p, _ := s.Preferences()
		return p.IntegrationsOff == engine.IntAlertmanager
	})
	mustDispatch(t, s, "preferences.set", map[string]any{"integrationDefaults": map[string]bool{"kyverno": false}})
	q.until(t, "the agent's change shown", func() bool { return !checks[4].(*widget.Check).Checked && !checks[2].(*widget.Check).Checked })
}

// The section over the real engine and the demo cluster: the metrics integrations' statuses from
// the hub (the demo's metrics its own: no Prometheus), the demo never asked; a switch is the
// cluster's choice and the Metrics source follows.
func TestIntegrationsSectionOnTheDemo(t *testing.T) {
	s, sh, _, until := demoSettingsShell(t)
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": engine.DemoContext, "section": "integrations"})
	until("the metrics integrations' statuses", func() bool {
		rows := sh.settings.integrationRows
		return len(rows) == len(engine.Integrations) &&
			integrationRowOf(t, rows, engine.IntegrationMetricsServer).status.text == "Found automatically" &&
			integrationRowOf(t, rows, engine.IntegrationPrometheus).status.text == "Not in this cluster: the demo cluster's metrics are its own"
	})
	if q := s.UIState().IntegrationsQuestion; q != "" || sh.intq.d != nil {
		t.Errorf("the demo cluster asked: %q", q)
	}
	if sh.settings.controls["integrationsAskRow"].Visible() {
		t.Error("Ask again on the demo cluster, which is never asked")
	}
	test.Tap(integrationRowOf(t, sh.settings.integrationRows, engine.IntegrationMetricsServer).check)
	until("the Metrics Server off", func() bool {
		u := s.UIState()
		return u.IntegrationsDecided(engine.DemoContext) && u.MetricsSourceOf(engine.DemoContext) == "prometheus" &&
			integrationRowOf(t, sh.settings.integrationRows, engine.IntegrationMetricsServer).state == engine.IntegrationOff
	})
}
