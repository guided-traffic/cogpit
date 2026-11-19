package fyneui

import (
	"image/color"
	"math"
	"strconv"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// A cluster colour picked freely (A93, owner; engine.IsCustomIconColor): the eight accents have a
// shade per theme that stays legible (TestIdentityColorsContrast); a free colour has none, so the
// window derives one — the lead's night decision for the owner to confirm: the colour's hue and
// saturation kept, its lightness moved, upwards in the dark theme and downwards in the light one,
// just far enough for a 3:1 contrast on every surface an identity is drawn on (the header, the
// sidebar, an input, the background), as the eight are held to.

// identitySurfaces are the surfaces an identity colour is drawn on.
var identitySurfaces = []fyne.ThemeColorName{theme.ColorNameHeaderBackground, colorSidebar, theme.ColorNameInputBackground, theme.ColorNameBackground}

// minIdentityContrast is the contrast a derived shade keeps on every identity surface.
const minIdentityContrast = 3.0

var customShades sync.Map // "<#rrggbb>/<variant>" → color.NRGBA

// customIdentityColor returns a free colour's legible shade for a variant; ok is false for a name
// that is no free colour's.
func customIdentityColor(n fyne.ThemeColorName, v fyne.ThemeVariant) (color.NRGBA, bool) {
	c, ok := strings.CutPrefix(string(n), string(identityColorName("")))
	if !ok || len(c) != 7 || c[0] != '#' {
		return color.NRGBA{}, false
	}
	rgb, err := strconv.ParseUint(c[1:], 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	key := c + "/" + strconv.Itoa(int(v))
	if got, ok := customShades.Load(key); ok {
		return got.(color.NRGBA), true
	}
	shade := legibleShade(hex(uint32(rgb)), paletteFor(v), v == theme.VariantDark)
	customShades.Store(key, shade)
	return shade, true
}

// legibleShade moves c's lightness (HSL) in steps until its contrast on every identity surface of
// p is at least minIdentityContrast: up on a dark theme, down on a light one.
func legibleShade(c color.NRGBA, p *palette, dark bool) color.NRGBA {
	h, s, l := toHSL(c)
	for range 101 {
		shade := fromHSL(h, s, l)
		if minContrastOn(shade, p) >= minIdentityContrast {
			return shade
		}
		if dark {
			l = math.Min(1, l+0.01)
		} else {
			l = math.Max(0, l-0.01)
		}
	}
	if dark {
		return color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	}
	return color.NRGBA{A: 0xff}
}

// minContrastOn is c's lowest contrast on p's identity surfaces.
func minContrastOn(c color.NRGBA, p *palette) float64 {
	low := math.Inf(1)
	for _, bg := range identitySurfaces {
		if b, ok := p.fyne[bg]; ok {
			low = math.Min(low, contrast(c, b))
		}
	}
	return low
}

// contrast is the WCAG contrast ratio of two colours.
func contrast(a, b color.Color) float64 {
	la, lb := relLuminance(a), relLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func relLuminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		x := float64(v) / 0xffff
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

func toHSL(c color.NRGBA) (h, s, l float64) {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l = (mx + mn) / 2
	if mx == mn {
		return 0, 0, l
	}
	d := mx - mn
	if l > 0.5 {
		s = d / (2 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	switch mx {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h / 6, s, l
}

func fromHSL(h, s, l float64) color.NRGBA {
	if s == 0 {
		v := uint8(math.Round(l * 255))
		return color.NRGBA{R: v, G: v, B: v, A: 0xff}
	}
	q := l * (1 + s)
	if l >= 0.5 {
		q = l + s - l*s
	}
	p := 2*l - q
	conv := func(t float64) uint8 {
		switch {
		case t < 0:
			t++
		case t > 1:
			t--
		}
		var v float64
		switch {
		case t < 1.0/6:
			v = p + (q-p)*6*t
		case t < 0.5:
			v = q
		case t < 2.0/3:
			v = p + (q-p)*(2.0/3-t)*6
		default:
			v = p
		}
		return uint8(math.Round(math.Max(0, math.Min(1, v)) * 255))
	}
	return color.NRGBA{R: conv(h + 1.0/3), G: conv(h), B: conv(h - 1.0/3), A: 0xff}
}
