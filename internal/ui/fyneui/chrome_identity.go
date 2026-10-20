package fyneui

// A cluster's identity colour must not borrow a status hue (Dark Cockpit: a coral chip reads as an
// alarm next to quiet rows, an amber one as a warning, a violet one as the agent). The eight names
// stay, because they are what Cluster settings and the stored settings use, but their shades are
// blues, teals, indigo, slate and sand at low chroma. "red" is therefore a teal and "orange" a sand
// on this look; TestIdentityColorsContrast still holds them to 3:1 on every surface.
var calmIdentityColors = map[string]map[string]uint32{
	"dark": {"cyan": glowCyan, "blue": 0x6aa9ff, "violet": 0x8e9bf2, "magenta": 0x9fb0c8,
		"red": 0x4fbfb2, "orange": 0xd6bd8e, "yellow": 0xc4bdb0, "green": 0x86b5b0},
	"light": {"cyan": 0x0b7c93, "blue": 0x1f66c9, "violet": 0x4f58b8, "magenta": 0x56687f,
		"red": 0x14796f, "orange": 0x8a6a2f, "yellow": 0x75695a, "green": 0x3a7a80},
}

func init() {
	for name, p := range map[string]*palette{"dark": &darkPalette, "light": &lightPalette} {
		for color, rgb := range calmIdentityColors[name] {
			p.fyne[identityColorName(color)] = hex(rgb)
		}
	}
}
