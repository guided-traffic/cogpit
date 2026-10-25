package fyneui

import (
	"fmt"
	"image/color"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The MCP details (F6, the owner's decision of 2026-10-15). The gear in the status bar's MCP item
// opens a panel above it with the external MCP server's switch, its endpoint, the bearer token, the
// registration commands, and Regenerate token, which warns first. The token is shown masked until
// the user shows it (as a Secret's value in the drawer), hidden again when the panel closes, and
// copied on request: a copy of the token or of the HTTP registration, which carries it, is cleared
// from the clipboard like a Secret's value. The token lives in the window's mcpState and the
// panel's widgets only, never in a toast, a log or engine state. Like the switch, the panel and
// Regenerate token are the window's alone: no engine command opens or runs them, so no agent can
// read the token there or cut the other clients off. The top bar's MCP button is gone; Help › Copy
// MCP command copies the command and opens this panel.

// mcpTokenMask stands for the token while it is hidden: always the same, so it says nothing of the
// token.
const mcpTokenMask = "••••••••••••••••••••••••" // #nosec G101 -- the mask that stands for the token, no credential

// mcpCommandMask stands for the token inside the HTTP registration while it is hidden.
const mcpCommandMask = "••••••••"

// The details' texts.
const (
	mcpDetailsIntro = "For other apps on this computer, such as Claude Code. Cogpit's own agent does not need it."
	mcpDetailsOff   = "Off: no app on this computer reaches Cogpit over MCP, cogpit-cli mcp included. The server starts " +
		"only when you switch it on, and Cogpit shows the command that registers it with Claude Code once it runs. " +
		"Switched on, the server keeps its token and its port, so the clients registered before connect again."
	mcpStdioText = "Run it once in a terminal: Claude Code then starts the cogpit-cli inside Cogpit.app, which drives " +
		"this window whenever Cogpit runs and follows restarts, updates and a new token by itself. " +
		"Run it again if you move Cogpit.app."
	mcpHTTPTextAlso = "Over HTTP, for clients that do not start programs: the URL and the token. It keeps working " +
		"across restarts; run it again after you regenerate the token or when the server had to move to another port."
	mcpHTTPTextOnly = "Run it once in a terminal. It holds the URL and the token and keeps working across restarts " +
		"of Cogpit; run it again after you regenerate the token or when the server had to move to another port."
	mcpRegenerateHint = "Ends every connection; clients then need the new token."
	// mcpRegenerateWarning is the warning before Regenerate token (F6: it warns first).
	mcpRegenerateWarning = "Regenerating the token ends every MCP connection now: calls under way stop, and a " +
		"call waiting for your confirmation does not run. The clients need the new token to connect again: " +
		"run the HTTP registration again, or give other MCP clients the new token. The port stays."
	mcpRegenerateBridge = "cogpit-cli mcp (the registration that runs once) reads the new token by itself."
	mcpRegeneratedText  = "MCP token regenerated. Clients registered with the HTTP command need the new one; " +
		"cogpit-cli mcp reads it by itself."
)

// mcpDetails is the open MCP details panel.
type mcpDetails struct {
	sh    *shell
	panel *mcpPanel
	// revealed shows the token instead of the mask, until the user hides it or the panel closes.
	revealed bool
	// note is the line at the panel's foot: what the last copy or Regenerate token did. A toast
	// would lie under the panel, which covers the toasts' corner; the note names no token.
	note      string
	noteColor fyne.ThemeColorName
	// warning is the open warning before Regenerate token.
	warning *mcpRegenerateDialog

	// The panel's controls, for tests; rebuilt with the panel's content.
	switchB, reveal                              *textButton
	revealB                                      *textButton // the token's Show and Hide, kept across rebuilds (VW1)
	regenerate                                   *textButton
	copyEndpoint, copyToken, copyStdio, copyHTTP *tipButton
}

// toggleMCPDetails opens the MCP details, or closes them: the gear's click.
func (sh *shell) toggleMCPDetails() {
	if sh.mcp.details != nil {
		sh.closeMCPDetails()
		return
	}
	sh.openMCPDetails()
}

// openMCPDetails shows the MCP details above the status bar's MCP item.
func (sh *shell) openMCPDetails() {
	if sh.mcp.details != nil || !sh.hasMCP() || sh.win == nil {
		return
	}
	sh.tip.hide()
	d := &mcpDetails{sh: sh}
	d.panel = newMCPPanel(d)
	sh.mcp.details = d
	sh.win.Canvas().Overlays().Add(d.panel)
	onEscape(sh.win.Canvas(), d.panel, sh.closeMCPDetails) // escape.go
	d.panel.Resize(sh.win.Canvas().Size())
	d.rebuild()
	sh.refreshMCPGear()
}

// closeMCPDetails hides the MCP details; a shown token is hidden with them.
func (sh *shell) closeMCPDetails() {
	d := sh.mcp.details
	if d == nil {
		return
	}
	sh.win.Canvas().Overlays().Remove(d.panel)
	sh.mcp.details = nil
	sh.refreshMCPGear()
}

// refreshMCPGear shows whether the details are open on the gear.
func (sh *shell) refreshMCPGear() {
	if sh.status != nil && sh.status.mcp != nil {
		sh.status.mcp.gear.Refresh()
	}
}

// copyMCPCommand puts the registration command of the running server on the clipboard (Help › Copy
// MCP command) and opens the MCP details, which say what it does. The HTTP registration carries
// the token, so it is cleared from the clipboard like a Secret's value. While the server is off
// there is no command: an HTTP client would reach nothing, and the bundled cogpit-cli would refuse
// to serve.
func (sh *shell) copyMCPCommand() {
	cmd := sh.mcp.command
	if !sh.mcp.on || cmd == "" {
		return
	}
	secret := sh.mcp.token != "" && strings.Contains(cmd, sh.mcp.token)
	sh.copyText(cmd, secret)
	sh.openMCPDetails()
	if d := sh.mcp.details; d != nil {
		d.copied("the registration command", secret)
	}
}

// rebuild shows the server's state in the open panel.
func (d *mcpDetails) rebuild() {
	if d.panel != nil {
		d.panel.setContent(d.content())
	}
}

// copied puts what was copied in the panel's note.
func (d *mcpDetails) copied(what string, secret bool) {
	d.note, d.noteColor = "Copied "+what+" to the clipboard.", colorMuted
	if secret { // the token, or a command that holds it
		d.note += fmt.Sprintf(" The clipboard is cleared in %s.", ttlText(d.sh.secretClipTTL))
	}
	d.rebuild()
}

// copy copies text, a credential when secret, and says so in the note.
func (d *mcpDetails) copy(text, what string, secret bool) {
	if text == "" {
		return
	}
	d.sh.copyText(text, secret)
	d.copied(what, secret)
}

// width is the panel's content width.
func (d *mcpDetails) width() float32 {
	th := d.sh.app.Settings().Theme()
	return th.Size(sizePickerWidth) - 2*th.Size(theme.SizeNameInnerPadding)
}

// content is the panel's card: the header with the switch, then the server's details.
func (d *mcpDetails) content() fyne.CanvasObject {
	d.switchB, d.reveal, d.regenerate = nil, nil, nil
	d.copyEndpoint, d.copyToken, d.copyStdio, d.copyHTTP = nil, nil, nil, nil
	sh, width := d.sh, d.width()
	title := newTextLine("MCP server", theme.ColorNameForeground, theme.SizeNameSubHeadingText, fyne.TextStyle{Bold: true})
	head := inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, container.NewBorder(nil, nil, nil, d.switchControls(), title))

	body := container.NewVBox(newParagraph(mcpDetailsIntro, colorMuted, width))
	switch m := sh.mcp; {
	case sh.opts.MCP == nil: // a server the window cannot switch (Options.MCPCommand): its command only
		block, btn := d.codeBlock(m.command, "Copy the registration command", width)
		btn.OnTapped = func() { d.copy(sh.mcp.command, "the registration command", false) }
		body.Add(block)
	case m.on:
		d.addRunning(body, width)
	default:
		d.addOff(body, width)
	}
	if d.note != "" {
		body.Add(newParagraph(d.note, d.noteColor, width))
	}
	return container.NewVBox(head, newDivider(false), inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, body))
}

// switchControls are the header's state and the server's switch, the status bar item's switch in
// a button.
func (d *mcpDetails) switchControls() fyne.CanvasObject {
	sh, m := d.sh, d.sh.mcp
	text, c := "Off", colorMuted
	if m.on {
		text, c = "On", colorAccent
	}
	state := container.NewCenter(newPill(text, c, true))
	if sh.opts.MCP == nil {
		return state
	}
	label := "Switch on"
	switch {
	case m.busy && m.on:
		label = "Stopping…"
	case m.busy:
		label = "Starting…"
	case m.on:
		label = "Switch off"
	}
	d.switchB = newTextButton(label, nil, sh.toggleMCP)
	if m.busy || m.regenerating {
		d.switchB.Disable()
	}
	return container.NewHBox(state, d.switchB)
}

// addRunning adds the running server's endpoint, token, port note, Regenerate token and the
// registration commands.
func (d *mcpDetails) addRunning(body *fyne.Container, width float32) {
	sh, m := d.sh, d.sh.mcp
	d.copyEndpoint = newTipButton(sh, theme.ContentCopyIcon(), "Copy the endpoint", func() { d.copy(sh.mcp.url, "the endpoint", false) })
	body.Add(d.row("Endpoint", newTextLine(m.url, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Monospace: true}), d.copyEndpoint))

	label := "Show"
	if d.revealed {
		label = hideLabel
	}
	if d.revealB == nil { // the token is a secret: Show marked as exposing (RW3); one widget for Show and Hide (VW1)
		d.revealB = revealButton(label, func() { d.revealed = !d.revealed; d.rebuild() })
	}
	setRevealLook(d.revealB, label)
	d.reveal = d.revealB
	d.copyToken = newTipButton(sh, theme.ContentCopyIcon(), "Copy the token", func() { d.copy(sh.mcp.token, "the token", true) })
	shown := fyne.CanvasObject(newTextLine(mcpTokenMask, colorMuted, theme.SizeNameText, fyne.TextStyle{}))
	if d.revealed {
		p := newParagraph(m.token, theme.ColorNameForeground, d.valueWidth(width, d.reveal, d.copyToken))
		p.style, p.hard = fyne.TextStyle{Monospace: true}, true
		shown = p
	}
	body.Add(d.row("Token", shown, d.reveal, d.copyToken))

	if m.portTaken != 0 {
		body.Add(newParagraph(fmt.Sprintf("Port %d was taken by another program when the server started, so it listens "+
			"on %s now and keeps that port. If you do not know that program, regenerate the token: a client registered "+
			"with the HTTP command may have sent it there.", m.portTaken, portOfURL(m.url)), colorStatusWarning, width))
	}
	body.Add(d.regenerateRow())

	body.Add(newSpace(theme.SizeNamePadding))
	body.Add(newDivider(false))
	body.Add(newTextLine("Register with Claude Code", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true}))
	httpText := mcpHTTPTextOnly
	if m.stdio {
		httpText = mcpHTTPTextAlso
		body.Add(newParagraph(mcpStdioText, colorMuted, width))
		block, btn := d.codeBlock(m.command, "Copy the registration command", width)
		btn.OnTapped = func() { d.copy(sh.mcp.command, "the registration command", false) }
		d.copyStdio = btn
		body.Add(block)
		body.Add(newSpace(theme.SizeNamePadding))
	}
	body.Add(newParagraph(httpText, colorMuted, width))
	shownCmd := m.httpCommand
	if !d.revealed && m.token != "" {
		shownCmd = strings.ReplaceAll(shownCmd, m.token, mcpCommandMask)
	}
	block, btn := d.codeBlock(shownCmd, "Copy the HTTP registration", width)
	btn.OnTapped = func() { d.copy(sh.mcp.httpCommand, "the HTTP registration", true) }
	d.copyHTTP = btn
	body.Add(block)
}

// addOff adds what the panel says while the server is off: why, and Regenerate token, which then
// takes effect at the next start.
func (d *mcpDetails) addOff(body *fyne.Container, width float32) {
	if e := d.sh.mcp.err; e != "" {
		body.Add(newParagraph("The last start failed: "+e, colorStatusError, width))
	}
	body.Add(newParagraph(mcpDetailsOff, theme.ColorNameForeground, width))
	body.Add(d.regenerateRow())
}

// regenerateRow is Regenerate token and its hint.
func (d *mcpDetails) regenerateRow() fyne.CanvasObject {
	d.regenerate = newPlateButton("Regenerate token", theme.ViewRefreshIcon(), d.sh.askRegenerateMCP)
	if d.sh.mcp.busy || d.sh.mcp.regenerating {
		d.regenerate.Disable()
	}
	return container.NewBorder(nil, nil, d.regenerate, nil,
		inset(theme.SizeNamePadding, "", newTextLine(mcpRegenerateHint, colorMuted, theme.SizeNameText, fyne.TextStyle{})))
}

// row is a named value with its buttons: the name in the drawer's label column, every part
// centred on the row.
func (d *mcpDetails) row(name string, value fyne.CanvasObject, buttons ...fyne.CanvasObject) fyne.CanvasObject {
	return container.NewBorder(nil, nil,
		sized(sizeDrawerLabel, "", newTextLine(name, colorMuted, theme.SizeNameText, fyne.TextStyle{})),
		container.NewHBox(buttons...), value)
}

// valueWidth is the room a row (row) leaves its value beside the label and the buttons: the
// Border's and the box's paddings between them come off.
func (d *mcpDetails) valueWidth(width float32, buttons ...fyne.CanvasObject) float32 {
	th := d.sh.app.Settings().Theme()
	pad := th.Size(theme.SizeNamePadding)
	w := width - th.Size(sizeDrawerLabel) - 2*pad
	for i, b := range buttons {
		w -= b.MinSize().Width
		if i > 0 {
			w -= pad
		}
	}
	return max(w, 80)
}

// codeBlock shows a command in monospace on the input background, wrapped to the width, with its
// copy button; the caller sets what the button copies.
func (d *mcpDetails) codeBlock(shown, tip string, width float32) (fyne.CanvasObject, *tipButton) {
	th := d.sh.app.Settings().Theme()
	btn := newTipButton(d.sh, theme.ContentCopyIcon(), tip, nil)
	pad := th.Size(theme.SizeNamePadding)
	p := newParagraph(shown, theme.ColorNameForeground, max(width-btn.MinSize().Width-3*pad, 80))
	p.style, p.hard = fyne.TextStyle{Monospace: true}, true
	block := container.NewStack(newSurface(theme.ColorNameInputBackground), inset(theme.SizeNamePadding, theme.SizeNamePadding, p))
	return container.NewBorder(nil, nil, nil, container.NewVBox(btn), block), btn
}

// mcpRegenerateDialog is the open warning before Regenerate token.
type mcpRegenerateDialog struct {
	d       *dialog.CustomDialog
	confirm *textButton
	cancel  *textButton
}

// askRegenerateMCP warns before the token is regenerated: every connection ends, and the clients
// need the new token. Its button arms after the confirmations' delay, as theirs do.
func (sh *shell) askRegenerateMCP() {
	d := sh.mcp.details
	if sh.opts.MCP == nil || sh.mcp.busy || sh.mcp.regenerating || d == nil {
		return
	}
	width := sh.app.Settings().Theme().Size(sizeConfirmWidth)
	w := &mcpRegenerateDialog{confirm: newPlateButton("Regenerate token", theme.WarningIcon(), nil),
		cancel: newPlateButton("Cancel", nil, nil)}
	w.confirm.Importance = widget.DangerImportance
	content := container.NewVBox(newParagraph(mcpRegenerateWarning, theme.ColorNameForeground, width),
		newSpace(theme.SizeNamePadding), newParagraph(mcpRegenerateBridge, colorMuted, width))
	w.d = dialog.NewCustomWithoutButtons("Regenerate the MCP token?", content, sh.win)
	w.d.SetButtons([]fyne.CanvasObject{w.cancel, w.confirm})
	w.d.SetOnClosed(func() {
		if d.warning == w {
			d.warning = nil
		}
	})
	w.confirm.OnTapped = func() {
		w.d.Hide()
		sh.regenerateMCP()
	}
	w.cancel.OnTapped = w.d.Hide
	if delay := sh.confirmer.armDelay; delay > 0 {
		w.confirm.Disable()
		time.AfterFunc(delay, func() { sh.do(w.confirm.Enable) })
	}
	d.warning = w
	sh.showDialog(w.d) // no button has the focus: Enter must not regenerate by accident; Escape is Cancel
}

// regenerateMCP replaces the token off the main goroutine (a file write, connections ending) and
// shows the new one, masked.
func (sh *shell) regenerateMCP() {
	ctl := sh.opts.MCP
	if ctl == nil || sh.mcp.busy || sh.mcp.regenerating {
		return
	}
	sh.mcp.regenerating = true
	sh.refreshMCP()
	go func() {
		var (
			ep  MCPEndpoint
			err error
		)
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("MCP token regeneration panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
					err = fmt.Errorf("internal error: %v", r)
				}
			}()
			ep, err = ctl.Regenerate()
		}()
		sh.do(func() { sh.regeneratedMCP(ep, err) })
	}()
}

// regeneratedMCP shows the outcome of Regenerate token: the new token, masked, and a notice that
// names no token.
func (sh *shell) regeneratedMCP(ep MCPEndpoint, err error) {
	m := &sh.mcp
	m.regenerating = false
	d := m.details
	if err != nil {
		if d != nil {
			d.note, d.noteColor = "The token was not regenerated: "+err.Error(), colorStatusError
		}
		sh.refreshMCP()
		sh.toast("error", "Cogpit could not regenerate the MCP token: "+err.Error())
		return
	}
	if d != nil {
		d.revealed = false
		d.note, d.noteColor = mcpRegeneratedText, colorMuted
	}
	if m.on {
		ep.PortTaken = m.portTaken // the port note stays for this run
		sh.applyMCP(ep.URL != "", ep, "")
	} else {
		sh.refreshMCP()
	}
	sh.toast("info", mcpRegeneratedText)
}

// mcpPanel lies over the window while the MCP details are open: the card above the status bar,
// its right edge at the gear's, and around it a transparent layer whose click closes the panel (a
// click on the gear too, which then does not open it again).
type mcpPanel struct {
	widget.BaseWidget
	d       *mcpDetails
	card    *canvas.Rectangle
	content *fyne.Container
	inner   fyne.CanvasObject // the content, whose size the card takes up to the room there is
}

var (
	_ fyne.Tappable          = (*mcpPanel)(nil)
	_ fyne.SecondaryTappable = (*mcpPanel)(nil)
)

func newMCPPanel(d *mcpDetails) *mcpPanel {
	p := &mcpPanel{d: d, card: canvas.NewRectangle(color.Transparent), content: container.NewStack()}
	p.ExtendBaseWidget(p)
	return p
}

// setContent shows o in the card, scrolling when the window is too low for it.
func (p *mcpPanel) setContent(o fyne.CanvasObject) {
	p.inner = o
	p.content.Objects = []fyne.CanvasObject{newVScroll(o)}
	p.content.Refresh()
	p.Refresh()
}

// Tapped implements fyne.Tappable: a tap reaches the layer only outside the card's widgets.
func (p *mcpPanel) Tapped(ev *fyne.PointEvent) {
	pos, size := p.card.Position(), p.card.Size()
	if ev.Position.X < pos.X || ev.Position.X > pos.X+size.Width || ev.Position.Y < pos.Y || ev.Position.Y > pos.Y+size.Height {
		p.d.sh.closeMCPDetails()
	}
}

// TappedSecondary implements fyne.SecondaryTappable: a right-click outside closes it too.
func (p *mcpPanel) TappedSecondary(ev *fyne.PointEvent) { p.Tapped(ev) }

// CreateRenderer implements fyne.Widget.
func (p *mcpPanel) CreateRenderer() fyne.WidgetRenderer { return &mcpPanelRenderer{p: p} }

type mcpPanelRenderer struct{ p *mcpPanel }

// Layout puts the card above the status bar, its right edge at the gear's.
func (r *mcpPanelRenderer) Layout(size fyne.Size) {
	p := r.p
	th := p.Theme()
	pad := th.Size(theme.SizeNamePadding)
	var cs fyne.Size
	if p.inner != nil {
		cs = p.inner.MinSize()
	}
	sh := p.d.sh
	drv := fyne.CurrentApp().Driver()
	bottom := size.Height - th.Size(sizeStatusHeight) - pad
	right := size.Width - th.Size(theme.SizeNameInnerPadding)
	if sh.status != nil && sh.status.mcp != nil && sh.status.mcp.Visible() {
		bottom = drv.AbsolutePositionForObject(sh.status.root).Y - pad
		g := sh.status.mcp.gear
		right = drv.AbsolutePositionForObject(g).X + g.Size().Width
	}
	cs = fyne.NewSize(min(cs.Width, size.Width-2*pad), min(cs.Height, bottom-2*pad))
	x := max(pad, min(right, size.Width-pad)-cs.Width)
	y := max(pad, bottom-cs.Height)
	p.card.Move(fyne.NewPos(x, y))
	p.card.Resize(cs)
	p.content.Move(fyne.NewPos(x, y))
	p.content.Resize(cs)
}

func (r *mcpPanelRenderer) MinSize() fyne.Size { return fyne.Size{} }

func (r *mcpPanelRenderer) Refresh() {
	p := r.p
	th := p.Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	p.card.FillColor = th.Color(theme.ColorNameOverlayBackground, v)
	p.card.StrokeColor = themeColor(th, theme.ColorNameSeparator)
	p.card.StrokeWidth = 1
	p.card.CornerRadius = th.Size(theme.SizeNamePopupRadius)
	p.card.Shadow = canvas.Shadow{Color: th.Color(theme.ColorNameShadow, v), BlurRadius: 14,
		Offset: fyne.NewPos(0, 4), Variant: canvas.DropShadow}
	p.card.Refresh()
	r.Layout(p.Size())
	p.content.Refresh()
}

func (r *mcpPanelRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.p.card, r.p.content}
}

func (r *mcpPanelRenderer) Destroy() {}
