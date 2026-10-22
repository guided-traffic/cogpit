// Package termview holds the dock's stream widgets: a terminal on fyne-io/terminal connected to a
// terminal.Session with RunWithConnection (local shell or pod shell, with resize), and a log view
// that follows a log stream with a bounded scrollback.
//
// Both show untrusted output (docs/security/trust-boundaries.md): the terminal passes it through
// an escape-sequence filter (sanitize.go), the log view strips escape sequences entirely.
//
// Layer L4.
package termview

import (
	"errors"
	"fmt"
	"image/color"
	"io"
	"log/slog"
	"math"
	"runtime/debug"
	"sync"
	"sync/atomic"

	fyneterm "github.com/fyne-io/terminal"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/terminal"
)

func hexColor(rgb uint32) color.Color {
	return color.NRGBA{R: uint8(rgb >> 16), G: uint8(rgb >> 8), B: uint8(rgb), A: 0xff}
}

// init gives the terminal Solarized's sixteen colours (D-22): VS Code's terminal.ansi* of
// Solarized Dark and Light, which are the same sixteen in both. The terminal's default text and
// background come from the theme.
func init() {
	fyneterm.SetANSIColors([16]color.Color{
		hexColor(0x073642), hexColor(0xdc322f), hexColor(0x859900), hexColor(0xb58900),
		hexColor(0x268bd2), hexColor(0xd33682), hexColor(0x2aa198), hexColor(0xeee8d5),
		hexColor(0x002b36), hexColor(0xcb4b16), hexColor(0x586e75), hexColor(0x657b83),
		hexColor(0x839496), hexColor(0x6c71c4), hexColor(0x93a1a1), hexColor(0xfdf6e3),
	})
}

// sessionEndedNotice is written into the terminal after the session's last output, in bright
// yellow, which is Solarized's base00 grey (bright black is the dark theme's background).
const sessionEndedNotice = "\r\n\x1b[93m[session ended]\x1b[0m\r\n"

// Terminal shows one terminal session.
//
// Copy and paste are the widget's (fyne-io/terminal): dragging selects (with Alt held at the
// press: a block), double-click selects a word; on macOS ⌘C copies the selection and ⌘V pastes, elsewhere
// Ctrl+Shift+C or Ctrl+Insert and Ctrl+Shift+V or Shift+Insert (the clipboard, a patch of the
// fork). The right mouse button neither copies nor pastes unless SetRightClickCopyPaste turned it
// on (the Preferences' setting): then it copies the selection, or pastes when nothing is
// selected. Bracketed paste is honoured when the program asks for it. A paste is the user's own
// input, as typing is; no command pastes, so no agent can.
type Terminal struct {
	widget.BaseWidget
	sess  terminal.Session
	term  *fyneterm.Terminal
	sizes chan fyneterm.Config
	ended chan struct{}

	rightClick bool // SetRightClickCopyPaste; main goroutine

	mu        sync.Mutex
	started   bool
	closeOnce sync.Once
	closeErr  error
	// stopping, once set (Stop), makes the widget's reading loop end at its next read without
	// drawing what it read: the app is quitting (D-103).
	stopping atomic.Bool
}

// Stop ends the widget's output at once, before the window goes: the widget calls fyne.Do itself,
// and once Fyne has drained its queue at quit, it would run them on its reading goroutine. Call it
// on the main goroutine while Fyne still queues (the window's close); Close ends the session.
func (t *Terminal) Stop() { t.stopping.Store(true) }

// NewTerminal connects a terminal widget to sess. The connection starts when the widget is first
// laid out with a usable size. Closing the widget closes the session.
func NewTerminal(sess terminal.Session) *Terminal {
	t := &Terminal{
		sess:  sess,
		term:  fyneterm.New(),
		sizes: make(chan fyneterm.Config, 8),
		ended: make(chan struct{}),
	}
	t.ExtendBaseWidget(t)
	t.term.SetRightClickAction(fyneterm.RightClickNone) // the widget's default pastes its last selection
	// The widget reports its size in cells here whenever a resize changes it; it drops a report
	// when the channel is full, so the forwarder drains to the latest.
	t.term.AddListener(t.sizes)
	return t
}

// Done is closed when the session's output has ended and "[session ended]" is shown, or after
// Close.
func (t *Terminal) Done() <-chan struct{} { return t.ended }

// Text returns the terminal's screen as text. Call it on the main goroutine.
func (t *Terminal) Text() string { return t.term.Text() }

// FocusTarget is the widget that takes the keyboard: the terminal widget inside, which a click
// focuses too. The dock gives it the keys when the user opens the tab (fyneui/dockfocus.go).
func (t *Terminal) FocusTarget() fyne.Focusable { return t.term }

// RightClickCopyPaste reports what SetRightClickCopyPaste last chose. Call it on the main goroutine.
func (t *Terminal) RightClickCopyPaste() bool { return t.rightClick }

// SetRightClickCopyPaste chooses what the right mouse button does: with on, it copies the
// selection to the clipboard, or pastes the clipboard when nothing is selected; without, nothing.
// Call it on the main goroutine.
func (t *Terminal) SetRightClickCopyPaste(on bool) {
	t.rightClick = on
	if on {
		t.term.SetRightClickAction(fyneterm.RightClickCopyPaste)
	} else {
		t.term.SetRightClickAction(fyneterm.RightClickNone)
	}
}

// Close ends the session. It is idempotent and does not block on the session.
func (t *Terminal) Close() error {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		notStarted := !t.started
		t.started = true // a later layout must not start the connection
		t.mu.Unlock()

		t.term.Close()
		if t.sess != nil {
			t.closeErr = t.sess.Close() // the pending Read in run returns io.EOF and the loop ends
		}
		if notStarted {
			close(t.ended)
		}
	})
	return t.closeErr
}

// CreateRenderer implements fyne.Widget.
func (t *Terminal) CreateRenderer() fyne.WidgetRenderer {
	return &terminalRenderer{t: t}
}

// start connects the widget to the session once it has a size in cells; the widget otherwise
// polls for one, racing the main goroutine.
func (t *Terminal) start() {
	t.mu.Lock()
	if t.started || t.sess == nil {
		t.mu.Unlock()
		return
	}
	t.started = true
	t.mu.Unlock()

	go t.forwardSizes()
	go t.run()
}

// run feeds the session's filtered output to the widget until it ends.
func (t *Terminal) run() {
	defer close(t.ended)
	defer func() {
		if r := recover(); r != nil {
			// The widget's parser panicked on its reading goroutine (see escFilter for the known
			// cases). Its state is unknown: end the session.
			slog.Error("termview: terminal widget panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
		_ = t.sess.Close()
	}()
	out := stopGate{src: &endNotice{src: newEscFilter(t.sess), notice: []byte(sessionEndedNotice)}, stopping: &t.stopping}
	if err := t.term.RunWithConnection(sessionInput{t.sess}, out); err != nil {
		slog.Warn("termview: terminal connection ended", "error", err.Error())
	}
}

// forwardSizes tells the session about the widget's size in cells.
func (t *Terminal) forwardSizes() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("termview: resize forwarder panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	var lastCols, lastRows uint16
	for {
		var cfg fyneterm.Config
		select {
		case cfg = <-t.sizes:
		case <-t.ended:
			return
		}
	drain:
		for {
			select {
			case cfg = <-t.sizes:
			default:
				break drain
			}
		}
		cols, rows := cells(cfg.Columns), cells(cfg.Rows)
		if cols == 0 || rows == 0 || (cols == lastCols && rows == lastRows) {
			continue
		}
		if err := t.sess.Resize(cols, rows); err == nil {
			lastCols, lastRows = cols, rows
		}
	}
}

func cells(n uint) uint16 {
	if n > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(n)
}

type terminalRenderer struct {
	t *Terminal
}

func (r *terminalRenderer) Layout(size fyne.Size) {
	r.t.term.Move(fyne.NewPos(0, 0))
	r.t.term.Resize(size)
	if minSize := r.t.term.MinSize(); size.Width >= minSize.Width && size.Height >= minSize.Height {
		r.t.start()
	}
}

func (r *terminalRenderer) MinSize() fyne.Size           { return r.t.term.MinSize() }
func (r *terminalRenderer) Refresh()                     { r.t.term.Refresh() }
func (r *terminalRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.t.term} }
func (r *terminalRenderer) Destroy()                     {}

// sessionInput is the widget's input side: keystrokes go to the session. Closing it does nothing;
// Terminal.Close and the end of run close the session.
type sessionInput struct {
	sess terminal.Session
}

func (s sessionInput) Write(p []byte) (int, error) { return s.sess.Write(p) }
func (s sessionInput) Close() error                { return nil }

// stopGate ends the output with io.EOF once stopping is set, dropping what the read brought and
// the end notice: nothing more reaches the widget (Terminal.Stop).
type stopGate struct {
	src      io.Reader
	stopping *atomic.Bool
}

func (g stopGate) Read(p []byte) (int, error) {
	if g.stopping.Load() {
		return 0, io.EOF
	}
	n, err := g.src.Read(p)
	if g.stopping.Load() {
		return 0, io.EOF
	}
	return n, err
}

// endNotice passes the session's output on and, when it ends, appends a notice and reports
// io.EOF. Any error ends it: the widget's loop treats errors other than EOF as transient and
// would spin on them.
type endNotice struct {
	src    io.Reader
	notice []byte
	ended  bool
}

func (e *endNotice) Read(p []byte) (int, error) {
	if !e.ended {
		n, err := e.src.Read(p)
		if err == nil {
			return n, nil
		}
		e.ended = true
		if !errors.Is(err, io.EOF) {
			slog.Debug("termview: session output ended", "error", err.Error())
		}
		if n > 0 {
			return n, nil
		}
	}
	if len(e.notice) > 0 {
		n := copy(p, e.notice)
		e.notice = e.notice[n:]
		return n, nil
	}
	return 0, io.EOF
}
