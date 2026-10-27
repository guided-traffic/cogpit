package testkit

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// The tests' credential plugin, on every platform: the test binary itself, which client-go starts
// as the program a kubeconfig's exec section names, and which Main makes the plugin when the
// section's environment sets PluginEnv. What a run does is set by the files in the plugin's folder
// (Plugin.Dir), which the test writes, and which each run reads when it starts.

// PluginEnv is the variable of a kubeconfig's exec section that makes a test binary the credential
// plugin; its value is the plugin's folder (Plugin.EnvYAML).
const PluginEnv = "COGPIT_TEST_PLUGIN"

// roleEnv marks the processes the testkit starts from a plugin's program itself: a run's stray
// child (roleStray) and the first start of a copy (roleWarm).
const (
	roleEnv   = "COGPIT_TEST_PLUGIN_ROLE"
	roleStray = "stray"
	roleWarm  = "warm"
)

// The files of a plugin's folder.
const (
	plugRuns   = "runs"   // a line per run, written by the plugin
	plugPIDs   = "pids"   // the number of each run's process, written by the plugin
	plugStdout = "stdout" // what a run prints to its standard output
	plugStderr = "stderr" // what a run prints to its standard error
	plugExit   = "exit"   // a run's exit code
	plugDelay  = "delay"  // how long a run waits before it answers
	plugBlock  = "block"  // a run prints its content to its standard error and waits until ended
	plugExec   = "exec"   // a run replaces itself with /bin/sleep 900 (not on Windows)
	plugStray  = "stray"  // a run leaves a child that holds its output, and exits
	plugHold   = "hold"   // the stray children run while it exists
	plugStrays = "strays" // the number of each stray child, written by the plugin
)

// The plugin's waits: how often a waiting run or stray child looks whether the test ended (its
// folder, or the hold file, went), and how long it waits at most, so that a test binary that
// crashed leaves no process behind for ever.
const (
	pluginPoll    = 50 * time.Millisecond
	pluginMaxWait = 15 * time.Minute
)

// DefaultBlockText is what a run that waits prints to its standard error (Plugin.BlockText).
const DefaultBlockText = "Log in in your browser to continue\n"

var mainRan atomic.Bool

// Main runs m's tests and exits with their result. In a test binary that a test started as its
// credential plugin (PluginEnv set), it is the plugin instead, and exits without the exit hooks: a
// coverage binary's print a warning to the standard error, and the race detector's wait a second.
// A package whose tests use Plugin calls it from its TestMain.
func Main(m *testing.M) {
	switch dir := os.Getenv(PluginEnv); {
	case os.Getenv(roleEnv) == roleWarm:
		syscall.Exit(0)
	case os.Getenv(roleEnv) == roleStray:
		waitWhile(filepath.Join(dir, plugHold))
		syscall.Exit(0)
	case dir != "":
		syscall.Exit(runPlugin(dir))
	}
	mainRan.Store(true)
	code := m.Run()
	removePrograms()
	os.Exit(code)
}

// Plugin is a credential plugin for tests: the test binary under a name the test gives (Cogpit
// tells a plugin by its program's name, Windows' search for its process by nothing else). A run
// notes itself (Runs, LastPID), then waits while Block is on, replaces itself while Exec is on,
// leaves a stray child while Stray is on, and otherwise waits for Delay and prints what Give or
// Output set. Its files go with the test, and a waiting run or stray child ends 50 ms after them.
type Plugin struct {
	t         testing.TB
	Command   string // the program as a kubeconfig names it: Path without Windows' .exe (PATHEXT)
	Path      string // the program's file
	Dir       string // the plugin's files: PluginEnv's value
	BlockText string // what a run that waits prints to its standard error; DefaultBlockText
}

// NewPlugin makes a plugin whose program is named name, in a folder of its own (Windows: one per
// test process, shared by the plugins of a name). The package's TestMain must call Main.
func NewPlugin(t testing.TB, name string) *Plugin {
	t.Helper()
	p := newPlugin(t)
	p.Command = programNamed(t, name)
	p.Path = p.Command + exeSuffix
	return p
}

// NewPluginAt makes a plugin whose program is at command (with Windows' .exe added), for a test
// that needs it in a folder of its own choosing: the kubeconfig's, which a relative command names.
func NewPluginAt(t testing.TB, command string) *Plugin {
	t.Helper()
	p := newPlugin(t)
	if err := os.MkdirAll(filepath.Dir(command), 0o700); err != nil {
		t.Fatal(err)
	}
	p.Command, p.Path = command, command+exeSuffix
	programAt(t, p.Path)
	return p
}

func newPlugin(t testing.TB) *Plugin {
	t.Helper()
	if !mainRan.Load() {
		t.Fatal("testkit.Plugin: the package's TestMain must call testkit.Main, which makes the test binary the plugin")
	}
	// A folder of its own: PluginEnv's value is a known secret, scrubbed wherever it appears, so it
	// is part of no other path a test reads (the program's, a kubeconfig's).
	return &Plugin{t: t, Dir: t.TempDir(), BlockText: DefaultBlockText}
}

// EnvYAML is PluginEnv's entry in a kubeconfig exec section's env list, each line after indent.
func (p *Plugin) EnvYAML(indent string) string {
	return indent + "- name: " + PluginEnv + "\n" + indent + "  value: " + YAMLQuote(p.Dir) + "\n"
}

// YAMLQuote quotes s as a single-quoted YAML scalar: a Windows path's backslashes stay as they are.
func YAMLQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// Give makes the plugin hand out an ExecCredential with token, expiring at exp (zero: no expiry).
func (p *Plugin) Give(token string, exp time.Time) {
	p.t.Helper()
	status := `"token":"` + token + `"`
	if !exp.IsZero() {
		status += `,"expirationTimestamp":"` + exp.UTC().Format(time.RFC3339) + `"`
	}
	p.write(plugStdout, `{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","status":{`+status+`}}`+"\n")
}

// Output makes the plugin print stdout and stderr and exit with code.
func (p *Plugin) Output(stdout, stderr string, code int) {
	p.t.Helper()
	p.write(plugStdout, stdout)
	p.write(plugStderr, stderr)
	p.write(plugExit, strconv.Itoa(code))
}

// Delay makes the plugin wait d before it answers; zero: not at all.
func (p *Plugin) Delay(d time.Duration) {
	p.t.Helper()
	p.mode(plugDelay, d > 0, d.String())
}

// Block makes the plugin's next runs print BlockText to their standard error and wait until they
// are ended, as a browser login nobody finishes does; off, the next runs answer again.
func (p *Plugin) Block(on bool) { p.t.Helper(); p.mode(plugBlock, on, p.BlockText) }

// Exec makes the plugin's next runs replace themselves with /bin/sleep 900, as a wrapper does that
// execs the login program: the process keeps its number, its command line becomes sleep's. Not on
// Windows, which has no exec.
func (p *Plugin) Exec(on bool) { p.t.Helper(); p.mode(plugExec, on, "") }

// Stray makes the plugin's next runs leave a child that holds the run's output open, and exit at
// once: client-go's run goes on without a process of Cogpit's to end (H-118). Off, the next runs
// do not, and the children end within 50 ms (as at the test's end), which ends their runs.
func (p *Plugin) Stray(on bool) {
	p.t.Helper()
	if on {
		p.mode(plugHold, true, "")
		p.mode(plugStray, true, "")
		return
	}
	p.mode(plugStray, false, "")
	p.mode(plugHold, false, "")
}

// Runs is how often the plugin ran.
func (p *Plugin) Runs() int { return len(p.lines(plugRuns)) }

// PIDs are the numbers of the plugin's processes, oldest first.
func (p *Plugin) PIDs() []int { return p.numbers(plugPIDs) }

// LastPID is the number of the plugin's latest process; 0 before its first run.
func (p *Plugin) LastPID() int {
	pids := p.PIDs()
	if len(pids) == 0 {
		return 0
	}
	return pids[len(pids)-1]
}

// Strays are the numbers of the stray children the plugin left, oldest first.
func (p *Plugin) Strays() []int { return p.numbers(plugStrays) }

// numbers are the process numbers the plugin noted in its file name, oldest first.
func (p *Plugin) numbers(name string) []int {
	var out []int
	for _, l := range p.lines(name) {
		if n, err := strconv.Atoi(l); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// lines are the lines the plugin noted in its file name.
func (p *Plugin) lines(name string) []string {
	data, err := os.ReadFile(filepath.Join(p.Dir, name))
	if err != nil {
		return nil
	}
	return strings.Fields(string(data))
}

func (p *Plugin) write(name, content string) {
	p.t.Helper()
	if err := os.WriteFile(filepath.Join(p.Dir, name), []byte(content), 0o600); err != nil {
		p.t.Fatal(err)
	}
}

// mode switches the plugin's file name on, with content, or off.
func (p *Plugin) mode(name string, on bool, content string) {
	p.t.Helper()
	if on {
		p.write(name, content)
		return
	}
	if err := os.Remove(filepath.Join(p.Dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		p.t.Fatal(err)
	}
}

// runPlugin is one run of the plugin whose files are in dir; it returns the run's exit code.
func runPlugin(dir string) int {
	note(dir, plugPIDs, strconv.Itoa(os.Getpid()))
	note(dir, plugRuns, "run")
	if text, ok := readPluginFile(dir, plugBlock); ok {
		_, _ = os.Stderr.WriteString(text)
		waitWhile(dir)
		return 1
	}
	if _, ok := readPluginFile(dir, plugExec); ok {
		err := syscall.Exec("/bin/sleep", []string{"/bin/sleep", "900"}, os.Environ())
		_, _ = fmt.Fprintln(os.Stderr, "exec:", err)
		return 1
	}
	if _, ok := readPluginFile(dir, plugStray); ok {
		return leaveStray(dir)
	}
	if text, ok := readPluginFile(dir, plugDelay); ok {
		if d, err := time.ParseDuration(text); err == nil {
			time.Sleep(d)
		}
	}
	if text, ok := readPluginFile(dir, plugStderr); ok {
		_, _ = os.Stderr.WriteString(text)
	}
	if text, ok := readPluginFile(dir, plugStdout); ok {
		_, _ = os.Stdout.WriteString(text)
	}
	if text, ok := readPluginFile(dir, plugExit); ok {
		if code, err := strconv.Atoi(text); err == nil {
			return code
		}
	}
	return 0
}

// leaveStray starts the program again as a child that holds the run's standard output and error
// open while the plugin's hold file exists, notes its number, and returns the run's exit code.
func leaveStray(dir string) int {
	self, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), roleEnv+"="+roleStray)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	note(dir, plugStrays, strconv.Itoa(cmd.Process.Pid))
	return 0
}

// waitWhile waits while path exists, at most pluginMaxWait.
func waitWhile(path string) {
	for end := time.Now().Add(pluginMaxWait); time.Now().Before(end); time.Sleep(pluginPoll) {
		if _, err := os.Stat(path); err != nil {
			return
		}
	}
}

// note appends line to the plugin's file name.
func note(dir, name, line string) {
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line + "\n")
	_ = f.Close()
}

// readPluginFile is the content of the plugin's file name, trimmed of its spaces where it is a
// setting (delay, exit); ok is false when the file does not exist.
func readPluginFile(dir, name string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", false
	}
	if name == plugDelay || name == plugExit {
		return strings.TrimSpace(string(data)), true
	}
	return string(data), true
}
