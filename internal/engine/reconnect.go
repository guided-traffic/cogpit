package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Reconnect (D-81, the owner's F28 and F29 of 2026-10-17). A connection keeps the credentials it
// read from the kubeconfig at its connect (cluster/loginfile.go): a client certificate renewed in
// the file never reached it, and Log in again, which renews a login on the same connection through
// its credential plugin, had nothing to renew for a context without one. Reconnect ends a cluster's
// connection and connects it anew from its kubeconfig file as it is now, without closing its tabs:
//
//   - The new connection is made first, as any connect makes it (the connector: the file read
//     again, the proxy of the cluster's settings, the approvals of credential plugins and unchecked
//     certificates, D-26 and A28, never weaker), while the old one goes on showing what its cache
//     holds. Once it stands it takes the old one's place in the pool (pool.go) and the old one
//     closes: a reconnect that fails leaves the cluster as it was, and says why.
//   - The UI state stays: the cluster's tabs, the view, its filter, sort, selection and drawer, the
//     dock. The views follow the new backend (state.shown), which lists anew; the selection drops
//     what the new cache does not hold.
//   - The dock: a logs tab opens its stream again on the new connection (DockTab.Session, the
//     window's stream key); a shell, attach or node shell ends as at an interruption, its tab saying
//     so with Reconnect (DockTab.Interrupted, Reconnected; dock.reconnect opens a new session, which
//     is a write and asks as it always does); a local terminal runs on, it reads the file itself.
//   - Port forwards listen again on the same local ports, carried to the new connection.
//   - The integrations (D-76 to D-79) end with the old backend and their followers start them on
//     the new one; the question of a new cluster's integrations is not asked again (it follows the
//     cluster's settings, not the connect).
//   - A job (a drain, D-63) or a Helm action running on the cluster refuses the reconnect: it would
//     stop it, and D-11's journal would have to finish it. While the connection is interrupted or
//     its login expired none runs (they stop at the interruption, D-58).
//
// Log in again (cluster.login) reconnects when the context runs no credential plugin (a
// certificate or a token in the kubeconfig: nothing else can renew it), or when the login in the
// file changed since the connect; with a plugin and the same login it runs the plugin once on the
// connection as before (D-58, D-73), which keeps the cache and the streams. A connection whose login
// the API server rejects reconnects by itself when its kubeconfig file holds another login than
// the one rejected and its context runs no credential plugin (the owner's renewed certificate):
// when the login expires, and when the kubeconfig watch sees the file change (renewFromFile).

// Errors of a reconnect.
var (
	errReconnecting  = errors.New("a reconnect of this cluster is under way")
	errReconnectDemo = errors.New("the demo cluster has no connection to renew")
	errReconnectLeft = errors.New("the cluster was disconnected while it reconnected")
	errNotReconnect  = errors.New("the cluster was connected anew meanwhile")
)

// ErrReconnectLoginGUIOnly refuses an agent's or the CLI's reconnect of a cluster whose login
// expired, or lapsed, while its context runs a credential plugin: the new connection would run the
// plugin to log in again, which may open a browser; that is Log in again, the user's in Cogpit's
// window (cluster.login is GUI-only, D-58).
var ErrReconnectLoginGUIOnly = errors.New("the login to this cluster expired and its context logs in through a credential plugin, " +
	"which may open a browser: only the user logs in again, in Cogpit's window (Log in again or Reconnect there)")

// Why a reconnect runs: what its first line in the connection log says.
const (
	reconnectByUser  = "Reconnecting (Reconnect): a new connection from the kubeconfig file as it is now; the tabs stay"
	reconnectByLogin = "Logging in again by reconnecting (Log in again): the context logs in with what its kubeconfig file holds, " +
		"so a new connection reads it again; the tabs stay"
	reconnectByFile = "Reconnecting by itself: the API server rejects the login and the kubeconfig file holds another one " +
		"(a renewed certificate or token); a new connection uses it, the tabs stay"
)

// reconnectRun is a reconnect under way (state.reconnects): its cancel, for a disconnect.
type reconnectRun struct {
	cancel context.CancelCauseFunc
}

// fileRenewal remembers the kubeconfig login a connection last reconnected by itself for
// (renewFromFile), so that a file whose login is rejected too is tried once, not at every look.
type fileRenewal struct {
	mu    sync.Mutex
	tried string
}

// try reports whether fp was not tried yet, and marks it tried.
func (f *fileRenewal) try(fp string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tried == fp {
		return false
	}
	f.tried = fp
	return true
}

// Reconnect ends a connected context's connection ("" the shown one) and connects it anew from its
// kubeconfig file as it is now, keeping its tabs (cluster.reconnect, D-81). It returns once the new
// connection stands, or with the error that kept it from standing: the old one then stays.
func (s *Session) Reconnect(ctx context.Context, contextID string) error {
	return s.reconnect(ctx, contextID, nil, reconnectByUser)
}

// ReconnectBlocked says why a connected context ("" the shown one) cannot reconnect now: it is not
// connected, it is the demo cluster, a reconnect of it runs, or a job or a Helm action runs on it.
func (s *Session) ReconnectBlocked(contextID string) error {
	b := s.BackendOf(contextID)
	switch {
	case b == nil && contextID == "":
		return ErrNotConnected
	case b == nil:
		return &NotConnectedError{Context: contextID}
	case b.Demo || b.Conn == nil:
		return errReconnectDemo
	case s.reconnecting(b.Context):
		return errReconnecting
	}
	return s.busyOn(b.Context)
}

// ReconnectRefused refuses origin's reconnect of a connected context ("" the shown one) whose login
// expired or lapsed while its context runs a credential plugin, unless the user's own action in the
// window asks for it (ErrReconnectLoginGUIOnly).
func (s *Session) ReconnectRefused(origin Origin, contextID string) error {
	if origin == OriginGUI {
		return nil
	}
	b := s.BackendOf(contextID)
	if b == nil || !b.CredentialPlugin {
		return nil
	}
	if st := b.watch.current(); st == LoginExpired || st == stateLapsed {
		return ErrReconnectLoginGUIOnly
	}
	return nil
}

// LogInAgain renews a connected context's expired or lapsed login ("" the shown one; cluster.login,
// the banner's Log in again): by a reconnect from the kubeconfig file when the context runs no
// credential plugin or its login in the file changed since the connect (D-81), else by running the
// plugin once on the connection (LoginContext). A plugin whose program changed since the user
// allowed it is refused with ErrNeedsNewConnect either way.
func (s *Session) LogInAgain(ctx context.Context, contextID string) error {
	b := s.BackendOf(contextID)
	if b == nil && contextID != "" {
		return &NotConnectedError{Context: contextID}
	}
	if b == nil {
		return ErrNotConnected
	}
	w := b.watch
	if st := w.current(); st != LoginExpired && st != stateLapsed {
		return errNotExpired
	}
	if w.approvalChanged() {
		return ErrNeedsNewConnect
	}
	if !loginByReconnect(b) {
		return s.LoginContext(b.Context)
	}
	return s.reconnect(ctx, b.Context, b, reconnectByLogin)
}

// loginByReconnect reports whether Log in again reconnects: the context runs no credential plugin,
// or its login in the kubeconfig file changed since the connect.
func loginByReconnect(b *Backend) bool {
	if !b.CredentialPlugin {
		return true
	}
	_, changed, err := b.Conn.LoginInFile()
	return err == nil && changed
}

// reconnecting reports whether a reconnect of the context runs.
func (s *Session) reconnecting(contextID string) bool {
	st := s.state
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.reconnects[contextID] != nil
}

// busyOn says why a reconnect would stop work running on the cluster: a job or a Helm action.
func (s *Session) busyOn(contextID string) error {
	if id := s.jobs.runningOn(contextID); id != "" {
		title := id
		if t, ok := s.UIState().DockTab(id); ok && t.Title != "" {
			title = t.Title
		}
		return fmt.Errorf("the job %q runs on this cluster and a reconnect would stop it: reconnect once it ended", title)
	}
	if s.opts.Helm.runningOn(contextID) {
		return errors.New("a Helm action runs on this cluster and a reconnect would stop it: reconnect once it ended")
	}
	return nil
}

// runningOn returns the tab id of a job running on the context, "" when none does.
func (js *jobSet) runningOn(contextID string) string {
	js.mu.Lock()
	defer js.mu.Unlock()
	ids := make([]string, 0, len(js.running))
	for id, r := range js.running {
		if r.context == contextID {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	slices.Sort(ids)
	return ids[0]
}

// runningOn reports whether a Helm action runs on the context. Nil-safe.
func (h *HelmOps) runningOn(contextID string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, on := range h.running {
		if on && k.context == contextID {
			return true
		}
	}
	return false
}

// reconnect connects contextID anew and puts the new connection in the old one's place. expect, when
// set, is the backend the reconnect is for: another one in the pool by then ends it. why is the
// attempt's first line.
func (s *Session) reconnect(ctx context.Context, contextID string, expect *Backend, why string) error {
	st := s.state
	connector := s.opts.Connector
	if connector == nil {
		return errors.New("no connector configured")
	}
	rctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	run := &reconnectRun{cancel: cancel}
	old, err := st.beginReconnect(contextID, expect, run, s.busyOn)
	if err != nil {
		return err
	}
	contextID = old.b.Context
	defer st.endReconnect(contextID, run)

	s.markReconnecting(contextID, old.gen, true)
	old.b.watch.setQuiet(true) // its lines would end the reconnect's attempt; the reconnect's are the log's now
	attempt := st.logs.begin(contextID, AttemptReconnect, LogInfo, why)
	steps := &connectSteps{logs: &st.logs, attempt: attempt}
	if line := s.proxyLogLine(contextID); line != "" {
		steps.step(LogInfo, cluster.StepProxy, line, 0)
	}
	login := s.newLoginNotes(contextID, steps)
	login.reconnect = true
	cctx := withLoginWatch(withConnectSteps(rctx, steps), login.lw)
	var b *Backend
	err = s.safeCall("reconnect", func() (err error) { b, err = connector.Connect(cctx, contextID); return err })
	login.connected()
	switch {
	case err == nil && b == nil:
		err = errors.New("the connector returned no cluster")
	case err == nil && b.Context != contextID:
		b.close(s.log)
		err = fmt.Errorf("the connector connected %s instead", kubeconfig.DisplayText(b.Context, 200))
	}
	if err != nil {
		s.markReconnecting(contextID, old.gen, false)
		old.b.watch.setQuiet(false)
		s.failReconnect(rctx, attempt, contextID, err, why == reconnectByFile)
		return fmt.Errorf("reconnect %s: %w", contextID, err)
	}
	b.login = login
	s.learnUser(cctx, contextID, b) // who the new login is (whoami.go, D-68)
	s.learnVersion(b)               // its version, which an upgrade may have changed (k8sversion.go, D-91)

	sw, err := s.swapBackend(old, b, steps)
	if err != nil {
		s.markReconnecting(contextID, old.gen, false)
		old.b.watch.setQuiet(false)
		s.failReconnect(rctx, attempt, contextID, err, why == reconnectByFile)
		return fmt.Errorf("reconnect %s: %w", contextID, err)
	}
	s.finishSwap(old, b, sw)
	text := "Reconnected to " + contextID + ": the new connection stands, the views read from it"
	st.logs.finish(attempt, OutcomeOK, LogInfo, text)
	s.log.Info("engine: reconnected anew", "context", contextID)
	msg := "Reconnected to " + kubeconfig.DisplayText(contextID, 200)
	if why == reconnectByFile {
		msg += " with the renewed login from its kubeconfig file"
	}
	s.emit(Event{Type: EventToast, Level: LevelInfo, Message: msg})
	if sw.shown {
		s.DropGoneSelected(ctx, s.UIState())
	}
	return nil
}

// failReconnect ends a reconnect's attempt that failed, or that a disconnect ended, and says so.
func (s *Session) failReconnect(rctx context.Context, attempt *ConnectAttempt, contextID string, err error, auto bool) {
	st := s.state
	cause := context.Cause(rctx)
	switch {
	case errors.Is(err, errReconnectLeft) || errors.Is(cause, errReconnectLeft) || errors.Is(cause, ErrClosed):
		st.logs.finish(attempt, OutcomeCancelled, LogInfo, "Reconnecting ended: the cluster was disconnected")
		return
	case errors.Is(err, errNotReconnect):
		st.logs.finish(attempt, OutcomeAbandoned, LogInfo, "Reconnecting ended: the cluster was connected anew meanwhile")
		return
	}
	st.logs.finish(attempt, OutcomeFailed, LogError, "Could not reconnect: "+err.Error()+"; the connection stays as it was")
	what := "Could not reconnect to "
	if auto {
		what = "Could not reconnect with the renewed login of "
	}
	s.emit(Event{Type: EventToast, Level: LevelWarning, Message: what + kubeconfig.DisplayText(contextID, 200) + ": " + err.Error()})
}

// beginReconnect registers a reconnect of a pooled context ("" the shown one; expect, when set, the
// backend it must be) and returns its pool entry.
func (st *state) beginReconnect(contextID string, expect *Backend, run *reconnectRun, busy func(string) error) (*pooled, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		return nil, ErrClosed
	}
	if contextID == "" {
		if st.shown == nil {
			return nil, ErrNotConnected
		}
		contextID = st.shown.Context
	}
	e := st.pool[contextID]
	switch {
	case e == nil:
		return nil, &NotConnectedError{Context: contextID}
	case expect != nil && e.b != expect:
		return nil, errNotReconnect
	case e.b.Demo || e.b.Conn == nil:
		return nil, errReconnectDemo
	case st.reconnects[contextID] != nil:
		return nil, errReconnecting
	}
	if err := busy(contextID); err != nil { // state.mu before the jobs' and Helm's own locks
		return nil, err
	}
	if st.reconnects == nil {
		st.reconnects = map[string]*reconnectRun{}
	}
	st.reconnects[contextID] = run
	return e, nil
}

// endReconnect forgets a reconnect that ended.
func (st *state) endReconnect(contextID string, run *reconnectRun) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.reconnects[contextID] == run {
		delete(st.reconnects, contextID)
	}
}

// cancelReconnectLocked ends a reconnect of a context that leaves the pool. state.mu is held.
func (st *state) cancelReconnectLocked(contextID string) {
	if r := st.reconnects[contextID]; r != nil {
		r.cancel(errReconnectLeft)
	}
}

// markReconnecting sets or clears the connection's Reconnecting, of the connect gen.
func (s *Session) markReconnecting(contextID string, gen uint64, on bool) {
	s.UpdateUI(func(u *UIState) {
		if u.Context == contextID && u.connGen == gen && u.Connection.State.Live() {
			u.Connection.Reconnecting = on
			if !on {
				u.Connection.WaitingForLogin = LoginWait{}
			}
		}
		if i := u.connectionIndex(contextID, gen); i >= 0 {
			u.Connections[i].Connection.Reconnecting = on
			if !on {
				u.Connections[i].Connection.WaitingForLogin = LoginWait{}
			}
		}
	})
}

// swapped is what a swap took from the old backend for the new one.
type swapped struct {
	gen      uint64
	shown    bool              // the window showed the cluster
	sessions []*trackedSession // its shells, attach and node shells: they end
	forwards []carriedForward  // its port forwards that listened: they listen again
}

// swapBackend puts b in old's place in the pool, as the window's shown backend too when it was.
// It fails when old left the pool meanwhile (b is closed then), or when a job or a Helm action
// began on the cluster meanwhile.
func (s *Session) swapBackend(old *pooled, b *Backend, steps *connectSteps) (swapped, error) {
	st := s.state
	st.mu.Lock()
	if st.closed || st.pool[b.Context] != old {
		st.mu.Unlock()
		b.close(s.log)
		return swapped{}, errReconnectLeft
	}
	if err := s.busyOn(b.Context); err != nil {
		st.mu.Unlock()
		b.close(s.log)
		return swapped{}, err
	}
	s.attachCustomKinds(b)
	s.noteAccessReviews(b, steps) // their first answers, when they come (accessreviews.go)
	s.attachNamespaceWatch(b)     // before the access reviews, whose goroutine pokes it (connect_run.go)
	s.attachAccess(b)
	s.attachRefusals(b)
	if line := insecureLogLine(b); line != "" {
		steps.step(LogWarn, cluster.StepTLS, line, 0)
	}
	sw := swapped{gen: st.newGenLocked()}
	b.watch = newConnWatch(s, b, sw.gen)
	logConnectedSteps(steps, b)
	sw.sessions = old.b.watch.takeSessions()
	sw.forwards = old.b.forwards.carried()
	old.stopCold()
	e := &pooled{b: b, gen: sw.gen}
	st.pool[b.Context] = e
	if sw.shown = st.shown == old.b; sw.shown {
		st.detachViewsLocked() // the views on the old backend release its cache
		st.shown = b           // and the next reconcile starts them on the new one
	} else {
		st.armColdLocked(e) // nothing of it is open: cold once nothing of it is warm
	}
	st.mu.Unlock()
	return sw, nil
}

// newGenLocked hands out the number of a connection made without a shown connect (a background
// connect's range, connect_background.go): it never takes a shown connect's sequence number.
// state.mu is held.
func (st *state) newGenLocked() uint64 {
	st.bgGen++
	return bgGenBase + st.bgGen
}

// finishSwap makes the UI state follow the new backend b of a swap, ends what the old one ran,
// closes it and starts b.
func (s *Session) finishSwap(old *pooled, b *Backend, sw swapped) {
	now := s.Now()
	ended := make(map[string]bool, len(sw.sessions))
	for _, t := range sw.sessions {
		ended[t.tab] = true
		t.endOnce()
	}
	conn := Connection{State: Connected, Demo: b.Demo, TLSUnchecked: tlsUnchecked(b), CredentialPlugin: b.CredentialPlugin}
	s.UpdateUI(func(u *UIState) {
		if u.Context == b.Context && u.connGen == old.gen && u.Connection.State.Live() {
			u.Connection = conn
			u.connGen = sw.gen // the new connection's watch writes it from now on
		}
		if i := u.connectionIndex(b.Context, old.gen); i >= 0 {
			u.Connections[i].gen = sw.gen
			u.Connections[i].Connection = conn
			u.Connections[i].LoginLapsed = false
		}
		for i := range u.Dock {
			t := &u.Dock[i]
			switch {
			case t.Context != b.Context:
			case t.Type == DockLogs:
				t.Session++ // the window opens the stream again, on the new connection
			case ended[t.ID]:
				t.Interrupted, t.Reconnected = now, true
			}
		}
	})
	for _, t := range sw.sessions {
		go func() {
			defer s.recoverTo("ending a shell")
			_ = t.Close()
		}()
	}
	old.b.close(s.log)
	b.watch.start()
	s.applyLoginHolds() // a cluster that is not shown may hold its login (connwatch_lapse.go)
	s.carryForwards(b, sw.forwards)
}

// takeSessions takes the shells, attach and node shells the watch follows. Nil-safe.
func (w *connWatch) takeSessions() []*trackedSession {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.takeSessionsLocked()
}

// endOnce marks the session ended by its connection's end (interrupted, its tab's Reconnect).
func (t *trackedSession) endOnce() {
	select {
	case <-t.ended:
	default:
		close(t.ended)
	}
}

// carriedForward is a port forward that listened on the old connection, to listen again on the new
// one: its object's name, what it forwards, the local port it had, its UID and creation time.
type carriedForward struct {
	ref  model.ObjectRef
	spec ForwardSpec
	meta forwardMetaInfo
}

// carried returns the forwards that listen, with the ports they listen on.
func (fs *forwardSet) carried() []carriedForward {
	fs.mu.Lock()
	runs := make([]*forwardRun, 0, len(fs.runs))
	for _, r := range fs.runs {
		runs = append(runs, r)
	}
	fs.mu.Unlock()
	out := make([]carriedForward, 0, len(runs))
	for _, r := range runs {
		f := r.snapshot()
		if !f.Listening() {
			continue
		}
		spec := r.spec
		spec.LocalPort = f.LocalPort
		out = append(out, carriedForward{ref: r.ref, spec: spec, meta: forwardMetaInfo{uid: r.uid, created: r.created}})
	}
	slices.SortFunc(out, func(a, b carriedForward) int { return strings.Compare(a.ref.Key(), b.ref.Key()) })
	return out
}

// carryForwards starts on b the forwards the old connection ran, each on its local port again; one
// that cannot listen there says so in the connection log and its object (Failed).
func (s *Session) carryForwards(b *Backend, list []carriedForward) {
	if len(list) == 0 {
		return
	}
	fw, ok := b.Writer().(PortForwarder)
	prof, served := b.Profile(PortForwardKind)
	if !ok || !served || !prof.Local {
		s.state.logs.add(b.Context, LogWarn, fmt.Sprintf("%d port forwards ended with the old connection: the new one cannot forward ports", len(list)))
		return
	}
	fs := &b.forwards
	for _, c := range list {
		meta := c.meta
		fs.opMu.Lock()
		_, err := s.startRunWith(b, fw, prof, c.ref, c.spec, &meta)
		fs.opMu.Unlock()
		if err != nil {
			s.state.logs.add(b.Context, LogWarn, "Port forward "+c.ref.Namespace+"/"+c.ref.Name+" could not listen again: "+err.Error())
		}
	}
}

// renewFromFile reconnects b by itself when the API server rejects its login and its kubeconfig
// file holds another one than it was made with, its context running no credential plugin (D-81):
// a certificate or token the user renewed in the file. Each version of the file is tried once.
// It reads the kubeconfig file and returns; the reconnect runs on a goroutine of its own.
func (s *Session) renewFromFile(b *Backend) {
	if b == nil || b.CredentialPlugin || b.Demo || b.Conn == nil || b.watch.current() != LoginExpired || !s.state.holds(b) {
		return
	}
	fp, changed, err := b.Conn.LoginInFile()
	if err != nil || !changed || !b.renewal.try(fp) {
		return
	}
	s.goSafe("reconnecting with the renewed login", func() {
		if err := s.reconnect(context.Background(), b.Context, b, reconnectByFile); err != nil {
			s.log.Info("engine: reconnecting with the kubeconfig's renewed login failed", "context", b.Context, "err", err)
		}
	})
}

// renewChangedLogins looks, after the kubeconfig sources changed, for connections whose login the
// API server rejects while their file holds another one now (renewFromFile).
func (s *Session) renewChangedLogins() {
	for _, b := range s.Backends() {
		s.renewFromFile(b)
	}
}
