//go:build darwin

package fyneui

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/testkit/prereq"
)

// The window's place on macOS (D-95): windowplace_darwin.m itself, built with a native harness into
// a process of its own (a window needs its process's main thread) and run. It describes the
// displays, measures GLFW's title bar, gives the window its frame exactly, reports each move and
// resize, keeps the frame before full screen through full screen (AppKit's resizes between the
// notifications are not the place), takes one observer however often it is installed, and stops
// at the window's close. Needs clang and a window server.
func TestWindowPlaceFollowsTheWindow(t *testing.T) {
	clang := prereq.LookPath(t, "clang")
	bin := filepath.Join(t.TempDir(), "windowplace_harness")
	build := exec.Command(clang, "-fno-objc-arc", "-framework", "Cocoa", "-I.", "-o", bin,
		filepath.Join("testdata", "windowplace_harness.m"), "windowplace_darwin.m")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the harness: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).Output()
	var exit *exec.ExitError
	prereq.Need(t, !errors.As(err, &exit) || exit.ExitCode() != 3, "a window server (AppKit described no display)")
	if err != nil {
		t.Fatalf("the harness: %v\n%s", err, out)
	}
	lines := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, rest, _ := strings.Cut(line, " ")
		lines[name] = rest
	}
	if _, ok := lines["wrong-id"]; ok {
		t.Errorf("a report under another id: %s", out)
	}

	var n int
	var x, y, w, h float64
	var primary uint32
	if _, err := fmt.Sscan(lines["screens"], &n, &x, &y, &w, &h, &primary); err != nil || n < 1 || x != 0 || y != 0 || w <= 0 || h <= 0 {
		t.Errorf("screens %q (%v): want the primary display first, at 0,0", lines["screens"], err)
	}
	var tb, measured float64
	if _, err := fmt.Sscan(lines["titlebar"], &tb, &measured); err != nil || tb <= 0 || tb != measured {
		t.Errorf("titlebar %q (%v): want GLFW's window's title bar, measured the same", lines["titlebar"], err)
	}
	if lines["followed"] != "120 140 1000 640" {
		t.Errorf("followed %q: the window's frame, want exactly 120 140 1000 640", lines["followed"])
	}

	type report struct {
		count      int
		frame      [4]float64
		fullScreen int
		display    uint32
	}
	want := map[string]report{
		"installed": {count: 0},
		"moved":     {count: 1, frame: [4]float64{200, 150, 1000, 640}},
		"resized":   {count: 1, frame: [4]float64{200, 150, 1100, 700}},
		"entered":   {count: 1, frame: [4]float64{200, 150, 1100, 700}, fullScreen: 1},
		"inside":    {count: 0, frame: [4]float64{200, 150, 1100, 700}, fullScreen: 1},
		"exited":    {count: 1, frame: [4]float64{210, 160, 1100, 700}},
		"again":     {count: 1, frame: [4]float64{230, 170, 1100, 700}},
		"closed":    {count: 0, frame: [4]float64{230, 170, 1100, 700}},
	}
	for _, step := range []string{"installed", "moved", "resized", "entered", "inside", "exited", "again", "closed"} {
		var got report
		line, ok := lines[step]
		if !ok {
			t.Errorf("no %s step in %q", step, out)
			continue
		}
		if _, err := fmt.Sscan(line, &got.count, &got.frame[0], &got.frame[1], &got.frame[2], &got.frame[3], &got.fullScreen, &got.display); err != nil {
			t.Fatalf("the %s step %q: %v", step, line, err)
		}
		wantStep := want[step]
		if got.count != wantStep.count || (step != "installed" && (got.frame != wantStep.frame || got.fullScreen != wantStep.fullScreen)) {
			t.Errorf("%s: %d reports, the last %v full screen %d; want %d, %v, %d", step, got.count, got.frame, got.fullScreen,
				wantStep.count, wantStep.frame, wantStep.fullScreen)
		}
		if step != "installed" && got.display == 0 {
			t.Errorf("%s: no display reported", step)
		}
	}
	if lines["state"] != "230 170 1100 700 0" {
		t.Errorf("state %q, want the place the last report gave", lines["state"])
	}
	// Full screen kept: an app that is not active gets no full screen from AppKit (measured on
	// macOS 26: it takes the style, then gives up), so the window waits for the app's activation
	// and its place says full screen meanwhile.
	var kept, style, active int
	if _, err := fmt.Sscan(lines["fullscreen"], &kept, &style, &active); err != nil || kept != 1 || style != active {
		t.Errorf("fullscreen %q (%v): want kept, in full screen only if the app is active", lines["fullscreen"], err)
	}
}
