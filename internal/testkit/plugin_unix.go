//go:build unix

package testkit

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// exeSuffix is what os/exec adds to a program's name: nothing here.
const exeSuffix = ""

// programNamed makes the program name of a plugin in a folder of the test's own and returns its
// path.
func programNamed(t testing.TB, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	programAt(t, path)
	return path
}

// programAt makes the test binary the program at path: a symbolic link, which the process runs
// under (its command line names the link), and which the system starts without the look it gives a
// new file on its first start (macOS: about a second).
func programAt(t testing.TB, path string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, path); err != nil {
		t.Fatal(err)
	}
}

// removePrograms has nothing to do here: the links go with their tests' folders.
func removePrograms() {}

// ProcessGone reports whether process pid has ended and was reaped.
func ProcessGone(pid int) bool { return pid > 0 && errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }
