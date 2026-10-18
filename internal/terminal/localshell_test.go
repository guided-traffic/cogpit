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

// userKubeconfig is a user's kubeconfig with a credential the terminal's own file must not copy.
// The server is unreachable on purpose: these tests only read and change kubeconfig state.
const userKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: cluster-a
  cluster:
    server: https://127.0.0.1:1
- name: cluster-b
  cluster:
    server: https://127.0.0.1:2
users:
- name: user-a
  user:
    token: secret-token-123
- name: user-b
  user:
    token: secret-token-456
contexts:
- name: ctx-a
  context:
    cluster: cluster-a
    user: user-a
    namespace: default
- name: ctx-b
  context:
    cluster: cluster-b
    user: user-b
current-context: ctx-b
`

func writeUserKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(userKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

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

func readTrimmed(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return strings.TrimSpace(string(data))
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

func TestStartLocalUsesCurrentContextAndItsNamespace(t *testing.T) {
	b, err := resolveLocal(LocalOptions{KubeconfigPaths: []string{writeUserKubeconfig(t)}})
	if err != nil {
		t.Fatal(err)
	}
	if b.context.Name != "ctx-b" || b.context.Cluster != "cluster-b" || b.context.User != "user-b" || b.context.Namespace != "" {
		t.Errorf("binding = %+v, want ctx-b/cluster-b/user-b without namespace", b.context)
	}
	b, err = resolveLocal(LocalOptions{KubeconfigPaths: []string{writeUserKubeconfig(t)}, Context: "ctx-a"})
	if err != nil {
		t.Fatal(err)
	}
	if b.context.Namespace != "default" {
		t.Errorf("namespace = %q, want the context's own (default)", b.context.Namespace)
	}
}

func TestStartLocalErrors(t *testing.T) {
	user := writeUserKubeconfig(t)
	cases := map[string]LocalOptions{
		"unknown context": {KubeconfigPaths: []string{user}, Context: "nope", Shell: "/bin/sh"},
		"bad namespace":   {KubeconfigPaths: []string{user}, Context: "ctx-a", Namespace: "Not_Valid", Shell: "/bin/sh"},
		"no shell":        {KubeconfigPaths: []string{user}, Shell: "/no/such/shell"},
		"no context":      {KubeconfigPaths: []string{filepath.Join(t.TempDir(), "missing")}, Shell: "/bin/sh"},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			sess, err := StartLocal(opts)
			if err == nil {
				_ = sess.Close()
				t.Fatal("StartLocal succeeded")
			}
			if strings.Contains(err.Error(), "secret-token") {
				t.Errorf("error leaks a credential: %v", err)
			}
		})
	}
	// No terminal kubeconfig is left behind by a failed start.
	left, _ := filepath.Glob(filepath.Join(os.TempDir(), "cogpit-terminal-*.kubeconfig"))
	for _, f := range left {
		if data, err := os.ReadFile(f); err == nil && strings.Contains(string(data), `"nope"`) {
			t.Errorf("failed start left %s behind", f)
		}
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
	sess, err := StartLocal(LocalOptions{KubeconfigPaths: []string{writeUserKubeconfig(t)}, Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("StartLocal without SHELL: %v", err)
	}
	_ = sess.Close()
}
