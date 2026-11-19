package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// resourceClaimTemplates has the columns of Lens's documentation (read, not captured): Name,
// Namespace, Device Classes (the classes the generated claims request devices from), Age; its
// drawer shows "the device requests each generated claim carries". Inferred: no status token (a
// template has no state) and the drawer's form (the generated claims' labels and annotations, then
// the requests as a table, as a claim's drawer shows them).
var resourceClaimTemplates = &model.KindProfile{
	Key:        keyResourceClaimTemplates,
	Group:      groupResource,
	Version:    "v1",
	Resource:   keyResourceClaimTemplates,
	Kind:       "ResourceClaimTemplate",
	Title:      "Resource Claim Templates",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		{ID: idDeviceClasses, Title: titleDeviceClasses, Type: model.ColumnText, Width: 260},
		ageColumn,
	},
	Cells: func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
		return []model.Cell{
			nameCell(obj),
			namespaceCell(obj),
			{Text: strings.Join(draClasses(templateRequests(obj)), ", ")},
			model.AgeCell(created(obj)),
		}, model.StatusNone
	},
	Fields: map[string]fieldFunc{
		"deviceClass": func(obj *unstructured.Unstructured) []string {
			return draClasses(templateRequests(obj))
		},
		"request": func(obj *unstructured.Unstructured) []string {
			var out []string
			for _, r := range templateRequests(obj) {
				out = appendUnique(out, r.name)
			}
			return out
		},
	},
	Detail: resourceClaimTemplateDetail,
}

// templateRequests reads the requests of the claims the template generates (spec.spec).
func templateRequests(obj any) []draRequest {
	return draRequests(model.NestedMap(obj, "spec", "spec"))
}

func resourceClaimTemplateDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	d.put(model.ChipsItem("Claim Labels", textChips(model.SortedPairs(model.NestedStringMap(obj, "spec", "metadata", "labels")))))
	d.put(model.ChipsItem("Claim Annotations", textChips(model.SortedPairs(model.NestedStringMap(obj, "spec", "metadata", "annotations")))))
	sections := withSection(nil, model.TitleProperties, d)
	if t := draRequestsTable(templateRequests(obj)); t != nil {
		sections = append(sections, model.DetailSection{Title: itemRequests, Table: t})
	}
	return sections
}

// textChips makes a chip of each text: labels and annotations as the drawer shows them.
func textChips(texts []string) []model.Chip {
	if len(texts) == 0 {
		return nil
	}
	out := make([]model.Chip, len(texts))
	for i, t := range texts {
		out[i] = model.Chip{Text: t}
	}
	return out
}
