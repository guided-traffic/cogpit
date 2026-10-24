package fyneui

import (
	"image"
	"image/color"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The light strip (D-new, the owner's VW14 of 2026-10-24): a strip of frosted glass under the tab
// bar, the width of the content column (not under the navigator or the chat), that shows the accent
// colour (D-80) of the cluster whose tab is shown, so that the user knows at a glance where they
// work: "Bin ich Beispielsweise auf Prod, wünsche ich mir einen Roten Akzent umd direkt zu wissen
// dass ich hier vorsichtig sein muss". It stands in the tab bar's place of its divider and shows and
// hides with the bar.
//
//   - Off, frosted glass with a fine grain, dark or light with the theme, for a cluster without an
//     accent colour, the welcome page, and while no tab of a connected cluster is shown ("Stellt der
//     Nutzer keine Akzentfarbe ein, ist der Leuchtstreifen ausgeschaltet"). Lit, the glass stays:
//     the light is drawn in it and the grain over the light ("Auch beleuchtet soll die Milchglas
//     Optik erhalten bleiben").
//   - The cluster's light strip setting moves the light: static, breathing, scanner or flow
//     (engine/lightstrip.go), its maths in lightstrip_model.go.
//   - Every change goes through glass: the old light fades out in 350 ms, then the new one fades in
//     in 350 ms from its beginning (a tab of another cluster shown, its colour or animation changed
//     in Cluster Settings, the window's start).
//   - It stands still, the static look at full strength, while Cogpit is not the active app, and
//     while motion is reduced (Preferences › App › Reduce motion or macOS's own): the lead's
//     decision, as for every motion in Cogpit (preferences_motion.go). Going to stand and back is a
//     change like any other, through glass, so that a scanner never resumes mid-sweep.
//   - Its clock is a ticker at the frame cap, posting a frame to the main goroutine, and it runs only
//     while something moves: a fade, or an animation shown. Static, off, a finished fade, the
//     background and reduced motion stop it, and with it the window's repaints.

// stripSlots are the lights the strip can draw at once: through glass only one shows at a time,
// so two leave room.
const stripSlots = 2

// lightStrip is the strip widget. Its objects are its own, made once and changed in place: a
// frame moves the scanner's and the flow's ramps (Fyne keeps their rasterised textures) and
// recolours a rectangle, never more.
type lightStrip struct {
	widget.BaseWidget
	sh     *shell
	now    func() time.Time
	fps    int  // the frame cap (stripFPS, or a development build's COGPIT_LIGHTSTRIP_FPS)
	active bool // Cogpit is the active app (the app's lifecycle); it starts so
	model  stripModel
	// ticker runs the clock while on and stops it otherwise (runTicker; a test records it).
	ticker  func(on bool)
	stop    chan struct{} // the running ticker's; nil while none runs
	pending atomic.Bool   // a frame is posted and has not run yet

	glass   *canvas.Rectangle
	slots   [stripSlots]*stripSlot
	grain   *canvas.Raster
	grainAt image.Point // the size the grain was last tiled for, in device pixels
	grainIm *image.NRGBA
	objects []fyne.CanvasObject
}

// stripSlot is one light's objects: a rectangle for a flat light, and the ramps of a scanner or a
// flow (two comets of two ramps each at most).
type stripSlot struct {
	flat  *canvas.Rectangle
	ramps [4]*canvas.LinearGradient
}

func newLightStrip(sh *shell) *lightStrip {
	s := &lightStrip{sh: sh, now: time.Now, fps: stripFPS, active: true,
		glass: canvas.NewRectangle(color.Transparent)}
	if sh != nil && sh.opts.LightStripFPS > 0 {
		s.fps = min(sh.opts.LightStripFPS, stripFPS)
	}
	s.ticker = s.runTicker
	s.objects = append(s.objects, s.glass)
	for i := range s.slots {
		sl := &stripSlot{flat: canvas.NewRectangle(color.Transparent)}
		sl.flat.Hide()
		s.objects = append(s.objects, sl.flat)
		for j := range sl.ramps {
			g := canvas.NewHorizontalGradient(color.Transparent, color.Transparent)
			g.Hide()
			sl.ramps[j] = g
			s.objects = append(s.objects, g)
		}
		s.slots[i] = sl
	}
	s.grain = canvas.NewRaster(s.grainImage)
	s.grain.ScaleMode = canvas.ImageScalePixels // a speck per device pixel, never smoothed
	s.objects = append(s.objects, s.grain)
	s.ExtendBaseWidget(s)
	return s
}

// grainImage is the grain raster's picture for w × h device pixels, tiled again only when the
// strip's size changed.
func (s *lightStrip) grainImage(w, h int) image.Image {
	if at := image.Pt(w, h); s.grainIm == nil || s.grainAt != at {
		s.grainIm, s.grainAt = stripGrain(w, h), at
	}
	return s.grainIm
}

// look is what the strip shows for the window's state: the accent colour of the shown tab's
// cluster, moving as its setting says while Cogpit is active and motion is not reduced; glass
// without one.
func (s *lightStrip) look() stripLook {
	ui := &s.sh.ui
	ctx := shownTabContext(ui)
	cs := ui.ClusterSettings[ctx]
	if ctx == "" || cs.AccentColor == "" {
		return stripLook{}
	}
	anim := engine.LightStripOf(cs)
	if !s.active || s.sh.reduceMotion() {
		anim = engine.LightStripStatic
	}
	return stripLook{color: cs.AccentColor, anim: anim}
}

// shownTabContext is the cluster of the tab the main tab bar shows as active, "" when it shows none
// (no tab, the welcome page, the active tab's cluster not connected).
func shownTabContext(ui *engine.UIState) string {
	if ui.Welcome || ui.ActiveTab == "" {
		return ""
	}
	for _, t := range ui.TabsShown() {
		if t.ID == ui.ActiveTab {
			return t.Context
		}
	}
	return ""
}

// follow shows the look the window's state asks for: a change goes through glass, and the clock
// runs while something moves. Main goroutine; the shell calls it after every UI state, the app's
// lifecycle and macOS's Reduce motion call it when they change.
func (s *lightStrip) follow() {
	now := s.now()
	s.model.retarget(s.look(), now)
	s.paint(now)
	s.ticker(s.model.moving(now))
}

// setActive follows Cogpit becoming the active app or another one.
func (s *lightStrip) setActive(on bool) {
	if s.active != on {
		s.active = on
		s.follow()
	}
}

// tick draws a frame and stops the clock once nothing moves.
func (s *lightStrip) tick() {
	now := s.now()
	s.paint(now)
	s.ticker(s.model.moving(now))
}

// runTicker starts the clock when on and none runs, and stops it otherwise: a goroutine that
// posts a frame to the main goroutine at the frame cap, never a second one while the first waits.
func (s *lightStrip) runTicker(on bool) {
	switch {
	case on && s.stop == nil:
		stop := make(chan struct{})
		s.stop = stop
		every, stopAll := time.Second/time.Duration(max(1, s.fps)), s.sh.stopTickers
		go func() {
			t := time.NewTicker(every)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					if s.pending.CompareAndSwap(false, true) {
						s.sh.do(func() {
							s.pending.Store(false)
							if s.stop == stop { // not a frame of a clock stopped meanwhile
								s.tick()
							}
						})
					}
				case <-stop:
					return
				case <-stopAll:
					return
				}
			}
		}()
	case !on && s.stop != nil:
		close(s.stop)
		s.stop = nil
	}
}

// paint draws the strip at now: the lights shown, each at its eased opacity and its animation's
// time, in the slots; the rest hidden.
func (s *lightStrip) paint(now time.Time) {
	s.model.prune(now)
	size := s.glass.Size()
	var shown []*stripLayer
	for _, l := range s.model.layers {
		if l.look.lit() && l.opacity(now) > 0 {
			shown = append(shown, l)
		}
	}
	if len(shown) > stripSlots {
		shown = shown[len(shown)-stripSlots:]
	}
	for i, sl := range s.slots {
		if i >= len(shown) {
			sl.hide()
			continue
		}
		l := shown[i]
		flat, ramps := layerPaint(l.look.anim, l.clock(now), size.Width, stripGlow*smoothstep(l.opacity(now)))
		sl.show(s.lightColor(l.look.color), flat, ramps, size)
	}
}

// lightColor is an accent colour in the theme's legible shade: one of the eight accents', or a
// colour picked freely in a shade of its hue (identitycolor.go), as its swatch shows it.
func (s *lightStrip) lightColor(c string) color.NRGBA {
	return nrgba(themeColor(s.Theme(), identityColorName(c)))
}

// strength is col at alpha a.
func strength(col color.NRGBA, a float64) color.NRGBA {
	col.A = uint8(clamp01(a)*255 + 0.5)
	return col
}

// show draws a light: a flat light over the whole strip, or its ramps.
func (sl *stripSlot) show(col color.NRGBA, flat float64, ramps []stripRamp, size fyne.Size) {
	if flat >= 0 {
		sl.flat.Resize(size)
		setFill(sl.flat, strength(col, flat))
		setShown(sl.flat, true)
	} else {
		setShown(sl.flat, false)
	}
	for i, g := range sl.ramps {
		if i >= len(ramps) {
			setShown(g, false)
			continue
		}
		r := ramps[i]
		g.Resize(fyne.NewSize(r.x2-r.x1, size.Height)) // rasterised again only when its size changes
		g.Move(fyne.NewPos(r.x1, 0))                   // a move only repaints: the texture is kept
		start, end := strength(col, r.a1), strength(col, r.a2)
		if g.StartColor != start || g.EndColor != end {
			g.StartColor, g.EndColor = start, end
			g.Refresh()
		}
		setShown(g, true)
	}
}

// hide hides the slot's light.
func (sl *stripSlot) hide() {
	setShown(sl.flat, false)
	for _, g := range sl.ramps {
		setShown(g, false)
	}
}

// setFill recolours a rectangle when its colour changed.
func setFill(r *canvas.Rectangle, c color.Color) {
	if r.FillColor != c {
		r.FillColor = c
		r.Refresh()
	}
}

// setShown shows or hides an object when that changes, and repaints.
func setShown(o fyne.CanvasObject, on bool) {
	if o.Visible() == on {
		return
	}
	if on {
		o.Show()
	} else {
		o.Hide()
	}
	canvas.Refresh(o)
}

// CreateRenderer implements fyne.Widget.
func (s *lightStrip) CreateRenderer() fyne.WidgetRenderer {
	r := &lightStripRenderer{s: s}
	r.Refresh()
	return r
}

// lightStripRenderer lays the strip's objects out and clips them to the strip (IsClip): a scanner's
// or a flow's ramp lies partly beyond its edges as it comes in and runs out.
type lightStripRenderer struct{ s *lightStrip }

func (r *lightStripRenderer) Layout(size fyne.Size) {
	s := r.s
	s.glass.Resize(size)
	s.grain.Resize(size)
	s.paint(s.now()) // the lights' places follow the width
}

func (r *lightStripRenderer) MinSize() fyne.Size { return fyne.NewSize(0, stripHeight) }

// Refresh follows the theme: the glass's colour and the lights' shades.
func (r *lightStripRenderer) Refresh() {
	s := r.s
	setFill(s.glass, themeColor(s.Theme(), colorLightStrip))
	s.paint(s.now())
}

func (r *lightStripRenderer) Objects() []fyne.CanvasObject { return r.s.objects }
func (r *lightStripRenderer) Destroy()                     {}

// IsClip marks the strip as a clip for Fyne's painters: nothing of it is drawn beyond its bounds.
func (r *lightStripRenderer) IsClip() {}
