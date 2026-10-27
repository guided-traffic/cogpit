package termview

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	fyneterm "github.com/fyne-io/terminal"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

// rawWidgetTimeout is how long the bare widget may take for one read of output. Output it has not
// processed by then counts as a hang: a tab at the right margin once looped for ever. Reads take
// milliseconds; the bound is generous because a loaded machine stalls fuzzing workers for seconds.
const rawWidgetTimeout = 30 * time.Second

// sessionCaptures are real programs' output (vim, less, pico, GNU screen) recorded in an 80x24
// pseudo-terminal, kept with the widget's own fuzz target.
const sessionCaptures = "../../../../third_party/fyne-terminal/test_data/sessions"

// rawRun is a bare fyne-io/terminal widget, without escFilter, connected to a fake session with
// RunWithConnection as termview connects it, with its widget work on a main loop.
type rawRun struct {
	t        *testing.T
	loop     *mainLoop
	raw      *fyneterm.Terminal
	win      fyne.Window
	sess     *fakeSession
	failed   chan string
	returned chan struct{}
	what     string // what the input was, for failures
}

// startRaw shows a bare widget in a window of the given size on loop and starts
// RunWithConnection; with a zero size, the widget waits for one before it reads.
func startRaw(t *testing.T, loop *mainLoop, size fyne.Size, what string) *rawRun {
	t.Helper()
	r := &rawRun{t: t, loop: loop, sess: newFakeSession(), failed: make(chan string, 1), returned: make(chan struct{}), what: what}
	loop.do(func() {
		r.raw = fyneterm.New()
		r.win = test.NewTempWindow(t, r.raw)
		r.win.SetPadded(false)
		r.win.Resize(size)
	})
	go func() {
		defer close(r.returned)
		defer func() {
			if p := recover(); p != nil {
				r.failed <- fmt.Sprintf("panic: %v\n%s", p, debug.Stack())
			}
		}()
		_ = r.raw.RunWithConnection(sessionInput{r.sess}, r.sess)
	}()
	return r
}

// await waits until the widget has processed what it was given and asks for more output, and
// fails on a panic on either goroutine or when that takes longer than rawWidgetTimeout.
func (r *rawRun) await(step string) {
	r.t.Helper()
	select {
	case <-r.sess.idle:
	case msg := <-r.failed:
		r.t.Fatalf("%s: %s", r.what, msg)
	case <-time.After(rawWidgetTimeout):
		r.t.Fatalf("%s: %s not done within %v", r.what, step, rawWidgetTimeout)
	}
	if msg := r.loop.panicked(); msg != "" {
		r.t.Fatalf("%s: %s", r.what, msg)
	}
}

// feed hands chunk to the widget and waits until it has been processed.
func (r *rawRun) feed(chunk []byte, step string) {
	r.t.Helper()
	r.sess.chunks <- chunk
	r.await(step)
}

// end closes the widget on the main loop as termview does, ends the session's output, waits for
// RunWithConnection to return, and releases what the widget left behind.
func (r *rawRun) end() {
	r.t.Helper()
	r.loop.do(r.raw.Close)
	r.sess.end()
	select {
	case <-r.returned:
	case <-time.After(rawWidgetTimeout):
		r.t.Fatalf("%s: RunWithConnection did not return after the session ended", r.what)
	}
	select {
	case msg := <-r.failed:
		r.t.Fatalf("%s: %s", r.what, msg)
	default:
	}
	if msg := r.loop.panicked(); msg != "" {
		r.t.Fatalf("%s: %s", r.what, msg)
	}
	// A bell that clears itself after the Close leaves the closed widget alone (ringBell).
	// Refreshes queue more work on the main loop: it has to be done before the renderers are
	// collected, or it makes them again after the test has dropped them.
	r.loop.flush()
	r.loop.do(func() { releaseWidget(r.t, r.raw) })
	r.loop.flush()
}

// FuzzRawWidget feeds arbitrary output to the fyne-io/terminal widget alone, without escFilter,
// through its public API as termview uses it: RunWithConnection on a session, then two reads of
// output with a window resize and, optionally, a double-click selecting a word between them. The
// widget's work runs on a main loop as in the app. It fails when the widget panics or does not
// finish a read within rawWidgetTimeout; the widget's own FuzzHandleOutput
// (third_party/fyne-terminal) fuzzes its parser without a window.
func FuzzRawWidget(f *testing.F) {
	for _, h := range widgetHazards {
		f.Add([]byte(h.input), uint16(0), uint16(400), uint16(200), false)
	}
	f.Add([]byte(demoOutput), uint16(len(demoOutput)/2), uint16(90), uint16(40), true)
	captures, _ := filepath.Glob(filepath.Join(sessionCaptures, "*.vt"))
	if len(captures) == 0 {
		f.Fatalf("no session captures in %s", sessionCaptures)
	}
	for _, name := range captures {
		data, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data, uint16(len(data)/3), uint16(300), uint16(120), true)
	}

	loop := useMainLoop(f)
	f.Fuzz(func(t *testing.T, data []byte, split, width, height uint16, selectWord bool) {
		r := startRaw(t, loop, fyne.NewSize(640, 320), fmt.Sprintf("%q (split %d, %dx%d px)", data, split, width, height))
		r.await("start")

		first, second := data, []byte(nil)
		if int(split) < len(data) {
			first, second = data[:split], data[split:]
		}
		r.feed(first, "first read")
		loop.do(func() {
			r.win.Resize(fyne.NewSize(float32(width%1600), float32(height%1000)))
			if selectWord {
				r.raw.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(float32(split%400), float32(width%200))})
				_ = r.raw.SelectedText()
			}
		})
		r.feed(second, "second read")
		loop.do(func() { _ = r.raw.Text() })
		r.end()
	})
}

// TestRawWidgetThreading runs the bare widget the way the desktop driver does, with its widget work
// on a main loop and its parser on the goroutine reading the session, through what used to cross
// the two (H-19): RunWithConnection started before the widget has a size, a resize, shift in and
// out with character sets designated, a selection made between reads, typing, media copy, the
// bell, blinking text and a title. Under the race detector (make test-unit-race) a field that one
// goroutine writes and the other reads without a fyne.Do in between is a failure.
func TestRawWidgetThreading(t *testing.T) {
	loop := useMainLoop(t)
	r := startRaw(t, loop, fyne.NewSize(0, 0), "threading")
	loop.do(func() { r.win.Resize(fyne.NewSize(640, 320)) })
	r.await("start")

	r.feed([]byte("\x1b]0;title\x07$ echo hello\r\nhello\r\n\x1b)0\x0elqqk\x0f \x1b(0x\x1b(B\r\n"), "first read")
	loop.do(func() {
		r.raw.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(20, 20)})
		r.raw.TypedRune('l')
		r.raw.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
		r.raw.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonSecondary})
	})
	r.feed([]byte("\x07\x1b[5mblink\x1b[0m\x1b[5ito the printer\x1b[4i shown\r\n$ "), "second read")

	var screen string
	loop.do(func() { screen = r.raw.Text() })
	if !strings.Contains(screen, "hello") || !strings.Contains(screen, "┌──┐ │") || !strings.Contains(screen, "blink shown") ||
		strings.Contains(screen, "printer") {
		t.Errorf("screen = %q", screen)
	}
	if in, _, _ := r.sess.state(); !strings.HasPrefix(in, "l\r") {
		t.Errorf("session input = %q", in)
	}
	r.end()
}

// releaseWidget stops what the widget leaves running and makes Fyne drop the cached renderers of
// the bare widget and of everything they show when the test ends: the test driver never expires
// them, and each keeps its widget, so a fuzz worker would keep every input's terminal (about 650 KB
// each) and slow down until inputs time out. A blinking cell keeps a goroutine refreshing the
// widget's grid until none is left, so the grid is emptied first. Call it on the main loop.
func releaseWidget(t *testing.T, raw *fyneterm.Terminal) {
	releaseRenderers(t, raw) // first: an emptied grid hides its rows from the walk
	for _, o := range test.TempWidgetRenderer(t, raw).Objects() {
		if grid, ok := o.(interface{ SetText(string) }); ok {
			grid.SetText("")
			o.Refresh()
		}
	}
}

// releaseRenderers makes Fyne drop the cached renderers of o and of everything they show when the
// test ends.
func releaseRenderers(t *testing.T, o fyne.CanvasObject) {
	switch o := o.(type) {
	case fyne.Widget:
		for _, child := range test.TempWidgetRenderer(t, o).Objects() {
			releaseRenderers(t, child)
		}
	case *fyne.Container:
		for _, child := range o.Objects {
			releaseRenderers(t, child)
		}
	}
}
