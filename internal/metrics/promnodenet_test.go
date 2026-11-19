package metrics_test

import (
	"context"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// A node's Network and Filesystem charts read node-exporter where it runs, as its CPU and memory
// do (G12): on kube-prometheus-stack, which drops cAdvisor's root cgroup, they stayed empty before.
// The exporter is found by the node's address, its pods' and overlays' interfaces are left out of
// the traffic, and its block devices are counted once each.
func TestNodeNetworkAndFilesystemFromNodeExporter(t *testing.T) {
	ctx := context.Background()
	inst := `instance=~"(n1|10\\.0\\.0\\.7)(:[0-9]+)?"`
	want := metrics.WantNetwork | metrics.WantFilesystem

	// kube-prometheus-stack's node-exporter labels a node's series with the exporter's host-network
	// address as the instance and no node label (a real cluster; the names here are synthetic).
	// Every query sums its series, so the answers carry no labels, as the real one's did.
	t.Run("kube-prometheus-stack: node-exporter by the address, no root cgroup", func(t *testing.T) {
		c := newPromCluster(t, false)
		c.prom.on(`node_network_receive_bytes_total{`+inst, s(nil, 1000, 900e3, 1030, 950e3))
		c.prom.on(`node_network_transmit_bytes_total{`+inst, s(nil, 1000, 230e3, 1030, 250e3))
		c.prom.on(`node_filesystem_size_bytes{`+inst, s(nil, 1000, 43e9, 1030, 43.3e9))
		prov := promProvider(t, c, metrics.PrometheusOptions{})
		q := nodeQuery("n1", "10.0.0.7")
		q.Want = want
		before := c.proxyCount()
		got, err := prov.ObjectRange(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		node := got.Targets[""]
		if node == nil || len(node.Network) != 2 || node.Network[1].ReceiveBytesPerSec != 950e3 || node.Network[1].TransmitBytesPerSec != 250e3 {
			t.Fatalf("network %+v", node)
		}
		if got.FilesystemSource != metrics.FilesystemUsage || len(node.Filesystem) != 2 || node.Filesystem[1].UsageBytes != 43.3e9 {
			t.Errorf("filesystem %q: %+v", got.FilesystemSource, node.Filesystem)
		}
		if n := c.proxyCount() - before; n != 3 {
			t.Errorf("%d requests, want 3: received, transmitted and the usage, one query each with both sources", n)
		}
		// Without the address the exporter is not found; nothing stands in for it.
		q = nodeQuery("n1")
		q.Want = want
		if got, err := prov.ObjectRange(ctx, q); err != nil || got.Targets[""] != nil {
			t.Errorf("by the name alone: %+v %v", got, err)
		}
	})

	t.Run("the prometheus chart without node-exporter: the root cgroup", func(t *testing.T) {
		c := newPromCluster(t, false)
		c.prom.on(`container_network_receive_bytes_total{id="/",node="n1"}`, s(nil, 1000, 10, 1030, 20))
		c.prom.on(`container_network_transmit_bytes_total{id="/",node="n1"}`, s(nil, 1000, 5, 1030, 6))
		c.prom.on(`container_fs_usage_bytes{id="/",device=~"/dev/.+",device!="/dev/shm",node="n1"}`, s(nil, 1000, 60e9))
		prov := promProvider(t, c, metrics.PrometheusOptions{})
		q := nodeQuery("n1")
		q.Want = want
		got, err := prov.ObjectRange(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if node := got.Targets[""]; node == nil || len(node.Network) != 2 || node.Network[1].ReceiveBytesPerSec != 20 || node.Filesystem[0].UsageBytes != 60e9 {
			t.Errorf("the root cgroup's network and filesystem: %+v", node)
		}
	})

	t.Run("the queries: node-exporter first, its own interfaces and disks", func(t *testing.T) {
		c := newPromCluster(t, false)
		prov := promProvider(t, c, metrics.PrometheusOptions{})
		q := nodeQuery("n1", "10.0.0.7")
		q.Want = want
		if _, err := prov.ObjectRange(ctx, q); err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, r := range c.prom.requests() {
			switch {
			case strings.HasPrefix(r.query, "sum(max by (node, instance) (sum by (node, instance, job) (rate(node_network_"):
				seen["network"] = true
				exp, cad, ok := strings.Cut(r.query, " or sum(rate(container_network_")
				if !ok || !strings.Contains(exp, `device!~"lo|veth.+|lxc.+|cali.+|cilium_.+|flannel.+`) || !strings.Contains(exp, inst) ||
					!strings.Contains(cad, `{id="/",`) {
					t.Errorf("a node's network query: %s", r.query)
				}
			case strings.HasPrefix(r.query, "sum(max by (node, instance) (sum by (node, instance, job) (max by (node, instance, job, device) (node_filesystem_size_bytes"):
				seen["usage"] = true
				if !strings.Contains(r.query, `node_filesystem_avail_bytes{`+inst+`,device=~"/dev/.+",device!="/dev/shm"}`) ||
					!strings.Contains(r.query, ` or sum(container_fs_usage_bytes{id="/",device=~"/dev/.+"`) {
					t.Errorf("a node's filesystem usage query: %s", r.query)
				}
			case strings.HasPrefix(r.query, "sum(max by (node, instance) (sum by (node, instance, job) (rate(node_disk_"):
				seen["io"] = true
				if !strings.Contains(r.query, `device!~"(dm-|md|rbd|nbd|loop|ram|zram|sr|fd).*"`) || !strings.Contains(r.query, " or sum(rate(container_fs_") {
					t.Errorf("a node's disk query: %s", r.query)
				}
			}
		}
		if !seen["network"] || !seen["usage"] || !seen["io"] {
			t.Errorf("queries seen %v, want the network, the usage and, since neither had a series, the disks' rates", seen)
		}
	})
}
