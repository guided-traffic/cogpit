package fyneui

import (
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A cluster's Settings › Node Conditions: a type typed and added is kept, Ready is refused under the
// field, a type is removed with its button, and an agent's clearing of the list shows at once.
func TestClusterNodeConditionsField(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	w.Resize(fyne.NewSize(1440, 900))
	mustDispatch(t, s, "cluster.settings.open", map[string]any{"context": "kind-dev"})
	q.until(t, "the page", sh.settings.visible)
	test.Tap(sh.settings.navItems[engine.SectionNodeConditions])
	q.until(t, "the section", func() bool { return sh.settings.controls["acceptedConditionEntry"] != nil })
	entry := entryOf(sh.settings.controls["acceptedConditionEntry"])
	kept := func() []string {
		cs, _ := s.ClusterSettingsOf("kind-dev")
		return cs.AcceptedNodeConditions
	}

	entry.SetText("FrequentContainerdRestart")
	test.Tap(sh.settings.controls["acceptedConditionAdd"].(*textButton))
	q.until(t, "the first type", func() bool {
		return slices.Equal(kept(), []string{"FrequentContainerdRestart"}) && slices.Equal(accessibleRows(sh), []string{"FrequentContainerdRestart"}) && entry.Text == ""
	})
	entry.SetText("CorruptDockerOverlay2")
	entry.OnSubmitted(entry.Text)
	q.until(t, "the second type, sorted", func() bool {
		return slices.Equal(accessibleRows(sh), []string{"CorruptDockerOverlay2", "FrequentContainerdRestart"})
	})
	settle(w)
	lookPNG(t, w, "cluster_settings_node_conditions.png")

	entry.SetText("Ready")
	test.Tap(sh.settings.controls["acceptedConditionAdd"].(*textButton))
	q.until(t, "the refusal", func() bool { return sh.settings.errs[acceptedConditionsSetting].Visible() })

	test.Tap(sh.settings.controls["remove:CorruptDockerOverlay2"].(*tipButton))
	q.until(t, "the type removed", func() bool { return slices.Equal(kept(), []string{"FrequentContainerdRestart"}) })

	mustDispatch(t, s, "cluster.settings.set", map[string]any{"context": "kind-dev", "acceptedNodeConditions": []string{}})
	q.until(t, "the list cleared", func() bool { return len(accessibleRows(sh)) == 0 })
}
