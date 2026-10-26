package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// A connect run (D-69; D-97, the owner's WF39 and WF40 of 2026-10-19). Every connect of a context
// that is not connected runs on its own, apart from what the window shows: the window's connect
// (Connect: a click on a cluster, cluster.connect) starts one, or joins the one under way for that
// context, shows the cluster as connecting and waits for it; the restore of the last session
// (ConnectBackground) starts one nobody waits for. When a run ends while the window's connect that
// waits for it is still the latest, that connect shows its cluster, or its error, and the window
// stays on it.
//
// Showing another cluster, or connecting another one, ends only that wait (errConnectSuperseded):
// the run goes on in the background, its dot blinking, its login waiting where it waits (a browser,
// an MFA push), its waiting line and Cancel Connecting in its menus. When it ends nobody waits for,
// the cluster joins the pool as one the window left and a toast says so, or a toast says why it
// failed. The cluster the window shows never changes by itself: before, showing another cluster
// cancelled a connect waiting for its login without a word (WF39), and a failed connect showed the
// cluster shown before again (WF40).

// connectRun is one connect under way, by context id in state.runs.
type connectRun struct {
	name    string
	restore bool // the restore of the last session started it (ConnectBackground)
	ctx     context.Context
	cancel  context.CancelCauseFunc // Disconnect: errConnectCancelled; the session's close: ErrClosed
	done    chan struct{}           // closed once it ended and the UI state says how
	gen     uint64                  // its connection's number (pooled.gen), from the background range

	// Guarded by state.mu.
	watchSeq uint64  // the window's connect waiting for it (its state.connectSeq); 0 for none
	ended    bool    // it ended: err, entry and watched hold
	watched  bool    // at its end the window's connect waiting for it was the latest, which says how it ended
	err      error   // how it failed; nil once connected
	entry    *pooled // the connection it made, or the pool's for its cluster when that was connected already
}

// newRunLocked registers a run of the connect of name; startConnectRun starts it. Its context keeps ctx's
// values and none of its cancellation: a run ends by itself, by Disconnect, or with the session.
// state.mu is held.
func (st *state) newRunLocked(ctx context.Context, name string, restore bool) *connectRun {
	rctx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	r := &connectRun{name: name, restore: restore, ctx: rctx, cancel: cancel, done: make(chan struct{}), gen: st.newGenLocked()}
	if st.runs == nil {
		st.runs = map[string]*connectRun{}
	}
	st.runs[name] = r
	return r
}

// startConnectRun runs r on a goroutine of its own.
func (s *Session) startConnectRun(r *connectRun) {
	s.goSafe("connect", func() {
		defer func() { // a panic still ends the run, so that nobody waits for it forever
			s.state.mu.Lock()
			unended := !r.ended
			if unended {
				r.ended, r.err = true, errors.New("internal error")
				delete(s.state.runs, r.name)
			}
			s.state.mu.Unlock()
			if unended {
				s.UpdateUI(func(u *UIState) { u.dropRunning(r.name) })
				close(r.done)
			}
		}()
		s.runConnect(r)
	})
}

// runConnect connects r's context through the connector: its entry of Connections connecting (its
// marks blink), its attempt in the connection log, its credential plugin's runs in the UI state;
// then the cluster in the pool as one the window left, or the error.
func (s *Session) runConnect(r *connectRun) {
	st := s.state
	connector := s.opts.Connector
	s.UpdateUI(func(u *UIState) { u.markRunning(r.name) })

	begin := "Connecting to " + r.name
	if r.restore {
		begin += " (restoring the last session, in the background)"
	}
	attempt := st.logs.begin(r.name, AttemptConnect, LogInfo, begin)
	steps := &connectSteps{logs: &st.logs, attempt: attempt, diag: st.diags.of(r.name),
		cancelled: func() bool { return errors.Is(context.Cause(r.ctx), errConnectCancelled) }}
	if line := s.proxyLogLine(r.name); line != "" {
		steps.step(LogInfo, cluster.StepProxy, line, 0) // the proxy of the cluster's settings, its password as ••• (proxy.go)
	}
	// The runs of its credential plugin: the UI state and the log say what it waits for (loginwait.go, D-73).
	login := s.newLoginNotes(r.name, steps)
	cctx := withLoginWatch(withConnectSteps(r.ctx, steps), login.lw)
	var b *Backend
	err := s.safeCall("connect", func() (err error) { b, err = connector.Connect(cctx, r.name); return err })
	login.connected()
	if err == nil && b == nil {
		err = errors.New("the connector returned no cluster")
	}
	if err != nil {
		s.endRunFailed(r, attempt, err)
		return
	}
	b.login = login
	s.learnUser(cctx, r.name, b) // who the API server says the user is, never failing the connect (whoami.go, D-68)
	s.learnVersion(b)            // its Kubernetes version and the Nodes list's check (k8sversion.go, D-91)

	st.mu.Lock()
	if cause := context.Cause(r.ctx); st.closed || cause != nil {
		// Cancelled (Disconnect) or closed after the connector answered: this connection closes.
		st.mu.Unlock()
		b.close(s.log)
		s.endRunFailed(r, attempt, cause)
		return
	}
	if e := st.pool[b.Context]; e != nil {
		// The connector named the cluster by a context id the pool holds already: that connection
		// stays, with its streams, and is the one shown; the new one closes.
		r.end(st, nil, e)
		st.mu.Unlock()
		b.close(s.log)
		st.logs.finish(attempt, OutcomeAbandoned, LogInfo, "Connected already as "+b.Context)
		s.UpdateUI(func(u *UIState) { u.dropRunning(r.name) })
		close(r.done)
		return
	}
	s.attachCustomKinds(b)
	s.noteAccessReviews(b, steps) // their first answers, when they come (accessreviews.go)
	// The namespaces watch before the access reviews: their goroutine pokes b.nsWatch
	// (accessChanged), so the field is set before it starts.
	s.attachNamespaceWatch(b) // while the namespace filter names namespaces (nswatch.go, AW11)
	s.attachAccess(b)
	s.attachRefusals(b) // a refused read in all namespaces is followed, whoever sent it (accessiblens_plan.go)
	if line := insecureLogLine(b); line != "" {
		steps.step(LogWarn, cluster.StepTLS, line, 0) // insecure-skip-tls-verify (tlswarning.go)
	}
	b.watch = newConnWatch(s, b, r.gen) // nil for the demo cluster (connwatch.go, D-58)
	s.noteNoAlertmanager(b)             // the pill hidden with the connection's first UI state (alerts.go)
	logConnectedSteps(steps, b)
	e := &pooled{b: b, gen: r.gen}
	st.addLocked(e)
	r.end(st, nil, e)
	watched := r.watched
	st.mu.Unlock()

	name := kubeconfig.DisplayText(r.name, 200)
	switch {
	case watched:
		st.logs.finish(attempt, OutcomeOK, LogInfo, "Connected to "+r.name)
	case r.restore:
		st.logs.finish(attempt, OutcomeOK, LogInfo, "Connected to "+r.name+" in the background")
	default:
		st.logs.finish(attempt, OutcomeOK, LogInfo, "Connected to "+r.name+" in the background: another cluster was shown meanwhile")
	}
	conn := Connection{State: Connected, Demo: b.Demo, TLSUnchecked: tlsUnchecked(b), CredentialPlugin: b.CredentialPlugin} // tlswarning.go
	s.UpdateUI(func(u *UIState) { u.setRunConnected(b.Context, r.name, conn, r.gen) })
	if !watched { // the window's connect shows it next (awaitRun); else a cluster the window left
		st.mu.Lock()
		if st.pooledLocked(b) == e && st.shown != b {
			st.armColdLocked(e) // after its entry, which the cold mark needs
		}
		st.mu.Unlock()
	}
	b.watch.start()                   // nil-safe; the watch writes its Connection from now on (connwatch.go)
	s.applyLoginHolds()               // a cluster that is not shown: its setting may hold its login (connwatch_lapse.go)
	s.settleGoneConnect(b.Context, b) // its file went or its source was removed meanwhile (connect_gone.go)
	close(r.done)
	if !watched && !r.restore {
		s.emit(Event{Type: EventToast, Level: LevelInfo, Message: "Connected to " + name + " in the background."})
	}
}

// endRunFailed ends a run whose connect failed, was cancelled (Disconnect, D-73) or ended with the
// session: its attempt says how, its entry of Connections goes, and a failure nobody waits for says
// why in a toast (the restore's say it themselves, restore.go).
func (s *Session) endRunFailed(r *connectRun, attempt *ConnectAttempt, err error) {
	st := s.state
	cause := context.Cause(r.ctx)
	st.mu.Lock()
	r.end(st, err, nil)
	watched := r.watched
	st.mu.Unlock()
	switch {
	case errors.Is(cause, errConnectCancelled):
		finishCancelledConnect(&st.logs, attempt, err)
	case cause != nil: // the session's close
		st.logs.finish(attempt, OutcomeAbandoned, LogInfo, "Ended: Cogpit closed")
	default:
		if stages, _ := cluster.StagesOf(err); !st.logs.hasStages(attempt) {
			st.logs.stagesTo(attempt, connectStages(stages)) // a failure before the cluster layer reported them
		}
		st.logs.finish(attempt, OutcomeFailed, LogError, err.Error())
	}
	s.UpdateUI(func(u *UIState) { u.dropRunning(r.name) })
	close(r.done)
	if cause == nil && !watched && !r.restore {
		s.emit(Event{Type: EventToast, Level: LevelError, Message: "Could not connect to " + kubeconfig.DisplayText(r.name, 200) +
			": " + oneLine(err.Error())})
	}
}

// end records how r ended and drops it from the runs under way: watched when the window's connect
// waiting for it is still the latest, which then shows it. state.mu is held.
func (r *connectRun) end(st *state, err error, e *pooled) {
	r.ended, r.err, r.entry = true, err, e
	r.watched = r.watchSeq != 0 && r.watchSeq == st.connectSeq && !st.closed
	if st.runs[r.name] == r {
		delete(st.runs, r.name)
	}
}

// errConnectInBackground is Connect's error when the window was asked to show another cluster
// while the connect waited: the connect goes on in the background (D-97).
var errConnectInBackground = errors.New("another cluster was shown; the connect goes on in the background")

// ErrConnectInBackground matches Connect's error when the window was asked to show another cluster
// while the connect waited: the connect goes on in the background, and a toast says how it ended.
var ErrConnectInBackground = errConnectInBackground

// ErrConnectCancelled matches Connect's error when Disconnect cancelled the connect (the window's
// Cancel Connecting, cluster.disconnect, D-73), also at the moment it ended: the cluster is not
// connected, and its attempt in the connection log says "cancelled" (D-66).
var ErrConnectCancelled = errConnectCancelled

// ErrConnectSuperseded matches Connect's error when a newer connect or a show came before this
// connect began: it never started, and the window shows the cluster chosen last.
var ErrConnectSuperseded = errConnectSuperseded

// ConnectNotFailed reports whether err, the error of a connect (Connect, and the commands that
// connect a cluster: cluster.connect, hotbar.openCluster, ui.tab.select, ui.setNavigatorScope), says
// that the connect did not fail: it goes on in the background (ErrConnectInBackground), it was
// cancelled (ErrConnectCancelled), or a newer connect superseded it before it began
// (ErrConnectSuperseded). The window raises no toast for any of them (the owner's AW15 and AW16 of
// 2026-10-22): the cluster's dot and its connection log say what happened.
func ConnectNotFailed(err error) bool {
	return errors.Is(err, errConnectInBackground) || errors.Is(err, errConnectCancelled) || errors.Is(err, errConnectSuperseded)
}

// awaitRun is the window's connect waiting for r, as the connect with sequence number seq: it
// shows r's cluster once r connected, or its error, while it is still the window's latest connect.
// Another connect or show ends the wait, not the run (errConnectInBackground); Disconnect cancels
// the run itself; the caller's own context's end cancels the run too, as before.
func (s *Session) awaitRun(cctx context.Context, r *connectRun, seq uint64) error {
	st := s.state
	select {
	case <-r.done:
	case <-cctx.Done():
		cause := context.Cause(cctx)
		st.mu.Lock()
		ended := r.ended
		st.mu.Unlock()
		switch {
		case ended: // it ended at the same moment: its end decides below
			<-r.done
		case errors.Is(cause, errConnectSuperseded):
			st.mu.Lock()
			if r.watchSeq == seq {
				r.watchSeq = 0
			}
			st.mu.Unlock()
			return fmt.Errorf("connect %s: %w", r.name, errConnectInBackground)
		case errors.Is(cause, errConnectCancelled), errors.Is(cause, ErrClosed):
			<-r.done // Disconnect or the close cancelled the run itself
			return fmt.Errorf("connect %s: %w", r.name, cause)
		default: // the caller gave up: the connect ends with it, as it always did
			r.cancel(cause)
			<-r.done
		}
	}
	st.mu.Lock()
	err, e, watched := r.err, r.entry, r.watched
	current := !st.closed && st.connectSeq == seq
	st.mu.Unlock()
	if !current {
		return s.leftRun(cctx, r, err, e, watched)
	}
	if err != nil {
		msg, callers := err.Error(), false
		if c := context.Cause(r.ctx); c != nil && !errors.Is(c, errConnectCancelled) && !errors.Is(c, ErrClosed) {
			msg, callers = c.Error(), true // the caller's own end: its cause, as before
		}
		conn, stages := failedConnection(err, msg, callers) // the cause in a sentence and the stages (connectfailure.go, D-114)
		at := s.opts.Now()
		s.UpdateUI(func(u *UIState) {
			if u.Context == r.name && u.Connection.State == Connecting {
				u.Connection = conn
				u.ConnectFailure = &ConnectFailure{Context: r.name, At: at.UTC(), Stages: stages}
			}
		})
		s.settleGoneConnect(r.name, nil) // its context left Cogpit meanwhile: forget it (connect_gone.go)
		return fmt.Errorf("connect %s: %w", r.name, err)
	}
	st.mu.Lock()
	if st.closed || st.connectSeq != seq {
		st.mu.Unlock()
		return s.leftRun(cctx, r, nil, e, watched)
	}
	if st.pooledLocked(e.b) != e {
		st.mu.Unlock()
		s.settleClosedConnect(r.name) // closed since it connected: its file went or its source was removed (connect_gone.go)
		return nil
	}
	st.showLocked(e)
	st.mu.Unlock()
	s.showConnected(e.b, e.gen)
	s.applyLoginHolds() // the shown cluster's login is renewed (connwatch_lapse.go)
	return nil
}

// leftRun is awaitRun's error when the window's connect waiting for r is no longer the latest once
// r ended, which is no failure (the owner's AW15 and AW16 of 2026-10-22: it raised an error toast,
// "superseded by a newer connect" among them): the session closed (ErrClosed); Disconnect cancelled
// r or, once r had ended, the window's wait cctx (ErrConnectCancelled: the cluster it connected
// closes behind it); or another connect or show came, and r ended in the background
// (ErrConnectInBackground). A run that ended as the window's (watched) before another connect
// replaced it says how it ended as a run nobody waited for says it (tellRunEnd).
func (s *Session) leftRun(cctx context.Context, r *connectRun, err error, e *pooled, watched bool) error {
	st := s.state
	st.mu.Lock()
	closed := st.closed
	st.mu.Unlock()
	if closed {
		return fmt.Errorf("connect %s: %w", r.name, ErrClosed)
	}
	if errors.Is(context.Cause(r.ctx), errConnectCancelled) || errors.Is(context.Cause(cctx), errConnectCancelled) {
		return fmt.Errorf("connect %s: %w", r.name, errConnectCancelled)
	}
	if watched { // it ended as the window's, which another connect replaced since: as if nobody waited
		shown := false
		if e != nil {
			st.mu.Lock()
			shown = st.shown == e.b // the newer connect was of its own cluster, which it shows now
			if st.pooledLocked(e.b) == e && !shown {
				st.armColdLocked(e)
			}
			st.mu.Unlock()
		}
		if !shown {
			s.tellRunEnd(r, err)
		}
	}
	return fmt.Errorf("connect %s: %w", r.name, errConnectInBackground)
}

// tellRunEnd says in a toast how a run ended that the window's connect waited for until another
// connect replaced that connect: as a run nobody waited for says it itself.
func (s *Session) tellRunEnd(r *connectRun, err error) {
	if r.restore || context.Cause(r.ctx) != nil {
		return
	}
	name := kubeconfig.DisplayText(r.name, 200)
	if err != nil {
		s.emit(Event{Type: EventToast, Level: LevelError, Message: "Could not connect to " + name + ": " + oneLine(err.Error())})
		return
	}
	s.emit(Event{Type: EventToast, Level: LevelInfo, Message: "Connected to " + name + " in the background."})
}

// markRunning marks the entry of Connections of a context whose connect runs (connectRun): it
// stays, connecting, when the window shows another cluster. A context without one gets one.
func (u *UIState) markRunning(contextID string) {
	if i := u.connectionIndex(contextID, 0); i >= 0 {
		u.Connections[i].running = true
		return
	}
	c := ConnectedCluster{Context: contextID, Connection: Connection{State: Connecting}, running: true}
	at, _ := slices.BinarySearchFunc(u.Connections, contextID, func(c ConnectedCluster, id string) int { return strings.Compare(c.Context, id) })
	u.Connections = slices.Insert(u.Connections, at, c)
}

// dropRunning removes the entry of a context whose connect run ended without a connection.
func (u *UIState) dropRunning(contextID string) {
	u.Connections = slices.DeleteFunc(u.Connections, func(c ConnectedCluster) bool { return c.Context == contextID && c.running })
	if len(u.Connections) == 0 {
		u.Connections = nil
	}
}

// setRunConnected makes the entry of a connect run that connected say so: its connection, made by
// gen, a cluster the window left until the connect waiting for it shows it (showConnected).
func (u *UIState) setRunConnected(contextID, runName string, conn Connection, gen uint64) {
	if contextID != runName {
		u.dropRunning(runName)
	}
	if i := u.connectionIndex(contextID, 0); i >= 0 {
		c := &u.Connections[i]
		c.Connection, c.gen, c.running, c.Cold = conn, gen, false, false
		return
	}
	c := ConnectedCluster{Context: contextID, Connection: conn, gen: gen}
	at, _ := slices.BinarySearchFunc(u.Connections, contextID, func(c ConnectedCluster, id string) int { return strings.Compare(c.Context, id) })
	u.Connections = slices.Insert(u.Connections, at, c)
}
