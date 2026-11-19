package kinds

import (
	"slices"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Curated profiles of kinds from custom resource definitions (architecture §4.5): when a cluster's
// definition serves a version the profile reads, its view uses the curated profile in place of the
// generic one ParseCRD's Profile builds; the key stays "<plural>.<group>". They are not built-in
// kinds: they come and go with their definitions, and the engine's custom kinds watch picks them.

// curated lists the curated profiles, one per file.
var curated = []*model.KindProfile{verticalPodAutoscalers}

// Curated returns the curated profiles of custom resource kinds. Callers must not modify them.
func Curated() []*model.KindProfile { return curated }

// ProfileFor returns the profile of a custom resource kind: Cogpit's curated profile when it has one
// for the kind's key, its scope matches and it reads the served version (its Version or one of its
// FallbackVersions; at another version than its own, a copy at that version), else the generic
// profile built from the definition.
func ProfileFor(k CustomKind) *model.KindProfile {
	key := k.Key()
	for _, p := range curated {
		if p.Key != key || p.Namespaced != k.Namespaced || p.Kind != k.Kind {
			continue
		}
		if p.Version == k.Version {
			return p
		}
		if slices.Contains(p.FallbackVersions, k.Version) {
			at := *p
			at.Version = k.Version
			return &at
		}
	}
	return k.Profile()
}

// Vertical Pod Autoscaler condition types (autoscaling.k8s.io/v1, the autoscaler's recommender).
const (
	vpaRecommendationProvided = "RecommendationProvided"
	vpaLowConfidence          = "LowConfidence"
	vpaNoPodsMatched          = "NoPodsMatched"
	vpaFetchingHistory        = "FetchingHistory"
	vpaConfigDeprecated       = "ConfigDeprecated"
	vpaConfigUnsupported      = "ConfigUnsupported"
	// vpaDefaultMode is the update mode of an autoscaler that sets none, as the API documents it.
	vpaDefaultMode = "Auto"
	idMode         = "mode"
)

// verticalPodAutoscalers has the default columns of Lens's documentation (read, not captured):
// Name, Namespace, Mode, Age, Status, and the row menu's Edit and Delete (generic). Lens places the
// view in Config after Horizontal Pod Autoscalers. Inferred: Mode "Auto" when the autoscaler sets
// none, Status as the True condition types with the rule of vpaStatus, the hidden columns Target,
// CPU and Memory (the target recommendation summed over the containers), and the drawer.
var verticalPodAutoscalers = &model.KindProfile{
	Key:              "verticalpodautoscalers.autoscaling.k8s.io",
	Group:            "autoscaling.k8s.io",
	Version:          "v1",
	FallbackVersions: []string{"v1beta2"},
	Resource:         "verticalpodautoscalers",
	Kind:             "VerticalPodAutoscaler",
	Title:            "Vertical Pod Autoscalers",
	Namespaced:       true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		{ID: idMode, Title: "Mode", Type: model.ColumnText, Width: 140},
		ageColumn,
		statusColumn,
		{ID: idTarget, Title: titleTarget, Type: model.ColumnText, Hidden: true},
		{ID: "cpuTarget", Title: "CPU", Type: model.ColumnNumber, Hidden: true},
		{ID: "memoryTarget", Title: "Memory", Type: model.ColumnNumber, Hidden: true},
	},
	Cells: vpaCells,
	Fields: map[string]fieldFunc{
		idTarget: func(obj *unstructured.Unstructured) []string {
			text, _ := vpaTargetRef(obj)
			return nonEmpty(text)
		},
		idMode: func(obj *unstructured.Unstructured) []string { return []string{vpaMode(obj)} },
		idStatus: func(obj *unstructured.Unstructured) []string {
			text, _ := vpaStatus(conditions(obj, "status", "conditions"))
			return nonEmpty(text)
		},
	},
	Detail: vpaDetail,
}

func vpaCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	text, token := vpaStatus(conditions(obj, "status", "conditions"))
	target, link := vpaTargetRef(obj)
	cpu, memory := vpaTargetTotals(obj)
	cpuCell, memCell := model.Cell{Text: "-"}, model.Cell{Text: "-"}
	if cpu > 0 {
		cpuCell = model.Cell{Text: model.FormatCPU(cpu), Num: cpu}
	}
	if memory > 0 {
		memCell = model.Cell{Text: model.FormatBytes(memory), Num: memory}
	}
	return []model.Cell{
		nameCell(obj),
		namespaceCell(obj),
		{Text: vpaMode(obj)},
		model.AgeCell(created(obj)),
		{Text: text, Status: token},
		{Text: target, Link: link},
		cpuCell,
		memCell,
	}, token
}

// vpaMode is spec.updatePolicy.updateMode, "Auto" when the autoscaler sets none.
func vpaMode(obj any) string {
	if m := model.NestedString(obj, "spec", "updatePolicy", "updateMode"); m != "" {
		return m
	}
	return vpaDefaultMode
}

// vpaStatus renders the Status column, the condition types that are True, sorted, and colours it
// (inferred): a configuration the recommender does not support is an error; no pod matching the
// target, or a deprecated configuration, a warning; no recommendation yet, low confidence or a
// history still being fetched pending; a recommendation ok. Without conditions the recommender has
// not seen it: pending.
func vpaStatus(conds []condition) (string, model.Status) {
	trueTypes := make([]string, 0, len(conds))
	isTrue := map[string]bool{}
	for _, c := range conds {
		if c.Status == condTrue {
			trueTypes = append(trueTypes, c.Type)
			isTrue[c.Type] = true
		}
	}
	sort.Strings(trueTypes)
	text := strings.Join(trueTypes, " ")
	switch {
	case len(conds) == 0:
		return text, model.StatusPending
	case isTrue[vpaConfigUnsupported]:
		return text, model.StatusError
	case isTrue[vpaNoPodsMatched], isTrue[vpaConfigDeprecated]:
		return text, model.StatusWarning
	case !isTrue[vpaRecommendationProvided], isTrue[vpaLowConfidence], isTrue[vpaFetchingHistory]:
		return text, model.StatusPending
	}
	return text, model.StatusOK
}

// vpaConditionToken colours one condition in the drawer, by the rule of vpaStatus.
func vpaConditionToken(c condition) model.Status {
	switch c.Type {
	case vpaRecommendationProvided:
		return conditionToken(c.Status, model.StatusOK, model.StatusPending)
	case vpaConfigUnsupported:
		return conditionToken(c.Status, model.StatusError, model.StatusNone)
	case vpaNoPodsMatched, vpaConfigDeprecated:
		return conditionToken(c.Status, model.StatusWarning, model.StatusNone)
	case vpaLowConfidence, vpaFetchingHistory:
		return conditionToken(c.Status, model.StatusPending, model.StatusNone)
	}
	return model.StatusNone
}

// vpaTargetRef returns spec.targetRef as "Kind/name" and a link to it for built-in kinds.
func vpaTargetRef(obj *unstructured.Unstructured) (string, *model.ObjectRef) {
	ref := model.NestedMap(obj, "spec", "targetRef")
	kind, name := model.NestedString(ref, idKind), model.NestedString(ref, fieldName)
	if name == "" {
		return "", nil
	}
	text := name
	if kind != "" {
		text = kind + "/" + name
	}
	return text, model.RefTo(model.NestedString(ref, "apiVersion"), kind, obj.GetNamespace(), name)
}

// vpaTargetTotals sums the target recommendation over the containers: CPU in millicores, memory
// in bytes; zero when there is none.
func vpaTargetTotals(obj any) (cpu, memory int64) {
	for _, c := range model.NestedSlice(obj, "status", "recommendation", "containerRecommendations") {
		u := model.UsageOf(c, "target")
		cpu += max(u.CPUMilli, 0)
		memory += max(u.MemoryBytes, 0)
	}
	return cpu, memory
}

func vpaDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	target, link := vpaTargetRef(obj)
	d.put(linkedItem("Target", target, link))
	d.add("Mode", vpaMode(obj))
	if n, ok := model.NestedInt64(obj, "spec", "updatePolicy", "minReplicas"); ok {
		d.add("Min Replicas", itoa(n))
	}
	var recommenders []string
	for _, r := range model.NestedSlice(obj, "spec", "recommenders") {
		recommenders = appendUnique(recommenders, model.NestedString(r, fieldName))
	}
	d.add("Recommenders", strings.Join(recommenders, ", "))
	sections := withSection(nil, model.TitleProperties, d)

	if policies := model.NestedSlice(obj, "spec", "resourcePolicy", "containerPolicies"); len(policies) > 0 {
		t := &model.DetailTable{Columns: []string{"Container", "Mode", "Min Allowed", "Max Allowed", "Controlled Resources", "Controlled Values"}}
		for _, p := range policies {
			t.Rows = append(t.Rows, []string{
				model.NestedString(p, "containerName"), model.NestedString(p, idMode),
				resourceListText(model.NestedMap(p, "minAllowed")), resourceListText(model.NestedMap(p, "maxAllowed")),
				strings.Join(model.NestedStrings(p, "controlledResources"), ", "), model.NestedString(p, "controlledValues"),
			})
		}
		sections = append(sections, model.DetailSection{Title: "Resource Policy", Table: t})
	}
	if recs := model.NestedSlice(obj, "status", "recommendation", "containerRecommendations"); len(recs) > 0 {
		t := &model.DetailTable{Columns: []string{"Container", titleTarget, "Lower Bound", "Upper Bound", "Uncapped Target"}}
		for _, r := range recs {
			t.Rows = append(t.Rows, []string{
				model.NestedString(r, "containerName"),
				resourceListText(model.NestedMap(r, "target")), resourceListText(model.NestedMap(r, "lowerBound")),
				resourceListText(model.NestedMap(r, "upperBound")), resourceListText(model.NestedMap(r, "uncappedTarget")),
			})
		}
		sections = append(sections, model.DetailSection{Title: "Recommendations", Table: t})
	}
	if cs, ok := conditionSection(conditions(obj, "status", "conditions"), vpaConditionToken); ok {
		sections = append(sections, cs)
	}
	return sections
}

// resourceListText renders a resource list as "name: quantity" lines, sorted by name, bytes in
// binary units (D-32).
func resourceListText(list map[string]any) string {
	lines := make([]string, 0, len(list))
	for _, k := range sortedKeys(list) {
		lines = append(lines, k+": "+model.FormatResource(k, model.NestedIntOrString(list, k)))
	}
	return strings.Join(lines, "\n")
}
