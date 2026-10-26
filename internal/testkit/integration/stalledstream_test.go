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

// A server whose streamed initial lists never end (D-new; the OpenShift cluster of 2026-10-26 ended
// them after a minute and more): the first read lists after the stall bound, every read after it
// lists without asking for a stream, and Connection Diagnostics says so once.
func TestStalledStreamsList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{StallWatchList: true})
	t.Cleanup(srv.Close)
	srv.Set(testkit.PodsGVR, testkit.Pod("team-a", "web-0"))
	srv.Set(testkit.ServicesGVR, testkit.Service("team-a", "web", "10.0.0.1"))
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
	open := func(view string) {
		t.Helper()
		start := time.Now()
		if _, err := s.Dispatch(ctx, engine.Invocation{ID: "ui.openView", Params: json.RawMessage(`{"view":"` + view + `"}`), Origin: gui}); err != nil {
			t.Fatal(err)
		}
		if !testkit.WaitFor(ctx, func() bool {
			ts := s.Table(view)
			return ts != nil && ts.Synced && len(ts.Rows) == 1
		}) {
			t.Fatalf("%s did not show: %+v", view, s.Table(view))
		}
		t.Logf("%s shown after %v", view, time.Since(start).Round(time.Millisecond))
	}
	open("pods")
	before := srv.Requests()["watchlist services"]
	open("services")
	if n := srv.Requests()["watchlist services"] - before; n != 0 {
		t.Errorf("services asked for %d streams after a stall", n)
	}

	res, err := s.Dispatch(ctx, engine.Invocation{ID: "diagnostics.get", Params: json.RawMessage(`{}`), Origin: gui})
	if err != nil {
		t.Fatal(err)
	}
	r := res.(engine.DiagnosticsReport)
	stalls := slices.DeleteFunc(slices.Clone(r.Log), func(e cluster.DiagEntry) bool {
		return e.Kind != cluster.DiagFallback || !strings.Contains(e.Text, "stalled")
	})
	if len(stalls) != 1 || !strings.Contains(stalls[0].Text, "every read of this connection lists from now on") {
		t.Errorf("stall lines = %+v; log = %+v", stalls, r.Log)
	}
	if !r.Streaming.Off || r.Streaming.Streamed != 0 {
		t.Errorf("streaming = %+v", r.Streaming)
	}
	res, err = s.Dispatch(ctx, engine.Invocation{ID: "diagnostics.report", Params: json.RawMessage(`{}`), Origin: gui})
	if err != nil {
		t.Fatal(err)
	}
	if text := res.(engine.DiagnosticsReportText).Text; !strings.Contains(text, "streams off: a stream stalled, every read lists") {
		t.Errorf("the report does not say the streams are off:\n%s", text)
	}
}
