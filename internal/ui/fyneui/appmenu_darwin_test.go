//go:build darwin

package fyneui

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/testkit/prereq"
)

// macOS's application menu has one About Cogpit, which calls Cogpit's dialog instead of AppKit's
// standard panel and keeps its place when Fyne builds the menu bar again (A60): appmenu_darwin.m
// itself, built with a native harness into a process of its own and run. Needs clang.
func TestAppMenuAbout(t *testing.T) {
	clang := prereq.LookPath(t, "clang")
	bin := filepath.Join(t.TempDir(), "appmenu_harness")
	build := exec.Command(clang, "-fno-objc-arc", "-framework", "Cocoa", "-I.", "-o", bin,
		filepath.Join("testdata", "appmenu_harness.m"), "appmenu_darwin.m")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the harness: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("the harness: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimSpace(string(out)), "\n")
	want := []string{
		"nomenu|0",                       // no menu bar yet: nothing to install into
		"glfw|About cogpit|1|6|1|0",      // GLFW's item, AppKit's standard panel
		"installed|About Cogpit|1|6|0|0", // the same item, Cogpit's title, Cogpit's action
		"rebuilt|About Cogpit|1|7|0|0",   // Fyne's rebuild with Preferences… keeps it first
		"again|About Cogpit|1|7|0|0",     // a second install finds it: still one
		"chosen|About Cogpit|1|7|0|1",    // choosing it calls cogpitAboutChosen
		"inserted|About Cogpit|1|5|0|1",  // a menu without one gets it at the top
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the harness said\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// AppKit's report that About Cogpit was chosen opens the shell's dialog on the main goroutine; before
// installAbout named a shell it does nothing. The test app has no menu bar: installAbout only warns.
func TestAboutChosenShowsTheDialog(t *testing.T) {
	t.Cleanup(func() { aboutShell.Store(nil) })
	aboutShell.Store(nil)
	cogpitAboutChosen()

	sh, w, q := newTestShell(t, newFakeEngine(t), Options{})
	sh.installAbout()
	if aboutShell.Load() != sh {
		t.Fatal("installAbout did not name the shell")
	}
	cogpitAboutChosen()
	q.until(t, "the About dialog", func() bool { return slices.Contains(dialogTexts(w), aboutTagline) })
}
