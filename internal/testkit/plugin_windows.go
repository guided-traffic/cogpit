package testkit

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// exeSuffix is what os/exec adds to a program's name that has none (PATHEXT's first that exists).
const exeSuffix = ".exe"

// The programs on Windows: Cogpit finds a plugin's process by its program's file name
// (pluginproc_windows.go in internal/cluster), and a process started through a symbolic link to
// the test binary has the binary's name, so a program is a copy of the binary under its own name.
// A plugin of a name has one copy per test process, in a folder Main removes once the tests ran; a
// copy at a test's place goes with the test's folder. Each copy is started once when it is made
// (roleWarm), so that the system's first look at a new program, its malware scan, falls on no
// test's timing.
var programs struct {
	sync.Mutex
	dir   string          // the test process's folder of copies; empty until the first
	names map[string]bool // the names copied there
}

// programNamed makes the program name of a plugin in the test process's folder of copies and
// returns its path, without .exe.
func programNamed(t testing.TB, name string) string {
	t.Helper()
	programs.Lock()
	defer programs.Unlock()
	if programs.dir == "" {
		dir, err := os.MkdirTemp("", "cogpit-test-plugins-")
		if err != nil {
			t.Fatal(err)
		}
		programs.dir, programs.names = dir, map[string]bool{}
	}
	path := filepath.Join(programs.dir, name)
	if !programs.names[name] {
		programAt(t, path+exeSuffix)
		programs.names[name] = true
	}
	return path
}

// programAt copies the test binary to path and starts the copy once.
func programAt(t testing.TB, path string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := copyFile(exe, path); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(path)
	cmd.Env = append(os.Environ(), roleEnv+"="+roleWarm)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the plugin's program %s did not start: %v\n%s", path, err, out)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// removePrograms removes the folder of copies once no plugin runs any more: a stray child of the
// last test ends 50 ms after its files.
func removePrograms() {
	programs.Lock()
	defer programs.Unlock()
	if programs.dir == "" {
		return
	}
	for range 50 {
		if err := os.RemoveAll(programs.dir); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ProcessGone reports whether process pid has ended: no process has its number, or the one that has
// it exited (a handle someone holds keeps an ended process's number).
func ProcessGone(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	event, err := windows.WaitForSingleObject(h, 0)
	return err == nil && event == windows.WAIT_OBJECT_0
}
