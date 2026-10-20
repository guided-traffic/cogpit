package fyneui

import (
	"image/color"
	"strings"
	"time"
	"weak"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// A cluster's connection shows as a dot, never a word (F13, the owner, 2026-10-16, amending D-58's
// S3): Connected in the Running containers' green, Connection failed and Login expired red, Not
// connected grey; while the cluster is (re)connecting its dots blink in that green, 500 ms full and
// 500 ms dimmed, for at least three intervals even when the reconnect succeeds at once, so that the
// user sees it (the minimum is the window's: agents read the true state in ui_state). Every dot of
// that cluster blinks in step: the top bar's, the status bar's, its hotbar tile's, its navigator
// rows' and its welcome-page row's. The dot's tooltip names the state, and a click on it opens the
// cluster's connection log (connectlog.go), never also its row or tile.
//
// With several clusters connected (D-69 item 5, stage 2, 2026-10-16) the dot's shape says live or
// resting: a connected cluster the window does not show is filled while its views are warm, a ring
// of the same size once they are released (cold, green) or once its login lapsed without a renewal
// (grey); clustermarks.go derives each cluster's mark. Each cluster's dots blink for its own
// (re)connect, a background cluster's too (D-69 item 7), and a blinking dot is drawn filled.

const (
	blinkPhase   = 500 * time.Millisecond // each phase, full and dimmed (the owner)
	blinkMinimum = 3 * time.Second        // three intervals at least (the owner)
	blinkDim     = 0.3                    // the dimmed phase's alpha: the dot stays (the lead's)
	blinkStill   = 0.5                    // the steady dot's alpha under Reduce motion (the lead's)
	ringStroke   = 1.5                    // a ring's line, in points: 1.5 px at 1× (D-69, the lead's)
)

// blinkingState reports whether a state is a (re)connect: Connecting or Interrupted.
func blinkingState(s engine.ConnectionState) bool {
	return s == engine.Connecting || s == engine.Interrupted
}

// connectionColor maps a connection state to a colour token: Connected and a (re)connect in the
// Running containers' green (the owner, F13), a failed connect and an expired login red, Not
// connected grey.
func connectionColor(c engine.Connection) fyne.ThemeColorName {
	switch c.State {
	case engine.Connected, engine.Connecting, engine.Interrupted:
		return colorContainerRunning
	case engine.ConnectError, engine.LoginExpired:
		return colorStatusError
	}
	return colorStatusUnknown
}

// blinker is the one clock of the blinking dots, on the main goroutine. It follows every
// cluster's connection the window knows (observeAll: the shown one's and each connected one's,
// D-69 item 7): a (re)connect of a cluster starts a blink of its dots that lasts while the engine
// says it (re)connects and at least blinkMinimum; Login expired, Connection failed or a disconnect
// end it at once. Each cluster blinks for itself, a background cluster's reconnect too; a cluster
// shown or left does not end another's blink. A ticker runs only while a blink lasts and refreshes
// the blinking clusters' dots at each phase; without a blink nothing runs.
type blinker struct {
	sh     *shell
	now    func() time.Time
	reduce func() bool // Reduce motion (shell.reduceMotion), read when a blink starts
	ticker func(on bool)

	blinks map[string]*blink // the clusters that blink, by context

	dots []weak.Pointer[connDot] // every dot made, to refresh the blinking clusters'
	stop chan struct{}           // the running ticker's; nil while none runs
}

// blink is one cluster's blink.
type blink struct {
	live  bool      // the engine still says it (re)connects
	start time.Time // the blink's start: its phases count from here, so its dots blink in step
	until time.Time // it blinks at least until then
	still bool      // Reduce motion: a steady half-alpha dot instead
}

// on reports whether the blink lasts at now.
func (k *blink) on(now time.Time) bool { return k != nil && (k.live || now.Before(k.until)) }

func newBlinker(sh *shell) *blinker {
	b := &blinker{sh: sh, now: time.Now, reduce: sh.reduceMotion, blinks: map[string]*blink{}}
	b.ticker = b.runTicker
	return b
}

// observeAll follows every cluster the window knows a state of (clusterStates): a cluster no
// longer among them (disconnected) ends its blink.
func (b *blinker) observeAll(states map[string]engine.ConnectionState) {
	for ctx := range b.blinks {
		if _, ok := states[ctx]; !ok {
			delete(b.blinks, ctx)
		}
	}
	for ctx, s := range states {
		b.follow(ctx, s)
	}
	b.tick()
}

// follow starts, keeps or ends a cluster's blink for its state.
func (b *blinker) follow(ctx string, s engine.ConnectionState) {
	if ctx == "" {
		return
	}
	now := b.now()
	k := b.blinks[ctx]
	switch {
	case blinkingState(s):
		if !k.on(now) {
			k = &blink{start: now, until: now.Add(blinkMinimum), still: b.reduce()}
			b.blinks[ctx] = k
		}
		k.live = true
	case k != nil && s == engine.Connected:
		k.live = false // the minimum may still run
	default:
		delete(b.blinks, ctx)
	}
}

// blinking reports whether a cluster's dots blink now.
func (b *blinker) blinking(ctx string) bool { return ctx != "" && b.blinks[ctx].on(b.now()) }

// any reports whether some cluster's dots blink now.
func (b *blinker) any() bool {
	now := b.now()
	for _, k := range b.blinks {
		if k.on(now) {
			return true
		}
	}
	return false
}

// look is how a dot of ctx in state s shows now: its colour token and alpha, and whether it blinks
// (a blinking dot is drawn filled, never as a ring).
func (b *blinker) look(ctx string, s engine.ConnectionState, base fyne.ThemeColorName) (fyne.ThemeColorName, float32, bool) {
	now := b.now()
	k := b.blinks[ctx]
	if ctx == "" || !k.on(now) || (!blinkingState(s) && s != engine.Connected) {
		return base, 1, false
	}
	switch {
	case k.still:
		return colorContainerRunning, blinkStill, true
	case (now.Sub(k.start)/blinkPhase)%2 == 1:
		return colorContainerRunning, blinkDim, true
	}
	return colorContainerRunning, 1, true
}

// tick refreshes the blinking clusters' dots and runs the ticker while a blink lasts; the last
// tick of a blink refreshes them once more, steady.
func (b *blinker) tick() {
	now := b.now()
	for ctx, k := range b.blinks {
		if !k.on(now) {
			delete(b.blinks, ctx)
		}
	}
	live := b.dots[:0]
	for _, w := range b.dots {
		d := w.Value()
		if d == nil {
			continue
		}
		live = append(live, w)
		if _, on := b.blinks[d.context]; d.context != "" && (on || d.blinked) {
			d.Refresh()
		}
	}
	clear(b.dots[len(live):])
	b.dots = live
	b.ticker(len(b.blinks) > 0)
}

// add keeps a dot to refresh while its cluster blinks.
func (b *blinker) add(d *connDot) { b.dots = append(b.dots, weak.Make(d)) }

// runTicker starts the ticker when on and none runs, and stops it otherwise: a goroutine that
// posts tick to the main goroutine every phase.
func (b *blinker) runTicker(on bool) {
	switch {
	case on && b.stop == nil:
		stop := make(chan struct{})
		b.stop = stop
		go func() {
			t := time.NewTicker(blinkPhase)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					b.sh.do(b.tick)
				case <-stop:
					return
				case <-b.sh.stopTickers:
					return
				}
			}
		}()
	case !on && b.stop != nil:
		close(b.stop)
		b.stop = nil
	}
}

// connDot is a cluster's connection dot: its colour from the state (connectionColor) or the
// blinker, its state in the tooltip, and a click opening the cluster's connection log.
type connDot struct {
	widget.BaseWidget
	sh       *shell
	context  string // the cluster; "" for none, which has no log
	conn     engine.Connection
	color    fyne.ThemeColorName // the colour when it does not blink
	ring     bool                // drawn as a ring, not filled, when it does not blink (D-69: resting)
	state    string              // the tooltip's first line in place of connectionText's, "" for that
	blinked  bool                // drawn blinking last time: refreshed once more when the blink ends
	tipExtra string              // a line the site adds to the tooltip
}

var (
	_ fyne.Tappable      = (*connDot)(nil)
	_ desktop.Hoverable  = (*connDot)(nil)
	_ desktop.Cursorable = (*connDot)(nil)
)

func newConnDot(sh *shell) *connDot {
	d := &connDot{sh: sh, color: colorStatusUnknown}
	d.ExtendBaseWidget(d)
	if sh != nil && sh.blink != nil {
		sh.blink.add(d)
	}
	return d
}

// setMark shows a cluster's mark (clustermarks.go): its connection, colour, shape and the state its
// tooltip names.
func (d *connDot) setMark(ctx string, m clusterMark) {
	if m.color == "" {
		m.color = connectionColor(m.conn)
	}
	if ctx == d.context && m.conn == d.conn && m.color == d.color && m.ring == d.ring && m.state == d.state {
		return
	}
	d.context, d.conn, d.color, d.ring, d.state = ctx, m.conn, m.color, m.ring, m.state
	d.Refresh()
}

// look is the dot's colour token and alpha now, and whether it is drawn as a ring.
func (d *connDot) look() (fyne.ThemeColorName, float32, bool) {
	if d.sh == nil || d.sh.blink == nil {
		return d.color, 1, d.ring
	}
	name, a, blinking := d.sh.blink.look(d.context, d.conn.State, d.color)
	return name, a, d.ring && !blinking
}

// tipText names the state: why and since when where the engine says so, the next attempt, and
// what a click does.
func (d *connDot) tipText() string {
	c := d.conn
	parts := []string{connectionText(c)}
	if d.state != "" {
		parts[0] = d.state
	}
	tf := d.sh.timeFormat()
	if !c.Since.IsZero() && (c.State == engine.Interrupted || c.State == engine.LoginExpired) {
		parts = append(parts, "since "+tf.FormatClock(c.Since))
	}
	if c.State == engine.Interrupted {
		next := "trying now"
		if !c.NextAttempt.IsZero() {
			next = "next attempt " + attemptIn(c.NextAttempt.Sub(d.sh.now()))
		}
		parts = append(parts, next)
	}
	text := strings.Join(parts, " · ")
	switch {
	case strings.TrimSpace(c.Reason) != "" && (c.State == engine.Interrupted || c.State == engine.LoginExpired):
		text += "\n" + upperFirst(strings.TrimSpace(c.Reason))
	case c.Error != "" && (c.State == engine.ConnectError || c.State == engine.Disconnected):
		text += "\n" + kubeconfig.DisplayText(c.Error, maxShownPath)
	}
	if line := loginWaitLine(c.WaitingForLogin, tf); line != "" { // its credential plugin runs (D-73)
		text += "\n" + line
		if c.State == engine.Connecting {
			text += " " + loginWaitCancel(c.WaitingForLogin, " in its menu")
		}
	}
	if d.tipExtra != "" {
		text += "\n" + d.tipExtra
	}
	if d.context != "" {
		text += "\nClick for its connection updates"
	}
	return text
}

// Tapped implements fyne.Tappable: the cluster's connection log; the row or tile under the dot
// gets no tap.
func (d *connDot) Tapped(*fyne.PointEvent) {
	if d.sh == nil {
		return
	}
	d.sh.tip.hideFor(d)
	if d.context != "" {
		d.sh.kc.showConnectLog(d.context)
	}
}

// Cursor implements desktop.Cursorable: a pointer over a dot that opens a log.
func (d *connDot) Cursor() desktop.Cursor {
	if d.context != "" {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

// MouseIn implements desktop.Hoverable.
func (d *connDot) MouseIn(ev *desktop.MouseEvent) {
	if d.sh != nil {
		d.sh.tip.hover(d, d.tipText(), ev.AbsolutePosition)
	}
}

// MouseMoved implements desktop.Hoverable.
func (d *connDot) MouseMoved(ev *desktop.MouseEvent) {
	if d.sh != nil {
		d.sh.tip.moved(d, ev.AbsolutePosition)
	}
}

// MouseOut implements desktop.Hoverable.
func (d *connDot) MouseOut() {
	if d.sh != nil {
		d.sh.tip.hideFor(d)
	}
}

// CreateRenderer implements fyne.Widget.
func (d *connDot) CreateRenderer() fyne.WidgetRenderer {
	r := &connDotRenderer{d: d, c: canvas.NewCircle(color.Transparent), halo: canvas.NewCircle(color.Transparent)}
	r.Refresh()
	return r
}

type connDotRenderer struct {
	d    *connDot
	c    *canvas.Circle
	halo *canvas.Circle // a soft ring of the dot's colour around a filled dot: the cockpit's lamp
}

// haloReach is how far the halo reaches past the dot, in points; haloAlpha its strength.
const (
	haloReach = 2.5
	haloAlpha = 0.22
)

func (r *connDotRenderer) diameter() float32 { return r.d.Theme().Size(sizeDotDiameter) }

func (r *connDotRenderer) Layout(size fyne.Size) {
	d := min(r.diameter(), size.Width, size.Height)
	r.c.Move(fyne.NewPos((size.Width-d)/2, (size.Height-d)/2))
	r.c.Resize(fyne.NewSize(d, d))
	// A stroke is centred on the circle's edge: half a reach out puts the ring right around the dot.
	r.halo.Move(fyne.NewPos((size.Width-d)/2-haloReach/2, (size.Height-d)/2-haloReach/2))
	r.halo.Resize(fyne.NewSize(d+haloReach, d+haloReach))
}

func (r *connDotRenderer) MinSize() fyne.Size {
	d := r.diameter()
	return fyne.NewSize(d, d)
}

func (r *connDotRenderer) Refresh() {
	name, a, ring := r.d.look()
	r.d.blinked = name != r.d.color || a != 1 || ring != r.d.ring
	c := themeColor(r.d.Theme(), name)
	if a < 1 {
		c = withAlpha(c, scaleAlpha(color.NRGBAModel.Convert(c).(color.NRGBA).A, a))
	}
	if ring { // the same size, an outline: the layout does not move (D-69 item 5)
		r.c.FillColor, r.c.StrokeColor, r.c.StrokeWidth = color.Transparent, c, ringStroke
		r.halo.Hide()
	} else {
		r.c.FillColor, r.c.StrokeColor, r.c.StrokeWidth = c, color.Transparent, 0
		r.halo.FillColor, r.halo.StrokeWidth = color.Transparent, haloReach
		r.halo.StrokeColor = withAlpha(c, scaleAlpha(color.NRGBAModel.Convert(c).(color.NRGBA).A, haloAlpha))
		r.halo.Show()
	}
	r.Layout(r.d.Size())
	r.halo.Refresh()
	r.c.Refresh()
}

func (r *connDotRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.c, r.halo} }
func (r *connDotRenderer) Destroy()                     {}

// scaleAlpha is an alpha of 255 times a: a dimmed or steady dot's.
func scaleAlpha(alpha uint8, a float32) uint8 { return uint8(float32(alpha) * a) }
