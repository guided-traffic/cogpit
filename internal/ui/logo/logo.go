// Package logo draws Cogpit's logo, every version of it there has been (versions.go): today the
// "C helm" of D-24, an open C around a ship's wheel with seven spokes and a needle in the C's
// opening, a hexagon hub, an inner ring, bar handles outside the C and a thin outer ring, on a
// rounded-square plate, the C glowing over the wheel. The geometry is the one of the reference SVGs
// (docs/developer/assets/logo/*.svg), in a 100×100 box; a C helm's colours, weights and light are
// the icon lab's settings (Look, docs/developer/assets/logo/*.json). The package rasterises with
// supersampling and blurs the glow with a Gaussian, and imports neither Fyne nor any other Cogpit
// package, so that the app icon generator (hack/icon) and the GUI (internal/ui/fyneui: the header,
// the welcome page, About, the README's banner) draw the same logo.
//
// Layer L4 (internal/ui).
package logo

import (
	"image"
	"image/color"
	"math"
)

// The geometry, in the reference SVGs' units: a 100×100 box, the wheel's centre at (50, 50).
const (
	Box = 100.0 // the box's side

	centre    = 50.0
	outerR    = 41.0 // the thin outer ring
	cR        = 28.0 // the C's radius
	cOpening  = 75.0 // the C's opening, in degrees, to the right
	innerR    = 12.0 // the inner ring
	spokeBeg  = 12.0 // a spoke runs from the inner ring
	spokeEnd  = 27.0 // to just inside the C
	handleBeg = 34.0 // a bar handle stands outside the C, from here
	handleEnd = 40.0 // to here
	needleEnd = 89.0 // the needle runs from the hub, through the C's opening, to here
	spokes    = 8    // spokes in all; the needle takes the place of the one pointing right

	// The plate: a rounded square, the radius the one fyne package masks an icon with.
	plateInset  = 2.0
	plateSide   = Box - 2*plateInset
	plateRadius = 22.0
	// The rim: a white line just inside the plate's edge.
	rimInset  = 2.5
	rimRadius = 21.5
	// The halo: a radial light in the C's colour around the wheel, as the lab's radialGradient.
	haloR = 48.0
)

// CornerRatio is the plate's corner radius over its side (22 on 96, about 23 %): a macOS app icon's
// rounding, which Cogpit's own icon has. The window rounds a cluster's icon by it at every size, an
// image and a colour tile alike (the owner's VW10 of 2026-10-24: "wie bei MacOS üblich, so wie unser
// cogpit-icon").
const CornerRatio = plateRadius / plateSide

// Look is a C helm in the icon lab's terms (D-24): each field is the lab's setting of the same name
// (in its JSON, docs/developer/assets/logo/*.json), in the lab's units: box units for weights and
// radii, 0…1 for strengths and opacities.
type Look struct {
	C           color.NRGBA // cColor: the C
	CWeight     float64     // cWeight: the C's stroke
	Handle      color.NRGBA // handleColor: the seven handles outside the C and the outer ring
	Glow        float64     // glow: the strength of the C's glow, which lies over the wheel
	GlowRadius  float64     // glowRadius: the glow's blur, a standard deviation
	Core        float64     // core: the C's bright core line
	Halo        float64     // halo: the plate's light around the wheel
	Accent      color.NRGBA // accent: the wheel, i.e. the inner ring, the spokes, the needle and the hub
	SpokeWeight float64     // spokeWeight: the wheel's and the handles' strokes, a factor on D-24's
	AccentGlow  float64     // accentGlow: the wheel's own glow, under the C's
	Outer       float64     // outer: the outer ring's opacity
	PlateTop    color.NRGBA // plateTop: the plate's colour at its top edge
	PlateBottom color.NRGBA // plateBottom: and at its bottom edge, a vertical gradient between them
	Rim         float64     // rim: the opacity of the white line inside the plate's edge
}

// strokes are a look's line weights in box units, derived as the lab derives them: the handles and
// the outer ring follow the spoke factor by 60 %, the hub by 35 %.
type strokes struct{ outer, inner, spoke, handle, needle, hub float64 }

func (l Look) strokes() strokes {
	sw := l.SpokeWeight
	hw := 1 + (sw-1)*.6
	return strokes{outer: .9 * hw, inner: 2 * sw, spoke: 1.6 * sw, handle: 2.6 * hw, needle: 2.2 * sw,
		hub: 5.13 * (1 + (sw-1)*.35)}
}

const samples = 4 // per axis: 16 samples a pixel

// premultiplied colour accumulators, in 0..1
type pm struct{ r, g, b, a float64 }

func (d *pm) over(c pm) {
	k := 1 - c.a
	d.r, d.g, d.b, d.a = c.r+d.r*k, c.g+d.g*k, c.b+d.b*k, c.a+d.a*k
}

func (d pm) times(k float64) pm { return pm{d.r * k, d.g * k, d.b * k, d.a * k} }

func ink(c color.Color, opacity float64) pm {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	a := float64(n.A) / 255 * opacity
	return pm{float64(n.R) / 255 * a, float64(n.G) / 255 * a, float64(n.B) / 255 * a, a}
}

var white = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}

// mix is the lab's colour mix: a toward b by t, each channel rounded to 8 bits.
func mix(a, b color.NRGBA, t float64) color.NRGBA {
	m := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return color.NRGBA{R: m(a.R, b.R), G: m(a.G, b.G), B: m(a.B, b.B), A: 0xff}
}

// lerp is a gradient's colour at t, unrounded.
func lerp(a, b color.NRGBA, t float64) pm {
	m := func(x, y uint8) float64 { return (float64(x) + (float64(y)-float64(x))*t) / 255 }
	return pm{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 1}
}

// layer is one painted shape, back to front. A shape paints where it covers; a light is a blurred
// shape, its coverage per pixel of the drawn area times its ink. A clipped layer paints inside the
// plate only, as the lab's clip-path has it.
type layer struct {
	covers  func(x, y float64) bool
	paint   func(x, y float64) pm
	light   []float64
	ink     pm
	clipped bool
}

func always(float64, float64) bool { return true }

func flat(p pm) func(float64, float64) pm { return func(float64, float64) pm { return p } }

// area is the part of an image a drawing touches, and where the box lies on it: the box's point
// (x, y) is the pixel (ox + x·scale, oy + y·scale).
type area struct {
	x0, y0, w, h  int
	scale, ox, oy float64
}

// sample is the box point of the sample (sx, sy) of the pixel (px, py).
func (a area) sample(px, py, sx, sy int) (x, y float64) {
	return (float64(px) + (float64(sx)+.5)/samples - a.ox) / a.scale, (float64(py) + (float64(sy)+.5)/samples - a.oy) / a.scale
}

// light is a shape's coverage of each pixel of the area, blurred with a Gaussian of the standard
// deviation sigma (box units), as the lab's SVG blurs its glow (feGaussianBlur).
func (a area) light(covers func(x, y float64) bool, sigma float64) []float64 {
	v := make([]float64, a.w*a.h)
	for py := range a.h {
		for px := range a.w {
			n := 0
			for sy := range samples {
				for sx := range samples {
					if covers(a.sample(a.x0+px, a.y0+py, sx, sy)) {
						n++
					}
				}
			}
			v[py*a.w+px] = float64(n) / (samples * samples)
		}
	}
	blur(v, a.w, a.h, sigma*a.scale)
	return v
}

// layers returns a C helm's shapes back to front, as the lab's SVG paints them: the plate and its
// halo, the outer ring, the wheel and the handles, the C's glow over them, the C and its core, and
// the rim over the plate's edge.
func (l Look) layers(a area) []layer {
	st := l.strokes()
	var ls []layer
	shape := func(covers func(x, y float64) bool, p pm) {
		ls = append(ls, layer{covers: covers, paint: flat(p), clipped: true})
	}
	light := func(covers func(x, y float64) bool, sigma float64, p pm) {
		ls = append(ls, layer{light: a.light(covers, sigma), ink: p, clipped: true})
	}
	tight := math.Max(.3, l.GlowRadius*.4) // the inner glow's blur

	ls = append(ls, layer{covers: always, clipped: true, paint: func(_, y float64) pm {
		return lerp(l.PlateTop, l.PlateBottom, math.Max(0, math.Min(1, (y-plateInset)/plateSide)))
	}})
	if l.Halo > 0 {
		ls = append(ls, layer{covers: always, clipped: true, paint: func(x, y float64) pm {
			return ink(l.C, l.Halo*haloOpacity(math.Hypot(x-centre, y-centre)/haloR))
		}})
	}
	shape(ring(outerR, st.outer), ink(l.Handle, l.Outer))
	wheel := wheelCovers(st)
	if l.AccentGlow > 0 {
		light(wheel, tight, ink(mix(l.Accent, white, .2), l.AccentGlow))
	}
	shape(wheel, ink(l.Accent, 1))
	shape(handlesCover(st.handle), ink(l.Handle, 1))
	if l.Glow > 0 {
		glow := mix(l.C, white, .18)
		light(arc(l.CWeight+2), l.GlowRadius, ink(glow, l.Glow*.8))
		light(arc(l.CWeight), tight, ink(glow, l.Glow))
	}
	shape(arc(l.CWeight), ink(l.C, 1))
	if l.Core > 0 {
		shape(arc(l.CWeight*.34), ink(mix(l.C, white, .72), l.Core))
	}
	if l.Rim > 0 {
		ls = append(ls, layer{paint: flat(ink(white, l.Rim)), covers: func(x, y float64) bool {
			return math.Abs(roundRect(x, y, rimInset, Box-2*rimInset, rimRadius)) <= .5
		}})
	}
	return ls
}

// haloOpacity is the halo's opacity at t, the distance from the centre in haloR: the lab's
// gradient stops .38 at the centre, .26 at 0.6 and nothing at 1.
func haloOpacity(t float64) float64 {
	switch {
	case t <= .6:
		return .38 - .12*t/.6
	case t <= 1:
		return .26 * (1 - (t-.6)/.4)
	}
	return 0
}

// ring is the cover of a circle round the centre with radius r and stroke w.
func ring(r, w float64) func(x, y float64) bool {
	return func(x, y float64) bool { return math.Abs(math.Hypot(x-centre, y-centre)-r) <= w/2 }
}

// arc is the cover of the C with stroke w.
func arc(w float64) func(x, y float64) bool {
	half := cOpening / 2 * math.Pi / 180
	return func(x, y float64) bool { return nearArc(x, y, half, w/2) }
}

// wheelCovers is the cover of the wheel: the inner ring, the seven spokes every 45° but to the
// right, where the needle is, the needle and the hexagon hub, a vertex up and one down.
func wheelCovers(st strokes) func(x, y float64) bool {
	parts := []func(x, y float64) bool{ring(innerR, st.inner), segment(centre, centre, needleEnd, centre, st.needle/2)}
	for k := 1; k < spokes; k++ {
		dx, dy := direction(k)
		parts = append(parts, segment(centre+spokeBeg*dx, centre+spokeBeg*dy, centre+spokeEnd*dx, centre+spokeEnd*dy, st.spoke/2))
	}
	return anyOf(append(parts, hexagon(st.hub)))
}

// hexagon is the cover of the hub: a hexagon of the circumradius r round the centre, a vertex up
// and one down, i.e. vertices at 30° + k·60°.
func hexagon(r float64) func(x, y float64) bool {
	var hex [6][2]float64
	for k := range hex {
		a := (30 + float64(k)*60) * math.Pi / 180
		hex[k] = [2]float64{centre + r*math.Cos(a), centre + r*math.Sin(a)}
	}
	return func(x, y float64) bool { return inConvex(hex[:], x, y) }
}

// handlesCover is the cover of the seven bar handles outside the C, one beyond each spoke.
func handlesCover(w float64) func(x, y float64) bool {
	var parts []func(x, y float64) bool
	for k := 1; k < spokes; k++ {
		dx, dy := direction(k)
		parts = append(parts, segment(centre+handleBeg*dx, centre+handleBeg*dy, centre+handleEnd*dx, centre+handleEnd*dy, w/2))
	}
	return anyOf(parts)
}

// direction is the unit vector of the k-th spoke, in screen direction: 0 points right.
func direction(k int) (dx, dy float64) {
	a := float64(k) * 2 * math.Pi / spokes
	return math.Cos(a), math.Sin(a)
}

func anyOf(parts []func(x, y float64) bool) func(x, y float64) bool {
	return func(x, y float64) bool {
		for _, p := range parts {
			if p(x, y) {
				return true
			}
		}
		return false
	}
}

// draw paints layers onto img, which it does not clear: the box's point (x, y) lands on the pixel
// (ox + x·scale, oy + y·scale).
func draw(img *image.NRGBA, layers func(area) []layer, scale, ox, oy float64) {
	b := img.Bounds()
	// Only the box's pixels (and a margin for anti-aliasing) can be touched.
	x0, x1 := max(b.Min.X, int(math.Floor(ox))-1), min(b.Max.X, int(math.Ceil(ox+Box*scale))+1)
	y0, y1 := max(b.Min.Y, int(math.Floor(oy))-1), min(b.Max.Y, int(math.Ceil(oy+Box*scale))+1)
	if x1 <= x0 || y1 <= y0 {
		return
	}
	a := area{x0: x0, y0: y0, w: x1 - x0, h: y1 - y0, scale: scale, ox: ox, oy: oy}
	ls := layers(a)
	const n = samples * samples
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			i := (py-y0)*a.w + px - x0
			var sum pm
			for sy := range samples {
				for sx := range samples {
					x, y := a.sample(px, py, sx, sy)
					inside := roundRect(x, y, plateInset, plateSide, plateRadius) <= 0
					var s pm
					for _, l := range ls {
						switch {
						case l.clipped && !inside: // outside the plate's clip
						case l.light != nil:
							s.over(l.ink.times(l.light[i]))
						case l.covers(x, y):
							s.over(l.paint(x, y))
						}
					}
					sum.r, sum.g, sum.b, sum.a = sum.r+s.r, sum.g+s.g, sum.b+s.b, sum.a+s.a
				}
			}
			if sum.a == 0 {
				continue
			}
			// The pixel's coverage composited over what the image holds.
			src := pm{sum.r / n, sum.g / n, sum.b / n, sum.a / n}
			old := img.NRGBAAt(px, py)
			dst := pm{float64(old.R) / 255 * float64(old.A) / 255, float64(old.G) / 255 * float64(old.A) / 255,
				float64(old.B) / 255 * float64(old.A) / 255, float64(old.A) / 255}
			dst.over(src)
			img.SetNRGBA(px, py, color.NRGBA{R: unit(dst.r / dst.a), G: unit(dst.g / dst.a), B: unit(dst.b / dst.a), A: unit(dst.a)})
		}
	}
}

// blur convolves v (w×h, row after row) with a Gaussian of the standard deviation sigma in
// pixels; outside the area counts as nothing.
func blur(v []float64, w, h int, sigma float64) {
	if sigma <= 0 {
		return
	}
	r := int(math.Ceil(3 * sigma))
	k := make([]float64, 2*r+1)
	var sum float64
	for i := range k {
		d := float64(i - r)
		k[i] = math.Exp(-d * d / (2 * sigma * sigma))
		sum += k[i]
	}
	for i := range k {
		k[i] /= sum
	}
	tmp := make([]float64, len(v))
	for y := range h {
		for x := range w {
			var s float64
			for j := max(-r, -x); j <= min(r, w-1-x); j++ {
				s += k[j+r] * v[y*w+x+j]
			}
			tmp[y*w+x] = s
		}
	}
	for y := range h {
		for x := range w {
			var s float64
			for j := max(-r, -y); j <= min(r, h-1-y); j++ {
				s += k[j+r] * tmp[(y+j)*w+x]
			}
			v[y*w+x] = s
		}
	}
}

func unit(v float64) uint8 { return uint8(math.Max(0, math.Min(1, v))*255 + .5) }

// roundRect is the signed distance of a point to a rounded square at (inset, inset) with the
// given side and corner radius: negative inside.
func roundRect(x, y, inset, side, r float64) float64 {
	cx, cy := inset+side/2, inset+side/2
	qx, qy := math.Abs(x-cx)-(side/2-r), math.Abs(y-cy)-(side/2-r)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
}

// nearArc reports whether a point lies within w of the C's arc, round caps included. The arc is
// centred on the wheel with radius cR and runs the long way from +half to -half (angles in screen
// direction, 0 pointing right).
func nearArc(x, y, half, w float64) bool {
	dx, dy := x-centre, y-centre
	a := math.Atan2(dy, dx)
	if math.Abs(a) >= half { // on the arc's side of the opening
		return math.Abs(math.Hypot(dx, dy)-cR) <= w
	}
	// In the opening: the nearest part of the arc is one of its two ends.
	for _, s := range [2]float64{-1, 1} {
		ex, ey := centre+cR*math.Cos(half), centre+s*cR*math.Sin(half)
		if math.Hypot(x-ex, y-ey) <= w {
			return true
		}
	}
	return false
}

// segment returns the cover of a line with round caps and the given half width.
func segment(x1, y1, x2, y2, w float64) func(x, y float64) bool {
	return func(x, y float64) bool {
		vx, vy := x2-x1, y2-y1
		t := ((x-x1)*vx + (y-y1)*vy) / (vx*vx + vy*vy)
		t = math.Max(0, math.Min(1, t))
		return math.Hypot(x-(x1+t*vx), y-(y1+t*vy)) <= w
	}
}

// inConvex reports whether a point lies inside a convex polygon.
func inConvex(poly [][2]float64, x, y float64) bool {
	sign := 0.0
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		cross := (b[0]-a[0])*(y-a[1]) - (b[1]-a[1])*(x-a[0])
		if cross == 0 {
			continue
		}
		if sign == 0 {
			sign = cross
		} else if sign*cross < 0 {
			return false
		}
	}
	return true
}
