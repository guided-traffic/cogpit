//go:build darwin || linux

package cluster

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// startChild starts a child of the test's that ends with the test; setpgid puts it in a process
// group of its own, as a terminal's shell is.
func startChild(t *testing.T, setpgid bool, name string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: setpgid}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

// WF38: a wrapper that replaces itself with the login program (exec) is found as the child that
// appeared at the run's begin, and keeps its identity (number and start time) after the exec; a
// child older than the run is not taken for it. The wrapper is the one new child, so the search
// decides the same at any pace (a shell in a group of its own: TestSearchTakesTheChildInCogpitsGroup).
func TestSearchFindsAWrapperAfterItsExec(t *testing.T) {
	quietSearches(t)
	older := startChild(t, false, "/bin/sleep", "900")
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "wrapper")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec /bin/sleep 900\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := []string{wrapper, "login"}
	stop := make(chan struct{})
	defer close(stop)
	s := newProcSearch(time.Now(), command, childSet())
	defer func() { s.finished(time.Now()) }()
	go s.run(stop)
	plugin := startChild(t, false, wrapper, "login")
	select {
	case <-s.done:
	case <-time.After(30 * time.Second):
		t.Fatal("the search did not decide")
	}
	if s.found.pid != plugin.Process.Pid {
		t.Fatalf("the search found %+v; the wrapper is %d, the older child %d", s.found, plugin.Process.Pid, older.Process.Pid)
	}
	// macOS may take a while before it runs a new script the first time: wait for the exec.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool { argv := commandLine(plugin.Process.Pid); return len(argv) > 0 && argv[0] == "/bin/sleep" }) {
		t.Fatalf("the wrapper did not replace itself: %v", commandLine(plugin.Process.Pid))
	}
	if !alive(s.found) {
		t.Error("the wrapper's identity changed with its exec")
	}
	target, why := s.locate(stop)
	if why != "" || target != s.found {
		t.Errorf("locate: %+v %q, want the wrapper", target, why)
	}
	_ = plugin.Process.Kill()
	_ = plugin.Wait()
	if alive(s.found) {
		t.Error("an ended child still counts as alive")
	}
}

// The plugin's process is found among Cogpit's children by its command line, a #! script's too.
func TestFindPluginByItsCommandLine(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "find-me")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile :; do sleep 0.1; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(script, "oidc", "--issuer=https://issuer.example")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	want := cmd.Process.Pid
	if !testkit.WaitFor(waitCtx(t, 5*time.Second), func() bool {
		id, n := findPlugin([]string{script, "oidc", "--issuer=https://issuer.example"}, nil)
		return n == 1 && id.pid == want
	}) {
		t.Fatalf("the plugin's process %d was not found among %+v", want, listChildren())
	}
	if id, n := findPlugin([]string{script, "oidc", "--issuer=https://other.example"}, nil); n != 0 {
		t.Errorf("another command line found process %d", id.pid)
	}
	// A plugin of the same name elsewhere is another context's, or another test's (CI run
	// 38975936330: a cancel's search ended the next test's plugin).
	if id, n := findPlugin([]string{filepath.Join(t.TempDir(), "find-me"), "oidc", "--issuer=https://issuer.example"}, nil); n != 0 {
		t.Errorf("a program of the same name in another folder found process %d", id.pid)
	}
	// A process that ran before the run began is not the run's (findPlugin's before).
	if _, n := findPlugin([]string{script, "oidc", "--issuer=https://issuer.example"}, childSet()); n != 0 {
		t.Error("a process older than the run was taken for its plugin")
	}
}

// Cogpit's children carry their start time and whether they are in its process group.
func TestListChildren(t *testing.T) {
	same := startChild(t, false, "/bin/sleep", "900")
	own := startChild(t, true, "/bin/sleep", "900")
	got := map[int]childProcess{}
	for _, c := range listChildren() {
		got[c.id.pid] = c
	}
	a, ok1 := got[same.Process.Pid]
	b, ok2 := got[own.Process.Pid]
	if !ok1 || !ok2 {
		t.Fatalf("the children %d and %d are not among %+v", same.Process.Pid, own.Process.Pid, got)
	}
	if !a.group || b.group {
		t.Errorf("process groups: %v for the child in the test's group, %v for the one in its own", a.group, b.group)
	}
	if a.id.start == 0 || b.id.start == 0 || a.id.start > b.id.start {
		t.Errorf("start times: %d, then %d", a.id.start, b.id.start)
	}
}

// On the system's own processes: a child in a process group of its own (a terminal's shell) is not
// taken for the run's, the one new child in Cogpit's group is. Both are started, and seen in their
// groups, before the search looks, so its looks see the same at any pace
// (TestSearchCountsTheGroupAChildEndsIn drives a child that leaves the group during the search).
func TestSearchTakesTheChildInCogpitsGroup(t *testing.T) {
	quietSearches(t)
	before := childSet()
	shell := startChild(t, true, "/bin/sleep", "900")
	plugin := startChild(t, false, "/bin/sleep", "900")
	if !testkit.WaitFor(waitCtx(t, 30*time.Second), func() bool {
		var shellOwn, pluginOurs bool
		for _, c := range listChildren() {
			shellOwn = shellOwn || (c.id.pid == shell.Process.Pid && !c.group)
			pluginOurs = pluginOurs || (c.id.pid == plugin.Process.Pid && c.group)
		}
		return shellOwn && pluginOurs
	}) {
		t.Fatalf("the children are not in their groups: %+v", listChildren())
	}
	stop := make(chan struct{})
	defer close(stop)
	s := newProcSearch(time.Now(), []string{"/no/such/plugin", "login"}, before)
	defer func() { s.finished(time.Now()) }()
	go s.run(stop)
	select {
	case <-s.done:
	case <-time.After(30 * time.Second):
		t.Fatal("the search did not decide")
	}
	if s.found.pid != plugin.Process.Pid {
		t.Fatalf("the search found %+v; the plugin is %d, the shell in its own group %d", s.found, plugin.Process.Pid, shell.Process.Pid)
	}
}

// A child that appears only after the search looked for searchWindow and saw none (client-go
// started the plugin late, on a loaded system) is the run's: the window runs from the first new
// child. A cancel in the meantime does not wait for it: the plugin may be one Cogpit cannot see.
func TestSearchTakesAChildThatAppearsLate(t *testing.T) {
	quietSearches(t)
	stop := make(chan struct{})
	defer close(stop)
	s := newProcSearch(time.Now(), []string{"/no/such/plugin", "login"}, childSet())
	defer func() { s.finished(time.Now()) }()
	go s.run(stop)
	select {
	case <-s.late:
	case <-time.After(30 * time.Second):
		t.Fatal("the search never said it saw no new child within its window")
	}
	located := make(chan string, 1)
	go func() { _, why := s.locate(stop); located <- why }()
	select {
	case why := <-located:
		if why != whyNotFound {
			t.Errorf("locate before the plugin appeared: %q, want %q", why, whyNotFound)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("locate waited for a plugin that had not appeared")
	}
	plugin := startChild(t, false, "/bin/sleep", "900")
	select {
	case <-s.done:
	case <-time.After(30 * time.Second):
		t.Fatal("the search did not decide")
	}
	if s.found.pid != plugin.Process.Pid {
		t.Fatalf("the search found %+v; the plugin is %d", s.found, plugin.Process.Pid)
	}
	if target, why := s.locate(stop); why != "" || target != s.found {
		t.Errorf("locate: %+v %q, want the plugin", target, why)
	}
}
