package fyneui

import (
	"fmt"
	"image/color"
	"math"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// One strong blue per region: the bar marks a selection, so its plate is a tint of the glow blue
// and not the deep blue that was there (about 14 % of the glow blue over the stage in the dark
// theme, a pale wash in the light one). The token stays the one every selection uses (rows, the
// navigator, the palette), so the whole window calms down at once; the text on it keeps
// colorRowSelectedText, which holds 4.5:1 on the new fills (TestThemeContrast).
func init() {
	darkPalette.fyne[colorRowSelected] = hex(0x16293f)
	lightPalette.fyne[colorRowSelected] = hex(0xe0ebfb)
}

// The text of a status pill must read at 4.5:1 on its own tint. The status colours are tuned as
// graphics (3:1: a spine, a dot), and on the light theme a word in them on a 14 to 18 % plate
// measures 3.5 to 4.1; so the pill's word takes the status colour moved in lightness, darker on
// light and lighter on dark, just far enough. The hue and saturation stay, so a pill still reads
// as its status.
const minPillContrast = 4.5

var pillInks sync.Map // "<rrggbb>/<alpha>/<bg>" -> color.NRGBA

// pillInk returns the word colour for a pill of status colour col, tinted at alpha over the stage.
func pillInk(th fyne.Theme, col color.Color, alpha uint8) color.Color {
	c := color.NRGBAModel.Convert(col).(color.NRGBA)
	bg := color.NRGBAModel.Convert(themeColor(th, theme.ColorNameBackground)).(color.NRGBA)
	key := fmt.Sprintf("%02x%02x%02x/%d/%02x%02x%02x", c.R, c.G, c.B, alpha, bg.R, bg.G, bg.B)
	if v, ok := pillInks.Load(key); ok {
		return v.(color.NRGBA)
	}
	plate := over(withAlpha(c, alpha), bg)
	dark := relLuminance(bg) < 0.4
	h, s, l := toHSL(c)
	out := c
	for range 101 {
		out = fromHSL(h, s, l)
		if contrast(out, plate) >= minPillContrast {
			break
		}
		if dark {
			l = math.Min(1, l+0.01)
		} else {
			l = math.Max(0, l-0.01)
		}
	}
	pillInks.Store(key, out)
	return out
}

// over composites a translucent colour on an opaque one.
func over(top color.Color, base color.NRGBA) color.NRGBA {
	t := color.NRGBAModel.Convert(top).(color.NRGBA)
	a := float64(t.A) / 255
	mix := func(f, b uint8) uint8 { return uint8(math.Round(float64(f)*a + float64(b)*(1-a))) }
	return color.NRGBA{R: mix(t.R, base.R), G: mix(t.G, base.G), B: mix(t.B, base.B), A: 0xff}
}

// pillFill is the alpha of a status pill's tint: 14 % on the dark theme, 18 % on the light one,
// where the same tint is paler.
func pillFill(th fyne.Theme) uint8 {
	if relLuminance(themeColor(th, theme.ColorNameBackground)) < 0.4 {
		return pillAlpha
	}
	return 0x2e
}
