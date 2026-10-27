//go:build darwin || linux

package cluster

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// waitingPlugin is a credential plugin that, while its block file exists, waits for ever as a
// browser login nobody finishes does (a loop of short sleeps, so that its own children never
// outlive it by more than 100 ms); while its exec file exists, it is a wrapper that replaces itself
// with /bin/sleep 900 (WF38: its number stays, its command line becomes sleep's); while its stray
// file exists, it leaves /bin/sleep 900 running with its standard output and exits, so that
// client-go's run goes on without a process of Cogpit's to end (H-118); otherwise it hands out the
// ExecCredential the test wrote, after the delay its delay file names. It notes the number of each
// of its processes, and of each stray sleep; the test's cleanup ends what still runs.
type waitingPlugin struct {
	*countingPlugin
	pids   string
	strays string
	block  string
	exec   string
	stray  string
	delay  string
}

func newWaitingPlugin(t *testing.T, srv *testkit.FakeAPIServer) *waitingPlugin {
	t.Helper()
	p := &waitingPlugin{countingPlugin: newCountingPlugin(t, srv)}
	dir := filepath.Dir(p.path)
	p.pids, p.strays = filepath.Join(dir, "pids"), filepath.Join(dir, "strays")
	p.block, p.exec, p.stray, p.delay = filepath.Join(dir, "block"), filepath.Join(dir, "exec"), filepath.Join(dir, "stray"), filepath.Join(dir, "delay")
	script := `#!/bin/sh
echo $$ >> "` + p.pids + `"
echo run >> "$PLUGIN_RUNS"
if [ -f "` + p.block + `" ]; then
  echo "Log in at http://127.0.0.1:1/callback?code=secret-code-0042 to continue" >&2
  while :; do sleep 0.1; done
fi
if [ -f "` + p.exec + `" ]; then exec /bin/sleep 900; fi
if [ -f "` + p.stray + `" ]; then
  /bin/sleep 900 &
  echo $! >> "` + p.strays + `"
  exit 0
fi
if [ -f "` + p.delay + `" ]; then sleep "$(cat "` + p.delay + `")"; fi
cat "$PLUGIN_CRED"
`
	if err := os.WriteFile(p.path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.endLeftovers)
	return p
}

// endLeftovers kills what the plugin left running: its processes that are still children of the
// test's, and its stray sleeps that still run sleep (a number the system gave to another process
// meanwhile is not hit).
func (p *waitingPlugin) endLeftovers() {
	for _, pid := range p.numbers(p.pids) {
		for _, c := range listChildren() {
			if c.id.pid == pid {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	}
	for _, pid := range p.numbers(p.strays) {
		if argv := commandLine(pid); len(argv) == 2 && argv[0] == "/bin/sleep" && argv[1] == "900" {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

// mode switches one of the plugin's files on or off.
func (p *waitingPlugin) mode(file string, on bool) {
	p.t.Helper()
	if !on {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			p.t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		p.t.Fatal(err)
	}
}

// blocking makes the plugin wait for ever, or answer again.
func (p *waitingPlugin) blocking(on bool) { p.t.Helper(); p.mode(p.block, on) }

// numbers are the process numbers noted in file, oldest first.
func (p *waitingPlugin) numbers(file string) []int {
	data, err := os.ReadFile(file) // #nosec G304 -- the test's own file
	if err != nil {
		return nil
	}
	var out []int
	for _, f := range strings.Fields(string(data)) {
		if pid, err := strconv.Atoi(f); err == nil {
			out = append(out, pid)
		}
	}
	return out
}

// lastPID is the number of the plugin's latest process; 0 before its first run.
func (p *waitingPlugin) lastPID() int {
	pids := p.numbers(p.pids)
	if len(pids) == 0 {
		return 0
	}
	return pids[len(pids)-1]
}

// gone reports whether process pid has ended (and was reaped).
func gone(pid int) bool { return pid > 0 && errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }

func waitCtx(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

// The owner's finding (2026-10-16 night): a connect waiting for an interactive login that the user
// abandoned could not be cancelled. Now a cancel of the connect ends the plugin's process at once,
// the connect returns with "login cancelled", and the watch says the login waited until then.
func TestCancelledConnectEndsThePlugin(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	p := newWaitingPlugin(t, srv)
	p.give(secretToken, false)
	p.blocking(true)
	var mu sync.Mutex
	var steps []ConnectStep
	var notes int
	watch := NewLoginWatch(func() { mu.Lock(); notes++; mu.Unlock() })
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
	if !testkit.WaitFor(wait, func() bool { _, on := watch.Current(); return on && p.lastPID() != 0 }) {
		t.Fatal("the connect never waited for the plugin")
	}
	run, _ := watch.Current()
	if run.Program != "login-plugin" || run.Until.Sub(run.Since) != DefaultLoginTimeout {
		t.Errorf("the run under way: %+v, want login-plugin bounded by %s", run, DefaultLoginTimeout)
	}
	pid := p.lastPID()

	cancel()
	var r result
	select {
	case r = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled connect did not return within 5 s")
	}
	var ended *LoginEndedError
	var ce *ConnectError
	if r.err == nil || !errors.As(r.err, &ce) || !errors.As(r.err, &ended) || ended.TimedOut() {
		t.Fatalf("the cancelled connect: %v, want a ConnectError of a cancelled login", r.err)
	}
	if ce.Problem != "login cancelled: Cogpit ended login-plugin" {
		t.Errorf("the problem: %q", ce.Problem)
	}
	if !testkit.WaitFor(wait, func() bool { return gone(pid) }) {
		t.Fatalf("the plugin's process %d still runs after the cancel", pid)
	}
	if !testkit.WaitFor(wait, func() bool { _, on := watch.Current(); return !on }) {
		t.Error("the watch still says the login runs")
	}
	// The watch tells its listener after its state changed: wait for the end's notice.
	if !testkit.WaitFor(wait, func() bool { mu.Lock(); defer mu.Unlock(); return notes >= 2 }) {
		mu.Lock()
		t.Errorf("the watch was told %d times, want the run's begin and end", notes)
		mu.Unlock()
	}
	mu.Lock()
	defer mu.Unlock()
	var logged bool
	for _, s := range steps {
		logged = logged || (s.Name == StepLogin && s.Failed && strings.Contains(s.Text, "Login cancelled: Cogpit ended login-plugin"))
		if strings.Contains(s.Text, "secret-code-0042") {
			t.Errorf("a step holds the plugin's code: %q", s.Text)
		}
	}
	if !logged {
		t.Errorf("no step says the login was cancelled: %+v", steps)
	}
}

// A plugin's run that does not return within its bound is ended, and the connect fails with a
// reason that says so; the plugin's standard error is in the log, scrubbed (D-66).
func TestLoginTimeoutEndsThePlugin(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	p := newWaitingPlugin(t, srv)
	p.give(secretToken, false)
	p.blocking(true)
	var steps []ConnectStep
	start := time.Now()
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{p.kconf}, LoginTimeout: time.Second, // room for a race-enabled binary to start the plugin
		Trace: func(s ConnectStep) { steps = append(steps, s) }})
	if c != nil {
		c.Close()
	}
	var ended *LoginEndedError
	var ce *ConnectError
	if !errors.As(err, &ce) || !errors.As(err, &ended) || !ended.TimedOut() {
		t.Fatalf("the connect: %v, want a timed-out login", err)
	}
	if want := "the login through login-plugin did not finish within 1s; Connect starts it again"; ce.Problem != want {
		t.Errorf("the problem: %q, want %q", ce.Problem, want)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the connect took %s with a bound of 1s", took)
	}
	if pid := p.lastPID(); !testkit.WaitFor(waitCtx(t, 5*time.Second), func() bool { return gone(pid) }) {
		t.Fatalf("the plugin's process %d still runs after its bound", pid)
	}
	var text strings.Builder
	for _, s := range steps {
		text.WriteString(s.Name + ": " + s.Text + "\n")
	}
	got := text.String()
	for _, want := range []string{"login: Login: the login through login-plugin did not finish within 1s; Cogpit ended it",
		"Class: timeout: the login through login-plugin did not finish within 1s", "Log in at http://127.0.0.1:1/callback"} {
		if !strings.Contains(got, want) {
			t.Errorf("the steps lack %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "secret-code-0042") {
		t.Errorf("the steps hold the plugin's code:\n%s", got)
	}
}

// Once Cogpit ended a run, the requests that waited behind client-go's lock do not start the plugin
// again (a browser tab each): they end as LoginEndedError. Log in again (ArmLogin) lets it run.
func TestEndedLoginDoesNotRunAgainUntilLogInAgain(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	p := newWaitingPlugin(t, srv)
	p.give(secretToken, true) // expired at once: every request needs a run
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{p.kconf}, LoginTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	base := p.count()
	p.blocking(true)
	errs := make(chan error, 5)
	for range 5 {
		go func() { errs <- c.Heartbeat(context.Background()) }()
	}
	for range 5 {
		select {
		case err := <-errs:
			var ended *LoginEndedError
			if !errors.As(err, &ended) || !ended.TimedOut() {
				t.Errorf("a heartbeat behind the ended run: %v, want the timed-out login", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("a heartbeat behind the ended run did not return")
		}
	}
	if n := p.count() - base; n != 1 {
		t.Fatalf("the plugin ran %d times for five waiting requests, want once", n)
	}
	if err := c.Heartbeat(context.Background()); err == nil || p.count()-base != 1 {
		t.Fatalf("a later heartbeat: %v, %d runs; want refused without a run", err, p.count()-base)
	}

	p.blocking(false)
	c.ArmLogin()
	if err := c.Heartbeat(context.Background()); err != nil {
		t.Fatalf("the heartbeat after Log in again: %v", err)
	}
	if n := p.count() - base; n != 2 {
		t.Errorf("Log in again ran the plugin %d times in all, want once more", n)
	}
}

// Closing a connection (Disconnect) whose renewal waits for its plugin ends the plugin's process,
// and the waiting request returns.
func TestCloseEndsARenewalsPlugin(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	p := newWaitingPlugin(t, srv)
	p.give(secretToken, true)
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{p.kconf}})
	if err != nil {
		t.Fatal(err)
	}
	p.blocking(true)
	done := make(chan error, 1)
	go func() { done <- c.Heartbeat(context.Background()) }()
	wait := waitCtx(t, 10*time.Second)
	if !testkit.WaitFor(wait, func() bool { _, on := c.LoginRun(); return on && p.count() >= 2 }) {
		t.Fatal("the renewal never ran the plugin")
	}
	pid := p.lastPID()
	c.Close()
	select {
	case err := <-done:
		if !errors.Is(err, ErrLoginCancelled) {
			t.Errorf("the renewal after Close: %v, want a cancelled login", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the renewal still waits after Close")
	}
	if !testkit.WaitFor(wait, func() bool { return gone(pid) }) {
		t.Fatalf("the plugin's process %d still runs after Close", pid)
	}
}

// A run another open connection with the same plugin configuration needs is not ended by this
// connection's cancel; closing that one too ends it.
func TestSharedRunEndsWithTheLastConnection(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	p := newWaitingPlugin(t, srv)
	p.give(secretToken, true)
	a, b := p.connect(), p.connect()
	base := p.count()
	p.blocking(true)
	go func() { _ = a.Heartbeat(context.Background()) }()
	wait := waitCtx(t, 10*time.Second)
	if !testkit.WaitFor(wait, func() bool { _, on := b.LoginRun(); return on && p.count() > base }) {
		t.Fatal("the shared login never ran")
	}
	pid, ends := p.lastPID(), a.loginEndMark()
	if b.CancelLogin() {
		t.Fatal("one connection's cancel ended a run the other one needs")
	}
	b.Close()
	if a.loginEndMark() != ends || gone(pid) { // end counts before it signals the process
		t.Fatal("closing one connection ended the shared run")
	}
	a.Close()
	if !testkit.WaitFor(wait, func() bool { return gone(pid) }) {
		t.Fatalf("the plugin's process %d still runs after both closed", pid)
	}
}

// The connect's own timeout bounds its request below the plugin: a login that takes longer than it
// (a browser login of 1.5 s against a timeout of 500 ms) connects. Before D-73 the connect failed
// with "no answer within" its timeout after the login had worked.
func TestConnectTimeoutDoesNotCountTheLogin(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	p := newWaitingPlugin(t, srv)
	p.give(secretToken, false)
	if err := os.WriteFile(p.delay, []byte("1.5"), 0o600); err != nil {
		t.Fatal(err)
	}
	var steps []ConnectStep
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{p.kconf}, ConnectTimeout: 500 * time.Millisecond,
		Trace: func(s ConnectStep) { steps = append(steps, s) }})
	if err != nil {
		t.Fatalf("a login slower than the connect's timeout: %v", err)
	}
	defer c.Close()
	var logged bool
	for _, s := range steps {
		logged = logged || (s.Name == StepLogin && !s.Failed && s.Text == "Login: login-plugin answered" && s.Took >= time.Second)
	}
	if !logged {
		t.Errorf("no step says how long the login took: %+v", steps)
	}
}

// The plugin's process is found among Cogpit's children by its command line, a #! script's too.
func TestFindPluginByItsCommandLine(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "find-me")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile :; do sleep 0.1; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(script, "oidc", "--issuer=https://issuer.example")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	want := cmd.Process.Pid
	if !testkit.WaitFor(waitCtx(t, 5*time.Second), func() bool {
		id, n := findPlugin([]string{script, "oidc", "--issuer=https://issuer.example"}, nil)
		return n == 1 && id.pid == want
	}) {
		t.Fatalf("the plugin's process %d was not found among %+v", want, listChildren())
	}
	if id, n := findPlugin([]string{script, "oidc", "--issuer=https://other.example"}, nil); n != 0 {
		t.Errorf("another command line found process %d", id.pid)
	}
	// A plugin of the same name elsewhere is another context's, or another test's (CI run
	// 38975936330: a cancel's search ended the next test's plugin).
	if id, n := findPlugin([]string{filepath.Join(t.TempDir(), "find-me"), "oidc", "--issuer=https://issuer.example"}, nil); n != 0 {
		t.Errorf("a program of the same name in another folder found process %d", id.pid)
	}
	// A process that ran before the run began is not the run's (findPlugin's before).
	if _, n := findPlugin([]string{script, "oidc", "--issuer=https://issuer.example"}, childSet()); n != 0 {
		t.Error("a process older than the run was taken for its plugin")
	}
}

// connectResult is what a Connect that runs on a goroutine of its own returned.
type connectResult struct {
	c   *Conn
	err error
}

// connectAsync connects p's context with ctx and watch on a goroutine of its own.
func (p *waitingPlugin) connectAsync(ctx context.Context, watch *LoginWatch, trace func(ConnectStep)) <-chan connectResult {
	done := make(chan connectResult, 1)
	go func() {
		c, err := Connect(ctx, Options{KubeconfigPaths: []string{p.kconf}, Login: watch, Trace: trace})
		done <- connectResult{c, err}
	}()
	return done
}

// result waits for a Connect's result, at most 5 s.
func result(t *testing.T, done <-chan connectResult) connectResult {
	t.Helper()
	select {
	case r := <-done:
		if r.c != nil {
			t.Cleanup(r.c.Close)
		}
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled connect did not return within 5 s")
	}
	return connectResult{}
}

// WF38 (the lead's walk of 2026-10-19), part 3: a credential plugin that is a wrapper and replaces
// itself with the real login program (exec) keeps its process number while its command line
// becomes the program's. Cogpit finds it at the run's begin and ends it by its number: the cancel
// ends the process, and the log says so truthfully.
func TestCancelEndsAPluginThatExecs(t *testing.T) {
	quietSearches(t)
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	p := newWaitingPlugin(t, srv)
	p.give(secretToken, false)
	p.mode(p.exec, true)
	var mu sync.Mutex
	var steps []ConnectStep
	watch := NewLoginWatch(func() {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := p.connectAsync(ctx, watch, func(s ConnectStep) { mu.Lock(); steps = append(steps, s); mu.Unlock() })
	wait := waitCtx(t, 10*time.Second)
	var pid int
	if !testkit.WaitFor(wait, func() bool {
		_, on := watch.Current()
		pid = p.lastPID()
		argv := commandLine(pid)
		return on && pid != 0 && len(argv) > 0 && argv[0] == "/bin/sleep"
	}) {
		t.Fatalf("the wrapper never replaced itself with sleep: %v", commandLine(pid))
	}

	cancel()
	r := result(t, done)
	var ended *LoginEndedError
	var ce *ConnectError
	if !errors.As(r.err, &ce) || !errors.As(r.err, &ended) || ended.RunsOn {
		t.Fatalf("the cancelled connect: %v, want a cancelled login whose process Cogpit ended", r.err)
	}
	if ce.Problem != "login cancelled: Cogpit ended login-plugin" {
		t.Errorf("the problem: %q", ce.Problem)
	}
	if !testkit.WaitFor(wait, func() bool { return gone(pid) }) {
		t.Fatalf("the wrapper's process %d (now %v) still runs after the cancel", pid, commandLine(pid))
	}
	mu.Lock()
	defer mu.Unlock()
	var logged bool
	for _, s := range steps {
		logged = logged || (s.Name == StepLogin && strings.Contains(s.Text, "Login cancelled: Cogpit ended login-plugin"))
	}
	if !logged {
		t.Errorf("no step says Cogpit ended the plugin: %+v", steps)
	}
}

// holdOf is the hold of the Authenticator that runs plugin.
func holdOf(t *testing.T, plugin string) *pluginHold {
	t.Helper()
	patchMu.Lock()
	defer patchMu.Unlock()
	for _, h := range holds {
		h.mu.Lock()
		match := len(h.command) > 0 && h.command[0] == plugin
		h.mu.Unlock()
		if match {
			return h
		}
	}
	t.Fatalf("no hold runs %s", plugin)
	return nil
}

// WF38, parts 1 and 2. A run whose process Cogpit cannot end (the plugin left a child that holds
// its output, and exited: client-go's run goes on, H-118): the cancel says Cogpit stopped waiting
// and the process may run on, not that Cogpit ended it. Two later connects whose requests queued
// behind that run, cancelled, start no run of their own once it ends: no connect or login asks for
// one any more (before, each started the plugin anew, with nobody waiting). A connect afterwards
// runs it once.
func TestCancelledConnectsStartNoRunAfterAStrayOne(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	p := newWaitingPlugin(t, srv)
	p.give(secretToken, false)
	p.mode(p.stray, true)
	var mu sync.Mutex
	var steps []ConnectStep
	watch := NewLoginWatch(func() {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := p.connectAsync(ctx, watch, func(s ConnectStep) { mu.Lock(); steps = append(steps, s); mu.Unlock() })
	wait := waitCtx(t, 20*time.Second)
	if !testkit.WaitFor(wait, func() bool { _, on := watch.Current(); return on && len(p.numbers(p.strays)) == 1 }) {
		t.Fatal("the connect never waited for the plugin's stray child")
	}
	stray := p.numbers(p.strays)[0]

	cancel()
	r := result(t, done)
	var ended *LoginEndedError
	var ce *ConnectError
	if !errors.As(r.err, &ce) || !errors.As(r.err, &ended) || !ended.RunsOn {
		t.Fatalf("the cancelled connect: %v, want a cancelled login whose process was not found", r.err)
	}
	const want = "login cancelled: Cogpit stopped waiting for login-plugin; its process was not found and may run on"
	if ce.Problem != want {
		t.Errorf("the problem: %q, want %q", ce.Problem, want)
	}
	mu.Lock()
	var logged bool
	for _, s := range steps {
		logged = logged || (s.Name == StepLogin && s.Failed && s.Text == "Login cancelled: Cogpit stopped waiting for login-plugin; "+
			"its process was not found and may run on")
		if strings.Contains(s.Text, "Cogpit ended") {
			t.Errorf("a step says Cogpit ended the plugin: %q", s.Text)
		}
	}
	mu.Unlock()
	if !logged {
		t.Errorf("no step says Cogpit stopped waiting: %+v", steps)
	}

	// Two connects whose requests queue behind the stray run, each cancelled.
	p.mode(p.stray, false)
	for range 2 {
		w := NewLoginWatch(func() {})
		cctx, ccancel := context.WithCancel(context.Background())
		d := p.connectAsync(cctx, w, nil)
		if !testkit.WaitFor(wait, func() bool { _, on := w.Current(); return on }) {
			ccancel()
			t.Fatal("a later connect does not see the run under way")
		}
		time.Sleep(300 * time.Millisecond) // its first request reaches client-go's lock
		ccancel()
		if r := result(t, d); r.err == nil {
			t.Fatal("a cancelled connect connected")
		}
	}
	hold := holdOf(t, p.path)
	_ = syscall.Kill(stray, syscall.SIGKILL) // the run ends: the queued requests take the lock
	refused := func() int { hold.mu.Lock(); defer hold.mu.Unlock(); return hold.notAsked }
	if !testkit.WaitFor(wait, func() bool { return refused() >= 2 }) {
		t.Fatalf("the cancelled connects' requests were refused %d times, want 2", refused())
	}
	if n := p.count(); n != 1 {
		t.Fatalf("the plugin ran %d times, want once: no run for a cancelled connect", n)
	}

	c := p.connect()
	if c == nil || p.count() != 2 {
		t.Errorf("a connect after the stray run: %d runs in all, want 2", p.count())
	}
}

// A connection whose login the user cancelled asks for no run until Log in again (ArmLogin), even
// while the plugin may run for others: its expired login is not renewed by the plugin.
func TestCancelledLoginRunsNoMoreUntilLogInAgain(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	p := newWaitingPlugin(t, srv)
	p.give(secretToken, true) // expired at once: every request needs a run
	c := p.connect()
	base := p.count()
	if c.CancelLogin() {
		t.Error("CancelLogin ended a run while none was under way")
	}
	if err := c.Heartbeat(context.Background()); err == nil || p.count() != base {
		t.Fatalf("a heartbeat after the cancel: %v, %d runs; want refused without a run", err, p.count()-base)
	}
	c.ArmLogin()
	if err := c.Heartbeat(context.Background()); err != nil || p.count() != base+1 {
		t.Fatalf("a heartbeat after Log in again: %v, %d runs; want one run", err, p.count()-base)
	}
}

// H-118's residual after WF38: a connect that waits behind a run Cogpit stopped waiting for and
// could not end (the plugin left a child that holds its output, and exited: client-go's run goes
// on) is told of that run as it registers, and the run says it has no end: the bound no longer
// acts on it (LoginRun.RunsOn, no Until). Before, the later connect was not told at all, and its
// watch gave the passed bound as the run's end. Its cancel stops its wait; the run goes on. A
// connect behind it that is not cancelled goes on once the run ends, with a run of its own.
func TestAWaitBehindARunCogpitCouldNotEndHasNoEnd(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	p := newWaitingPlugin(t, srv)
	p.give(secretToken, false)
	p.mode(p.stray, true)
	watch := NewLoginWatch(func() {})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := p.connectAsync(ctx, watch, nil)
	wait := waitCtx(t, 20*time.Second)
	if !testkit.WaitFor(wait, func() bool { _, on := watch.Current(); return on && len(p.numbers(p.strays)) == 1 }) {
		t.Fatal("the connect never waited for the plugin's stray child")
	}
	if run, _ := watch.Current(); run.RunsOn || run.Until.Sub(run.Since) != DefaultLoginTimeout {
		t.Errorf("the run before the cancel: %+v, want its end after %s", run, DefaultLoginTimeout)
	}
	cancel()
	if r := result(t, done); !errors.Is(r.err, ErrLoginCancelled) {
		t.Fatalf("the cancelled connect: %v, want a cancelled login", r.err)
	}
	stray := p.numbers(p.strays)[0]
	p.mode(p.stray, false)

	var told atomic.Int32
	later := NewLoginWatch(func() { told.Add(1) })
	lctx, lcancel := context.WithCancel(context.Background())
	defer lcancel()
	ldone := p.connectAsync(lctx, later, nil)
	if !testkit.WaitFor(wait, func() bool { return told.Load() > 0 }) {
		t.Fatal("the later connect was not told of the run it waits behind")
	}
	run, on := later.Current()
	if !on || run.Program != "login-plugin" || !run.RunsOn || !run.Until.IsZero() {
		t.Fatalf("the run the later connect waits behind: %+v (under way %v), want login-plugin, RunsOn, no end", run, on)
	}
	time.Sleep(300 * time.Millisecond) // its first request reaches client-go's lock
	lcancel()
	r := result(t, ldone)
	var ce *ConnectError
	if !errors.As(r.err, &ce) || ce.Problem != "connecting was cancelled" {
		t.Fatalf("the cancelled later connect: %v, want cancelled", r.err)
	}
	if gone(stray) {
		t.Error("the stray child ended: the test no longer shows a run Cogpit could not end")
	}

	// A connect behind the run that is not cancelled goes on once the stray child ends: with a run
	// of its own (the stray run handed out no credential), bounded as any.
	third := NewLoginWatch(func() {})
	tdone := p.connectAsync(context.Background(), third, nil)
	if !testkit.WaitFor(wait, func() bool { run, on := third.Current(); return on && run.RunsOn }) {
		t.Fatal("the third connect does not see the run it waits behind")
	}
	time.Sleep(300 * time.Millisecond) // its first request reaches client-go's lock
	_ = syscall.Kill(stray, syscall.SIGKILL)
	if r := result(t, tdone); r.err != nil {
		t.Fatalf("the connect behind the run, once it ended: %v", r.err)
	}
	if n := p.count(); n != 2 {
		t.Errorf("the plugin ran %d times, want twice: the stray run and the third connect's own", n)
	}
	if last, _, by, ok := third.Last(); !ok || by != nil || last.Until.Sub(last.Since) != DefaultLoginTimeout {
		t.Errorf("the third connect's own run: %+v (ended by %v), want one bounded by %s", last, by, DefaultLoginTimeout)
	}
}

// The same for a connect's own run that passes its bound and whose process Cogpit cannot end: its
// watch is told at the bound, and the run has no end from then on (the connect still waits behind
// it, until the user cancels or its process ends).
func TestARunPastItsBoundThatCogpitCouldNotEndHasNoEnd(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	p := newWaitingPlugin(t, srv)
	p.give(secretToken, false)
	p.mode(p.stray, true)
	var told atomic.Int32
	watch := NewLoginWatch(func() { told.Add(1) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan connectResult, 1)
	go func() {
		c, err := Connect(ctx, Options{KubeconfigPaths: []string{p.kconf}, Login: watch, LoginTimeout: time.Second})
		done <- connectResult{c, err}
	}()
	wait := waitCtx(t, 20*time.Second)
	if !testkit.WaitFor(wait, func() bool { _, on := watch.Current(); return on && len(p.numbers(p.strays)) == 1 }) {
		t.Fatal("the connect never waited for the plugin's stray child")
	}
	begun := told.Load()
	if !testkit.WaitFor(wait, func() bool { run, _ := watch.Current(); return run.RunsOn }) {
		run, _ := watch.Current()
		t.Fatalf("the run past its bound: %+v, want RunsOn", run)
	}
	if run, on := watch.Current(); !on || !run.Until.IsZero() || run.Since.IsZero() {
		t.Errorf("the run past its bound: %+v (under way %v), want its begin and no end", run, on)
	}
	// The watch hears of it on a goroutine of its own (notifyAll): wait for it, never assume it ran.
	if !testkit.WaitFor(wait, func() bool { return told.Load() > begun }) {
		t.Error("the watch was not told when Cogpit stopped waiting for the run")
	}
	select {
	case r := <-done:
		t.Fatalf("the connect returned while the run went on: %v", r.err)
	default:
	}
	cancel()
	if r := result(t, done); r.err == nil {
		t.Fatal("a cancelled connect connected")
	}
}
