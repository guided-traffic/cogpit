package fyneui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// stripT0 is the light strip tests' clock at their start.
var stripT0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// stripClock is the strip's clock and ticker in a test: the test sets the time, and the ticker only
// records whether it would run.
type stripClock struct {
	now     time.Time
	running bool
	starts  int
}

// takeStrip stops the strip's own ticker and hands its clock and ticker to the test, the strip
// starting afresh at stripT0 from glass, as at the window's start.
func takeStrip(sh *shell) *stripClock {
	c := &stripClock{now: stripT0}
	s := sh.strip
	s.runTicker(false)
	s.now = func() time.Time { return c.now }
	s.ticker = func(on bool) {
		if on && !c.running {
			c.starts++
		}
		c.running = on
	}
	s.model = stripModel{}
	s.follow()
	return c
}

// at moves the strip's clock to stripT0 + d and draws its frame there, as a tick does.
func (c *stripClock) at(sh *shell, d time.Duration) {
	c.now = stripT0.Add(d)
	sh.strip.tick()
}

// stripShell is the tab bar's scene (tabsFake: demo's Services and Pods tabs, the Pods tab
// shown, and prod-eu's tab, Mixed environments on) with demo's accent colour and light strip set;
// the strip's clock is the test's.
func stripShell(t *testing.T, mode ThemeMode, color, anim string) (*shell, fyne.Window, *mainQueue, *fakeEngine, *stripClock) {
	t.Helper()
	f := tabsFake(t)
	if color != "" {
		f.ui.ClusterSettings = map[string]engine.ClusterSettings{"demo": {AccentColor: color, LightStrip: anim}}
	}
	sh, w, q := newTestShell(t, f, Options{Theme: mode, MCPCommand: "claude mcp add cogpit -- cogpit-cli mcp"})
	q.until(t, "the shell", func() bool { return len(sh.kinds) > 0 && sh.diag.total > 0 && len(sh.tabs.shown) == 3 })
	return sh, w, q, f, takeStrip(sh)
}

// stripLit returns the light the strip draws now: its flat rectangle's colour, and its ramps shown.
func stripLit(s *lightStrip) (flat *canvas.Rectangle, ramps []*canvas.LinearGradient) {
	for _, sl := range s.slots {
		if sl.flat.Visible() {
			flat = sl.flat
		}
		for _, g := range sl.ramps {
			if g.Visible() {
				ramps = append(ramps, g)
			}
		}
	}
	return flat, ramps
}

// The strip lies right under the view's header (LS20), the view's width, 4 pt high, and shows the
// accent colour of the shown tab's cluster, through glass: from glass at the start, back to glass
// for a tab of a cluster without one, and the colour again for the first. Without tabs the bar still
// shows, a tab high (LS11).
func TestLightStripFollowsTheShownTab(t *testing.T) {
	sh, _, q, f, c := stripShell(t, ThemeDark, "red", engine.LightStripStatic)
	s := sh.strip
	head := sh.view.head
	if !s.Visible() || s.Size().Height != stripHeight || s.Size().Width != head.Size().Width {
		t.Fatalf("the strip: shown %v, size %v, want the view header's width (%v) and %v pt", s.Visible(), s.Size(), head.Size().Width, stripHeight)
	}
	d := fyne.CurrentApp().Driver()
	if pos, hp := d.AbsolutePositionForObject(s), d.AbsolutePositionForObject(head); pos.Y != hp.Y+head.Size().Height || pos.X != hp.X {
		t.Errorf("the strip at %v, the view's header at %v (%v high): want it right under it", pos, hp, head.Size().Height)
	}
	if flat, _ := stripLit(s); flat != nil {
		t.Errorf("at the start the light shows at once: %+v", flat.FillColor)
	}
	c.at(sh, stripHalf/2)
	flat, _ := stripLit(s)
	red := s.lightColor("red")
	if flat == nil || nrgba(flat.FillColor) != strength(red, 0.5) {
		t.Fatalf("half way in: %+v, want red at half strength", flat)
	}
	c.at(sh, stripHalf)
	if flat, _ := stripLit(s); flat == nil || nrgba(flat.FillColor) != red || c.running {
		t.Fatalf("settled: %+v, ticking %v; want red, standing", flat, c.running)
	}

	// prod-eu's tab: no accent colour, so the light fades out to glass.
	f.update(func(u *engine.UIState) { u.ActiveTab = "tab-3" })
	q.drain()
	if !c.running {
		t.Error("the change does not run the clock")
	}
	c.at(sh, stripHalf+stripHalf/2)
	if flat, _ := stripLit(s); flat == nil || nrgba(flat.FillColor).A != strength(red, 0.5).A {
		t.Errorf("a quarter into the change: %+v, want red at half strength on its way out", flat)
	}
	c.at(sh, 2*stripHalf)
	if flat, ramps := stripLit(s); flat != nil || len(ramps) != 0 || c.running {
		t.Errorf("after the fade out: light %v %v, ticking %v; want glass alone, standing", flat, ramps, c.running)
	}

	// The welcome page: glass; and no tab at all: the bar shows still, as high as with tabs.
	high := sh.tabs.root.Size().Height
	f.update(func(u *engine.UIState) { u.ActiveTab, u.Welcome = "", true })
	q.drain()
	if s.look() != (stripLook{}) {
		t.Errorf("the welcome page's look %+v, want glass", s.look())
	}
	f.update(func(u *engine.UIState) { u.Tabs = nil })
	q.drain()
	if !sh.tabs.root.Visible() || sh.tabs.root.MinSize().Height != high {
		t.Errorf("without tabs the bar: shown %v, %v high; want shown, %v high", sh.tabs.root.Visible(), sh.tabs.root.MinSize().Height, high)
	}
}

// One static colour after another spreads from the shown tab's place (LS22): prod-eu's tab (its long
// context), right of demo's, makes blue spread from its middle over demo's red, which stays under it until the
// strip is blue; with motion reduced the same change crossfades (LS23).
func TestLightStripSpreadsFromTheTab(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		sh, _, q, f, c := stripShell(t, ThemeDark, "red", engine.LightStripStatic)
		sh.systemReduce = func() bool { return reduced }
		s := sh.strip
		f.update(func(u *engine.UIState) {
			u.ClusterSettings = map[string]engine.ClusterSettings{"demo": {AccentColor: "red"}, longContext: {AccentColor: "blue"}}
		})
		q.drain()
		c.at(sh, stripFade)
		change := c.now
		f.update(func(u *engine.UIState) { u.ActiveTab = "tab-3" })
		q.drain()
		top := s.model.layers[len(s.model.layers)-1]
		d := fyne.CurrentApp().Driver()
		tab := sh.tabs.activeWidget()
		want := float64((d.AbsolutePositionForObject(tab).X + tab.Size().Width/2 - d.AbsolutePositionForObject(s).X) / s.Size().Width)
		cover := coverSpread
		if reduced {
			cover = coverCross
		}
		if top.look.color != "blue" || top.cover != cover || !top.in.Equal(change) || (!reduced && !stripNear(top.origin, want, 1e-6)) || !c.running {
			t.Fatalf("reduced %v: blue's layer %+v, want cover %v at once from %v, ticking", reduced, top, cover, want)
		}
		c.at(sh, stripFade+stripFade/2)
		flat, ramps := stripLit(s)
		red, blue := s.lightColor("red"), s.lightColor("blue")
		if reduced {
			if len(ramps) != 0 || flat == nil || nrgba(flat.FillColor) != strength(blue, 0.5) {
				t.Errorf("reduced, half way: flat %+v, %d ramps; want blue at half over red", flat, len(ramps))
			}
		} else if flat == nil || nrgba(flat.FillColor) != red || len(ramps) != 3 {
			t.Errorf("half way: red under %+v, %d ramps of blue; want red whole and blue's core and edges", flat, len(ramps))
		}
		c.at(sh, 2*stripFade)
		if flat, ramps := stripLit(s); flat == nil || nrgba(flat.FillColor) != blue || len(ramps) != 0 || c.running || len(s.model.layers) != 1 {
			t.Errorf("reduced %v, done: %+v, %d ramps, ticking %v, %d layers; want blue alone, standing", reduced, flat, len(ramps), c.running, len(s.model.layers))
		}
	}
}

// The setting's animation moves the light; a colour or an animation changed in Cluster Settings is a
// change like a tab switch, through glass, the new animation from its beginning.
func TestLightStripAnimatesAndChangesThroughGlass(t *testing.T) {
	sh, _, q, f, c := stripShell(t, ThemeDark, "green", engine.LightStripScanner)
	s := sh.strip
	c.at(sh, time.Millisecond) // its clock starts with its fade-in
	_, ramps := stripLit(s)
	if len(ramps) != 2 || !c.running {
		t.Fatalf("the scanner: %d ramps, ticking %v", len(ramps), c.running)
	}
	if end := ramps[1].Position().X + ramps[1].Size().Width; end > 0.01 {
		t.Errorf("the scanner at its start reaches %v, want wholly beyond the left edge", end)
	}
	c.at(sh, 4500*time.Millisecond) // three quarters of a period: the middle, on its way back
	_, ramps = stripLit(s)
	if mid := ramps[0].Position().X + ramps[0].Size().Width; mid < s.Size().Width*0.45 || mid > s.Size().Width*0.55 {
		t.Errorf("4.5 s in the scanner's centre is at %v of %v, want the middle", mid, s.Size().Width)
	}

	change := c.now
	f.update(func(u *engine.UIState) {
		u.ClusterSettings = map[string]engine.ClusterSettings{"demo": {AccentColor: "green", LightStrip: engine.LightStripFlow}}
	})
	q.drain()
	if top := s.model.layers[len(s.model.layers)-1]; top.look.anim != engine.LightStripFlow || !top.in.Equal(change.Add(stripHalf)) {
		t.Errorf("the flow begins %v after the change, want once the scanner has faded out, %v", top.in.Sub(change), stripHalf)
	}
	since := change.Sub(stripT0)
	c.at(sh, since+stripHalf+time.Millisecond)
	if _, ramps := stripLit(s); len(ramps) != 2 || ramps[1].Position().X+ramps[1].Size().Width > 0.5 {
		t.Errorf("the flow at its beginning: %d ramps, want one light wholly outside", len(ramps))
	}
	c.at(sh, since+stripHalf+time.Duration(stripFlowPeriod*float64(time.Second))+time.Second)
	if _, ramps := stripLit(s); len(ramps) != 4 {
		t.Errorf("the flow's second pass: %d ramps, want two lights", len(ramps))
	}
}

// The clock runs only while the strip moves: never for a static light once it has faded in, nor
// while Cogpit is not the active app (the light stands, static at full strength, after a change
// through glass) or motion is reduced; back in front the animation starts afresh.
func TestLightStripTickerStopsWhenStill(t *testing.T) {
	sh, _, q, _, c := stripShell(t, ThemeDark, "red", engine.LightStripBreathing)
	s := sh.strip
	c.at(sh, stripHalf)
	if !c.running {
		t.Fatal("a breathing light does not run the clock")
	}
	c.at(sh, stripHalf+2*time.Second)
	s.setActive(false) // another app in front
	if !c.running {
		t.Error("the change to standing does not run its fade")
	}
	c.at(sh, stripHalf+2*time.Second+stripFade)
	if flat, _ := stripLit(s); c.running || flat == nil || nrgba(flat.FillColor) != s.lightColor("red") {
		t.Errorf("in the background: ticking %v, light %+v; want the clock stopped and red at full strength", c.running, flat)
	}
	starts := c.starts
	s.setActive(true)
	c.at(sh, stripHalf+2*time.Second+2*stripFade)
	top := s.model.layers[len(s.model.layers)-1]
	if !c.running || c.starts != starts+1 || top.look.anim != engine.LightStripBreathing || top.clock(c.now) != stripHalf.Seconds() {
		t.Errorf("back in front: ticking %v, the breath %v s in, want it running from its beginning", c.running, top.clock(c.now))
	}

	// Reduced motion (Preferences › App, or macOS's): the light stands as in the background.
	sh.systemReduce = func() bool { return true }
	s.follow()
	c.at(sh, stripHalf+2*time.Second+4*stripFade)
	if c.running || s.look().anim != engine.LightStripStatic {
		t.Errorf("with reduced motion: ticking %v, look %+v", c.running, s.look())
	}
	sh.systemReduce = func() bool { return false }
	q.drain()
}

// `cogpit --demo` with the demo cluster's accent colour and light strip in cluster-settings.json,
// as the lead's measurement starts it (light-strip.md § 6): the Pods tab shows, and so does its
// light.
func TestLightStripOnTheDemoCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a demo cluster")
	}
	dir := filepath.Join(t.TempDir(), "Cogpit")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	doc := `{"version": 1, "clusters": {"demo": {"accentColor": "red", "lightStrip": "scanner"}}}`
	if err := os.WriteFile(filepath.Join(dir, engine.ClusterSettingsFile), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := &engine.ClusterConnector{IncludeDemo: true}
	conn.DemoOptions.Pods, conn.DemoOptions.NoChurn, conn.DemoOptions.Seed = 300, true, 11
	s := newTestEngine(engine.Options{Connector: conn, ClusterSettings: engine.NewClusterSettingsStore(dir)})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	a := test.NewTempApp(t)
	a.Settings().SetTheme(newTheme(ThemeDark))
	w := test.NewTempWindow(t, nil)
	q := &mainQueue{}
	sh := newShell(s, a, w, Options{AutoConnect: engine.DemoContext}, q.do)
	sh.systemReduce = func() bool { return false }
	ticking := false
	sh.strip.ticker = func(on bool) { ticking = on }
	w.SetContent(sh.root)
	w.Resize(fyne.NewSize(1440, 900))
	sh.start()
	t.Cleanup(func() {
		sh.stop()
		q.drain()
	})
	q.until(t, "the demo cluster's Pods tab", func() bool {
		ui := sh.ui
		return ui.Connection.State == engine.Connected && ui.View == "pods" && shownTabContext(&ui) == engine.DemoContext
	})
	if got := sh.strip.look(); got != (stripLook{color: "red", anim: engine.LightStripScanner}) || !sh.strip.Visible() || !sh.tabs.root.Visible() || !ticking {
		t.Errorf("the strip on --demo: look %+v, shown %v, ticking %v; want red, scanning", got, sh.strip.Visible(), ticking)
	}
}

// The frame cap is stripFPS; a development build's COGPIT_LIGHTSTRIP_FPS (Options.LightStripFPS)
// lowers it for a measurement, never above stripFPS.
func TestLightStripFrameCap(t *testing.T) {
	for in, want := range map[int]int{0: stripFPS, 30: 30, 15: 15, 1: 1, 120: stripFPS} {
		if got := newLightStrip(&shell{opts: Options{LightStripFPS: in}}).fps; got != want {
			t.Errorf("LightStripFPS %d: cap %d, want %d", in, got, want)
		}
	}
}

// The real clock: while the scanner shows, its ticker posts frames to the main goroutine, one at a
// time; once the light stands (another app in front, its fade done) the next frame stops it.
func TestLightStripTickerRuns(t *testing.T) {
	sh, _, q, _, c := stripShell(t, ThemeDark, "red", engine.LightStripScanner)
	s := sh.strip
	frames := 0
	s.now = func() time.Time { frames++; return c.now }
	s.ticker = s.runTicker
	s.follow()
	if s.stop == nil {
		t.Fatal("a scanner does not start the clock")
	}
	start := frames
	q.until(t, "frames from the clock", func() bool { return frames >= start+3 })
	s.setActive(false)
	c.now = c.now.Add(stripFade + time.Second) // the fade to standing is over
	q.until(t, "the clock stopped", func() bool { return s.stop == nil })
	// A frame the goroutine posted as it stopped runs and draws nothing new; then none comes.
	ended := make(chan struct{})
	go func() { s.ticking.Wait(); close(ended) }()
	select {
	case <-ended:
	case <-time.After(15 * time.Second):
		t.Fatal("the clock's goroutine did not end")
	}
	if q.drain(); s.pending.Load() || s.stop != nil {
		t.Errorf("after the clock stopped: a frame posted %v, a clock %v", s.pending.Load(), s.stop != nil)
	}
}
