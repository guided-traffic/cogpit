package metrics

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// The cluster's totals from Prometheus (the cluster Overview's history, hubcluster.go): the sums of
// CPU and memory over the nodes, from the series the node charts read (promrange.go, nodePlan):
// node-exporter's where the Prometheus has them (A43; kube-prometheus-stack drops cAdvisor's root
// cgroup), else the nodes' root cgroups, so that the history and the Metrics Server's live values
// (a node's usage is its root cgroup's working set) mean the same. Every node is one query per
// metric naming the cluster's nodes by their node label or instance (their names or addresses,
// RangeQuery.Nodes), so that a machine the Prometheus scrapes besides them (a hypervisor, a
// storage host) is not counted (D-157); the masters are the control-plane nodes named the same
// way; the workers are every node less the masters. A cluster with too many nodes to name in a
// query (groupQueryMax), or whose nodes the engine did not give, is summed over every series the
// Prometheus has, and the answer says how many machines more than nodes that counts.

// groupQueryMax is the longest a query naming the cluster's nodes may be, URL-encoded: it goes as
// a GET through the API server's service proxy, and a proxy in front of the API server may cap the
// request line (ingress-nginx's default: 8 KiB, the path and the range included). The API server
// itself takes 1 MiB.
const groupQueryMax = 7 << 10

// fitsGroup reports whether the queries naming a group of nodes are short enough to send.
func fitsGroup(exprs ...string) bool {
	for _, e := range exprs {
		if len(url.QueryEscape(e)) > groupQueryMax {
			return false
		}
	}
	return true
}

// clusterPlan is the queries of the cluster's chart from one source: CPU and memory over every
// node, and over the masters when the cluster has any ("" otherwise). named: every node is the
// cluster's nodes by name and address; else every series the Prometheus has.
type clusterPlan struct {
	cpuAll, memAll, cpuMaster, memMaster string
	named                                bool
}

// clusterQueries are the queries of the cluster's chart at a rate window from cAdvisor's root
// cgroups. Nodes and masters are found by their names and addresses (matchGroup).
func clusterQueries(q RangeQuery, masters []string, win time.Duration) clusterPlan {
	cp := clusterPlan{
		cpuAll: fmt.Sprintf(`sum(rate(%s{id="/"}[%s]))`, metricCPU, promDuration(win)),
		memAll: fmt.Sprintf(`sum(%s{id="/"})`, metricMemory),
	}
	if len(q.Nodes) > 0 {
		m := matchGroup(q.Nodes, q.NodeAddresses)
		if cpu, mem := nodeSum(metricCPU, m, win), nodeSum(metricMemory, m, 0); fitsGroup(cpu, mem) {
			cp.cpuAll, cp.memAll, cp.named = cpu, mem, true
		}
	}
	if len(masters) > 0 {
		m := matchGroup(masters, q.Addresses)
		cp.cpuMaster, cp.memMaster = nodeSum(metricCPU, m, win), nodeSum(metricMemory, m, 0)
	}
	return cp
}

// clusterExporterQueries are clusterQueries from node-exporter's series (A43), by the node label or
// the instance.
func clusterExporterQueries(q RangeQuery, masters []string, win time.Duration) clusterPlan {
	cpu := func(x string) string { return exporterCPU(x, win) }
	cp := clusterPlan{cpuAll: cpu(""), memAll: exporterMemory("")}
	if len(q.Nodes) > 0 {
		m := matchGroup(q.Nodes, q.NodeAddresses)
		if c, mem := exporterOf(cpu, m), exporterOf(exporterGroupMemory, m); fitsGroup(c, mem) {
			cp.cpuAll, cp.memAll, cp.named = c, mem, true
		}
	}
	if len(masters) > 0 {
		m := matchGroup(masters, q.Addresses)
		cp.cpuMaster, cp.memMaster = exporterOf(cpu, m), exporterOf(exporterMemory, m)
	}
	return cp
}

// foreignMachines counts the machines with series of source beyond the cluster's nodes: one
// instant query, sent only when the cluster's sums could not name its nodes. A failure counts
// none: the count is the history's footnote, not the history.
func (p *Prometheus) foreignMachines(ctx context.Context, source string, nodes int) int {
	sel := metricNodeMemTotal
	if source == UsageCAdvisor {
		sel = selector(metricMemory, `id="/"`)
	}
	s, err := p.queryInstant(ctx, "count(max by (node, instance) ("+sel+"))")
	if err != nil || len(s) == 0 || len(s[0].points) == 0 {
		return 0
	}
	return max(int(s[0].points[len(s[0].points)-1].v)-nodes, 0)
}

// clusterRange answers a range query of the cluster scope: the targets "" (every node), master and
// worker. CPU and memory (WantUsage, every query must answer) and the pod counts (WantPods,
// kube-state-metrics': a failure leaves them out) exist for the cluster; a query that wants neither
// costs nothing. CPU and memory come from node-exporter where this Prometheus has its series, else
// from cAdvisor's root cgroups, as a node's chart's do (nodeSource, A43), and UsageSource says
// which.
func (p *Prometheus) clusterRange(ctx context.Context, q RangeQuery) (*ObjectSeries, error) {
	out := &ObjectSeries{Targets: map[string]*TargetSeries{}, From: q.From, To: q.To, Step: q.Step}
	if q.Want.Has(WantPods) {
		p.clusterPods(ctx, q, out)
	}
	if q.Want != 0 && !q.Want.Has(WantUsage) {
		return out, nil
	}
	masters := ClusterMasters(q.Name)
	win := rangeRate(q.Step)
	run := func(expr string) ([]promSeries, error) { return p.queryRange(ctx, expr, q.From, q.To, q.Step) }
	var res [4][]promSeries
	ask := func(exprs ...string) (bool, error) {
		var errs [4]error
		res = [4][]promSeries{}
		var jobs []func()
		for i, expr := range exprs {
			if expr != "" {
				jobs = append(jobs, func() { res[i], errs[i] = run(expr) })
			}
		}
		runParallel(jobs...)
		if err := firstStateError(errs[:]...); err != nil {
			return false, err
		}
		return hasPoints(res[0]) || hasPoints(res[1]), nil // every node's: the masters' may lack an exporter
	}
	named := false
	plan := func(cp clusterPlan) (bool, error) {
		named = cp.named
		return ask(cp.cpuAll, cp.memAll, cp.cpuMaster, cp.memMaster)
	}
	exporter := func() (bool, error) { return plan(clusterExporterQueries(q, masters, win)) }
	cadvisor := func() (bool, error) { return plan(clusterQueries(q, masters, win)) }
	source, err := p.nodeSource(exporter, cadvisor)
	if err != nil {
		return nil, err
	}
	out.UsageSource = source
	if !named && len(q.Nodes) > 0 && source != UsageNone {
		out.ForeignMachines = p.foreignMachines(ctx, source, len(q.Nodes))
	}
	first := func(s []promSeries) []point {
		if len(s) == 0 {
			return nil
		}
		return s[0].points // a sum without "by" is one series
	}
	set := func(target string, cpu, mem []point) {
		if u := mergeUsage(cpu, mem); len(u) > 0 {
			targetOf(out, target).Usage = u
		}
	}
	allCPU, allMem := first(res[0]), first(res[1])
	set("", allCPU, allMem)
	mCPU, mMem := first(res[2]), first(res[3])
	if len(masters) == 0 || source == UsageNodeExporter && len(mCPU) == 0 && len(mMem) == 0 {
		// No masters; or no exporter on the masters (a DaemonSet that does not tolerate their
		// taint): every exporter's sum is the workers', and the masters have no history.
		set(TargetWorker, allCPU, allMem)
		return out, nil
	}
	set(TargetMaster, mCPU, mMem)
	set(TargetWorker, subtract(allCPU, mCPU), subtract(allMem, mMem))
	return out, nil
}

// subtract is a - b at the times both have a point: the workers' sum is every node's less the
// masters'. A time only a has is left out (the masters' part is not known then).
func subtract(a, b []point) []point {
	var out []point
	joinHas(a, b, func(t time.Time, x, y float64, hasA, hasB bool) {
		if hasA && hasB {
			out = append(out, point{t: t.UnixMilli(), v: max(x-y, 0)})
		}
	})
	return out
}
