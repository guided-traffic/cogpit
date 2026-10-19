package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// openIntegrationsQuestion shows the question about kind-dev on its pre-choice, with defaultsOff
// off by default for new clusters and Kyverno's detection not settled yet.
func openIntegrationsQuestion(t *testing.T, mode ThemeMode, defaultsOff engine.IntegrationSet) (*shell, fyne.Window, *mainQueue, *integrationsFake) {
	t.Helper()
	inf := newIntegrationsFake(t)
	inf.ui.Preferences = &engine.Preferences{IntegrationsOff: defaultsOff}
	inf.mu.Lock()
	inf.statuses[4] = engine.IntegrationStatus{Name: engine.IntegrationKyverno, State: engine.IntegrationChecking}
	inf.mu.Unlock()
	sh, w, q := newTestShell(t, inf.fakeEngine, Options{Theme: mode})
	inf.update(func(u *engine.UIState) {
		u.IntegrationsQuestion, u.IntegrationsStep = "kind-dev", engine.IntegrationsPreChoice
	})
	q.until(t, "the pre-choice", func() bool { return sh.intq.d != nil && len(sh.intq.choices) == 3 })
	return sh, w, q, inf
}

// stepCall is the step a cluster.integrations.step call asked for.
func stepCalls(inf *integrationsFake) []string {
	var out []string
	for _, c := range inf.callsOf("cluster.integrations.step") {
		out = append(out, string(normalize(c.Params)))
	}
	return out
}

// The pre-choice before anything is looked for (D-82, the owner's WF5): three choices, the first
// naming the default's integrations while it switches some off; no status is read. Choose shows
// the rows unticked, "Not looked for", and a tick looks for that one; Back returns to the
// pre-choice; Check the default integrations ticks what the default keeps on, the statuses live;
// Apply answers with the ticks and the checkbox, and the dialog closes when the engine drops the
// question.
func TestIntegrationsQuestionPreChoice(t *testing.T) {
	sh, _, q, inf := openIntegrationsQuestion(t, ThemeDark, engine.IntTrivy)
	iq := sh.intq
	labels := []string{iq.choices[0].Text, iq.choices[1].Text, iq.choices[2].Text}
	if labels[0] != integrationsAskDefault || labels[1] != integrationsAskChoose || labels[2] != integrationsAskNone {
		t.Errorf("the choices: %q", labels)
	}
	if text, hint := iq.checkAllText(); text != integrationsAskDefault ||
		hint != "Looks for Kubernetes Metrics Server, Prometheus, Alertmanager and Kyverno now, your default for new clusters; untick what you don't want." {
		t.Errorf("the default's line: %q", hint)
	}
	if len(iq.rows) != 0 || len(inf.callsOf("cluster.settings.get")) != 0 {
		t.Errorf("the pre-choice shows %d rows, read the statuses %d times", len(iq.rows), len(inf.callsOf("cluster.settings.get")))
	}

	// Choose: every row unticked, nothing looked for; a tick looks for that one.
	test.Tap(iq.choices[1])
	q.until(t, "the rows of Choose", func() bool { return iq.page == engine.IntegrationsChoose && len(iq.rows) == len(engine.Integrations) })
	for _, r := range iq.rows {
		if r.check.Checked || r.status.text != integrationsAskUnticked {
			t.Errorf("%s on Choose: ticked %v, %q", r.name, r.check.Checked, r.status.text)
		}
	}
	prom := integrationRowOf(t, iq.rows, engine.IntegrationPrometheus)
	test.Tap(prom.check)
	q.until(t, "Prometheus looked for", func() bool {
		return prom.check.Checked && prom.status.text == "Needs manual configuration: none found by name"
	})
	if calls := inf.callsOf("cluster.integrations.lookFor"); len(calls) != 1 ||
		string(normalize(calls[0].Params)) != `{"context":"kind-dev","integrations":{"prometheus":true}}` {
		t.Errorf("lookFor: %d calls", len(calls))
	}
	test.Tap(iq.back)
	q.until(t, "back on the pre-choice", func() bool { return iq.page == engine.IntegrationsPreChoice && len(iq.choices) == 3 })

	// Check the default integrations: all but Trivy ticked, the statuses live.
	test.Tap(iq.choices[0])
	q.until(t, "the rows of Check all", func() bool {
		return iq.page == engine.IntegrationsCheckAll && len(iq.rows) == len(engine.Integrations) &&
			integrationRowOf(t, iq.rows, engine.IntegrationAlertmanager).state == engine.IntegrationFound
	})
	for _, r := range iq.rows {
		if want := r.name != engine.IntegrationTrivy; r.check.Checked != want {
			t.Errorf("%s ticked %v", r.name, r.check.Checked)
		}
	}
	if kyv := integrationRowOf(t, iq.rows, engine.IntegrationKyverno); kyv.status.text != "Checking…" || kyv.status.color != colorStatusPending {
		t.Errorf("Kyverno: %q", kyv.status.text)
	}
	if trivy := integrationRowOf(t, iq.rows, engine.IntegrationTrivy); trivy.status.text != integrationsAskUnticked {
		t.Errorf("Trivy, off by default: %q", trivy.status.text)
	}
	if iq.save.Checked || iq.save.Text != integrationsAskSave {
		t.Errorf("the checkbox: %q, %v", iq.save.Text, iq.save.Checked)
	}
	inf.mu.Lock()
	inf.statuses[4] = engine.IntegrationStatus{Name: engine.IntegrationKyverno, State: engine.IntegrationFound, Where: "wgpolicyk8s.io/v1alpha2"}
	inf.mu.Unlock()
	inf.emit(engine.Event{Type: engine.EventIntegrations, View: "kind-dev"})
	q.until(t, "Kyverno found", func() bool {
		return integrationRowOf(t, iq.rows, engine.IntegrationKyverno).status.text == "Found automatically: wgpolicyk8s.io/v1alpha2"
	})
	if got := stepCalls(inf); len(got) != 3 || got[0] != `{"context":"kind-dev","step":"choose"}` ||
		got[1] != `{"context":"kind-dev","step":"preChoice"}` || got[2] != `{"context":"kind-dev","step":"checkAll"}` {
		t.Errorf("steps: %q", got)
	}

	test.Tap(integrationRowOf(t, iq.rows, engine.IntegrationAlertmanager).check)
	test.Tap(iq.save)
	test.Tap(iq.apply)
	q.until(t, "the dialog closed", func() bool { return iq.d == nil && iq.ctx == "" })
	calls := inf.callsOf("cluster.integrations.answer")
	want := `{"context":"kind-dev","integrations":{"alertmanager":false,"kyverno":true,"metrics-server":true,"prometheus":true,"trivy":false},"saveAsDefault":true}`
	if len(calls) != 1 || string(normalize(calls[0].Params)) != want {
		t.Fatalf("answer: %d calls, %s", len(calls), calls[0].Params)
	}
	if len(inf.callsOf("cluster.integrations.later")) != 0 || len(inf.callsOf("cluster.settings.set")) != 0 {
		t.Error("closing after the answer put it off, or a setting changed before Apply")
	}
}

// Don't use integrations (D-82): a warning over the pre-choice that names what goes and what
// stays; No goes back to the pre-choice, Yes answers with every integration off.
func TestIntegrationsQuestionDontUse(t *testing.T) {
	sh, _, q, inf := openIntegrationsQuestion(t, ThemeDark, 0)
	iq := sh.intq
	if iq.choices[0].Text != integrationsAskAll {
		t.Errorf("the first choice without a default of its own: %q", iq.choices[0].Text)
	}
	test.Tap(iq.choices[2])
	q.until(t, "the warning", func() bool { return iq.warn != nil })
	d := iq.d
	test.Tap(iq.no)
	q.until(t, "back on the pre-choice", func() bool { return iq.warn == nil && sh.ui.IntegrationsStep == engine.IntegrationsPreChoice })
	if iq.d != d || iq.d == nil {
		t.Error("No closed the question")
	}
	test.Tap(iq.choices[2])
	q.until(t, "the warning again", func() bool { return iq.warn != nil })
	test.Tap(iq.yes)
	q.until(t, "answered without integrations", func() bool { return iq.d == nil && iq.warn == nil })
	calls := inf.callsOf("cluster.integrations.answer")
	want := `{"context":"kind-dev","integrations":{"alertmanager":false,"kyverno":false,"metrics-server":false,"prometheus":false,"trivy":false},"saveAsDefault":false}`
	if len(calls) != 1 || string(normalize(calls[0].Params)) != want {
		t.Fatalf("answer: %d calls, %s", len(calls), calls[0].Params)
	}
	if len(inf.callsOf("cluster.integrations.later")) != 0 {
		t.Error("the answer put it off")
	}
}

// Later puts it off; a choice made elsewhere (Cluster Settings › Integrations) closes the open
// dialog without a Later; a step the engine took turns the dialog's page: the dialog follows the
// engine's state, whoever changed it. (Until H-126, 2026-10-19, an agent could step and answer; the
// test keeps its name.)
func TestIntegrationsQuestionLaterAndAgent(t *testing.T) {
	sh, _, q, inf := openIntegrationsQuestion(t, ThemeLight, 0)
	test.Tap(sh.intq.later)
	q.until(t, "put off", func() bool { return sh.intq.d == nil })
	if n := len(inf.callsOf("cluster.integrations.later")); n != 1 {
		t.Fatalf("%d Later calls", n)
	}

	inf.update(func(u *engine.UIState) {
		u.IntegrationsQuestion, u.IntegrationsStep = "kind-dev", engine.IntegrationsPreChoice
	})
	q.until(t, "asked again", func() bool { return sh.intq.d != nil })
	inf.update(func(u *engine.UIState) { // the engine took Check all
		u.IntegrationsStep, u.IntegrationsLookFor = engine.IntegrationsCheckAll, engine.IntAll
	})
	q.until(t, "the engine's Check all", func() bool { return sh.intq.page == engine.IntegrationsCheckAll && len(sh.intq.rows) == 5 })
	inf.setIntegrations("kind-dev", map[string]bool{"trivy": false}) // chosen in Cluster Settings › Integrations
	q.until(t, "closed by a choice made elsewhere", func() bool { return sh.intq.d == nil })
	if n := len(inf.callsOf("cluster.integrations.later")); n != 1 {
		t.Errorf("%d Later calls after a choice made elsewhere", n)
	}
}

// Cluster Settings › Metrics of a cluster without a choice of integrations (D-82): under its two
// sources (D-87) a line says nothing is asked until the user chooses. A metricsSource (Automatic)
// from the window's command is a choice: the line goes; the other integrations stay off.
func TestMetricsSourceLineUndecided(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "metrics"})
	undecided := func() bool {
		l, ok := sh.settings.controls["metricsSourceUndecided"].(*textLine)
		return ok && l.Visible() && l.text == metricsSourceUndecided
	}
	q.until(t, "the undecided line", func() bool { return sh.settings.view != nil && undecided() })
	for _, name := range metricsSourceIntegrations {
		if sh.settings.controls["metricsSource:"+string(name)] == nil {
			t.Errorf("no source row for %s", name)
		}
	}
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "metricsSource": "auto"})
	q.until(t, "Automatic chosen", func() bool { return sh.settings.controls["metricsSourceUndecided"] != nil && !undecided() })
	if u := s.UIState(); !u.IntegrationsDecided("kind-dev") || u.IntegrationOn("kind-dev", engine.IntegrationTrivy) {
		t.Errorf("Automatic on an undecided cluster: decided %v, Trivy on %v", u.IntegrationsDecided("kind-dev"),
			u.IntegrationOn("kind-dev", engine.IntegrationTrivy))
	}
}

// The pre-choice, the rows of Choose with the Metrics Server ticked and found and the others not
// looked for, and Don't use's warning, dark and light.
func TestRenderIntegrationsQuestion(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		t.Run(string(mode), func(t *testing.T) {
			_, w, _, _ := openIntegrationsQuestion(t, mode, 0)
			savePNG(t, w, "integrations_question_"+string(mode)+".png")
		})
		t.Run(string(mode)+"/choose", func(t *testing.T) {
			sh, w, q, inf := openIntegrationsQuestion(t, mode, 0)
			inf.update(func(u *engine.UIState) {
				u.IntegrationsStep, u.IntegrationsLookFor = engine.IntegrationsChoose, engine.IntMetricsServer
			})
			q.until(t, "the Metrics Server found", func() bool {
				return sh.intq.page == engine.IntegrationsChoose && len(sh.intq.rows) == 5 &&
					integrationRowOf(t, sh.intq.rows, engine.IntegrationMetricsServer).state == engine.IntegrationFound
			})
			savePNG(t, w, "integrations_question_choose_"+string(mode)+".png")
		})
		t.Run(string(mode)+"/warning", func(t *testing.T) {
			sh, w, q, inf := openIntegrationsQuestion(t, mode, 0)
			inf.update(func(u *engine.UIState) { u.IntegrationsStep = engine.IntegrationsDontUse })
			q.until(t, "the warning", func() bool { return sh.intq.warn != nil })
			savePNG(t, w, "integrations_question_warning_"+string(mode)+".png")
		})
	}
}
