package engine

import "testing"

// A comparison of the revision shown against the one compared reads the way the preference says.
func TestRevisionsDiffSides(t *testing.T) {
	for _, c := range []struct {
		way              string
		shown, compared  int
		wantFrom, wantTo int
	}{
		{"", 5, 6, 6, 5}, {RevisionsComparedToShown, 6, 3, 3, 6},
		{RevisionsOlderToNewer, 5, 6, 5, 6}, {RevisionsOlderToNewer, 6, 3, 3, 6},
		{RevisionsShownToCompared, 5, 6, 5, 6}, {RevisionsShownToCompared, 6, 3, 6, 3},
	} {
		if from, to := RevisionsDiffSides(c.way, c.shown, c.compared); from != c.wantFrom || to != c.wantTo {
			t.Errorf("%q, shown %d, compared %d: %d → %d, want %d → %d", c.way, c.shown, c.compared, from, to, c.wantFrom, c.wantTo)
		}
	}
	for _, ok := range []string{"", RevisionsComparedToShown, RevisionsOlderToNewer, RevisionsShownToCompared} {
		if err := CheckRevisionsDiff(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	if err := CheckRevisionsDiff("newerToOlder"); err == nil {
		t.Error("an unknown way was taken")
	}
	p := DefaultPreferences()
	p.RevisionsDiff = "sideways"
	if p.Check() == nil {
		t.Error("preferences with an unknown way passed their check")
	}
}
