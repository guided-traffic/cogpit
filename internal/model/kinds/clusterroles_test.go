package kinds_test

import (
	"fmt"
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

func FuzzClusterRoles(f *testing.F) { fuzzProfile(f, "clusterroles") }

// The warning texts of roles and cluster roles (the rules are inferred).
const (
	accessNoRules      = "No rules: grants nothing"
	accessNoAggregated = "Aggregates no rules: grants nothing"
)

func TestClusterRoleTexts(t *testing.T) {
	checkStatusTexts(t, []statusText{
		{"clusterroles", "healthy", "signals", "", model.StatusNone, model.StatusNone},
		{"clusterroles", "aggregated", "signals", "", model.StatusNone, model.StatusNone},
		{"clusterroles", "failing", "signals", accessNoRules, model.StatusWarning, model.StatusWarning},
		{"clusterroles", "aggregated-empty", "signals", accessNoAggregated, model.StatusWarning, model.StatusWarning},
		{"clusterroles", "minimal", "signals", accessNoRules, model.StatusWarning, model.StatusWarning},
		{"clusterroles", "terminating", "signals", "", model.StatusNone, model.StatusTerminating},
		{"clusterroles", "healthy", "age", "51d", model.StatusNone, model.StatusNone},
	})
}

// TestClusterRoleColumns pins the columns captured from Lens on 2026-10-11: Name, the warning
// icon, Age; cluster-scoped, so no namespace filter.
func TestClusterRoleColumns(t *testing.T) {
	assertColumnOrder(t, "clusterroles", []model.Column{
		{ID: "name", Title: "Name"},
		{ID: "signals", Title: "Signals"},
		{ID: "age", Title: "Age"},
	})
	if p, _ := kinds.ByKey("clusterroles"); p.Namespaced || p.GVR().Group != "rbac.authorization.k8s.io" {
		t.Errorf("clusterroles: namespaced %v, group %q", p.Namespaced, p.GVR().Group)
	}
}

// TestClusterRoleRulesTable: a row per rule, its lists an entry per line; the core group spelled
// out; the Non-Resource URLs column only when a rule has some; the aggregation rule a line per
// selector.
func TestClusterRoleRulesTable(t *testing.T) {
	healthy := accessDetail(t, "clusterroles", "healthy")
	rules := accessSection(t, healthy, "Rules").Table
	if want := []string{"API Groups", "Resources", "Non-Resource URLs", "Resource Names", "Verbs"}; !slices.Equal(rules.Columns, want) {
		t.Fatalf("columns %q, want %q", rules.Columns, want)
	}
	want := [][]string{
		{`"" (core)`, "nodes\nnodes/metrics\npods\nservices", "", "", "get\nlist\nwatch"},
		{"apps", "deployments\nstatefulsets", "", "", "get\nlist\nwatch"},
		{`"" (core)`, "configmaps", "", "metrics-collector-config", "get\nupdate"},
		{"", "", "/metrics\n/healthz", "", "get"},
	}
	if !slices.EqualFunc(rules.Rows, want, slices.Equal[[]string]) {
		t.Errorf("rows %q, want %q", rules.Rows, want)
	}

	aggregated := accessDetail(t, "clusterroles", "aggregated")
	if got := accessItem(t, &aggregated[0], "Aggregation Rule").Value; got != "rbac.example.com/aggregate-to-monitoring-edit=true\nrbac.example.com/aggregate-to in (monitoring,observability)" {
		t.Errorf("aggregation rule %q", got)
	}
	if cols := accessSection(t, aggregated, "Rules").Table.Columns; slices.Contains(cols, "Non-Resource URLs") {
		t.Errorf("a role without non-resource URLs shows the column: %q", cols)
	}
	empty := accessDetail(t, "clusterroles", "aggregated-empty")
	if it := accessItem(t, &empty[0], "Warning"); it.Value != accessNoAggregated || it.Status != model.StatusWarning {
		t.Errorf("warning item %+v", it)
	}
	for _, s := range empty {
		if s.Title == "Rules" {
			t.Error("a role without rules shows a Rules section")
		}
	}
}

// TestClusterRoleRulesCapped: a role with more rules than the drawer lists ends its table with
// how many more there are.
func TestClusterRoleRulesCapped(t *testing.T) {
	var rules []any
	for i := range 230 {
		rules = append(rules, map[string]any{"apiGroups": []any{"example.com"}, "resources": []any{fmt.Sprintf("widgets%d", i)}, "verbs": []any{"get"}})
	}
	p, _ := kinds.ByKey("clusterroles")
	obj := &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": "big"}, "rules": rules}}
	table := accessSection(t, p.Detail(obj), "Rules").Table
	if len(table.Rows) != 201 || table.Rows[200][0] != "+30 more rules (in the YAML)" {
		t.Errorf("%d rows, last %q", len(table.Rows), table.Rows[len(table.Rows)-1])
	}
}

// TestRoleFields: the verbs and resources a role's rules grant, each once, and a cluster role's
// non-resource URLs.
func TestRoleFields(t *testing.T) {
	healthy := accessCase(t, "clusterroles", "healthy")
	p, _ := kinds.ByKey("clusterroles")
	if got := model.FieldValues(p, "verb", healthy); !slices.Equal(got, []string{"get", "list", "watch", "update"}) {
		t.Errorf("verbs %q", got)
	}
	if got := model.FieldValues(p, "nonResourceURL", healthy); !slices.Equal(got, []string{"/metrics", "/healthz"}) {
		t.Errorf("non-resource URLs %q", got)
	}
	r, _ := kinds.ByKey("roles")
	if got := model.FieldValues(r, "resource", accessCase(t, "roles", "healthy")); !slices.Equal(got, []string{"configmaps", "leases"}) {
		t.Errorf("role resources %q", got)
	}
	if _, has := r.Fields["nonResourceURL"]; has {
		t.Error("roles cannot grant non-resource URLs")
	}
}

// accessDetail returns a recorded object's drawer sections.
func accessDetail(t *testing.T, key, name string) []model.DetailSection {
	t.Helper()
	p, ok := kinds.ByKey(key)
	if !ok {
		t.Fatalf("no profile %s", key)
	}
	return p.Detail(accessCase(t, key, name))
}

// accessCase returns one recorded object of a kind.
func accessCase(t *testing.T, key, name string) *unstructured.Unstructured {
	t.Helper()
	for _, c := range loadCases(t, key) {
		if c.name == name {
			return c.obj
		}
	}
	t.Fatalf("no testdata %s/%s", key, name)
	return nil
}

// accessSection returns the drawer section with the given title.
func accessSection(t *testing.T, sections []model.DetailSection, title string) *model.DetailSection {
	t.Helper()
	for i := range sections {
		if sections[i].Title == title {
			return &sections[i]
		}
	}
	t.Fatalf("no section %q in %+v", title, sections)
	return nil
}
