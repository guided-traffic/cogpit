package fyneui

import (
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestClusterSettingsLightStrip (D-new): General's Light strip row lies under the accent colour.
// Without an accent colour it is disabled and says why; with one it shows Static, and a choice sets
// the cluster's light strip through the GUI-only command, kept and shown at once. Clearing the
// colour disables it again and keeps the choice.
func TestClusterSettingsLightStrip(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := openGeneral(t, s, "kind-dev", ThemeDark)
	choice := sh.settings.controls["lightStripChoice"].(*widget.RadioGroup)
	why := sh.settings.controls["lightStripWhy"].(*textLine)
	if !choice.Disabled() || !why.Visible() || why.text != lightStripNoAccent || choice.Selected != "Static" {
		t.Fatalf("without an accent colour: disabled %v, reason shown %v (%q), selected %q", choice.Disabled(), why.Visible(), why.text, choice.Selected)
	}
	_, eight, _ := accentSwatches(t, sh)
	test.Tap(eight[4]) // red
	q.until(t, "the choice enabled", func() bool { return !choice.Disabled() && !why.Visible() })

	choice.SetSelected("Scanner") // the user's click on Scanner
	q.until(t, "scanner kept and shown", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.LightStrip == engine.LightStripScanner && sh.ui.ClusterSettings["kind-dev"].LightStrip == engine.LightStripScanner
	})

	mustDispatch(t, s, "cluster.settings.setLightStrip", map[string]any{"context": "kind-dev", "animation": "flow"})
	q.until(t, "the window's own change shown", func() bool { return choice.Selected == "Flow" })

	none, _, _ := accentSwatches(t, sh)
	test.Tap(none)
	q.until(t, "disabled again", func() bool { return choice.Disabled() && why.Visible() && choice.Selected == "Flow" })
	if cs, _ := s.ClusterSettingsOf("kind-dev"); cs.LightStrip != engine.LightStripFlow {
		t.Errorf("clearing the colour changed the light strip: %+v", cs)
	}
}
