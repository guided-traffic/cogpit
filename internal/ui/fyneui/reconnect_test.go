package fyneui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// fakeReconnect gives the fake engine cluster.reconnect with the real command's metadata.
func fakeReconnect(t testing.TB, f *fakeEngine) {
	t.Helper()
	c, ok := realCommands(t).Get("cluster.reconnect")
	if !ok {
		t.Fatal("the registry has no cluster.reconnect")
	}
	probe[contextParams](t, f, c)
}

// reconnectCalls are the contexts cluster.reconnect was dispatched for, in order.
func reconnectCalls(t *testing.T, f *fakeEngine) []string {
	t.Helper()
	var out []string
	for _, c := range f.callsOf("cluster.reconnect") {
		out = append(out, decodeParams[contextParams](t, c).Context)
	}
	return out
}

// itemLabels are a menu's items' labels, a greyed one marked with a trailing "(off)".
func itemLabels(items []*fyne.MenuItem) []string {
	var out []string
	for _, it := range items {
		if it == nil || it.IsSeparator {
			continue
		}
		l := it.Label
		if it.Disabled {
			l += " (off)"
		}
		out = append(out, l)
	}
	return out
}

// TestReconnectFromTheClustersList is F29 in the window: in the list of all clusters (the
// navigator's section) a connected cluster's row has a ↻ that reconnects it, the others none; its
// menus and the window's Cluster menu have Reconnect beside Disconnect; while it reconnects, the ↻
// is muted and does nothing, the items say "Reconnecting…" greyed, and its dots blink.
func TestReconnectFromTheClustersList(t *testing.T) {
	f := sectionFake(t)
	fakeReconnect(t, f)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the section's clusters", func() bool { return sh.side.sec.loaded && sh.side.sec.rows[secKind] != nil })
	rows := sh.side.sec.rows
	if !rows[secKind].reconnect.Visible() || rows[secProd].reconnect.Visible() || rows[secEKS].reconnect.Visible() {
		t.Fatalf("the ↻: connected %v, others %v %v", rows[secKind].reconnect.Visible(), rows[secProd].reconnect.Visible(),
			rows[secEKS].reconnect.Visible())
	}
	if rows[secKind].reconnect.tip != reconnectTip {
		t.Errorf("tooltip %q", rows[secKind].reconnect.tip)
	}
	test.Tap(rows[secKind].reconnect)
	q.until(t, "cluster.reconnect", func() bool { return len(f.callsOf("cluster.reconnect")) == 1 })
	if got := reconnectCalls(t, f); got[0] != secKind {
		t.Errorf("reconnect of %q", got[0])
	}
	if got := itemLabels(sh.clusterItems(secKind)); len(got) == 0 || got[0] != reconnectLabel {
		t.Errorf("the row's menu: %v", got)
	}
	if got := itemLabels(sh.clusterItems(secProd)); strings.Contains(strings.Join(got, ","), reconnectLabel) {
		t.Errorf("a cluster not connected offers Reconnect: %v", got)
	}
	if got := itemLabels(sh.clusterMenuItems()); len(got) == 0 || got[0] != reconnectLabel {
		t.Errorf("the Cluster menu: %v", got)
	}

	f.update(func(u *engine.UIState) { u.Connection.Reconnecting = true })
	q.until(t, "the muted ↻", func() bool { return sh.side.sec.rows[secKind].reconnect.muted })
	test.Tap(sh.side.sec.rows[secKind].reconnect)
	if got := itemLabels(sh.clusterMenuItems()); got[0] != reconnectingText+" (off)" {
		t.Errorf("the Cluster menu while it reconnects: %v", got)
	}
	if !sh.blink.blinking(secKind) {
		t.Error("the dots do not blink while it reconnects")
	}
	if n := len(f.callsOf("cluster.reconnect")); n != 1 {
		t.Errorf("%d reconnects: the ↻ worked while one ran", n)
	}
	f.update(func(u *engine.UIState) { u.Connection.Reconnecting = false })
	q.until(t, "the ↻ again", func() bool { return !sh.side.sec.rows[secKind].reconnect.muted })
}

// TestReconnectInTheBanner: interrupted, the banner's button is Reconnect; a login without a
// credential plugin that expired is renewed by Reconnect alone, the banner saying the file renews
// it (F28); one with a plugin has Log in again and Reconnect beside it; while a reconnect runs the
// button says so, greyed.
func TestReconnectInTheBanner(t *testing.T) {
	f := connectedFake(t, 20)
	fakeReconnect(t, f)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	now := interruptedAt.Add(4 * time.Minute)
	sh.now = func() time.Time { return now }
	b := sh.conn

	interrupt(f, engine.Interrupted, now)
	q.until(t, "the banner", func() bool { return b.root.Visible() })
	if b.action.Text != reconnectLabel || b.action.Disabled() || b.reconnect.Visible() {
		t.Errorf("interrupted: %q disabled %v, second %v", b.action.Text, b.action.Disabled(), b.reconnect.Visible())
	}
	test.Tap(b.action)
	q.until(t, "the reconnect", func() bool { return len(f.callsOf("cluster.reconnect")) == 1 })

	f.update(func(u *engine.UIState) {
		u.Connection = engine.Connection{State: engine.LoginExpired, Demo: true, Since: interruptedAt,
			Reason: "the API server still rejects the login (401 Unauthorized)"}
	})
	q.until(t, "the file's login", func() bool { return strings.Contains(b.detail.text, "kubeconfig file") })
	if b.action.Text != reconnectLabel || b.reconnect.Visible() || strings.Contains(b.detail.text, "browser") {
		t.Errorf("an expired login without a plugin: %q, second %v, detail %q", b.action.Text, b.reconnect.Visible(), b.detail.text)
	}
	test.Tap(b.action)
	q.until(t, "its reconnect", func() bool { return len(f.callsOf("cluster.reconnect")) == 2 })
	if n := len(f.callsOf("cluster.login")); n != 0 {
		t.Errorf("%d logins for a context without a plugin", n)
	}

	f.update(func(u *engine.UIState) { u.Connection.CredentialPlugin = true })
	q.until(t, "Log in again", func() bool { return b.action.Text == "Log in again" })
	if !b.reconnect.Visible() || b.reconnect.Disabled() {
		t.Errorf("Reconnect beside Log in again: %v disabled %v", b.reconnect.Visible(), b.reconnect.Disabled())
	}
	test.Tap(b.reconnect)
	q.until(t, "the plugin context's reconnect", func() bool { return len(f.callsOf("cluster.reconnect")) == 3 })
	test.Tap(b.action)
	q.until(t, "cluster.login", func() bool { return len(f.callsOf("cluster.login")) == 1 })

	f.update(func(u *engine.UIState) { u.Connection.Reconnecting = true })
	q.until(t, "Reconnecting…", func() bool { return b.action.Text == reconnectingText })
	if !b.action.Disabled() || b.reconnect.Visible() {
		t.Errorf("while it reconnects: disabled %v, second %v", b.action.Disabled(), b.reconnect.Visible())
	}
}

// TestReconnectInTheConnectionLogAndOverview: the Connection Updates dialog and the cluster
// Overview have Reconnect for the connected cluster.
func TestReconnectInTheConnectionLogAndOverview(t *testing.T) {
	f := sectionFake(t)
	fakeReconnect(t, f)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the section's clusters", func() bool { return sh.side.sec.loaded })
	if !sh.view.cluster.reconnect.Visible() {
		t.Error("the Overview has no Reconnect")
	}
	sh.kc.openConnectLog(secKind, connectLogView{Context: secKind})
	cl := sh.kc.connectLog
	if cl == nil || cl.reconnect == nil || cl.reconnect.Disabled() {
		t.Fatal("the dialog's Reconnect is not offered")
	}
	test.Tap(cl.reconnect)
	q.until(t, "cluster.reconnect", func() bool { return len(f.callsOf("cluster.reconnect")) == 1 })
	sh.kc.openConnectLog(secProd, connectLogView{Context: secProd})
	if cl := sh.kc.connectLog; !cl.reconnect.Disabled() {
		t.Error("a cluster not connected: the dialog's Reconnect is enabled")
	}
}

func TestRenderReconnectListDark(t *testing.T) {
	renderReconnectList(t, ThemeDark, "reconnect_section_dark.png")
}

// renderReconnectList: the list of all clusters, the connected kind-cogpit with its ↻, the pointer
// on it and its tooltip.
func renderReconnectList(t *testing.T, mode ThemeMode, name string) {
	f := sectionFake(t)
	fakeReconnect(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the section and diagnostics", func() bool {
		return sh.side.sec.loaded && len(sh.side.sec.visible) == 7 && len(sh.kinds) > 0 && sh.diag.total > 0
	})
	settle(w)
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(sh.side.sec.rows[secKind].reconnect))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	savePNG(t, w, name)
}

func TestRenderReconnectBannerDark(t *testing.T) {
	renderReconnectBanner(t, ThemeDark, "reconnect_banner_dark.png")
}

// renderReconnectBanner: a client certificate's login expired (the owner's F28), the banner with
// Reconnect and what renews it.
func renderReconnectBanner(t *testing.T, mode ThemeMode, name string) {
	f := sectionFake(t)
	fakeReconnect(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the section and diagnostics", func() bool {
		return sh.side.sec.loaded && len(sh.kinds) > 0 && sh.diag.total > 0
	})
	now := interruptedAt.Add(19 * time.Minute)
	sh.now = func() time.Time { return now }
	f.update(func(u *engine.UIState) {
		u.Connection = engine.Connection{State: engine.LoginExpired, Since: interruptedAt,
			Reason: "the API server still rejects the login (401 Unauthorized)"}
	})
	q.until(t, "the banner", func() bool { return sh.conn.root.Visible() && sh.conn.action.Text == reconnectLabel })
	settle(w)
	savePNG(t, w, name)
}
