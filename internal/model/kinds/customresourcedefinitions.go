package kinds

import (
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// KeyCustomResourceDefinitions is the kind key of the Definitions view; the engine watches this
// kind to learn the cluster's custom resource kinds.
const KeyCustomResourceDefinitions = "customresourcedefinitions"

// CRD condition types.
const (
	crdEstablished   = "Established"
	crdNamesAccepted = "NamesAccepted"
	crdTerminating   = "Terminating"
)

// Ids of the Definitions view: columns, searchable fields and the spec fields they show.
const (
	idVersion = "version"
	idScope   = "scope"
)

// hiddenNameColumn is the name column of a view that does not show it but sorts by it.
var hiddenNameColumn = func() model.Column {
	c := nameColumn
	c.Hidden = true
	return c
}()

// customResourceDefinitions is Lens's Definitions view (Custom Resources › Definitions, captured):
// Resource (the Kind, a link to that kind's view), Group, Version, Scope, Age. The hidden Name
// column (the definition's name, "<plural>.<group>") is the default sort, so the list is in the
// order of the definitions' names, as captured (that the order is by name is inferred).
var customResourceDefinitions = &model.KindProfile{
	Key:      KeyCustomResourceDefinitions,
	Group:    "apiextensions.k8s.io",
	Version:  "v1",
	Resource: "customresourcedefinitions",
	Kind:     "CustomResourceDefinition",
	Title:    "Definitions",
	Columns: []model.Column{
		hiddenNameColumn,
		{ID: idResource, Title: "Resource", Type: model.ColumnText},
		{ID: "group", Title: "Group", Type: model.ColumnText},
		{ID: idVersion, Title: "Version", Type: model.ColumnText},
		{ID: idScope, Title: "Scope", Type: model.ColumnText},
		ageColumn,
	},
	Cells: crdCells,
	Fields: map[string]fieldFunc{
		"group": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", "group"))
		},
		idKind: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", "names", "kind"))
		},
		idScope: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", idScope))
		},
		idVersion: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(ServedVersionName(obj))
		},
	},
	Detail: crdDetail,
}

func crdCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	resource := model.Cell{Text: model.NestedString(obj, "spec", "names", "kind")}
	// The Resource cell opens the kind's list view (a link without a name), once the API server
	// serves the kind.
	if k, err := ParseCRD(obj); err == nil {
		resource.Link = &model.ObjectRef{Kind: k.Key()}
	}
	return []model.Cell{
		nameCell(obj),
		resource,
		{Text: model.NestedString(obj, "spec", "group")},
		{Text: ServedVersionName(obj)},
		{Text: model.NestedString(obj, "spec", idScope)},
		model.AgeCell(created(obj)),
	}, crdStatus(obj)
}

// crdStatus is a definition's status token (inferred; Lens's Definitions view has no status
// column): established is ok, names in conflict an error, anything else pending.
func crdStatus(obj any) model.Status {
	switch {
	case conditionStatus(obj, crdNamesAccepted, "status", "conditions") == condFalse:
		return model.StatusError
	case conditionStatus(obj, crdEstablished, "status", "conditions") == condTrue:
		return model.StatusOK
	}
	return model.StatusPending
}

// crdDetail is the drawer of a definition (inferred: no capture of Lens's definition drawer):
// the names, the versions with the one Cogpit shows, the printer columns of that version, then
// the conditions.
func crdDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	d.add("Group", model.NestedString(obj, "spec", "group"))
	d.add("Kind", model.NestedString(obj, "spec", "names", "kind"))
	d.add("Plural", model.NestedString(obj, "spec", "names", "plural"))
	d.add("Singular", model.NestedString(obj, "spec", "names", "singular"))
	d.add("Short Names", strings.Join(model.NestedStrings(obj, "spec", "names", "shortNames"), ", "))
	d.add("Categories", strings.Join(model.NestedStrings(obj, "spec", "names", "categories"), ", "))
	d.add("Scope", model.NestedString(obj, "spec", idScope))
	d.add("Versions", strings.Join(crdVersions(obj), ", "))
	d.add("Stored Versions", strings.Join(model.NestedStrings(obj, "status", "storedVersions"), ", "))
	d.add("Conversion", model.NestedString(obj, "spec", "conversion", "strategy"))
	sections := []model.DetailSection{{Title: "Custom Resource Definition", Items: d}}

	if cols := crdPrinterColumns(obj); len(cols) > 0 {
		sections = append(sections, model.DetailSection{Title: "Printer Columns", Items: cols})
	}
	if s, ok := conditionSection(conditions(obj, "status", "conditions"), func(c condition) model.Status {
		if c.Type == crdTerminating {
			return conditionToken(c.Status, model.StatusTerminating, model.StatusOK)
		}
		return conditionToken(c.Status, model.StatusOK, model.StatusError)
	}); ok {
		sections = append(sections, s)
	}
	return sections
}

// crdVersions lists the versions: "v1 (served, storage)", "v1beta1 (not served)".
func crdVersions(obj any) []string {
	var out []string
	for _, v := range model.NestedSlice(obj, "spec", "versions") {
		name := model.NestedString(v, fieldName)
		if name == "" {
			continue
		}
		var flags []string
		if served, _ := model.NestedBool(v, "served"); served {
			flags = append(flags, "served")
		} else {
			flags = append(flags, "not served")
		}
		if storage, _ := model.NestedBool(v, "storage"); storage {
			flags = append(flags, "storage")
		}
		if deprecated, _ := model.NestedBool(v, "deprecated"); deprecated {
			flags = append(flags, "deprecated")
		}
		out = append(out, name+" ("+strings.Join(flags, ", ")+")")
	}
	return out
}

// crdPrinterColumns lists the printer columns of the version Cogpit shows: "State: .status.state
// (string)", with the priority when it is not 0.
func crdPrinterColumns(obj any) []model.DetailItem {
	v := servedVersion(model.NestedMap(obj, "spec"))
	var items []model.DetailItem
	for _, c := range model.NestedSlice(v, "additionalPrinterColumns") {
		name := model.NestedString(c, fieldName)
		path := model.NestedString(c, "jsonPath")
		if name == "" || path == "" {
			continue
		}
		value := path
		if t := model.NestedString(c, "type"); t != "" {
			value += " (" + t
			if p, _ := model.NestedInt64(c, "priority"); p != 0 {
				value += ", priority " + strconv.FormatInt(p, 10)
			}
			value += ")"
		}
		items = append(items, model.DetailItem{Name: name, Value: value})
		if len(items) == maxPrinterColumns {
			break
		}
	}
	return items
}
