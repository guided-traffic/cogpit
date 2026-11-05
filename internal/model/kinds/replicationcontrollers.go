package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// replicationControllers has the columns of Lens's documentation (not captured): Name, Namespace,
// Replicas (the current pods, status.replicas), Desired Replicas (spec.replicas, which the API
// defaults to 1) and Selector ("key=value, …"). Lens documents no Age column. The status rule is
// the replica sets' (inferred): a ReplicationController is a replica set with an equality-based
// selector.
var replicationControllers = &model.KindProfile{
	Key:        "replicationcontrollers",
	Version:    "v1",
	Resource:   "replicationcontrollers",
	Kind:       "ReplicationController",
	Title:      "Replication Controllers",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		{ID: idReplicas, Title: titleReplicas, Type: model.ColumnNumber},
		{ID: "desiredReplicas", Title: "Desired Replicas", Type: model.ColumnNumber},
		{ID: idSelector, Title: "Selector", Type: model.ColumnText, Hidden: true, Pairs: model.PairsSelector}, // the drawer shows it (D-new)
	},
	Cells: replicationControllerCells,
	Fields: map[string]fieldFunc{
		idImage: func(obj *unstructured.Unstructured) []string {
			return templateImages(obj)
		},
		idSelector: func(obj *unstructured.Unstructured) []string {
			return model.SortedPairs(model.NestedStringMap(obj, "spec", "selector"))
		},
		idControlledBy: controlledByField,
	},
	Detail: replicationControllerDetail,
}

func replicationControllerCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	desired := desiredReplicas(obj)
	current := nonNegative(obj, "status", "replicas")
	ready := nonNegative(obj, "status", "readyReplicas")
	return []model.Cell{
		nameCell(obj),
		namespaceCell(obj),
		{Num: current},
		{Num: desired},
		{Text: strings.Join(model.SortedPairs(model.NestedStringMap(obj, "spec", "selector")), ", ")},
	}, replicaSetStatus(obj, desired, current, ready)
}

// replicationControllerDetail is Cogpit's drawer for a ReplicationController (inferred, Lens's is
// not captured), as for a replica set: its pods by state, selector and images in Properties, then
// its conditions.
func replicationControllerDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	d.add(titlePods, itoa(desiredReplicas(obj))+" desired / "+
		itoa(nonNegative(obj, "status", "replicas"))+" current / "+
		itoa(nonNegative(obj, "status", "readyReplicas"))+" ready / "+
		itoa(nonNegative(obj, "status", "availableReplicas"))+" available")
	d.add("Selector", strings.Join(model.SortedPairs(model.NestedStringMap(obj, "spec", "selector")), "\n"))
	d.add("Images", strings.Join(templateImages(obj), "\n"))
	sections := withSection(nil, model.TitleProperties, d)
	if cs, ok := conditionSection(conditions(obj, "status", "conditions"), workloadConditionToken); ok {
		sections = append(sections, cs)
	}
	return sections
}
