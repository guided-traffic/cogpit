package kinds_test

import (
	"slices"
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
)

func FuzzRoles(f *testing.F) { fuzzProfile(f, "roles") }

func TestRoleTexts(t *testing.T) {
	checkStatusTexts(t, []statusText{
		{"roles", "healthy", "signals", "", model.StatusNone, model.StatusNone},
		{"roles", "healthy", "namespace", "checkout", model.StatusNone, model.StatusNone},
		{"roles", "failing", "signals", accessNoRules, model.StatusWarning, model.StatusWarning},
		{"roles", "minimal", "signals", accessNoRules, model.StatusWarning, model.StatusWarning},
		{"roles", "terminating", "signals", "", model.StatusNone, model.StatusTerminating},
	})
}

// TestRoleColumns pins the columns captured from Lens on 2026-10-11: Name, the warning icon,
// Namespace, Age.
func TestRoleColumns(t *testing.T) {
	assertColumnOrder(t, "roles", []model.Column{
		{ID: "name", Title: "Name"},
		{ID: "signals", Title: "Signals"},
		{ID: "namespace", Title: "Namespace"},
		{ID: "age", Title: "Age"},
	})
}

// TestRoleDetail: a role's drawer has the rules table without the Non-Resource URLs column, and
// adds nothing to Properties when it grants something.
func TestRoleDetail(t *testing.T) {
	healthy := accessDetail(t, "roles", "healthy")
	if len(healthy) != 1 {
		t.Fatalf("sections %+v, want only Rules", healthy)
	}
	table := accessSection(t, healthy, "Rules").Table
	if want := []string{"API Groups", "Resources", "Resource Names", "Verbs"}; !slices.Equal(table.Columns, want) {
		t.Errorf("columns %q, want %q", table.Columns, want)
	}
	if want := []string{`"" (core)`, "configmaps", "checkout-feature-flags", "update\npatch"}; !slices.Equal(table.Rows[1], want) {
		t.Errorf("row %q, want %q", table.Rows[1], want)
	}
	failing := accessDetail(t, "roles", "failing")
	if len(failing) != 1 || failing[0].Title != model.TitleProperties || accessItem(t, &failing[0], "Warning").Value != accessNoRules {
		t.Errorf("failing sections %+v", failing)
	}
}
