package fyneui

import (
	"encoding/json"
	"log/slog"
	"math"
	"reflect"
	"strconv"
	"sync"
	"time"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The window's place (D-95, the owner's WF11 of 2026-10-17): where the window was, how large, on
// which display and whether in full screen, kept in a file of its own in Cogpit's configuration
// directory and given back at the next start, whatever Preferences › App › Restore the last session
// says (macOS apps come back where they were). It is the window's, not engine state: agents drive
// what the window shows, not where it sits on the desktop, so no command reads or sets it.
//
// macOS keeps the frame in AppKit's screen coordinates (points, origin at the bottom left of the
// primary display, y up) with the display it was on, and follows every move, resize and full-screen
// change (windowplace_darwin.m), written once the window stayed put for windowPlaceDelay, so that a
// crash loses at most that last moment. At start the frame comes back on its display wherever that
// display is arranged now, clipped to the display's visible area; a frame on no current display (an
// unplugged one) gives the first start's window, 1440 × 900 in the middle of the primary display.
// Fyne has no position for a window on Windows and Linux: there the canvas's size is kept when the
// window closes, and whether it was in full screen (windowplace_other.go).

// WindowPlaceFile is the window's place in Cogpit's configuration directory.
const WindowPlaceFile = "window.json"

const (
	windowPlaceVersion = 1
	windowPlaceMaxSize = 64 << 10
	// windowPlaceDelay is how long the window stays put before its place is written: a drag or a
	// live resize reports every step.
	windowPlaceDelay = 500 * time.Millisecond
	// minPlaceWidth and minPlaceHeight: a kept frame or size smaller than this is not a window
	// Cogpit made, and the first start's window comes instead.
	minPlaceWidth, minPlaceHeight = 320, 200
	// maxPlaceCoord bounds every kept number: beyond it the file is not Cogpit's.
	maxPlaceCoord = 1e6
)

// defaultWindowSize is the window's content at the first start and the fallback, in Fyne units.
var defaultWindowSize = fyne.NewSize(1440, 900)

// placeRect is a rectangle in AppKit's screen coordinates, in points.
type placeRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func (r placeRect) maxX() float64 { return r.X + r.Width }
func (r placeRect) maxY() float64 { return r.Y + r.Height }

// usable says whether r can be a window's frame: finite, inside maxPlaceCoord, at least the
// minimum size.
func (r placeRect) usable() bool {
	for _, v := range []float64{r.X, r.Y, r.Width, r.Height} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > maxPlaceCoord {
			return false
		}
	}
	return r.Width >= minPlaceWidth && r.Height >= minPlaceHeight
}

// overlap is the area r and o share.
func (r placeRect) overlap(o placeRect) float64 {
	w := math.Min(r.maxX(), o.maxX()) - math.Max(r.X, o.X)
	h := math.Min(r.maxY(), o.maxY()) - math.Max(r.Y, o.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// placeSize is a canvas's size in Fyne units (Windows and Linux).
type placeSize struct {
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

// placeDisplay is the display a frame was on: macOS's display id (NSScreenNumber), its name and
// its frame then.
type placeDisplay struct {
	ID    uint32    `json:"id"`
	Name  string    `json:"name,omitempty"`
	Frame placeRect `json:"frame"`
}

// windowPlace is the file's content.
type windowPlace struct {
	Version int `json:"version"`
	// Frame is the window's frame on macOS, never the full-screen one: with FullScreen it is the
	// frame the window leaves full screen to.
	Frame *placeRect `json:"frame,omitempty"`
	// Display is the display the window was on (macOS).
	Display *placeDisplay `json:"display,omitempty"`
	// Size is the canvas's size on Windows and Linux, never the full-screen one.
	Size *placeSize `json:"size,omitempty"`
	// FullScreen: the window was in full screen.
	FullScreen bool `json:"fullScreen,omitempty"`
}

// screenInfo is one of the computer's displays as AppKit describes it; the primary display (the
// menu bar's, origin 0,0) comes first.
type screenInfo struct {
	ID      uint32
	Name    string
	Frame   placeRect
	Visible placeRect // the frame without the menu bar and the Dock
}

// placeFrame is where the window goes at start: the kept frame on its display, else on the display
// it overlaps most, fitted into that display's visible area (a frame larger than it is clipped, one
// that hangs over its edge is moved in); a frame on no display, or none kept, gives def's size in
// the middle of the primary display's visible area, clipped to it. False without displays (no
// window server): Fyne's own placement then.
func placeFrame(kept windowPlace, screens []screenInfo, def placeRect) (placeRect, bool) {
	if len(screens) == 0 {
		return placeRect{}, false
	}
	if f, vis, ok := keptFrameOn(kept, screens); ok {
		return fitInto(f, vis), true
	}
	return centreIn(def, screens[0].Visible), true
}

// keptFrameOn finds the display for the kept frame: the one it was on, by its id, else by its name
// when one display alone has it (an external display's id can change with the port it is plugged
// into), the frame moved with the display's top left if the display was arranged elsewhere since;
// else the display whose visible area it overlaps most. Not found: no frame, or one off every
// display.
func keptFrameOn(kept windowPlace, screens []screenInfo) (placeRect, placeRect, bool) {
	if kept.Frame == nil || !kept.Frame.usable() {
		return placeRect{}, placeRect{}, false
	}
	f := *kept.Frame
	if d := kept.Display; d != nil {
		if s, ok := sameDisplay(*d, screens); ok {
			if d.Frame.usable() {
				f.X += s.Frame.X - d.Frame.X
				f.Y += s.Frame.maxY() - d.Frame.maxY()
			}
			return f, s.Visible, true
		}
	}
	best, area := -1, 0.0
	for i, s := range screens {
		if a := f.overlap(s.Visible); a > area {
			best, area = i, a
		}
	}
	if best < 0 {
		return placeRect{}, placeRect{}, false
	}
	return f, screens[best].Visible, true
}

// sameDisplay finds d among screens: by id, else by a name only one display has.
func sameDisplay(d placeDisplay, screens []screenInfo) (screenInfo, bool) {
	if d.ID != 0 {
		for _, s := range screens {
			if s.ID == d.ID {
				return s, true
			}
		}
	}
	if d.Name == "" {
		return screenInfo{}, false
	}
	found, n := screenInfo{}, 0
	for _, s := range screens {
		if s.Name == d.Name {
			found, n = s, n+1
		}
	}
	return found, n == 1
}

// fitInto puts f inside vis: as large as vis at most, moved in only as far as it hangs over, in
// whole points.
func fitInto(f, vis placeRect) placeRect {
	w := math.Floor(math.Min(f.Width, vis.Width))
	h := math.Floor(math.Min(f.Height, vis.Height))
	return placeRect{
		X:      clampPoint(math.Round(f.X), vis.X, vis.maxX()-w),
		Y:      clampPoint(math.Round(f.Y), vis.Y, vis.maxY()-h),
		Width:  w,
		Height: h,
	}
}

// centreIn is def's size, clipped to vis, in the middle of vis, in whole points.
func centreIn(def, vis placeRect) placeRect {
	w := math.Floor(math.Min(def.Width, vis.Width))
	h := math.Floor(math.Min(def.Height, vis.Height))
	return placeRect{
		X:      clampPoint(math.Round(vis.X+(vis.Width-w)/2), vis.X, vis.maxX()-w),
		Y:      clampPoint(math.Round(vis.Y+(vis.Height-h)/2), vis.Y, vis.maxY()-h),
		Width:  w,
		Height: h,
	}
}

// clampPoint keeps v within whole points of [lo, hi].
func clampPoint(v, lo, hi float64) float64 {
	lo, hi = math.Ceil(lo), math.Floor(hi)
	if v > hi {
		v = hi
	}
	if v < lo {
		v = lo
	}
	return v
}

// glfwPlacement turns a frame in AppKit's coordinates into what Fyne asks GLFW for before the
// window exists: the content's top left in GLFW's coordinates (points from the primary display's
// top left, y down; GLFW's _glfwSetWindowPosCocoa) and the content's size in Fyne units. titleBar
// is the title bar of the window GLFW creates (Cogpit's own title bar takes its room only once the
// window runs), mainHeight the primary display's height, scale Fyne's canvas scale.
func glfwPlacement(f placeRect, titleBar, mainHeight float64, scale float32) (x, y int, size fyne.Size) {
	if scale <= 0 {
		scale = 1
	}
	contentTop := f.maxY() - titleBar
	x = int(math.Round(f.X))
	y = int(math.Round(mainHeight - contentTop))
	return x, y, fyne.NewSize(float32(f.Width)/scale, float32(f.Height-titleBar)/scale)
}

// canvasScaleFor is the scale Fyne's driver gives a window's canvas on macOS, where the system's
// is 1 (GLFW's userScale and calculateScale): FYNE_SCALE (env) unless empty or "auto", else Fyne's
// scale setting, else 1, to a tenth.
func canvasScaleFor(env string, setting float32) float32 {
	user := float32(0)
	if env != "" && env != "auto" {
		if v, err := strconv.ParseFloat(env, 32); err == nil && v != 0 {
			user = float32(v)
		}
	}
	if user == 0 && env != "auto" && setting > 0 {
		user = setting
	}
	if user <= 0 {
		user = 1
	}
	return float32(math.Round(float64(user)*10) / 10)
}

// restoredSize is the window's content size at start on Windows and Linux: the kept one, else the
// first start's.
func restoredSize(kept windowPlace) fyne.Size {
	s := kept.Size
	if s == nil || !(placeRect{Width: float64(s.Width), Height: float64(s.Height)}).usable() {
		return defaultWindowSize
	}
	return fyne.NewSize(s.Width, s.Height)
}

// sizePlace is what Windows and Linux keep when the window closes: the canvas's size and whether
// it is in full screen; in full screen the size before stays.
func sizePlace(prev windowPlace, size fyne.Size, full bool) windowPlace {
	p := windowPlace{Version: windowPlaceVersion, Size: prev.Size, FullScreen: full}
	if !full {
		p.Size = &placeSize{Width: size.Width, Height: size.Height}
	}
	return p
}

// windowPlaceKeeper keeps the window's place in its file. A keeper without a path keeps nothing
// (tests, a run without a configuration directory). Safe for concurrent use: AppKit's reports come
// on the main thread, the writes on a timer's goroutine.
type windowPlaceKeeper struct {
	path  string
	delay time.Duration
	write func(path string, data []byte) error

	mu      sync.Mutex
	saved   windowPlace  // what the file holds
	pending *windowPlace // reported, not written yet
	timer   *time.Timer
	done    bool
}

// openWindowPlace reads the kept place at path; an empty path keeps nothing. A file that does not
// load is logged and left for the next change to replace: it holds nothing but the place.
func openWindowPlace(path string) *windowPlaceKeeper {
	k := &windowPlaceKeeper{path: path, delay: windowPlaceDelay, write: engine.WritePrivateFile}
	if path == "" {
		return k
	}
	var p windowPlace
	if err := engine.ReadPrivateJSON(path, windowPlaceMaxSize, &p); err != nil {
		slog.Warn("the window's place could not be read; the window opens in the middle of the screen", "err", err)
		return k
	}
	if p.Version != windowPlaceVersion {
		return k // a newer Cogpit's, or none
	}
	k.saved = p
	return k
}

// kept is the place the file holds.
func (k *windowPlaceKeeper) kept() windowPlace {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.saved
}

// changed takes the window's place now; it is written once no other came for k.delay.
func (k *windowPlaceKeeper) changed(p windowPlace) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.done || k.path == "" {
		return
	}
	p.Version = windowPlaceVersion
	k.pending = &p
	if k.timer == nil {
		k.timer = time.AfterFunc(k.delay, k.flush)
		return
	}
	k.timer.Reset(k.delay)
}

// flush writes what is pending.
func (k *windowPlaceKeeper) flush() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.writeLocked()
}

// finish writes what is pending at once and takes no change after it: the window is gone.
func (k *windowPlaceKeeper) finish() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.done = true
	if k.timer != nil {
		k.timer.Stop()
	}
	k.writeLocked()
}

// writeLocked writes the pending place unless the file holds it already; k.mu is held.
func (k *windowPlaceKeeper) writeLocked() {
	p := k.pending
	k.pending = nil
	if p == nil || reflect.DeepEqual(*p, k.saved) {
		return
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err == nil {
		err = k.write(k.path, append(data, '\n'))
	}
	if err != nil {
		slog.Warn("the window's place could not be kept", "err", err)
		return
	}
	k.saved = *p
}
