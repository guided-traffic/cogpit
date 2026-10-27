package fyneui

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// A node's drawer with its Devices (resource-parity › Nodes, G6): the resource slices that name the
// node, one section each under a Devices heading. PNGs: testdata/node_devices_drawer_*.png.

// fixtureNodeSlices are two drivers' slices on the busy fixture node: GPUs with attributes and
// capacity, and network virtual functions, one of them tainted; and a slice of another node.
func fixtureNodeSlices() []*unstructured.Unstructured {
	slice := func(name, node, driver string, devices ...map[string]any) *unstructured.Unstructured {
		devs := make([]any, 0, len(devices))
		for _, d := range devices {
			devs = append(devs, d)
		}
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "resource.k8s.io/v1", "kind": "ResourceSlice",
			"metadata": map[string]any{"name": name, "uid": "uid-slice-" + name, "creationTimestamp": ts(0)},
			"spec": map[string]any{"driver": driver, "nodeName": node, "devices": devs,
				"pool": map[string]any{"name": node, "generation": int64(3), "resourceSliceCount": int64(1)}},
		}}
	}
	gpu := func(name string, index int64) map[string]any {
		return map[string]any{"name": name,
			"attributes": map[string]any{"model": map[string]any{"string": "A100"}, "index": map[string]any{"int": index},
				"driverVersion": map[string]any{"version": "550.54.15"}},
			"capacity": map[string]any{"memory": map[string]any{"value": "40Gi"}}}
	}
	vf := func(name, pci string, taint bool) map[string]any {
		d := map[string]any{"name": name, "attributes": map[string]any{"pciAddress": map[string]any{"string": pci},
			"sriov": map[string]any{"bool": true}}}
		if taint {
			d["taints"] = []any{map[string]any{"key": "nic.example.net/link-down", "effect": "NoSchedule"}}
		}
		return d
	}
	return []*unstructured.Unstructured{
		slice(busyNode+"-gpu.example.com-x7k2p", busyNode, "gpu.example.com", gpu("gpu-0", 0), gpu("gpu-1", 1)),
		slice(busyNode+"-nic.example.net-b7x4q", busyNode, "nic.example.net",
			vf("vf-0", "0000:3b:00.2", false), vf("vf-1", "0000:3b:00.3", true)),
		slice("worker-b-01-gpu.example.com-q9m2z", "worker-b-01", "gpu.example.com", gpu("gpu-0", 0)),
	}
}

// nodeDevicesFake is kindsFake on the Nodes view with the busy node's drawer built with the
// fixture's slices as related objects, as the engine builds it.
func nodeDevicesFake(t testing.TB) (*fakeEngine, model.ObjectRef) {
	f := kindsFake(t, "nodes")
	f.kinds = append(f.kinds, kindInfo{Kind: "resourceslices", Title: "Resource Slices", APIKind: "ResourceSlice", Group: "resource.k8s.io", Version: "v1"})
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: busyNode}
	p, _ := kinds.ByKey("nodes")
	var node *unstructured.Unstructured
	for _, o := range fixtureNodeObjects() {
		if o.GetName() == busyNode {
			node = o
		}
	}
	rel := fixtureRelated{"pods": fixturePodObjects(), "resourceslices": fixtureNodeSlices()}
	m, err := engine.BuildDetailWith(p, ref, node, fixtureEventObjects(), fixedNow, rel)
	if err != nil {
		t.Fatal(err)
	}
	f.details[ref.String()] = m
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	return f, ref
}

// TestNodeDrawerDevices: the drawer shows the Devices heading, a section per slice of the node with
// its pool, slice link and device count, and the slice's link navigates to it.
func TestNodeDrawerDevices(t *testing.T) {
	f, _ := nodeDevicesFake(t)
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the drawer", func() bool { return sh.drawer.last != nil })
	var drivers []string
	for _, sec := range sh.drawer.last.Sections {
		if sec.Group == "Devices" {
			drivers = append(drivers, sec.Title)
		}
	}
	if len(drivers) != 2 || drivers[0] != "gpu.example.com" || drivers[1] != "nic.example.net" {
		t.Fatalf("Devices sections %q", drivers)
	}
	var heading bool
	for _, l := range findAll[*sectionHead](sh.drawer.details) {
		heading = heading || l.title == "Devices"
	}
	if !heading {
		t.Error("no Devices heading in the drawer")
	}
}

func renderNodeDevices(t *testing.T, mode ThemeMode, name string) {
	f, _ := nodeDevicesFake(t)
	registerProbes(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the drawer", func() bool {
		return len(sh.kinds) > 0 && sh.drawer.last != nil && sh.diag.total > 0
	})
	settle(w)
	scrollDrawerTo(t, sh, "Devices")
	settle(w)
	savePNG(t, w, name)
}

func TestRenderNodeDevicesDrawerDark(t *testing.T) {
	renderNodeDevices(t, ThemeDark, "node_devices_drawer_dark.png")
}
