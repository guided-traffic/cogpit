package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// promHandler is a Prometheus that has CPU, memory, network and filesystem usage of web/a's
// container "app": a sample 50 minutes back, one 60 s and one 30 s back from the query's end.
func promHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/v1/status/buildinfo" {
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.55.1"}}`))
		return
	}
	q := r.URL.Query().Get("query")
	end, _ := strconv.ParseFloat(r.URL.Query().Get("end"), 64)
	var metric map[string]string
	var base float64
	switch {
	case strings.Contains(q, "container_cpu_usage_seconds_total"):
		metric, base = map[string]string{"container": "app"}, 0.5
	case strings.Contains(q, "container_memory_working_set_bytes"):
		metric, base = map[string]string{"container": "app"}, 1<<26
	case strings.Contains(q, "container_network_"):
		metric, base = map[string]string{}, 1000
	case strings.Contains(q, "container_fs_usage_bytes"):
		metric, base = map[string]string{"container": "app"}, 4096
	default:
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
		return
	}
	values := [][]any{
		{end - 3000, strconv.FormatFloat(base, 'f', -1, 64)},
		{end - 60, strconv.FormatFloat(base*2, 'f', -1, 64)},
		{end - 30, strconv.FormatFloat(base*3, 'f', -1, 64)},
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{
		"resultType": "matrix", "result": []any{map[string]any{"metric": metric, "values": values}},
	}})
}

// The whole path in the engine against a fake API server with a Prometheus behind the service
// proxy and no Metrics Server (Prometheus alone serves everything): detection from the cached
// Services, the drawer's charts from range queries, the charts' ranges and variants, the network
// and the filesystem asked for only while a chart shows them, the source line and the status line,
// and the request budget (P13).
func TestPrometheusInTheDrawer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Access: testkit.AdminAccess()})
	defer srv.Close()
	pod := testkit.Pod("web", "a")
	if err := unstructured.SetNestedSlice(pod.Object, []any{map[string]any{
		"name": "app", "image": "registry.example/app:1.0",
		"resources": map[string]any{"requests": map[string]any{"cpu": "100m"}, "limits": map[string]any{"cpu": "500m", "memory": "128Mi"}},
	}}, "spec", "containers"); err != nil {
		t.Fatal(err)
	}
	srv.Set(testkit.PodsGVR, pod)
	srv.Set(testkit.ServicesGVR, testkit.Service("monitoring", "prometheus-server", "10.0.0.5"))
	srv.Set(testkit.ServicesGVR, testkit.Service("default", "web", "10.0.0.6"))
	srv.SetProxy("monitoring", "prometheus-server:80", http.HandlerFunc(promHandler))

	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{
		KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "default")},
		MetricsOptions:  metrics.Options{ObjectInterval: 50 * time.Millisecond},
	}})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	proxy := func() int64 { return srv.Requests()["get services/proxy"] }
	ref := model.ObjectRef{Kind: "pods", Namespace: "web", Name: "a"}

	// The Pods list is the open view: its CPU and Memory columns cost the detection's probe and
	// two instant queries (cpu, memory) for all pods, once per 60 s.
	if !testkit.WaitFor(ctx, func() bool { return proxy() >= 3 }) {
		t.Fatalf("%d proxy requests with the Pods list open, want 3 (probe + 2 queries)", proxy())
	}
	time.Sleep(200 * time.Millisecond)
	if n := proxy(); n != 3 {
		t.Fatalf("%d proxy requests with the Pods list open, want 3 (probe + 2 queries)", n)
	}
	if err := s.SetMetricsRange(ref, "", 10*time.Minute); !errors.Is(err, engine.ErrDrawerNotOpen) {
		t.Errorf("SetMetricsRange without a drawer: %v", err)
	}

	s.UpdateUI(func(u *engine.UIState) { u.Drawer = &ref })
	var m *engine.ObjectMetrics
	if !testkit.WaitFor(ctx, func() bool {
		m, _ = s.ObjectMetrics(ref)
		return m != nil && len(m.Targets) == 2 && len(m.Targets[0].Samples) >= 3 && len(m.Targets[1].Samples) >= 3
	}) {
		t.Fatalf("the drawer never got the history: %+v", m)
	}
	// The drawer's first poll: CPU and memory over the hour, nothing else; the next poll (the newest
	// points) is a step (30 s) away.
	if n := proxy(); n != 3+2 {
		t.Errorf("%d proxy requests after the drawer opened, want 5 (3 + cpu and memory once)", n)
	}
	if m.Status != metrics.StatusAvailable || m.Source != "Prometheus (monitoring/prometheus-server)" || m.Message != "" {
		t.Errorf("status %q source %q message %q", m.Status, m.Source, m.Message)
	}
	if c := m.Capabilities; !c.History || !c.Network || !c.Filesystem || m.FilesystemSource != "" || len(m.Targets[0].Network) != 0 {
		t.Errorf("capabilities %+v, filesystem source %q, %d network points before a chart showed the network", c, m.FilesystemSource, len(m.Targets[0].Network))
	}
	whole, app := m.Targets[0], m.Targets[1]
	if whole.Current == nil || whole.Current.CPUMilli != 1500 || app.Name != "app" || app.Current == nil || app.Limits.CPUMilli != 500 || whole.Variant != "cpu" {
		t.Errorf("targets = %+v / %+v", whole, app)
	}
	// The status line: Prometheus serves the history and the live values, the Metrics Server is
	// not served; ui.state carries the same.
	if len(m.Providers) != 2 || m.Providers[0].Provider != "prometheus" || m.Providers[0].Service != "monitoring/prometheus-server:80" ||
		!slices.Contains(m.Providers[0].Serves, metrics.ServesHistory) || !slices.Contains(m.Providers[0].Serves, metrics.ServesLive) ||
		m.Providers[1].Provider != "metrics-server" || m.Providers[1].Status != metrics.StatusNotServed {
		t.Errorf("providers = %+v", m.Providers)
	}
	raw, err := s.Dispatch(ctx, engine.Invocation{ID: "ui.state", Params: json.RawMessage(`{}`), Origin: engine.OriginExternalAgent})
	if st, ok := raw.(commands.StateResult); err != nil || !ok || len(st.DrawerMetricsProviders) != 2 || len(st.DrawerCharts) != 2 || st.DrawerCharts[0].Variant != "cpu" {
		t.Errorf("ui.state: %+v, %v", raw, err)
	}

	// The detection came from the cached Services: one list-and-watch of them, no list of our own.
	reqs := srv.Requests()
	if n := reqs["list services"] + reqs["watchlist services"]; n != 1 {
		t.Errorf("services were read %d times (%v), want the cache's one", n, reqs)
	}

	// The pod's chart shows the network, the container's the filesystem: each asked for once over
	// the hour, at once (receive and transmit, the filesystem's usage).
	for _, p := range []string{`{"ref":{"kind":"pods","namespace":"web","name":"a"},"variant":"network"}`, `{"ref":{"kind":"pods","namespace":"web","name":"a"},"container":"app","variant":"filesystem"}`} {
		if _, err := s.Dispatch(ctx, engine.Invocation{ID: "ui.setMetricsVariant", Params: json.RawMessage(p), Origin: engine.OriginGUI}); err != nil {
			t.Fatal(err)
		}
	}
	if !testkit.WaitFor(ctx, func() bool {
		m, _ = s.ObjectMetrics(ref)
		return m != nil && len(m.Targets) == 2 && len(m.Targets[0].Network) == 3 && len(m.Targets[1].Filesystem) == 3
	}) {
		t.Fatalf("the network and the filesystem never came: %+v", m)
	}
	whole, app = m.Targets[0], m.Targets[1]
	if !app.NetworkOfPod || len(app.Network) != 3 || whole.NetworkOfPod || m.FilesystemSource != "usage" || whole.Variant != "network" || app.Variant != "filesystem" {
		t.Errorf("network: pod %v, container %v of pod %v; filesystem %q; variants %q %q", len(whole.Network), len(app.Network), app.NetworkOfPod, m.FilesystemSource, whole.Variant, app.Variant)
	}
	if !testkit.WaitFor(ctx, func() bool { return proxy() == 3+2+3 }) {
		t.Errorf("%d proxy requests after the network and the filesystem were shown, want 8 (5 + 2 + 1)", proxy())
	}

	// A chart set to ten minutes shows the tail: the sample 50 minutes back is gone, the others stay.
	if err := s.SetMetricsRange(ref, "", 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool {
		m, _ = s.ObjectMetrics(ref)
		return m != nil && len(m.Targets) == 2 && len(m.Targets[0].Samples) == 2 && len(m.Targets[1].Samples) == 3
	}) {
		t.Fatalf("pod chart %d samples, container chart %d, want 2 and 3", len(m.Targets[0].Samples), len(m.Targets[1].Samples))
	}
	if m.Targets[0].Range != "10m" || m.Targets[1].Range != "1h" {
		t.Errorf("the targets' ranges are %q and %q, want 10m and the default 1h", m.Targets[0].Range, m.Targets[1].Range)
	}
	// The change also made the drawer ask once more, at the finer step, for what the charts show:
	// the network and the filesystem; no chart shows CPU or memory, so they are not asked (since
	// 2026-10-16 night; before, 13: CPU and memory too).
	if !testkit.WaitFor(ctx, func() bool { return proxy() == 8+3 }) {
		t.Errorf("%d proxy requests after the range changed, want 11 (8 + 2 + 1)", proxy())
	}
	if err := s.SetMetricsRange(ref, "", -time.Second); err == nil {
		t.Error("a negative range was accepted")
	}

	// Closing the drawer ends the polls.
	subscribed := s.Backend().Metrics.Subscribed()
	s.UpdateUI(func(u *engine.UIState) { u.Drawer = nil })
	if !testkit.WaitFor(ctx, func() bool { return s.Backend().Metrics.Subscribed() == subscribed-1 }) {
		t.Fatal("still polling after the drawer closed")
	}
	time.Sleep(100 * time.Millisecond)
	settled := proxy()
	time.Sleep(300 * time.Millisecond)
	if n := proxy(); n != settled {
		t.Errorf("%d proxy requests after the drawer closed", n-settled)
	}
	t.Logf("proxy requests: %d (%v)", settled, srv.Requests())
}
