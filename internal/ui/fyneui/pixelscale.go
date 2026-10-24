package fyneui

import (
	"math"

	"fyne.io/fyne/v2"
)

// Text after a change of the window's pixel scale (D-121). Fyne 2.8.2 keeps a text's texture by
// its text, size, style, font, colour and canvas, not by the resolution it was drawn at
// (internal/cache/text.go, painter/gl/texture.go). On macOS a change of the window's backing scale
// (a display at another scale, the displays switched off and on again around a sleep) only sets
// the canvas's framebuffer density, Fyne's texScale (glfw/window.go processFrameSized), and
// refreshes the content: the texts drawn before keep their textures at the old resolution, soft on
// a 2× display. Fyne drops them only in RescaleContext (DeleteTextTexturesFor), through reloadScale:
// from its settings listener (glfw/loop.go) or when its own scale changes on a move, and SetTheme
// always notifies the listener: the theme switch that sharpened a colleague's text after a wake. So Cogpit applies the current theme again, once, when the density
// changed. On Windows and Linux (X11) Fyne's scale changes go through reloadScale already.

// densityProbe is the length in points whose pixels canvasDensity counts: long enough that the
// rounding of PixelCoordinateForPosition stays below a ten-thousandth.
const densityProbe = 10000

// densityTolerance is the least difference between two densities that counts as a change: more
// than canvasDensity's rounding, less than any real scale step.
const densityTolerance = 0.01

// canvasDensity is the canvas's framebuffer pixels per canvas pixel: Fyne's texScale, 2 on a
// Retina display on macOS, else 1. PixelCoordinateForPosition multiplies the canvas scale by it
// (pixelsFor).
func canvasDensity(c fyne.Canvas) float32 {
	px, _ := c.PixelCoordinateForPosition(fyne.NewPos(densityProbe, 0))
	s := c.Scale()
	if s <= 0 {
		s = 1
	}
	return float32(px) / densityProbe / s
}

// densityWatch applies the theme again when the window's density differs from the one the texts
// were last drawn for: the window's at the start or at the last re-apply. Main goroutine only.
type densityWatch struct {
	read    func() float32 // the density now (canvasDensity)
	reapply func()
	last    float32 // 0 until the first reading
}

// check reads the density and applies the theme again if it changed since the last reading. The
// first reading only records it, and a window without a framebuffer (a density of 0) changes
// nothing.
func (d *densityWatch) check() {
	now := d.read()
	if now <= 0 || math.Abs(float64(now-d.last)) < densityTolerance {
		return
	}
	was := d.last
	d.last = now
	if was > 0 {
		d.reapply()
	}
}

// reapplyTheme applies the current theme again: Fyne's settings listener clears its font and
// theme caches, drops the windows' text textures and refreshes every widget, as a theme switch
// does (preferences.go), with the same theme.
func reapplyTheme(a fyne.App) {
	a.Settings().SetTheme(a.Settings().Theme())
}

// watchPixelScale starts the window's density watch (D-121): the density read now, and read again
// whenever macOS reports that the window's backing properties changed (pixelscale_darwin.go); no
// reports, so no watch, on other systems. Main goroutine, once the window exists (queued from
// OnStarted).
func (sh *shell) watchPixelScale() {
	sh.startDensityWatch(func() float32 { return canvasDensity(sh.win.Canvas()) }, watchBackingScale)
}

// startDensityWatch records the density read gives and hands watch the function that checks it
// again on the main goroutine (sh.do): after the report's pass, when Fyne has taken the new
// framebuffer size.
func (sh *shell) startDensityWatch(read func() float32, watch func(fyne.Window, func())) {
	d := &densityWatch{read: read, reapply: func() { reapplyTheme(sh.app) }}
	d.check()
	watch(sh.win, func() { sh.do(d.check) })
}
