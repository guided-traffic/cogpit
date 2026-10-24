//go:build darwin

package fyneui

/*
#cgo LDFLAGS: -framework AppKit
#include "reducemotion_darwin.h"
*/
import "C"

import "sync"

// systemReduceMotion reports macOS's Reduce motion (System Settings › Accessibility › Display),
// which stops Cogpit's motion as its own preference does (shell.reduceMotion, preferences_motion.go).
// A property read, cheap: it is read whenever a motion would start, so a change shows with the
// next one; the light strip, which moves for as long as it shows, follows a change at once
// (watchReduceMotion).
func systemReduceMotion() bool { return C.cogpitReduceMotion() != 0 }

var (
	motionMu   sync.Mutex
	motionFn   func()
	motionOnce sync.Once
)

// cogpitReduceMotionChanged is called by the Objective-C side on AppKit's main thread when the
// display's accessibility options changed. It must not block.
//
//export cogpitReduceMotionChanged
func cogpitReduceMotionChanged() {
	motionMu.Lock()
	fn := motionFn
	motionMu.Unlock()
	if fn != nil {
		fn()
	}
}

// watchReduceMotion calls fn whenever macOS's display accessibility options change (Reduce motion
// among them: NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification), from now on; the
// observer starts once per process.
func watchReduceMotion(fn func()) {
	motionMu.Lock()
	motionFn = fn
	motionMu.Unlock()
	motionOnce.Do(func() { C.cogpitWatchReduceMotion() })
}
