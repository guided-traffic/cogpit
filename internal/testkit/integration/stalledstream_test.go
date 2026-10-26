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
// lists without asking for a stream, and since the list answered at once Cogpit switches the
// cluster's streams off in its settings: the next connect lists from the start. Switched back to
// Automatic, the connect after it streams again.
func TestStalledStreamsList(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{StallWatchList: true})
	t.Cleanup(srv.Close)
	srv.Set(testkit.PodsGVR, testkit.Pod("team-a", "web-0"))
	srv.Set(testkit.ServicesGVR, testkit.Service("team-a", "web", "10.0.0.1"))
	store := engine.NewClusterSettingsStore(t.TempDir())
	const stall = 300 * time.Millisecond
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
	gui := engine.OriginGUI
	dispatch := func(id, params string) any {
		t.Helper()
		res, err := s.Dispatch(ctx, engine.Invocation{ID: id, Params: json.RawMessage(params), Origin: gui})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return res
	}
	open := func(view string) time.Duration {
		t.Helper()
		start := time.Now()
		dispatch("ui.openView", `{"view":"`+view+`"}`)
		if !testkit.WaitFor(ctx, func() bool {
			ts := s.Table(view)
			return ts != nil && ts.Synced && len(ts.Rows) == 1
		}) {
			t.Fatalf("%s did not show: %+v", view, s.Table(view))
		}
		return time.Since(start)
	}
	streams := func(resource string) int64 { return srv.Requests()["watchlist "+resource] }
	streamsOff := func() *engine.StreamsOff {
		cs, _ := s.ClusterSettingsOf("fake")
		return cs.StreamsOff
	}
	reconnect := func() {
		t.Helper()
		if err := s.Disconnect(); err != nil {
			t.Fatal(err)
		}
		if err := s.Connect(ctx, "fake"); err != nil {
			t.Fatal(err)
		}
	}

	if took := open("pods"); took < stall {
		t.Errorf("pods shown after %v, before the stall bound %v", took, stall)
	}
	before := streams("services")
	open("services")
	if n := streams("services") - before; n != 0 {
		t.Errorf("services asked for %d streams after a stall", n)
	}
	if !testkit.WaitFor(ctx, func() bool { return streamsOff() != nil }) {
		t.Fatal("the streams were not switched off after a stall whose list answered at once")
	}
	if off := streamsOff(); off.By != engine.StreamsOffByCogpit || !strings.Contains(off.Why, "a list answered in") {
		t.Errorf("streams off = %+v", off)
	}
	r := dispatch("diagnostics.get", `{}`).(engine.DiagnosticsReport)
	stalls := slices.DeleteFunc(slices.Clone(r.Log), func(e cluster.DiagEntry) bool {
		return e.Kind != cluster.DiagFallback || !strings.Contains(e.Text, "stalled")
	})
	if len(stalls) != 1 || !strings.Contains(stalls[0].Text, "streams are off for this cluster from now on") {
		t.Errorf("stall lines = %+v; log = %+v", stalls, r.Log)
	}
	if r.Streaming.Off == "" || r.Streaming.Streamed != 0 {
		t.Errorf("streaming = %+v", r.Streaming)
	}
	text := dispatch("diagnostics.report", `{}`).(engine.DiagnosticsReportText).Text
	if !strings.Contains(text, "streams off, every read lists: Cogpit switched them off after a stream stalled (Cluster Settings)") {
		t.Errorf("the report does not say why the streams are off:\n%s", text)
	}

	// The next connect lists from the start: no stream, no wait.
	reconnect()
	before = streams("pods")
	if took := open("pods"); took >= stall {
		t.Errorf("pods shown after %v on a connect that lists", took)
	}
	if n := streams("pods") - before; n != 0 {
		t.Errorf("pods asked for %d streams on a connect that lists", n)
	}

	// Back to Automatic: the next connect streams again.
	dispatch("cluster.settings.set", `{"context":"fake","streamInitialLists":true}`)
	if off := streamsOff(); off != nil {
		t.Fatalf("streams still off: %+v", off)
	}
	reconnect()
	before = streams("pods")
	open("pods")
	if n := streams("pods") - before; n == 0 {
		t.Error("pods asked for no stream after the streams were switched back to Automatic")
	}

	// Switched off by the user: the connected cluster lists at once.
	dispatch("cluster.settings.set", `{"context":"fake","streamInitialLists":false}`)
	if off := streamsOff(); off == nil || off.By != engine.StreamsOffByUser {
		t.Fatalf("streams off = %+v", off)
	}
	if r := dispatch("diagnostics.get", `{}`).(engine.DiagnosticsReport); r.Streaming.Off != "switched off in Cluster Settings" {
		t.Errorf("streaming = %+v", r.Streaming)
	}
}
