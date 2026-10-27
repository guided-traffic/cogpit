package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

// A credential plugin's run that Cogpit ended at its bound (D-73) is the login's answer whenever
// the heartbeat that waited for it returns: a check after the end meets only the login guard's 401
// (cluster.ErrLoginRejected), which says the API server rejects the login instead of why it was
// not renewed. Each test forces when that answer comes: after the heartbeat's own deadline, after
// LoginWait, as Log in again's first answer.

// boundEnd is the answer of a heartbeat whose plugin's run Cogpit ended at its bound.
var boundEnd = &cluster.LoginEndedError{Program: "login-plugin", Bound: 3 * time.Second, Cause: cluster.ErrLoginTimedOut}

const boundEndReason = "the login through login-plugin did not finish within 3s; Log in again starts it again"

// endsAtBound is a prober whose first heartbeat's plugin run Cogpit ends at its bound, answering
// once the test calls answer; the guard rejects every heartbeat after it.
func endsAtBound() (*fakeProber, func()) {
	block := make(chan struct{})
	p := &fakeProber{block: block, endNext: boundEnd, reject: true}
	return p, func() {
		p.set(func(f *fakeProber) { f.block = nil }) // the heartbeats after it do not wait
		close(block)
	}
}

// The heartbeat answers after its request's deadline (ProbeTimeout and ProbeSlack) while the check
// waits on for its plugin, as a renewal's run that began with the heartbeat and reaches its bound
// does: the check keeps that answer instead of asking again.
func TestAnEndedRunAnsweredAfterTheDeadlineExpiresTheLogin(t *testing.T) {
	overdue := make(chan struct{}, 1)
	gate := func() { // only the first heartbeat's check gets here: every heartbeat waits until answer
		select {
		case overdue <- struct{}{}:
		default:
		}
	}
	overdueGate.Store(&gate)
	t.Cleanup(func() { overdueGate.Store(nil) })
	opts := fastWatch
	opts.LoginWait = time.Hour // the check waits for the plugin until it answers
	p, answer := endsAtBound()
	s := watchedSessionOf(t, opts, p)

	select {
	case <-overdue:
	case <-time.After(10 * time.Second):
		t.Fatal("no check waited past its heartbeat's deadline")
	}
	answer()
	if c := waitState(t, s, LoginExpired); c.Reason != boundEndReason {
		t.Errorf("the reason: %q, want %q", c.Reason, boundEndReason)
	}
}

// The heartbeat's plugin has not answered within LoginWait, so the login expired saying so; its
// run's end at its bound then replaces that reason with its own.
func TestAnEndedRunAnsweredAfterTheLoginWaitSaysSo(t *testing.T) {
	p, answer := endsAtBound()
	s := watchedSessionOf(t, fastWatch, p)
	if c := waitState(t, s, LoginExpired); !strings.Contains(c.Reason, "has not answered") {
		t.Fatalf("the reason while the plugin runs: %q", c.Reason)
	}
	answer()
	waitUntil(t, "the run's end in the reason", func() bool {
		return !strings.Contains(s.UIState().Connection.Reason, "has not answered")
	})
	if c := s.UIState().Connection; c.State != LoginExpired || c.Reason != boundEndReason {
		t.Errorf("after the run's end: %s %q, want %s %q", c.State, c.Reason, LoginExpired, boundEndReason)
	}
}

// Log in again whose plugin run Cogpit ends at its bound leaves the login expired with the bound's
// reason, not "still rejects".
func TestLogInAgainWhoseRunEndedSaysSo(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	p.set(func(f *fakeProber) { f.reject = true })
	rejected := waitState(t, s, LoginExpired).Reason
	p.set(func(f *fakeProber) { f.endNext = boundEnd }) // no heartbeat runs while expired: Log in again's takes it
	if err := s.Login(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "Log in again's answer", func() bool {
		c := s.UIState().Connection
		return c.Reason != rejected && !c.LoggingIn
	})
	if c := s.UIState().Connection; c.State != LoginExpired || c.Reason != boundEndReason {
		t.Errorf("after Log in again: %s %q, want %s %q", c.State, c.Reason, LoginExpired, boundEndReason)
	}
}
