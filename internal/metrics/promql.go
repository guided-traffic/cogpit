package metrics

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The PromQL the Prometheus provider sends (architecture §9). Every query is built here, and every
// label value that comes from user data (a pod's, a namespace's, a node's name) goes through
// quoteLabel, so a name can never end the string literal and add a matcher or a function. The
// queries use the cAdvisor metrics the kubelet exposes, which every Prometheus that scrapes a
// cluster has: container_cpu_usage_seconds_total, container_memory_working_set_bytes,
// container_network_*_bytes_total, container_fs_*.

// rateWindow is the range of the rate() queries of the current values (the lists, metrics.get) and
// the shortest of the charts'. Prometheus asks for at least four scrape intervals: the prometheus
// chart scrapes every minute, and cAdvisor stamps a sample with its own collection time, up to a
// minute before the scrape. On kind with that chart (2026-10-12) a [2m] range left 5 to 7 of 52
// points of a half-hour chart empty and the newest one or two minutes missing; [4m] left none.
const rateWindow = 4 * time.Minute

// rangeRate is the rate() range of a chart's query at step: every sample between two points counts
// (a step and a scrape interval, which Cogpit takes as a minute), and never less than rateWindow.
// A week's chart (a step of about ten minutes) so shows a spike between two of its points.
func rangeRate(step time.Duration) time.Duration { return max(rateWindow, step+time.Minute) }

// promDuration writes a range for PromQL in whole minutes or seconds: "4m", "665s".
func promDuration(d time.Duration) string {
	if d%time.Minute == 0 {
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	}
	return strconv.FormatInt(int64((d+time.Second-1)/time.Second), 10) + "s"
}

// quoteLabel returns s as a double-quoted PromQL string literal.
func quoteLabel(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// The metrics, by variant.
const (
	metricCPU      = "container_cpu_usage_seconds_total"
	metricMemory   = "container_memory_working_set_bytes"
	metricNetRx    = "container_network_receive_bytes_total"
	metricNetTx    = "container_network_transmit_bytes_total"
	metricFSUsage  = "container_fs_usage_bytes"
	metricFSReads  = "container_fs_reads_bytes_total"
	metricFSWrites = "container_fs_writes_bytes_total"
)

// containerMatchers keeps the series of real containers: the pod-level cgroup has container=""
// and the pause container "POD", and counting them would double every sum. It is Lens's "filter
// empty containers", on by default (captured); keepEmptyMatchers is the filter off.
const containerMatchers = `container!="",container!="POD"`

// keepEmptyMatchers is the container matcher with the empty-container filter off (Cluster Settings
// › Metrics, QuerySettings.KeepEmptyContainers): the series without a container name stay in the
// answer, where the pod-level cgroup is the pod's own line and, on a Prometheus whose cAdvisor
// series carry no container name, the only one. The pause container stays out.
const keepEmptyMatchers = `container!="POD"`

// containerFilter is the container matcher of a cluster's settings.
func containerFilter(keepEmpty bool) string {
	if keepEmpty {
		return keepEmptyMatchers
	}
	return containerMatchers
}

// podMatchers selects a pod; namespace "" selects the pod in any namespace (never used for a
// named pod), name "" every pod of the namespace.
func podMatchers(namespace, pod string) string {
	var m []string
	if namespace != "" {
		m = append(m, "namespace="+quoteLabel(namespace))
	}
	if pod != "" {
		m = append(m, "pod="+quoteLabel(pod))
	}
	return strings.Join(m, ",")
}

// withContainers builds {matchers,<filter>}: the filter is containerMatchers or keepEmptyMatchers.
func withContainers(matchers, filter string) string {
	if matchers == "" {
		return "{" + filter + "}"
	}
	return "{" + matchers + "," + filter + "}"
}

// podRate is the per-container rate of a counter over win: sum by (container) (rate(metric{...}[4m])).
func podRate(metric, namespace, pod, filter string, win time.Duration) string {
	return fmt.Sprintf("sum by (container) (rate(%s%s[%s]))", metric, withContainers(podMatchers(namespace, pod), filter), promDuration(win))
}

// podGauge is the per-container value of a gauge.
func podGauge(metric, namespace, pod, filter string) string {
	return fmt.Sprintf("sum by (container) (%s%s)", metric, withContainers(podMatchers(namespace, pod), filter))
}

// podNetwork is the pod's traffic: network metrics belong to the pod's network namespace, so they
// are summed over the pod's interfaces without a container matcher.
func podNetwork(metric, namespace, pod string, win time.Duration) string {
	return fmt.Sprintf("sum(rate(%s{%s}[%s]))", metric, podMatchers(namespace, pod), promDuration(win))
}

// podListRate and podListGauge are the list forms: one series per container of every pod of a
// namespace (or of every namespace).
func podListRate(metric, namespace, filter string) string {
	return fmt.Sprintf("sum by (namespace, pod, container) (rate(%s%s[%s]))", metric, withContainers(podMatchers(namespace, ""), filter), promDuration(rateWindow))
}

func podListGauge(metric, namespace, filter string) string {
	return fmt.Sprintf("sum by (namespace, pod, container) (%s%s)", metric, withContainers(podMatchers(namespace, ""), filter))
}

// nodeMatch is how the series of one node, or of a set of nodes (the cluster's control plane), are
// found: by the node label (kube-prometheus-stack's cAdvisor series, the prometheus chart's service
// discovery), else by the instance, which is the node's name (the prometheus chart's cAdvisor job)
// or one of its addresses (an exporter scraped under its host-network address, as
// kube-prometheus-stack's node-exporter is), with or without a port. Names and addresses are user
// data: quoted, and escaped for the regular expressions. It is how the disks' series are matched
// (A43, D-43; nodeKeys in the engine).
type nodeMatch struct {
	byNode, byInstance string
}

// matchNode builds the two matchers of one node known by its name and, optionally, its addresses.
func matchNode(name string, addrs []string) nodeMatch {
	inst := regexp.QuoteMeta(name) + "(:[0-9]+)?"
	if len(addrs) > 0 {
		inst = instanceOf([]string{name}, addrs)
	}
	return nodeMatch{byNode: "node=" + quoteLabel(name), byInstance: "instance=~" + quoteLabel(inst)}
}

// matchGroup builds the two matchers of a group of nodes (the control plane): alternations of
// their names, and of their names and addresses.
func matchGroup(names, addrs []string) nodeMatch {
	return nodeMatch{byNode: "node=~" + quoteLabel(alternation(names)), byInstance: "instance=~" + quoteLabel(instanceOf(names, addrs))}
}

// alternation is "(a|b)" of the names, each escaped for a regular expression.
func alternation(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = regexp.QuoteMeta(n)
	}
	return "(" + strings.Join(q, "|") + ")"
}

// instanceOf is the instance pattern of nodes known by names and addresses: any of them, with or
// without a port, an IPv6 address also in brackets ("[fd00::1]:9100").
func instanceOf(names, addrs []string) string {
	alts := make([]string, 0, len(names)+2*len(addrs))
	seen := map[string]bool{}
	add := func(a string) {
		if a != "" && !seen[a] {
			seen[a] = true
			alts = append(alts, a)
		}
	}
	for _, n := range names {
		add(regexp.QuoteMeta(n))
	}
	for _, a := range addrs {
		add(regexp.QuoteMeta(a))
		if strings.Contains(a, ":") {
			add(`\[` + regexp.QuoteMeta(a) + `\]`)
		}
	}
	return "(" + strings.Join(alts, "|") + ")(:[0-9]+)?"
}

// selector writes metric{matchers}, the empty matchers left out; a metric alone has no braces.
func selector(metric string, matchers ...string) string {
	m := make([]string, 0, len(matchers))
	for _, x := range matchers {
		if x != "" {
			m = append(m, x)
		}
	}
	if len(m) == 0 {
		return metric
	}
	return metric + "{" + strings.Join(m, ",") + "}"
}

// nodeSelectors are the two ways the series of one node's root cgroup are labelled: with the
// kube-prometheus-stack's node label, or with the instance (the node's name, which the Prometheus
// chart's cAdvisor job gives, with or without the kubelet's port, or one of its addresses).
func nodeSelectors(metric, extra string, m nodeMatch) [2]string {
	root := `id="/"`
	if extra != "" {
		root += "," + extra
	}
	return [2]string{selector(metric, root, m.byNode), selector(metric, root, m.byInstance)}
}

// nodeDisks keeps the node's block devices among its root cgroup's filesystem series. cAdvisor
// reports every mount there, and the overlay mounts of the containers repeat the disk they live
// on, so a sum over all of them counts one disk many times (kind, 2026-10-12: nine overlays of
// /dev/vda1 summed to 503 GiB on a 56 GiB disk). /dev/shm is memory.
const nodeDisks = `device=~"/dev/.+",device!="/dev/shm"`

// nodeSum sums the root cgroup of one node: a gauge as is (win 0), a counter as its rate over win.
// The two selectors are joined by "or", which yields the second only when the first matches
// nothing.
func nodeSum(metric string, m nodeMatch, win time.Duration) string {
	return nodeSumWhere(metric, "", m, win)
}

// nodeSumWhere is nodeSum over the series that also match extra.
func nodeSumWhere(metric, extra string, m nodeMatch, win time.Duration) string {
	sel := nodeSelectors(metric, extra, m)
	f := func(s string) string {
		if win > 0 {
			return fmt.Sprintf("sum(rate(%s[%s]))", s, promDuration(win))
		}
		return "sum(" + s + ")"
	}
	return f(sel[0]) + " or " + f(sel[1])
}

// node-exporter's metrics, which a node's CPU and memory come from where the cluster's Prometheus
// has them (A43, D-43, owner 2026-10-14 evening): kube-prometheus-stack's kubelet ServiceMonitor
// drops cAdvisor's root cgroup, so a node there had no history (F1).
const (
	metricNodeCPU          = "node_cpu_seconds_total"
	metricNodeMemTotal     = "node_memory_MemTotal_bytes"
	metricNodeMemFree      = "node_memory_MemFree_bytes"
	metricNodeInactiveFile = "node_memory_Inactive_file_bytes"
)

// nodeBusy keeps the CPU time a node worked: not idle, not waiting for I/O (idle time with I/O
// outstanding) and not stolen by the hypervisor, none of which the root cgroup's usage (the
// Metrics Server's node CPU) counts either.
const nodeBusy = `mode!~"idle|iowait|steal"`

// exporterCPU is the cores the node-exporter series that match worked over win, each exporter
// counted once however many jobs scrape it (max by node and instance, as the disks' query); an
// empty matcher is every exporter.
func exporterCPU(matcher string, win time.Duration) string {
	return exporterRate(metricNodeCPU, nodeBusy, matcher, win)
}

// exporterRate is the rate over win of a node-exporter counter, summed over the series that match
// matcher and extra (a node's CPUs, interfaces or disks) and each exporter counted once however many
// jobs scrape it.
func exporterRate(metric, extra, matcher string, win time.Duration) string {
	return fmt.Sprintf("sum(max by (node, instance) (sum by (node, instance, job) (rate(%s[%s]))))",
		selector(metric, matcher, extra), promDuration(win))
}

// node-exporter's network and disk metrics, which a node's Network and Filesystem charts read
// where the cluster's Prometheus has them (G12, 2026-10-16), as its CPU and memory do (A43):
// kube-prometheus-stack drops cAdvisor's root cgroup, so those charts stayed empty there. The
// filesystem's usage reads the disks' gauges (metricFSSize, metricFSAvail in disk.go).
const (
	metricNodeNetRx       = "node_network_receive_bytes_total"
	metricNodeNetTx       = "node_network_transmit_bytes_total"
	metricNodeDiskRead    = "node_disk_read_bytes_total"
	metricNodeDiskWritten = "node_disk_written_bytes_total"
)

// nodeInterfaces keeps a node's own network interfaces: the loopback and the virtual interfaces
// the container runtimes, the network plugins and the kernel create (the pods' veth and lxc ends,
// the plugins' bridges, overlays and tunnels, the kernel's fallback tunnel devices, WireGuard) are
// left out, since the same bytes pass them and the node's own interface, and a sum over all of
// them counts the traffic several times (a real cluster with Cilium, 2026-10-16: 34 interfaces on
// one node, the overlay, WireGuard and the physical interface each carrying the pods' traffic).
// The names are the plugins' and the kernel's defaults; one not listed is counted (not guarded),
// and so are a bond and its members (counted twice, not guarded).
const nodeInterfaces = `device!~"lo|veth.+|lxc.+|cali.+|cilium_.+|flannel.+|cni.+|cbr.+|docker.+|br-.+|virbr.+|vxlan.+|genev.+|tunl.+|tun.+|wg.+|kube-.+|weave.*|nodelocaldns|antrea-.+|ovs-.+|ovn-.+|gre.+|erspan.+|ip6gre.+|ip6tnl.+|ip_vti.+|ip6_vti.+|sit.+|dummy.+|nerdctl.+|[a-f0-9]{15}"`

// nodeBlockDevices keeps the disks among node-exporter's disk statistics: device-mapper and
// software RAID devices repeat the disks they sit on, network block devices (Ceph's rbd, nbd) are
// network traffic, and loop, RAM and optical devices are no disks. node-exporter itself leaves the
// partitions out.
const nodeBlockDevices = `device!~"(dm-|md|rbd|nbd|loop|ram|zram|sr|fd).*"`

// exporterFSUsage is the bytes used on a node's block devices from node-exporter: each device's
// size less what is available to an unprivileged process (the disks' rule, D-43), each device
// counted once however often it is mounted (bind mounts repeat it), each exporter once.
func exporterFSUsage(matcher string) string {
	return fmt.Sprintf("sum(max by (node, instance) (sum by (node, instance, job) (max by (node, instance, job, device) (%s - %s))))",
		selector(metricFSSize, matcher, nodeDisks), selector(metricFSAvail, matcher, nodeDisks))
}

// exporterMemory is the memory in use the node-exporter series that match give: the total less the
// free memory and the inactive file cache. That is how the kubelet counts the root cgroup's working
// set (its usage less inactive_file; on cgroup v2 the root's usage is MemTotal less MemFree), so
// the history and the Metrics Server's live values mean the same. Each exporter counted once.
func exporterMemory(matcher string) string {
	return fmt.Sprintf("sum(max by (node, instance) (%s - %s - %s))",
		selector(metricNodeMemTotal, matcher), selector(metricNodeMemFree, matcher), selector(metricNodeInactiveFile, matcher))
}

// exporterGroupMemory is exporterMemory with the matcher on the total only: the free and the
// inactive series join it one to one, so it holds for all three, and a query naming every node of
// the cluster is a third as long. The free and inactive series of every exporter are read, as an
// every-node query reads them anyway.
func exporterGroupMemory(matcher string) string {
	return fmt.Sprintf("sum(max by (node, instance) (%s - %s - %s))",
		selector(metricNodeMemTotal, matcher), metricNodeMemFree, metricNodeInactiveFile)
}

// exporterOf joins the two ways the exporter series of one node (or of the control plane) are
// labelled by "or", as nodeSum does: the node label first, else the instance.
func exporterOf(q func(matcher string) string, m nodeMatch) string {
	return q(m.byNode) + " or " + q(m.byInstance)
}

// nodeListSum is the list form: one series per node, labelled by node and instance.
func nodeListSum(metric string, rate bool) string {
	if rate {
		return fmt.Sprintf(`sum by (node, instance) (rate(%s{id="/"}[%s]))`, metric, promDuration(rateWindow))
	}
	return fmt.Sprintf(`sum by (node, instance) (%s{id="/"})`, metric)
}
