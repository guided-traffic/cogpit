package fyneui

import (
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
