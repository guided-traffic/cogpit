//go:build windows

package terminal

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A local terminal on Windows runs its shell on a pseudo console (ConPTY, Windows 10 1809 and
// later), Windows' counterpart of a PTY: the console host that CreatePseudoConsole starts
// (conhost.exe) keeps the screen, reads the user's keystrokes as VT input from one pipe and writes
// the screen as VT output to another. golang.org/x/sys/windows has every call this takes. The
// pseudo console package the terminal widget's own shell uses (ActiveState's termtest/conpty, of
// the Go 1.12 days) ends the process when a pipe cannot be created and starts the shell running
// before it could be put in a job; Cogpit does not use it.
//
// Windows has neither process groups nor a hangup. The shell runs in a job object of its own that
// kills every process left in it when the session ends (JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE), so
// that ending the terminal ends the programs started from it, as closing a console window ends
// the programs in it. The shell starts suspended and runs only once it is in the job, so nothing
// it starts is outside. A program that asks to leave the job (CREATE_BREAKAWAY_FROM_JOB) may, as
// a program on Unix may leave the terminal's session with setsid; one that is refused fails to
// start instead.

const (
	// drainAfterExit is how long output is still read after the shell exited. Closing the pseudo
	// console paints its last frame and then ends the output pipe; a console host that does
	// neither in time has the pipe closed under it, and the session ends regardless.
	drainAfterExit = 500 * time.Millisecond
	// removeFor is how long removing the session's files is retried after the shell exited:
	// Windows does not delete a file that a process opened without allowing it (as every Go
	// program, kubectl among them, opens the kubeconfig it reads), and the processes the job just
	// ended let go of theirs only as they exit.
	removeFor   = 2 * time.Second
	removeRetry = 50 * time.Millisecond
)

// localSession is a shell on a pseudo console that Cogpit started itself.
type localSession struct {
	in      *os.File       // the write end of the console's input pipe
	out     *os.File       // the read end of the console's output pipe
	process windows.Handle // the shell's process; wait closes it
	cfgPath string
	ownDir  string // the session's own directory (a kubectl not named kubectl.exe); "" without
	input   *inputQueue
	done    chan struct{}

	mu      sync.Mutex
	console windows.Handle // the pseudo console; 0 once closed
	job     windows.Handle // the job the shell and its programs run in; 0 once ended

	// closeOut closes out once: on Close, or when the drain after the shell's exit is over.
	closeOut  func() error
	closeOnce sync.Once
	closeErr  error
}

// defaultShell is the shell a local terminal runs when the options name none (the lead's
// decision, open to revision): PowerShell 7 when pwsh.exe is on PATH, since whoever installed it
// chose it over the PowerShell Windows brings; else Windows PowerShell, which every Windows 10 and
// 11 has; else %ComSpec%, cmd.exe. $SHELL is not asked: on Windows only Unix-like environments
// (Git Bash, MSYS2, Cygwin) set it, to a path of theirs that Windows cannot start.
func defaultShell(lookPath func(string) (string, error), exists func(string) bool) string {
	if pwsh, err := lookPath("pwsh.exe"); err == nil {
		return pwsh
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	if ps := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"); exists(ps) {
		return ps
	}
	if comspec := os.Getenv("ComSpec"); comspec != "" {
		return comspec
	}
	return filepath.Join(root, "System32", "cmd.exe")
}

// isFile reports whether path is a regular file; Windows has no executable bit to look at.
func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

func startLocal(opts LocalOptions) (Session, error) {
	binding, err := resolveLocal(opts)
	if err != nil {
		return nil, err
	}

	shell := opts.Shell
	if shell == "" {
		shell = defaultShell(exec.LookPath, isFile)
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
	var ownDir string // the session's own directory, for a kubectl not named kubectl.exe
	var kubectlEnv []string
	if opts.Kubectl != "" {
		if ownDir, kubectlEnv, err = kubectlOnPath(opts.Kubectl, baseEnv(opts.Env)); err != nil {
			_ = os.Remove(cfgPath)
			return nil, err
		}
	}

	// Windows passes a program one command line, which it splits itself; Cogpit composes it with
	// the quoting rules of Microsoft's C runtime, as os/exec does. There is no login shell to ask
	// for: PowerShell and cmd read their profiles and AutoRun commands in every interactive start.
	argv := append([]string{shellPath}, opts.Args...)
	s, err := startConsole(shellPath, argv, dir, localEnv(opts.Env, binding.kubeconfigVar(cfgPath), kubectlEnv...))
	if err != nil {
		removeSessionFiles(cfgPath, ownDir)
		return nil, fmt.Errorf("local terminal: starting %s: %w", shellPath, err)
	}
	s.cfgPath, s.ownDir = cfgPath, ownDir
	go s.pumpInput()
	go s.wait()
	return s, nil
}

// kubectlOnPath is how kubectl comes first on Windows, where a symbolic link (kubectlFirst's on
// Unix) needs a privilege that users rarely have. A kubectl named kubectl.exe has its own folder
// put first on PATH: no file to write or remove, and right for every kind of kubectl, a shim that
// finds its program next to itself (Scoop's) among them, while the folder's other programs come
// first in this terminal too (Cogpit's downloads have none). A kubectl by another name gets the
// session's own directory with a hard link named kubectl.exe, else a copy (kubectlFirst,
// linkKubectl), which the caller removes when the session ends. env is the shell's environment
// before these entries; its last PATH counts, in any case.
func kubectlOnPath(kubectl string, env []string) (ownDir string, entries []string, err error) {
	if !filepath.IsAbs(kubectl) {
		return "", nil, fmt.Errorf("local terminal: kubectl %q is not an absolute path", kubectl)
	}
	if fi, err := os.Stat(kubectl); err != nil || !fi.Mode().IsRegular() {
		return "", nil, fmt.Errorf("local terminal: kubectl %s is not a file", kubectl)
	}
	if !strings.EqualFold(filepath.Base(kubectl), binaryName()) {
		// No shell name matches kubectlFirst's zsh, bash or fish here, and enforcing is theirs:
		// only the PATH entry comes back.
		dir, start, err := kubectlFirst(kubectl, "", env, false)
		return dir, start.env, err
	}
	folder := filepath.Dir(kubectl)
	if strings.ContainsRune(folder, os.PathListSeparator) {
		return "", nil, fmt.Errorf("local terminal: the folder of kubectl %s contains %q and cannot be put on PATH", kubectl, os.PathListSeparator)
	}
	path, _ := lastEnv(env, "PATH")
	if path != "" {
		path = string(os.PathListSeparator) + path
	}
	return "", []string{"PATH=" + folder + path}, nil
}

// linkKubectl puts kubectl at link for kubectlFirst: a hard link, which Windows makes without a
// privilege for a file on the same volume that the user may write; else a copy.
func linkKubectl(kubectl, link string) error {
	if os.Link(kubectl, link) == nil {
		return nil
	}
	src, err := os.Open(kubectl) // #nosec G304 -- the kubectl the Preferences name, checked to be a file
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(link, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}

// startConsole starts the program at path with argv, in dir with env, on a new pseudo console of
// 80 by 24 cells and in a new job, and returns its session without files or goroutines.
func startConsole(path string, argv []string, dir string, env []string) (s *localSession, err error) {
	s = &localSession{input: newInputQueue(), done: make(chan struct{})}
	defer func() {
		if err != nil {
			s.release()
		}
	}()
	envp, err := envBlock(env)
	if err != nil {
		return nil, err
	}

	// The console host reads keystrokes from inR and writes the screen to outW; Cogpit keeps inW
	// and outR. CreatePipe's handles are not inheritable, so no program Cogpit starts gets one
	// (what close-on-exec does on Unix), and the shell gets its console only.
	var inR, inW, outR, outW windows.Handle
	if err = windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, fmt.Errorf("creating the console's input pipe: %w", err)
	}
	s.in = os.NewFile(uintptr(inW), "pseudo console input")
	if err = windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		_ = windows.CloseHandle(inR)
		return nil, fmt.Errorf("creating the console's output pipe: %w", err)
	}
	s.out = os.NewFile(uintptr(outR), "pseudo console output")
	s.closeOut = sync.OnceValue(s.out.Close)
	var console windows.Handle
	err = windows.CreatePseudoConsole(windows.Coord{X: 80, Y: 24}, inR, outW, 0, &console)
	// The console host holds handles of its own to its ends of the pipes now.
	_ = windows.CloseHandle(inR)
	_ = windows.CloseHandle(outW)
	if err != nil {
		return nil, fmt.Errorf("creating the pseudo console: %w", err)
	}
	s.console = console
	if s.job, err = newJob(); err != nil {
		return nil, err
	}
	if s.process, err = createProcess(path, argv, dir, envp, console, s.job); err != nil {
		return nil, err
	}
	return s, nil
}

// procSetInformationJobObject is called through LazyProc.Call, which keeps the information's
// memory in place during the call (go:uintptrescapes); x/sys's wrapper takes a bare uintptr, which
// a stack that grows during the call would leave pointing at the stack's old place.
var procSetInformationJobObject = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetInformationJobObject")

// newJob creates the job a session's shell runs in: ending it, or closing its last handle, kills
// every process in it; a process leaves it only by asking (CREATE_BREAKAWAY_FROM_JOB).
func newJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("creating the shell's job: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
		LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK,
	}}
	// #nosec G103 -- the address of info, with its size, for the duration of the call.
	r, _, err := procSetInformationJobObject.Call(uintptr(job), windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if r == 0 {
		_ = windows.CloseHandle(job)
		return 0, fmt.Errorf("setting the shell's job to end with the session: %w", err)
	}
	return job, nil
}

// createProcess starts the program at path suspended on console, puts it in job and only then
// lets it run, so that nothing it starts is outside the job. It returns the process's handle.
func createProcess(path string, argv []string, dir string, env *uint16, console, job windows.Handle) (windows.Handle, error) {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return 0, fmt.Errorf("preparing the shell's attributes: %w", err)
	}
	defer attrs.Delete()
	// The attribute's value is the pseudo console's handle itself, not its address.
	// #nosec G103 -- the handle's bits as the pointer-sized value UpdateProcThreadAttribute takes; the list keeps it until Delete.
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&console)), unsafe.Sizeof(console)); err != nil {
		return 0, fmt.Errorf("attaching the pseudo console: %w", err)
	}
	si := &windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(*si))
	// Empty standard handles of its own: without them a shell started by a Cogpit whose own
	// output goes to a file or a pipe (a test, a log) writes there instead of to its console.
	si.Flags = windows.STARTF_USESTDHANDLES

	pathp, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(argv))
	if err != nil {
		return 0, err
	}
	dirp, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if err := windows.CreateProcess(pathp, cmdline, nil, nil, false, flags, env, dirp, &si.StartupInfo, &pi); err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(pi.Thread) }()
	fail := func(what string, err error) (windows.Handle, error) {
		_ = windows.TerminateProcess(pi.Process, 1)
		_ = windows.CloseHandle(pi.Process)
		return 0, fmt.Errorf("%s: %w", what, err)
	}
	if err := windows.AssignProcessToJobObject(job, pi.Process); err != nil {
		return fail("putting the shell in its job", err)
	}
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		return fail("letting the shell run", err)
	}
	return pi.Process, nil
}

// envBlock is env as CreateProcess takes it: NUL-terminated UTF-16 entries, then an empty one.
// Windows' names ignore case (Path is PATH), so a name given twice keeps its last value, in the
// first one's place, as os/exec does; this is how opts.Env and Cogpit's own entries win.
func envBlock(env []string) (*uint16, error) {
	index := make(map[string]int, len(env))
	kept := make([]string, 0, len(env))
	for _, kv := range env {
		name := strings.ToUpper(envName(kv))
		if name == "" || !strings.Contains(kv, "=") {
			continue
		}
		if i, ok := index[name]; ok {
			kept[i] = kv
			continue
		}
		index[name] = len(kept)
		kept = append(kept, kv)
	}
	var block []uint16
	for _, kv := range kept {
		u, err := windows.UTF16FromString(kv)
		if err != nil {
			return nil, fmt.Errorf("the environment entry %s: %w", envName(kv), err)
		}
		block = append(block, u...)
	}
	if len(block) == 0 {
		block = append(block, 0)
	}
	block = append(block, 0)
	return &block[0], nil
}

// envName is the name of an environment entry. On Windows a name can start with "=": cmd keeps
// each drive's current directory as "=C:=C:\Users".
func envName(kv string) string {
	if rest, ok := strings.CutPrefix(kv, "="); ok {
		name, _, _ := strings.Cut(rest, "=")
		return "=" + name
	}
	name, _, _ := strings.Cut(kv, "=")
	return name
}

// release ends the job and closes every handle the session holds; for a start that failed.
func (s *localSession) release() {
	s.end()
	if s.out != nil {
		_ = s.closeOut()
	}
	if s.in != nil {
		_ = s.in.Close()
	}
	if s.process != 0 {
		_ = windows.CloseHandle(s.process)
	}
}

// pumpInput delivers queued keystrokes to the pseudo console.
func (s *localSession) pumpInput() {
	defer recoverTo("local shell input", nil)
	_, _ = io.Copy(s.in, s.input)
}

// wait waits for the shell to exit, ends what it left in its job, closes the console, lets the
// remaining output drain for a moment, removes the session's files and ends the session.
func (s *localSession) wait() {
	defer close(s.done)
	defer recoverTo("local shell wait", nil)
	_, _ = windows.WaitForSingleObject(s.process, windows.INFINITE)
	_ = windows.CloseHandle(s.process)
	_ = s.input.Close()
	// The programs the shell left in its console end with it, as when a console window closes.
	// Closing the console paints its last frame and ends the output pipe, which the reader sees
	// as the end of the output.
	s.end()
	time.AfterFunc(drainAfterExit, func() {
		defer recoverTo("local shell drain", nil)
		_ = s.closeOut()
	})
	s.removeFiles()
}

// end ends every process in the session's job and closes its pseudo console; it is idempotent.
func (s *localSession) end() {
	s.mu.Lock()
	job, console := s.job, s.console
	s.job, s.console = 0, 0
	s.mu.Unlock()
	if job != 0 {
		// Closing the job's only handle would end its processes too (KILL_ON_JOB_CLOSE);
		// TerminateJobObject says so, and does it whoever else holds the job.
		_ = windows.TerminateJobObject(job, 1)
		_ = windows.CloseHandle(job)
	}
	if console != 0 {
		// ClosePseudoConsole can wait until the console host has written its last frame, which
		// waits for the reader (Microsoft documents it for Windows before 11 24H2): never in the
		// caller's goroutine. Close ends that wait by closing the output pipe.
		go func() {
			defer recoverTo("local shell console", nil)
			windows.ClosePseudoConsole(console)
		}()
	}
}

// Read returns the shell's output; io.EOF once the session has ended and its output is read.
func (s *localSession) Read(p []byte) (int, error) {
	n, err := s.out.Read(p)
	// A broken pipe, the console host gone, is io.EOF already; a closed pipe (Close, or the end of
	// the drain after the exit) means the same thing to the reader: the session is over.
	if err != nil && errors.Is(err, os.ErrClosed) {
		err = io.EOF
	}
	return n, err
}

// Write queues keystrokes for the shell; it does not block.
func (s *localSession) Write(p []byte) (int, error) {
	return s.input.Write(p)
}

// Resize sets the pseudo console's size; the console host tells the programs in it.
func (s *localSession) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 || cols > math.MaxInt16 || rows > math.MaxInt16 {
		return fmt.Errorf("local terminal: invalid size %dx%d", cols, rows)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.console == 0 {
		return fmt.Errorf("local terminal: resizing: %w", os.ErrClosed)
	}
	return windows.ResizePseudoConsole(s.console, windows.Coord{X: int16(cols), Y: int16(rows)}) // #nosec G115 -- both checked above
}

// Done is closed when the shell has exited and the session's files are removed.
func (s *localSession) Done() <-chan struct{} { return s.done }

// Close ends the shell and every program in its job, closes the pseudo console and the pipes. It
// does not wait: the session's files are removed once the shell is gone (wait), since Windows
// does not delete a file that a program on its way out may still hold.
func (s *localSession) Close() error {
	s.closeOnce.Do(func() {
		_ = s.input.Close()
		s.end()
		// Closing the output also ends a pending Read, and a console host still writing to it.
		s.closeErr = s.closeOut()
		_ = s.in.Close()
	})
	return s.closeErr
}

// removeFiles removes the terminal kubeconfig and the session's own directory, retrying for
// removeFor while a process still holds one of them.
func (s *localSession) removeFiles() {
	deadline := time.Now().Add(removeFor)
	for !removeSessionFiles(s.cfgPath, s.ownDir) && time.Now().Before(deadline) {
		time.Sleep(removeRetry)
	}
}

// removeSessionFiles removes the terminal kubeconfig and ownDir, if any, and reports whether both
// are gone.
func removeSessionFiles(cfgPath, ownDir string) bool {
	err := os.Remove(cfgPath)
	gone := err == nil || errors.Is(err, fs.ErrNotExist)
	if ownDir != "" && os.RemoveAll(ownDir) != nil {
		gone = false
	}
	return gone
}
