package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/ui/fyneui"
)

// The smoke run (--smoke <bound>, D-112): the app as it ships, started once until its window
// presented a drawn frame; CI runs it on each platform's packaged app (make smoke, hack/smoke.sh).
// It reads and writes nothing of the user's: HOME, the configuration and cache folders and
// $KUBECONFIG point into a home of its own in the system's temporary folder (smokeEnv). The run
// leaves that home, which its verdict names: Fyne's settings watcher makes its folder again while
// the process lives, and Windows keeps the open log; hack/smoke.sh gives the run a temporary folder
// of its own and removes it once the process ended. The run starts nothing that reaches beyond the
// process: no MCP server, no updater (loaded, not started), no login item, no cluster connection
// (run). It fails, exit 1, when the window drew no frame, when an error reached the log, or when it
// took longer than its bound (the watchdog's exit, whatever the main goroutine is doing).

// smokeErrorsShown bounds the logged errors a failed smoke run repeats.
const smokeErrorsShown = 5

// smokeRun is a smoke run's state; a nil one is no smoke run.
type smokeRun struct {
	home     string
	bound    time.Duration
	begin    time.Time
	watchdog *time.Timer

	mu      sync.Mutex
	frame   string   // "a frame of <w>×<h> pixels after <d>", once the window drew one
	noFrame error    // why the window had no frame to read
	errs    []string // the first smokeErrorsShown errors logged
	nErrs   int
}

// startSmoke starts a smoke run bounded by bound: its home in the system's temporary folder, the
// environment that points there, and the watchdog.
func startSmoke(bound time.Duration) (*smokeRun, error) {
	home, err := os.MkdirTemp("", "cogpit-smoke-")
	if err != nil {
		return nil, err
	}
	if err := smokeEnv(home); err != nil {
		_ = os.RemoveAll(home)
		return nil, err
	}
	s := &smokeRun{home: home, bound: bound, begin: time.Now()}
	s.watchdog = time.AfterFunc(bound, s.expire)
	return s, nil
}

// smokeEnv makes home the process's home on every platform: HOME and USERPROFILE, the
// configuration, cache and data folders (APPDATA, LOCALAPPDATA, XDG_*) and $KUBECONFIG, a file
// there that does not exist. It removes what leads elsewhere: Helm's own paths (HELM_*), the
// in-cluster variables of a pod and Windows' HOMEDRIVE and HOMEPATH, which client-go reads before
// USERPROFILE. X11's authority file stays the user's, which a desktop session may keep in the real
// home without naming it in XAUTHORITY.
func smokeEnv(home string) error {
	if os.Getenv("XAUTHORITY") == "" {
		if userHome, err := os.UserHomeDir(); err == nil {
			if auth := filepath.Join(userHome, ".Xauthority"); fileExists(auth) {
				if err := os.Setenv("XAUTHORITY", auth); err != nil {
					return err
				}
			}
		}
	}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(name, "HELM_"), name == "KUBERNETES_SERVICE_HOST", name == "KUBERNETES_SERVICE_PORT",
			name == "HOMEDRIVE", name == "HOMEPATH":
			if err := os.Unsetenv(name); err != nil {
				return err
			}
		}
	}
	for name, dir := range map[string]string{
		"HOME":            home,
		"USERPROFILE":     home,
		"APPDATA":         filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA":    filepath.Join(home, "AppData", "Local"),
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_CACHE_HOME":  filepath.Join(home, ".cache"),
		"XDG_DATA_HOME":   filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME":  filepath.Join(home, ".local", "state"),
		"KUBECONFIG":      filepath.Join(home, ".kube", "config"),
	} {
		if err := os.Setenv(name, dir); err != nil {
			return err
		}
	}
	return nil
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// apply makes the window's options a smoke run's: nothing connects at start, and the window's
// first drawn frame ends the run.
func (s *smokeRun) apply(opts *fyneui.Options) {
	opts.AutoConnect = ""
	opts.Smoke = s.frameDrawn
}

// frameDrawn is fyneui.Options.Smoke: the size of the window's first drawn frame, or why it had
// none to read.
func (s *smokeRun) frameDrawn(width, height int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.noFrame = err
		return
	}
	s.frame = fmt.Sprintf("a frame of %d×%d pixels after %s", width, height, time.Since(s.begin).Round(time.Millisecond))
}

// watch wraps the app log's handler so that the run counts the errors that reach the log: records
// at level ERROR, and Fyne's, which come through the standard log package at INFO ("Fyne error:",
// fyne.LogError) or, in a build with Fyne's thread check on, as its warning ("*** Error in Fyne call
// thread"). Every record still goes to h. Without a smoke run, h itself.
func (s *smokeRun) watch(h slog.Handler) slog.Handler {
	if s == nil {
		return h
	}
	return smokeLog{Handler: h, run: s}
}

type smokeLog struct {
	slog.Handler
	run *smokeRun
}

func (h smokeLog) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError || strings.HasPrefix(r.Message, "Fyne error") ||
		strings.HasPrefix(r.Message, "*** Error in Fyne call thread") {
		h.run.noteError(r.Message)
	}
	return h.Handler.Handle(ctx, r)
}

func (h smokeLog) WithAttrs(attrs []slog.Attr) slog.Handler {
	return smokeLog{Handler: h.Handler.WithAttrs(attrs), run: h.run}
}

func (h smokeLog) WithGroup(name string) slog.Handler {
	return smokeLog{Handler: h.Handler.WithGroup(name), run: h.run}
}

func (s *smokeRun) noteError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nErrs++
	if len(s.errs) < smokeErrorsShown {
		s.errs = append(s.errs, msg)
	}
}

// end says the run's verdict and returns the process's exit code: 0 when the run passed, else the
// app's own exit code, or 1 where that is 0. Without a smoke run, code.
func (s *smokeRun) end(code int) int {
	if s == nil {
		return code
	}
	s.watchdog.Stop()
	s.mu.Lock()
	why := s.failure(code)
	frame := s.frame
	s.mu.Unlock()
	if why != "" {
		fmt.Fprintf(os.Stderr, "cogpit: smoke: failed: %s (home %s)\n", why, s.home)
		if code == 0 {
			code = 1
		}
		return code
	}
	fmt.Fprintf(os.Stderr, "cogpit: smoke: passed: %s, quit after %s (home %s)\n", frame,
		time.Since(s.begin).Round(time.Millisecond), s.home)
	return code
}

// failure is why the run failed, "" when it passed; s.mu held.
func (s *smokeRun) failure(code int) string {
	switch {
	case code != 0:
		return fmt.Sprintf("the app ended with exit code %d%s", code, s.loggedErrors())
	case s.noFrame != nil:
		return s.noFrame.Error() + s.loggedErrors()
	case s.frame == "":
		return "the window closed before it drew a frame" + s.loggedErrors()
	case s.nErrs > 0:
		return strings.TrimPrefix(s.loggedErrors(), "; ")
	}
	return ""
}

// loggedErrors names the errors that reached the log, "" for none; s.mu held.
func (s *smokeRun) loggedErrors() string {
	if s.nErrs == 0 {
		return ""
	}
	return fmt.Sprintf("; errors logged (%d): %s", s.nErrs, strings.Join(s.errs, " | "))
}

// expire is the watchdog: the run took longer than its bound. It ends the process.
func (s *smokeRun) expire() {
	s.mu.Lock()
	what := "the window drew no frame"
	if s.frame != "" {
		what = "the window drew " + s.frame + ", but Cogpit did not quit"
	}
	fmt.Fprintf(os.Stderr, "cogpit: smoke: failed: %s within %s%s (home %s)\n", what, s.bound, s.loggedErrors(), s.home)
	s.mu.Unlock()
	os.Exit(1)
}
