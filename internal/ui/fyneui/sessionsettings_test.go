package fyneui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// D-69 items 4 and 8 in the window: Preferences › App › Restore the last session (off by default)
// and Cluster Settings › General › Login while not shown (on by default).

// The restore's check is off at first; the user's tap turns the preference on and off.
func TestPreferencesRestoreSession(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "app"})
	q.until(t, "the row", func() bool { _, ok := sh.settings.controls["restoreSession"].(*widget.Check); return ok })
	check := sh.settings.controls["restoreSession"].(*widget.Check)
	if check.Checked || check.Text != restoreSessionCheck {
		t.Fatalf("checked %v, label %q", check.Checked, check.Text)
	}
	test.Tap(check)
	q.until(t, "on", func() bool { p, _ := s.Preferences(); return p.RestoreSession && check.Checked })
	test.Tap(check)
	q.until(t, "off", func() bool { p, _ := s.Preferences(); return !p.RestoreSession && !check.Checked })
}

// The renewal's check is on at first; the user's tap holds the cluster's login in the background,
// and a change from elsewhere (an agent's off) shows at once.
func TestClusterSettingsRenewLogin(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "general"})
	q.until(t, "the row", func() bool {
		c, ok := sh.settings.controls["renewLoginInBackground"].(*widget.Check)
		return ok && c.Checked
	})
	check := sh.settings.controls["renewLoginInBackground"].(*widget.Check)
	if check.Text != renewLoginCheck {
		t.Errorf("label %q", check.Text)
	}
	test.Tap(check)
	q.until(t, "off", func() bool { return s.UIState().ClusterSettings["kind-dev"].HoldBackgroundLogin && !check.Checked })
	test.Tap(check)
	q.until(t, "on", func() bool { return !s.UIState().ClusterSettings["kind-dev"].HoldBackgroundLogin && check.Checked })
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "renewLoginInBackground": false})
	q.until(t, "off from elsewhere", func() bool { return !check.Checked })
}

// A change from elsewhere that comes before the window has shown the state of the user's own tap:
// the window takes the two states as one, which equals the state it showed before the tap, and the
// check shows the change all the same (TestClusterSettingsRenewLogin on CI, 2026-10-21 and 2026-10-22).
func TestClusterSettingsRenewLoginChangedElsewhereRightAfterATap(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "general"})
	q.until(t, "the row", func() bool { _, ok := sh.settings.controls["renewLoginInBackground"].(*widget.Check); return ok })
	check := sh.settings.controls["renewLoginInBackground"].(*widget.Check)
	test.Tap(check)
	// The window shows the off, and the page's read of its settings for it has answered: no answer
	// still under way shows the state again by chance.
	q.until(t, "off", func() bool {
		v := sh.settings.view
		return sh.ui.ClusterSettings["kind-dev"].HoldBackgroundLogin && v != nil && !v.RenewLoginInBackground
	})
	took := make(chan struct{}, 1)
	cancel := s.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventUIState && ev.UIState != nil && !ev.UIState.ClusterSettings["kind-dev"].HoldBackgroundLogin {
			select {
			case took <- struct{}{}:
			default:
			}
		}
	})
	defer cancel()
	test.Tap(check)
	select {
	case <-took: // the engine took the tap; the window has not run what it queued for it
	case <-time.After(15 * time.Second):
		t.Fatal("the tap never reached the engine")
	}
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "renewLoginInBackground": false})
	q.until(t, "off from elsewhere", func() bool { return !check.Checked })
}

// Preferences › App with the restore on, dark and light: the PNGs of the pull request (AGENTS.md
// rule 7).
func TestRenderPreferencesRestoreSessionDark(t *testing.T) {
	renderPreferencesRestoreSession(t, ThemeDark, "preferences_restore_session_dark.png")
}

func TestRenderPreferencesRestoreSessionLight(t *testing.T) {
	renderPreferencesRestoreSession(t, ThemeLight, "preferences_restore_session_light.png")
}

func renderPreferencesRestoreSession(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "preferences.set", map[string]any{"theme": string(mode), "timezone": "Europe/Berlin", "restoreSession": true})
	sh, w, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "app"})
	q.until(t, "the row", func() bool {
		c, ok := sh.settings.controls["restoreSession"].(*widget.Check)
		return ok && c.Checked
	})
	q.drain()
	savePNG(t, w, name)
}

// Cluster Settings › General with the renewal off, dark and light.
func TestRenderClusterSettingsLoginDark(t *testing.T) {
	renderClusterSettingsLogin(t, ThemeDark, "cluster_settings_login_dark.png")
}

func TestRenderClusterSettingsLoginLight(t *testing.T) {
	renderClusterSettingsLogin(t, ThemeLight, "cluster_settings_login_light.png")
}

func renderClusterSettingsLogin(t *testing.T, mode ThemeMode, name string) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "name": "Dev cluster", "iconColor": "violet",
		"renewLoginInBackground": false})
	sh, w, q := newEngineShell(t, s, Options{Theme: mode})
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev", "section": "general"})
	q.until(t, "the General section", func() bool {
		c, ok := sh.settings.controls["renewLoginInBackground"].(*widget.Check)
		name := entryOf(sh.settings.controls["name"])
		return sh.settings.visible() && ok && !c.Checked && name != nil && name.Text == "Dev cluster" // the settings read back
	})
	q.drain()
	savePNG(t, w, name)
}
