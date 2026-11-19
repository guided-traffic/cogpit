package logo

import "math"

// The "gauge" version, the MVP's app icon: a cog around a cockpit gauge, drawn full-bleed into
// 1024×1024 pixels, which fyne package masked to its rounded square. Kept as it was drawn then
// (hack/icon at a8658f6), in its own pixel grid; gaugeLayers lays that square onto the plate, which
// stands for fyne package's mask.
const (
	gSize   = 1024.0
	gCentre = gSize / 2

	gTeeth     = 10
	gTip       = 430.0 // gear tooth tips
	gRoot      = 365.0 // gear body
	gFace      = 288.0 // gauge face, inside the gear ring
	gRimWidth  = 9.0   // dark line between the ring and the face
	gToothRoot = 66.0  // half-width of a tooth at its root, in pixels of arc
	gToothTip  = 44.0  // half-width at its tip

	gSweepStart = 225.0 // gauge sweep, degrees counter-clockwise from +x: lower left …
	gSweepEnd   = -45.0 // … over the top to lower right
	gNeedleAt   = 0.62  // needle position along the sweep
	gRedFrom    = 0.74  // the amber band starts here
)

// The gauge's palette, in 0…255.
var (
	gBgTop     = gRGB{0x0b, 0x1d, 0x33}
	gBgBottom  = gRGB{0x06, 0x12, 0x22}
	gGearLight = gRGB{0x45, 0xd9, 0xc8}
	gGearDark  = gRGB{0x17, 0x93, 0x88}
	gRim       = gRGB{0x0a, 0x4a, 0x45}
	gFaceIn    = gRGB{0x14, 0x2b, 0x46}
	gFaceOut   = gRGB{0x0a, 0x18, 0x2b}
	gTick      = gRGB{0xd6, 0xea, 0xff}
	gBand      = gRGB{0xff, 0xb6, 0x27}
	gNeedle    = gRGB{0xff, 0x7a, 0x3d}
	gHub       = gRGB{0xe8, 0xf1, 0xfa}
	gHubCore   = gRGB{0x0d, 0x1b, 0x2e}
)

type gRGB struct{ r, g, b float64 }

func gMix(a, b gRGB, t float64) gRGB {
	t = math.Max(0, math.Min(1, t))
	return gRGB{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
}

// gaugeLayers is the gauge as one layer: the 1024-pixel square scaled onto the plate.
func gaugeLayers(area) []layer {
	return []layer{{covers: always, clipped: true, paint: func(x, y float64) pm {
		c := gaugeShade((x-plateInset)/plateSide*gSize, (y-plateInset)/plateSide*gSize)
		return pm{c.r / 255, c.g / 255, c.b / 255, 1}
	}}}
}

// gaugeShade returns the colour at a point of the 1024-pixel square, painting the layers back to
// front.
func gaugeShade(x, y float64) gRGB {
	dx, dy := x-gCentre, gCentre-y // y up
	r := math.Hypot(dx, dy)
	theta := math.Atan2(dy, dx)

	c := gMix(gBgTop, gBgBottom, y/gSize)
	if gaugeInGear(r, theta) {
		c = gMix(gGearLight, gGearDark, (y-(gCentre-gTip))/(2*gTip))
	}
	if r <= gFace+gRimWidth {
		c = gRim
	}
	if r > gFace {
		return c
	}
	c = gMix(gFaceIn, gFaceOut, r/gFace)

	// The sweep position of this point: 0 at gSweepStart, 1 at gSweepEnd, clockwise.
	deg := theta * 180 / math.Pi
	pos := math.Mod(gSweepStart-deg+720, 360) / (gSweepStart - gSweepEnd)
	if pos <= 1 {
		if pos >= gRedFrom && r >= 262 && r <= 276 {
			c = gBand
		}
		if gaugeOnTick(r, pos) {
			c = gTick
		}
	}
	if gaugeOnNeedle(dx, dy) {
		c = gNeedle
	}
	switch {
	case r <= 18:
		c = gHubCore
	case r <= 40:
		c = gHub
	}
	return c
}

// gaugeInGear reports whether a point lies on the cog: its body or one of its trapezoid teeth.
func gaugeInGear(r, theta float64) bool {
	if r > gTip {
		return false
	}
	if r <= gRoot {
		return true
	}
	pitch := 2 * math.Pi / gTeeth
	a := math.Mod(theta-math.Pi/2+pitch/2+4*math.Pi, pitch) - pitch/2 // angle from the nearest tooth's centre; a tooth points up
	half := gToothRoot + (gToothTip-gToothRoot)*(r-gRoot)/(gTip-gRoot)
	return math.Abs(a)*r <= half
}

// gaugeOnTick reports whether a point lies on a gauge tick: eleven major ticks and minor ones
// between.
func gaugeOnTick(r, pos float64) bool {
	const steps = 20
	k := math.Round(pos * steps)
	arc := math.Abs(pos-k/steps) * (gSweepStart - gSweepEnd) * math.Pi / 180 * r // distance along the arc
	if int(k)%2 == 0 {
		return r >= 210 && r <= 256 && arc <= 7
	}
	return r >= 232 && r <= 256 && arc <= 3.5
}

// gaugeOnNeedle reports whether a point lies on the needle: a tapered bar from the hub to the dial.
func gaugeOnNeedle(dx, dy float64) bool {
	angle := (gSweepStart - gNeedleAt*(gSweepStart-gSweepEnd)) * math.Pi / 180
	ux, uy := math.Cos(angle), math.Sin(angle)
	along := dx*ux + dy*uy
	across := math.Abs(-dx*uy + dy*ux)
	const length, tail = 236.0, 52.0
	if along < -tail || along > length {
		return false
	}
	half := 15.0
	if along > 0 {
		half = 15 - 12*along/length
	}
	return across <= half
}
