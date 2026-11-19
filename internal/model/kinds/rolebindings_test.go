package kinds_test

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

func FuzzRoleBindings(f *testing.F) { fuzzProfile(f, "rolebindings") }

func TestRoleBindingTexts(t *testing.T) {
	checkStatusTexts(t, []statusText{
		{"rolebindings", "healthy", "bindings", "ci-deployer, checkout-api, team:checkout", model.StatusNone, model.StatusNone},
		{"rolebindings", "healthy", "namespace", "checkout", model.StatusNone, model.StatusNone},
		// No warning icon for role bindings (capture, 2026-10-11): a binding without subjects is
		// not coloured in the list.
		{"rolebindings", "failing", "bindings", "", model.StatusNone, model.StatusNone},
		{"rolebindings", "terminating", "bindings", "preview-bot", model.StatusNone, model.StatusTerminating},
		{"rolebindings", "minimal", "bindings", "", model.StatusNone, model.StatusNone},
	})
}

// TestRoleBindingColumns pins the columns captured from Lens on 2026-10-11: Name, Namespace,
// Bindings, Age, and no warning icon; role bindings read no other kind.
func TestRoleBindingColumns(t *testing.T) {
	assertColumnOrder(t, "rolebindings", []model.Column{
		{ID: "name", Title: "Name"},
		{ID: "namespace", Title: "Namespace"},
		{ID: "bindings", Title: "Bindings"},
		{ID: "age", Title: "Age"},
	})
	if p, _ := kinds.ByKey("rolebindings"); len(p.Needs) != 0 {
		t.Errorf("rolebindings needs %v", p.Needs)
	}
}

// TestRoleBindingNoSubjectsWarnsInTheDrawer: without a state slot in the Signals column, only the
// drawer says that a binding without subjects grants nothing.
func TestRoleBindingNoSubjectsWarnsInTheDrawer(t *testing.T) {
	failing := accessDetail(t, "rolebindings", "failing")
	if it := accessItem(t, &failing[0], "Warning"); it.Value != accessNoSubjects || it.Status != model.StatusWarning {
		t.Errorf("warning item %+v", it)
	}
}
