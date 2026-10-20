package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// TestRenderGlowPrimitives draws the design prototype's shapes on the stage, once per theme, for a
// look: to COGPIT_PNG_DIR or a temporary directory.
func TestRenderGlowPrimitives(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		t.Run(string(mode), func(t *testing.T) {
			a := test.NewTempApp(t)
			a.Settings().SetTheme(newTheme(mode))
			sq := func(c ...string) []tok {
				out := []tok{}
				for _, n := range c {
					out = append(out, tok{color: fyneColor(n), weight: 1})
				}
				return out
			}
			cyan := newTokenRow(14, sq("a", "a", "a", "a", "a", "a", "a", "a", "a", "a", "a", "a")...)
			cyan.fade = true
			bars := newTokenRow(10, sq("a", "a", "a", "a", "a", "a")...)
			bars.bars, bars.fade = true, true
			long := newTokenRow(10, sq("a", "a", "a")...)
			long.bars = true
			status := newTokenRow(10, tok{color: colorStatusOK}, tok{color: colorStatusOK}, tok{color: colorStatusWarning},
				tok{color: colorStatusError}, tok{color: colorTokenDim, hollow: true})
			thinking := newWave(colorAgent, 4, 0.5)
			thinking.min = fyne.NewSize(420, 40)
			spark := newWave(colorAccent, 0, 0)
			spark.SetValues([]float64{3, 4, 4, 6, 5, 7, 9, 8, 6, 7, 5, 6, 8, 11, 9})
			spark.min = fyne.NewSize(240, 36)
			glow := newGlowFrame(widget.NewButton("Delete 12 pods", nil), colorStatusError, 7)
			hz := newHorizon()
			col := container.NewVBox(
				newBracketLabel("paper 2 · the thinking", colorAgent), thinking,
				newBracketLabel("paper 1 · the writing", colorAccent), cyan, bars, long,
				newBracketLabel("pod · inventory", ""), status,
				newBracketLabel("cpu · 1h", colorAccent), spark,
				container.NewPadded(glow),
			)
			w := test.NewTempWindow(t, container.NewStack(hz, container.NewPadded(container.NewHBox(col))))
			w.Resize(fyne.NewSize(1440, 900))
			savePNG(t, w, "glow_primitives_"+string(mode)+".png")
		})
	}
}

// fyneColor maps the test's short names to tokens.
func fyneColor(n string) fyne.ThemeColorName {
	if n == "a" {
		return colorAccent
	}
	return fyne.ThemeColorName(n)
}
