package kinds

import (
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// secrets has the columns of Lens's documentation (not captured). The cache holds Secrets in full
// (D-13 revised, owner 2026-10-12; H-8): Keys lists the names of a Secret's keys, sorted, and Type
// its type, as in Lens; the profile never reads a value. A Secret has no state, so no status token
// (inferred). The drawer's Data section (each key with its value masked) is the engine's
// (secretdata.go), never this profile's.
var secrets = &model.KindProfile{
	Key:        keySecrets,
	Version:    "v1",
	Resource:   keySecrets,
	Kind:       "Secret",
	Title:      "Secrets",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		{ID: "labels", Title: "Labels", Type: model.ColumnText, Hidden: true, Pairs: model.PairsLabels}, // the drawer shows them (D-153)
		{ID: "keys", Title: "Keys", Type: model.ColumnText},
		typeColumn,
		ageColumn,
	},
	Cells:  secretCells,
	Detail: secretDetail,
}

// keySecrets is the kind key and resource of Secrets.
const keySecrets = "secrets"

func secretCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	return []model.Cell{
		nameCell(obj),
		namespaceCell(obj),
		{Text: strings.Join(model.SortedPairs(model.NestedStringMap(obj, "metadata", "labels")), ", ")},
		{Text: strings.Join(secretKeys(obj), ", ")},
		{Text: model.NestedString(obj, idType)},
		model.AgeCell(created(obj)),
	}, model.StatusNone
}

// secretKeys returns the keys of a Secret's data and stringData, sorted, each once; nil for a
// Secret without data. Only the keys: a profile never reads a Secret's values.
func secretKeys(obj any) []string {
	data, plain := model.NestedMap(obj, "data"), model.NestedMap(obj, "stringData")
	if len(data)+len(plain) == 0 {
		return nil
	}
	keys := make([]string, 0, len(data)+len(plain))
	for k := range data {
		keys = append(keys, k)
	}
	for k := range plain {
		if _, dup := data[k]; !dup {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// secretDetail adds the Type to the drawer's Properties; the engine adds the Data section.
func secretDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var props detailItems
	props.add(typeColumn.Title, model.NestedString(obj, idType))
	return withSection(nil, model.TitleProperties, props)
}
