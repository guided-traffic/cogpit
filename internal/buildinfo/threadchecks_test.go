package buildinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTheThreadCheckTag: the Makefile's FYNE_THREAD_CHECKS=off passes FYNE_NOCHECK_TAG,
// threadchecks_off.go is built with it, and Fyne still turns its thread check off for an app whose
// metadata declares the fyneDo migration, as such a build does (D-103). A tag no file knows is
// silently ignored: a release would keep the check and its cost and report "off" though it is on.
// A Fyne upgrade (Renovate brings them) that stops reading the migration fails here, not in the
// owner's scrolling.
func TestTheThreadCheckTag(t *testing.T) {
	mk, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^FYNE_NOCHECK_TAG\s*:?=\s*(\w+)\s*$`).FindSubmatch(mk)
	if m == nil {
		t.Fatal("the Makefile has no FYNE_NOCHECK_TAG")
	}
	tag := string(m[1])

	for file, want := range map[string]string{"threadchecks_off.go": tag, "threadchecks_on.go": "!" + tag} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if c := regexp.MustCompile(`(?m)^//go:build (\S+)$`).FindSubmatch(b); c == nil || string(c[1]) != want {
			t.Errorf("%s: build constraint %q, want %q", file, c, want)
		}
	}

	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "fyne.io/fyne/v2").Output()
	if err != nil {
		t.Skipf("go list -m fyne.io/fyne/v2: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "internal", "build", "build.go"))
	if err != nil {
		t.Fatalf("Fyne's build package: %v", err)
	}
	if !strings.Contains(string(b), `Metadata().Migrations["fyneDo"]`) || !strings.Contains(string(b), "func MigratedToFyneDo() bool") {
		t.Errorf("Fyne's internal/build no longer reads the fyneDo migration from the app's metadata: the release's speed is gone, see D-103")
	}
}
