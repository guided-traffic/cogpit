package fyneui

import (
	"os"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/layout"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The window's zoom (D-134, engine/zoom.go): VS Code's, which scales the whole window by Chromium's
// page zoom, text, icons, room and panels alike, the window keeping its size on the screen. Fyne's
// equivalent is the canvas scale: every size is in Fyne units times the scale, and text is drawn
// again at the new size, sharp. Fyne 2.8.2 has no call that sets it at run time; it reads it, on
// every recalculation, as FYNE_SCALE (glfw's userScale) times the system's, and recalculates every
// window's when its settings change (glfw/loop.go's settings listener: reloadScale). So the zoom
// sets FYNE_SCALE in Cogpit's own environment to the scale asked for at the start times the zoom's
// factor and applies the theme again (reapplyTheme, pixelscale.go). The lead's reading of Fyne's
// source; Fyne's settings could carry a scale of their own only in its settings.json, which every
// Fyne app on the computer shares, and a wrapper around them would break Fyne's own type assertion
// on macOS (app_desktop_darwin.go).
//
// Fyne's rescale keeps a window's content size in Fyne units (RescaleContext, made for a move to a
// display of another density): the window would grow by the factor. applyZoom gives it back its
// size on the screen at once, in the same pass on the main goroutine, as VS Code's window keeps its
// size; full screen keeps the screen. The shell started from the local terminal gets FYNE_SCALE as
// Cogpit was started with it (terminal.baseEnv).

// fyneScaleEnv is Fyne's scale variable.
const fyneScaleEnv = "FYNE_SCALE"

// windowZoom is the zoom the window is drawn at.
type windowZoom struct {
	level int
	// ceiling is the largest level the window's content fits the window at (zoomRefit), fitted
	// whether it was found; fitSize the window's size and fitLevel the level it was found for.
	ceiling  int
	fitted   bool
	fitSize  fyne.Size
	fitLevel int
	// base is the canvas scale asked for at the start, which the zoom multiplies: FYNE_SCALE, else
	// Fyne's own scale setting, else 1.
	base float64
	// orig is FYNE_SCALE as Cogpit was started with it, origSet whether it was set: what 100% gives
	// back.
	orig    string
	origSet bool
	// setenv sets FYNE_SCALE (ok) or unsets it; nil in tests, whose canvases have no scale to change
	// and whose processes run other tests.
	setenv func(value string, ok bool)
}

// newWindowZoom is the zoom of the real window, at 100% until set: the scale asked for read from
// FYNE_SCALE and Fyne's scale setting.
func newWindowZoom(setting float32) *windowZoom {
	orig, set := os.LookupEnv(fyneScaleEnv)
	return &windowZoom{base: float64(canvasScaleFor(orig, setting)), orig: orig, origSet: set,
		setenv: func(v string, ok bool) {
			if ok {
				_ = os.Setenv(fyneScaleEnv, v) // only fails for an invalid name or value
				return
			}
			_ = os.Unsetenv(fyneScaleEnv)
		}}
}

// set makes level the zoom Fyne's next scale calculation reads; it reports whether that changed.
func (z *windowZoom) set(level int) bool {
	level = max(engine.MinZoomLevel, min(engine.MaxZoomLevel, level))
	if level == z.level {
		return false
	}
	z.level = level
	if z.setenv == nil {
		return true
	}
	if level == 0 {
		z.setenv(z.orig, z.origSet)
		return true
	}
	z.setenv(strconv.FormatFloat(z.base*engine.ZoomFactor(level), 'f', 3, 64), true)
	return true
}

// applyZoom draws the window at the zoom level: the canvas scale changed and every text drawn
// again, the window keeping its size on the screen. Main goroutine.
func (sh *shell) applyZoom(level int) {
	c := sh.win.Canvas()
	was, size := c.Scale(), c.Size()
	if sh.zoom.set(level) {
		reapplyTheme(sh.app) // Fyne's settings listener: every window's scale read again (reloadScale)
		if now := c.Scale(); now > 0 && now != was && !sh.win.FullScreen() {
			// Fyne kept the content's size in Fyne units and grew the window; the screen's size back.
			sh.win.Resize(fyne.NewSize(size.Width*was/now, size.Height*was/now))
		}
	}
	sh.zoomRefit()
	sh.status.zoom.set(sh.zoom.level, sh.zoom.ceiling)
}

// The window's fit (engine/zoom.go, ErrZoomFit): Zoom In stops at the largest level whose content
// fits the window as it is, since Fyne would widen the window to the content's minimum size and VS
// Code's window never grows when it zooms. Sizes in Fyne units scale with the zoom, so the content's
// minimum size at another level is the one now times the factors' ratio (text measures within a
// fraction of a unit of it).

// zoomCeiling is the largest zoom level, from the one shown up, at which the window's content
// fits the window as it is now.
func (sh *shell) zoomCeiling(size fyne.Size) int {
	least := sh.root.MinSize()
	now := engine.ZoomFactor(sh.zoom.level)
	ceiling := sh.zoom.level
	for l := sh.zoom.level + 1; l <= engine.MaxZoomLevel; l++ {
		r := float32(engine.ZoomFactor(l) / now)
		if least.Width*r > size.Width+0.5 || least.Height*r > size.Height+0.5 {
			break
		}
		ceiling = l
	}
	return ceiling
}

// zoomRefit finds the window's fit again when its size or the zoom changed since, tells the session
// and greys the status bar's + where the next level would not fit. Main goroutine.
func (sh *shell) zoomRefit() {
	size := sh.win.Canvas().Size()
	z := sh.zoom
	if z.fitted && size == z.fitSize && z.level == z.fitLevel {
		return
	}
	z.fitSize, z.fitLevel = size, z.level
	ceiling := sh.zoomCeiling(size)
	if z.fitted && ceiling == z.ceiling {
		return
	}
	z.ceiling, z.fitted = ceiling, true
	if sh.sess != nil {
		sh.sess.SetZoomCeiling(ceiling)
	}
	if sh.status != nil {
		sh.status.zoom.set(z.level, ceiling)
	}
}

// zoomFitLayout is the window's root: a stack, which follows the window's size for the zoom's fit.
type zoomFitLayout struct{ sh *shell }

func (zoomFitLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return layout.NewStackLayout().MinSize(objs)
}

func (l *zoomFitLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	layout.NewStackLayout().Layout(objs, size)
	l.sh.zoomRefit()
}

// zoomCommands are the View menu's zoom items, in VS Code's order.
var zoomCommands = []struct {
	id  string
	run func(sh *shell)
}{
	{"ui.zoom.in", (*shell).zoomIn},
	{"ui.zoom.out", (*shell).zoomOut},
	{"ui.zoom.reset", (*shell).zoomReset},
}

// zoomMenuItems are View › Zoom In, Zoom Out and Reset Zoom with their keys, which work from any
// focus (a main menu's shortcut), as VS Code's do in its terminal and its fields.
func (sh *shell) zoomMenuItems() []*fyne.MenuItem {
	var out []*fyne.MenuItem
	for _, z := range zoomCommands {
		c, ok := sh.eng.Registry().Get(z.id)
		if !ok {
			continue
		}
		run := z.run
		out = append(out, sh.withShortcut(fyne.NewMenuItem(c.Label, func() { run(sh) }), z.id))
	}
	return out
}

// zoomIn, zoomOut and zoomReset are the keys' and the menu's: at the end of the range a key does
// nothing, as VS Code's, where the command would refuse.
func (sh *shell) zoomIn() {
	sh.zoom.fitted = false // the content may have grown since the window last changed its size
	sh.zoomRefit()
	switch {
	case sh.zoom.level >= engine.MaxZoomLevel:
	case sh.zoom.level >= sh.zoom.ceiling:
		sh.toast("info", "Zoom In: "+engine.ZoomFitError(sh.zoom.level+1).Error()+".")
	default:
		sh.dispatch("ui.zoom.in", struct{}{}, nil)
	}
}

func (sh *shell) zoomOut() {
	if sh.zoom.level > engine.MinZoomLevel {
		sh.dispatch("ui.zoom.out", struct{}{}, nil)
	}
}

func (sh *shell) zoomReset() {
	if sh.zoom.level != 0 {
		sh.dispatch("ui.zoom.reset", struct{}{}, nil)
	}
}
