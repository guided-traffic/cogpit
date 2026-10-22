package agent

import (
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// testClock is the agent's clock (Options.Now) where the test moves it.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// TestNotifyWhileATimerIsPending: a streamed change that comes while a throttled EventChat is
// already pending is not sent on its own and arms no timer of its own: the window hears nothing,
// also when such a timer would have been due at once, and the pending notification stays as it was
// (still pending, the last EventChat's time unchanged). Once NotifyEvery has passed since the last
// EventChat, the next change is sent at once with the latest revision, and the timer still armed
// finds nothing left to send.
//
// The agent's clock is the test's, its timers run on the bubble's (testing/synctest): the timer the
// first delta arms is due an hour of the bubble's time later, while a timer the second delta armed
// would be due a nanosecond later; a step of the bubble's time runs every timer that came due, and
// returns only once their goroutines are done.
func TestNotifyWhileATimerIsPending(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := engine.New(engine.Options{})
		t.Cleanup(s.Close)
		clock := &testClock{now: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}
		a := New(Options{Session: s, Now: clock.Now, NotifyEvery: time.Hour})
		conv, err := a.NewConversation("test", "")
		if err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		var sent []uint64 // the revisions of the conversation's EventChat, as the window hears them
		t.Cleanup(s.Subscribe(func(ev engine.Event) {
			if ev.Type == engine.EventChat && ev.View == conv.ID {
				mu.Lock()
				sent = append(sent, ev.Revision)
				mu.Unlock()
			}
		}))
		heard := func() []uint64 {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(sent)
		}
		a.mu.Lock()
		c := a.convs[conv.ID]
		a.mu.Unlock()
		// change is what a streamed delta does before it notifies: the conversation's next revision.
		change := func() uint64 {
			a.mu.Lock()
			defer a.mu.Unlock()
			return c.bump()
		}
		pending := func() (bool, time.Time) {
			a.mu.Lock()
			defer a.mu.Unlock()
			return c.timer, c.lastNotify
		}

		first := change()
		a.notify(c, true) // a state change: sent at once
		sentAt := clock.Now()

		change()
		a.notify(c, false) // a delta right after it: a timer for when NotifyEvery has passed
		if on, _ := pending(); !on {
			t.Fatal("a delta within NotifyEvery left no notification pending")
		}

		// Another delta while that notification is pending, a moment before it is due: a timer of
		// its own would be due at once and send this revision early.
		clock.add(time.Hour - time.Nanosecond)
		change()
		a.notify(c, false)
		time.Sleep(time.Millisecond) // the bubble's time: what came due has run
		if got := heard(); !slices.Equal(got, []uint64{first}) {
			t.Fatalf("the window heard revisions %v while a notification was pending, want only %d", got, first)
		}
		if on, last := pending(); !on || !last.Equal(sentAt) {
			t.Errorf("after a delta while a notification was pending: pending %v, the last EventChat at %s; want it pending, the last at %s",
				on, last, sentAt)
		}

		clock.add(time.Nanosecond) // NotifyEvery since the last EventChat
		latest := change()
		a.notify(c, false)
		if got := heard(); !slices.Equal(got, []uint64{first, latest}) {
			t.Errorf("the window heard revisions %v, want %d and then the latest, %d, at once", got, first, latest)
		}
		if on, last := pending(); on || !last.Equal(clock.Now()) {
			t.Errorf("after the change sent at once: pending %v, the last EventChat at %s; want none pending, the last now", on, last)
		}

		// The timer the second delta armed comes due an hour of the bubble's time after it: the
		// change sent at once took what it had to send, so the timer sends nothing.
		time.Sleep(time.Hour)
		if got := heard(); !slices.Equal(got, []uint64{first, latest}) {
			t.Errorf("after the pending timer came due, the window heard revisions %v, want only %d and %d", got, first, latest)
		}
	})
}
