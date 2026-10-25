package fyneui

import (
	"image/color"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// selParagraph makes a paragraph selectable as the drawer's texts are (selection.go, D-25): a drag
// selects across its wrapped lines, a double click selects a word, ⌘C (Ctrl+C) copies. A toast's
// text and a notification's in the bell's panel are such paragraphs (the owner's WF41, D-99). The
// paragraph draws the text; selParagraph adds the selection beneath and the mouse handling, so
// that tests and layouts still see a paragraph.
//
// The selection is in the text as the paragraph shows it: its wrapped lines joined by a space
// where a line wraps and by a line break where the text has one (a run of spaces shows as one, as
// the paragraph draws it). A copy icon beside it copies the text as it came.
type selParagraph struct {
	widget.BaseWidget
	textSelection
	p        *paragraph
	lines    []pline             // the wrapped lines and where each starts in the shown text
	shown    string              // the lines joined
	selColor fyne.ThemeColorName // the selection's colour on the paragraph's surface
	changed  func()              // the selection changed, or a drag began or ended
}

// pline is a wrapped line: its text and the rune offset of its start in the shown text.
type pline struct {
	text  string
	start int
}

func newSelParagraph(p *paragraph, sel fyne.ThemeColorName, copied func(string)) *selParagraph {
	s := &selParagraph{p: p, selColor: sel}
	s.ExtendBaseWidget(s)
	s.self = s
	s.getText = func() string { return s.shown }
	s.offsetAt = s.offset
	s.repaint = func() {
		s.Refresh()
		if s.changed != nil {
			s.changed()
		}
	}
	s.copied = copied
	s.wrap()
	return s
}

// selecting reports whether the paragraph holds a selection or a drag makes one.
func (s *selParagraph) selecting() bool { return s.dragging || s.anchor != s.caret }

// TypedKey implements fyne.Focusable: Escape drops a selection; other keys go on to the window
// (Escape without a selection closes the bell's panel, F1–F12 are the hotbar's).
func (s *selParagraph) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape && s.anchor != s.caret {
		s.clear()
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(s); c != nil && c.OnTypedKey() != nil {
		c.OnTypedKey()(ev)
	}
}

// DragEnd implements fyne.Draggable.
func (s *selParagraph) DragEnd() {
	if s.endDrag() && s.changed != nil {
		s.changed()
	}
}

// wrap breaks the text as the paragraph does, a paragraph of the text at a time, so that it knows
// which line ends where the text has a line break. The text and its width are the paragraph's for
// good, so it wraps once.
func (s *selParagraph) wrap() {
	p := s.p
	size := fyne.CurrentApp().Settings().Theme().Size(p.size)
	fn := wrapLines
	switch {
	case p.names:
		fn = wrapName
	case p.hard:
		fn = wrapFlow
	}
	s.lines = s.lines[:0]
	var b strings.Builder
	n := 0
	for i, para := range strings.Split(p.text, "\n") {
		if i > 0 {
			b.WriteByte('\n')
			n++
		}
		for j, line := range fn(para, p.width, size, p.style) {
			if j > 0 {
				b.WriteByte(' ')
				n++
			}
			s.lines = append(s.lines, pline{text: line, start: n})
			b.WriteString(line)
			n += len([]rune(line))
		}
	}
	s.shown = b.String()
}

// metrics are the paragraph's text size and line height, as it draws them.
func (s *selParagraph) metrics() (size, lineH float32) {
	th := s.p.Theme()
	size = th.Size(s.p.size)
	return size, fyne.MeasureText("Mg", size, s.p.style).Height + th.Size(theme.SizeNameLineSpacing)
}

// offset maps a point to the nearest rune offset of the shown text.
func (s *selParagraph) offset(pt fyne.Position) int {
	if len(s.lines) == 0 {
		return 0
	}
	size, lineH := s.metrics()
	l := s.lines[max(0, min(int(pt.Y/lineH), len(s.lines)-1))]
	runes := []rune(l.text)
	width := func(k int) float32 { return fyne.MeasureText(string(runes[:k]), size, s.p.style).Width }
	if pt.X <= 0 {
		return l.start
	}
	k := sort.Search(len(runes)+1, func(k int) bool { return width(k) > pt.X }) // pt.X lies before rune k's end
	switch {
	case k > len(runes):
		return l.start + len(runes)
	case k == 0:
		return l.start
	case pt.X-width(k-1) < width(k)-pt.X:
		return l.start + k - 1
	}
	return l.start + k
}

// CreateRenderer implements fyne.Widget.
func (s *selParagraph) CreateRenderer() fyne.WidgetRenderer { return &selParagraphRenderer{s: s} }

type selParagraphRenderer struct {
	s    *selParagraph
	hls  []*canvas.Rectangle
	objs []fyne.CanvasObject // the highlights, then the paragraph
}

func (r *selParagraphRenderer) MinSize() fyne.Size { return r.s.p.MinSize() }

func (r *selParagraphRenderer) Layout(size fyne.Size) {
	s := r.s
	s.p.Move(fyne.Position{})
	s.p.Resize(size)
	textSize, lineH := s.metrics()
	th := s.Theme()
	lo, hi := s.bounds()
	if len(r.hls) < len(s.lines) || r.objs == nil {
		for len(r.hls) < len(s.lines) {
			r.hls = append(r.hls, canvas.NewRectangle(color.Transparent))
		}
		r.objs = r.objs[:0]
		for _, hl := range r.hls {
			r.objs = append(r.objs, hl)
		}
		r.objs = append(r.objs, s.p)
	}
	for i, hl := range r.hls {
		if i >= len(s.lines) {
			hl.Hide()
			continue
		}
		l := s.lines[i]
		runes := []rune(l.text)
		end := l.start + len(runes)
		a, b := max(lo, l.start), min(hi, end)
		gap := hi > end && lo <= end && i < len(s.lines)-1 // the space or line break after the line
		if a > b || a == b && !gap {
			hl.Hide()
			continue
		}
		x1 := fyne.MeasureText(string(runes[:a-l.start]), textSize, s.p.style).Width
		x2 := fyne.MeasureText(string(runes[:b-l.start]), textSize, s.p.style).Width
		if gap {
			x2 += fyne.MeasureText(" ", textSize, s.p.style).Width
		}
		hl.FillColor = themeColor(th, s.selColor)
		hl.Move(fyne.NewPos(x1, float32(i)*lineH))
		hl.Resize(fyne.NewSize(x2-x1, lineH))
		hl.Show()
	}
}

func (r *selParagraphRenderer) Refresh() {
	r.Layout(r.s.Size())
	for _, hl := range r.hls {
		hl.Refresh()
	}
	r.s.p.Refresh()
}

func (r *selParagraphRenderer) Objects() []fyne.CanvasObject {
	if r.objs == nil {
		r.Layout(r.s.Size())
	}
	return r.objs
}

func (r *selParagraphRenderer) Destroy() {}
