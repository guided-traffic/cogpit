//go:build !windows

package terminal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const (
	// closeGrace is how long a shell has to exit after SIGHUP before its process group is killed.
	closeGrace = 2 * time.Second
	// drainAfterExit is how long output is still read after the shell exited. A background
	// process can keep the PTY open after its shell is gone; the session ends regardless.
	drainAfterExit = 500 * time.Millisecond
)

// localSession is a shell on a PTY that Cogpit started itself.
type localSession struct {
	pty     *os.File // the PTY master, non-blocking so that Close unblocks a pending Read
	cmd     *exec.Cmd
	pid     int // the shell's pid, which is also its process group id (it runs in its own session)
	cfgPath string
	ownDir  string // the session's own directory (the kubectl link, zsh's .zshenv); "" without
	input   *inputQueue
	done    chan struct{}
	// exited is set as soon as the shell is reaped. From then on its pid may belong to another
	// process, so nothing signals it any more.
	exited atomic.Bool

	closeOnce  sync.Once
	closeErr   error
	removeOnce sync.Once
}

// fallbackShells are tried in order when neither the options nor $SHELL name a shell: macOS's
// default first, then the shells every Unix has.
var fallbackShells = []string{"/bin/zsh", "/bin/bash", "/bin/sh"}

// defaultShell is the first fallback shell that exists, or /bin/sh when none does (the start then
// reports it missing).
func defaultShell(exists func(string) bool) string {
	for _, s := range fallbackShells {
		if exists(s) {
			return s
		}
	}
	return "/bin/sh"
}

// DefaultShell is the shell a local terminal runs when the preference names none, as Preferences ›
// Terminal shows it: $SHELL, else the first fallback shell that exists.
func DefaultShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return defaultShell(isExecutable)
}

// linkKubectl puts kubectl at link for kubectlFirst: a symbolic link.
func linkKubectl(kubectl, link string) error {
	return os.Symlink(kubectl, link)
}

// isExecutable reports whether path is a regular file someone may execute.
func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0
}

func startLocal(opts LocalOptions) (Session, error) {
	binding, err := resolveLocal(opts)
	if err != nil {
		return nil, err
	}

	shell := opts.Shell
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		shell = defaultShell(isExecutable)
	}
	shellPath, err := exec.LookPath(shell)
	if err != nil {
		return nil, fmt.Errorf("local terminal: shell %q: %w", shell, err)
	}
	dir := opts.Dir
	if dir == "" {
		if dir, err = os.UserHomeDir(); err != nil {
			return nil, fmt.Errorf("local terminal: no working directory given and no home directory: %w", err)
		}
	}

	cfgPath, err := writeTerminalConfig(binding.context)
	if err != nil {
		return nil, err
	}
	var ownDir string // the session's own directory for opts.Kubectl (kubectl.go)
	start := kubectlStart{login: true}
	if opts.Kubectl != "" {
		if ownDir, start, err = kubectlFirst(opts.Kubectl, shellPath, baseEnv(opts.Env), opts.EnforceKubectl); err != nil {
			_ = os.Remove(cfgPath)
			return nil, err
		}
	}
	removeConfig := func() {
		_ = os.Remove(cfgPath)
		if ownDir != "" {
			_ = os.RemoveAll(ownDir)
		}
	}

	// A leading "-" in argv[0] makes every common shell (sh, bash, zsh, fish, dash) a login
	// shell, which reads the user's profile as a terminal app's shell does; a bash whose kubectl
	// Cogpit chose reads the profile through its --init-file instead (kubectl.go).
	argv0 := filepath.Base(shellPath)
	if start.login {
		argv0 = "-" + argv0
	}
	args := append([]string{argv0}, start.args...)
	cmd := &exec.Cmd{
		Path: shellPath,
		Args: append(args, opts.Args...),
		Dir:  dir,
		Env:  localEnv(opts.Env, binding.kubeconfigVar(cfgPath), start.env...),
	}
	// pty.Start runs the shell in a new session (Setsid) with the PTY as its controlling
	// terminal, so the shell leads its own process group and Close can signal the group.
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		removeConfig()
		return nil, fmt.Errorf("local terminal: starting %s: %w", shellPath, err)
	}
	nb, err := nonBlocking(master)
	if err != nil {
		_ = master.Close()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
		removeConfig()
		return nil, fmt.Errorf("local terminal: %w", err)
	}

	s := &localSession{
		pty:     nb,
		cmd:     cmd,
		pid:     cmd.Process.Pid,
		cfgPath: cfgPath,
		ownDir:  ownDir,
		input:   newInputQueue(),
		done:    make(chan struct{}),
	}
	go s.pumpInput()
	go s.wait()
	return s, nil
}

// nonBlocking returns a close-on-exec, non-blocking duplicate of the PTY master and closes the
// original. creack/pty opens the master in blocking mode, where Close cannot interrupt a Read
// that waits for output; Go's poller can. Close-on-exec keeps the master out of other shells
// Cogpit starts later.
func nonBlocking(master *os.File) (*os.File, error) {
	syscall.ForkLock.RLock()
	fd, err := syscall.Dup(int(master.Fd()))
	if err == nil {
		syscall.CloseOnExec(fd)
	}
	syscall.ForkLock.RUnlock()
	_ = master.Close()
	if err != nil {
		return nil, fmt.Errorf("duplicating the PTY: %w", err)
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("setting the PTY non-blocking: %w", err)
	}
	return os.NewFile(uintptr(fd), "/dev/ptmx"), nil
}

// pumpInput delivers queued keystrokes to the PTY.
func (s *localSession) pumpInput() {
	defer recoverTo("local shell input", nil)
	_, _ = io.Copy(s.pty, s.input)
}

// wait reaps the shell, then lets remaining output drain for a moment and ends the session.
func (s *localSession) wait() {
	defer close(s.done)
	defer recoverTo("local shell wait", nil)
	_ = s.cmd.Wait()
	s.exited.Store(true)
	_ = s.input.Close()
	s.removeConfig()
	// Normally the PTY reports end of file as soon as the shell's output is read. A deadline
	// covers a background process that still holds the PTY open.
	_ = s.pty.SetReadDeadline(time.Now().Add(drainAfterExit))
}

// Read returns the shell's output; io.EOF once the shell has exited and its output is read.
func (s *localSession) Read(p []byte) (int, error) {
	n, err := s.pty.Read(p)
	if err != nil {
		// EIO (Linux, once the shell side is closed), a closed file and the drain deadline all
		// mean the same thing to the reader: the session is over.
		if errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed) || errors.Is(err, os.ErrDeadlineExceeded) {
			err = io.EOF
		}
	}
	return n, err
}

// Write queues keystrokes for the shell; it does not block.
func (s *localSession) Write(p []byte) (int, error) {
	return s.input.Write(p)
}

// Resize sets the PTY's window size; the kernel tells the foreground program with SIGWINCH.
func (s *localSession) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return fmt.Errorf("local terminal: invalid size %dx%d", cols, rows)
	}
	return pty.Setsize(s.pty, &pty.Winsize{Cols: cols, Rows: rows})
}

// Done is closed when the shell has exited.
func (s *localSession) Done() <-chan struct{} { return s.done }

// Close hangs up the shell's process group, closes the PTY and removes the terminal kubeconfig.
// It does not wait: a shell that ignores SIGHUP is killed after closeGrace in the background.
func (s *localSession) Close() error {
	s.closeOnce.Do(func() {
		if !s.exited.Load() {
			_ = syscall.Kill(-s.pid, syscall.SIGHUP)
			go s.killAfterGrace()
		}
		_ = s.input.Close()
		// Closing the master also hangs up the terminal for processes in other process groups
		// (the shell's jobs).
		s.closeErr = s.pty.Close()
		s.removeConfig()
	})
	return s.closeErr
}

func (s *localSession) killAfterGrace() {
	defer recoverTo("local shell kill", nil)
	select {
	case <-s.done:
	case <-time.After(closeGrace):
		if !s.exited.Load() {
			_ = syscall.Kill(-s.pid, syscall.SIGKILL)
		}
	}
}

func (s *localSession) removeConfig() {
	s.removeOnce.Do(func() {
		_ = os.Remove(s.cfgPath)
		if s.ownDir != "" {
			_ = os.RemoveAll(s.ownDir)
		}
	})
}
