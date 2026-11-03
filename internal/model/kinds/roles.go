package kinds

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// roles has the columns captured from Lens on 2026-10-11: Name, the warning icon, Namespace, Age.
// The warning rule is inferred: a role without rules grants nothing. A role has no state, so a row
// without a warning has no status token.
var roles = &model.KindProfile{
	Key:        keyRoles,
	Group:      groupRBAC,
	Version:    "v1",
	Resource:   keyRoles,
	Kind:       kindRole,
	Title:      "Roles",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		signalsColumn,
		namespaceColumn,
		ageColumn,
	},
	Cells:  roleCells,
	Fields: roleFields(false),
	Detail: roleDetail,
}

func roleCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	warning, status := roleWarning(obj)
	return []model.Cell{
		nameCell(obj),
		warning,
		namespaceCell(obj),
		model.AgeCell(created(obj)),
	}, status
}

// roleDetail adds the warning to Properties, then the rules as a table (inferred).
func roleDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var props detailItems
	warning, _ := roleWarning(obj)
	props.addStatus(itemWarning, warning.Text, warning.Status)
	return rulesSection(withSection(nil, model.TitleProperties, props), obj)
}
