package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// sliderTheme draws a slider as the design language has it: a 3 pt track in the bracket's line
// colour, the filled part and the knob in the interactive blue. Fyne draws the track in the input
// background, which on a card is the card's own colour: a knob on nothing. Only the slider is
// themed this way (themedSlider), so no other control changes.
type sliderTheme struct{ fyne.Theme }

func (t sliderTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameInputBackground:
		return themeColor(t.Theme, colorBracket)
	case theme.ColorNameForeground:
		return t.Theme.Color(theme.ColorNamePrimary, v)
	}
	return t.Theme.Color(n, v)
}

// Size makes the track 3 pt: Fyne's track is twice the input border's width.
func (t sliderTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameInputBorder {
		return 1.5
	}
	return t.Theme.Size(n)
}

// themedSlider wraps a slider in sliderTheme, following the app's theme as it changes.
func themedSlider(s fyne.CanvasObject) fyne.CanvasObject {
	return container.NewThemeOverride(s, sliderTheme{fyne.CurrentApp().Settings().Theme()})
}

// fixedSpace is empty space of w by h points: the design language's rhythm (6 pt label to control,
// 24 pt between settings) is in points, and no theme size has those values.
func fixedSpace(w, h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(w, h))
	return r
}

// SetText changes a paragraph's text.
func (p *paragraph) SetText(s string) {
	if s == p.text {
		return
	}
	p.text = s
	p.Refresh()
}
