//go:build kind

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The sources the kind cluster's charts name: Prometheus for the history, the Metrics Server for
// the live values (both together, the normal case: G12, owner 2026-10-12, evening).
const (
	kindPromSource = "Prometheus (monitoring/prometheus-server)"
	kindBothSource = kindPromSource + " and Kubernetes Metrics Server"
)

// kindMetricsSession connects the engine to the kind cluster, or skips without it or without its
// Prometheus.
func kindMetricsSession(ctx context.Context, t *testing.T, opts metrics.Options) (*engine.Session, func(id string, params any) any) {
	t.Helper()
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{kubeconfig}, MetricsOptions: opts}})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, contextName); err != nil {
		t.Fatal(err)
	}
	svc := model.ObjectRef{Kind: "services", Namespace: "monitoring", Name: "prometheus-server"}
	if _, err := s.Object(ctx, svc); errors.Is(err, engine.ErrNotFound) {
		t.Skip("the kind cluster has no Prometheus: run make kind-prometheus")
	} else if err != nil {
		t.Fatal(err)
	}
	dispatch := func(id string, params any) any {
		t.Helper()
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: raw, Origin: engine.OriginExternalAgent})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return res
	}
	return s, dispatch
}

// TestKindPrometheus checks the drawer's charts against a real Prometheus (the prometheus-community
// `prometheus` chart that `make kind-prometheus` installs as monitoring/prometheus-server, scraping
// the kubelets' cAdvisor, node-exporter and kube-state-metrics every minute) and the kind cluster's
// metrics-server, both together: detection finds Prometheus among the cached Services when a
// drawer opens; a node's drawer (from node-exporter, A43) and then a running pod's drawer (from
// cAdvisor) get their history from Prometheus once, reaching back before the drawer opened (F1),
// and their live values from the Metrics Server; the network and the filesystem come when a chart
// shows them; a chart's range
// changes through ui.setMetricsRange; metrics.get answers from the Metrics Server; the status line
// says who serves what. The node comes first on purpose: on containerd the kubelet's cAdvisor has
// container_fs_usage_bytes for the node's root cgroup but not for containers, so the filesystem
// metric must be found per kind. It only reads. Skipped when the cluster has no Prometheus.
func TestKindPrometheus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	s, dispatch := kindMetricsSession(ctx, t, metrics.Options{})
	proxy := func() int64 { return s.Diagnostics().Requests.ByVerbResource["get services/proxy"] }

	// The Pods list alone: the Metrics Server's list, nothing of Prometheus.
	if !testkit.WaitFor(ctx, func() bool { return s.Diagnostics().Requests.ByVerbResource["list pods.metrics.k8s.io"] > 0 }) {
		t.Fatal("the Pods list never listed the Metrics Server")
	}
	time.Sleep(2 * time.Second)
	if n := proxy(); n != 0 {
		t.Errorf("the Pods list sent %d requests to Prometheus, want none while the Metrics Server answers", n)
	}

	// A node's drawer: CPU and memory from both, then the network and the filesystem as the chart
	// shows them.
	dispatch("ui.openView", map[string]any{"view": "nodes"})
	var node model.ObjectRef
	if !testkit.WaitFor(ctx, func() bool {
		tbl := s.Table("nodes")
		if tbl == nil || !tbl.Synced || len(tbl.Rows) == 0 {
			return false
		}
		node = tbl.Rows[0].Ref
		return true
	}) {
		t.Fatal("no node")
	}
	nodeOpened := time.Now()
	dispatch("ui.navigate", map[string]any{"ref": node})
	m := waitHistory(ctx, t, s, node, func(m *engine.ObjectMetrics) bool {
		return len(m.Targets[0].Samples) > 0 && m.Source == kindBothSource && m.HistorySource != ""
	})
	if !m.Capabilities.History || !m.Capabilities.Network || !m.Capabilities.Filesystem {
		t.Errorf("node: capabilities %+v", m.Capabilities)
	}
	// The node's past comes from node-exporter (A43, A115, F1), from before the drawer opened.
	if m.HistorySource != metrics.UsageNodeExporter {
		t.Errorf("node: history from %q, want node-exporter (make kind-prometheus again: its values enable node-exporter since A115)", m.HistorySource)
	}
	checkPast(t, "node", m.Targets[0].Samples, nodeOpened)
	logProviders(t, "node", m.Providers)
	dispatch("ui.setMetricsVariant", map[string]any{"ref": node, "variant": "network"})
	m = waitHistory(ctx, t, s, node, func(m *engine.ObjectMetrics) bool { return len(m.Targets[0].Network) > 0 })
	net := m.Targets[0].Network
	dispatch("ui.setMetricsVariant", map[string]any{"ref": node, "variant": "filesystem"})
	m = waitHistory(ctx, t, s, node, func(m *engine.ObjectMetrics) bool { return len(m.Targets[0].Filesystem) > 0 })
	m.Targets[0].Network = net
	logTarget(t, "node "+node.Name, m.FilesystemSource, m.Targets[0])

	// A running pod's drawer: the seed's nginx. Its chart shows the network, its container's the
	// filesystem.
	dispatch("ui.openView", map[string]any{"view": "pods"})
	var pod model.ObjectRef
	if !testkit.WaitFor(ctx, func() bool {
		tbl := s.Table("pods")
		if tbl == nil || !tbl.Synced {
			return false
		}
		for _, r := range tbl.Rows {
			if r.Ref.Namespace == "cogpit-demo" && strings.HasPrefix(r.Ref.Name, "web-") && r.Status == model.StatusOK {
				pod = r.Ref
				return true
			}
		}
		return false
	}) {
		t.Fatal("no running web pod in cogpit-demo (make kind-seed)")
	}
	podOpened := time.Now()
	dispatch("ui.navigate", map[string]any{"ref": pod})
	m = waitHistory(ctx, t, s, pod, func(m *engine.ObjectMetrics) bool {
		return len(m.Targets) >= 2 && len(m.Targets[0].Samples) > 0 && len(m.Targets[1].Samples) > 0 && m.Source == kindBothSource
	})
	if m.HistorySource != metrics.UsageCAdvisor {
		t.Errorf("pod: history from %q, want cadvisor", m.HistorySource)
	}
	checkPast(t, "pod", m.Targets[0].Samples, podOpened)
	ctrName := m.Targets[1].Name
	dispatch("ui.setMetricsVariant", map[string]any{"ref": pod, "variant": "network"})
	dispatch("ui.setMetricsVariant", map[string]any{"ref": pod, "container": ctrName, "variant": "filesystem"})
	m = waitHistory(ctx, t, s, pod, func(m *engine.ObjectMetrics) bool {
		w, c := m.Targets[0], m.Targets[1]
		return len(w.Network) > 0 && len(c.Network) > 0 && len(c.Filesystem) > 0
	})
	whole, ctr := m.Targets[0], m.Targets[1]
	if whole.Current == nil || whole.Current.MemoryBytes <= 0 || !ctr.NetworkOfPod || whole.NetworkOfPod {
		t.Errorf("pod: current %+v, network of pod %v/%v", whole.Current, whole.NetworkOfPod, ctr.NetworkOfPod)
	}
	if m.FilesystemSource != metrics.FilesystemUsage && m.FilesystemSource != metrics.FilesystemIO {
		t.Errorf("pod filesystem source %q", m.FilesystemSource)
	}
	whole.Filesystem = ctr.Filesystem
	logTarget(t, "pod "+pod.Name, m.FilesystemSource, whole)
	logTarget(t, "container "+ctr.Name, m.FilesystemSource, ctr)
	// The seam: one point per time, oldest first, and no hole longer than a few minutes between
	// Prometheus's history and the Metrics Server's measurements.
	checkSeam(t, "pod", whole.Samples)

	// The status line, also in ui.state.
	st := dispatch("ui.state", struct{}{}).(commands.StateResult)
	if len(st.DrawerMetricsProviders) != 2 {
		t.Fatalf("ui.state's providers = %+v", st.DrawerMetricsProviders)
	}
	prom, ms := st.DrawerMetricsProviders[0], st.DrawerMetricsProviders[1]
	if prom.Provider != "prometheus" || prom.Status != metrics.StatusAvailable || prom.Service != "monitoring/prometheus-server:80" ||
		!slices.Contains(prom.Serves, metrics.ServesHistory) || slices.Contains(prom.Serves, metrics.ServesLive) {
		t.Errorf("Prometheus's status = %+v", prom)
	}
	if ms.Provider != "metrics-server" || ms.Status != metrics.StatusAvailable || !slices.Equal(ms.Serves, []string{metrics.ServesLive}) {
		t.Errorf("the Metrics Server's status = %+v", ms)
	}
	logProviders(t, "pod", st.DrawerMetricsProviders)

	// The pod's chart over six hours: the engine holds the range, ui.state shows it, the history
	// is asked for once more.
	dispatch("ui.setMetricsRange", map[string]any{"ref": pod, "range": "6h"})
	m = waitHistory(ctx, t, s, pod, func(m *engine.ObjectMetrics) bool {
		return m.Targets[0].Range == "6h" && len(m.Targets[0].Samples) > 0
	})
	st = dispatch("ui.state", struct{}{}).(commands.StateResult)
	if len(st.DrawerCharts) < 2 || st.DrawerCharts[0].Range != "6h" || st.DrawerCharts[1].Range != "1h" || st.DrawerCharts[0].Variant != "network" || st.DrawerCharts[1].Variant != "filesystem" {
		t.Errorf("ui.state's drawer charts = %+v", st.DrawerCharts)
	}
	t.Logf("pod chart at 6h: %d samples from %s", len(m.Targets[0].Samples), m.Targets[0].Samples[0].At.Format(time.RFC3339))

	// metrics.get reads the current values from the Metrics Server.
	pods := dispatch("metrics.get", map[string]any{"kind": "pods", "namespace": "cogpit-demo"}).(*engine.MetricsResult)
	if pods.Status != metrics.StatusAvailable || pods.Source != metrics.ServerLabel || len(pods.Pods) == 0 || len(pods.Providers) != 2 {
		t.Fatalf("metrics.get pods: %+v", pods)
	}
	for _, p := range pods.Pods {
		if p.MemoryBytes <= 0 {
			t.Errorf("metrics.get: %s has no memory", p.Name)
		}
		t.Logf("metrics.get %s: cpu %dm memory %s", p.Name, p.CPUMilli, model.FormatBytes(p.MemoryBytes))
	}
	nodes := dispatch("metrics.get", map[string]any{"kind": "nodes"}).(*engine.MetricsResult)
	if nodes.Source != metrics.ServerLabel || len(nodes.Nodes) == 0 || nodes.Nodes[0].MemoryBytes <= 0 {
		t.Errorf("metrics.get nodes: %+v", nodes)
	} else {
		t.Logf("metrics.get node %s: cpu %dm memory %s", nodes.Nodes[0].Name, nodes.Nodes[0].CPUMilli, model.FormatBytes(nodes.Nodes[0].MemoryBytes))
	}

	// Budgets P12 and P13 against the real providers, 90 seconds each with the Pods list and the
	// pod's drawer at 1h open, all GETs. With CPU and Memory shown: no Prometheus request at all
	// (its history came when the range was set), a Metrics Server get every 15 s and a list a
	// minute. With the network shown: two range queries per 30 s step.
	dispatch("ui.setMetricsRange", map[string]any{"ref": pod, "range": "default"})
	dispatch("ui.setMetricsVariant", map[string]any{"ref": pod, "variant": "cpu"})
	dispatch("ui.setMetricsVariant", map[string]any{"ref": pod, "container": ctrName, "variant": "memory"})
	time.Sleep(3 * time.Second)
	measure := func(what string) (proxyN, gets, lists int64) {
		reqs := func() map[string]int64 { return s.Diagnostics().Requests.ByVerbResource }
		p0, g0, l0, w0 := proxy(), reqs()["get pods.metrics.k8s.io"], reqs()["list pods.metrics.k8s.io"], writes(s)
		time.Sleep(90 * time.Second)
		proxyN, gets, lists = proxy()-p0, reqs()["get pods.metrics.k8s.io"]-g0, reqs()["list pods.metrics.k8s.io"]-l0
		if w := writes(s) - w0; w != 0 {
			t.Errorf("%s: %d writes", what, w)
		}
		t.Logf("budget on kind, %s: %d Prometheus requests, %d Metrics Server gets, %d lists in 90 s", what, proxyN, gets, lists)
		return proxyN, gets, lists
	}
	if p, g, l := measure("CPU and Memory shown"); p != 0 || g < 5 || g > 7 || l < 1 || l > 2 {
		t.Errorf("CPU and Memory shown: %d Prometheus requests, %d gets, %d lists in 90 s; want 0, 6 (±1), 1-2", p, g, l)
	}
	dispatch("ui.setMetricsVariant", map[string]any{"ref": pod, "variant": "network"})
	if p, _, _ := measure("the network shown"); p < 4 || p > 10 {
		t.Errorf("the network shown: %d Prometheus requests in 90 s, want 2 at once and 2 per 30 s step", p)
	}
}

// TestKindNodeExporterByAddress checks A43's matching against a real node-exporter (A115: the
// `prometheus` chart's, installed by make kind-prometheus): its series carry the node label, and
// the exporter's host-network address as the instance, which is all kube-prometheus-stack's carry.
// Asked by a name no series has and the node's InternalIP, the node's history is found by the
// instance alone; without the address it is not. It also logs node-exporter's newest point beside
// the Metrics Server's measurement of the node: on kind they differ (the exporter reads the Docker
// VM's /proc, the kubelet the kind node's cgroup), on a real node they measure the same machine.
// It only reads.
func TestKindNodeExporterByAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	kubeconfig, contextName := os.Getenv("COGPIT_KIND_KUBECONFIG"), os.Getenv("COGPIT_KIND_CONTEXT")
	if kubeconfig == "" || contextName == "" {
		t.Skip("COGPIT_KIND_KUBECONFIG and COGPIT_KIND_CONTEXT are not set; run make test-kind")
	}
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	kube := conn.Kube()
	if _, err := kube.CoreV1().Services("monitoring").Get(ctx, "prometheus-server", metav1.GetOptions{}); err != nil {
		t.Skipf("the kind cluster has no Prometheus (make kind-prometheus): %v", err)
	}
	nodes, err := kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil || len(nodes.Items) == 0 {
		t.Fatalf("nodes: %v", err)
	}
	node := nodes.Items[0]
	var ip string
	for _, a := range node.Status.Addresses {
		if a.Type == "InternalIP" {
			ip = a.Address
		}
	}
	prom := metrics.NewPrometheus(kube, metrics.Target{Namespace: "monitoring", Service: "prometheus-server", Port: "http"}, metrics.PrometheusOptions{})
	to := time.Now()
	q := metrics.RangeQuery{Kind: metrics.KindNodes, Name: "cogpit-no-such-node", From: to.Add(-30 * time.Minute), To: to, Step: time.Minute, Want: metrics.WantUsage}
	if got, err := prom.ObjectRange(ctx, q); err != nil || got.UsageSource != metrics.UsageNone {
		t.Fatalf("a name no series has, without an address: %+v, %v", got, err)
	}
	q.Addresses = []string{ip}
	got, err := prom.ObjectRange(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsageSource != metrics.UsageNodeExporter || got.Targets[""] == nil || len(got.Targets[""].Usage) == 0 {
		t.Fatalf("by the address %s: source %q, %+v (make kind-prometheus again: its values enable node-exporter since A115)", ip, got.UsageSource, got.Targets)
	}
	pts := got.Targets[""].Usage
	last := pts[len(pts)-1]
	line := "node-exporter by " + ip + ": " + itoa(len(pts)) + " points from " + pts[0].At.Format(time.TimeOnly) +
		", newest cpu " + itoa(int(last.CPUMilli)) + "m, memory " + model.FormatBytes(last.MemoryBytes)
	if ms, err := metrics.ForConn(conn); err == nil {
		if nu, err := ms.GetNode(ctx, node.Name); err == nil {
			line += "; the Metrics Server: cpu " + itoa(int(nu.CPUMilli)) + "m, memory " + model.FormatBytes(nu.MemoryBytes)
		}
	}
	t.Log(line)
}

// checkPast fails unless a chart reaches back before its drawer opened (F1: the past from
// Prometheus, not only what Cogpit measured since), and logs how far.
func checkPast(t *testing.T, what string, pts []model.UsageSample, opened time.Time) {
	t.Helper()
	past := 0
	for _, p := range pts {
		if p.At.Before(opened.Add(-time.Minute)) {
			past++
		}
	}
	if past == 0 {
		t.Errorf("%s: no point from before the drawer opened at %s (%d points)", what, opened.Format(time.TimeOnly), len(pts))
		return
	}
	t.Logf("%s: %d of %d points from before the drawer opened, the first %s", what, past, len(pts), pts[0].At.Format(time.TimeOnly))
}

// checkSeam fails on a point twice or out of order and logs the longest hole of a chart's series.
// Only the last 15 minutes count for holes: that is where the seam lies, while Prometheus's own
// history may have holes of its own (the kind Prometheus was down while Docker restarted).
func checkSeam(t *testing.T, what string, pts []model.UsageSample) {
	t.Helper()
	var longest, recent time.Duration
	for i := 1; i < len(pts); i++ {
		d := pts[i].At.Sub(pts[i-1].At)
		if d <= 0 {
			t.Errorf("%s: points %d and %d out of order or twice: %s, %s", what, i-1, i, pts[i-1].At, pts[i].At)
		}
		longest = max(longest, d)
		if pts[len(pts)-1].At.Sub(pts[i].At) < 15*time.Minute {
			recent = max(recent, d)
		}
	}
	if recent > 3*time.Minute {
		t.Errorf("%s: a hole of %s in the chart's last 15 minutes", what, recent)
	}
	if n := len(pts); n > 0 {
		t.Logf("%s: %d points from %s to %s, longest step %s, in the last 15 minutes %s", what, n, pts[0].At.Format(time.TimeOnly), pts[n-1].At.Format(time.TimeOnly), longest, recent)
	}
}

func logProviders(t *testing.T, what string, ps []metrics.ProviderStatus) {
	t.Helper()
	for _, p := range ps {
		t.Logf("%s: provider %s %s: %s, serves %v, last ok %s, last error %q", what, p.Label, p.Service, p.Status, p.Serves, p.LastOK.Format(time.TimeOnly), p.LastError)
	}
}

// writes counts the connection's requests that would change something; the access reviews (a
// create of a self-subject review, which stores nothing) are not among them.
func writes(s *engine.Session) int64 {
	var n int64
	for key, c := range s.Diagnostics().Requests.ByVerbResource {
		verb, resource, _ := strings.Cut(key, " ")
		switch verb {
		case "create", "update", "patch", "delete", "deletecollection":
			if !strings.HasPrefix(resource, "selfsubject") {
				n += c
			}
		}
	}
	return n
}

// waitHistory waits until the drawer's metrics of ref are available and ok accepts them.
func waitHistory(ctx context.Context, t *testing.T, s *engine.Session, ref model.ObjectRef, ok func(*engine.ObjectMetrics) bool) *engine.ObjectMetrics {
	t.Helper()
	var m *engine.ObjectMetrics
	if !testkit.WaitFor(ctx, func() bool {
		m, _ = s.ObjectMetrics(ref)
		return m != nil && m.Status == metrics.StatusAvailable && len(m.Targets) > 0 && ok(m)
	}) {
		if m != nil {
			for _, tg := range m.Targets {
				t.Logf("%s %q: %d samples, %d network, %d filesystem", ref.Name, tg.Name, len(tg.Samples), len(tg.Network), len(tg.Filesystem))
			}
			t.Fatalf("%s: status %q source %q message %q filesystem %q", ref.Name, m.Status, m.Source, m.Message, m.FilesystemSource)
		}
		t.Fatalf("%s: no metrics", ref.Name)
	}
	return m
}

// logTarget logs a chart's latest numbers, which the test's report quotes.
func logTarget(t *testing.T, what, fsSource string, tg engine.MetricsTarget) {
	t.Helper()
	line := what + ": "
	if n := len(tg.Samples); n > 0 {
		u := tg.Samples[n-1]
		line += tg.Samples[0].At.Format(time.TimeOnly) + "–" + u.At.Format(time.TimeOnly) + ", " +
			itoa(n) + " points; cpu " + itoa(int(u.CPUMilli)) + "m, memory " + model.FormatBytes(u.MemoryBytes)
	}
	if n := len(tg.Network); n > 0 {
		x := tg.Network[n-1]
		line += "; network rx " + itoa(int(x.ReceiveBytesPerSec)) + " B/s tx " + itoa(int(x.TransmitBytesPerSec)) + " B/s"
	}
	if n := len(tg.Filesystem); n > 0 {
		f := tg.Filesystem[n-1]
		line += "; filesystem (" + fsSource + ") usage " + itoa(int(f.UsageBytes)) + " B, read " + itoa(int(f.ReadBytesPerSec)) + " B/s, write " + itoa(int(f.WriteBytesPerSec)) + " B/s"
	}
	t.Log(line)
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// TestKindMetricsFallback watches the engine fall back to one provider while the other is down
// and return to both when it is back (owner, 2026-10-12): it scales the kind cluster's
// metrics-server to zero (Prometheus takes the live values over in the drawer and the lists), back
// to one (the Metrics Server serves them again), then Prometheus to zero (the Metrics Server's live
// values go on without new history; the status line says Prometheus does not answer), and back.
// It changes kube-system and monitoring of the kind cluster, so it runs only with
// COGPIT_KIND_FALLBACK=1 and never with the rest of make test-kind; it restores both deployments
// to one replica and waits for them to be ready, also when it fails.
func TestKindMetricsFallback(t *testing.T) {
	if os.Getenv("COGPIT_KIND_FALLBACK") != "1" {
		t.Skip("scales the kind cluster's metrics-server and Prometheus: set COGPIT_KIND_FALLBACK=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	// A failed provider is asked again after 20 s instead of a minute, so the test sees the way
	// back within its time.
	s, dispatch := kindMetricsSession(ctx, t, metrics.Options{ProviderRetry: 20 * time.Second, RetryDetectTTL: 20 * time.Second})
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{os.Getenv("COGPIT_KIND_KUBECONFIG")}, Context: os.Getenv("COGPIT_KIND_CONTEXT")})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	kube := conn.Kube()
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer ccancel()
		for _, d := range [][2]string{{"kube-system", "metrics-server"}, {"monitoring", "prometheus-server"}} {
			if err := scaleTo(cctx, kube, d[0], d[1], 1); err != nil {
				t.Errorf("restore %s/%s: %v", d[0], d[1], err)
			}
		}
	})

	var pod model.ObjectRef
	if !testkit.WaitFor(ctx, func() bool {
		tbl := s.Table("pods")
		if tbl == nil || !tbl.Synced {
			return false
		}
		for _, r := range tbl.Rows {
			if r.Ref.Namespace == "cogpit-demo" && strings.HasPrefix(r.Ref.Name, "web-") && r.Status == model.StatusOK {
				pod = r.Ref
				return true
			}
		}
		return false
	}) {
		t.Fatal("no running web pod in cogpit-demo")
	}
	dispatch("ui.navigate", map[string]any{"ref": pod})
	m := waitHistory(ctx, t, s, pod, func(m *engine.ObjectMetrics) bool { return m.Source == kindBothSource })
	logProviders(t, "both", m.Providers)
	start := time.Now()
	status := func(m *engine.ObjectMetrics, id string) metrics.ProviderStatus {
		for _, p := range m.Providers {
			if p.Provider == id {
				return p
			}
		}
		return metrics.ProviderStatus{}
	}

	// The Metrics Server goes away: Prometheus serves the live values.
	if err := scaleTo(ctx, kube, "kube-system", "metrics-server", 0); err != nil {
		t.Fatal(err)
	}
	m = waitHistory(ctx, t, s, pod, func(m *engine.ObjectMetrics) bool {
		ms, prom := status(m, metrics.ServerID), status(m, metrics.PrometheusID)
		return ms.Status != metrics.StatusAvailable && slices.Contains(prom.Serves, metrics.ServesLive) && m.Source == kindPromSource
	})
	t.Logf("the Metrics Server down after %s", time.Since(start).Round(time.Second))
	logProviders(t, "metrics-server down", m.Providers)
	newest := func(m *engine.ObjectMetrics) time.Time { pts := m.Targets[0].Samples; return pts[len(pts)-1].At }
	before := newest(m)
	m = waitHistory(ctx, t, s, pod, func(m *engine.ObjectMetrics) bool { return newest(m).After(before) })
	t.Logf("Prometheus's live values: the newest point moved from %s to %s", before.Format(time.TimeOnly), newest(m).Format(time.TimeOnly))
	checkSeam(t, "Prometheus's live values", m.Targets[0].Samples)
	// A one-off read that no fresh result covers (the pods' are, by the open Pods list's last poll,
	// for two of its intervals) asks the Metrics Server, then Prometheus, in the same call.
	list := dispatch("metrics.get", map[string]any{"kind": "nodes"}).(*engine.MetricsResult)
	t.Logf("metrics.get nodes with the Metrics Server down: %s, %d nodes, %s", list.Status, len(list.Nodes), list.Source)
	if list.Status != metrics.StatusAvailable || list.Source != kindPromSource || len(list.Nodes) == 0 {
		t.Errorf("metrics.get nodes with the Metrics Server down: %+v", list)
	}

	// It comes back: the normal case again.
	if err := scaleTo(ctx, kube, "kube-system", "metrics-server", 1); err != nil {
		t.Fatal(err)
	}
	back := time.Now()
	m = waitHistory(ctx, t, s, pod, func(m *engine.ObjectMetrics) bool {
		ms := status(m, metrics.ServerID)
		return ms.Status == metrics.StatusAvailable && slices.Contains(ms.Serves, metrics.ServesLive) && m.Source == kindBothSource
	})
	t.Logf("the Metrics Server serves again %s after it was ready", time.Since(back).Round(time.Second))
	logProviders(t, "metrics-server back", m.Providers)
	checkSeam(t, "after the Metrics Server came back", m.Targets[0].Samples)

	// Prometheus goes away: the live values go on; a range change asks it and finds it not
	// answering; the status line says so.
	if err := scaleTo(ctx, kube, "monitoring", "prometheus-server", 0); err != nil {
		t.Fatal(err)
	}
	back = time.Now()
	dispatch("ui.setMetricsRange", map[string]any{"ref": pod, "range": "3h"})
	m = waitHistory(ctx, t, s, pod, func(m *engine.ObjectMetrics) bool {
		prom := status(m, metrics.PrometheusID)
		return prom.Status != metrics.StatusAvailable && prom.LastError != ""
	})
	t.Logf("Prometheus not answering seen %s after it was scaled to zero", time.Since(back).Round(time.Second))
	logProviders(t, "prometheus down", m.Providers)
	before = newest(m)
	gets := s.Diagnostics().Requests.ByVerbResource["get pods.metrics.k8s.io"]
	m = waitHistory(ctx, t, s, pod, func(m *engine.ObjectMetrics) bool {
		return s.Diagnostics().Requests.ByVerbResource["get pods.metrics.k8s.io"] > gets+1 && newest(m).After(before)
	})
	if ms := status(m, metrics.ServerID); !slices.Equal(ms.Serves, []string{metrics.ServesLive}) || m.Source == "" {
		t.Errorf("with Prometheus down the Metrics Server = %+v, source %q", ms, m.Source)
	}

	// Prometheus comes back: the 3h history is asked for again.
	if err := scaleTo(ctx, kube, "monitoring", "prometheus-server", 1); err != nil {
		t.Fatal(err)
	}
	back = time.Now()
	m = waitHistory(ctx, t, s, pod, func(m *engine.ObjectMetrics) bool {
		prom := status(m, metrics.PrometheusID)
		return prom.Status == metrics.StatusAvailable && slices.Contains(prom.Serves, metrics.ServesHistory) && m.Source == kindBothSource
	})
	t.Logf("Prometheus serves again %s after it was ready", time.Since(back).Round(time.Second))
	logProviders(t, "prometheus back", m.Providers)
	checkSeam(t, "after Prometheus came back", m.Targets[0].Samples)
}

// TestKindKubePrometheusStack checks Cogpit against a real kube-prometheus-stack (G12, "left of
// Prometheus"): installed by hand into the namespace cogpit-kps as the release kps (the operator,
// one Prometheus, the kubelet's cAdvisor scrape every 30 s, nothing else; docs/developer/
// testing.md has the values) and uninstalled afterwards, because detection prefers it to the
// `prometheus` chart the other kind tests expect. Detection must find
// kps-kube-prometheus-stack-prometheus (a short release keeps "-stack" in the name); a node's charts
// come from series labelled by the node label (the chart's kubelet ServiceMonitor), a pod's as
// before. It only reads; skipped without that service.
func TestKindKubePrometheusStack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	s, dispatch := kindMetricsSession(ctx, t, metrics.Options{})
	if _, err := s.Object(ctx, model.ObjectRef{Kind: "services", Namespace: "cogpit-kps", Name: "kps-kube-prometheus-stack-prometheus"}); errors.Is(err, engine.ErrNotFound) {
		t.Skip("no kube-prometheus-stack in cogpit-kps (installed by hand for this check only)")
	} else if err != nil {
		t.Fatal(err)
	}
	const source = "Prometheus (cogpit-kps/kps-kube-prometheus-stack-prometheus)"
	dispatch("ui.openView", map[string]any{"view": "nodes"})
	var node model.ObjectRef
	if !testkit.WaitFor(ctx, func() bool {
		tbl := s.Table("nodes")
		if tbl == nil || !tbl.Synced || len(tbl.Rows) == 0 {
			return false
		}
		node = tbl.Rows[0].Ref
		return true
	}) {
		t.Fatal("no node")
	}
	opened := time.Now()
	dispatch("ui.navigate", map[string]any{"ref": node})
	// The chart's default kubelet ServiceMonitor drops cAdvisor's series without a pod ("Drop
	// cgroup metrics with no pod": sourceLabels [id, pod], regex ".+;"), the root cgroup the node
	// charts read until A43: the node had no history (2026-10-12; F1 in the owner's window). Its
	// node-exporter (on since A115) has it, labelled with the exporter's host-network address only.
	m := waitHistory(ctx, t, s, node, func(m *engine.ObjectMetrics) bool {
		return strings.HasPrefix(m.Source, source) && len(m.Targets[0].Samples) >= 2 && m.HistorySource != ""
	})
	logProviders(t, "kube-prometheus-stack", m.Providers)
	if p := m.Providers[0]; p.Service != "cogpit-kps/kps-kube-prometheus-stack-prometheus:9090" || !slices.Contains(p.Serves, metrics.ServesHistory) {
		t.Errorf("Prometheus's status = %+v", p)
	}
	if m.HistorySource != metrics.UsageNodeExporter {
		t.Errorf("kube-prometheus-stack node: history from %q, want node-exporter by the node's address", m.HistorySource)
	}
	checkPast(t, "kube-prometheus-stack node "+node.Name, m.Targets[0].Samples, opened)

	dispatch("ui.openView", map[string]any{"view": "pods"})
	var pod model.ObjectRef
	if !testkit.WaitFor(ctx, func() bool {
		tbl := s.Table("pods")
		if tbl == nil || !tbl.Synced {
			return false
		}
		for _, r := range tbl.Rows {
			if r.Ref.Namespace == "cogpit-demo" && strings.HasPrefix(r.Ref.Name, "web-") && r.Status == model.StatusOK {
				pod = r.Ref
				return true
			}
		}
		return false
	}) {
		t.Fatal("no running web pod in cogpit-demo")
	}
	dispatch("ui.navigate", map[string]any{"ref": pod})
	m = waitHistory(ctx, t, s, pod, func(m *engine.ObjectMetrics) bool {
		return strings.HasPrefix(m.Source, source) && len(m.Targets) >= 2 && len(m.Targets[1].Samples) > 0
	})
	ctr := m.Targets[1].Name
	dispatch("ui.setMetricsVariant", map[string]any{"ref": pod, "variant": "network"})
	dispatch("ui.setMetricsVariant", map[string]any{"ref": pod, "container": ctr, "variant": "filesystem"})
	m = waitHistory(ctx, t, s, pod, func(m *engine.ObjectMetrics) bool {
		return len(m.Targets[0].Network) > 0 && len(m.Targets[1].Filesystem) > 0
	})
	whole := m.Targets[0]
	whole.Filesystem = m.Targets[1].Filesystem
	logTarget(t, "kube-prometheus-stack pod "+pod.Name, m.FilesystemSource, whole)
	checkSeam(t, "kube-prometheus-stack pod", whole.Samples)
}

// scaleTo sets a deployment's replicas and waits until that many are ready (and the old pods
// gone when scaling to zero).
func scaleTo(ctx context.Context, kube kubernetes.Interface, ns, name string, replicas int32) error {
	deps := kube.AppsV1().Deployments(ns)
	if _, err := deps.UpdateScale(ctx, name, &autoscalingv1.Scale{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       autoscalingv1.ScaleSpec{Replicas: replicas},
	}, metav1.UpdateOptions{}); err != nil {
		return err
	}
	for {
		d, err := deps.Get(ctx, name, metav1.GetOptions{})
		if err == nil && d.Status.ReadyReplicas == replicas && d.Status.Replicas == replicas && d.Status.UpdatedReplicas == replicas {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
