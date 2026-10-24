package fyneui

import (
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestKubectlPreferencesFields: Preferences › Kubernetes sets the four kubectl preferences through
// the engine, from the window; a refused value shows under its field and is not kept.
func TestKubectlPreferencesFields(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{Theme: ThemeDark})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "kubernetes"})
	q.until(t, "the Kubernetes section", func() bool { return sh.settings.controls["kubectlPath"] != nil })
	p := sh.settings
	if !p.controls["kubectlDownload"].(*widget.Check).Checked {
		t.Error("downloads are not offered by default")
	}
	prefs := func() engine.Preferences { pr, _ := s.Preferences(); return pr }

	test.Tap(p.controls["kubectlDownload"].(*widget.Check))
	q.until(t, "downloads off", func() bool { return !prefs().KubectlDownload })

	mirror := entryOf(p.controls["kubectlMirror"])
	mirror.SetText("http://mirror.example.com/k8s")
	q.until(t, "the mirror refused", func() bool { return p.errs["kubectlMirror"].Visible() })
	if prefs().KubectlMirror != "" {
		t.Errorf("a refused mirror was kept: %q", prefs().KubectlMirror)
	}
	mirror.SetText("https://mirror.example.com/k8s")
	q.until(t, "the mirror kept", func() bool { return prefs().KubectlMirror == "https://mirror.example.com/k8s" })
	q.until(t, "the refusal cleared", func() bool { return !p.errs["kubectlMirror"].Visible() })

	folder := t.TempDir()
	p.pickFolder = func(done func(string)) { done(folder) }
	browse := findAll[*tipButton](p.body)
	if len(browse) != 1 {
		t.Fatalf("%d folder buttons", len(browse))
	}
	test.Tap(browse[0])
	q.until(t, "the directory kept", func() bool { return prefs().KubectlDir == folder })

	path := entryOf(p.controls["kubectlPath"])
	path.SetText(filepath.Join(t.TempDir(), "missing"))
	q.until(t, "the path refused", func() bool { return p.errs["kubectlPath"].Visible() })
	k := filepath.Join(t.TempDir(), "kubectl")
	if err := os.WriteFile(k, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path.SetText(k)
	q.until(t, "the path kept", func() bool { return prefs().KubectlPath == k })

	// The page follows a change made elsewhere (an agent may switch downloads back on).
	mustDispatch(t, s, "preferences.set", map[string]any{"kubectlDownload": true})
	q.until(t, "the check follows", func() bool { return p.controls["kubectlDownload"].(*widget.Check).Checked })
}

// renderKubectlPreferences: Preferences › Kubernetes with the kubectl settings, a mirror and a path
// set, downloads on.
func renderKubectlPreferences(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	k := filepath.Join(t.TempDir(), "kubectl")
	if err := os.WriteFile(k, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustDispatch(t, s, "preferences.set", map[string]any{"theme": string(mode), "kubectlMirror": "https://mirror.example.com/kubernetes/kubectl"})
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "kubernetes"})
	q.until(t, "the page", func() bool {
		e := entryOf(sh.settings.controls["kubectlMirror"])
		return sh.settings.visible() && e != nil && e.Text != ""
	})
	savePNG(t, w, name)
}

func TestRenderKubectlPreferencesDark(t *testing.T) {
	renderKubectlPreferences(t, ThemeDark, "preferences_kubectl_dark.png")
}
func TestRenderKubectlPreferencesLight(t *testing.T) {
	renderKubectlPreferences(t, ThemeLight, "preferences_kubectl_light.png")
}
