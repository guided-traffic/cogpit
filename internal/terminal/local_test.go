package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The local terminal's tests that hold on every system; the ones that start a shell are in
// localshell_test.go (Unix) and localpty_windows_test.go.

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

func readTrimmed(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return strings.TrimSpace(string(data))
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
		"unknown context": {KubeconfigPaths: []string{user}, Context: "nope", Shell: plainShell()},
		"bad namespace":   {KubeconfigPaths: []string{user}, Context: "ctx-a", Namespace: "Not_Valid", Shell: plainShell()},
		"no shell":        {KubeconfigPaths: []string{user}, Shell: "/no/such/shell"},
		"no context":      {KubeconfigPaths: []string{filepath.Join(t.TempDir(), "missing")}, Shell: plainShell()},
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

// A kubectl that is not a file, or not given as an absolute path, is refused before the shell
// starts.
func TestStartLocalRefusesABadKubectl(t *testing.T) {
	user := writeUserKubeconfig(t)
	for _, bad := range []string{binaryName(), filepath.Join(t.TempDir(), "missing", binaryName()), t.TempDir()} {
		sess, err := StartLocal(LocalOptions{KubeconfigPaths: []string{user}, Shell: plainShell(), Dir: t.TempDir(), Kubectl: bad})
		if err == nil {
			_ = sess.Close()
			t.Errorf("kubectl %q accepted", bad)
		}
	}
}
