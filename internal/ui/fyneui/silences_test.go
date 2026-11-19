package fyneui

import (
	"context"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// silenceDraftOf drafts a silence as the board's Silence does, from the engine.
func silenceDraftOf(t *testing.T, s *engine.Session, card, fingerprint string) engine.SilenceDraft {
	t.Helper()
	d, err := s.SilenceDraft("", card, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The silences in the window (D-166): a card's Silence form drafts its matchers, previews what they
// mute and creates the silence; its alerts leave the board; the board's Silenced link opens the
// silences page in the board's place, without a namespace filter, its card names the silence and
// offers Expire.
func TestSilencesInTheWindow(t *testing.T) {
	am := &testkit.FakeAlertmanager{}
	am.Set(testkit.SampleAlerts(fixedNow)...)
	s := alertEngineAM(t, am)
	sh, w, q := newEngineShell(t, s, Options{})
	b := openBoard(t, sh, w, q)
	if b.data.Muted != 1 {
		t.Errorf("the board counts %d silenced alerts, want the sample's one", b.data.Muted)
	}

	f := sh.showSilenceForm(silenceDraftOf(t, s, "warning/KubePodCrashLooping", ""))
	if len(f.rows) < 2 || f.rows[0].name.Text != "alertname" || f.rows[0].value.Text != "KubePodCrashLooping" || f.duration.Text != "2h" {
		t.Fatalf("the form's draft: %d rows, %q=%q, %q", len(f.rows), f.rows[0].name.Text, f.rows[0].value.Text, f.duration.Text)
	}
	q.until(t, "the preview", func() bool { return strings.Contains(textsOf(f.mutes), "3 alerts shown now") })
	if !f.confirm.Disabled() {
		t.Error("Silence is enabled without a comment")
	}
	f.comment.SetText("the cart rollout")
	f.author.SetText("hans")
	if f.confirm.Disabled() {
		t.Fatal("Silence stays disabled")
	}
	test.Tap(f.confirm)
	q.until(t, "the silence", func() bool { return len(am.Silences()) == 1 })
	q.until(t, "the form closed", func() bool { return !f.d.shown })
	if x := am.Silences()[0]; x.CreatedBy != "hans" || x.Comment != "the cart rollout" || x.EndsAt.Sub(x.StartsAt) < 119*time.Minute {
		t.Errorf("silence %+v", x)
	}
	q.until(t, "the muted alerts leave the board", func() bool { return b.data != nil && b.data.Total == 21 && b.data.Muted == 4 })

	// A refused silence keeps the form open and says why, naming the request.
	f = sh.showSilenceForm(silenceDraftOf(t, s, "", ""))
	f.rows[0].value.SetText("KubeJobFailed")
	f.comment.SetText("c")
	am.Fail(403)
	test.Tap(f.confirm)
	q.until(t, "the refusal", func() bool { return f.problem.Visible() })
	if !f.d.shown || !strings.Contains(f.problem.text, "POST ") || !strings.Contains(f.problem.text, "/api/v2/silences") {
		t.Errorf("refusal %q, shown %v", f.problem.text, f.d.shown)
	}
	am.Fail(0)
	f.d.Hide()

	// The silences page, through the engine as the board's link does: in the board's place.
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "alerts.openSilences", Params: []byte(`{}`), Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "the silences page", func() bool { return b.page == engine.AlertSilences && b.sdata != nil && b.sdata.Total == 1 })
	settle(w)
	if sh.view.nsBox.Visible() || !b.root.Visible() || sh.view.title.text != engine.AlertSilencesTitle || sh.view.count.text != "1 items" && sh.view.count.text != "1 item" {
		t.Errorf("header: ns filter %v, title %q, count %q", sh.view.nsBox.Visible(), sh.view.title.text, sh.view.count.text)
	}
	if x := b.sdata.Groups[0].Silences[0]; x.Mutes != 3 || x.CreatedBy != "hans" {
		t.Errorf("silence on the page %+v", x)
	}
	if !strings.Contains(textsOf(b.box), "Expire") {
		t.Error("no Expire on an active silence")
	}
}

// Renders of the silences page and the silence form, dark, for review: an active silence muting a
// crash loop, a pending one, an expired one unfolded; the form over the board on a card's draft.
func TestRenderSilencesDark(t *testing.T) {
	am := &testkit.FakeAlertmanager{Clock: func() time.Time { return fixedNow }}
	am.Set(testkit.SampleAlerts(fixedNow)...)
	eq := func(name, value string) map[string]any {
		return map[string]any{"name": name, "value": value, "isRegex": false, "isEqual": true}
	}
	am.SetSilences(
		testkit.FakeSilence{ID: "6f1d2c9e-41aa-4b3e-9b1f-3c0a8e2d7f10", Matchers: []map[string]any{eq("alertname", "KubePodCrashLooping"), eq("namespace", "shop")},
			StartsAt: fixedNow.Add(-40 * time.Minute), EndsAt: fixedNow.Add(80 * time.Minute), UpdatedAt: fixedNow.Add(-40 * time.Minute),
			CreatedBy: "Hans Fischer", Comment: "The cart rollout restarts its pods; the crash loop is expected until the new image is out."},
		testkit.FakeSilence{ID: "0b7e95a4-2d63-4f8a-a1c5-77e6d9f3b2a8", Matchers: []map[string]any{eq("alertname", "NodeFilesystemAlmostOutOfSpace"),
			{"name": "instance", "value": "worker-[0-9]+", "isRegex": true, "isEqual": true}},
			StartsAt: fixedNow.Add(3 * time.Hour), EndsAt: fixedNow.Add(7 * time.Hour), UpdatedAt: fixedNow.Add(-5 * time.Minute),
			CreatedBy: "ops", Comment: "Disk swap in tonight's maintenance window."},
		testkit.FakeSilence{ID: "c4a8f2d1-9e3b-4c7a-8f6e-2b1d0a9c8e7f", Matchers: []map[string]any{eq("alertname", "Watchdog")},
			StartsAt: fixedNow.Add(-26 * time.Hour), EndsAt: fixedNow.Add(-2 * time.Hour), UpdatedAt: fixedNow.Add(-2 * time.Hour),
			CreatedBy: "Hans Fischer", Comment: "Testing the pipeline."},
	)
	s := alertEngineAM(t, am)
	sh, w, q := newEngineShell(t, s, Options{Theme: ThemeDark})
	b := openBoard(t, sh, w, q)
	f := sh.showSilenceForm(silenceDraftOf(t, s, "warning/KubeDeploymentReplicasMismatch", ""))
	f.comment.SetText("Scaling the deployment down for the migration.")
	q.until(t, "the preview", func() bool { return strings.Contains(textsOf(f.mutes), "shown now") })
	savePNG(t, w, "silence_form_dark.png")
	f.d.Hide()

	for _, id := range []string{"alerts.openSilences"} {
		if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: []byte(`{}`), Origin: engine.OriginGUI}); err != nil {
			t.Fatal(err)
		}
	}
	q.until(t, "the silences", func() bool { return b.page == engine.AlertSilences && b.sdata != nil && b.sdata.Total == 3 })
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "alerts.fold", Params: []byte(`{"key":"silences/expired"}`), Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "the expired unfolded", func() bool { return len(b.sdata.Groups) == 3 && !b.sdata.Groups[2].Folded })
	savePNG(t, w, "silences_dark.png")
}

// textsOf is every text under root: text lines, paragraphs, buttons' labels and span texts, a line
// each.
func textsOf(root fyne.CanvasObject) string {
	out := allTexts(root)
	for _, b := range findAll[*textButton](root) {
		out = append(out, b.Text)
	}
	for _, x := range findAll[*spanText](root) {
		for _, sp := range x.spans {
			out = append(out, sp.text)
		}
	}
	return strings.Join(out, "\n")
}

// The board's state box (the owner): in the page's middle instead of cards, no alert firing is a
// cup and how many are silenced; none in the namespace chosen says how many the cluster has and
// removes the filter; none the search keeps clears it; no Alertmanager found offers Set Its
// Address…. Renders, dark, for review.
func TestBoardStateBox(t *testing.T) {
	am := &testkit.FakeAlertmanager{}
	s := alertEngineAM(t, am)
	sh, w, q := newEngineShell(t, s, Options{Theme: ThemeDark})
	b := sh.view.alerts
	dispatch := func(id, params string) {
		t.Helper()
		if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: id, Params: []byte(params), Origin: engine.OriginGUI}); err != nil {
			t.Fatal(err)
		}
	}
	dispatch("alerts.open", `{}`)
	q.until(t, "the empty board", func() bool { return b.data != nil && b.center.Visible() })
	if text := textsOf(b.center); !strings.Contains(text, "great job") || strings.Contains(text, "silenced") {
		t.Errorf("empty board: %q", text)
	}
	savePNG(t, w, "alert_board_empty_dark.png")

	am.Set(testkit.SampleAlerts(fixedNow)...)
	dispatch("alerts.refresh", `{}`)
	q.until(t, "the alerts", func() bool { return b.data != nil && b.data.Total == 24 && !b.center.Visible() })
	dispatch("ui.setNamespace", `{"namespaces":["quiet"]}`)
	q.until(t, "the namespace's box", func() bool { return b.center.Visible() && strings.Contains(textsOf(b.center), "namespace quiet") })
	if text := textsOf(b.center); !strings.Contains(text, "24 alerts in the whole cluster") || !strings.Contains(text, "Remove Namespace Filter") {
		t.Errorf("namespace box: %q", text)
	}
	test.Tap(findAll[*textButton](b.center)[0])
	q.until(t, "the filter removed", func() bool { return len(sh.ui.Namespaces) == 0 && !b.center.Visible() })

	dispatch("ui.setFilter", `{"filter":"nothing-matches-this"}`)
	q.until(t, "the search's box", func() bool { return b.center.Visible() && strings.Contains(textsOf(b.center), "Clear Search") })
	test.Tap(findAll[*textButton](b.center)[0])
	q.until(t, "the search cleared", func() bool { return sh.ui.Filter == "" && !b.center.Visible() })

	b.show(&engine.AlertBoardModel{Context: "staging", State: engine.AlertsNotFound})
	if text := textsOf(b.center); !strings.Contains(text, "No Alertmanager found") || !strings.Contains(text, "Set Its Address") {
		t.Errorf("not found: %q", text)
	}
	savePNG(t, w, "alert_board_notfound_dark.png")
}
