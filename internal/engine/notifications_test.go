package engine

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// notificationSession is a session without a cluster on a fixed clock that records its events.
func notificationSession(t *testing.T) (*Session, func() []Event) {
	t.Helper()
	at := time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)
	var tick atomic.Int64
	s := New(Options{Now: func() time.Time { return at.Add(time.Duration(tick.Add(1)) * time.Second) }})
	t.Cleanup(s.Close)
	var mu sync.Mutex
	var events []Event
	s.Subscribe(func(ev Event) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})
	return s, func() []Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]Event(nil), events...)
	}
}

// Every toast the engine emits goes into the log with its level and time, before the subscribers
// see it: the toast event carries its id, and a UI state with the new summary comes first.
func TestToastsAreRecorded(t *testing.T) {
	s, events := notificationSession(t)
	s.Emit(Event{Type: EventToast, Level: LevelWarning, Message: "Node n1: the eviction of pod web-0 waits"})
	s.Emit(Event{Type: EventToast, Level: LevelError, Message: "the Pods view failed to update: internal error"})
	s.Emit(Event{Type: EventToast, Level: "", Message: "Release web uninstalled"}) // no level: an info
	s.Emit(Event{Type: EventToast, Level: LevelInfo})                              // no text: nothing to show or keep

	list := s.Notifications(false)
	if len(list.Notifications) != 3 || list.Total != 3 || list.Unread != 3 || list.UnreadLevel != LevelError || list.Latest != 3 {
		t.Fatalf("the log: %+v", list)
	}
	newest, oldest := list.Notifications[0], list.Notifications[2]
	if newest.ID != 3 || newest.Level != LevelInfo || newest.Message != "Release web uninstalled" || newest.Read {
		t.Errorf("newest first: %+v", newest)
	}
	if oldest.Level != LevelWarning || !oldest.Time.Equal(time.Date(2026, 10, 13, 2, 0, 1, 0, time.UTC)) {
		t.Errorf("the oldest keeps its level and time: %+v", oldest)
	}
	if got := s.UIState().Notifications; got != list.NotificationSummary {
		t.Errorf("UI state %+v, log %+v", got, list.NotificationSummary)
	}

	var toasts []Event
	sawState := false
	for _, ev := range events() {
		switch ev.Type {
		case EventToast:
			if !sawState {
				t.Error("a toast reached the subscribers before the UI state that counts it")
			}
			toasts = append(toasts, ev)
		case EventUIState:
			sawState = true
		}
	}
	if len(toasts) != 4 || toasts[0].Notification != 1 || toasts[2].Notification != 3 || toasts[3].Notification != 0 {
		t.Errorf("the toasts' ids: %+v", toasts)
	}
}

// The log keeps the newest MaxNotifications; ids go on, and the summary counts what is kept.
func TestNotificationLogIsBounded(t *testing.T) {
	s, _ := notificationSession(t)
	for i := 1; i <= MaxNotifications+25; i++ {
		s.Emit(Event{Type: EventToast, Level: LevelInfo, Message: fmt.Sprintf("toast %d", i)})
	}
	list := s.Notifications(true)
	if len(list.Notifications) != MaxNotifications || list.Total != MaxNotifications || list.Unread != MaxNotifications {
		t.Fatalf("%d kept, summary %+v", len(list.Notifications), list.NotificationSummary)
	}
	if first, last := list.Notifications[0], list.Notifications[MaxNotifications-1]; first.ID != MaxNotifications+25 ||
		first.Message != fmt.Sprintf("toast %d", MaxNotifications+25) || last.ID != 26 {
		t.Errorf("newest %+v, oldest kept %+v", first, last)
	}
}

// Marking read: the ids named, or every one; the summary's unread count and level follow, and an
// unchanged log publishes no new UI state.
func TestMarkNotificationsRead(t *testing.T) {
	s, _ := notificationSession(t)
	s.Emit(Event{Type: EventToast, Level: LevelError, Message: "first"})
	s.Emit(Event{Type: EventToast, Level: LevelWarning, Message: "second"})
	s.Emit(Event{Type: EventToast, Level: LevelInfo, Message: "third"})

	u := s.MarkNotificationsRead([]uint64{1, 99})
	if n := u.Notifications; n.Unread != 2 || n.UnreadLevel != LevelWarning || n.Total != 3 {
		t.Errorf("after marking the error read: %+v", n)
	}
	if list := s.Notifications(true); !list.Notifications[2].Read || list.Notifications[1].Read {
		t.Errorf("read flags: %+v", list.Notifications)
	}
	rev := s.UIState().Revision
	if again := s.MarkNotificationsRead([]uint64{1}); again.Revision != rev {
		t.Errorf("marking a read one again moved the revision %d → %d", rev, again.Revision)
	}
	u = s.MarkNotificationsRead(nil)
	if n := u.Notifications; n.Unread != 0 || n.UnreadLevel != "" || n.Total != 3 || u.Revision != rev+1 {
		t.Errorf("after marking every one read: %+v (revision %d)", n, u.Revision)
	}
}

// Clearing empties the log; the ids of later toasts go on, so an old id never names a new toast.
func TestClearNotifications(t *testing.T) {
	s, _ := notificationSession(t)
	s.Emit(Event{Type: EventToast, Level: LevelError, Message: "first"})
	s.Emit(Event{Type: EventToast, Level: LevelInfo, Message: "second"})
	u := s.ClearNotifications()
	if u.Notifications != (NotificationSummary{}) || len(s.Notifications(true).Notifications) != 0 {
		t.Fatalf("after clearing: %+v", u.Notifications)
	}
	if again := s.ClearNotifications(); again.Revision != u.Revision {
		t.Error("clearing an empty log published a new UI state")
	}
	s.Emit(Event{Type: EventToast, Level: LevelInfo, Message: "third"})
	if list := s.Notifications(true); len(list.Notifications) != 1 || list.Notifications[0].ID != 3 || list.Latest != 3 {
		t.Errorf("after clearing and one more: %+v", list)
	}
}

// The window's own toasts and an engine toast marked WindowOnly reach every caller but the window
// as text since A19 (owner), filtered (no Secret is cached here, so the text is the window's); the
// others read in full. An EmitWindowToast's inline values are removed for them.
func TestWindowOnlyNotificationsReachAgents(t *testing.T) {
	s, events := notificationSession(t)
	id := s.RecordWindowToast(LevelError, "helm.chart.install: template: name LOOKED-UP-SECRET-DATA is not valid")
	s.Emit(Event{Type: EventToast, Level: LevelWarning, Message: "Release web was not changed: LOOKED-UP-SECRET-DATA", WindowOnly: true})
	s.Emit(Event{Type: EventToast, Level: LevelInfo, Message: "Release web uninstalled"})
	if id != 1 {
		t.Errorf("the window's toast got id %d", id)
	}
	for _, ev := range events() {
		if ev.Type == EventToast && ev.Notification == id {
			t.Error("the window's own toast came back to it as an event")
		}
	}
	agent, window := s.Notifications(false), s.Notifications(true)
	if len(agent.Notifications) != 3 || len(window.Notifications) != 3 {
		t.Fatalf("agent %d, window %d", len(agent.Notifications), len(window.Notifications))
	}
	for i, n := range agent.Notifications {
		w := window.Notifications[i]
		if n.ID != w.ID || n.Level != w.Level || !n.Time.Equal(w.Time) {
			t.Errorf("the agent's %+v differs from the window's %+v beyond the text", n, w)
		}
		switch {
		case n.Message == model.Withheld:
			t.Errorf("an agent reads %q withheld", w.Message)
		case n.Message != w.Message:
			t.Errorf("an agent reads %q for %q", n.Message, w.Message)
		}
	}
	if window.Notifications[2].Message != "helm.chart.install: template: name LOOKED-UP-SECRET-DATA is not valid" ||
		!window.Notifications[2].WindowOnly || !window.Notifications[1].WindowOnly || window.Notifications[0].WindowOnly {
		t.Errorf("the window's view: %+v", window.Notifications)
	}

	s.EmitWindowToast(LevelWarning, "upgrade failed: value adminPassword hunter2-long at x; replicas 3", "adminPassword: hunter2-long\nreplicas: 3\n")
	agent, window = s.Notifications(false), s.Notifications(true)
	if got := agent.Notifications[0].Message; got != "upgrade failed: value adminPassword «inline value» at x; replicas 3" {
		t.Errorf("the agent reads %q", got) // "3" is shorter than minInlineValue: kept
	}
	if got := window.Notifications[0].Message; !strings.Contains(got, "hunter2-long") {
		t.Errorf("the window reads %q", got)
	}
}

// withoutInlineValues removes the scalars of the inline values, longest first, and takes text that
// is no YAML line by line.
func TestWithoutInlineValues(t *testing.T) {
	for _, c := range []struct{ msg, inline, want string }{
		{"no values", "", "no values"},
		{"a secretvalue and secretvalue2", "a: secretvalue\nb: secretvalue2\n", "a «inline value» and «inline value»"},
		{"nested deep-value-x, list item-one", "x:\n  y: deep-value-x\nl: [item-one]\n", "nested «inline value», list «inline value»"},
		{"short abc kept", "k: abc\n", "short abc kept"},
		{"broken [yaml-value", "k: [yaml-value\n", "broken «inline value»"},
	} {
		if got := withoutInlineValues(c.msg, c.inline); got != c.want {
			t.Errorf("withoutInlineValues(%q, %q) = %q, want %q", c.msg, c.inline, got, c.want)
		}
	}
}

// The log is written from many goroutines (view workers, commands, the window) while it is read
// and marked: run with -race.
func TestNotificationLogConcurrent(t *testing.T) {
	s, _ := notificationSession(t)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 50 {
				switch i % 5 {
				case 0:
					s.RecordWindowToast(LevelError, fmt.Sprintf("window %d/%d", g, i))
				case 1:
					s.MarkNotificationsRead(nil)
				case 2:
					_ = s.Notifications(g%2 == 0)
				default:
					s.Emit(Event{Type: EventToast, Level: LevelWarning, Message: fmt.Sprintf("engine %d/%d", g, i)})
				}
			}
		}()
	}
	wg.Wait()
	list := s.Notifications(true)
	if list.Latest != 8*30 || list.Total != MaxNotifications || s.UIState().Notifications != list.NotificationSummary {
		t.Errorf("after the writers: %+v, UI state %+v", list.NotificationSummary, s.UIState().Notifications)
	}
}
