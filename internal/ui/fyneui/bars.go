package fyneui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"net/url"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/buildinfo"
	"github.com/guided-traffic/cogpit/internal/engine"
	logoart "github.com/guided-traffic/cogpit/internal/ui/logo"
)

// notConnected is what the window says where no cluster is connected: the top bar and the status
// bar in place of a cluster's name, the navigator's head, the View menu, an empty view and the
// Diagnostics, in the same words and case everywhere (the owner's walk, 2026-10-17, WF2: "No
// Cluster" above and "no cluster" below; "eher not connected"). The engine's reasons for a
// disabled command keep "no cluster connected": a reason in a sentence ("… — no cluster connected"),
// which agents read too (the lead's decision).
const notConnected = "Not connected"

// connectionText describes a connection state in a word or two: the dots' tooltips (conndot.go)
// and the navigator's; the bars show the dot alone (F13).
func connectionText(c engine.Connection) string {
	switch c.State {
	case engine.Connected:
		return "Connected"
	case engine.Connecting:
		return "Connecting…"
	case engine.ConnectError:
		return "Connection failed"
	case engine.Interrupted:
		return "Reconnecting…"
	case engine.LoginExpired:
		return "Login expired"
	}
	return notConnected
}

// topBar is the window's top bar: the Cogpit wordmark and the build's version (F12), the history
// arrows and at the right: the Update button while an update is available, the alerts pill
// (alerts.go), the notifications' bell, a divider and the layout toggles at the right end
// (layoutbar.go, F7; their order the owner's, 2026-10-16 night; the MCP details are the status
// bar's, F6). It names no cluster (the owner, 2026-10-21, D-54): the shown cluster, its connection's
// dot and its kubeconfig are the status bar's and the navigator's. On macOS it is also the window's
// title bar (titlebar.go): room for the window buttons before the logo, and a drag area under its
// contents.
type topBar struct {
	sh          *shell
	lead        *canvas.Rectangle // the window buttons' room; hidden unless Cogpit draws the title bar
	history     *titleHistory     // the history arrows and the bar's context menu (titlehistory.go)
	notes       *notificationsUI  // the bell and its panel (notifications.go)
	left        *fyne.Container
	right       *fyne.Container // the Update button, the alerts, the bell, a divider, the layout toggles
	alerts      *alertsButton   // the alerts pill left of the bell (alerts.go): the shown cluster's Alertmanager (D-77)
	appearance  *themeToggle    // the light and dark toggle directly left of the bell (themetoggle.go, the owner's of 2026-10-21)
	version     *buildLabel     // the build's version right of the wordmark (buildlabel.go, F12)
	edge        *surface        // the line under the bar, directly under it (shell.go, F11)
	titleHeight float32         // the title bar's height the bar covers, in Fyne units; 0 without
	titleLead   float32         // the window buttons' room last made (lead's width); 0 without
	titleSet    bool            // setTitlebar ran: titleLead and titleHeight are what it applied
	root        fyne.CanvasObject
	// layout is the Update button and the layout toggles left of the right end (layoutbar.go, F7).
	layout *layoutBar
	// menus is the window's menus' titles at the bar's left on Windows and Linux (menubar.go); nil
	// on macOS, whose menu bar is the system's.
	menus *menuBar
}

func newTopBar(sh *shell) *topBar {
	t := &topBar{sh: sh}
	wordmark := newTextLine("Cogpit", theme.ColorNameForeground, theme.SizeNameSubHeadingText, fyne.TextStyle{Bold: true})
	t.lead = canvas.NewRectangle(color.Transparent)
	t.lead.Hide()
	t.version = newBuildLabel(sh, buildinfo.Read())
	t.history = newTitleHistory(sh)
	t.notes = newNotificationsUI(sh)
	left := []fyne.CanvasObject{t.lead, newLogo(), wordmark, t.version}
	if sh.inlineMenus { // the menus before the logo, a divider between (menubar.go)
		t.menus = newMenuBar(sh)
		left = append([]fyne.CanvasObject{t.menus.root, newHGap(theme.SizeNameInnerPadding), newDivider(true),
			newHGap(theme.SizeNameInnerPadding)}, left...)
	}
	if t.history.shown() { // the arrows a gap away from the version
		t.version.gap = newHGap(theme.SizeNameInnerPadding)
		left = append(left, t.version.gap)
	}
	t.left = container.NewHBox(append(left, t.history.root)...)
	t.layout = newLayoutBar(sh)
	t.alerts = newAlertsButton(sh)
	// The divider is a short line, a line of text high (the owner: "wie so ein Zeichen |").
	t.alerts.divider = []fyne.CanvasObject{newHGap(theme.SizeNameInnerPadding), box(compactLayout{}, newDivider(true)),
		newHGap(theme.SizeNameInnerPadding)}
	t.appearance = newThemeToggle(sh)
	t.right = container.NewHBox(append(append([]fyne.CanvasObject{t.layout.updateBox, t.alerts, t.appearance, t.notes.bell},
		t.alerts.divider...), t.layout.root)...)
	// The line under the bar lies directly under it (shell.go): what the bar centres then sits
	// midway between the window's top and the line (F11, the owner: "hängt ein wenig zu hoch"; with
	// a box's padding between the two the line lay 4 units lower, and in the dark theme, the
	// window's background and the bar's nearly one colour, everything in the bar looked high).
	t.edge = newDivider(false)
	drag := newBarDragArea(func() { sh.titlebar.mouseDown() })
	drag.secondary = t.history.showMenu
	t.root = box(&topBarLayout{t: t}, newSurface(colorTopBar), drag,
		inset(theme.SizeNameInnerPadding, "", box(barRowLayout{t: t}, t.left, t.right)))
	return t
}

// splitContextID splits a source context's id, "<context> [<file>]" (engine.SourceContextID), into
// the context's name and the file it comes from, so that the navigator's header shows the name and
// the file apart; an id without a file (demo) is a name alone.
func splitContextID(id string) (name, file string) {
	if i := strings.LastIndex(id, " ["); i > 0 && strings.HasSuffix(id, "]") {
		return id[:i], id[i+2 : len(id)-1]
	}
	return id, ""
}

// newLogo draws Cogpit's logo in the header: the current version's tile (internal/ui/logo, D-24),
// the same in both themes (owner, 2026-10-15), drawn at the pixel size the canvas shows it at.
func newLogo() fyne.CanvasObject {
	l := &logo{}
	l.ExtendBaseWidget(l)
	return l
}

type logo struct {
	widget.BaseWidget
	px int // the size the picture was drawn at, in pixels
}

// CreateRenderer implements fyne.Widget.
func (l *logo) CreateRenderer() fyne.WidgetRenderer {
	img := canvas.NewImageFromImage(nil)
	img.FillMode, img.ScaleMode = canvas.ImageFillContain, canvas.ImageScaleSmooth
	r := &logoRenderer{l: l, img: img}
	r.Refresh()
	return r
}

type logoRenderer struct {
	l   *logo
	img *canvas.Image
}

func (r *logoRenderer) MinSize() fyne.Size {
	s := r.l.Theme().Size(sizeLogo)
	return fyne.NewSize(s, s)
}

func (r *logoRenderer) Layout(size fyne.Size) {
	s := r.MinSize().Width
	r.img.Move(fyne.NewPos((size.Width-s)/2, (size.Height-s)/2))
	r.img.Resize(fyne.NewSize(s, s))
}

func (r *logoRenderer) Refresh() {
	l := r.l
	scale := float32(1)
	if c := fyne.CurrentApp().Driver().CanvasForObject(l); c != nil {
		scale = c.Scale()
	}
	px := int(math.Ceil(float64(r.MinSize().Width * scale)))
	if px != l.px || r.img.Image == nil {
		r.img.Image = logoart.Current.Tile(px)
		l.px = px
	}
	r.Layout(l.Size())
	r.img.Refresh()
}

func (r *logoRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.img} }
func (r *logoRenderer) Destroy()                     {}

// diagView is what the status bar shows from engine.Diagnostics, prepared off the main
// goroutine. The count of table snapshots is not among it: a developer's diagnostic that only the
// Diagnostics tab shows (diagnostics.go; owner, 2026-10-14).
type diagView struct {
	total, list, watch int64
	informers          int
	demo               bool
	demoRate           float64
	demoObjects        int64
}

func newDiagView(d engine.Diagnostics) diagView {
	v := diagView{total: d.Requests.Total, informers: len(d.Informers), demo: d.Demo}
	if d.Requests.ByVerb != nil {
		v.list, v.watch = d.Requests.ByVerb["list"], d.Requests.ByVerb["watch"]
	}
	if d.DemoStats != nil {
		// DemoStats crosses the seam as a JSON-shaped value; the UI may not import package demo.
		var s struct {
			Objects      int64   `json:"objects"`
			EventsPerSec float64 `json:"eventsPerSec"`
		}
		if b, err := json.Marshal(d.DemoStats); err == nil && json.Unmarshal(b, &s) == nil {
			v.demo, v.demoRate, v.demoObjects = true, s.EventsPerSec, s.Objects
		}
	}
	return v
}

// statusBar is the bottom line: the connection's dot and the context, API requests, informers, the
// demo cluster's event rate, the link to the owner and the MCP indicator.
type statusBar struct {
	sh      *shell
	link    *linkText // the link to the owner (D-23), left of the MCP item
	mcp     *mcpItem  // the MCP indicator and its switch, at the right end; nil without an MCP server
	layout  *statusLayout
	dot     *connDot // the connection's state, no word beside it (F13)
	context *tipLine // the cluster's short name, its full context id the tooltip (clustername.go, D-68)
	// tls says "TLS not checked" in the warning colour beside the context while the connection does
	// not check the server's certificate (engine.Connection.TLSUnchecked, the owner's A29); hidden
	// otherwise.
	tls       *textLine
	requests  *textLine
	informers *textLine
	demo      *textLine
	demoSep   fyne.CanvasObject
	pulse     *pulse // the event rate's wave beside the demo text (chrome_bar.go)
	pulseBox  fyne.CanvasObject
	root      fyne.CanvasObject
}

func newStatusBar(sh *shell) *statusBar {
	s := &statusBar{sh: sh}
	caption := func() *textLine {
		return newTextLine("", colorMuted, sizeStatusText, fyne.TextStyle{})
	}
	s.dot = newConnDot(sh)
	s.context = newTipLine(sh, colorMuted, sizeStatusText, fyne.TextStyle{})
	s.requests, s.informers, s.demo = caption(), caption(), caption()
	s.tls = newTextLine(engine.TLSUncheckedLabel, colorStatusWarning, sizeStatusText, fyne.TextStyle{})
	s.tls.Hide()
	s.demoSep = newStatusDivider()
	s.pulse = newPulse()
	s.pulseBox = container.NewCenter(s.pulse.w)
	s.pulseBox.Hide()
	left := container.NewHBox(s.dot, s.context, s.tls, newStatusDivider(), s.requests, newStatusDivider(),
		s.informers, s.demoSep, s.demo, s.pulseBox)
	right := container.NewHBox()
	if sh.hasMCP() {
		s.mcp = newMCPItem(sh, sh.mcp.on)
		right.Add(s.mcp)
	}
	s.link = newLinkText(ownerLinkText, func() { sh.openOwnerLink() })
	s.link.size = sizeStatusText
	sep := newStatusDivider()
	s.layout = &statusLayout{link: s.link, sep: sep, left: left, right: right}
	s.root = sized("", sizeStatusHeight, newSurface(colorStatusBar),
		inset(theme.SizeNameInnerPadding, "", box(s.layout, s.link, sep, left, right)))
	return s
}

// The status bar's link to the owner (D-23): shown always, a GUI link and not an engine command, so
// that no agent can open the page. Only a click opens it; nothing is sent before.
const (
	ownerLinkText = "Need a Kubernetes Expert? Visit: hans-fischer.com"
	ownerLinkURL  = "https://hans-fischer.com"
)

// openOwnerLink opens the owner's page in the user's browser.
func (sh *shell) openOwnerLink() {
	u, err := url.Parse(ownerLinkURL)
	if err == nil {
		err = openBrowser(sh.app, u)
	}
	if err != nil {
		sh.toast("error", "cannot open "+ownerLinkURL+": "+err.Error())
	}
}

// statusLayout lays the status bar out: the status items at the left end, the MCP item at the right
// end, and the link to the owner and its divider directly left of the MCP item. The items keep
// their minimum widths; the link takes what is left between them, is shortened with an ellipsis
// when that is less than its text, and is hidden, with its divider, when it is less than a few
// characters. The bar's minimum width is the items' alone, so the link never costs them room (D-23).
type statusLayout struct {
	link, sep   fyne.CanvasObject
	left, right fyne.CanvasObject
}

// linkMinChars is the shortest the link is shown: its first characters and an ellipsis.
const linkMinChars = "Need a…"

// statusLift is how far the status bar's items sit above the bar's centre: the room a text line
// keeps below its letters for descenders, about an eighth of the text size, so that the letters,
// not the line's box, are centred (VS Code's status bar centres its items so).
func statusLift(textSize float32) float32 {
	return float32(math.Round(float64(textSize) * 0.12))
}

// newStatusDivider is a status bar divider as tall as a text line and centred on the items,
// instead of spanning the bar from edge to edge.
func newStatusDivider() fyne.CanvasObject {
	return box(statusDividerLayout{}, newDivider(true))
}

// statusDividerLayout centres its divider at the height of one status line.
type statusDividerLayout struct{}

func (statusDividerLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(1, 0) }

func (statusDividerLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	th := fyne.CurrentApp().Settings().Theme()
	h := fyne.Min(size.Height, fyne.MeasureText("Mg", th.Size(sizeStatusText), fyne.TextStyle{}).Height)
	for _, o := range objs {
		o.Resize(fyne.NewSize(1, h))
		o.Move(fyne.NewPos(0, (size.Height-h)/2))
	}
}

func (l *statusLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	lm, rm := l.left.MinSize(), l.right.MinSize()
	return fyne.NewSize(lm.Width+rm.Width, max(lm.Height, rm.Height, l.link.MinSize().Height))
}

func (l *statusLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	th := fyne.CurrentApp().Settings().Theme()
	lm, rm := l.left.MinSize(), l.right.MinSize()
	gap := th.Size(theme.SizeNameInnerPadding)
	textSize := th.Size(sizeStatusText)
	if lt, ok := l.link.(*linkText); ok {
		textSize = th.Size(lt.size)
	}
	// The items are laid out in a band lifted by statusLift: a text line centred in the whole bar
	// reads low, since its box keeps room for descenders below the letters (owner, 2026-10-14).
	lift := statusLift(textSize)
	size.Height -= 2 * lift
	full := fyne.MeasureText(ownerLinkText, textSize, fyne.TextStyle{}).Width
	least := fyne.MeasureText(linkMinChars, textSize, fyne.TextStyle{}).Width
	// What is left between the items for the link; its divider and the gaps around it come off. The
	// divider sits between the link and the MCP item, and a gap keeps the link off the left items.
	// Without an MCP item (--no-mcp) the link ends the bar and has no divider.
	withSep := rm.Width > 0
	room := size.Width - lm.Width - rm.Width - 3*gap - 1
	if !withSep {
		room += gap + 1
	}
	if room >= least {
		w := fyne.Min(room, full+1)
		l.show(true)
		if withSep {
			sepX := size.Width - rm.Width - gap - 1
			l.link.Move(fyne.NewPos(sepX-gap-w, 0))
			l.sep.Move(fyne.NewPos(sepX, 0))
			l.sep.Resize(fyne.NewSize(1, size.Height))
		} else {
			l.link.Move(fyne.NewPos(size.Width-w, 0))
			l.sep.Hide()
		}
		l.link.Resize(fyne.NewSize(w, size.Height))
	} else {
		l.show(false)
	}
	l.left.Move(fyne.NewPos(0, 0))
	l.left.Resize(fyne.NewSize(lm.Width, size.Height))
	l.right.Resize(fyne.NewSize(rm.Width, size.Height))
	l.right.Move(fyne.NewPos(size.Width-rm.Width, 0))
}

func (l *statusLayout) show(on bool) {
	for _, o := range []fyne.CanvasObject{l.link, l.sep} {
		if o.Visible() != on {
			if on {
				o.Show()
			} else {
				o.Hide()
			}
		}
	}
}

func (s *statusBar) update() {
	ui, d := s.sh.ui, s.sh.diag
	s.dot.setMark(ui.Context, s.sh.mark(ui.Context, false)) // the shown cluster's mark (clustermarks.go)
	name, tip := notConnected, ""
	if ui.Context != "" {
		name, tip = s.sh.identity(ui.Context).name, ui.Context // the short name; the full context id, its file included
	}
	s.context.SetText(name)
	s.context.tip = tip
	if ui.Connection.State.Live() && ui.Connection.TLSUnchecked {
		s.tls.Show()
	} else {
		s.tls.Hide()
	}
	s.requests.SetText(fmt.Sprintf("API %s req · list %s · watch %s",
		formatCount(d.total), formatCount(d.list), formatCount(d.watch)))
	s.informers.SetText(strconv.Itoa(d.informers) + " informers")
	if d.demo {
		s.demo.SetText(fmt.Sprintf("demo %s objects · %s events/s",
			formatCount(d.demoObjects), formatCount(int64(d.demoRate+0.5))))
		s.demo.Show()
		s.demoSep.Show()
		s.pulse.add(d.demoRate)
		s.pulseBox.Show()
	} else {
		s.demo.Hide()
		s.demoSep.Hide()
		s.pulseBox.Hide()
	}
}
