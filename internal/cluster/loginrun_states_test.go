//go:build darwin || linux

package cluster

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A connect cancelled while its login runs leaves the run alone when another open connection with
// the same plugin configuration needs it too, and the connect's log says that the login still runs,
// with how long it has run so far; the run goes on for the other connection until that one closes.
// The plugin waits until the test ends it, so the run is under way when the connect gives up.
func TestCancelledConnectLogsASharedLoginThatStillRuns(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	p := newWaitingPlugin(t, srv)
	p.give(secretToken, true) // expired at once: the connect's first request needs a run
	other := p.connect()
	base := p.count()
	p.blocking(true)

	var mu sync.Mutex
	var steps []ConnectStep
	watch := NewLoginWatch(func() {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		c   *Conn
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := Connect(ctx, Options{KubeconfigPaths: []string{p.kconf}, Login: watch,
			Trace: func(s ConnectStep) { mu.Lock(); steps = append(steps, s); mu.Unlock() }})
		done <- result{c, err}
	}()
	wait := waitCtx(t, 10*time.Second)
	if !testkit.WaitFor(wait, func() bool { _, on := watch.Current(); return on && p.count() > base }) {
		t.Fatal("the connect never ran the plugin")
	}
	pid, ends := p.lastPID(), other.loginEndMark()

	cancel()
	var r result
	select {
	case r = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the cancelled connect did not return")
	}
	var ce *ConnectError
	if r.c != nil || !errors.As(r.err, &ce) || ce.Problem != "connecting was cancelled" {
		t.Fatalf("the cancelled connect: %v; want a ConnectError of a cancelled connect", r.err)
	}
	mu.Lock()
	var logged []ConnectStep // the run's steps: timed, unlike the setup's line on the login
	for _, s := range steps {
		if s.Name == StepLogin && s.Took > 0 {
			logged = append(logged, s)
		}
		if strings.Contains(s.Text, "secret-code-0042") {
			t.Errorf("a step holds the plugin's code: %q", s.Text)
		}
	}
	mu.Unlock()
	if len(logged) != 1 || logged[0].Text != "Login: login-plugin still runs" || !logged[0].Failed || logged[0].Took < cancelSettle {
		t.Fatalf("the run's steps: %+v; want one failed step that says login-plugin still runs, after at least %s", logged, cancelSettle)
	}

	// Cogpit counts a run it ends before it signals the run's process, so an unchanged count and a
	// process still there mean the cancelled connect ended nothing (as in
	// TestLoginSharedBetweenConnections).
	if _, on := other.LoginRun(); !on || other.loginEndMark() != ends || gone(pid) {
		t.Fatalf("the run the other connection needs: under way %v, runs ended %d (was %d), its process %d gone %v; want it running on",
			on, other.loginEndMark(), ends, pid, gone(pid))
	}
	other.Close()
	if !testkit.WaitFor(wait, func() bool { return gone(pid) }) {
		t.Fatalf("the plugin's process %d still runs after the last connection closed", pid)
	}
}
