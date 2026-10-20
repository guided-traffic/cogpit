package fyneui

import (
	"testing"
	"time"
)

// Preferences › Kubernetes › Helm storage driver: the select shows the HELM_DRIVER preference
// (Secrets, Helm's default, at first), a choice is kept through preferences.set, and the select
// follows a change made elsewhere (an agent setting it back to secret).
func TestPreferencesHelmDriverSelect(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "kubernetes"})
	q.until(t, "the Kubernetes section", func() bool {
		sel, ok := sh.settings.controls["helmDriver"].(*selectField)
		return ok && sel.Selected == "Secrets (Helm's default)"
	})
	sel := sh.settings.controls["helmDriver"].(*selectField)
	if len(sel.Options) != 2 || sel.Options[1] != "ConfigMaps" {
		t.Fatalf("options %q", sel.Options)
	}
	sel.SetSelected("ConfigMaps")
	q.until(t, "the driver kept and shown", func() bool {
		p, _ := s.Preferences()
		return p.HelmDriver == "configmap" && sh.ui.Preferences != nil && sh.ui.Preferences.HelmDriver == "configmap"
	})
	mustDispatch(t, s, "preferences.set", map[string]any{"helmDriver": "secret"})
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		q.drain()
		sel, ok := sh.settings.controls["helmDriver"].(*selectField)
		if ok && sel.Selected == "Secrets (Helm's default)" {
			break
		}
		if time.Now().After(deadline) {
			p, _ := s.Preferences()
			t.Fatalf("the select shows %q, the preference is %q, the UI state's %+v", sel.Selected, p.HelmDriver, sh.ui.Preferences)
		}
	}
	if p, _ := s.Preferences(); p.HelmDriver != "" {
		t.Errorf("kept %q, want the default", p.HelmDriver)
	}
}

// Preferences › Kubernetes with the ConfigMaps driver chosen, dark and light: the PNGs of the pull
// request (AGENTS.md rule 7).
func TestRenderPreferencesHelmDriverDark(t *testing.T) {
	renderPreferencesHelmDriver(t, ThemeDark, "preferences_helm_driver_dark.png")
}

func TestRenderPreferencesHelmDriverLight(t *testing.T) {
	renderPreferencesHelmDriver(t, ThemeLight, "preferences_helm_driver_light.png")
}

func renderPreferencesHelmDriver(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "preferences.set", map[string]any{"theme": string(mode), "helmDriver": "configmap"})
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "kubernetes"})
	q.until(t, "the Kubernetes section", func() bool {
		sel, ok := sh.settings.controls["helmDriver"].(*selectField)
		return sh.settings.visible() && ok && sel.Selected == "ConfigMaps"
	})
	q.drain()
	savePNG(t, w, name)
}
