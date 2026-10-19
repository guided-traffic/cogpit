package cluster

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// The process of a credential plugin's run, for ending it (loginrun.go, D-73): client-go starts it
// with exec.Command and gives Cogpit no handle on it (no *exec.Cmd, no context, no SysProcAttr), so
// Cogpit finds it among its own child processes (pluginproc_*.go: macOS through proc_info, Linux
// through /proc, Windows through a Toolhelp snapshot).
//
// It is found at the run's begin and followed by its number and start time from then on (WF38, the
// lead's walk of 2026-10-19): a plugin that is a wrapper and replaces itself with the real login
// program (exec) keeps its number while its command line becomes the program's, so a search by the
// command line at the cancel, minutes later, did not find it; the process ran on while the log said
// Cogpit had ended it. The wrapper of client-go's interactiveFunc (loginhold.go) notes Cogpit's
// children just before client-go starts the plugin (pluginHold.begin); for searchWindow a goroutine
// looks at each child that is new since, then decides which one is the run's (decideProcess):
//
//   - the one new child seen running the plugin's command line (childProcess.runs), before its exec
//     or without one;
//   - else the one new child in Cogpit's own process group (client-go starts the plugin there; a
//     terminal's shell runs in a session of its own), when no other plugin run was under way within
//     the window before or after this one's begin: two runs that begin together could take each
//     other's process;
//   - else none.
//
// A cancel or the bound then signals that process if it still runs with the same start time (a
// number the system gave to another process meanwhile is not hit); without one, a child that is new
// since the run's begin and runs the plugin's command line (one that appeared after the window).
// With neither, Cogpit stops waiting for the run and says so: its process may run on (H-118).

// searchWindow is how long after a run's begin its process is looked for among Cogpit's new
// children: client-go starts it at once, so a child that appears later is not taken for it.
const searchWindow = 500 * time.Millisecond

// The search looks every searchFastPause for searchFastFor, to see a wrapper's own command line
// before it replaces itself, then every searchPause.
const (
	searchFastFor   = 50 * time.Millisecond
	searchFastPause = time.Millisecond
	searchPause     = 10 * time.Millisecond
)

// goneWait is how long a cancel waits for a run whose process the search found but that no longer
// runs: it ended a moment ago, and client-go is about to return.
const goneWait = 200 * time.Millisecond

// Why Cogpit could not end a run's process (LoginEndedError).
const (
	whyNotFound   = "its process was not found"
	whyNotAllowed = "its process could not be signalled"
)

// procID identifies a process across an exec: its number and when it started (fork time: µs since
// the epoch on macOS, clock ticks since boot on Linux, a FILETIME on Windows).
type procID struct {
	pid   int
	start int64
}

// childProcess is one of Cogpit's child processes.
type childProcess struct {
	id    procID
	group bool     // in Cogpit's own process group
	argv  []string // its command line; nil where it was not read or the system does not tell it
	exe   string   // its program's file name (Windows)
}

// runs reports whether the process runs command: its arguments last, after the program (argv[0], or
// a script's path after its interpreter, as the kernel starts a #! script); by the program's file
// name where the command line is not known.
func (c childProcess) runs(command []string) bool {
	if len(command) == 0 {
		return false
	}
	prog := filepath.Base(command[0])
	if c.argv == nil {
		exe := strings.TrimSuffix(strings.ToLower(c.exe), ".exe")
		return exe != "" && exe == strings.TrimSuffix(strings.ToLower(prog), ".exe")
	}
	args := command[1:]
	if len(c.argv) < len(command) || !slices.Equal(c.argv[len(c.argv)-len(args):], args) {
		return false
	}
	ran := c.argv[len(c.argv)-len(args)-1]
	if filepath.IsAbs(command[0]) && strings.ContainsRune(ran, filepath.Separator) {
		// A program the kubeconfig names by its path runs under that path, a script's too (the
		// kernel passes it to the interpreter as given): a program of the same name in another
		// folder, a plugin of another context, is not it.
		return filepath.Clean(ran) == filepath.Clean(command[0])
	}
	return ran == command[0] || filepath.Base(ran) == prog // a name looked up in PATH; a script's resolved path
}

// childSet is Cogpit's child processes now, by identity.
func childSet() map[procID]bool {
	out := map[procID]bool{}
	for _, c := range listChildren() {
		out[c.id] = true
	}
	return out
}

// alive reports whether id is still one of Cogpit's child processes: the same number, started at
// the same time.
func alive(id procID) bool {
	for _, c := range listChildren() {
		if c.id == id {
			return true
		}
	}
	return false
}

// findPlugin returns the one child of Cogpit's that is not in before and runs command, and how many
// do (two runs of the same command line from different plugin configurations are not told apart).
func findPlugin(command []string, before map[procID]bool) (procID, int) {
	var found procID
	n := 0
	for _, c := range listChildren() {
		if before[c.id] {
			continue
		}
		c.argv = commandLine(c.id.pid)
		if c.runs(command) {
			found = c.id
			n++
		}
	}
	return found, n
}

// procSearch looks for a run's process among the children Cogpit starts after the run's begin.
type procSearch struct {
	at      time.Time
	command []string
	before  map[procID]bool // Cogpit's children at the run's begin
	done    chan struct{}   // closed when the search decided, or stopped because the run ended
	found   procID          // the run's process; zero for none; written before done closes
	ended   time.Time       // when the run ended (searches.mu)
}

// searches are the process searches of the runs that began lately, of every Authenticator.
var searches struct {
	mu     sync.Mutex
	recent []*procSearch
}

// newProcSearch notes a run that begins at at with Cogpit's children before (childSet, taken before
// client-go starts the plugin). The caller starts run.
func newProcSearch(at time.Time, command []string, before map[procID]bool) *procSearch {
	s := &procSearch{at: at, command: command, before: before, done: make(chan struct{})}
	searches.mu.Lock()
	defer searches.mu.Unlock()
	keep := searches.recent[:0]
	for _, o := range searches.recent {
		if at.Sub(o.at) < 3*searchWindow {
			keep = append(keep, o)
		}
	}
	searches.recent = append(keep, s)
	return s
}

// finished notes that the search's run ended at t: a run that ended before another began started
// no process that could be taken for the other's.
func (s *procSearch) finished(t time.Time) {
	if s == nil {
		return
	}
	searches.mu.Lock()
	defer searches.mu.Unlock()
	if s.ended.IsZero() {
		s.ended = t
	}
}

// neighbours reports whether another plugin run was under way within searchWindow of s's begin,
// and whether one of them runs the same command line.
func (s *procSearch) neighbours() (near, same bool) {
	searches.mu.Lock()
	defer searches.mu.Unlock()
	for _, o := range searches.recent {
		if o == s || (!o.ended.IsZero() && !o.ended.After(s.at)) {
			continue
		}
		if d := s.at.Sub(o.at); d > -searchWindow && d < searchWindow {
			near = true
			same = same || slices.Equal(o.command, s.command)
		}
	}
	return near, same
}

// sighting is what the search saw of a new child.
type sighting struct {
	group   bool // in Cogpit's process group
	matched bool // seen running the plugin's command line
}

// run looks at Cogpit's new children until searchWindow after the run's begin, then decides; it
// stops without a decision when stop closes (the run ended).
func (s *procSearch) run(stop <-chan struct{}) {
	defer close(s.done)
	defer func() {
		if r := recover(); r != nil { // a crash is a bug, not a reason to end Cogpit (D-11)
			slog.Error("cluster: looking for a credential plugin's process failed", "panic", fmt.Sprint(r))
		}
	}()
	seen := map[procID]*sighting{}
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		s.look(seen)
		since := time.Since(s.at)
		if since >= searchWindow {
			break
		}
		pause := searchPause
		if since < searchFastFor {
			pause = searchFastPause
		}
		t.Reset(min(pause, searchWindow-since))
	}
	near, same := s.neighbours()
	s.found = decideProcess(seen, near, same)
}

// look notes each child that is new since the run's begin, and whether it runs the plugin's command
// line; a child's command line is read until it does (a fork shows Cogpit's own until its exec).
func (s *procSearch) look(seen map[procID]*sighting) {
	for _, c := range listChildren() {
		if s.before[c.id] {
			continue
		}
		o := seen[c.id]
		if o == nil {
			o = &sighting{group: c.group}
			seen[c.id] = o
		}
		if !o.matched {
			c.argv = commandLine(c.id.pid)
			o.matched = c.runs(s.command)
		}
	}
}

// decideProcess picks the run's process among the new children seen: the one seen running the
// plugin's command line, unless another run of the same command line was under way alongside; else
// the one in Cogpit's process group, unless any other run was; else none (zero).
func decideProcess(seen map[procID]*sighting, near, same bool) procID {
	var matched, grouped []procID
	for id, o := range seen {
		if o.matched {
			matched = append(matched, id)
		}
		if o.group {
			grouped = append(grouped, id)
		}
	}
	switch {
	case len(matched) == 1 && !same:
		return matched[0]
	case len(matched) == 0 && len(grouped) == 1 && !near:
		return grouped[0]
	}
	return procID{}
}

// locate returns the process to end for a run: the one the search found at its begin, if it still
// runs; else the one child of Cogpit's that is new since the begin and runs the plugin's command
// line. why is empty when it found one that Cogpit may signal, and says why not otherwise; both are
// empty when the run ended meanwhile (stop closed). It waits for the search's decision, at most
// until searchWindow after the run's begin.
func (s *procSearch) locate(stop <-chan struct{}) (target procID, why string) {
	select {
	case <-s.done:
	case <-stop:
		return procID{}, ""
	}
	target = s.found
	if target != (procID{}) && !alive(target) {
		select { // it ended a moment ago: the run ends with it
		case <-stop:
			return procID{}, ""
		case <-time.After(goneWait):
		}
		target = procID{}
	}
	if target == (procID{}) {
		found, n := findPlugin(s.command, s.before)
		if n != 1 {
			return procID{}, whyNotFound
		}
		target = found
	}
	if !canSignal(target.pid) {
		return procID{}, whyNotAllowed
	}
	return target, ""
}

// terminate ends run's process target: SIGTERM, and SIGKILL after killGrace if the run has not
// ended and target still runs (TerminateProcess twice on Windows).
func terminate(run *pluginRun, target procID) {
	defer func() {
		if r := recover(); r != nil { // a crash is a bug, not a reason to end Cogpit (D-11)
			slog.Error("cluster: ending a credential plugin failed", "program", run.Program, "panic", fmt.Sprint(r))
		}
	}()
	if err := signalPlugin(target.pid, false); err != nil {
		slog.Warn("cluster: cannot end the credential plugin", "program", run.Program, "pid", target.pid, "err", err)
	}
	t := time.NewTimer(killGrace)
	defer t.Stop()
	select {
	case <-run.done:
		return
	case <-t.C:
	}
	if !alive(target) { // it ended, or the number is another process's now
		return
	}
	slog.Warn("cluster: the credential plugin did not end on SIGTERM; killing it", "program", run.Program, "pid", target.pid)
	if err := signalPlugin(target.pid, true); err != nil {
		slog.Warn("cluster: cannot kill the credential plugin", "program", run.Program, "pid", target.pid, "err", err)
	}
}
