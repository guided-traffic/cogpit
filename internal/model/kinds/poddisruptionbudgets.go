package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

const (
	// pdbNotSet is what the bound columns read when the budget does not set that bound (capture,
	// 2026-10-11).
	pdbNotSet = "N/A"
	// pdbDisruptionAllowed is the budget's condition, False when no pod may be evicted;
	// pdbSyncFailed its reason when the disruption controller could not compute the budget.
	pdbDisruptionAllowed = "DisruptionAllowed"
	pdbSyncFailed        = "SyncFailed"
)

// Warning texts of the Pod Disruption Budgets view (the rule is inferred, not captured from Lens).
const (
	pdbTextSyncFailed = "The disruption controller could not compute the budget"
	pdbTextNoneMayGo  = "Allows no disruptions: evictions and node drains wait"
)

// podDisruptionBudgets has the columns captured from Lens on 2026-10-11: Name, the warning icon,
// Namespace, Min Available, Max Unavailable, Current Healthy, Desired Healthy, Age, and "N/A" for a
// bound the budget does not set. When the icon shows is not captured; the rule of pdbHealth is
// inferred, as is the drawer.
var podDisruptionBudgets = &model.KindProfile{
	Key:        "poddisruptionbudgets",
	Group:      "policy",
	Version:    "v1",
	Resource:   "poddisruptionbudgets",
	Kind:       "PodDisruptionBudget",
	Title:      "Pod Disruption Budgets",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		signalsColumn,
		namespaceColumn,
		{ID: "minAvailable", Title: "Min Available", Type: model.ColumnText},
		{ID: "maxUnavailable", Title: "Max Unavailable", Type: model.ColumnText},
		{ID: "currentHealthy", Title: "Current Healthy", Type: model.ColumnNumber, Width: 124},
		{ID: "desiredHealthy", Title: "Desired Healthy", Type: model.ColumnNumber, Width: 124},
		ageColumn,
	},
	Cells: pdbCells,
	Fields: map[string]fieldFunc{
		idSelector: func(obj *unstructured.Unstructured) []string {
			return labelSelector(obj, "spec", "selector")
		},
	},
	Detail: pdbDetail,
}

func pdbCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	warning, status := pdbHealth(obj)
	return []model.Cell{
		nameCell(obj),
		warning,
		namespaceCell(obj),
		{Text: pdbBound(obj, "minAvailable")},
		{Text: pdbBound(obj, "maxUnavailable")},
		{Num: nonNegative(obj, "status", "currentHealthy")},
		{Num: nonNegative(obj, "status", "desiredHealthy")},
		model.AgeCell(created(obj)),
	}, status
}

// pdbBound is spec.minAvailable or spec.maxUnavailable, a count or a percentage, or "N/A".
func pdbBound(obj any, field string) string {
	if v := model.NestedIntOrString(obj, "spec", field); v != "" {
		return v
	}
	return pdbNotSet
}

// pdbHealth returns the warning cell and the row's token. The rule is inferred: a budget the
// disruption controller could not compute is an error; one whose healthy pods fall short of the
// desired count, or that allows no disruption at all, is a warning, because evictions and node
// drains wait on it; a budget whose selector matches no pod guards nothing and is not coloured;
// without a status the controller has not seen it yet.
func pdbHealth(obj any) (model.Cell, model.Status) {
	if model.NestedMap(obj, "status") == nil {
		return model.Cell{}, model.StatusPending
	}
	if c := pdbCondition(obj); c.Status == condFalse && c.Reason == pdbSyncFailed {
		return stateCell(model.StatusError, pdbTextSyncFailed), model.StatusError
	}
	if nonNegative(obj, "status", "expectedPods") == 0 {
		return model.Cell{}, model.StatusNone
	}
	current := nonNegative(obj, "status", "currentHealthy")
	desired := nonNegative(obj, "status", "desiredHealthy")
	switch {
	case current < desired:
		return stateCell(model.StatusWarning, pdbNotMet(current, desired)), model.StatusWarning
	case nonNegative(obj, "status", "disruptionsAllowed") == 0:
		return stateCell(model.StatusWarning, pdbTextNoneMayGo), model.StatusWarning
	}
	return model.Cell{}, model.StatusOK
}

// pdbNotMet is the warning of a budget short of healthy pods: "Budget not met: 1 of 2 desired
// pods healthy".
func pdbNotMet(current, desired int64) string {
	return "Budget not met: " + itoa(current) + " of " + itoa(desired) + " desired pods healthy"
}

// pdbCondition returns the budget's DisruptionAllowed condition, or a zero one.
func pdbCondition(obj any) condition {
	c, _ := findCondition(conditions(obj, "status", "conditions"), pdbDisruptionAllowed)
	return c
}

func pdbDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	warning, _ := pdbHealth(obj)
	d.addStatus("Warning", warning.Text, warning.Status)
	d.add("Selector", strings.Join(labelSelector(obj, "spec", "selector"), "\n"))
	d.add("Min Available", pdbBound(obj, "minAvailable"))
	d.add("Max Unavailable", pdbBound(obj, "maxUnavailable"))
	d.add("Unhealthy Pod Eviction Policy", model.NestedString(obj, "spec", "unhealthyPodEvictionPolicy"))
	for _, f := range []struct{ field, name string }{
		{"currentHealthy", "Current Healthy"},
		{"desiredHealthy", "Desired Healthy"},
		{"disruptionsAllowed", "Disruptions Allowed"},
		{"expectedPods", "Expected Pods"},
	} {
		if n, ok := model.NestedInt64(obj, "status", f.field); ok {
			d.add(f.name, itoa(n))
		}
	}
	sections := withSection(nil, model.TitleProperties, d)
	if cs, ok := conditionSection(conditions(obj, "status", "conditions"), pdbConditionToken); ok {
		sections = append(sections, cs)
	}
	return sections
}

// pdbConditionToken colours DisruptionAllowed: True is fine, False a warning (no pod may go), and
// an error when the controller could not compute the budget.
func pdbConditionToken(c condition) model.Status {
	if c.Type == pdbDisruptionAllowed && c.Status == condFalse && c.Reason == pdbSyncFailed {
		return model.StatusError
	}
	return conditionToken(c.Status, model.StatusOK, model.StatusWarning)
}
