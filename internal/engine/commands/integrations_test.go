package commands_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// integrationsHarness connects "test" with cluster settings and preferences kept in the folders
// given (the same folders are the next start's).
func integrationsHarness(t *testing.T, settingsDir, prefsDir string) *harness {
	t.Helper()
	return newHarness(t, seed(), engine.Options{ClusterSettings: engine.NewClusterSettingsStore(settingsDir),
		Preferences: engine.NewPreferencesStore(prefsDir)})
}

// decideAll makes a context's choice of integrations "all on", as a cluster the user answered for:
// tests that read an integration in a session that keeps cluster settings (D-82).
func decideAll(t testing.TB, s *engine.Session, ctx string) {
	t.Helper()
	if _, err := s.ChangeClusterSettings(ctx, func(cs *engine.ClusterSettings) error {
		none := engine.IntegrationSet(0)
		cs.IntegrationsOff = &none
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// askedState is ui_state's question as an agent reads it.
type askedState struct {
	Question string   `json:"integrationsQuestion"`
	Step     string   `json:"integrationsStep"`
	LookFor  []string `json:"integrationsLookFor"`
}

// asked reads ui_state's question as an agent.
func asked(t *testing.T, h *harness) askedState {
	t.Helper()
	var u askedState
	h.must(t, engine.OriginExternalAgent, "ui.state", struct{}{}, &u)
	return u
}

// question is ui_state's integrationsQuestion as an agent reads it.
func question(t *testing.T, h *harness) string {
	t.Helper()
	return asked(t, h).Question
}

// integrationsOf reads cluster.settings.get's integrations as an agent: name → on, and whether
// the user chose them.
func integrationsOf(t *testing.T, h *harness, ctx string) (map[string]bool, bool) {
	t.Helper()
	var v engine.ClusterSettingsView
	h.must(t, engine.OriginExternalAgent, "cluster.settings.get", map[string]any{"context": ctx}, &v)
	on := map[string]bool{}
	for _, in := range v.Integrations {
		on[string(in.Name)] = in.On
	}
	if len(on) != len(engine.Integrations) {
		t.Fatalf("%d integrations listed", len(v.Integrations))
	}
	return on, v.IntegrationsDecided
}

// The question's lifecycle (D-76, D-82): asked at the first connect of a cluster without a choice,
// on the pre-choice, nothing on, as ui_state shows it; Check all looks for every integration; Later
// puts it off for the run (a reconnect does not ask again) and nothing is on meanwhile, the next
// start asks again; an answer decides the cluster, with saveAsDefault the default for new clusters
// too, which the next new cluster's Check all looks for; a decided cluster is not asked. The window
// steps and answers (H-126: agents may not); an agent reads the question and may put it off.
func TestIntegrationsQuestionLifecycle(t *testing.T) {
	settings, prefs := t.TempDir(), t.TempDir()
	h := integrationsHarness(t, settings, prefs)
	if a := asked(t, h); a.Question != "test" || a.Step != "preChoice" || len(a.LookFor) != 0 {
		t.Fatalf("question after the first connect: %+v", a)
	}
	on, decided := integrationsOf(t, h, "test")
	if decided || slices.Contains(boolValues(on), true) {
		t.Fatalf("before an answer: decided %v, %v", decided, on)
	}
	h.must(t, engine.OriginGUI, "cluster.integrations.step", map[string]any{"step": "checkAll"}, nil)
	if a := asked(t, h); a.Step != "checkAll" || len(a.LookFor) != len(engine.Integrations) {
		t.Errorf("Check all: %+v", a)
	}
	if on, _ := integrationsOf(t, h, "test"); slices.Contains(boolValues(on), false) {
		t.Errorf("Check all looks for: %v", on)
	}

	// Later: gone for this run, a reconnect included, nothing on; a second Later has nothing to put off.
	h.must(t, engine.OriginExternalAgent, "cluster.integrations.later", map[string]any{}, nil)
	if a := asked(t, h); a.Question != "" || a.Step != "" || len(a.LookFor) != 0 {
		t.Fatalf("after Later: %+v", a)
	}
	if on, _ := integrationsOf(t, h, "test"); slices.Contains(boolValues(on), true) {
		t.Errorf("put off, still on: %v", on)
	}
	if err := h.run(t, engine.OriginExternalAgent, "cluster.integrations.later", map[string]any{}, nil); !errors.Is(err, engine.ErrNoIntegrationsQuestion) &&
		!strings.Contains(errText(err), engine.ErrNoIntegrationsQuestion.Error()) {
		t.Errorf("Later twice: %v", err)
	}
	if err := h.s.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if q := question(t, h); q != "" {
		t.Errorf("asked again in the same run: %q", q)
	}

	// The next start asks again, until answered.
	h2 := integrationsHarness(t, settings, prefs)
	if q := question(t, h2); q != "test" {
		t.Fatalf("the next start: %q", q)
	}
	h2.must(t, engine.OriginGUI, "cluster.integrations.step", map[string]any{"step": "checkAll"}, nil)
	h2.must(t, engine.OriginGUI, "cluster.integrations.answer", map[string]any{"integrations": map[string]bool{"alertmanager": false}}, nil)
	if q := question(t, h2); q != "" {
		t.Errorf("after the answer: %q", q)
	}
	on, decided = integrationsOf(t, h2, "test")
	if !decided || on["alertmanager"] || !on["kyverno"] {
		t.Errorf("answered: decided %v, %v", decided, on)
	}
	if p, _ := h2.s.Preferences(); p.IntegrationsOff != 0 {
		t.Errorf("the default changed without saveAsDefault: %v", engine.IntegrationNames(p.IntegrationsOff))
	}

	// Another cluster, new: asked; answered as the window does, every switch, with saveAsDefault.
	if err := h2.s.Connect(context.Background(), "other"); err != nil {
		t.Fatal(err)
	}
	if q := question(t, h2); q != "other" {
		t.Fatalf("a second new cluster: %q", q)
	}
	h2.must(t, engine.OriginGUI, "cluster.integrations.answer", map[string]any{"context": "other",
		"integrations":  map[string]bool{"metrics-server": true, "prometheus": true, "alertmanager": true, "trivy": false, "kyverno": false},
		"saveAsDefault": true}, nil)
	var pr commands.PreferencesResult
	h2.must(t, engine.OriginExternalAgent, "preferences.get", struct{}{}, &pr)
	if got := engine.IntegrationNames(pr.Preferences.IntegrationsOff); !slices.Equal(got, []string{"trivy", "kyverno"}) {
		t.Errorf("the default saved: %v", got)
	}

	// The next new cluster's Check all looks for it (the cluster settings of a third start are new,
	// its preferences the same): nothing on before, Trivy and Kyverno unticked after.
	h3 := integrationsHarness(t, t.TempDir(), prefs)
	if q := question(t, h3); q != "test" {
		t.Fatalf("a new cluster after the default was saved: %q", q)
	}
	if on, decided := integrationsOf(t, h3, "test"); decided || slices.Contains(boolValues(on), true) {
		t.Errorf("before the pre-choice: decided %v, %v", decided, on)
	}
	h3.must(t, engine.OriginGUI, "cluster.integrations.step", map[string]any{"step": "checkAll"}, nil)
	on, decided = integrationsOf(t, h3, "test")
	if decided || on["trivy"] || on["kyverno"] || !on["alertmanager"] || !on["prometheus"] {
		t.Errorf("Check all with the default: decided %v, %v", decided, on)
	}
	// A decided cluster is not asked (the first start's folder: test decided by h2).
	h4 := integrationsHarness(t, settings, prefs)
	if q := question(t, h4); q != "" {
		t.Errorf("a decided cluster asked: %q", q)
	}
}

// The pre-choice's commands as the window drives them (D-82): Choose ticks one at a time, a tick
// before Check all or Choose is refused with the reason, Back looks for nothing, Don't use's Yes
// answers with every one off, and Ask again (an agent's too) brings the question back from the
// pre-choice, for a decided cluster or one put off; refused for a cluster asked already.
func TestIntegrationsConsentCommands(t *testing.T) {
	h := integrationsHarness(t, t.TempDir(), t.TempDir())
	if err := h.run(t, engine.OriginGUI, "cluster.integrations.lookFor", map[string]any{"integrations": map[string]bool{"trivy": true}}, nil); err == nil ||
		!strings.Contains(err.Error(), "Check all or Choose") {
		t.Errorf("a tick on the pre-choice: %v", err)
	}
	if err := h.run(t, engine.OriginGUI, "cluster.integrations.step", map[string]any{"step": "later"}, nil); err == nil ||
		!strings.Contains(err.Error(), "preChoice") {
		t.Errorf("an unknown step: %v", err)
	}
	if err := h.run(t, engine.OriginExternalAgent, "cluster.integrations.ask", map[string]any{}, nil); err == nil ||
		!strings.Contains(err.Error(), "asks about this cluster") {
		t.Errorf("Ask again while asked: %v", err)
	}
	h.must(t, engine.OriginGUI, "cluster.integrations.step", map[string]any{"step": "choose"}, nil)
	h.must(t, engine.OriginGUI, "cluster.integrations.lookFor", map[string]any{"integrations": map[string]bool{"metrics-server": true, "kyverno": true}}, nil)
	h.must(t, engine.OriginGUI, "cluster.integrations.lookFor", map[string]any{"integrations": map[string]bool{"kyverno": false}}, nil)
	if a := asked(t, h); a.Step != "choose" || !slices.Equal(a.LookFor, []string{"metrics-server"}) {
		t.Errorf("Choose, one ticked: %+v", a)
	}
	if on, _ := integrationsOf(t, h, "test"); !on["metrics-server"] || on["kyverno"] || on["prometheus"] {
		t.Errorf("looked for: %v", on)
	}
	h.must(t, engine.OriginGUI, "cluster.integrations.step", map[string]any{"step": "preChoice"}, nil)
	if a := asked(t, h); a.Step != "preChoice" || len(a.LookFor) != 0 {
		t.Errorf("back on the pre-choice: %+v", a)
	}

	// Don't use, then the warning's Yes: every integration off, the cluster decided.
	h.must(t, engine.OriginGUI, "cluster.integrations.step", map[string]any{"step": "dontUse"}, nil)
	off := map[string]bool{}
	for _, in := range engine.Integrations {
		off[string(in.Name)] = false
	}
	h.must(t, engine.OriginGUI, "cluster.integrations.answer", map[string]any{"integrations": off}, nil)
	on, decided := integrationsOf(t, h, "test")
	if !decided || slices.Contains(boolValues(on), true) || question(t, h) != "" {
		t.Fatalf("without integrations: decided %v, %v", decided, on)
	}

	// Ask again: forgotten, asked from the pre-choice.
	var v engine.ClusterSettingsView
	h.must(t, engine.OriginExternalAgent, "cluster.integrations.ask", map[string]any{}, &v)
	if v.IntegrationsDecided {
		t.Error("Ask again kept the choice")
	}
	if a := asked(t, h); a.Question != "test" || a.Step != "preChoice" {
		t.Errorf("asked again: %+v", a)
	}
	// Put off, then asked again within the run.
	h.must(t, engine.OriginExternalAgent, "cluster.integrations.later", map[string]any{}, nil)
	h.must(t, engine.OriginExternalAgent, "cluster.integrations.ask", map[string]any{}, nil)
	if q := question(t, h); q != "test" {
		t.Errorf("Ask again after Later: %q", q)
	}
}

// The demo cluster, and a session that keeps no cluster settings (the CLI's), are never asked.
func TestIntegrationsQuestionNotAsked(t *testing.T) {
	h := newHarness(t, seed(), engine.Options{})
	if q := question(t, h); q != "" {
		t.Errorf("asked without cluster settings: %q", q)
	}
	if err := h.run(t, engine.OriginExternalAgent, "cluster.integrations.ask", map[string]any{}, nil); err == nil {
		t.Error("Ask again without cluster settings was accepted")
	}
	d := newDemoHarness(t, engine.Options{ClusterSettings: engine.NewClusterSettingsStore(t.TempDir())})
	if q := question(t, d); q != "" {
		t.Errorf("the demo cluster asked: %q", q)
	}
	if err := d.run(t, engine.OriginExternalAgent, "cluster.integrations.ask", map[string]any{}, nil); err == nil ||
		!strings.Contains(err.Error(), "demo") {
		t.Errorf("Ask again for the demo cluster: %v", err)
	}
}

// cluster.settings.set's integrations and preferences.set's integrationDefaults: by name, unknown
// names refused with the known ones, the metrics switches shown as the Metrics source; not
// together with a metricsSource that names the same providers. The user switches both ways, an
// agent only off (TestIntegrationsOnAreTheUsers). A cluster without a choice starts from nothing on
// (D-82): only what is named is switched on.
func TestIntegrationSwitchCommands(t *testing.T) {
	h := integrationsHarness(t, t.TempDir(), t.TempDir())
	var v engine.ClusterSettingsView
	h.must(t, engine.OriginGUI, "cluster.settings.set", map[string]any{"integrations": map[string]bool{"metrics-server": true, "trivy": true}}, &v)
	if v.MetricsSource != "metrics-server" || !v.IntegrationsDecided {
		t.Errorf("after switching the Metrics Server on: source %q, decided %v", v.MetricsSource, v.IntegrationsDecided)
	}
	for _, in := range v.Integrations {
		if want := in.Name == engine.IntegrationMetricsServer || in.Name == engine.IntegrationTrivy; in.On != want {
			t.Errorf("%s on %v", in.Name, in.On)
		}
	}
	h.must(t, engine.OriginExternalAgent, "cluster.settings.set", map[string]any{"integrations": map[string]bool{"prometheus": false}}, &v)
	h.must(t, engine.OriginGUI, "cluster.settings.set", map[string]any{"integrations": map[string]bool{"prometheus": true, "alertmanager": true, "kyverno": true}}, &v)
	if v.MetricsSource != "auto" {
		t.Errorf("the user switched Prometheus on again: source %q", v.MetricsSource)
	}
	for _, in := range v.Integrations {
		if !in.On {
			t.Errorf("%s off", in.Name)
		}
	}
	err := h.run(t, engine.OriginExternalAgent, "cluster.settings.set", map[string]any{"integrations": map[string]bool{"falco": false}}, nil)
	if err == nil || !strings.Contains(err.Error(), "alertmanager") {
		t.Errorf("an unknown integration: %v", err)
	}
	err = h.run(t, engine.OriginGUI, "cluster.settings.set", map[string]any{"metricsSource": "none", "integrations": map[string]bool{"prometheus": true}}, nil)
	if err == nil {
		t.Error("metricsSource and the metrics integrations together were accepted")
	}

	var pr commands.PreferencesResult
	h.must(t, engine.OriginExternalAgent, "preferences.set", map[string]any{"integrationDefaults": map[string]bool{"alertmanager": false, "kyverno": false}}, &pr)
	h.must(t, engine.OriginGUI, "preferences.set", map[string]any{"integrationDefaults": map[string]bool{"kyverno": true}}, &pr)
	if got := engine.IntegrationNames(pr.Preferences.IntegrationsOff); !slices.Equal(got, []string{"alertmanager"}) {
		t.Errorf("the default for new clusters: %v", got)
	}
	if err := h.run(t, engine.OriginExternalAgent, "preferences.set", map[string]any{"integrationDefaults": map[string]bool{"falco": false}}, nil); err == nil {
		t.Error("an unknown integration in the default was accepted")
	}
}

// TestIntegrationsOnAreTheUsers (H-126, the owner's decision of 2026-10-19: "Nein"): only the user,
// in the window, answers a new cluster's integrations question or switches an integration on. The
// question's step, ticks and answer refuse agents and the CLI, and so does every switch of a cluster
// the window still asks about (it would answer the question); on a cluster the user chose for they
// may switch integrations off (the lead's decision, 2026-10-19), by name or as the Metrics source
// none, but never on: by name, by a Metrics source that names a provider, or as the default for new
// clusters; a change that switches one on and another off is refused as a whole. Also where the
// user allowed agent writes; nobody is asked, nothing is looked for. Later and Ask again stay theirs.
func TestIntegrationsOnAreTheUsers(t *testing.T) {
	settings, prefs := t.TempDir(), t.TempDir()
	h := integrationsHarness(t, settings, prefs)
	c := &confirmations{answer: true}
	h.s.SetConfirmer(c)
	agents := []engine.Origin{engine.OriginExternalAgent, engine.OriginBuiltInAgent, engine.OriginCLI}
	isRefused := func(err error) bool { return errors.Is(err, engine.ErrIntegrationsGUIOnly) }

	// The question's steps, ticks and answer, and every switch while the cluster has no choice.
	h.must(t, engine.OriginGUI, "cluster.integrations.step", map[string]any{"step": "choose"}, nil)
	h.must(t, engine.OriginGUI, "cluster.integrations.lookFor", map[string]any{"integrations": map[string]bool{"metrics-server": true}}, nil)
	undecided := func(when string) {
		t.Helper()
		for _, o := range agents {
			for id, p := range map[string]map[string]any{
				"cluster.integrations.step":    {"step": "checkAll"},
				"cluster.integrations.lookFor": {"integrations": map[string]bool{"kyverno": true}},
				"cluster.integrations.answer":  {"integrations": map[string]bool{"trivy": false}},
			} {
				err := h.run(t, o, id, p, nil)
				var ge *engine.GUIOnlyError
				if !errors.As(err, &ge) || !isRefused(err) {
					t.Errorf("%s: %s %s: %v, want a GUI-only refusal", when, o, id, err)
				}
			}
			for _, p := range []map[string]any{
				{"integrations": map[string]bool{"alertmanager": false}}, {"metricsSource": "none"},
				{"integrations": map[string]bool{"kyverno": true}},
			} {
				if err := h.run(t, o, "cluster.settings.set", p, nil); !isRefused(err) {
					t.Errorf("%s: %s switched %v on a cluster without a choice: %v", when, o, p, err)
				}
			}
		}
		if a := asked(t, h); a.Question != "test" || a.Step != "choose" || !slices.Equal(a.LookFor, []string{"metrics-server"}) {
			t.Errorf("%s: the question after the refusals: %+v", when, a)
		}
		if _, decided := integrationsOf(t, h, "test"); decided {
			t.Errorf("%s: a refused switch decided the cluster", when)
		}
	}
	undecided("agent writes not allowed")
	h.must(t, engine.OriginGUI, "cluster.settings.setAgentWrites", map[string]any{"allowed": true}, nil)
	undecided("agent writes allowed")

	// Later stays the agents'; the next start asks again, and the user answers: every one on.
	h.must(t, engine.OriginExternalAgent, "cluster.integrations.later", map[string]any{}, nil)
	h2 := integrationsHarness(t, settings, prefs)
	h2.s.SetConfirmer(c)
	h2.must(t, engine.OriginGUI, "cluster.integrations.step", map[string]any{"step": "checkAll"}, nil)
	h2.must(t, engine.OriginGUI, "cluster.integrations.answer", map[string]any{}, nil)
	if on, decided := integrationsOf(t, h2, "test"); !decided || slices.Contains(boolValues(on), false) {
		t.Fatalf("the user's answer: decided %v, %v", decided, on)
	}

	// On a cluster the user chose for: off by name and as the source none; never on.
	for _, o := range agents {
		for _, p := range []map[string]any{
			{"integrations": map[string]bool{"alertmanager": true}},
			{"integrations": map[string]bool{"kyverno": false, "trivy": true}},
			{"metricsSource": "auto"}, {"metricsSource": "prometheus"}, {"metricsSource": "metrics-server"},
		} {
			if err := h2.run(t, o, "cluster.settings.set", p, nil); !isRefused(err) {
				t.Errorf("%s switched %v on: %v", o, p, err)
			}
		}
		if err := h2.run(t, o, "preferences.set", map[string]any{"integrationDefaults": map[string]bool{"trivy": true}}, nil); !isRefused(err) {
			t.Errorf("%s switched a default on: %v", o, err)
		}
	}
	if on, _ := integrationsOf(t, h2, "test"); slices.Contains(boolValues(on), false) {
		t.Errorf("a refused switch changed the cluster: %v", on)
	}
	h2.must(t, engine.OriginExternalAgent, "cluster.settings.set", map[string]any{"integrations": map[string]bool{"kyverno": false, "trivy": false}}, nil)
	h2.must(t, engine.OriginBuiltInAgent, "cluster.settings.set", map[string]any{"metricsSource": "none"}, nil)
	h2.must(t, engine.OriginExternalAgent, "preferences.set", map[string]any{"integrationDefaults": map[string]bool{"kyverno": false}}, nil)
	on, _ := integrationsOf(t, h2, "test")
	if on["kyverno"] || on["trivy"] || on["metrics-server"] || on["prometheus"] || !on["alertmanager"] {
		t.Errorf("after the agents' off switches: %v", on)
	}
	if p, _ := h2.s.Preferences(); !slices.Equal(engine.IntegrationNames(p.IntegrationsOff), []string{"kyverno"}) {
		t.Errorf("the default after an agent's off: %v", engine.IntegrationNames(p.IntegrationsOff))
	}
	if len(c.all()) != 0 {
		t.Errorf("the user was asked: %+v", c.all())
	}

	// The tools say so: the question's commands are GUI only, the switches' fields name the rule.
	for _, id := range []string{"cluster.integrations.step", "cluster.integrations.lookFor", "cluster.integrations.answer"} {
		if cmd, _ := h.s.Registry().Get(id); !cmd.GUIOnly || !strings.Contains(cmd.ToolDescription(), "GUI only") {
			t.Errorf("%s is not described as GUI only", id)
		}
	}
	set, _ := h.s.Registry().Get("cluster.settings.set")
	for _, field := range []string{"integrations", "metricsSource"} {
		if d := set.Schema.Properties[field].Description; !strings.Contains(d, "Agents and the CLI may only") {
			t.Errorf("cluster_settings_set's %s does not say what agents may: %q", field, d)
		}
	}
}

func boolValues(m map[string]bool) []bool {
	out := make([]bool, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// newDemoHarness connects "test" as a demo cluster (Backend.Demo, Connection.Demo).
func newDemoHarness(t *testing.T, opts engine.Options) *harness {
	t.Helper()
	h := &harness{src: seed(), writer: &fakeWriter{}}
	opts.Connector = &fakeConnector{src: h.src, writer: h.writer, demo: true}
	h.s = engine.New(opts)
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	return h
}
