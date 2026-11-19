package demo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
)

func TestDemoMetricsServer(t *testing.T) {
	c := New(Options{Pods: 400, NoChurn: true, Seed: 3})
	m := c.Metrics()
	fixed := time.Date(2026, 10, 12, 12, 0, 7, 0, time.UTC)
	m.now = func() time.Time { return fixed }
	ctx := context.Background()

	var _ metrics.Provider = m
	pods, err := m.ListPods(ctx, "")
	if err != nil || len(pods) < 100 {
		t.Fatalf("%d pods measured, err %v", len(pods), err)
	}
	running := 0
	c.mu.Lock()
	for _, p := range c.store[kindPods] {
		if phase, _, _ := nestedStr(p.Object, "status", "phase"); phase == "Running" {
			running++
		}
	}
	c.mu.Unlock()
	if len(pods) > running {
		t.Errorf("%d measured pods, only %d are running: others have no measurement", len(pods), running)
	}
	want := fixed.Truncate(15 * time.Second)
	for _, p := range pods {
		if !p.Timestamp.Equal(want) || len(p.Containers) == 0 {
			t.Fatalf("pod %s/%s: %+v", p.Namespace, p.Name, p)
		}
		for _, ct := range p.Containers {
			if ct.CPUMilli < 1 || ct.MemoryBytes <= 0 {
				t.Fatalf("container %s/%s/%s uses %+v", p.Namespace, p.Name, ct.Name, ct.Usage)
			}
		}
	}

	// Deterministic for a pod and a moment; another moment, another number.
	again, _ := m.GetPod(ctx, pods[0].Namespace, pods[0].Name)
	if again.Total() != pods[0].Total() {
		t.Errorf("%+v then %+v at the same moment", pods[0].Total(), again.Total())
	}
	m.now = func() time.Time { return fixed.Add(5 * time.Minute) }
	later, _ := m.GetPod(ctx, pods[0].Namespace, pods[0].Name)
	if later.Total() == pods[0].Total() {
		t.Error("usage does not move over five minutes")
	}

	if _, err := m.GetPod(ctx, "nope", "nope"); !errors.Is(err, metrics.ErrNoData) {
		t.Errorf("a missing pod: %v, want ErrNoData", err)
	}
	nodes, err := m.ListNodes(ctx)
	if err != nil || len(nodes) == 0 {
		t.Fatalf("%d nodes measured, err %v", len(nodes), err)
	}
	for _, n := range nodes {
		if n.CPUMilli <= 0 || n.MemoryBytes <= 0 {
			t.Errorf("node %s uses %+v", n.Name, n.Usage)
		}
	}
	if m.Requests() < 4 || c.Stats().MetricsRequests != m.Requests() {
		t.Errorf("requests: provider %d, stats %d", m.Requests(), c.Stats().MetricsRequests)
	}
}

func nestedStr(obj map[string]any, fields ...string) (string, bool, error) {
	var cur any = obj
	for _, f := range fields {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false, nil
		}
		cur = m[f]
	}
	s, ok := cur.(string)
	return s, ok, nil
}

// The demo's clock starts at Options.Now: two clusters made with the same Now measure the same
// numbers within the first resolution, whenever they are made, and a measurement's timestamp is
// the computer's time of that measurement, within a resolution before now.
func TestDemoMetricsFollowTheDemosClock(t *testing.T) {
	now := time.Date(2026, 10, 11, 14, 30, 0, 0, time.UTC)
	opts := Options{Pods: 200, NoChurn: true, Seed: 3, Now: now, MetricsResolution: time.Hour}
	ctx := context.Background()
	before := time.Now()
	first, err := New(opts).Metrics().ListPods(ctx, "")
	if err != nil || len(first) == 0 {
		t.Fatalf("%d pods measured, err %v", len(first), err)
	}
	second, _ := New(opts).Metrics().ListPods(ctx, "")
	got := map[string]int64{}
	for _, p := range second {
		got[p.Namespace+"/"+p.Name] = p.Total().CPUMilli + p.Total().MemoryBytes
	}
	for _, p := range first {
		if got[p.Namespace+"/"+p.Name] != p.Total().CPUMilli+p.Total().MemoryBytes {
			t.Fatalf("pod %s/%s measured %+v, then %d", p.Namespace, p.Name, p.Total(), got[p.Namespace+"/"+p.Name])
		}
		if p.Timestamp.After(time.Now()) || p.Timestamp.Before(before.Add(-opts.MetricsResolution)) {
			t.Fatalf("pod %s/%s measured at %v, not the computer's time %v", p.Namespace, p.Name, p.Timestamp, before)
		}
	}
}
