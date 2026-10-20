//go:build linux

package update

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeApt stands in for dpkg-query, apt-cache and pkexec: the versions apt knows, and what the
// helper does when pkexec runs it.
type fakeApt struct {
	mu        sync.Mutex
	installed string
	candidate string
	calls     []string
	helper    func(f *fakeApt) ([]byte, error) // pkexec's run; nil: installs the candidate
}

func (f *fakeApt) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	switch name {
	case "dpkg-query":
		if f.installed == "" {
			return []byte("dpkg-query: no packages found matching cogpit"), errors.New("exit status 1")
		}
		return []byte("install ok installed"), nil
	case "apt-cache":
		none := func(v string) string {
			if v == "" {
				return "(none)"
			}
			return v
		}
		return []byte("cogpit:\n  Installed: " + none(f.installed) + "\n  Candidate: " + none(f.candidate) +
			"\n  Version table:\n *** " + none(f.installed) + " 500\n"), nil
	case "pkexec":
		if f.helper != nil {
			return f.helper(f)
		}
		f.installed = f.candidate
		return []byte("Setting up cogpit (" + f.candidate + ") ...\n"), nil
	}
	return nil, exec.ErrNotFound
}

func (f *fakeApt) set(installed, candidate string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installed, f.candidate = installed, candidate
}

func (f *fakeApt) ran(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.ContainsFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, prefix) })
}

// aptTest wires a fake package installation: this process is /usr/bin/cogpit at running, the
// sources file exists, and apt is f. It returns the messages sent and the restarts started.
type aptTest struct {
	f        *fakeApt
	mu       sync.Mutex
	msgs     []string
	restarts []string
	quits    int
}

func newAptTest(t *testing.T, running string) *aptTest {
	t.Helper()
	sources := filepath.Join(t.TempDir(), "cogpit.sources")
	if err := os.WriteFile(sources, []byte("Types: deb\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	at := &aptTest{f: &fakeApt{installed: running, candidate: running}}
	saved := []any{debSources, executable, runningVersion, runCommand, startRestart, quitSelf, checkInterval}
	debSources = sources
	executable = func() (string, error) { return debExecutable, nil }
	runningVersion = func() (string, bool) { return running, true }
	runCommand = at.f.run
	startRestart = func(pid int, exe string, args []string) error {
		at.mu.Lock()
		defer at.mu.Unlock()
		at.restarts = append(at.restarts, exe)
		return nil
	}
	quitSelf = func() error {
		at.mu.Lock()
		defer at.mu.Unlock()
		at.quits++
		return nil
	}
	t.Cleanup(func() {
		setAutomaticChecks(false)
		debSources = saved[0].(string)
		executable = saved[1].(func() (string, error))
		runningVersion = saved[2].(func() (string, bool))
		runCommand = saved[3].(func(context.Context, string, ...string) ([]byte, error))
		startRestart = saved[4].(func(int, string, []string) error)
		quitSelf = saved[5].(func() error)
		checkInterval = saved[6].(time.Duration)
		resetUpdateState()
	})
	resetUpdateState()
	messages.listen(func(level, message string) {
		at.mu.Lock()
		defer at.mu.Unlock()
		at.msgs = append(at.msgs, level+": "+message)
	})
	return at
}

func resetUpdateState() {
	notice.mu.Lock()
	notice.available, notice.version, notice.restart, notice.listeners = false, "", false, nil
	notice.mu.Unlock()
	messages.mu.Lock()
	messages.listeners = nil
	messages.mu.Unlock()
	apt.mu.Lock()
	apt.running, apt.busy = "", false
	apt.mu.Unlock()
}

// waitFor polls cond for up to five seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waited 5 s for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (at *aptTest) messages() []string {
	at.mu.Lock()
	defer at.mu.Unlock()
	return slices.Clone(at.msgs)
}

func TestParsePolicy(t *testing.T) {
	out := []byte("cogpit:\n  Installed: 0.33.0\n  Candidate: 0.34.1\n  Version table:\n     0.34.1 500\n" +
		"        500 https://guided-traffic.github.io/cogpit/apt stable/main amd64 Packages\n *** 0.33.0 100\n")
	if got := parsePolicy(out); got != (aptVersions{installed: "0.33.0", candidate: "0.34.1"}) {
		t.Errorf("parsePolicy = %+v", got)
	}
	if got := parsePolicy([]byte("cogpit:\n  Installed: (none)\n  Candidate: 0.34.1\n")); got.installed != "" || got.candidate != "0.34.1" {
		t.Errorf("not installed: %+v", got)
	}
	if got := parsePolicy(nil); got != (aptVersions{}) {
		t.Errorf("nothing: %+v", got)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int // its sign
	}{
		{"0.10.0", "0.9.3", 1}, {"0.9.3", "0.10.0", -1}, {"1.2.3", "1.2.3", 0}, {"1.2.3", "1.2", 1},
		{"0.34.0", "", 1}, {"", "0.1.0", -1}, {"0.33.0-1", "0.33.0", 1},
	} {
		got := compareVersions(c.a, c.b)
		if (got > 0) != (c.want > 0) || (got < 0) != (c.want < 0) {
			t.Errorf("compareVersions(%q, %q) = %d, want the sign of %d", c.a, c.b, got, c.want)
		}
	}
}

// load says why a Cogpit cannot update through apt: not the package's executable, a development
// build, the package not installed, its sources file deleted.
func TestAptLoadReasons(t *testing.T) {
	at := newAptTest(t, "0.33.0")
	if err := load(); err != nil {
		t.Fatalf("the installed package: %v", err)
	}
	if apt.running != "0.33.0" {
		t.Errorf("running = %q", apt.running)
	}
	executable = func() (string, error) { return "/usr/bin/cogpit (deleted)", nil }
	if err := load(); err != nil {
		t.Errorf("replaced under the running process: %v", err)
	}
	executable = func() (string, error) { return "/home/me/bin/cogpit", nil }
	if err := load(); !errors.Is(err, ErrNotFromPackage) {
		t.Errorf("a copy elsewhere: %v", err)
	}
	executable = func() (string, error) { return debExecutable, nil }
	runningVersion = func() (string, bool) { return "dev", false }
	if err := load(); !errors.Is(err, ErrNotFromPackage) {
		t.Errorf("a development build: %v", err)
	}
	runningVersion = func() (string, bool) { return "0.33.0", true }
	at.f.set("", "0.33.0")
	if err := load(); !errors.Is(err, ErrNotFromPackage) {
		t.Errorf("the package not installed: %v", err)
	}
	at.f.set("0.33.0", "0.33.0")
	if err := os.Remove(debSources); err != nil {
		t.Fatal(err)
	}
	if err := load(); !errors.Is(err, ErrNoRepository) {
		t.Errorf("the sources file deleted: %v", err)
	}
	if head, _, _ := strings.Cut(ErrNoRepository.Error(), ":"); head != "no package repository" {
		t.Errorf("the menu's short reason: %q", head)
	}
}

// The notice follows apt: a candidate newer than installed is an update to install, an installed
// release newer than this process one to restart into, the same versions none.
func TestAptNoticeFollowsApt(t *testing.T) {
	at := newAptTest(t, "0.33.0")
	if err := load(); err != nil {
		t.Fatal(err)
	}
	u := &Updater{}
	for _, c := range []struct {
		installed, candidate string
		version              string
		available, restart   bool
	}{
		{"0.33.0", "0.33.0", "", false, false},
		{"0.33.0", "0.34.0", "0.34.0", true, false},
		{"0.34.0", "0.34.0", "0.34.0", true, true},
		{"0.34.0", "0.35.0", "0.34.0", true, true}, // a restart first: the newer one comes next
		{"0.32.0", "0.33.0", "", false, false},     // apt's lists older than this process
	} {
		at.f.set(c.installed, c.candidate)
		if _, err := refreshNotice(); err != nil {
			t.Fatal(err)
		}
		v, ok := u.AvailableUpdate()
		if ok != c.available || v != c.version || u.RestartPending() != c.restart {
			t.Errorf("installed %s, candidate %s: AvailableUpdate = %q %v, RestartPending = %v; want %q %v %v",
				c.installed, c.candidate, v, ok, u.RestartPending(), c.version, c.available, c.restart)
		}
	}
}

// The scheduled checks read apt at once and every interval, start once, and stop when turned off.
func TestAptScheduledChecks(t *testing.T) {
	at := newAptTest(t, "0.33.0")
	if err := load(); err != nil {
		t.Fatal(err)
	}
	checkInterval = 20 * time.Millisecond
	changed := make(chan struct{}, 10)
	notice.listen(func() { changed <- struct{}{} })
	at.f.set("0.33.0", "0.34.0")
	start(true)
	setAutomaticChecks(true) // a second start starts nothing more
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("no check at the start")
	}
	if v, ok := notice.get(); !ok || v != "0.34.0" {
		t.Errorf("after the first check: %q %v", v, ok)
	}
	at.f.set("0.35.0", "0.35.0") // the system's updater installed a newer one meanwhile
	waitFor(t, "the next check", notice.restartPending)
	setAutomaticChecks(false)
	at.f.mu.Lock()
	n := len(at.f.calls)
	at.f.mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	at.f.mu.Lock()
	defer at.f.mu.Unlock()
	if len(at.f.calls) != n {
		t.Errorf("checks after they were turned off: %v", at.f.calls[n:])
	}
}

// The Update button installs through pkexec and the helper, says so, and offers the restart; its
// second click restarts into the installed release.
func TestAptUpdateThenRestart(t *testing.T) {
	at := newAptTest(t, "0.33.0")
	if err := load(); err != nil {
		t.Fatal(err)
	}
	at.f.set("0.33.0", "0.34.0")
	if _, err := refreshNotice(); err != nil {
		t.Fatal(err)
	}
	check()
	waitFor(t, "the install's message", func() bool { return len(at.messages()) == 1 })
	if !at.f.ran("pkexec " + debHelper) {
		t.Errorf("pkexec did not run the helper: %v", at.f.calls)
	}
	if got := at.messages()[0]; got != "info: Cogpit 0.34.0 is installed: click Restart to Update to use it" {
		t.Errorf("message %q", got)
	}
	if !notice.restartPending() {
		t.Fatal("no restart pending after the install")
	}
	waitFor(t, "the first click's end", func() bool { apt.mu.Lock(); defer apt.mu.Unlock(); return !apt.busy })
	check()
	waitFor(t, "the restart", func() bool { at.mu.Lock(); defer at.mu.Unlock(); return at.quits == 1 })
	at.mu.Lock()
	defer at.mu.Unlock()
	if !slices.Equal(at.restarts, []string{debExecutable}) {
		t.Errorf("restarts %v", at.restarts)
	}
}

// Nothing newer: the helper runs (a fresh list needs root) and says this is the newest.
func TestAptCheckWithoutUpdate(t *testing.T) {
	at := newAptTest(t, "0.33.0")
	if err := load(); err != nil {
		t.Fatal(err)
	}
	check()
	waitFor(t, "the message", func() bool { return len(at.messages()) == 1 })
	if got := at.messages()[0]; got != "info: Cogpit 0.33.0 is the newest release in Cogpit's package repository" {
		t.Errorf("message %q", got)
	}
}

// What a failed helper says: the dialog dismissed, not authorized, no repository, apt's error, no
// pkexec.
func TestAptInstallFailures(t *testing.T) {
	exitErr := func(code int) error {
		err := exec.Command("/bin/sh", "-c", "exit "+strconv.Itoa(code)).Run()
		var e *exec.ExitError
		if !errors.As(err, &e) || e.ExitCode() != code {
			t.Fatalf("exit %d: %v", code, err)
		}
		return err
	}
	for _, c := range []struct {
		err   error
		out   string
		level string
		has   string
	}{
		{exitErr(126), "", LevelInfo, "cancelled: no password was given"},
		{exitErr(127), "", LevelError, "not authorized"},
		{exitErr(3), "", LevelError, "no package repository"},
		{exitErr(100), "Get:1 …\nE: Failed to fetch https://guided-traffic.github.io/cogpit/apt/pool/x.deb  404\n\n", LevelError,
			"failed: E: Failed to fetch https://guided-traffic.github.io/cogpit/apt/pool/x.deb  404; update Cogpit in a terminal"},
		{exec.ErrNotFound, "", LevelError, "pkexec is not installed"},
		{context.DeadlineExceeded, "", LevelError, "did not finish within"},
	} {
		level, msg := installFailure(c.err, []byte(c.out))
		if level != c.level || !strings.Contains(msg, c.has) {
			t.Errorf("%v: %s %q, want %s containing %q", c.err, level, msg, c.level, c.has)
		}
	}
}

// A second click while the first runs says so and starts nothing.
func TestAptOneUpdateAtATime(t *testing.T) {
	at := newAptTest(t, "0.33.0")
	if err := load(); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	at.f.helper = func(f *fakeApt) ([]byte, error) {
		f.mu.Unlock()
		<-release
		f.mu.Lock()
		return nil, nil
	}
	check()
	waitFor(t, "the helper", func() bool { return at.f.ran("pkexec") })
	check()
	waitFor(t, "the second click's message", func() bool { return len(at.messages()) == 1 })
	if got := at.messages()[0]; got != "info: Cogpit's update is already running" {
		t.Errorf("message %q", got)
	}
	close(release)
	waitFor(t, "the first click's message", func() bool { return len(at.messages()) == 2 })
}

// The restart script waits for the process to end, then starts the program with its arguments.
func TestRestartScriptWaitsThenStarts(t *testing.T) {
	sleeper := exec.Command("sleep", "0.3")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = sleeper.Wait(); close(done) }()
	out := filepath.Join(t.TempDir(), "started")
	if err := startDetached(sleeper.Process.Pid, "/bin/sh", []string{"-c", `echo "$0" > "$1"`, "restarted", out}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(out); err == nil {
		t.Fatal("started before the process ended")
	}
	<-done
	waitFor(t, "the start", func() bool { b, _ := os.ReadFile(out); return strings.TrimSpace(string(b)) == "restarted" })
}
