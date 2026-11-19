package kinds_test

import (
	"testing"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

func FuzzIngressClasses(f *testing.F) { fuzzProfile(f, "ingressclasses") }

func TestIngressClassTexts(t *testing.T) {
	checkStatusTexts(t, []statusText{
		{"ingressclasses", "healthy", "default", "Yes", model.StatusNone, model.StatusNone},
		{"ingressclasses", "healthy", "controller", "k8s.io/ingress-nginx", model.StatusNone, model.StatusNone},
		{"ingressclasses", "healthy", "scope", "", model.StatusNone, model.StatusNone},
		// Only "true" makes a class the default; "True" does not.
		{"ingressclasses", "failing", "default", "", model.StatusNone, model.StatusNone},
		{"ingressclasses", "terminating", "kind", "IngressClassParams", model.StatusNone, model.StatusTerminating},
		{"ingressclasses", "namespace-parameters", "scope", "Namespace", model.StatusNone, model.StatusNone},
		{"ingressclasses", "namespace-parameters", "apiGroup", "gateway.example.com", model.StatusNone, model.StatusNone},
		// The API server defaults the scope of parameters to Cluster.
		{"ingressclasses", "unscoped-parameters", "scope", "Cluster", model.StatusNone, model.StatusNone},
		{"ingressclasses", "minimal", "controller", "", model.StatusNone, model.StatusNone},
		// The Namespace column is empty: the kind is cluster-scoped, but Lens shows the column.
		{"ingressclasses", "namespace-parameters", "namespace", "", model.StatusNone, model.StatusNone},
	})
}

// TestIngressClassColumns pins the columns captured from Lens: Name, the star column (Default here:
// Fyne's bundled fonts have no star), an empty Namespace, Controller, API Group, Scope, Kind, and
// no Age.
func TestIngressClassColumns(t *testing.T) {
	assertColumnOrder(t, "ingressclasses", []model.Column{
		{ID: "name", Title: "Name"},
		{ID: "default", Title: "Default"},
		{ID: "namespace", Title: "Namespace"},
		{ID: "controller", Title: "Controller"},
		{ID: "apiGroup", Title: "API Group"},
		{ID: "scope", Title: "Scope"},
		{ID: "kind", Title: "Kind"},
	})
	if p, _ := kinds.ByKey("ingressclasses"); p.Namespaced || p.GVR().Group != "networking.k8s.io" {
		t.Errorf("ingressclasses: namespaced %v, group %q; want cluster-scoped networking.k8s.io", p.Namespaced, p.GVR().Group)
	}
}

// assertColumnOrder asserts a profile's column ids and titles, in order.
func assertColumnOrder(t *testing.T, key string, want []model.Column) {
	t.Helper()
	p, ok := kinds.ByKey(key)
	if !ok {
		t.Fatalf("no built-in profile %s", key)
	}
	if len(p.Columns) != len(want) {
		t.Fatalf("%s: %d columns, want %d", key, len(p.Columns), len(want))
	}
	for i, c := range p.Columns {
		if c.ID != want[i].ID || c.Title != want[i].Title {
			t.Errorf("%s column %d = %s %q, want %s %q", key, i, c.ID, c.Title, want[i].ID, want[i].Title)
		}
	}
}
