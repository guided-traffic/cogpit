package engine

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A node's drawer shows the node's disks (G12, D-90): its real filesystems from Prometheus's
// node-exporter series, a usage bar each; the Nodes list's Disk column shows the fullest. While a
// node's drawer is open and the cluster's settings leave the disk visible, the drawer holds a
// subscription of the disks' scope (metrics.DiskScope):
// shared with an open Nodes list, polled at the list interval (60 s), one instant query of
// Prometheus per poll; released when the drawer closes, shows another object or the disk is hidden.

// followDisk starts or stops the drawer's poll of the disks as the hidden metrics say. A drawer
// that stopped subscribes nothing: stop releases what was subscribed before it (wmu orders them).
func (d *drawerWatch) followDisk(s *Session, hidden MetricSet) {
	b := d.backend
	want := b.Metrics != nil && d.profile.DiskCell != nil && hidden.diskShown(d.profile.Usage) && b.Metrics.DisksPossible()
	d.wmu.Lock()
	var old *metrics.Subscription
	switch {
	case want && d.disk == nil && !d.stopped.Load():
		kind := d.ref.Kind
		d.disk = b.Metrics.Subscribe(metrics.DiskScope, func() { s.emit(Event{Type: EventMetrics, View: kind}) })
	case !want && d.disk != nil:
		old, d.disk = d.disk, nil
	}
	d.wmu.Unlock()
	old.Release()
}

// diskSub returns the drawer's poll of the disks; nil while none runs.
func (d *drawerWatch) diskSub() *metrics.Subscription {
	d.wmu.Lock()
	defer d.wmu.Unlock()
	return d.disk
}

// drawerDisk is a node's disk for its drawer: the disks' latest fresh result by the node's name or
// addresses, else why there is none. It sends no request.
func drawerDisk(hub *metrics.Hub, ref model.ObjectRef, obj *unstructured.Unstructured) (*model.NodeDisk, string) {
	if d, ok := hub.NodeDisk(nodeKeys(ref.Name, obj)...); ok {
		return &d, ""
	}
	st := hub.DiskState()
	switch st.Status {
	case metrics.StatusUnknown:
		return nil, "looking for Prometheus…"
	case metrics.StatusAvailable:
		return nil, "node-exporter reports no filesystem of this node"
	}
	return nil, st.Message
}

// nodeAddresses are a node's addresses besides its name (nodeKeys without the name), which the
// metrics hub matches a node's Prometheus series by (Hub.SetNodeAddresses, A43).
func nodeAddresses(name string, obj *unstructured.Unstructured) []string {
	return nodeKeys(name, obj)[1:]
}

// setNodeAddresses gives the metrics hub a node drawer's node's addresses from the cache before its
// chart's first poll: kube-prometheus-stack's node-exporter is known by them only (A43). A node the
// cache does not hold yet gets them when ObjectMetrics reads it.
func (d *drawerWatch) setNodeAddresses() {
	if d.profile.Usage != model.UsageNodes || d.backend.Metrics == nil {
		return
	}
	if obj, ok := KindGet(d.obj, d.profile, d.namespace(), d.ref.Name); ok {
		d.backend.Metrics.SetNodeAddresses(d.ref.Name, nodeAddresses(d.ref.Name, obj))
	}
}

// nodeKeys are the names node-exporter's series may give a node: its name, then its addresses
// (InternalIP, ExternalIP, Hostname), for an exporter whose instance is the node's address.
func nodeKeys(name string, obj *unstructured.Unstructured) []string {
	keys := []string{name}
	if obj != nil {
		for _, a := range model.NestedSlice(obj.Object, "status", "addresses") {
			if addr := model.NestedString(a, "address"); addr != "" && addr != name {
				keys = append(keys, addr)
			}
		}
	}
	return keys
}
