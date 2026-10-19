package termview

import (
	"image/color"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/model"
)

// LogView is how a log view shows the lines it holds: the options of a logs tab that need no new
// stream (the engine holds them; the GUI hands them over).
type LogView struct {
	// Prefixed says every line starts with a resource name and a space ("<pod>/<container> "),
	// as the merged logs of a controlling resource do.
	Prefixed bool
	// Names shows that prefix, in the accent colour; off, it is hidden.
	Names bool
	// Stamped says every line from the cluster starts (after the prefix) with the API server's
	// RFC 3339 timestamp: shown with Timestamps, hidden without.
	Stamped bool
	// Timestamps shows the line's timestamp (after the prefix), muted, written by Time.
	Timestamps bool
	// Time writes a shown timestamp: the user's zone and format with milliseconds (D-31,
	// model.TimeFormat.FormatPrecise); the zero value writes ISO 8601 in UTC. A timestamp that
	// does not parse shows as it came.
	Time model.TimeFormat
	// Wrap wraps long lines at the view's width, at a space where one is near.
	Wrap bool
	// Search is looked for in the lines, case-insensitively: matches are highlighted.
	Search string
	// Filter shows only the lines that contain Search.
	Filter bool
	// Pod and Container narrow a prefixed view to the lines of one pod, and of the containers of
	// one name; empty selects all. Lines without a "<pod>/<container>" prefix (Cogpit's own)
	// always show.
	Pod, Container string
	// Workload narrows a group's view (an application's, whose lines start with
	// "<workload>/<pod>/<container>") to the lines of the workload of that name; empty selects
	// all.
	Workload string
}

// Bounds of the log view's rendering.
const (
	maxMarksPerRow = 32 // highlighted matches per row; more are not marked
)

// displayRow is one row of the list: a part of a line (the whole line unless it wraps), by byte
// offsets into the line's shown text.
type displayRow struct {
	line     int64 // the line's absolute number: lines dropped before it plus its index
	from, to int32 // to < 0: to the end
}

// shownSpans are a shown line's parts: the resource name [0, nameEnd) and the timestamp
// [tsStart, tsEnd), each empty when absent.
type shownSpans struct {
	nameEnd, tsStart, tsEnd int
}

// shown returns the text a line shows under v (the prefix dropped when names are hidden, a
// stamped line's timestamp when timestamps are) and where its name and timestamp are.
func (v LogView) shown(raw string) (string, shownSpans) {
	text := raw
	var sp shownSpans
	start := 0
	if v.Prefixed {
		i := strings.IndexByte(text, ' ')
		if i < 0 {
			i = len(text)
		}
		if v.Names {
			sp.nameEnd = i
			start = min(i+1, len(text))
		} else {
			text = text[min(i+1, len(text)):]
		}
	}
	if v.Timestamps || v.Stamped {
		rest := text[start:]
		end := strings.IndexByte(rest, ' ')
		if end < 0 {
			end = len(rest)
		}
		switch {
		case !looksLikeTimestamp(rest[:end]):
		case v.Timestamps:
			if t, err := time.Parse(time.RFC3339Nano, rest[:end]); err == nil {
				local := v.Time.FormatPrecise(t)
				text = text[:start] + local + rest[end:]
				end = len(local)
			}
			sp.tsStart, sp.tsEnd = start, start+end
		case start == 0:
			text = text[min(end+1, len(text)):]
		default:
			text = text[:start] + text[min(start+end+1, len(text)):]
		}
	}
	return text, sp
}

// narrows reports whether the view shows the lines of some sources only.
func (v LogView) narrows() bool {
	return v.Prefixed && (v.Pod != "" || v.Container != "" || v.Workload != "")
}

// selects reports whether the view shows a line by its source: every line unless it narrows, and
// then the lines of the selected workload, pod and containers and the lines without a source. A
// source is "<pod>/<container>", or a group's "<workload>/<pod>/<container>".
func (v LogView) selects(raw string) bool {
	if !v.narrows() {
		return true
	}
	src, _, _ := strings.Cut(raw, " ")
	parts := strings.Split(src, "/")
	var workload, pod, c string
	switch len(parts) {
	case 2:
		pod, c = parts[0], parts[1]
	case 3:
		workload, pod, c = parts[0], parts[1], parts[2]
	default:
		return true // Cogpit's own line
	}
	return (v.Workload == "" || v.Workload == workload) && (v.Pod == "" || v.Pod == pod) && (v.Container == "" || v.Container == c)
}

// looksLikeTimestamp reports whether s has the shape of an RFC 3339 timestamp:
// 2026-10-11T12:00:01.123456789Z. Lines Cogpit writes itself have none.
func looksLikeTimestamp(s string) bool {
	return len(s) >= 20 && s[4] == '-' && s[7] == '-' && s[10] == 'T' && s[13] == ':' && s[0] >= '0' && s[0] <= '9'
}

// matches reports whether the shown text contains the search (an empty search matches).
func (v LogView) matches(text string) bool {
	return v.Search == "" || indexFold(text, v.Search) >= 0
}

// indexFold returns the byte index of the first case-insensitive match of sub in s, or -1. Case
// folding that changes a character's encoded length (rare outside ASCII) is not matched.
func indexFold(s, sub string) int {
	n := len(sub)
	if n == 0 {
		return 0
	}
	first := sub[0]
	ascii := first < utf8.RuneSelf
	lowerFirst, upperFirst := first, first
	if ascii && first >= 'A' && first <= 'Z' {
		lowerFirst = first + 'a' - 'A'
	}
	if ascii && first >= 'a' && first <= 'z' {
		upperFirst = first - ('a' - 'A')
	}
	for i := 0; i+n <= len(s); i++ {
		if ascii && s[i] != lowerFirst && s[i] != upperFirst {
			continue
		}
		if strings.EqualFold(s[i:i+n], sub) {
			return i
		}
	}
	return -1
}

// wrapLine splits text into rows of at most cols characters, breaking after a space in the row
// when there is one, else inside a word. cols ≤ 0 keeps one row.
func wrapLine(text string, cols int, add func(from, to int)) {
	if cols <= 0 || len(text) <= cols && utf8.RuneCountInString(text) <= cols {
		add(0, len(text))
		return
	}
	for start := 0; start < len(text); {
		end, space, n := start, -1, 0
		for end < len(text) && n < cols {
			r, size := utf8.DecodeRuneInString(text[end:])
			if r == ' ' {
				space = end
			}
			end += size
			n++
		}
		if end < len(text) && space > start {
			end = space + 1
		}
		add(start, end)
		start = end
	}
}

// logRun is a piece of a row in one colour, highlighted or not.
type logRun struct {
	text  string
	color fyne.ThemeColorName
	mark  bool
}

// runsOf splits text[from:to] into runs: the name in the accent colour, the timestamp muted, the
// rest in the foreground colour, matches of search highlighted.
func runsOf(text string, sp shownSpans, from, to int, search string, dst []logRun) []logRun {
	colorAt := func(i int) fyne.ThemeColorName {
		switch {
		case i < sp.nameEnd:
			return theme.ColorNamePrimary
		case i >= sp.tsStart && i < sp.tsEnd:
			return theme.ColorNamePlaceHolder
		}
		return theme.ColorNameForeground
	}
	// Match ranges within [from, to).
	var marks [][2]int
	if search != "" {
		lo := max(0, from-len(search)+1)
		for i := lo; i < to && len(marks) < maxMarksPerRow; {
			j := indexFold(text[i:], search)
			if j < 0 {
				break
			}
			start, end := i+j, i+j+len(search)
			if start >= to {
				break
			}
			if end > from {
				marks = append(marks, [2]int{max(start, from), min(end, to)})
			}
			i = end
		}
	}
	// Boundaries: the spans' edges and the marks' edges.
	bounds := []int{from, to}
	for _, b := range []int{sp.nameEnd, sp.tsStart, sp.tsEnd} {
		if b > from && b < to {
			bounds = append(bounds, b)
		}
	}
	for _, m := range marks {
		bounds = append(bounds, m[0], m[1])
	}
	sortInts(bounds)
	inMark := func(i int) bool {
		for _, m := range marks {
			if i >= m[0] && i < m[1] {
				return true
			}
		}
		return false
	}
	dst = dst[:0]
	for k := 0; k+1 < len(bounds); k++ {
		a, b := bounds[k], bounds[k+1]
		if a >= b {
			continue
		}
		run := logRun{text: text[a:b], color: colorAt(a), mark: inMark(a)}
		if n := len(dst); n > 0 && dst[n-1].color == run.color && dst[n-1].mark == run.mark {
			dst[n-1].text += run.text
			continue
		}
		dst = append(dst, run)
	}
	return dst
}

func sortInts(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// logRow draws one row of the list: its runs left to right in monospace, highlighted ones (a
// search's matches) on the find colour, and its part of the selection (logselect.go) on the
// theme's selection colour beneath.
type logRow struct {
	widget.BaseWidget
	runs []logRun
	sel  rowSel
	item widget.ListItemID // the list item it shows (Logs.updateItem)
	rend *logRowRenderer   // nil until rendered
}

func newLogRow() *logRow {
	r := &logRow{}
	r.ExtendBaseWidget(r)
	return r
}

func (r *logRow) set(runs []logRun, sel rowSel) {
	r.runs = append(r.runs[:0], runs...)
	r.sel = sel
	r.Refresh()
}

// setSel repaints the row's part of the selection alone.
func (r *logRow) setSel(sel rowSel) {
	r.sel = sel
	if r.rend != nil {
		r.rend.placeSel(r.Size())
		r.rend.sel.Refresh()
	}
}

// CreateRenderer implements fyne.Widget.
func (r *logRow) CreateRenderer() fyne.WidgetRenderer {
	sel := canvas.NewRectangle(color.Transparent)
	sel.Hide()
	r.rend = &logRowRenderer{row: r, sel: sel, objs: []fyne.CanvasObject{sel}}
	return r.rend
}

type logRowRenderer struct {
	row   *logRow
	sel   *canvas.Rectangle // the selection, beneath the marks and the texts
	texts []*canvas.Text
	marks []*canvas.Rectangle
	objs  []fyne.CanvasObject
}

func (lr *logRowRenderer) MinSize() fyne.Size {
	return fyne.NewSize(0, lineHeight(lr.row.Theme()))
}

func (lr *logRowRenderer) Layout(size fyne.Size) {
	x := float32(0)
	for i, run := range lr.row.runs {
		if i >= len(lr.texts) {
			break
		}
		t := lr.texts[i]
		w := t.MinSize().Width
		t.Move(fyne.NewPos(x, 0))
		t.Resize(fyne.NewSize(w, size.Height))
		m := lr.marks[i]
		if run.mark {
			m.Move(fyne.NewPos(x, 0))
			m.Resize(fyne.NewSize(w, size.Height))
		}
		x += w
	}
	lr.placeSel(size)
}

// xAt is where the byte off of the row's text (its runs one after another) is drawn.
func (lr *logRowRenderer) xAt(off int) float32 {
	x, pos := float32(0), 0
	for i, run := range lr.row.runs {
		if i >= len(lr.texts) {
			break
		}
		if off < pos+len(run.text) {
			if off <= pos {
				return x
			}
			t := lr.texts[i]
			return x + fyne.MeasureText(run.text[:off-pos], t.TextSize, t.TextStyle).Width
		}
		x += lr.texts[i].MinSize().Width
		pos += len(run.text)
	}
	return x
}

// placeSel puts the selection's rectangle under its part of the row; a selected line break adds
// a character's width after the text, and a selection that goes on fills the gap below the row,
// so that the rows of a selection join.
func (lr *logRowRenderer) placeSel(size fyne.Size) {
	s := lr.row.sel
	if !s.on() {
		lr.sel.Hide()
		return
	}
	th := lr.row.Theme()
	x1, x2 := lr.xAt(s.from), lr.xAt(s.to)
	if s.eol {
		x2 += charWidth(th)
	}
	lr.sel.FillColor = th.Color(theme.ColorNameSelection, fyne.CurrentApp().Settings().ThemeVariant())
	h := size.Height
	if s.cont {
		h += th.Size(theme.SizeNamePadding) // the list's gap between rows
	}
	lr.sel.Move(fyne.NewPos(x1, 0))
	lr.sel.Resize(fyne.NewSize(x2-x1, h))
	lr.sel.Show()
}

func (lr *logRowRenderer) Refresh() {
	th := lr.row.Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	runs := lr.row.runs
	for len(lr.texts) < len(runs) {
		t := canvas.NewText("", nil)
		t.TextStyle = fyne.TextStyle{Monospace: true}
		m := canvas.NewRectangle(nil)
		lr.texts = append(lr.texts, t)
		lr.marks = append(lr.marks, m)
		lr.objs = append(lr.objs, m, t)
	}
	for i, t := range lr.texts {
		m := lr.marks[i]
		if i >= len(runs) {
			t.Hide()
			m.Hide()
			continue
		}
		t.Text = runs[i].text
		t.Color = th.Color(runs[i].color, v)
		t.TextSize = th.Size(theme.SizeNameText)
		t.Show()
		if runs[i].mark {
			m.FillColor = findColor(th, v)
			m.Show()
		} else {
			m.Hide()
		}
		t.Refresh()
		m.Refresh()
	}
	lr.Layout(lr.row.Size())
	lr.sel.Refresh()
}

func (lr *logRowRenderer) Objects() []fyne.CanvasObject { return lr.objs }
func (lr *logRowRenderer) Destroy()                     {}

// findColor is a search's match: the theme's warning hue, translucent, as editors and browsers
// mark what a search found, so that it is not taken for selected text (D-99).
func findColor(th fyne.Theme, v fyne.ThemeVariant) color.Color {
	c := color.NRGBAModel.Convert(th.Color(theme.ColorNameWarning, v)).(color.NRGBA)
	c.A = findAlpha
	return c
}

// findAlpha is the find colour's opacity.
const findAlpha = 0x60

// lineHeight is the height of one row of monospace text.
func lineHeight(th fyne.Theme) float32 {
	return fyne.MeasureText("M", th.Size(theme.SizeNameText), fyne.TextStyle{Monospace: true}).Height
}

// charWidth is the width of one monospace character.
func charWidth(th fyne.Theme) float32 {
	return fyne.MeasureText("M", th.Size(theme.SizeNameText), fyne.TextStyle{Monospace: true}).Width
}

// logList is the list of rows; it hands "/" to the view (focus the search) instead of ignoring it
// when it has the keyboard focus, which a tap on a row gives it.
type logList struct {
	widget.List
	onSlash func()
}

// TypedRune implements fyne.Focusable.
func (l *logList) TypedRune(r rune) {
	if r == '/' && l.onSlash != nil {
		l.onSlash()
		return
	}
	l.List.TypedRune(r)
}

// WriteLines writes lines, one per line, to w: the file a logs tab's download saves.
func WriteLines(w io.Writer, lines []string) error {
	for _, l := range lines {
		if _, err := io.WriteString(w, l); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	return nil
}
