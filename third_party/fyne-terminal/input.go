package terminal

import (
	"runtime"
	"strconv"
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
	if keyModifiers()&fyne.KeyModifierShift != 0 && t.typeModifiedKey(e.Name, fyne.KeyModifierShift) {
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

// modifiedKeys are the keys xterm sends with a modifier parameter: CSI <n> ; <m> <final>, the
// parameter m 1 plus 1 for ⇧ and 4 for Ctrl, as the terminfo entry xterm-256color names them
// (kLFT, kRIT, kri, kind, kHOM, kEND, kPRV, kNXT, kIC, kDC, kf13–kf24 with ⇧; kLFT5, kf25–kf36 and
// the like with Ctrl). Cogpit patch, see typeModifiedKey.
var modifiedKeys = map[fyne.KeyName]struct {
	n     string
	final byte
}{
	fyne.KeyUp: {"1", 'A'}, fyne.KeyDown: {"1", 'B'}, fyne.KeyRight: {"1", 'C'}, fyne.KeyLeft: {"1", 'D'},
	fyne.KeyHome: {"1", 'H'}, fyne.KeyEnd: {"1", 'F'},
	fyne.KeyF1: {"1", 'P'}, fyne.KeyF2: {"1", 'Q'}, fyne.KeyF3: {"1", 'R'}, fyne.KeyF4: {"1", 'S'},
	fyne.KeyF5: {"15", '~'}, fyne.KeyF6: {"17", '~'}, fyne.KeyF7: {"18", '~'}, fyne.KeyF8: {"19", '~'},
	fyne.KeyF9: {"20", '~'}, fyne.KeyF10: {"21", '~'}, fyne.KeyF11: {"23", '~'}, fyne.KeyF12: {"24", '~'},
	fyne.KeyInsert: {"2", '~'}, fyne.KeyDelete: {"3", '~'}, fyne.KeyPageUp: {"5", '~'}, fyne.KeyPageDown: {"6", '~'},
}

// typeModifiedKey writes xterm's sequence for the key name typed with mods, of which it encodes ⇧
// and Ctrl, and reports whether the key has one; ⇧Tab is CSI Z. Cogpit patch: ⇧ and an arrow went
// out as ESC [ A ; 2, which no program reads as one key (a shell takes the arrow and types ";2"),
// ⇧F1 to ⇧F3 as ESC [ 25 ~, ESC [ 26 ~ and ESC O R ; 2 ~, ⇧Tab as nothing; and Ctrl with a named
// key as the control character of the name's first letter: Ctrl+↓ and Ctrl+Delete ^D, which ends
// a shell at an empty prompt, Ctrl+↑ ^U, Ctrl+← ^L, Ctrl+→ and Ctrl+Return ^R.
func (t *Terminal) typeModifiedKey(name fyne.KeyName, mods fyne.KeyModifier) bool {
	if name == fyne.KeyTab && mods == fyne.KeyModifierShift {
		_, _ = t.in.Write([]byte{asciiEscape, '[', 'Z'})
		return true
	}
	k, ok := modifiedKeys[name]
	if !ok {
		return false
	}
	m := 1
	if mods&fyne.KeyModifierShift != 0 {
		m++
	}
	if mods&fyne.KeyModifierControl != 0 {
		m += 4
	}
	_, _ = t.in.Write([]byte("\x1b[" + k.n + ";" + strconv.Itoa(m) + string(k.final)))
	return true
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

		// Cogpit patch: Ctrl, or Ctrl and ⇧, with a named key (an arrow, Delete, F1) sends xterm's
		// sequence (typeModifiedKey); Return, Enter and Backspace, which have none, go as without
		// Ctrl, as in xterm and Terminal.app; Tab and Escape send nothing. It took the name's first
		// letter as a character.
		named := len(ds.KeyName) > 1 && ds.KeyName != fyne.KeySpace
		if named && ds.Modifier&fyne.KeyModifierControl != 0 &&
			ds.Modifier&^(fyne.KeyModifierControl|fyne.KeyModifierShift) == 0 {
			if !t.typeModifiedKey(ds.KeyName, ds.Modifier) {
				switch ds.KeyName {
				case fyne.KeyReturn, fyne.KeyEnter:
					_, _ = t.in.Write([]byte{'\r'})
				case fyne.KeyBackspace:
					_, _ = t.in.Write([]byte{asciiBackspace})
				}
			}
			return
		}

		// handle CTRL+A to CTRL+_ and everything in-between
		if ds.Modifier == fyne.KeyModifierControl && len(ds.KeyName) > 0 && !named {
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
