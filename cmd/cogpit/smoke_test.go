package main

import (
	"errors"
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

// TestSmokeVerdict: a run passes with a drawn frame, no logged error and the app's own exit code
// 0; anything else fails it, exit code 1 unless the app's own is another.
func TestSmokeVerdict(t *testing.T) {
	for _, c := range []struct {
		name    string
		prepare func(s *smokeRun)
		code    int
		want    string // in the failure; "" passes
		exit    int
	}{
		{"a drawn frame", func(s *smokeRun) { s.frameDrawn(1440, 900, nil) }, 0, "", 0},
		{"no frame", func(*smokeRun) {}, 0, "the window closed before it drew a frame", 1},
		{"no surface", func(s *smokeRun) { s.frameDrawn(0, 0, errors.New("the window has no GL surface")) }, 0,
			"the window has no GL surface", 1},
		{"a logged error", func(s *smokeRun) { s.frameDrawn(1440, 900, nil); s.noteError("boom") }, 0,
			"errors logged (1): boom", 1},
		{"the app's exit code", func(s *smokeRun) { s.frameDrawn(1440, 900, nil) }, 2, "exit code 2", 2},
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
// 1 and says so, whatever its main goroutine does.
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
	if !strings.Contains(string(out), "cogpit: smoke: failed: the window drew no frame within 50ms") {
		t.Errorf("the stuck run said:\n%s", out)
	}
}
