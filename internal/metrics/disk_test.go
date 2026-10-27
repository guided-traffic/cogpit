package metrics_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The nodes' disks (G12, the Nodes list's Disk column): node-exporter's real filesystems through
// Prometheus (D-90), one instant query per list poll, matched to nodes by their node label or
// instance; never the kubelet's summary API (nodes/proxy).

const gib = float64(1 << 30)

// diskPart is a disk series' labels: the part, then l and a filesystem's mountpoint, device, type.
func diskPart(name string, l map[string]string, mountpoint, device, fstype string) map[string]string {
	out := map[string]string{"part": name, "mountpoint": mountpoint, "device": device, "fstype": fstype}
	for k, v := range l {
		out[k] = v
	}
	return out
}

// diskSeries is node-exporter's answer to the disk query: a node by its node label (the prometheus
// chart's and kube-prometheus-stack's service discovery), one by its instance (the node's name with
// the exporter's port), one by its address, and a size without its available bytes (left out); each
// with its root filesystem. n5 has more, shaped like a real node's: a data disk fuller than its
// root, the boot partitions, memory filesystems, its root's device bind-mounted at
// /var/lib/kubelet, a pod's volume under it, a container's layers and a single file under /etc.
func diskSeries(p *fakeProm) {
	root := func(name string, l map[string]string) map[string]string {
		return diskPart(name, l, "/", "/dev/sda1", "ext4")
	}
	byLabel := map[string]string{"node": "n1", "instance": "10.0.0.1:9100"}
	byName := map[string]string{"instance": "n2:9100"}
	byAddr := map[string]string{"instance": "10.0.0.3:9100"}
	half := map[string]string{"node": "n4", "instance": "10.0.0.4:9100"}
	n5 := map[string]string{"node": "n5", "instance": "10.0.0.5:9100"}
	both := func(l map[string]string, mountpoint, device, fstype string, size, avail float64) []promTestSeries {
		return []promTestSeries{
			s(diskPart("size", l, mountpoint, device, fstype), 0, size),
			s(diskPart("avail", l, mountpoint, device, fstype), 0, avail),
		}
	}
	series := []promTestSeries{
		s(root("size", byLabel), 0, 100*gib), s(root("avail", byLabel), 0, 25*gib),
		s(root("size", byName), 0, 50*gib), s(root("avail", byName), 0, 45*gib),
		s(root("size", byAddr), 0, 200*gib), s(root("avail", byAddr), 0, 10*gib),
		s(root("size", half), 0, 80*gib),
	}
	for _, fs := range []struct {
		mountpoint, device, fstype string
		size, avail                float64
	}{
		{"/", "/dev/sda1", "ext4", 120 * gib, 70 * gib},
		{"/data", "/dev/sdb1", "xfs", 1000 * gib, 80 * gib},
		{"/boot", "/dev/sda16", "ext4", 0.9 * gib, 0.7 * gib},
		{"/boot/efi", "/dev/sda15", "vfat", 0.1 * gib, 0.09 * gib},
		{"/run", "tmpfs", "tmpfs", 3 * gib, 3 * gib},
		{"/run/lock", "tmpfs", "tmpfs", 0.01 * gib, 0.01 * gib},
		{"/var/lib/kubelet", "/dev/sda1", "ext4", 120 * gib, 70 * gib},
		{"/var/lib/kubelet/pods/7c1e/volumes/kubernetes.io~csi/pvc-1/mount", "/dev/sdc", "ext4", 10 * gib, 1 * gib},
		{"/var/lib/containerd/io.containerd.runtime.v2.task/k8s.io/1f2e/rootfs", "overlay", "overlay", 120 * gib, 70 * gib},
		{"/etc/hosts", "/dev/sda1", "ext4", 120 * gib, 70 * gib},
		{"/srv/backup", "/dev/sdd1", "ext4", 500 * gib, 450 * gib},
	} {
		series = append(series, both(n5, fs.mountpoint, fs.device, fs.fstype, fs.size, fs.avail)...)
	}
	p.onRel(`node_filesystem_size_bytes{fstype!~"tmpfs|`, series...)
}

// dualHub is a hub over a fake cluster's Metrics Server and its Prometheus behind the service proxy.
func dualHub(t *testing.T, c *promCluster, opts metrics.Options) *metrics.Hub {
	t.Helper()
	c.srv.SetProxy("monitoring", "prometheus-server:80", c.prom)
	src, _, err := metrics.SourcesForConn(c.conn, servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h := metrics.NewHubFor(src, opts)
	t.Cleanup(h.Close)
	return h
}

func TestNodeDisksFromNodeExporter(t *testing.T) {
	c := newPromCluster(t, true)
	diskSeries(c.prom)
	h := dualHub(t, c, metrics.Options{ListInterval: time.Hour})
	onChange, polls := pollCounter()
	sub := h.Subscribe(metrics.DiskScope, onChange)
	defer sub.Release()
	waitPoll(t, polls, 1)

	if st := h.DiskState(); !st.Usable() || st.Label != "Prometheus (monitoring/prometheus-server)" {
		t.Fatalf("disk state = %+v", st)
	}
	for _, tc := range []struct {
		keys       []string
		used, size float64
	}{
		{[]string{"n1"}, 75 * gib, 100 * gib},
		{[]string{"n2", "10.0.0.2"}, 5 * gib, 50 * gib},
		{[]string{"n3", "10.0.0.3"}, 190 * gib, 200 * gib}, // by its address
	} {
		d, ok := h.NodeDisk(tc.keys...)
		if !ok || d.UsedBytes != int64(tc.used) || d.SizeBytes != int64(tc.size) || d.Timestamp.IsZero() ||
			d.Mountpoint != "/" || len(d.Filesystems) != 1 || d.Filesystems[0].Device != "/dev/sda1" || d.Filesystems[0].FSType != "ext4" {
			t.Errorf("NodeDisk(%v) = %+v, %v; want / with %v of %v", tc.keys, d, ok, tc.used, tc.size)
		}
	}
	if d, ok := h.NodeDisk("n4", "10.0.0.4"); ok {
		t.Errorf("a node with a size and no available bytes has a disk: %+v", d)
	}
	if _, ok := h.NodeDisk("n9"); ok {
		t.Error("an unknown node has a disk")
	}
	var queries []string
	for _, r := range c.prom.requests() {
		if r.query != "" {
			queries = append(queries, r.query)
		}
	}
	// One query: both gauges of every node's filesystems but those that are not the node's own
	// (the query's text checked against a real Prometheus and node-exporter, D-90).
	sel := `{fstype!~"tmpfs|ramfs|devtmpfs|rootfs|overlay|squashfs|erofs|iso9660|nsfs|proc|procfs|sysfs|cgroup|cgroup2|` +
		`autofs|binfmt_misc|bpf|configfs|debugfs|devpts|fusectl|fuse.lxcfs|hugetlbfs|mqueue|pstore|rpc_pipefs|securityfs|selinuxfs|tracefs|nfsd",` +
		`mountpoint!~"/(boot|dev|proc|run|sys|snap)(/.*)?|/etc/.+|/var/lib/(kubelet|docker|containerd|containers|rancher)/.+"}`
	want := `label_replace(max by (node, instance, device, fstype, mountpoint) (node_filesystem_size_bytes` + sel + `), "part", "size", "", "") or ` +
		`label_replace(max by (node, instance, device, fstype, mountpoint) (node_filesystem_avail_bytes` + sel + `), "part", "avail", "", "")`
	if len(queries) != 1 || queries[0] != want {
		t.Errorf("queries = %q, want the one disk query %q", queries, want)
	}
}

// A node's real filesystems (D-90): the root first, then by mountpoint; the boot partitions, the
// memory filesystems, the pods' volumes, the containers' layers and a file under /etc left out;
// the root's device once, at "/"; the summary the fullest (/data at 92%).
func TestNodeDiskFilesystems(t *testing.T) {
	c := newPromCluster(t, true)
	diskSeries(c.prom)
	h := dualHub(t, c, metrics.Options{ListInterval: time.Hour})
	onChange, polls := pollCounter()
	sub := h.Subscribe(metrics.DiskScope, onChange)
	defer sub.Release()
	waitPoll(t, polls, 1)
	d, ok := h.NodeDisk("n5")
	if !ok {
		t.Fatal("n5 has no disk")
	}
	var got []string
	for _, f := range d.Filesystems {
		got = append(got, f.Mountpoint+" "+f.Device+" "+f.FSType)
	}
	want := []string{"/ /dev/sda1 ext4", "/data /dev/sdb1 xfs", "/srv/backup /dev/sdd1 ext4"}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Errorf("filesystems %q, want %q", got, want)
	}
	if d.Mountpoint != "/data" || d.UsedBytes != int64(920*gib) || d.SizeBytes != int64(1000*gib) {
		t.Errorf("summary %s %d of %d, want the fullest: /data, 920 GiB of 1000 GiB", d.Mountpoint, d.UsedBytes, d.SizeBytes)
	}
	if f := d.Filesystems[2]; f.FreeBytes() != int64(450*gib) || f.Share() != 0.1 {
		t.Errorf("/srv/backup free %d share %v", f.FreeBytes(), f.Share())
	}
}

// Without Prometheus the column's state says why, and nothing is sent but the detection's look at
// the cached Services (none here: no request at all).
func TestNodeDisksWithoutPrometheus(t *testing.T) {
	c := newPromCluster(t, true)
	src, _, err := metrics.SourcesForConn(c.conn, servicesOf(), metrics.PrometheusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h := metrics.NewHubFor(src, metrics.Options{ListInterval: time.Hour})
	t.Cleanup(h.Close)
	if st := h.DiskState(); st.Status != metrics.StatusUnknown {
		t.Errorf("state before the first poll = %+v", st)
	}
	onChange, polls := pollCounter()
	sub := h.Subscribe(metrics.DiskScope, onChange)
	defer sub.Release()
	waitPoll(t, polls, 1)
	st := h.DiskState()
	if st.Status != metrics.StatusNotServed || !strings.Contains(st.Message, "no Prometheus was found") {
		t.Errorf("state = %+v", st)
	}
	if n := c.proxyCount(); n != 0 {
		t.Errorf("%d proxy requests without a Prometheus", n)
	}
	if _, ok := h.NodeDisk("n1"); ok {
		t.Error("a disk without a provider")
	}
}

// A Prometheus without node-exporter answers: the column's state names it, and Prometheus stays
// found for the charts (its slot is not marked failing).
func TestNodeDisksWithoutNodeExporter(t *testing.T) {
	c := newPromCluster(t, true)
	h := dualHub(t, c, metrics.Options{ListInterval: time.Hour})
	onChange, polls := pollCounter()
	sub := h.Subscribe(metrics.DiskScope, onChange)
	defer sub.Release()
	waitPoll(t, polls, 1)
	st := h.DiskState()
	if st.Status != metrics.StatusNotServed || !strings.Contains(st.Message, "Prometheus (monitoring/prometheus-server) has no node-exporter series") {
		t.Errorf("state = %+v", st)
	}
	if ps := h.Providers(); ps[0].Provider != metrics.PrometheusID || ps[0].Status != metrics.StatusAvailable || ps[0].LastError != "" {
		t.Errorf("Prometheus after an empty disk answer = %+v", ps[0])
	}
}

// A user refused the service proxy: the column says so (forbidden), with the class, never a body.
func TestNodeDisksForbidden(t *testing.T) {
	c := newPromCluster(t, true)
	c.prom.status = http.StatusForbidden
	h := dualHub(t, c, metrics.Options{ListInterval: time.Hour})
	onChange, polls := pollCounter()
	sub := h.Subscribe(metrics.DiskScope, onChange)
	defer sub.Release()
	waitPoll(t, polls, 1)
	if st := h.DiskState(); st.Status != metrics.StatusForbidden && st.Status != metrics.StatusUnavailable {
		t.Errorf("state with the proxy refused = %+v", st)
	}
	if st := h.DiskState(); strings.Contains(st.Message, "refused") {
		t.Errorf("the state quotes the answer's body: %+v", st)
	}
}

// The disk's share and cells: model.NodeDisk.
func TestNodeDiskShare(t *testing.T) {
	d := model.NodeDisk{UsedBytes: 75, SizeBytes: 100}
	if d.Share() != 0.75 || (model.NodeDisk{}).Share() != 0 {
		t.Errorf("shares = %v, %v", d.Share(), (model.NodeDisk{}).Share())
	}
}
