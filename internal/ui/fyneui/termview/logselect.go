package termview

import (
	"image/color"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Selecting and copying text in a log view (the owner's WF36, D-99). A drag selects across rows,
// scrolling the view while the pointer is beyond its top or bottom; ⇧-click extends the selection
// from where it began; a double click selects a word; ⌘A (Ctrl+A) selects every line the view
// shows (its sources and its filter applied); ⌘C (Ctrl+C) copies the selection as plain text,
// one line per line, without the line ending after the last; a right-click offers Copy, Select All
// and Copy All Lines Shown. A selection made with the pointer stops following, as a jump does.
//
// The list recycles its rows as it scrolls, so the selection is the view's, not a row's: a place
// in a line is the line's absolute number (as displayRow has it) and a byte offset into the
// line's shown text. It survives scrolling, new lines, wrapping at another width, a search and
// a filter (copying takes the lines the view shows within it); lines the scrollback drops take
// their part of it with them; a view that shows the lines' text differently (names, timestamps,
// the time format) drops it. A surface over the list (logSurface) takes the pointer and the keys,
// so that a drag that leaves the list or makes it scroll still knows where it is. Selecting is
// the window's alone, as in the detail drawer (D-25): no command, no engine state, no audit line.

// Bounds of selecting.
const (
	autoScrollEvery   = 50 * time.Millisecond // a step of scrolling while a drag is beyond an edge
	autoScrollMaxRows = 8                     // rows per step at most, the farther the faster
)

// logPos is a place in the lines: before the byte off of line's shown text.
type logPos struct {
	line int64
	off  int
}

func (p logPos) before(q logPos) bool { return p.line < q.line || p.line == q.line && p.off < q.off }

// logSel is a log view's selection: from anchor (where it began) to caret (where it ends now).
type logSel struct{ anchor, caret logPos }

func (s logSel) has() bool { return s.anchor != s.caret }

// bounds returns the selection's start and end, start first.
func (s logSel) bounds() (lo, hi logPos) {
	if s.caret.before(s.anchor) {
		return s.caret, s.anchor
	}
	return s.anchor, s.caret
}

// rowSel is the part of one row that is selected, in byte offsets into the text the row renders
// (its runs one after another).
type rowSel struct {
	from, to int
	eol      bool // the selection goes on past the line's end: the line break is selected too
	cont     bool // the selection goes on past the row: it fills the gap to the next row too
}

func (s rowSel) on() bool { return s.from < s.to || s.eol }

// SetOnCopy sets what a copy does with the text (the shell puts it on the clipboard its way);
// nil puts it on the app's clipboard. Main goroutine only.
func (l *Logs) SetOnCopy(fn func(string)) { l.onCopy = fn }

// SelectedText returns the selected text: the lines the view shows within the selection, as it
// shows them, joined by "\n"; "" when nothing is selected. Main goroutine only.
func (l *Logs) SelectedText() string {
	if !l.sel.has() || len(l.rows) == 0 {
		return ""
	}
	lo, hi := l.sel.bounds()
	var b strings.Builder
	n := 0
	for i := l.firstRowOf(lo.line); i < len(l.rows) && l.rows[i].line <= hi.line; i++ {
		line := l.rows[i].line
		if i > 0 && l.rows[i-1].line == line {
			continue // a wrapped line's other rows
		}
		text, _ := l.view.shown(l.lines.at(int(line - l.lines.evicted)))
		from, to := 0, len(text)
		if line == lo.line {
			from = min(lo.off, to)
		}
		if line == hi.line {
			to = min(hi.off, to)
		}
		if n > 0 {
			b.WriteByte('\n')
		}
		n++
		b.WriteString(text[min(from, to):to])
	}
	return b.String()
}

// SelectAll selects every line the view shows. Main goroutine only.
func (l *Logs) SelectAll() {
	if len(l.rows) == 0 {
		return
	}
	first, last := l.rows[0].line, l.rows[len(l.rows)-1].line
	text, _ := l.view.shown(l.lines.at(int(last - l.lines.evicted)))
	l.setSelection(logPos{line: first}, logPos{line: last, off: len(text)})
}

// ClearSelection drops the selection. Main goroutine only.
func (l *Logs) ClearSelection() {
	if l.sel.has() {
		l.setSelection(l.sel.caret, l.sel.caret)
	}
}

// copyText hands text to the caller's copy, or to the clipboard.
func (l *Logs) copyText(text string) {
	if text == "" {
		return
	}
	if l.onCopy != nil {
		l.onCopy(text)
		return
	}
	if a := fyne.CurrentApp(); a != nil {
		a.Clipboard().SetContent(text)
	}
}

// copyAllShown copies every line the view shows, as a download saves them.
func (l *Logs) copyAllShown() { l.copyText(strings.Join(l.DownloadLines(), "\n")) }

// setSelection changes the selection and repaints the rows on screen.
func (l *Logs) setSelection(anchor, caret logPos) {
	l.sel = logSel{anchor: anchor, caret: caret}
	l.repaintSelection()
}

// setCaret moves the selection's end, keeping where it began.
func (l *Logs) setCaret(p logPos) {
	if p == l.sel.caret {
		return
	}
	l.setSelection(l.sel.anchor, p)
}

// repaintSelection gives the rows on screen their part of the selection. Only the selection's
// rectangles change: the list keeps its rows, their texts and its place.
func (l *Logs) repaintSelection() {
	first, last := l.onScreen()
	for id := first; id <= last; id++ {
		if r := l.shownRows[id]; r != nil && r.item == id {
			if sel := l.itemSel(id); sel != r.sel {
				r.setSel(sel)
			}
		}
	}
}

// clampSelection follows the scrollback: the part of the selection in dropped lines goes with
// them, and a selection wholly in them goes.
func (l *Logs) clampSelection() {
	first := logPos{line: l.lines.evicted}
	lo, hi := l.sel.bounds()
	switch {
	case !lo.before(first):
	case !first.before(hi):
		l.sel = logSel{}
	case l.sel.anchor == lo:
		l.sel.anchor = first
	default:
		l.sel.caret = first
	}
}

// shownChanged reports whether a view shows the lines' text differently from another: the
// selection's offsets then point elsewhere.
func shownChanged(a, b LogView) bool {
	return a.Prefixed != b.Prefixed || a.Names != b.Names || a.Stamped != b.Stamped ||
		a.Timestamps != b.Timestamps || a.Time != b.Time
}

// firstRowOf returns the index of the first row of a line at or after line.
func (l *Logs) firstRowOf(line int64) int {
	return sort.Search(len(l.rows), func(i int) bool { return l.rows[i].line >= line })
}

// rowSpan returns row i's line as the view shows it, and the part of it the row renders:
// [from, to), cut when a line too long for one row is shortened (no wrap).
func (l *Logs) rowSpan(i int) (text string, sp shownSpans, from, to int, cut bool) {
	r := l.rows[i]
	text, sp = l.view.shown(l.lines.at(int(r.line - l.lines.evicted)))
	from, to = int(r.from), int(r.to)
	if to < 0 || to > len(text) {
		to = len(text)
	}
	from = min(from, to)
	if !l.view.Wrap && to-from > logMaxShownBytes {
		to = from + len(shownText(text[from:to])) - len(truncatedMark)
		cut = true
	}
	return text, sp, from, to, cut
}

// rowEnd is where row i ends in its line of n bytes: the next row's start, or the line's end.
func (l *Logs) rowEnd(i, n int) int {
	if to := int(l.rows[i].to); to >= 0 && to < n {
		return to
	}
	return n
}

// itemSel is list item id's part of the selection.
func (l *Logs) itemSel(id widget.ListItemID) rowSel {
	i := id - l.headRows()
	if !l.sel.has() || i < 0 || i >= len(l.rows) {
		return rowSel{}
	}
	lo, hi := l.sel.bounds()
	line := l.rows[i].line
	if line < lo.line || line > hi.line {
		return rowSel{}
	}
	text, _, from, to, cut := l.rowSpan(i)
	end := l.rowEnd(i, len(text))
	a, b := from, end
	if line == lo.line {
		a = max(a, lo.off)
	}
	if line == hi.line {
		b = min(b, hi.off)
	}
	eol := end == len(text) && line < hi.line && (line > lo.line || lo.off <= len(text))
	if a > end || b < from || a > b {
		return rowSel{eol: eol && a <= end}
	}
	sa, sb := a-from, b-from
	if cut { // the row renders [from, to) and the mark: past it is the mark
		if b > to {
			sb = to - from + len(truncatedMark)
		}
		if a > to {
			sa = to - from
		}
	}
	return rowSel{from: sa, to: sb, eol: eol, cont: line < hi.line || hi.off > end}
}

// posAt returns the place in the lines nearest a point of the surface: a point on the head row or
// above the rows is the first row's start, one below them the last row's end; a point above or
// below the view is in the rows scrolled out there.
func (l *Logs) posAt(p fyne.Position) (logPos, bool) {
	if len(l.rows) == 0 {
		return logPos{}, false
	}
	item := int(math.Floor(float64((p.Y + l.list.GetScrollOffset()) / l.stride())))
	i := item - l.headRows()
	switch {
	case i < 0:
		return logPos{line: l.rows[0].line, off: int(l.rows[0].from)}, true
	case i >= len(l.rows):
		i = len(l.rows) - 1
		text, _, _, _, _ := l.rowSpan(i)
		return logPos{line: l.rows[i].line, off: l.rowEnd(i, len(text))}, true
	}
	return logPos{line: l.rows[i].line, off: l.offsetIn(i, p.X)}, true
}

// onHead reports whether a point of the surface is on the head row.
func (l *Logs) onHead(p fyne.Position) bool {
	return l.headRows() == 1 && p.Y >= 0 && p.Y+l.list.GetScrollOffset() < l.stride()
}

// offsetIn returns the byte offset in row i's line nearest x: the boundary between two characters
// closest to it; right of the row's text, the row's end (the line's end past a cut line's mark).
func (l *Logs) offsetIn(i int, x float32) int {
	text, _, from, to, cut := l.rowSpan(i)
	if x <= 0 {
		return from
	}
	part := text[from:to]
	th := l.list.Theme()
	size, style := th.Size(theme.SizeNameText), fyne.TextStyle{Monospace: true}
	width := func(n int) float32 { return fyne.MeasureText(part[:n], size, style).Width }
	if x >= width(len(part)) {
		if cut {
			return len(text)
		}
		return l.rowEnd(i, len(text))
	}
	if isPlainText([]byte(part)) { // monospace ASCII: a column per character
		return from + min(int(x/charWidth(th)+0.5), len(part))
	}
	starts := make([]int, 0, len(part)+1)
	for k := range part {
		starts = append(starts, k)
	}
	starts = append(starts, len(part))
	k := sort.Search(len(starts), func(k int) bool { return width(starts[k]) > x }) // x lies before starts[k]
	if k == 0 {
		return from
	}
	left, right := width(starts[k-1]), width(starts[k])
	if x-left < right-x {
		return from + starts[k-1]
	}
	return from + starts[k]
}

// wordAt returns the run of characters that are no white space at off in text, or the one that
// ends there; empty when there is none.
func wordAt(text string, off int) (lo, hi int) {
	off = min(max(off, 0), len(text))
	isWord := func(r rune) bool { return !unicode.IsSpace(r) }
	r, _ := utf8.DecodeRuneInString(text[off:])
	if off == len(text) || !isWord(r) {
		if p, size := utf8.DecodeLastRuneInString(text[:off]); off > 0 && isWord(p) {
			off -= size
		} else {
			return off, off
		}
	}
	lo, hi = off, off
	for lo > 0 {
		p, size := utf8.DecodeLastRuneInString(text[:lo])
		if !isWord(p) {
			break
		}
		lo -= size
	}
	for hi < len(text) {
		n, size := utf8.DecodeRuneInString(text[hi:])
		if !isWord(n) {
			break
		}
		hi += size
	}
	return lo, hi
}

// selectWord selects the word at p.
func (l *Logs) selectWord(p logPos) {
	i := int(p.line - l.lines.evicted)
	if i < 0 || i >= l.lines.len() {
		return
	}
	text, _ := l.view.shown(l.lines.at(i))
	lo, hi := wordAt(text, p.off)
	l.setSelection(logPos{line: p.line, off: lo}, logPos{line: p.line, off: hi})
}

// post runs fn on the main goroutine without waiting.
func (l *Logs) post(fn func()) {
	if l.do != nil {
		l.do(fn)
		return
	}
	fyne.Do(fn)
}

// logSurface lies over the list, but for its scroll bar, and turns the pointer and the keys into
// the view's selection. It is no Hoverable, so the rows below still show the pointer's row.
type logSurface struct {
	widget.BaseWidget
	l        *Logs
	pressed  bool              // a primary press set the anchor
	dragging bool              // a drag selects
	last     fyne.Position     // the pointer's place in the drag
	every    time.Duration     // the auto-scroll's step; 0 scrolls only when a test steps it
	autoStop chan struct{}     // closed to stop the auto-scroll's ticker; nil while it does not run
	menu     *widget.PopUpMenu // the context menu shown last, and its items
	items    *fyne.Menu
}

var (
	_ desktop.Mouseable      = (*logSurface)(nil)
	_ desktop.Cursorable     = (*logSurface)(nil)
	_ fyne.Draggable         = (*logSurface)(nil)
	_ fyne.Tappable          = (*logSurface)(nil)
	_ fyne.DoubleTappable    = (*logSurface)(nil)
	_ fyne.SecondaryTappable = (*logSurface)(nil)
	_ fyne.Focusable         = (*logSurface)(nil)
	_ fyne.Shortcutable      = (*logSurface)(nil)
)

func newLogSurface(l *Logs) *logSurface {
	s := &logSurface{l: l, every: autoScrollEvery}
	s.ExtendBaseWidget(s)
	return s
}

// CreateRenderer implements fyne.Widget: the surface draws nothing.
func (s *logSurface) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

// Cursor implements desktop.Cursorable.
func (s *logSurface) Cursor() desktop.Cursor { return desktop.TextCursor }

func (s *logSurface) focus() {
	if c := fyne.CurrentApp().Driver().CanvasForObject(s); c != nil {
		c.Focus(s)
	}
}

// MouseDown implements desktop.Mouseable: a press takes the keyboard and begins a selection
// there, or with ⇧ extends the one there is to there. A press on the head row leaves it to Tapped.
func (s *logSurface) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button != desktop.MouseButtonPrimary {
		return
	}
	l := s.l
	s.focus()
	l.list.UnselectAll() // a jump's line is no longer marked
	s.pressed = false
	if l.onHead(ev.Position) {
		return
	}
	p, ok := l.posAt(ev.Position)
	if !ok {
		return
	}
	s.pressed = true
	if kept := l.sel.anchor.line - l.lines.evicted; ev.Modifier&fyne.KeyModifierShift != 0 && kept >= 0 && kept < int64(l.lines.len()) {
		l.SetFollow(false)
		l.setCaret(p)
		return
	}
	l.setSelection(p, p)
}

// MouseUp implements desktop.Mouseable.
func (s *logSurface) MouseUp(*desktop.MouseEvent) {}

// Dragged implements fyne.Draggable: the selection runs from where the drag began to the
// pointer; beyond the view's top or bottom the view scrolls.
func (s *logSurface) Dragged(ev *fyne.DragEvent) {
	l := s.l
	if !s.dragging {
		s.dragging = true
		l.SetFollow(false) // the lines stay where the drag finds them
		if !s.pressed {    // the press was on the head row
			if p, ok := l.posAt(ev.Position.Subtract(ev.Dragged)); ok {
				l.setSelection(p, p)
				s.pressed = true
			}
		}
	}
	s.last = ev.Position
	if p, ok := l.posAt(ev.Position); ok && s.pressed {
		l.setCaret(p)
	}
	if s.beyond() != 0 && s.autoStop == nil && s.every > 0 {
		s.startAutoScroll()
	}
}

// DragEnd implements fyne.Draggable.
func (s *logSurface) DragEnd() {
	s.dragging = false
	s.stopAutoScroll()
}

// beyond is how far the drag's pointer is above the view's top (negative) or below its bottom.
func (s *logSurface) beyond() float32 {
	switch y, h := s.last.Y, s.Size().Height; {
	case y < 0:
		return y
	case y > h:
		return y - h
	}
	return 0
}

// autoScroll is a step of scrolling while the drag's pointer is beyond an edge: a row, or more the
// farther it is, and the selection follows to the pointer.
func (s *logSurface) autoScroll() {
	d := s.beyond()
	if !s.dragging || d == 0 {
		s.stopAutoScroll()
		return
	}
	l := s.l
	stride := l.stride()
	rows := float32(min(int(math.Abs(float64(d))/float64(stride))+1, autoScrollMaxRows))
	if d < 0 {
		rows = -rows
	}
	l.list.ScrollToOffset(max(l.list.GetScrollOffset()+rows*stride, 0))
	if p, ok := l.posAt(s.last); ok && s.pressed {
		l.setCaret(p)
	}
}

func (s *logSurface) startAutoScroll() {
	stop, done, every := make(chan struct{}), s.l.stop, s.every
	s.autoStop = stop
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-done:
				return
			case <-t.C:
				s.l.post(func() {
					if s.autoStop == stop {
						s.autoScroll()
					}
				})
			}
		}
	}()
}

func (s *logSurface) stopAutoScroll() {
	if s.autoStop != nil {
		close(s.autoStop)
		s.autoStop = nil
	}
}

// Tapped implements fyne.Tappable: the head row acts.
func (s *logSurface) Tapped(ev *fyne.PointEvent) {
	if s.l.onHead(ev.Position) {
		s.l.list.Select(0) // the list's OnSelected runs the head's action (logsolder.go)
	}
}

// DoubleTapped implements fyne.DoubleTappable: it selects the word under the pointer.
func (s *logSurface) DoubleTapped(ev *fyne.PointEvent) {
	l := s.l
	if l.onHead(ev.Position) {
		return
	}
	if p, ok := l.posAt(ev.Position); ok {
		l.SetFollow(false)
		l.selectWord(p)
	}
}

// TappedSecondary implements fyne.SecondaryTappable: Copy, Select All and Copy All Lines Shown.
func (s *logSurface) TappedSecondary(ev *fyne.PointEvent) {
	l := s.l
	c := fyne.CurrentApp().Driver().CanvasForObject(s)
	if c == nil {
		return
	}
	selected := l.SelectedText() // what the menu's Copy copies, whatever happens meanwhile
	copyItem := fyne.NewMenuItem("Copy", func() { l.copyText(selected) })
	copyItem.Shortcut = &fyne.ShortcutCopy{}
	copyItem.Disabled = selected == ""
	all := fyne.NewMenuItem("Select All", l.SelectAll)
	all.Shortcut = &fyne.ShortcutSelectAll{}
	all.Disabled = len(l.rows) == 0
	shown := fyne.NewMenuItem("Copy All Lines Shown", l.copyAllShown)
	shown.Disabled = len(l.rows) == 0
	s.items = fyne.NewMenu("", copyItem, all, fyne.NewMenuItemSeparator(), shown)
	s.menu = widget.NewPopUpMenu(s.items, c)
	menu := s.menu
	menu.OnDismiss = func() {
		menu.Hide()
		s.focus() // the keys come back to the view: ⌘C after the menu copies
	}
	menu.ShowAtPosition(ev.AbsolutePosition)
}

// FocusGained implements fyne.Focusable.
func (s *logSurface) FocusGained() {}

// FocusLost implements fyne.Focusable: the selection stays (a context menu or the toolbar take
// the focus); a press in the view begins a new one.
func (s *logSurface) FocusLost() {}

// TypedRune implements fyne.Focusable: "/" focuses the tab's search; other runes go on to the
// window.
func (s *logSurface) TypedRune(r rune) {
	if r == '/' && s.l.list.onSlash != nil {
		s.l.list.onSlash()
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(s); c != nil && c.OnTypedRune() != nil {
		c.OnTypedRune()(r)
	}
}

// TypedKey implements fyne.Focusable: Escape drops a selection; other keys go on to the window
// (F1–F12 for the hotbar, Escape without a selection).
func (s *logSurface) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape && s.l.sel.has() {
		s.l.ClearSelection()
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(s); c != nil && c.OnTypedKey() != nil {
		c.OnTypedKey()(ev)
	}
}

// TypedShortcut implements fyne.Shortcutable: copy and select all; other shortcuts go on to the
// window's, which a focused widget that takes shortcuts would otherwise keep.
func (s *logSurface) TypedShortcut(sc fyne.Shortcut) {
	switch sc.(type) {
	case *fyne.ShortcutCopy:
		s.l.copyText(s.l.SelectedText())
		return
	case *fyne.ShortcutSelectAll:
		s.l.SelectAll()
		return
	}
	if c, ok := fyne.CurrentApp().Driver().CanvasForObject(s).(interface{ TypedShortcut(fyne.Shortcut) }); ok {
		c.TypedShortcut(sc)
	}
}

// surfaceLayout lays the list out over the whole area and the surface over it but for the list's
// scroll bar, which keeps its drag.
type surfaceLayout struct{}

func (surfaceLayout) MinSize(objs []fyne.CanvasObject) fyne.Size { return objs[0].MinSize() }

func (surfaceLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	objs[0].Move(fyne.Position{})
	objs[0].Resize(size)
	bar := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameScrollBar)
	objs[1].Move(fyne.Position{})
	objs[1].Resize(fyne.NewSize(max(size.Width-bar, 0), size.Height))
}
