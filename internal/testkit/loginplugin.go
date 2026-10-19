//go:build darwin || linux

package testkit

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// LoginPlugin is a credential plugin for tests of a login that waits (D-73): a shell script named
// login-plugin that notes each run and the number of each of its processes, and either hands out the
// ExecCredential the test gave it or, while Block is on, waits for ever as a browser login nobody
// finishes does (a loop of short sleeps, so that no child of it outlives it by more than 100 ms), or,
// while Stray is on, leaves a child that keeps its standard output open and exits (H-118).
// Kubeconfig is a file with one context that logs in with it against the fake API server.
type LoginPlugin struct {
	t          testing.TB
	Path       string // the plugin
	Kubeconfig string // a kubeconfig whose one context runs it
	dir        string
}

// NewLoginPlugin writes the plugin and a kubeconfig of context contextName that runs it against
// srv. The plugin hands out nothing until Give.
func NewLoginPlugin(t testing.TB, srv *FakeAPIServer, contextName string) *LoginPlugin {
	t.Helper()
	dir := t.TempDir()
	p := &LoginPlugin{t: t, dir: dir, Path: filepath.Join(dir, "login-plugin"), Kubeconfig: filepath.Join(dir, contextName+".yaml")}
	script := `#!/bin/sh
echo $$ >> "` + p.file("pids") + `"
echo run >> "` + p.file("runs") + `"
if [ -f "` + p.file("block") + `" ]; then
  echo "Log in in your browser to continue" >&2
  while :; do sleep 0.1; done
fi
if [ -f "` + p.file("stray") + `" ]; then
  (while [ -f "` + p.file("stray") + `" ]; do sleep 0.1; done) &
  exit 0
fi
cat "` + p.file("cred.json") + `"
`
	if err := os.WriteFile(p.Path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	doc := `apiVersion: v1
kind: Config
clusters:
- name: ` + contextName + `-cluster
  cluster:
    server: ` + srv.URL + `
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString(srv.CAData()) + `
users:
- name: ` + contextName + `-plugin
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: ` + p.Path + `
      interactiveMode: IfAvailable
contexts:
- name: ` + contextName + `
  context: {cluster: ` + contextName + `-cluster, user: ` + contextName + `-plugin}
`
	if err := os.WriteFile(p.Kubeconfig, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *LoginPlugin) file(name string) string { return filepath.Join(p.dir, name) }

// Give makes the plugin hand out token, expiring at exp (zero: no expiry).
func (p *LoginPlugin) Give(token string, exp time.Time) {
	p.t.Helper()
	status := `"token":"` + token + `"`
	if !exp.IsZero() {
		status += `,"expirationTimestamp":"` + exp.UTC().Format(time.RFC3339) + `"`
	}
	cred := `{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","status":{` + status + `}}`
	if err := os.WriteFile(p.file("cred.json"), []byte(cred), 0o600); err != nil {
		p.t.Fatal(err)
	}
}

// Block makes the plugin wait for ever from its next run on, or answer again.
func (p *LoginPlugin) Block(on bool) { p.t.Helper(); p.mode("block", on) }

// Stray makes the plugin's next runs leave a child that keeps the plugin's standard output open, and
// exit at once: client-go's run goes on without a process of Cogpit's to end (H-118), until
// Stray(false), which ends that child within 100 ms (as the test's end does).
func (p *LoginPlugin) Stray(on bool) { p.t.Helper(); p.mode("stray", on) }

// mode switches the plugin's file name on or off.
func (p *LoginPlugin) mode(name string, on bool) {
	p.t.Helper()
	if !on {
		if err := os.Remove(p.file(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			p.t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(p.file(name), nil, 0o600); err != nil {
		p.t.Fatal(err)
	}
}

// Runs is how often the plugin ran.
func (p *LoginPlugin) Runs() int {
	data, err := os.ReadFile(p.file("runs"))
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "run\n")
}

// LastPID is the number of the plugin's latest process; 0 before its first run.
func (p *LoginPlugin) LastPID() int {
	data, err := os.ReadFile(p.file("pids"))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	pid, _ := strconv.Atoi(fields[len(fields)-1])
	return pid
}

// ProcessGone reports whether process pid has ended and was reaped.
func ProcessGone(pid int) bool { return pid > 0 && errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }
