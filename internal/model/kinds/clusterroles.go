package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// clusterRoles has the columns captured from Lens on 2026-10-11: Name, the warning icon, Age; no
// namespace filter (the kind is cluster-scoped). The warning rule is inferred: a cluster role
// without rules grants nothing, and an aggregated one whose rules the aggregation controller left
// empty matches no cluster role that grants anything. A cluster role has no state, so a row
// without a warning has no status token.
var clusterRoles = &model.KindProfile{
	Key:      keyClusterRoles,
	Group:    groupRBAC,
	Version:  "v1",
	Resource: keyClusterRoles,
	Kind:     kindClusterRole,
	Title:    "Cluster Roles",
	Columns: []model.Column{
		nameColumn,
		signalsColumn,
		ageColumn,
	},
	Cells:  clusterRoleCells,
	Fields: roleFields(true),
	Detail: clusterRoleDetail,
}

func clusterRoleCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	warning, status := roleWarning(obj)
	return []model.Cell{
		nameCell(obj),
		warning,
		model.AgeCell(created(obj)),
	}, status
}

// roleFields are the searchable fields of roles and cluster roles: the verbs and resources their
// rules grant, and, for cluster roles, the non-resource URLs.
func roleFields(cluster bool) map[string]fieldFunc {
	fields := map[string]fieldFunc{
		"verb": func(obj *unstructured.Unstructured) []string {
			return ruleValues(obj, "verbs")
		},
		idResource: func(obj *unstructured.Unstructured) []string {
			return ruleValues(obj, "resources")
		},
	}
	if cluster {
		fields["nonResourceURL"] = func(obj *unstructured.Unstructured) []string {
			return ruleValues(obj, "nonResourceURLs")
		}
	}
	return fields
}

// clusterRoleDetail adds the warning and the aggregation rule to Properties, then the rules as a
// table (inferred).
func clusterRoleDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var props detailItems
	warning, _ := roleWarning(obj)
	props.addStatus(itemWarning, warning.Text, warning.Status)
	if selectors, aggregated := aggregationSelectors(obj); aggregated {
		text := strings.Join(selectors, "\n")
		if text == "" {
			text = "No selectors"
		}
		props.add("Aggregation Rule", text)
	}
	return rulesSection(withSection(nil, model.TitleProperties, props), obj)
}
