//go:build darwin || linux

package commands_test

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The window's Cancel Connecting and an agent alike (D-73, D-2: it stops only what the user's own
// connect started): cluster.disconnect of a connect that waits for its credential plugin returns
// at once, the plugin's process ends, and the cluster is disconnected.
func TestCancelConnectingFromTheWindowAndAnAgent(t *testing.T) {
	const token = "cancel-connect-token-0042"
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: token})
	defer srv.Close()
	p := testkit.NewLoginPlugin(t, srv, "oidc")
	p.Give(token, time.Time{})
	p.Block(true)
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{KubeconfigPaths: []string{p.Kubeconfig}}})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	h := &harness{s: s}
	for _, origin := range []engine.Origin{engine.OriginGUI, engine.OriginExternalAgent, engine.OriginBuiltInAgent} {
		before := p.Runs()
		done := make(chan error, 1)
		go func() { done <- s.Connect(context.Background(), "oidc") }()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if !testkit.WaitFor(ctx, func() bool {
			c := s.UIState().Connection
			return c.State == engine.Connecting && c.WaitingForLogin.Program == "login-plugin" && p.Runs() > before
		}) {
			cancel()
			t.Fatalf("%s: the connect never waited for the plugin: %+v", origin, s.UIState().Connection)
		}
		pid := p.LastPID()
		var res engine.UIState
		start := time.Now()
		h.must(t, origin, "cluster.disconnect", nil, &res)
		if took := time.Since(start); took > 5*time.Second {
			t.Errorf("%s: cluster.disconnect took %s", origin, took)
		}
		if res.Connection.State != engine.Disconnected {
			t.Errorf("%s: the state after the cancel: %+v", origin, res.Connection)
		}
		if !testkit.WaitFor(ctx, func() bool { return testkit.ProcessGone(pid) }) {
			t.Errorf("%s: the plugin's process %d still runs", origin, pid)
		}
		select {
		case _ = <-done:
		case <-ctx.Done():
			t.Fatalf("%s: the cancelled connect did not return", origin)
		}
		cancel()
	}
}
