package kinds

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/model"
)

// A node's Devices in its drawer (resource-parity › Nodes, G6): the devices dynamic resource
// allocation drivers publish for the node in resource slices, from the cache through the
// related-objects index SlicesByNode. Lens's documentation (its Device Resources pages, read, not
// captured) names the section and that it lists each device a slice publishes for the node with its
// driver, pool and attributes and the claim holding it.
//
// Cogpit's form, inferred (not the owner's decision): after Allocatable and before the Pods, under
// a "Devices" heading, one section per resource slice, titled by its driver, sorted by driver, pool
// and slice name: the Pool, the Slice (a link) and how many Devices it publishes for the node, then
// a table of those devices, their attributes, capacity and, when one has some, its taints. A slice
// names the node in spec.nodeName, or, with perDeviceNodeSelection, in a device's nodeName (only
// those devices are listed); slices for all nodes or a node selector are not the node's own and
// stay out. The claim holding a device is left out: it would make the Nodes view watch every claim
// in the cluster, as the pod drawer leaves out a claim's state. The section appears only when the
// cluster serves resource.k8s.io/v1 (the profile is offered there only), the user may list resource
// slices, and a slice names the node.

// SlicesByNode indexes resource slices by the nodes they publish devices for: spec.nodeName, or
// with perDeviceNodeSelection each device's nodeName.
var SlicesByNode = model.RelatedIndex{Kind: keyResourceSlices, Name: "byNode", Keys: sliceNodeKeys}

// Bounds of the node's Devices. A slice publishes at most 128 devices (maxSliceDevices).
const (
	maxNodeSlices      = 32 // the sections; the rest are counted in a last one
	nodeDevicesGroup   = "Devices"
	itemSlice          = "Slice"
	moreSlicesTitle    = "More"
	moreSlicesItemName = "Not shown"
)

func sliceNodeKeys(slice *unstructured.Unstructured) []string {
	keys := nonEmpty(model.NestedString(slice, "spec", "nodeName"))
	if per, _ := model.NestedBool(slice, "spec", "perDeviceNodeSelection"); per {
		for _, d := range model.NestedSlice(slice, "spec", "devices") {
			keys = appendUnique(keys, model.NestedString(d, "nodeName"))
		}
	}
	return keys
}

// nodeDevices returns the node's Devices sections, none when no cached slice names the node.
func nodeDevices(node *unstructured.Unstructured, rel model.Related) []model.DetailSection {
	name := node.GetName()
	found := rel.Objects(SlicesByNode, name)
	if name == "" || len(found) == 0 {
		return nil
	}
	slicesOf := slices.Clone(found)
	slices.SortFunc(slicesOf, func(a, b *unstructured.Unstructured) int {
		return cmp.Or(
			strings.Compare(model.NestedString(a, "spec", fieldDriver), model.NestedString(b, "spec", fieldDriver)),
			strings.Compare(model.NestedString(a, "spec", "pool", fieldName), model.NestedString(b, "spec", "pool", fieldName)),
			strings.Compare(a.GetName(), b.GetName()))
	})
	var out []model.DetailSection
	for i, s := range slicesOf {
		if i == maxNodeSlices {
			var more detailItems
			more.add(moreSlicesItemName, strconv.Itoa(len(slicesOf)-i)+" more resource slices")
			out = append(out, model.DetailSection{Group: nodeDevicesGroup, Title: moreSlicesTitle, Items: more})
			break
		}
		if sec, ok := nodeSliceSection(name, s, rel); ok {
			out = append(out, sec)
		}
	}
	return out
}

// nodeSliceSection is one slice's section in the node's Devices: false when the slice publishes no
// device for the node (a per-device slice whose devices are all another node's).
func nodeSliceSection(node string, slice *unstructured.Unstructured, rel model.Related) (model.DetailSection, bool) {
	devices := model.NestedSlice(slice, "spec", "devices")
	if model.NestedString(slice, "spec", "nodeName") != node {
		var mine []any
		for _, d := range devices {
			if model.NestedString(d, "nodeName") == node {
				mine = append(mine, d)
			}
		}
		devices = mine
	}
	if len(devices) == 0 && model.NestedString(slice, "spec", "nodeName") != node {
		return model.DetailSection{}, false
	}
	driver := model.NestedString(slice, "spec", fieldDriver)
	if driver == "" {
		driver = "(no driver)"
	}
	var items detailItems
	items.add(titlePool, model.NestedString(slice, "spec", "pool", fieldName))
	items.put(linkedItem(itemSlice, slice.GetName(), linkTo(keyResourceSlices, "", slice.GetName())))
	items.add(titleDevices, strconv.Itoa(len(devices)))
	sec := model.DetailSection{Group: nodeDevicesGroup, Title: driver, Items: items}
	if len(devices) == 0 {
		return sec, true
	}

	shown := devices[:min(len(devices), maxSliceDevices)]
	tainted := slices.ContainsFunc(shown, func(d any) bool { return len(model.NestedSlice(d, "taints")) > 0 })
	cols := []model.Column{
		{Title: titleName, Type: model.ColumnText}, {Title: "Attributes", Type: model.ColumnText},
		{Title: titleCapacity, Type: model.ColumnText},
	}
	if tainted {
		cols = append(cols, model.Column{Title: itemTaints, Type: model.ColumnText})
	}
	pool := model.NestedString(slice, "spec", "pool", fieldName)
	claimed := rel.Listed(ClaimsByDevice)
	if claimed { // the claim that holds each device, once the claims are listed (A108)
		cols = append(cols, model.Column{Title: titleClaim, Type: model.ColumnText})
	}
	rows := make([][]model.Cell, 0, len(shown))
	for _, d := range shown {
		row := []model.Cell{
			{Text: model.NestedString(d, fieldName)},
			{Text: strings.Join(deviceAttributes(d), "\n")},
			{Text: strings.Join(deviceCapacity(d), "\n")},
		}
		if tainted {
			row = append(row, model.Cell{Text: strings.Join(deviceTaints(d), "\n")})
		}
		if claimed {
			row = append(row, deviceClaimCell(model.NestedString(slice, "spec", fieldDriver), pool, model.NestedString(d, fieldName), rel))
		}
		rows = append(rows, row)
	}
	sec.Table = model.CellTable(cols, rows)
	return sec, true
}
