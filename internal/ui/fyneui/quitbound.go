package fyneui

import (
	"log/slog"
	"sync"
	"time"
)

// The quit bound (D-136). Fyne 2.8.2's teardown can wait for good on its own lifecycle queue with
// the window gone, and Run never returns (docs/parity/gaps.md, G16). The bound starts when Cogpit
// has handed the quit to Fyne: at the window's close, once Cogpit's own part of it ran, or, for a
// signal or the updater's request, once Fyne's queue ran or released the request's a.Quit (Fyne's
// own signal handler ends the run without the window's close). Fyne's return ends it, before Run's
// own end. Should the bound pass first, its goroutine logs one warning and runs what would follow
// Fyne's return: Run's end and the caller's (Options.QuitHung), which ends the process. Whichever of
// the two gets there first runs that end, once; none of Cogpit's own shutdown runs under the bound.

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
