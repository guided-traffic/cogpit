package fyneui

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Preferences › App › Revision diffs reads from the compared revision to the shown one at first;
// the user's pick sets the preference, an agent's change shows in the row.
func TestPreferencesRevisionsDiff(t *testing.T) {
	s, _ := settingsEngine(t)
	sh, _, q := newEngineShell(t, s, Options{})
	mustDispatch(t, s, "preferences.open", map[string]any{"section": "app"})
	q.until(t, "the row", func() bool { _, ok := sh.settings.controls["revisionsDiff"].(*selectField); return ok })
	sel := sh.settings.controls["revisionsDiff"].(*selectField)
	if sel.Selected != revisionsDiffChoices[0].label || len(sel.Options) != 3 {
		t.Fatalf("selected %q of %q", sel.Selected, sel.Options)
	}
	sel.SetSelected(revisionsDiffChoices[1].label)
	q.until(t, "older to newer", func() bool { p, _ := s.Preferences(); return p.RevisionsDiff == engine.RevisionsOlderToNewer })
	mustDispatch(t, s, "preferences.set", map[string]any{"revisionsDiff": engine.RevisionsShownToCompared})
	q.until(t, "the agent's change in the row", func() bool { return sel.Selected == revisionsDiffChoices[2].label })
}
