package cluster

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/client-go/rest"
)

// A credential plugin's run that a connection waits for, ended when the user cancels and after a
// bound (D-73; the owner's finding of 2026-10-16 night: a connect that waited for an OIDC login in
// the browser, which the user had not finished, could not be cancelled, had no timeout, and kept
// Disconnect waiting behind it).
//
// client-go's exec authenticator runs the plugin with exec.Command and cmd.Run, without a context,
// under the Authenticator's lock (k8s.io/client-go v0.37.1, plugin/pkg/client/auth/exec/exec.go:
// 458–472), and every request that needs the credential waits for that lock whatever its own
// context says (getCreds, exec.go:402–415): a plugin that waits for a browser callback nobody sends
// keeps the connect, and every request of the connection, waiting for as long as it runs. client-go
// offers no hook to the process: no context, no *exec.Cmd, no SysProcAttr.
//
// So Cogpit follows the run where client-go starts it and ends the process itself:
//
//   - the wrapper of the Authenticator's interactiveFunc (loginhold.go), which refreshCredsLocked
//     asks before it builds the command (exec.go:436), notes a run's begin (pluginHold.begin); a
//     goroutine that takes the Authenticator's lock once the run let go of it notes its end;
//   - a run that has not ended after its bound (Options.LoginTimeout, DefaultLoginTimeout), or that
//     the user cancels (a cancelled Connect, Conn.CancelLogin, Conn.Close; Disconnect in the
//     engine), is ended: Cogpit signals the plugin's process, found among its own child processes
//     at the run's begin and followed by its number and start time (pluginproc.go), SIGTERM and
//     SIGKILL after killGrace if it still runs (TerminateProcess on Windows). Only the plugin's own
//     process: it shares Cogpit's process group, so there is no group of its own to signal, and a
//     browser it opened is no child of it (macOS's open hands the URL to the system) and is never
//     ended. A run whose process is not found Cogpit stops waiting for, and says so
//     (LoginEndedError.RunsOn): its process may run on (H-118);
//   - once Cogpit ended a run, the Authenticator starts the plugin no more until the user connects
//     or logs in again (rearm): client-go would start it again for each request that waited behind
//     the lock, a browser tab each. A request that needed it ends as a *LoginEndedError;
//   - a connection the user cancelled or closed asks for no run (loginhold.go, admit): a request of
//     it that waited behind the lock does not start the plugin once the run before it ended (WF38).
//
// A run serves every connection whose plugin configuration is the same (client-go keeps one
// Authenticator for them, loginhold.go): a cancel ends it only when no other connection that does
// not hold its login, and was not cancelled, uses it; the bound ends it whoever waits.

// DefaultLoginTimeout bounds one run of a credential plugin (Options.LoginTimeout; the lead's
// decision of the night run 2026-10-16/17, D-73): long enough for a browser login with a second
// factor, short of what a login nobody finishes would cost.
const DefaultLoginTimeout = 5 * time.Minute

// killGrace is how long a plugin has to end after SIGTERM before SIGKILL.
const killGrace = 3 * time.Second

// Why Cogpit ended a credential plugin's run (LoginEndedError.Cause).
var (
	// ErrLoginCancelled: the user cancelled the connect or the login (Disconnect, Cancel Connecting).
	ErrLoginCancelled = errors.New("login cancelled")
	// ErrLoginTimedOut: the run did not return within its bound.
	ErrLoginTimedOut = errors.New("the login did not finish in time")
)

// LoginEndedError ends a request that needed a credential plugin's run that Cogpit ended (Cause:
// ErrLoginCancelled or ErrLoginTimedOut), or that Cogpit did not start again after it ended one,
// until the user connects or logs in again. Its text names the program, never its arguments, and
// says "Cogpit ended" only when Cogpit found the run's process and signals it.
type LoginEndedError struct {
	Program string
	Bound   time.Duration // the run's bound
	Cause   error
	// RunsOn: Cogpit stopped waiting for the run without ending its process, which it did not find
	// among its children (or may not signal): the process may run on (H-118). The zero value is a
	// run Cogpit ended.
	RunsOn bool
	why    string // why it runs on, in words; empty: whyNotFound
}

func (e *LoginEndedError) Error() string {
	if e.TimedOut() {
		text := fmt.Sprintf("the login through %s did not finish within %s", e.Program, boundText(e.Bound))
		if e.RunsOn {
			text += "; Cogpit stopped waiting for it; " + e.runsOnText()
		}
		return text
	}
	return "login cancelled: " + e.endText()
}

// endText says what Cogpit did with the run's process: "Cogpit ended pinniped", or that it stopped
// waiting for it and why the process may run on.
func (e *LoginEndedError) endText() string {
	if e.RunsOn {
		return "Cogpit stopped waiting for " + e.Program + "; " + e.runsOnText()
	}
	return "Cogpit ended " + e.Program
}

func (e *LoginEndedError) runsOnText() string {
	why := e.why
	if why == "" {
		why = whyNotFound
	}
	return why + " and may run on"
}

func (e *LoginEndedError) Unwrap() error { return e.Cause }

// TimedOut reports whether the run ended because it did not return within its bound.
func (e *LoginEndedError) TimedOut() bool { return errors.Is(e.Cause, ErrLoginTimedOut) }

// boundText writes a bound in words: "5 minutes", "1 minute", else Go's form ("200ms").
func boundText(d time.Duration) string {
	switch {
	case d == time.Minute:
		return "1 minute"
	case d > 0 && d%time.Minute == 0:
		return fmt.Sprintf("%d minutes", d/time.Minute)
	}
	return d.String()
}

// LoginRun is a run of a connection's credential plugin that is under way.
type LoginRun struct {
	Program string    // the plugin's program, its file name ("pinniped")
	Since   time.Time // when it began
	Until   time.Time // when Cogpit ends it unless it returned
}

// LoginWatch tells the owner of a connection when a run of its credential plugin begins or ends
// (Options.Login), from Connect on and for the connection's life: notify runs on a goroutine of its
// own, never under a lock of the cluster layer's or client-go's, and Current says what is under way
// when it is asked. Nil-safe.
type LoginWatch struct {
	notify func()
	hold   atomic.Pointer[pluginHold]
}

// NewLoginWatch returns a watch that calls notify at each begin and end of a run.
func NewLoginWatch(notify func()) *LoginWatch { return &LoginWatch{notify: notify} }

// Current returns the run under way of the connection's credential plugin, if one is.
func (w *LoginWatch) Current() (LoginRun, bool) {
	if w == nil {
		return LoginRun{}, false
	}
	if p := w.hold.Load(); p != nil {
		return p.current()
	}
	return LoginRun{}, false
}

// Last returns the last run of the connection's credential plugin that ended, when it ended, and
// the error if Cogpit ended it; ok is false before the first one ended.
func (w *LoginWatch) Last() (run LoginRun, ended time.Time, endedBy *LoginEndedError, ok bool) {
	if w == nil {
		return LoginRun{}, time.Time{}, nil, false
	}
	p := w.hold.Load()
	if p == nil {
		return LoginRun{}, time.Time{}, nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last == nil {
		return LoginRun{}, time.Time{}, nil, false
	}
	return p.last.LoginRun, p.last.ended, p.last.ending, true
}

// pluginRun is one run of an Authenticator's plugin.
type pluginRun struct {
	LoginRun
	command  []string      // the command line it runs, to find its process
	search   *procSearch   // its process, looked for from its begin (pluginproc.go)
	done     chan struct{} // closed when it ended
	timer    *time.Timer   // its bound
	ended    time.Time     // when it ended
	stopping bool          // Cogpit is ending it: its process is being looked for
	ending   *LoginEndedError
	// watches were told its begin: each is told its end too, also when its connection stopped
	// using the plugin meanwhile (a cancelled connect unregisters before the run ends).
	watches []*LoginWatch
}

// loginEnds is a connection's part: the runs Cogpit ended, or refused to start after it ended one,
// while the connection used the plugin (a request that failed meanwhile needed it), the last one's
// error, and the connection's watch.
type loginEnds struct {
	count atomic.Uint64
	last  atomic.Pointer[LoginEndedError]
	watch *LoginWatch // set before the connection registers; nil without one
}

// configureRuns gives the hold what its runs need: the plugin's command line, its program and the
// runs' bound (the last connection's to register sets it); and the watch of the connection g.
func (p *pluginHold) configureRuns(cfg *rest.Config, g *loginGuard, bound time.Duration) {
	command := append([]string{filepath.Clean(cfg.ExecProvider.Command)}, cfg.ExecProvider.Args...)
	p.mu.Lock()
	p.command, p.program = command, filepath.Base(command[0])
	if bound > 0 {
		p.bound = bound
	}
	p.stopped = nil // a connect is the user's: the plugin may run again (rearm)
	p.mu.Unlock()
	if g.ends.watch != nil {
		g.ends.watch.hold.Store(p)
	}
}

// current is the run under way.
func (p *pluginHold) current() (LoginRun, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.run == nil {
		return LoginRun{}, false
	}
	return p.run.LoginRun, true
}

// begin notes a run that client-go starts now. It runs inside refreshCredsLocked, under the
// Authenticator's lock (lock), which client-go lets go once the plugin's process ended; Cogpit's
// children are noted first, before client-go starts the plugin, so that its process is the one that
// appears after (pluginproc.go).
func (p *pluginHold) begin(lock *sync.Mutex) {
	before := childSet()
	now := time.Now()
	p.mu.Lock()
	var told []*LoginWatch
	if prev := p.run; prev != nil {
		p.finishLocked(prev, now) // the lock serialises the runs: the previous one ended
		told = prev.watches
	}
	bound := p.bound
	if bound <= 0 {
		bound = DefaultLoginTimeout
	}
	run := &pluginRun{LoginRun: LoginRun{Program: p.program, Since: now, Until: now.Add(bound)},
		command: slices.Clone(p.command), done: make(chan struct{})}
	run.search = newProcSearch(now, run.command, before)
	run.timer = time.AfterFunc(bound, func() { _ = p.end(run, ErrLoginTimedOut) }) // the connections are told
	p.run = run
	run.watches = p.watchesLocked()
	p.mu.Unlock()
	go run.search.run(run.done)
	notifyAll(unionWatches(told, run.watches))
	go func() {
		// refreshCredsLocked holds the Authenticator's lock until cmd.Run returned: taking it is
		// the run's end, the only one client-go lets Cogpit see.
		lock.Lock()
		lock.Unlock() //nolint:staticcheck // SA2001: the empty critical section is the point: a wait for the run's end
		p.finish(run)
	}()
}

// finish notes that run ended.
func (p *pluginHold) finish(run *pluginRun) {
	p.mu.Lock()
	if p.run != run {
		p.mu.Unlock()
		return
	}
	p.finishLocked(run, time.Now())
	watches := unionWatches(run.watches, p.watchesLocked())
	p.mu.Unlock()
	notifyAll(watches)
}

// unionWatches is a and b without a watch twice.
func unionWatches(a, b []*LoginWatch) []*LoginWatch {
	out := slices.Clone(a)
	for _, w := range b {
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

func (p *pluginHold) finishLocked(run *pluginRun, now time.Time) {
	run.timer.Stop()
	run.ended = now
	close(run.done)
	run.search.finished(now)
	p.run = nil
	p.last = run
}

// watchesLocked are the watches of the connections that use the plugin.
func (p *pluginHold) watchesLocked() []*LoginWatch {
	var out []*LoginWatch
	for g := range p.conns {
		if w := g.ends.watch; w != nil && w.notify != nil {
			out = append(out, w)
		}
	}
	return out
}

func notifyAll(watches []*LoginWatch) {
	for _, w := range watches {
		go w.notify()
	}
}

// end ends run for cause, unless it ended or is being ended: its process is looked up first (it may
// wait for the search of it, at most until searchWindow after the run's begin); then no run starts
// any more until rearm, each connection using the plugin is told, and the process, if found, is
// signalled. It returns what the connections were told: RunsOn when the process was not found;
// nil when it ended nothing (the run ended by itself meanwhile, or is being ended).
func (p *pluginHold) end(run *pluginRun, cause error) *LoginEndedError {
	p.mu.Lock()
	if p.run != run || run.ending != nil || run.stopping {
		p.mu.Unlock()
		return nil
	}
	run.stopping = true
	p.mu.Unlock()
	target, why := run.search.locate(run.done)
	p.mu.Lock()
	if p.run != run { // it answered or failed meanwhile, by itself
		p.mu.Unlock()
		return nil
	}
	e := &LoginEndedError{Program: run.Program, Bound: run.Until.Sub(run.Since), Cause: cause, RunsOn: why != "", why: why}
	run.ending = e
	p.stopped = e
	p.tellLocked(e) // before the signal: a request the plugin's end fails sees it
	p.mu.Unlock()
	after := time.Since(run.Since).Round(time.Millisecond)
	if e.RunsOn {
		slog.Warn("cluster: stopped waiting for a credential plugin's login; its process may run on", "program", run.Program,
			"why", e.Error(), "after", after)
		return e
	}
	slog.Warn("cluster: ending a credential plugin's login", "program", run.Program, "why", e.Error(), "after", after)
	go terminate(run, target)
	return e
}

// tellLocked counts an ended or refused run for every connection that uses the plugin.
func (p *pluginHold) tellLocked(e *LoginEndedError) {
	for g := range p.conns {
		g.ends.last.Store(e)
		g.ends.count.Add(1)
	}
}

// rearm lets the plugin run again for connection g: its user logs in again.
func (p *pluginHold) rearm(g *loginGuard) {
	p.mu.Lock()
	p.stopped = nil
	if st, ok := p.conns[g]; ok {
		st.withdrawn = false
		p.conns[g] = st
	}
	p.mu.Unlock()
}

// cancelFor is connection g's cancel: g asks for no run any more (until rearm), and the run under
// way is ended unless another connection that asks for runs uses the plugin (the run serves it
// too). It reports whether Cogpit found the run's process and signals it.
func (p *pluginHold) cancelFor(g *loginGuard) bool {
	p.mu.Lock()
	if st, ok := p.conns[g]; ok {
		st.withdrawn = true
		p.conns[g] = st
	}
	run := p.run
	shared := false
	for o, st := range p.conns {
		if o != g && st.asks() {
			shared = true
		}
	}
	p.mu.Unlock()
	if run == nil || shared {
		return false
	}
	e := p.end(run, ErrLoginCancelled)
	return e != nil && !e.RunsOn
}

// lastSince is the last run that began at or after t: the one under way or the last that ended.
func (p *pluginHold) lastSince(t time.Time) (pluginRun, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range []*pluginRun{p.run, p.last} {
		if r != nil && !r.Since.Before(t) {
			return *r, true
		}
	}
	return pluginRun{}, false
}

// LoginRun returns the run of the connection's credential plugin that is under way, if one is: a
// run serves every connection with the same plugin configuration, and each request of theirs that
// needs the credential waits for it.
func (c *Conn) LoginRun() (LoginRun, bool) {
	if c == nil || c.hold == nil {
		return LoginRun{}, false
	}
	return c.hold.p.current()
}

// CancelLogin ends the run of the connection's credential plugin that is under way, as the user's
// cancel: its process is ended and the plugin does not run again until the user connects or logs in
// again (ArmLogin); the connection asks for no run until then, so a request of it that still waits
// behind the run starts none afterwards. A run that another open connection with the same plugin
// configuration needs too, and that one does not hold, runs on. It reports whether it ended one:
// false too when Cogpit only stopped waiting for a run whose process it did not find
// (LoginEndedError.RunsOn).
func (c *Conn) CancelLogin() bool {
	if c == nil || c.hold == nil {
		return false
	}
	return c.hold.p.cancelFor(c.hold.g)
}

// loginEndMark is the count of runs Cogpit ended or refused for this connection: a request that
// fails while it grows needed one.
func (c *Conn) loginEndMark() uint64 {
	if c == nil || c.hold == nil {
		return 0
	}
	return c.hold.g.ends.count.Load()
}

// loginEndedSince is the error of a run Cogpit ended or refused for this connection since mark; nil
// when none was.
func (c *Conn) loginEndedSince(mark uint64) *LoginEndedError {
	if c == nil || c.hold == nil || c.hold.g.ends.count.Load() == mark {
		return nil
	}
	return c.hold.g.ends.last.Load()
}

// endedWithStderr is the error of a request that failed because Cogpit ended (or did not start
// again) the plugin's run, e: after a timeout with what the plugin wrote to its standard error since
// mark, scrubbed (D-66: a failed login's; it may say what it waited for), after a cancel without it.
func endedWithStderr(err error, e *LoginEndedError, sink *pluginStderr, mark int64, known []string) error {
	if !e.TimedOut() {
		sink.discard()
		return e
	}
	var pe *PluginError
	if errors.As(withPluginStderr(err, sink, mark, known), &pe) && pe.Stderr != "" {
		return &PluginError{Err: e, Stderr: pe.Stderr}
	}
	return e
}

// traceLoginRun reports the plugin's run that began at or after start for the connection log:
// how long it took, or that Cogpit ended it and why.
func (c *Conn) traceLoginRun(start time.Time) {
	if c == nil || c.hold == nil || c.trace == nil {
		return
	}
	run, ok := c.hold.p.lastSince(start)
	if !ok {
		return
	}
	end := run.ended
	if end.IsZero() {
		end = time.Now()
	}
	took := end.Sub(run.Since)
	switch {
	case run.ending != nil && run.ending.TimedOut() && run.ending.RunsOn:
		c.trace.timed(StepLogin, "Login: "+run.ending.Error(), took, true)
	case run.ending != nil && run.ending.TimedOut():
		c.trace.timed(StepLogin, "Login: "+run.ending.Error()+"; Cogpit ended it", took, true)
	case run.ending != nil:
		c.trace.timed(StepLogin, "Login cancelled: "+run.ending.endText(), took, true)
	case run.ended.IsZero():
		c.trace.timed(StepLogin, "Login: "+run.Program+" still runs", took, true)
	default:
		c.trace.timed(StepLogin, "Login: "+run.Program+" answered", took, false)
	}
}
