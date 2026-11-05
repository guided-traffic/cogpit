package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

var namespaces = &model.KindProfile{
	Key:      "namespaces",
	Version:  "v1",
	Resource: "namespaces",
	Kind:     "Namespace",
	Title:    "Namespaces",
	Columns: []model.Column{
		nameColumn,
		{ID: "labels", Title: "Labels", Type: model.ColumnText, Hidden: true, Pairs: model.PairsLabels}, // the drawer shows them (D-153)
		ageColumn,
		statusColumn,
	},
	Cells: namespaceCells,
	Fields: map[string]fieldFunc{
		idStatus: func(obj *unstructured.Unstructured) []string {
			text, _ := namespaceStatus(obj)
			return []string{text}
		},
	},
	Detail: namespaceDetail,
}

func namespaceCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	text, token := namespaceStatus(obj)
	return []model.Cell{
		nameCell(obj),
		{Text: strings.Join(model.SortedPairs(model.NestedStringMap(obj, "metadata", "labels")), ", ")},
		model.AgeCell(created(obj)),
		{Text: text, Status: token},
	}, token
}

// namespaceStatus reads status.phase. The API server sets the phase on every namespace, so a
// missing one is shown as Unknown rather than guessed.
func namespaceStatus(obj any) (string, model.Status) {
	phase := model.NestedString(obj, "status", "phase")
	if deleting(obj) {
		phase = "Terminating"
	}
	switch phase {
	case "Active":
		return phase, model.StatusOK
	case "Terminating":
		return phase, model.StatusTerminating
	case "":
		return textUnknown, model.StatusUnknown
	}
	return phase, model.StatusUnknown
}

func namespaceDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	text, token := namespaceStatus(obj)
	d.addStatus("Phase", text, token)
	d.add("Finalizers", strings.Join(model.NestedStrings(obj, "spec", "finalizers"), ", "))
	// The namespace controller reports why a deletion is stuck with conditions whose True status
	// is the problem (content remaining, discovery failed).
	for _, c := range conditions(obj, "status", "conditions") {
		d.addStatus(c.Type, c.text(), conditionToken(c.Status, model.StatusWarning, model.StatusOK))
	}
	return []model.DetailSection{{Title: titleStatus, Items: d}}
}
