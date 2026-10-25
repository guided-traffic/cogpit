package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// The drawer's row's outline (the owner's VW16 of 2026-10-25, his pick (a); list_rows.go): one
// rounded rectangle round the whole row, although a Fyne table draws its cells one by one. Each
// cell draws its part: the top and the bottom edge in every cell, reaching across the gaps to the
// next cell's, and in the first cell (the checkbox's) the left side with its two rounded corners, in
// the last (the ⋮'s) the right side with its two.
//
// The parts are filled shapes that need no clip: straight edges are rectangles and a corner is a
// quarter ring (canvas.Arc). A stroked rectangle cut to the cell by a container.Clip would draw the
// same in one object, but Fyne's software painter, which the render tests use, misplaces a stroked
// shape that a clip cuts on its left or top (drawOblongStroke, internal/painter/software/draw.go:330
// in v2.8.2, offsets the source only for negative coordinates), so the pictures would not show what
// the GL painter draws.
//
// The outline lies on the bounds of the row's cells, not in the gaps around them: the table's
// scroller cuts off what lies left of the first column and above the first row, which would take
// the first row's top edge and every row's left side away.

// rowOutline is a cell's part of the outline, made the first time the cell is on the drawer's row.
type rowOutline struct {
	box                      *fyne.Container // every part; hidden off the drawer's row
	top, bottom, left, right *canvas.Rectangle
	corners                  [4]*canvas.Arc // top left, bottom left, top right, bottom right
	color                    color.Color    // the colour painted last
}

// The outline's joins, in points.
const (
	outlineSeam    = 1   // an edge reaches past the gap into the next cell's, so that two cells' edges meet without a gap
	outlineOverlap = 0.5 // a straight edge reaches into its corner, so that the two meet without a seam
)

func newRowOutline() *rowOutline {
	o := &rowOutline{
		top: canvas.NewRectangle(color.Transparent), bottom: canvas.NewRectangle(color.Transparent),
		left: canvas.NewRectangle(color.Transparent), right: canvas.NewRectangle(color.Transparent),
	}
	objs := make([]fyne.CanvasObject, 0, 8)
	objs = append(objs, o.top, o.bottom, o.left, o.right)
	for i, start := range [4]float32{270, 180, 0, 90} { // Fyne's angles: 0 at the top, clockwise
		o.corners[i] = canvas.NewArc(start, start+90, 0, color.Transparent)
		objs = append(objs, o.corners[i])
	}
	o.box = container.NewWithoutLayout(objs...)
	return o
}

// paint colours every part; the same colour again costs nothing (a hover on the row repaints its
// cells).
func (o *rowOutline) paint(c color.Color) {
	if c == o.color {
		return
	}
	o.color = c
	for _, r := range []*canvas.Rectangle{o.top, o.bottom, o.left, o.right} {
		r.FillColor = c
		r.Refresh()
	}
	for _, a := range o.corners {
		a.FillColor = c
		a.Refresh()
	}
}

// layout places the parts in a cell of size whose row's cells stand gap apart, with corners of
// radius; first and last say whether the cell begins or ends the row.
func (o *rowOutline) layout(size fyne.Size, gap, radius float32, first, last bool) {
	o.box.Resize(size)
	r := min(radius, size.Height/2, size.Width/2)
	x0, x1 := -gap/2, size.Width+gap/2+outlineSeam
	if first {
		x0 = r - outlineOverlap
	}
	if last {
		x1 = size.Width - r + outlineOverlap
	}
	placeOutline(o.top, x0, 0, x1-x0, outlineWidth)
	placeOutline(o.bottom, x0, size.Height-outlineWidth, x1-x0, outlineWidth)
	side := max(0, size.Height-2*r+2*outlineOverlap)
	placeOutline(o.left, 0, r-outlineOverlap, outlineWidth, side)
	placeOutline(o.right, size.Width-outlineWidth, r-outlineOverlap, outlineWidth, side)
	setVisible(o.left, first)
	setVisible(o.right, last)
	at := [4]fyne.Position{{X: 0, Y: 0}, {X: 0, Y: size.Height - 2*r}, {X: size.Width - 2*r, Y: 0}, {X: size.Width - 2*r, Y: size.Height - 2*r}}
	for i, a := range o.corners {
		a.CutoutRatio = 0
		if r > 0 {
			a.CutoutRatio = max(0, r-outlineWidth) / r // a ring as wide as the edges
		}
		a.Move(at[i])
		a.Resize(fyne.NewSize(2*r, 2*r))
		setVisible(a, i < 2 && first || i >= 2 && last)
	}
}

// placeOutline moves and sizes a part of the outline.
func placeOutline(o fyne.CanvasObject, x, y, w, h float32) {
	o.Move(fyne.NewPos(x, y))
	o.Resize(fyne.NewSize(max(0, w), max(0, h)))
}
