package fyneui

import (
	"image"
	"image/color"
	"math"
	"testing"

	"fyne.io/fyne/v2"
)

// zigzag is a metrics-like line at fractional positions: steps of 7.3 units up and down by up to
// half a step, so joints fall anywhere within a pixel.
func zigzag() []fyne.Position {
	var pts []fyne.Position
	for i := range 40 {
		y := 40.35 + 3.4*float32(math.Sin(float64(i)*1.7))
		pts = append(pts, fyne.NewPos(10.4+7.3*float32(i), y))
	}
	return pts
}

// columnCoverage sums each pixel column's alpha (in pixels of full coverage) over the image.
func columnCoverage(img *image.RGBA) []float64 {
	b := img.Bounds()
	out := make([]float64, b.Dx())
	for x := b.Min.X; x < b.Max.X; x++ {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			out[x-b.Min.X] += float64(img.RGBAAt(x, y).A) / 255
		}
	}
	return out
}

// A series' line is one stroke (WF23, D-89): every column of it is as thick as the line's width
// across its slope (width × √(1+m²) of the segment the column lies on), at the joints too, on a 1×
// and on a 2× screen. Separate antialiased segments thinned the line where they met.
func TestChartLineKeepsItsWidth(t *testing.T) {
	pts := zigzag()
	for _, scale := range []float32{1, 2} {
		l := newChartLayer(true, chartLineWidth)
		l.place(0, 0, 310, 80, 2*chartLineWidth, 80, []chartPath{{pts: pts, color: color.NRGBA{R: 0x20, G: 0xc0, B: 0xb0, A: 0xff}}})
		img := l.draw(int(l.size.Width*scale), int(l.size.Height*scale)).(*image.RGBA)
		cov := columnCoverage(img)
		worst := 0.0
		for px := range cov {
			ux := l.origin.X + (float32(px)+0.5)/scale // the column's centre, in units
			if ux < pts[0].X+2 || ux > pts[len(pts)-1].X-2 {
				continue // the caps
			}
			// The slopes of the segments within a pixel of the column: at a joint, the stroke's
			// thickness lies between the two segments'.
			lo, hi := math.Inf(1), 0.0
			for i := 1; i < len(pts); i++ {
				a, b := pts[i-1], pts[i]
				if ux+1/scale < a.X || ux-1/scale > b.X {
					continue
				}
				m := float64((b.Y - a.Y) / (b.X - a.X))
				want := float64(chartLineWidth*scale) * math.Sqrt(1+m*m)
				lo, hi = min(lo, want), max(hi, want)
			}
			got := cov[px]
			dev := max(lo*0.9-got, got-hi*1.1-0.15*float64(scale), 0) / lo
			worst = max(worst, dev)
			if dev > 0 {
				t.Errorf("scale %v: column %d (x %.2f) covers %.2f px, want %.2f to %.2f", scale, px, ux, got, lo, hi)
			}
		}
		if t.Failed() {
			t.Logf("scale %v: worst deviation %.0f%%", scale, worst*100)
		}
	}
}

// The layer redraws into the same image while its size stays: a poll allocates nothing of the
// plot's size.
func TestChartLayerReusesItsImage(t *testing.T) {
	l := newChartLayer(true, chartLineWidth)
	l.place(0, 0, 310, 80, 3, 80, []chartPath{{pts: zigzag(), color: color.White}})
	first := l.draw(400, 100)
	if again := l.draw(400, 100); again != first {
		t.Error("a redraw at the same size made a new image")
	}
	if other := l.draw(800, 200); other == first || other.Bounds().Dx() != 800 {
		t.Error("a redraw at a new size kept the old image")
	}
	if img := l.draw(0, 0); !img.Bounds().Empty() {
		t.Error("an empty layer drew something")
	}
}
