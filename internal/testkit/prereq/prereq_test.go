package prereq

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recorder is a testing.TB whose Skipf and Fatalf record their message and return, so that a test
// reads what Need decided.
type recorder struct {
	testing.TB
	skipped, failed string
}

func (r *recorder) Helper() {}

func (r *recorder) Skipf(format string, args ...any) { r.skipped = fmt.Sprintf(format, args...) }

func (r *recorder) Fatalf(format string, args ...any) { r.failed = fmt.Sprintf(format, args...) }

// TestNeed: a prerequisite that is there changes nothing; a missing one skips the test, and fails
// it where CI is set to a true value (GitHub's runners set CI=true).
func TestNeed(t *testing.T) {
	for _, c := range []struct {
		ci         string
		ok         bool
		skip, fail bool
	}{
		{ci: "", ok: true},
		{ci: "true", ok: true},
		{ci: "", skip: true},
		{ci: "false", skip: true},
		{ci: "woodpecker", skip: true},
		{ci: "true", fail: true},
		{ci: "1", fail: true},
	} {
		t.Setenv("CI", c.ci)
		r := &recorder{TB: t}
		Need(r, c.ok, "clang on PATH")
		if (r.skipped != "") != c.skip || (r.failed != "") != c.fail {
			t.Errorf("CI=%q, ok %v: skipped %q, failed %q", c.ci, c.ok, r.skipped, r.failed)
		}
		if c.skip && r.skipped != "needs clang on PATH" {
			t.Errorf("CI=%q: skipped with %q", c.ci, r.skipped)
		}
		if c.fail && !strings.HasPrefix(r.failed, "needs clang on PATH, which CI provides") {
			t.Errorf("CI=%q: failed with %q", c.ci, r.failed)
		}
	}
}

// TestLookPath: a program on PATH is found; one that is not is Need's.
func TestLookPath(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir, name := filepath.Split(exe)
	t.Setenv("PATH", dir)
	t.Setenv("CI", "true")
	r := &recorder{TB: t}
	if got := LookPath(r, name); got != filepath.Join(dir, name) || r.failed != "" {
		t.Errorf("LookPath(%q) = %q, failed %q", name, got, r.failed)
	}
	if got := LookPath(r, "cogpit-no-such-program"); got != "" || !strings.HasPrefix(r.failed, "needs cogpit-no-such-program on PATH") {
		t.Errorf("a missing program: %q, failed %q", got, r.failed)
	}
}
