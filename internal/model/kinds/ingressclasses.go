package kinds

import (
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

const (
	// ingressClassDefault marks the class that Ingresses without a class get. Kubernetes counts
	// only the exact value "true".
	ingressClassDefault = "ingressclass.kubernetes.io/is-default-class"
	// scopeCluster is what the API server defaults spec.parameters.scope to.
	scopeCluster = "Cluster"
	idDefault    = "default"
	// keyIngressClasses is the kind key and resource of Ingress Classes.
	keyIngressClasses = "ingressclasses"
)

// ingressClasses has the columns captured from Lens on 2026-10-11: Name, a star column, an empty
// Namespace column (the kind is cluster-scoped, but Lens shows the column), Controller and the
// parameters' API Group, Scope and Kind; no Age. That the star marks the default class is
// inferred. Fyne's bundled fonts have no star glyph (U+2605 is in none of them; U+2B50 only in the
// colour emoji font, which ignores theme colours), so the column is titled Default and reads
// "Yes". Ingress classes have no state, so no status token.
var ingressClasses = &model.KindProfile{
	Key:      keyIngressClasses,
	Group:    groupNetworking,
	Version:  "v1",
	Resource: keyIngressClasses,
	Kind:     "IngressClass",
	Title:    "Ingress Classes",
	Columns: []model.Column{
		nameColumn,
		{ID: idDefault, Title: "Default", Type: model.ColumnText, Width: 70},
		namespaceColumn,
		{ID: "controller", Title: "Controller", Type: model.ColumnText},
		{ID: "apiGroup", Title: "API Group", Type: model.ColumnText},
		{ID: "scope", Title: "Scope", Type: model.ColumnText},
		{ID: idKind, Title: titleKind, Type: model.ColumnText},
	},
	Cells: ingressClassCells,
	Fields: map[string]fieldFunc{
		"controller": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", "controller"))
		},
		idDefault: func(obj *unstructured.Unstructured) []string {
			return []string{strconv.FormatBool(isDefaultIngressClass(obj))}
		},
		// "parameters" is the parameters object as "Kind/name", the way an agent names it.
		"parameters": func(obj *unstructured.Unstructured) []string {
			kind := model.NestedString(obj, "spec", "parameters", "kind")
			name := model.NestedString(obj, "spec", "parameters", "name")
			if kind == "" || name == "" {
				return nil
			}
			return []string{kind + "/" + name}
		},
	},
	Detail: ingressClassDetail,
}

func ingressClassCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	var def model.Cell
	if isDefaultIngressClass(obj) {
		def.Text = "Yes"
	}
	return []model.Cell{
		nameCell(obj),
		def,
		{}, // Namespace: empty, as in Lens; the kind is cluster-scoped
		{Text: model.NestedString(obj, "spec", "controller")},
		{Text: model.NestedString(obj, "spec", "parameters", "apiGroup")},
		{Text: ingressClassScope(obj)},
		{Text: model.NestedString(obj, "spec", "parameters", "kind")},
	}, model.StatusNone
}

func isDefaultIngressClass(obj any) bool {
	return model.NestedString(obj, "metadata", "annotations", ingressClassDefault) == textTrue
}

// ingressClassScope is spec.parameters.scope; the API server defaults it to Cluster when the class
// has parameters, so an object built without the default reads the same.
func ingressClassScope(obj any) string {
	params := model.NestedMap(obj, "spec", "parameters")
	if params == nil {
		return ""
	}
	if s := model.NestedString(params, "scope"); s != "" {
		return s
	}
	return scopeCluster
}

func ingressClassDetail(obj *unstructured.Unstructured) []model.DetailSection {
	var d detailItems
	d.add("Controller", model.NestedString(obj, "spec", "controller"))
	if isDefaultIngressClass(obj) {
		d.add("Default", "Yes: Ingresses without a class get this one")
	}
	sections := withSection(nil, "Ingress Class", d)

	var p detailItems
	params := model.NestedMap(obj, "spec", "parameters")
	p.add("API Group", model.NestedString(params, "apiGroup"))
	p.add("Kind", model.NestedString(params, "kind"))
	p.add("Name", model.NestedString(params, "name"))
	p.add("Namespace", model.NestedString(params, "namespace"))
	if len(p) > 0 {
		p.add("Scope", ingressClassScope(obj))
	}
	return withSection(sections, "Parameters", p)
}
