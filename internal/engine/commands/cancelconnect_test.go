package commands_test

import (
	"context"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// An agent's Cancel Connecting as the window's (D-73, D-2: it stops only what the user's own connect
// started): cluster.disconnect of a connect that waits for its credential plugin returns at once,
// the plugin's process ends, and the cluster is disconnected. The command is the same for every
// origin (DangerUI); the window's own Disconnect of a waiting login is
// engine.TestCancelConnectingEndsTheLogin, its cluster.disconnect of a waiting connect
// TestConnectCommandsAnswerACancel.
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
	done := make(chan error, 1)
	go func() { done <- s.Connect(context.Background(), "oidc") }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		c := s.UIState().Connection
		return c.State == engine.Connecting && c.WaitingForLogin.Program == "login-plugin" && p.Runs() > 0
	}) {
		t.Fatalf("the connect never waited for the plugin: %+v", s.UIState().Connection)
	}
	pid := p.LastPID()
	var res engine.UIState
	start := time.Now()
	h.must(t, engine.OriginExternalAgent, "cluster.disconnect", nil, &res)
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("cluster.disconnect took %s", took)
	}
	if res.Connection.State != engine.Disconnected {
		t.Errorf("the state after the cancel: %+v", res.Connection)
	}
	if !testkit.WaitFor(ctx, func() bool { return testkit.ProcessGone(pid) }) {
		t.Errorf("the plugin's process %d still runs", pid)
	}
	select {
	case _ = <-done:
	case <-ctx.Done():
		t.Fatal("the cancelled connect did not return")
	}
}
