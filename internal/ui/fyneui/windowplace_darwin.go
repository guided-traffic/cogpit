//go:build darwin

package fyneui

/*
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include "windowplace_darwin.h"
*/
import "C"

import (
	"os"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/driver/desktop"
)

// The window's place on macOS (D-95, windowplace.go): the frame and its display from AppKit,
// given to Fyne before the window exists, set exactly once it runs, and followed from then on.

// maxPlaceScreens bounds the displays described at start.
const maxPlaceScreens = 16

var (
	placeKeepers sync.Map // id → *windowPlaceKeeper, for cogpitWindowPlaceChanged
	placeIDs     atomic.Uint64
)

// windowPlacer gives the window its kept place and hands its changes to the keeper. Main
// goroutine only.
type windowPlacer struct {
	keeper     *windowPlaceKeeper
	frame      placeRect // where restore put the window; zero: Fyne's placement
	fullScreen bool
	id         uint64 // the key AppKit's reports name the keeper by
}

func newWindowPlacer(k *windowPlaceKeeper) *windowPlacer { return &windowPlacer{keeper: k} }

// restore gives the window its kept place before it shows: the frame placeFrame chose, as Fyne's
// content size and GLFW's position (the window then opens there, no jump), else Fyne's 1440 × 900
// in the middle of the screen. Call it on the main goroutine before ShowAndRun; Fyne's driver has
// started AppKit by then (a.NewWindow).
func (p *windowPlacer) restore(a fyne.App, w fyne.Window) {
	screens := placeScreens()
	titleBar := float64(C.cogpitWindowPlaceTitleBarHeight())
	def := placeRect{Width: float64(defaultWindowSize.Width), Height: float64(defaultWindowSize.Height) + titleBar}
	kept := p.keeper.kept()
	f, ok := placeFrame(kept, screens, def)
	dw, isDesktop := w.(desktop.Window)
	if !ok || !isDesktop {
		w.Resize(defaultWindowSize)
		return
	}
	p.frame, p.fullScreen = f, kept.FullScreen
	scale := canvasScaleFor(os.Getenv("FYNE_SCALE"), a.Settings().Scale())
	x, y, size := glfwPlacement(f, titleBar, screens[0].Frame.Height, scale)
	w.Resize(size)
	dw.RequestPosition(x, y)
}

// follow runs once the window is up, after Cogpit's title bar took its room (titlebar.go): the
// frame exactly as restore chose it, full screen if the window was (once the app is active), and
// every change of its place kept from then on.
func (p *windowPlacer) follow(w fyne.Window) {
	win := placeNSWindow(w)
	if win == 0 {
		return
	}
	if p.id == 0 {
		p.id = placeIDs.Add(1)
		placeKeepers.Store(p.id, p.keeper)
	}
	full := C.int(0)
	if p.fullScreen {
		full = 1
	}
	C.cogpitWindowPlaceFollow(C.uintptr_t(win), C.uintptr_t(p.id), placeCRect(p.frame), full)
}

// finishWindow runs once the app's run ended, when GLFW has destroyed the window: AppKit's reports
// are no longer taken, and what is pending is written at once. Nothing asks for the window then
// (asking GLFW for it panics, titlebar_darwin.go).
func (p *windowPlacer) finishWindow(fyne.Window) {
	if p.id != 0 {
		placeKeepers.Delete(p.id)
	}
	p.keeper.finish()
}

// placeNSWindow is the window's NSWindow, or 0 before it exists and after it closed. RunNative
// runs its function at once, on the caller's goroutine: the main one.
func placeNSWindow(w fyne.Window) uintptr {
	var win uintptr
	if nw, ok := w.(driver.NativeWindow); ok {
		nw.RunNative(func(ctx any) {
			if mc, ok := ctx.(driver.MacWindowContext); ok {
				win = mc.NSWindow
			}
		})
	}
	return win
}

// placeScreens describes the computer's displays, the primary one first.
func placeScreens() []screenInfo {
	var buf [maxPlaceScreens]C.cogpitPlaceScreen
	n := int(C.cogpitWindowPlaceScreens(&buf[0], C.int(len(buf))))
	n = min(n, len(buf))
	screens := make([]screenInfo, 0, n)
	for i := range n {
		screens = append(screens, screenFromC(&buf[i]))
	}
	return screens
}

func screenFromC(s *C.cogpitPlaceScreen) screenInfo {
	return screenInfo{ID: uint32(s.id), Name: C.GoString(&s.name[0]), Frame: placeFromCRect(s.frame), Visible: placeFromCRect(s.visible)}
}

func placeFromCRect(r C.cogpitPlaceRect) placeRect {
	return placeRect{X: float64(r.x), Y: float64(r.y), Width: float64(r.width), Height: float64(r.height)}
}

func placeCRect(r placeRect) C.cogpitPlaceRect {
	return C.cogpitPlaceRect{x: C.double(r.X), y: C.double(r.Y), width: C.double(r.Width), height: C.double(r.Height)}
}

// placeFromState is a report as the file keeps it.
func placeFromState(st *C.cogpitPlaceState) windowPlace {
	f := placeFromCRect(st.frame)
	p := windowPlace{Frame: &f, FullScreen: st.fullScreen != 0}
	if st.display.id != 0 {
		s := screenFromC(&st.display)
		p.Display = &placeDisplay{ID: s.ID, Name: s.Name, Frame: s.Frame}
	}
	return p
}

// cogpitWindowPlaceChanged is AppKit's report that the window moved, was resized, went to another
// display or into or out of full screen. It runs on the main thread and only hands the place on.
//
//export cogpitWindowPlaceChanged
func cogpitWindowPlaceChanged(id C.uintptr_t, st *C.cogpitPlaceState) {
	if v, ok := placeKeepers.Load(uint64(id)); ok {
		v.(*windowPlaceKeeper).changed(placeFromState(st))
	}
}
