package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// countingHandler counts the requests a fake Prometheus behind the service proxy gets.
type countingHandler struct {
	n atomic.Int64
	h http.Handler
}

func (c *countingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.n.Add(1)
	c.h.ServeHTTP(w, r)
}

// The user names the cluster's Prometheus (G12; Lens: Cluster settings → Metrics): a service that
// detection by name would never find (observability/thanos-query, under a route prefix). The
// setting replaces detection at once (the open drawer asks the named service for its history),
// is kept in Cogpit's own preferences for the context and applies to the next connection before
// anything is detected; auto brings detection back. What is not a service address is refused.
func TestPrometheusOverrideFromTheSettings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Access: testkit.AdminAccess()})
	defer srv.Close()
	pod := testkit.Pod("web", "a")
	if err := unstructured.SetNestedSlice(pod.Object, []any{map[string]any{"name": "app", "image": "registry.example/app:1.0"}}, "spec", "containers"); err != nil {
		t.Fatal(err)
	}
	srv.Set(testkit.PodsGVR, pod)
	srv.Set(testkit.ServicesGVR, testkit.Service("monitoring", "prometheus-server", "10.0.0.5"))
	srv.Set(testkit.ServicesGVR, testkit.Service("observability", "thanos-query", "10.0.0.7"))
	byName := &countingHandler{h: http.HandlerFunc(promHandler)}
	named := &countingHandler{h: http.StripPrefix("/prometheus", http.HandlerFunc(promHandler))}
	srv.SetProxy("monitoring", "prometheus-server:80", byName)
	srv.SetProxy("observability", "thanos-query:9090", named)
	dir := filepath.Join(t.TempDir(), "Cogpit")
	kubeconfig := srv.WriteKubeconfig(t, "fake", "default")
	ref := model.ObjectRef{Kind: "pods", Namespace: "web", Name: "a"}

	session := func(t *testing.T) *engine.Session {
		t.Helper()
		settings, err := engine.NewMetricsSettings(dir)
		if err != nil {
			t.Fatal(err)
		}
		s := engine.New(engine.Options{Connector: &engine.ClusterConnector{
			KubeconfigPaths: []string{kubeconfig}, MetricsSettings: settings,
			MetricsOptions: metrics.Options{ObjectInterval: 50 * time.Millisecond},
		}})
		t.Cleanup(s.Close)
		if err := commands.RegisterAll(s.Registry()); err != nil {
			t.Fatal(err)
		}
		if err := s.Connect(ctx, "fake"); err != nil {
			t.Fatal(err)
		}
		return s
	}
	dispatch := func(s *engine.Session, id, params string) (any, error) {
		return s.Dispatch(ctx, engine.Invocation{ID: id, Params: json.RawMessage(params), Origin: engine.OriginGUI})
	}
	waitSource := func(s *engine.Session, source string) *engine.ObjectMetrics {
		t.Helper()
		var m *engine.ObjectMetrics
		if !testkit.WaitFor(ctx, func() bool {
			m, _ = s.ObjectMetrics(ref)
			return m != nil && m.Status == metrics.StatusAvailable && m.Source == source && len(m.Targets) > 0 && len(m.Targets[0].Samples) > 0
		}) {
			t.Fatalf("the drawer never showed %q: %+v", source, m)
		}
		return m
	}

	s := session(t)
	s.UpdateUI(func(u *engine.UIState) { u.Drawer = &ref })
	waitSource(s, "Prometheus (monitoring/prometheus-server)")

	// Named in the settings: the open drawer asks the named service, under its prefix.
	res, err := dispatch(s, "cluster.setPrometheus", `{"address":"observability/thanos-query:9090","prefix":"prometheus"}`)
	if err != nil {
		t.Fatal(err)
	}
	if set := res.(engine.PrometheusSetting); set.Auto || set.Address != "observability/thanos-query:9090" || set.Prefix != "/prometheus" || !set.Kept {
		t.Errorf("setting = %+v", set)
	}
	m := waitSource(s, "Thanos (observability/thanos-query)") // a Thanos by its name (D-71, 2026-10-16 night)
	if p := m.Providers[0]; p.Service != "observability/thanos-query:9090/prometheus" || p.Status != metrics.StatusAvailable {
		t.Errorf("status line = %+v", p)
	}
	if named.n.Load() == 0 {
		t.Error("the named service got no request")
	}
	res, err = dispatch(s, "cluster.prometheus", `{}`)
	if set, ok := res.(engine.PrometheusSetting); err != nil || !ok || set.Auto || set.Address != "observability/thanos-query:9090" ||
		set.InUse != "Thanos observability/thanos-query:9090/prometheus" || set.InUseStatus != metrics.StatusAvailable {
		t.Errorf("cluster.prometheus = %+v, %v", res, err) // the one in use: Cluster Settings › Metrics names it (2026-10-16 night)
	}

	// Not a service address: refused, the setting stays.
	for _, bad := range []string{`{"address":"observability/thanos-query"}`, `{"address":"observability/thanos-query:9090","prefix":"/../api/v1/secrets"}`, `{"address":"Observability/x:9090"}`} {
		if _, err := dispatch(s, "cluster.setPrometheus", bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}

	// The next connection starts with the setting: nothing asks the service detection would find.
	s.Close()
	before := byName.n.Load()
	s2 := session(t)
	s2.UpdateUI(func(u *engine.UIState) { u.Drawer = &ref })
	waitSource(s2, "Thanos (observability/thanos-query)")
	if n := byName.n.Load(); n != before {
		t.Errorf("the service found by name got %d requests while the setting named another", n-before)
	}

	// Auto: detection by name again.
	if _, err := dispatch(s2, "cluster.setPrometheus", `{"address":"auto"}`); err != nil {
		t.Fatal(err)
	}
	waitSource(s2, "Prometheus (monitoring/prometheus-server)")
	if set, _ := s2.PrometheusSetting(""); !set.Auto || set.Address != "" {
		t.Errorf("after auto: %+v", set)
	}
	if again, _ := engine.NewMetricsSettings(dir); again.Override("fake") != nil {
		t.Errorf("auto left %+v in the preferences", again.Override("fake"))
	}
}
