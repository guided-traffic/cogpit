package fyneui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// interruptedAt is the fixed time of the banner tests' interruption.
var interruptedAt = time.Date(2026, 10, 15, 14, 3, 0, 0, time.UTC)

// interrupt puts the fake's connection into state at interruptedAt, the next attempt 8 s after now.
func interrupt(f *fakeEngine, state engine.ConnectionState, now time.Time) {
	f.update(func(u *engine.UIState) {
		u.Connection = engine.Connection{State: state, Demo: u.Connection.Demo, Since: interruptedAt,
			Reason: "no answer from the API server"}
		if state == engine.Interrupted {
			u.Connection.NextAttempt = now.Add(8 * time.Second)
		} else {
			u.Connection.Reason = "the API server rejects the login (401 Unauthorized)"
			u.Connection.CredentialPlugin = true // Log in again runs it (without one: reconnect_test.go)
		}
	})
}

// TestConnectionBannerFollowsTheState is S3 as the owner amended it (F13, 2026-10-16): interrupted,
// the status bar's dot is the Running green (it blinks, conndot_test.go) and no word stands beside it
// in either bar (the top bar has no dot, D-54), the banner over the view says what is interrupted, since when and the next
// attempt, and Reconnect dispatches cluster.reconnect (D-81); an expired login turns the dot red and
// offers Log in again (cluster.login); connected again, the banner goes.
func TestConnectionBannerFollowsTheState(t *testing.T) {
	f := connectedFake(t, 20)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	if sh.conn.root.Visible() {
		t.Fatal("a banner while connected")
	}
	now := interruptedAt.Add(4 * time.Minute)
	sh.now = func() time.Time { return now }
	interrupt(f, engine.Interrupted, now)
	q.until(t, "the banner", func() bool { return sh.conn.root.Visible() })
	if sh.status.dot.color != colorContainerRunning || barSaysState(sh) {
		t.Errorf("status bar: dot %v, words %q", sh.status.dot.color, barTexts(sh))
	}
	if got := sh.conn.title.text; !strings.HasPrefix(got, "The connection to demo is interrupted · since ") {
		t.Errorf("title %q", got)
	}
	if got := sh.conn.detail.text; !strings.Contains(got, "No answer from the API server") || !strings.Contains(got, "next attempt in 8 s") ||
		!strings.Contains(got, "changes are disabled") {
		t.Errorf("detail %q", got)
	}
	if sh.status.context.text != "demo" {
		t.Errorf("status bar %q", sh.status.context.text)
	}
	if sh.view.root.Visible() == false || len(sh.kinds) == 0 {
		t.Error("the view or the navigator went with the interruption")
	}
	test.Tap(sh.conn.action)
	q.until(t, "cluster.reconnect", func() bool { return len(f.callsOf("cluster.reconnect")) == 1 })

	interrupt(f, engine.LoginExpired, now)
	q.until(t, "the login banner", func() bool { return sh.conn.action.Text == "Log in again" })
	if sh.status.dot.color != statusColorName(model.StatusError) || barSaysState(sh) {
		t.Errorf("status bar: dot %v, words %q", sh.status.dot.color, barTexts(sh))
	}
	if !strings.Contains(sh.conn.detail.text, "may open your browser") {
		t.Errorf("detail %q", sh.conn.detail.text)
	}
	test.Tap(sh.conn.action)
	q.until(t, "cluster.login", func() bool { return len(f.callsOf("cluster.login")) == 1 })

	f.update(func(u *engine.UIState) { u.Connection = engine.Connection{State: engine.Connected, Demo: true} })
	q.until(t, "the banner to go", func() bool { return !sh.conn.root.Visible() })
	if sh.status.dot.color != colorContainerRunning || barSaysState(sh) {
		t.Errorf("status bar: dot %v, words %q", sh.status.dot.color, barTexts(sh))
	}
}

// barTexts are the texts of the top bar's left part and of the status bar.
func barTexts(sh *shell) []string {
	return append(lineTexts(sh.top.left), lineTexts(sh.status.root)...)
}

// lineTexts are the texts of the text lines under root, the tooltip lines (a cluster's name) too.
func lineTexts(root fyne.CanvasObject) []string {
	var out []string
	for _, l := range findAll[*textLine](root) {
		out = append(out, l.text)
	}
	for _, l := range findAll[*tipLine](root) {
		out = append(out, l.text)
	}
	return out
}

// barSaysState reports whether a bar names the connection's state in a word (F13: never).
func barSaysState(sh *shell) bool {
	for _, s := range barTexts(sh) {
		for _, w := range []string{"Connected", "Connecting", "Reconnecting", "Login expired", "Connection failed", "Not connected"} {
			if strings.Contains(s, w) {
				return true
			}
		}
	}
	return false
}

// TestAShellTabOffersReconnect is S7's shells in the window: a shell tab the interruption ended
// shows a row with Reconnect, disabled until the connection is back; its click dispatches
// dock.reconnect, and a new session count opens a new session in the same tab.
func TestAShellTabOffersReconnect(t *testing.T) {
	skipTerminalUnderRace(t)
	f := connectedFake(t, 20)
	f.quietTerminals = true
	pod := f.tables["pods"].Rows[0].Ref
	f.ui.Dock = []engine.DockTab{{ID: "shell-1", Type: engine.DockShell, Title: "Shell: " + pod.Name, Context: "demo", Ref: &pod, Container: "api"}}
	f.ui.ActiveDock = "shell-1"
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the shell", func() bool { c := sh.dock.contents["shell-1"]; return c != nil && c.term != nil })
	c := sh.dock.contents["shell-1"]
	if c.reconnect == nil || c.reconnect.root.Visible() {
		t.Fatal("a Reconnect row on a working shell")
	}
	now := interruptedAt.Add(time.Minute)
	interrupt(f, engine.Interrupted, now)
	f.update(func(u *engine.UIState) { u.Dock[0].Interrupted = interruptedAt })
	q.until(t, "the row", func() bool { return c.reconnect.root.Visible() })
	if !c.reconnect.button.Disabled() || !strings.Contains(c.reconnect.text.text, "once the connection is back") {
		t.Errorf("while interrupted: disabled %v, %q", c.reconnect.button.Disabled(), c.reconnect.text.text)
	}
	f.update(func(u *engine.UIState) { u.Connection = engine.Connection{State: engine.Connected, Demo: true} })
	q.until(t, "Reconnect enabled", func() bool { return !c.reconnect.button.Disabled() })
	test.Tap(c.reconnect.button)
	q.until(t, "dock.reconnect", func() bool { return len(f.callsOf("dock.reconnect")) == 1 })
	f.update(func(u *engine.UIState) { u.Dock[0].Interrupted, u.Dock[0].Session = time.Time{}, 1 })
	q.until(t, "a new session", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.opened["shell-1"] == 2
	})
	q.until(t, "the new terminal", func() bool { return c.term != nil && !c.reconnect.root.Visible() })
}

// connectionScene is the shell over the pods view with the drawer open, the connection interrupted
// (or the login expired) four minutes ago, the dots' blink in its first phase.
func connectionScene(t *testing.T, mode ThemeMode, state engine.ConnectionState) fyne.Window {
	w, _, _ := blinkScene(t, mode, state, nil)
	return w
}

// blinkScene is connectionScene with the blinker's clock in the test's hands; before runs ahead of
// the interruption (Reduce motion is read when a blink starts).
func blinkScene(t *testing.T, mode ThemeMode, state engine.ConnectionState, before func(*shell)) (fyne.Window, *shell, *blinkClock) {
	f := connectedFake(t, 200)
	row := f.tables["pods"].Rows[3]
	ref := row.Ref
	f.details[ref.String()] = fixturePodDetail(row)
	f.ui.Selected = []model.ObjectRef{ref}
	f.ui.Drawer = &ref
	f.ui.Context = "prod-eu [config]"
	f.ui.Connection.Demo = false
	// The cluster's Pods tab, as a cluster that was connected has: without a tab the window shows
	// the welcome page while the connection is not up (the renders did from 2026-10-21 to 2026-10-24).
	f.ui.Tabs = []engine.ViewTab{{ID: "tab-1", Context: f.ui.Context, View: "pods", Title: "Pods - prod-eu"}}
	f.ui.ActiveTab = "tab-1"
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the shell to settle", func() bool { return len(sh.kinds) > 0 && sh.drawer.last != nil && sh.diag.total > 0 })
	now := interruptedAt.Add(4 * time.Minute)
	sh.now = func() time.Time { return now }
	clk := fakeBlinkClock(sh)
	if before != nil {
		before(sh)
	}
	interrupt(f, state, now)
	q.until(t, "the banner", func() bool { return sh.conn.root.Visible() })
	return w, sh, clk
}

func TestRenderConnectionInterruptedDark(t *testing.T) {
	savePNG(t, connectionScene(t, ThemeDark, engine.Interrupted), "connection_interrupted_dark.png")
}

func TestRenderConnectionLoginExpiredDark(t *testing.T) {
	savePNG(t, connectionScene(t, ThemeDark, engine.LoginExpired), "connection_login_expired_dark.png")
}

// upgrade marks the fake's connection as upgraded from v1.35.2 to v1.36.1 while connected, the
// navigator's version following it.
func upgrade(f *fakeEngine) {
	f.update(func(u *engine.UIState) {
		u.Connection.State = engine.Connected
		u.Connection.Upgraded = &engine.ServerUpgrade{From: "v1.35.2", To: "v1.36.1", Since: interruptedAt}
		cv := engine.ClusterVersion{Context: u.Context, Version: "v1.36.1"}
		u.ClusterVersion = &cv
	})
}

// TestConnectionBannerSaysTheClusterWasUpgraded (the owner's "Option A" of 2026-10-26): a connected
// cluster whose API server runs another minor than the connect's shows the strip in the accent
// colour with both versions and Reconnect (cluster.reconnect); its lines are selectable; a
// reconnect under way greys the button; the strip goes with the mark.
func TestConnectionBannerSaysTheClusterWasUpgraded(t *testing.T) {
	f := connectedFake(t, 20)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	upgrade(f)
	q.until(t, "the banner", func() bool { return sh.conn.root.Visible() })
	if got := sh.conn.title.text; got != "The API server of demo runs v1.36.1 now · Cogpit connected to v1.35.2" {
		t.Errorf("title %q", got)
	}
	if got := sh.conn.detail.text; !strings.HasPrefix(got, "The kinds and API versions Cogpit knows are the ones v1.35.2 served: Reconnect loads v1.36.1's") {
		t.Errorf("detail %q", got)
	}
	if sh.conn.tint.color != colorAccent || sh.conn.action.Text != reconnectLabel || sh.conn.action.Disabled() || sh.conn.reconnect.Visible() {
		t.Errorf("tint %s, button %q (disabled %v)", sh.conn.tint.color, sh.conn.action.Text, sh.conn.action.Disabled())
	}
	if n := len(findAll[*selLine](sh.conn.root)); n != 2 {
		t.Errorf("%d selectable lines in the strip, want 2", n)
	}
	if sh.status.dot.color != colorContainerRunning {
		t.Errorf("the status bar's dot %v while connected", sh.status.dot.color)
	}
	test.Tap(sh.conn.action)
	q.until(t, "cluster.reconnect", func() bool { return len(f.callsOf("cluster.reconnect")) == 1 })

	f.update(func(u *engine.UIState) { u.Connection.Reconnecting = true })
	q.until(t, "the reconnect", func() bool { return sh.conn.action.Disabled() })
	if sh.conn.action.Text != reconnectingText {
		t.Errorf("button %q while reconnecting", sh.conn.action.Text)
	}

	f.update(func(u *engine.UIState) { u.Connection = engine.Connection{State: engine.Connected, Demo: true} })
	q.until(t, "the banner to go", func() bool { return !sh.conn.root.Visible() })
}

// upgradedScene is the shell over the pods view with the drawer open, the cluster upgraded to
// another minor while connected.
func upgradedScene(t *testing.T, mode ThemeMode) fyne.Window {
	f := connectedFake(t, 200)
	row := f.tables["pods"].Rows[3]
	ref := row.Ref
	f.details[ref.String()] = fixturePodDetail(row)
	f.ui.Selected = []model.ObjectRef{ref}
	f.ui.Drawer = &ref
	f.ui.Context = "prod-eu [config]"
	f.ui.Connection.Demo = false
	f.ui.Tabs = []engine.ViewTab{{ID: "tab-1", Context: f.ui.Context, View: "pods", Title: "Pods - prod-eu"}}
	f.ui.ActiveTab = "tab-1"
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the shell to settle", func() bool { return len(sh.kinds) > 0 && sh.drawer.last != nil && sh.diag.total > 0 })
	upgrade(f)
	q.until(t, "the banner", func() bool { return sh.conn.root.Visible() })
	return w
}

func TestRenderConnectionUpgradedDark(t *testing.T) {
	savePNG(t, upgradedScene(t, ThemeDark), "connection_upgraded_dark.png")
}
