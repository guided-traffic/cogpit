package main

import (
	"errors"
	"image/color"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/app"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui"
)

// within reports whether path lies in dir.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// TestSmokeEnvLeavesTheUsersHome: in a smoke run every folder Cogpit and its libraries read or
// write lies in the run's own home, whatever the user's environment names: the configuration,
// cache and home folders, Helm's repositories file and $KUBECONFIG; the in-cluster variables are
// gone; X11's authority file stays the user's.
func TestSmokeEnvLeavesTheUsersHome(t *testing.T) {
	user := t.TempDir()
	// Every variable smokeEnv sets or removes comes back after the test.
	for _, kv := range os.Environ() {
		if name, value, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "HELM_") {
			t.Setenv(name, value)
		}
	}
	for name, value := range map[string]string{
		"HOME": user, "USERPROFILE": user, "APPDATA": filepath.Join(user, "roaming"), "LOCALAPPDATA": filepath.Join(user, "local"),
		"XDG_CONFIG_HOME": filepath.Join(user, "config"), "XDG_CACHE_HOME": filepath.Join(user, "cache"),
		"XDG_DATA_HOME": filepath.Join(user, "data"), "XDG_STATE_HOME": filepath.Join(user, "state"),
		"KUBECONFIG": filepath.Join(user, ".kube", "config"), "HELM_CONFIG_HOME": filepath.Join(user, "helm"),
		"HELM_REPOSITORY_CONFIG": filepath.Join(user, "helm", "repositories.yaml"), "XAUTHORITY": "",
		"KUBERNETES_SERVICE_HOST": "10.96.0.1", "KUBERNETES_SERVICE_PORT": "443", "HOMEDRIVE": "C:", "HOMEPATH": `\Users\someone`,
	} {
		t.Setenv(name, value)
	}
	if err := os.WriteFile(filepath.Join(user, ".Xauthority"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	if err := smokeEnv(home); err != nil {
		t.Fatal(err)
	}
	dirs, err := app.UserDirs()
	if err != nil {
		t.Fatal(err)
	}
	for what, path := range map[string]string{
		"configuration folder": dirs.Config, "cache folder": dirs.Cache, "home": dirs.Home,
		"Helm's repositories file": helm.RepositoryConfigPath(), "$KUBECONFIG": os.Getenv("KUBECONFIG"),
	} {
		if !within(home, path) {
			t.Errorf("the %s is %s, outside the smoke run's home %s", what, path, home)
		}
	}
	for _, name := range []string{"HELM_CONFIG_HOME", "HELM_REPOSITORY_CONFIG", "KUBERNETES_SERVICE_HOST",
		"KUBERNETES_SERVICE_PORT", "HOMEDRIVE", "HOMEPATH"} {
		if v, ok := os.LookupEnv(name); ok {
			t.Errorf("%s is still set (%q)", name, v)
		}
	}
	if got := os.Getenv("XAUTHORITY"); got != filepath.Join(user, ".Xauthority") {
		t.Errorf("XAUTHORITY is %q, not the user's authority file", got)
	}
}

// newTestSmoke is a smoke run without a home or a watchdog that could fire.
func newTestSmoke(t *testing.T) *smokeRun {
	t.Helper()
	s := &smokeRun{home: t.TempDir(), bound: time.Hour, begin: time.Now()}
	s.watchdog = time.AfterFunc(time.Hour, func() {})
	t.Cleanup(func() { s.watchdog.Stop() })
	return s
}

// drawn is the read of a drawn frame of 1440 × 900 pixels.
var drawn = fyneui.SmokeFrame{From: "the X server's copy of the window", Width: 1440, Height: 900, Drawn: true}

// TestSmokeVerdict: a run passes with a drawn frame, no logged error and the app's own exit code
// 0; anything else fails it, exit code 1 unless the app's own is another. Fyne's preferences load
// error after the frame comes from the quit and does not count; before it, it does.
func TestSmokeVerdict(t *testing.T) {
	prefs := "Fyne error:  " + fynePrefsLoadError
	for _, c := range []struct {
		name    string
		prepare func(s *smokeRun)
		code    int
		want    string // in the failure; "" passes
		exit    int
	}{
		{"a drawn frame", func(s *smokeRun) { s.read(drawn) }, 0, "", 0},
		{"no frame", func(*smokeRun) {}, 0, "the window closed before it drew a frame", 1},
		{"no drawn frame", func(s *smokeRun) { s.read(fyneui.SmokeFrame{Width: 1440, Height: 900}) }, 0,
			"the window closed before it drew a frame", 1},
		{"no surface", func(s *smokeRun) { s.read(fyneui.SmokeFrame{Err: errors.New("the window has no GL surface")}) }, 0,
			"the window has no GL surface", 1},
		{"a logged error", func(s *smokeRun) { s.read(drawn); s.noteError("boom") }, 0,
			"errors logged (1): boom", 1},
		{"Fyne's preferences race while quitting", func(s *smokeRun) { s.read(drawn); s.noteError(prefs) }, 0, "", 0},
		{"a preferences load error before the frame", func(s *smokeRun) { s.noteError(prefs); s.read(drawn) }, 0,
			"errors logged (1): " + prefs, 1},
		{"the app's exit code", func(s *smokeRun) { s.read(drawn) }, 2, "exit code 2", 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newTestSmoke(t)
			c.prepare(s)
			s.mu.Lock()
			got := s.failure(c.code)
			s.mu.Unlock()
			if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
				t.Errorf("failure %q, want %q", got, c.want)
			}
			if exit := s.end(c.code); exit != c.exit {
				t.Errorf("exit code %d, want %d", exit, c.exit)
			}
		})
	}
	var none *smokeRun
	if none.end(3) != 3 {
		t.Error("without a smoke run the exit code changed")
	}
}

// TestSmokeCountsLoggedErrors: records at level ERROR count, and Fyne's errors, which reach the
// log at INFO; warnings and other records do not. Every record still reaches the log.
func TestSmokeCountsLoggedErrors(t *testing.T) {
	s := newTestSmoke(t)
	var out strings.Builder
	logger := slog.New(s.watch(slog.NewTextHandler(&out, nil)))
	logger.Error("boom")
	logger.Info("Fyne error:  window creation error")
	logger.With("k", "v").WithGroup("g").Error("grouped")
	logger.Info("*** Error in Fyne call thread, this should have been called in fyne.Do[AndWait] ***")
	logger.Warn("a warning")
	logger.Info("fine")
	if s.nErrs != 4 || s.errs[0] != "boom" || s.errs[2] != "grouped" {
		t.Errorf("counted %d: %q", s.nErrs, s.errs)
	}
	if n := strings.Count(out.String(), "\n"); n != 6 {
		t.Errorf("%d records reached the log, want 6:\n%s", n, out.String())
	}
	if h := slog.NewTextHandler(io.Discard, nil); (*smokeRun)(nil).watch(h) != slog.Handler(h) {
		t.Error("without a smoke run the log's handler is wrapped")
	}
}

// TestSmokeProgress: a run without a drawn frame says how far its reads got: none, or how many,
// from where, how long ago the last was and what it held.
func TestSmokeProgress(t *testing.T) {
	s := newTestSmoke(t)
	if got := s.progress(); got != "; no read of the window's frame ran" {
		t.Errorf("no read: %q", got)
	}
	gl := "the GL front buffer"
	s.read(fyneui.SmokeFrame{From: gl})
	s.read(fyneui.SmokeFrame{From: gl, Width: 1440, Height: 900, Colour: color.NRGBA{R: 0x1e, G: 0x1f, B: 0x22, A: 0xff}})
	got := s.progress()
	if !strings.HasPrefix(got, "; 2 reads of the window's frame from the GL front buffer, the last ") ||
		!strings.HasSuffix(got, " ago: 1440×900 pixels of one colour #1e1f22ff") {
		t.Errorf("two reads: %q", got)
	}
	s.read(fyneui.SmokeFrame{From: gl})
	if got := s.progress(); !strings.HasSuffix(got, " ago: no pixels, the canvas had no size") {
		t.Errorf("a read without pixels: %q", got)
	}
	s.read(fyneui.SmokeFrame{From: drawn.From, Unread: "the window is not viewable"})
	if got := s.progress(); !strings.Contains(got, "from the X server's copy of the window, the last ") ||
		!strings.HasSuffix(got, " ago: the window is not viewable") {
		t.Errorf("an unread frame: %q", got)
	}
	s.read(drawn)
	if got := s.progress(); got != "" {
		t.Errorf("a drawn frame: %q", got)
	}
	if !strings.HasSuffix(s.frame, ", read from the X server's copy of the window") {
		t.Errorf("the drawn frame is %q", s.frame)
	}
}

// Goroutine stacks as Go prints them, from a smoke run whose quit hung in Fyne 2.8.2's shutdown
// (CI-like container, 2026-10-27), shortened.
const (
	stackMainWaitsForEvents = `goroutine 1 [chan receive, locked to thread]:
fyne.io/fyne/v2/internal/app.(*Lifecycle).WaitForEvents(...)
	/go/pkg/mod/fyne.io/fyne/v2@v2.8.2/internal/app/lifecycle.go:120
fyne.io/fyne/v2/internal/driver/glfw.(*gLDriver).Run(0x7a3caaf92160)
	/go/pkg/mod/fyne.io/fyne/v2@v2.8.2/internal/driver/glfw/driver.go:199 +0x130
main.main()
	/home/runner/exp/cmd/cogpit/main.go:50 +0x110`
	stackQueueWaitsForMain = `goroutine 114 [chan receive]:
fyne.io/fyne/v2/internal/driver/glfw.runOnMainWithWait(0x7a3caaac04c8, 0x1)
	/go/pkg/mod/fyne.io/fyne/v2@v2.8.2/internal/driver/glfw/loop.go:53 +0x108
fyne.io/fyne/v2/internal/driver/glfw.(*gLDriver).DoFromGoroutine(0x7a3caa5698f0?, 0x7a3caaac04c8, 0x1?)
	/go/pkg/mod/fyne.io/fyne/v2@v2.8.2/internal/driver/glfw/driver.go:102 +0x68
fyne.io/fyne/v2/internal/app.(*Lifecycle).RunEventQueue(...)
	/go/pkg/mod/fyne.io/fyne/v2@v2.8.2/internal/app/lifecycle.go:111
created by fyne.io/fyne/v2/app.(*fyneApp).Run in goroutine 1
	/go/pkg/mod/fyne.io/fyne/v2@v2.8.2/app/app.go:77 +0xc8`
	stackMainInSwap = `goroutine 1 [syscall, locked to thread]:
github.com/go-gl/glfw/v3.4/glfw._Cfunc_glfwSwapBuffers(0x2a0f1c0)
	_cgo_gotypes.go:1872 +0x30
main.main()
	/home/runner/exp/cmd/cogpit/main.go:50 +0x110`
	stackOther = `goroutine 100 [select]:
fyne.io/fyne/v2/internal/async.(*UnboundedChan[...]).processing(0xaaaacaf62480)
	/go/pkg/mod/fyne.io/fyne/v2@v2.8.2/internal/async/chan.go:43 +0xa8`
)

// TestSmokeFyneQuitDeadlock: Fyne's shutdown deadlocked on its lifecycle queue is the main
// goroutine waiting in Lifecycle.WaitForEvents and the queue's goroutine in runOnMainWithWait,
// both; either alone is not.
func TestSmokeFyneQuitDeadlock(t *testing.T) {
	join := func(gs ...string) string { return strings.Join(gs, "\n\n") + "\n" }
	for _, c := range []struct {
		name   string
		stacks string
		want   bool
	}{
		{"both", join(stackMainWaitsForEvents, stackOther, stackQueueWaitsForMain), true},
		{"the main goroutine alone", join(stackMainWaitsForEvents, stackOther), false},
		{"the queue alone", join(stackMainInSwap, stackQueueWaitsForMain), false},
		{"neither", join(stackMainInSwap, stackOther), false},
	} {
		if got := fyneQuitDeadlock(c.stacks); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestSmokeExpired: the watchdog's verdict. Without a drawn frame it fails with how far the reads
// got and the main goroutine's stack; with one and a quit stuck in Fyne's shutdown deadlock it
// passes and names it, unless an error was logged; with one and a quit stuck elsewhere it fails.
func TestSmokeExpired(t *testing.T) {
	deadlock := strings.Join([]string{stackMainWaitsForEvents, stackQueueWaitsForMain}, "\n\n")
	elsewhere := strings.Join([]string{stackMainInSwap, stackOther}, "\n\n")
	for _, c := range []struct {
		name    string
		prepare func(s *smokeRun)
		stacks  string
		code    int
		want    []string
	}{
		{"no frame", func(s *smokeRun) { s.read(fyneui.SmokeFrame{From: drawn.From, Width: 1440, Height: 900}) }, elsewhere, 1,
			[]string{"failed: the window drew no frame within 1h0m0s; 1 read of the window's frame from the X server's", "of one colour #00000000",
				"the main goroutine, which runs Fyne's event loop:\n" + stackMainInSwap}},
		{"Fyne's quit deadlock", func(s *smokeRun) { s.read(drawn) }, deadlock, 0,
			[]string{"passed: a frame of 1440×900 pixels after", "not counted: Fyne's shutdown deadlocked on its lifecycle queue"}},
		{"Fyne's quit deadlock and a logged error", func(s *smokeRun) { s.read(drawn); s.noteError("boom") }, deadlock, 1,
			[]string{"failed: errors logged (1): boom", "not counted: Fyne's shutdown deadlocked"}},
		{"a quit stuck elsewhere", func(s *smokeRun) { s.read(drawn) }, elsewhere, 1,
			[]string{"failed: the window drew a frame of 1440×900 pixels after", "but Cogpit did not quit within",
				"the main goroutine, which runs Fyne's event loop:\n" + stackMainInSwap}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newTestSmoke(t)
			c.prepare(s)
			msg, code := s.expired(c.stacks)
			if code != c.code {
				t.Errorf("exit code %d, want %d: %s", code, c.code, msg)
			}
			for _, w := range c.want {
				if !strings.Contains(msg, w) {
					t.Errorf("the verdict does not say %q:\n%s", w, msg)
				}
			}
		})
	}
}

// smokeStuckEnv makes the test binary a smoke run whose window never comes
// (TestSmokeWatchdogEndsAStuckRun).
const smokeStuckEnv = "COGPIT_SMOKE_STUCK_HELPER"

// TestSmokeStuckHelper is not a test: run as a subprocess with smokeStuckEnv set, it starts a smoke
// run of 50 ms and then waits far longer, as a window that hangs.
func TestSmokeStuckHelper(t *testing.T) {
	if os.Getenv(smokeStuckEnv) == "" {
		t.Skip("a helper process for TestSmokeWatchdogEndsAStuckRun")
	}
	if _, err := startSmoke(50 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Minute)
}

// TestSmokeWatchdogEndsAStuckRun: a smoke run that takes longer than its bound ends with exit code
// 1 and says so, whatever its main goroutine does, with how far its reads got and that goroutine's
// stack.
func TestSmokeWatchdogEndsAStuckRun(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a process")
	}
	tmp := t.TempDir() // the run's home goes there
	cmd := exec.Command(os.Args[0], "-test.run=^TestSmokeStuckHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), smokeStuckEnv+"=1", "TMPDIR="+tmp, "TMP="+tmp, "TEMP="+tmp)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("the stuck run ended with %v, want exit code 1:\n%s", err, out)
	}
	for _, want := range []string{
		"cogpit: smoke: failed: the window drew no frame within 50ms; no read of the window's frame ran",
		"cogpit: smoke: the main goroutine, which runs Fyne's event loop:\ngoroutine 1 [",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the stuck run did not say %q:\n%s", want, out)
		}
	}
}
