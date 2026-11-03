package kinds

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// clusterRoleBindings has the columns captured from Lens on 2026-10-11: Name, the warning icon,
// Bindings (the subjects' names, comma-separated), Age; no namespace filter (the kind is
// cluster-scoped). The warning rule is inferred: a binding grants nothing when the cluster role it
// names does not exist (read from the cached cluster roles through ClusterRolesByName) or when it
// has no subjects, unless the API server maintains it (system:node has none on purpose). A
// binding has no state, so a row without a warning has no status token.
var clusterRoleBindings = &model.KindProfile{
	Key:      "clusterrolebindings",
	Group:    groupRBAC,
	Version:  "v1",
	Resource: "clusterrolebindings",
	Kind:     "ClusterRoleBinding",
	Title:    "Cluster Role Bindings",
	Columns: []model.Column{
		nameColumn,
		signalsColumn,
		bindingsColumn,
		ageColumn,
	},
	Needs: []model.RelatedIndex{ClusterRolesByName},
	Cells: func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
		return clusterRoleBindingCells(obj, model.NoRelated)
	},
	CellsWith: clusterRoleBindingCells,
	Fields:    rbacBindingFields,
	Detail: func(obj *unstructured.Unstructured) []model.DetailSection {
		return clusterRoleBindingDetail(obj, model.NoRelated)
	},
	DetailWith: clusterRoleBindingDetail,
}

// rbacBindingFields are the searchable fields of both RBAC binding kinds: the role a binding names and its
// subjects' names, as the Bindings column shows them.
var rbacBindingFields = map[string]fieldFunc{
	"role": func(obj *unstructured.Unstructured) []string {
		return nonEmpty(model.NestedString(obj, "roleRef", "name"))
	},
	idBindings: func(obj *unstructured.Unstructured) []string {
		return subjectNames(obj)
	},
}

func clusterRoleBindingCells(obj *unstructured.Unstructured, rel model.Related) ([]model.Cell, model.Status) {
	warning, status := grantsNothingCell(clusterRoleBindingCauses(obj, rel))
	return []model.Cell{
		nameCell(obj),
		warning,
		bindingsCell(obj),
		model.AgeCell(created(obj)),
	}, status
}

// clusterRoleBindingCauses returns why a cluster role binding grants nothing: its cluster role is
// known not to exist (it grants nothing until the role is created), it has no subjects; none when
// it grants something.
func clusterRoleBindingCauses(obj any, rel model.Related) []string {
	var causes []string
	if clusterRoleRefMissing(obj, rel) {
		causes = append(causes, "cluster role "+model.NestedString(obj, "roleRef", "name")+" not found")
	}
	if c := subjectsCause(obj); c != "" {
		causes = append(causes, c)
	}
	return causes
}

// clusterRoleRefMissing reports whether the cluster role a binding names is known not to exist.
func clusterRoleRefMissing(obj any, rel model.Related) bool {
	return model.NestedString(obj, "roleRef", "kind") == kindClusterRole &&
		clusterRoleMissing(rel, model.NestedString(obj, "roleRef", "name"))
}

// clusterRoleBindingDetail adds the warning and the role, a link to the cluster role, to
// Properties, then the subjects as a table (inferred).
func clusterRoleBindingDetail(obj *unstructured.Unstructured, rel model.Related) []model.DetailSection {
	warning, _ := grantsNothingCell(clusterRoleBindingCauses(obj, rel))
	var props detailItems
	props.addStatus(itemWarning, warning.Text, warning.Status)
	props.put(roleRefItem(obj, clusterRoleRefMissing(obj, rel)))
	return subjectsSection(withSection(nil, model.TitleProperties, props), obj)
}
