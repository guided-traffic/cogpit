package terminal

import (
	"runtime"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// lastKeyTime is when a key was last typed into a terminal. Cogpit patch: an atomic; keys write it
// on Fyne's main goroutine and a local shell's directory check reads it on a ticker's goroutine, so
// a plain time.Time raced.
var lastKeyTime atomic.Pointer[time.Time]

func init() {
	keyTyped()
}

// keyTyped records that a key was typed now.
func keyTyped() {
	now := time.Now()
	lastKeyTime.Store(&now)
}

// sinceKeyTyped returns the time since a key was last typed.
func sinceKeyTyped() time.Duration {
	return time.Since(*lastKeyTime.Load())
}

// goos is runtime.GOOS, which picks the copy and paste keys. Cogpit patch: a variable, so that tests
// on one system cover the keys of the others.
var goos = runtime.GOOS

// keyModifiers returns the modifier keys of the key event being handled. Fyne's desktop driver
// writes the event's own modifiers before it hands the key to the widget (window_desktop.go's
// keyPressed in Fyne v2.8.2; GLFW takes them from the event, on macOS NSEvent's modifierFlags), so
// they are exact while a key is handled; between keys they are the last key's. Fyne's test driver
// has none, so tests replace the function.
//
// Cogpit patch: the widget kept ⇧, Ctrl and Alt itself from the KeyDown and KeyUp it saw, and a
// release it never saw left a key "held": ⇧⌘4 starts macOS's screenshot overlay, which takes the
// keys while the window stays key, and every arrow key went out with ⇧ and Return as nothing until
// the focus changed (Cogpit's G16 GW8). A key's modifiers now come from the key itself.
var keyModifiers = func() fyne.KeyModifier {
	a := fyne.CurrentApp()
	if a == nil {
		return 0
	}
	if d, ok := a.Driver().(desktop.Driver); ok {
		return d.CurrentKeyModifiers()
	}
	return 0
}

// TypedRune is called when the user types a visible character
func (t *Terminal) TypedRune(r rune) {
	keyTyped()
	t.followOutput() // Cogpit patch: typing returns the view to the screen
	b := make([]byte, utf8.UTFMax)
	size := utf8.EncodeRune(b, r)
	_, _ = t.in.Write(b[:size])
}

// TypedKey will be called if a non-printable keyboard event occurs
func (t *Terminal) TypedKey(e *fyne.KeyEvent) {
	keyTyped()
	if !isModifier(e.Name) {
		t.followOutput() // Cogpit patch: typing returns the view to the screen; ⌘ or Ctrl alone does not
	}
	// Cogpit patch: ⇧ from the key's own event (keyModifiers), and a key without a form of its own
	// with ⇧ (Return, Enter, Backspace, Escape) goes as without it, as in xterm; it sent nothing.
	if keyModifiers()&fyne.KeyModifierShift != 0 && t.keyTypedWithShift(e) {
		return
	}

	switch e.Name {
	case fyne.KeyReturn:
		_, _ = t.in.Write([]byte{'\r'})
	case fyne.KeyEnter:
		if t.newLineMode {
			_, _ = t.in.Write([]byte{'\r'})
			return
		}
		_, _ = t.in.Write([]byte{'\n'})
	case fyne.KeyTab:
		_, _ = t.in.Write([]byte{'\t'})
	case fyne.KeyF1:
		_, _ = t.in.Write([]byte{asciiEscape, 'O', 'P'})
	case fyne.KeyF2:
		_, _ = t.in.Write([]byte{asciiEscape, 'O', 'Q'})
	case fyne.KeyF3:
		_, _ = t.in.Write([]byte{asciiEscape, 'O', 'R'})
	case fyne.KeyF4:
		_, _ = t.in.Write([]byte{asciiEscape, 'O', 'S'})
	case fyne.KeyF5:
		_, _ = t.in.Write([]byte{asciiEscape, '[', '1', '5', '~'})
	case fyne.KeyF6:
		_, _ = t.in.Write([]byte{asciiEscape, '[', '1', '7', '~'})
	case fyne.KeyF7:
		_, _ = t.in.Write([]byte{asciiEscape, '[', '1', '8', '~'})
	case fyne.KeyF8:
		_, _ = t.in.Write([]byte{asciiEscape, '[', '1', '9', '~'})
	case fyne.KeyF9:
		_, _ = t.in.Write([]byte{asciiEscape, '[', '2', '0', '~'})
	case fyne.KeyF10:
		_, _ = t.in.Write([]byte{asciiEscape, '[', '2', '1', '~'})
	case fyne.KeyF11:
		_, _ = t.in.Write([]byte{asciiEscape, '[', '2', '3', '~'})
	case fyne.KeyF12:
		_, _ = t.in.Write([]byte{asciiEscape, '[', '2', '4', '~'})
	case fyne.KeyEscape:
		_, _ = t.in.Write([]byte{asciiEscape})
	case fyne.KeyBackspace:
		_, _ = t.in.Write([]byte{asciiBackspace})
	case fyne.KeyDelete:
		_, _ = t.in.Write([]byte{asciiEscape, '[', '3', '~'})
	case fyne.KeyUp, fyne.KeyDown, fyne.KeyLeft, fyne.KeyRight:
		t.typeCursorKey(e.Name)
	case fyne.KeyPageUp:
		_, _ = t.in.Write([]byte{asciiEscape, '[', '5', '~'})
	case fyne.KeyPageDown:
		_, _ = t.in.Write([]byte{asciiEscape, '[', '6', '~'})
	case fyne.KeyHome:
		_, _ = t.in.Write([]byte{asciiEscape, 'O', 'H'})
	case fyne.KeyInsert:
		_, _ = t.in.Write([]byte{asciiEscape, '[', '2', '~'})
	case fyne.KeyEnd:
		_, _ = t.in.Write([]byte{asciiEscape, 'O', 'F'})
	}
}

// shiftKeys are xterm's sequences for keys typed with ⇧, as the terminfo entry xterm-256color names
// them (kf13–kf24, kLFT, kRIT, kri, kind, kHOM, kEND, kPRV, kNXT, kIC, kDC, kcbt). Cogpit patch:
// ⇧ and an arrow went out as ESC [ A ; 2, which no program reads as one key (a shell takes the arrow
// and types ";2"), ⇧F1 to ⇧F3 as ESC [ 25 ~, ESC [ 26 ~ and ESC O R ; 2 ~, and ⇧Tab as nothing.
var shiftKeys = map[fyne.KeyName]string{
	fyne.KeyF1: "\x1b[1;2P", fyne.KeyF2: "\x1b[1;2Q", fyne.KeyF3: "\x1b[1;2R", fyne.KeyF4: "\x1b[1;2S",
	fyne.KeyF5: "\x1b[15;2~", fyne.KeyF6: "\x1b[17;2~", fyne.KeyF7: "\x1b[18;2~", fyne.KeyF8: "\x1b[19;2~",
	fyne.KeyF9: "\x1b[20;2~", fyne.KeyF10: "\x1b[21;2~", fyne.KeyF11: "\x1b[23;2~", fyne.KeyF12: "\x1b[24;2~",
	fyne.KeyUp: "\x1b[1;2A", fyne.KeyDown: "\x1b[1;2B", fyne.KeyRight: "\x1b[1;2C", fyne.KeyLeft: "\x1b[1;2D",
	fyne.KeyHome: "\x1b[1;2H", fyne.KeyEnd: "\x1b[1;2F", fyne.KeyPageUp: "\x1b[5;2~", fyne.KeyPageDown: "\x1b[6;2~",
	fyne.KeyInsert: "\x1b[2;2~", fyne.KeyDelete: "\x1b[3;2~", fyne.KeyTab: "\x1b[Z",
}

// keyTypedWithShift writes xterm's sequence for e typed with ⇧ and reports whether e has one.
func (t *Terminal) keyTypedWithShift(e *fyne.KeyEvent) bool {
	seq, ok := shiftKeys[e.Name]
	if ok {
		_, _ = t.in.Write([]byte(seq))
	}
	return ok
}

// KeyDown is called when we get a down key event. Cogpit patch: it does nothing; the widget no
// longer keeps the modifier keys itself (keyModifiers, and a drag's ⌥ from its press).
func (t *Terminal) KeyDown(_ *fyne.KeyEvent) {}

// KeyUp is called when we get an up key event. Cogpit patch: it does nothing, see KeyDown.
func (t *Terminal) KeyUp(_ *fyne.KeyEvent) {}

// FocusGained notifies the terminal that it has focus
func (t *Terminal) FocusGained() {
	t.focused = true
	t.Refresh()
}

// TypedShortcut handles key combinations, we pass them on to the tty.
func (t *Terminal) TypedShortcut(s fyne.Shortcut) {
	if !isCopy(s) {
		t.followOutput() // Cogpit patch: what goes to the shell (ctrl+C, paste) returns the view to the screen
	}
	if ds, ok := s.(*desktop.CustomShortcut); ok {
		t.ShortcutHandler.TypedShortcut(s) // it's not clear how we can check if this consumed the event

		// handle CTRL+A to CTRL+_ and everything in-between
		if ds.Modifier == fyne.KeyModifierControl && len(ds.KeyName) > 0 {
			char := ds.KeyName[0]
			off := char - 'A' + 1
			switch {
			case ds.Key() == fyne.KeySpace:
				fallthrough
			case ds.Key() == "@":
				off = 0
				fallthrough
			case char >= 'A' && char <= '_':
				_, _ = t.in.Write([]byte{off})
			}
		}
		return
	}

	if goos == "darwin" {
		// do the default thing for macOS as they separate ctrl/cmd
		t.ShortcutHandler.TypedShortcut(s)
	} else {
		// we need to override the default ctrl-X/C/V/A for non-mac and do it ourselves
		switch sh := s.(type) {
		case *fyne.ShortcutCut:
			if sh.Secondary {
				// shift+del - cut is no-op
			} else {
				_, _ = t.in.Write([]byte{0x18})
			}
		case *fyne.ShortcutCopy:
			if sh.Secondary {
				// Cogpit patch: ctrl+ins copies to the clipboard, as Windows and Linux terminals do;
				// it copied to this terminal's own selection, which nothing outside it reads.
				t.copySelectedText(clipboardOf(sh.Clipboard)) // ctrl+ins
			} else {
				_, _ = t.in.Write([]byte{0x3})
			}
		case *fyne.ShortcutPaste:
			if sh.Secondary {
				t.pasteText(clipboardOf(sh.Clipboard)) // shift+ins; Cogpit patch: the clipboard, as ctrl+ins
			} else {
				_, _ = t.in.Write([]byte{0x16})
			}
		case *fyne.ShortcutUndo:
			_, _ = t.in.Write([]byte{0x1a})
		case *fyne.ShortcutRedo:
			_, _ = t.in.Write([]byte{0x19})
		case *fyne.ShortcutSelectAll:
			_, _ = t.in.Write([]byte{0x1})
		}
	}
}

// FocusLost tells the terminal it no longer has focus
func (t *Terminal) FocusLost() {
	t.focused = false
	t.Refresh()
}

// Focused is used to determine if this terminal currently has focus
func (t *Terminal) Focused() bool {
	return t.focused
}

func (t *Terminal) typeCursorKey(key fyne.KeyName) {
	cursorPrefix := byte('[')
	if t.bufferMode {
		cursorPrefix = 'O'
	}

	switch key {
	case fyne.KeyUp:
		_, _ = t.in.Write([]byte{asciiEscape, cursorPrefix, 'A'})
	case fyne.KeyDown:
		_, _ = t.in.Write([]byte{asciiEscape, cursorPrefix, 'B'})
	case fyne.KeyLeft:
		_, _ = t.in.Write([]byte{asciiEscape, cursorPrefix, 'D'})
	case fyne.KeyRight:
		_, _ = t.in.Write([]byte{asciiEscape, cursorPrefix, 'C'})
	}
}

type discardWriter struct{}

func (d discardWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (d discardWriter) Close() error {
	return nil
}

// clipboardOf is the clipboard a shortcut carries, else the app's. Cogpit patch.
func clipboardOf(c fyne.Clipboard) fyne.Clipboard {
	if c != nil {
		return c
	}
	return fyne.CurrentApp().Clipboard()
}
