package yamlview

import (
	"image/color"
	"math"
	"strconv"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

// metrics are the theme-derived sizes of one render.
type metrics struct {
	textSize float32
	// cw is the advance of one monospace column, lh the height of one line.
	cw, lh float64
	pad    float32
	// bar and barSmall are the scroll bar widths under the pointer and at rest.
	bar, barSmall, barRadius float32
	caretW                   float32
	// numW is the width of the line numbers, gutterW the whole gutter.
	numW, gutterW float32
}

// textX is the x of column 0 when not scrolled.
func (m metrics) textX() float64 { return float64(m.gutterW + 2*m.pad) }

// metrics measures the theme's monospace font. Without a running app (no driver to measure with)
// it falls back to the default theme's sizes, so that no call can panic.
func (v *View) metrics() metrics {
	th := v.Theme()
	m := metrics{
		textSize:  th.Size(theme.SizeNameText),
		pad:       th.Size(theme.SizeNamePadding),
		bar:       th.Size(theme.SizeNameScrollBar),
		barSmall:  th.Size(theme.SizeNameScrollBarSmall),
		barRadius: th.Size(theme.SizeNameScrollBarRadius),
		caretW:    max(2*th.Size(theme.SizeNameInputBorder), 2),
	}
	if v.textSize > 0 { // the Preferences' editor font size
		m.textSize = v.textSize
	}
	if app := fyne.CurrentApp(); app != nil && app.Driver() != nil {
		// Ten columns, so that the fractional advance survives; a line's runs are placed at
		// column × advance and must meet where the font would have put them.
		s := fyne.MeasureText("MMMMMMMMMM", m.textSize, fyne.TextStyle{Monospace: true})
		m.cw = float64(s.Width) / 10
		m.lh = math.Ceil(float64(s.Height))
	}
	if m.cw <= 0 || m.lh <= 0 {
		m.cw, m.lh = float64(m.textSize)*0.6, math.Ceil(float64(m.textSize)*1.3)
	}
	if v.hideNumbers { // a thin margin with the separator, no numbers
		m.gutterW = 2 * m.pad
		return m
	}
	digits := max(len(strconv.Itoa(len(v.doc.lines))), 2)
	m.numW = float32(float64(digits) * m.cw)
	m.gutterW = 2*m.pad + m.numW + 2*m.pad
	return m
}

// viewRenderer draws the visible part of the document. It owns pools of canvas objects that it
// reassigns on every build; nothing in it grows with the document.
type viewRenderer struct {
	v *View

	bg, current, caret, gutterBg, gutterSep, vThumb, hThumb *canvas.Rectangle

	sels  []*canvas.Rectangle
	marks []*canvas.Rectangle
	texts []*canvas.Text
	nums  []*canvas.Text

	objects []fyne.CanvasObject

	// Per-build context for emit.
	m       metrics
	pal     palette
	originX float64
}

func newRenderer(v *View) *viewRenderer {
	r := &viewRenderer{v: v}
	for _, rect := range []**canvas.Rectangle{&r.bg, &r.current, &r.caret, &r.gutterBg, &r.gutterSep, &r.vThumb, &r.hThumb} {
		*rect = canvas.NewRectangle(nil)
	}
	r.build(v.Size())
	return r
}

// Destroy implements fyne.WidgetRenderer.
func (r *viewRenderer) Destroy() {}

// Layout implements fyne.WidgetRenderer.
func (r *viewRenderer) Layout(size fyne.Size) {
	v := r.v
	if v.revealPending && size.Width > 0 && size.Height > 0 {
		v.revealPending = false
		v.revealCaret()
	}
	v.clampScroll()
	r.build(size)
}

// MinSize implements fyne.WidgetRenderer: a few lines of a few columns. The view scrolls, so its
// minimum never depends on the document.
func (r *viewRenderer) MinSize() fyne.Size {
	m := r.v.metrics()
	return fyne.NewSize(m.gutterW+float32(12*m.cw), float32(3*m.lh)+2*m.pad)
}

// Objects implements fyne.WidgetRenderer.
func (r *viewRenderer) Objects() []fyne.CanvasObject { return r.objects }

// Refresh implements fyne.WidgetRenderer.
func (r *viewRenderer) Refresh() {
	r.build(r.v.Size())
	canvas.Refresh(r.v)
}

// build places every canvas object for the current scroll position, caret and selection. The cost
// is the visible lines times their visible columns; the document size enters only through the
// tokenizer state scan, which runs once per line ever reached and again only after an edit above.
func (r *viewRenderer) build(size fyne.Size) {
	v := r.v
	m := v.metrics()
	pal := resolvePalette(v.Theme(), themeVariant())
	r.m, r.pal = m, pal
	lines := v.doc.lines

	lh, cw := m.lh, m.cw
	r.originX = m.textX() - v.scrollX
	originY := float64(m.pad) - v.scrollY // y of line 0
	first := max(0, int(math.Floor((v.scrollY-float64(m.pad))/lh)))
	last := min(len(lines), int(math.Ceil((v.scrollY+float64(size.Height))/lh))+1)
	first = min(first, last)
	colStart := max(0, int(math.Floor(v.scrollX/cw)))
	colEnd := colStart + int(math.Ceil(float64(size.Width-m.gutterW)/cw)) + 2

	setRect(r.bg, pal.bg, 0, 0, size.Width, size.Height)
	objects := append(r.objects[:0], r.bg)

	// Current line: a band behind the caret's line while focused.
	if v.focused && v.caret.line >= first && v.caret.line < last {
		y := float32(originY + float64(v.caret.line)*lh)
		setRect(r.current, pal.currentLine, m.gutterW, y, size.Width-m.gutterW, float32(lh))
		objects = append(objects, r.current)
	}

	// Selection: one band per visible line, reaching one column past the end of a line whose
	// line break is selected.
	ns := 0
	if a, b := order(v.anchor, v.caret); a != b {
		for i := max(a.line, first); i <= b.line && i < last; i++ {
			line := lines[i]
			startCol := 0
			if i == a.line {
				startCol = displayCol(line, a.off)
			}
			endCol := 0
			if i == b.line {
				endCol = displayCol(line, b.off)
			} else {
				endCol = displayCol(line, len(line)) + 1
			}
			x0 := max(r.originX+float64(startCol)*cw, float64(m.gutterW))
			x1 := min(r.originX+float64(endCol)*cw, float64(size.Width))
			if x1 <= x0 {
				continue
			}
			if ns == len(r.sels) {
				r.sels = append(r.sels, canvas.NewRectangle(nil))
			}
			setRect(r.sels[ns], pal.selection, float32(x0), float32(originY+float64(i)*lh), float32(x1-x0), float32(lh))
			objects = append(objects, r.sels[ns])
			ns++
		}
	}

	// Find marks: every match on a visible line, over the selection (the current match is the
	// selection), under the text.
	nm := 0
	for i, mk := range v.marks {
		if mk.Line < first || mk.Line >= last || mk.Line >= len(lines) {
			continue
		}
		line := lines[mk.Line]
		x0 := max(r.originX+float64(displayCol(line, v.posOf(mk.Line, mk.Start).off))*cw, float64(m.gutterW))
		x1 := min(r.originX+float64(displayCol(line, v.posOf(mk.Line, mk.End).off))*cw, float64(size.Width))
		if x1 <= x0 {
			continue
		}
		if nm == len(r.marks) {
			r.marks = append(r.marks, canvas.NewRectangle(nil))
		}
		mr := r.marks[nm]
		fill, stroke := pal.findMatch, color.Color(color.Transparent)
		if i == v.markCur {
			fill, stroke = pal.findCurrent, pal.findStroke
		}
		setRect(mr, fill, float32(x0), float32(originY+float64(mk.Line)*lh), float32(x1-x0), float32(lh))
		mr.StrokeColor, mr.StrokeWidth = stroke, 0
		if i == v.markCur {
			mr.StrokeWidth = 1
		}
		objects = append(objects, mr)
		nm++
	}

	// Text runs.
	nt := 0
	for i := first; i < last; i++ {
		nt = r.emitLine(nt, lines[i], v.lineTokens(i), colStart, colEnd, originY+float64(i)*lh)
	}
	for _, t := range r.texts[:nt] {
		objects = append(objects, t)
	}
	v.pruneCache(first, last)

	// Caret.
	if v.focused && v.caret.line >= first && v.caret.line < last {
		x := r.originX + float64(displayCol(lines[v.caret.line], v.caret.off))*cw
		if x >= float64(m.gutterW)-1 && x < float64(size.Width) {
			setRect(r.caret, pal.caret, float32(x)-m.caretW/2, float32(originY+float64(v.caret.line)*lh), m.caretW, float32(lh))
			objects = append(objects, r.caret)
		}
	}

	// Gutter, drawn over the text so that text scrolled left disappears beneath it.
	setRect(r.gutterBg, pal.bg, 0, 0, m.gutterW, size.Height)
	setRect(r.gutterSep, pal.separator, m.gutterW-1, 0, 1, size.Height)
	objects = append(objects, r.gutterBg, r.gutterSep)
	nn := 0
	if v.hideNumbers {
		last = first // no numbers to draw
	}
	for i := first; i < last; i++ {
		if nn == len(r.nums) {
			t := canvas.NewText("", nil)
			t.TextStyle = fyne.TextStyle{Monospace: true}
			t.Alignment = fyne.TextAlignTrailing
			r.nums = append(r.nums, t)
		}
		t := r.nums[nn]
		t.Text = strconv.Itoa(i + 1)
		t.Color = pal.gutterFg
		if i == v.caret.line && v.focused {
			t.Color = pal.gutterFgActive
		}
		t.TextSize = m.textSize
		t.Move(fyne.NewPos(2*m.pad, float32(originY+float64(i)*lh)))
		t.Resize(fyne.NewSize(m.numW, float32(lh)))
		objects = append(objects, t)
		nn++
	}

	// Scroll bars, over everything.
	barW := m.barSmall
	if v.barHover || v.drag == dragVBar || v.drag == dragHBar {
		barW = m.bar
	}
	if v.vScrollable(size, m) {
		off, length, _ := v.vThumb(size, m)
		setRect(r.vThumb, pal.scrollBar, size.Width-barW-m.pad/2, off, barW, length)
		r.vThumb.CornerRadius = m.barRadius
		objects = append(objects, r.vThumb)
	}
	if v.hScrollable(size, m) {
		off, length, _ := v.hThumb(size, m)
		setRect(r.hThumb, pal.scrollBar, m.gutterW+off, size.Height-barW-m.pad/2, length, barW)
		r.hThumb.CornerRadius = m.barRadius
		objects = append(objects, r.hThumb)
	}
	r.objects = objects
}

// seg is a run of one token kind on one line, in bytes, starting at column col.
type seg struct {
	start, end, col int
	kind            tokKind
}

// emitLine places the runs of one line that fall into columns [colStart, colEnd) and returns the
// number of text objects in use. Runs of printable ASCII share one canvas.Text per token; every
// other rune gets its own, placed on its column, so that a glyph from a fallback font of another
// width cannot shift the rest of the line off the grid.
func (r *viewRenderer) emitLine(n int, line string, toks []token, colStart, colEnd int, y float64) int {
	cur := seg{start: -1}
	col, ti := 0, 0
	for off := 0; off < len(line) && col < colEnd; {
		for ti < len(toks) && toks[ti].end <= off {
			ti++
		}
		kind := tokPlain
		if ti < len(toks) && toks[ti].start <= off {
			kind = toks[ti].kind
		}
		c := line[off]
		if c >= 0x20 && c < 0x7f {
			if col >= colStart {
				switch {
				case cur.start >= 0 && cur.kind == kind:
					cur.end = off + 1
				case c != ' ':
					n = r.flush(n, line, cur, y)
					cur = seg{start: off, end: off + 1, col: col, kind: kind}
				}
			}
			col++
			off++
			continue
		}
		n = r.flush(n, line, cur, y)
		cur.start = -1
		if c == '\t' {
			col = nextTabStop(col)
			off++
			continue
		}
		ru, size := utf8.DecodeRuneInString(line[off:])
		w := runeWidth(ru)
		if col+w > colStart {
			text := line[off : off+size]
			if ru < 0x20 || ru == 0x7f || (ru == utf8.RuneError && size == 1) {
				text = "�" // control characters and invalid bytes
			}
			n = r.place(n, text, col, kind, y)
		}
		col += w
		off += size
	}
	return r.flush(n, line, cur, y)
}

// flush places the open run, without its trailing spaces.
func (r *viewRenderer) flush(n int, line string, s seg, y float64) int {
	if s.start < 0 {
		return n
	}
	end := trimRightSpace(line, s.start, s.end)
	if end <= s.start {
		return n
	}
	return r.place(n, line[s.start:end], s.col, s.kind, y)
}

// place sets text object n to text at column col.
func (r *viewRenderer) place(n int, text string, col int, kind tokKind, y float64) int {
	if n == len(r.texts) {
		t := canvas.NewText("", nil)
		t.TextStyle = fyne.TextStyle{Monospace: true}
		r.texts = append(r.texts, t)
	}
	t := r.texts[n]
	t.Text = text
	t.Color = r.pal.kinds[kind]
	t.TextSize = r.m.textSize
	t.Move(fyne.NewPos(float32(r.originX+float64(col)*r.m.cw), float32(y)))
	t.Resize(fyne.NewSize(float32(float64(len(text))*r.m.cw), float32(r.m.lh)))
	return n + 1
}

func setRect(rect *canvas.Rectangle, fill color.Color, x, y, w, h float32) {
	rect.FillColor = fill
	rect.Move(fyne.NewPos(x, y))
	rect.Resize(fyne.NewSize(w, h))
}

// themeVariant is the app's light or dark variant, dark when there is no app.
func themeVariant() fyne.ThemeVariant {
	if app := fyne.CurrentApp(); app != nil && app.Settings() != nil {
		return app.Settings().ThemeVariant()
	}
	return theme.VariantDark
}
