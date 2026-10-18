package metrics_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// promProvider detects the fake Prometheus through the service proxy and returns its provider.
func promProvider(t *testing.T, c *promCluster, opts metrics.PrometheusOptions) metrics.RangeProvider {
	t.Helper()
	c.srv.SetProxy("monitoring", "prometheus-server:80", c.prom)
	det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(web("monitoring", "prometheus-server", 80)), opts)
	prov, st := det.Detect(context.Background())
	if prov == nil {
		t.Fatalf("no provider: %+v", st)
	}
	rp, ok := prov.(metrics.RangeProvider)
	if !ok {
		t.Fatalf("%T has no history", prov)
	}
	return rp
}

func s(labels map[string]string, vals ...float64) promTestSeries {
	ps := promTestSeries{labels: labels}
	for i := 0; i+1 < len(vals); i += 2 {
		ps.values = append(ps.values, [2]float64{vals[i], vals[i+1]})
	}
	return ps
}

func lc(name string) map[string]string { return map[string]string{"container": name} }

func podQuery(ns, name string) metrics.RangeQuery {
	to := time.Unix(100020, 0)
	return metrics.RangeQuery{Kind: metrics.KindPods, Namespace: ns, Name: name, From: to.Add(-time.Hour), To: to, Step: 30 * time.Second}
}

const mib = 1 << 20

func podData(p *fakeProm) {
	p.on("container_cpu_usage_seconds_total", s(lc("app"), 1000, 0.25, 1030, 0.5), s(lc("sidecar"), 1000, 0.01, 1030, 0.02))
	p.on("container_memory_working_set_bytes", s(lc("app"), 1000, 100*mib, 1030, 110*mib), s(lc("sidecar"), 1000, 10*mib, 1030, 10*mib))
	p.on("container_network_receive_bytes_total", s(nil, 1000, 1000, 1030, 2000))
	p.on("container_network_transmit_bytes_total", s(nil, 1000, 500, 1030, 700))
}

func TestObjectRangeOfAPod(t *testing.T) {
	c := newPromCluster(t, false)
	podData(c.prom)
	c.prom.on("container_fs_usage_bytes", s(lc("app"), 1000, 5000, 1030, 6000))
	prov := promProvider(t, c, metrics.PrometheusOptions{})
	before := c.proxyCount()

	got, err := prov.ObjectRange(context.Background(), podQuery("web", "a"))
	if err != nil {
		t.Fatal(err)
	}
	if n := c.proxyCount() - before; n != 5 {
		t.Errorf("%d requests for a pod's charts, want 5 (cpu, memory, rx, tx, filesystem)", n)
	}

	whole, app := got.Targets[""], got.Targets["app"]
	if whole == nil || app == nil || got.Targets["sidecar"] == nil || len(got.Targets) != 3 {
		t.Fatalf("targets = %v", got.Targets)
	}
	if len(whole.Usage) != 2 || whole.Usage[0].CPUMilli != 260 || whole.Usage[0].MemoryBytes != 110*mib || whole.Usage[1].CPUMilli != 520 || whole.Usage[1].MemoryBytes != 120*mib {
		t.Errorf("whole usage = %+v", whole.Usage)
	}
	if !whole.Usage[0].At.Equal(time.Unix(1000, 0)) || !whole.Usage[1].At.Equal(time.Unix(1030, 0)) {
		t.Errorf("sample times = %v %v", whole.Usage[0].At, whole.Usage[1].At)
	}
	if app.Usage[1].CPUMilli != 500 || app.Usage[1].MemoryBytes != 110*mib {
		t.Errorf("app usage = %+v", app.Usage)
	}
	if len(whole.Network) != 2 || whole.Network[1].ReceiveBytesPerSec != 2000 || whole.Network[1].TransmitBytesPerSec != 700 {
		t.Errorf("network = %+v", whole.Network)
	}
	if len(app.Network) != 0 {
		t.Errorf("a container has no network of its own: %+v", app.Network)
	}
	if got.FilesystemSource != "usage" || len(app.Filesystem) != 2 || app.Filesystem[1].UsageBytes != 6000 || whole.Filesystem[1].UsageBytes != 6000 {
		t.Errorf("filesystem %q: app %+v whole %+v", got.FilesystemSource, app.Filesystem, whole.Filesystem)
	}
	if len(got.Targets["sidecar"].Filesystem) != 0 {
		t.Errorf("sidecar has no filesystem series: %+v", got.Targets["sidecar"].Filesystem)
	}

	// The queries carry the range: an aligned start, the step in seconds.
	var seen int
	for _, r := range c.prom.requests() {
		if !strings.Contains(r.path, "query_range") {
			continue
		}
		seen++
		start, _ := strconv.ParseFloat(r.start, 64)
		if r.step != "30" || start != 96420 || r.end != "100020" {
			t.Errorf("range params: %+v", r)
		}
	}
	if seen != 5 {
		t.Errorf("%d range queries", seen)
	}
}

func TestFilesystemFallsBackToIOAndRemembersIt(t *testing.T) {
	c := newPromCluster(t, false)
	podData(c.prom)
	c.prom.on("container_fs_reads_bytes_total", s(lc("app"), 1000, 10, 1030, 20))
	c.prom.on("container_fs_writes_bytes_total", s(lc("app"), 1000, 1, 1030, 2))
	prov := promProvider(t, c, metrics.PrometheusOptions{})

	before := c.proxyCount()
	got, err := prov.ObjectRange(context.Background(), podQuery("web", "a"))
	if err != nil {
		t.Fatal(err)
	}
	if n := c.proxyCount() - before; n != 7 {
		t.Errorf("%d requests while discovering the filesystem metric, want 7 (4 + usage + reads + writes)", n)
	}
	fs := got.Targets["app"].Filesystem
	if got.FilesystemSource != "io" || len(fs) != 2 || fs[1].ReadBytesPerSec != 20 || fs[1].WriteBytesPerSec != 2 || fs[1].UsageBytes != 0 {
		t.Errorf("filesystem %q: %+v", got.FilesystemSource, fs)
	}
	if got.Targets[""].Filesystem[0].ReadBytesPerSec != 10 {
		t.Errorf("whole filesystem = %+v", got.Targets[""].Filesystem)
	}

	before = c.proxyCount()
	if _, err := prov.ObjectRange(context.Background(), podQuery("web", "a")); err != nil {
		t.Fatal(err)
	}
	if n := c.proxyCount() - before; n != 6 {
		t.Errorf("%d requests once the metric is known, want 6 (4 + reads + writes)", n)
	}
	if n := c.prom.queryCount("container_fs_usage_bytes"); n != 1 {
		t.Errorf("the usage metric was asked for %d times", n)
	}
}

func TestObjectRangeWithoutAnyFilesystemMetric(t *testing.T) {
	c := newPromCluster(t, false)
	podData(c.prom)
	prov := promProvider(t, c, metrics.PrometheusOptions{})
	got, err := prov.ObjectRange(context.Background(), podQuery("web", "a"))
	if err != nil {
		t.Fatal(err)
	}
	if got.FilesystemSource != "" || len(got.Targets[""].Filesystem) != 0 || len(got.Targets[""].Usage) == 0 {
		t.Errorf("source %q, whole %+v", got.FilesystemSource, got.Targets[""])
	}
}

func TestObjectRangeOfANode(t *testing.T) {
	c := newPromCluster(t, false)
	c.prom.on(`node="n1"`, s(nil, 1000, 2, 1030, 4)) // first selector of every node query
	prov := promProvider(t, c, metrics.PrometheusOptions{})
	q := podQuery("", "n1")
	q.Kind = metrics.KindNodes
	got, err := prov.ObjectRange(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	u := got.Targets[""].Usage
	if len(got.Targets) != 1 || len(u) != 2 || u[1].CPUMilli != 4000 || u[1].MemoryBytes != 4 {
		t.Errorf("node usage = %+v (targets %d)", u, len(got.Targets))
	}
	// CPU and memory from node-exporter (A43); the network and the filesystem from node-exporter or
	// else the root cgroup, both in one query each (2026-10-16).
	if got.UsageSource != metrics.UsageNodeExporter || c.prom.queryCount("node_cpu_seconds_total") != 1 || c.prom.queryCount("container_cpu_usage_seconds_total") != 0 {
		t.Errorf("usage source %q", got.UsageSource)
	}
	if n, e := c.prom.queryCount(`id="/"`), c.prom.queryCount("node_network_"); n != 3 || e != 2 {
		t.Errorf("%d node queries ask the root cgroup and %d node-exporter's network, want 3 (received, transmitted, filesystem) and 2", n, e)
	}
}

// nodeQuery is a node's range query with its addresses.
func nodeQuery(name string, addrs ...string) metrics.RangeQuery {
	q := podQuery("", name)
	q.Kind, q.Addresses, q.Want = metrics.KindNodes, addrs, metrics.WantUsage
	return q
}

// A node's CPU and memory history comes from node-exporter where the Prometheus has its series,
// cAdvisor's root cgroup otherwise (A43, D-43, F1), matched by the node label or by the instance:
// the node's name or one of its addresses, as kube-prometheus-stack's exporter is scraped under its
// host-network address without a node label, and its kubelet ServiceMonitor drops the root cgroup.
func TestNodeHistoryFromNodeExporter(t *testing.T) {
	ctx := context.Background()
	t.Run("kube-prometheus-stack: the exporter by its address, no root cgroup", func(t *testing.T) {
		c := newPromCluster(t, false)
		c.prom.on(`node_cpu_seconds_total{instance=~"(n1|10\\.0\\.0\\.7)(:[0-9]+)?"`, s(nil, 1000, 1.5, 1030, 1.75))
		c.prom.on(`node_memory_MemTotal_bytes{instance=~"(n1|10\\.0\\.0\\.7)(:[0-9]+)?"`, s(nil, 1000, 3<<30, 1030, 3<<30))
		prov := promProvider(t, c, metrics.PrometheusOptions{})

		// Without the address the exporter is not found, nor the dropped root cgroup: no history,
		// and the answer says so.
		got, err := prov.ObjectRange(ctx, nodeQuery("n1"))
		if err != nil || got.UsageSource != metrics.UsageNone || got.Targets[""] != nil {
			t.Fatalf("by name alone: %+v %v", got, err)
		}
		// With it, an hour of node-exporter's points.
		before := c.proxyCount()
		got, err = prov.ObjectRange(ctx, nodeQuery("n1", "10.0.0.7"))
		if err != nil {
			t.Fatal(err)
		}
		u := got.Targets[""].Usage
		if got.UsageSource != metrics.UsageNodeExporter || len(u) != 2 || u[1].CPUMilli != 1750 || u[1].MemoryBytes != 3<<30 {
			t.Errorf("source %q, usage %+v", got.UsageSource, u)
		}
		if n := c.proxyCount() - before; n != 2 {
			t.Errorf("%d requests, want 2: node-exporter's CPU and memory, cAdvisor not asked once they answered", n)
		}
	})
	t.Run("the prometheus chart with node-exporter: the node label, preferred to the root cgroup", func(t *testing.T) {
		c := newPromCluster(t, false)
		c.prom.on(`node_cpu_seconds_total{node="n1"`, s(nil, 1000, 0.25))
		c.prom.on(`node_memory_MemTotal_bytes{node="n1"}`, s(nil, 1000, 1<<30))
		c.prom.on(`container_cpu_usage_seconds_total{id="/"`, s(nil, 1000, 9))
		c.prom.on(`container_memory_working_set_bytes{id="/"`, s(nil, 1000, 9<<30))
		prov := promProvider(t, c, metrics.PrometheusOptions{})
		got, err := prov.ObjectRange(ctx, nodeQuery("n1"))
		if err != nil || got.UsageSource != metrics.UsageNodeExporter || got.Targets[""].Usage[0].CPUMilli != 250 {
			t.Fatalf("%+v %v", got, err)
		}
		if c.prom.queryCount("container_cpu_usage_seconds_total") != 0 {
			t.Error("the root cgroup was asked although node-exporter answered")
		}
		// A node without an exporter (its DaemonSet does not run there) falls back to its root cgroup.
		got, err = prov.ObjectRange(ctx, nodeQuery("n2"))
		if err != nil || got.UsageSource != metrics.UsageCAdvisor || got.Targets[""].Usage[0].CPUMilli != 9000 {
			t.Errorf("a node without an exporter: %+v %v", got, err)
		}
	})
	t.Run("the prometheus chart without node-exporter: cAdvisor, remembered", func(t *testing.T) {
		c := newPromCluster(t, false)
		c.prom.on(`container_cpu_usage_seconds_total{id="/"`, s(nil, 1000, 2))
		c.prom.on(`container_memory_working_set_bytes{id="/"`, s(nil, 1000, 2<<30))
		prov := promProvider(t, c, metrics.PrometheusOptions{})
		before := c.proxyCount()
		got, err := prov.ObjectRange(ctx, nodeQuery("n1"))
		if err != nil || got.UsageSource != metrics.UsageCAdvisor || got.Targets[""].Usage[0].MemoryBytes != 2<<30 {
			t.Fatalf("%+v %v", got, err)
		}
		if n := c.proxyCount() - before; n != 4 {
			t.Errorf("%d requests, want 4: node-exporter tried once, then the root cgroup", n)
		}
		before = c.proxyCount()
		if _, err := prov.ObjectRange(ctx, nodeQuery("n2")); err != nil {
			t.Fatal(err)
		}
		if n := c.proxyCount() - before; n != 2 || c.prom.queryCount("node_cpu_seconds_total") != 1 {
			t.Errorf("%d requests for the next node, want 2: this Prometheus's nodes come from cAdvisor", n)
		}
	})
	t.Run("neither: none, and node-exporter is asked again next time", func(t *testing.T) {
		c := newPromCluster(t, false)
		prov := promProvider(t, c, metrics.PrometheusOptions{})
		for range 2 {
			if got, err := prov.ObjectRange(ctx, nodeQuery("n1")); err != nil || got.UsageSource != metrics.UsageNone || len(got.Targets) != 0 {
				t.Fatalf("%+v %v", got, err)
			}
		}
		if n := c.prom.queryCount("node_cpu_seconds_total"); n != 2 {
			t.Errorf("node-exporter asked %d times, want each time while nothing was found", n)
		}
	})
	t.Run("a pod's source", func(t *testing.T) {
		c := newPromCluster(t, false)
		podData(c.prom)
		prov := promProvider(t, c, metrics.PrometheusOptions{})
		q := podQuery("web", "a")
		q.Want = metrics.WantUsage
		if got, err := prov.ObjectRange(ctx, q); err != nil || got.UsageSource != metrics.UsageCAdvisor {
			t.Errorf("%+v %v", got, err)
		}
		q.Name = "gone"
		c.prom.mu.Lock()
		c.prom.rules = nil
		c.prom.mu.Unlock()
		if got, err := prov.ObjectRange(ctx, q); err != nil || got.UsageSource != metrics.UsageNone {
			t.Errorf("a pod without series: %+v %v", got, err)
		}
	})
}

// The filesystem metric is found per kind: on containerd the kubelet's cAdvisor has
// container_fs_usage_bytes for the node's root cgroup but not for containers, so a node's drawer
// opened first must not leave the pods' Filesystem charts empty (seen on kind with the prometheus
// chart, 2026-10-12). The node's usage counts its block devices only, not the overlays that
// repeat them.
func TestFilesystemModeIsPerKind(t *testing.T) {
	c := newPromCluster(t, false)
	podData(c.prom)
	c.prom.on(`container_fs_usage_bytes{id="/",device=~"/dev/.+",device!="/dev/shm",node="n1"}`, s(nil, 1000, 60e9, 1030, 61e9))
	c.prom.on("container_fs_reads_bytes_total{namespace", s(lc("app"), 1000, 10, 1030, 20))
	c.prom.on("container_fs_writes_bytes_total{namespace", s(lc("app"), 1000, 1, 1030, 2))
	prov := promProvider(t, c, metrics.PrometheusOptions{})
	nodeQ := podQuery("", "n1")
	nodeQ.Kind = metrics.KindNodes

	node, err := prov.ObjectRange(context.Background(), nodeQ)
	if err != nil {
		t.Fatal(err)
	}
	if f := node.Targets[""].Filesystem; node.FilesystemSource != "usage" || len(f) != 2 || f[1].UsageBytes != 61e9 {
		t.Errorf("node filesystem %q: %+v", node.FilesystemSource, f)
	}
	pod, err := prov.ObjectRange(context.Background(), podQuery("web", "a"))
	if err != nil {
		t.Fatal(err)
	}
	if f := pod.Targets["app"].Filesystem; pod.FilesystemSource != "io" || len(f) != 2 || f[1].ReadBytesPerSec != 20 {
		t.Errorf("after a node with usage, the pod's filesystem %q: %+v", pod.FilesystemSource, f)
	}
	p, ok := prov.(*metrics.Prometheus)
	if !ok {
		t.Fatalf("%T", prov)
	}
	if p.FilesystemSource(metrics.KindNodes) != "usage" || p.FilesystemSource(metrics.KindPods) != "io" {
		t.Errorf("remembered: nodes %q, pods %q", p.FilesystemSource(metrics.KindNodes), p.FilesystemSource(metrics.KindPods))
	}
	// Once known, a pod's poll asks for its rates only: 6 queries, no usage.
	usage := c.prom.queryCount("container_fs_usage_bytes{namespace")
	before := c.proxyCount()
	if _, err := prov.ObjectRange(context.Background(), podQuery("web", "a")); err != nil {
		t.Fatal(err)
	}
	if n := c.proxyCount() - before; n != 6 || c.prom.queryCount("container_fs_usage_bytes{namespace") != usage {
		t.Errorf("%d requests for the pod once its metric is known, want 6 without usage", n)
	}
	for _, r := range c.prom.requests() {
		if strings.Contains(r.query, "container_fs_usage_bytes{id") && !strings.Contains(r.query, `device!="/dev/shm"`) {
			t.Errorf("a node's usage counts every mount: %s", r.query)
		}
	}
}

func TestPodNamesAreQuotedInQueriesOnTheWire(t *testing.T) {
	c := newPromCluster(t, false)
	podData(c.prom)
	prov := promProvider(t, c, metrics.PrometheusOptions{})
	if _, err := prov.ObjectRange(context.Background(), podQuery("web", `a"} or vector(1) #`)); err != nil {
		t.Fatal(err)
	}
	for _, r := range c.prom.requests() {
		if r.query != "" && !strings.Contains(r.query, `pod="a\"} or vector(1) #"`) {
			t.Errorf("unquoted pod name in %s", r.query)
		}
	}
}

func TestPrometheusFailuresAreStates(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		want   metrics.Status
	}{
		"forbidden":   {403, metrics.StatusForbidden},
		"gone":        {404, metrics.StatusNotServed},
		"unavailable": {503, metrics.StatusUnavailable},
		"server":      {500, metrics.StatusUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			c := newPromCluster(t, false)
			podData(c.prom)
			prov := promProvider(t, c, metrics.PrometheusOptions{})
			c.prom.mu.Lock()
			c.prom.status = tc.status
			c.prom.mu.Unlock()
			_, err := prov.ObjectRange(context.Background(), podQuery("web", "a"))
			var se *metrics.StateError
			if !errors.As(err, &se) || se.State.Status != tc.want || se.State.Message == "" {
				t.Errorf("err = %v, want state %s", err, tc.want)
			}
		})
	}
}

func TestResponseIsBounded(t *testing.T) {
	c := newPromCluster(t, false)
	podData(c.prom)
	prov := promProvider(t, c, metrics.PrometheusOptions{MaxResponseBytes: 64})
	// buildinfo is ~70 bytes against a probe limit of its own; the queries answer more than 64.
	_, err := prov.ObjectRange(context.Background(), podQuery("web", "a"))
	var se *metrics.StateError
	if !errors.As(err, &se) || se.State.Status != metrics.StatusUnavailable || !strings.Contains(err.Error(), "larger than 64 B") {
		t.Errorf("err = %v", err)
	}
}

func TestInstantQueriesForLists(t *testing.T) {
	c := newPromCluster(t, false)
	c.prom.on("container_cpu_usage_seconds_total{container", s(map[string]string{"namespace": "web", "pod": "a", "container": "app"}, 1000, 0.25), s(map[string]string{"namespace": "web", "pod": "b", "container": "app"}, 1000, 1.5))
	c.prom.on("container_memory_working_set_bytes{container", s(map[string]string{"namespace": "web", "pod": "a", "container": "app"}, 1000, 64*mib))
	prov := promProvider(t, c, metrics.PrometheusOptions{})
	before := c.proxyCount()
	pods, err := prov.ListPods(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if n := c.proxyCount() - before; n != 2 {
		t.Errorf("%d requests for a list, want 2 (cpu, memory)", n)
	}
	if len(pods) != 2 || pods[0].Name != "a" || pods[0].Total().CPUMilli != 250 || pods[0].Total().MemoryBytes != 64*mib || pods[1].Total().CPUMilli != 1500 {
		t.Errorf("pods = %+v", pods)
	}
	if _, err := prov.GetPod(context.Background(), "web", "nosuch"); !errors.Is(err, metrics.ErrNoData) {
		t.Errorf("GetPod of a pod without series: %v", err)
	}

	c.prom.on(`sum by (node, instance) (rate(container_cpu`, s(map[string]string{"node": "n1", "instance": "10.0.0.1:10250"}, 1000, 2), s(map[string]string{"instance": "n2:10250"}, 1000, 0.5))
	c.prom.on(`sum by (node, instance) (container_memory`, s(map[string]string{"node": "n1"}, 1000, 1<<30))
	nodes, err := prov.ListNodes(context.Background())
	if err != nil || len(nodes) != 2 || nodes[0].Name != "n1" || nodes[0].CPUMilli != 2000 || nodes[0].MemoryBytes != 1<<30 || nodes[1].Name != "n2" || nodes[1].CPUMilli != 500 {
		t.Errorf("nodes = %+v, %v", nodes, err)
	}
}
