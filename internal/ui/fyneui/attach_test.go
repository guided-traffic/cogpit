package fyneui

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/terminal"
)

// attachEngine is the fake engine with what an attach session to a REPL shows.
type attachEngine struct {
	*fakeEngine
	last *atomic.Pointer[openStream]
}

const replOutput = "[cogpit] attached to orders-repl-0/repl; if no prompt shows, press Enter\r\n" +
	"orders-repl v2.3.1 (python 3.12.4)\r\n>>> orders.count(status=\"open\")\r\n1284\r\n>>> "

func (a attachEngine) OpenTerminal(ctx context.Context, tabID string) (terminal.Session, error) {
	if !strings.HasPrefix(tabID, "attach-") {
		return a.fakeEngine.OpenTerminal(ctx, tabID)
	}
	a.mu.Lock()
	a.opened[tabID]++
	a.mu.Unlock()
	s := newGatedStream(a.fakeEngine, tabID, replOutput)
	a.last.Store(s)
	return s, nil
}

// openStream is a session that writes its output and then keeps running, as an attached REPL
// does, until it is closed.
type openStream struct {
	fakeStream
	pw      *io.PipeWriter
	gate    chan struct{} // closed when the output may flow
	reading atomic.Bool   // the reader has asked for output
	size    int64         // bytes of output
	taken   atomic.Int64  // bytes the reader has taken
	idle    atomic.Bool   // it had taken them all and asked for more: it processed them
}

func newOpenStream(f *fakeEngine, id, out string) *openStream {
	s := newGatedStream(f, id, out)
	close(s.gate)
	return s
}

// newGatedStream is an openStream whose output waits until the test opens the gate.
func newGatedStream(f *fakeEngine, id, out string) *openStream {
	pr, pw := io.Pipe()
	s := &openStream{fakeStream: fakeStream{Reader: pr, f: f, id: id, done: make(chan struct{})}, pw: pw, gate: make(chan struct{}), size: int64(len(out))}
	go func() {
		<-s.gate
		_, _ = io.WriteString(pw, out) // returns once the reader has taken all of it
	}()
	return s
}

func (s *openStream) Read(p []byte) (int, error) {
	s.reading.Store(true)
	if s.taken.Load() == s.size {
		s.idle.Store(true)
	}
	n, err := s.fakeStream.Read(p)
	s.taken.Add(int64(n))
	return n, err
}

func (s *openStream) Close() error {
	_ = s.pw.Close()
	return s.fakeStream.Close()
}

// attachScene is the pods view with an attach tab open in the dock, its output shown.
func attachScene(t *testing.T, mode ThemeMode) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := connectedFake(t, 200)
	ref := f.tables["pods"].Rows[3].Ref
	f.ui.Dock = []engine.DockTab{
		{ID: "terminal-1", Type: engine.DockTerminal, Title: "Terminal 1", Context: "demo"},
		{ID: "attach-2", Type: engine.DockAttach, Title: "Attach: orders-repl-0/repl", Context: "demo", Ref: &ref, Container: "repl"},
	}
	f.ui.ActiveDock = "attach-2"
	eng := attachEngine{fakeEngine: f, last: &atomic.Pointer[openStream]{}}
	sh, w, q := startShell(t, eng, f, Options{Theme: mode})
	// The terminal widget takes output on its own goroutine, where Fyne's test driver runs its
	// fyne.Do inline: the output flows only once the shell has settled and the widget waits in
	// Read, and the test goroutine touches no widget until the widget has processed it all.
	q.until(t, "the terminal to read", func() bool {
		s := eng.last.Load()
		return len(sh.kinds) > 0 && s != nil && s.reading.Load()
	})
	q.drain()
	s := eng.last.Load()
	close(s.gate)
	deadline := time.Now().Add(5 * time.Second)
	for !s.idle.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the terminal did not take the attach session's output")
		}
		time.Sleep(time.Millisecond)
	}
	// Before the shell stops (cleanups run last first): end the session and wait until the widget
	// has shown its end, so that it does not draw on its goroutine while the shell's do.
	term := sh.dock.contents["attach-2"].term
	t.Cleanup(func() {
		_ = term.Close()
		select {
		case <-term.Done():
		case <-time.After(10 * time.Second):
			t.Error("the attach session's widget did not end")
		}
	})
	return sh, w, q, f
}

// TestAttachTabShowsATerminal: an attach tab is a terminal on Session.OpenTerminal, with the
// attachment icon. (Closing a tab closes its stream: TestDockOpensAndClosesStreams.)
func TestAttachTabShowsATerminal(t *testing.T) {
	sh, _, _, _ := attachScene(t, ThemeDark)
	c := sh.dock.contents["attach-2"]
	term, ok := c.term, c.term != nil
	if !ok {
		t.Fatalf("attach tab shows %T", c.obj.Objects[0])
	}
	if text := term.Text(); !strings.Contains(text, "[cogpit] attached to orders-repl-0/repl") || !strings.Contains(text, ">>> orders.count") {
		t.Errorf("terminal %q", text)
	}
	if b := sh.dock.tabRow.Objects[1].(*tabButton); b.title != "Attach: orders-repl-0/repl" || !strings.Contains(b.icon.Name(), "attachment") {
		t.Errorf("tab %q icon %q", b.title, b.icon.Name())
	}
}

func TestRenderAttachDark(t *testing.T) {
	_, w, _, _ := attachScene(t, ThemeDark)
	savePNG(t, w, "attach_dark.png")
}
