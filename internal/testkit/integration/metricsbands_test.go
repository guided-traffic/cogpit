package integration_test

import (
	"context"
	"net/http"
	"strconv"
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

// stepRecorder is promHandler that remembers the step and the span of every CPU range query.
type stepRecorder struct {
	mu    sync.Mutex
	steps []string
	spans []float64 // end − start, seconds
}

func (s *stepRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if q := r.URL.Query(); r.URL.Path == "/api/v1/query_range" && strings.Contains(q.Get("query"), "container_cpu_usage_seconds_total") {
		start, _ := strconv.ParseFloat(q.Get("start"), 64)
		end, _ := strconv.ParseFloat(q.Get("end"), 64)
		s.mu.Lock()
		s.steps, s.spans = append(s.steps, q.Get("step")), append(s.spans, end-start)
		s.mu.Unlock()
	}
	promHandler(w, r)
}

func (s *stepRecorder) span(i int) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spans[i]
}

func (s *stepRecorder) cpuSteps() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...)
}

// A46 through the engine: the drawer tells the hub every chart's range (the container's default
// hour too), so the pod's chart at a week and the container's at the hour get a query set each:
// the week at its own step, the hour at 30 s (before: one set at ten minutes, the hour six points).
func TestDrawerChartsFarApartGetQuerySetsOfTheirOwn(t *testing.T) {
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
	rec := &stepRecorder{}
	srv.SetProxy("monitoring", "prometheus-server:80", rec)
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
	ref := model.ObjectRef{Kind: "pods", Namespace: "web", Name: "a"}
	s.UpdateUI(func(u *engine.UIState) { u.Drawer = &ref })
	if !testkit.WaitFor(ctx, func() bool {
		m, _ := s.ObjectMetrics(ref)
		return m != nil && len(m.Targets) == 2 && len(m.Targets[0].Samples) > 0
	}) {
		t.Fatal("the drawer never got its history")
	}
	if err := s.SetMetricsRange(ref, "", 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool { return strings.Contains(strings.Join(rec.cpuSteps(), ","), "605") }) {
		t.Fatalf("the week was never asked at its own step: %v", rec.cpuSteps())
	}
	m, _ := s.ObjectMetrics(ref)
	if m.Targets[0].Range != "7d" || m.Targets[1].Range != "1h" {
		t.Errorf("ranges %q and %q", m.Targets[0].Range, m.Targets[1].Range)
	}
	// The hour's set stood: asked once when the drawer opened, at 30 s, never at the week's step.
	if steps := rec.cpuSteps(); len(steps) != 2 || steps[0] != "30" || steps[1] != "605" {
		t.Errorf("CPU range queries at steps %v, want the hour's at 30 and the week's at 605", steps)
	}

	// The container's chart at the week too: one range, whose set holds the week already. Without a
	// Metrics Server, Prometheus gives the live values: the next query asks for the newest points
	// at the week's step, not for the week again.
	if err := s.SetMetricsRange(ref, "app", 7*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool { return len(rec.cpuSteps()) >= 3 }) {
		t.Fatalf("no live values after every chart showed the week: %v", rec.cpuSteps())
	}
	if steps := rec.cpuSteps(); steps[2] != "605" || rec.span(2) > 3600 {
		t.Errorf("the third CPU query at step %s over %.0f s, want the newest points at 605", steps[2], rec.span(2))
	}
}
