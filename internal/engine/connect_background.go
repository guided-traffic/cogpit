package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

// A connect that shows nothing (D-69 stage 3: the restore of the last session connects the shown
// cluster first and the others behind it, restore.go). The cluster joins the pool as a cluster the
// window left: its heartbeat, discovery, access reviews and login hold (connwatch_lapse.go) start,
// its views are not listed until it is shown (it is cold at once), and the window's state stays
// as it is. UIState.Connections has its entry while it connects (connecting, so that its marks
// blink) and once it is connected; a connect that fails leaves none and says why in a toast.
//
// Its connect is the user's own connect of that cluster, made at start: the connector asks the
// kubeconfig's credential plugin as on any connect (an approved one runs; one not allowed yet, or
// whose program changed, is refused with the reason, D-26), and nothing else is sent. A connect the
// user starts meanwhile for the same context waits for it and shows its connection (awaitBackground)
// rather than running a second one beside it.

// bgGenBase is where the numbers of background connects start: a range of their own, so that they
// never take a shown connect's sequence number, which decides which connect is current.
const bgGenBase = uint64(1) << 62

// errConnectedAlready is ConnectBackground's answer for a context the pool holds already.
var errConnectedAlready = errors.New("connected already")

// ConnectBackground connects a context without showing it: the cluster joins the pool as one the
// window left. A context that is connected already, or whose connect is under way, is left alone
// (errConnectedAlready for the former, nil for the latter).
func (s *Session) ConnectBackground(ctx context.Context, name string) error {
	connector := s.opts.Connector
	if connector == nil {
		return errors.New("no connector configured")
	}
	if r, ok := connector.(contextResolver); ok {
		resolved, err := r.ResolveContext(name)
		if err != nil {
			return fmt.Errorf("connect %s: %w", name, err)
		}
		name = resolved
	}
	st := s.state
	st.mu.Lock()
	switch {
	case st.closed:
		st.mu.Unlock()
		return ErrClosed
	case st.pool[name] != nil:
		st.mu.Unlock()
		return errConnectedAlready
	case st.background[name] != nil:
		st.mu.Unlock()
		return nil
	}
	if u := s.UIState(); u.Context == name && u.Connection.State == Connecting {
		st.mu.Unlock()
		return nil // the window connects it already
	}
	done := make(chan struct{})
	if st.background == nil {
		st.background = map[string]chan struct{}{}
		st.bgCancel = map[string]context.CancelCauseFunc{}
	}
	ctx, cancel := context.WithCancelCause(ctx) // Disconnect cancels it (D-73)
	defer cancel(nil)
	st.background[name], st.bgCancel[name] = done, cancel
	st.bgGen++
	gen := bgGenBase + st.bgGen
	st.mu.Unlock()
	defer func() {
		st.mu.Lock()
		delete(st.background, name)
		delete(st.bgCancel, name)
		st.mu.Unlock()
		close(done)
	}()

	s.UpdateUI(func(u *UIState) {
		if u.connectionIndex(name, 0) >= 0 {
			return
		}
		c := ConnectedCluster{Context: name, Connection: Connection{State: Connecting}, restoring: true}
		at, _ := slices.BinarySearchFunc(u.Connections, name, func(c ConnectedCluster, id string) int { return strings.Compare(c.Context, id) })
		u.Connections = slices.Insert(u.Connections, at, c)
	})
	dropEntry := func() {
		s.UpdateUI(func(u *UIState) {
			u.Connections = slices.DeleteFunc(u.Connections, func(c ConnectedCluster) bool { return c.Context == name && c.restoring })
			if len(u.Connections) == 0 {
				u.Connections = nil
			}
		})
	}

	attempt := st.logs.begin(name, AttemptConnect, LogInfo, "Connecting to "+name+" (restoring the last session, in the background)")
	steps := &connectSteps{logs: &st.logs, attempt: attempt}
	if line := s.proxyLogLine(name); line != "" {
		steps.step(LogInfo, cluster.StepProxy, line, 0)
	}
	login := s.newLoginNotes(name, steps) // what it waits for (loginwait.go, D-73)
	cctx := withLoginWatch(withConnectSteps(ctx, steps), login.lw)
	var b *Backend
	err := s.safeCall("connect", func() (err error) { b, err = connector.Connect(cctx, name); return err })
	login.connected()
	if err == nil && b == nil {
		err = errors.New("the connector returned no cluster")
	}
	if err != nil {
		dropEntry()
		if errors.Is(context.Cause(cctx), errConnectCancelled) {
			finishLeftConnect(cctx, &st.logs, attempt, err)
			return fmt.Errorf("connect %s: %w", name, errConnectCancelled)
		}
		st.logs.finish(attempt, OutcomeFailed, LogError, err.Error())
		return fmt.Errorf("connect %s: %w", name, err)
	}
	b.login = login
	s.learnUser(cctx, name, b) // whoami.go, D-68
	s.learnVersion(b)          // k8sversion.go, D-91

	st.mu.Lock()
	if st.closed || st.pool[b.Context] != nil {
		// Closed meanwhile, or the user's connect of the context won: this connection closes.
		st.mu.Unlock()
		b.close(s.log)
		st.logs.finish(attempt, OutcomeAbandoned, LogInfo, "Connected already")
		dropEntry()
		return errConnectedAlready
	}
	s.attachCustomKinds(b)
	s.noteAccessReviews(b, steps)
	s.attachAccess(b)
	s.attachRefusals(b)
	if line := insecureLogLine(b); line != "" {
		steps.step(LogWarn, cluster.StepTLS, line, 0)
	}
	b.watch = newConnWatch(s, b, gen)
	s.noteNoAlertmanager(b) // alerts.go
	logConnectedSteps(steps, b)
	st.logs.finish(attempt, OutcomeOK, LogInfo, "Connected to "+name+" in the background")
	e := &pooled{b: b, gen: gen}
	st.addLocked(e)
	st.armColdLocked(e) // no view of it is open: cold once nothing of it is warm
	st.mu.Unlock()

	conn := Connection{State: Connected, Demo: b.Demo, TLSUnchecked: tlsUnchecked(b), CredentialPlugin: b.CredentialPlugin}
	s.UpdateUI(func(u *UIState) {
		u.Connections = slices.DeleteFunc(u.Connections, func(c ConnectedCluster) bool { return c.Context == b.Context && c.restoring })
		c := ConnectedCluster{Context: b.Context, Connection: conn, gen: gen}
		at, _ := slices.BinarySearchFunc(u.Connections, b.Context, func(c ConnectedCluster, id string) int { return strings.Compare(c.Context, id) })
		u.Connections = slices.Insert(u.Connections, at, c)
	})
	b.watch.start()
	s.applyLoginHolds() // a cluster that is not shown: its setting may hold its login (connwatch_lapse.go)
	s.settleGoneConnect(b.Context, b)
	return nil
}

// awaitBackground waits for a background connect of the context, if one runs, so that the
// caller's connect shows its connection; ctx's error when the caller gave up.
func (st *state) awaitBackground(ctx context.Context, name string) error {
	st.mu.Lock()
	done := st.background[name]
	st.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
