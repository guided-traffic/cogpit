package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Ids and values the workload profiles share.
const (
	groupApps      = "apps"
	titlePods      = "Pods"
	idPods         = podsResource // the Pods column: ready/desired pods
	idImage        = "image"
	idNodeSelector = "nodeSelector"
	strategyOnDel  = "OnDelete"
)

var daemonSets = &model.KindProfile{
	Key:        "daemonsets",
	Group:      groupApps,
	Version:    "v1",
	Resource:   "daemonsets",
	Kind:       "DaemonSet",
	Title:      "Daemon Sets",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		{ID: idPods, Title: titlePods, Type: model.ColumnNumber},                                                       // ready/desired, sorted by ready
		{ID: idNodeSelector, Title: "Node Selector", Type: model.ColumnText, Hidden: true, Pairs: model.PairsSelector}, // the drawer shows it (D-new)
		ageColumn,
	},
	Cells: daemonSetCells,
	Fields: map[string]fieldFunc{
		idImage: func(obj *unstructured.Unstructured) []string {
			return templateImages(obj)
		},
		idSelector: func(obj *unstructured.Unstructured) []string {
			return labelSelector(obj, "spec", "selector")
		},
		idNodeSelector: func(obj *unstructured.Unstructured) []string {
			return model.SortedPairs(model.NestedStringMap(obj, "spec", "template", "spec", "nodeSelector"))
		},
	},
	Detail: daemonSetDetail,
	// The Pods table in the drawer (workloadpods.go, D-40).
	DetailNeeds: directPodsNeeds,
	DetailWith:  withPods(daemonSetDetail, nil),
}

func daemonSetCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	desired := nonNegative(obj, "status", "desiredNumberScheduled")
	ready := nonNegative(obj, "status", "numberReady")
	nodeSelector := model.SortedPairs(model.NestedStringMap(obj, "spec", "template", "spec", "nodeSelector"))
	return []model.Cell{
		nameCell(obj),
		namespaceCell(obj),
		ratio(ready, desired),
		{Text: strings.Join(nodeSelector, ", ")},
		model.AgeCell(created(obj)),
	}, daemonSetStatus(obj, desired, ready)
}

// daemonSetStatus is the row's token, after the deployment rule: pending while the controller has
// not seen the latest spec, nothing to colour when no node should run a pod, ok when every
// scheduled pod is ready, pending while a rolling update or the creation of pods is under way,
// and a warning when the daemon set is short of ready pods with nothing in progress to fix it. A
// daemon set has no progress condition, so pods that are still starting after the controller
// created them read as a warning until they are ready.
func daemonSetStatus(obj any, desired, ready int64) model.Status {
	generation, _ := model.NestedInt64(obj, "metadata", "generation")
	observed, _ := model.NestedInt64(obj, "status", "observedGeneration")
	switch {
	case observed < generation:
		return model.StatusPending
	case desired == 0:
		return model.StatusNone
	case ready == desired:
		return model.StatusOK
	case daemonSetRolling(obj, desired):
		return model.StatusPending
	}
	return model.StatusWarning
}

// daemonSetRolling reports whether the controller is still creating or replacing pods. With the
// OnDelete strategy it replaces nothing by itself, so outdated pods are not progress.
func daemonSetRolling(obj any, desired int64) bool {
	if nonNegative(obj, "status", "currentNumberScheduled") < desired {
		return true
	}
	if model.NestedString(obj, "spec", "updateStrategy", "type") == strategyOnDel {
		return false
	}
	return nonNegative(obj, "status", "updatedNumberScheduled") < desired
}

func daemonSetDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	pods := itoa(nonNegative(obj, "status", "desiredNumberScheduled")) + " desired / " +
		itoa(nonNegative(obj, "status", "currentNumberScheduled")) + " current / " +
		itoa(nonNegative(obj, "status", "numberReady")) + " ready / " +
		itoa(nonNegative(obj, "status", "updatedNumberScheduled")) + " updated / " +
		itoa(nonNegative(obj, "status", "numberAvailable")) + " available"
	if n := nonNegative(obj, "status", "numberMisscheduled"); n > 0 {
		pods += " / " + itoa(n) + " misscheduled"
	}
	d.add("Pods", pods)
	d.add("Selector", strings.Join(labelSelector(obj, "spec", "selector"), "\n"))
	d.add("Node Selector", strings.Join(model.SortedPairs(
		model.NestedStringMap(obj, "spec", "template", "spec", "nodeSelector")), "\n"))
	d.add("Update Strategy", updateStrategyText(model.NestedMap(obj, "spec", "updateStrategy")))
	d.add("Images", strings.Join(templateImages(obj), "\n"))
	sections := []model.DetailSection{{Title: "Daemon Set", Items: d}}
	if cs, ok := conditionSection(conditions(obj, "status", "conditions"), workloadConditionToken); ok {
		sections = append(sections, cs)
	}
	return sections
}

// updateStrategyText renders a daemon set's or stateful set's update strategy: "OnDelete",
// "RollingUpdate (max unavailable 1, max surge 0)", "RollingUpdate (partition 2)".
func updateStrategyText(strategy map[string]any) string {
	typ := model.NestedString(strategy, "type")
	if typ != "RollingUpdate" {
		return typ
	}
	var opts []string
	if s := model.NestedIntOrString(strategy, "rollingUpdate", "partition"); s != "" {
		opts = append(opts, "partition "+s)
	}
	if s := model.NestedIntOrString(strategy, "rollingUpdate", "maxUnavailable"); s != "" {
		opts = append(opts, "max unavailable "+s)
	}
	if s := model.NestedIntOrString(strategy, "rollingUpdate", "maxSurge"); s != "" {
		opts = append(opts, "max surge "+s)
	}
	if len(opts) == 0 {
		return typ
	}
	return typ + " (" + strings.Join(opts, ", ") + ")"
}

// templateImages returns the images of a workload's pod template (spec.template.spec).
func templateImages(obj any) []string {
	return podSpecImages(model.NestedMap(obj, "spec", "template", "spec"))
}

// workloadConditionToken colours the conditions of daemon sets, stateful sets and replica sets,
// which the API documents sparsely: a ReplicaFailure is a problem when True, any other condition
// is good when True.
func workloadConditionToken(c condition) model.Status {
	if c.Type == deployReplicaFailure {
		return conditionToken(c.Status, model.StatusError, model.StatusOK)
	}
	return conditionToken(c.Status, model.StatusOK, model.StatusWarning)
}
