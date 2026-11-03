package kinds_test

import (
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

func FuzzClusterRoleBindings(f *testing.F) { fuzzProfile(f, "clusterrolebindings") }

const accessNoSubjects = "No subjects: grants nothing"

func TestClusterRoleBindingTexts(t *testing.T) {
	checkStatusTexts(t, []statusText{
		{"clusterrolebindings", "healthy", "bindings", "metrics-collector, oncall:sre, alice@example.com", model.StatusNone, model.StatusNone},
		{"clusterrolebindings", "healthy", "signals", "", model.StatusNone, model.StatusNone},
		{"clusterrolebindings", "failing", "signals", accessNoSubjects, model.StatusWarning, model.StatusWarning},
		{"clusterrolebindings", "failing", "bindings", "", model.StatusNone, model.StatusWarning},
		// The API server's system:node binding has no subjects on purpose.
		{"clusterrolebindings", "bootstrap-policy", "signals", "", model.StatusNone, model.StatusNone},
		{"clusterrolebindings", "minimal", "signals", accessNoSubjects, model.StatusWarning, model.StatusWarning},
		{"clusterrolebindings", "terminating", "bindings", "dashboard", model.StatusNone, model.StatusTerminating},
	})
}

// TestClusterRoleBindingColumns pins the columns captured from Lens on 2026-10-11: Name, the
// warning icon, Bindings, Age; cluster-scoped, so no namespace filter.
func TestClusterRoleBindingColumns(t *testing.T) {
	assertColumnOrder(t, "clusterrolebindings", []model.Column{
		{ID: "name", Title: "Name"},
		{ID: "signals", Title: "Signals"},
		{ID: "bindings", Title: "Bindings"},
		{ID: "age", Title: "Age"},
	})
	p, _ := kinds.ByKey("clusterrolebindings")
	if p.Namespaced || !slices.ContainsFunc(p.Needs, func(ix model.RelatedIndex) bool { return ix.ID() == kinds.ClusterRolesByName.ID() }) {
		t.Errorf("clusterrolebindings: namespaced %v, needs %v", p.Namespaced, p.Needs)
	}
}

// accessRelated serves one related-objects index from objects, as the engine does from the cache.
type accessRelated map[string]map[string][]*unstructured.Unstructured

func accessRelatedFrom(ix model.RelatedIndex, objs ...*unstructured.Unstructured) accessRelated {
	byKey := map[string][]*unstructured.Unstructured{}
	for _, o := range objs {
		for _, k := range ix.Keys(o) {
			byKey[k] = append(byKey[k], o)
		}
	}
	return accessRelated{ix.ID(): byKey}
}

// Listed: a fake built without objects stands for a kind still listing.
func (r accessRelated) Listed(ix model.RelatedIndex) bool { return len(r[ix.ID()]) > 0 }

func (r accessRelated) Objects(ix model.RelatedIndex, key string) []*unstructured.Unstructured {
	return r[ix.ID()][key]
}

func accessClusterRole(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRole", "metadata": map[string]any{"name": name},
	}}
}

// TestClusterRoleBindingRoleNotFound: a binding warns that its cluster role does not exist only
// once the cached cluster roles say so; while they are listing (an empty index), or without them,
// nothing is known and nothing warns. The drawer's Role links to the cluster role, except when it
// is known not to exist.
func TestClusterRoleBindingRoleNotFound(t *testing.T) {
	p, _ := kinds.ByKey("clusterrolebindings")
	healthy := accessCase(t, "clusterrolebindings", "healthy")
	failing := accessCase(t, "clusterrolebindings", "failing")
	const notFound = "Cluster role metrics-collector not found: grants nothing"
	others := accessRelatedFrom(kinds.ClusterRolesByName, accessClusterRole("view"), accessClusterRole("edit"))
	with := accessRelatedFrom(kinds.ClusterRolesByName, accessClusterRole("view"), accessClusterRole("metrics-collector"))
	listing := accessRelatedFrom(kinds.ClusterRolesByName)

	tests := []struct {
		name    string
		obj     *unstructured.Unstructured
		rel     model.Related
		warning string
		link    bool
	}{
		{"role missing", healthy, others, notFound, false},
		{"role exists", healthy, with, "", true},
		{"cluster roles listing", healthy, listing, "", true},
		{"no related objects", healthy, model.NoRelated, "", true},
		{"role missing and no subjects", failing, accessRelatedFrom(kinds.ClusterRolesByName, accessClusterRole("view")),
			"Cluster role backup-operator not found, no subjects: grants nothing", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := model.BuildRowWith(p, "test", tt.obj, tt.rel)
			cell := row.Cells[p.ColumnIndex("signals")]
			wantStatus := model.StatusNone
			if tt.warning != "" {
				wantStatus = model.StatusWarning
			}
			if cell.Text != tt.warning || cell.Status != wantStatus || row.Status != wantStatus {
				t.Errorf("warning %q (%q, row %q), want %q (%q)", cell.Text, cell.Status, row.Status, tt.warning, wantStatus)
			}
			props := p.DetailSections(tt.obj, tt.rel)[0]
			role := accessItem(t, &props, "Role")
			if (role.Link != nil) != tt.link {
				t.Errorf("role item %+v, link wanted: %v", role, tt.link)
			}
			if tt.link && *role.Link != (model.ObjectRef{Kind: "clusterroles", Name: model.NestedString(tt.obj, "roleRef", "name")}) {
				t.Errorf("role link %+v", role.Link)
			}
		})
	}
}

// TestClusterRolesByNameKeys: a cluster role is found under its name only (Related.Listed says
// whether the index has listed); an object without a name, or with hostile metadata, under none.
func TestClusterRolesByNameKeys(t *testing.T) {
	keys := kinds.ClusterRolesByName.Keys
	if got := keys(accessClusterRole("view")); !slices.Equal(got, []string{"view"}) {
		t.Errorf("keys %q", got)
	}
	for _, obj := range []map[string]any{{}, {"metadata": nil}, {"metadata": "x"}, {"metadata": map[string]any{"name": int64(3)}}} {
		if got := keys(&unstructured.Unstructured{Object: obj}); got != nil {
			t.Errorf("keys of %v = %q", obj, got)
		}
	}
}

// TestBindingDetail: a binding's Role links to its cluster role or role, and its subjects are a
// table in which a ServiceAccount's name links to it, in the binding's namespace when the subject
// names none.
func TestBindingDetail(t *testing.T) {
	crb := accessDetail(t, "clusterrolebindings", "healthy")
	if r := accessItem(t, &crb[0], "Role"); r.Value != "ClusterRole metrics-collector" {
		t.Errorf("role %+v", r)
	}
	subjects := accessSection(t, crb, "Subjects").Table
	if want := []string{"Kind", "Name", "Namespace"}; !slices.Equal(subjects.Columns, want) {
		t.Fatalf("columns %q", subjects.Columns)
	}
	want := [][]string{{"ServiceAccount", "metrics-collector", "monitoring"}, {"Group", "oncall:sre", ""}, {"User", "alice@example.com", ""}}
	if !slices.EqualFunc(subjects.Rows, want, slices.Equal[[]string]) {
		t.Errorf("rows %q", subjects.Rows)
	}
	if l := subjects.Link(0, 1); l == nil || *l != (model.ObjectRef{Kind: "serviceaccounts", Namespace: "monitoring", Name: "metrics-collector"}) {
		t.Errorf("service account link %+v", l)
	}
	if subjects.Link(1, 1) != nil || subjects.Link(2, 1) != nil {
		t.Error("users and groups are not objects to link to")
	}

	rb := accessDetail(t, "rolebindings", "healthy")
	if r := accessItem(t, &rb[0], "Role"); r.Link == nil || *r.Link != (model.ObjectRef{Kind: "clusterroles", Name: "edit"}) {
		t.Errorf("role binding's cluster role %+v", r)
	}
	rbSubjects := accessSection(t, rb, "Subjects").Table
	if got := rbSubjects.Rows[1]; !slices.Equal(got, []string{"ServiceAccount", "checkout-api", "checkout"}) {
		t.Errorf("subject without a namespace %q", got)
	}
	if l := rbSubjects.Link(1, 1); l == nil || *l != (model.ObjectRef{Kind: "serviceaccounts", Namespace: "checkout", Name: "checkout-api"}) {
		t.Errorf("link of a subject without a namespace %+v", l)
	}
	toRole := accessDetail(t, "rolebindings", "role-ref")
	if r := accessItem(t, &toRole[0], "Role"); r.Value != "Role checkout-api" || r.Link == nil ||
		*r.Link != (model.ObjectRef{Kind: "roles", Namespace: "checkout", Name: "checkout-api"}) {
		t.Errorf("role binding's role %+v", r)
	}
	for _, s := range accessDetail(t, "clusterrolebindings", "failing") {
		if s.Title == "Subjects" {
			t.Error("a binding without subjects shows a Subjects table")
		}
	}
}
