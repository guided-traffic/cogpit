package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A connect cancelled by Disconnect (the user's Cancel Connecting, or an agent's cluster_disconnect)
// and one superseded before it began are no failures (the owner's AW15 of 2026-10-22): an agent
// reads them as cancelled, the cancel's text telling it not to connect again on its own, never as
// "failed".
func TestConnectOutcomesAreNoFailures(t *testing.T) {
	reg := engine.NewRegistry()
	if err := engine.Register(reg, engine.Spec[struct{}]{
		ID: "cluster.connect", Label: "Connect to Cluster", Danger: engine.DangerUI, Description: "Connects.",
		Run: func(context.Context, *engine.Session, struct{}) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	cmd, _ := reg.Get("cluster.connect")
	for _, c := range []struct {
		err  error
		says string
	}{
		{fmt.Errorf("connect walk-login-hang: %w", engine.ErrConnectCancelled), "its connect was cancelled"},
		{fmt.Errorf("connect walk-login-hang: %w", engine.ErrConnectSuperseded), "another cluster was chosen"},
	} {
		res := failure(cmd, nil, c.err)
		code := res.StructuredContent.(map[string]string)["error"] // as the server holds it, before the wire
		if got := text(res); !res.IsError || code != codeCancelled || !strings.Contains(got, c.says) || strings.Contains(got, "failed:") {
			t.Errorf("%v: isError %v, code %q, text %q", c.err, res.IsError, code, got)
		}
	}
}
