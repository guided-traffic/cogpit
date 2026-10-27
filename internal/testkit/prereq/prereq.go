// Package prereq holds what a test needs of the machine it runs on: a program (clang, a shell,
// perl, ping, PowerShell), a framework or DLL a make target fetches, a window server. Without it
// the test skips on a developer's machine and fails in CI, where the job that runs the test
// provides it: CI runs go test without -v, so a skip there would read as a pass.
//
// It imports nothing of Cogpit's, so the tests of every package may use it, the window's included.
// For tests only (depguard's no-testkit-in-product).
package prereq

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
)

// CI reports whether the environment is CI's: GitHub's runners set CI=true.
func CI() bool {
	ci, _ := strconv.ParseBool(os.Getenv("CI"))
	return ci
}

// Need skips the test when ok is false, or fails it in CI. what names the prerequisite and, where
// that helps, how to get it ("Sparkle.framework in .cache/sparkle: make sparkle"). A prerequisite
// that no CI job running the test can promise is a plain t.Skip with its reason beside it instead.
func Need(t testing.TB, ok bool, what string) {
	t.Helper()
	switch {
	case ok:
	case CI():
		t.Fatalf("needs %s, which CI provides for this test (CI=%s): a skip would pass unseen", what, os.Getenv("CI"))
	default:
		t.Skipf("needs %s", what)
	}
}

// LookPath is exec.LookPath for a program the test needs: its path, or Need's skip or failure.
func LookPath(t testing.TB, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	Need(t, err == nil, name+" on PATH")
	return path
}
