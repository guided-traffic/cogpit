package demo

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The demo cluster's disks (WF26, D-90): the demo's metrics provider is also a disk provider, so
// that a development build's demo shows the Nodes list's Disk column and the node drawer's bar per
// filesystem without a Prometheus. Every node has its root on /dev/sda1 (ext4, 100 to 250 GiB); about
// half the workers have a data disk at /data (xfs on an NVMe drive, 0.5 to 2 TiB), some of it near
// full, and a few a backup disk at /srv/backup. The numbers are made up, deterministic for a node
// and an hour, and creep up over the day. They cost no request: the real ones are one Prometheus
// query per poll.

var _ metrics.DiskProvider = (*Metrics)(nil)

// ListNodeDisks implements metrics.DiskProvider.
func (m *Metrics) ListNodeDisks(_ context.Context) ([]model.NodeDisk, error) {
	m.c.mu.Lock()
	names := make([]string, 0, len(m.c.store[kindNodes]))
	for _, n := range m.c.store[kindNodes] {
		names = append(names, n.GetName())
	}
	m.c.mu.Unlock()
	slices.Sort(names)
	at := m.measuredAt()
	out := make([]model.NodeDisk, 0, len(names))
	for _, name := range names {
		out = append(out, model.NewNodeDisk(name, at, demoFilesystems(name, at)))
	}
	return out, nil
}

// demoFilesystems are a node's made-up filesystems at a moment, the root first.
func demoFilesystems(name string, at time.Time) []model.NodeFilesystem {
	day := float64(at.Unix()%86400) / 86400 // the day's creep, 0 to 1
	fs := func(mountpoint, device, fstype string, sizeGiB, share float64) model.NodeFilesystem {
		size := int64(sizeGiB * (1 << 30))
		return model.NodeFilesystem{Mountpoint: mountpoint, Device: device, FSType: fstype, SizeBytes: size, UsedBytes: int64(float64(size) * min(share, 0.99))}
	}
	out := []model.NodeFilesystem{fs("/", "/dev/sda1", "ext4", 100+150*unit(name, 11), 0.2+0.5*unit(name, 12)+0.03*day)}
	if !strings.HasPrefix(name, "worker-") {
		return out
	}
	if unit(name, 13) < 0.5 {
		out = append(out, fs("/data", "/dev/nvme0n1", "xfs", 512+1536*unit(name, 14), 0.3+0.65*unit(name, 15)+0.04*day))
	}
	if unit(name, 16) < 0.15 {
		out = append(out, fs("/srv/backup", "/dev/sdb1", "ext4", 1024, 0.1+0.3*unit(name, 17)))
	}
	return out
}
