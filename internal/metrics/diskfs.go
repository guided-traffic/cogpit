package metrics

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Which of a node's filesystems count (D-90, the owner's WF26 of 2026-10-18: "Hat der Server
// vielleicht auch weitere Blockstorages … eine SSD an /data"): node-exporter reports every
// filesystem the host mounts, by mountpoint, device and type; a node's drawer shows a usage bar for
// each real one, the root first, and the Nodes list's Disk column the fullest. Real is what holds
// the node's own data on a device of its own:
//
//   - not a filesystem in memory or of the kernel (tmpfs, devtmpfs, proc, sysfs, cgroup, …), not a
//     container's layers (overlay) or a read-only image (squashfs, erofs, iso9660): fsTypesSkipped;
//   - not the kernel's and the boot loader's mounts (/boot, /boot/efi, /dev, /proc, /run, /sys),
//     a snap's (/snap), a single file bind-mounted under /etc (a kind node's /etc/hosts), the pods'
//     volumes under /var/lib/kubelet and the container runtimes' state under /var/lib/docker,
//     /var/lib/containerd, /var/lib/containers and /var/lib/rancher: mountsSkipped. A disk mounted
//     at /var/lib/kubelet or /var/lib/containerd itself counts: it is the node's;
//   - one mountpoint per device: a device mounted more than once (a bind mount, a btrfs
//     subvolume) is the same space, kept at "/" when it is the root, else at its shortest
//     mountpoint.
//
// The query skips what it can (the answer stays small on a node with a hundred pods' volumes
// where the exporter excludes nothing) and diskUsages applies the same rules again to what comes
// back, so a store that matches regular expressions differently still shows the same filesystems.

// fsTypesSkipped are the filesystem types that hold none of the node's own data: node-exporter's
// default exclusions, with tmpfs and ramfs (memory) and rootfs (the pseudo filesystem older
// kernels list beside the real root). A PromQL regular expression, matched whole.
const fsTypesSkipped = `tmpfs|ramfs|devtmpfs|rootfs|overlay|squashfs|erofs|iso9660|nsfs|proc|procfs|sysfs|cgroup|cgroup2|` +
	`autofs|binfmt_misc|bpf|configfs|debugfs|devpts|fusectl|fuse.lxcfs|hugetlbfs|mqueue|pstore|rpc_pipefs|securityfs|selinuxfs|tracefs|nfsd`

// mountsSkipped are the mountpoints that are not the node's own filesystems. A PromQL regular
// expression, matched whole.
const mountsSkipped = `/(boot|dev|proc|run|sys|snap)(/.*)?|/etc/.+|/var/lib/(kubelet|docker|containerd|containers|rancher)/.+`

var (
	fsTypeSkipped = regexp.MustCompile(`^(?:` + fsTypesSkipped + `)$`)
	mountSkipped  = regexp.MustCompile(`^(?:` + mountsSkipped + `)$`)
)

// realFilesystem reports whether a series' filesystem counts: a mountpoint, a type that holds
// data, a mountpoint of the node's own.
func realFilesystem(l map[string]string) bool {
	mp := l["mountpoint"]
	return strings.HasPrefix(mp, "/") && !fsTypeSkipped.MatchString(l["fstype"]) && !mountSkipped.MatchString(mp)
}

// metricFSSize and metricFSAvail are node-exporter's filesystem gauges.
const (
	metricFSSize  = "node_filesystem_size_bytes"
	metricFSAvail = "node_filesystem_avail_bytes"
)

// diskSelector selects the real filesystems among node-exporter's series, as the exporter sees the
// host (its chart mounts the host's root and passes --path.rootfs, so the host's "/" reads as "/").
const diskSelector = `fstype!~"` + fsTypesSkipped + `",mountpoint!~"` + mountsSkipped + `"`

// diskQuery is the one query of a disk poll: the size and the available bytes of every node's real
// filesystems, each series marked with a "part" label so that one answer carries both. max by
// (node, instance, device, fstype, mountpoint) keeps one series per filesystem however many jobs
// scrape the exporter; the node label (kube-prometheus-stack's and the prometheus chart's service
// discovery) or the instance names the node.
func diskQuery() string {
	part := func(metric, name string) string {
		return fmt.Sprintf(`label_replace(max by (node, instance, device, fstype, mountpoint) (%s{%s}), "part", %q, "", "")`, metric, diskSelector, name)
	}
	return part(metricFSSize, "size") + " or " + part(metricFSAvail, "avail")
}

// fsHalf is one filesystem's two gauges as they come in.
type fsHalf struct {
	fs                model.NodeFilesystem
	size, avail       float64
	hasSize, hasAvail bool
	at                int64
}

// diskUsages joins the size and the available bytes of each node's real filesystems; a filesystem
// with only one of the two is left out (half an answer is no share), and so is a node without one.
func diskUsages(series []promSeries) []model.NodeDisk {
	type key struct{ node, mountpoint string }
	halves := map[key]*fsHalf{}
	for _, s := range series {
		name := nodeName(s.labels)
		if name == "" || len(s.points) == 0 || !realFilesystem(s.labels) {
			continue
		}
		pt := s.points[len(s.points)-1]
		k := key{name, s.labels["mountpoint"]}
		h := halves[k]
		if h == nil {
			h = &fsHalf{fs: model.NodeFilesystem{Mountpoint: k.mountpoint, Device: s.labels["device"], FSType: s.labels["fstype"]}}
			halves[k] = h
		}
		switch s.labels["part"] {
		case "size":
			h.size, h.hasSize = max(h.size, pt.v), true
		case "avail":
			h.avail, h.hasAvail = max(h.avail, pt.v), true
		default:
			continue
		}
		h.at = max(h.at, pt.t)
	}
	byNode := map[string][]*fsHalf{}
	for k, h := range halves {
		if h.hasSize && h.hasAvail && h.size > 0 {
			h.fs.SizeBytes, h.fs.UsedBytes = int64(h.size), int64(max(h.size-h.avail, 0))
			byNode[k.node] = append(byNode[k.node], h)
		}
	}
	out := make([]model.NodeDisk, 0, len(byNode))
	for name, hs := range byNode {
		out = append(out, nodeDisk(name, hs))
	}
	slices.SortFunc(out, func(a, b model.NodeDisk) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// nodeDisk is a node's filesystems, one per device, the root first, then by mountpoint; its
// summary the fullest of them (the root on a tie).
func nodeDisk(name string, hs []*fsHalf) model.NodeDisk {
	slices.SortFunc(hs, func(a, b *fsHalf) int { return compareMounts(a.fs.Mountpoint, b.fs.Mountpoint) })
	var (
		fs  []model.NodeFilesystem
		at  time.Time
		dev = map[string]bool{}
	)
	for _, h := range hs {
		// Sorted by preference, so the first mountpoint of a device is the one kept: "/" before
		// any other, then the shortest.
		if d := h.fs.Device; d != "" {
			if dev[d] {
				continue
			}
			dev[d] = true
		}
		fs = append(fs, h.fs)
		if t := time.UnixMilli(h.at); t.After(at) {
			at = t
		}
	}
	slices.SortFunc(fs, func(a, b model.NodeFilesystem) int { return compareShown(a.Mountpoint, b.Mountpoint) })
	return model.NewNodeDisk(name, at, fs)
}

// compareMounts orders the mountpoints of one device by preference: "/" first, then the
// shortest, then by name.
func compareMounts(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "/":
		return -1
	case b == "/":
		return 1
	case len(a) != len(b):
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

// compareShown orders a node's filesystems as the drawer shows them: "/" first, then by mountpoint.
func compareShown(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "/":
		return -1
	case b == "/":
		return 1
	}
	return strings.Compare(a, b)
}
