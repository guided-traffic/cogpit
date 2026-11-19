package kinds

import (
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// keyMutatingAdmissionPolicies is the kind key a mutating binding's Policy cell links to.
const keyMutatingAdmissionPolicies = "mutatingadmissionpolicies"

// mutatingAdmissionPolicies has the columns of Lens's documentation (not captured): Name,
// Mutations (how many the policy has), Reinvocation Policy, Failure Policy, Age. The view needs
// admissionregistration.k8s.io/v1 (Kubernetes 1.36 and later): Lens showed it on 1.36.2 and not on
// 1.35.x (capture), where the kind is v1beta1 and off by default, so it has no fallback versions.
// The kind has no status, so no status token; the drawer is inferred.
var mutatingAdmissionPolicies = &model.KindProfile{
	Key:      keyMutatingAdmissionPolicies,
	Group:    groupAdmission,
	Version:  "v1",
	Resource: "mutatingadmissionpolicies",
	Kind:     "MutatingAdmissionPolicy",
	Title:    "Mutating Admission Policies",
	Columns: []model.Column{
		nameColumn,
		{ID: "mutations", Title: "Mutations", Type: model.ColumnNumber},
		{ID: "reinvocationPolicy", Title: itemReinvocationPolicy, Type: model.ColumnText},
		{ID: idFailurePolicy, Title: itemFailurePolicy, Type: model.ColumnText},
		ageColumn,
	},
	Cells: mutatingPolicyCells,
	Fields: map[string]fieldFunc{
		idFailurePolicy: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", "failurePolicy"))
		},
	},
	Detail: mutatingPolicyDetail,
}

func mutatingPolicyCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	return []model.Cell{
		nameCell(obj),
		{Num: int64(len(model.NestedSlice(obj, "spec", "mutations")))},
		{Text: model.NestedString(obj, "spec", "reinvocationPolicy")},
		{Text: model.NestedString(obj, "spec", "failurePolicy")},
		model.AgeCell(created(obj)),
	}, model.StatusNone
}

func mutatingPolicyDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	d.add(itemFailurePolicy, model.NestedString(obj, "spec", "failurePolicy"))
	d.add(itemReinvocationPolicy, model.NestedString(obj, "spec", "reinvocationPolicy"))
	d.addPolicySpec(obj)
	sections := withSection(nil, model.TitleProperties, d)

	for i, m := range model.NestedSlice(obj, "spec", "mutations") {
		var items detailItems
		items.add("Patch Type", model.NestedString(m, "patchType"))
		items.add("Apply Configuration", model.NestedString(m, "applyConfiguration", "expression"))
		items.add("JSON Patch", model.NestedString(m, "jsonPatch", "expression"))
		sections = append(sections, model.DetailSection{
			Title: "Mutation " + strconv.Itoa(i+1), Group: "Mutations", Items: items,
		})
	}
	return sections
}
