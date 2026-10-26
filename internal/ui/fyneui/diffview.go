package fyneui

import (
	"fmt"
	"image/color"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// diffView shows a line diff as the engine computes it (engine.DiffLines, compacted): removed
// lines on a tint of the error colour, added lines on a tint of the ok colour, unchanged context
// plain, with the old and new line numbers, and a row saying how many unchanged lines were left
// out between two hunks. It is a widget.List, so a diff of a large object costs only the rows on
// screen. A line's text is selectable as the drawer's texts are (selection.go, D-25: a drag, a
// double click on a word, ⌘C copies through copied); the owner, 2026-10-26: "Ich möchte texte
// markieren können. Überall".
type diffView struct {
	rows   []diffRow
	list   *widget.List
	copied func(string)
}

// diffRow is a diff line, or (skipped > 0) a marker for unchanged lines left out before it.
type diffRow struct {
	line    engine.DiffLine
	skipped int
}

func newDiffView(copied func(string)) *diffView {
	d := &diffView{copied: copied}
	d.list = widget.NewList(
		func() int { return len(d.rows) },
		func() fyne.CanvasObject { return newDiffLineWidget(d.copied) },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i >= 0 && i < len(d.rows) {
				o.(*diffLineWidget).set(d.rows[i])
			}
		})
	d.list.HideSeparators = true
	return d
}

// set shows lines, inserting a marker wherever the line numbers jump.
func (d *diffView) set(lines []engine.DiffLine) {
	d.rows = d.rows[:0]
	lastOld, lastNew := 0, 0
	for _, l := range lines {
		skipped := 0
		switch {
		case l.Old > 0:
			skipped = l.Old - lastOld - 1
		case l.New > 0:
			skipped = l.New - lastNew - 1
		}
		if skipped > 0 {
			d.rows = append(d.rows, diffRow{skipped: skipped})
		}
		d.rows = append(d.rows, diffRow{line: l})
		if l.Old > 0 {
			lastOld = l.Old
		}
		if l.New > 0 {
			lastNew = l.New
		}
	}
	d.list.Refresh()
	d.list.ScrollToTop()
}

// diffLineWidget is one row of the diff view; its text is selectable.
type diffLineWidget struct {
	widget.BaseWidget
	textSelection
	row diffRow

	bg       *canvas.Rectangle
	hl       *canvas.Rectangle // the selection
	old, new *canvas.Text
	sign     *canvas.Text
	text     *canvas.Text
}

func newDiffLineWidget(copied func(string)) *diffLineWidget {
	w := &diffLineWidget{
		bg:   canvas.NewRectangle(color.Transparent),
		hl:   canvas.NewRectangle(color.Transparent),
		old:  canvas.NewText("", color.Transparent),
		new:  canvas.NewText("", color.Transparent),
		sign: canvas.NewText("", color.Transparent),
		text: canvas.NewText("", color.Transparent),
	}
	for _, t := range []*canvas.Text{w.old, w.new, w.sign, w.text} {
		t.TextStyle = fyne.TextStyle{Monospace: true}
	}
	w.old.Alignment, w.new.Alignment = fyne.TextAlignTrailing, fyne.TextAlignTrailing
	w.ExtendBaseWidget(w)
	w.self = w
	w.getText = func() string { return w.text.Text }
	w.offsetAt = w.offset
	w.repaint = w.Refresh
	w.copied = copied
	return w
}

func (w *diffLineWidget) set(r diffRow) {
	if r != w.row { // a row the list reuses for another line drops its selection
		w.anchor, w.caret = 0, 0
	}
	w.row = r
	w.Refresh()
}

// offset maps a point to the nearest rune offset of the line's text.
func (w *diffLineWidget) offset(p fyne.Position) int {
	runes := []rune(w.text.Text)
	x := p.X - w.text.Position().X
	prev := float32(0)
	for i := range runes {
		cw := fyne.MeasureText(string(runes[:i+1]), w.text.TextSize, w.text.TextStyle).Width
		if x < (prev+cw)/2 {
			return i
		}
		prev = cw
	}
	return len(runes)
}

// CreateRenderer implements fyne.Widget.
func (w *diffLineWidget) CreateRenderer() fyne.WidgetRenderer {
	r := &diffLineRenderer{w: w}
	r.Refresh()
	return r
}

type diffLineRenderer struct{ w *diffLineWidget }

func (r *diffLineRenderer) dims() (size, pad, num, sign float32) {
	th := r.w.Theme()
	size = th.Size(theme.SizeNameText)
	pad = th.Size(theme.SizeNameInnerPadding)
	style := fyne.TextStyle{Monospace: true}
	num = fyne.MeasureText("00000", size, style).Width
	sign = fyne.MeasureText("+ ", size, style).Width
	return size, pad, num, sign
}

func (r *diffLineRenderer) MinSize() fyne.Size {
	size, pad, num, sign := r.dims()
	h := fyne.MeasureText("Mg", size, fyne.TextStyle{Monospace: true}).Height
	return fyne.NewSize(pad+2*(num+pad)+sign+r.w.text.MinSize().Width, h+r.w.Theme().Size(theme.SizeNameLineSpacing))
}

func (r *diffLineRenderer) Layout(s fyne.Size) {
	_, pad, num, sign := r.dims()
	w := r.w
	w.bg.Resize(s)
	place := func(t *canvas.Text, x, width float32) {
		ms := t.MinSize()
		t.Move(fyne.NewPos(x, (s.Height-ms.Height)/2))
		t.Resize(fyne.NewSize(width, ms.Height))
	}
	place(w.old, pad, num)
	place(w.new, pad+num+pad, num)
	x := pad + 2*(num+pad)
	place(w.sign, x, sign)
	place(w.text, x+sign, fyne.Max(s.Width-x-sign, w.text.MinSize().Width))
	lo, hi := w.bounds()
	if lo == hi {
		w.hl.Hide()
		return
	}
	runes := []rune(w.text.Text)
	lo, hi = min(lo, len(runes)), min(hi, len(runes))
	x1 := fyne.MeasureText(string(runes[:lo]), w.text.TextSize, w.text.TextStyle).Width
	x2 := fyne.MeasureText(string(runes[:hi]), w.text.TextSize, w.text.TextStyle).Width
	h := w.text.MinSize().Height
	w.hl.Move(fyne.NewPos(w.text.Position().X+x1, (s.Height-h)/2))
	w.hl.Resize(fyne.NewSize(x2-x1, h))
	w.hl.Show()
}

func (r *diffLineRenderer) Refresh() {
	w := r.w
	th := w.Theme()
	size, _, _, _ := r.dims()
	muted, fg := themeColor(th, colorMuted), themeColor(th, theme.ColorNameForeground)
	for _, t := range []*canvas.Text{w.old, w.new, w.sign, w.text} {
		t.TextSize = size
		t.Color = fg
	}
	w.old.Color, w.new.Color = muted, muted
	w.bg.FillColor = color.Transparent
	w.hl.FillColor = themeColor(th, theme.ColorNameSelection)
	l := w.row.line
	w.old.Text, w.new.Text = lineNumber(l.Old), lineNumber(l.New)
	w.text.Text = l.Text
	switch {
	case w.row.skipped > 0:
		w.old.Text, w.new.Text, w.sign.Text = "", "", ""
		w.text.Text = fmt.Sprintf("⋯ %d unchanged line%s", w.row.skipped, plural(w.row.skipped))
		w.text.Color = muted
		w.bg.FillColor = withAlpha(themeColor(th, colorMuted), 0x14)
	case l.Op == engine.DiffAdded:
		w.sign.Text = "+"
		c := themeColor(th, colorStatusOK)
		w.sign.Color, w.text.Color = c, c
		w.bg.FillColor = withAlpha(c, 0x1f)
	case l.Op == engine.DiffRemoved:
		w.sign.Text = "-"
		c := themeColor(th, colorStatusError)
		w.sign.Color, w.text.Color = c, c
		w.bg.FillColor = withAlpha(c, 0x1f)
	default:
		w.sign.Text = " "
	}
	r.Layout(w.Size())
	for _, o := range r.Objects() {
		o.Refresh()
	}
}

func (r *diffLineRenderer) Objects() []fyne.CanvasObject {
	w := r.w
	return []fyne.CanvasObject{w.bg, w.hl, w.old, w.new, w.sign, w.text}
}

func (r *diffLineRenderer) Destroy() {}

func lineNumber(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
