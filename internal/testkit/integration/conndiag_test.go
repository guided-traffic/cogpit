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

// Connection Diagnostics (D-133) on a cluster as a strongly limited user meets it: the user may
// read pods in team-a alone, the server does not stream initial lists (no WatchList), and one
// aggregated API is down. The log holds the refused read of pods in all namespaces, the kind read
// namespace by namespace after it, and the broken group's discovery; the reads are listed, as
// Cogpit reads by default (D-new), so no line says a stream was refused; the report has the
// server's address replaced.
func TestConnectionDiagnosticsOfALimitedUser(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	read := []string{"get", "list", "watch"}
	access := &testkit.FakeAccess{Enforce: true, Rules: map[string][]testkit.FakeRule{
		"team-a": {{Verbs: read, APIGroups: []string{""}, Resources: []string{"pods"}}},
	}}
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Access: access, DisableWatchList: true, BrokenGroups: []string{"metrics.k8s.io"}})
	t.Cleanup(srv.Close)
	for _, name := range []string{"web-0", "web-1"} {
		srv.Set(testkit.PodsGVR, testkit.Pod("team-a", name))
	}
	srv.Set(testkit.PodsGVR, testkit.Pod("team-b", "other"))
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
		return ts != nil && ts.Synced && len(ts.Rows) == 2
	}) {
		t.Fatalf("the pods of team-a did not show: %+v", s.Table("pods"))
	}

	var r engine.DiagnosticsReport
	has := func(kind cluster.DiagKind, what string) bool {
		return slices.ContainsFunc(r.Log, func(e cluster.DiagEntry) bool { return e.Kind == kind && strings.Contains(e.What, what) })
	}
	if !testkit.WaitFor(ctx, func() bool {
		res, err := s.Dispatch(ctx, engine.Invocation{ID: "diagnostics.get", Params: json.RawMessage(`{}`), Origin: gui})
		if err != nil {
			t.Fatal(err)
		}
		r = res.(engine.DiagnosticsReport)
		return has(cluster.DiagRefused, "list pods") && has(cluster.DiagNamespaced, "pods") &&
			has(cluster.DiagServerError, "/apis/metrics.k8s.io")
	}) {
		t.Fatalf("log = %+v", r.Log)
	}
	refused := r.Log[slices.IndexFunc(r.Log, func(e cluster.DiagEntry) bool { return e.Kind == cluster.DiagRefused })]
	if refused.Status != 403 || refused.Method != "GET" || !strings.HasPrefix(refused.URL, srv.URL+"/api/v1/pods") || refused.Text == "" {
		t.Errorf("refused line = %+v", refused)
	}
	if has(cluster.DiagFallback, "") {
		t.Errorf("a fallback line where nothing was streamed: %+v", r.Log)
	}
	if r.Problems == 0 || r.Server == "" || r.Since.IsZero() || r.Streaming.Listed == 0 || r.Streaming.FellBack != 0 || r.Streaming.Streamed != 0 {
		t.Errorf("report = problems %d, server %q, since %v, streaming %+v", r.Problems, r.Server, r.Since, r.Streaming)
	}
	if !slices.ContainsFunc(r.Budgets, func(b cluster.BudgetStats) bool {
		return b.Name == cluster.BudgetRequests && b.QPS == 5 && b.Requests > 0
	}) {
		t.Errorf("budgets = %+v", r.Budgets)
	}
	// The status bar's count is the report's. The log may still take a line between two reads (a
	// retried refusal, another namespace's fallback: CI's race job, 2026-10-26, 4 against 3), so
	// both are read until a pair of reads agrees; counts that differ by design never would.
	var bar, report int
	if !testkit.WaitFor(ctx, func() bool {
		res, err := s.Dispatch(ctx, engine.Invocation{ID: "diagnostics.get", Params: json.RawMessage(`{}`), Origin: gui})
		if err != nil {
			t.Fatal(err)
		}
		report = res.(engine.DiagnosticsReport).Problems
		bar = s.Diagnostics().Problems
		return bar == report
	}) {
		t.Errorf("the status bar's count = %d, the report's %d", bar, report)
	}

	res, err := s.Dispatch(ctx, engine.Invocation{ID: "diagnostics.report", Params: json.RawMessage(`{}`), Origin: engine.OriginExternalAgent})
	if err != nil {
		t.Fatal(err)
	}
	rep := res.(engine.DiagnosticsReportText)
	host := strings.TrimPrefix(srv.URL, "https://")
	if strings.Contains(rep.Text, host) || strings.Contains(rep.Text, "127.0.0.1") {
		t.Errorf("the report keeps the server's address %s:\n%s", host, rep.Text)
	}
	for _, want := range []string{"Cogpit Connection Diagnostics", "https://ip-1/api/v1/pods", "refused", "team-a",
		"listed after the server did not stream them", "requests: 5 per second, burst 10"} {
		if !strings.Contains(rep.Text, want) {
			t.Errorf("report misses %q:\n%s", want, rep.Text)
		}
	}
	if !strings.HasPrefix(rep.Name, "cogpit-connection-diagnostics-") || !strings.HasSuffix(rep.Name, ".txt") {
		t.Errorf("name = %q", rep.Name)
	}
}
