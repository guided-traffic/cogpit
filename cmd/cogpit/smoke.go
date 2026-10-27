package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
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
// took longer than its bound or, once the window drew its frame, than smokeQuitGrace to quit (the
// watchdog's exit, whatever the main goroutine is doing); then it says how far the reads of the
// window's frame got and where the main goroutine was. Two races in Fyne 2.8.2's own stop are named
// and not counted: its preferences load error (fynePrefsLoadError) and its deadlock on its lifecycle
// queue (fyneQuitDeadlock), which the app's quit bound ends as in every run (quit.go) and the run
// judges by the stacks at that moment (quitBoundPassed). Any other error or hang while quitting
// counts.

// smokeErrorsShown bounds the logged errors a failed smoke run repeats.
const smokeErrorsShown = 5

// smokeQuitGrace is how long a smoke run gives Cogpit to quit once its window drew a frame, within
// the run's bound: longer than the quit bound, which ends a hang of Fyne's teardown, so that what
// the grace catches is a quit Cogpit itself did not finish.
const smokeQuitGrace = quitBound + 10*time.Second

// fynePrefsLoadError is the line Fyne logs when it cannot read its preferences file. Its save of
// the preferences when the app stops races its own watch of that file: the watch can reload the
// file between the save's truncation and its write and read nothing (cause EOF; fyne.io/fyne/v2/app
// preferences.go, save and watch). A smoke run quits once its frame was drawn, so this line, logged
// after the frame, comes from that quit: the run names it and does not count it.
const fynePrefsLoadError = "Preferences load error:"

// smokeRun is a smoke run's state; a nil one is no smoke run.
type smokeRun struct {
	home     string
	bound    time.Duration
	begin    time.Time
	watchdog *time.Timer

	mu      sync.Mutex
	frame   string            // "a frame of <w>×<h> pixels after <d>, read from <where>", once the window drew one
	noFrame error             // why the window had no frame to read
	reads   int               // the reads of the window's frame so far
	last    fyneui.SmokeFrame // the last one
	lastAt  time.Time
	errs    []string // the first smokeErrorsShown errors logged
	nErrs   int
	nQuit   int // Fyne's preferences load errors after the frame, from the quit (fynePrefsLoadError)
	// What the quit bound found when it passed (quitBoundPassed): Fyne's deadlock on its lifecycle
	// queue, or another hang of its teardown with the main goroutine's stack.
	quitDeadlock bool
	quitHung     string
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
	opts.Smoke = s.read
}

// read is fyneui.Options.Smoke: one read of the window's presented frame, the run's end when it
// holds a drawn window or there was none to read.
func (s *smokeRun) read(f fyneui.SmokeFrame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	s.last, s.lastAt = f, time.Now()
	switch {
	case f.Err != nil:
		s.noFrame = f.Err
	case f.Drawn:
		s.frame = fmt.Sprintf("a frame of %d×%d pixels after %s, read from %s", f.Width, f.Height,
			time.Since(s.begin).Round(time.Millisecond), f.From)
		s.watchdog.Reset(min(smokeQuitGrace, time.Until(s.begin.Add(s.bound))))
	}
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
	if s.frame != "" && strings.Contains(msg, fynePrefsLoadError) {
		s.nQuit++
		return
	}
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
	msg, code := s.verdict(code)
	fmt.Fprint(os.Stderr, msg)
	return code
}

// verdict is what end says and the exit code it returns.
func (s *smokeRun) verdict(code int) (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	why, note := s.failure(code), s.uncounted()
	if why == "" {
		return fmt.Sprintf("cogpit: smoke: passed: %s, quit after %s%s (home %s)\n", s.frame,
			time.Since(s.begin).Round(time.Millisecond), note, s.home), code
	}
	msg := fmt.Sprintf("cogpit: smoke: failed: %s%s (home %s)\n", why, note, s.home)
	if s.quitHung != "" {
		msg += "cogpit: smoke: the main goroutine, which runs Fyne's event loop:\n" + s.quitHung + "\n"
	}
	if code == 0 {
		code = 1
	}
	return msg, code
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
	case s.quitHung != "":
		return fmt.Sprintf("Fyne did not end its run within the quit bound of %s%s", quitBound, s.loggedErrors())
	case s.nErrs > 0:
		return strings.TrimPrefix(s.loggedErrors(), "; ")
	}
	return ""
}

// quitBoundPassed is the quit bound's report (quitHung): Fyne had not ended its run within it, and
// stacks are every goroutine's at that moment. Fyne's deadlock on its lifecycle queue is named and
// not counted; any other hang of its teardown fails the run, with the main goroutine's stack.
// Without a smoke run, nothing.
func (s *smokeRun) quitBoundPassed(stacks string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if fyneQuitDeadlock(stacks) {
		s.quitDeadlock = true
		return
	}
	s.quitHung = goroutine(stacks, 1)
}

// loggedErrors names the errors that reached the log, "" for none; s.mu held.
func (s *smokeRun) loggedErrors() string {
	if s.nErrs == 0 {
		return ""
	}
	return fmt.Sprintf("; errors logged (%d): %s", s.nErrs, strings.Join(s.errs, " | "))
}

// uncounted names Fyne's races of its stop the run did not count, "" for none; s.mu held.
func (s *smokeRun) uncounted() string {
	var note string
	if s.nQuit > 0 {
		note = fmt.Sprintf("; not counted: Fyne's %q while quitting (%d), its preferences save at the stop racing its file watch",
			fynePrefsLoadError, s.nQuit)
	}
	if s.quitDeadlock {
		note += fmt.Sprintf("; not counted: Fyne's shutdown deadlocked on its lifecycle queue, ended by the quit bound of %s",
			quitBound)
	}
	return note
}

// progress says how far the reads of the window's frame got, "" once one held a drawn window;
// s.mu held. No read at all: the main goroutine, which runs them between Fyne's frames, never got
// to one. Reads whose last is long ago: it stopped after that one.
func (s *smokeRun) progress() string {
	switch {
	case s.frame != "":
		return ""
	case s.reads == 0:
		return "; no read of the window's frame ran"
	}
	f := s.last
	what := fmt.Sprintf("%d×%d pixels of one colour #%02x%02x%02x%02x", f.Width, f.Height, f.Colour.R, f.Colour.G, f.Colour.B, f.Colour.A)
	switch {
	case f.Unread != "":
		what = f.Unread
	case f.Width == 0 || f.Height == 0:
		what = "no pixels, the canvas had no size"
	}
	reads := fmt.Sprintf("%d reads", s.reads)
	if s.reads == 1 {
		reads = "1 read"
	}
	return fmt.Sprintf("; %s of the window's frame from %s, the last %s ago: %s", reads, f.From,
		time.Since(s.lastAt).Round(time.Millisecond), what)
}

// expire is the watchdog: the run took longer than its bound, or than smokeQuitGrace to quit once
// its window drew a frame. It says so, with the main goroutine's stack, and ends the process with
// exit code 1.
func (s *smokeRun) expire() {
	fmt.Fprint(os.Stderr, s.expired(goroutineStacks()))
	os.Exit(1)
}

// expired is the watchdog's verdict, given every goroutine's stack. A hang of Fyne's own teardown
// ends sooner, through the quit bound (quitBoundPassed), so what the watchdog finds is a window
// without a frame or a quit Cogpit did not finish.
func (s *smokeRun) expired(stacks string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	what := fmt.Sprintf("the window drew no frame within %s%s", s.bound, s.progress())
	if s.frame != "" {
		what = fmt.Sprintf("the window drew %s, but Cogpit did not quit within %s of it", s.frame,
			time.Since(s.lastAt).Round(time.Second))
	}
	return fmt.Sprintf("cogpit: smoke: failed: %s%s%s (home %s)\ncogpit: smoke: the main goroutine, which runs Fyne's event loop:\n%s\n",
		what, s.loggedErrors(), s.uncounted(), s.home, goroutine(stacks, 1))
}

// goroutineStacks is every goroutine's stack, as Go prints them.
func goroutineStacks() string {
	buf := make([]byte, 1<<20)
	return string(buf[:runtime.Stack(buf, true)])
}

// goroutine is goroutine id's stack in stacks.
func goroutine(stacks string, id int) string {
	head := fmt.Sprintf("goroutine %d [", id)
	for g := range strings.SplitSeq(stacks, "\n\n") {
		if strings.HasPrefix(g, head) {
			return g
		}
	}
	return "(not found)"
}

// fyneQuitDeadlock reports whether stacks, every goroutine's, show Fyne's shutdown deadlocked on
// its own lifecycle queue: the main goroutine waits in Lifecycle.WaitForEvents for the queue to
// run its last event, and the queue's goroutine waits in runOnMainWithWait for an event it handed
// the main loop after the loop's last drain, which nothing runs any more (fyne.io/fyne/v2 2.8,
// internal/driver/glfw loop.go: runGL's drain and runOnMainWithWait's check of drained race).
func fyneQuitDeadlock(stacks string) bool {
	var mainWaits, queueWaits bool
	for g := range strings.SplitSeq(stacks, "\n\n") {
		switch {
		case strings.HasPrefix(g, "goroutine 1 ["):
			mainWaits = strings.Contains(g, "fyne.io/fyne/v2/internal/app.(*Lifecycle).WaitForEvents")
		case strings.Contains(g, "fyne.io/fyne/v2/internal/driver/glfw.runOnMainWithWait") &&
			strings.Contains(g, "fyne.io/fyne/v2/internal/app.(*Lifecycle).RunEventQueue"):
			queueWaits = true
		}
	}
	return mainWaits && queueWaits
}
