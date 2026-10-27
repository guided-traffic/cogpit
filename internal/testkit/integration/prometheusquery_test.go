package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// queryLog records the queries a fake Prometheus gets.
type queryLog struct {
	mu      sync.Mutex
	queries []string
	h       http.Handler
}

func (l *queryLog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	if q := r.URL.Query().Get("query"); q != "" {
		l.queries = append(l.queries, q)
	}
	l.mu.Unlock()
	l.h.ServeHTTP(w, r)
}

func (l *queryLog) has(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, q := range l.queries {
		if strings.Contains(q, sub) {
			return true
		}
	}
	return false
}

// Lens's other Metrics provider settings (G12, Cluster Settings › Metrics, 2026-10-16): the query
// format, the empty-container filter and HTTPS, set with cluster.setPrometheus beside the address,
// applied at once (the open drawer asks the service over https with the filter off), kept in
// Cogpit's own preferences per context and applied to the next connection before anything is
// detected; a format Cogpit does not know is refused.
func TestPrometheusQuerySettings(t *testing.T) {
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
	plain := &queryLog{h: http.HandlerFunc(promHandler)}
	tls := &queryLog{h: http.HandlerFunc(promHandler)}
	srv.SetProxy("monitoring", "prometheus-server:80", plain)
	srv.SetProxy("monitoring", "https:prometheus-server:80", tls)
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
		s.UpdateUI(func(u *engine.UIState) { u.Drawer = &ref })
		return s
	}
	dispatch := func(s *engine.Session, id, params string) (any, error) {
		return s.Dispatch(ctx, engine.Invocation{ID: id, Params: json.RawMessage(params), Origin: engine.OriginGUI})
	}
	waitService := func(s *engine.Session, service string) {
		t.Helper()
		var m *engine.ObjectMetrics
		if !testkit.WaitFor(ctx, func() bool {
			m, _ = s.ObjectMetrics(ref)
			return m != nil && m.Status == metrics.StatusAvailable && len(m.Providers) > 0 && m.Providers[0].Service == service &&
				len(m.Targets) > 0 && len(m.Targets[0].Samples) > 0
		}) {
			t.Fatalf("the drawer never read %q: %+v", service, m)
		}
	}

	s := session(t)
	waitService(s, "monitoring/prometheus-server:80")
	if set, _ := s.PrometheusSetting(""); set.Format != "auto" || !set.FilterEmptyContainers || set.HTTPS {
		t.Errorf("the defaults: %+v", set)
	}
	res, err := dispatch(s, "cluster.setPrometheus", `{"address":"auto","format":"prometheus-operator","filterEmptyContainers":false,"https":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if set := res.(engine.PrometheusSetting); !set.Auto || set.Format != "prometheus-operator" || set.FilterEmptyContainers || !set.HTTPS || !set.Kept {
		t.Errorf("setting = %+v", set)
	}
	waitService(s, "monitoring/prometheus-server:80 (https)")
	if !testkit.WaitFor(ctx, func() bool { return tls.has(`container!="POD"`) }) || tls.has(`container!=""`) {
		t.Error("the pod's queries over https still filter the empty containers")
	}
	if _, err := dispatch(s, "cluster.setPrometheus", `{"format":"helm-14"}`); err == nil {
		t.Error("an unknown query format was accepted")
	}

	// The next connection asks over https from its first request.
	s.Close()
	s2 := session(t)
	waitService(s2, "monitoring/prometheus-server:80 (https)")
	if set, _ := s2.PrometheusSetting(""); set.Format != "prometheus-operator" || set.FilterEmptyContainers || !set.HTTPS {
		t.Errorf("read back: %+v", set)
	}
	// The whole setting back to the defaults: nothing is kept for the context.
	if _, err := dispatch(s2, "cluster.setPrometheus", `{"address":"auto"}`); err != nil {
		t.Fatal(err)
	}
	waitService(s2, "monitoring/prometheus-server:80")
	if again, _ := engine.NewMetricsSettings(dir); again.Query("fake") != (metrics.QuerySettings{}) {
		t.Errorf("the defaults left %+v in the preferences", again.Query("fake"))
	}
}
