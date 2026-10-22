package fyneui

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A failed connect's error page (D-114, the owner's AW17 of 2026-10-22), the view's placeholder that
// stays inside the view (AW18), the welcome page without "Other clusters" (AW13) and the stages in
// Connection Updates.

// walkCertCause is the cause of the owner's walk-cert-gone failure as the engine now says it.
const walkCertCause = "the client certificate file /Users/hfi/walk-cert/client.crt does not exist"

// walkCertError is the owner's walk-cert-gone error of the alpha walk as the engine wrote it until
// D-114: the whole chain, in one line.
const walkCertError = `context "walk-cert-gone": invalid kubeconfig: invalid configuration: unable to read client-cert /Users/hfi/walk-cert/client.crt for walk-cert-gone due to open /Users/hfi/walk-cert/client.crt: no such file or directory`

// walkCertFailure is its connect's stages.
func walkCertFailure(ctx string) *engine.ConnectFailure {
	return &engine.ConnectFailure{Context: ctx, At: fixedNow, Stages: []engine.ConnectStage{
		{Name: "kubeconfig", Result: "failed", Text: walkCertCause},
		{Name: "dns", Result: "notReached"}, {Name: "tcp", Result: "notReached"}, {Name: "tls", Result: "notReached"}, {Name: "api", Result: "notReached"},
	}}
}

// failRestored puts the fake in the state the restore of the last session leaves after the shown
// cluster's connect failed: its tabs kept (hidden, GW13), its error and stages.
func failRestored(f *fakeEngine, ctx string) {
	f.update(func(u *engine.UIState) {
		u.Tabs = append(u.Tabs, engine.ViewTab{ID: "tab-7", Context: ctx, View: kindPods, Title: "Pods - " + ctx, Sort: engine.DefaultSort})
		u.Context, u.Connection = ctx, engine.Connection{State: engine.ConnectError, Error: walkCertCause}
		u.ConnectFailure = walkCertFailure(ctx)
		u.ActiveTab, u.View = "", ""
		for i := range u.Connections {
			u.Connections[i].Shown = false
		}
	})
}

// The restore whose shown cluster fails leaves the window on that cluster's error page, whether or
// not a tab is open: the page says it is an error page, the cause in a sentence under the stage it
// failed in, the connect stage by stage, Connect Again and Show Connection Updates; not the welcome
// page; the cluster's tabs hidden (GW13).
func TestARestoredFailedConnectShowsTheErrorPage(t *testing.T) {
	sh, _, q, f, _ := marksScene(t, ThemeDark, false)
	failRestored(f, cFailed)
	p := sh.tabs.errPage
	q.until(t, "the error page", func() bool { return p.shown && len(p.rows) == 5 })
	if sh.tabs.welcome.shown || sh.tabs.welcome.root.Visible() || !p.root.Visible() {
		t.Fatalf("the welcome page %v, the error page %v", sh.tabs.welcome.shown, p.root.Visible())
	}
	if p.title.text != "Could not connect to "+cFailed || p.head.text != "error · kubeconfig valid and complete" ||
		p.cause.text != "The client certificate file /Users/hfi/walk-cert/client.crt does not exist" {
		t.Errorf("title %q, head %q, cause %q", p.title.text, p.head.text, p.cause.text)
	}
	for i, want := range []string{"kubeconfig failed", "dns notReached", "tcp notReached", "tls notReached", "api notReached"} {
		if got := p.rows[i].stage.Name + " " + p.rows[i].stage.Result; got != want {
			t.Errorf("row %d: %s, want %s", i, got, want)
		}
	}
	if !strings.HasPrefix(p.when.text, "Tried ") || !strings.Contains(p.when.text, "Connection Updates") {
		t.Errorf("the line under the buttons %q", p.when.text)
	}
	for _, tab := range sh.tabs.shown {
		if tab.tab.Context == cFailed {
			t.Errorf("the failed cluster's tab %s is shown (GW13)", tab.tab.ID)
		}
	}

	// Connect Again connects the cluster through the engine; Show Connection Updates reads its log.
	p.retry.OnTapped()
	q.until(t, "the connect", func() bool { return len(f.callsOf("cluster.connect")) == 1 })
	if c := f.callsOf("cluster.connect")[0]; !strings.Contains(string(normalize(c.Params)), cFailed) {
		t.Errorf("Connect Again connected %s", c.Params)
	}
	p.connLog.OnTapped()
	q.until(t, "the log read", func() bool { return len(f.callsOf("cluster.connectLog")) == 1 })

	// Another cluster shown: the page goes.
	f.update(func(u *engine.UIState) {
		u.Context, u.Connection, u.ConnectFailure = cShown, engine.Connection{State: engine.Connected}, nil
		u.ActiveTab, u.View = "tab-7", kindPods
	})
	q.until(t, "the page gone", func() bool { return !p.shown && !p.root.Visible() })
}

// A very long cause wraps within the page's column, inside the workspace, never over the navigator.
func TestTheErrorPageWraps(t *testing.T) {
	sh, w, q, f, _ := marksScene(t, ThemeDark, false)
	failRestored(f, cFailed)
	long := strings.Repeat("the API server at https://api.qa-eu-west.example.com:6443 answered something nobody expected; ", 6)
	f.update(func(u *engine.UIState) { u.Connection.Error = long })
	p := sh.tabs.errPage
	q.until(t, "the long cause", func() bool { return p.shown && strings.HasPrefix(p.cause.text, "The API server") })
	settle(w)
	d := fyne.CurrentApp().Driver()
	work := d.AbsolutePositionForObject(p.root)
	for name, o := range map[string]fyne.CanvasObject{"cause": p.cause, "title": p.title, "when": p.when} {
		at := d.AbsolutePositionForObject(o)
		if at.X < work.X || at.X+o.Size().Width > work.X+p.root.Size().Width {
			t.Errorf("%s spans x %.0f to %.0f, the workspace %.0f to %.0f", name, at.X, at.X+o.Size().Width, work.X, work.X+p.root.Size().Width)
		}
	}
	if lines := len(test.WidgetRenderer(p.cause).Objects()); lines < 3 {
		t.Errorf("the long cause on %d lines", lines)
	}
}

// AW18: the view's placeholder never draws beyond the view, whatever its line says: before, the
// failed connect's "Could not connect to <cluster>: <error>" was centred at its full width and a
// clipped "Could not" stood over the navigator beside the welcome page.
func TestThePlaceholderStaysInsideTheView(t *testing.T) {
	sh, w, q, f, _ := marksScene(t, ThemeDark, false)
	for name, set := range map[string]func(u *engine.UIState){
		"failed without a page": func(u *engine.UIState) {
			u.Context, u.Connection = cFailed, engine.Connection{State: engine.ConnectError, Error: walkCertError}
		},
		"connecting": func(u *engine.UIState) {
			u.Context = "a-cluster-with-a-very-long-name-" + strings.Repeat("x", 120) + " [kubernetes_configs/wds18-k8s-stage.yaml]"
			u.Connection = engine.Connection{State: engine.Connecting}
		},
	} {
		f.update(set)
		q.until(t, name, func() bool { return sh.view.placeholderText.text != "" && sh.view.placeholder.Visible() })
		q.drain()
		settle(w)
		d := fyne.CurrentApp().Driver()
		view, text := d.AbsolutePositionForObject(sh.view.root), d.AbsolutePositionForObject(sh.view.placeholderText)
		if text.X < view.X || text.X+sh.view.placeholderText.Size().Width > view.X+sh.view.root.Size().Width {
			t.Errorf("%s: the placeholder %q spans x %.0f to %.0f, the view %.0f to %.0f", name, sh.view.placeholderText.text,
				text.X, text.X+sh.view.placeholderText.Size().Width, view.X, view.X+sh.view.root.Size().Width)
		}
		if strings.Contains(sh.view.placeholderText.text, "Could not connect") {
			t.Errorf("%s: the view repeats the error page's line: %q", name, sh.view.placeholderText.text)
		}
	}
}

// AW13: the welcome page has no "Other clusters": with a hotbar or a recent cluster it lists those
// only; on a first start (no hotbar tile, no recent cluster) the kubeconfigs' clusters as "Your
// clusters", the current one first.
func TestTheWelcomePageHasNoOtherClusters(t *testing.T) {
	f := newFakeEngine(t)
	f.contexts = fixtureContexts()
	f.ui.Welcome = true
	sh, _, q := newTestShell(t, f, Options{})
	w := sh.tabs.welcome
	q.until(t, "the first start's group", func() bool { return w.loaded && len(w.rows) > 0 })
	if g := w.groups(); len(g) != 1 || g[0].title != "Your clusters" || len(g[0].contexts) != len(f.contexts) {
		t.Fatalf("groups on a first start %+v", groupTitles(g))
	}

	withHotbar(f, cBlink)
	f.update(func(u *engine.UIState) {}) // the hotbar's change reaches the page
	q.until(t, "the hotbar alone", func() bool {
		g := w.groups()
		return len(g) == 1 && g[0].title == "Hotbar"
	})
	if g := w.groups(); len(g[0].contexts) != 1 || g[0].contexts[0].Name != cBlink {
		t.Errorf("the hotbar's group %+v", groupTitles(g))
	}
	f.update(func(u *engine.UIState) { u.Context = cShown }) // a recent cluster
	q.until(t, "the hotbar and Recent", func() bool { return strings.Join(groupTitles(w.groups()), ",") == "Hotbar,Recent" })
}

func groupTitles(g []welcomeGroup) []string {
	out := make([]string, len(g))
	for i, x := range g {
		out[i] = x.title
	}
	return out
}

// Connection Updates shows a connect's stages under its header, and Copy writes them.
func TestConnectLogDialogShowsTheStages(t *testing.T) {
	a := failedAttempt(logAt)
	a.Stages = []engine.ConnectStage{
		{Name: "kubeconfig", Result: "ok", Text: `context "team" in /Users/me/kubeconfigs/team.yaml: server https://team.example:6443, login by the credential plugin kubelogin`},
		{Name: "login", Result: "failed", Text: "the credential plugin kubelogin failed: exec: executable kubelogin failed with exit code 1", TookMS: 2195},
		{Name: "dns", Result: "notReached"}, {Name: "tcp", Result: "notReached"}, {Name: "tls", Result: "notReached"}, {Name: "api", Result: "notReached"},
	}
	lf := newLogFake(t, a)
	sh, _, q := newTestShell(t, lf.fakeEngine, Options{})
	sh.kc.showConnectLog(logCtx)
	q.until(t, "the dialog", func() bool { return sh.kc.connectLog != nil })
	rows := findAll[*stageRow](sh.kc.connectLog.rows)
	if len(rows) != 6 || rows[1].stage.Result != "failed" || rows[2].stage.Result != "notReached" {
		t.Fatalf("the dialog's stages %d", len(rows))
	}
	text := connectLogText(logCtx, sh.kc.connectLog.view, sh.timeFormat())
	for _, want := range []string{"Stages:\n", "  passed      Kubeconfig valid and complete: context \"team\"",
		"  FAILED      Login by the credential plugin: the credential plugin kubelogin failed", "(2.2 s)",
		"  not reached DNS: the API server's name resolved: not reached\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("the copy lacks %q:\n%s", want, text)
		}
	}
}

// Each result has its glyph and its status colour (design-language.md: glow is never the only
// signal).
func TestStageMarks(t *testing.T) {
	for result, want := range map[string]fyne.ThemeColorName{
		"ok": colorStatusOK, "failed": colorStatusError, "notReached": colorMuted, "skipped": colorMuted, "cancelled": colorMuted,
	} {
		res, c, word := stageMark(result)
		if res == nil || c != want || word == "" {
			t.Errorf("%s: %v %s %q", result, res, c, word)
		}
	}
	if got := stageDetail(engine.ConnectStage{Name: "dns", Result: "skipped", Text: "10.0.0.9 is an address: no lookup"}); got != "10.0.0.9 is an address: no lookup" {
		t.Errorf("a skipped stage says %q", got)
	}
}

// The PNGs of the pull request: the error page after the failed restore, dark and light; the
// welcome page on a first start ("Your clusters"); Connection Updates with a connect's stages.
func TestRenderConnectFailedPageDark(t *testing.T)  { renderConnectFailedPage(t, ThemeDark) }
func TestRenderConnectFailedPageLight(t *testing.T) { renderConnectFailedPage(t, ThemeLight) }

func renderConnectFailedPage(t *testing.T, mode ThemeMode) {
	sh, w, q, f, _ := marksScene(t, mode, false)
	failRestored(f, cFailed)
	q.until(t, "the error page", func() bool { return sh.tabs.errPage.shown && len(sh.tabs.errPage.rows) == 5 })
	q.drain()
	savePNG(t, w, "connect_failed_page_"+string(mode)+".png")
}

func TestRenderWelcomeFirstStartDark(t *testing.T) {
	f := newFakeEngine(t)
	f.contexts = fixtureContexts()
	f.ui.Welcome = true
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the first start's group", func() bool { return sh.tabs.welcome.loaded && len(sh.tabs.welcome.rows) > 0 })
	q.drain()
	savePNG(t, w, "welcome_first_start_dark.png")
}

func TestRenderConnectionUpdatesStagesDark(t *testing.T) {
	a := failedAttempt(logAt.Add(26 * time.Hour))
	a.Stages = []engine.ConnectStage{
		{Name: "kubeconfig", Result: "ok", Text: `context "team" in /Users/me/kubeconfigs/team.yaml: server https://team.example:6443, login by the credential plugin kubelogin`},
		{Name: "login", Result: "failed", Text: "the credential plugin kubelogin failed: exec: executable kubelogin failed with exit code 1", TookMS: 2195},
		{Name: "dns", Result: "notReached"}, {Name: "tcp", Result: "notReached"}, {Name: "tls", Result: "notReached"}, {Name: "api", Result: "notReached"},
	}
	ok := successAttempt(logAt)
	ok.Stages = []engine.ConnectStage{
		{Name: "kubeconfig", Result: "ok", Text: `context "team" in /Users/me/kubeconfigs/team.yaml: server https://team.example:6443, login by the credential plugin kubelogin`},
		{Name: "login", Result: "ok", Text: "kubelogin answered", TookMS: 640},
		{Name: "dns", Result: "ok", Text: "team.example: 10.20.0.7, 10.20.0.8", TookMS: 12},
		{Name: "tcp", Result: "ok", Text: "10.20.0.7:6443 reached", TookMS: 3},
		{Name: "tls", Result: "ok", Text: "TLS 1.3 with team.example, its certificate checked against the kubeconfig's certificate authority", TookMS: 21},
		{Name: "api", Result: "ok", Text: "GET /version answered: Kubernetes v1.36.1; the login accepted", TookMS: 176},
	}
	lf := newLogFake(t, a, ok)
	sh, w, q := newTestShell(t, lf.fakeEngine, Options{})
	q.until(t, "the section's rows", func() bool { return sh.side.sec.loaded })
	sh.kc.showConnectLog(logCtx)
	q.until(t, "the dialog", func() bool { return sh.kc.connectLog != nil })
	savePNG(t, w, "connection_updates_stages_dark.png")
}
