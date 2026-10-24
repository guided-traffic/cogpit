package fyneui

import (
	"crypto/sha256"
	"fmt"
	"math"
	"testing"
	"time"

	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// stripNear reports whether two numbers are within eps.
func stripNear(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

// The breathing light starts at its lowest point, is full half a breath later, and low again after
// one (the owner's pick: 4.6 s down to 48 %).
func TestLightStripBreathingStartsAtItsLowest(t *testing.T) {
	for _, c := range []struct{ t, want float64 }{{0, stripBreatheMin}, {stripBreathePeriod / 2, 1}, {stripBreathePeriod, stripBreatheMin}} {
		if got := breathAlpha(c.t); !stripNear(got, c.want, 1e-9) {
			t.Errorf("breathAlpha(%v) = %v, want %v", c.t, got, c.want)
		}
	}
	if flat, ramps := layerPaint(engine.LightStripBreathing, 0, 1000, 0.5); !stripNear(flat, 0.5*stripBreatheMin, 1e-9) || ramps != nil {
		t.Errorf("breathing at its start: flat %v, ramps %v", flat, ramps)
	}
}

// The scanner's first sweep comes in from beyond the left edge, its light wholly outside at the
// start, and runs to the right end, where the swing takes over without a jump or a jolt; from then
// on it swings between its ends and never leaves the strip.
func TestLightStripScannerEntersFromOutside(t *testing.T) {
	const w = float32(1000)
	half := stripScanWidth * w / 2
	lo, hi := half*0.6, w-half*0.6
	if c := scannerCentre(0, w, half); c+half > 0 {
		t.Errorf("at the start the light reaches %v, want it wholly beyond the left edge", c+half)
	}
	mid := stripScanPeriod / 2
	at := func(t float64) float64 { return float64(scannerCentre(t, w, half)) }
	if !stripNear(at(mid), float64(hi), 1e-3) {
		t.Errorf("half a period in: %v, want the right end %v", at(mid), hi)
	}
	// No jump: just before and after the hand-over the light is at the right end; no jolt: it comes
	// to rest there from both sides (the steps either side shrink to nothing alike).
	const eps = 1e-3
	before, after := at(mid-eps), at(mid+eps)
	if !stripNear(before, float64(hi), 0.01) || !stripNear(after, float64(hi), 0.01) {
		t.Errorf("around the hand-over %v and %v, want both at %v", before, after, hi)
	}
	if vb, va := (at(mid)-at(mid-eps))/eps, (at(mid+eps)-at(mid))/eps; math.Abs(vb) > 1 || math.Abs(va) > 1 {
		t.Errorf("the speed at the hand-over: %v before, %v after, want rest on both sides", vb, va)
	}
	for s := mid; s < 4*stripScanPeriod; s += 0.05 {
		if c := at(s); c < float64(lo)-1e-3 || c > float64(hi)+1e-3 {
			t.Fatalf("at %.2f s the centre %v left [%v, %v]", s, c, lo, hi)
		}
	}
	if !stripNear(at(stripScanPeriod), float64(lo), 1e-3) {
		t.Errorf("a period in: %v, want the left end %v", at(stripScanPeriod), lo)
	}
	_, ramps := layerPaint(engine.LightStripScanner, 0, w, 1)
	if len(ramps) != 2 || ramps[0].a1 != 0 || ramps[0].a2 != 1 || ramps[1].a1 != 1 || ramps[1].a2 != 0 || ramps[1].x2 > 0 {
		t.Errorf("the scanner at its start: %+v, want a light rising and falling, wholly left of the strip", ramps)
	}
}

// The flow's light comes in wholly from beyond the left edge, its soft front too; the next one sets
// off the moment the head has gone under at the right, while the last one's tail still runs out;
// none before the first.
func TestLightStripFlowsTwoComets(t *testing.T) {
	const w = float32(1000)
	length := stripFlowWidth * w
	front := length * stripFlowHead
	if heads := flowHeads(0, w, front); len(heads) != 1 || heads[0] != -front {
		t.Errorf("at the start: heads %v, want one at -%v (its front wholly outside)", heads, front)
	}
	for s := 0.0; s < stripFlowPeriod; s += 0.1 {
		if heads := flowHeads(s, w, front); len(heads) != 1 {
			t.Fatalf("at %.1f s, before the first light is through: %d lights", s, len(heads))
		}
	}
	end := flowHeads(stripFlowPeriod-1e-6, w, front)
	if !stripNear(float64(end[0]), float64(w), 0.01) {
		t.Errorf("at the end of the first pass the head is at %v, want gone under at %v", end[0], w)
	}
	next := flowHeads(stripFlowPeriod, w, front)
	if len(next) != 2 || !stripNear(float64(next[0]), float64(-front), 1e-3) || !stripNear(float64(next[1]), float64(w), 1e-3) {
		t.Errorf("as the second sets off: heads %v, want the new one at -%v and the last at %v", next, front, w)
	}
	_, ramps := layerPaint(engine.LightStripFlow, stripFlowPeriod+1, w, 1)
	if len(ramps) != 4 {
		t.Fatalf("from the second pass on: %d ramps, want two comets of two", len(ramps))
	}
	if tail := ramps[2]; tail.x1 >= w || tail.x2-tail.x1 != length {
		t.Errorf("the last light's tail %+v: want it still running out in the strip, %v long", tail, length)
	}
	if _, first := layerPaint(engine.LightStripFlow, 0, w, 1); len(first) != 2 || first[1].x2 > 0 {
		t.Errorf("the first light at its start: %+v, want one comet wholly outside", first)
	}
}

// Every change goes through glass: the old light fades out over half the fade, then the new one fades
// in over the other half, its animation's clock starting at its fade-in, eased by smoothstep. A
// change back while the new one waits fades the old out still and starts the newest afresh.
func TestLightStripTransitionClockStartsAtFadeIn(t *testing.T) {
	t0 := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	red := stripLook{color: "red", anim: engine.LightStripScanner}
	green := stripLook{color: "green", anim: engine.LightStripBreathing}
	var m stripModel
	if !m.retarget(red, t0) || m.retarget(red, t0.Add(time.Millisecond)) {
		t.Fatal("the first look starts a change, the same one again none")
	}
	if a := m.layers[0]; a.opacity(t0) != 0 || a.clock(t0) != 0 || !stripNear(a.opacity(t0.Add(stripHalf)), 1, 1e-9) {
		t.Errorf("the first look comes from glass: opacity %v at once, %v after half the fade", a.opacity(t0), a.opacity(t0.Add(stripHalf)))
	}
	t1 := t0.Add(5 * time.Second)
	m.retarget(green, t1)
	old, next := m.layers[0], m.layers[1]
	if !next.in.Equal(t1.Add(stripHalf)) {
		t.Errorf("the new look begins at %v, want once the old has faded out, %v", next.in.Sub(t1), stripHalf)
	}
	half := t1.Add(stripHalf / 2)
	if !stripNear(old.opacity(half), 0.5, 1e-9) || next.opacity(half) != 0 || next.clock(half) != 0 {
		t.Errorf("a quarter in: old %v, new %v (clock %v)", old.opacity(half), next.opacity(half), next.clock(half))
	}
	in := t1.Add(stripHalf)
	if next.clock(in) != 0 || next.clock(in.Add(time.Second)) != 1 {
		t.Errorf("the new animation's clock: %v at its fade-in, %v a second later", next.clock(in), next.clock(in.Add(time.Second)))
	}
	if threeQ := in.Add(stripHalf / 2); !stripNear(next.opacity(threeQ), 0.5, 1e-9) || !stripNear(smoothstep(0.5), 0.5, 1e-9) || !stripNear(smoothstep(0.25), 0.15625, 1e-9) {
		t.Errorf("three quarters in: %v", next.opacity(threeQ))
	}
	m.prune(in)
	if len(m.layers) != 1 || m.layers[0] != next {
		t.Errorf("once faded out the old layer is dropped: %d layers", len(m.layers))
	}

	// Back to red while green waits: green never shows, red starts from its beginning again.
	var b stripModel
	b.retarget(red, t0)
	b.retarget(green, t1)
	t2 := t1.Add(100 * time.Millisecond)
	b.retarget(red, t2)
	last := b.layers[len(b.layers)-1]
	if last.look != red || !last.in.Equal(t1.Add(stripHalf)) {
		t.Errorf("red again begins %v after the first change, want when the old red has gone", last.in.Sub(t1))
	}
	b.prune(t2)
	for _, l := range b.layers {
		if l.look == green {
			t.Error("green, which never showed, is kept")
		}
	}
}

// The strip moves while a light fades or an animation shows, never for glass alone or a light that
// stands.
func TestLightStripMovesOnlyWhileSomethingChanges(t *testing.T) {
	t0 := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	settled := t0.Add(stripFade)
	cases := []struct {
		look       stripLook
		start, end bool
	}{
		{stripLook{}, false, false},
		{stripLook{color: "red", anim: engine.LightStripStatic}, true, false},
		{stripLook{color: "red", anim: engine.LightStripBreathing}, true, true},
		{stripLook{color: "red", anim: engine.LightStripScanner}, true, true},
		{stripLook{color: "#336699", anim: engine.LightStripFlow}, true, true},
	}
	for _, c := range cases {
		var m stripModel
		m.retarget(c.look, t0)
		if got := m.moving(t0); got != c.start {
			t.Errorf("%+v at its start: moving %v, want %v", c.look, got, c.start)
		}
		if got := m.moving(settled); got != c.end {
			t.Errorf("%+v settled: moving %v, want %v", c.look, got, c.end)
		}
	}
	// A light going to glass moves until it has gone.
	var m stripModel
	m.retarget(stripLook{color: "red", anim: engine.LightStripStatic}, t0)
	m.retarget(stripLook{}, settled)
	if !m.moving(settled.Add(stripHalf/2)) || m.moving(settled.Add(stripHalf)) {
		t.Error("a light fading to glass: moving while it fades, still once it has gone")
	}
}

// The grain is the lab's texture: 256 × 64 black and white specks from the seeded generator, the
// same at every start; tiled over the strip in device pixels at the grain's strength.
func TestLightStripGrainIsTheLabsTexture(t *testing.T) {
	n := stripNoise()
	if b := n.Bounds(); b.Dx() != stripGrainW || b.Dy() != stripGrainH {
		t.Fatalf("the texture is %v", b)
	}
	// The lab's first draw from seed 7: s = 7·1664525 + 1013904223 = 1025555898, r ≈ 0.2388:
	// black, alpha (2·|r − ½|)² · 255 ≈ 69.6.
	if c := n.NRGBAAt(0, 0); c.R != 0 || c.G != 0 || c.B != 0 || c.A != 70 {
		t.Errorf("the first speck %+v, want black at 70", c)
	}
	for i := 0; i < len(n.Pix); i += 4 {
		if v := n.Pix[i]; (v != 0 && v != 0xff) || n.Pix[i+1] != v || n.Pix[i+2] != v {
			t.Fatalf("speck %d is %v, want black or white", i/4, n.Pix[i:i+4])
		}
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(n.Pix))[:16]; got != grainDigest {
		t.Errorf("the texture's digest %s, want %s: the grain changed", got, grainDigest)
	}
	g := stripGrain(600, 8)
	if g.Bounds().Dx() != 600 || g.Bounds().Dy() != 8 {
		t.Fatalf("tiled: %v", g.Bounds())
	}
	at, src := g.NRGBAAt(256+3, 5), n.NRGBAAt(3, 5)
	if at.R != src.R || at.A != uint8(math.Round(float64(src.A)*stripGrainStrength*stripGrainFactor)) {
		t.Errorf("tiled speck %+v of %+v, want the texture's at %v of its alpha", at, src, stripGrainStrength*stripGrainFactor)
	}
}

// grainDigest is the start of the grain texture's SHA-256, the same as the lab's NOISE data run in
// Node (2026-10-24): the port draws the lab's specks byte for byte. It changes only with the texture.
const grainDigest = "e2d798af478a3160"

// The frosted glass is the lab's: the stage mixed by the owner's density, kept in the palettes.
func TestLightStripGlassIsTheLabsMix(t *testing.T) {
	for _, dark := range []bool{true, false} {
		p := &lightPalette
		if dark {
			p = &darkPalette
		}
		stage := p.fyne[theme.ColorNameBackground]
		want := stripGlassOf(nrgba(stage), dark)
		if got := nrgba(p.fyne[colorLightStrip]); got != want {
			t.Errorf("dark %v: the palette's glass %+v, want the lab's mix %+v", dark, got, want)
		}
	}
}
