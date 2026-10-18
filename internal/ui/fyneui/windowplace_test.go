package fyneui

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
)

// The displays of the tests, in AppKit's coordinates: a laptop's built-in display as the primary
// one (a 37-point menu bar, the Dock 80 points at the bottom) and an external display to its right
// whose top is 300 points higher.
var (
	placeBuiltIn = screenInfo{ID: 1, Name: "Built-in Retina Display",
		Frame:   placeRect{X: 0, Y: 0, Width: 1512, Height: 982},
		Visible: placeRect{X: 0, Y: 80, Width: 1512, Height: 865}}
	placeExternal = screenInfo{ID: 7, Name: "LG HDR 4K",
		Frame:   placeRect{X: 1512, Y: 0, Width: 2560, Height: 1282},
		Visible: placeRect{X: 1512, Y: 0, Width: 2560, Height: 1257}}
	placeDefFrame = placeRect{Width: 1440, Height: 928} // 1440 × 900 and a 28-point title bar
)

func placeFrameOn(f placeRect, d *screenInfo, full bool) windowPlace {
	p := windowPlace{Version: windowPlaceVersion, Frame: &f, FullScreen: full}
	if d != nil {
		p.Display = &placeDisplay{ID: d.ID, Name: d.Name, Frame: d.Frame}
	}
	return p
}

// The window comes back where it was, as large as it was, on its display; a frame on no display
// now, or none at all, gives 1440 × 900 in the middle of the primary display; one larger than its
// display is clipped to the visible area, one over its edge moved in (D-95, the owner's WF11).
func TestPlaceFrame(t *testing.T) {
	inside := placeRect{X: 200, Y: 120, Width: 1100, Height: 760}
	onExternal := placeRect{X: 2000, Y: 300, Width: 1800, Height: 900}
	tests := []struct {
		name    string
		kept    windowPlace
		screens []screenInfo
		want    placeRect
	}{
		{"none kept: the first start's, centred on the primary display",
			windowPlace{}, []screenInfo{placeBuiltIn, placeExternal},
			placeRect{X: 36, Y: 80, Width: 1440, Height: 865}},
		{"kept inside its display: unchanged",
			placeFrameOn(inside, &placeBuiltIn, false), []screenInfo{placeBuiltIn},
			inside},
		{"on the external display: there again",
			placeFrameOn(onExternal, &placeExternal, false), []screenInfo{placeBuiltIn, placeExternal},
			onExternal},
		{"the external display unplugged: the fallback on the primary one",
			placeFrameOn(onExternal, &placeExternal, false), []screenInfo{placeBuiltIn},
			placeRect{X: 36, Y: 80, Width: 1440, Height: 865}},
		{"larger than its display: clipped to the visible area",
			placeFrameOn(placeRect{X: -50, Y: 0, Width: 3000, Height: 2000}, &placeBuiltIn, false), []screenInfo{placeBuiltIn},
			placeBuiltIn.Visible},
		{"over the right edge: moved in, its size kept",
			placeFrameOn(placeRect{X: 1000, Y: 100, Width: 800, Height: 600}, &placeBuiltIn, false), []screenInfo{placeBuiltIn},
			placeRect{X: 712, Y: 100, Width: 800, Height: 600}},
		{"under the Dock and above the menu bar: moved into the visible area",
			placeFrameOn(placeRect{X: 100, Y: 40, Width: 800, Height: 950}, &placeBuiltIn, false), []screenInfo{placeBuiltIn},
			placeRect{X: 100, Y: 80, Width: 800, Height: 865}},
		{"the external display arranged to the left now: the window moves with it",
			placeFrameOn(onExternal, &placeExternal, false),
			[]screenInfo{placeBuiltIn, {ID: 7, Name: placeExternal.Name,
				Frame:   placeRect{X: -2560, Y: -100, Width: 2560, Height: 1282},
				Visible: placeRect{X: -2560, Y: -100, Width: 2560, Height: 1257}}},
			placeRect{X: 2000 - 1512 - 2560, Y: 300 - 100, Width: 1800, Height: 900}},
		{"the external display's id changed, its name unique: found by name",
			placeFrameOn(onExternal, &placeExternal, false),
			[]screenInfo{placeBuiltIn, {ID: 9, Name: placeExternal.Name, Frame: placeExternal.Frame, Visible: placeExternal.Visible}},
			onExternal},
		{"its name not unique and its id gone: the display it overlaps most",
			placeFrameOn(onExternal, &placeExternal, false),
			[]screenInfo{placeBuiltIn, {ID: 9, Name: placeExternal.Name, Frame: placeExternal.Frame, Visible: placeExternal.Visible},
				{ID: 10, Name: placeExternal.Name, Frame: placeRect{X: 4072, Width: 2560, Height: 1282}, Visible: placeRect{X: 4072, Width: 2560, Height: 1257}}},
			onExternal},
		{"no display kept: the display it overlaps most",
			windowPlace{Version: 1, Frame: &onExternal}, []screenInfo{placeBuiltIn, placeExternal},
			onExternal},
		{"no display kept and off every display: the fallback",
			windowPlace{Version: 1, Frame: &placeRect{X: 9000, Y: 9000, Width: 800, Height: 600}}, []screenInfo{placeBuiltIn, placeExternal},
			placeRect{X: 36, Y: 80, Width: 1440, Height: 865}},
		{"a smaller resolution of its display: clipped and moved in",
			placeFrameOn(placeRect{X: 2000, Y: 100, Width: 2400, Height: 1100}, &placeExternal, false),
			[]screenInfo{placeBuiltIn, {ID: 7, Name: placeExternal.Name,
				Frame:   placeRect{X: 1512, Y: 0, Width: 1920, Height: 1282},
				Visible: placeRect{X: 1512, Y: 182, Width: 1920, Height: 1075}}},
			placeRect{X: 1512, Y: 182, Width: 1920, Height: 1075}},
		{"too small to be Cogpit's: the fallback",
			placeFrameOn(placeRect{X: 100, Y: 100, Width: 20, Height: 10}, &placeBuiltIn, false), []screenInfo{placeBuiltIn},
			placeRect{X: 36, Y: 80, Width: 1440, Height: 865}},
		{"not a number: the fallback",
			placeFrameOn(placeRect{X: math.NaN(), Y: 100, Width: 800, Height: 600}, &placeBuiltIn, false), []screenInfo{placeBuiltIn},
			placeRect{X: 36, Y: 80, Width: 1440, Height: 865}},
		{"fractional points: whole ones",
			placeFrameOn(placeRect{X: 100.4, Y: 200.6, Width: 800.7, Height: 600.2}, &placeBuiltIn, false), []screenInfo{placeBuiltIn},
			placeRect{X: 100, Y: 201, Width: 800, Height: 600}},
		{"full screen: the frame it leaves full screen to",
			placeFrameOn(inside, &placeBuiltIn, true), []screenInfo{placeBuiltIn},
			inside},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := placeFrame(tt.kept, tt.screens, placeDefFrame)
			if !ok {
				t.Fatal("no frame")
			}
			if got != tt.want {
				t.Errorf("placeFrame = %+v, want %+v", got, tt.want)
			}
			if !insideOneVisible(got, tt.screens) {
				t.Errorf("%+v is not inside one display's visible area", got)
			}
		})
	}
	if _, ok := placeFrame(windowPlace{}, nil, placeDefFrame); ok {
		t.Error("a frame without displays; want Fyne's placement")
	}
}

// insideOneVisible says whether r lies wholly in one display's visible area.
func insideOneVisible(r placeRect, screens []screenInfo) bool {
	for _, s := range screens {
		v := s.Visible
		if r.X >= v.X && r.Y >= v.Y && r.maxX() <= v.maxX() && r.maxY() <= v.maxY() {
			return true
		}
	}
	return false
}

// GLFW is asked for the content's top left from the primary display's top left, y down, and Fyne
// for the content's size: the frame less the title bar GLFW's window has at first, over Fyne's
// scale.
func TestGLFWPlacement(t *testing.T) {
	f := placeRect{X: 100, Y: 50, Width: 1440, Height: 928}
	x, y, size := glfwPlacement(f, 28, 982, 1)
	if x != 100 || y != 982-(50+928-28) || size != fyne.NewSize(1440, 900) {
		t.Errorf("glfwPlacement = %d, %d, %v; want 100, %d, 1440×900", x, y, size, 982-(50+928-28))
	}
	// A display left of and above the primary one: negative coordinates; macOS 26's 32-point title
	// bar on a 1440-point primary display.
	x, y, size = glfwPlacement(placeRect{X: -2000, Y: 1600, Width: 800, Height: 632}, 32, 1440, 1)
	if x != -2000 || y != 1440-2200 || size != fyne.NewSize(800, 600) {
		t.Errorf("glfwPlacement left above = %d, %d, %v; want -2000, %d, 800×600", x, y, size, 1440-2200)
	}
	_, _, size = glfwPlacement(f, 28, 982, 2)
	if size != fyne.NewSize(720, 450) {
		t.Errorf("at scale 2 the size is %v, want 720×450", size)
	}
	_, _, size = glfwPlacement(f, 28, 982, 0)
	if size != fyne.NewSize(1440, 900) {
		t.Errorf("at scale 0 the size is %v, want scale 1's", size)
	}
}

// Fyne's canvas scale on macOS: FYNE_SCALE over the setting, "auto" and nonsense as 1, to a tenth.
func TestCanvasScaleFor(t *testing.T) {
	for _, tt := range []struct {
		env     string
		setting float32
		want    float32
	}{
		{"", 0, 1}, {"", -1, 1}, {"", 1.25, 1.3}, {"2", 1.5, 2}, {"auto", 1.5, 1},
		{"nonsense", 1.5, 1.5}, {"0", 0, 1}, {"0.84", 0, 0.8},
	} {
		if got := canvasScaleFor(tt.env, tt.setting); got != tt.want {
			t.Errorf("canvasScaleFor(%q, %v) = %v, want %v", tt.env, tt.setting, got, tt.want)
		}
	}
}

// Windows and Linux keep the canvas's size, the one before in full screen, and give it back at
// least as large as the minimum.
func TestSizePlace(t *testing.T) {
	p := sizePlace(windowPlace{}, fyne.NewSize(1200, 800), false)
	if p.Size == nil || *p.Size != (placeSize{Width: 1200, Height: 800}) || p.FullScreen || p.Version != windowPlaceVersion {
		t.Fatalf("sizePlace = %+v", p)
	}
	if got := restoredSize(p); got != fyne.NewSize(1200, 800) {
		t.Errorf("restoredSize = %v, want 1200×800", got)
	}
	full := sizePlace(p, fyne.NewSize(2560, 1440), true)
	if full.Size == nil || *full.Size != *p.Size || !full.FullScreen {
		t.Errorf("in full screen sizePlace = %+v, want the size before and full screen", full)
	}
	for _, kept := range []windowPlace{{}, {Size: &placeSize{Width: 10, Height: 10}}, {Size: &placeSize{Width: float32(math.Inf(1)), Height: 900}}} {
		if got := restoredSize(kept); got != defaultWindowSize {
			t.Errorf("restoredSize(%+v) = %v, want the default", kept, got)
		}
	}
}

// waitForPlace waits up to 5 seconds for cond.
func waitForPlace(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The keeper writes a place once the window stayed put, the last of a burst only, not again when
// nothing changed, at once when the window is gone, and reads it back at the next start; without
// a path it keeps nothing.
func TestWindowPlaceKeeper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Cogpit", WindowPlaceFile)
	k := openWindowPlace(path)
	var mu sync.Mutex
	writes := 0
	write := k.write
	k.write = func(p string, data []byte) error {
		err := write(p, data)
		mu.Lock()
		writes++
		mu.Unlock()
		return err
	}
	k.delay = 20 * time.Millisecond
	t.Cleanup(k.finish) // no timer's write after the test
	written := func() int { mu.Lock(); defer mu.Unlock(); return writes }

	for i := range 10 { // a drag: ten moves, one write
		k.changed(placeFrameOn(placeRect{X: float64(100 + i), Y: 100, Width: 900, Height: 700}, &placeBuiltIn, false))
	}
	waitForPlace(t, "the first write", func() bool { return written() == 1 })
	want := placeFrameOn(placeRect{X: 109, Y: 100, Width: 900, Height: 700}, &placeBuiltIn, false)
	if got := readPlace(t, path); got.Frame == nil || *got.Frame != *want.Frame || got.Display == nil || *got.Display != *want.Display {
		t.Errorf("the file holds %+v, want %+v", got, want)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the file: %v, mode %v; want owner-only", err, fi.Mode().Perm())
	}

	k.changed(want) // the same place again: not written
	k.flush()
	if n := written(); n != 1 {
		t.Errorf("%d writes after the same place again, want 1", n)
	}

	full := placeFrameOn(*want.Frame, &placeBuiltIn, true)
	k.changed(full)
	k.finish() // the window is gone: written at once, nothing after
	if n := written(); n != 2 {
		t.Errorf("%d writes after finish, want 2", n)
	}
	k.changed(placeFrameOn(placeRect{X: 1, Y: 1, Width: 900, Height: 700}, &placeBuiltIn, false))
	k.flush()
	if n := written(); n != 2 || !readPlace(t, path).FullScreen {
		t.Errorf("a change after finish was kept: %d writes", n)
	}

	again := openWindowPlace(path) // the next start
	if got := again.kept(); !got.FullScreen || got.Frame == nil || *got.Frame != *want.Frame {
		t.Errorf("the next start reads %+v", got)
	}

	none := openWindowPlace("")
	none.changed(want)
	none.finish()
	if got := none.kept(); got.Frame != nil {
		t.Errorf("a keeper without a path kept %+v", got)
	}
}

// A file that does not load, or a newer Cogpit's, gives the first start's window and is replaced
// by the next change; a failed write is tried again with the next change.
func TestWindowPlaceKeeperUnreadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, WindowPlaceFile)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	k := openWindowPlace(path)
	if got := k.kept(); got.Frame != nil {
		t.Fatalf("an unreadable file gave %+v", got)
	}
	k.delay = time.Hour // the test flushes itself; no timer's write meanwhile
	t.Cleanup(k.finish)
	k.write = func(string, []byte) error { return errors.New("disk full") }
	p := placeFrameOn(placeRect{X: 10, Y: 90, Width: 900, Height: 700}, &placeBuiltIn, false)
	k.changed(p)
	k.flush()
	if k.kept().Frame != nil {
		t.Error("a failed write counts as kept")
	}
	k.write = func(path string, data []byte) error { return os.WriteFile(path, data, 0o600) }
	k.changed(p)
	k.flush()
	if got := readPlace(t, path); got.Frame == nil || *got.Frame != *p.Frame {
		t.Errorf("the file holds %+v after the next change", got)
	}

	newer, _ := json.Marshal(map[string]any{"version": 2, "frame": map[string]any{"x": 1, "y": 1, "width": 900, "height": 700}})
	if err := os.WriteFile(path, newer, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := openWindowPlace(path).kept(); got.Frame != nil {
		t.Errorf("a newer Cogpit's file gave %+v", got)
	}
}

func readPlace(t *testing.T, path string) windowPlace {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var p windowPlace
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	return p
}
