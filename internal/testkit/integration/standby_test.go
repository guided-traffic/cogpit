package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// standbyWatch are D-58's intervals in tenths: a heartbeat every 1.5 s with 0.5 s to answer.
var standbyWatch = engine.ConnWatchOptions{
	Heartbeat: 1500 * time.Millisecond, ProbeTimeout: 500 * time.Millisecond, Confirm: 200 * time.Millisecond,
	LoginWait: 3 * time.Second, BackoffFirst: 100 * time.Millisecond, BackoffMax: 800 * time.Millisecond,
}

// TestStandbyFrozenControlPlane is D-58's (e) on the real client-go stack: the engine connected
// through cluster.Connect to the fake API server behind a proxy that goes silent, as a cluster does
// for a computer that slept. Frozen, the connection is Interrupted within the heartbeat window, the
// view keeps its rows, every write is refused at once, the informers wait instead of hammering the
// proxy; thawed, it is Connected again within the backoff and the cache catches up with what
// changed meanwhile.
func TestStandbyFrozenControlPlane(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "standby-token"})
	defer srv.Close()
	srv.Set(testkit.PodsGVR, testkit.Pod("shop", "web-0"))
	srv.Set(testkit.PodsGVR, testkit.Pod("shop", "web-1"))
	proxy := testkit.NewFreezableProxy(t, strings.TrimPrefix(srv.URL, "https://"))
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, proxy.ProxyKubeconfig(srv, "fake", ""), 0o600); err != nil {
		t.Fatal(err)
	}
	s := engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{path}},
		SnapshotInterval: 20 * time.Millisecond, ConnWatch: standbyWatch,
	})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	rows := func() int {
		if tbl := s.Table("pods"); tbl != nil && tbl.Synced {
			return len(tbl.Rows)
		}
		return -1
	}
	if !testkit.WaitFor(ctx, func() bool { return rows() == 2 }) {
		t.Fatalf("pods: %d", rows())
	}

	proxy.Freeze()
	frozen := time.Now()
	if !testkit.WaitFor(ctx, func() bool { return s.UIState().Connection.State == engine.Interrupted }) {
		t.Fatalf("still %s", s.UIState().Connection.State)
	}
	// Two heartbeats: the next one, its timeout, the confirmation's wait and timeout.
	window := standbyWatch.Heartbeat + 2*standbyWatch.ProbeTimeout + standbyWatch.Confirm + 2*time.Second
	if d := time.Since(frozen); d > window {
		t.Errorf("interrupted after %v, want within %v", d, window)
	}
	c := s.UIState().Connection
	if c.Since.IsZero() || c.Reason == "" {
		t.Errorf("connection %+v", c)
	}
	if rows() != 2 || s.Table("pods").Error != "" {
		t.Errorf("the view while interrupted: %d rows, error %q", rows(), s.Table("pods").Error)
	}
	// Writes are refused at once, for every origin.
	pod := model.ObjectRef{Context: "fake", Kind: "pods", Namespace: "shop", Name: "web-0"}
	raw, _ := json.Marshal(map[string]any{"ref": pod})
	for _, o := range []engine.Origin{engine.OriginGUI, engine.OriginExternalAgent} {
		start := time.Now()
		_, err := s.Dispatch(ctx, engine.Invocation{ID: "object.delete", Params: raw, Origin: o})
		var de *engine.DisabledError
		if !errors.As(err, &de) || de.Reason != "connection interrupted" || time.Since(start) > 500*time.Millisecond {
			t.Errorf("%s's delete while interrupted: %v after %v", o, err, time.Since(start))
		}
	}
	// What changes meanwhile shows once the connection is back.
	srv.Set(testkit.PodsGVR, testkit.Pod("shop", "web-2"))
	accepts := proxy.Accepts()
	time.Sleep(2 * time.Second)
	// The probes dial (one per attempt, with the backoff); the informers wait at the gate.
	if n := proxy.Accepts() - accepts; n > 8 {
		t.Errorf("%d connections in 2 s of an interruption", n)
	}

	proxy.Thaw()
	thawed := time.Now()
	if !testkit.WaitFor(ctx, func() bool { return s.UIState().Connection.State == engine.Connected }) {
		t.Fatalf("still %s after the thaw", s.UIState().Connection.State)
	}
	if d := time.Since(thawed); d > standbyWatch.BackoffMax+standbyWatch.ProbeTimeout*2+time.Second {
		t.Errorf("reconnected after %v", d)
	}
	if !testkit.WaitFor(ctx, func() bool { return rows() == 3 }) {
		t.Fatalf("pods after the reconnect: %d", rows())
	}
	srv.Set(testkit.PodsGVR, testkit.Pod("shop", "web-3")) // the watch works again
	if !testkit.WaitFor(ctx, func() bool { return rows() == 4 }) {
		t.Fatalf("pods after a change: %d", rows())
	}
	if _, err := s.Dispatch(ctx, engine.Invocation{ID: "object.delete", Params: raw, Origin: engine.OriginCLI}); err != nil &&
		errors.As(err, new(*engine.DisabledError)) {
		t.Errorf("a write after the reconnect is still disabled: %v", err)
	}
}

// TestStandbyRevokedLogin is S6 on the real stack: a token the cluster stops accepting makes the
// login expired after the guard's one renewal, writes refused, and no further request (no plugin
// run) until the user logs in.
func TestStandbyRevokedLogin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "standby-token"})
	defer srv.Close()
	srv.Set(testkit.PodsGVR, testkit.Pod("shop", "web-0"))
	watch := standbyWatch
	watch.Heartbeat = 500 * time.Millisecond // nothing freezes here: the heartbeats only have to come
	s := engine.New(engine.Options{
		Connector:        &engine.ClusterConnector{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}},
		SnapshotInterval: 20 * time.Millisecond, ConnWatch: watch,
	})
	defer s.Close()
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(ctx, "fake"); err != nil {
		t.Fatal(err)
	}
	srv.SetToken("rotated")
	if !testkit.WaitFor(ctx, func() bool { return s.UIState().Connection.State == engine.LoginExpired }) {
		t.Fatalf("still %s", s.UIState().Connection.State)
	}
	before := srv.Requests()["discovery"]
	time.Sleep(3 * watch.Heartbeat)
	if after := srv.Requests()["discovery"]; after != before {
		t.Errorf("%d probes while the login expired", after-before)
	}
	srv.SetToken("standby-token") // the user logged in elsewhere; Log in again checks it
	if err := s.Login(); err != nil {
		t.Fatal(err)
	}
	if !testkit.WaitFor(ctx, func() bool { return s.UIState().Connection.State == engine.Connected }) {
		t.Fatalf("still %s after logging in", s.UIState().Connection.State)
	}
}
