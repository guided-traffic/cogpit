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
	"time"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// execPlugin makes a credential plugin whose program is dir/bin/cred-plugin and that hands out an
// ExecCredential with token.
func execPlugin(t *testing.T, dir, token string) *testkit.Plugin {
	t.Helper()
	p := testkit.NewPluginAt(t, filepath.Join(dir, "bin", "cred-plugin"))
	p.Give(token, time.Time{})
	return p
}

// execKubeconfig is a kubeconfig for srv whose user runs plugin as command (relative to the file's
// folder when it starts with ./) with args.
func execKubeconfig(srv *testkit.FakeAPIServer, contextName, command string, plugin *testkit.Plugin, args ...string) string {
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
      command: ` + testkit.YAMLQuote(command) + `
      args: [` + strings.Join(quoted, ", ") + `]
      interactiveMode: Never
      env:
` + plugin.EnvYAML("      ") + `contexts:
- name: ` + contextName + `
  context: {cluster: fake, user: plugin}
`
}

// TestConnectAsksTheExecPolicyBeforeThePluginRuns: a policy that refuses keeps the plugin from
// running and names the reason; one that allows lets it run and the connection use its token. The
// plugin the policy sees is the one InspectFile describes, fingerprint for fingerprint, including
// the relative command resolved against the kubeconfig's folder.
func TestConnectAsksTheExecPolicyBeforeThePluginRuns(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: secretToken})
	defer srv.Close()
	dir := t.TempDir()
	plugin := execPlugin(t, dir, secretToken)
	path := filepath.Join(dir, "kubeconfig")
	// client-go resolves a command against the kubeconfig's folder when it holds the system's
	// separator (Windows': a backslash).
	command := "." + string(filepath.Separator) + filepath.Join("bin", "cred-plugin")
	if err := os.WriteFile(path, []byte(execKubeconfig(srv, "plugged", command, plugin, "--flag", "x")), 0o600); err != nil {
		t.Fatal(err)
	}

	in, err := InspectFile(path)
	if err != nil {
		t.Fatal(err)
	}
	listed := in.Exec["plugged"]
	if listed == nil || listed.Command != plugin.Command || listed.File != path || listed.Server != srv.URL {
		t.Fatalf("InspectFile's plugin: %+v (want command %s)", listed, plugin.Command)
	}

	refusal := errors.New("not allowed in this test")
	var seen *kubeconfig.ExecPlugin
	_, err = Connect(context.Background(), Options{KubeconfigPaths: []string{path}, Context: "plugged",
		ExecPolicy: func(p *kubeconfig.ExecPlugin) error { seen = p; return refusal }})
	var ce *ConnectError
	if !errors.As(err, &ce) || !errors.Is(err, refusal) || !strings.Contains(err.Error(), "not allowed in this test") {
		t.Fatalf("Connect with a refusing policy: %v", err)
	}
	if n := plugin.Runs(); n != 0 {
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
	if n := plugin.Runs(); n < 1 {
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
