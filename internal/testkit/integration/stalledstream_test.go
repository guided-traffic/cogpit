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

// streamSession connects a session to srv's context "fake" with the given stall bound, the
// session's and the connector's settings in one store.
func streamSession(ctx context.Context, t *testing.T, srv *testkit.FakeAPIServer, stall time.Duration) *engine.Session {
	t.Helper()
	store := engine.NewClusterSettingsStore(t.TempDir())
	s := engine.New(engine.Options{
		Connector: &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "team-a")},
			ClusterSettings: store, StreamStall: stall},
		ClusterSettings: store,
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	decideIntegrations(t, s, "fake")
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	return s
}

func dispatchTo(ctx context.Context, t *testing.T, s *engine.Session, id, params string) any {
	t.Helper()
	res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: json.RawMessage(params), Origin: engine.OriginGUI})
	if err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return res
}

func openView(ctx context.Context, t *testing.T, s *engine.Session, view string) time.Duration {
	t.Helper()
	start := time.Now()
	dispatchTo(ctx, t, s, "ui.openView", `{"view":"`+view+`"}`)
	if !testkit.WaitFor(ctx, func() bool {
		ts := s.Table(view)
		return ts != nil && ts.Synced && len(ts.Rows) == 1
	}) {
		t.Fatalf("%s did not show: %+v", view, s.Table(view))
	}
	return time.Since(start)
}

func reconnect(ctx context.Context, t *testing.T, s *engine.Session) {
	t.Helper()
	if err := s.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
}

// A server whose streamed initial lists never end (D-new; the OpenShift cluster of 2026-10-26 ended
// them after a minute and more). By default Cogpit lists and asks for no stream. Streaming switched
// on (the beta opt-in), the next connect streams: the first read lists after the stall bound, every
// read after it lists without asking for a stream, Connection Diagnostics says so once, and the
// setting stays on. Switched off, the connected cluster lists at once.
func TestStalledStreamsList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{StallWatchList: true})
	t.Cleanup(srv.Close)
	srv.Set(testkit.PodsGVR, testkit.Pod("team-a", "web-0"))
	srv.Set(testkit.ServicesGVR, testkit.Service("team-a", "web", "10.0.0.1"))
	const stall = 300 * time.Millisecond
	s := streamSession(ctx, t, srv, stall)
	streams := func(resource string) int64 { return srv.Requests()["watchlist "+resource] }

	// The default: listed, no stream asked for, nothing to note.
	if took := openView(ctx, t, s, "pods"); took >= stall {
		t.Errorf("pods shown after %v on a connect that lists", took)
	}
	if n := streams("pods"); n != 0 {
		t.Errorf("pods asked for %d streams by default", n)
	}

	// Streaming switched on: from the next connect.
	dispatchTo(ctx, t, s, "cluster.settings.set", `{"context":"fake","streamInitialLists":true}`)
	reconnect(ctx, t, s)
	if took := openView(ctx, t, s, "pods"); took < stall {
		t.Errorf("pods shown after %v, before the stall bound %v", took, stall)
	}
	if streams("pods") == 0 {
		t.Error("pods asked for no stream with streaming on")
	}
	before := streams("services")
	openView(ctx, t, s, "services")
	if n := streams("services") - before; n != 0 {
		t.Errorf("services asked for %d streams after a stall", n)
	}
	r := dispatchTo(ctx, t, s, "diagnostics.get", `{}`).(engine.DiagnosticsReport)
	stalls := slices.DeleteFunc(slices.Clone(r.Log), func(e cluster.DiagEntry) bool {
		return e.Kind != cluster.DiagFallback || !strings.Contains(e.Text, "stalled")
	})
	if len(stalls) != 1 || !strings.Contains(stalls[0].Text, "this connection lists from now on, the next one streams again") {
		t.Errorf("stall lines = %+v; log = %+v", stalls, r.Log)
	}
	if r.Streaming.Off != "for this connection: a stream stalled" {
		t.Errorf("streaming = %+v", r.Streaming)
	}
	if cs, _ := s.ClusterSettingsOf("fake"); !cs.Streaming {
		t.Error("a stall switched streaming off in the settings")
	}

	// Switched off: at once.
	dispatchTo(ctx, t, s, "cluster.settings.set", `{"context":"fake","streamInitialLists":false}`)
	if r := dispatchTo(ctx, t, s, "diagnostics.get", `{}`).(engine.DiagnosticsReport); r.Streaming.Off != "streaming switched off in Cluster Settings" {
		t.Errorf("streaming = %+v", r.Streaming)
	}
}

// Streaming switched on, a server that streams: the reads stream, nothing is noted.
func TestStreamingOptIn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	t.Cleanup(srv.Close)
	srv.Set(testkit.PodsGVR, testkit.Pod("team-a", "web-0"))
	s := streamSession(ctx, t, srv, 0)
	dispatchTo(ctx, t, s, "cluster.settings.set", `{"context":"fake","streamInitialLists":true}`)
	reconnect(ctx, t, s)
	before := srv.Requests()
	openView(ctx, t, s, "pods")
	after := srv.Requests()
	if after["watchlist pods"] == before["watchlist pods"] || after["list pods"] != before["list pods"] {
		t.Errorf("requests %v → %v; want the pods streamed, not listed", before, after)
	}
	r := dispatchTo(ctx, t, s, "diagnostics.get", `{}`).(engine.DiagnosticsReport)
	if r.Streaming.Streamed == 0 || r.Streaming.Off != "" || r.Problems != 0 {
		t.Errorf("streaming = %+v, problems %d", r.Streaming, r.Problems)
	}
}
