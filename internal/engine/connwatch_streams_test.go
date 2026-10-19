package engine

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/model"
)

// TestAPodLogResumesAfterTheInterruption is S7's logs: a followed pod log whose stream ends with the
// connection waits for it without asking the server again, then marks the gap ("── no
// connection 14:03:12 – 14:07:40 CEST (4 min 28 s) · …") and goes on from its last line's time, the line written meanwhile included and
// none twice.
func TestAPodLogResumesAfterTheInterruption(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	f := newServerLogs()
	const key = "web-1/app"
	t0 := time.Date(2026, 10, 15, 14, 3, 0, 0, time.UTC)
	line := func(ms int, text string) string {
		return t0.Add(time.Duration(ms)*time.Millisecond).Format(time.RFC3339Nano) + " " + text
	}
	f.add(key, t0.Add(100*time.Millisecond), "one")
	f.add(key, t0.Add(200*time.Millisecond), "two")
	ref := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "default", Name: "web-1"}
	tab := DockTab{ID: s.NewDockID(DockLogs), Type: DockLogs, Title: "web-1", Context: "test", Ref: &ref, Container: "app"}
	s.UpdateUI(func(u *UIState) { u.Dock = append(u.Dock, tab) })
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	lr.wait(t, line(100, "one"), line(200, "two"))
	waitUntil(t, "the stream to follow", func() bool { return f.opened(key) })

	p.set(func(f *fakeProber) { f.down = true })
	waitState(t, s, Interrupted)
	f.drop(key) // the stream ends with the connection
	f.add(key, t0.Add(1500*time.Millisecond), "during")
	time.Sleep(5 * fastWatch.BackoffFirst) // attempts that fail: no log request meanwhile
	if n := len(f.requestsFor(key)); n != 1 {
		t.Fatalf("%d log requests while interrupted, want the first only", n)
	}
	p.set(func(f *fakeProber) { f.down = false })
	waitState(t, s, Connected)
	lr.wait(t, line(1500, "during"))
	waitUntil(t, "the resumed stream to follow", func() bool { return f.opened(key) })
	f.add(key, t0.Add(2*time.Second), "after")
	lr.wait(t, line(2000, "after"))

	got := lr.all()
	if len(got) != 5 || got[0] != line(100, "one") || got[1] != line(200, "two") || !strings.HasPrefix(got[2], NoticePrefix+" "+interruptedMarkerStart) ||
		!strings.HasSuffix(got[2], " ──") || got[3] != line(1500, "during") || got[4] != line(2000, "after") {
		t.Errorf("lines %q", got)
	}
	reqs := f.requestsFor(key)
	if len(reqs) != 2 || !reqs[1].SinceTime.Equal(t0) || reqs[1].TailLines != 0 || !reqs[1].Follow {
		t.Errorf("the resumption's request: %+v", reqs)
	}
}

// TestAPodLogThatEndsWhileConnectedEndsAsBefore: a stream that ends while the connection answers
// (the container exited) ends the log; nothing is asked again.
func TestAPodLogThatEndsWhileConnectedEndsAsBefore(t *testing.T) {
	s, _ := watchedSession(t, fastWatch)
	f := newServerLogs()
	const key = "web-1/app"
	f.add(key, time.Now(), "one")
	ref := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "default", Name: "web-1"}
	tab := DockTab{ID: s.NewDockID(DockLogs), Type: DockLogs, Title: "web-1", Context: "test", Ref: &ref, Container: "app"}
	s.UpdateUI(func(u *UIState) { u.Dock = append(u.Dock, tab) })
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	waitUntil(t, "the stream to follow", func() bool { return f.opened(key) })
	f.drop(key)
	select {
	case <-lr.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the log did not end")
	}
	if n := len(f.requestsFor(key)); n != 1 {
		t.Errorf("%d requests", n)
	}
}

// fakeSession is a shell's session the test ends.
type fakeSession struct {
	once   sync.Once
	done   chan struct{}
	closed bool
	mu     sync.Mutex
}

func newFakeSession() *fakeSession { return &fakeSession{done: make(chan struct{})} }

func (f *fakeSession) Read([]byte) (int, error) { <-f.done; return 0, io.EOF }
func (f *fakeSession) Write(p []byte) (int, error) {
	return len(p), nil
}
func (f *fakeSession) Resize(uint16, uint16) error { return nil }
func (f *fakeSession) Done() <-chan struct{}       { return f.done }
func (f *fakeSession) Close() error {
	f.once.Do(func() {
		f.mu.Lock()
		f.closed = true
		f.mu.Unlock()
		close(f.done)
	})
	return nil
}

// TestAShellEndsWithTheConnection is S7's shells: a pod shell's session ends when the connection is
// interrupted, its tab says when; a local terminal is not tracked.
func TestAShellEndsWithTheConnection(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	ref := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "default", Name: "web-1"}
	tab := DockTab{ID: s.NewDockID(DockShell), Type: DockShell, Title: "Shell: web-1", Context: "test", Ref: &ref, Container: "app"}
	s.UpdateUI(func(u *UIState) { u.Dock = append(u.Dock, tab) })
	inner := newFakeSession()
	cancelled := make(chan struct{})
	sess := s.connWatch("").trackSession(tab.ID, inner, func() { close(cancelled) })
	p.set(func(f *fakeProber) { f.down = true })
	waitState(t, s, Interrupted)
	select {
	case <-inner.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the shell's session did not end")
	}
	<-cancelled // a dial under way ends too
	waitUntil(t, "the tab's mark", func() bool {
		got, ok := s.UIState().DockTab(tab.ID)
		return ok && !got.Interrupted.IsZero()
	})
	if !sess.(*trackedSession).interrupted() {
		t.Error("the session does not say the interruption ended it")
	}
	if err := sess.Close(); err != nil { // the window closes it again: once is enough
		t.Fatal(err)
	}
	// While interrupted, a shell does not even start.
	if _, err := s.OpenTerminal(context.Background(), tab.ID); !errors.Is(err, errConnInterrupted) {
		t.Errorf("a shell while interrupted: %v", err)
	}
}

// TestAShellOpenedDuringTheInterruptionEndsAtOnce: a session whose open raced the interruption is
// ended as the others.
func TestAShellOpenedDuringTheInterruptionEndsAtOnce(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	p.set(func(f *fakeProber) { f.down = true })
	waitState(t, s, Interrupted)
	inner := newFakeSession()
	s.connWatch("").trackSession("shell-9", inner, func() {})
	select {
	case <-inner.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("not ended")
	}
}

// TestUIStateStaysAcrossAnInterruption: the tabs, the view, the drawer and the navigator's scope
// stay while the connection is interrupted and after it is back; no welcome page shows.
func TestUIStateStaysAcrossAnInterruption(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	waitTable(t, s, "pods", func(tb *TableSnapshot) bool { return tb.Synced })
	ref := model.ObjectRef{Context: "test", Kind: "pods", Namespace: "default", Name: "web-1"}
	s.UpdateUI(func(u *UIState) { u.Drawer = &ref; u.Filter = "web" })
	before := s.UIState()
	p.set(func(f *fakeProber) { f.down = true })
	during := waitState(t, s, Interrupted)
	_ = during
	u := s.UIState()
	if u.View != before.View || u.Drawer == nil || u.Filter != "web" || u.Welcome || u.NavigatorScope != before.NavigatorScope ||
		!slices.EqualFunc(u.Tabs, before.Tabs, func(a, b ViewTab) bool { return a.ID == b.ID }) {
		t.Errorf("the UI changed with the interruption: view %q drawer %v filter %q welcome %v scope %q tabs %d/%d",
			u.View, u.Drawer, u.Filter, u.Welcome, u.NavigatorScope, len(u.Tabs), len(before.Tabs))
	}
	s.UpdateUI(func(u *UIState) { u.Filter = "web-1" }) // the user works on
	p.set(func(f *fakeProber) { f.down = false })
	waitState(t, s, Connected)
	if u := s.UIState(); u.Filter != "web-1" || u.Drawer == nil || u.View != before.View {
		t.Errorf("after the reconnect: view %q drawer %v filter %q", u.View, u.Drawer, u.Filter)
	}
}
