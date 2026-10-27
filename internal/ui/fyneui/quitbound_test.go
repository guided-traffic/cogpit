package fyneui

import (
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// newTestQuitBound is a quit bound of 5 s whose end counts its runs and whose log is returned.
func newTestQuitBound(t *testing.T) (*quitBound, *atomic.Int32, *lockedBuffer) {
	t.Helper()
	var ends atomic.Int32
	q := newQuitBound(5*time.Second, func() { ends.Add(1) })
	if q == nil {
		t.Fatal("no bound")
	}
	logs := &lockedBuffer{}
	q.log = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return q, &ends, logs
}

// TestQuitBoundEndsAHungTeardown: armed, the bound passes at its time when Fyne's run has not
// returned, not before; its end runs once after one warning that names the bound, and Fyne's late
// return does not go on. A second arm keeps the first's time.
func TestQuitBoundEndsAHungTeardown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q, ends, logs := newTestQuitBound(t)
		q.arm()
		time.Sleep(3 * time.Second)
		q.arm() // the quit request's arm after the window's close
		time.Sleep(2*time.Second - time.Nanosecond)
		synctest.Wait()
		if n := ends.Load(); n != 0 {
			t.Fatalf("the end ran %d times before the bound passed", n)
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if n := ends.Load(); n != 1 {
			t.Fatalf("the end ran %d times once the bound passed, want 1", n)
		}
		if q.returned() {
			t.Error("Fyne's return after the bound passed goes on with Run's end")
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if n := ends.Load(); n != 1 {
			t.Errorf("the end ran %d times, want 1", n)
		}
		out := logs.String()
		if strings.Count(out, "level=WARN") != 1 ||
			!strings.Contains(out, `msg="Fyne did not end its run within the quit bound; Cogpit ends the process after its own shutdown" bound=5s`) {
			t.Errorf("the log is not the one warning naming the bound:\n%s", out)
		}
	})
}

// TestQuitBoundDisarmedByFynesReturn: Fyne's run returning within the bound goes on with Run's end
// and disarms the bound for good: it never passes, warns or runs the end, and a later arm (a
// signal after the run) starts nothing. The return logs its time at debug level.
func TestQuitBoundDisarmedByFynesReturn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q, ends, logs := newTestQuitBound(t)
		q.arm()
		time.Sleep(120 * time.Millisecond)
		if !q.returned() {
			t.Fatal("Fyne's return within the bound does not go on")
		}
		q.arm()
		time.Sleep(time.Minute)
		synctest.Wait()
		if n := ends.Load(); n != 0 {
			t.Errorf("the end ran %d times after a normal return", n)
		}
		out := logs.String()
		if strings.Contains(out, "level=WARN") || !strings.Contains(out, `msg="Fyne ended its run" after=120ms bound=5s`) {
			t.Errorf("the log after a normal return:\n%s", out)
		}
	})
}

// TestQuitBoundUnarmed: a run whose quit was never handed to Fyne goes on at its return, and
// nothing passes later; without a time or an end there is no bound, and a nil one goes on.
func TestQuitBoundUnarmed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q, ends, logs := newTestQuitBound(t)
		if !q.returned() {
			t.Fatal("an unarmed bound keeps Run from its end")
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if n := ends.Load(); n != 0 || logs.String() != "" {
			t.Errorf("an unarmed bound ran its end %d times, logged %q", n, logs.String())
		}
	})
	if newQuitBound(0, func() {}) != nil || newQuitBound(time.Second, nil) != nil {
		t.Error("a bound without a time or an end")
	}
	var none *quitBound
	none.arm()
	if !none.returned() {
		t.Error("no bound keeps Run from its end")
	}
}

// errGLFWNotInitialized is go-gl/glfw's panic when a call reaches GLFW after its Terminate.
var errGLFWNotInitialized = errors.New("NotInitialized: The GLFW library is not initialized")

// The ways Fyne takes a quit request's fyne.DoAndWait, given its loop's queue.
var (
	// inLoop: the loop runs the call on its goroutine.
	inLoop = func(loop chan<- func()) func(func()) {
		return func(fn func()) {
			done := make(chan struct{})
			loop <- func() { defer close(done); fn() }
			<-done
		}
	}
	// atOnce: the loop drained its queue at the end of its run; Fyne runs the call on the caller's
	// goroutine.
	atOnce = func(chan<- func()) func(func()) { return func(fn func()) { fn() } }
	// closingQueue: the call's send meets Fyne's queue closing at the end of its run.
	closingQueue = func(chan<- func()) func(func()) {
		return func(fn func()) {
			q := make(chan func())
			close(q)
			q <- fn
		}
	}
)

// TestQuitRequestAfterFynesDrain: the quit request's hand-over to Fyne (handQuit). a.Quit runs in
// Fyne's loop, and not when Fyne, its loop drained, runs the call at once on the request's
// goroutine, where its window close would panic in the terminated GLFW (here every a.Quit off the
// loop's goroutine panics so). A panic on the request's goroutine, of Fyne's closing queue or of an
// a.Quit without the guard, is logged once and crashes nothing. In every case the bound starts and
// passes once.
func TestQuitRequestAfterFynesDrain(t *testing.T) {
	for _, c := range []struct {
		name      string
		stopped   bool
		doAndWait func(loop chan<- func()) func(func())
		guarded   bool  // a.Quit runs only on the loop's goroutine (onGoroutine), as Run passes it
		quits     int32 // a.Quit's runs
		logged    string
	}{
		{"Fyne runs it in its loop", false, inLoop, true, 1, ""},
		{"Fyne runs it at once after its drain", false, atOnce, true, 0, ""},
		{"the window's close ran already", true, inLoop, true, 0, ""},
		{"Fyne's queue closes under the call", false, closingQueue, true, 0,
			`level=ERROR msg="the quit request panicked in Fyne" panic="send on closed channel" stack=`},
		{"an a.Quit without the guard panics in GLFW", false, atOnce, false, 1,
			`level=ERROR msg="the quit request panicked in Fyne" panic="NotInitialized: The GLFW library is not initialized" stack=`},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				q, ends, logs := newTestQuitBound(t)
				loopID, loop := goroutineID(), make(chan func())
				var quits atomic.Int32
				quit := func() {
					quits.Add(1)
					if goroutineID() != loopID {
						panic(errGLFWNotInitialized) // GLFW ends with Fyne's loop
					}
				}
				if c.guarded {
					quit = onGoroutine(loopID, quit)
				}
				handed := make(chan struct{})
				go func() {
					defer close(handed)
					q.handQuit(func() bool { return c.stopped }, c.doAndWait(loop), quit)
				}()
				for running := true; running; { // Fyne's loop
					select {
					case fn := <-loop:
						fn()
					case <-handed:
						running = false
					}
				}
				if n := quits.Load(); n != c.quits {
					t.Errorf("a.Quit ran %d times, want %d", n, c.quits)
				}
				time.Sleep(5 * time.Second)
				synctest.Wait()
				if n := ends.Load(); n != 1 {
					t.Errorf("the bound's end ran %d times, want 1", n)
				}
				out := logs.String()
				if c.logged == "" && strings.Contains(out, "level=ERROR") ||
					c.logged != "" && (strings.Count(out, "level=ERROR") != 1 || !strings.Contains(out, c.logged)) {
					t.Errorf("the log, want one line %q or none:\n%s", c.logged, out)
				}
			})
		})
	}
	var none *quitBound
	none.handQuit(func() bool { return false }, func(fn func()) { fn() }, func() {}) // no bound: nothing to start
}
