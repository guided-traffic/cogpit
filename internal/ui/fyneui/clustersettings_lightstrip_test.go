package fyneui

import (
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// TestClusterSettingsLightStrip (D-new): General's Light strip row lies under the accent colour:
// Static, and the animations in their frame (LS15). Without an accent colour it is disabled and
// says why; with one it shows Static, and a choice in either group sets the cluster's light strip
// through the GUI-only command, kept and shown at once, the other group cleared. While motion is
// reduced a chosen animation's warning says the strip stands still (LS13). Clearing the colour
// disables it again and keeps the choice.
func TestClusterSettingsLightStrip(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := openGeneral(t, s, "kind-dev", ThemeDark)
	static := sh.settings.controls["lightStripStatic"].(*widget.RadioGroup)
	anim := sh.settings.controls["lightStripAnim"].(*widget.RadioGroup)
	why := sh.settings.controls["lightStripWhy"].(*textLine)
	stands := sh.settings.controls["lightStripStands"].(*paragraph)
	if !static.Disabled() || !anim.Disabled() || !why.Visible() || why.text != lightStripNoAccent || static.Selected != "Static" || anim.Selected != "" {
		t.Fatalf("without an accent colour: disabled %v %v, reason shown %v (%q), selected %q %q",
			static.Disabled(), anim.Disabled(), why.Visible(), why.text, static.Selected, anim.Selected)
	}
	_, eight, _ := accentSwatches(t, sh)
	test.Tap(eight[4]) // red
	q.until(t, "the choice enabled", func() bool { return !static.Disabled() && !anim.Disabled() && !why.Visible() })

	anim.SetSelected("Scanner") // the user's click on Scanner
	q.until(t, "scanner kept and shown", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.LightStrip == engine.LightStripScanner && sh.ui.ClusterSettings["kind-dev"].LightStrip == engine.LightStripScanner &&
			static.Selected == "" && anim.Selected == "Scanner"
	})
	if stands.Visible() {
		t.Errorf("a scanner free to move warns: %q", stands.text)
	}

	mustDispatch(t, s, "cluster.settings.setLightStrip", map[string]any{"context": "kind-dev", "animation": "flow"})
	q.until(t, "the window's own change shown", func() bool { return anim.Selected == "Flow" })

	// Reduced motion: the flow stands, and the row says so.
	sh.systemReduce = func() bool { return true }
	sh.settings.syncControls()
	if !stands.Visible() || stands.text != lightStripReduced {
		t.Errorf("with motion reduced: warning shown %v (%q)", stands.Visible(), stands.text)
	}
	sh.systemReduce = func() bool { return false }

	static.SetSelected("Static")
	q.until(t, "static kept, the animations cleared", func() bool {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return engine.LightStripOf(cs) == engine.LightStripStatic && anim.Selected == "" && static.Selected == "Static" && !stands.Visible()
	})
	anim.SetSelected("Flow")
	q.until(t, "flow again", func() bool { return sh.ui.ClusterSettings["kind-dev"].LightStrip == engine.LightStripFlow })

	none, _, _ := accentSwatches(t, sh)
	test.Tap(none)
	q.until(t, "disabled again", func() bool { return anim.Disabled() && why.Visible() && anim.Selected == "Flow" && !stands.Visible() })
	if cs, _ := s.ClusterSettingsOf("kind-dev"); cs.LightStrip != engine.LightStripFlow {
		t.Errorf("clearing the colour changed the light strip: %+v", cs)
	}
}
