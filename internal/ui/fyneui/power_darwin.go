//go:build darwin

package fyneui

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include "power_darwin.h"
*/
import "C"

import "sync"

// systemOnBattery reports whether the Mac runs on its battery (IOKit's providing power source,
// power_darwin.m); a Mac without a battery (Mac Studio, iMac, Mac mini) never does. A snapshot of
// the power sources, cheap: it is read whenever the light strip decides its look
// (shell.batteryStopsStrip).
func systemOnBattery() bool { return C.cogpitOnBattery() != 0 }

var (
	powerMu   sync.Mutex
	powerFn   func()
	powerLast bool // the state the last call of powerFn followed
	powerOnce sync.Once
)

// cogpitPowerChanged is called by the IOKit side on AppKit's main thread whenever a power source's
// information changed, a battery's charge too: it calls the handler only when the computer went
// from the power adapter to its battery or back. It must not block.
//
//export cogpitPowerChanged
func cogpitPowerChanged() {
	now := systemOnBattery()
	powerMu.Lock()
	fn, changed := powerFn, now != powerLast
	powerLast = now
	powerMu.Unlock()
	if fn != nil && changed {
		fn()
	}
}

// watchPower calls fn whenever the Mac goes from its power adapter to its battery or back, from
// now on. IOKit's notification starts once per process and stays, as watchReduceMotion's observer
// does, so stop (the polls' end on Windows and Linux) is not needed here: fn posts to the shell,
// which drops work once it stopped.
func watchPower(_ <-chan struct{}, fn func()) {
	now := systemOnBattery()
	powerMu.Lock()
	powerFn, powerLast = fn, now
	powerMu.Unlock()
	powerOnce.Do(func() { C.cogpitWatchPower() })
}
