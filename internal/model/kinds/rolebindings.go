package kinds

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// roleBindings has the columns captured from Lens on 2026-10-11: Name, Namespace, Bindings (the
// subjects' names, comma-separated), Age, and no warning icon. Without a state slot in the Signals
// column (D-150) it reads no other kind: whether its role exists would cost watches on every Role and ClusterRole for the
// drawer alone. A binding has no state, so no status token.
var roleBindings = &model.KindProfile{
	Key:        "rolebindings",
	Group:      groupRBAC,
	Version:    "v1",
	Resource:   "rolebindings",
	Kind:       "RoleBinding",
	Title:      "Role Bindings",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		bindingsColumn,
		ageColumn,
	},
	Cells:  roleBindingCells,
	Fields: rbacBindingFields,
	Detail: roleBindingDetail,
}

func roleBindingCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	return []model.Cell{
		nameCell(obj),
		namespaceCell(obj),
		bindingsCell(obj),
		model.AgeCell(created(obj)),
	}, model.StatusNone
}

// roleBindingDetail adds the role, a link to the role or cluster role, and a warning for a binding
// without subjects to Properties, then the subjects as a table (inferred).
func roleBindingDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var props detailItems
	if c := subjectsCause(obj); c != "" {
		props.addStatus(itemWarning, grantsNothing([]string{c}), model.StatusWarning)
	}
	props.put(roleRefItem(obj, false))
	return subjectsSection(withSection(nil, model.TitleProperties, props), obj)
}
