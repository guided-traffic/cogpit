package logo

import (
	"image"
	"image/color"
)

// Version is one of Cogpit's logos as the owner chose it. Every one is kept, so that any of them
// can be drawn again (owner): Current is the one Cogpit shows, `go run ./hack/icon -version <name>`
// draws any of them, and docs/developer/assets/logo holds each one's picture and reference.
type Version struct {
	Name   string // as hack/icon's -version takes it, and the files' name in docs/developer/assets/logo
	Date   string // the day the owner chose it
	About  string // what it is
	layers func(area) []layer
}

// Versions are all of Cogpit's logos, oldest first. A new one is appended, never replaces one.
var Versions = []Version{
	{Name: "gauge", Date: "2026-10-11", About: "a cog around a cockpit gauge: the MVP's app icon", layers: gaugeLayers},
	{Name: "helm", Date: "2026-10-12", About: "the C helm of D-24: a grey C and a cyan wheel on Solarized base03", layers: helmLayers},
	{Name: "helm-glow", Date: "2026-10-15", About: "D-24 revised: a blue, glowing C over a pale wheel on a petrol plate", layers: HelmGlow.layers},
}

// Current is the logo Cogpit shows: the app icon (hack/icon), the header, the welcome page, About
// and the README's banner and social preview. To show another one, point it at that version.
var Current = Versions[2]

// Find returns the version of the given name.
func Find(name string) (Version, bool) {
	for _, v := range Versions {
		if v.Name == name {
			return v, true
		}
	}
	return Version{}, false
}

// HelmGlow is the C helm the owner chose in the icon lab (D-24 revised;
// docs/developer/assets/logo/helm-glow.json): a blue C with a white core, glowing over a pale wheel,
// white handles, on a petrol plate darkening downwards.
var HelmGlow = Look{
	C: rgb(0x4f, 0xb6, 0xff), CWeight: 7.3, Handle: rgb(0xee, 0xf6, 0xff), Glow: 1, GlowRadius: 4.5, Core: 1, Halo: .35,
	Accent: rgb(0xbf, 0xe9, 0xff), SpokeWeight: .8, Outer: 1,
	PlateTop: rgb(0x03, 0x29, 0x3a), PlateBottom: rgb(0x00, 0x14, 0x1d), Rim: .16,
}

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: 0xff} }

// Draw paints the version onto img, which it does not clear: the box's point (x, y) lands on the
// pixel (ox + x·scale, oy + y·scale); the plate is the box's 2…98.
func (v Version) Draw(img *image.NRGBA, scale, ox, oy float64) { draw(img, v.layers, scale, ox, oy) }

// Tile draws the version's plate filling a side×side image: the logo as the GUI shows it.
func (v Version) Tile(side int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	scale := float64(side) / plateSide
	v.Draw(img, scale, -plateInset*scale, -plateInset*scale)
	return img
}
