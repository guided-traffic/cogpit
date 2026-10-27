//go:build darwin

package fyneui

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/testkit/prereq"
)

// terminatedWindow is a window after GLFW terminated: asking for its native handle panics, as
// GLFW's GetCocoaWindow does then.
type terminatedWindow struct{ fyne.Window }

func (terminatedWindow) RunNative(func(any)) {
	panic("NotInitialized: The GLFW library is not initialized")
}

// The shell stops the title bar after the app's run ended; that must not ask GLFW for the window
// (every quit panicked there, 2026-10-12).
func TestTitlebarStopAfterTheRunEnded(t *testing.T) {
	test.NewTempApp(t)
	tb := newTitlebar(&shell{win: terminatedWindow{test.NewTempWindow(t, nil)}})
	tb.window = 1
	titlebars.Store(tb.id, tb)

	tb.stop()

	if _, ok := titlebars.Load(tb.id); ok || tb.window != 0 {
		t.Errorf("still registered %v, window %#x", ok, tb.window)
	}
}

// The window buttons sit centred in Cogpit's 40-point top bar, not where AppKit's own title bar
// has them (owner, 2026-10-13: "too high"), and stay there when AppKit lays the title bar out
// again: titlebar_darwin.m itself, built with a native harness into a process of its own (a window
// needs its process's main thread) and run. Their x stays AppKit's. Needs clang and a window
// server.
func TestTitlebarButtonsCentred(t *testing.T) {
	clang := prereq.LookPath(t, "clang")
	bin := filepath.Join(t.TempDir(), "titlebar_harness")
	build := exec.Command(clang, "-fno-objc-arc", "-framework", "Cocoa", "-I.", "-o", bin,
		filepath.Join("testdata", "titlebar_harness.m"), "titlebar_darwin.m")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the harness: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).Output()
	var exit *exec.ExitError
	prereq.Need(t, !errors.As(err, &exit) || exit.ExitCode() != 3, "a window server (AppKit gave the window no buttons)")
	if err != nil {
		t.Fatalf("the harness: %v\n%s", err, out)
	}
	steps := map[string][4]float64{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var got, topLeft int
		if _, err := fmt.Sscanf(line, "placement %d %d", &got, &topLeft); err == nil {
			// A resize keeps the last frame where it was drawn, not scaled (the text flickered).
			if got != topLeft {
				t.Errorf("the content view's layer contents placement is %d, want top left (%d)", got, topLeft)
			}
			continue
		}
		var name string
		var v [4]float64
		if _, err := fmt.Sscan(line, &name, &v[0], &v[1], &v[2], &v[3]); err != nil {
			t.Fatalf("a line of the harness %q: %v", line, err)
		}
		steps[name] = v
	}
	appkit := steps["appkit"]
	for _, step := range []string{"centred", "resized", "restyled"} {
		got, ok := steps[step]
		if !ok {
			t.Fatalf("no %s step in %q", step, out)
		}
		if got[0] != 20 || got[2] != 20 {
			t.Errorf("%s: the buttons' centres %v and %v points from the top, want 20 (AppKit: %v)", step, got[0], got[2], appkit[0])
		}
		if got[1] != appkit[1] || got[3] != appkit[3] {
			t.Errorf("%s: the buttons moved sideways: x %v and %v, AppKit %v and %v", step, got[1], got[3], appkit[1], appkit[3])
		}
	}
}
