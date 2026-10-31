package termview

import (
	"image/color"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Scrolling sideways (the owner's finding of 2026-10-31: "Im Dock-Tab "Logs" eines Pods kann man
// bei langen Log-Zeilen und deaktiviertem "Warp" nicht nach rechts scrollen um sie vollständig
// erfassen zu können."). The lead's details (D-143):
//   - while the lines do not wrap, the rows scroll sideways together: a sideways swipe or wheel over
//     the lines (⇧ and the wheel where the system makes that one), and a scroll bar along the
//     bottom, Fyne's own, as wide as the widest line; the head row stays where it is;
//   - Fyne's list lays every row out at the list's width, so a wider list would put its vertical
//     scroll bar out of view: a scroller of its own (Logs.side) over the list, under the surface,
//     holds the rows' width and nothing else, and the rows follow its offset;
//   - a row draws the characters in view only, from the first the offset reaches on, as many as
//     twice the view's width holds (narrow characters take less room): its cost is the view's
//     width, not its line's, so a line shows whole up to the stream's bound (logMaxLineBytes);
//   - a column is a character of the monospace font: a line of wider ones (CJK) ends before the
//     bar does, one of narrower ones after it.

// newSide makes the sideways scroller; its content is as wide as the rows (updateSide).
func (l *Logs) newSide() {
	l.sideFill = canvas.NewRectangle(color.Transparent)
	l.side = container.NewHScroll(l.sideFill)
	l.side.OnScrolled = func(p fyne.Position) { l.setShift(p.X) }
}

// sideWidth is how wide the rows are: the widest line and a character for a selected line end; 0
// while they wrap.
func (l *Logs) sideWidth() float32 {
	if l.view.Wrap {
		return 0
	}
	return float32(l.widest+1) * charWidth(l.list.Theme())
}

// updateSide gives the scroller the rows' width after they changed, keeping its offset within it;
// the surface leaves the bar its place while it shows (surfaceLayout).
func (l *Logs) updateSide() {
	w := l.sideWidth()
	if w == l.sideFill.MinSize().Width {
		return
	}
	l.sideFill.SetMinSize(fyne.NewSize(w, 0))
	size := l.side.Size()
	l.sideFill.Resize(fyne.NewSize(max(w, size.Width), size.Height)) // the offset is kept within its size
	l.side.Refresh()
	if l.rowsBox != nil {
		l.rowsBox.Layout.Layout(l.rowsBox.Objects, l.rowsBox.Size())
	}
	l.followSide()
}

// followSide takes the scroller's offset: it changes it without OnScrolled when its content
// shrinks to its width (a resize, wrapping turned on).
func (l *Logs) followSide() { l.setShift(l.side.Offset.X) }

// setShift scrolls the rows sideways to x.
func (l *Logs) setShift(x float32) {
	if x == l.shift {
		return
	}
	l.shift = x
	l.redrawRows()
}

// redrawRows draws the rows on screen again, as the list would on a refresh.
func (l *Logs) redrawRows() {
	for id, r := range l.shownRows {
		if r.item == id {
			r.left = l.leftOf(id)
			r.set(l.itemRuns(id), l.itemSel(id))
		}
	}
}

// sideCol is the first character the rows draw while they do not wrap, and where it is drawn: at
// the row's left, or left of it by the part of it scrolled out.
func (l *Logs) sideCol() (col int, left float32) {
	cw := charWidth(l.list.Theme())
	col = int(l.shift / cw)
	return col, float32(col)*cw - l.shift
}

// leftOf is where list item id's text begins.
func (l *Logs) leftOf(id widget.ListItemID) float32 {
	if l.view.Wrap || id < l.headRows() {
		return 0
	}
	_, left := l.sideCol()
	return left
}

// window returns the part of a line a row draws while the rows do not wrap: from the first
// character in view, twice as many as the view is wide.
func (l *Logs) window(text string) (from, to int) {
	col, _ := l.sideCol()
	from = runeOffset(text, 0, col)
	return from, runeOffset(text, from, 2*l.cols+2)
}

// runeOffset returns the byte offset n characters after byte from of text, or text's end.
func runeOffset(text string, from, n int) int {
	for ; n > 0 && from < len(text); n-- {
		if text[from] < utf8.RuneSelf {
			from++
			continue
		}
		_, size := utf8.DecodeRuneInString(text[from:])
		from += size
	}
	return from
}

var _ fyne.Scrollable = (*logSurface)(nil)

// Scrolled implements fyne.Scrollable: a scroll's vertical part goes to the list, its sideways
// part to the rows.
func (s *logSurface) Scrolled(ev *fyne.ScrollEvent) {
	l := s.l
	if dy := ev.Scrolled.DY; dy != 0 && l.list.scroll != nil {
		l.list.scroll.Scrolled(&fyne.ScrollEvent{PointEvent: ev.PointEvent, Scrolled: fyne.Delta{DY: dy}})
	}
	if dx := ev.Scrolled.DX; dx != 0 {
		l.side.Scrolled(&fyne.ScrollEvent{PointEvent: ev.PointEvent, Scrolled: fyne.Delta{DX: dx}})
	}
}
