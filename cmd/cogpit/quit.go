package main

import (
	"sync"
	"time"
)

// quitBound is how long Fyne gets to end its run once Cogpit handed it the quit (D-136,
// fyneui.Options.QuitBound). Its teardown takes a fraction of a second at a normal quit, also under
// load; one that waits on its own lifecycle queue (Fyne 2.8.2) never ends.
const quitBound = 5 * time.Second

// runEnd is what run does at its return, the last added first, once: the closes it would defer,
// kept in a list so that the quit bound's goroutine runs them when Fyne's teardown hangs and run
// never returns (quitHung).
type runEnd struct {
	mu  sync.Mutex
	fns []func()
}

func (e *runEnd) add(fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.fns = append(e.fns, fn)
}

func (e *runEnd) run() {
	e.mu.Lock()
	fns := e.fns
	e.fns = nil
	e.mu.Unlock()
	for i := len(fns) - 1; i >= 0; i-- {
		fns[i]()
	}
}

// quitHung is fyneui.Options.QuitHung: what follows run's return after a normal quit, on the quit
// bound's goroutine after fyneui.Run's own end ran there. A smoke run judges the hang by every
// goroutine's stack; then run's end runs, and exit ends the process with the code a normal quit
// has.
func quitHung(smoke *smokeRun, end *runEnd, exit func(code int)) func() {
	return func() {
		smoke.quitBoundPassed(goroutineStacks())
		end.run()
		exit(0)
	}
}
