package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// connectPaths are the session's ways a click connects a cluster that is not connected: Connect
// (cluster.connect, a hotbar tile's hotbar.openCluster), one of its tabs (ui.tab.select and its
// cycling) and the navigator's › on its row (ui.setNavigatorScope).
var connectPaths = []struct {
	name string
	run  func(s *Session, contextID string) error
}{
	{"connect", func(s *Session, id string) error { return s.Connect(context.Background(), id) }},
	{"tab", func(s *Session, _ string) error { _, err := s.SelectTab(context.Background(), "tab-alpha"); return err }},
	{"navigator", func(s *Session, id string) error {
		_, err := s.SetNavigatorScope(context.Background(), NavScopeCluster(id))
		return err
	}},
}

// pathSession connects beta, then connects alpha, which waits at its gate (a login in a browser),
// through one of connectPaths, and waits until the window shows alpha connecting; the path's
// error arrives on the channel.
func pathSession(t *testing.T, path func(*Session, string) error, failAlpha bool) (*Session, *heldConnector, <-chan error, *runToasts) {
	t.Helper()
	hc := &heldConnector{poolConnector: newPoolConnector(time.Minute, "alpha", "beta"),
		gates: map[string]chan struct{}{"alpha": make(chan struct{})}, fail: map[string]bool{"alpha": failAlpha}}
	s := New(Options{Connector: hc, SnapshotInterval: 10 * time.Millisecond, ConnWatch: fastWatch})
	t.Cleanup(s.Close)
	toasts := &runToasts{}
	t.Cleanup(s.Subscribe(toasts.add))
	mustConnect(t, s, "beta")
	s.UpdateUI(func(u *UIState) { // a tab of alpha, as the last session's tabs come back (D-41)
		u.Tabs = append(u.Tabs, ViewTab{ID: "tab-alpha", Context: "alpha", View: "pods", Title: "Pods - alpha"})
	})
	errc := make(chan error, 1)
	go func() { errc <- path(s, "alpha") }()
	waitUntil(t, "alpha connecting", func() bool {
		u := s.UIState()
		return u.Context == "alpha" && u.Connection.State == Connecting
	})
	return s, hc, errc, toasts
}

// A connect that goes on in the background because another cluster was shown while it waited is
// no failure on any path (the owner's AW16 of 2026-10-22): Connect says ErrConnectInBackground, the
// tab and the navigator answer the state without an error, as cluster.connect answers connecting
// (D-97); the window shows the cluster chosen last.
func TestABackgroundConnectIsNoFailureOnEveryPath(t *testing.T) {
	for _, p := range connectPaths {
		t.Run(p.name, func(t *testing.T) {
			s, _, errc, _ := pathSession(t, p.run, false)
			if err := s.Show("beta"); err != nil {
				t.Fatal(err)
			}
			err := <-errc
			switch {
			case p.name == "connect" && !errors.Is(err, ErrConnectInBackground):
				t.Errorf("Connect: %v, want ErrConnectInBackground", err)
			case p.name != "connect" && err != nil:
				t.Errorf("%s: %v, want the state without an error", p.name, err)
			}
			if err != nil && !ConnectNotFailed(err) {
				t.Errorf("ConnectNotFailed(%v) is false", err)
			}
			if u := s.UIState(); u.Context != "beta" || u.Connection.State != Connected {
				t.Errorf("shown %q %s", u.Context, u.Connection.State)
			}
		})
	}
}

// The user's own Cancel Connecting (Disconnect of the cluster that connects, cluster.disconnect,
// D-73) is no failure on any path either (AW15): each path says ErrConnectCancelled, never "failed"
// and never a superseded connect, and the window is left Disconnected on the cluster.
func TestACancelledConnectIsNoFailureOnEveryPath(t *testing.T) {
	for _, p := range connectPaths {
		t.Run(p.name, func(t *testing.T) {
			s, _, errc, toasts := pathSession(t, p.run, false)
			if err := s.Disconnect(); err != nil {
				t.Fatal(err)
			}
			err := <-errc
			if !errors.Is(err, ErrConnectCancelled) || !ConnectNotFailed(err) {
				t.Errorf("%s after Cancel Connecting: %v, want ErrConnectCancelled", p.name, err)
			}
			if u := s.UIState(); u.Context != "alpha" || u.Connection.State != Disconnected {
				t.Errorf("after the cancel: %q %+v", u.Context, u.Connection)
			}
			if got := toasts.texts(); len(got) != 0 {
				t.Errorf("toasts %q", got)
			}
		})
	}
}

// A connect that fails is a failure on every path, as before: Connect returns its error, the tab
// and the navigator the state with ConnectError (D-97: the window stays on the failed cluster).
func TestAFailedConnectStaysAFailureOnEveryPath(t *testing.T) {
	for _, p := range connectPaths {
		t.Run(p.name, func(t *testing.T) {
			s, hc, errc, _ := pathSession(t, p.run, true)
			hc.open("alpha")
			err := <-errc
			if ConnectNotFailed(err) {
				t.Errorf("ConnectNotFailed(%v) for a failed connect", err)
			}
			if p.name == "connect" && err == nil {
				t.Error("the failed connect returned no error")
			}
			if u := s.UIState(); u.Context != "alpha" || u.Connection.State != ConnectError {
				t.Errorf("after the failure: %q %+v", u.Context, u.Connection)
			}
		})
	}
}

// The window's connect may see its run's end and its own end at the same moment (both channels
// ready, the select picks either): a run that Disconnect cancelled, or whose wait Disconnect
// cancelled once the run had ended as the window's, is still ErrConnectCancelled, without a toast;
// one that ended in the background after another cluster was shown still ErrConnectInBackground,
// never "superseded by a newer connect" (AW15, AW16: it raised an error toast). Many rounds, so that
// the select takes both ways.
func TestAwaitRunAtTheMomentItsRunEnded(t *testing.T) {
	cases := []struct {
		name       string
		cause      error // the window's wait's end: Disconnect, or another connect or show
		run        error // the run's: its connector's error, nil for connected
		endedFirst bool  // the run ended as the window's, before the newer connect, show or Disconnect
		want       error
		toast      string // what the window is told, if anything
	}{
		{"cancelled", errConnectCancelled, errConnectCancelled, false, ErrConnectCancelled, ""},
		{"cancelled once it had connected", errConnectCancelled, nil, true, ErrConnectCancelled, ""},
		{"connected in the background", errConnectSuperseded, nil, false, ErrConnectInBackground, ""},
		{"failed in the background", errConnectSuperseded, errors.New("dial tcp: refused"), false, ErrConnectInBackground, ""},
		{"connected as the window's, then left", errConnectSuperseded, nil, true, ErrConnectInBackground, "Connected to alpha in the background."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for range 50 {
				s := New(Options{})
				toasts := &runToasts{}
				unsubscribe := s.Subscribe(toasts.add)
				st := s.state
				st.mu.Lock()
				st.connectSeq++
				seq := st.connectSeq
				r := st.newRunLocked(context.Background(), "alpha", false)
				r.watchSeq = seq
				if c.endedFirst {
					r.end(st, c.run, nil)
				}
				st.connectSeq++ // the newer connect, show or Disconnect
				if errors.Is(c.cause, errConnectCancelled) && !c.endedFirst {
					r.cancel(errConnectCancelled) // Disconnect cancels a run under way; one that ended is no longer in state.runs
				}
				if !c.endedFirst {
					r.end(st, c.run, nil) // it ended after that: nobody waited for it
				}
				st.mu.Unlock()
				close(r.done)
				cctx, cancel := context.WithCancelCause(context.Background())
				cancel(c.cause)
				err := s.awaitRun(cctx, r, seq)
				unsubscribe()
				s.Close()
				if !errors.Is(err, c.want) {
					t.Fatalf("awaitRun: %v, want %v", err, c.want)
				}
				if got := strings.Join(toasts.texts(), "\n"); got != c.toast {
					t.Fatalf("toasts %q, want %q", got, c.toast)
				}
			}
		})
	}
}

// A connect that a newer one, a show or Disconnect replaced before it began says which: superseded
// by the newer connect or show (ErrConnectSuperseded) or cancelled (ErrConnectCancelled), both no
// failure (the window raises no toast for either).
func TestAConnectReplacedBeforeItBeganSaysWhy(t *testing.T) {
	cases := []struct {
		name  string
		cause error
		want  error
	}{
		{"superseded", errConnectSuperseded, ErrConnectSuperseded},
		{"cancelled", errConnectCancelled, ErrConnectCancelled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := poolSession(t, time.Minute, "alpha")
			st := s.state
			st.connMu.Lock() // a connect before it holds the lock, as one that shows a cluster from the pool
			errc := make(chan error, 1)
			go func() { errc <- s.Connect(context.Background(), "alpha") }()
			waitUntil(t, "the connect registered", func() bool {
				st.mu.Lock()
				defer st.mu.Unlock()
				return st.connectSeq == 1 && st.connectCancel != nil
			})
			st.mu.Lock()
			st.connectCancel(c.cause) // what Connect, Show and Disconnect do
			st.connectSeq++
			st.mu.Unlock()
			st.connMu.Unlock()
			if err := <-errc; !errors.Is(err, c.want) || !ConnectNotFailed(err) {
				t.Errorf("the replaced connect: %v, want %v", err, c.want)
			}
		})
	}
}

// A connect of the cluster the window's connect waits for already joins that wait (the lead's
// finding of 2026-10-22: it ended the first connect's wait with ErrConnectInBackground, and the
// window toasted "another cluster was shown" although nothing else was shown): the first connect
// goes on waiting, and both answer with its end: connected, failed, in the background once another
// cluster is shown after all, or cancelled; one login, no toast.
func TestAConnectOfTheSameClusterJoinsTheWait(t *testing.T) {
	for _, c := range []struct {
		name  string
		fail  bool
		then  func(s *Session, hc *heldConnector) error
		want  ConnectionState // alpha's, the cluster shown after both
		check func(error) bool
	}{
		{"connected", false, func(_ *Session, hc *heldConnector) error { hc.open("alpha"); return nil }, Connected,
			func(err error) bool { return err == nil }},
		{"failed", true, func(_ *Session, hc *heldConnector) error { hc.open("alpha"); return nil }, ConnectError,
			func(err error) bool { return err != nil && !ConnectNotFailed(err) }},
		{"another cluster shown", false, func(s *Session, _ *heldConnector) error { return s.Show("beta") }, Connecting,
			func(err error) bool { return errors.Is(err, ErrConnectInBackground) }},
		{"cancelled", false, func(s *Session, _ *heldConnector) error { return s.Disconnect() }, Disconnected,
			func(err error) bool { return errors.Is(err, ErrConnectCancelled) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, hc, first, toasts := runSession(t, c.fail)
			second := make(chan error, 1)
			go func() { second <- s.Connect(context.Background(), "alpha") }()
			var firstErr error
			firstBack := false
			waitUntil(t, "the second connect to join, or the first to return", func() bool {
				select {
				case firstErr = <-first:
					firstBack = true
				default:
				}
				s.state.mu.Lock()
				defer s.state.mu.Unlock()
				return firstBack || (s.state.connectWait != nil && s.state.connectWait.joined == 1)
			})
			if firstBack {
				t.Fatalf("the first connect returned when its cluster was connected again: %v", firstErr)
			}
			if err := c.then(s, hc); err != nil {
				t.Fatal(err)
			}
			for i, err := range []error{<-first, <-second} {
				if !c.check(err) {
					t.Errorf("connect %d: %v", i+1, err)
				}
			}
			u := s.UIState()
			state := Disconnected // alpha's entry while it runs or stands, else the window's on alpha
			if e, ok := u.ConnectionOf("alpha"); ok {
				state = e.Connection.State
			} else if u.Context == "alpha" {
				state = u.Connection.State
			}
			if state != c.want {
				t.Errorf("alpha after both: %s, want %s (shown %q %s)", state, c.want, u.Context, u.Connection.State)
			}
			if attempts, _ := s.ConnectAttempts("alpha", 0); len(attempts) != 1 {
				t.Errorf("%d connect attempts of alpha, want one login", len(attempts))
			}
			if got := toasts.texts(); len(got) != 0 {
				t.Errorf("toasts %q", got)
			}
		})
	}
}
