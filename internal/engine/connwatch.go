package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// The connection watch (D-58, standby and lost connections; owner, 2026-10-15). A computer that
// sleeps drops every connection, and nothing told the user: the connection said Connected while
// the views stood still. Each connection to a real cluster now has a watch that finds out, says
// so, and reconnects:
//
//   - Three signals check the connection at once: macOS's wake (NoteWake), a change of the network
//     (NoteNetworkChange), and a list or watch of the cache that broke (cache.OnStreamError); and
//     while connected a heartbeat (GET /version with the connection's credentials, every 15 s, 5 s
//     to answer) checks it anyway. Two failed checks in a row (the second 2 s after the first; one
//     at wake) make the connection Interrupted.
//   - Interrupted: the views keep the cache's last state, every mutating command is disabled
//     ("connection interrupted", writeBlocked), the gates of package cluster hold the informers'
//     lists and watches and refuse every other request at once, and the connection's network
//     connections are closed so that nothing waits on a dead one. Cogpit tries to reach the
//     cluster at once on wake and on a network change, and after 1, 2, 4, 8, 16, then every 30
//     seconds, without end; cluster.reconnect connects anew from the kubeconfig file at once
//     (reconnect.go, D-81). A try asks without credentials (Reachable), so it never runs a credential
//     plugin; once the server answers, one heartbeat checks the login and the connection is back:
//     the held requests go out, the informers relist or watch on by themselves, and the cache,
//     the tabs and the views stay.
//   - A login the API server rejects: the first 401 reaches client-go's credential plugin, which
//     runs once to renew it (an approved plugin only: one whose program changed since it was
//     allowed is not run, D-26); a second check that is rejected too, or a plugin that has not
//     answered within 30 s (it may wait for a browser login), makes it LoginExpired: no request
//     reaches the plugin until the user logs in again (cluster.login), so no plugin runs in the
//     background and no browser tabs pile up.
//   - A cluster that is not shown, whose cluster setting "Renew the login while the cluster is not
//     shown" is off (D-69 item 4, loginhold.go), holds its credential plugin: a login that expires
//     or is rejected meanwhile lapses (stateLapsed) instead of being renewed. The UI state says so
//     (ConnectedCluster.LoginLapsed, the connection Connected: whether a renewal would work is not
//     known, so it is not red); the heartbeat pauses, writes are disabled ("login lapsed"), streams
//     that have to open again wait. Showing the cluster renews the login as above (one run of the
//     plugin, LoginExpired if that does not help); so does Log in again.
type connWatch struct {
	s   *Session
	b   *Backend
	p   HealthProber
	o   ConnWatchOptions
	gen uint64 // the connect's number, as UIState.connGen holds it

	ctx    context.Context // ends with stop: a login waiting for its plugin is abandoned
	cancel context.CancelFunc
	kicks  chan connKick
	done   chan struct{}
	once   sync.Once

	mu            sync.Mutex
	state         ConnectionState // Connected, Interrupted, LoginExpired or stateLapsed
	since         time.Time
	next          time.Time     // when the next attempt is due, on the session's clock (shown)
	delay         time.Duration // the wait before it (waited)
	attempt       int
	reason        string
	loggingIn     bool
	needsApproval bool
	lastFrom      time.Time     // the last interruption that ended: when it began
	lastTo        time.Time     // and when it ended
	back          chan struct{} // closed while Connected; a new one at each interruption
	broken        chan struct{} // closed while not Connected; a new one at each reconnect
	waiters       []chan struct{}
	sessions      map[*trackedSession]struct{} // pod shells, node shells, attach (connwatch_streams.go)
	pending       chan error                   // a heartbeat that has not returned yet (its plugin runs)
	loggedReason  string                       // the reason the connection log has of the last failed try (connwatch_log.go)
	held          bool                         // the engine holds the login: the cluster is not shown and its setting is off (loginhold.go)
	renewing      bool                         // a renewal of a lapsed login runs (showing the cluster, Log in again): the plugin may run
	candidate     string                       // a version one heartbeat read, which the next confirms (connwatch_version.go)
	upgrade       *ServerUpgrade               // the server answers with another minor than the connect's (Connection.Upgraded)

	quiet atomic.Bool // a reconnect replaces the connection: the reconnect's attempt is the one the log writes (reconnect.go)
}

// ConnWatchOptions tune the connection watch; zero fields are the owner's values (D-58), and
// Cogpit's for ProbeSlack.
type ConnWatchOptions struct {
	// Heartbeat is the time between two heartbeats while connected (15 s).
	Heartbeat time.Duration
	// ProbeTimeout bounds one heartbeat or reconnect attempt (5 s).
	ProbeTimeout time.Duration
	// Confirm is the time before the second check after one that failed (2 s).
	Confirm time.Duration
	// LoginWait is how long a credential plugin that renews a login may take before the login
	// counts as expired (30 s).
	LoginWait time.Duration
	// BackoffFirst and BackoffMax bound the waits between reconnect attempts (1 s doubling to 30 s).
	BackoffFirst, BackoffMax time.Duration
	// ProbeSlack is how much longer than ProbeTimeout a heartbeat may take before it counts as
	// waiting for its credential plugin, which client-go runs outside the request (1 s).
	ProbeSlack time.Duration
}

// The owner's values (D-58).
const (
	DefaultHeartbeat    = 15 * time.Second
	DefaultProbeTimeout = 5 * time.Second
	DefaultConfirm      = 2 * time.Second
	DefaultLoginWait    = 30 * time.Second
	DefaultBackoffFirst = time.Second
	DefaultBackoffMax   = 30 * time.Second
)

// defaultProbeSlack is ProbeSlack's value (Cogpit's: room for client-go to run the plugin).
const defaultProbeSlack = time.Second

func (o ConnWatchOptions) withDefaults() ConnWatchOptions {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&o.Heartbeat, DefaultHeartbeat)
	def(&o.ProbeTimeout, DefaultProbeTimeout)
	def(&o.Confirm, DefaultConfirm)
	def(&o.LoginWait, DefaultLoginWait)
	def(&o.BackoffFirst, DefaultBackoffFirst)
	def(&o.BackoffMax, DefaultBackoffMax)
	def(&o.ProbeSlack, defaultProbeSlack)
	return o
}

// HealthProber is what the watch asks of a connection: *cluster.Conn, or a fake in tests.
type HealthProber interface {
	// Heartbeat asks the API server with the connection's credentials; an error wrapping
	// cluster.ErrLoginRejected when it rejects them.
	Heartbeat(ctx context.Context) error
	// Reachable asks without credentials: nil when the server answers at all.
	Reachable(ctx context.Context) error
	// SetHealth holds or lets go the connection's requests.
	SetHealth(cluster.Health)
	// ResetConnections closes the connection's network connections.
	ResetConnections()
	// ArmLogin lets the next 401 reach the credential plugin.
	ArmLogin()
}

// Why writes are disabled (S4): the reason the window and agents read.
var (
	errConnInterrupted = errors.New("connection interrupted")
	errConnLoginGone   = errors.New("login expired: log in again first")
	errConnLoginLapsed = errors.New("login lapsed: show the cluster to renew it")
)

// stateLapsed is the watch's own state of a login that lapsed while it was held (D-69 item 4): the
// UI state shows it as Connected with ConnectedCluster.LoginLapsed, never as this value.
const stateLapsed ConnectionState = "loginLapsed"

// lapsedReason is the connection's reason while its login lapsed.
const lapsedReason = "the login lapsed; it is renewed when the cluster is shown"

type connKick int

const (
	kickWake    connKick = iota + 1 // the computer woke
	kickNetwork                     // the network changed
	kickStream                      // a list, a watch or a stream broke
	kickNow                         // try at once (TryNow)
	kickLogin                       // Log in again
	kickRenew                       // the hold ended (the cluster is shown, or its setting is on again): renew a lapsed login
)

// loginWaitReason is LoginExpired's reason when the credential plugin did not answer in time.
const loginWaitReason = "the login program has not answered for %s; it may wait for you to log in, in a browser or a terminal"

// newConnWatch makes the watch of a backend that is about to be published; start runs it.
func newConnWatch(s *Session, b *Backend, gen uint64) *connWatch {
	p := b.Health
	if p == nil && b.Conn != nil && b.Conn.Probed() {
		p = b.Conn
	}
	if p == nil {
		return nil // the demo cluster: nothing to lose
	}
	ctx, cancel := context.WithCancel(context.Background())
	back := make(chan struct{})
	close(back)
	return &connWatch{
		s: s, b: b, p: p, o: s.opts.ConnWatch.withDefaults(), gen: gen, ctx: ctx, cancel: cancel,
		kicks: make(chan connKick, 8), done: make(chan struct{}), state: Connected,
		back: back, broken: make(chan struct{}), sessions: map[*trackedSession]struct{}{},
	}
}

// start runs the watch and follows the cache's broken streams.
func (w *connWatch) start() {
	if w == nil {
		return
	}
	if w.b.Cache != nil {
		w.b.Cache.OnStreamError(func(cache.Scope, error) { w.kick(kickStream) })
	}
	go w.run()
}

// stop ends the watch, waiting a bounded time for its loop. Nil-safe.
func (w *connWatch) stop() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		w.cancel()
		if w.b.Cache != nil {
			w.b.Cache.OnStreamError(nil)
		}
	})
	select {
	case <-w.done:
	case <-time.After(5 * time.Second):
		w.s.log.Warn("engine: the connection watch did not stop in time", "context", w.b.Context)
	}
	w.mu.Lock()
	sessions := w.takeSessionsLocked()
	w.mu.Unlock()
	for _, t := range sessions {
		t.untrack() // the backend closes; the window closes its tabs' sessions itself
	}
}

// logs is where the watch writes the connection log (D-66): the session's, nil while a reconnect
// that replaces the connection runs (reconnect.go, D-81), whose attempt is the one that runs then;
// the log's writers take nil as "write nothing".
func (w *connWatch) logs() *connectLogs {
	if w.quiet.Load() {
		return nil
	}
	return &w.s.state.logs
}

// setQuiet stops or starts the watch's lines in the connection log (logs). Nil-safe.
func (w *connWatch) setQuiet(on bool) {
	if w != nil {
		w.quiet.Store(on)
	}
}

// kick asks the loop to look now; a kick already queued covers it.
func (w *connWatch) kick(k connKick) {
	if w == nil {
		return
	}
	select {
	case w.kicks <- k:
	default:
	}
}

// current returns the watch's state.
func (w *connWatch) current() ConnectionState {
	if w == nil {
		return Connected
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *connWatch) run() {
	defer close(w.done)
	defer w.s.recoverTo("the connection watch")
	var fails, rejects int
	for {
		var ok bool
		switch w.current() {
		case Interrupted:
			ok = w.interrupted()
		case LoginExpired:
			ok = w.expired()
		case stateLapsed:
			ok = w.lapsedStep()
		default:
			ok = w.healthy(&fails, &rejects)
		}
		if !ok {
			return
		}
	}
}

// wait waits d, or for a kick that accept takes; false once the watch stops.
func (w *connWatch) wait(d time.Duration, accept func(connKick) bool) (connKick, bool) {
	t := time.NewTimer(max(d, 0))
	defer t.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return 0, false
		case <-t.C:
			return 0, true
		case k := <-w.kicks:
			if accept(k) {
				return k, true
			}
		}
	}
}

// healthy is one step while connected: the next heartbeat, or a signal.
func (w *connWatch) healthy(fails, rejects *int) bool {
	d := w.o.Heartbeat
	if *fails > 0 || *rejects > 0 {
		d = w.o.Confirm
	}
	k, ok := w.wait(d, func(connKick) bool { return true })
	if !ok {
		return false
	}
	// At wake the check may renew a login that expired in the night: an approved plugin only (D-26).
	if k == kickWake && w.approvalChanged() {
		w.expire("the login program of this context changed since you allowed it; log in again to review it", true)
		return true
	}
	res, err := w.check(w.o.LoginWait)
	switch res {
	case checkQuit:
		return false
	case checkOK:
		if *rejects > 0 { // the login the plugin renewed works (connectlog.go)
			w.logs().end(w.b.Context, AttemptLogin, OutcomeOK, LogInfo, "The renewed login works")
		}
		*fails, *rejects = 0, 0
		w.verdict()
	case checkDown:
		*fails++
		if *fails >= 2 || k == kickWake { // after a sleep one failure says enough
			*fails, *rejects = 0, 0
			w.interrupt(err)
		}
	case checkRejected:
		*rejects++
		if *rejects == 1 { // a login attempt: the guard handed this 401 to the plugin (connectlog.go)
			w.logs().begin(w.b.Context, AttemptLogin, LogInfo,
				"The API server rejected the login (401 Unauthorized); a credential plugin renews it once, and the next check tells")
		}
		if *rejects >= 2 { // the first 401 renewed the login once (the guard); this one did not help
			*fails, *rejects = 0, 0
			w.expire(rejectedReason(err), false)
		}
	case checkWaiting:
		*fails, *rejects = 0, 0
		w.expire(fmt.Sprintf(loginWaitReason, w.o.LoginWait), false)
	case checkLapsed:
		*fails, *rejects = 0, 0
		w.lapse()
	case checkEnded: // Cogpit ended the plugin's run: Log in again starts it again (D-73)
		*fails, *rejects = 0, 0
		w.expire(endedReason(err), false)
	}
	return true
}

// interrupted is one step while interrupted: the next attempt, at its time or at once on a wake, a
// network change or TryNow.
func (w *connWatch) interrupted() bool {
	w.mu.Lock()
	d := w.delay
	w.mu.Unlock()
	k, ok := w.wait(d, func(k connKick) bool { return k == kickWake || k == kickNetwork || k == kickNow })
	if !ok {
		return false
	}
	if k != 0 {
		w.mu.Lock()
		w.attempt = 0 // at once, and the backoff starts again
		w.mu.Unlock()
	}
	w.attempting()
	ctx, cancel := context.WithTimeout(w.ctx, w.o.ProbeTimeout)
	err := w.p.Reachable(ctx)
	cancel()
	if w.ctx.Err() != nil {
		return false
	}
	if err != nil {
		w.retryLater(err)
		return true
	}
	// The server answers: one check of the login, which may renew it once.
	if w.approvalChanged() {
		w.expire("the login program of this context changed since you allowed it; log in again to review it", true)
		return true
	}
	return w.afterReach()
}

// afterReach checks the login of a cluster that answers again and decides.
func (w *connWatch) afterReach() bool {
	res, err := w.check(w.o.LoginWait)
	if res == checkRejected { // the guard handed this 401 to the plugin, which ran once: ask again
		res, err = w.check(w.o.LoginWait)
	}
	switch res {
	case checkQuit:
		return false
	case checkOK:
		w.reconnect()
	case checkDown:
		w.retryLater(err)
	case checkRejected:
		w.expire(rejectedReason(err), false)
	case checkWaiting:
		w.expire(fmt.Sprintf(loginWaitReason, w.o.LoginWait), false)
	case checkLapsed: // the cluster answers again, its login lapsed meanwhile and is held
		w.lapse()
	case checkEnded:
		w.expire(endedReason(err), false)
	}
	return true
}

// expired is one step while the login is expired: a login the user asks for, or the answer of a
// heartbeat whose plugin still ran (the user logged in in the browser after all).
func (w *connWatch) expired() bool {
	w.mu.Lock()
	pending := w.pending
	w.mu.Unlock()
	for {
		select {
		case <-w.ctx.Done():
			return false
		case _ = <-pending: // nil blocks; whatever it says, afterReach asks again
			w.mu.Lock()
			w.pending = nil
			w.mu.Unlock()
			return w.afterReach() // the plugin answered: what does the login say now
		case k := <-w.kicks:
			if k == kickLogin {
				w.login()
				return true
			}
		}
	}
}

// login renews an expired login: the credential plugin may run once, with its browser, for as
// long as the user takes.
func (w *connWatch) login() {
	if w.approvalChanged() {
		w.expire("the login program of this context changed since you allowed it; connect again to review it", true)
		return
	}
	w.setLoggingIn(true)
	defer w.setLoggingIn(false)
	w.setRenewing(true) // a held login is let go for the user's own login (loginhold.go)
	defer w.setRenewing(false)
	w.logs().begin(w.b.Context, AttemptLogin, LogInfo, "Logging in again (Log in again): the credential plugin may run once, with its browser")
	w.p.ArmLogin()
	w.mu.Lock()
	ch := w.pending
	w.pending = nil
	w.mu.Unlock()
	if ch == nil { // else the plugin runs already: its answer is this login's
		ch = make(chan error, 1)
		w.heartbeat(w.ctx, ch)
	}
	select {
	case <-w.ctx.Done():
		return
	case _ = <-ch:
	}
	// Whatever the first answer was, it came with or before the plugin's run: ask again.
	switch res, err := w.check(w.o.LoginWait); res {
	case checkOK:
		w.reconnect()
	case checkDown:
		w.interrupt(err)
	case checkRejected:
		w.expire(strings.Replace(rejectedReason(err), "rejects", "still rejects", 1), false) // "… still rejects the login (401 Unauthorized)"
	case checkWaiting:
		w.expire(fmt.Sprintf(loginWaitReason, w.o.LoginWait), false)
	case checkLapsed:
		w.lapse()
	case checkEnded:
		w.expire(endedReason(err), false)
	}
}

type checkResult int

const (
	checkOK checkResult = iota
	checkDown
	checkRejected
	checkWaiting // no answer while the credential plugin runs
	checkQuit
	checkLapsed // the request needed the credential plugin, which the hold kept from running
	checkEnded  // Cogpit ended the credential plugin's run (its bound, a cancel), or did not start it again (D-73)
)

// heartbeat runs one heartbeat on its own goroutine and sends its error to ch.
func (w *connWatch) heartbeat(ctx context.Context, ch chan<- error) {
	go func() {
		var err error
		defer func() {
			if r := recover(); r != nil {
				w.s.log.Error("engine: a heartbeat panicked", "panic", fmt.Sprint(r))
				err = fmt.Errorf("internal error: %v", r)
			}
			ch <- err
		}()
		err = w.p.Heartbeat(ctx)
	}()
}

// check runs a heartbeat bounded by ProbeTimeout. One that does not return although its request's
// deadline passed waits for its credential plugin: check waits for it up to bound, and asks again
// once it returned (its own answer came after its deadline); past bound the heartbeat is kept as
// pending and the result is checkWaiting.
func (w *connWatch) check(bound time.Duration) (checkResult, error) {
	ch := make(chan error, 1)
	ctx, cancel := context.WithTimeout(w.ctx, w.o.ProbeTimeout)
	w.heartbeat(ctx, ch)
	first := time.NewTimer(w.o.ProbeTimeout + w.o.ProbeSlack)
	defer first.Stop()
	select {
	case err := <-ch:
		cancel()
		w.logPluginStderr(err)
		return w.classify(err), err
	case <-first.C:
	case <-w.ctx.Done():
		cancel()
		return checkQuit, nil
	}
	rest := time.NewTimer(max(bound-w.o.ProbeTimeout-w.o.ProbeSlack, 0))
	defer rest.Stop()
	select {
	case _ = <-ch:
		cancel()
		// The plugin answered within the bound; this heartbeat's own request ran out of time.
		again := make(chan error, 1)
		ctx2, cancel2 := context.WithTimeout(w.ctx, w.o.ProbeTimeout)
		defer cancel2()
		w.heartbeat(ctx2, again)
		select {
		case err := <-again:
			w.logPluginStderr(err)
			return w.classify(err), err
		case <-w.ctx.Done():
			return checkQuit, nil
		}
	case <-rest.C:
		w.mu.Lock()
		w.pending = ch
		w.mu.Unlock()
		go func() { <-w.ctx.Done(); cancel() }() // the request ends with the watch; the plugin runs on
		return checkWaiting, nil
	case <-w.ctx.Done():
		cancel()
		return checkQuit, nil
	}
}

// classify is a heartbeat's result; one that answered also tells the server's version
// (noteVersion).
func (w *connWatch) classify(err error) checkResult {
	res := classifyCheck(err)
	if res == checkOK {
		w.noteVersion()
	}
	return res
}

func classifyCheck(err error) checkResult {
	var ended *cluster.LoginEndedError
	switch {
	case err == nil:
		return checkOK
	case errors.As(err, &ended):
		return checkEnded
	case errors.Is(err, cluster.ErrLoginLapsed):
		return checkLapsed
	case errors.Is(err, cluster.ErrLoginRejected):
		return checkRejected
	}
	return checkDown
}

// rejectedReason is LoginExpired's reason for a rejected login.
func rejectedReason(err error) string {
	if err == nil {
		return "the API server rejects the login"
	}
	return "the API server rejects the login (401 Unauthorized)"
}

// downReason is Interrupted's reason, without credentials and on one line.
func downReason(err error) string {
	if err == nil {
		return "the cluster does not answer"
	}
	msg := kubeconfig.SafeError(err).Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "Client.Timeout") || errors.Is(err, cluster.ErrNoAnswer):
		return "no answer from the API server"
	}
	return oneLine(kubeconfig.DisplayText(msg, 300))
}

// approvalChanged reports whether the context's credential plugin may no longer run as it is now:
// not allowed, or its program changed since it was (D-26). A context without a plugin, or a session
// without kubeconfig sources, has nothing to check.
func (w *connWatch) approvalChanged() bool {
	info, err := w.s.ExecPlugin(w.b.Context)
	if err != nil {
		return false
	}
	return info.State != ExecApproved
}

// verdict ends the waits for a check (await): the connection answered.
func (w *connWatch) verdict() {
	w.mu.Lock()
	w.releaseWaitersLocked()
	w.mu.Unlock()
}

func (w *connWatch) releaseWaitersLocked() {
	for _, ch := range w.waiters {
		close(ch)
	}
	w.waiters = nil
}

// interrupt makes the connection Interrupted: the gates hold, the network connections close, the
// shells end, the long operations stop, the window says so.
func (w *connWatch) interrupt(cause error) {
	now := w.s.Now()
	w.mu.Lock()
	if w.state == Connected {
		close(w.broken)
		w.back = make(chan struct{})
	}
	w.state, w.since, w.reason = Interrupted, now, downReason(cause)
	w.attempt = 1
	w.delay = w.o.BackoffFirst
	w.next = now.Add(w.delay)
	w.needsApproval = false
	w.releaseWaitersLocked()
	sessions := w.takeSessionsLocked()
	reason := w.reason
	w.mu.Unlock()
	w.p.SetHealth(cluster.HealthInterrupted)
	w.p.ResetConnections()
	w.logInterrupted(reason, cause) // a reconnect attempt begins (connectlog.go)
	w.s.log.Warn("engine: connection interrupted", "context", w.b.Context, "reason", reason)
	w.publish()
	w.s.endSessions(w, sessions, now) // after the state: the tabs' Reconnect waits for the connection
}

// expire makes the login LoginExpired. Coming from an interruption, its time stays: the views show
// the state of then.
func (w *connWatch) expire(reason string, needsApproval bool) {
	now := w.s.Now()
	w.mu.Lock()
	if w.state == Connected {
		close(w.broken)
		w.back = make(chan struct{})
		w.since = now
	} else if w.since.IsZero() {
		w.since = now
	}
	w.state, w.reason, w.needsApproval = LoginExpired, reason, needsApproval
	w.next = time.Time{}
	w.releaseWaitersLocked()
	w.mu.Unlock()
	w.p.SetHealth(cluster.HealthLoginExpired)
	// The attempt that runs (a login, a reconnect) failed; without one, a login renewal by the
	// plugin's own expiry did (connectlog.go).
	w.logs().end(w.b.Context, AttemptLogin, OutcomeFailed, LogWarn, "Login expired: "+reason)
	w.s.log.Warn("engine: login expired", "context", w.b.Context, "reason", reason)
	w.publish()
	w.s.renewFromFile(w.b) // without a credential plugin, a login renewed in the kubeconfig file (reconnect.go, D-81)
}

// reconnect makes the connection Connected again: the held requests go out, the streams waiting
// for it go on.
func (w *connWatch) reconnect() {
	now := w.s.Now()
	w.mu.Lock()
	from := w.since
	kind := AttemptReconnect
	lapsed := w.state == stateLapsed
	if w.state == LoginExpired || lapsed {
		kind = AttemptLogin
	}
	w.lastFrom, w.lastTo = from, now
	w.state, w.since, w.reason, w.next, w.needsApproval = Connected, time.Time{}, "", time.Time{}, false
	w.attempt = 0
	close(w.back)
	w.broken = make(chan struct{})
	w.releaseWaitersLocked()
	w.mu.Unlock()
	w.p.SetHealth(cluster.HealthOK)
	if a, ok := w.b.Access.(*accessReviews); ok {
		a.forgetFailures() // the reviews the interruption failed are asked again
	}
	w.s.renewUser(w.b) // the login may be another user's now (whoami.go, D-68)
	away := now.Sub(from)
	text := fmt.Sprintf("Reconnected to %s after %s", w.b.Context, FormatSpan(away))
	switch {
	case lapsed:
		text = fmt.Sprintf("Renewed the login to %s, lapsed for %s", w.b.Context, FormatSpan(away))
	case kind == AttemptLogin:
		text = fmt.Sprintf("Logged in again to %s; the connection was away %s", w.b.Context, FormatSpan(away))
	}
	w.logs().end(w.b.Context, kind, OutcomeOK, LogInfo, text)
	w.s.log.Info("engine: reconnected", "context", w.b.Context, "after", away.Round(time.Millisecond))
	w.publish()
	if lapsed {
		return // the user showed the cluster or logged in: the window shows the rest, no toast
	}
	w.s.emit(Event{Type: EventToast, Level: LevelInfo, Message: fmt.Sprintf("Reconnected to %s (interrupted %s–%s)",
		kubeconfig.DisplayText(w.b.Context, 200), w.s.clockText(from), w.s.clockText(now))})
}

// attempting says an attempt runs now.
func (w *connWatch) attempting() {
	w.mu.Lock()
	w.next = time.Time{}
	w.mu.Unlock()
	w.publish()
}

// retryLater schedules the next attempt after the backoff: 1, 2, 4, 8, 16, then every 30 seconds.
func (w *connWatch) retryLater(err error) {
	w.mu.Lock()
	w.delay = min(w.o.BackoffFirst<<min(w.attempt, 16), w.o.BackoffMax)
	w.attempt++
	w.next = w.s.Now().Add(w.delay)
	w.reason = downReason(err)
	reason, delay := w.reason, w.delay
	w.mu.Unlock()
	w.logTry(reason, delay)
	w.publish()
}

func (w *connWatch) setLoggingIn(on bool) {
	w.mu.Lock()
	w.loggingIn = on
	w.mu.Unlock()
	w.publish()
}

// connection is the UI state's Connection as the watch sees it, its Demo and TLS fields kept.
func (w *connWatch) connection(old Connection) Connection {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.state
	if st == stateLapsed {
		st = Connected // not red: whether a renewal would work is not known (D-69 item 4)
	}
	wait := w.b.login.wait() // a run of its credential plugin under way (loginwait.go, D-73)
	if wait.Program == "" && old.Reconnecting {
		wait = old.WaitingForLogin // the reconnect's connect waits for it (reconnect.go, D-81)
	}
	return Connection{
		State: st, Demo: old.Demo, TLSUnchecked: old.TLSUnchecked, CredentialPlugin: old.CredentialPlugin, Reconnecting: old.Reconnecting,
		Since: w.since, NextAttempt: w.next, Reason: w.reason, LoggingIn: w.loggingIn || w.renewing, NeedsApproval: w.needsApproval,
		WaitingForLogin: wait, Upgraded: w.upgrade,
	}
}

// lapsedShown reports whether the UI state shows the login as lapsed: it lapsed and no renewal
// runs.
func (w *connWatch) lapsedShown() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state == stateLapsed && !w.renewing
}

// publish writes the watch's state into the UI state: the shown cluster's Connection, or its entry
// of Connections while another cluster is shown (D-69); nothing once its backend left the pool or
// another connect of its context took over.
func (w *connWatch) publish() {
	if !w.s.state.holds(w.b) {
		return
	}
	lapsed := w.lapsedShown()
	w.s.UpdateUI(func(u *UIState) {
		if u.connGen == w.gen && u.Context == w.b.Context && u.Connection.State.Live() {
			u.Connection = w.connection(u.Connection) // syncConnections mirrors it into its entry
			if i := u.connectionIndex(w.b.Context, w.gen); i >= 0 {
				u.Connections[i].LoginLapsed = lapsed
			}
			return
		}
		if i := u.connectionIndex(w.b.Context, w.gen); i >= 0 && u.Connections[i].Connection.State.Live() {
			u.Connections[i].Connection = w.connection(u.Connections[i].Connection)
			u.Connections[i].LoginLapsed = lapsed
		}
	})
}

// needsNewConnect reports whether the connection's login expired and only a new connect may renew
// it, its credential plugin having changed since it was allowed (D-26): connecting the context
// again then connects anew instead of showing the pooled backend. Nil-safe.
func (w *connWatch) needsNewConnect() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state == LoginExpired && w.needsApproval
}

// connWatch returns the watch of a connected context's backend, the shown one for ""; nil without
// one (the demo cluster, a context that is not connected).
func (s *Session) connWatch(contextID string) *connWatch {
	if b := s.BackendOf(contextID); b != nil {
		return b.watch
	}
	return nil
}

// NoteWake tells the session that the computer woke from sleep (macOS's
// NSWorkspaceDidWakeNotification): every connection is checked at once, and an interrupted one
// tried at once (D-69 item 7: every connected cluster, shown or not).
func (s *Session) NoteWake() {
	for _, b := range s.Backends() {
		b.watch.kick(kickWake)
	}
}

// NoteNetworkChange tells the session that the computer's network changed (an interface came or
// went, a VPN): as NoteWake, but a single failed check does not interrupt a connection.
func (s *Session) NoteNetworkChange() {
	for _, b := range s.Backends() {
		b.watch.kick(kickNetwork)
	}
}

// writeBlocked is the reason a command of danger d on a context's cluster ("" the shown one)
// cannot start: a write on a connection that is interrupted or whose login expired (S4). Each
// cluster's connection decides for its own commands (D-69): an interrupted cluster the window does
// not show blocks the writes to it, not those to the shown one. Reads and UI commands run on.
func (s *Session) writeBlocked(d Danger, contextID string) error {
	if s == nil || (d != DangerMutating && d != DangerDestructive) {
		return nil
	}
	switch s.connWatch(contextID).current() {
	case Interrupted:
		return errConnInterrupted
	case LoginExpired:
		return errConnLoginGone
	case stateLapsed:
		return errConnLoginLapsed
	}
	return nil
}

// TryNow tries to reach a connected context's cluster ("" the shown one) at once while its
// connection is interrupted and starts the backoff again, as a wake does for every connection; it
// asks without credentials and runs no login program. cluster.reconnect connects anew instead
// (reconnect.go, D-81).
func (s *Session) TryNow(contextID string) error {
	if s.BackendOf(contextID) == nil && contextID != "" {
		return &NotConnectedError{Context: contextID}
	}
	w := s.connWatch(contextID)
	if w.current() != Interrupted {
		return errNotInterrupted
	}
	w.kick(kickNow)
	return nil
}

// errNotInterrupted refuses TryNow on a connection that works.
var errNotInterrupted = errors.New("the connection is not interrupted")

// ErrNeedsNewConnect refuses cluster.login when the context's credential plugin changed since it
// was allowed: only a new connect, which asks the user first, may run it (D-26).
var ErrNeedsNewConnect = errors.New("the login program of this context changed since you allowed it: connect to the context again, which asks you first")

// errNotExpired refuses cluster.login on a connection whose login works.
var errNotExpired = errors.New("the login has not expired")

// Login renews the shown cluster's expired login (cluster.login, the banner's Log in again): the
// context's credential plugin may run once, with its browser if it needs one.
func (s *Session) Login() error { return s.LoginContext("") }

// LoginContext is Login for a connected context, "" the shown one (cluster.login with a context):
// an expired login, or one that lapsed while the cluster was not shown (D-69 item 4), whose hold
// the login lets go for its run.
func (s *Session) LoginContext(contextID string) error {
	if s.BackendOf(contextID) == nil && contextID != "" {
		return &NotConnectedError{Context: contextID}
	}
	w := s.connWatch(contextID)
	if st := w.current(); st != LoginExpired && st != stateLapsed {
		return errNotExpired
	}
	if w.approvalChanged() {
		return ErrNeedsNewConnect
	}
	w.kick(kickLogin)
	return nil
}

// StaleNote is what an agent's read of the shown cluster is told while its connection is
// interrupted or its login expired (D-58): the cache's data is the cluster's state as of then, and
// writes are refused. "" otherwise.
func (s *Session) StaleNote() string {
	u := s.UIState()
	return staleNote(u.Context, u.Connection)
}

// StaleNoteFor is StaleNote for the cluster an invocation reads (D-69): the context its parameters
// name (a context parameter, an object's reference, a dock tab's cluster), else the shown one. The
// MCP server and the built-in agent add it to the results of read commands.
func (s *Session) StaleNoteFor(inv Invocation) string {
	contextID := ""
	if cmd, ok := s.reg.Get(inv.ID); ok {
		if p, err := cmd.decode(inv.Params); err == nil {
			contextID = targetContext(s, p)
		}
	}
	u := s.UIState()
	if contextID == "" || contextID == u.Context {
		return staleNote(u.Context, u.Connection)
	}
	if c, ok := u.ConnectionOf(contextID); ok {
		if c.LoginLapsed {
			return lapsedNote(contextID, c.Connection)
		}
		return staleNote(contextID, c.Connection)
	}
	return ""
}

// lapsedNote is what an agent's read of a cluster whose login lapsed while it was not shown is
// told (D-69 item 4): the cache's data may be older than the read, and what needs the cluster is
// refused until the cluster is shown; the agent's read never renews the login itself.
func lapsedNote(contextID string, c Connection) string {
	since := c.Since.UTC().Format(time.RFC3339)
	return fmt.Sprintf("Note from Cogpit: the login to %s lapsed (since %s) and Cogpit does not renew it while the cluster is not "+
		"shown (its cluster setting renewLoginInBackground is off); this is data from %s or later, as Cogpit's cache last saw it. "+
		"Reads that need the cluster and writes are refused until the cluster is shown in Cogpit's window, which renews the login.",
		kubeconfig.DisplayText(contextID, 200), since, since)
}

func staleNote(contextID string, c Connection) string {
	since := c.Since.UTC().Format(time.RFC3339)
	name := kubeconfig.DisplayText(contextID, 200)
	switch c.State {
	case Interrupted:
		return fmt.Sprintf("Note from Cogpit: the connection to %s is interrupted since %s and Cogpit is reconnecting by itself; "+
			"this is data from %s, the cluster's state as Cogpit's cache last saw it. Writes are refused until the connection is back "+
			"(ui_state's connection.state is then connected again).", name, since, since)
	case LoginExpired:
		return fmt.Sprintf("Note from Cogpit: the login to %s expired (since %s); this is data from %s or later, as Cogpit's cache "+
			"last saw it. Writes are refused until the user logs in again in Cogpit's window.", name, since, since)
	}
	return ""
}

// clockText writes t as the time of day in the user's zone and format, without seconds ("14:03",
// D-31).
func (s *Session) clockText(t time.Time) string { return s.TimeFormat().FormatClock(t) }
