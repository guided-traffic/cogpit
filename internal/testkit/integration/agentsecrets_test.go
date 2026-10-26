package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/scorecard"
)

// TestBudgetAgentReadTakesTheSecrets measures, on the real client-go stack against the fake API
// server, what the owner's option b for H-82 costs (2026-10-14): the window's read of a pod costs
// no Secrets request; an agent's first read takes the Secrets (one streaming watch) and reads the
// value of a Secret that a pod's annotation repeats as its placeholder; a second agent read within
// the warm period costs no request.
func TestBudgetAgentReadTakesTheSecrets(t *testing.T) {
	const pass = "agent-budget-pass-4711"
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "budget-token",
		Resources: append(testkit.DefaultFakeResources, testkit.FakeResource{GVR: testkit.SecretsGVR, Kind: "Secret", Namespaced: true})})
	t.Cleanup(srv.Close)
	pod := testkit.Pod("shop", "web-0")
	pod.SetAnnotations(map[string]string{"debug/dsn": "postgres://app:" + pass + "@db/app"})
	srv.Set(testkit.PodsGVR, pod)
	srv.Set(testkit.SecretsGVR, testkit.Secret("shop", "db", nil, map[string]string{"password": pass}))
	s := engine.New(engine.Options{
		Connector:       &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}},
		ClusterSettings: engine.NewClusterSettingsStore(t.TempDir()),
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	get := func(origin engine.Origin) string {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "web-0"}})
		res, err := s.Dispatch(ctx, engine.Invocation{ID: "object.get", Params: raw, Origin: origin})
		if err != nil {
			t.Fatalf("object.get (%s): %v", origin, err)
		}
		b, _ := json.Marshal(res)
		return string(b)
	}
	secretReads := func() int64 {
		var n int64
		for k, v := range srv.Requests() {
			if strings.HasSuffix(k, " secrets") {
				n += v
			}
		}
		return n
	}

	if got := get(engine.OriginGUI); !strings.Contains(got, pass) {
		t.Fatalf("the window's read: %s", got)
	}
	scorecard.Record(t, scorecard.Measurement{Budget: "P16", Metric: "Secrets requests of the window's read of a pod", Value: float64(secretReads()), Unit: "requests"})
	if n := secretReads(); n != 0 {
		t.Fatalf("the window's read cost %d Secrets requests", n)
	}
	start := time.Now()
	got := get(engine.OriginExternalAgent)
	took := time.Since(start)
	if strings.Contains(got, pass) || !strings.Contains(got, "«secret shop/db/password»") {
		t.Fatalf("the agent's first read: %s", got)
	}
	first := secretReads()
	if first < 1 || first > 2 {
		t.Errorf("the agent's first read cost %d Secrets requests, want a list and a watch (or one streaming watch)", first)
	}
	if got := get(engine.OriginExternalAgent); strings.Contains(got, pass) {
		t.Fatalf("the agent's second read: %s", got)
	}
	if n := secretReads(); n != first {
		t.Errorf("the second agent read within the warm period cost %d more Secrets requests", n-first)
	}
	how := "the real client-go stack against the fake API server over HTTPS; Cogpit lists and then watches (D-135), streaming switched off by default"
	scorecard.Record(t, scorecard.Measurement{Budget: "P16", Metric: "Secrets requests of an agent's first read of a pod", Value: float64(first), Unit: "requests", Limit: 2, How: how})
	scorecard.Record(t, scorecard.Measurement{Budget: "P16", Metric: "Secrets requests of the second read within the warm period", Value: float64(secretReads() - first), Unit: "requests", How: how})
	t.Logf("H-82 option b: the first agent read took %v and %d Secrets request(s); requests %v", took, first, srv.Requests())
}
