package fyneui

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// helmHTTPFake answers helm.repo.list with three repositories, one of them waiting to send its
// credentials over plain http, and records helm.repo.allowHTTPCredentials, after which the
// repository is allowed.
type helmHTTPFake struct {
	f       *fakeEngine
	mu      sync.Mutex
	allowed bool
}

func newHelmHTTPFake(f *fakeEngine) *helmHTTPFake {
	h := &helmHTTPFake{f: f}
	f.handlers["helm.repo.list"] = func(context.Context, engine.Invocation) (any, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		waiting := engine.HelmRepoInfo{Name: "internal", URL: "http://charts.corp.example/stable", HasCredentials: true, PlainHTTP: true,
			HTTPCredentials: engine.HTTPCredentialsRequired, HTTPToken: "0f1e2d3c4b5a69788796a5b4c3d2e1f0",
			Error: "the Helm repository internal sends its user name and password over plain http, unencrypted; …"}
		if h.allowed {
			waiting.HTTPCredentials, waiting.HTTPToken, waiting.Error = engine.HTTPCredentialsAllowed, "", ""
		}
		return engine.HelmReposList{File: "/tmp/helm/repositories.yaml", Status: engine.HelmFileOK, Repositories: []engine.HelmRepoInfo{
			{Name: "bitnami", URL: "https://charts.bitnami.com/bitnami", Charts: 120},
			waiting,
			{Name: "local", URL: "http://127.0.0.1:8879", PlainHTTP: true},
		}}, nil
	}
	f.handlers["helm.repo.allowHTTPCredentials"] = func(context.Context, engine.Invocation) (any, error) {
		h.mu.Lock()
		h.allowed = true
		h.mu.Unlock()
		go f.emit(engine.Event{Type: engine.EventHelmRepos})
		return []engine.HelmRepoInfo{}, nil
	}
	return h
}

func helmHTTPScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := connectedFake(t, 200)
	newHelmHTTPFake(f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	return sh, w, q, f
}

func buttonNamed(root fyne.CanvasObject, text string) *widget.Button {
	for _, b := range findAll[*widget.Button](root) {
		if b.Text == text {
			return b
		}
	}
	return nil
}

// TestHelmHTTPBannerAsksTheUser (H-40): with the Charts view open, a repository waiting to send
// its credentials over plain http gets a row (the others none); Allow… asks plainly, Cancel
// allows nothing, Allow over http dispatches helm.repo.allowHTTPCredentials with the repository's
// name and token, and the row goes once the engine says so. Another view hides the banner.
func TestHelmHTTPBannerAsksTheUser(t *testing.T) {
	sh, w, q, f := helmHTTPScene(t, ThemeDark)
	q.until(t, "no banner on the pods view", func() bool { return !sh.helmHTTP.root.Visible() })
	f.update(func(u *engine.UIState) { u.View = helmChartsKind })
	q.until(t, "the banner", func() bool { return sh.helmHTTP.root.Visible() && len(sh.helmHTTP.repos) == 1 })
	if sh.helmHTTP.repos[0].Name != "internal" {
		t.Fatalf("rows %+v", sh.helmHTTP.repos)
	}

	test.Tap(buttonNamed(sh.helmHTTP.root, "Allow…"))
	q.until(t, "the question", func() bool { return w.Canvas().Overlays().Top() != nil })
	test.Tap(buttonNamed(w.Canvas().Overlays().Top(), "Cancel"))
	q.until(t, "cancelled", func() bool { return w.Canvas().Overlays().Top() == nil })
	if n := len(f.callsOf("helm.repo.allowHTTPCredentials")); n != 0 {
		t.Fatalf("Cancel allowed it (%d calls)", n)
	}

	test.Tap(buttonNamed(sh.helmHTTP.root, "Allow…"))
	q.until(t, "the question", func() bool { return w.Canvas().Overlays().Top() != nil })
	test.Tap(buttonNamed(w.Canvas().Overlays().Top(), "Allow over http"))
	q.until(t, "allowed", func() bool {
		return len(f.callsOf("helm.repo.allowHTTPCredentials")) == 1 && !sh.helmHTTP.root.Visible()
	})
	var p helmHTTPParams
	if err := json.Unmarshal(normalize(f.callsOf("helm.repo.allowHTTPCredentials")[0].Params), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Repositories) != 1 || p.Repositories[0].Name != "internal" || p.Repositories[0].Token != "0f1e2d3c4b5a69788796a5b4c3d2e1f0" {
		t.Errorf("allowed %+v", p)
	}
	if o := f.callsOf("helm.repo.allowHTTPCredentials")[0].Origin; o != engine.OriginGUI {
		t.Errorf("dispatched as %s", o)
	}

	f.update(func(u *engine.UIState) { u.View = "pods" })
	q.until(t, "hidden on another view", func() bool { return !sh.helmHTTP.root.Visible() })
}

// renderHelmHTTP draws the Charts view with the banner, and the question, for a look: to
// COGPIT_PNG_DIR when that is set, else it is only captured.
func renderHelmHTTP(t *testing.T, mode ThemeMode, suffix string) {
	sh, w, q, f := helmHTTPScene(t, mode)
	f.update(func(u *engine.UIState) { u.View = helmChartsKind })
	q.until(t, "the banner", func() bool { return sh.helmHTTP.root.Visible() })
	savePNG(t, w, "helm_http_banner_"+suffix+".png")
	test.Tap(buttonNamed(sh.helmHTTP.root, "Allow…"))
	q.until(t, "the question", func() bool { return w.Canvas().Overlays().Top() != nil })
	savePNG(t, w, "helm_http_question_"+suffix+".png")
}

func TestRenderHelmHTTPDark(t *testing.T) { renderHelmHTTP(t, ThemeDark, "dark") }
