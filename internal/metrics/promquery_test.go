package metrics_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

// promProviderWith detects the fake Prometheus with a cluster's query settings.
func promProviderWith(t *testing.T, c *promCluster, q metrics.QuerySettings) metrics.RangeProvider {
	t.Helper()
	return promProvider(t, c, metrics.PrometheusOptions{Query: q})
}

// A query format that fixes where a node's series come from asks that source alone (G12,
// Cluster Settings › Metrics, 2026-10-16): the Prometheus Operator's never the root cgroup its
// kubelet ServiceMonitor drops, cAdvisor-only never node-exporter.
func TestQueryFormatFixesTheNodeSource(t *testing.T) {
	ctx := context.Background()
	t.Run("Prometheus Operator: node-exporter alone", func(t *testing.T) {
		c := newPromCluster(t, false)
		prov := promProviderWith(t, c, metrics.QuerySettings{Format: metrics.QueryOperator})
		q := nodeQuery("n1", "10.0.0.7")
		q.Want = metrics.WantAll
		before := c.proxyCount()
		got, err := prov.ObjectRange(ctx, q)
		if err != nil || got.UsageSource != metrics.UsageNone {
			t.Fatalf("%+v %v", got, err)
		}
		// CPU and memory (2), received and transmitted (2), the usage, then the disks' rates (3):
		// node-exporter's alone, the root cgroup never asked, not even when nothing answered.
		if n := c.proxyCount() - before; n != 7 || c.prom.queryCount(`id="/"`) != 0 {
			t.Errorf("%d requests, %d of the root cgroup; want 7 and none", n, c.prom.queryCount(`id="/"`))
		}
	})
	t.Run("cAdvisor only: the root cgroup alone", func(t *testing.T) {
		c := newPromCluster(t, false)
		c.prom.on(`container_cpu_usage_seconds_total{id="/"`, s(nil, 1000, 2))
		c.prom.on(`container_memory_working_set_bytes{id="/"`, s(nil, 1000, 2<<30))
		prov := promProviderWith(t, c, metrics.QuerySettings{Format: metrics.QueryCAdvisor})
		q := nodeQuery("n1")
		q.Want = metrics.WantAll
		got, err := prov.ObjectRange(ctx, q)
		if err != nil || got.UsageSource != metrics.UsageCAdvisor || got.Targets[""].Usage[0].CPUMilli != 2000 {
			t.Fatalf("%+v %v", got, err)
		}
		if n := c.prom.queryCount("node_"); n != 0 {
			t.Errorf("%d queries of node-exporter, want none", n)
		}
	})
	t.Run("auto: both, node-exporter first", func(t *testing.T) {
		c := newPromCluster(t, false)
		prov := promProviderWith(t, c, metrics.QuerySettings{})
		q := nodeQuery("n1")
		q.Want = metrics.WantNetwork
		if _, err := prov.ObjectRange(ctx, q); err != nil {
			t.Fatal(err)
		}
		reqs := c.prom.requests()
		if len(reqs) < 2 || !strings.Contains(reqs[len(reqs)-1].query, "node_network_") || !strings.Contains(reqs[len(reqs)-1].query, `container_network_`) {
			t.Errorf("the network query of auto: %+v", reqs)
		}
	})
	for _, tc := range []struct{ in, name string }{{"", "auto"}, {"auto", "auto"}, {"Prometheus-Operator", "prometheus-operator"}, {"cadvisor", "cadvisor"}} {
		f, err := metrics.ParseQueryFormat(tc.in)
		if err != nil || f.Name() != tc.name {
			t.Errorf("ParseQueryFormat(%q) = %q %v, want %s", tc.in, f.Name(), err, tc.name)
		}
	}
	if _, err := metrics.ParseQueryFormat("helm-14"); err == nil {
		t.Error("an unknown format was taken")
	}
}

// The empty-container filter (Lens's checkbox, on by default) keeps the pod's own cgroup and the
// series without a container name out of the queries; off, they stay: a pod's line is its own
// cgroup's where the answer has one, and a Prometheus whose cAdvisor series carry no container name
// still shows the pod, in the charts and in the lists.
func TestEmptyContainerFilter(t *testing.T) {
	ctx := context.Background()
	t.Run("on: real containers only, the pod their sum", func(t *testing.T) {
		c := newPromCluster(t, false)
		podData(c.prom)
		prov := promProviderWith(t, c, metrics.QuerySettings{})
		q := podQuery("web", "a")
		q.Want = metrics.WantUsage
		got, err := prov.ObjectRange(ctx, q)
		if err != nil || got.Targets[""].Usage[1].CPUMilli != 520 {
			t.Fatalf("%+v %v", got, err)
		}
		for _, r := range c.prom.requests() {
			if strings.Contains(r.query, "container_cpu") && !strings.Contains(r.query, `container!="",container!="POD"`) {
				t.Errorf("the filter is missing: %s", r.query)
			}
		}
	})
	t.Run("off: the pod's own cgroup is its line", func(t *testing.T) {
		c := newPromCluster(t, false)
		c.prom.on("container_cpu_usage_seconds_total", s(lc(""), 1000, 0.3, 1030, 0.6), s(lc("app"), 1000, 0.25, 1030, 0.5))
		c.prom.on("container_memory_working_set_bytes", s(lc(""), 1000, 120*mib, 1030, 130*mib), s(lc("app"), 1000, 100*mib, 1030, 110*mib))
		prov := promProviderWith(t, c, metrics.QuerySettings{KeepEmptyContainers: true})
		q := podQuery("web", "a")
		q.Want = metrics.WantUsage
		got, err := prov.ObjectRange(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if w, a := got.Targets[""].Usage[1], got.Targets["app"].Usage[1]; w.CPUMilli != 600 || w.MemoryBytes != 130*mib || a.CPUMilli != 500 {
			t.Errorf("the pod %+v, its container %+v", w, a)
		}
		for _, r := range c.prom.requests() {
			if strings.Contains(r.query, "container_cpu") && (strings.Contains(r.query, `container!=""`) || !strings.Contains(r.query, `container!="POD"`)) {
				t.Errorf("the filter is on: %s", r.query)
			}
		}
	})
	t.Run("off, the lists: a pod's own cgroup counts only where it has no named container", func(t *testing.T) {
		c := newPromCluster(t, false)
		c.prom.on("container_cpu_usage_seconds_total", s(map[string]string{"namespace": "web", "pod": "named", "container": ""}, 1000, 0.3),
			s(map[string]string{"namespace": "web", "pod": "named", "container": "app"}, 1000, 0.25),
			s(map[string]string{"namespace": "web", "pod": "bare", "container": ""}, 1000, 0.1))
		c.prom.on("container_memory_working_set_bytes", s(map[string]string{"namespace": "web", "pod": "named", "container": ""}, 1000, 120*mib),
			s(map[string]string{"namespace": "web", "pod": "named", "container": "app"}, 1000, 100*mib),
			s(map[string]string{"namespace": "web", "pod": "bare", "container": ""}, 1000, 10*mib))
		prov := promProviderWith(t, c, metrics.QuerySettings{KeepEmptyContainers: true})
		pods, err := prov.ListPods(ctx, "web")
		if err != nil || len(pods) != 2 {
			t.Fatalf("%+v %v", pods, err)
		}
		bare, named := pods[0], pods[1]
		if len(named.Containers) != 1 || named.Containers[0].Name != "app" || named.Containers[0].CPUMilli != 250 {
			t.Errorf("a pod with a named container: %+v", named)
		}
		if len(bare.Containers) != 1 || bare.Containers[0].CPUMilli != 100 || bare.Containers[0].MemoryBytes != 10*mib {
			t.Errorf("a pod whose series carry no container name: %+v", bare)
		}
	})
}

// HTTPS asks the service over TLS through the service proxy (https:<service>:<port>), the probe
// and every query, and the status line says so.
func TestPrometheusOverHTTPS(t *testing.T) {
	c := newPromCluster(t, false)
	podData(c.prom)
	c.srv.SetProxy("monitoring", "https:prometheus-server:80", c.prom)
	det := metrics.NewPrometheusDetector(c.conn.Kube(), servicesOf(web("monitoring", "prometheus-server", 80)), metrics.PrometheusOptions{})
	det.SetQuery(metrics.QuerySettings{HTTPS: true})
	prov, st := det.Detect(context.Background())
	if prov == nil {
		t.Fatalf("no provider over https: %+v", st)
	}
	p := prov.(*metrics.Prometheus)
	if p.Where() != "monitoring/prometheus-server:80 (https)" || !p.Query().HTTPS || det.Query() != (metrics.QuerySettings{HTTPS: true}) {
		t.Errorf("where %q, settings %+v", p.Where(), p.Query())
	}
	if _, err := p.ObjectRange(context.Background(), podQuery("web", "a")); err != nil {
		t.Errorf("a range query over https: %v", err)
	}
	// Plain http finds nothing behind the https name: the scheme is part of what is asked.
	det.SetQuery(metrics.QuerySettings{})
	if prov, _ := det.Detect(context.Background()); prov != nil {
		t.Errorf("found over http: %s", prov.(*metrics.Prometheus).Where())
	}
}

// The prometheus chart's server is found under any release's name, and none ranks first for being
// the release Cogpit Metrics installed in v0.16.0 and v0.17.0 (cogpit-metrics/cogpit-prometheus-server):
// Cogpit installs nothing of its own (D-61, owner 2026-10-16), detection ranks what the cluster runs.
func TestPrometheusChartServerOfAnyRelease(t *testing.T) {
	cands := metrics.Candidates([]metrics.Service{
		web("cogpit-metrics", "cogpit-prometheus-server", 80),
		web("monitoring", "kube-prometheus-stack-prometheus", 9090),
		web("team-a", "monitoring-prometheus-server", 80),
		web("monitoring", "myprometheus-server", 80),
		web("monitoring", "prometheus-server-exporter", 80), // not the chart's server
	})
	var got []string
	for _, c := range cands {
		got = append(got, fmt.Sprintf("%s=%d", c.Target, c.Score))
	}
	want := []string{"monitoring/kube-prometheus-stack-prometheus=100", "monitoring/myprometheus-server=80",
		"cogpit-metrics/cogpit-prometheus-server=80", "team-a/monitoring-prometheus-server=80"}
	if !slices.Equal(got, want) {
		t.Errorf("candidates\n got %v\nwant %v", got, want)
	}
}
