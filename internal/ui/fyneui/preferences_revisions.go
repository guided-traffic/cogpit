package fyneui

import (
	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Preferences › App › Revision diffs (the owner, 2026-10-25, at the design review of the Revisions
// tab: "der Nutzer [soll] in den App Settings Global seinen favorisierten Weg für die Anzeige
// konfigurieren können"; engine.Preferences.RevisionsDiff, D-131): which revision a comparison in
// a release's Revisions tab goes from, its old side (−), and which to, its new side (+).

// revisionsDiffChoices are the select's choices: its label and the preference's value.
var revisionsDiffChoices = []struct{ label, value string }{
	{"From the compared revision to the shown one", ""},
	{"From the older revision to the newer one", engine.RevisionsOlderToNewer},
	{"From the shown revision to the compared one", engine.RevisionsShownToCompared},
}

const (
	revisionsDiffTitle = "Revision diffs"
	revisionsDiffHelp  = "How a Helm release's Revisions tab compares two revisions: the first is the diff's old side (−), the second its new side (+)."
)

// revisionsDiffSetting returns the row and the function that shows the preference in it.
func (p *settingsPage) revisionsDiffSetting() (fyne.CanvasObject, func()) {
	labels := make([]string, len(revisionsDiffChoices))
	for i, c := range revisionsDiffChoices {
		labels[i] = c.label
	}
	sel := newSelectField(labels, func(label string) {
		for _, c := range revisionsDiffChoices {
			if c.label == label {
				p.setPreference("revisionsDiff", c.value)
			}
		}
	})
	sync := func() {
		cur := p.prefs().RevisionsDiff
		if cur == engine.RevisionsComparedToShown {
			cur = ""
		}
		for _, c := range revisionsDiffChoices {
			if c.value == cur {
				sel.SetSelected(c.label)
			}
		}
	}
	return p.setting("revisionsDiff", revisionsDiffTitle, revisionsDiffHelp, sel), sync
}

// revisionsDiff is the way the window reads a comparison of two revisions (the preference).
func (sh *shell) revisionsDiff() string {
	if p := sh.ui.Preferences; p != nil {
		return p.RevisionsDiff
	}
	return ""
}
