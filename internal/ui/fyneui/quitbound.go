package fyneui

import (
	"fmt"
	"log/slog"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The quit bound (D-136). Fyne 2.8.2's teardown can wait for good on its own lifecycle queue with
// the window gone, and Run never returns (docs/parity/gaps.md, G16). The bound starts when Cogpit
// has handed the quit to Fyne: at the window's close, once Cogpit's own part of it ran, or, for a
// signal or the updater's request, once Fyne took the request's a.Quit (handQuit; Fyne's own signal
// handler ends the run without the window's close). Fyne's return ends it, before Run's own end.
// Should the bound pass first, its goroutine logs one warning and runs what would follow Fyne's
// return: Run's end and the caller's (Options.QuitHung), which ends the process. Whichever of the
// two gets there first runs that end, once; none of Cogpit's own shutdown runs under the bound.

// The states of a quitBound.
const (
	quitIdle     = iota // the quit is not handed to Fyne yet
	quitCounting        // the bound runs
	quitReturned        // Fyne's run returned: Run's end runs on the main goroutine
	quitPassed          // the bound passed: its goroutine runs the end
)

// quitBound bounds Fyne's own teardown at a quit, as above. A nil one bounds nothing.
type quitBound struct {
	bound time.Duration
	end   func() // what follows Fyne's return, on the bound's goroutine; it ends the process
	log   *slog.Logger

	mu    sync.Mutex
	state int
	timer *time.Timer
	since time.Time // when the bound started
}

// newQuitBound returns a bound of the given time with its end, nil for no bound (none or no end).
func newQuitBound(bound time.Duration, end func()) *quitBound {
	if bound <= 0 || end == nil {
		return nil
	}
	return &quitBound{bound: bound, end: end, log: slog.Default()}
}

// arm starts the bound: Cogpit has handed the quit to Fyne. Only the first call starts it.
func (q *quitBound) arm() {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.state != quitIdle {
		return
	}
	q.state, q.since = quitCounting, time.Now()
	q.timer = time.AfterFunc(q.bound, q.pass)
}

// returned is Fyne's return from its run. It reports whether Run goes on with its end: false once
// the bound passed, whose goroutine runs that end and ends the process.
func (q *quitBound) returned() bool {
	if q == nil {
		return true
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	switch q.state {
	case quitPassed:
		return false
	case quitCounting:
		q.timer.Stop()
		q.log.Debug("Fyne ended its run", "after", time.Since(q.since).Round(time.Millisecond), "bound", q.bound)
	}
	q.state = quitReturned
	return true
}

// pass is the bound's end: unless Fyne's run returned first, the warning and the end, here.
func (q *quitBound) pass() {
	q.mu.Lock()
	if q.state != quitCounting {
		q.mu.Unlock()
		return
	}
	q.state = quitPassed
	q.mu.Unlock()
	q.log.Warn("Fyne did not end its run within the quit bound; Cogpit ends the process after its own shutdown", "bound", q.bound)
	q.end()
}

// handQuit is the quit request's goroutine (a signal, Options.Quit): quit handed to Fyne with
// doAndWait unless stopped (the window's close ran already), then the bound armed. Fyne runs the
// call in its loop or releases it unrun at its drain; after the drain it runs it at once on this
// goroutine, with GLFW terminated, so Run passes an a.Quit that runs only on the loop's goroutine
// (onGoroutine). A panic here (Fyne's queue closing under the call's send) is logged and ends
// nothing: the bound starts either way.
func (q *quitBound) handQuit(stopped func() bool, doAndWait func(func()), quit func()) {
	defer q.arm()
	defer func() {
		if r := recover(); r != nil {
			log := slog.Default()
			if q != nil {
				log = q.log
			}
			log.Error("the quit request panicked in Fyne", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	if !stopped() {
		doAndWait(quit)
	}
}

// onGoroutine returns fn to run only on the goroutine numbered id; on any other it does nothing.
func onGoroutine(id uint64, fn func()) func() {
	return func() {
		if goroutineID() == id {
			fn()
		}
	}
}

// goroutineID is the calling goroutine's number from the first line of its stack ("goroutine 7
// [running]:"), as Fyne tells its main goroutine. 0 when the line has another form: every goroutine
// then has the same number, and onGoroutine runs fn on any.
func goroutineID() uint64 {
	var buf [64]byte
	line, _, _ := strings.Cut(string(buf[:runtime.Stack(buf[:], false)]), "\n")
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "goroutine" {
		return 0
	}
	id, err := strconv.ParseUint(f[1], 10, 64)
	if err != nil {
		return 0
	}
	return id
}
