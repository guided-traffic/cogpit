package fyneui

import (
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The parts of an alert board's card (alertboard.go): its text with slots (spanText), its 24-hour
// history (historyStrip) and the table of its alerts (tableGrid).

// span is a run of a spanText: text in one colour and style.
type span struct {
	text  string
	color fyne.ThemeColorName
	style fyne.TextStyle
}

// spanText is a wrapped text of runs in their own colours and styles: a card's text, its slots
// muted and in italics. It wraps at the width it is laid out at, so that a masonry's column decides
// its height. It is selectable as the drawer's texts are (selection.go; the owner's QW11): a drag
// selects across its lines, a double click selects a word, ⌘C (Ctrl+C) copies the text as it
// shows, its slots as their names ("‹pod›").
type spanText struct {
	widget.BaseWidget
	textSelection
	spans []span
	size  fyne.ThemeSizeName
	width float32 // the width it last wrapped at

	// The text as it shows, from the last wrap: its lines joined by a space where a line wraps and
	// by a line break where the text has one, and each run's place in it.
	shown  string
	pieces []spanPiece
	rows   []spanRow
	r      *spanTextRenderer
	stops  [][]spanStop // per line, every rune boundary and its x; nil until a selection needs it
}

// spanPiece is a run of a wrapped line: its text, where it starts on the line and in the shown
// text.
type spanPiece struct {
	row   int
	x     float32
	text  string
	style fyne.TextStyle
	start int
}

// spanRow is a wrapped line's start and end in the shown text.
type spanRow struct{ start, end int }

// spanStop is a rune boundary on a line: its offset in the shown text and its x.
type spanStop struct {
	at int
	x  float32
}

func newSpanText(size fyne.ThemeSizeName, copied func(string), spans ...span) *spanText {
	t := &spanText{spans: spans, size: size}
	t.ExtendBaseWidget(t)
	t.self = t
	t.getText = func() string { return t.shown }
	t.offsetAt = t.offset
	t.repaint = func() {
		if t.r != nil {
			t.r.placeHighlights()
			canvas.Refresh(t)
		}
	}
	t.copied = copied
	return t
}

// TypedKey implements fyne.Focusable: Escape drops a selection; other keys go on to the window.
func (t *spanText) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape && t.anchor != t.caret {
		t.clear()
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(t); c != nil && c.OnTypedKey() != nil {
		c.OnTypedKey()(ev)
	}
}

// lineStops returns every rune boundary of line row with its x, in order.
func (t *spanText) lineStops(row int) []spanStop {
	if t.stops == nil {
		size := t.Theme().Size(t.size)
		t.stops = make([][]spanStop, len(t.rows))
		for i, rw := range t.rows {
			t.stops[i] = []spanStop{{at: rw.start}}
		}
		for _, p := range t.pieces {
			runes := []rune(p.text)
			line := t.stops[p.row]
			if last := line[len(line)-1]; last.at < p.start { // the space before the run
				line = append(line, spanStop{at: p.start, x: p.x})
			} else {
				line[len(line)-1].x = p.x
			}
			for k := 1; k <= len(runes); k++ {
				line = append(line, spanStop{at: p.start + k, x: p.x + fyne.MeasureText(string(runes[:k]), size, p.style).Width})
			}
			t.stops[p.row] = line
		}
	}
	if row < 0 || row >= len(t.stops) {
		return nil
	}
	return t.stops[row]
}

// offset maps a point to the nearest rune boundary of the line under it.
func (t *spanText) offset(p fyne.Position) int {
	if len(t.rows) == 0 || t.r == nil {
		return 0
	}
	row := min(max(int(p.Y/t.r.lineHeight()), 0), len(t.rows)-1)
	best, dist := t.rows[row].start, float32(math.MaxFloat32)
	for _, s := range t.lineStops(row) {
		if d := float32(math.Abs(float64(s.x - p.X))); d < dist {
			best, dist = s.at, d
		}
	}
	return best
}

// xAt is the x of the boundary at offset o on line row.
func (t *spanText) xAt(row, o int) float32 {
	var x float32
	for _, s := range t.lineStops(row) {
		if s.at > o {
			break
		}
		x = s.x
	}
	return x
}

// CreateRenderer implements fyne.Widget.
func (t *spanText) CreateRenderer() fyne.WidgetRenderer {
	r := &spanTextRenderer{t: t}
	t.r = r
	r.wrap(t.width)
	return r
}

type spanTextRenderer struct {
	t     *spanText
	texts []*canvas.Text
	at    []fyne.Position
	hls   []*canvas.Rectangle // the selection's background, a line each
	rows  int
}

func (r *spanTextRenderer) lineHeight() float32 {
	th := r.t.Theme()
	return fyne.MeasureText("Mg", th.Size(r.t.size), fyne.TextStyle{}).Height + th.Size(theme.SizeNameLineSpacing)
}

// MinSize implements fyne.WidgetRenderer: as high as its lines at the width it last wrapped at, and
// narrow, so that the column it is in decides its width.
func (r *spanTextRenderer) MinSize() fyne.Size {
	return fyne.NewSize(40, float32(max(1, r.rows))*r.lineHeight())
}

func (r *spanTextRenderer) Layout(size fyne.Size) {
	if size.Width != r.t.width {
		r.t.width = size.Width
		r.wrap(size.Width)
	}
	for i, o := range r.texts {
		o.Move(r.at[i])
		o.Resize(o.MinSize())
	}
	r.placeHighlights()
}

func (r *spanTextRenderer) Objects() []fyne.CanvasObject {
	out := make([]fyne.CanvasObject, 0, len(r.hls)+len(r.texts))
	for _, o := range r.hls {
		out = append(out, o)
	}
	for _, o := range r.texts {
		out = append(out, o)
	}
	return out
}

func (r *spanTextRenderer) Destroy() {}

func (r *spanTextRenderer) Refresh() {
	r.wrap(r.t.width)
	r.placeHighlights()
	canvas.Refresh(r.t)
}

// placeHighlights puts the selection's background behind each line it covers.
func (r *spanTextRenderer) placeHighlights() {
	t := r.t
	lo, hi := t.bounds()
	th := t.Theme()
	h := fyne.MeasureText("Mg", th.Size(t.size), fyne.TextStyle{}).Height
	n := 0
	for i, rw := range t.rows {
		a, b := max(lo, rw.start), min(hi, rw.end)
		if a >= b {
			continue
		}
		if n == len(r.hls) {
			r.hls = append(r.hls, canvas.NewRectangle(color.Transparent))
		}
		x1, x2 := t.xAt(i, a), t.xAt(i, b)
		hl := r.hls[n]
		hl.FillColor = themeColor(th, theme.ColorNameSelection)
		hl.Move(fyne.NewPos(x1, float32(i)*r.lineHeight()))
		hl.Resize(fyne.NewSize(x2-x1, h))
		hl.Show()
		n++
	}
	for _, hl := range r.hls[n:] {
		hl.Hide()
	}
}

// spanWord is a word of a spanText: the runs between two spaces (a slot inside "ns/‹pod›" is a run
// of the word).
type spanWord []span

// wrap breaks the runs into lines at width (0: one line per paragraph) and makes a text of each
// stretch of a line in one colour and style. It keeps the text as it shows and where each run is,
// for the selection.
func (r *spanTextRenderer) wrap(width float32) {
	th := r.t.Theme()
	size := th.Size(r.t.size)
	var paras [][]spanWord
	var para []spanWord
	var word spanWord
	endWord := func() {
		if len(word) > 0 {
			para = append(para, word)
			word = nil
		}
	}
	for _, s := range r.t.spans {
		for i, line := range strings.Split(s.text, "\n") {
			if i > 0 {
				endWord()
				paras = append(paras, para)
				para = nil
			}
			for j, piece := range strings.Split(line, " ") {
				if j > 0 {
					endWord()
				}
				if piece != "" {
					word = append(word, span{piece, s.color, s.style})
				}
			}
		}
	}
	endWord()
	paras = append(paras, para)

	measure := func(s span) float32 { return fyne.MeasureText(s.text, size, s.style).Width }
	wordWidth := func(w spanWord) float32 {
		var x float32
		for _, s := range w {
			x += measure(s)
		}
		return x
	}
	space := fyne.MeasureText(" ", size, fyne.TextStyle{}).Width
	r.texts, r.at, r.rows = r.texts[:0], r.at[:0], 0
	var shown strings.Builder
	n := 0 // runes in shown
	add := func(s string) {
		shown.WriteString(s)
		n += utf8.RuneCountInString(s)
	}
	t := r.t
	t.pieces, t.rows, t.stops = t.pieces[:0], t.rows[:0], nil
	h := r.lineHeight()
	for pi, p := range paras {
		if pi > 0 {
			add("\n")
		}
		t.rows = append(t.rows, spanRow{start: n, end: n})
		var x float32
		var last *canvas.Text
		lastStyle := span{}
		for wi, w := range p {
			ww := wordWidth(w)
			if wi > 0 {
				add(" ")
			}
			if x > 0 && width > 0 && x+space+ww > width {
				r.rows++
				x, last = 0, nil
				t.rows = append(t.rows, spanRow{start: n, end: n})
			}
			if x > 0 {
				x += space
			}
			for i, s := range w {
				t.pieces = append(t.pieces, spanPiece{row: len(t.rows) - 1, x: x, text: s.text, style: s.style, start: n})
				add(s.text)
				t.rows[len(t.rows)-1].end = n
				gap := ""
				if i == 0 && last != nil {
					gap = " "
				}
				if last != nil && lastStyle.color == s.color && lastStyle.style == s.style {
					last.Text += gap + s.text
				} else {
					o := canvas.NewText(s.text, themeColor(th, s.color))
					o.TextSize, o.TextStyle = size, s.style
					r.texts, r.at = append(r.texts, o), append(r.at, fyne.NewPos(x, float32(r.rows)*h))
					last, lastStyle = o, s
				}
				x += measure(s)
			}
		}
		r.rows++
	}
	t.shown = shown.String()
	if lo, hi := t.bounds(); hi > n || lo > n {
		t.anchor, t.caret = 0, 0
	}
}

// cardSpans are a card's text as a spanText's runs: its slots in the muted colour, in italics, named
// after their column ("‹pod›", "‹value›").
func cardSpans(parts []engine.AlertTextPart, cols []engine.AlertColumn, c fyne.ThemeColorName) []span {
	title := map[string]string{}
	for _, col := range cols {
		title[col.Key] = strings.ToLower(col.Title)
	}
	out := make([]span, 0, len(parts))
	for _, p := range parts {
		if p.Slot == "" {
			out = append(out, span{text: p.Text, color: c})
			continue
		}
		name := title[p.Slot]
		if name == "" {
			name = p.Slot
		}
		out = append(out, span{text: "‹" + name + "›", color: colorMuted, style: fyne.TextStyle{Italic: true}})
	}
	return out
}

// historyStrip is a card's last 24 hours, an hour a cell, oldest left: how often the alert started
// firing in it, the severity's colour the stronger the more often (1, 2, 3, 4 or more); an hour
// without one in the chip colour; outlined cells where nothing is known (Prometheus not asked yet,
// not answering, not evaluating this rule). A cell's tooltip says its hour and count.
type historyStrip struct {
	widget.BaseWidget
	sh   *shell
	hist engine.AlertHistory
	tone fyne.ThemeColorName
}

// historyStripHeight is a strip's height; historyCellGap the room between two cells.
const (
	historyStripHeight = 8
	historyCellGap     = 2
)

var _ desktop.Hoverable = (*historyStrip)(nil)

func newHistoryStrip(sh *shell, h engine.AlertHistory, tone fyne.ThemeColorName) *historyStrip {
	s := &historyStrip{sh: sh, hist: h, tone: tone}
	s.ExtendBaseWidget(s)
	return s
}

// known reports whether the strip's hours are counted.
func (s *historyStrip) known() bool {
	return s.hist.State == engine.HistoryOK && len(s.hist.Hours) == engine.HistoryHours
}

// tipAt is the tooltip of the cell under x.
func (s *historyStrip) tipAt(x float32) string {
	if !s.known() {
		switch s.hist.State {
		case engine.HistoryChecking:
			return "History: asking Prometheus…"
		case engine.HistoryNoRule:
			return "History: the cluster's Prometheus does not evaluate this alert's rule"
		}
		if s.hist.Detail != "" {
			return "History: " + s.hist.Detail
		}
		return "History: Prometheus does not answer"
	}
	cell := (s.Size().Width + historyCellGap) / engine.HistoryHours
	i := min(engine.HistoryHours-1, max(0, int(x/cell)))
	end := s.hist.AsOf.Add(-time.Duration(engine.HistoryHours-1-i) * time.Hour)
	f := s.sh.timeFormat()
	when := f.FormatClock(end.Add(-time.Hour)) + "–" + f.FormatClock(end)
	switch n := s.hist.Hours[i]; n {
	case 0:
		return when + ": did not start firing"
	case 1:
		return when + ": started firing once"
	default:
		return when + ": started firing " + strconv.Itoa(n) + " times"
	}
}

// MouseIn implements desktop.Hoverable.
func (s *historyStrip) MouseIn(ev *desktop.MouseEvent) {
	s.sh.tip.hover(s, s.tipAt(ev.Position.X), ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (s *historyStrip) MouseMoved(ev *desktop.MouseEvent) {
	s.sh.tip.hover(s, s.tipAt(ev.Position.X), ev.AbsolutePosition)
}

// MouseOut implements desktop.Hoverable.
func (s *historyStrip) MouseOut() { s.sh.tip.hideFor(s) }

// CreateRenderer implements fyne.Widget.
func (s *historyStrip) CreateRenderer() fyne.WidgetRenderer {
	r := &historyStripRenderer{s: s}
	for range engine.HistoryHours {
		r.cells = append(r.cells, canvas.NewRectangle(color.Transparent))
	}
	r.Refresh()
	return r
}

type historyStripRenderer struct {
	s     *historyStrip
	cells []*canvas.Rectangle
}

func (r *historyStripRenderer) MinSize() fyne.Size {
	return fyne.NewSize(engine.HistoryHours*(historyCellGap+2), historyStripHeight)
}

func (r *historyStripRenderer) Layout(size fyne.Size) {
	w := (size.Width - historyCellGap*(engine.HistoryHours-1)) / engine.HistoryHours
	for i, c := range r.cells {
		c.Move(fyne.NewPos(float32(i)*(w+historyCellGap), 0))
		c.Resize(fyne.NewSize(w, size.Height))
	}
}

func (r *historyStripRenderer) Objects() []fyne.CanvasObject {
	out := make([]fyne.CanvasObject, len(r.cells))
	for i, c := range r.cells {
		out[i] = c
	}
	return out
}

func (r *historyStripRenderer) Destroy() {}

// historyAlpha is a cell's strength by its count: none, then 35, 55, 75 and 100 per cent.
var historyAlpha = []float32{0, 0.35, 0.55, 0.75, 1}

func (r *historyStripRenderer) Refresh() {
	th := r.s.Theme()
	chip := themeColor(th, colorChip)
	for i, c := range r.cells {
		c.CornerRadius = 1.5
		c.StrokeWidth, c.StrokeColor = 0, color.Transparent
		switch {
		case !r.s.known():
			c.FillColor = color.Transparent
			c.StrokeColor, c.StrokeWidth = chip, 1
		case r.s.hist.Hours[i] == 0:
			c.FillColor = chip
		default:
			c.FillColor = fadeColor(themeColor(th, r.s.tone), historyAlpha[min(r.s.hist.Hours[i], len(historyAlpha)-1)])
		}
		c.Refresh()
	}
}

// historySummary is a card's history in words: "fired 7× in 24 h", "flapping: 9× in 24 h" (3 or more
// in one hour), "no new start in 24 h"; "" while it is not counted.
func historySummary(h *engine.AlertHistory) string {
	if h == nil || h.State != engine.HistoryOK {
		return ""
	}
	peak := 0
	for _, n := range h.Hours {
		peak = max(peak, n)
	}
	switch {
	case h.Total == 0:
		return "no new start in 24 h"
	case peak >= 3:
		return "flapping: " + strconv.Itoa(h.Total) + "× in 24 h"
	}
	return "fired " + strconv.Itoa(h.Total) + "× in 24 h"
}

// fadeColor is c at a share of its opacity.
func fadeColor(c color.Color, share float32) color.Color {
	return withAlpha(c, scaleAlpha(nrgba(c).A, share))
}

// tableGrid lays out a card's rows (gridRow) in shared columns: each column as wide as its widest
// cell, the last one (since when) right-aligned in what is left; when the cells do not fit, the
// widest column gives way first, then all in proportion (their texts cut off with an ellipsis).
type tableGrid struct {
	natural []float32 // each column's widest cell
	gap     float32
}

// widths returns the columns' widths in a row of width w.
func (g *tableGrid) widths(w float32) []float32 {
	n := len(g.natural)
	out := make([]float32, n)
	if n == 0 {
		return out
	}
	avail := w - g.gap*float32(n-1)
	var sum float32
	for _, x := range g.natural[:n-1] {
		sum += x
	}
	last := g.natural[n-1]
	copy(out, g.natural)
	if over := sum + last - avail; over > 0 && n > 1 {
		widest := 0
		for i, x := range g.natural[:n-1] {
			if x > g.natural[widest] {
				widest = i
			}
		}
		give := min(over, max(0, out[widest]-60))
		out[widest] -= give
		sum -= give
		if over -= give; over > 0 && sum > 0 {
			k := max(0, (sum-over)/sum)
			for i := range out[:n-1] {
				out[i] *= k
			}
			sum *= k
		}
	}
	out[n-1] = max(last, avail-sum)
	return out
}

// gridRow is a row of a tableGrid: its cells, one per column.
type gridRow struct{ g *tableGrid }

// Layout implements fyne.Layout.
func (l gridRow) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	ws := l.g.widths(size.Width)
	var x float32
	for i, o := range objs {
		if i >= len(ws) {
			break
		}
		h := o.MinSize().Height
		o.Move(fyne.NewPos(x, (size.Height-h)/2))
		o.Resize(fyne.NewSize(ws[i], h))
		x += ws[i] + l.g.gap
	}
}

// MinSize implements fyne.Layout: as high as its highest cell, and narrow, the card deciding its
// width.
func (l gridRow) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var h float32
	for _, o := range objs {
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(60, h)
}
