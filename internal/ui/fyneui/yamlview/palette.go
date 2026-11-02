package yamlview

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Theme colour names for YAML highlighting. Cogpit's theme may define them; a theme that does not
// (Fyne's default returns transparent for unknown names) gets colours derived from its own primary,
// success, warning, error and placeholder colours, corrected for contrast against the editor
// background so that dark and light variants both stay readable.
const (
	ColorNameKey      fyne.ThemeColorName = "cogpit.yaml.key"
	ColorNameString   fyne.ThemeColorName = "cogpit.yaml.string"
	ColorNameNumber   fyne.ThemeColorName = "cogpit.yaml.number"
	ColorNameLiteral  fyne.ThemeColorName = "cogpit.yaml.literal" // booleans and null
	ColorNameComment  fyne.ThemeColorName = "cogpit.yaml.comment"
	ColorNamePunct    fyne.ThemeColorName = "cogpit.yaml.punct"    // indicators and block scalar headers
	ColorNameAnchor   fyne.ThemeColorName = "cogpit.yaml.anchor"   // anchors, aliases and tags
	ColorNameDocument fyne.ThemeColorName = "cogpit.yaml.document" // document markers and directives
)

// Minimum contrast ratios (WCAG) against the editor background: text kinds the reader scans need
// 4.5, de-emphasised kinds (comments, punctuation, line numbers) 3.
const (
	contrastText  = 4.5
	contrastMuted = 3.0
)

// palette is the resolved set of colours for one render.
type palette struct {
	bg, gutterFg, gutterFgActive, separator color.Color
	caret, selection, currentLine           color.Color
	scrollBar                               color.Color
	// findMatch tints a find bar's matches, findCurrent fills its selected one and findStroke
	// outlines it (View.SetMarks).
	findMatch, findCurrent, findStroke color.Color
	kinds                                   [numTokKinds]color.Color
}

// resolvePalette derives the palette from th in variant v.
func resolvePalette(th fyne.Theme, v fyne.ThemeVariant) palette {
	c := func(n fyne.ThemeColorName) color.Color { return th.Color(n, v) }
	bg := opaque(c(theme.ColorNameInputBackground), c(theme.ColorNameBackground))
	fg := c(theme.ColorNameForeground)
	primary := c(theme.ColorNamePrimary)
	errorC := c(theme.ColorNameError)
	muted := c(theme.ColorNamePlaceHolder)

	p := palette{
		bg:             bg,
		gutterFg:       ensureContrast(muted, bg, contrastMuted),
		gutterFgActive: fg,
		separator:      c(theme.ColorNameSeparator),
		caret:          primary,
		selection:      c(theme.ColorNameSelection),
		currentLine:    c(theme.ColorNameHover),
		scrollBar:      c(theme.ColorNameScrollBar),
	}
	derive := []struct {
		kind     tokKind
		name     fyne.ThemeColorName
		fallback color.Color
		ratio    float64
	}{
		{tokPlain, "", fg, 0},
		{tokKey, ColorNameKey, primary, contrastText},
		{tokString, ColorNameString, c(theme.ColorNameSuccess), contrastText},
		{tokNumber, ColorNameNumber, c(theme.ColorNameWarning), contrastText},
		{tokLiteral, ColorNameLiteral, mix(primary, errorC, 0.5), contrastText},
		{tokComment, ColorNameComment, mix(muted, bg, 0.3), contrastMuted},
		{tokPunct, ColorNamePunct, mix(fg, bg, 0.35), contrastMuted},
		{tokAnchor, ColorNameAnchor, errorC, contrastText},
		{tokDocument, ColorNameDocument, muted, contrastMuted},
	}
	for _, d := range derive {
		if d.name != "" {
			if own := c(d.name); !isUnset(own) {
				p.kinds[d.kind] = own
				continue
			}
		}
		p.kinds[d.kind] = ensureContrast(d.fallback, bg, d.ratio)
	}
	scanned := []color.Color{p.kinds[tokPlain], p.kinds[tokKey], p.kinds[tokString], p.kinds[tokNumber], p.kinds[tokLiteral], p.kinds[tokAnchor]}
	p.findMatch, p.findCurrent, p.findStroke = findColors(c(theme.ColorNameWarning), bg, scanned)
	return p
}

// findColors are the find marks' colours from the theme's warning colour (amber in Cogpit's): the
// selected match's fill as strong as the colours of the text one scans allow (kinds, each keeping a
// contrast of 3 on it; comments and punctuation are muted already), outlined in the warning colour
// itself; every other match a lighter tint of it. The text keeps its colours on both (the owner's
// QW1: the match was hard to find on the selection's colour).
func findColors(warning, bg color.Color, kinds []color.Color) (match, current, stroke color.Color) {
	current = mix(warning, bg, 0.8)
	for t := 0.35; t <= 0.8; t += 0.05 {
		fill := mix(warning, bg, t)
		ok := true
		for _, k := range kinds {
			if k != nil && contrast(k, fill) < contrastMuted {
				ok = false
				break
			}
		}
		if ok {
			current = fill
			break
		}
	}
	return mix(warning, bg, 0.78), current, warning
}

// isUnset reports whether a theme returned no colour for a name.
func isUnset(c color.Color) bool {
	if c == nil {
		return true
	}
	_, _, _, a := c.RGBA()
	return a == 0
}

// opaque returns c, or fallback when c is unset or translucent; the gutter must hide text scrolled
// beneath it.
func opaque(c, fallback color.Color) color.Color {
	if isUnset(c) {
		return fallback
	}
	if _, _, _, a := c.RGBA(); a != 0xffff {
		return fallback
	}
	return c
}

// mix blends a towards b by t (0 = a, 1 = b), ignoring alpha.
func mix(a, b color.Color, t float64) color.Color {
	ar, ag, ab := rgb(a)
	br, bg, bb := rgb(b)
	f := func(x, y float64) uint8 { return uint8(math.Round((x*(1-t) + y*t) * 255)) }
	return color.NRGBA{R: f(ar, br), G: f(ag, bg), B: f(ab, bb), A: 0xff}
}

// rgb returns the colour's channels in 0..1, un-premultiplied.
func rgb(c color.Color) (r, g, b float64) {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	return float64(n.R) / 255, float64(n.G) / 255, float64(n.B) / 255
}

// luminance is the WCAG relative luminance.
func luminance(c color.Color) float64 {
	lin := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	r, g, b := rgb(c)
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// contrast is the WCAG contrast ratio of two colours.
func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// ensureContrast darkens fg on a light background, or lightens it on a dark one, in small steps
// until it reaches ratio against bg; the hue stays recognisable.
func ensureContrast(fg, bg color.Color, ratio float64) color.Color {
	if ratio <= 0 || contrast(fg, bg) >= ratio {
		return fg
	}
	target := color.Color(color.White)
	if luminance(bg) > 0.4 {
		target = color.Black
	}
	for t := 0.1; t < 1; t += 0.1 {
		c := mix(fg, target, t)
		if contrast(c, bg) >= ratio {
			return c
		}
	}
	return target
}
