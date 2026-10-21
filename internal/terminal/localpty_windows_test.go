//go:build windows

package terminal

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// plainShell is the shell the tests start where they need one that reads no profile: cmd.exe
// (started with /d, it runs no AutoRun command).
func plainShell() string {
	if comspec := os.Getenv("ComSpec"); comspec != "" {
		return comspec
	}
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
}

// startTestCmd starts cmd.exe without its AutoRun commands in a temporary folder, which is also
// its USERPROFILE, and records its output.
func startTestCmd(t *testing.T, opts LocalOptions) (Session, *outputRecorder, string) {
	t.Helper()
	home := t.TempDir()
	opts.Shell = plainShell()
	opts.Args = append([]string{"/d"}, opts.Args...)
	opts.Dir = home
	opts.Env = append(opts.Env, "USERPROFILE="+home, "PROMPT=$G ", "COGPIT_TEST_MARK=42")
	sess, err := StartLocal(opts)
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	closeOnCleanup(t, sess)
	return sess, record(sess), home
}

// typeLines types each line into the session as the terminal widget does (Enter is a carriage
// return), then a last one that writes done.txt, and waits for that file: the console host
// repaints the screen as it likes, so results go to files, not through the output.
func typeLines(t *testing.T, sess Session, home string, lines ...string) {
	t.Helper()
	lines = append(lines, "echo done> done.txt")
	if _, err := sess.Write([]byte(strings.Join(lines, "\r") + "\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForFile(t, filepath.Join(home, "done.txt"), "done", 20*time.Second)
}

func waitForFile(t *testing.T, path, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s did not say %q within %v", path, want, timeout)
}

func TestStartLocalWindowsBindsKubectlToContextAndNamespace(t *testing.T) {
	userPath := writeUserKubeconfig(t)
	userBefore, _ := os.ReadFile(userPath)
	sess, out, home := startTestCmd(t, LocalOptions{
		KubeconfigPaths: []string{userPath},
		Context:         "ctx-a",
		Namespace:       "team-x",
	})

	// The marker is the variable's value, so that the echo of the typed line does not contain it.
	lines := []string{"echo %KUBECONFIG%> env.txt", "echo __DONE%COGPIT_TEST_MARK%__"}
	kubectl, kubectlErr := exec.LookPath("kubectl")
	if kubectlErr == nil {
		lines = append(lines,
			`"`+kubectl+`" config current-context > ctx.txt 2>&1`,
			`"`+kubectl+`" config view --minify -o jsonpath="{.contexts[0].context.namespace}" > ns.txt 2>&1`,
			`"`+kubectl+`" config use-context ctx-b > nul 2>&1`,
			`"`+kubectl+`" config current-context > ctx2.txt 2>&1`)
	}
	typeLines(t, sess, home, lines...)
	out.waitFor(t, "__DONE42__", 10*time.Second)

	// KUBECONFIG = <own file>;<user's file>.
	env := strings.Split(readTrimmed(t, filepath.Join(home, "env.txt")), string(os.PathListSeparator))
	if len(env) != 2 || env[1] != userPath {
		t.Fatalf("KUBECONFIG = %q, want <own file>;%s", env, userPath)
	}
	own := env[0]
	ownData, err := os.ReadFile(own)
	if err != nil {
		t.Fatalf("terminal kubeconfig: %v", err)
	}
	if strings.Contains(string(ownData), "token") || !strings.Contains(string(ownData), "team-x") {
		t.Errorf("terminal kubeconfig:\n%s", ownData)
	}
	if kubectlErr == nil {
		if got := readTrimmed(t, filepath.Join(home, "ctx.txt")); got != "ctx-a" {
			t.Errorf("kubectl config current-context = %q, want ctx-a", got)
		}
		if got := readTrimmed(t, filepath.Join(home, "ns.txt")); got != "team-x" {
			t.Errorf("namespace = %q, want team-x", got)
		}
		if got := readTrimmed(t, filepath.Join(home, "ctx2.txt")); got != "ctx-b" {
			t.Errorf("after use-context: current-context = %q, want ctx-b", got)
		}
	} else {
		t.Log("kubectl not on PATH: KUBECONFIG checked, kubectl's reading of it not")
	}
	if userAfter, _ := os.ReadFile(userPath); string(userAfter) != string(userBefore) {
		t.Errorf("the user's kubeconfig changed:\n%s", userAfter)
	}

	start := time.Now()
	if err := sess.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("Close blocked for %v", d)
	}
	waitClosed(t, sess.Done(), 5*time.Second, "Done")
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Errorf("terminal kubeconfig still exists after the session ended (stat: %v)", err)
	}
	if err := sess.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := sess.Write([]byte("x")); err == nil {
		t.Error("Write after Close succeeded")
	}
	if err := out.waitEnd(t, 5*time.Second); err == nil || err.Error() != "EOF" {
		t.Errorf("Read after Close: %v, want EOF", err)
	}
	if err := sess.Resize(100, 30); err == nil {
		t.Error("Resize after Close succeeded")
	}
}

// TestStartLocalWindowsResize: the console's programs see the size the terminal sets. Windows
// PowerShell reports it; cmd's mode con says it in the system's language.
func TestStartLocalWindowsResize(t *testing.T) {
	ps := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if !isFile(ps) {
		t.Skip("no Windows PowerShell")
	}
	home := t.TempDir()
	sess, err := StartLocal(LocalOptions{
		KubeconfigPaths: []string{writeUserKubeconfig(t)}, Shell: ps, Args: []string{"-NoLogo", "-NoProfile"}, Dir: home,
	})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, sess)
	record(sess)
	if err := sess.Resize(123, 45); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	for _, bad := range [][2]uint16{{0, 10}, {10, 0}, {40000, 10}} {
		if err := sess.Resize(bad[0], bad[1]); err == nil {
			t.Errorf("Resize(%d, %d) succeeded", bad[0], bad[1])
		}
	}
	line := `$s = $Host.UI.RawUI.WindowSize; "$($s.Width) $($s.Height)" | Set-Content -Encoding ascii size.txt; 'done' | Set-Content -Encoding ascii done.txt` + "\r"
	if _, err := sess.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, filepath.Join(home, "done.txt"), "done", 30*time.Second)
	if got := readTrimmed(t, filepath.Join(home, "size.txt")); got != "123 45" {
		t.Errorf("PowerShell's window size = %q, want \"123 45\"", got)
	}
}

func TestStartLocalWindowsShellExitEndsSession(t *testing.T) {
	sess, out, _ := startTestCmd(t, LocalOptions{KubeconfigPaths: []string{writeUserKubeconfig(t)}})
	if _, err := sess.Write([]byte("exit 3\r")); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, sess.Done(), 10*time.Second, "Done")
	if err := out.waitEnd(t, 5*time.Second); err == nil || err.Error() != "EOF" {
		t.Errorf("Read after exit: %v, want EOF", err)
	}
}

// procQueryInformationJobObject lists a job's processes; through LazyProc.Call, which keeps the
// list in place during the call (newJob says why).
var procQueryInformationJobObject = windows.NewLazySystemDLL("kernel32.dll").NewProc("QueryInformationJobObject")

// jobProcesses returns the ids of the processes in the session's job (JOBOBJECT_BASIC_PROCESS_ID_LIST).
func jobProcesses(t *testing.T, s *localSession) []uint32 {
	t.Helper()
	var list struct {
		assigned, listed uint32
		ids              [32]uintptr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job == 0 {
		return nil
	}
	r, _, err := procQueryInformationJobObject.Call(uintptr(s.job), windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&list)), unsafe.Sizeof(list), 0)
	if r == 0 {
		t.Fatalf("listing the job's processes: %v", err)
	}
	ids := make([]uint32, 0, list.listed)
	for _, id := range list.ids[:list.listed] {
		ids = append(ids, uint32(id))
	}
	return ids
}

// TestStartLocalWindowsCloseEndsTheTree: a program the shell started (ping, running for a minute)
// is in the session's job and ends when the session closes, without Close waiting for it.
func TestStartLocalWindowsCloseEndsTheTree(t *testing.T) {
	sess, _, _ := startTestCmd(t, LocalOptions{KubeconfigPaths: []string{writeUserKubeconfig(t)}})
	if _, err := sess.Write([]byte("ping -n 60 127.0.0.1 > nul\r")); err != nil {
		t.Fatal(err)
	}
	var ids []uint32
	deadline := time.Now().Add(10 * time.Second)
	for len(ids) < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		ids = jobProcesses(t, sess.(*localSession))
	}
	if len(ids) < 2 {
		t.Fatalf("the job holds %v, want cmd and ping", ids)
	}
	var procs []windows.Handle
	for _, id := range ids {
		h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, id)
		if err != nil {
			t.Fatalf("opening process %d: %v", id, err)
		}
		defer func() { _ = windows.CloseHandle(h) }()
		procs = append(procs, h)
	}
	start := time.Now()
	if err := sess.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("Close blocked for %v", d)
	}
	for i, h := range procs {
		if ev, err := windows.WaitForSingleObject(h, 5000); ev != windows.WAIT_OBJECT_0 {
			t.Errorf("process %d still runs 5s after Close (%d, %v)", ids[i], ev, err)
		}
	}
	waitClosed(t, sess.Done(), 5*time.Second, "Done")
}

// The shell gets the Preferences' arguments after its name and starts in the cluster's working
// directory (Cluster settings › Terminal).
func TestStartLocalWindowsPassesArgsAndDir(t *testing.T) {
	work := t.TempDir()
	sess, err := StartLocal(LocalOptions{
		KubeconfigPaths: []string{writeUserKubeconfig(t)}, Context: "ctx-a",
		Shell: plainShell(), Args: []string{"/d", "/c", "cd > out.txt & echo %KUBECONFIG%>> out.txt"}, Dir: work,
	})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, sess)
	record(sess)
	waitClosed(t, sess.Done(), 10*time.Second, "the shell running /c")
	out := strings.Split(readTrimmed(t, filepath.Join(work, "out.txt")), "\n")
	if got := strings.TrimSpace(out[0]); !strings.EqualFold(got, work) {
		t.Errorf("the shell ran in %q, want %q", got, work)
	}
	if len(out) < 2 || !strings.Contains(out[1], "cogpit-terminal-") {
		t.Errorf("KUBECONFIG in the shell: %q", out)
	}
}

// Without a shell in the options, the local terminal starts pwsh.exe from PATH, else Windows
// PowerShell, else %ComSpec%.
func TestDefaultShellWindows(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SystemRoot", root)
	t.Setenv("ComSpec", `D:\cmd.exe`)
	ps := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	pwsh := func(string) (string, error) { return `C:\Program Files\PowerShell\7\pwsh.exe`, nil }
	none := func(string) (string, error) { return "", exec.ErrNotFound }
	exists := func(p string) bool { return p == ps }
	missing := func(string) bool { return false }
	for _, tc := range []struct {
		name     string
		lookPath func(string) (string, error)
		exists   func(string) bool
		comspec  string
		want     string
	}{
		{"pwsh on PATH", pwsh, exists, `D:\cmd.exe`, `C:\Program Files\PowerShell\7\pwsh.exe`},
		{"Windows PowerShell", none, exists, `D:\cmd.exe`, ps},
		{"ComSpec", none, missing, `D:\cmd.exe`, `D:\cmd.exe`},
		{"cmd.exe", none, missing, "", filepath.Join(root, "System32", "cmd.exe")},
	} {
		t.Setenv("ComSpec", tc.comspec)
		if got := defaultShell(tc.lookPath, tc.exists); got != tc.want {
			t.Errorf("%s: defaultShell = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The default shell starts (Windows PowerShell on a computer without PowerShell 7).
func TestStartLocalWindowsDefaultShellStarts(t *testing.T) {
	home := t.TempDir()
	sess, err := StartLocal(LocalOptions{KubeconfigPaths: []string{writeUserKubeconfig(t)}, Dir: home, Args: []string{"-NoLogo", "-NoProfile"}})
	if err != nil {
		t.Fatalf("StartLocal without a shell: %v", err)
	}
	closeOnCleanup(t, sess)
	out := record(sess)
	if _, err := sess.Write([]byte("exit\r")); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, sess.Done(), 30*time.Second, "Done")
	if err := out.waitEnd(t, 5*time.Second); err == nil || err.Error() != "EOF" {
		t.Errorf("Read after exit: %v, want EOF", err)
	}
}

// fakeKubectlExe writes a file named name that says says; cmd's where finds it by its name, and
// the tests read it back, so it need not run.
func fakeKubectlExe(t *testing.T, dir, name, says string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(says), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestStartLocalWindowsKubectlFolderFirst: a kubectl named kubectl.exe has its folder first on
// PATH, ahead of another kubectl the user's PATH has first; no session directory is made.
func TestStartLocalWindowsKubectlFolderFirst(t *testing.T) {
	target := fakeKubectlExe(t, filepath.Join(t.TempDir(), "1.36.1"), "kubectl.exe", "cogpit")
	shadow := filepath.Dir(fakeKubectlExe(t, filepath.Join(t.TempDir(), "shadow"), "kubectl.exe", "shadow"))
	sess, _, home := startTestCmd(t, LocalOptions{
		KubeconfigPaths: []string{writeUserKubeconfig(t)}, Kubectl: target,
		Env: []string{"Path=" + shadow + ";" + os.Getenv("PATH")},
	})
	typeLines(t, sess, home, "where kubectl > where.txt", "echo %PATH%> path.txt")
	where := strings.Fields(readTrimmed(t, filepath.Join(home, "where.txt")))
	if len(where) < 2 || where[0] != target || where[1] != filepath.Join(shadow, "kubectl.exe") {
		t.Errorf("where kubectl: %q, want %s first, then the shadow", where, target)
	}
	path := strings.Split(readTrimmed(t, filepath.Join(home, "path.txt")), ";")
	if path[0] != filepath.Dir(target) || len(path) < 2 || path[1] != shadow {
		t.Errorf("PATH starts %q, want the kubectl's folder, then the user's PATH", path[:min(len(path), 3)])
	}
	if s := sess.(*localSession); s.ownDir != "" {
		t.Errorf("a session directory for a kubectl.exe: %s", s.ownDir)
	}
}

// TestStartLocalWindowsKubectlByAnotherName: a kubectl not named kubectl.exe gets the session's
// own directory with a kubectl.exe that is it, first on PATH, removed when the session ends.
func TestStartLocalWindowsKubectlByAnotherName(t *testing.T) {
	target := fakeKubectlExe(t, t.TempDir(), "kubectl-1.36.exe", "cogpit-renamed")
	sess, _, home := startTestCmd(t, LocalOptions{KubeconfigPaths: []string{writeUserKubeconfig(t)}, Kubectl: target})
	typeLines(t, sess, home, "where kubectl > where.txt")
	where := strings.Fields(readTrimmed(t, filepath.Join(home, "where.txt")))
	if len(where) == 0 || !strings.Contains(where[0], "cogpit-terminal-") || filepath.Base(where[0]) != "kubectl.exe" {
		t.Fatalf("where kubectl: %q, want the session's own kubectl.exe first", where)
	}
	if got := readTrimmed(t, where[0]); got != "cogpit-renamed" {
		t.Errorf("the session's kubectl.exe says %q, want the chosen kubectl", got)
	}
	own := sess.(*localSession).ownDir
	if own == "" || !strings.HasPrefix(where[0], own) {
		t.Errorf("session directory %q, kubectl %q", own, where[0])
	}
	_ = sess.Close()
	waitClosed(t, sess.Done(), 10*time.Second, "Done")
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Errorf("the session's directory is still there: %v", err)
	}
}

// TestStartLocalWindowsRemovesHeldFiles: when the session ends, its kubectl.exe still runs and a
// reader still holds the terminal kubeconfig open (as kubectl does while it reads it; Go opens
// files without FILE_SHARE_DELETE, so Windows refuses to delete them meanwhile). Both are gone
// once the session has ended. The kubectl is a copy of ping, which runs long.
func TestStartLocalWindowsRemovesHeldFiles(t *testing.T) {
	ping, err := os.ReadFile(filepath.Join(os.Getenv("SystemRoot"), "System32", "PING.EXE"))
	if err != nil {
		t.Skip("no ping:", err)
	}
	target := filepath.Join(t.TempDir(), "kubectl-ping.exe")
	if err := os.WriteFile(target, ping, 0o700); err != nil {
		t.Fatal(err)
	}
	sess, _, home := startTestCmd(t, LocalOptions{KubeconfigPaths: []string{writeUserKubeconfig(t)}, Kubectl: target})
	typeLines(t, sess, home)
	if _, err := sess.Write([]byte("kubectl -n 60 127.0.0.1 > nul\r")); err != nil {
		t.Fatal(err)
	}
	s := sess.(*localSession)
	deadline := time.Now().Add(10 * time.Second)
	for len(jobProcesses(t, s)) < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	held, err := os.Open(s.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.cfgPath); err == nil {
		t.Fatal("an open terminal kubeconfig could be removed: the test does not exercise the retry")
	}
	time.AfterFunc(300*time.Millisecond, func() { _ = held.Close() })
	_ = sess.Close()
	waitClosed(t, sess.Done(), removeFor+5*time.Second, "Done")
	if _, err := os.Stat(s.cfgPath); !os.IsNotExist(err) {
		t.Errorf("the terminal kubeconfig is still there: %v", err)
	}
	if _, err := os.Stat(s.ownDir); !os.IsNotExist(err) {
		t.Errorf("the session's directory is still there: %v", err)
	}
}

// linkKubectl copies what it cannot link: a file the user may not write (one of Windows' own,
// owned by TrustedInstaller) or one on another volume.
func TestLinkKubectlCopiesWhatItCannotLink(t *testing.T) {
	for _, src := range []string{
		filepath.Join(os.Getenv("SystemRoot"), "System32", "whoami.exe"),
		fakeKubectlExe(t, t.TempDir(), "kubectl-own.exe", "linked"),
	} {
		dst := filepath.Join(t.TempDir(), "kubectl.exe")
		if err := linkKubectl(src, dst); err != nil {
			t.Fatalf("linkKubectl(%s): %v", src, err)
		}
		want, _ := os.ReadFile(src)
		if got, err := os.ReadFile(dst); err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: the link holds %d bytes (%v), want %d", src, len(got), err, len(want))
		}
	}
}

func TestKubectlOnPathRefuses(t *testing.T) {
	semi := fakeKubectlExe(t, filepath.Join(t.TempDir(), "a;b"), "kubectl.exe", "x")
	for _, bad := range []string{"kubectl.exe", filepath.Join(t.TempDir(), "missing.exe"), t.TempDir(), semi} {
		if dir, _, err := kubectlOnPath(bad, []string{"Path=C:\\Windows"}); err == nil {
			_ = os.RemoveAll(dir)
			t.Errorf("kubectl %q accepted", bad)
		}
	}
}

// Windows' environment names ignore case: Path is PATH, and a name given twice keeps its last
// value.
func TestWindowsEnvironmentIgnoresCase(t *testing.T) {
	if v, _ := lastEnv([]string{"Path=a", "OTHER=x"}, "PATH"); v != "a" {
		t.Errorf("lastEnv(Path) = %q", v)
	}
	if v, _ := lastEnv([]string{"Path=a", "PATH=b", "path=c"}, "PATH"); v != "c" {
		t.Errorf("lastEnv = %q, want the last", v)
	}
	block, err := envBlock([]string{"=C:=C:\\Users", "Path=a", "TERM=x", "PATH=b", "KUBECONFIG=k", "noequals"})
	if err != nil {
		t.Fatal(err)
	}
	// The block ends in an empty entry: two NULs in a row.
	var units []uint16
	for p := block; len(units) < 2 || units[len(units)-1] != 0 || units[len(units)-2] != 0; p = (*uint16)(unsafe.Add(unsafe.Pointer(p), 2)) {
		units = append(units, *p)
	}
	entries := strings.Split(string(utf16.Decode(units[:len(units)-2])), "\x00")
	if got := strings.Join(entries, "|"); got != `=C:=C:\Users|PATH=b|TERM=x|KUBECONFIG=k` {
		t.Errorf("environment block %q", got)
	}
	if _, err := envBlock([]string{"A=x\x00y"}); err == nil {
		t.Error("an entry with NUL accepted")
	}
}
