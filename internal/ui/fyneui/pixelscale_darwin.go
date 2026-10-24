//go:build darwin

package fyneui

/*
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include "pixelscale_darwin.h"
*/
import "C"

import (
	"sync"

	"fyne.io/fyne/v2"
)

// macOS's report that the window's backing properties changed (D-121, pixelscale.go): one observer
// on the window, its reports handed to one function. Cogpit has one window.

var (
	backingMu sync.Mutex
	backingFn func()
)

// watchBackingScale hands fn each of AppKit's reports that the window's backing properties changed
// (NSWindowDidChangeBackingPropertiesNotification: its scale factor or its colour space), on the
// main thread, in the main queue's pass after the report. Main goroutine, once the window exists;
// a window without an NSWindow (a test's) is not watched.
func watchBackingScale(w fyne.Window, fn func()) {
	win := placeNSWindow(w)
	if win == 0 {
		return
	}
	backingMu.Lock()
	backingFn = fn
	backingMu.Unlock()
	C.cogpitWatchBackingScale(C.uintptr_t(win))
}

// cogpitBackingScaleChanged is AppKit's report, on the main thread. It must not block.
//
//export cogpitBackingScaleChanged
func cogpitBackingScaleChanged() {
	backingMu.Lock()
	fn := backingFn
	backingMu.Unlock()
	if fn != nil {
		fn()
	}
}
