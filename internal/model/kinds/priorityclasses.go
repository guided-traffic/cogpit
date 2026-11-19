package kinds

import (
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// priorityClasses has the columns of Lens's documentation (not captured): Name, Value, Global
// Default, Age, and the pod counts of Lens 2026.9 as a Pods column, the pods whose
// spec.priorityClassName names the class, read through the PodsByPriorityClass index. Inferred:
// the Pods column's title and place, Global Default as "true" or "false" (as Lens shows the Cron
// Jobs' Suspend), and the drawer. A priority class has no state, so no status token.
var priorityClasses = &model.KindProfile{
	Key:      "priorityclasses",
	Group:    "scheduling.k8s.io",
	Version:  "v1",
	Resource: "priorityclasses",
	Kind:     "PriorityClass",
	Title:    "Priority Classes",
	Columns: []model.Column{
		nameColumn,
		{ID: "value", Title: "Value", Type: model.ColumnNumber, Width: 110},
		{ID: "globalDefault", Title: "Global Default", Type: model.ColumnText, Width: 120},
		{ID: idPods, Title: titlePods, Type: model.ColumnNumber, Related: podsResource},
		ageColumn,
	},
	Needs:     []model.RelatedIndex{PodsByPriorityClass},
	CellsWith: priorityClassCells,
	// Cells and Detail are the same without the pods, for rows built outside a view.
	Cells: func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
		return priorityClassCells(obj, model.NoRelated)
	},
	Fields: map[string]fieldFunc{
		"globalDefault": func(obj *unstructured.Unstructured) []string {
			return []string{priorityClassDefault(obj)}
		},
		"preemptionPolicy": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "preemptionPolicy"))
		},
	},
	DetailWith: priorityClassDetail,
	Detail: func(obj *unstructured.Unstructured) []model.DetailSection {
		return priorityClassDetail(obj, model.NoRelated)
	},
}

func priorityClassCells(obj *unstructured.Unstructured, rel model.Related) ([]model.Cell, model.Status) {
	value, _ := model.NestedInt64(obj, "value")
	return []model.Cell{
		nameCell(obj),
		{Num: value},
		{Text: priorityClassDefault(obj)},
		{Num: int64(len(rel.Objects(PodsByPriorityClass, obj.GetName())))},
		model.AgeCell(created(obj)),
	}, model.StatusNone
}

// priorityClassDefault is the Global Default column: "true" for the class pods without a class
// get, else "false".
func priorityClassDefault(obj any) string {
	def, _ := model.NestedBool(obj, "globalDefault")
	return strconv.FormatBool(def)
}

// podPriorityClassKeys finds a pod under its priority class. The Priority admission plugin writes
// the default class's name into pods created without one, so they count for that class.
func podPriorityClassKeys(pod *unstructured.Unstructured) []string {
	return nonEmpty(model.NestedString(pod, "spec", "priorityClassName"))
}

func priorityClassDetail(obj *unstructured.Unstructured, rel model.Related) []model.DetailSection {
	var d detailItems
	if v, ok := model.NestedInt64(obj, "value"); ok {
		d.add("Value", itoa(v))
	}
	d.add("Global Default", priorityClassDefault(obj))
	d.add("Preemption Policy", model.NestedString(obj, "preemptionPolicy"))
	d.add("Description", model.NestedString(obj, "description"))
	d.add(titlePods, strconv.Itoa(len(rel.Objects(PodsByPriorityClass, obj.GetName()))))
	return []model.DetailSection{{Title: model.TitleProperties, Items: d}}
}
