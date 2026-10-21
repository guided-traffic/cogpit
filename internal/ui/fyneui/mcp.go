package fyneui

import (
	"fmt"
	"image/color"
	"log/slog"
	"net/url"
	"runtime/debug"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// MCPEndpoint is what a running MCP server tells the window: where agents connect, the token, and
// the commands that register it with Claude Code.
type MCPEndpoint struct {
	// URL is the endpoint, shown in the status bar item's tooltip and the MCP details.
	URL string
	// Command is the `claude mcp add` command Help › Copy MCP command copies: HTTPCommand, or with
	// Stdio the registration of the cogpit-cli in the app's bundle.
	Command string
	// Stdio marks a Command that registers the bundled cogpit-cli's stdio bridge: run once, it
	// follows every restart of Cogpit and a regenerated token (D-2).
	Stdio bool
	// HTTPCommand is the HTTP registration, with the URL and the token: it keeps working across
	// restarts until the user regenerates the token or a start finds the port taken (F6).
	HTTPCommand string
	// Token is the bearer token. It is a credential: the MCP details show it masked until the user
	// shows it and copy it on request (cleared from the clipboard like a Secret's value); it never
	// goes to a log, an error or a notification.
	Token string
	// PortTaken is the port the server kept, which another program held at this start, so that it
	// listens on another port now and keeps that one; 0 when it got its port.
	PortTaken int
}

// MCPControl lets the user switch the external MCP server, the loopback HTTP endpoint for other
// programs, on and off from the window (D-23), and regenerate its token (F6). The server is opt-in:
// it stays off until the user switches it on (D-49). The control touches nothing else of the
// engine: an in-process agent keeps working while the endpoint is off. The window calls it from a
// goroutine of its own, but for the first Start before the window runs (startMCP, on the main
// goroutine before Fyne's loop), so it must make no Fyne call; it is the only way to start the
// server or replace its token: neither is an engine command or an MCP tool, so no agent can switch
// the server on or cut the other clients off. The desktop app implements it over internal/mcp
// (cmd/cogpit), which internal/ui may not import.
type MCPControl interface {
	// Start brings the server up, with the token and the port it kept, and returns its endpoint.
	// fromUser is true for a click on the status bar item and false for the start at launch, so
	// that the implementation can record the user's switch in the audit log.
	Start(fromUser bool) (MCPEndpoint, error)
	// Stop takes the server down: it stops listening and ends every agent connection and pending
	// tool call; the token and the port stay for the next start. Only the user's click calls it;
	// quitting is the caller's own business.
	Stop() error
	// Regenerate replaces the token: the old one is refused at once and every connection it
	// admitted ends, a call waiting for the user's confirmation included; the port stays. It
	// returns the endpoint with the new token while the server runs, the zero endpoint while it is
	// off (the next Start serves the new token). Only the user's confirmed click calls it.
	Regenerate() (MCPEndpoint, error)
}

// MCPSwitchStore keeps the user's switch where cogpit-cli reads it too (D-49, A15): an MCPControl
// that implements it is asked at launch and told after every click. The desktop app's does
// (cmd/cogpit, a file beside the token file); without it the window keeps the switch in Fyne's
// preferences only.
type MCPSwitchStore interface {
	// StoredMCP returns the stored switch and whether the user ever set it.
	StoredMCP() (on, set bool)
	// StoreMCP stores the switch.
	StoreMCP(on bool) error
}

// prefMCPEnabled is the preference that remembered the user's switch before D-49, and still does
// beside the switch store. Only a click writes it. Absent means the user never chose, which is off:
// the server is opt-in (the owner, 2026-10-21); before that, absent meant on.
const prefMCPEnabled = "mcp.enabled"

// legacyMCP returns the preference's choice and whether the user made one. Fyne has no "is it
// set" query, so the two fallbacks tell a stored value from an absent one: a stored value ignores
// the fallback, an absent one returns it.
func (sh *shell) legacyMCP() (on, chosen bool) {
	if sh.app == nil {
		return false, false
	}
	p := sh.app.Preferences()
	on = p.BoolWithFallback(prefMCPEnabled, false)
	return on, on == p.BoolWithFallback(prefMCPEnabled, true)
}

// storedMCP returns whether the server starts at launch: only a choice to switch it on does. The
// switch store's choice decides when the user ever set it; else the preference's does, and an
// explicit one (a switch set before D-49, "on" included) is copied into the store once, where
// cogpit-cli reads it. Neither set, a new installation, is off, and nothing is written.
func (sh *shell) storedMCP(ctl MCPControl) bool {
	pref, chosen := sh.legacyMCP()
	st, ok := ctl.(MCPSwitchStore)
	if !ok {
		return pref
	}
	if on, set := st.StoredMCP(); set {
		return on
	}
	if !chosen {
		return false
	}
	if err := st.StoreMCP(pref); err != nil {
		slog.Warn("MCP switch not stored", "err", err)
	}
	return pref
}

// mcpState is what the window knows of the MCP server. Main goroutine only.
type mcpState struct {
	on      bool   // the server runs
	busy    bool   // a start or stop is under way; clicks are ignored
	command string // the registration command of the current run; "" while off
	stdio   bool   // command registers the bundled cogpit-cli (MCPEndpoint.Stdio)
	url     string
	err     string // why the last start failed; "" otherwise
	// httpCommand and token are the HTTP registration and the bearer token while the server runs,
	// "" while off. The token is a credential: the details show it masked unless revealed, and it
	// never reaches a toast or a log (F6).
	httpCommand string
	token       string
	// portTaken is the kept port another program held when the server last started (MCPEndpoint.
	// PortTaken); 0 when it got its port.
	portTaken int
	// regenerating: a Regenerate token is under way; the switch waits.
	regenerating bool
	// details is the open MCP details panel (mcpdetails.go), nil while closed.
	details *mcpDetails
}

// hasMCP reports whether the window has an MCP server at all: --no-mcp leaves neither the item nor
// the menu entry.
func (sh *shell) hasMCP() bool { return sh.opts.MCP != nil || sh.opts.MCPCommand != "" }

// mcpTip is the status bar item's tooltip.
func (sh *shell) mcpTip() string {
	m := sh.mcp
	switch {
	case m.busy && m.on:
		return mcpTipName + ": stopping…"
	case m.busy:
		return mcpTipName + ": starting…"
	case m.on && m.url != "":
		return mcpTipName + ": on — " + m.url + " — click to stop"
	case m.on:
		return mcpTipName + ": on — click to stop"
	case m.err != "":
		return mcpTipName + ": off — click to start\nThe last start failed: " + m.err
	}
	return mcpTipName + ": off until you switch it on — click to start"
}

// mcpTipName names what the switch controls: the HTTP endpoint other programs connect to with the
// token. A built-in chat (D-10) calls the engine in-process and does not depend on it.
const mcpTipName = "External MCP server (for other apps on this computer)"

// startMCP starts the server at launch only if the user switched it on in an earlier run: it is
// opt-in, so a new installation shows the item off. A server that cannot start leaves the item in
// its off state with the reason in the tooltip; the stored choice stays as it was. Call it once,
// before the window shows.
func (sh *shell) startMCP() {
	ctl := sh.opts.MCP
	if ctl == nil {
		return
	}
	if !sh.storedMCP(ctl) {
		sh.applyMCP(false, MCPEndpoint{}, "")
		return
	}
	ep, err := ctl.Start(false)
	if err != nil {
		slog.Warn("MCP server not started", "err", err)
		sh.applyMCP(false, MCPEndpoint{}, err.Error())
		return
	}
	sh.applyMCP(true, ep, "")
}

// mcpStartNotice shows, once the window is up (showStartNotices), that the server found its kept
// port taken at launch.
func (sh *shell) mcpStartNotice() {
	if sh.mcp.on && sh.mcp.portTaken != 0 {
		sh.toast("warning", sh.mcpMovedText())
	}
}

// mcpMovedText says that the server moved to another port, and what that means for the clients.
// It names ports only, never the token.
func (sh *shell) mcpMovedText() string {
	return fmt.Sprintf("Another program held the MCP server's port %d, so the server listens on %s now and keeps it. "+
		"Clients registered with the HTTP command need the new port: copy it again from the gear of the MCP item. "+
		"cogpit-cli mcp follows by itself.", sh.mcp.portTaken, portOfURL(sh.mcp.url))
}

// portOfURL is the port of an endpoint URL, "?" when it has none.
func portOfURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Port() == "" {
		return "?"
	}
	return u.Port()
}

// toggleMCP is a click on the status bar item: it stops a running server and starts a stopped one.
// The work runs off the main goroutine (stopping waits for open requests); a click while it runs
// is ignored. The choice is stored once the switch worked.
func (sh *shell) toggleMCP() {
	ctl := sh.opts.MCP
	if ctl == nil || sh.mcp.busy || sh.mcp.regenerating {
		return
	}
	want := !sh.mcp.on
	sh.mcp.busy = true
	sh.refreshMCP()
	go func() {
		var (
			ep  MCPEndpoint
			err error
		)
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("MCP switch panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
					err = fmt.Errorf("internal error: %v", r)
				}
			}()
			if want {
				ep, err = ctl.Start(true)
			} else {
				err = ctl.Stop()
			}
		}()
		sh.do(func() {
			sh.mcp.busy = false
			switch {
			case want && err != nil:
				sh.applyMCP(false, MCPEndpoint{}, err.Error())
				sh.toast("error", "cannot start the MCP server: "+err.Error())
				return
			case want:
				sh.applyMCP(true, ep, "")
				if ep.PortTaken != 0 {
					sh.toast("warning", sh.mcpMovedText())
				}
			default:
				// A failed stop (the token file could not be removed) still leaves nothing
				// listening: the server is off either way.
				sh.applyMCP(false, MCPEndpoint{}, "")
				if err != nil {
					sh.toast("error", "the MCP server is off, but: "+err.Error())
				}
			}
			if sh.app != nil {
				sh.app.Preferences().SetBool(prefMCPEnabled, want)
			}
		})
		if st, ok := ctl.(MCPSwitchStore); ok { // off the main goroutine: a file write
			if err := st.StoreMCP(want); err != nil {
				sh.do(func() { sh.toast("error", "Cogpit could not store the MCP switch for cogpit-cli: "+err.Error()) })
			}
		}
	}()
}

// applyMCP records the server's state and brings the item, the details and the menu in line, so
// that no control offers a command or a token of a server that is not running.
func (sh *shell) applyMCP(on bool, ep MCPEndpoint, errText string) {
	m := &sh.mcp
	m.on, m.err = on, errText
	m.command, m.url, m.stdio, m.httpCommand, m.token, m.portTaken = "", "", false, "", "", 0
	if on {
		m.command, m.url, m.stdio, m.httpCommand, m.token, m.portTaken = ep.Command, ep.URL, ep.Stdio, ep.HTTPCommand, ep.Token, ep.PortTaken
	}
	sh.refreshMCP()
}

// refreshMCP shows sh.mcp in the status bar item, the MCP details and the Help menu.
func (sh *shell) refreshMCP() {
	if sh.status != nil && sh.status.mcp != nil {
		sh.status.mcp.set(sh.mcp.on, sh.mcpTip())
	}
	if d := sh.mcp.details; d != nil {
		d.rebuild()
	}
	if sh.mcpMenu != nil {
		sh.mcpMenu.Disabled = !sh.mcp.on
		if sh.win != nil {
			if m := sh.win.MainMenu(); m != nil {
				m.Refresh()
			}
		}
	}
}

// mcpItem is the status bar's MCP indicator and its switch: the accent pill while the server runs,
// a muted pill with a line through it while it is off. Clicking the pill asks the shell to switch
// the server; the tooltip says what a click does. The gear after it opens the MCP details (F6).
type mcpItem struct {
	widget.BaseWidget
	sh   *shell
	pill *pill
	gear *mcpGear
	on   bool
	tip  string
}

func newMCPItem(sh *shell, on bool) *mcpItem {
	it := &mcpItem{sh: sh, pill: newPill("MCP", colorAccent, true), gear: newMCPGear(sh), on: on}
	it.ExtendBaseWidget(it)
	it.set(on, sh.mcpTip())
	return it
}

// set shows the state and the tooltip text.
func (it *mcpItem) set(on bool, tip string) {
	it.on, it.tip = on, tip
	it.pill.tinted = on
	if on {
		it.pill.Set("MCP", colorAccent)
	} else {
		it.pill.Set("MCP", colorMuted)
	}
	it.pill.Refresh()
	it.Refresh()
	if it.sh.tip != nil && it.sh.tip.owner == it { // a shown tooltip follows the state
		it.sh.tip.hide()
	}
}

// Tapped implements fyne.Tappable.
func (it *mcpItem) Tapped(*fyne.PointEvent) {
	it.sh.tip.hideFor(it)
	it.sh.toggleMCP()
}

// Cursor implements desktop.Cursorable.
func (it *mcpItem) Cursor() desktop.Cursor { return desktop.PointerCursor }

// MouseIn implements desktop.Hoverable.
func (it *mcpItem) MouseIn(ev *desktop.MouseEvent) { it.sh.tip.hover(it, it.tip, ev.AbsolutePosition) }

// MouseMoved implements desktop.Hoverable.
func (it *mcpItem) MouseMoved(ev *desktop.MouseEvent) { it.sh.tip.moved(it, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (it *mcpItem) MouseOut() { it.sh.tip.hideFor(it) }

// CreateRenderer implements fyne.Widget.
func (it *mcpItem) CreateRenderer() fyne.WidgetRenderer {
	r := &mcpItemRenderer{it: it, strike: canvas.NewLine(color.Transparent)}
	r.strike.StrokeWidth = 1.5
	r.Refresh()
	return r
}

type mcpItemRenderer struct {
	it     *mcpItem
	strike *canvas.Line
}

// gap is the room between the pill and the gear.
func (r *mcpItemRenderer) gap() float32 { return r.it.Theme().Size(theme.SizeNamePadding) / 2 }

func (r *mcpItemRenderer) MinSize() fyne.Size {
	p, g := r.it.pill.MinSize(), r.it.gear.MinSize()
	return fyne.NewSize(p.Width+r.gap()+g.Width, max(p.Height, g.Height))
}

func (r *mcpItemRenderer) Layout(size fyne.Size) {
	g := r.it.gear.MinSize()
	pillW := max(size.Width-g.Width-r.gap(), 0)
	r.it.pill.Move(fyne.NewPos(0, 0))
	r.it.pill.Resize(fyne.NewSize(pillW, size.Height))
	r.it.gear.Move(fyne.NewPos(size.Width-g.Width, (size.Height-g.Height)/2))
	r.it.gear.Resize(g)
	// The line runs through the pill's text, a little inside its ends.
	ms := r.it.pill.MinSize()
	x0 := (pillW - ms.Width) / 2
	y := size.Height / 2
	r.strike.Position1 = fyne.NewPos(x0+3, y)
	r.strike.Position2 = fyne.NewPos(x0+ms.Width-3, y)
}

func (r *mcpItemRenderer) Refresh() {
	r.strike.StrokeColor = themeColor(r.it.Theme(), colorMuted)
	if r.it.on {
		r.strike.Hide()
	} else {
		r.strike.Show()
	}
	r.Layout(r.it.Size())
	r.it.pill.Refresh()
	r.strike.Refresh()
	r.it.gear.Refresh()
}

func (r *mcpItemRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.it.pill, r.strike, r.it.gear}
}

func (r *mcpItemRenderer) Destroy() {}

// mcpGearTip is the gear's tooltip.
const mcpGearTip = "MCP connection details and settings"

// mcpGear is the gear in the status bar's MCP item: a click opens the MCP details, or closes them.
// It is the window's alone, as the switch is: no command opens it.
type mcpGear struct {
	widget.BaseWidget
	sh      *shell
	hovered bool
}

var (
	_ fyne.Tappable      = (*mcpGear)(nil)
	_ desktop.Hoverable  = (*mcpGear)(nil)
	_ desktop.Cursorable = (*mcpGear)(nil)
)

func newMCPGear(sh *shell) *mcpGear {
	g := &mcpGear{sh: sh}
	g.ExtendBaseWidget(g)
	return g
}

// Tapped implements fyne.Tappable.
func (g *mcpGear) Tapped(*fyne.PointEvent) {
	g.sh.tip.hideFor(g)
	g.sh.toggleMCPDetails()
}

// Cursor implements desktop.Cursorable.
func (g *mcpGear) Cursor() desktop.Cursor { return desktop.PointerCursor }

// MouseIn implements desktop.Hoverable.
func (g *mcpGear) MouseIn(ev *desktop.MouseEvent) {
	g.hovered = true
	g.Refresh()
	g.sh.tip.hover(g, mcpGearTip, ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (g *mcpGear) MouseMoved(ev *desktop.MouseEvent) { g.sh.tip.moved(g, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (g *mcpGear) MouseOut() {
	g.hovered = false
	g.Refresh()
	g.sh.tip.hideFor(g)
}

// CreateRenderer implements fyne.Widget.
func (g *mcpGear) CreateRenderer() fyne.WidgetRenderer {
	r := &mcpGearRenderer{g: g, bg: canvas.NewRectangle(color.Transparent), img: newIconImage()}
	r.Refresh()
	return r
}

type mcpGearRenderer struct {
	g   *mcpGear
	bg  *canvas.Rectangle
	img *iconImage
}

// icon is the gear's size: a little more than the status bar's text.
func (r *mcpGearRenderer) icon() float32 { return r.g.Theme().Size(sizeStatusText) + 3 }

func (r *mcpGearRenderer) MinSize() fyne.Size {
	is := r.icon()
	pad := r.g.Theme().Size(theme.SizeNamePadding)
	return fyne.NewSize(is+pad, is+pad/2)
}

func (r *mcpGearRenderer) Layout(size fyne.Size) {
	is := r.icon()
	r.bg.Resize(size)
	r.img.img.Move(fyne.NewPos((size.Width-is)/2, (size.Height-is)/2))
	r.img.img.Resize(fyne.NewSize(is, is))
}

func (r *mcpGearRenderer) Refresh() {
	th := r.g.Theme()
	r.bg.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
	r.bg.FillColor = color.Transparent
	c := colorMuted
	if r.g.hovered || r.g.sh.mcp.details != nil {
		r.bg.FillColor = themeColor(th, theme.ColorNameHover)
		c = theme.ColorNameForeground
	}
	r.img.set(th, theme.SettingsIcon(), c)
	r.Layout(r.g.Size())
	r.bg.Refresh()
}

func (r *mcpGearRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.bg, r.img.img} }
func (r *mcpGearRenderer) Destroy()                     {}
