//go:build linux

package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/guided-traffic/cogpit/internal/buildinfo"
)

// On Linux Cogpit updates through apt (D-108; docs/security/updates.md#linux). The package cogpit
// sets up Cogpit's signed APT repository (/etc/apt/sources.list.d/cogpit.sources, its key inline),
// so apt, the Software Updater and the user's own unattended-upgrades settings see its new
// releases, and apt checks every file against the repository's signature before it installs it.
// The app only reads what apt knows and asks it to act:
//
//   - A check reads `apt-cache policy cogpit`: the version installed and the newest one in the
//     package lists the system last fetched (apt's own daily refresh). No network, no root; at
//     Start and every hour while the preference Check for updates automatically is on.
//   - A newer one than installed: AvailableUpdate names it, and the window's Update button runs
//     CheckForUpdates, which runs the package's helper /usr/libexec/cogpit/update through pkexec:
//     polkit asks for an administrator's password, the helper refreshes the list of Cogpit's
//     repository and upgrades the package cogpit, nothing else. Help › Check for Updates… does the
//     same, since a fresh list needs root.
//   - Installed newer than this process runs: apt replaced the files under the running app (the
//     helper, the Software Updater or unattended-upgrades). RestartPending says so, the button
//     reads Restart, and CheckForUpdates restarts Cogpit into it: a detached shell waits for this
//     process to end and starts /usr/bin/cogpit, and the process quits as SIGTERM makes it (the
//     window closes, the caller's cleanup runs). Installing never restarts by itself: the user's
//     second click does (the lead's choice, 2026-10-20).
//
// What an update the user started came to (installed, already the newest, cancelled, failed) goes
// to OnUpdateMessage's listeners, the window's notifications.

// The package's files, and what runs them; variables for the tests.
var (
	debExecutable = "/usr/bin/cogpit"
	debSources    = "/etc/apt/sources.list.d/cogpit.sources"
	debHelper     = "/usr/libexec/cogpit/update"
	// checkInterval is how often the scheduled checks read apt's lists.
	checkInterval = time.Hour
	// installTimeout bounds the helper: the password prompt, the refresh and the download.
	installTimeout = 20 * time.Minute
	// readTimeout bounds a read of apt's state.
	readTimeout = 30 * time.Second

	executable     = os.Executable
	runningVersion = func() (string, bool) { i := buildinfo.Read(); return i.Version, i.Release }
	runCommand     = runLocale
	startRestart   = startDetached
	quitSelf       = func() error { return syscall.Kill(os.Getpid(), syscall.SIGTERM) }
)

// The user's click installs or restarts here: only the window runs it (engine.UpdateActor).
func init() { checkActs = true }

// apt is the Linux updater's state: one per process, as the notice is.
var apt aptState

type aptState struct {
	mu      sync.Mutex
	running string        // the version this process runs
	stop    chan struct{} // closed to end the scheduled checks; nil while they are off
	busy    bool          // a helper or restart the user started runs
}

// load checks that this process is the installed package's app and its repository is set up.
func load() error {
	exe, err := executable()
	if err != nil {
		return ErrNotFromPackage
	}
	exe = strings.TrimSuffix(exe, " (deleted)") // the package replaced it since the start
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	version, release := runningVersion()
	if exe != debExecutable || !release {
		return ErrNotFromPackage
	}
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	out, err := runCommand(ctx, "dpkg-query", "--show", "--showformat=${Status}", "cogpit")
	if err != nil || strings.TrimSpace(string(out)) != "install ok installed" {
		return ErrNotFromPackage
	}
	if _, err := os.Stat(debSources); err != nil {
		return ErrNoRepository
	}
	apt.mu.Lock()
	apt.running = version
	apt.mu.Unlock()
	return nil
}

func startFailure() error { return nil }

// start begins the scheduled checks when they are on: one at once, then every checkInterval.
func start(automaticChecks bool) { setAutomaticChecks(automaticChecks) }

func setAutomaticChecks(on bool) {
	apt.mu.Lock()
	defer apt.mu.Unlock()
	switch {
	case on && apt.stop == nil:
		apt.stop = make(chan struct{})
		go scheduledChecks(apt.stop)
	case !on && apt.stop != nil:
		close(apt.stop)
		apt.stop = nil
	}
}

// scheduledChecks reads apt's state at once and every checkInterval until stop closes.
func scheduledChecks(stop <-chan struct{}) {
	defer func() { _ = recover() }() // D-11: a check's panic never ends the app
	t := time.NewTicker(checkInterval)
	defer t.Stop()
	for {
		_, _ = refreshNotice()
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

// aptVersions is what apt knows of the package: Installed and Candidate of apt-cache policy, ""
// for "(none)".
type aptVersions struct{ installed, candidate string }

// readVersions runs apt-cache policy cogpit.
func readVersions() (aptVersions, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	out, err := runCommand(ctx, "apt-cache", "policy", "cogpit")
	if err != nil {
		return aptVersions{}, fmt.Errorf("apt-cache policy cogpit: %w", err)
	}
	return parsePolicy(out), nil
}

// parsePolicy reads apt-cache policy's Installed and Candidate lines (in the C locale).
func parsePolicy(out []byte) aptVersions {
	var v aptVersions
	for line := range strings.SplitSeq(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "(none)" {
			value = ""
		}
		switch key {
		case "Installed":
			v.installed = value
		case "Candidate":
			v.candidate = value
		}
	}
	return v
}

// refreshNotice reads apt's state into the notice: the installed release when it is newer than
// this process (restart), else the candidate when it is newer than the installed one and this
// process.
func refreshNotice() (aptVersions, error) {
	v, err := readVersions()
	if err != nil {
		return v, err
	}
	apt.mu.Lock()
	running := apt.running
	apt.mu.Unlock()
	switch {
	case v.installed != "" && compareVersions(v.installed, running) > 0:
		notice.setState(true, v.installed, true)
	case v.candidate != "" && v.installed != "" && compareVersions(v.candidate, v.installed) > 0 &&
		compareVersions(v.candidate, running) > 0:
		notice.setState(true, v.candidate, false)
	default:
		notice.setState(false, "", false)
	}
	return v, nil
}

// check is the user's click (the Update button, Help › Check for Updates…): a restart into the
// installed release when there is one, else the helper. It does not block; one runs at a time.
func check() {
	apt.mu.Lock()
	if apt.busy {
		apt.mu.Unlock()
		messages.send(LevelInfo, "Cogpit's update is already running")
		return
	}
	apt.busy = true
	apt.mu.Unlock()
	go func() {
		defer func() {
			apt.mu.Lock()
			apt.busy = false
			apt.mu.Unlock()
			if r := recover(); r != nil { // D-11: never ends the app
				messages.send(LevelError, fmt.Sprintf("Cogpit's update failed: internal error (%v)", r))
			}
		}()
		if _, err := refreshNotice(); err == nil && notice.restartPending() {
			restart()
			return
		}
		install()
	}()
}

// install runs the helper through pkexec, then reads apt's state again and says what came of it.
func install() {
	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	out, err := runCommand(ctx, "pkexec", debHelper)
	if err != nil {
		messages.send(installFailure(err, out))
		return
	}
	v, err := refreshNotice()
	switch {
	case err != nil:
		messages.send(LevelWarning, "Cogpit's update ran, but its result could not be read: "+err.Error())
	case notice.restartPending():
		messages.send(LevelInfo, "Cogpit "+v.installed+" is installed: click Restart to Update to use it")
	default:
		messages.send(LevelInfo, "Cogpit "+v.installed+" is the newest release in Cogpit's package repository")
	}
}

// installFailure says why the helper did not install: pkexec's own exit codes (126: the password
// dialog dismissed, 127: not authorized or no polkit agent), the helper's 3 (no repository), or
// apt's last line.
func installFailure(err error, out []byte) (level, message string) {
	const manual = "; update Cogpit in a terminal: sudo apt update && sudo apt install --only-upgrade cogpit"
	if errors.Is(err, exec.ErrNotFound) {
		return LevelError, "Cogpit cannot update itself: pkexec is not installed" + manual
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return LevelError, "Cogpit's update did not finish within " + installTimeout.String() + manual
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		switch exit.ExitCode() {
		case 126:
			return LevelInfo, "Cogpit's update was cancelled: no password was given"
		case 127:
			return LevelError, "Cogpit's update was not authorized (no polkit agent asked for a password)" + manual
		case 3:
			return LevelError, "Cogpit's update failed: " + ErrNoRepository.Error()
		}
	}
	return LevelError, "Cogpit's update failed: " + lastLine(out, err) + manual
}

// lastLine is the output's last line that says something, cut to 300 bytes; err without one.
func lastLine(out []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, l := range slices.Backward(lines) {
		if l = strings.TrimSpace(l); l != "" {
			if len(l) > 300 {
				l = l[:300] + "…"
			}
			return l
		}
	}
	return err.Error()
}

// restart starts the installed release once this process has ended, then quits.
func restart() {
	if err := startRestart(os.Getpid(), debExecutable, os.Args[1:]); err != nil {
		messages.send(LevelError, "Cogpit could not restart itself: "+err.Error()+"; quit Cogpit and start it again")
		return
	}
	if err := quitSelf(); err != nil {
		messages.send(LevelError, "Cogpit could not quit for its restart: "+err.Error())
	}
}

// restartScript waits up to two minutes for the process $1 to end, then starts the rest of its
// arguments in its place.
const restartScript = `pid=$1; shift; n=0
while kill -0 "$pid" 2>/dev/null && [ "$n" -lt 600 ]; do sleep 0.2; n=$((n + 1)); done
exec "$@"`

// startDetached starts the shell that starts exe with args once pid has ended, in a session of its
// own, so that it outlives this process.
func startDetached(pid int, exe string, args []string) error {
	cmd := exec.Command("/bin/sh", append([]string{"-c", restartScript, "cogpit-restart", strconv.Itoa(pid), exe}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// runLocale runs a command with its messages in the C locale, so that apt's words parse, and
// returns its standard output and error together.
func runLocale(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANGUAGE=")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return out.Bytes(), err
}

// send tells every listener, each on a goroutine of its own.
func (m *updateMessages) send(level, message string) {
	m.mu.Lock()
	fns := slices.Clone(m.listeners)
	m.mu.Unlock()
	for _, fn := range fns {
		go fn(level, message)
	}
}
