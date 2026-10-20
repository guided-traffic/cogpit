//go:build darwin

package fyneui

/*
#cgo LDFLAGS: -framework AppKit
#include "reducemotion_darwin.h"
*/
import "C"

// systemReduceMotion reports macOS's Reduce motion (System Settings › Accessibility › Display),
// which stops Cogpit's motion as its own preference does (shell.reduceMotion, preferences_motion.go).
// A property read, cheap: it is read whenever a motion would start, so a change shows with the
// next one.
func systemReduceMotion() bool { return C.cogpitReduceMotion() != 0 }
