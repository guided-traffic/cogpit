package cluster

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// execPluginScript writes an exec credential plugin into dir: a shell script that appends a line
// to the file $COGPIT_TEST_MARKER (so a test sees whether it ran) and prints an ExecCredential
// with token. It returns the script's path.
func execPluginScript(t *testing.T, dir, token string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the test plugin is a shell script")
	}
	p := filepath.Join(dir, "bin", "cred-plugin")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho ran >> \"$COGPIT_TEST_MARKER\"\n" +
		`echo '{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","status":{"token":"` + token + `"}}'` + "\n"
	if err := os.WriteFile(p, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

// execKubeconfig is a kubeconfig for srv whose user runs command (relative to the file's folder
// when it starts with ./) with args, telling it to mark marker.
func execKubeconfig(srv *testkit.FakeAPIServer, contextName, command, marker string, args ...string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = fmt.Sprintf("%q", a)
	}
	return `apiVersion: v1
kind: Config
clusters:
- name: fake
  cluster:
    server: ` + srv.URL + `
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString(srv.CAData()) + `
users:
- name: plugin
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: ` + command + `
      args: [` + strings.Join(quoted, ", ") + `]
      interactiveMode: Never
      env:
      - name: COGPIT_TEST_MARKER
        value: ` + marker + `
contexts:
- name: ` + contextName + `
  context: {cluster: fake, user: plugin}
`
}

func ran(t *testing.T, marker string) int {
	t.Helper()
	data, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "ran")
}

// TestConnectAsksTheExecPolicyBeforeThePluginRuns: a policy that refuses keeps the plugin from
// running and names the reason; one that allows lets it run and the connection use its token. The
// plugin the policy sees is the one InspectFile describes, fingerprint for fingerprint, including
// the relative command resolved against the kubeconfig's folder.
func TestConnectAsksTheExecPolicyBeforeThePluginRuns(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	dir := t.TempDir()
	script := execPluginScript(t, dir, secretToken)
	marker := filepath.Join(dir, "marker")
	path := filepath.Join(dir, "kubeconfig")
	if err := os.WriteFile(path, []byte(execKubeconfig(srv, "plugged", "./bin/cred-plugin", marker, "--flag", "x")), 0o600); err != nil {
		t.Fatal(err)
	}

	in, err := InspectFile(path)
	if err != nil {
		t.Fatal(err)
	}
	listed := in.Exec["plugged"]
	if listed == nil || listed.Command != script || listed.File != path || listed.Server != srv.URL {
		t.Fatalf("InspectFile's plugin: %+v (want command %s)", listed, script)
	}

	refusal := errors.New("not allowed in this test")
	var seen *kubeconfig.ExecPlugin
	_, err = Connect(context.Background(), Options{KubeconfigPaths: []string{path}, Context: "plugged",
		ExecPolicy: func(p *kubeconfig.ExecPlugin) error { seen = p; return refusal }})
	var ce *ConnectError
	if !errors.As(err, &ce) || !errors.Is(err, refusal) || !strings.Contains(err.Error(), "not allowed in this test") {
		t.Fatalf("Connect with a refusing policy: %v", err)
	}
	if n := ran(t, marker); n != 0 {
		t.Fatalf("the plugin ran %d times although the policy refused it", n)
	}
	if seen == nil || seen.Fingerprint() != listed.Fingerprint() {
		t.Fatalf("the policy saw %+v, InspectFile listed %+v", seen, listed)
	}

	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{path}, Context: "plugged",
		ExecPolicy: func(*kubeconfig.ExecPlugin) error { return nil }})
	if err != nil {
		t.Fatalf("Connect with an allowing policy: %v", err)
	}
	defer c.Close()
	if n := ran(t, marker); n < 1 {
		t.Fatal("the plugin did not run")
	}

	// No policy: kubectl's behaviour, the plugin runs.
	c2, err := Connect(context.Background(), Options{KubeconfigPaths: []string{path}, Context: "plugged"})
	if err != nil {
		t.Fatalf("Connect without a policy: %v", err)
	}
	c2.Close()
}

// TestConnectPolicyIgnoresUsersWithoutAPlugin: a token user never reaches the policy.
func TestConnectPolicyIgnoresUsersWithoutAPlugin(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	path := srv.WriteKubeconfig(t, "fake", "")
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{path},
		ExecPolicy: func(*kubeconfig.ExecPlugin) error { return errors.New("asked for a token user") }})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}

func TestInspectFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, "two", kubeconfigFor("alpha", "https://a.example", "team-a"))
	in, err := InspectFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Contexts) != 1 || in.Contexts[0].Name != "alpha" || len(in.Exec) != 0 || in.Path != path {
		t.Errorf("InspectFile = %+v", in)
	}

	// A command on PATH stays a name; a relative path with a separator resolves against the file
	// (the system's separator: ..\tools\login on Windows).
	onPath := filepath.Join(dir, "path.yaml")
	cfg := "users:\n- name: u\n  user:\n    exec: {command: aws, apiVersion: client.authentication.k8s.io/v1beta1}\n" +
		"- name: v\n  user:\n    exec: {command: " + filepath.FromSlash("../tools/login") + ", apiVersion: client.authentication.k8s.io/v1beta1}\n" +
		"contexts:\n- name: a\n  context: {user: u}\n- name: b\n  context: {user: v}\n"
	if err := os.WriteFile(onPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	in, err = InspectFile(onPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := in.Exec["a"].Command; got != "aws" {
		t.Errorf("PATH command = %q", got)
	}
	if got, want := in.Exec["b"].Command, filepath.Join(filepath.Dir(dir), "tools", "login"); got != want {
		t.Errorf("relative command = %q, want %q", got, want)
	}

	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, []byte(strings.Repeat("#", kubeconfig.MaxFileSize+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	broken := writeFile(t, "broken", "users:\n- name: u\n  user:\n    token: "+secretToken+"\n- name: u\n  user:\n    token: x\n")
	for name, p := range map[string]string{
		"missing": filepath.Join(dir, "nope"), "folder": dir, "too large": big, "duplicate users": broken,
	} {
		_, err := InspectFile(p)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if strings.Contains(err.Error(), secretToken) {
			t.Errorf("%s: the error leaks the token: %v", name, err)
		}
	}
}

func TestListSyncFolder(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := write("a.yaml", "x")
	hidden := write(".hidden", "x") // Lens reads dot files, and so does Cogpit (2026-10-12)
	write(".DS_Store", "x")
	write("b.yaml~", "x")
	write("huge", strings.Repeat("#", kubeconfig.MaxFileSize+1))
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	want := ".hidden,a.yaml"
	if runtime.GOOS != "windows" { // symlinks need a privilege there
		target := writeFile(t, "elsewhere", "x")
		link := filepath.Join(dir, "linked")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "dangling")); err != nil {
			t.Fatal(err)
		}
		want += ",linked"
	}
	got, err := ListSyncFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range got {
		names = append(names, filepath.Base(f.Path))
		if f.Size == 0 || f.ModTime == 0 {
			t.Errorf("%s: no size or time", f.Path)
		}
	}
	if strings.Join(names, ",") != want || got[0].Path != hidden || got[1].Path != a {
		t.Errorf("ListSyncFolder = %v", names)
	}
	if _, err := ListSyncFolder(filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing folder is not an error")
	}
}

func TestLookPathNow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	if got := LookPathNow("sh"); !filepath.IsAbs(got) {
		t.Errorf("LookPathNow(sh) = %q", got)
	}
	if got := LookPathNow("cogpit-no-such-plugin-x"); got != "" {
		t.Errorf("a missing command was found at %q", got)
	}
	if LookPathNow("") != "" {
		t.Error("an empty command was found")
	}
}
