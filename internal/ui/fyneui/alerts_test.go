package fyneui

import (
	"context"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/alerting"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The top bar's right part, left to right (the owner, 2026-10-16 night): the Update button's
// place, the alerts pill, the bell, a divider, then the three layout toggles at the bar's right
// end.
func TestTopBarRightOrder(t *testing.T) {
	s := chatEngine(t)
	sh, w, _ := newEngineShell(t, s, Options{})
	settle(w)
	top := sh.top
	got := top.right.Objects
	if len(got) != 7 || got[0] != top.layout.updateBox || got[1] != top.alerts || got[2] != top.notes.bell ||
		got[6] != top.layout.root { // gaps of the inner padding around the divider
		t.Fatalf("the bar's right part holds %v", got)
	}
	div, ok := got[4].(*fyne.Container)
	if !ok || len(div.Objects) != 1 {
		t.Fatalf("no divider between the bell and the toggles: %T", got[4])
	}
	if _, ok := div.Objects[0].(*surface); !ok || div.Objects[0].Size().Height >= top.right.Size().Height {
		t.Errorf("the divider %T is %v high in a row of %v: want a short line", div.Objects[0], div.Objects[0].Size().Height, top.right.Size().Height)
	}
	_, chatEnd := absRect(top.layout.chat)
	_, rightEnd := absRect(top.right)
	near(t, "the chat toggle at the bar's right end", chatEnd.X, rightEnd.X)
	top.alerts.set(&engine.AlertsSummary{State: engine.AlertsFound, Answered: true})
	settle(w)
	bell, _ := absRect(top.notes.bell)
	_, alertsEnd := absRect(top.alerts)
	if alertsEnd.X > bell.X {
		t.Errorf("the alerts pill ends at %v, right of the bell at %v", alertsEnd.X, bell.X)
	}
}

// The alerts pill (D-54 changed by D-77, the owner's look of 2026-10-20): hidden without the shown
// cluster's Alertmanager (off, no cluster, the demo cluster); grey with a dash while the first look
// runs, grey with "warn" while nothing answers; then the number of every alert in the worst one's
// colour: red from a critical, orange from a warning (major and unlabelled ones count as warning in
// the engine), yellow from an info alert, green with "0" without alerts; "99+" beyond 99; a failing
// Alertmanager keeps its last answer, the tooltip says so; the sign and the number inside the pill;
// a click opens the board.
func TestAlertsPill(t *testing.T) {
	f := connectedFake(t, 10)
	sh, w, q := newTestShell(t, f, Options{})
	settle(w)
	a := sh.top.alerts
	if a.Visible() {
		t.Error("the pill shows without the integration's state")
	}
	since := fixedNow.Add(-3 * time.Minute)
	for _, c := range []struct {
		name   string
		sum    engine.AlertsSummary
		colour fyne.ThemeColorName
		text   string
		tip    string
	}{
		{"checking", engine.AlertsSummary{State: engine.AlertsChecking}, colorStatusUnknown, "–", "looking for"},
		{"not found", engine.AlertsSummary{State: engine.AlertsNotFound, Detail: "no Alertmanager found"}, colorStatusUnknown, "warn", "no Alertmanager found in this cluster"},
		{"forbidden", engine.AlertsSummary{State: engine.AlertsForbidden, Detail: "the current user may not use the service proxy"}, colorStatusUnknown, "warn", "may not use the service proxy"},
		{"none", engine.AlertsSummary{State: engine.AlertsFound, Answered: true}, colorStatusOK, "0", "No active alerts"},
		{"info", engine.AlertsSummary{State: engine.AlertsFound, Answered: true, Total: 2, Info: 2, Worst: "info"}, colorStatusPending, "2", "Alerts: 2 info"},
		{"warning", engine.AlertsSummary{State: engine.AlertsFound, Answered: true, Total: 4, Warning: 3, Info: 1, Worst: "warning"}, colorStatusWarning, "4", "Alerts: 3 warning, 1 info"},
		{"critical", engine.AlertsSummary{State: engine.AlertsFound, Answered: true, Total: 38, Critical: 4, Warning: 33, Info: 1, Worst: "critical"}, colorStatusError, "38", "Alerts: 4 critical, 33 warning, 1 info"},
		{"many", engine.AlertsSummary{State: engine.AlertsFound, Answered: true, Total: 120, Warning: 120, Worst: "warning"}, colorStatusWarning, "99+", "120 warning"},
		{"failing", engine.AlertsSummary{State: engine.AlertsUnavailable, Answered: true, Total: 5, Critical: 5, Worst: "critical", FailingSince: since},
			colorStatusError, "5", "does not answer since"},
	} {
		sum := c.sum
		a.set(&sum)
		if !a.Visible() || a.colour() != c.colour || a.countText() != c.text || !strings.Contains(a.tipText(), c.tip) {
			t.Errorf("%s: visible %v, colour %v, text %q, tip %q; want %v, %q, %q", c.name, a.Visible(), a.colour(), a.countText(), a.tipText(), c.colour, c.text, c.tip)
		}
		settle(w)
		// The sign and the number inside the pill (the owner, 2026-10-20): both within its width,
		// the sign within its height; the number's box holds the line's descent below the digits,
		// so it is taller than the pill, its digits measured inside it in the renders.
		r := test.WidgetRenderer(a).(*alertsRenderer)
		bg, pill := r.bg.Position(), r.bg.Size()
		for _, o := range []fyne.CanvasObject{r.img.img, r.count} {
			at, sz := o.Position().Subtract(bg), o.Size()
			if at.X < 0 || at.X+sz.Width > pill.Width {
				t.Errorf("%s: %T at %v size %v reaches out of the pill %v", c.name, o, at, sz, pill)
			}
		}
		if at, sz := r.img.img.Position().Subtract(bg), r.img.img.Size(); at.Y < 0 || at.Y+sz.Height > pill.Height {
			t.Errorf("%s: the sign at %v size %v reaches out of the pill %v", c.name, at, sz, pill)
		}
	}
	if bell := sh.top.notes.bell; a.MinSize().Height > bell.MinSize().Height {
		t.Errorf("the pill (%v) is taller than the bell (%v)", a.MinSize(), bell.MinSize())
	}
	test.Tap(a)
	q.until(t, "alerts.open", func() bool { return len(f.callsOf("alerts.open")) == 1 })
	a.set(nil)
	if a.Visible() {
		t.Error("the pill shows with the integration off")
	}
	a.conceal()
	a.set(&engine.AlertsSummary{State: engine.AlertsFound, Answered: true})
	if a.Visible() {
		t.Error("a concealed pill came back")
	}
}

// The top bar's right part with the alerts pill in each colour, for review: no alerts (green 0),
// info, warning, critical, and "warn" in grey while none answers.
func TestRenderTopBarAlertsDark(t *testing.T) { renderTopBarAlerts(t, ThemeDark) }

func TestRenderTopBarAlertsLight(t *testing.T) { renderTopBarAlerts(t, ThemeLight) }

func renderTopBarAlerts(t *testing.T, mode ThemeMode) {
	for _, c := range []struct {
		name string
		sum  engine.AlertsSummary
	}{
		{"none", engine.AlertsSummary{State: engine.AlertsFound, Answered: true}},
		{"info", engine.AlertsSummary{State: engine.AlertsFound, Answered: true, Total: 3, Info: 3, Worst: "info"}},
		{"warning", engine.AlertsSummary{State: engine.AlertsFound, Answered: true, Total: 21, Warning: 18, Info: 3, Worst: "warning"}},
		{"critical", engine.AlertsSummary{State: engine.AlertsFound, Answered: true, Total: 24, Critical: 3, Warning: 18, Info: 3, Worst: "critical"}},
		{"notfound", engine.AlertsSummary{State: engine.AlertsNotFound}},
	} {
		sh, w, _ := newEngineShell(t, chatEngine(t), Options{Theme: mode})
		sum := c.sum
		sh.top.alerts.set(&sum)
		savePNG(t, w, "topbar_alerts_"+c.name+"_"+string(mode)+".png")
	}
}

// alertEngine is the real engine and commands over a fake API server whose
// monitoring/kube-prometheus-alertmanager serves testkit.SampleAlerts (the shell's fixed clock), the
// cluster connected and its Alertmanager answered.
func alertEngine(t *testing.T) *engine.Session {
	t.Helper()
	all := []testkit.FakeRule{{Verbs: []string{"*"}, APIGroups: []string{"*"}, Resources: []string{"*"}}}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Access: &testkit.FakeAccess{Rules: map[string][]testkit.FakeRule{"": all}}})
	t.Cleanup(srv.Close)
	srv.Set(testkit.ServicesGVR, testkit.Object("v1", "Service", "monitoring", "kube-prometheus-alertmanager", map[string]any{
		"spec": map[string]any{"ports": []any{map[string]any{"name": "http-web", "port": int64(9093)}}}}))
	for _, ns := range []string{"monitoring", "shop", "flux-system", "payments", "search", "reports", "ingress", "rook-ceph", "velero", "iam"} {
		srv.Set(testkit.NamespacesGVR, testkit.Namespace(ns))
	}
	am := &testkit.FakeAlertmanager{}
	am.Set(testkit.SampleAlerts(fixedNow)...)
	srv.SetProxy("monitoring", "kube-prometheus-alertmanager:http-web", am)
	s := engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "staging", "default")}},
		SnapshotInterval: 20 * time.Millisecond, ClusterSettings: engine.NewClusterSettingsStore(""),
		Alerts: alerting.Options{Interval: time.Hour},
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	// The cluster's integrations chosen (all on): the question about a new cluster's integrations
	// (D-76) does not cover the board.
	if _, err := s.ChangeClusterSettings("staging", func(cs *engine.ClusterSettings) error {
		cs.SetIntegrations(0, engine.IntAll, 0)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background(), "staging"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for u := s.UIState(); u.Alerts == nil || !u.Alerts.Answered; u = s.UIState() {
		if time.Now().After(deadline) {
			t.Fatalf("no answer: %+v", u.Alerts)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return s
}

// openBoard shows the board through the pill and waits for its groups.
func openBoard(t *testing.T, sh *shell, w fyne.Window, q *mainQueue) *alertBoard {
	t.Helper()
	q.until(t, "the pill", func() bool { return sh.top.alerts.Visible() })
	test.Tap(sh.top.alerts)
	b := sh.view.alerts
	q.until(t, "the board", func() bool { return b.shown && b.data != nil && len(b.data.Groups) > 0 })
	settle(w)
	return b
}

// The board in the window: opened by the pill, the header a view's ("Alerts", its count, the
// namespace filter, the search), the groups worst first with their counts, a card per alertname, the
// folding and the search driven through the engine (alerts.fold, ui.setFilter), links into the
// views.
func TestAlertBoard(t *testing.T) {
	s := alertEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	if a := sh.top.alerts; a.countText() != "24" || a.colour() != colorStatusError {
		q.until(t, "the pill's count", func() bool { return a.countText() == "24" })
	}
	b := openBoard(t, sh, w, q)
	if sh.ui.View != engine.AlertBoard || sh.view.title.text != engine.AlertBoardTitle || sh.view.count.text != "24 items" {
		t.Errorf("header %q %q, view %q", sh.view.title.text, sh.view.count.text, sh.ui.View)
	}
	if !sh.view.nsBox.Visible() || sh.view.csvButton.Visible() || !b.root.Visible() {
		t.Error("the board's header shows no namespace filter, or a CSV button, or the board hides")
	}
	var titles []string
	for _, g := range b.data.Groups {
		titles = append(titles, g.Title)
	}
	if strings.Join(titles, ",") != "Critical,Warning,Info" {
		t.Errorf("groups %v", titles)
	}

	// Fold the warning group: through the engine, the board follows.
	s.UpdateUI(func(u *engine.UIState) {}) // nothing changes
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "alerts.fold", Params: []byte(`{"key":"warning","folded":true}`), Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "the warning group folded", func() bool { return len(b.data.Groups) == 3 && b.data.Groups[1].Folded })
	// The search narrows the board, through the UI state.
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "ui.setFilter", Params: []byte(`{"filter":"postgres"}`), Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "the search", func() bool { return b.data.Matched == 1 })
	q.until(t, "the count", func() bool { return sh.view.count.text == "1 of 24 items" })
}

// Renders of the board, dark and light, for review: the synthetic set shaped like a real cluster's,
// one description unfolded.
func TestRenderAlertBoardDark(t *testing.T) { renderAlertBoard(t, ThemeDark) }

func TestRenderAlertBoardLight(t *testing.T) { renderAlertBoard(t, ThemeLight) }

func renderAlertBoard(t *testing.T, mode ThemeMode) {
	s := alertEngine(t)
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	b := openBoard(t, sh, w, q)
	fp := b.data.Groups[0].Cards[0].Alerts[0].Fingerprint
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "alerts.fold",
		Params: []byte(`{"key":"` + fp + `","folded":true,"description":true}`), Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	q.until(t, "the description", func() bool { return b.data.Groups[0].Cards[0].Alerts[0].Expanded })
	firstCardHead(t, b).MouseIn(&desktop.MouseEvent{}) // the pointer on the first card: its copy icon after the name (WF34)
	settle(w)
	savePNG(t, w, "alert_board_"+string(mode)+".png")
}

// openAlertmanagerSettings shows the cluster's Settings › Alertmanager (D-87) and waits for its
// fields to show the engine's setting.
func openAlertmanagerSettings(t *testing.T, s *engine.Session, sh *shell, q *mainQueue) *amFields {
	t.Helper()
	ctx := s.UIState().Context
	if _, err := s.Dispatch(context.Background(), engine.Invocation{ID: "cluster.settings.open",
		Params: []byte(`{"context":"` + ctx + `","section":"alertmanager"}`), Origin: engine.OriginGUI}); err != nil {
		t.Fatal(err)
	}
	var f *amFields
	q.until(t, "the Alertmanager's fields", func() bool {
		f, _ = sh.settings.controls[amFieldsControl].(*amFields)
		return f != nil && f.enabled && f.line.text == "Auto detect: in use monitoring/kube-prometheus-alertmanager:9093."
	})
	q.until(t, "the integrations' status lines", func() bool {
		for _, r := range sh.settings.integrationRows {
			if r.status.text == "" {
				return false
			}
		}
		return len(sh.settings.integrationRows) > 0
	})
	return f
}

// The Alertmanager's address on Cluster Settings (D-77; on Alertmanager's own section, D-87): the setting in use read from the engine, an address typed
// set with cluster.setAlertmanager after the typing pause and kept by the engine, emptied: found by
// name again.
func TestAlertmanagerAddressFields(t *testing.T) {
	s := alertEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	sh.settings.typing = time.Millisecond
	f := openAlertmanagerSettings(t, s, sh, q)
	if !f.prefix.Disabled() {
		t.Error("the path prefix is enabled without an address")
	}
	test.Type(f.address, "monitoring/kube-prometheus-alertmanager:http-web")
	q.until(t, "the address set", func() bool {
		set, err := s.AlertmanagerSetting("")
		return err == nil && !set.Auto && set.Address == "monitoring/kube-prometheus-alertmanager:http-web"
	})
	q.until(t, "the line", func() bool { return strings.HasPrefix(f.line.text, "Set: ") })
	settle(w)
	f.address.SetText("")
	q.until(t, "found by name again", func() bool { set, _ := s.AlertmanagerSetting(""); return set.Auto })
}

// Renders of Alertmanager's section with its address, dark and light, for review.
func TestRenderAlertmanagerFieldsDark(t *testing.T) { renderAlertmanagerFields(t, ThemeDark) }

func TestRenderAlertmanagerFieldsLight(t *testing.T) { renderAlertmanagerFields(t, ThemeLight) }

func renderAlertmanagerFields(t *testing.T, mode ThemeMode) {
	s := alertEngine(t)
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	openAlertmanagerSettings(t, s, sh, q)
	settle(w)
	savePNG(t, w, "settings_alertmanager_"+string(mode)+".png")
}

// TestAlertNameCopy (WF21): a card's alertname has the lists' copy icon, shown while the pointer is
// on the card's head or on the icon; a click puts the name on the clipboard.
func TestAlertNameCopy(t *testing.T) {
	s := alertEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	b := openBoard(t, sh, w, q)
	var head *alertTap
	for _, a := range findAll[*alertTap](b.root) {
		if a.in != nil {
			head = a
			break
		}
	}
	btns := findAll[*copyButton](b.root)
	if head == nil || len(btns) == 0 {
		t.Fatalf("no card head with a copy icon: heads %v, icons %d", head != nil, len(btns))
	}
	btn := btns[0]
	if btn.Visible() {
		t.Error("the copy icon shows before the pointer is on the card")
	}
	head.MouseIn(&desktop.MouseEvent{})
	if !btn.Visible() {
		t.Fatal("the copy icon does not show on the card's head")
	}
	head.MouseOut() // the pointer moves onto the icon
	btn.MouseIn(&desktop.MouseEvent{})
	if !btn.Visible() {
		t.Fatal("the copy icon hid under the pointer")
	}
	test.Tap(btn)
	if want, got := b.data.Groups[0].Cards[0].Name, sh.app.Clipboard().Content(); got != want {
		t.Errorf("copied %q, want the alertname %q", got, want)
	}
	btn.MouseOut()
	if btn.Visible() {
		t.Error("the copy icon stays after the pointer left")
	}
}

// TestAlertNameCopyAcrossARebuild (WF34): the copy icon sits right after the alertname; a board
// rebuilt under the pointer (the next answer of the Alertmanager) shows the icon of the card the
// pointer is on at once, and the same answer again rebuilds nothing.
func TestAlertNameCopyAcrossARebuild(t *testing.T) {
	s := alertEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	b := openBoard(t, sh, w, q)
	head := firstCardHead(t, b)
	row := head.content.(*fyne.Container)
	if _, ok := row.Objects[1].(*textLine); !ok || len(findAll[*copyButton](row.Objects[2])) != 1 {
		t.Fatalf("the head's objects %T: want the chevron, the name and its copy icon first", row.Objects)
	}
	head.MouseIn(&desktop.MouseEvent{})

	same := *b.data
	same.AsOf = same.AsOf.Add(30 * time.Second)
	before := b.box.Objects[0]
	b.show(&same)
	if b.box.Objects[0] != before {
		t.Error("the same answer rebuilt the board")
	}

	next := *b.data
	next.Total++ // a new answer: the board is built anew
	b.show(&next)
	fresh := firstCardHead(t, b)
	if fresh == head {
		t.Fatal("the board was not rebuilt")
	}
	btn := findAll[*copyButton](fresh)[0]
	if !btn.Visible() {
		t.Fatal("the rebuilt card's copy icon waits for the pointer to move")
	}
	head.MouseOut() // the pointer moves: the old head leaves first, then the new one is entered if it is there
	if btn.Visible() {
		t.Error("the rebuilt card's icon stays after the pointer left")
	}
	fresh.MouseIn(&desktop.MouseEvent{})
	if !btn.Visible() {
		t.Error("the rebuilt card's icon does not show on its head")
	}
}

func firstCardHead(t *testing.T, b *alertBoard) *alertTap {
	t.Helper()
	for _, a := range findAll[*alertTap](b.root) {
		if a.in != nil {
			return a
		}
	}
	t.Fatal("no card head")
	return nil
}
