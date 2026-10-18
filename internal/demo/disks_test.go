package demo

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// The demo makes up every node's disks (D-90): a root on each, a data disk on some workers, the
// summary the fullest; the same numbers for the same moment, no request counted; a hub over it
// knows before its first poll that it has disks, so the demo's Nodes list polls them.
func TestDemoDisks(t *testing.T) {
	c := New(Options{Pods: 100, NoChurn: true, Seed: 3})
	m := c.Metrics()
	fixed := time.Date(2026, 10, 18, 12, 0, 7, 0, time.UTC)
	m.now = func() time.Time { return fixed }
	disks, err := m.ListNodeDisks(context.Background())
	if err != nil || len(disks) == 0 {
		t.Fatalf("%d disks, err %v", len(disks), err)
	}
	several := 0
	for _, d := range disks {
		if len(d.Filesystems) == 0 || d.Filesystems[0].Mountpoint != "/" || d.Filesystems[0].Device != "/dev/sda1" {
			t.Fatalf("%s: %+v, want the root first", d.Name, d.Filesystems)
		}
		for _, f := range d.Filesystems {
			if f.Share() <= 0 || f.Share() >= 1 || f.Share() > d.Share() {
				t.Errorf("%s %s: share %v, the summary's %v", d.Name, f.Mountpoint, f.Share(), d.Share())
			}
		}
		if len(d.Filesystems) > 1 {
			several++
		}
	}
	if several == 0 {
		t.Error("no node with more than one filesystem")
	}
	again, _ := m.ListNodeDisks(context.Background())
	if again[0].UsedBytes != disks[0].UsedBytes || m.Requests() != 0 {
		t.Errorf("not the same numbers for the same moment, or %d requests counted", m.Requests())
	}
	h := metrics.NewHub(metrics.Fixed(m), metrics.Options{})
	defer h.Close()
	if !h.DisksPossible() {
		t.Error("a hub over the demo's provider has no disks before its first poll")
	}
}
