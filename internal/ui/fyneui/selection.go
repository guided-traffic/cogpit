package fyneui

import (
	"image/color"
	"math"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Selecting and copying text in the detail drawer (D-25). The drawer's texts are selectable: a drag
// selects, a double click selects a word, and the copy shortcut of the platform (⌘C on macOS,
// Ctrl+C elsewhere) copies the selection. Two widgets do it: selLine over a textLine (proportional
// text, one line, cut off with an ellipsis when it does not fit) and secretText (a revealed Secret
// value: monospace, wrapped at any character). Both share textSelection.
//
// The widgets take focus when clicked, but are "disabled" for Fyne's focus chain: Tab does not stop
// at every line of the drawer (Fyne lets a disabled object that has SelectedText take focus by a
// click, as it does for its own selectable labels). Selecting is a GUI behaviour only: it has no
// command, and an agent reads values through object_get.

// textSelection is the selection state and the mouse and keyboard handling of a selectable text.
// The widget that embeds it sets the functions.
type textSelection struct {
	self interface {
		fyne.CanvasObject
		fyne.Focusable
	}
	anchor, caret int // rune offsets into the text; equal when nothing is selected
	dragging      bool
	guard         dragGuard // a drag pressed outside the text selects nothing (VW20, dragguard.go)
	getText       func() string
	offsetAt      func(fyne.Position) int // the rune offset nearest a point of the widget
	repaint       func()                  // the selection changed
	copied        func(selected string)   // the copy shortcut ran
}

var (
	_ desktop.Mouseable   = (*textSelection)(nil)
	_ desktop.Cursorable  = (*textSelection)(nil)
	_ fyne.Draggable      = (*textSelection)(nil)
	_ fyne.DoubleTappable = (*textSelection)(nil)
	_ fyne.Focusable      = (*textSelection)(nil)
	_ fyne.Shortcutable   = (*textSelection)(nil)
	_ fyne.Disableable    = (*textSelection)(nil)
)

// bounds returns the selection's start and end, start first.
func (s *textSelection) bounds() (lo, hi int) {
	lo, hi = s.anchor, s.caret
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

// SelectedText returns the selected text, empty when nothing is selected.
func (s *textSelection) SelectedText() string {
	lo, hi := s.bounds()
	if lo == hi {
		return ""
	}
	r := []rune(s.getText())
	lo, hi = min(lo, len(r)), min(hi, len(r))
	return string(r[lo:hi])
}

func (s *textSelection) clear() {
	if s.anchor == s.caret {
		return
	}
	s.anchor, s.caret = 0, 0
	s.repaint()
}

func (s *textSelection) grab() {
	if c := fyne.CurrentApp().Driver().CanvasForObject(s.self); c != nil {
		c.Focus(s.self)
	}
}

// MouseDown implements desktop.Mouseable: a click takes focus and drops a selection.
func (s *textSelection) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button != desktop.MouseButtonPrimary {
		return
	}
	s.grab()
	s.anchor = s.offsetAt(ev.Position)
	s.caret = s.anchor
	s.repaint()
}

// MouseUp implements desktop.Mouseable.
func (s *textSelection) MouseUp(*desktop.MouseEvent) {}

// Dragged implements fyne.Draggable: the drag selects from where it started to the pointer; one
// pressed outside the text selects nothing and takes no focus.
func (s *textSelection) Dragged(ev *fyne.DragEvent) {
	if !s.guard.pass(s.self, ev) {
		return
	}
	if !s.dragging {
		s.dragging = true
		s.grab()
		s.anchor = s.offsetAt(ev.Position.Subtract(ev.Dragged))
	}
	s.caret = s.offsetAt(ev.Position)
	s.repaint()
}

// DragEnd implements fyne.Draggable.
func (s *textSelection) DragEnd() { s.endDrag() }

// endDrag ends a drag and reports whether it went on to the text: false for one pressed outside.
func (s *textSelection) endDrag() bool {
	s.dragging = false
	return s.guard.end()
}

// DoubleTapped implements fyne.DoubleTappable: it selects the word under the pointer, a run of
// characters that are no white space.
func (s *textSelection) DoubleTapped(ev *fyne.PointEvent) {
	r := []rune(s.getText())
	if len(r) == 0 {
		return
	}
	i := min(s.offsetAt(ev.Position), len(r)-1)
	if unicode.IsSpace(r[i]) && i > 0 {
		i--
	}
	if unicode.IsSpace(r[i]) {
		return
	}
	lo, hi := i, i+1
	for lo > 0 && !unicode.IsSpace(r[lo-1]) {
		lo--
	}
	for hi < len(r) && !unicode.IsSpace(r[hi]) {
		hi++
	}
	s.grab()
	s.anchor, s.caret = lo, hi
	s.repaint()
}

// Cursor implements desktop.Cursorable.
func (s *textSelection) Cursor() desktop.Cursor { return desktop.TextCursor }

// FocusGained implements fyne.Focusable.
func (s *textSelection) FocusGained() {}

// FocusLost implements fyne.Focusable: the selection goes when another text takes the focus.
func (s *textSelection) FocusLost() { s.clear() }

// TypedRune implements fyne.Focusable.
func (s *textSelection) TypedRune(rune) {}

// TypedKey implements fyne.Focusable.
func (s *textSelection) TypedKey(*fyne.KeyEvent) {}

// TypedShortcut implements fyne.Shortcutable: copy and select all.
func (s *textSelection) TypedShortcut(sc fyne.Shortcut) {
	switch sc.(type) {
	case *fyne.ShortcutCopy:
		if t := s.SelectedText(); t != "" && s.copied != nil {
			s.copied(t)
		}
	case *fyne.ShortcutSelectAll:
		s.anchor, s.caret = 0, len([]rune(s.getText()))
		s.repaint()
	}
}

// Disabled implements fyne.Disableable: always true, so that the focus chain skips the text; a
// click still focuses it.
func (s *textSelection) Disabled() bool { return true }

// Disable implements fyne.Disableable.
func (s *textSelection) Disable() {}

// Enable implements fyne.Disableable.
func (s *textSelection) Enable() {}

// selLine makes a textLine selectable, and a place where the copy icon shows (copy.go). The line
// draws the text; selLine adds the selection's background and the mouse handling, so that tests
// and layouts still see a textLine.
type selLine struct {
	widget.BaseWidget
	textSelection
	line    *textLine
	hook    *copyHook // the copy icon's target; nil: the text has no icon
	room    float32   // kept free of text at the right end: the copy icon is there (copy.go)
	tap     func()    // a click that drags nothing: the row's (passClick); nil: it only drops a selection
	pressed bool      // the primary button went down on the line
}

// passClick makes a click on the line the click of the row it stands in: a list row's text worth
// copying (a version, a date, a name) still selects with a drag or a double click, and a click on it
// picks the row at once, as one beside it does. Fyne hands a click to the line under the pointer,
// never to the row beneath it, and a double-tappable widget's Tapped comes only after the system's
// double-click interval (the owner, 2026-10-26: the Revisions tab's rows could hardly be clicked).
func (s *selLine) passClick(tap func()) { s.tap = tap }

// MouseDown implements desktop.Mouseable: a click takes focus and drops a selection.
func (s *selLine) MouseDown(ev *desktop.MouseEvent) {
	s.pressed = ev.Button == desktop.MouseButtonPrimary
	s.textSelection.MouseDown(ev)
}

// MouseUp implements desktop.Mouseable: a click that dragged nothing goes on to the row. The driver
// ends a drag after this, so a drag that selected is still dragging here.
func (s *selLine) MouseUp(*desktop.MouseEvent) {
	pressed := s.pressed
	s.pressed = false
	if pressed && !s.dragging && s.tap != nil {
		s.tap()
	}
}

// setIconRoom implements iconRoom: a cut-off line shortens its text before the copy icon.
func (s *selLine) setIconRoom(w float32) {
	if s.room != w {
		s.room = w
		s.Refresh()
	}
}

// textWidth is the width the line's text is shortened to: the line's, less the icon's room.
func (s *selLine) textWidth() float32 { return lessRoom(s.Size().Width, s.room) }

var _ desktop.Hoverable = (*selLine)(nil)

func newSelLine(line *textLine, hook *copyHook, copied func(string)) *selLine {
	s := &selLine{line: line, hook: hook}
	hook.bind(s)
	s.ExtendBaseWidget(s)
	s.self = s
	s.getText = func() string { return line.text }
	s.offsetAt = s.offset
	s.repaint = s.Refresh
	s.copied = copied
	return s
}

// shown is the text the line draws now, and whether it is cut off.
func (s *selLine) shown() (string, bool) {
	l := s.line
	if l.truncate {
		size := s.Theme().Size(l.size)
		if cut := ellipsize(l.text, s.textWidth(), size, l.style); cut != l.text {
			return cut, true
		}
	}
	return l.text, false
}

// offset maps a point to the nearest rune offset of the full text. An ellipsis stands for the text
// it hides: a point right of it is the end of the text.
func (s *selLine) offset(p fyne.Position) int {
	shown, cut := s.shown()
	runes := []rune(shown)
	size, style := s.Theme().Size(s.line.size), s.line.style
	prev := float32(0)
	i := 0
	for ; i < len(runes); i++ {
		w := fyne.MeasureText(string(runes[:i+1]), size, style).Width
		if p.X < (prev+w)/2 {
			break
		}
		prev = w
	}
	if cut && i >= len(runes)-1 { // at or after the ellipsis
		if i == len(runes) {
			return len([]rune(s.line.text))
		}
		return len(runes) - 1
	}
	return i
}

// CreateRenderer implements fyne.Widget.
func (s *selLine) CreateRenderer() fyne.WidgetRenderer {
	hl := canvas.NewRectangle(color.Transparent)
	return &selLineRenderer{s: s, hl: hl}
}

type selLineRenderer struct {
	s  *selLine
	hl *canvas.Rectangle
}

func (r *selLineRenderer) MinSize() fyne.Size { return r.s.line.MinSize() }

func (r *selLineRenderer) Layout(size fyne.Size) {
	r.s.line.Move(fyne.Position{})
	r.s.line.Resize(fyne.NewSize(lessRoom(size.Width, r.s.room), size.Height))
	r.placeHighlight(size)
}

func (r *selLineRenderer) placeHighlight(size fyne.Size) {
	s := r.s
	lo, hi := s.bounds()
	if lo == hi {
		r.hl.Hide()
		return
	}
	shown, cut := s.shown()
	runes := []rune(shown)
	toShown := func(o int) int {
		if cut && o >= len(runes)-1 {
			if o >= len([]rune(s.line.text)) {
				return len(runes)
			}
			return len(runes) - 1
		}
		return min(o, len(runes))
	}
	th := s.Theme()
	text, style := th.Size(s.line.size), s.line.style
	x1 := fyne.MeasureText(string(runes[:toShown(lo)]), text, style).Width
	x2 := fyne.MeasureText(string(runes[:toShown(hi)]), text, style).Width
	h := fyne.MeasureText("Mg", text, style).Height
	r.hl.FillColor = themeColor(th, theme.ColorNameSelection)
	r.hl.Move(fyne.NewPos(x1, (size.Height-h)/2))
	r.hl.Resize(fyne.NewSize(x2-x1, h))
	r.hl.Show()
}

func (r *selLineRenderer) Refresh() {
	r.s.line.Refresh()
	r.Layout(r.s.Size())
	r.hl.Refresh()
}

func (r *selLineRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.hl, r.s.line}
}

func (r *selLineRenderer) Destroy() {}

// MouseIn implements desktop.Hoverable: the copy icon shows by the text.
func (s *selLine) MouseIn(*desktop.MouseEvent) {
	if s.hook != nil {
		natural := fyne.MeasureText(s.line.text, s.Theme().Size(s.line.size), s.line.style).Width
		s.hook.enter(s, natural)
	}
}

// MouseMoved implements desktop.Hoverable.
func (s *selLine) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (s *selLine) MouseOut() {
	if s.hook != nil {
		s.hook.leave(s)
	}
}

// secretText is a revealed Secret value: monospace text wrapped at any character, so that a long
// token or a certificate can be read and copied whole, and selectable. Its selection is copied
// through the shell, which clears the clipboard again later (copy.go).
type secretText struct {
	widget.BaseWidget
	textSelection
	text string
	hook *copyHook
	cols int // the columns the last layout wrapped at
}

var _ desktop.Hoverable = (*secretText)(nil)

// secretMinCols is the width a revealed value asks for, in characters; it wraps in what it is given.
const secretMinCols = 16

func newSecretText(text string, hook *copyHook, copied func(string)) *secretText {
	t := &secretText{text: text, hook: hook, cols: 48}
	hook.bind(t)
	t.ExtendBaseWidget(t)
	t.self = t
	t.getText = func() string { return t.text }
	t.offsetAt = t.offset
	t.repaint = t.Refresh
	t.copied = copied
	return t
}

// setText shows another text, its selection cleared. The kubeconfig's path on Cluster Settings ›
// General is such a text too (the owner's WF28, D-87): whole, wrapped, selectable.
func (t *secretText) setText(s string) {
	if s == t.text {
		return
	}
	t.text = s
	t.anchor, t.caret = 0, 0
	t.Refresh()
}

// vline is one wrapped line: the rune offsets of its text in the full text.
type vline struct{ start, end int }

// wrap breaks the text into lines of at most cols characters, at newlines too.
func (t *secretText) wrap(cols int) []vline {
	var out []vline
	pos := 0
	r := []rune(t.text)
	for {
		end := pos
		for end < len(r) && r[end] != '\n' {
			end++
		}
		for start := pos; ; start += cols {
			stop := min(start+cols, end)
			out = append(out, vline{start, stop})
			if stop >= end {
				break
			}
		}
		if end >= len(r) {
			return out
		}
		pos = end + 1
	}
}

// metrics returns the width of one character and the height of one line.
func (t *secretText) metrics() (charW, lineH float32) {
	size := t.Theme().Size(theme.SizeNameText)
	m := fyne.MeasureText("M", size, fyne.TextStyle{Monospace: true})
	return m.Width, m.Height
}

func (t *secretText) colsFor(width float32) int {
	charW, _ := t.metrics()
	if charW <= 0 {
		return secretMinCols
	}
	return max(secretMinCols, int(math.Floor(float64(width/charW))))
}

func (t *secretText) offset(p fyne.Position) int {
	charW, lineH := t.metrics()
	lines := t.wrap(t.cols)
	row := max(0, min(int(p.Y/lineH), len(lines)-1))
	l := lines[row]
	col := max(0, min(int(math.Round(float64(p.X/charW))), l.end-l.start))
	return l.start + col
}

// CreateRenderer implements fyne.Widget.
func (t *secretText) CreateRenderer() fyne.WidgetRenderer { return &secretTextRenderer{t: t} }

type secretTextRenderer struct {
	t     *secretText
	texts []*canvas.Text
	hls   []*canvas.Rectangle
	objs  []fyne.CanvasObject
}

func (r *secretTextRenderer) MinSize() fyne.Size {
	charW, lineH := r.t.metrics()
	return fyne.NewSize(charW*secretMinCols, lineH*float32(len(r.t.wrap(r.t.cols))))
}

func (r *secretTextRenderer) Layout(size fyne.Size) {
	t := r.t
	t.cols = t.colsFor(size.Width)
	lines := t.wrap(t.cols)
	charW, lineH := t.metrics()
	th := t.Theme()
	runes := []rune(t.text)
	lo, hi := t.bounds()
	for len(r.texts) < len(lines) {
		r.texts = append(r.texts, canvas.NewText("", color.Transparent))
		r.hls = append(r.hls, canvas.NewRectangle(color.Transparent))
	}
	r.objs = r.objs[:0]
	for i, l := range lines {
		txt, hl := r.texts[i], r.hls[i]
		txt.Text = string(runes[l.start:l.end])
		txt.Color = themeColor(th, theme.ColorNameForeground)
		txt.TextSize = th.Size(theme.SizeNameText)
		txt.TextStyle = fyne.TextStyle{Monospace: true}
		txt.Move(fyne.NewPos(0, float32(i)*lineH))
		txt.Resize(fyne.NewSize(size.Width, lineH))
		a, b := max(lo, l.start), min(hi, l.end)
		if a < b {
			hl.FillColor = themeColor(th, theme.ColorNameSelection)
			hl.Move(fyne.NewPos(float32(a-l.start)*charW, float32(i)*lineH))
			hl.Resize(fyne.NewSize(float32(b-a)*charW, lineH))
			hl.Show()
		} else {
			hl.Hide()
		}
		r.objs = append(r.objs, hl, txt)
	}
}

func (r *secretTextRenderer) Refresh() {
	r.Layout(r.t.Size())
	for _, o := range r.objs {
		o.Refresh()
	}
}

func (r *secretTextRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *secretTextRenderer) Destroy()                     {}

// MouseIn implements desktop.Hoverable.
func (t *secretText) MouseIn(*desktop.MouseEvent) {
	if t.hook != nil {
		charW, _ := t.metrics()
		longest := 0
		for _, l := range t.wrap(t.cols) {
			longest = max(longest, l.end-l.start)
		}
		t.hook.enter(t, charW*float32(longest))
	}
}

// MouseMoved implements desktop.Hoverable.
func (t *secretText) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (t *secretText) MouseOut() {
	if t.hook != nil {
		t.hook.leave(t)
	}
}

// lines is for tests: the text's lines as the wrapper breaks them.
func (t *secretText) lines() []string {
	r := []rune(t.text)
	wrapped := t.wrap(t.cols)
	out := make([]string, 0, len(wrapped))
	for _, l := range wrapped {
		out = append(out, string(r[l.start:l.end]))
	}
	return out
}
