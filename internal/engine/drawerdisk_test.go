package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Nodes list's Disk column and a node drawer's disk (G12, D-90): node-exporter's filesystems
// through Prometheus, matched to a node by its name or one of its addresses, polled only while
// shown; the header's tooltip says where the numbers come from or why the column is empty.

// diskProv is a provider with disks: n1 by its name, n2 by its InternalIP.
type diskProv struct{ onePodProvider }

func (diskProv) ID() string    { return metrics.PrometheusID }
func (diskProv) Label() string { return "Prometheus (monitoring/prometheus-server)" }
func (diskProv) ListNodeDisks(context.Context) ([]model.NodeDisk, error) {
	return []model.NodeDisk{
		{Name: "n1", Timestamp: time.Now(), UsedBytes: 90 << 30, SizeBytes: 100 << 30},
		{Name: "10.0.0.2", Timestamp: time.Now(), UsedBytes: 10 << 30, SizeBytes: 40 << 30},
	}, nil
}

func nodeObject(name, ip string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{
		"addresses":   []any{map[string]any{"type": "InternalIP", "address": ip}, map[string]any{"type": "Hostname", "address": name}},
		"allocatable": map[string]any{"cpu": "4", "memory": "8Gi"},
	}}}
	o.SetName(name)
	o.SetUID(types.UID("uid-" + name))
	o.SetResourceVersion("1")
	return o
}

func waitSignal(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("the feed never polled")
	}
}

func TestNodesListDiskColumn(t *testing.T) {
	p, _ := kinds.ByKey("nodes")
	hub := metrics.NewHub(metrics.Fixed(diskProv{}), metrics.Options{ListInterval: time.Hour})
	defer hub.Close()
	hub.Fetch(context.Background(), metrics.Scope{Kind: metrics.KindNodes}) // the detector answers: a provider with disks
	b := &Backend{Metrics: hub}
	signalled := make(chan struct{}, 8)
	feed := b.startListFeed(p, nsPlan{}, feedWants{disk: true}, func() { signalled <- struct{}{} })
	if feed == nil || len(feed.subs) != 1 {
		t.Fatalf("feed = %+v, want the disks' scope alone", feed)
	}
	waitSignal(t, signalled)

	objs := []*unstructured.Unstructured{nodeObject("n1", "10.0.0.1"), nodeObject("n2", "10.0.0.2"), nodeObject("n3", "10.0.0.3")}
	rc := &rowCache{}
	snap := rc.buildWith(p, "ctx", objs, viewParams{sort: SortSpec{Column: model.ColumnDisk, Desc: true}}, nil, 0, feed, nil)
	disk := p.ColumnIndex(model.ColumnDisk)
	got := map[string]model.Cell{}
	for i := range snap.Rows {
		got[snap.Rows[i].Ref.Name] = cellAt(&snap.Rows[i], disk)
	}
	if c := got["n1"]; c.Text != "90%" || c.Bar == nil || c.Bar.Status != model.StatusError || c.Bar.Detail != "90.0 GiB of 100.0 GiB" {
		t.Errorf("n1 (by its name) = %+v, bar %+v", c, c.Bar)
	}
	if c := got["n2"]; c.Text != "25%" || c.Bar == nil || c.Bar.Status != model.StatusNone {
		t.Errorf("n2 (by its address) = %+v", c)
	}
	if c := got["n3"]; c.Text != "" || c.Bar != nil {
		t.Errorf("n3 has no disk but shows %+v", c)
	}
	if snap.Rows[0].Ref.Name != "n1" || snap.Rows[2].Ref.Name != "n3" {
		t.Errorf("sorted by Disk descending: %s, %s, %s", snap.Rows[0].Ref.Name, snap.Rows[1].Ref.Name, snap.Rows[2].Ref.Name)
	}
	// The CPU and Memory cells stay the profile's: the feed polls no usage.
	if c := cellAt(&snap.Rows[0], p.ColumnIndex(model.ColumnCPU)); c.Text != "" {
		t.Errorf("cpu = %+v without a usage poll", c)
	}
	if tip := diskColumnTip(feed); !strings.Contains(tip, "node-exporter") || !strings.HasSuffix(tip, "Prometheus (monitoring/prometheus-server).") {
		t.Errorf("tip = %q", tip)
	}
	feed.release()
	if hub.Subscribed() != 0 {
		t.Error("the disks' poll outlived the feed")
	}
}

// Without a provider that knows disks (the Metrics Server alone, the demo cluster) the disks are
// not polled, the column is empty and its tooltip says why. (A cluster whose Prometheus is not
// found: TestNodeDisksWithoutPrometheus in package metrics, the state the tooltip quotes.)
func TestNodesListDiskColumnWithoutPrometheus(t *testing.T) {
	p, _ := kinds.ByKey("nodes")
	hub := metrics.NewHub(metrics.Fixed(onePodProvider{}), metrics.Options{ListInterval: time.Hour})
	defer hub.Close()
	hub.Fetch(context.Background(), metrics.Scope{Kind: metrics.KindNodes})
	b := &Backend{Metrics: hub}
	feed := b.startListFeed(p, nsPlan{}, feedWants{usage: true, disk: true}, func() {})
	defer feed.release()
	if len(feed.subs) != 1 || feed.disk {
		t.Errorf("the feed polls %d scopes, disks %v: want the nodes' usage alone", len(feed.subs), feed.disk)
	}
	if tip := diskColumnTip(feed); !strings.HasSuffix(tip, "Empty: this cluster's metrics provider measures no disk.") {
		t.Errorf("tip = %q", tip)
	}
	if hub.DisksPossible() {
		t.Error("a hub over the Metrics Server alone may have disks")
	}
	both := metrics.NewHubFor(metrics.Sources{Live: metrics.Fixed(onePodProvider{}), History: metrics.Fixed(diskProv{})}, metrics.Options{})
	defer both.Close()
	if !both.DisksPossible() {
		t.Error("a hub with a Prometheus slot may not have disks")
	}
	st := metrics.State{Status: metrics.StatusNotServed, Message: "the disk usage comes from node-exporter through Prometheus, and no Prometheus was found"}
	if tip := diskTipOf(st); !strings.HasSuffix(tip, ". Empty: the disk usage comes from node-exporter through Prometheus, and no Prometheus was found.") {
		t.Errorf("tip = %q", tip)
	}
	if tip := diskColumnTip(nil); tip != "Disk: the fullest of the node's filesystems as node-exporter reports them, read through Prometheus." {
		t.Errorf("tip without a feed = %q", tip)
	}
}

// The disks are polled only while the Disk column shows: hidden by the cluster's settings (the
// nodes, or the filesystem) or put away by the user (ui.setColumns), the feed asks for none.
func TestDiskPolledOnlyWhileShown(t *testing.T) {
	p, _ := kinds.ByKey("nodes")
	for _, tc := range []struct {
		name   string
		hidden MetricSet
		params viewParams
		want   feedWants
	}{
		{"shown", 0, viewParams{}, feedWants{usage: true, disk: true}},
		{"filesystem hidden", HideFilesystem, viewParams{}, feedWants{usage: true}},
		{"nodes hidden", HideNodes, viewParams{}, feedWants{}},
		{"cpu and memory hidden", HideCPU | HideMemory, viewParams{}, feedWants{disk: true}},
		{"column put away", 0, viewParams{hidden: []string{model.ColumnDisk}, hiddenSet: true}, feedWants{usage: true}},
	} {
		if got := listFeedWants(p, tc.hidden, tc.params); got != tc.want {
			t.Errorf("%s: wants %+v, want %+v", tc.name, got, tc.want)
		}
	}
	cols, ids := hideMetricColumns(p, p.Columns, HideFilesystem)
	if i := p.ColumnIndex(model.ColumnDisk); !cols[i].Hidden || len(ids) != 1 || ids[0] != model.ColumnDisk {
		t.Errorf("Filesystem hidden: disk column %+v, metricsHidden %v", cols[i], ids)
	}
	pods, _ := kinds.ByKey("pods")
	if got := listFeedWants(pods, 0, viewParams{}); got.disk {
		t.Error("the Pods list polls disks")
	}
}

// A node's drawer polls the disks while it is open (unless hidden) and reads its node's disk by name
// or address; a pod's drawer has none.
func TestNodeDrawerDisk(t *testing.T) {
	hub := metrics.NewHub(metrics.Fixed(diskProv{}), metrics.Options{ListInterval: time.Hour})
	defer hub.Close()
	if d, note := drawerDisk(hub, model.ObjectRef{Kind: "nodes", Name: "n1"}, nil); d != nil || note != "looking for Prometheus…" {
		t.Errorf("before the first poll: %+v, %q", d, note)
	}
	signalled := make(chan struct{}, 4)
	sub := hub.Subscribe(metrics.DiskScope, func() { signalled <- struct{}{} })
	defer sub.Release()
	waitSignal(t, signalled)
	if d, _ := drawerDisk(hub, model.ObjectRef{Kind: "nodes", Name: "n2"}, nodeObject("n2", "10.0.0.2")); d == nil || d.SizeBytes != 40<<30 {
		t.Errorf("n2 by its address = %+v", d)
	}
	if d, note := drawerDisk(hub, model.ObjectRef{Kind: "nodes", Name: "n3"}, nodeObject("n3", "10.0.0.3")); d != nil || !strings.Contains(note, "no filesystem of this node") {
		t.Errorf("n3 = %+v, %q", d, note)
	}

	p, _ := kinds.ByKey("nodes")
	s := &Session{}
	d := &drawerWatch{backend: &Backend{Metrics: hub}, ref: model.ObjectRef{Kind: "nodes", Name: "n1"}, profile: p}
	d.followDisk(s, 0)
	if d.diskSub() == nil {
		t.Fatal("a node's drawer does not poll the disks")
	}
	d.followDisk(s, HideFilesystem)
	if d.diskSub() != nil {
		t.Error("the disks are polled while the filesystem is hidden")
	}
	d.stopped.Store(true)
	d.followDisk(s, 0)
	if d.diskSub() != nil {
		t.Error("a stopped drawer subscribed the disks")
	}
	pods, _ := kinds.ByKey("pods")
	pd := &drawerWatch{backend: &Backend{Metrics: hub}, ref: model.ObjectRef{Kind: "pods", Name: "a"}, profile: pods}
	pd.followDisk(s, 0)
	if pd.diskSub() != nil {
		t.Error("a pod's drawer polls the disks")
	}
}
