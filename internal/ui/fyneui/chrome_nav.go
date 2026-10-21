package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

// The navigator's selection in the Dark Cockpit design: the open item is a rounded plate, inset
// from the sidebar's edges, in the selection fill with a 2 point bar in the glow blue at its inner
// left edge. The bar says "you are here" in the colour that means Cogpit and you; the fill alone
// would be a quiet blue on blue.
const (
	navPlateInset = 4 // the plate's distance from the sidebar's left and right edge
	navPlateGap   = 1 // and from the row's top and bottom
	navBarWidth   = 2
	navBarInset   = 8 // the bar's distance from the plate's top and bottom
)

// newSelBar returns the bar of a selected row, hidden until paintSelBar shows it.
func newSelBar() *canvas.Rectangle {
	b := canvas.NewRectangle(color.Transparent)
	b.CornerRadius = navBarWidth / 2
	b.Hide()
	return b
}

// paintSelBar shows the bar in the theme's primary colour while the row is the open one.
func paintSelBar(th fyne.Theme, bar *canvas.Rectangle, on bool) {
	if bar == nil {
		return
	}
	bar.FillColor = themeColor(th, theme.ColorNamePrimary)
	if on {
		bar.Show()
	} else {
		bar.Hide()
	}
	bar.Refresh()
}

// layoutPlate places a row's plate and focus ring inset in size and the bar at the plate's inner
// left edge.
func layoutPlate(bg, ring, bar *canvas.Rectangle, size fyne.Size) {
	pos := fyne.NewPos(navPlateInset, navPlateGap)
	sz := fyne.NewSize(max(0, size.Width-2*navPlateInset), max(0, size.Height-2*navPlateGap))
	for _, o := range []*canvas.Rectangle{bg, ring} {
		o.Move(pos)
		o.Resize(sz)
	}
	if bar != nil {
		bar.Move(fyne.NewPos(navPlateInset, navPlateGap+navBarInset/2))
		bar.Resize(fyne.NewSize(navBarWidth, max(0, sz.Height-navBarInset)))
	}
}
