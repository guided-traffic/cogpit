package fyneui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The owner's finding of 2026-10-16 night (D-73): a connect that waited for an OIDC login nobody
// finished blinked with no way out. The connecting cluster's workspace and menus offer Cancel
// Connecting (cluster.disconnect), and its state says what it waits for.

// waitingConnection is a connect whose credential plugin pinniped has waited two minutes.
func waitingConnection() engine.Connection {
	return engine.Connection{State: engine.Connecting,
		Reason:          "waiting for the login through pinniped; it may wait for you to log in, in a browser",
		WaitingForLogin: engine.LoginWait{Program: "pinniped", Since: fixedNow.Add(-2 * time.Minute), Until: fixedNow.Add(3 * time.Minute)}}
}

// strayWaitConnection is a connect that waits behind a run of pinniped that began twelve minutes
// ago and that Cogpit stopped waiting for and could not end (H-118): no end time.
func strayWaitConnection() engine.Connection {
	return engine.Connection{State: engine.Connecting,
		Reason:          "waiting behind the login through pinniped, which Cogpit stopped waiting for and could not end; it has no end time",
		WaitingForLogin: engine.LoginWait{Program: "pinniped", Since: fixedNow.Add(-12 * time.Minute), RunsOn: true}}
}

// connectingScene is the shell while prod-eu-central connects and its plugin waits, the dots'
// blink in its first phase.
func connectingScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	return connectingSceneWith(t, mode, waitingConnection())
}

// connectingSceneWith is connectingScene with conn as the connect's state.
func connectingSceneWith(t *testing.T, mode ThemeMode, conn engine.Connection) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := connectedFake(t, 0)
	f.ui.Context, f.ui.View = "prod-eu-central", ""
	f.ui.Connection = engine.Connection{State: engine.Disconnected}
	if err := engine.Register(f.reg, engine.Spec[engine.OnCluster]{ID: "cluster.disconnect", Label: "Disconnect", Danger: engine.DangerUI,
		Run: func(context.Context, *engine.Session, engine.OnCluster) (any, error) { return nil, nil }}); err != nil {
		t.Fatal(err)
	}
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.drain()
	fakeBlinkClock(sh)
	f.update(func(u *engine.UIState) { u.Connection = conn })
	q.until(t, "Cancel Connecting", func() bool { return sh.view.cancelBox.Visible() && sh.view.detailBox.Visible() })
	return sh, w, q, f
}

// The connecting workspace says what the connect waits for and offers Cancel Connecting, as the
// window's Cluster menu, the cluster's menus and its dot's tooltip do; a tap cancels the connect.
func TestConnectingOffersCancelConnecting(t *testing.T) {
	sh, w, q, f := connectingScene(t, ThemeDark)
	if got := sh.view.placeholderText.text; got != "Connecting to prod-eu-central…" {
		t.Errorf("the placeholder: %q", got)
	}
	want := "Waiting for the login through pinniped since 14:28; it may wait for you in your browser. Cogpit ends it at 14:33."
	if got := sh.view.placeholderDetail.text; got != want || sh.view.placeholderDetailEnd.Visible() {
		t.Errorf("the detail: %q (a second line %v), want %q", got, sh.view.placeholderDetailEnd.Visible(), want)
	}
	if item := windowDisconnect(t, w); item.Label != cancelConnectingLabel || item.Disabled {
		t.Errorf("the Cluster menu's item: %q, disabled %v", item.Label, item.Disabled)
	}
	var labels []string
	for _, it := range sh.clusterItems("prod-eu-central") {
		labels = append(labels, it.Label)
	}
	if !strings.Contains(strings.Join(labels, ","), cancelConnectingLabel) {
		t.Errorf("the cluster's menu: %q", labels)
	}
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(sh.status.dot))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	if tip := sh.tip.visibleText(); !strings.Contains(tip, "Connecting…") || !strings.Contains(tip, want) ||
		!strings.Contains(tip, "Cancel Connecting in its menu ends it now.") {
		t.Errorf("the dot's tooltip: %q", tip)
	}
	test.MoveMouse(w.Canvas(), fyne.NewPos(1, 1))

	test.Tap(sh.view.cancelButton)
	q.until(t, "cluster.disconnect", func() bool { return len(f.callsOf("cluster.disconnect")) == 1 })
	f.update(func(u *engine.UIState) { u.Connection = engine.Connection{State: engine.Disconnected} })
	q.until(t, "the cancel gone", func() bool { return !sh.view.cancelBox.Visible() && !sh.view.detailBox.Visible() })
	if item := windowDisconnect(t, w); item.Label != "Disconnect" || !item.Disabled {
		t.Errorf("disconnected: the Cluster menu's item %q, disabled %v", item.Label, item.Disabled)
	}
}

// A cluster that connects in the background (the restore, D-69) offers Cancel Connecting in its
// menus, which names its context.
func TestABackgroundConnectOffersCancelConnecting(t *testing.T) {
	f := connectedFake(t, 0)
	f.ui.Connections = []engine.ConnectedCluster{{Context: "staging-us-east", Connection: waitingConnection()}}
	if err := engine.Register(f.reg, engine.Spec[engine.OnCluster]{ID: "cluster.disconnect", Label: "Disconnect", Danger: engine.DangerUI,
		Run: func(context.Context, *engine.Session, engine.OnCluster) (any, error) { return nil, nil }}); err != nil {
		t.Fatal(err)
	}
	sh, _, q := newTestShell(t, f, Options{})
	q.drain()
	var cancel *fyne.MenuItem
	for _, it := range sh.clusterItems("staging-us-east") {
		if it.Label == cancelConnectingLabel {
			cancel = it
		}
	}
	if cancel == nil {
		t.Fatal("no Cancel Connecting for the background connect")
	}
	cancel.Action()
	q.until(t, "cluster.disconnect", func() bool { return len(f.callsOf("cluster.disconnect")) == 1 })
	var p struct {
		Context string `json:"context"`
	}
	if err := json.Unmarshal(f.callsOf("cluster.disconnect")[0].Params, &p); err != nil || p.Context != "staging-us-east" {
		t.Errorf("the cancel's parameters: %s", f.callsOf("cluster.disconnect")[0].Params)
	}
}

// H-118: a connect behind a run Cogpit stopped waiting for and could not end names no end time,
// in the workspace and the dot's tooltip, and says that Cancel Connecting only stops the wait.
func TestConnectingBehindALoginCogpitCouldNotEnd(t *testing.T) {
	sh, w, q, _ := connectingSceneWith(t, ThemeDark, strayWaitConnection())
	what := "Waiting behind the login through pinniped, running since 14:18, which Cogpit stopped waiting for and could not end."
	end := "It has no end time: it lasts until its process ends."
	if got := sh.view.placeholderDetail.text; got != what {
		t.Errorf("the detail: %q, want %q", got, what)
	}
	if got, want := sh.view.placeholderDetailEnd.text, end+" Cancel Connecting stops this wait; that login goes on."; got != want ||
		!sh.view.placeholderDetailEnd.Visible() {
		t.Errorf("the detail's second line: %q (shown %v), want %q", got, sh.view.placeholderDetailEnd.Visible(), want)
	}
	line := what + " " + end
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(sh.status.dot))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	tip := sh.tip.visibleText()
	if !strings.Contains(tip, line+" Cancel Connecting in its menu stops this wait; that login goes on.") {
		t.Errorf("the dot's tooltip: %q", tip)
	}
	if strings.Contains(tip, "Cogpit ends it") || strings.Contains(tip, "ends it now") {
		t.Errorf("the dot's tooltip promises an end: %q", tip)
	}
}

func renderConnecting(t *testing.T, mode ThemeMode, name string) {
	renderConnectingWith(t, mode, waitingConnection(), name)
}

func renderConnectingWith(t *testing.T, mode ThemeMode, conn engine.Connection, name string) {
	sh, w, q, _ := connectingSceneWith(t, mode, conn)
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(sh.status.dot))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	savePNG(t, w, name)
}

func TestRenderConnectingLoginWaitDark(t *testing.T) {
	renderConnecting(t, ThemeDark, "connecting_login_wait_dark.png")
}

func TestRenderConnectingLoginWaitLight(t *testing.T) {
	renderConnecting(t, ThemeLight, "connecting_login_wait_light.png")
}

func TestRenderConnectingBehindAStrayLoginDark(t *testing.T) {
	renderConnectingWith(t, ThemeDark, strayWaitConnection(), "connecting_stray_login_dark.png")
}

func TestRenderConnectingBehindAStrayLoginLight(t *testing.T) {
	renderConnectingWith(t, ThemeLight, strayWaitConnection(), "connecting_stray_login_light.png")
}
