//go:build darwin

package fyneui

/*
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include "titlebar_darwin.h"
*/
import "C"

import (
	"os"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2/driver"
)

// systemTitlebarEnv keeps macOS's standard title bar when set: a way back while Cogpit's own is new.
const systemTitlebarEnv = "COGPIT_SYSTEM_TITLEBAR"

// titlebar is Cogpit's own title bar on macOS (titlebar.go): installed on the window once it
// exists, it keeps the top bar's room for the window buttons current and turns mouse-downs on the
// bar into window drags, zooms and minimisations. Main goroutine only, but for the id.
type titlebar struct {
	sh     *shell
	id     uint64 // the key AppKit's reports name it by (titlebars)
	window uintptr
}

var (
	titlebars   sync.Map // id → *titlebar, for cogpitTitlebarChanged
	titlebarIDs atomic.Uint64
)

func newTitlebar(sh *shell) *titlebar { return &titlebar{sh: sh, id: titlebarIDs.Add(1)} }

// nsWindow returns the shell window's NSWindow, or 0 before it exists and after it closed.
// RunNative runs its function at once, on the caller's goroutine: the main one.
func (tb *titlebar) nsWindow() uintptr {
	var win uintptr
	if nw, ok := tb.sh.win.(driver.NativeWindow); ok {
		nw.RunNative(func(ctx any) {
			if mc, ok := ctx.(driver.MacWindowContext); ok {
				win = mc.NSWindow
			}
		})
	}
	return win
}

// install takes over the title bar's area. Call it on the main goroutine once the window exists:
// the app's OnStarted queues it behind the window's creation.
func (tb *titlebar) install() {
	if tb.window != 0 {
		return
	}
	win := tb.nsWindow()
	if win == 0 {
		return
	}
	// With the system's title bar too:
	C.cogpitWindowKeepContentPlaced(C.uintptr_t(win))
	C.cogpitWindowUseSRGB(C.uintptr_t(win))
	if os.Getenv(systemTitlebarEnv) != "" {
		return
	}
	tb.window = win
	titlebars.Store(tb.id, tb)
	C.cogpitTitlebarInstall(C.uintptr_t(win), C.uintptr_t(tb.id))
	tb.update()
}

// barHeightPoints is the top bar's height in points, which the window buttons are centred in: the
// theme's bar height in Fyne units, times the canvas scale (titlebarLayout divides by it).
func (tb *titlebar) barHeightPoints() float64 {
	scale := float32(1)
	if c := tb.sh.win.Canvas(); c != nil && c.Scale() > 0 {
		scale = c.Scale()
	}
	return float64(tb.sh.app.Settings().Theme().Size(sizeBarHeight) * scale)
}

// update gives the top bar the room the window buttons take now.
func (tb *titlebar) update() {
	if tb.window == 0 || tb.nsWindow() != tb.window {
		return
	}
	C.cogpitTitlebarCentreButtons(C.uintptr_t(tb.window), C.double(tb.barHeightPoints()))
	var g C.cogpitTitlebarGeom
	C.cogpitTitlebarGeometry(C.uintptr_t(tb.window), &g)
	if g.ok == 0 {
		tb.sh.applyTitlebar(titlebarGeometry{})
		return
	}
	tb.sh.applyTitlebar(titlebarGeometry{
		ButtonsMinX: float32(g.buttonsMinX), ButtonsMaxX: float32(g.buttonsMaxX),
		Height: float32(g.height), FullScreen: g.fullScreen != 0,
	})
}

// mouseDown runs in Fyne's mouse-down on the bar, inside AppKit's dispatch of the event: a single
// click starts a window drag, the second click of a double click does what the user's setting
// says.
func (tb *titlebar) mouseDown() {
	if tb.window == 0 || tb.nsWindow() != tb.window {
		return
	}
	win := C.uintptr_t(tb.window)
	switch clicks := C.cogpitTitlebarClickCount(win); {
	case clicks == 1:
		C.cogpitTitlebarDrag(win)
	case clicks >= 2:
		switch doubleClickSetting() {
		case titlebarZoom:
			C.cogpitTitlebarZoom(win)
		case titlebarMinimize:
			C.cogpitTitlebarMiniaturize(win)
		}
	}
}

// doubleClickSetting reads the user's "double-click a window's title bar to".
func doubleClickSetting() titlebarAction {
	var buf [32]C.char
	var legacySet, legacyMinimize C.int
	C.cogpitTitlebarDoubleClickSetting(&buf[0], C.int(len(buf)), &legacySet, &legacyMinimize)
	return doubleClickAction(C.GoString(&buf[0]), legacySet != 0, legacyMinimize != 0)
}

// stop ends AppKit's reports. It runs once the app's run ended, when GLFW has terminated and
// destroyed the window: asking GLFW for the window then panics, which every quit did until
// 2026-10-12. Forgetting the id is enough: the observer is the window's associated object and goes
// with it, and a report still on its way finds no title bar under the id.
func (tb *titlebar) stop() {
	titlebars.Delete(tb.id)
	tb.window = 0
}

// cogpitTitlebarChanged is AppKit's report that the window's title bar may have moved: full screen
// entered or left, a resize, another screen. It runs on the main thread.
//
//export cogpitTitlebarChanged
func cogpitTitlebarChanged(id C.uintptr_t) {
	if v, ok := titlebars.Load(uint64(id)); ok {
		tb := v.(*titlebar)
		tb.sh.do(tb.update)
	}
}
