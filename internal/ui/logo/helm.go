package logo

import "math"

// The "helm" version, the C helm of D-24 as it was drawn then (internal/ui/logo at 6c65340): base1
// and Solarized cyan on base03, in the app icon's weights, the wheel painted over the C and the
// plate's edge line on the plate's edge. The icon lab's "D-24 heute" comes close but paints the
// wheel under the C and the rim inside the edge.
var (
	helmPlate  = rgb(0x00, 0x2b, 0x36) // base03
	helmFrame  = rgb(0x93, 0xa1, 0xa1) // base1: the C, the handles and the outer ring
	helmAccent = rgb(0x2a, 0xa1, 0x98) // cyan: the inner ring, the spokes, the needle and the hub
)

// helmLayers are D-24's shapes back to front, as its reference SVG paints them.
func helmLayers(area) []layer {
	const outerRing, outerOpacity, c, inner, spoke, handle, needle, hub = .9, .6, 5.6, 2, 1.6, 2.6, 2.2, 5.13
	var ls []layer
	add := func(covers func(x, y float64) bool, p pm) { ls = append(ls, layer{covers: covers, paint: flat(p)}) }

	add(func(x, y float64) bool { return roundRect(x, y, plateInset, plateSide, plateRadius) <= 0 }, ink(helmPlate, 1))
	add(func(x, y float64) bool { return math.Abs(roundRect(x, y, plateInset, plateSide, plateRadius)) <= .5 }, ink(white, .06))
	add(ring(outerR, outerRing), ink(helmFrame, outerOpacity))
	add(arc(c), ink(helmFrame, 1))
	add(ring(innerR, inner), ink(helmAccent, 1))
	// The seven spokes, each an inner bar in the accent ink and a handle outside the C in the frame
	// ink, every 45° but to the right, where the needle is.
	for k := 1; k < spokes; k++ {
		dx, dy := direction(k)
		add(segment(centre+spokeBeg*dx, centre+spokeBeg*dy, centre+spokeEnd*dx, centre+spokeEnd*dy, spoke/2), ink(helmAccent, 1))
		add(segment(centre+handleBeg*dx, centre+handleBeg*dy, centre+handleEnd*dx, centre+handleEnd*dy, handle/2), ink(helmFrame, 1))
	}
	add(segment(centre, centre, needleEnd, centre, needle/2), ink(helmAccent, 1))
	add(hexagon(hub), ink(helmAccent, 1))
	return ls
}
