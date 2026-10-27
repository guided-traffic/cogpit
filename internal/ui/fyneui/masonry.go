package fyneui

import (
	"fyne.io/fyne/v2"
)

// masonry lays out cards in columns, each card in the column that is shortest when its turn comes
// (the alert board, the owner, 2026-10-27: "ein Dashboard im Masonry-Layout"): as many columns of
// at least min points as the width holds, gap points apart, the cards as wide as a column and as
// high as their content at that width. The cards keep their order: the first of them are at the top
// of the columns. Its height depends on its width, which its MinSize cannot be told: whoever lays
// out its scroller tells it first (setWidth), so that the scroller asks for the right height.
type masonry struct {
	min, gap float32
	width    float32 // the width it is laid out at, set before the scroller asks for its MinSize
}

// setWidth tells the masonry the width it will be laid out at.
func (m *masonry) setWidth(w float32) { m.width = w }

// columns returns how many columns a width holds and how wide they are.
func (m *masonry) columns(w float32) (int, float32) {
	n := max(1, int((w+m.gap)/(m.min+m.gap)))
	return n, (w - float32(n-1)*m.gap) / float32(n)
}

// place lays the visible objects out at width w (moving and sizing them when move is set) and
// returns the height they take.
func (m *masonry) place(objs []fyne.CanvasObject, w float32, move bool) float32 {
	if w <= 0 {
		w = m.min
	}
	n, colW := m.columns(w)
	heights := make([]float32, n)
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		if o.Size().Width != colW { // its text wraps at the column's width: lay it out there first
			o.Resize(fyne.NewSize(colW, o.MinSize().Height))
		}
		h := o.MinSize().Height
		col := 0
		for i := range heights {
			if heights[i] < heights[col] {
				col = i
			}
		}
		if move {
			o.Move(fyne.NewPos(float32(col)*(colW+m.gap), heights[col]))
			o.Resize(fyne.NewSize(colW, h))
		}
		heights[col] += h + m.gap
	}
	tallest := float32(0)
	for _, h := range heights {
		tallest = max(tallest, h-m.gap)
	}
	return max(0, tallest)
}

// Layout implements fyne.Layout.
func (m *masonry) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	m.width = size.Width
	m.place(objs, size.Width, true)
}

// MinSize implements fyne.Layout: a column wide, and as high as the cards are at the width it was
// told.
func (m *masonry) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(m.min, m.place(objs, m.width, false))
}
