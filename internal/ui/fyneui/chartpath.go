package fyneui

import (
	"image"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"github.com/srwiley/rasterx"
)

// A chart's series are drawn as paths (WF23, D-89): each run of a series' points (a line between
// gaps, chartgap.go) is one continuous stroke with round joins and round caps, rasterised with
// rasterx (Fyne's own vector rasteriser, already in the module) into a raster at the screen's
// pixel size, so a line is as thick on a slope as on the flat, at a joint as between two, and as
// crisp on a 2× screen as on a 1×. Separate canvas.Line segments, which the charts drew before,
// are antialiased each on its own without a join, so the line's apparent width changed with its
// slope and where the joints fell. The area under each run (chartfill.go) is a filled path of the
// same points down to zero, rasterised the same way, so its top edge lies exactly under the line.
//
// A chart has two such layers: the fills, beneath the event markers and the reference lines, and
// the lines above them. Each keeps its raster object, its image and its scanner across layouts and
// polls: a poll redraws the layer into the same image (no allocation of the plot's size while the
// size stays), and the hover, which changes only the overlay (chart_hover.go), redraws neither.

// chartPath is one run of a series' points, in the chart's coordinates, and its colour.
type chartPath struct {
	pts   []fyne.Position
	color color.Color
}

// seriesPaths are the series' lines and the areas under them as paths in the chart's coordinates
// (geometry g), one per run of points (chartgap.go), at most chartMaxSegmentsPerSer segments a
// series; a run of a single point is a dot instead.
func (r *chartRenderer) seriesPaths(g chartGeom) (lines, fills []chartPath, dots []fyne.CanvasObject) {
	th := r.c.Theme()
	d := r.c.data
	for _, s := range d.Series {
		c := themeColor(th, fallback(s.Color, colorAccent))
		segs := 0
		for _, run := range seriesRuns(s.Points, d.From, d.To) {
			pts := s.Points[run[0]:run[1]]
			if len(pts) == 1 {
				dot := canvas.NewCircle(c)
				dot.Move(fyne.NewPos(g.x(pts[0].At)-chartPointRadius, g.y(pts[0].Value)-chartPointRadius))
				dot.Resize(fyne.NewSize(2*chartPointRadius, 2*chartPointRadius))
				dots = append(dots, dot)
				continue
			}
			if segs >= chartMaxSegmentsPerSer {
				continue
			}
			pts = pts[:min(len(pts), chartMaxSegmentsPerSer-segs+1)]
			segs += len(pts) - 1
			path := make([]fyne.Position, len(pts))
			for i, p := range pts {
				path[i] = fyne.NewPos(g.x(p.At), g.y(p.Value))
			}
			lines = append(lines, chartPath{pts: path, color: c})
			fills = append(fills, chartPath{pts: path, color: withAlpha(c, chartFillAlpha)})
		}
	}
	return lines, fills, dots
}

// chartLayer is a raster over the plot that draws paths: strokes (the series' lines) or the areas
// under them down to base (the plot's y of zero).
type chartLayer struct {
	raster *canvas.Raster
	stroke bool
	width  float32 // a stroke's width, in units

	origin fyne.Position // the raster's top-left, in the chart's coordinates
	size   fyne.Size
	base   float32
	paths  []chartPath

	img  *image.RGBA
	scan *rasterx.ScannerGV
}

func newChartLayer(stroke bool, width float32) *chartLayer {
	l := &chartLayer{stroke: stroke, width: width}
	l.raster = canvas.NewRaster(l.draw)
	return l
}

// place puts the layer over the rectangle from (x0, y0) to (x1, y1) in the chart, widened by pad
// and rounded out to whole units (whole pixels on a 1× and a 2× screen, so the raster's pixels
// meet the screen's), with the paths it draws next. The paths' points stay the caller's.
func (l *chartLayer) place(x0, y0, x1, y1, pad, base float32, paths []chartPath) {
	ox, oy := floorUnit(x0-pad), floorUnit(y0-pad)
	l.origin = fyne.NewPos(ox, oy)
	l.size = fyne.NewSize(max(ceilUnit(x1+pad)-ox, 1), max(ceilUnit(y1+pad)-oy, 1))
	l.base, l.paths = base, paths
	l.raster.Move(l.origin)
	l.raster.Resize(l.size)
}

func floorUnit(v float32) float32 { return float32(math.Floor(float64(v))) }
func ceilUnit(v float32) float32  { return float32(math.Ceil(float64(v))) }

// draw is the raster's generator: the layer's paths at w × h pixels. It runs on the main
// goroutine, when the window paints the layer anew (a refresh or a new size), and reuses the image
// and the scanner while the size stays.
func (l *chartLayer) draw(w, h int) image.Image {
	if w <= 0 || h <= 0 || l.size.Width <= 0 || l.size.Height <= 0 {
		return image.NewRGBA(image.Rectangle{})
	}
	if l.img == nil || l.img.Rect.Dx() != w || l.img.Rect.Dy() != h {
		l.img = image.NewRGBA(image.Rect(0, 0, w, h))
		l.scan = rasterx.NewScannerGV(w, h, l.img, l.img.Bounds())
	} else {
		clear(l.img.Pix)
	}
	sx, sy := float64(w)/float64(l.size.Width), float64(h)/float64(l.size.Height)
	at := func(p fyne.Position) (float64, float64) {
		return float64(p.X-l.origin.X) * sx, float64(p.Y-l.origin.Y) * sy
	}
	// The stroke's width in the rasteriser's fixed point (ToFixedP's X), at the pixels' scale.
	width := rasterx.ToFixedP(float64(l.width)*(sx+sy)/2, 0).X
	for _, p := range l.paths {
		if len(p.pts) < 2 {
			continue
		}
		l.scan.Clear()
		l.scan.SetColor(p.color)
		first, last := p.pts[0], p.pts[len(p.pts)-1]
		if l.stroke {
			s := rasterx.NewStroker(w, h, l.scan)
			s.SetStroke(width, 4<<6, rasterx.RoundCap, rasterx.RoundCap, rasterx.RoundGap, rasterx.Round)
			s.Start(rasterx.ToFixedP(at(first)))
			for _, q := range p.pts[1:] {
				s.Line(rasterx.ToFixedP(at(q)))
			}
			s.Stop(false)
			s.Draw()
			continue
		}
		f := rasterx.NewFiller(w, h, l.scan)
		f.Start(rasterx.ToFixedP(at(first)))
		for _, q := range p.pts[1:] {
			f.Line(rasterx.ToFixedP(at(q)))
		}
		f.Line(rasterx.ToFixedP(at(fyne.NewPos(last.X, l.base))))
		f.Line(rasterx.ToFixedP(at(fyne.NewPos(first.X, l.base))))
		f.Stop(true)
		f.Draw()
	}
	return l.img
}
