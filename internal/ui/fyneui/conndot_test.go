package fyneui

import (
	"image/color"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Every dot reads connectionColor: Connected and a (re)connect in the Running containers' green,
// a failed connect and an expired login red, Not connected grey; no amber (F13).
func TestConnectionColor(t *testing.T) {
	for s, want := range map[engine.ConnectionState]fyne.ThemeColorName{
		engine.Connected: colorContainerRunning, engine.Connecting: colorContainerRunning, engine.Interrupted: colorContainerRunning,
		engine.ConnectError: colorStatusError, engine.LoginExpired: colorStatusError, engine.Disconnected: colorStatusUnknown,
	} {
		if got := connectionColor(engine.Connection{State: s}); got != want {
			t.Errorf("%s: %s, want %s", s, got, want)
		}
	}
}

// blinkClock gives a shell's blinker a clock the test moves and records its ticker instead of
// running one: tick is called by the test, never by a timer.
type blinkClock struct {
	now     time.Time
	running bool
	b       *blinker
}

func fakeBlinkClock(sh *shell) *blinkClock {
	c := &blinkClock{now: interruptedAt, b: sh.blink}
	sh.blink.now = func() time.Time { return c.now }
	sh.blink.ticker = func(on bool) { c.running = on }
	return c
}

// after moves the clock on by d and runs a tick, as the ticker would.
func (c *blinkClock) after(d time.Duration) {
	c.now = c.now.Add(d)
	c.b.tick()
}

// dotsOf are the window's shown dots of a cluster (a hidden one, such as the navigator's section
// behind a cluster's tree, follows when it shows).
func dotsOf(w fyne.Window, ctx string) []*connDot {
	var out []*connDot
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		if d, ok := o.(*connDot); ok && d.context == ctx {
			out = append(out, d)
		}
		switch x := o.(type) {
		case *fyne.Container:
			for _, c := range x.Objects {
				walk(c)
			}
		case fyne.Widget:
			for _, c := range test.WidgetRenderer(x).Objects() {
				walk(c)
			}
		}
	}
	walk(w.Content())
	return out
}

// drawn is a dot's colour as drawn.
func drawn(d *connDot) color.NRGBA {
	c := test.WidgetRenderer(d).Objects()[0].(*canvas.Circle).FillColor
	return color.NRGBAModel.Convert(c).(color.NRGBA)
}

// checkDots fails unless every dot of the cluster is drawn in token at alpha (of 255).
func checkDots(t *testing.T, w fyne.Window, ctx string, token fyne.ThemeColorName, alpha uint8, what string) {
	t.Helper()
	want := color.NRGBAModel.Convert(themeColor(fyne.CurrentApp().Settings().Theme(), token)).(color.NRGBA)
	want.A = alpha
	for _, d := range dotsOf(w, ctx) {
		if got := drawn(d); got != want {
			t.Errorf("%s: a dot of %s is %v, want %v (%s)", what, ctx, got, want, token)
		}
	}
}

// The alphas a dot is drawn with, as the renderer computes them (of 255).
var (
	alphaFull  uint8 = 255
	alphaDim         = scaleAlpha(255, blinkDim)
	alphaStill       = scaleAlpha(255, blinkStill)
)

// While a cluster reconnects every dot of it blinks in step, 500 ms full and 500 ms dimmed: the
// status bar's, the navigator's root, its hotbar tile's (the top bar has none, D-54); the other
// clusters' do not.
// The ticker runs only while it blinks.
func TestDotsBlinkWhileReconnecting(t *testing.T) {
	sh, w, q, f, _ := hotbarScene(t, ThemeDark)
	clk := fakeBlinkClock(sh)
	const ctx = "kind-cogpit"
	checkDots(t, w, ctx, colorContainerRunning, alphaFull, "connected")
	if clk.running {
		t.Fatal("a ticker runs without a blink")
	}

	interrupt(f, engine.Interrupted, clk.now)
	q.until(t, "the blink", func() bool { return sh.blink.blinking(ctx) })
	q.until(t, "the tile's state", func() bool {
		for _, ti := range tilesOf(sh.hot) {
			if ti.tile.Context == ctx {
				return ti.tile.Connection == engine.Interrupted
			}
		}
		return false
	})
	sites := map[*connDot]string{sh.status.dot: "status bar", sh.side.dot: "navigator root"}
	for _, ti := range tilesOf(sh.hot) {
		if ti.tile.Context == ctx {
			sites[test.WidgetRenderer(ti).(*hotbarTileRenderer).dot] = "hotbar tile"
		}
	}
	all := dotsOf(w, ctx)
	for d, name := range sites {
		if d.context != ctx {
			t.Errorf("the %s's dot stands for %q", name, d.context)
		}
	}
	if len(all) < len(sites) || len(sites) != 3 {
		t.Fatalf("%d dots of the cluster, %d sites", len(all), len(sites))
	}
	if !clk.running {
		t.Error("no ticker while it blinks")
	}
	checkDots(t, w, ctx, colorContainerRunning, alphaFull, "the first phase")
	clk.after(blinkPhase)
	checkDots(t, w, ctx, colorContainerRunning, alphaDim, "the second phase")
	clk.after(blinkPhase)
	checkDots(t, w, ctx, colorContainerRunning, alphaFull, "the third phase")
	for _, other := range []string{"prod-eu-central", "staging-us-east"} {
		checkDots(t, w, other, colorStatusUnknown, alphaFull, "another cluster")
	}

	clk.after(10 * time.Second) // a long reconnect blinks on
	clk.after(blinkPhase)
	if !sh.blink.blinking(ctx) || !clk.running {
		t.Fatal("the blink ended while the engine still reconnects")
	}
	f.update(func(u *engine.UIState) { u.Connection = engine.Connection{State: engine.Connected} })
	q.until(t, "connected", func() bool { return sh.ui.Connection.State == engine.Connected })
	clk.after(blinkPhase) // the minimum ran out long ago: it stops at the next phase
	if clk.running || sh.blink.any() {
		t.Errorf("still blinking after the reconnect: ticker %v, a cluster blinking %v", clk.running, sh.blink.any())
	}
	checkDots(t, w, ctx, colorContainerRunning, alphaFull, "connected again")
}

// A reconnect that succeeds at once still blinks three intervals, from the moment the window saw
// it: the window's minimum, while the engine's state is Connected already.
func TestBlinkLastsThreeIntervalsAfterAQuickReconnect(t *testing.T) {
	f := connectedFake(t, 10)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	clk := fakeBlinkClock(sh)
	interrupt(f, engine.Interrupted, clk.now)
	q.until(t, "the blink", func() bool { return sh.blink.blinking("demo") })
	clk.now = clk.now.Add(100 * time.Millisecond)
	f.update(func(u *engine.UIState) { u.Connection = engine.Connection{State: engine.Connected, Demo: true} })
	q.until(t, "connected", func() bool { return sh.ui.Connection.State == engine.Connected })
	clk.after(400 * time.Millisecond) // 500 ms after the start
	checkDots(t, w, "demo", colorContainerRunning, alphaDim, "connected, 0.5 s")
	clk.after(2400 * time.Millisecond) // 2.9 s
	checkDots(t, w, "demo", colorContainerRunning, alphaDim, "connected, 2.9 s")
	if !clk.running {
		t.Error("the ticker stopped before the minimum")
	}
	clk.after(100 * time.Millisecond) // 3 s
	checkDots(t, w, "demo", colorContainerRunning, alphaFull, "connected, 3 s")
	if clk.running {
		t.Error("the ticker runs after the minimum")
	}
}

// An expired login (and a failed connect) ends the blink at once: the dots turn red, steady.
func TestLoginExpiredStopsTheBlink(t *testing.T) {
	f := connectedFake(t, 10)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	clk := fakeBlinkClock(sh)
	interrupt(f, engine.Interrupted, clk.now)
	q.until(t, "the blink", func() bool { return sh.blink.blinking("demo") })
	clk.after(blinkPhase)
	checkDots(t, w, "demo", colorContainerRunning, alphaDim, "dimmed")
	interrupt(f, engine.LoginExpired, clk.now)
	q.until(t, "the login expired", func() bool { return sh.ui.Connection.State == engine.LoginExpired })
	checkDots(t, w, "demo", colorStatusError, alphaFull, "login expired")
	if clk.running || sh.blink.any() {
		t.Errorf("the blink goes on: ticker %v, a cluster blinking %v", clk.running, sh.blink.any())
	}
}

// Under Reduce motion the dots do not blink: a steady half-alpha green while (re)connecting.
func TestReduceMotionKeepsTheDotSteady(t *testing.T) {
	f := connectedFake(t, 10)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	clk := fakeBlinkClock(sh)
	sh.blink.reduce = func() bool { return true }
	f.update(func(u *engine.UIState) { u.Connection = engine.Connection{State: engine.Connecting, Demo: true} })
	q.until(t, "connecting", func() bool { return sh.blink.blinking("demo") })
	checkDots(t, w, "demo", colorContainerRunning, alphaStill, "first phase")
	clk.after(blinkPhase)
	checkDots(t, w, "demo", colorContainerRunning, alphaStill, "second phase")
}

// The dot's tooltip names the state, why and since when, the next attempt, and what a click does;
// the bars say no word of it.
func TestDotTooltipNamesTheState(t *testing.T) {
	f := connectedFake(t, 10)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 })
	now := interruptedAt.Add(4 * time.Minute)
	sh.now = func() time.Time { return now }
	interrupt(f, engine.Interrupted, now)
	q.until(t, "interrupted", func() bool { return sh.ui.Connection.State == engine.Interrupted })
	settle(w)
	sh.tip.delay = time.Millisecond
	test.MoveMouse(w.Canvas(), middleOf(sh.status.dot))
	q.until(t, "the tooltip", func() bool { return sh.tip.visibleText() != "" })
	tip := sh.tip.visibleText()
	for _, want := range []string{"Reconnecting…", "since ", "next attempt in 8 s", "No answer from the API server", "Click for its connection updates"} {
		if !strings.Contains(tip, want) {
			t.Errorf("tooltip %q lacks %q", tip, want)
		}
	}
	if barSaysState(sh) || sh.status.context.text != "demo" {
		t.Errorf("the bars say %q and %q", barTexts(sh), sh.status.context.text)
	}
}

// A click on a dot opens its cluster's connection log, and only that: not the navigator row it
// sits in, not its hotbar tile.
func TestDotClickOpensTheConnectionLog(t *testing.T) {
	sh, w, q, f, _ := hotbarScene(t, ThemeDark)
	settle(w)
	var tile *hotbarTile
	for _, ti := range tilesOf(sh.hot) {
		if ti.tile.Context == "prod-eu-central" {
			tile = ti
		}
	}
	if tile == nil {
		t.Fatal("no tile")
	}
	before := f.mark()
	test.TapCanvas(w.Canvas(), middleOf(test.WidgetRenderer(tile).(*hotbarTileRenderer).dot))
	q.until(t, "the log", func() bool { return len(f.callsOf("cluster.connectLog")) == 1 })
	// What the click on prod's dot sent: its log, and nothing that connects, opens or shows the
	// cluster.
	var clicked []engine.Invocation
	for _, c := range f.callsSince(before) {
		if strings.HasPrefix(c.ID, "cluster.") || strings.HasPrefix(c.ID, "hotbar.") || strings.Contains(string(c.Params), "prod-eu-central") {
			clicked = append(clicked, c)
		}
	}
	if len(clicked) != 1 || clicked[0].ID != "cluster.connectLog" || !strings.Contains(string(clicked[0].Params), "prod-eu-central") {
		t.Errorf("the tile's dot dispatched %v", clicked)
	}

	// The shown cluster's connection log is reached from the status bar's dot (the top bar has none,
	// D-54): the click lands on the dot, not on the bar under it.
	if got := clickTarget(w.Canvas().Content(), centre(sh.status.dot)); got != sh.status.dot {
		t.Errorf("a click on the status bar's dot reaches %T", got)
	}
	test.TapCanvas(w.Canvas(), middleOf(sh.status.dot))
	q.until(t, "the status bar's log", func() bool { return len(f.callsOf("cluster.connectLog")) == 2 })
	if p := string(f.callsOf("cluster.connectLog")[1].Params); !strings.Contains(p, "kind-cogpit") {
		t.Errorf("the status bar's dot asked for %s", p)
	}
}

// In the navigator's section a click on a cluster's dot opens its log and neither drills into the
// cluster nor connects it.
func TestSectionDotOpensTheLogNotTheRow(t *testing.T) {
	sh, w, q, f := sectionScene(t, ThemeDark)
	settle(w)
	row := sh.side.sec.rows[secProd]
	if row == nil || !row.Visible() {
		t.Fatal("no row")
	}
	before := f.mark()
	test.TapCanvas(w.Canvas(), middleOf(row.dot))
	q.until(t, "the log", func() bool { return len(f.callsOf("cluster.connectLog")) == 1 })
	calls := f.callsSince(before)
	// What the click on the prod cluster's dot sent: its log, and nothing that drills into it or
	// connects it.
	var clicked []engine.Invocation
	for _, c := range calls {
		if strings.HasPrefix(c.ID, "cluster.") || strings.Contains(string(c.Params), secProd) {
			clicked = append(clicked, c)
		}
	}
	if len(clicked) != 1 || clicked[0].ID != "cluster.connectLog" {
		t.Errorf("the dot dispatched %v", calls)
	}
}
