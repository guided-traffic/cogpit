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
// child in a process group of its own (a terminal's shell) that starts alongside is not taken for
// it, and neither is a child older than the run.
func TestSearchFindsAWrapperAfterItsExec(t *testing.T) {
	time.Sleep(searchWindow) // no other test's run began within the search's window
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
	shell := startChild(t, true, "/bin/sleep", "900")
	plugin := startChild(t, false, wrapper, "login")
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the search did not decide")
	}
	if s.found.pid != plugin.Process.Pid {
		t.Fatalf("the search found %+v; the wrapper is %d, the shell %d, the older child %d", s.found, plugin.Process.Pid,
			shell.Process.Pid, older.Process.Pid)
	}
	// macOS may take a while before it runs a new script the first time: wait for the exec.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
