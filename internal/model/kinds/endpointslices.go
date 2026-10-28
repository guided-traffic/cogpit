package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

const (
	groupDiscovery = "discovery.k8s.io"
	// serviceNameLabel names the Service an endpoint slice belongs to.
	serviceNameLabel = "kubernetes.io/service-name"
	// sliceManagedByLabel names the controller that manages an endpoint slice.
	sliceManagedByLabel = "endpointslice.kubernetes.io/managed-by"
	keyEndpoints        = "endpoints"
	keyEndpointSlices   = "endpointslices"
	fieldAddressType    = "addressType"
	titleAddressType    = "Address Type"
	fieldEndpoints      = "endpoints"
	fieldHostname       = "hostname"
	fieldReady          = "ready"
	titlePorts          = "Ports"
	idPorts             = "ports"
	titleNode           = "Node"
	titleHostname       = "Hostname"
	idAddress           = "address"
	idPort              = "port"
	idNode              = "node"
	idTarget            = "target"
	titleEndpoints      = "Endpoints"
	titleService        = "Service"
	// endpointsShown is how many addresses an Endpoints cell lists before "+ N more", as kubectl
	// prints them.
	endpointsShown = 3
	// maxEndpointRows caps the rows of a drawer's endpoints table; an endpoint slice holds up to
	// 1,000 endpoints.
	maxEndpointRows = 100
)

// endpointSlices has the columns of Lens's documentation (not captured): Name, Namespace, Address
// Type, Endpoints, Ports, Age. The cell formats are inferred: Endpoints lists the first three
// addresses and "+ N more", as kubectl does; Ports reads "8080/TCP" as the Services view does.
// Endpoint slices have no state of their own, so no status token; the drawer colours each
// endpoint by its conditions.
var endpointSlices = &model.KindProfile{
	Key:        keyEndpointSlices,
	Group:      groupDiscovery,
	Version:    "v1",
	Resource:   keyEndpointSlices,
	Kind:       "EndpointSlice",
	Title:      "Endpoint Slices",
	Namespaced: true,
	Columns: []model.Column{
		nameColumn,
		namespaceColumn,
		{ID: fieldAddressType, Title: titleAddressType, Type: model.ColumnText},
		{ID: keyEndpoints, Title: titleEndpoints, Type: model.ColumnText},
		{ID: idPorts, Title: titlePorts, Type: model.ColumnText},
		ageColumn,
	},
	Cells: endpointSliceCells,
	Fields: map[string]fieldFunc{
		fieldService: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "metadata", "labels", serviceNameLabel))
		},
		fieldAddressType: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, fieldAddressType))
		},
		idAddress: func(obj *unstructured.Unstructured) []string {
			return sliceAddresses(obj)
		},
		idPort: func(obj *unstructured.Unstructured) []string {
			var out []string
			for _, p := range model.NestedSlice(obj, "ports") {
				out = appendUnique(out, model.NestedIntOrString(p, "port"))
			}
			return out
		},
		idNode: func(obj *unstructured.Unstructured) []string {
			var out []string
			for _, e := range model.NestedSlice(obj, fieldEndpoints) {
				out = appendUnique(out, model.NestedString(e, "nodeName"))
			}
			return out
		},
		// "target" is each endpoint's target as "Kind/name": "Pod/web-0".
		idTarget: func(obj *unstructured.Unstructured) []string {
			var out []string
			for _, e := range model.NestedSlice(obj, fieldEndpoints) {
				out = appendUnique(out, targetText(model.NestedMap(e, "targetRef")))
			}
			return out
		},
	},
	Detail: endpointSliceDetail,
}

func endpointSliceCells(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
	return []model.Cell{
		nameCell(obj),
		namespaceCell(obj),
		{Text: model.NestedString(obj, fieldAddressType)},
		{Text: shortList(sliceAddresses(obj))},
		{Text: orDash(slicePorts(model.NestedSlice(obj, "ports")))},
		model.AgeCell(created(obj)),
	}, model.StatusNone
}

// sliceAddresses returns every endpoint's addresses, ready or not, in order.
func sliceAddresses(obj any) []string {
	var out []string
	for _, e := range model.NestedSlice(obj, fieldEndpoints) {
		out = appendUnique(out, model.NestedStrings(e, "addresses")...)
	}
	return out
}

// shortList renders a list the way kubectl prints endpoints: the first three, then "+ N more";
// "-" for none.
func shortList(list []string) string {
	if len(list) <= endpointsShown {
		return orDash(list)
	}
	return strings.Join(list[:endpointsShown], ", ") + " + " + itoa(int64(len(list)-endpointsShown)) + " more"
}

// slicePorts renders ports as "8080/TCP". A port without a number stands for every port and
// reads "*/TCP".
func slicePorts(ports []any) []string {
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		out = append(out, slicePort(p)+"/"+protocol(p))
	}
	return out
}

func slicePort(p any) string {
	if port := model.NestedIntOrString(p, "port"); port != "" {
		return port
	}
	return "*"
}

// slicePortLines renders ports for the drawer: "http: 8080/TCP (app protocol http)", "*/TCP
// (every port)".
func slicePortLines(ports []any) []string {
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		port := slicePort(p)
		line := port + "/" + protocol(p)
		if name := model.NestedString(p, "name"); name != "" {
			line = name + ": " + line
		}
		var notes []string
		if port == "*" {
			notes = append(notes, "every port")
		}
		if ap := model.NestedString(p, "appProtocol"); ap != "" {
			notes = append(notes, "app protocol "+ap)
		}
		if len(notes) > 0 {
			line += " (" + strings.Join(notes, ", ") + ")"
		}
		out = append(out, line)
	}
	return out
}

// targetText renders a target reference as "Kind/name".
func targetText(ref map[string]any) string {
	kind, name := model.NestedString(ref, "kind"), model.NestedString(ref, "name")
	switch {
	case name == "":
		return ""
	case kind == "":
		return name
	}
	return kind + "/" + name
}

// targetCell is a target reference as a cell: "Pod web-0", a link to the pod.
func targetCell(ref map[string]any, namespace string) model.Cell {
	kind, name := model.NestedString(ref, "kind"), model.NestedString(ref, "name")
	if name == "" {
		return model.Cell{}
	}
	if ns := model.NestedString(ref, "namespace"); ns != "" {
		namespace = ns
	}
	text := name
	if kind != "" {
		text = kind + " " + name
	}
	return model.Cell{Text: text, Link: model.RefTo(model.NestedString(ref, "apiVersion"), kind, namespace, name)}
}

// endpointConditions renders an endpoint's conditions and colours them (inferred): "Ready" ok,
// "Serving, Terminating" while it drains, "Not ready" a warning. A condition the API leaves unset
// reads as consumers are told to read it: ready and serving unless said otherwise, not
// terminating.
func endpointConditions(e any) model.Cell {
	isSet := func(field string, unset bool) bool {
		if b, ok := model.NestedBool(e, "conditions", field); ok {
			return b
		}
		return unset
	}
	ready, serving, terminating := isSet(fieldReady, true), isSet("serving", true), isSet("terminating", false)
	switch {
	case terminating && serving:
		return model.Cell{Text: "Serving, Terminating", Status: model.StatusTerminating}
	case terminating:
		return model.Cell{Text: podTerminating, Status: model.StatusTerminating}
	case ready:
		return model.Cell{Text: nodeReady, Status: model.StatusOK}
	case serving:
		return model.Cell{Text: "Not ready, Serving", Status: model.StatusWarning}
	}
	return model.Cell{Text: "Not ready", Status: model.StatusWarning}
}

// endpointColumns are the columns of the drawer's endpoints table (inferred).
var endpointColumns = []model.Column{
	{ID: "addresses", Title: "Addresses", Type: model.ColumnText},
	{ID: "conditions", Title: itemConditions, Type: model.ColumnText},
	{ID: fieldHostname, Title: titleHostname, Type: model.ColumnText},
	{ID: idNode, Title: titleNode, Type: model.ColumnText},
	{ID: "zone", Title: "Zone", Type: model.ColumnText},
	{ID: idTarget, Title: titleTarget, Type: model.ColumnText},
}

// endpointSliceDetail is Cogpit's drawer for a slice (inferred, Lens's is not captured): the
// Service it belongs to, the controller that manages it and its ports in Properties, then its
// endpoints as a table with links to their nodes and targets.
func endpointSliceDetail(obj *unstructured.Unstructured) []model.DetailSection {
	ns := obj.GetNamespace()
	var d detailItems
	d.add(titleAddressType, model.NestedString(obj, fieldAddressType))
	svc := model.NestedString(obj, "metadata", "labels", serviceNameLabel)
	d.put(linkedItem(titleService, svc, linkTo(keyServices, ns, svc)))
	d.add("Managed By", model.NestedString(obj, "metadata", "labels", sliceManagedByLabel))
	d.add(titlePorts, strings.Join(slicePortLines(model.NestedSlice(obj, "ports")), "\n"))
	sections := withSection(nil, model.TitleProperties, d)

	endpoints := model.NestedSlice(obj, fieldEndpoints)
	rows := make([][]model.Cell, 0, min(len(endpoints), maxEndpointRows+1))
	for i, e := range endpoints {
		if i == maxEndpointRows {
			rows = append(rows, []model.Cell{{Text: "… " + itoa(int64(len(endpoints)-i)) + " more"}})
			break
		}
		node := model.NestedString(e, "nodeName")
		rows = append(rows, []model.Cell{
			{Text: strings.Join(model.NestedStrings(e, "addresses"), ", ")},
			endpointConditions(e),
			{Text: model.NestedString(e, fieldHostname)},
			{Text: node, Link: linkTo(keyNodes, "", node)},
			{Text: model.NestedString(e, "zone")},
			targetCell(model.NestedMap(e, "targetRef"), ns),
		})
	}
	if len(rows) > 0 {
		sections = append(sections, model.DetailSection{Title: titleEndpoints, Table: model.CellTable(endpointColumns, rows)})
	}
	return sections
}

// serviceSliceColumns are the columns of a Service's Endpoint Slices section: the slices list's,
// without what the Service already says (namespace) or the drawer does not need (age).
var serviceSliceColumns = []model.Column{
	nameColumn,
	{ID: fieldAddressType, Title: titleAddressType, Type: model.ColumnText},
	{ID: keyEndpoints, Title: titleEndpoints, Type: model.ColumnText},
	{ID: idPorts, Title: titlePorts, Type: model.ColumnText},
}

// serviceDetailWith is the Service's drawer with its endpoint slices, from the related-objects
// index EndpointSlicesByService: a table after Connection, each slice a link with its address
// type, endpoints and ports as the Endpoint Slices list shows them. Lens shows a Service's
// endpoint slices in a detail tab (documented, not captured); the section is Cogpit's form of it
// (inferred). A Service without slices, or whose slices are still listing, has no section.
func serviceDetailWith(obj *unstructured.Unstructured, rel model.Related) []model.DetailSection {
	sections := serviceDetailRel(obj, rel)
	slices := append([]*unstructured.Unstructured(nil), rel.Objects(EndpointSlicesByService, obj.GetNamespace()+"/"+obj.GetName())...)
	if len(slices) == 0 {
		return sections
	}
	sortByName(slices)
	rows := make([][]model.Cell, 0, len(slices))
	for _, s := range slices {
		rows = append(rows, []model.Cell{
			{Text: s.GetName(), Link: linkTo(keyEndpointSlices, s.GetNamespace(), s.GetName())},
			{Text: model.NestedString(s, fieldAddressType)},
			{Text: shortList(sliceAddresses(s))},
			{Text: orDash(slicePorts(model.NestedSlice(s, idPorts)))},
		})
	}
	return append(sections, model.DetailSection{Title: "Endpoint Slices", Table: model.CellTable(serviceSliceColumns, rows)})
}
