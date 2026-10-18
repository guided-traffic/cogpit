package fyneui

import (
	"image/color"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The metrics' source row under the drawer's and the Overview's charts (G12; the owner's WF24 and
// WF25 of 2026-10-18, D-88). One compact row: Prometheus at the left, the Kubernetes Metrics Server
// at the right, each as "<name> ● <when>": the dot in the cluster dots' colours (conndot.go: green
// while it answers, red while it fails or may not be read, grey while it is not found, a grey ring
// while nothing has asked it yet) and when it last answered ("30 s ago", "now"), else in a word why
// not ("not answering", "no permission", "not found", "waiting"). Where it runs, what it serves
// now, the last good answer and the last failure (a class, never a response body) are in each
// one's tooltip. While one is not reachable its part of the row links to its section of Cluster
// Settings (D-87): "Set its address…" for a Prometheus not found, "Settings…" otherwise; while both
// answer the row holds nothing more. Before 2026-10-18 there were a source line in prose with the
// Prometheus's namespace and service as links, a line per provider and a "Prometheus…" button;
// where the Prometheus runs is on its settings section now. A provider the cluster's settings
// switch off is not on the row. A cluster whose integrations are not chosen yet (D-82) has none on:
// its row is "Choose integrations…", which asks the question again (cluster.integrations.ask).
// The data is the engine's (ObjectMetrics.Providers, ClusterOverviewModel.Providers), which agents
// read in metrics.get and ui.state. Lens shows one source line; the row is Cogpit's.

const (
	// chooseIntegrationsLink is the row of a cluster whose integrations are not chosen yet.
	chooseIntegrationsLink = "Choose integrations…"
	// setAddressLink and settingsLink lead to a provider's section of Cluster Settings.
	setAddressLink = "Set its address…"
	settingsLink   = "Settings…"
)

// providerStatusLine is the source row of a drawer's or the Overview's charts.
type providerStatusLine struct {
	sh      *shell
	context string // the cluster the charts are of; "" for the shown one
	root    *fyne.Container
	chips   []*sourceChip
	choose  *linkText // "Choose integrations…" while the cluster's integrations are not chosen (D-82)
	layout  *sourceRowLayout
}

func newProviderStatusLine(sh *shell) *providerStatusLine {
	l := &providerStatusLine{sh: sh, layout: &sourceRowLayout{}}
	l.choose = newLinkText(chooseIntegrationsLink, func() {
		sh.dispatch("cluster.integrations.ask", clusterSettingsContext{Context: l.context}, nil)
	})
	l.choose.size = theme.SizeNameCaptionText
	l.choose.Hide()
	l.root = box(l.layout, l.choose)
	return l
}

// setAvail gives the row the width it may take before it stacks its two parts (the drawer's);
// without it the row has the room it is given.
func (l *providerStatusLine) setAvail(avail func() float32) { l.layout.avail = avail }

// set shows the providers' states as of now; nothing to show hides the row.
func (l *providerStatusLine) set(providers []metrics.ProviderStatus, now time.Time) {
	shown := make([]metrics.ProviderStatus, 0, len(providers))
	for _, ps := range providers {
		if ps.Label != "" {
			shown = append(shown, ps)
		}
	}
	slices.SortStableFunc(shown, func(a, b metrics.ProviderStatus) int { return providerRank(a.Provider) - providerRank(b.Provider) })
	for len(l.chips) < len(shown) {
		c := newSourceChip(l.sh)
		l.chips = append(l.chips, c)
		l.root.Add(c)
	}
	tf := l.sh.timeFormat()
	for i, c := range l.chips {
		if i >= len(shown) {
			c.Hide()
			continue
		}
		c.set(shown[i], now, tf, l.openSettings)
		c.Show()
	}
	ctx := l.context
	if ctx == "" {
		ctx = l.sh.ui.Context
	}
	if len(shown) == 0 && noIntegrationsChosen(l.sh.ui, ctx) {
		l.choose.Show()
	} else {
		l.choose.Hide()
	}
	if len(shown) == 0 && !l.choose.Visible() {
		l.root.Hide()
	} else {
		l.root.Show()
	}
	l.root.Refresh()
}

// openSettings opens the section of Cluster Settings of a provider's integration (D-87): its
// provider id is the integration's name.
func (l *providerStatusLine) openSettings(provider string) {
	l.sh.dispatch("cluster.settings.open", clusterSettingsOpenParams{Context: l.context,
		Section: engine.IntegrationSection(engine.Integration(provider))}, nil)
}

// noIntegrationsChosen reports whether the window asks before it looks (D-82) and the user chose
// no integrations for a context yet: nothing of them is read until then. The demo cluster is never
// asked.
func noIntegrationsChosen(u engine.UIState, ctx string) bool {
	return u.IntegrationsAskFirst && ctx != "" && !u.IntegrationsDecided(ctx) && !engine.IsDemoContext(ctx) &&
		(u.Context != ctx || !u.Connection.Demo)
}

// noMetricsCaption is the caption of charts without a metrics provider: why there are none. A
// cluster whose integrations are not chosen yet says so (D-82: nothing is read until then; its row
// offers the question) rather than that both are switched off.
func noMetricsCaption(u engine.UIState, ctx, message string) string {
	if ctx == "" {
		ctx = u.Context
	}
	if noIntegrationsChosen(u, ctx) {
		return "No metrics: no integrations are chosen for this cluster yet."
	}
	return "No metrics: " + message
}

// providerRank orders the row: Prometheus at the left, the Metrics Server at the right (the
// owner's), any other provider after them.
func providerRank(id string) int {
	switch id {
	case metrics.PrometheusID:
		return 0
	case metrics.ServerID:
		return 1
	}
	return 2
}

// dotLook is how a status dot shows: its colour token, and a ring for a state not known yet.
type dotLook struct {
	color fyne.ThemeColorName
	ring  bool
}

// The dots' looks, in the cluster dots' colours (conndot.go).
var (
	dotAnswering = dotLook{color: colorContainerRunning}
	dotFailing   = dotLook{color: colorStatusError}
	dotAbsent    = dotLook{color: colorStatusUnknown}
	dotWaiting   = dotLook{color: colorStatusUnknown, ring: true}
)

// providerDot is a metrics provider's dot: answering, failing (not answering, no permission), not
// found, or not asked yet.
func providerDot(s metrics.Status) dotLook {
	switch s {
	case metrics.StatusAvailable:
		return dotAnswering
	case metrics.StatusUnavailable, metrics.StatusForbidden:
		return dotFailing
	case metrics.StatusNotServed:
		return dotAbsent
	}
	return dotWaiting
}

// integrationDot is an integration's dot on Cluster Settings (D-87), in the same colours: found or
// configured; not answering or no permission; not in the cluster, needing its address, or off; not
// known yet (checking, not connected).
func integrationDot(s engine.IntegrationState) dotLook {
	switch s {
	case engine.IntegrationFound, engine.IntegrationConfigured:
		return dotAnswering
	case engine.IntegrationUnavailable, engine.IntegrationForbidden:
		return dotFailing
	case engine.IntegrationMissing, engine.IntegrationManual, engine.IntegrationOff:
		return dotAbsent
	}
	return dotWaiting
}

// statusDot is a dot in a dotLook, the size of the cluster dots, with nothing to click: a state on
// Cluster Settings (the Metrics section's sources, D-87).
type statusDot struct {
	widget.BaseWidget
	look dotLook
}

func newStatusDot() *statusDot {
	d := &statusDot{look: dotWaiting}
	d.ExtendBaseWidget(d)
	return d
}

// setLook shows a state.
func (d *statusDot) setLook(l dotLook) {
	if l != d.look {
		d.look = l
		d.Refresh()
	}
}

// CreateRenderer implements fyne.Widget.
func (d *statusDot) CreateRenderer() fyne.WidgetRenderer {
	r := &statusDotRenderer{d: d, c: canvas.NewCircle(color.Transparent)}
	r.Refresh()
	return r
}

type statusDotRenderer struct {
	d *statusDot
	c *canvas.Circle
}

func (r *statusDotRenderer) MinSize() fyne.Size {
	return fyne.NewSquareSize(r.d.Theme().Size(sizeDotDiameter))
}

func (r *statusDotRenderer) Layout(size fyne.Size) {
	s := min(r.d.Theme().Size(sizeDotDiameter), size.Width, size.Height)
	r.c.Move(fyne.NewPos((size.Width-s)/2, (size.Height-s)/2))
	r.c.Resize(fyne.NewSquareSize(s))
}

func (r *statusDotRenderer) Refresh() {
	paintDot(r.c, themeColor(r.d.Theme(), r.d.look.color), r.d.look.ring)
	r.Layout(r.d.Size())
	r.c.Refresh()
}

func (r *statusDotRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.c} }
func (r *statusDotRenderer) Destroy()                     {}

// paintDot fills a dot in a colour, or draws it as a ring of that colour (a state not known yet).
func paintDot(c *canvas.Circle, col color.Color, ring bool) {
	if ring {
		c.FillColor, c.StrokeColor, c.StrokeWidth = color.Transparent, col, ringStroke
	} else {
		c.FillColor, c.StrokeColor, c.StrokeWidth = col, color.Transparent, 0
	}
}

// shortProviderName is a provider's name on the row: "Metrics Server" for the Kubernetes Metrics
// Server (the owner's), the others' as they are ("Prometheus", "Thanos", "VictoriaMetrics").
func shortProviderName(label string) string {
	if label == metrics.ServerLabel {
		return "Metrics Server"
	}
	return label
}

// providerWhen is the word after a provider's dot: when it last answered, else why not.
func providerWhen(ps metrics.ProviderStatus, now time.Time) string {
	switch ps.Status {
	case metrics.StatusAvailable:
		if ps.LastOK.IsZero() {
			return "answering"
		}
		return agoText(now.Sub(ps.LastOK))
	case metrics.StatusUnavailable:
		return "not answering"
	case metrics.StatusForbidden:
		return "no permission"
	case metrics.StatusNotServed:
		return "not found"
	}
	return "waiting"
}

// providerLink is the link a provider not reachable offers to its settings, "" while it answers or
// is not asked yet (WF25): a Prometheus not found by name needs its address.
func providerLink(ps metrics.ProviderStatus) string {
	switch ps.Status {
	case metrics.StatusNotServed:
		if ps.Provider == metrics.PrometheusID {
			return setAddressLink
		}
		return settingsLink
	case metrics.StatusUnavailable, metrics.StatusForbidden:
		return settingsLink
	}
	return ""
}

// servesText names what a provider serves: "history, live values, network, filesystem".
func servesText(serves []string) string {
	words := make([]string, 0, len(serves))
	for _, s := range serves {
		if s == metrics.ServesLive {
			s = "live values"
		}
		words = append(words, s)
	}
	return strings.Join(words, ", ")
}

// providerTip is a provider's tooltip on the row: where it runs, its state, what it serves, the last
// good answer and the last failure; the times of day in tf, the user's zone and format (D-31).
func providerTip(ps metrics.ProviderStatus, now time.Time, tf model.TimeFormat) string {
	name := ps.Label
	if ps.Service != "" {
		name += " " + ps.Service
	}
	since := func(t time.Time) string { return agoText(now.Sub(t)) }
	var state string
	switch ps.Status {
	case metrics.StatusAvailable:
		state = "Found and answering."
	case metrics.StatusUnavailable:
		state = "Found, but it does not answer: its charts keep what they have."
	case metrics.StatusForbidden:
		state = "The current user may not read it."
	case metrics.StatusNotServed:
		state = "Not found in this cluster."
	default:
		state = "Not looked for yet: Cogpit looks for it when a chart needs it."
	}
	tip := []string{name, state}
	if what := servesText(ps.Serves); what != "" {
		tip = append(tip, "Serves: "+what+".")
	}
	if !ps.LastOK.IsZero() {
		tip = append(tip, "Last good answer: "+tf.FormatTime(ps.LastOK)+" ("+since(ps.LastOK)+")")
	}
	if ps.LastError != "" {
		line := "Last error: " + ps.LastError
		if !ps.LastErrorAt.IsZero() {
			line += " at " + tf.FormatTime(ps.LastErrorAt) + " (" + since(ps.LastErrorAt) + ")"
		}
		tip = append(tip, line)
	}
	return strings.Join(tip, "\n")
}

// sourceRowLayout lays the row's parts out in the row's middle (the owner, 2026-10-18, on WF24:
// "nicht ganz außen rechts und links, sondern mittig zentriert, rechts und links"): one centred;
// two side by side, the first ending a gap's half left of the centre and the second starting a
// gap's half right of it, or the pair centred as one when the wider of them does not fit its half;
// one under the other, each centred, when they do not fit side by side in the room the row has
// (avail, the drawer's width; else the size it is given).
type sourceRowLayout struct {
	avail func() float32
}

func (l *sourceRowLayout) gap() float32 {
	return 2 * fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameInnerPadding)
}

// visible are the shown parts and their natural sizes.
func visibleParts(objs []fyne.CanvasObject) ([]fyne.CanvasObject, []fyne.Size) {
	var out []fyne.CanvasObject
	var sizes []fyne.Size
	for _, o := range objs {
		if o.Visible() {
			out = append(out, o)
			sizes = append(sizes, o.MinSize())
		}
	}
	return out, sizes
}

// stacked reports whether the parts go one under the other in a width.
func (l *sourceRowLayout) stacked(sizes []fyne.Size, width float32) bool {
	if len(sizes) < 2 {
		return false
	}
	sum := -l.gap()
	for _, s := range sizes {
		sum += s.Width + l.gap()
	}
	return sum > width
}

// MinSize implements fyne.Layout: the widest part (the row never asks for more than its room), one
// line high, or the parts' lines when they do not fit side by side in the room the row has.
func (l *sourceRowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	_, sizes := visibleParts(objs)
	var w, h, sum float32
	for _, s := range sizes {
		w, h = max(w, s.Width), max(h, s.Height)
		sum += s.Height
	}
	if l.avail == nil {
		if len(sizes) > 1 {
			w = 0
			for _, s := range sizes {
				w += s.Width
			}
			w += l.gap() * float32(len(sizes)-1)
		}
		return fyne.NewSize(w, h)
	}
	if l.stacked(sizes, l.avail()) {
		h = sum + fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameLineSpacing)*float32(len(sizes)-1)
	}
	return fyne.NewSize(w, h)
}

// Layout implements fyne.Layout.
func (l *sourceRowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	parts, sizes := visibleParts(objs)
	if l.stacked(sizes, size.Width) {
		y := float32(0)
		for i, o := range parts {
			w := min(sizes[i].Width, size.Width)
			o.Move(fyne.NewPos((size.Width-w)/2, y))
			o.Resize(fyne.NewSize(w, sizes[i].Height))
			y += sizes[i].Height + fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameLineSpacing)
		}
		return
	}
	for i, o := range parts {
		o.Move(fyne.NewPos(l.partX(sizes, i, size.Width), (size.Height-sizes[i].Height)/2))
		o.Resize(sizes[i])
	}
}

// partX is where part i of parts side by side starts: around the centre (Layout).
func (l *sourceRowLayout) partX(sizes []fyne.Size, i int, width float32) float32 {
	gap, mid := l.gap(), width/2
	if len(sizes) == 2 && 2*max(sizes[0].Width, sizes[1].Width)+gap <= width {
		if i == 0 {
			return mid - gap/2 - sizes[0].Width
		}
		return mid + gap/2
	}
	total := -gap // the parts centred as one
	for _, s := range sizes {
		total += s.Width + gap
	}
	x := (width - total) / 2
	for _, s := range sizes[:i] {
		x += s.Width + gap
	}
	return x
}

// sourceChip is one provider on the source row: its name, its dot, when it last answered or why
// not, the details in the tooltip; while it is not reachable, a link to its settings after it.
type sourceChip struct {
	widget.BaseWidget
	sh        *shell
	provider  string // the provider's id, the name of its integration
	name      string
	when      string
	look      dotLook
	tip       string
	link      string // "" for none
	onLink    func(provider string)
	hoverLink bool
}

var (
	_ fyne.Tappable      = (*sourceChip)(nil)
	_ desktop.Hoverable  = (*sourceChip)(nil)
	_ desktop.Cursorable = (*sourceChip)(nil)
)

func newSourceChip(sh *shell) *sourceChip {
	c := &sourceChip{sh: sh}
	c.ExtendBaseWidget(c)
	return c
}

// set shows a provider's state as of now; the same state costs nothing (a poll sets it every 15 s).
func (c *sourceChip) set(ps metrics.ProviderStatus, now time.Time, tf model.TimeFormat, onLink func(string)) {
	name, when, look, tip, link := shortProviderName(ps.Label), providerWhen(ps, now), providerDot(ps.Status), providerTip(ps, now, tf), providerLink(ps)
	c.onLink = onLink
	if c.provider == ps.Provider && c.name == name && c.when == when && c.look == look && c.tip == tip && c.link == link {
		return
	}
	c.provider, c.name, c.when, c.look, c.tip, c.link = ps.Provider, name, when, look, tip, link
	c.Refresh()
}

// text is the chip as one line of text, for tests: "Prometheus ● 30 s ago · Settings…".
func (c *sourceChip) text() string {
	s := c.name + " ● " + c.when
	if c.link != "" {
		s += " · " + c.link
	}
	return s
}

func (c *sourceChip) textSize() float32 { return c.Theme().Size(theme.SizeNameCaptionText) }

// gap is the room between the name, the dot, the time and the link.
func (c *sourceChip) gap() float32 { return c.Theme().Size(theme.SizeNameInnerPadding) / 2 }

// linkStart is where the link begins; the chip's width when it has none.
func (c *sourceChip) linkStart() float32 {
	size := c.textSize()
	x := fyne.MeasureText(c.name, size, fyne.TextStyle{}).Width + c.gap() + c.Theme().Size(sizeDotDiameter) + c.gap() +
		fyne.MeasureText(c.when, size, fyne.TextStyle{}).Width
	if c.link != "" {
		x += 3 * c.gap()
	}
	return x
}

func (c *sourceChip) overLink(x float32) bool { return c.link != "" && x >= c.linkStart() }

// Tapped implements fyne.Tappable: the link opens the provider's settings.
func (c *sourceChip) Tapped(ev *fyne.PointEvent) {
	if c.overLink(ev.Position.X) && c.onLink != nil {
		c.sh.tip.hideFor(c)
		c.onLink(c.provider)
	}
}

// Cursor implements desktop.Cursorable: a hand over the link.
func (c *sourceChip) Cursor() desktop.Cursor {
	if c.hoverLink {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

// MouseIn implements desktop.Hoverable: the details as the tooltip.
func (c *sourceChip) MouseIn(ev *desktop.MouseEvent) {
	c.sh.tip.hover(c, c.tip, ev.AbsolutePosition)
	c.MouseMoved(ev)
}

// MouseMoved implements desktop.Hoverable: the link under the pointer is underlined.
func (c *sourceChip) MouseMoved(ev *desktop.MouseEvent) {
	c.sh.tip.moved(c, ev.AbsolutePosition)
	if on := c.overLink(ev.Position.X); on != c.hoverLink {
		c.hoverLink = on
		c.Refresh()
	}
}

// MouseOut implements desktop.Hoverable.
func (c *sourceChip) MouseOut() {
	c.sh.tip.hideFor(c)
	if c.hoverLink {
		c.hoverLink = false
		c.Refresh()
	}
}

// CreateRenderer implements fyne.Widget.
func (c *sourceChip) CreateRenderer() fyne.WidgetRenderer {
	r := &sourceChipRenderer{c: c, name: canvas.NewText("", color.Transparent), when: canvas.NewText("", color.Transparent),
		link: canvas.NewText("", color.Transparent), dot: canvas.NewCircle(color.Transparent), line: canvas.NewRectangle(color.Transparent)}
	r.Refresh()
	return r
}

type sourceChipRenderer struct {
	c                *sourceChip
	name, when, link *canvas.Text
	dot              *canvas.Circle
	line             *canvas.Rectangle // the hovered link's underline
}

func (r *sourceChipRenderer) MinSize() fyne.Size {
	c := r.c
	w := c.linkStart()
	if c.link != "" {
		w += fyne.MeasureText(c.link, c.textSize(), fyne.TextStyle{}).Width
	}
	h := fyne.MeasureText("Mg", c.textSize(), fyne.TextStyle{}).Height
	return fyne.NewSize(w, h)
}

func (r *sourceChipRenderer) Layout(size fyne.Size) {
	c := r.c
	ts := c.textSize()
	th := fyne.MeasureText("Mg", ts, fyne.TextStyle{}).Height
	y := (size.Height - th) / 2
	x := float32(0)
	place := func(t *canvas.Text) {
		ms := fyne.MeasureText(t.Text, ts, fyne.TextStyle{})
		t.Move(fyne.NewPos(x, y))
		t.Resize(fyne.NewSize(ms.Width, th))
		x += ms.Width
	}
	place(r.name)
	x += c.gap()
	d := c.Theme().Size(sizeDotDiameter)
	r.dot.Move(fyne.NewPos(x, (size.Height-d)/2))
	r.dot.Resize(fyne.NewSquareSize(d))
	x += d + c.gap()
	place(r.when)
	r.link.Hide()
	r.line.Hide()
	if c.link != "" {
		x = c.linkStart()
		start := x
		place(r.link)
		r.link.Show()
		if c.hoverLink {
			r.line.Move(fyne.NewPos(start, y+th-2))
			r.line.Resize(fyne.NewSize(x-start, 1))
			r.line.Show()
		}
	}
}

func (r *sourceChipRenderer) Refresh() {
	c := r.c
	th := c.Theme()
	ts := c.textSize()
	muted := themeColor(th, colorMuted)
	r.name.Text, r.name.TextSize, r.name.Color = c.name, ts, muted
	r.when.Text, r.when.TextSize, r.when.Color = c.when, ts, muted
	r.link.Text, r.link.TextSize, r.link.Color = c.link, ts, themeColor(th, colorLink)
	r.line.FillColor = themeColor(th, colorLink)
	paintDot(r.dot, themeColor(th, c.look.color), c.look.ring)
	r.Layout(c.Size())
	for _, o := range r.Objects() {
		o.Refresh()
	}
}

func (r *sourceChipRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.name, r.dot, r.when, r.link, r.line}
}

func (r *sourceChipRenderer) Destroy() {}
