package integration_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// An API server that answers the first watch of a resource 429 "storage is (re)initializing", as
// every API server does while it builds its cache of the resource (a custom resource's at the first
// request for it; the owner's staging cluster, 2026-10-26): the view shows the objects after
// client-go's retry, Connection Diagnostics has a grey "cache warming" line, and nothing counts as a
// problem (D-new, the owner's option a).
func TestCacheWarmingIsNoProblem(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{WarmingCaches: []string{"pods"}})
	t.Cleanup(srv.Close)
	srv.Set(testkit.PodsGVR, testkit.Pod("team-a", "web-0"))
	s := engine.New(engine.Options{
		Connector:       &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "team-a")}},
		ClusterSettings: engine.NewClusterSettingsStore(t.TempDir()),
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	decideIntegrations(t, s, "fake")
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	gui := engine.OriginGUI
	if _, err := s.Dispatch(ctx, engine.Invocation{ID: "ui.openView", Params: json.RawMessage(`{"view":"pods"}`), Origin: gui}); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool {
		ts := s.Table("pods")
		return ts != nil && ts.Synced && len(ts.Rows) == 1
	}) {
		t.Fatalf("the pods did not show: %+v", s.Table("pods"))
	}
	res, err := s.Dispatch(ctx, engine.Invocation{ID: "diagnostics.get", Params: json.RawMessage(`{}`), Origin: gui})
	if err != nil {
		t.Fatal(err)
	}
	r := res.(engine.DiagnosticsReport)
	i := slices.IndexFunc(r.Log, func(e cluster.DiagEntry) bool { return e.Kind == cluster.DiagWarming })
	if i < 0 {
		t.Fatalf("no cache warming line: %+v", r.Log)
	}
	if w := r.Log[i]; w.Problem || w.Count != 1 || w.Status != 429 || w.RetryAfter != "1" || !strings.HasPrefix(w.What, "watch pods") {
		t.Errorf("cache warming line = %+v", w)
	}
	if slices.ContainsFunc(r.Log, func(e cluster.DiagEntry) bool { return e.Kind == cluster.DiagTooMany }) {
		t.Errorf("a cache warming up shows as too many requests: %+v", r.Log)
	}
	if r.Problems != 0 || s.Diagnostics().Problems != 0 {
		t.Errorf("problems: report %d, status bar %d; a cache warming up is none", r.Problems, s.Diagnostics().Problems)
	}
	res, err = s.Dispatch(ctx, engine.Invocation{ID: "diagnostics.report", Params: json.RawMessage(`{}`), Origin: gui})
	if err != nil {
		t.Fatal(err)
	}
	if text := res.(engine.DiagnosticsReportText).Text; !strings.Contains(text, "cache warming") {
		t.Errorf("the report has no cache warming line:\n%s", text)
	}
}
