package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// heldConnector is a poolConnector whose named contexts wait at a gate of their own, as a connect
// waits for a login in a browser, until the test opens it; one in fail fails then.
type heldConnector struct {
	*poolConnector
	mu    sync.Mutex
	gates map[string]chan struct{}
	fail  map[string]bool
}

func (c *heldConnector) Connect(ctx context.Context, name string) (*Backend, error) {
	c.mu.Lock()
	gate, fail := c.gates[name], c.fail[name]
	c.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		}
	}
	if fail {
		return nil, errors.New("the login through walk-login.sh did not finish within 5 minutes; Cogpit ended it")
	}
	return c.poolConnector.Connect(ctx, name)
}

// open lets name's connect go on.
func (c *heldConnector) open(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	close(c.gates[name])
}

// runSession connects beta, then starts a connect of alpha that waits at its gate and waits until
// the window shows alpha connecting; the connect's error arrives on the channel.
func runSession(t *testing.T, failAlpha bool) (*Session, *heldConnector, <-chan error, *runToasts) {
	t.Helper()
	hc := &heldConnector{poolConnector: newPoolConnector(time.Minute, "alpha", "beta"),
		gates: map[string]chan struct{}{"alpha": make(chan struct{})}, fail: map[string]bool{"alpha": failAlpha}}
	s := New(Options{Connector: hc, SnapshotInterval: 10 * time.Millisecond, ConnWatch: fastWatch})
	t.Cleanup(s.Close)
	toasts := &runToasts{}
	t.Cleanup(s.Subscribe(toasts.add))
	mustConnect(t, s, "beta")
	errc := make(chan error, 1)
	go func() { errc <- s.Connect(context.Background(), "alpha") }()
	waitUntil(t, "alpha connecting", func() bool {
		c, ok := s.UIState().ConnectionOf("alpha")
		return ok && c.Shown && c.Connection.State == Connecting
	})
	return s, hc, errc, toasts
}

type runToasts struct {
	mu   sync.Mutex
	list []string
}

func (l *runToasts) add(ev Event) {
	if ev.Type == EventToast {
		l.mu.Lock()
		l.list = append(l.list, ev.Message)
		l.mu.Unlock()
	}
}

func (l *runToasts) texts() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.list)
}

// Showing another cluster while a connect waits for its login leaves that connect running in the
// background (WF39): its entry stays connecting, not shown; when it connects, the cluster joins the
// pool as one the window left, a toast says so, and the window stays on the cluster the user chose.
func TestShowingAnotherClusterLeavesTheConnectRunning(t *testing.T) {
	s, hc, errc, toasts := runSession(t, false)
	if err := s.Show("beta"); err != nil {
		t.Fatal(err)
	}
	if err := <-errc; !errors.Is(err, ErrConnectInBackground) {
		t.Fatalf("the left connect returned %v", err)
	}
	u := s.UIState()
	if u.Context != "beta" || u.Connection.State != Connected {
		t.Fatalf("shown %q %s", u.Context, u.Connection.State)
	}
	if c, ok := u.ConnectionOf("alpha"); !ok || c.Shown || c.Connection.State != Connecting {
		t.Fatalf("alpha's entry while it runs: %+v (%v)", c, ok)
	}
	hc.open("alpha")
	waitUntil(t, "alpha connected in the background", func() bool {
		c, ok := s.UIState().ConnectionOf("alpha")
		return ok && !c.Shown && c.Connection.State == Connected
	})
	if u := s.UIState(); u.Context != "beta" {
		t.Errorf("the window left beta for %q", u.Context)
	}
	waitUntil(t, "the toast", func() bool { return slices.Contains(toasts.texts(), "Connected to alpha in the background.") })
	if b := s.BackendOf("alpha"); b == nil {
		t.Fatal("alpha is not in the pool")
	}
	attempts, _ := s.ConnectAttempts("alpha", 1)
	if len(attempts) != 1 || attempts[0].Outcome != OutcomeOK || !strings.Contains(lastLine(attempts[0]), "in the background: another cluster was shown meanwhile") {
		t.Errorf("alpha's attempt %+v", attempts)
	}
	mustConnect(t, s, "alpha") // only shown now
	if n := hc.connectCount("alpha"); n != 1 {
		t.Errorf("alpha connected %d times", n)
	}
}

// A connect left in the background that fails says why in a toast; the window stays where it is,
// and the context has no entry left (WF39, WF40).
func TestALeftConnectThatFailsSaysSo(t *testing.T) {
	s, hc, errc, toasts := runSession(t, true)
	if err := s.Connect(context.Background(), "beta"); err != nil { // connected already: only shown
		t.Fatal(err)
	}
	if err := <-errc; !errors.Is(err, ErrConnectInBackground) {
		t.Fatalf("the left connect returned %v", err)
	}
	hc.open("alpha")
	waitUntil(t, "the toast", func() bool {
		return slices.ContainsFunc(toasts.texts(), func(s string) bool { return strings.HasPrefix(s, "Could not connect to alpha: the login through") })
	})
	waitUntil(t, "alpha's entry gone", func() bool { _, ok := s.UIState().ConnectionOf("alpha"); return !ok })
	if u := s.UIState(); u.Context != "beta" || u.Connection.State != Connected {
		t.Errorf("shown %q %s", u.Context, u.Connection.State)
	}
}

// Connecting a context again whose connect runs in the background shows it connecting and waits
// for that connect, rather than starting a second login.
func TestConnectAgainJoinsTheRun(t *testing.T) {
	s, hc, errc, _ := runSession(t, false)
	if err := s.Show("beta"); err != nil {
		t.Fatal(err)
	}
	<-errc
	again := make(chan error, 1)
	go func() { again <- s.Connect(context.Background(), "alpha") }()
	waitUntil(t, "alpha shown connecting again", func() bool {
		u := s.UIState()
		return u.Context == "alpha" && u.Connection.State == Connecting
	})
	hc.open("alpha")
	if err := <-again; err != nil {
		t.Fatal(err)
	}
	if u := s.UIState(); u.Context != "alpha" || u.Connection.State != Connected || u.View != "pods" {
		t.Errorf("alpha after the joined connect: %q %s view %q", u.Context, u.Connection.State, u.View)
	}
	if n := hc.connectCount("alpha"); n != 1 {
		t.Errorf("alpha connected %d times", n)
	}
	if c, _ := s.UIState().ConnectionOf("beta"); c.Shown || c.Connection.State != Connected {
		t.Errorf("beta %+v", c)
	}
}

// Cancel Connecting of a connect in the background (its menus: cluster.disconnect with its context)
// ends it: its entry goes, its attempt says cancelled, and nothing is said in a toast.
func TestCancelALeftConnect(t *testing.T) {
	s, _, errc, toasts := runSession(t, false)
	if err := s.Show("beta"); err != nil {
		t.Fatal(err)
	}
	<-errc
	if err := s.DisconnectContext("alpha"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.UIState().ConnectionOf("alpha"); ok {
		t.Error("alpha's entry after the cancel")
	}
	attempts, _ := s.ConnectAttempts("alpha", 1)
	if len(attempts) != 1 || attempts[0].Outcome != OutcomeCancelled {
		t.Errorf("alpha's attempt %+v", attempts)
	}
	time.Sleep(50 * time.Millisecond)
	if got := toasts.texts(); len(got) != 0 {
		t.Errorf("toasts %q", got)
	}
	if s.BackendOf("alpha") != nil {
		t.Error("alpha connected after the cancel")
	}
}

// A connect that fails leaves the window on its cluster with the error, never on the cluster shown
// before (WF40, the owner: "Immer"); that one stays connected behind it.
func TestAFailedConnectStaysOnItsCluster(t *testing.T) {
	s, hc, errc, _ := runSession(t, true)
	hc.open("alpha")
	if err := <-errc; err == nil {
		t.Fatal("the connect did not fail")
	}
	u := s.UIState()
	if u.Context != "alpha" || u.Connection.State != ConnectError || !strings.Contains(u.Connection.Error, "did not finish within 5 minutes") {
		t.Errorf("after the failed connect: %q %+v", u.Context, u.Connection)
	}
	if c, ok := u.ConnectionOf("beta"); !ok || c.Shown || c.Connection.State != Connected {
		t.Errorf("beta %+v", c)
	}
	if _, ok := u.ConnectionOf("alpha"); ok {
		t.Error("the failed alpha has an entry")
	}
}

func lastLine(a ConnectAttempt) string {
	if len(a.Lines) == 0 {
		return ""
	}
	return a.Lines[len(a.Lines)-1].Text
}
