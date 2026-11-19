package metrics_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// A pod-metrics refusal affects only its namespace (D-39, owner): the Metrics Server refuses `list
// pods.metrics.k8s.io` in team-b (RBAC on the Metrics API is not the pods'), answers in team-a.
// team-b's pods get no values and the state forbidden for team-b; team-a keeps the Metrics Server's
// values; the Metrics Server's status stays as it is, and Prometheus is not asked in its place.

// refusingServer is a Metrics Server that refuses one namespace's pods.
type refusingServer struct {
	mu      sync.Mutex
	refused string
	asked   []string // the namespaces of its lists and gets, in order
}

func (f *refusingServer) ID() string                         { return metrics.ServerID }
func (f *refusingServer) Label() string                      { return metrics.ServerLabel }
func (f *refusingServer) Capabilities() metrics.Capabilities { return metrics.Capabilities{} }

func (f *refusingServer) answer(verb, ns string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, ns)
	if ns != f.refused {
		return nil
	}
	return apierrors.NewForbidden(schema.GroupResource{Group: "metrics.k8s.io", Resource: "pods"}, "",
		errors.New(`User "dev" cannot `+verb+` resource "pods" in API group "metrics.k8s.io" in the namespace "`+ns+`"`))
}

func (f *refusingServer) ListPods(_ context.Context, ns string) ([]model.PodUsage, error) {
	if err := f.answer("list", ns); err != nil {
		return nil, err
	}
	return []model.PodUsage{{Namespace: ns, Name: "web-0", Containers: []model.ContainerUsage{{Name: "app", Usage: model.Usage{CPUMilli: 100}}}}}, nil
}

func (f *refusingServer) GetPod(_ context.Context, ns, name string) (model.PodUsage, error) {
	if err := f.answer("get", ns); err != nil {
		return model.PodUsage{}, err
	}
	return model.PodUsage{Namespace: ns, Name: name, Containers: []model.ContainerUsage{{Name: "app", Usage: model.Usage{CPUMilli: 100}}}}, nil
}

func (f *refusingServer) ListNodes(context.Context) ([]model.NodeUsage, error) { return nil, nil }
func (f *refusingServer) GetNode(context.Context, string) (model.NodeUsage, error) {
	return model.NodeUsage{}, metrics.ErrNoData
}

func TestPodMetricsRefusalStaysInItsNamespace(t *testing.T) {
	clk := &testClock{t: time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)}
	live := &refusingServer{refused: "team-b"}
	hist := &histFake{clk: clk}
	liveDet, histDet := detectorFor(live), detectorFor(hist)
	h := metrics.NewHubFor(metrics.Sources{
		Live:    identified{liveDet, metrics.ServerID, metrics.ServerLabel},
		History: identified{histDet, metrics.PrometheusID, metrics.PrometheusLabel},
	}, metrics.Options{ListInterval: time.Hour, ObjectInterval: time.Hour, Now: clk.Now})
	t.Cleanup(h.Close)
	ctx := context.Background()
	teamA, teamB := metrics.Scope{Kind: metrics.KindPods, Namespace: "team-a"}, metrics.Scope{Kind: metrics.KindPods, Namespace: "team-b"}

	// The refused namespace: no values, the state forbidden, naming it; nobody else asked.
	r := h.Fetch(ctx, teamB)
	if r.State.Status != metrics.StatusForbidden || len(r.Pods) != 0 || !strings.Contains(r.State.Message, "team-b") {
		t.Errorf("team-b: %+v, want forbidden for team-b without pods", r)
	}
	if st := h.State(teamB); st.Status != metrics.StatusForbidden {
		t.Errorf("team-b's state %+v", st)
	}
	if _, ok := h.Pod("team-b", "web-0"); ok {
		t.Error("a pod of the refused namespace has values")
	}

	// The other namespace: the Metrics Server's values.
	clk.Advance(20 * time.Second) // past the TTL, within the minute a failed provider used to be left alone
	r = h.Fetch(ctx, teamA)
	if !r.State.Usable() || r.State.Provider != metrics.ServerID || len(r.Pods) != 1 {
		t.Errorf("team-a: %+v, want the Metrics Server's values", r)
	}

	// A drawer of a pod in team-b: its get is refused too, and that refusal stays in team-b as well.
	podB := metrics.Scope{Kind: metrics.KindPods, Namespace: "team-b", Name: "db-0"}
	polls := make(chan int, 8)
	n := 0
	sub := h.Subscribe(podB, func() { n++; polls <- n })
	t.Cleanup(sub.Release)
	waitPoll(t, polls, 1)
	if st := h.State(podB); !st.Usable() || st.Provider != metrics.PrometheusID {
		t.Errorf("team-b's drawer: %+v, want Prometheus's history alone", st)
	}
	if qs := hist.ranges(); len(qs) != 1 || qs[0].Namespace != "team-b" {
		t.Errorf("Prometheus's range queries %+v, want the drawer's history once, no live values in its place", qs)
	}
	clk.Advance(20 * time.Second) // past the TTL, within the minute a failed provider used to be left alone
	r = h.Fetch(ctx, teamA)
	if !r.State.Usable() || r.State.Provider != metrics.ServerID {
		t.Errorf("team-a after team-b's drawer: %+v, want the Metrics Server's values", r)
	}

	// The provider's status stays as it was, and Prometheus never stood in for the current values.
	if hist.instantCount() != 0 {
		t.Errorf("Prometheus was asked %d times for current values", hist.instantCount())
	}
	ms := provider(h.Providers(), metrics.ServerID)
	if ms.Status != metrics.StatusAvailable || ms.LastError != "" || !ms.LastErrorAt.IsZero() {
		t.Errorf("the Metrics Server's status %+v, want available without a failure", ms)
	}
	live.mu.Lock()
	asked := strings.Join(live.asked, ",")
	live.mu.Unlock()
	if asked != "team-b,team-a,team-b,team-a" {
		t.Errorf("the Metrics Server was asked for %s", asked)
	}
}

// With the Metrics Server alone, a drawer of a pod in the refused namespace reads forbidden for
// that namespace, and the lists of the others keep their values.
func TestPodMetricsRefusalWithoutPrometheus(t *testing.T) {
	live := &refusingServer{refused: "team-b"}
	h := metrics.NewHub(metrics.Fixed(live), metrics.Options{ListInterval: time.Hour, ObjectInterval: time.Hour})
	t.Cleanup(h.Close)
	podB := metrics.Scope{Kind: metrics.KindPods, Namespace: "team-b", Name: "db-0"}
	polls := make(chan int, 8)
	n := 0
	sub := h.Subscribe(podB, func() { n++; polls <- n })
	t.Cleanup(sub.Release)
	waitPoll(t, polls, 1)
	if st := h.State(podB); st.Status != metrics.StatusForbidden || !strings.Contains(st.Message, "team-b") {
		t.Errorf("team-b's drawer: %+v, want forbidden for team-b", st)
	}
	if r := h.Fetch(context.Background(), metrics.Scope{Kind: metrics.KindPods, Namespace: "team-a"}); !r.State.Usable() || len(r.Pods) != 1 {
		t.Errorf("team-a: %+v", r)
	}
	if ms := provider(h.Providers(), metrics.ServerID); ms.Status != metrics.StatusAvailable || ms.LastError != "" {
		t.Errorf("the Metrics Server's status %+v", ms)
	}
}
