package fyneui

import (
	"image/color"
	"net/url"
	"strings"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Selecting and copying text in the agent's column, block by block (the owner's choice of
// 2026-10-21): a paragraph, a heading, a quote, a list item's text, a table's cell, a code block, a
// note and a tool's arguments and result are each selectable as the drawer's texts are
// (selection.go, D-25, D-99): a drag selects within the block, a double click selects a word, the
// copy shortcut (⌘C, Ctrl+C) copies, Escape drops the selection. A drag ends at the block's edge; an
// answer's copy icon still copies all of it as Markdown. The pinned message and the empty chat's
// sample questions stay clickable, not selectable.

// mdHooks are what a message's text does on a click and a copy.
type mdHooks struct {
	link func(*url.URL) // opens a link
	copy func(string)   // copies a selection; nil: the text is not selectable
}

// mdHooks are the column's: a link opens in the browser, a selection goes to the clipboard as the
// answer's copy icon puts an answer there.
func (c *chatPanel) mdHooks() mdHooks {
	return mdHooks{link: c.openLink, copy: c.copySelection}
}

func (c *chatPanel) copySelection(s string) { c.sh.copyText(s, false) }

// selArea is the selectable surface over a block: the last object of the block's renderer, so that
// it takes the mouse, the focus and the copy shortcut (textSelection), and passes a click and the
// pointer on to the block (a link opens, the hand shows over it).
type selArea struct {
	widget.BaseWidget
	textSelection
	tap      func(*fyne.PointEvent)   // a click: the block's link
	onLink   func(fyne.Position) bool // whether a point lies on a link; nil: no point does
	overLink bool
}

var (
	_ fyne.Tappable     = (*selArea)(nil)
	_ desktop.Hoverable = (*selArea)(nil)
)

func newSelArea(text func() string, offset func(fyne.Position) int, repaint func(), copied func(string)) *selArea {
	a := &selArea{}
	a.ExtendBaseWidget(a)
	a.self, a.getText, a.offsetAt, a.repaint, a.copied = a, text, offset, repaint, copied
	return a
}

// reset drops the selection without a repaint: the block's text changed under it.
func (a *selArea) reset() { a.anchor, a.caret, a.dragging = 0, 0, false }

// Tapped implements fyne.Tappable.
func (a *selArea) Tapped(ev *fyne.PointEvent) {
	if a.tap != nil {
		a.tap(ev)
	}
}

// Cursor implements desktop.Cursorable: the hand over a link, the text cursor elsewhere.
func (a *selArea) Cursor() desktop.Cursor {
	if a.overLink {
		return desktop.PointerCursor
	}
	return desktop.TextCursor
}

// MouseIn implements desktop.Hoverable.
func (a *selArea) MouseIn(ev *desktop.MouseEvent) { a.MouseMoved(ev) }

// MouseMoved implements desktop.Hoverable.
func (a *selArea) MouseMoved(ev *desktop.MouseEvent) {
	a.overLink = a.onLink != nil && a.onLink(ev.Position)
}

// MouseOut implements desktop.Hoverable.
func (a *selArea) MouseOut() { a.overLink = false }

// TypedKey implements fyne.Focusable: Escape drops a selection; other keys go on to the window (an
// Escape without one stops a running reply, as the input's does).
func (a *selArea) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeyEscape && a.anchor != a.caret {
		a.clear()
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(a); c != nil && c.OnTypedKey() != nil {
		c.OnTypedKey()(ev)
	}
}

// CreateRenderer implements fyne.Widget: the area draws nothing; the block draws the selection.
func (a *selArea) CreateRenderer() fyne.WidgetRenderer { return &selAreaRenderer{} }

type selAreaRenderer struct{}

func (*selAreaRenderer) Layout(fyne.Size)             {}
func (*selAreaRenderer) MinSize() fyne.Size           { return fyne.Size{} }
func (*selAreaRenderer) Refresh()                     {}
func (*selAreaRenderer) Objects() []fyne.CanvasObject { return nil }
func (*selAreaRenderer) Destroy()                     {}

// newSelHighlight is one highlight of a selection, hidden until placed.
func newSelHighlight() *canvas.Rectangle {
	r := canvas.NewRectangle(color.Transparent)
	r.Hide()
	return r
}

func selHighlightColor(th fyne.Theme) color.Color { return themeColor(th, theme.ColorNameSelection) }

func runeCount(s string) int { return utf8.RuneCountInString(s) }

func measureRunes(r []rune, size float32, st fyne.TextStyle) float32 {
	return fyne.MeasureText(string(r), size, st).Width
}

// nearestRune returns the rune boundary of text nearest x, measured in size and style.
func nearestRune(text string, x, size float32, st fyne.TextStyle) int {
	r := []rune(text)
	if x <= 0 || len(r) == 0 {
		return 0
	}
	lo, hi := 0, len(r)
	for lo < hi { // the first boundary k whose width reaches x
		mid := (lo + hi) / 2
		if measureRunes(r[:mid], size, st) < x {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo > 0 && x-measureRunes(r[:lo-1], size, st) < measureRunes(r[:lo], size, st)-x {
		return lo - 1
	}
	return lo
}

// --- a flow: a paragraph, heading, quote, list item's text or table cell ---

// selectable makes the flow's text selectable; copied gets what the copy shortcut copies.
func (f *mdFlow) selectable(copied func(string)) {
	f.sel = newSelArea(func() string { t, _ := f.selText(); return t }, f.selOffset, f.Refresh, copied)
	f.sel.tap = f.Tapped
	f.sel.onLink = func(p fyne.Position) bool { return f.linkAt(p) != nil }
}

// selText is the flow's text as it is selected and copied, and the rune offset each piece starts
// at: the pieces in order, a wrapped line running on, a line break of the text kept.
func (f *mdFlow) selText() (string, []int) {
	f.relayout()
	var b strings.Builder
	starts := make([]int, len(f.pieces))
	n := 0
	for i, p := range f.pieces {
		if i > 0 && p.line != f.pieces[i-1].line {
			sep := ""
			switch p.brk {
			case flowHard:
				sep = strings.Repeat("\n", max(p.hard, 1))
			case flowSpace:
				sep = " "
			}
			b.WriteString(sep)
			n += len(sep)
		}
		starts[i] = n
		b.WriteString(p.text)
		n += runeCount(p.text)
	}
	return b.String(), starts
}

// pieceTextX is where a piece's text starts: a code chip's after its padding.
func pieceTextX(p flowPiece) float32 {
	if p.code {
		return p.x + chipPad
	}
	return p.x
}

// selOffset maps a point to the nearest rune offset of selText: on the line under it, or the
// nearest line above or below the text.
func (f *mdFlow) selOffset(pos fyne.Position) int {
	_, starts := f.selText()
	if len(f.pieces) == 0 {
		return 0
	}
	// The line: the first whose bottom lies below the point, else the last.
	line := f.pieces[len(f.pieces)-1].line
	for _, p := range f.pieces {
		if pos.Y <= p.y+p.h {
			line = p.line
			break
		}
	}
	first, last := -1, -1
	for i, p := range f.pieces {
		if p.line == line {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if pos.X <= f.pieces[first].x {
		return starts[first]
	}
	for i := first; i <= last; i++ {
		p := f.pieces[i]
		if pos.X < p.x+p.w {
			return starts[i] + nearestRune(p.text, pos.X-pieceTextX(p), p.size, p.style)
		}
	}
	return starts[last] + runeCount(f.pieces[last].text)
}

// placeSelection shows the selection under the pieces it covers, in hls (one per piece).
func (f *mdFlow) placeSelection(hls []*canvas.Rectangle) {
	lo, hi := 0, 0
	if f.sel != nil {
		lo, hi = f.sel.bounds()
	}
	var starts []int
	if lo < hi {
		_, starts = f.selText()
	}
	th := f.Theme()
	for i, hl := range hls {
		if lo >= hi || i >= len(f.pieces) || i >= len(starts) {
			hl.Hide()
			continue
		}
		p := f.pieces[i]
		r := []rune(p.text)
		a, b := max(lo, starts[i])-starts[i], min(hi, starts[i]+len(r))-starts[i]
		if a >= b {
			hl.Hide()
			continue
		}
		x1 := pieceTextX(p) + measureRunes(r[:a], p.size, p.style)
		x2 := pieceTextX(p) + measureRunes(r[:b], p.size, p.style)
		hl.FillColor = selHighlightColor(th)
		hl.Move(fyne.NewPos(x1, p.y-1))
		hl.Resize(fyne.NewSize(x2-x1, p.h+2))
		hl.Show()
		hl.Refresh()
	}
}

// --- a code block ---

// selectable makes the code block's text selectable; copied gets what the copy shortcut copies.
func (c *mdCode) selectable(copied func(string)) {
	c.sel = newSelArea(func() string { t, _ := c.selText(); return t }, c.selOffset, c.Refresh, copied)
}

// selText is the block's text as it is selected and copied, and the rune offset each drawn line
// starts at: a line the width broke runs on (prose by a space, code without one), a line of the
// text ends in a line break.
func (c *mdCode) selText() (string, []int) {
	c.relayout()
	var b strings.Builder
	starts := make([]int, len(c.lines))
	n := 0
	for i, l := range c.lines {
		if i > 0 {
			sep := "\n"
			if i < len(c.cont) && c.cont[i] {
				sep = ""
				if c.words {
					sep = " "
				}
			}
			b.WriteString(sep)
			n += len(sep)
		}
		starts[i] = n
		b.WriteString(l)
		n += runeCount(l)
	}
	return b.String(), starts
}

func (c *mdCode) selOffset(pos fyne.Position) int {
	_, starts := c.selText()
	if len(c.lines) == 0 {
		return 0
	}
	i := max(0, min(int((pos.Y-c.pad())/c.lineHeight()), len(c.lines)-1))
	return starts[i] + nearestRune(c.lines[i], pos.X-c.pad(), c.textSize(), fyne.TextStyle{Monospace: true})
}

// placeSelection shows the selection under the lines it covers, in hls (one per line).
func (c *mdCode) placeSelection(hls []*canvas.Rectangle) {
	lo, hi := 0, 0
	if c.sel != nil {
		lo, hi = c.sel.bounds()
	}
	var starts []int
	if lo < hi {
		_, starts = c.selText()
	}
	th := c.Theme()
	st := fyne.TextStyle{Monospace: true}
	pad, lh := c.pad(), c.lineHeight()
	for i, hl := range hls {
		if lo >= hi || i >= len(c.lines) || i >= len(starts) {
			hl.Hide()
			continue
		}
		r := []rune(c.lines[i])
		end := starts[i] + len(r)
		a, b := max(lo, starts[i])-starts[i], min(hi, end)-starts[i]
		inside := lo <= end && hi > end // the selection runs on past this line's end
		if a > b || a == b && !inside {
			hl.Hide() // nothing of this line; an empty line inside the selection keeps a sliver
			continue
		}
		x1 := pad + measureRunes(r[:a], c.textSize(), st)
		x2 := max(pad+measureRunes(r[:b], c.textSize(), st), x1+3)
		hl.FillColor = selHighlightColor(th)
		hl.Move(fyne.NewPos(x1, pad+float32(i)*lh))
		hl.Resize(fyne.NewSize(x2-x1, lh))
		hl.Show()
		hl.Refresh()
	}
}
