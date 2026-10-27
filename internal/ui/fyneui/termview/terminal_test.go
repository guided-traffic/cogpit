package termview

import (
	"bytes"
	"flag"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	fyneterm "github.com/fyne-io/terminal"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var update = flag.Bool("update", false, "rewrite the offscreen PNGs in testdata/")

// fakeSession is a terminal.Session whose output the test hands over chunk by chunk. After
// feed returns, the widget has processed the chunk and waits for more: the reader goroutine
// is blocked in Read, so the test goroutine may look at the widget.
type fakeSession struct {
	chunks chan []byte
	idle   chan struct{}
	done   chan struct{}
	ending []byte

	mu     sync.Mutex
	input  bytes.Buffer
	sizes  [][2]uint16
	closed bool
}

func newFakeSession() *fakeSession {
	return &fakeSession{chunks: make(chan []byte), idle: make(chan struct{}, 1), done: make(chan struct{})}
}

func (f *fakeSession) Read(p []byte) (int, error) {
	if len(f.ending) == 0 {
		f.idle <- struct{}{}
		chunk, ok := <-f.chunks
		if !ok {
			return 0, io.EOF
		}
		f.ending = chunk
	}
	n := copy(p, f.ending)
	f.ending = f.ending[n:]
	return n, nil
}

// waitIdle waits until the widget asks for output.
func (f *fakeSession) waitIdle(t *testing.T) {
	t.Helper()
	select {
	case <-f.idle:
	case <-time.After(5 * time.Second):
		t.Fatal("the terminal did not start reading the session")
	}
}

// feed hands s to the widget and waits until it has been processed.
func (f *fakeSession) feed(t *testing.T, s string) {
	t.Helper()
	f.chunks <- []byte(s)
	f.waitIdle(t)
}

// end makes the session's output end.
func (f *fakeSession) end() { close(f.chunks) }

func (f *fakeSession) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, io.ErrClosedPipe
	}
	return f.input.Write(p)
}

func (f *fakeSession) Resize(cols, rows uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizes = append(f.sizes, [2]uint16{cols, rows})
	return nil
}

func (f *fakeSession) Done() <-chan struct{} { return f.done }

func (f *fakeSession) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.done)
	}
	return nil
}

func (f *fakeSession) state() (string, [][2]uint16, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.input.String(), append([][2]uint16(nil), f.sizes...), f.closed
}

// darkTheme is the default theme in its dark variant, for screenshots.
type darkTheme struct{ fyne.Theme }

func (d darkTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return d.Theme.Color(n, theme.VariantDark)
}

func newTestApp(t *testing.T) fyne.App {
	t.Helper()
	a := test.NewTempApp(t)
	a.Settings().SetTheme(darkTheme{theme.DefaultTheme()})
	return a
}

// showTerminal lays a Terminal for sess out in an offscreen window and waits until it reads.
func showTerminal(t *testing.T, sess *fakeSession, size fyne.Size) (*Terminal, fyne.Window) {
	t.Helper()
	newTestApp(t)
	term := NewTerminal(sess)
	// The window gets its final size before the terminal's first layout, which starts the
	// connection: the widget's start-up writes its config on the reading goroutine and would
	// race a resize on the test goroutine (as it would race one on the main goroutine).
	w := test.NewTempWindow(t, widget.NewLabel(""))
	w.SetPadded(false)
	w.Resize(size)
	w.SetContent(term)
	sess.waitIdle(t)
	return term, w
}

func waitDone(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("not done within 5s")
	}
}

func TestTerminalShowsOutputAndForwardsInputAndSize(t *testing.T) {
	sess := newFakeSession()
	term, w := showTerminal(t, sess, fyne.NewSize(640, 320))

	sess.feed(t, "$ echo hi\r\nhi\r\n$ ")
	if got := term.Text(); !strings.Contains(got, "$ echo hi\nhi\n$") {
		t.Errorf("screen = %q", got)
	}

	term.term.TypedRune('l')
	term.term.TypedRune('s')
	term.term.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	if in, _, _ := sess.state(); in != "ls\r" {
		t.Errorf("session input = %q, want \"ls\\r\"", in)
	}

	// The first size arrives with the first layout; a resize sends a new one.
	waitSizes := func(n int) [][2]uint16 {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, sizes, _ := sess.state(); len(sizes) >= n {
				return sizes
			}
			time.Sleep(10 * time.Millisecond)
		}
		_, sizes, _ := sess.state()
		t.Fatalf("sizes = %v, want %d", sizes, n)
		return nil
	}
	first := waitSizes(1)[0]
	if first[0] < 40 || first[1] < 10 {
		t.Errorf("first size %v is implausible for 640x320", first)
	}
	w.Resize(fyne.NewSize(960, 480))
	second := waitSizes(2)[1]
	if second[0] <= first[0] || second[1] <= first[1] {
		t.Errorf("after growing the window: %v, before %v", second, first)
	}

	sess.end()
	waitDone(t, term.Done())
	if got := term.Text(); !strings.Contains(got, "[session ended]") {
		t.Errorf("screen after the end = %q", got)
	}
	if _, _, closed := sess.state(); !closed {
		t.Error("session not closed after its output ended")
	}
	if err := term.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestTerminalCopyAndPaste records what the widget offers: a double-click selects a word, the
// platform's copy shortcut puts the selection on the clipboard, the paste shortcut types the
// clipboard (bracketed when the program asks). A right click does nothing until
// SetRightClickCopyPaste turns it on; then it copies a selection, else pastes the clipboard.
func TestTerminalCopyAndPaste(t *testing.T) {
	sess := newFakeSession()
	term, _ := showTerminal(t, sess, fyne.NewSize(640, 320))
	sess.feed(t, "$ echo hello\r\nhello\r\n$ ")

	cell := canvas.NewText("M", color.White)
	cell.TextStyle.Monospace = true
	size := cell.MinSize()
	cellW, cellH := float32(math.Round(float64(size.Width))), float32(math.Round(float64(size.Height)))
	// Row 2, column 3: inside "hello".
	term.term.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(cellW*2.5, cellH*1.5)})
	if got := term.term.SelectedText(); got != "hello" {
		t.Fatalf("double-click selected %q, want hello", got)
	}

	clip := fyne.CurrentApp().Clipboard() // the widget uses the app clipboard
	copyKey, pasteKey := fyne.Shortcut(&fyne.ShortcutCopy{Clipboard: clip}), fyne.Shortcut(&fyne.ShortcutPaste{Clipboard: clip})
	if runtime.GOOS != "darwin" {
		copyKey = &desktop.CustomShortcut{KeyName: fyne.KeyC, Modifier: fyne.KeyModifierShift | fyne.KeyModifierShortcutDefault}
		pasteKey = &desktop.CustomShortcut{KeyName: fyne.KeyV, Modifier: fyne.KeyModifierShift | fyne.KeyModifierShortcutDefault}
	}
	term.term.TypedShortcut(copyKey)
	if got := clip.Content(); got != "hello" {
		t.Errorf("clipboard after copy = %q, want hello", got)
	}

	clip.SetContent("kubectl get pods")
	term.term.TypedShortcut(pasteKey)
	if in, _, _ := sess.state(); in != "kubectl get pods" {
		t.Errorf("session input after paste = %q", in)
	}

	sess.feed(t, "\x1b[?2004h") // the shell asks for bracketed paste
	term.term.TypedShortcut(pasteKey)
	if in, _, _ := sess.state(); !strings.HasSuffix(in, "\x1b[200~kubectl get pods\x1b[201~") {
		t.Errorf("bracketed paste: input = %q", in)
	}

	// The right button: nothing by default (the widget's own default pasted its last selection).
	right := func() { term.term.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonSecondary}) }
	word := func() { term.term.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(cellW*2.5, cellH*1.5)}) }
	before, _, _ := sess.state()
	word()
	right()
	right()
	if in, _, _ := sess.state(); in != before || clip.Content() != "kubectl get pods" {
		t.Errorf("right click without the preference: input %q, clipboard %q", in, clip.Content())
	}
	// With the preference: a selection is copied, nothing typed; then, nothing selected, a paste.
	term.SetRightClickCopyPaste(true)
	word()
	right()
	if in, _, _ := sess.state(); in != before || clip.Content() != "hello" {
		t.Errorf("right click on a selection: input %q, clipboard %q", in, clip.Content())
	}
	right()
	if in, _, _ := sess.state(); in != before+"\x1b[200~hello\x1b[201~" {
		t.Errorf("right click without a selection: input = %q", in)
	}
	term.SetRightClickCopyPaste(false)
	right()
	if in, _, _ := sess.state(); in != before+"\x1b[200~hello\x1b[201~" {
		t.Errorf("right click after the preference went off: input = %q", in)
	}

	// Ctrl+C is the interrupt, not copy (macOS: copy is ⌘C).
	if runtime.GOOS == "darwin" {
		term.term.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyC, Modifier: fyne.KeyModifierControl})
		if in, _, _ := sess.state(); !strings.HasSuffix(in, "\x03") {
			t.Errorf("Ctrl+C: input = %q, want it to end in ^C", in)
		}
	}
	_ = term.Close()
	sess.end()
	waitDone(t, term.Done())
}

func TestTerminalCloseBeforeLayout(t *testing.T) {
	newTestApp(t)
	sess := newFakeSession()
	term := NewTerminal(sess)
	if err := term.Close(); err != nil {
		t.Fatal(err)
	}
	waitDone(t, term.Done())
	if _, _, closed := sess.state(); !closed {
		t.Error("session not closed")
	}
	// A layout after Close must not connect.
	w := test.NewTempWindow(t, term)
	w.Resize(fyne.NewSize(400, 200))
	select {
	case <-sess.idle:
		t.Error("the terminal started reading after Close")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTerminalCloseWhileRunning(t *testing.T) {
	sess := newFakeSession()
	term, _ := showTerminal(t, sess, fyne.NewSize(400, 200))
	sess.feed(t, "$ ")
	if err := term.Close(); err != nil {
		t.Fatal(err)
	}
	sess.end() // what a closed session's Read does
	waitDone(t, term.Done())
}

// runRawWidget connects a bare fyne-io/terminal widget, without Cogpit's filter, and reports
// whether processing input panicked.
func runRawWidget(t *testing.T, input string) (panicked any, cwdAfter string) {
	t.Helper()
	newTestApp(t)
	raw := fyneterm.New()
	w := test.NewTempWindow(t, raw)
	w.Resize(fyne.NewSize(400, 200))
	sess := newFakeSession()
	result := make(chan any, 1)
	go func() {
		defer func() { result <- recover() }()
		_ = raw.RunWithConnection(sessionInput{sess}, sess)
	}()
	sess.waitIdle(t)
	sess.chunks <- []byte(input)
	select {
	case <-sess.idle: // processed without panicking
		cwdAfter, _ = os.Getwd()
		raw.Close()
		sess.end()
		<-result
		return nil, cwdAfter
	case p := <-result:
		cwdAfter, _ = os.Getwd()
		return p, cwdAfter
	case <-time.After(5 * time.Second):
		t.Fatal("raw widget hung")
		return nil, ""
	}
}

// Inputs that crashed or froze fyne-io/terminal at 91063bb29d69 (the first nine) or at
// 46cd12724ff3 (found by fuzzing the widget alone, 2026-10-12). Cogpit builds against a patched
// copy (third_party/fyne-terminal, see its PATCHES.md) that survives all of them, and keeps
// escFilter in front of it.
var widgetHazards = []struct {
	name, input string
}{
	{"CSI with non-ASCII final", "\x1b[é"},
	{"set mode without ?", "\x1b[h"},
	{"delete characters past the row's end", "\x1b[1;10H\x1b[P"},
	{"reverse index before rows exist", "\x1bM"},
	{"delete characters at the pending-wrap column after erase line", "\x1b[9999Gx\x1b[2K\x1b[P"},
	{"tab at the right margin", "\x1b[999Gx\t"},
	{"tab after a full line", strings.Repeat("x", 50) + "\t"},
	{"insert characters on a fresh screen", "\x1b[5;1H\x1b[@"},
	{"scroll region with bottom 0, then index", "a\r\n\x1b[1;0r\x1bD"},
	{"repeat a character the largest number of times", "x\x1b[9223372036854775807b"},
	{"delete the largest number of lines", "\x1b[9223372036854775807M"},
	{"insert the largest number of lines", "\x1b[9223372036854775807L"},
	{"scroll up the largest number of lines", "\x1b[9223372036854775807S"},
	{"scroll down the largest number of lines in a region", "\x1b[2;5r\x1b[9223372036854775807T"},
	{"APC ended by ST", "\x1b_Gf=100;AAAA\x1b\\"},
}

func TestRawWidgetHazards(t *testing.T) {
	for _, h := range widgetHazards {
		t.Run(h.name, func(t *testing.T) {
			if p, _ := runRawWidget(t, h.input); p != nil {
				t.Errorf("bare widget on %q: panic %v", h.input, p)
			}
		})
	}
}

func TestWidgetIgnoresOSC7ForTheProcessDirectory(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	target, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, cwd := runRawWidget(t, "\x1b]7;file://"+target+"\x07")
	if cwd == target {
		t.Errorf("the widget moved Cogpit's process to %s on OSC 7; the patched copy must not", cwd)
	}
}

func TestTerminalFilterNeutralisesHazards(t *testing.T) {
	orig, _ := os.Getwd()
	target, _ := filepath.EvalSymlinks(t.TempDir())
	sess := newFakeSession()
	term, _ := showTerminal(t, sess, fyne.NewSize(400, 200))
	for _, h := range widgetHazards {
		sess.feed(t, h.input)
	}
	sess.feed(t, "\x1b]7;file://"+target+"\x07")
	sess.feed(t, "\x1b]52;c;"+strings.Repeat("QUFB", 1000)+"\x07")
	start := time.Now()
	sess.feed(t, "x\x1b[999999999b") // clamped to 9999 repeats
	// Timed without the race detector only; TestEscFilter asserts the clamp itself.
	if d := time.Since(start); d > 2*time.Second && !raceEnabled {
		t.Errorf("a huge repeat count took %v", d)
	}
	sess.feed(t, "\r\nstill alive")
	select {
	case <-term.Done():
		t.Fatal("the terminal ended on hostile input")
	default:
	}
	if !strings.Contains(term.Text(), "still alive") {
		t.Errorf("screen = %q", term.Text())
	}
	if cwd, _ := os.Getwd(); cwd != orig {
		t.Errorf("working directory changed to %s", cwd)
	}
	_ = term.Close()
	sess.end()
	waitDone(t, term.Done())
}

// demoOutput is a shell session as the screenshot shows it.
const demoOutput = "\x1b]0;jane@cogpit: ~/clusters\x07" + // title: filtered
	"\x1b[1;32mjane@cogpit\x1b[0m:\x1b[1;34m~/clusters\x1b[0m$ kubectl get pods -n shop\r\n" +
	"NAME                       READY   STATUS             RESTARTS      AGE\r\n" +
	"web-7d4b9c6f5-2xkqp        1/1     \x1b[32mRunning\x1b[0m            0             3d\r\n" +
	"web-7d4b9c6f5-9hjzt        1/1     \x1b[32mRunning\x1b[0m            0             3d\r\n" +
	"worker-5f8d7b8c4-lq2mn     0/1     \x1b[31mCrashLoopBackOff\x1b[0m   12 (2m ago)   41m\r\n" +
	"redis-0                    1/1     \x1b[32mRunning\x1b[0m            0             12d\r\n" +
	"\x1b]52;c;cm0gLXJmIH4=\x07" + // clipboard write: filtered
	"\x1b[1;32mjane@cogpit\x1b[0m:\x1b[1;34m~/clusters\x1b[0m$ kubectl config current-context\r\n" +
	"kind-cogpit\r\n" +
	"\x1b[1;32mjane@cogpit\x1b[0m:\x1b[1;34m~/clusters\x1b[0m$ "

func TestTerminalScreenshot(t *testing.T) {
	sess := newFakeSession()
	term, w := showTerminal(t, sess, fyne.NewSize(760, 240))
	sess.feed(t, demoOutput)
	if got := term.Text(); !strings.Contains(got, "CrashLoopBackOff") || strings.Contains(got, "52;") {
		t.Errorf("screen = %q", got)
	}
	writePNG(t, w, "terminal.png")
	_ = term.Close()
	sess.end()
	waitDone(t, term.Done())
}

// writePNG captures the window; with -update it writes testdata/<name>.
func writePNG(t *testing.T, w fyne.Window, name string) {
	t.Helper()
	img := w.Canvas().Capture()
	if b := img.Bounds(); b.Dx() == 0 || b.Dy() == 0 {
		t.Fatalf("empty capture %v", b)
	}
	if !*update {
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", name)
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (%s)", path, fmt.Sprint(img.Bounds().Size()))
}
