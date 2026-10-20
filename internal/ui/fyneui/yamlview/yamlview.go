// Package yamlview is Cogpit's YAML viewer and editor: a widget that keeps its own lines and draws
// only the lines in the viewport, with YAML highlighting from theme tokens, line numbers,
// selection, copy and paste, undo and redo (history.go). Fyne's TextGrid stores every character as
// a cell and walks all rows for its minimum size, so a 5 MB document must never be loaded into one
// grid (docs/developer/spike-fyne.md, criterion S4).
//
// The view draws each visible line as a few canvas.Text runs, one per highlighted token, placed on a
// monospace column grid; a line costs a handful of canvas objects instead of two per character, and
// the GL painter caches run textures by content, so a keystroke re-rasterises only the edited run.
//
// Layer L4.
package yamlview

import (
	"math"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// View is the YAML viewer and editor widget. It starts read-only; read-only still allows the caret,
// selection and copy. Like every widget, call its methods on Fyne's main goroutine (fyne.Do from
// elsewhere).
type View struct {
	widget.BaseWidget

	// OnChanged is called after the user edits the text, not after SetText.
	OnChanged func()
	// OnScrollPast, when set, gets the part of a vertical scroll the view cannot take because it
	// is at its top or bottom (or has nothing to scroll), so that a view inside a scrolling
	// container (the detail drawer's editors) hands the rest on to it.
	OnScrollPast func(*fyne.ScrollEvent)
	// OnCopy, when set, gets the text a copy or a cut takes instead of the clipboard: an editor
	// that holds a Secret's value hands it to the clipboard for Secret values (cleared later).
	OnCopy func(string)
	// OnNothingToUndo, when set, gets an undo shortcut (⌘Z, Ctrl+Z) that finds nothing to undo in
	// the view's own history: an Edit tab hands it to its Undo Revert, whose text the history no
	// longer reaches because a Revert sets the text from outside.
	OnNothingToUndo func()
	// OnFind, when set, gets the find shortcut (⌘F, Ctrl+F): the find bar of the dock tab the view
	// edits in opens (the window's findbar.go). Without it the view ignores the key.
	OnFind func()

	doc       document
	readOnly  bool
	highlight bool
	// plainStrings colours plain scalars as strings (SetPlainStrings).
	plainStrings bool

	// caret is where typing goes; anchor is the other end of the selection (equal when none).
	caret, anchor pos
	// goalCol is the column vertical moves aim for, so that a caret moving through short lines
	// returns to its column; -1 when unset.
	goalCol int

	// scrollX and scrollY are the content offsets in pixels, as float64 so that the last lines of
	// a very long document stay addressable.
	scrollX, scrollY float64

	focused   bool
	shiftDown bool
	barHover  bool
	drag      dragMode
	// revealPending defers revealCaret until the widget has a size.
	revealPending bool

	// hist is the undo and redo history of the user's edits (history.go).
	hist history

	// cache holds the tokens of recently drawn lines, keyed by line index and checked against the
	// line's text and start state, so an edit or a shift of line numbers never shows stale colours.
	cache map[int]lineCache

	// The editor's preferences (Cogpit's Preferences › Editor): the text size in points (0: the
	// theme's), whether line numbers are hidden, and the indentation step (0: two spaces).
	textSize    float32
	hideNumbers bool
	tabSize     int
}

type lineCache struct {
	text  string
	start lineState
	toks  []token
}

type dragMode uint8

const (
	dragNone dragMode = iota
	dragSelect
	dragVBar
	dragHBar
)

var (
	_ fyne.Widget         = (*View)(nil)
	_ fyne.Focusable      = (*View)(nil)
	_ fyne.Shortcutable   = (*View)(nil)
	_ fyne.Tabbable       = (*View)(nil)
	_ fyne.Scrollable     = (*View)(nil)
	_ fyne.Draggable      = (*View)(nil)
	_ fyne.Tappable       = (*View)(nil)
	_ fyne.DoubleTappable = (*View)(nil)
	_ desktop.Keyable     = (*View)(nil)
	_ desktop.Mouseable   = (*View)(nil)
	_ desktop.Hoverable   = (*View)(nil)
	_ desktop.Cursorable  = (*View)(nil)
)

// New returns an empty, read-only view with highlighting on.
func New() *View {
	v := &View{
		doc:       newDocument(),
		readOnly:  true,
		highlight: true,
		goalCol:   -1,
		cache:     map[int]lineCache{},
	}
	v.ExtendBaseWidget(v)
	return v
}

// SetTextSize sets the text size in points; 0 takes the theme's.
func (v *View) SetTextSize(size float32) {
	if size != v.textSize {
		v.textSize = size
		v.Refresh()
	}
}

// SetLineNumbers shows or hides the line numbers; they are shown by default.
func (v *View) SetLineNumbers(on bool) {
	if on == v.hideNumbers {
		v.hideNumbers = !on
		v.Refresh()
	}
}

// SetTabSize sets the indentation step in spaces: what Tab inserts and indents by, Shift+Tab and
// Backspace in the indentation take away, and a new line after a key adds. Below 1 means two.
func (v *View) SetTabSize(n int) { v.tabSize = n }

// TextSize is the text size SetTextSize set; 0: the theme's.
func (v *View) TextSize() float32 { return v.textSize }

// LineNumbers reports whether the view shows line numbers.
func (v *View) LineNumbers() bool { return !v.hideNumbers }

// TabSize is the indentation step in spaces.
func (v *View) TabSize() int { return v.indentStep() }

// indentStep is the indentation step in spaces.
func (v *View) indentStep() int {
	if v.tabSize < 1 {
		return 2
	}
	return v.tabSize
}

// SetText replaces the document, clears the selection and the undo history and scrolls to the top.
// The view keeps text and slices it into lines without copying; CRLF and CR line ends become LF.
func (v *View) SetText(text string) {
	v.doc.setText(text)
	v.hist.reset()
	v.caret, v.anchor = pos{}, pos{}
	v.goalCol = -1
	v.scrollX, v.scrollY = 0, 0
	clear(v.cache)
	v.Refresh()
}

// UpdateText replaces the document but keeps the scroll position, the caret and the selection,
// clamped to the new text: for a document that changes while the user reads it, such as an object
// in the detail drawer that the cache updates. Unchanged text costs one comparison and keeps the
// undo history; a changed text clears it, as SetText does. Like SetText it does not call OnChanged.
func (v *View) UpdateText(text string) {
	if normaliseNewlines(text) == v.doc.joined() {
		return
	}
	v.doc.setText(text)
	v.hist.reset()
	v.caret = v.doc.clamp(v.caret)
	v.anchor = v.doc.clamp(v.anchor)
	clear(v.cache)
	v.clampScroll()
	v.Refresh()
}

// Text returns the document with "\n" line ends. After an edit this joins the lines, which costs
// time and memory proportional to the document; it is cached until the next edit.
func (v *View) Text() string { return v.doc.joined() }

// SetReadOnly switches editing off or on. A read-only view still moves the caret, selects and copies.
func (v *View) SetReadOnly(ro bool) {
	v.readOnly = ro
	v.Refresh()
}

// ReadOnly reports whether editing is off.
func (v *View) ReadOnly() bool { return v.readOnly }

// SetHighlighting switches YAML highlighting on or off.
func (v *View) SetHighlighting(on bool) {
	v.highlight = on
	v.Refresh()
}

// Highlighting reports whether YAML highlighting is on.
func (v *View) Highlighting() bool { return v.highlight }

// SetPlainStrings colours a plain scalar that is no boolean, null or number as a string, as an
// editor colours a value Kubernetes reads as a string: a Config Map's values in the drawer, where
// "auto" is a string and "true" a boolean literal. Off by default: the YAML of an object shows
// plain values in the text colour.
func (v *View) SetPlainStrings(on bool) {
	if v.plainStrings == on {
		return
	}
	v.plainStrings = on
	clear(v.cache)
	v.Refresh()
}

// HeightFor is the height that shows lines lines of the document in a view width wide: the
// lines, the padding above and below, and the horizontal scroll bar when the document is wider.
func (v *View) HeightFor(lines int, width float32) float32 {
	m := v.metrics()
	h := float64(lines)*m.lh + 2*float64(m.pad)
	if width > 0 && v.maxScrollX(fyne.NewSize(width, 1), m) > 0 {
		h += float64(m.bar)
	}
	return float32(math.Ceil(h))
}

// LineCount returns the number of lines; an empty document has one.
func (v *View) LineCount() int { return len(v.doc.lines) }

// CursorPosition returns the caret's 0-based line and its column in runes.
func (v *View) CursorPosition() (line, column int) {
	return v.caret.line, utf8.RuneCountInString(v.doc.lines[v.caret.line][:v.caret.off])
}

// SetCursorPosition moves the caret to a 0-based line and rune column, clears the selection and
// scrolls the caret into view. Out-of-range values are clamped.
func (v *View) SetCursorPosition(line, column int) {
	v.moveTo(v.posOf(line, column), false)
}

// posOf is the position of a 0-based line and rune column, clamped into the document.
func (v *View) posOf(line, column int) pos {
	line = min(max(line, 0), len(v.doc.lines)-1)
	text := v.doc.lines[line]
	off := 0
	for n := 0; n < column && off < len(text); n++ {
		_, size := utf8.DecodeRuneInString(text[off:])
		off += size
	}
	return pos{line, off}
}

// SelectRange selects from a 0-based line and rune column to another, the caret at the end, and
// scrolls the end into view: what a find widget does with a match. Out-of-range values are clamped.
func (v *View) SelectRange(fromLine, fromColumn, toLine, toColumn int) {
	v.anchor = v.posOf(fromLine, fromColumn)
	v.moveTo(v.posOf(toLine, toColumn), true)
}

// ReplaceSelection replaces the selection, or inserts at the caret when nothing is selected, as
// typing does: OnChanged follows. A read-only view ignores it.
func (v *View) ReplaceSelection(s string) { v.replaceSelection(normaliseNewlines(s)) }

// ScrollToLine scrolls so that the 0-based line is at the top of the view, or as near as the end of
// the document allows. The caret stays where it is.
func (v *View) ScrollToLine(line int) {
	line = min(max(line, 0), len(v.doc.lines)-1)
	v.scrollY = float64(line) * v.metrics().lh
	v.clampScroll()
	v.Refresh()
}

// SelectedText returns the selected text, or "" when nothing is selected.
func (v *View) SelectedText() string {
	if v.anchor == v.caret {
		return ""
	}
	return v.doc.slice(v.anchor, v.caret)
}

// SelectAll selects the whole document.
func (v *View) SelectAll() {
	v.hist.seal()
	v.anchor = pos{}
	v.caret = v.doc.end()
	v.goalCol = -1
	v.Refresh()
}

// Undo takes the newest step of the history back and restores the caret and the selection it found;
// false when there is nothing to undo or the view is read-only. OnChanged follows, as for any edit.
func (v *View) Undo() bool {
	if v.readOnly {
		return false
	}
	s, ok := v.hist.undo()
	if !ok {
		return false
	}
	v.doc.remove(s.start, s.end)
	v.doc.insert(s.start, s.old)
	v.restoreCursor(s.before)
	return true
}

// Redo does the step Undo took back again and restores the caret and the selection it left; false
// when there is nothing to redo or the view is read-only.
func (v *View) Redo() bool {
	if v.readOnly {
		return false
	}
	s, ok := v.hist.redo()
	if !ok {
		return false
	}
	v.doc.remove(s.start, endOf(s.start, s.old))
	v.doc.insert(s.start, s.new)
	v.restoreCursor(s.after)
	return true
}

// ClearHistory forgets every edit Undo and Redo could reach, and the text they hold: a Secret's
// editor in the drawer drops it when the drawer moves away.
func (v *View) ClearHistory() { v.hist.reset() }

// restoreCursor puts the caret and the selection back after an undo or a redo and finishes it as an
// edit.
func (v *View) restoreCursor(c cursor) {
	v.caret, v.anchor = v.doc.clamp(c.caret), v.doc.clamp(c.anchor)
	v.edited()
}

// CreateRenderer implements fyne.Widget.
func (v *View) CreateRenderer() fyne.WidgetRenderer {
	return newRenderer(v)
}

// FocusGained implements fyne.Focusable.
func (v *View) FocusGained() {
	v.focused = true
	v.Refresh()
}

// FocusLost implements fyne.Focusable.
func (v *View) FocusLost() {
	v.focused = false
	v.shiftDown = false
	v.drag = dragNone
	v.hist.seal()
	v.Refresh()
}

// AcceptsTab implements fyne.Tabbable: an editable view takes Tab to indent; a read-only one lets it
// move the focus on.
func (v *View) AcceptsTab() bool { return !v.readOnly }

// TypedRune implements fyne.Focusable.
func (v *View) TypedRune(r rune) {
	if v.readOnly || (r < 0x20 && r != '\t') || r == 0x7f || !utf8.ValidRune(r) {
		return
	}
	v.replace(string(r), editTyping)
}

// TypedKey implements fyne.Focusable.
func (v *View) TypedKey(ev *fyne.KeyEvent) {
	extend := v.shiftHeld()
	switch ev.Name {
	case fyne.KeyLeft:
		v.moveHorizontal(-1, extend)
	case fyne.KeyRight:
		v.moveHorizontal(1, extend)
	case fyne.KeyUp:
		v.moveVertical(-1, extend)
	case fyne.KeyDown:
		v.moveVertical(1, extend)
	case fyne.KeyPageUp:
		v.movePage(-1, extend)
	case fyne.KeyPageDown:
		v.movePage(1, extend)
	case fyne.KeyHome:
		v.moveHome(extend)
	case fyne.KeyEnd:
		v.moveTo(pos{v.caret.line, len(v.doc.lines[v.caret.line])}, extend)
	case fyne.KeyReturn, fyne.KeyEnter:
		v.newline()
	case fyne.KeyBackspace:
		v.backspace()
	case fyne.KeyDelete:
		v.deleteForward()
	case fyne.KeyTab:
		if extend {
			v.indentLines(false)
		} else {
			v.tab()
		}
	case fyne.KeyEscape:
		if v.anchor != v.caret {
			v.anchor = v.caret
			v.Refresh()
		}
	}
}

// KeyDown implements desktop.Keyable; it tracks Shift for keyboard selection.
func (v *View) KeyDown(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		v.shiftDown = true
	}
}

// KeyUp implements desktop.Keyable.
func (v *View) KeyUp(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		v.shiftDown = false
	}
}

// TypedShortcut implements fyne.Shortcutable: copy, cut, paste, select all, undo (⌘Z, Ctrl+Z) and
// redo (⌘Y, Ctrl+Y, and ⇧⌘Z or Ctrl+Shift+Z, which Fyne delivers as a custom shortcut), find (⌘F,
// Ctrl+F, to OnFind), and the navigation shortcuts that arrive with modifiers (document start and
// end, line start and end, word moves).
func (v *View) TypedShortcut(s fyne.Shortcut) {
	switch sc := s.(type) {
	case *fyne.ShortcutUndo:
		if !v.Undo() && !v.readOnly && v.OnNothingToUndo != nil {
			v.OnNothingToUndo()
		}
	case *fyne.ShortcutRedo:
		v.Redo()
	case *fyne.ShortcutCopy:
		v.copySelection(clipboardOr(sc.Clipboard))
	case *fyne.ShortcutCut:
		v.cutSelection(clipboardOr(sc.Clipboard))
	case *fyne.ShortcutPaste:
		v.paste(clipboardOr(sc.Clipboard))
	case *fyne.ShortcutSelectAll:
		v.SelectAll()
	case *desktop.CustomShortcut:
		v.customShortcut(sc)
	}
}

// Scrolled implements fyne.Scrollable. What of a vertical scroll the view cannot take goes to
// OnScrollPast.
func (v *View) Scrolled(ev *fyne.ScrollEvent) {
	y := v.scrollY
	v.scrollX -= float64(ev.Scrolled.DX)
	v.scrollY -= float64(ev.Scrolled.DY)
	v.clampScroll()
	v.Refresh()
	if v.OnScrollPast == nil || ev.Scrolled.DY == 0 {
		return
	}
	if rest := ev.Scrolled.DY - float32(y-v.scrollY); rest != 0 {
		v.OnScrollPast(&fyne.ScrollEvent{PointEvent: ev.PointEvent, Scrolled: fyne.Delta{DY: rest}})
	}
}

// Tapped implements fyne.Tappable; MouseDown places the caret.
func (v *View) Tapped(*fyne.PointEvent) { v.requestFocus() }

// DoubleTapped implements fyne.DoubleTappable: it selects the word under the pointer.
func (v *View) DoubleTapped(ev *fyne.PointEvent) {
	p := v.posAt(ev.Position)
	line := v.doc.lines[p.line]
	start, end := p.off, p.off
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(line[:start])
		if !isWordRune(r) {
			break
		}
		start -= size
	}
	for end < len(line) {
		r, size := utf8.DecodeRuneInString(line[end:])
		if !isWordRune(r) {
			break
		}
		end += size
	}
	v.hist.seal()
	v.anchor, v.caret = pos{p.line, start}, pos{p.line, end}
	v.goalCol = -1
	v.Refresh()
}

// MouseDown implements desktop.Mouseable: it places the caret, extends the selection with Shift,
// or grabs a scroll bar.
func (v *View) MouseDown(ev *desktop.MouseEvent) {
	v.requestFocus()
	if ev.Button != desktop.MouseButtonPrimary {
		return
	}
	size := v.Size()
	m := v.metrics()
	if mode, ok := v.barAt(ev.Position, size, m); ok {
		v.drag = mode
		v.jumpBar(mode, ev.Position, size, m)
		v.Refresh()
		return
	}
	extend := ev.Modifier&fyne.KeyModifierShift != 0 || v.shiftDown
	v.drag = dragSelect
	v.moveTo(v.posAt(ev.Position), extend)
}

// MouseUp implements desktop.Mouseable.
func (v *View) MouseUp(*desktop.MouseEvent) {
	if v.drag != dragNone {
		v.drag = dragNone
		v.Refresh()
	}
}

// Dragged implements fyne.Draggable: it extends the selection, scrolling when the pointer leaves the
// view, or drags a scroll bar.
func (v *View) Dragged(ev *fyne.DragEvent) {
	size := v.Size()
	m := v.metrics()
	switch v.drag {
	case dragVBar:
		_, thumbH, track := v.vThumb(size, m)
		if span := track - thumbH; span > 0 {
			v.scrollY += float64(ev.Dragged.DY) * v.maxScrollY(size, m) / float64(span)
		}
	case dragHBar:
		_, thumbW, track := v.hThumb(size, m)
		if span := track - thumbW; span > 0 {
			v.scrollX += float64(ev.Dragged.DX) * v.maxScrollX(size, m) / float64(span)
		}
	default:
		if v.drag == dragNone { // a drag without MouseDown (touch): start where it started
			v.drag = dragSelect
			start := ev.Position.Subtract(ev.Dragged)
			v.anchor = v.posAt(fyne.NewPos(start.X, start.Y))
		}
		v.hist.seal()
		v.caret = v.posAt(ev.Position)
		v.goalCol = -1
		v.revealCaret()
	}
	v.clampScroll()
	v.Refresh()
}

// DragEnd implements fyne.Draggable. The view takes the focus back, so that a selection or a scroll
// bar dragged out of it keeps the keyboard wherever the button comes up, as a macOS text view does
// (the owner's WF8, 2026-10-17): Fyne 2.8.2's desktop driver runs its "a click outside the focused
// widget unfocuses it" rule on the release as on the press (glfw window.go, processMouseClicked), so
// a release over another widget unfocused the editor, and Backspace then deleted nothing. A press
// in the view always focuses it (MouseDown), so this only undoes the release's unfocus.
func (v *View) DragEnd() {
	v.drag = dragNone
	v.requestFocus()
	v.Refresh()
}

// MouseIn implements desktop.Hoverable.
func (v *View) MouseIn(ev *desktop.MouseEvent) { v.MouseMoved(ev) }

// MouseMoved implements desktop.Hoverable: the scroll bars widen under the pointer.
func (v *View) MouseMoved(ev *desktop.MouseEvent) {
	_, hover := v.barAt(ev.Position, v.Size(), v.metrics())
	if hover != v.barHover {
		v.barHover = hover
		v.Refresh()
	}
}

// MouseOut implements desktop.Hoverable.
func (v *View) MouseOut() {
	if v.barHover {
		v.barHover = false
		v.Refresh()
	}
}

// Cursor implements desktop.Cursorable.
func (v *View) Cursor() desktop.Cursor { return desktop.TextCursor }

// --- caret movement ---

// moveTo puts the caret at p; extend keeps the anchor, so the selection grows. A move ends a run of
// typing in the undo history.
func (v *View) moveTo(p pos, extend bool) {
	v.hist.seal()
	v.caret = v.doc.clamp(p)
	if !extend {
		v.anchor = v.caret
	}
	v.revealCaret()
	v.Refresh()
}

func (v *View) moveHorizontal(dir int, extend bool) {
	v.goalCol = -1
	if !extend && v.anchor != v.caret {
		a, b := order(v.anchor, v.caret)
		if dir < 0 {
			v.moveTo(a, false)
		} else {
			v.moveTo(b, false)
		}
		return
	}
	if dir < 0 {
		v.moveTo(v.doc.prev(v.caret), extend)
	} else {
		v.moveTo(v.doc.next(v.caret), extend)
	}
}

func (v *View) moveVertical(lines int, extend bool) {
	line := v.caret.line + lines
	switch {
	case line < 0:
		v.goalCol = -1
		v.moveTo(pos{0, 0}, extend)
		return
	case line >= len(v.doc.lines):
		v.goalCol = -1
		v.moveTo(v.doc.end(), extend)
		return
	}
	if v.goalCol < 0 {
		v.goalCol = displayCol(v.doc.lines[v.caret.line], v.caret.off)
	}
	goal := v.goalCol
	v.moveTo(pos{line, offsetForCol(v.doc.lines[line], goal)}, extend)
	v.goalCol = goal
}

// movePage moves the caret and the view by a page less one line.
func (v *View) movePage(dir int, extend bool) {
	size := v.Size()
	m := v.metrics()
	rows := max(1, int(float64(size.Height)/m.lh)-1)
	v.scrollY += float64(dir*rows) * m.lh
	v.clampScroll()
	v.moveVertical(dir*rows, extend)
}

// moveHome goes to the first non-space character of the line, or to column 0 when already there.
func (v *View) moveHome(extend bool) {
	v.goalCol = -1
	line := v.doc.lines[v.caret.line]
	target := indentOf(line)
	if v.caret.off == target {
		target = 0
	}
	v.moveTo(pos{v.caret.line, target}, extend)
}

// primaryModifier is the modifier of the platform's editing shortcuts, as Fyne's driver maps them:
// ⌘ on macOS, Ctrl elsewhere.
var primaryModifier = func() fyne.KeyModifier {
	if runtime.GOOS == "darwin" {
		return fyne.KeyModifierSuper
	}
	return fyne.KeyModifierControl
}()

func (v *View) customShortcut(sc *desktop.CustomShortcut) {
	extend := sc.Modifier&fyne.KeyModifierShift != 0
	mod := sc.Modifier &^ fyne.KeyModifierShift
	super := mod == fyne.KeyModifierSuper
	ctrl := mod == fyne.KeyModifierControl
	word := mod == fyne.KeyModifierAlt || ctrl
	switch {
	case sc.KeyName == fyne.KeyZ && extend && mod == primaryModifier: // ⇧⌘Z, Ctrl+Shift+Z: redo
		v.Redo()
	case sc.KeyName == fyne.KeyF && !extend && mod == primaryModifier: // ⌘F, Ctrl+F: find
		if v.OnFind != nil {
			v.OnFind()
		}
	case (sc.KeyName == fyne.KeyHome && (ctrl || super)) || (sc.KeyName == fyne.KeyUp && super):
		v.goalCol = -1
		v.moveTo(pos{}, extend)
	case (sc.KeyName == fyne.KeyEnd && (ctrl || super)) || (sc.KeyName == fyne.KeyDown && super):
		v.goalCol = -1
		v.moveTo(v.doc.end(), extend)
	case sc.KeyName == fyne.KeyLeft && super:
		v.moveHome(extend)
	case sc.KeyName == fyne.KeyRight && super:
		v.goalCol = -1
		v.moveTo(pos{v.caret.line, len(v.doc.lines[v.caret.line])}, extend)
	case sc.KeyName == fyne.KeyLeft && word:
		v.goalCol = -1
		v.moveTo(v.wordLeft(v.caret), extend)
	case sc.KeyName == fyne.KeyRight && word:
		v.goalCol = -1
		v.moveTo(v.wordRight(v.caret), extend)
	case sc.KeyName == fyne.KeyBackspace && word && !v.readOnly:
		kind := editOther // a selection deleted
		if v.anchor == v.caret {
			v.anchor, kind = v.wordLeft(v.caret), editBackspace
		}
		v.replace("", kind)
	case sc.KeyName == fyne.KeyDelete && word && !v.readOnly:
		kind := editOther
		if v.anchor == v.caret {
			v.anchor, kind = v.wordRight(v.caret), editDelete
		}
		v.replace("", kind)
	}
}

// wordLeft returns the start of the word before p, crossing to the previous line at column 0.
func (v *View) wordLeft(p pos) pos {
	if p.off == 0 {
		return v.doc.prev(p)
	}
	line := v.doc.lines[p.line]
	off := p.off
	for off > 0 { // spaces
		r, size := utf8.DecodeLastRuneInString(line[:off])
		if !unicode.IsSpace(r) {
			break
		}
		off -= size
	}
	if off > 0 {
		r, _ := utf8.DecodeLastRuneInString(line[:off])
		word := isWordRune(r)
		for off > 0 {
			r, size := utf8.DecodeLastRuneInString(line[:off])
			if isWordRune(r) != word || unicode.IsSpace(r) {
				break
			}
			off -= size
		}
	}
	return pos{p.line, off}
}

// wordRight returns the end of the word after p, crossing to the next line at the end of a line.
func (v *View) wordRight(p pos) pos {
	line := v.doc.lines[p.line]
	if p.off == len(line) {
		return v.doc.next(p)
	}
	off := p.off
	for off < len(line) { // spaces
		r, size := utf8.DecodeRuneInString(line[off:])
		if !unicode.IsSpace(r) {
			break
		}
		off += size
	}
	if off < len(line) {
		r, _ := utf8.DecodeRuneInString(line[off:])
		word := isWordRune(r)
		for off < len(line) {
			r, size := utf8.DecodeRuneInString(line[off:])
			if isWordRune(r) != word || unicode.IsSpace(r) {
				break
			}
			off += size
		}
	}
	return pos{p.line, off}
}

// isWordRune says what a double click and a word move treat as one word; '-' and '_' join, so that
// Kubernetes names like kube-system stay whole.
func isWordRune(r rune) bool {
	return r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// --- editing ---

// replaceSelection replaces the selection (or inserts at the caret) with s, a step of its own in the
// undo history.
func (v *View) replaceSelection(s string) { v.replace(s, editOther) }

// replace replaces the selection (or inserts at the caret) with s and records the edit as kind:
// typing and a deletion at the caret join the step before them (history.go).
func (v *View) replace(s string, kind editKind) {
	if v.readOnly {
		return
	}
	a, b := order(v.doc.clamp(v.anchor), v.doc.clamp(v.caret))
	if a == b && s == "" {
		return
	}
	s = normaliseNewlines(s)
	before := cursor{v.caret, v.anchor}
	old := v.doc.remove(a, b)
	p := v.doc.insert(a, s)
	v.caret, v.anchor = p, p
	v.hist.record(step{start: a, old: old, new: s, before: before, after: cursor{p, p}, kind: kind})
	v.edited()
}

// edited finishes every edit: caret into view, repaint, OnChanged.
func (v *View) edited() {
	v.goalCol = -1
	v.revealCaret()
	v.Refresh()
	if v.OnChanged != nil {
		v.OnChanged()
	}
}

// newline inserts a line break keeping the current line's indentation, two spaces more after a key
// or a block scalar header, as Lens's editor does.
func (v *View) newline() {
	if v.readOnly {
		return
	}
	a, _ := order(v.anchor, v.caret)
	line := v.doc.lines[a.line]
	indent := line[:min(indentOf(line), a.off)]
	before := strings.TrimRight(line[:a.off], " \t")
	if strings.HasSuffix(before, ":") || endsWithBlockHeader(before) {
		indent += strings.Repeat(" ", v.indentStep())
	}
	v.replace("\n"+indent, editTyping) // a new line starts a new run of typing
}

// endsWithBlockHeader reports whether s ends in a block scalar header such as "|", ">-" or "|+2".
func endsWithBlockHeader(s string) bool {
	i := strings.LastIndexAny(s, "|>")
	if i < 0 || (i > 0 && !isSpace(s[i-1])) {
		return false
	}
	return isBlockHeader(s, i)
}

func (v *View) backspace() {
	if v.readOnly {
		return
	}
	kind := editOther // a selection deleted: a step of its own
	if v.anchor == v.caret {
		kind = editBackspace
		line := v.doc.lines[v.caret.line]
		if v.caret.off > 0 && indentOf(line) >= v.caret.off {
			// In the indentation: back to the previous indentation stop.
			step := v.indentStep()
			n := v.caret.off % step
			if n == 0 {
				n = step
			}
			v.anchor = pos{v.caret.line, v.caret.off - n}
		} else {
			v.anchor = v.doc.prev(v.caret)
		}
	}
	v.replace("", kind)
}

func (v *View) deleteForward() {
	if v.readOnly {
		return
	}
	kind := editOther
	if v.anchor == v.caret {
		v.anchor, kind = v.doc.next(v.caret), editDelete
	}
	v.replace("", kind)
}

// tab indents the selected lines when the selection spans lines, and otherwise inserts spaces to
// the next indentation stop (two spaces unless SetTabSize says otherwise); YAML forbids tabs for
// indentation.
func (v *View) tab() {
	if v.readOnly {
		return
	}
	if v.anchor.line != v.caret.line {
		v.indentLines(true)
		return
	}
	a, _ := order(v.anchor, v.caret)
	step := v.indentStep()
	n := step - displayCol(v.doc.lines[a.line], a.off)%step
	v.replace(strings.Repeat(" ", n), editTyping)
}

// indentLines adds or removes one indentation step (indentStep) on every line the selection
// touches, or on the caret's line. A selection ending at column 0 leaves that line alone. The change
// is one step in the undo history: the lines as they were and as they are.
func (v *View) indentLines(add bool) {
	if v.readOnly {
		return
	}
	a, b := order(v.anchor, v.caret)
	last := b.line
	if b.line > a.line && b.off == 0 {
		last--
	}
	first := pos{a.line, 0}
	lines := func() string { return v.doc.slice(first, pos{last, len(v.doc.lines[last])}) }
	old, before := lines(), cursor{v.caret, v.anchor}
	changed := false
	width := v.indentStep()
	for i := a.line; i <= last; i++ {
		var delta int
		if add {
			if v.doc.lines[i] == "" {
				continue
			}
			v.doc.insert(pos{i, 0}, strings.Repeat(" ", width))
			delta = width
		} else {
			n := min(width, indentOf(v.doc.lines[i]))
			if n == 0 {
				continue
			}
			v.doc.remove(pos{i, 0}, pos{i, n})
			delta = -n
		}
		changed = true
		for _, p := range []*pos{&v.caret, &v.anchor} {
			if p.line == i && (p.off > 0 || add) {
				p.off = max(0, p.off+delta)
			}
		}
	}
	if changed {
		v.hist.record(step{start: first, old: old, new: lines(), before: before, after: cursor{v.caret, v.anchor}})
		v.edited()
	}
}

func (v *View) copySelection(cb fyne.Clipboard) {
	text := v.SelectedText()
	switch {
	case text == "":
	case v.OnCopy != nil:
		v.OnCopy(text)
	case cb != nil:
		cb.SetContent(text)
	}
}

// cutSelection copies and removes the selection; in a read-only view it does nothing.
func (v *View) cutSelection(cb fyne.Clipboard) {
	if v.readOnly || cb == nil || v.anchor == v.caret {
		return
	}
	v.copySelection(cb)
	v.replaceSelection("")
}

func (v *View) paste(cb fyne.Clipboard) {
	if v.readOnly || cb == nil {
		return
	}
	if text := cb.Content(); text != "" {
		v.replaceSelection(text)
	}
}

// --- geometry ---

// posAt maps a point in the widget to the nearest document position; points outside the text area
// clamp to the first or last line and column 0.
func (v *View) posAt(p fyne.Position) pos {
	m := v.metrics()
	y := float64(p.Y) - float64(m.pad) + v.scrollY
	line := 0
	if y > 0 {
		line = min(int(y/m.lh), len(v.doc.lines)-1)
	}
	x := float64(p.X) - m.textX() + v.scrollX
	col := 0
	if x > 0 {
		col = int(math.Round(x / m.cw))
	}
	return pos{line, offsetForCol(v.doc.lines[line], col)}
}

// revealCaret scrolls just enough to show the caret.
func (v *View) revealCaret() {
	size := v.Size()
	if size.Width <= 0 || size.Height <= 0 {
		v.revealPending = true
		return
	}
	m := v.metrics()
	top := float64(v.caret.line) * m.lh
	viewH := float64(size.Height) - 2*float64(m.pad)
	if v.hScrollable(size, m) {
		viewH -= float64(m.bar)
	}
	if top < v.scrollY {
		v.scrollY = top
	} else if top+m.lh > v.scrollY+viewH {
		v.scrollY = top + m.lh - viewH
	}
	x := float64(displayCol(v.doc.lines[v.caret.line], v.caret.off)) * m.cw
	areaW := float64(size.Width) - m.textX() - float64(m.bar)
	margin := 4 * m.cw
	if x < v.scrollX {
		v.scrollX = x - margin
		if x+m.cw+margin <= areaW { // the line start fits too: show it
			v.scrollX = 0
		}
	} else if x+m.cw > v.scrollX+areaW {
		v.scrollX = x + m.cw - areaW + margin
	}
	v.clampScroll()
}

// clampScroll keeps the offsets inside the content; without a size it only stops negative offsets.
func (v *View) clampScroll() {
	size := v.Size()
	if size.Width > 0 && size.Height > 0 {
		m := v.metrics()
		v.scrollX = min(v.scrollX, v.maxScrollX(size, m))
		v.scrollY = min(v.scrollY, v.maxScrollY(size, m))
	}
	v.scrollX = max(v.scrollX, 0)
	v.scrollY = max(v.scrollY, 0)
}

func (v *View) maxScrollY(size fyne.Size, m metrics) float64 {
	content := float64(len(v.doc.lines))*m.lh + 2*float64(m.pad)
	if v.hScrollable(size, m) {
		content += float64(m.bar)
	}
	return max(0, content-float64(size.Height))
}

func (v *View) maxScrollX(size fyne.Size, m metrics) float64 {
	content := float64(v.doc.extent+2) * m.cw
	return max(0, content-(float64(size.Width)-m.textX()))
}

func (v *View) vScrollable(size fyne.Size, m metrics) bool { return v.maxScrollY(size, m) > 0 }

func (v *View) hScrollable(size fyne.Size, m metrics) bool { return v.maxScrollX(size, m) > 0 }

// vThumb returns the vertical thumb's offset and length and the length of its track.
func (v *View) vThumb(size fyne.Size, m metrics) (offset, length, track float32) {
	track = size.Height
	maxY := v.maxScrollY(size, m)
	content := float64(track) + maxY
	length = max(float32(float64(track)*float64(track)/content), 2*m.bar)
	length = min(length, track)
	if maxY > 0 {
		offset = float32(v.scrollY / maxY * float64(track-length))
	}
	return offset, length, track
}

// hThumb is vThumb for the horizontal bar, whose track starts after the gutter.
func (v *View) hThumb(size fyne.Size, m metrics) (offset, length, track float32) {
	track = max(size.Width-m.gutterW-m.bar, 0)
	maxX := v.maxScrollX(size, m)
	content := float64(track) + maxX
	if content <= 0 {
		return 0, 0, track
	}
	length = min(max(float32(float64(track)*float64(track)/content), 2*m.bar), track)
	if maxX > 0 {
		offset = float32(v.scrollX / maxX * float64(track-length))
	}
	return offset, length, track
}

// barAt reports which scroll bar, if any, is under p.
func (v *View) barAt(p fyne.Position, size fyne.Size, m metrics) (dragMode, bool) {
	if p.X >= size.Width-m.bar && v.vScrollable(size, m) {
		return dragVBar, true
	}
	if p.Y >= size.Height-m.bar && p.X >= m.gutterW && v.hScrollable(size, m) {
		return dragHBar, true
	}
	return dragNone, false
}

// jumpBar centres the thumb on a click in the track outside it; a click on the thumb only grabs it.
func (v *View) jumpBar(mode dragMode, p fyne.Position, size fyne.Size, m metrics) {
	switch mode {
	case dragVBar:
		off, length, track := v.vThumb(size, m)
		if p.Y < off || p.Y > off+length {
			if span := track - length; span > 0 {
				v.scrollY = float64((p.Y-length/2)/span) * v.maxScrollY(size, m)
			}
		}
	case dragHBar:
		off, length, track := v.hThumb(size, m)
		x := p.X - m.gutterW
		if x < off || x > off+length {
			if span := track - length; span > 0 {
				v.scrollX = float64((x-length/2)/span) * v.maxScrollX(size, m)
			}
		}
	}
	v.clampScroll()
}

// --- helpers ---

// shiftHeld reports whether Shift is down, from the key events this view saw or from the driver.
func (v *View) shiftHeld() bool {
	if v.shiftDown {
		return true
	}
	if app := fyne.CurrentApp(); app != nil {
		if d, ok := app.Driver().(desktop.Driver); ok {
			return d.CurrentKeyModifiers()&fyne.KeyModifierShift != 0
		}
	}
	return false
}

func (v *View) requestFocus() {
	app := fyne.CurrentApp()
	if app == nil || app.Driver() == nil {
		return
	}
	if c := app.Driver().CanvasForObject(v); c != nil && c.Focused() != v {
		c.Focus(v)
	}
}

// clipboardOr returns cb, or the app's clipboard when the shortcut carries none.
func clipboardOr(cb fyne.Clipboard) fyne.Clipboard {
	if cb != nil {
		return cb
	}
	if app := fyne.CurrentApp(); app != nil {
		return app.Clipboard()
	}
	return nil
}

// lineTokens returns the tokens of line i, from the cache when the line and its start state are
// unchanged.
func (v *View) lineTokens(i int) []token {
	if !v.highlight {
		return nil
	}
	text := v.doc.lines[i]
	st := v.doc.stateAt(i)
	if c, ok := v.cache[i]; ok && c.start == st && c.text == text {
		return c.toks
	}
	toks, _ := tokenizeAs(text, st, v.plainStrings)
	v.cache[i] = lineCache{text: text, start: st, toks: toks}
	return toks
}

// pruneCache drops cached lines far from the viewport [first, last).
func (v *View) pruneCache(first, last int) {
	rows := last - first
	if len(v.cache) <= 4*rows+64 {
		return
	}
	for i := range v.cache {
		if i < first-rows || i >= last+rows {
			delete(v.cache, i)
		}
	}
}
