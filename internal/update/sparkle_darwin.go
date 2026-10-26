//go:build darwin && cgo

package update

/*
#cgo CFLAGS: -fno-objc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "sparkle_darwin.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"
)

// detailSize bounds the text the Objective-C side hands back.
const detailSize = 512

var (
	// checkInterval is how often the scheduled checks read the feed: an hour (owner, 2026-10-16,
	// D-4), as on Linux and Windows.
	checkInterval = time.Hour
	// probe runs Sparkle's probing check (sparkle_darwin.m): it reads the feed and reports what it
	// found, holding nothing for later.
	probe = func() { C.cogpitSparkleProbe() }
)

// scheduled keeps Cogpit's scheduled checks. Sparkle's own stay off (sparkle_darwin.m): a release
// one of them finds is held until the user acts on it, and no check runs while it is held.
var scheduled struct {
	mu   sync.Mutex
	stop chan struct{} // closed to end the scheduled checks; nil while they are off
}

// call runs f with a buffer for the detail text and returns its result and the text.
func call(f func(buf *C.char, size C.size_t) C.int) (int, string) {
	buf := (*C.char)(C.calloc(detailSize, 1))
	defer C.free(unsafe.Pointer(buf))
	code := f(buf, detailSize)
	return int(code), C.GoString(buf)
}

// load checks the bundle and loads Sparkle.framework (sparkle_darwin.m).
func load() error {
	code, detail := call(func(buf *C.char, size C.size_t) C.int { return C.cogpitSparkleLoad(buf, size) })
	switch code {
	case C.COGPIT_SPARKLE_OK:
		return nil
	case C.COGPIT_SPARKLE_NO_BUNDLE:
		return ErrNoBundle
	case C.COGPIT_SPARKLE_NOT_INSTALLED:
		return ErrNotInstalled
	case C.COGPIT_SPARKLE_NO_FRAMEWORK:
		return ErrNoFramework
	case C.COGPIT_SPARKLE_NO_FEED:
		return ErrNoFeed
	case C.COGPIT_SPARKLE_NO_KEY:
		return ErrNoKey
	}
	return fmt.Errorf("the updater could not be loaded: %s", detail)
}

// startFailure is why Sparkle did not start, once it tried.
func startFailure() error {
	code, detail := call(func(buf *C.char, size C.size_t) C.int { return C.cogpitSparkleState(buf, size) })
	if code == C.COGPIT_SPARKLE_FAILED {
		return errors.New("the updater did not start: " + detail)
	}
	return nil
}

// start starts Sparkle with its own scheduled checks off, then Cogpit's when they are on: Sparkle
// starts on the main queue, and the first check, queued after it, runs once it has.
func start(automaticChecks bool) {
	C.cogpitSparkleStart(C.bool(true))
	setAutomaticChecks(automaticChecks)
}

func check() { C.cogpitSparkleCheck() }

func setAutomaticChecks(on bool) {
	scheduled.mu.Lock()
	defer scheduled.mu.Unlock()
	switch {
	case on && scheduled.stop == nil:
		scheduled.stop = make(chan struct{})
		go scheduledChecks(scheduled.stop, checkInterval, probe)
	case !on && scheduled.stop != nil:
		close(scheduled.stop)
		scheduled.stop = nil
	}
}

// scheduledChecks runs check at once and every interval until stop closes.
func scheduledChecks(stop <-chan struct{}, interval time.Duration, check func()) {
	defer func() { _ = recover() }() // D-11: a check's panic never ends the app
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		check()
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

// cogpitSparkleUpdateChanged is the delegate's report (sparkle_darwin.m): a check found a newer
// release, with length bytes of its version (at most COGPIT_SPARKLE_VERSION_MAX), or found none,
// or the update session of the user's check ended. It runs on the main thread inside AppKit's event dispatch, so it only keeps
// the report: the listeners run on goroutines of their own.
//
//export cogpitSparkleUpdateChanged
func cogpitSparkleUpdateChanged(available C.bool, version *C.char, length C.int) {
	v := ""
	if version != nil && length > 0 && length <= C.COGPIT_SPARKLE_VERSION_MAX {
		v = C.GoStringN(version, length)
	}
	notice.set(bool(available), v)
}
