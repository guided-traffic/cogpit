package kinds

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// ClaimsByPool indexes resource claims by the pools their allocated devices come from, as
// "<driver>/<pool>": the claims holding devices a resource slice publishes.
var ClaimsByPool = model.RelatedIndex{Kind: keyResourceClaims, Name: "byPool", Keys: claimPoolKeys}

func claimPoolKeys(claim *unstructured.Unstructured) []string {
	var keys []string
	for _, r := range claimResults(claim) {
		keys = appendUnique(keys, poolKey(r.driver, r.pool))
	}
	return keys
}

func poolKey(driver, pool string) string { return driver + "/" + pool }

// Texts of the Node column for a slice that is not one node's (inferred).
const (
	sliceAllNodes  = "All nodes"
	sliceSelector  = "Node selector"
	slicePerDevice = "Per device"
	// maxSliceDevices bounds the devices the drawer lists (the API allows 128 per slice).
	maxSliceDevices = 128
)

// resourceSlices has the columns of Lens's documentation (read, not captured): Name, Driver, Pool,
// Node, Devices (how many the slice publishes), Allocated (how many of those are allocated to a
// claim), Age; its drawer shows Driver, Pool and Node, then each device with its attributes,
// capacity and taints. Allocated is read through the ClaimsByPool index over the cached claims.
// Inferred: the Node column of a slice that is not one node's (All nodes, Node selector, Per
// device), Allocated as "-" while the claims are not listed, no status token, the drawer's form.
var resourceSlices = &model.KindProfile{
	Key:      keyResourceSlices,
	Group:    groupResource,
	Version:  "v1",
	Resource: keyResourceSlices,
	Kind:     "ResourceSlice",
	Title:    "Resource Slices",
	Columns: []model.Column{
		nameColumn,
		{ID: fieldDriver, Title: titleDriver, Type: model.ColumnText, Width: 200},
		{ID: "pool", Title: titlePool, Type: model.ColumnText, Width: 160},
		{ID: idNode, Title: titleNode, Type: model.ColumnText, Width: 160},
		{ID: "devices", Title: titleDevices, Type: model.ColumnNumber},
		{ID: "allocated", Title: draClaimAllocated, Type: model.ColumnNumber, Related: keyResourceClaims},
		ageColumn,
	},
	Needs:     []model.RelatedIndex{ClaimsByPool},
	CellsWith: resourceSliceCells,
	Cells: func(obj *unstructured.Unstructured) ([]model.Cell, model.Status) {
		return resourceSliceCells(obj, model.NoRelated)
	},
	Fields: map[string]fieldFunc{
		fieldDriver: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", fieldDriver))
		},
		"pool": func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", "pool", fieldName))
		},
		idNode: func(obj *unstructured.Unstructured) []string {
			return nonEmpty(model.NestedString(obj, "spec", "nodeName"))
		},
		"device": func(obj *unstructured.Unstructured) []string {
			return sliceDeviceNames(obj)
		},
	},
	DetailWith: resourceSliceDetail,
	Detail: func(obj *unstructured.Unstructured) []model.DetailSection {
		return resourceSliceDetail(obj, model.NoRelated)
	},
}

func resourceSliceCells(obj *unstructured.Unstructured, rel model.Related) ([]model.Cell, model.Status) {
	node, link := sliceNode(obj)
	allocated := model.Cell{Text: "-"}
	if rel.Listed(ClaimsByPool) {
		allocated = model.Cell{Num: int64(len(sliceAllocations(obj, rel)))}
	}
	return []model.Cell{
		nameCell(obj),
		{Text: model.NestedString(obj, "spec", fieldDriver)},
		{Text: model.NestedString(obj, "spec", "pool", fieldName)},
		{Text: node, Link: link},
		{Num: int64(len(model.NestedSlice(obj, "spec", "devices")))},
		allocated,
		model.AgeCell(created(obj)),
	}, model.StatusNone
}

// sliceNode is the Node column: the node whose devices the slice publishes (a link to it), else
// what the slice says about the nodes that reach its devices.
func sliceNode(obj any) (string, *model.ObjectRef) {
	if n := model.NestedString(obj, "spec", "nodeName"); n != "" {
		return n, linkTo(keyNodes, "", n)
	}
	if all, _ := model.NestedBool(obj, "spec", "allNodes"); all {
		return sliceAllNodes, nil
	}
	if model.NestedMap(obj, "spec", "nodeSelector") != nil {
		return sliceSelector, nil
	}
	if per, _ := model.NestedBool(obj, "spec", "perDeviceNodeSelection"); per {
		return slicePerDevice, nil
	}
	return "", nil
}

func sliceDeviceNames(obj any) []string {
	var out []string
	for _, d := range model.NestedSlice(obj, "spec", "devices") {
		out = appendUnique(out, model.NestedString(d, fieldName))
	}
	return out
}

// sliceAllocations maps each of the slice's devices that a cached claim holds to that claim.
func sliceAllocations(obj *unstructured.Unstructured, rel model.Related) map[string]*model.ObjectRef {
	driver := model.NestedString(obj, "spec", fieldDriver)
	pool := model.NestedString(obj, "spec", "pool", fieldName)
	out := map[string]*model.ObjectRef{}
	if driver == "" || pool == "" {
		return out
	}
	mine := map[string]bool{}
	for _, n := range sliceDeviceNames(obj) {
		mine[n] = true
	}
	claims := slices.Clone(rel.Objects(ClaimsByPool, poolKey(driver, pool)))
	sort.Slice(claims, func(i, j int) bool { // the same claim wins every time a device is shared
		if claims[i].GetNamespace() != claims[j].GetNamespace() {
			return claims[i].GetNamespace() < claims[j].GetNamespace()
		}
		return claims[i].GetName() < claims[j].GetName()
	})
	for _, c := range claims {
		for _, r := range claimResults(c) {
			if r.driver == driver && r.pool == pool && mine[r.device] && out[r.device] == nil {
				out[r.device] = linkTo(keyResourceClaims, c.GetNamespace(), c.GetName())
			}
		}
	}
	return out
}

func resourceSliceDetail(obj *unstructured.Unstructured, rel model.Related) []model.DetailSection {
	var d detailItems
	d.add(titleDriver, model.NestedString(obj, "spec", fieldDriver))
	pool := model.NestedString(obj, "spec", "pool", fieldName)
	if pool != "" {
		gen, _ := model.NestedInt64(obj, "spec", "pool", "generation")
		count, _ := model.NestedInt64(obj, "spec", "pool", "resourceSliceCount")
		unit := " slices)"
		if count == 1 {
			unit = " slice)"
		}
		pool += " (generation " + itoa(gen) + ", " + itoa(count) + unit
	}
	d.add(titlePool, pool)
	node, link := sliceNode(obj)
	d.put(linkedItem(titleNode, node, link))
	if sel := nodeSelectorText(model.NestedMap(obj, "spec", "nodeSelector")); sel != "" {
		d.add("Node Selector", sel)
	}
	sections := withSection(nil, model.TitleProperties, d)

	devices := model.NestedSlice(obj, "spec", "devices")
	if len(devices) == 0 {
		return sections
	}
	held := map[string]*model.ObjectRef{}
	if rel.Listed(ClaimsByPool) {
		held = sliceAllocations(obj, rel)
	}
	cols := []model.Column{
		{Title: titleName, Type: model.ColumnText}, {Title: "Attributes", Type: model.ColumnText},
		{Title: titleCapacity, Type: model.ColumnText}, {Title: itemTaints, Type: model.ColumnText},
		{Title: titleClaim, Type: model.ColumnText},
	}
	rows := make([][]model.Cell, 0, min(len(devices), maxSliceDevices))
	for _, dev := range devices[:min(len(devices), maxSliceDevices)] {
		name := model.NestedString(dev, fieldName)
		claim := model.Cell{}
		if ref := held[name]; ref != nil {
			claim = model.Cell{Text: ref.Namespace + "/" + ref.Name, Link: ref}
		}
		rows = append(rows, []model.Cell{
			{Text: name},
			{Text: strings.Join(deviceAttributes(dev), "\n")},
			{Text: strings.Join(deviceCapacity(dev), "\n")},
			{Text: strings.Join(deviceTaints(dev), "\n")},
			claim,
		})
	}
	return append(sections, model.DetailSection{Title: titleDevices, Table: model.CellTable(cols, rows)})
}

// deviceAttributes renders a device's attributes as "name: value", sorted by name. An attribute
// holds one of int, bool, string or version.
func deviceAttributes(dev any) []string {
	attrs := model.NestedMap(dev, "attributes")
	out := make([]string, 0, len(attrs))
	for _, k := range sortedKeys(attrs) {
		a := attrs[k]
		var v string
		switch {
		case hasValue(model.NestedMap(a), "string"):
			v = model.NestedString(a, "string")
		case hasValue(model.NestedMap(a), "version"):
			v = model.NestedString(a, "version")
		case hasValue(model.NestedMap(a), "bool"):
			b, _ := model.NestedBool(a, "bool")
			v = strconv.FormatBool(b)
		default:
			if n, ok := model.NestedInt64(a, "int"); ok {
				v = itoa(n)
			}
		}
		out = append(out, k+": "+v)
	}
	return out
}

// deviceCapacity renders a device's capacity as "name: quantity", sorted by name. The driver
// names its capacities and Kubernetes declares no unit for them: one named as a byte resource
// ("memory") is shown in binary units (D-32), any other ("size", "cores") as the driver writes it.
func deviceCapacity(dev any) []string {
	capacity := model.NestedMap(dev, "capacity")
	out := make([]string, 0, len(capacity))
	for _, k := range sortedKeys(capacity) {
		out = append(out, k+": "+model.FormatResource(k, model.NestedIntOrString(capacity[k], "value")))
	}
	return out
}

// deviceTaints renders a device's taints as kubectl writes node taints: "key=value:Effect".
func deviceTaints(dev any) []string {
	taints := model.NestedSlice(dev, "taints")
	out := make([]string, 0, len(taints))
	for _, t := range taints {
		s := model.NestedString(t, "key")
		if v := model.NestedString(t, "value"); v != "" {
			s += "=" + v
		}
		if e := model.NestedString(t, "effect"); e != "" {
			s += ":" + e
		}
		out = append(out, s)
	}
	return out
}

// nodeSelectorText renders a core NodeSelector: its terms, ORed, each term's requirements ANDed as
// "key op (values)".
func nodeSelectorText(sel map[string]any) string {
	var terms []string
	for _, term := range model.NestedSlice(sel, "nodeSelectorTerms") {
		var reqs []string
		for _, field := range []string{"matchExpressions", "matchFields"} {
			for _, r := range model.NestedSlice(term, field) {
				req := model.NestedString(r, "key") + " " + model.NestedString(r, "operator")
				if vs := model.NestedStrings(r, "values"); len(vs) > 0 {
					req += " (" + strings.Join(vs, ",") + ")"
				}
				reqs = append(reqs, req)
			}
		}
		if len(reqs) > 0 {
			terms = append(terms, strings.Join(reqs, ", "))
		}
	}
	return strings.Join(terms, " or ")
}
