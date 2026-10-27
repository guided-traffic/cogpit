package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// The masonry puts each card in the column that is shortest at its turn, as many columns of at least
// min points as the width holds, the cards as wide as a column; its MinSize is the height at the
// width it was told.
func TestMasonry(t *testing.T) {
	card := func(h float32) fyne.CanvasObject {
		r := canvas.NewRectangle(nil)
		r.SetMinSize(fyne.NewSize(10, h))
		return r
	}
	objs := []fyne.CanvasObject{card(100), card(40), card(30), card(50)}
	m := &masonry{min: 360, gap: 8}
	if n, w := m.columns(736); n != 2 || w != 364 {
		t.Errorf("736 points: %d columns of %v", n, w)
	}
	if n, _ := m.columns(300); n != 1 {
		t.Errorf("300 points: %d columns", n)
	}
	m.setWidth(736)
	// The first column holds 100; the second 40, then 30 under it (78), then 50 under that (136).
	if ms := m.MinSize(objs); ms.Height != 136 {
		t.Errorf("height %v", ms.Height)
	}
	m.Layout(objs, fyne.NewSize(736, 136))
	want := []fyne.Position{{X: 0, Y: 0}, {X: 372, Y: 0}, {X: 372, Y: 48}, {X: 372, Y: 86}}
	for i, o := range objs {
		if o.Position() != want[i] || o.Size().Width != 364 {
			t.Errorf("card %d at %v, %v wide; want %v", i, o.Position(), o.Size().Width, want[i])
		}
	}
}
