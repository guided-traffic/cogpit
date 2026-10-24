package fyneui

import (
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Preferences › App › Light strip on battery is off at first; the user's tap turns the preference
// on and off.
func TestPreferencesLightStripMainsOnly(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "app"})
	q.until(t, "the row", func() bool { _, ok := sh.settings.controls["lightStripMainsOnly"].(*widget.Check); return ok })
	check := sh.settings.controls["lightStripMainsOnly"].(*widget.Check)
	if check.Checked || check.Text != lightStripPowerCheck {
		t.Fatalf("checked %v, label %q", check.Checked, check.Text)
	}
	test.Tap(check)
	q.until(t, "on", func() bool { p, _ := s.Preferences(); return p.LightStripMainsOnly && check.Checked })
	test.Tap(check)
	q.until(t, "off", func() bool { p, _ := s.Preferences(); return !p.LightStripMainsOnly && !check.Checked })
	// An agent's change shows in the row.
	mustDispatch(t, s, "preferences.set", map[string]any{"lightStripMainsOnly": true})
	q.until(t, "an agent's on", func() bool { return check.Checked })
}

// The battery stops the strip only while the preference asks for it, and the power source is not
// read without it.
func TestBatteryStopsStrip(t *testing.T) {
	for _, c := range []struct {
		prefs     *engine.Preferences
		battery   bool
		want      bool
		wantReads int
	}{
		{nil, true, false, 0},
		{&engine.Preferences{}, true, false, 0},
		{&engine.Preferences{}, false, false, 0},
		{&engine.Preferences{LightStripMainsOnly: true}, false, false, 1},
		{&engine.Preferences{LightStripMainsOnly: true}, true, true, 1},
	} {
		reads := 0
		sh := &shell{systemBattery: func() bool { reads++; return c.battery }}
		sh.ui.Preferences = c.prefs
		if got := sh.batteryStopsStrip(); got != c.want || reads != c.wantReads {
			t.Errorf("preferences %+v, battery %v: %v after %d reads, want %v after %d", c.prefs, c.battery, got, reads, c.want, c.wantReads)
		}
	}
	// Without a reader (a shell not built by newShell) nothing stops it.
	sh := &shell{}
	sh.ui.Preferences = &engine.Preferences{LightStripMainsOnly: true}
	if sh.batteryStopsStrip() {
		t.Error("stopped without a reader")
	}
}
