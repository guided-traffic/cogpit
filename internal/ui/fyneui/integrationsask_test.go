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
// naming the default's integrations; no status is read. Choose shows the rows unticked, "Not looked
// for", and a tick looks for that one, the statuses live; Back returns to the pre-choice; Apply
// answers with the ticks and the checkbox, and the dialog closes when the engine drops the
// question. Use my default integrations answers at once with the default (the owner's QW4), with no
// page of rows.
func TestIntegrationsQuestionPreChoice(t *testing.T) {
	sh, _, q, inf := openIntegrationsQuestion(t, ThemeDark, engine.IntTrivy)
	iq := sh.intq
	labels := []string{iq.choices[0].Text, iq.choices[1].Text, iq.choices[2].Text}
	if labels[0] != integrationsAskDefault || labels[1] != integrationsAskChoose || labels[2] != integrationsAskNone {
		t.Errorf("the choices: %q", labels)
	}
	if hint := iq.defaultsHint(); hint != "Turns on Kubernetes Metrics Server, Prometheus, Alertmanager and Kyverno now, as Preferences › Kubernetes says for new clusters." {
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
	// The statuses live: Kyverno, ticked, is checked and then found.
	kyv := integrationRowOf(t, iq.rows, engine.IntegrationKyverno)
	test.Tap(kyv.check)
	q.until(t, "Kyverno looked for", func() bool { return kyv.check.Checked && kyv.status.text == "Checking…" })
	if kyv.status.color != colorMuted || kyv.dot.fill != colorStatusPending {
		t.Errorf("Kyverno checking: %q", kyv.status.text)
	}
	if trivy := integrationRowOf(t, iq.rows, engine.IntegrationTrivy); trivy.status.text != integrationsAskUnticked {
		t.Errorf("Trivy, unticked: %q", trivy.status.text)
	}
	if iq.save.Checked || iq.save.Text != integrationsAskSave {
		t.Errorf("the checkbox: %q, %v", iq.save.Text, iq.save.Checked)
	}
	inf.mu.Lock()
	inf.statuses[4] = engine.IntegrationStatus{Name: engine.IntegrationKyverno, State: engine.IntegrationFound, Where: "wgpolicyk8s.io/v1alpha2"}
	inf.mu.Unlock()
	inf.emit(engine.Event{Type: engine.EventIntegrations, View: "kind-dev"})
	q.until(t, "Kyverno found", func() bool { return kyv.status.text == "Found automatically: wgpolicyk8s.io/v1alpha2" })

	test.Tap(iq.back)
	q.until(t, "back on the pre-choice", func() bool { return iq.page == engine.IntegrationsPreChoice && len(iq.choices) == 3 })
	test.Tap(iq.choices[1])
	q.until(t, "the rows of Choose again", func() bool { return iq.page == engine.IntegrationsChoose && len(iq.rows) == len(engine.Integrations) })
	for _, n := range []engine.Integration{engine.IntegrationMetricsServer, engine.IntegrationKyverno} {
		test.Tap(integrationRowOf(t, iq.rows, n).check)
	}
	test.Tap(iq.save)
	test.Tap(iq.apply)
	q.until(t, "the dialog closed", func() bool { return iq.d == nil && iq.ctx == "" })
	if got := stepCalls(inf); len(got) != 3 || got[0] != `{"context":"kind-dev","step":"choose"}` ||
		got[1] != `{"context":"kind-dev","step":"preChoice"}` || got[2] != `{"context":"kind-dev","step":"choose"}` {
		t.Errorf("steps: %q", got)
	}
	calls := inf.callsOf("cluster.integrations.answer")
	want := `{"context":"kind-dev","integrations":{"alertmanager":false,"kyverno":true,"metrics-server":true,"prometheus":false,"trivy":false},"saveAsDefault":true}`
	if len(calls) != 1 || string(normalize(calls[0].Params)) != want {
		t.Fatalf("answer: %d calls, %s", len(calls), calls[0].Params)
	}
	if len(inf.callsOf("cluster.integrations.later")) != 0 || len(inf.callsOf("cluster.settings.set")) != 0 {
		t.Error("closing after the answer put it off, or a setting changed before Apply")
	}
}

// Use my default integrations (the owner's QW4): it answers at once with the default for new
// clusters, every integration it keeps on switched on and the others off, without a page of rows
// and without saving a default; the dialog closes when the engine drops the question.
func TestIntegrationsQuestionUseDefaults(t *testing.T) {
	sh, _, q, inf := openIntegrationsQuestion(t, ThemeDark, engine.IntTrivy|engine.IntKyverno)
	iq := sh.intq
	test.Tap(iq.choices[0])
	q.until(t, "the dialog closed", func() bool { return iq.d == nil && iq.ctx == "" })
	if n := len(stepCalls(inf)); n != 0 || len(iq.rows) != 0 {
		t.Errorf("%d steps, %d rows: a page of rows came between", n, len(iq.rows))
	}
	calls := inf.callsOf("cluster.integrations.answer")
	want := `{"context":"kind-dev","integrations":{"alertmanager":true,"kyverno":false,"metrics-server":true,"prometheus":true,"trivy":false},"saveAsDefault":false}`
	if len(calls) != 1 || string(normalize(calls[0].Params)) != want {
		t.Fatalf("answer: %d calls, %v", len(calls), calls)
	}

	// Every integration off by default: the line says so, and the answer turns none on.
	sh2, _, q2, inf2 := openIntegrationsQuestion(t, ThemeDark, engine.IntAll)
	if hint := sh2.intq.defaultsHint(); hint != "Your default for new clusters has every integration off: none is turned on." {
		t.Errorf("all off: %q", hint)
	}
	test.Tap(sh2.intq.choices[0])
	q2.until(t, "answered", func() bool { return len(inf2.callsOf("cluster.integrations.answer")) == 1 })
	if got := string(normalize(inf2.callsOf("cluster.integrations.answer")[0].Params)); got != `{"context":"kind-dev","integrations":{"alertmanager":false,"kyverno":false,"metrics-server":false,"prometheus":false,"trivy":false},"saveAsDefault":false}` {
		t.Errorf("all off: %s", got)
	}
}

// Don't use integrations (D-82): a warning over the pre-choice that names what goes and what
// stays; No goes back to the pre-choice, Yes answers with every integration off.
func TestIntegrationsQuestionDontUse(t *testing.T) {
	sh, _, q, inf := openIntegrationsQuestion(t, ThemeDark, 0)
	iq := sh.intq
	if hint := iq.defaultsHint(); hint != "Turns on all of them now, as Preferences › Kubernetes says for new clusters." {
		t.Errorf("the first choice's line with every integration on by default: %q", hint)
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

// No Later (the owner's QR1, QR2): Escape does nothing on the pre-choice and goes Back from the
// rows; Back has its arrow at the left, Apply is at the right; Don't use is in the error colour. An
// agent's later closes the open dialog; a choice made elsewhere (Cluster Settings › Integrations)
// closes it too; a step the engine took turns the dialog's page: the dialog follows the engine's
// state, whoever changed it.
func TestIntegrationsQuestionEscapeAndAgent(t *testing.T) {
	sh, _, q, inf := openIntegrationsQuestion(t, ThemeLight, 0)
	iq := sh.intq
	if iq.choices[2].tone != colorStatusError || iq.choices[1].tone != "" {
		t.Errorf("tones: Don't use %q, Choose %q", iq.choices[2].tone, iq.choices[1].tone)
	}
	sh.escape()
	q.drain()
	if iq.d == nil || len(inf.callsOf("cluster.integrations.later")) != 0 || len(stepCalls(inf)) != 0 {
		t.Fatalf("Escape on the pre-choice: dialog %v, %d later, %d steps", iq.d != nil,
			len(inf.callsOf("cluster.integrations.later")), len(stepCalls(inf)))
	}

	test.Tap(iq.choices[1])
	q.until(t, "the rows of Choose", func() bool { return iq.page == engine.IntegrationsChoose && len(iq.rows) == len(engine.Integrations) })
	settle(sh.win)
	if iq.back.Icon == nil || iq.back.Position().X > iq.apply.Position().X {
		t.Errorf("Back: icon %v, at %v; Apply at %v", iq.back.Icon != nil, iq.back.Position(), iq.apply.Position())
	}
	row := iq.apply.Position().X + iq.apply.Size().Width
	if content := iq.rows[0].box.Size().Width; row < content-1 {
		t.Errorf("Apply ends at %v, the rows at %v", row, content)
	}
	sh.escape()
	q.until(t, "Back by Escape", func() bool { return iq.page == engine.IntegrationsPreChoice && iq.d != nil })
	if got := stepCalls(inf); len(got) != 2 || got[1] != `{"context":"kind-dev","step":"preChoice"}` {
		t.Errorf("steps: %q", got)
	}

	inf.update(func(u *engine.UIState) { u.IntegrationsQuestion, u.IntegrationsStep = "", "" }) // an agent's later
	q.until(t, "put off by an agent", func() bool { return iq.d == nil })
	inf.update(func(u *engine.UIState) {
		u.IntegrationsQuestion, u.IntegrationsStep = "kind-dev", engine.IntegrationsPreChoice
	})
	q.until(t, "asked again", func() bool { return iq.d != nil })
	inf.update(func(u *engine.UIState) { // the engine took Choose, every one ticked
		u.IntegrationsStep, u.IntegrationsLookFor = engine.IntegrationsChoose, engine.IntAll
	})
	q.until(t, "the engine's Choose", func() bool { return iq.page == engine.IntegrationsChoose && len(iq.rows) == 5 })
	inf.setIntegrations("kind-dev", map[string]bool{"trivy": false}) // chosen in Cluster Settings › Integrations
	q.until(t, "closed by a choice made elsewhere", func() bool { return iq.d == nil })
	if n := len(inf.callsOf("cluster.integrations.later")); n != 0 {
		t.Errorf("%d later calls from the window", n)
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
	for _, mode := range []ThemeMode{ThemeDark} {
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
