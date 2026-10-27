package cluster

import (
	"os/exec"
	"testing"

	"github.com/guided-traffic/cogpit/internal/testkit/prereq"
)

// The Toolhelp search on Windows itself (pluginproc_windows.go): a child started after the run's
// begin is found by its program's file name with its start time, may be ended, and is gone once
// TerminateProcess ended it.
func TestWindowsChildIsFoundAndEnded(t *testing.T) {
	ping := prereq.LookPath(t, "ping")
	command := []string{ping, "-n", "900", "127.0.0.1"}
	before := childSet()
	cmd := exec.Command(command[0], command[1:]...) // #nosec G204 -- the test's own command
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	id, n := findPlugin(command, before)
	if n != 1 || id.pid != cmd.Process.Pid || id.start == 0 {
		t.Fatalf("found %+v (%d), the child is %d", id, n, cmd.Process.Pid)
	}
	if again, _ := findPlugin(command, childSet()); again != (procID{}) {
		t.Errorf("a child from before the run's begin was taken: %+v", again)
	}
	if !alive(id) || !canSignal(id.pid) {
		t.Fatalf("the child: alive %v, may be ended %v", alive(id), canSignal(id.pid))
	}
	if err := signalPlugin(id.pid, false); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if alive(id) {
		t.Error("the child is listed after TerminateProcess ended it")
	}
}
