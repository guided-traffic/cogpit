package fyneui

import (
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// The title bar's appearance toggle switches the window between its dark and light look through
// preferences.set, so the preference holds the explicit look and a restart keeps it; the icon is
// the look a click goes to.
func TestThemeToggleSwitchesTheLook(t *testing.T) {
	s, _ := settingsEngine(t)
	mustDispatch(t, s, "preferences.set", map[string]any{"theme": string(ThemeDark)})
	sh, _, q := newEngineShell(t, s, Options{})
	b := sh.top.appearance
	q.until(t, "the dark look", func() bool { return b.shownDark() })
	if b.tipText() != "Switch to light" {
		t.Errorf("tooltip in the dark: %q", b.tipText())
	}
	if r, ok := test.WidgetRenderer(b).(*themeToggleRenderer); !ok || r.img.res != iconSun {
		t.Error("the dark look shows no sun")
	}

	test.Tap(b)
	q.until(t, "the light look", func() bool {
		p, err := s.Preferences()
		return err == nil && p.Theme == string(ThemeLight) && !b.shownDark()
	})
	if b.tipText() != "Switch to dark" {
		t.Errorf("tooltip in the light: %q", b.tipText())
	}

	test.Tap(b)
	q.until(t, "dark again", func() bool {
		p, err := s.Preferences()
		return err == nil && p.Theme == string(ThemeDark) && b.shownDark()
	})
}

// A run started with --theme keeps its look: the toggle changes nothing and says why.
func TestThemeToggleFollowsTheThemeFlag(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{Theme: ThemeLight})
	b := sh.top.appearance
	q.drain()
	test.Tap(b)
	q.drain()
	if p, err := s.Preferences(); err != nil || p.Theme == string(ThemeDark) {
		t.Errorf("the toggle set the preference under --theme: %+v %v", p, err)
	}
	if want := "This run was started with --theme light; Preferences › App › Theme applies from the next start"; b.tipText() != want {
		t.Errorf("tooltip: %q", b.tipText())
	}
	if sh.app.Settings().Theme().(*cogTheme).variant(theme.VariantDark) != theme.VariantLight {
		t.Error("the run's look changed")
	}
}
