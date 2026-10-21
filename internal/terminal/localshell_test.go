//go:build !windows

package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// plainShell is the shell the tests start where they need one that reads no profile: /bin/sh.
func plainShell() string { return "/bin/sh" }

// startTestShell starts /bin/sh with a temporary HOME, so the login shell reads none of the
// developer's profile files, and records its output.
func startTestShell(t *testing.T, opts LocalOptions) (Session, *outputRecorder, string) {
	t.Helper()
	home := t.TempDir()
	opts.Shell = "/bin/sh"
	opts.Dir = home
	opts.Env = append(opts.Env, "HOME="+home, "ENV=", "ZDOTDIR="+home, "PS1=$ ")
	sess, err := StartLocal(opts)
	if err != nil {
		t.Fatalf("StartLocal: %v", err)
	}
	closeOnCleanup(t, sess)
	return sess, record(sess), home
}

func TestStartLocalBindsKubectlToContextAndNamespace(t *testing.T) {
	kubectl, err := exec.LookPath("kubectl")
	if err != nil {
		t.Skip("kubectl not on PATH")
	}
	userPath := writeUserKubeconfig(t)
	userBefore, _ := os.ReadFile(userPath)

	sess, out, home := startTestShell(t, LocalOptions{
		KubeconfigPaths: []string{userPath},
		Context:         "ctx-a",
		Namespace:       "team-x",
	})

	// Results go to files, not through the TTY; the marker is computed by the shell so that the
	// echo of the typed line does not contain it.
	script := strings.Join([]string{
		"k='" + kubectl + "'",
		`printf '%s\n' "$KUBECONFIG" > env.txt`,
		`"$k" config current-context > ctx.txt 2>&1`,
		`"$k" config view --minify -o jsonpath='{.contexts[0].context.namespace}' > ns.txt 2>&1`,
		`"$k" config use-context ctx-b > /dev/null 2>&1`,
		`"$k" config current-context > ctx2.txt 2>&1`,
		`echo "__DONE$((40+2))__"`,
	}, "\n") + "\n"
	if _, err := sess.Write([]byte(script)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out.waitFor(t, "__DONE42__", 20*time.Second)

	if got := readTrimmed(t, filepath.Join(home, "ctx.txt")); got != "ctx-a" {
		t.Errorf("kubectl config current-context = %q, want ctx-a", got)
	}
	if got := readTrimmed(t, filepath.Join(home, "ns.txt")); got != "team-x" {
		t.Errorf("namespace = %q, want team-x", got)
	}

	// KUBECONFIG = <own file>:<user's file>.
	env := strings.Split(readTrimmed(t, filepath.Join(home, "env.txt")), string(os.PathListSeparator))
	if len(env) != 2 || env[1] != userPath {
		t.Fatalf("KUBECONFIG = %q, want <own file>:%s", env, userPath)
	}
	own := env[0]
	info, err := os.Stat(own)
	if err != nil {
		t.Fatalf("terminal kubeconfig: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("terminal kubeconfig mode = %o, want 600", perm)
	}
	ownData, err := os.ReadFile(own)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ownData), "secret-token") || strings.Contains(string(ownData), "token") {
		t.Errorf("terminal kubeconfig holds a credential:\n%s", ownData)
	}

	// A context switch inside the terminal lands in the terminal's file, never in the user's.
	if got := readTrimmed(t, filepath.Join(home, "ctx2.txt")); got != "ctx-b" {
		t.Errorf("after use-context: current-context = %q, want ctx-b", got)
	}
	if userAfter, _ := os.ReadFile(userPath); string(userAfter) != string(userBefore) {
		t.Errorf("the user's kubeconfig changed:\n%s", userAfter)
	}

	if err := sess.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Errorf("terminal kubeconfig still exists after Close (stat: %v)", err)
	}
	waitClosed(t, sess.Done(), 5*time.Second, "Done")
	if err := sess.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := sess.Write([]byte("x")); err == nil {
		t.Error("Write after Close succeeded")
	}
	if err := out.waitEnd(t, 5*time.Second); err == nil || err.Error() != "EOF" {
		t.Errorf("Read after Close: %v, want EOF", err)
	}
}

func TestStartLocalResize(t *testing.T) {
	if _, err := exec.LookPath("stty"); err != nil {
		t.Skip("stty not on PATH")
	}
	sess, out, home := startTestShell(t, LocalOptions{KubeconfigPaths: []string{writeUserKubeconfig(t)}})
	if err := sess.Resize(123, 45); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if err := sess.Resize(0, 10); err == nil {
		t.Error("Resize(0, 10) succeeded")
	}
	if _, err := sess.Write([]byte("stty size > size.txt; echo \"__SIZE$((1+1))__\"\n")); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "__SIZE2__", 10*time.Second)
	if got := readTrimmed(t, filepath.Join(home, "size.txt")); got != "45 123" {
		t.Errorf("stty size = %q, want \"45 123\"", got)
	}
}

func TestStartLocalShellExitEndsSession(t *testing.T) {
	sess, out, _ := startTestShell(t, LocalOptions{KubeconfigPaths: []string{writeUserKubeconfig(t)}})
	if _, err := sess.Write([]byte("exit 3\n")); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, sess.Done(), 10*time.Second, "Done")
	if err := out.waitEnd(t, 5*time.Second); err == nil || err.Error() != "EOF" {
		t.Errorf("Read after exit: %v, want EOF", err)
	}
}

func TestStartLocalCloseKillsShellIgnoringHangup(t *testing.T) {
	sess, out, _ := startTestShell(t, LocalOptions{KubeconfigPaths: []string{writeUserKubeconfig(t)}})
	// The shell becomes a process that ignores SIGHUP and does not touch the terminal, so
	// neither the hangup nor the closed PTY ends it; only the SIGKILL after the grace does.
	if _, err := sess.Write([]byte("trap '' HUP; echo \"__TRAP$((2+3))__\"; exec sleep 60 </dev/null >/dev/null 2>&1\n")); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, "__TRAP5__", 10*time.Second)
	time.Sleep(200 * time.Millisecond) // let the exec happen
	start := time.Now()
	if err := sess.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("Close blocked for %v", d)
	}
	waitClosed(t, sess.Done(), closeGrace+3*time.Second, "Done after SIGKILL")
	if d := time.Since(start); d < closeGrace-100*time.Millisecond {
		t.Errorf("process ended after %v, before the SIGKILL grace; the test did not exercise the kill", d)
	}
}

// Without a shell in the options or in $SHELL, the local terminal starts the first of zsh, bash
// and sh that exists, not a zsh the system may lack (CI's runner pods export no SHELL and have no
// zsh, 2026-10-13).
func TestDefaultShellIsOneThatExists(t *testing.T) {
	for _, tc := range []struct {
		exist []string
		want  string
	}{
		{[]string{"/bin/zsh", "/bin/bash", "/bin/sh"}, "/bin/zsh"},
		{[]string{"/bin/bash", "/bin/sh"}, "/bin/bash"},
		{[]string{"/bin/sh"}, "/bin/sh"},
		{nil, "/bin/sh"},
	} {
		exists := func(p string) bool { return slices.Contains(tc.exist, p) }
		if got := defaultShell(exists); got != tc.want {
			t.Errorf("with %v: defaultShell = %q, want %q", tc.exist, got, tc.want)
		}
	}
	t.Setenv("SHELL", "")
	// A HOME of its own, as startTestShell's: the login shell reads none of the developer's profile
	// files and writes its history there, and the test waits for it to exit (closeOnCleanup).
	home := t.TempDir()
	sess, err := StartLocal(LocalOptions{KubeconfigPaths: []string{writeUserKubeconfig(t)}, Dir: home,
		Env: []string{"HOME=" + home, "ENV=", "ZDOTDIR=" + home}})
	if err != nil {
		t.Fatalf("StartLocal without SHELL: %v", err)
	}
	closeOnCleanup(t, sess)
}
