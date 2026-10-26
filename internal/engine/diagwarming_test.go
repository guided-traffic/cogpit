package engine

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

// A line that is no problem is grey whatever its kind: a cache warming up once is information, the
// same line repeated is a warning (D-135, the owner's option a).
func TestDiagEntrySeverity(t *testing.T) {
	for _, tc := range []struct {
		e    cluster.DiagEntry
		want string
	}{
		{cluster.DiagEntry{Kind: cluster.DiagWarming}, SeverityInfo},
		{cluster.DiagEntry{Kind: cluster.DiagWarming, Problem: true}, SeverityWarning},
		{cluster.DiagEntry{Kind: cluster.DiagTooMany, Problem: true}, SeverityWarning},
		{cluster.DiagEntry{Kind: cluster.DiagRefused, Problem: true}, SeverityError},
		{cluster.DiagEntry{Kind: cluster.DiagConnect}, SeverityInfo},
	} {
		if got := DiagEntrySeverity(tc.e); got != tc.want {
			t.Errorf("%s (problem %v): %s, want %s", tc.e.Kind, tc.e.Problem, got, tc.want)
		}
	}
	if got := DiagKindLabel(string(cluster.DiagWarming)); got != "cache warming" {
		t.Errorf("label %q", got)
	}
}
