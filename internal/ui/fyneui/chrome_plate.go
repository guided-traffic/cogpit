package fyneui

import (
	"image"
	"image/color"
	"math"

	"fyne.io/fyne/v2/canvas"
)

// plate is a rounded square lit from above: the identity colour as a vertical gradient from a
// strong alpha at the top to a weak one at the bottom, with a 1 point inner highlight in white. The
// hotbar's tiles are plates. Fyne's gradients have no rounded corners, so a plate is drawn into a
// raster at the display's pixel size, once per Refresh: a hotbar has a handful of tiles, not
// thousands of rows, so a raster each costs nothing that matters.
type plate struct {
	img       *canvas.Raster
	col       color.NRGBA
	top, base uint8   // the gradient's alpha at the top and at the bottom
	radius    float32 // in points
	highlight float32 // the inner highlight's alpha, 0 to 1
}

func newPlate() *plate {
	p := &plate{col: color.NRGBA{A: 0}, highlight: 0.08}
	p.img = canvas.NewRaster(p.draw)
	p.img.ScaleMode = canvas.ImageScaleSmooth
	return p
}

// set changes the plate's look and redraws it.
func (p *plate) set(c color.Color, top, base uint8, radius float32) {
	p.col = color.NRGBAModel.Convert(c).(color.NRGBA)
	p.top, p.base, p.radius = top, base, radius
	p.img.Refresh()
}

// draw paints the gradient through a rounded rectangle's coverage.
func (p *plate) draw(pw, ph int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, pw, ph))
	if pw < 2 || ph < 2 || p.col.A == 0 && p.top == 0 {
		return img
	}
	scale := float64(pw) / math.Max(1, float64(p.img.Size().Width))
	r := math.Min(float64(p.radius)*scale, math.Min(float64(pw), float64(ph))/2)
	hw, hh := float64(pw)/2, float64(ph)/2
	band := 1 * scale
	for y := range ph {
		t := (float64(y) + 0.5) / float64(ph)
		a := (float64(p.top) + (float64(p.base)-float64(p.top))*t) / 255
		for x := range pw {
			// The signed distance to the rounded rectangle's edge, negative inside.
			dx := math.Abs(float64(x)+0.5-hw) - (hw - r)
			dy := math.Abs(float64(y)+0.5-hh) - (hh - r)
			d := math.Hypot(math.Max(dx, 0), math.Max(dy, 0)) + math.Min(math.Max(dx, dy), 0) - r
			cov := math.Min(1, math.Max(0, 0.5-d))
			if cov == 0 {
				continue
			}
			oa := a * cov
			rr, gg, bb := float64(p.col.R), float64(p.col.G), float64(p.col.B)
			if -d < band { // the inner highlight: white over the plate along its edge
				h := float64(p.highlight) * cov * math.Min(1, band+d+1)
				na := oa + h*(1-oa)
				rr = (rr*oa*(1-h) + 255*h) / na
				gg = (gg*oa*(1-h) + 255*h) / na
				bb = (bb*oa*(1-h) + 255*h) / na
				oa = na
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(rr), G: uint8(gg), B: uint8(bb), A: uint8(255 * math.Min(1, oa))})
		}
	}
	return img
}
