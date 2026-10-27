package fyneui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// The contexts of the client certificate scene (D-75), at fixedNow (2026-10-11 14:30 UTC).
const (
	certValid   = "kind-cogpit [.kube/config]"     // 65 days left of a year's certificate
	certNear    = "staging-us-east [.kube/config]" // 5 days left: expiring
	certExpired = "prod-eu-central [prod.yaml]"    // expired 3 days ago
	certNone    = "eks-payments [eks.yaml]"        // a credential plugin: nothing known
)

const certDay = 24 * time.Hour

// certValidities are the scene's certificates.
var certValidities = map[string]kubeconfig.CertValidity{
	certValid:   {NotBefore: fixedNow.Add(-300 * certDay), NotAfter: fixedNow.Add(65*certDay + 2*time.Hour)},
	certNear:    {NotBefore: fixedNow.Add(-360 * certDay), NotAfter: fixedNow.Add(5*certDay + 3*time.Hour)},
	certExpired: {NotBefore: fixedNow.Add(-400 * certDay), NotAfter: fixedNow.Add(-3*certDay - 2*time.Hour)},
}

// certFake is kind-cogpit connected, the navigator at the section level, with the scene's contexts;
// the welcome page without a cluster instead when welcome is set.
func certFake(t *testing.T, welcome bool) *fakeEngine {
	t.Helper()
	f := connectedFake(t, 200)
	if welcome {
		f = newFakeEngine(t)
	}
	at := func(name string) *kubeconfig.CertValidity {
		v := certValidities[name]
		return v.At(fixedNow)
	}
	f.contexts = []engine.ContextInfo{
		{Name: "demo", Namespace: "default", Demo: true},
		{Name: certValid, KubeContext: "kind-cogpit", Source: ".kube/config", Cluster: "kind-cogpit", User: "kind-cogpit",
			Server: "https://127.0.0.1:52341", File: "/Users/dev/.kube/config", ClientCert: at(certValid)},
		{Name: certNear, KubeContext: "staging-us-east", Source: ".kube/config", Cluster: "staging-us-east",
			Server: "https://api.staging-us-east.example.com:6443", File: "/Users/dev/.kube/config", ClientCert: at(certNear)},
		{Name: certExpired, KubeContext: "prod-eu-central", Source: "prod.yaml", Cluster: "prod-eu-central",
			Server: "https://api.prod-eu-central.example.com:6443", File: "/Users/dev/prod.yaml", ClientCert: at(certExpired)},
		{Name: certNone, KubeContext: "eks-payments", Source: "eks.yaml", Cluster: "eks-payments",
			Server: "https://ABCD.gr7.eu-central-1.eks.amazonaws.com", File: "/Users/dev/eks.yaml", Exec: engine.ExecApproved, ExecKind: "program"},
	}
	if welcome {
		f.ui.Welcome = true
		return f
	}
	f.ui.Context = certValid
	f.ui.Connection = engine.Connection{State: engine.Connected}
	f.ui.NavigatorScope = engine.NavScopeLocal
	fakeNavScope(t, f)
	return f
}

// certScene shows certFake's section.
func certScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := certFake(t, false)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the section's clusters", func() bool {
		return sh.side.sec.loaded && len(sh.side.sec.rows) == len(f.contexts) && len(sh.kinds) > 0
	})
	return sh, w, q, f
}

// pillOf returns the row's pill whose text starts with prefix.
func pillOf(r *clusterRow, prefix string) *pill {
	for _, o := range r.badges.Objects {
		if p, ok := o.(*pill); ok && strings.HasPrefix(p.text, prefix) {
			return p
		}
	}
	return nil
}

// The cluster lists say how long a context's client certificate is valid (D-75, F22): the
// navigator's section a second line under the name, muted while valid, in the warning colour while
// expiring, in the error colour once expired, nothing for a context without one; the row's tooltip
// a line with the date and time; the welcome page a pill; the hotbar tile's tooltip the same line.
func TestClusterListsShowTheClientCertificate(t *testing.T) {
	tf := func(sh *shell, at time.Time) string {
		return sh.timeFormat().FormatDate(at) + " " + sh.timeFormat().FormatClock(at)
	}
	want := map[string]struct {
		line, pill string
		color      fyne.ThemeColorName
	}{
		certValid:   {"cert expires in 65 days", "client certificate expires in 65 days", colorMuted},
		certNear:    {"cert expires in 5 days", "client certificate expires in 5 days", colorStatusWarning},
		certExpired: {"expired", "expired", colorStatusError}, // just "expired" (the owner, 2026-10-17)
	}

	t.Run("the navigator's section", func(t *testing.T) {
		f := certFake(t, false)
		withHotbar(f, certExpired, certNone)
		sh, _, q := newTestShell(t, f, Options{})
		n := sh.side.sec
		q.until(t, "the section's clusters and the tiles", func() bool {
			return n.loaded && len(n.rows) == len(f.contexts) && len(tilesOf(sh.hot)) == 2
		})
		for id, w := range want {
			r := n.rows[id]
			if !r.cert.Visible() || r.cert.text != w.line || r.cert.color != w.color {
				t.Errorf("%s: line %q in %s (shown %v), want %q in %s", id, r.cert.text, r.cert.color, r.cert.Visible(), w.line, w.color)
			}
		}
		for _, id := range []string{certNone, "demo"} {
			if r := n.rows[id]; r.cert.Visible() || strings.Contains(r.tipText(), "certificate") {
				t.Errorf("%s shows a certificate: %q / %q", id, r.cert.text, r.tipText())
			}
		}
		exp := certValidities[certExpired].NotAfter
		line := "Client certificate expired (valid until " + tf(sh, exp) + ")"
		if tip := n.rows[certExpired].tipText(); !strings.HasSuffix(tip, "\n"+line) {
			t.Errorf("the expired row's tooltip %q, want it to end in %q", tip, line)
		}
		if tip := n.rows[certValid].tipText(); !strings.HasSuffix(tip, "\nClient certificate expires in 65 days (valid until "+
			tf(sh, certValidities[certValid].NotAfter)+")") {
			t.Errorf("the valid row's tooltip %q", tip)
		}
		if tip := tileOf(sh, certExpired).tipText(); !strings.HasSuffix(tip, "\n"+line) {
			t.Errorf("the tile's tooltip %q", tip)
		}
		if tip := tileOf(sh, certNone).tipText(); strings.Contains(tip, "certificate") {
			t.Errorf("the plugin's tile %q", tip)
		}
	})

	t.Run("the welcome page", func(t *testing.T) {
		f := certFake(t, true)
		sh, _, q := newTestShell(t, f, Options{})
		w := welcomeShown(t, sh, q)
		q.until(t, "every row", func() bool { return len(w.rows) == len(f.contexts) })
		for id, wnt := range want {
			p := pillOf(welcomeRowOf(t, sh, id[:strings.Index(id, " [")]), wnt.pill) // an expired one's reads "expired" alone
			if p == nil || p.text != wnt.pill || p.fg != wnt.color {
				t.Errorf("%s: pill %+v, want %q in %s", id, p, wnt.pill, wnt.color)
			}
		}
		if p := pillOf(welcomeRowOf(t, sh, "eks-payments"), "client certificate"); p != nil {
			t.Errorf("the plugin's row has %q", p.text)
		}
	})
}

// The lines follow the clock without a new listing (the shell's tick, certsTick): 3 days and 4
// hours later the expiring certificate has 47 h left, the valid one 61 days; the welcome page's
// pills follow too. Nothing is read or asked for it.
func TestClientCertificateLinesFollowTheClock(t *testing.T) {
	sh, _, q, f := certScene(t, ThemeDark)
	n := sh.side.sec
	reads := len(f.callsOf("cluster.contexts"))
	later := fixedNow.Add(3*certDay + 4*time.Hour)
	sh.now = func() time.Time { return later }
	sh.certsTick()
	q.drain()
	if r := n.rows[certNear]; r.cert.text != "cert expires in 47 h" || r.cert.color != colorStatusWarning {
		t.Errorf("the expiring line %q in %s", r.cert.text, r.cert.color)
	}
	if r := n.rows[certValid]; r.cert.text != "cert expires in 61 days" {
		t.Errorf("the valid line %q", r.cert.text)
	}
	if r := n.rows[certExpired]; r.cert.text != "expired" {
		t.Errorf("the expired line %q", r.cert.text)
	}
	sh.now = func() time.Time { return certValidities[certNear].NotAfter.Add(time.Hour) }
	sh.certsTick()
	if r := n.rows[certNear]; r.cert.text != "expired" || r.cert.color != colorStatusError {
		t.Errorf("after its end: %q in %s", r.cert.text, r.cert.color)
	}
	if got := len(f.callsOf("cluster.contexts")); got != reads {
		t.Errorf("the tick listed the contexts again: %d -> %d", reads, got)
	}

	fw := certFake(t, true)
	shw, _, qw := newTestShell(t, fw, Options{})
	w := welcomeShown(t, shw, qw)
	qw.until(t, "every row", func() bool { return len(w.rows) == len(fw.contexts) })
	shw.now = func() time.Time { return later }
	shw.certsTick()
	qw.drain()
	if p := pillOf(welcomeRowOf(t, shw, "staging-us-east"), "client certificate"); p == nil || p.text != "client certificate expires in 47 h" {
		t.Errorf("the welcome page's pill %+v", p)
	}
}

// An expired certificate never holds a connect (the owner: "ein Verbindungsaufbau soll dadurch
// nicht blockiert werden, auch wenn er fehlschlagen wird"): a click on its row in the section
// drills into it (ui.setNavigatorScope, which connects) with no dialog first, and a click on its
// welcome page row dispatches cluster.connect.
func TestAnExpiredClientCertificateDoesNotHoldTheConnect(t *testing.T) {
	sh, w, q, f := certScene(t, ThemeDark)
	test.Tap(sh.side.sec.rows[certExpired])
	q.until(t, "the drill", func() bool { return len(scopeCalls(t, f)) > 0 })
	if got := scopeCalls(t, f); got[len(got)-1] != engine.NavScopeCluster(certExpired) {
		t.Errorf("scopes %q", got)
	}
	if overlays := w.Canvas().Overlays().List(); len(overlays) != 0 {
		t.Errorf("a dialog or menu opened: %d overlays", len(overlays))
	}

	fw := certFake(t, true)
	shw, _, qw := newTestShell(t, fw, Options{})
	wp := welcomeShown(t, shw, qw)
	qw.until(t, "every row", func() bool { return len(wp.rows) == len(fw.contexts) })
	test.Tap(welcomeRowOf(t, shw, "prod-eu-central"))
	qw.until(t, "the connect", func() bool { return len(fw.callsOf("cluster.connect")) == 1 })
	if p := decodeParams[contextParams](t, fw.callsOf("cluster.connect")[0]); p.Context != certExpired {
		t.Errorf("connect %+v", p)
	}
}

// The navigator's section with a valid, an expiring and an expired client certificate and one
// context without, dark and light; the pointer on the expired row shows its tooltip with the date.
func TestRenderNavigatorClientCertsDark(t *testing.T) {
	renderCertSection(t, ThemeDark, "navigator_client_certs_dark.png")
}
func renderCertSection(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _ := certScene(t, mode)
	settle(w)
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(sh.side.sec.rows[certExpired].cert))
	q.until(t, "the tooltip", func() bool { return strings.Contains(sh.tip.visibleText(), "Client certificate expired") })
	savePNG(t, w, name)
}

// The welcome page's rows with the certificates' pills, dark and light.
func TestRenderWelcomeClientCertsDark(t *testing.T) {
	renderCertWelcome(t, ThemeDark, "welcome_client_certs_dark.png")
}
func renderCertWelcome(t *testing.T, mode ThemeMode, name string) {
	f := certFake(t, true)
	sh, q := welcomeOnFake(t, f, mode)
	q.drain()
	savePNG(t, sh.win, name)
}
