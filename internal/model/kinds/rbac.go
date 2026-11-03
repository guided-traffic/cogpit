package kinds

import (
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// What the Access Control views share: Service Accounts, Cluster Roles, Roles, Cluster Role
// Bindings and Role Bindings. Their columns are captured from Lens (2026-10-11); their warning
// rules and drawers are Cogpit's (inferred), because the captures show neither.

const (
	groupRBAC          = "rbac.authorization.k8s.io"
	keyServiceAccounts = "serviceaccounts"
	keyClusterRoles    = "clusterroles"
	keyRoles           = "roles"
	kindClusterRole    = "ClusterRole"
	kindRole           = "Role"
	kindServiceAccount = "ServiceAccount"
	idBindings         = "bindings"
	itemWarning        = "Warning"
	itemRole           = "Role"
	// annotationAutoupdate marks the RBAC objects of the API server's bootstrap policy, which it
	// reconciles on every start (cluster-admin, system:node).
	annotationAutoupdate = "rbac.authorization.kubernetes.io/autoupdate"
	// rulesShown caps the rows of a role's Rules table; the YAML has every rule.
	rulesShown = 200
	// coreGroup is how the rules table shows the core API group, whose name is empty.
	coreGroup = `"" (core)`
)

// Why a role or binding warns. The rules are inferred, not captured from Lens: a role that grants
// nothing and a binding that grants nothing warn; what a role grants is not judged. The causes are
// joined into one warning by grantsNothing.
const (
	causeNoRules      = "no rules"
	causeNoAggregated = "aggregates no rules" // its selectors match no cluster role with rules
	causeNoSubjects   = "no subjects"
)

// grantsNothing renders the causes of a role or binding that grants nothing as its warning, short
// enough for the drawer's pill: "Cluster role x not found, no subjects: grants nothing".
func grantsNothing(causes []string) string {
	s := strings.Join(causes, ", ") + ": grants nothing"
	return strings.ToUpper(s[:1]) + s[1:]
}

// bindingsColumn is the binding views' Bindings column, wide: a binding often names several
// subjects.
var bindingsColumn = model.Column{ID: idBindings, Title: "Bindings", Type: model.ColumnText, Width: 380}

// clusterRoleKeys finds a cluster role under its name (ClusterRolesByName).
func clusterRoleKeys(obj *unstructured.Unstructured) []string {
	if name := model.NestedString(obj, "metadata", "name"); name != "" {
		return []string{name}
	}
	return nil
}

// clusterRoleMissing reports whether the cluster role a binding names is known not to exist: the
// index's kind is listed (Related.Listed) and no cluster role has the name. While the cluster
// roles are listing, or the user may not list them, nothing is known and nothing warns.
func clusterRoleMissing(rel model.Related, name string) bool {
	if name == "" || len(rel.Objects(ClusterRolesByName, name)) > 0 {
		return false
	}
	return rel.Listed(ClusterRolesByName)
}

// roleRules returns a role's rules that are objects; anything else in the list is skipped.
func roleRules(obj any) []any {
	return objectsIn(model.NestedSlice(obj, "rules"))
}

// objectsIn returns the entries of a list that are objects.
func objectsIn(list []any) []any {
	var out []any
	for _, e := range list {
		if _, ok := e.(map[string]any); ok {
			out = append(out, e)
		}
	}
	return out
}

// hasObject reports whether a list has an entry that is an object, without allocating: the
// warning rules run on every row build.
func hasObject(list []any) bool {
	for _, e := range list {
		if _, ok := e.(map[string]any); ok {
			return true
		}
	}
	return false
}

// ruleValues returns the unique values of one field (verbs, resources) over a role's rules: the
// searchable fields.
func ruleValues(obj any, field string) []string {
	var out []string
	for _, r := range roleRules(obj) {
		out = appendUnique(out, model.NestedStrings(r, field)...)
	}
	return out
}

// rulesTable renders a role's rules as a table, a row per rule: API Groups, Resources,
// Non-Resource URLs (only when a rule has some, which only cluster roles can), Resource Names
// and Verbs, each a list with an entry per line, so that the columns stay narrow enough for the
// drawer; nil without rules. An empty Resource Names cell means every object of the resources.
func rulesTable(obj any) *model.DetailTable {
	rules := roleRules(obj)
	if len(rules) == 0 {
		return nil
	}
	urls := false
	for _, r := range rules {
		if len(model.NestedStrings(r, "nonResourceURLs")) > 0 {
			urls = true
			break
		}
	}
	t := &model.DetailTable{Columns: []string{"API Groups", titleResources}}
	if urls {
		t.Columns = append(t.Columns, "Non-Resource URLs")
	}
	t.Columns = append(t.Columns, "Resource Names", "Verbs")
	for i, r := range rules {
		if i == rulesShown {
			more := make([]string, len(t.Columns))
			more[0] = "+" + strconv.Itoa(len(rules)-i) + " more rules (in the YAML)"
			t.Rows = append(t.Rows, more)
			break
		}
		groups := model.NestedStrings(r, "apiGroups")
		for j, g := range groups {
			if g == "" {
				groups[j] = coreGroup
			}
		}
		row := []string{strings.Join(groups, "\n"), strings.Join(model.NestedStrings(r, "resources"), "\n")}
		if urls {
			row = append(row, strings.Join(model.NestedStrings(r, "nonResourceURLs"), "\n"))
		}
		row = append(row, strings.Join(model.NestedStrings(r, "resourceNames"), "\n"),
			strings.Join(model.NestedStrings(r, "verbs"), "\n"))
		t.Rows = append(t.Rows, row)
	}
	return t
}

// rulesSection is a role's Rules section, or none without rules.
func rulesSection(sections []model.DetailSection, obj any) []model.DetailSection {
	if t := rulesTable(obj); t != nil {
		sections = append(sections, model.DetailSection{Title: "Rules", Table: t})
	}
	return sections
}

// subjects returns a binding's subjects that are objects.
func subjects(obj any) []any {
	return objectsIn(model.NestedSlice(obj, "subjects"))
}

// subjectNames is the Bindings column: the subjects' names, comma-separated, in their order
// (capture, 2026-10-11).
func subjectNames(obj any) []string {
	var out []string
	for _, s := range model.NestedSlice(obj, "subjects") {
		if n := model.NestedString(s, "name"); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// bindingsCell is a binding's cell of the Bindings column.
func bindingsCell(obj any) model.Cell {
	return model.Cell{Text: strings.Join(subjectNames(obj), ", ")}
}

// subjectNamespace is the namespace of a ServiceAccount subject: its own, or, in a RoleBinding,
// the binding's, as the authorizer reads a subject without one. Users and groups have none.
func subjectNamespace(s any, bindingNamespace string) string {
	if model.NestedString(s, "kind") != kindServiceAccount {
		return model.NestedString(s, "namespace")
	}
	if ns := model.NestedString(s, "namespace"); ns != "" {
		return ns
	}
	return bindingNamespace
}

// subjectsSection is a binding's Subjects section: a table of Kind, Name and Namespace in which a
// ServiceAccount's name links to it; none without subjects.
func subjectsSection(sections []model.DetailSection, obj any) []model.DetailSection {
	subs := subjects(obj)
	if len(subs) == 0 {
		return sections
	}
	bindingNS := model.NestedString(obj, "metadata", "namespace")
	cols := []model.Column{{ID: idKind, Title: titleKind, Type: model.ColumnText}, nameColumn, namespaceColumn}
	rows := make([][]model.Cell, 0, len(subs))
	links := false
	for _, s := range subs {
		kind, name, ns := model.NestedString(s, "kind"), model.NestedString(s, "name"), subjectNamespace(s, bindingNS)
		var link *model.ObjectRef
		if kind == kindServiceAccount && ns != "" {
			link = linkTo(keyServiceAccounts, ns, name)
		}
		links = links || link != nil
		rows = append(rows, []model.Cell{{Text: kind}, {Text: name, Link: link}, {Text: ns}})
	}
	t := model.CellTable(cols, rows)
	if !links { // a plain table: the texts say it all
		t.Types, t.Cells = nil, nil
	}
	return append(sections, model.DetailSection{Title: "Subjects", Table: t})
}

// roleRefLink links a binding's role reference to the role or cluster role; nil for a kind the
// API does not allow. A RoleBinding's Role is in the binding's namespace.
func roleRefLink(obj any) *model.ObjectRef {
	name := model.NestedString(obj, "roleRef", "name")
	switch model.NestedString(obj, "roleRef", "kind") {
	case kindClusterRole:
		return linkTo(keyClusterRoles, "", name)
	case kindRole:
		if ns := model.NestedString(obj, "metadata", "namespace"); ns != "" {
			return linkTo(keyRoles, ns, name)
		}
	}
	return nil
}

// roleRefItem is a binding's Role item, "ClusterRole cluster-admin", a link to the role; plain
// text when the role is known not to exist.
func roleRefItem(obj any, missing bool) model.DetailItem {
	kind, name := model.NestedString(obj, "roleRef", "kind"), model.NestedString(obj, "roleRef", "name")
	value := strings.TrimSpace(kind + " " + name)
	if name == "" {
		value = ""
	}
	var link *model.ObjectRef
	if !missing {
		link = roleRefLink(obj)
	}
	return linkedItem(itemRole, value, link)
}

// bootstrapPolicy reports whether an RBAC object belongs to the API server's bootstrap policy,
// which the API server maintains itself: system:node's binding has no subjects on purpose.
func bootstrapPolicy(obj any) bool {
	return model.NestedString(obj, "metadata", "annotations", annotationAutoupdate) == textTrue
}

// grantsNothingCell is the warning cell and row token of a role or binding: a warning with the
// causes it grants nothing; none without causes.
func grantsNothingCell(causes []string) (model.Cell, model.Status) {
	if len(causes) == 0 {
		return stateCell(model.StatusNone, ""), model.StatusNone
	}
	return stateCell(model.StatusWarning, grantsNothing(causes)), model.StatusWarning
}

// subjectsCause is the cause a binding without subjects grants nothing, or "" when it has
// subjects or belongs to the bootstrap policy.
func subjectsCause(obj any) string {
	if hasObject(model.NestedSlice(obj, "subjects")) || bootstrapPolicy(obj) {
		return ""
	}
	return causeNoSubjects
}

// roleWarning is the warning cell and row token of a role: it warns when it has no rules.
func roleWarning(obj any) (model.Cell, model.Status) {
	if hasObject(model.NestedSlice(obj, "rules")) {
		return grantsNothingCell(nil)
	}
	if _, aggregated := aggregationSelectors(obj); aggregated {
		return grantsNothingCell([]string{causeNoAggregated})
	}
	return grantsNothingCell([]string{causeNoRules})
}

// aggregationSelectors returns a cluster role's aggregation rule, a line per selector (a cluster
// role that matches any of them is aggregated), and whether the role has the rule at all.
func aggregationSelectors(obj any) ([]string, bool) {
	rule := model.NestedMap(obj, "aggregationRule")
	if rule == nil {
		return nil, false
	}
	var out []string
	for _, sel := range model.NestedSlice(rule, "clusterRoleSelectors") {
		if model.NestedMap(sel) == nil {
			continue
		}
		if parts := labelSelector(sel); len(parts) > 0 {
			out = append(out, strings.Join(parts, ", "))
		} else {
			out = append(out, "(every cluster role)") // an empty selector matches everything
		}
	}
	return out, true
}

// nameList returns the names of a list of object references (a service account's secrets).
func nameList(obj any, fields ...string) []string {
	var out []string
	for _, e := range model.NestedSlice(obj, fields...) {
		out = appendUnique(out, model.NestedString(e, "name"))
	}
	return out
}
