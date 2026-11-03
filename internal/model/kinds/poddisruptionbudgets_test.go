package kinds_test

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

func FuzzPodDisruptionBudgets(f *testing.F) { fuzzProfile(f, "poddisruptionbudgets") }

func TestPodDisruptionBudgetsStatusTexts(t *testing.T) {
	const (
		none    = model.StatusNone
		ok      = model.StatusOK
		warning = model.StatusWarning
		bad     = model.StatusError
	)
	checkStatusTexts(t, []statusText{
		// As captured from Lens (2026-10-11): "N/A" for a bound the budget does not set.
		{"poddisruptionbudgets", "healthy", "minAvailable", "2", none, ok},
		{"poddisruptionbudgets", "healthy", "maxUnavailable", "N/A", none, ok},
		{"poddisruptionbudgets", "minimal", "minAvailable", "N/A", none, model.StatusPending},
		{"poddisruptionbudgets", "minimal", "maxUnavailable", "N/A", none, model.StatusPending},
		{"poddisruptionbudgets", "terminating", "minAvailable", "N/A", none, model.StatusTerminating},
		{"poddisruptionbudgets", "terminating", "maxUnavailable", "1", none, model.StatusTerminating},

		{"poddisruptionbudgets", "healthy", "currentHealthy", "3", none, ok},
		{"poddisruptionbudgets", "healthy", "desiredHealthy", "2", none, ok},
		{"poddisruptionbudgets", "percent", "minAvailable", "50%", none, ok},
		{"poddisruptionbudgets", "minimal", "currentHealthy", "0", none, model.StatusPending},

		// The warning icon's rule is inferred, not captured.
		{"poddisruptionbudgets", "healthy", "signals", "", none, ok},
		{"poddisruptionbudgets", "failing", "signals", "Budget not met: 1 of 2 desired pods healthy", warning, warning},
		{"poddisruptionbudgets", "no-disruptions", "signals", "Allows no disruptions: evictions and node drains wait", warning, warning},
		{"poddisruptionbudgets", "no-pods", "signals", "", none, none},
		{"poddisruptionbudgets", "sync-failed", "signals", "The disruption controller could not compute the budget", bad, bad},
		{"poddisruptionbudgets", "minimal", "signals", "", none, model.StatusPending},
	})
}

// TestPodDisruptionBudgetColumns pins the columns captured from Lens on 2026-10-11.
func TestPodDisruptionBudgetColumns(t *testing.T) {
	assertColumnOrder(t, "poddisruptionbudgets", []model.Column{
		{ID: "name", Title: "Name"},
		{ID: "signals", Title: "Signals"},
		{ID: "namespace", Title: "Namespace"},
		{ID: "minAvailable", Title: "Min Available"},
		{ID: "maxUnavailable", Title: "Max Unavailable"},
		{ID: "currentHealthy", Title: "Current Healthy"},
		{ID: "desiredHealthy", Title: "Desired Healthy"},
		{ID: "age", Title: "Age"},
	})
}
