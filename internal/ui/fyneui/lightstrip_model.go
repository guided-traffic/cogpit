package fyneui

import (
	"image"
	"image/color"
	"math"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The light strip's model and maths (D-new, lightstrip.go): pure functions of time and width, so
// that the tests drive them without a window. Ported from the owner's lab (plan/light-strip-lab.html
// v8, drawLayer, stepLayers, retarget and NOISE: the maths, not the canvas calls), with his picks of
// 2026-10-24 as the constants below.

// His picks from the lab (light-strip.md § 2; the lab's knobs in brackets).
const (
	stripHeight        = 4    // the strip's height in points (height)
	stripGlassDensity  = 0.32 // how dense the frosted glass is (glassOff 32 %)
	stripGrainStrength = 0.10 // the grain's strength (grain 10 %, the owner's LS2 and LS20 from his lab's 15 %; 1 device pixel per speck)
	stripGlow          = 1.0  // the light's strength (glow 100 %); no base glow, no halo, no edges
	stripBreathePeriod = 4.6  // seconds per breath (breathePeriod)
	stripBreatheMin    = 0.48 // the breath's lowest point (breatheMin 48 %)
	stripScanPeriod    = 6.0  // seconds there and back (scanPeriod)
	stripScanWidth     = 0.50 // the scanner's light, a share of the strip's width (scanWidth 50 %)
	stripFlowPeriod    = 5.0  // seconds per pass of the flow's head across (flowPeriod)
	stripFlowWidth     = 0.46 // the flow's light, a share of the width (flowWidth 46 %)
	stripFlowHead      = 0.25 // its soft front, a share of the light (flowHead 25 %)
	stripFPS           = 60   // the frame cap: the ticker's rate while the strip moves (fps)

	// stripFade is a whole change through glass (fadeMs 700, fadeKind glass): the old light fades out
	// in its first half, the new one fades in in its second. A static colour that follows another
	// spreads from the tab or crossfades over the same 700 ms (LS22, LS23).
	stripFade = 700 * time.Millisecond
	stripHalf = stripFade / 2

	// stripSpreadSoft is the soft edge of a colour spreading from the tab, a share of the way it
	// runs (the transition lab's edge softness, 18 %, plan/light-strip-transitions-lab.html).
	stripSpreadSoft = 0.18

	// The grain: one fixed texture of black and white specks, the lab's NOISE: its size, its seed,
	// and its strength's factor (the lab draws it at grain × 0.6).
	stripGrainW, stripGrainH = 256, 64
	stripGrainSeed           = 7
	stripGrainFactor         = 0.6
)

// smoothstep eases a transition's progress a, 0 to 1 (3a² − 2a³), as the lab eases a layer's
// opacity.
func smoothstep(a float64) float64 {
	a = clamp01(a)
	return a * a * (3 - 2*a)
}

func clamp01(a float64) float64 { return math.Max(0, math.Min(1, a)) }

// breathAlpha is the breathing light's strength t seconds after it began: from its lowest point,
// stripBreatheMin, up to full and back once every stripBreathePeriod.
func breathAlpha(t float64) float64 {
	phase := 0.5 - 0.5*math.Cos(2*math.Pi*t/stripBreathePeriod)
	return stripBreatheMin + (1-stripBreatheMin)*phase
}

// scannerCentre is the scanner's light's centre t seconds after it began, on a strip w wide whose
// light is 2·half wide. The first sweep enters from beyond the left edge (its light wholly outside)
// and runs to the right end, hi; from then on it swings between lo and hi. Both meet at hi with no
// speed, so the hand-over has no jolt (the owner: "Beim Scannen ist der Start wie beim Fließen von
// außen rein in den viewpoint").
func scannerCentre(t float64, w, half float32) float32 {
	p := float32(0.5 - 0.5*math.Cos(2*math.Pi*t/stripScanPeriod))
	lo, hi := half*0.6, w-half*0.6
	from := lo
	if t < stripScanPeriod/2 {
		from = -half
	}
	return from + (hi-from)*p
}

// flowHeads are the heads of the flow's lights t seconds after it began, on a strip w wide whose
// lights are length long with a soft front of front. A head runs from -front (its front wholly
// beyond the left edge) to w (its front gone under at the right) once every stripFlowPeriod; the
// next one sets off the moment the last one's head has gone under, so from the second pass on the
// previous light's tail still runs out at the right while the next comes in (the owner: "Beim
// fließen, soll der Streifen vorne schon wieder losgehen wenn er hinten schon eingetaucht ist").
// None before the first.
func flowHeads(t float64, w, front float32) []float32 {
	d := w + front
	pass := math.Floor(t / stripFlowPeriod)
	head := -front + float32(t/stripFlowPeriod-pass)*d
	if pass >= 1 {
		return []float32{head, head + d}
	}
	return []float32{head}
}

// stripRamp is a horizontal run of the light from x1 to x2 (points from the strip's left edge), its
// strength going from a1 to a2; the strip clips what lies beyond its edges.
type stripRamp struct {
	x1, x2 float32
	a1, a2 float64
}

// layerPaint is how one light shows t seconds after its animation began, on a strip w wide at
// strength k (the glow times the layer's eased opacity): a flat strength over the whole strip
// (static and breathing; flat < 0 for none) or its ramps (scanner and flow).
func layerPaint(anim string, t float64, w float32, k float64) (flat float64, ramps []stripRamp) {
	switch anim {
	case engine.LightStripBreathing:
		return k * breathAlpha(t), nil
	case engine.LightStripScanner:
		half := max(3, stripScanWidth*w) / 2
		c := scannerCentre(t, w, half)
		return -1, []stripRamp{{c - half, c, 0, k}, {c, c + half, k, 0}}
	case engine.LightStripFlow:
		length := max(4, stripFlowWidth*w)
		front := length * stripFlowHead
		heads := flowHeads(t, w, front)
		ramps = make([]stripRamp, 0, 2*len(heads))
		for _, h := range heads {
			ramps = append(ramps, stripRamp{h - length, h, 0, k}, stripRamp{h, h + front, k, 0})
		}
		return -1, ramps
	}
	return k, nil // static, and any light standing still
}

// stripLook is what the strip shows: a cluster's accent colour as its settings keep it ("" for
// none: frosted glass alone) and how it moves (an engine.LightStrip* animation; static while the
// strip stands still).
type stripLook struct {
	color string
	anim  string
}

// lit reports whether the look has a light, rather than the glass alone.
func (l stripLook) lit() bool { return l.color != "" }

// stripCover is how a layer comes in.
type stripCover int

const (
	// coverGlass: through glass, the old layers fading out first; every change but one static
	// colour to another (the owner, 2026-10-24).
	coverGlass stripCover = iota
	// coverSpread: a static colour over another, spreading from the shown tab's place to both ends
	// over stripFade (the owner's LS22: "bei übergang von static zu static machen wir: von Tab aus").
	coverSpread
	// coverCross: a static colour over another while motion is reduced, crossfading in sRGB over
	// stripFade (the owner's LS23: "wenn der nutzer \"Bewegung reduzieren\" gewählt hat, dann erfolgt
	// die überblendung von static zu static durch: Überblenden (sRGB)").
	coverCross
)

// stripLayer is one look on its way in or out. Its animation's clock starts when it begins to fade
// in (the owner: "Bei einem übergang über Milchglas, soll die Annimation stehts von vorne beginnen").
// A covering layer (coverSpread, coverCross) comes in over the layers below it, which stay as they
// are until it covers them all.
type stripLayer struct {
	look   stripLook
	in     time.Time // when it begins to fade in, and its animation's clock starts
	out    time.Time // when it began to fade out; zero while it has not
	from   float64   // its opacity when it began to fade out
	cover  stripCover
	origin float64 // a spread's start, a share of the strip's width from its left edge
}

// opacity is the layer's opacity at now, before easing: 0 to 1 over stripHalf as it fades in (a
// crossfade over stripFade; a spread is whole from its start and grows instead), and back to 0 from
// where it was as it fades out.
func (l *stripLayer) opacity(now time.Time) float64 {
	if !l.out.IsZero() {
		return clamp01(l.from - float64(now.Sub(l.out))/float64(stripHalf))
	}
	if now.Before(l.in) {
		return 0
	}
	switch l.cover {
	case coverSpread:
		return 1
	case coverCross:
		return clamp01(float64(now.Sub(l.in)) / float64(stripFade))
	}
	return clamp01(float64(now.Sub(l.in)) / float64(stripHalf))
}

// covering reports whether the layer still comes in over the layers below it at now.
func (l *stripLayer) covering(now time.Time) bool {
	return l.cover != coverGlass && l.out.IsZero() && now.Before(l.in.Add(stripFade))
}

// spread is how far a spreading layer has come at now, eased, 0 to 1.
func (l *stripLayer) spread(now time.Time) float64 {
	return smoothstep(float64(now.Sub(l.in)) / float64(stripFade))
}

// spreadRamps are a colour spreading from origin (a share of the strip's width) at progress e
// (eased, 0 to 1) on a strip w wide, at strength k: a flat core around the origin and a soft edge
// to each side, stripSpreadSoft of the way wide. The way runs to the farther end, so both ends are
// reached together only when the origin is the middle; the nearer one is covered earlier.
func spreadRamps(origin float64, e float64, w float32, k float64) []stripRamp {
	o := float32(clamp01(origin)) * w
	far := max(o, w-o)
	edge := -stripSpreadSoft/2 + e*(1+stripSpreadSoft) // the soft edge's middle, a share of far
	in := float32(max(0, edge-stripSpreadSoft/2)) * far
	out := float32(max(0, edge+stripSpreadSoft/2)) * far
	if out <= 0 {
		return nil
	}
	ramps := make([]stripRamp, 0, 3)
	if in > 0 {
		ramps = append(ramps, stripRamp{o - in, o + in, k, k})
	}
	return append(ramps, stripRamp{o - out, o - in, 0, k}, stripRamp{o + in, o + out, k, 0})
}

// clock is the seconds since the layer's animation began, 0 before.
func (l *stripLayer) clock(now time.Time) float64 { return max(0, now.Sub(l.in).Seconds()) }

// gone reports whether the layer has faded out.
func (l *stripLayer) gone(now time.Time) bool { return !l.out.IsZero() && l.opacity(now) <= 0 }

// stripModel holds the strip's layers, the newest last. Every change goes through glass (the owner,
// 2026-10-24: "statisch zu statisch über aus, ja"): the old layers fade out, then the new one fades
// in from its beginning. Time-based where the lab stepped frame by frame: a layer's opacity is a
// function of the clock, so a frame rate never changes a transition and a test sets any moment.
type stripModel struct {
	layers []*stripLayer
}

// retarget shows look from now on; it reports whether that started a change (the look shown or on
// its way in already starts none). A static colour over another static colour that shows (or comes
// in over one) spreads from origin, or crossfades while motion is reduced; every other change goes
// through glass.
func (m *stripModel) retarget(look stripLook, now time.Time, origin float64, reduced bool) bool {
	n := len(m.layers)
	if n > 0 && m.layers[n-1].out.IsZero() && m.layers[n-1].look == look {
		return false
	}
	if n > 0 && staticLit(look) {
		if top := m.layers[n-1]; top.out.IsZero() && staticLit(top.look) && (top.cover != coverGlass || top.opacity(now) >= 1) {
			cover := coverSpread
			if reduced {
				cover = coverCross
			}
			m.layers = append(m.layers, &stripLayer{look: look, in: now, cover: cover, origin: origin})
			m.prune(now)
			return true
		}
	}
	in := now
	for _, l := range m.layers {
		if l.out.IsZero() {
			l.from, l.out = l.opacity(now), now
		}
		if end := l.out.Add(time.Duration(l.from * float64(stripHalf))); end.After(in) {
			in = end // the new one waits until every old one has gone
		}
	}
	m.layers = append(m.layers, &stripLayer{look: look, in: in})
	m.prune(now)
	return true
}

// staticLit reports whether a look is a colour standing still.
func staticLit(l stripLook) bool { return l.lit() && l.anim == engine.LightStripStatic }

// prune drops the layers that have faded out, and those a covering layer has covered.
func (m *stripModel) prune(now time.Time) {
	from := 0
	for i, l := range m.layers {
		if l.cover != coverGlass && l.out.IsZero() && !l.covering(now) {
			from = i // a layer that has come in over every one below it
		}
	}
	kept := m.layers[:0]
	for _, l := range m.layers[from:] {
		if !l.gone(now) {
			kept = append(kept, l)
		}
	}
	clear(m.layers[len(kept):])
	m.layers = kept
}

// moving reports whether the strip still changes at now: a light fading in or out (or waiting to),
// or one whose animation runs. Glass alone draws nothing, so its own fades need no frame.
func (m *stripModel) moving(now time.Time) bool {
	for _, l := range m.layers {
		switch o := l.opacity(now); {
		case !l.look.lit():
		case l.covering(now):
			return true
		case !l.out.IsZero():
			if o > 0 {
				return true
			}
		case o < 1 || l.look.anim != engine.LightStripStatic:
			return true
		}
	}
	return false
}

// stripNoise is the grain's texture, the lab's NOISE: 256 × 64 specks, each black or white at
// random with an alpha of (2|r − ½|)², from a linear congruential generator seeded with 7, so the
// grain is the same at every start and in both themes. Made once.
var stripNoise = sync.OnceValue(func() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, stripGrainW, stripGrainH))
	s := uint32(stripGrainSeed)
	for i := 0; i < len(img.Pix); i += 4 {
		s = s*1664525 + 1013904223
		r := float64(s) / 4294967296
		var v uint8
		if r >= 0.5 {
			v = 0xff
		}
		a := math.Abs(r-0.5) * 2
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = v, v, v, uint8(math.Round(a*a*255))
	}
	return img
})

// stripGrain tiles the grain's texture over w × h device pixels, one speck per pixel, at the
// strength the lab draws it with: its alpha times stripGrainStrength × stripGrainFactor.
func stripGrain(w, h int) *image.NRGBA {
	noise := stripNoise()
	img := image.NewNRGBA(image.Rect(0, 0, max(w, 0), max(h, 0)))
	for y := range img.Rect.Dy() {
		for x := range img.Rect.Dx() {
			c := noise.NRGBAAt(x%stripGrainW, y%stripGrainH)
			c.A = uint8(math.Round(float64(c.A) * stripGrainStrength * stripGrainFactor))
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// mixColor mixes a toward b by t, channel by channel, rounded as the lab's mix does.
func mixColor(a, b color.NRGBA, t float64) color.NRGBA {
	m := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return color.NRGBA{R: m(a.R, b.R), G: m(a.G, b.G), B: m(a.B, b.B), A: 0xff}
}

// stripGlassOf is the frosted glass's colour on a stage: on a dark stage the stage mixed toward
// white by 3 % plus the density's 9 %, on a light one toward a cool grey (#7f8da3) by 5 % plus the
// density's 15 % (the lab's glassColor). The palettes keep the result as colorLightStrip.
func stripGlassOf(stage color.NRGBA, dark bool) color.NRGBA {
	if dark {
		return mixColor(stage, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, 0.03+stripGlassDensity*0.09)
	}
	return mixColor(stage, color.NRGBA{R: 0x7f, G: 0x8d, B: 0xa3, A: 0xff}, 0.05+stripGlassDensity*0.15)
}
