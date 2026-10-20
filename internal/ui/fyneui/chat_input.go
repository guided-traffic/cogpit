package fyneui

import (
	"image/color"
	"runtime"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The chat's input (chat.go): a rounded box with the multi-line entry (Enter sends, Shift+Enter
// starts a new line) and, inside its lower edge, the chip of the view and selection the message
// attaches (its × drops it, "+" brings it back) and the send button, which is Stop while a turn
// runs (Escape in the entry and ⌘. stop too). Below the box: the conversation's connection and
// model with a lock for a private one (a click picks another), and the cluster's agent writes
// setting, read-only (a click opens Cluster Settings › Agents, where only the user changes it).
// While the user cannot send, the entry is disabled and says why.

// chatInput is the chat's input.
type chatInput struct {
	c       *chatPanel
	root    *fyne.Container
	entry   *chatEntry
	box     *surface
	chip    *chatChip
	attachB *tipButton
	sendB   *tipButton
	conn    *chatChip
	connTap *tapTarget
	writes  *chatChip
	errLine *textLine

	attach  bool   // the chip goes with the next message
	dropped string // the chip the user dropped: it comes back for another view or selection
	focused bool
	// shift reports whether Shift is held (the desktop driver's; tests replace it).
	shift func() bool
}

func newChatInput(c *chatPanel) *chatInput {
	in := &chatInput{c: c, attach: true, shift: shiftHeld}
	in.entry = newChatEntry(in)
	in.box = newSurface(theme.ColorNameInputBackground)
	in.box.radius, in.box.stroke = theme.SizeNameInputRadius, theme.ColorNameInputBorder
	in.chip = newChatChip(iconTarget, in.dropChip)
	in.attachB = newTipButton(c.sh, theme.ContentAddIcon(), "Attach the view and selection", in.restoreChip)
	in.sendB = newTipButton(c.sh, iconSend, "Send (Enter)", in.sendOrStop)
	in.sendB.Importance = widget.HighImportance
	in.conn = newChatChip(theme.ComputerIcon(), nil)
	in.connTap = newTapTarget(in.conn, in.pickConnection)
	in.writes = newChatChip(theme.DocumentCreateIcon(), nil)
	in.errLine = newTextLine("", colorStatusError, theme.SizeNameCaptionText, fyne.TextStyle{})
	in.errLine.truncate = true
	in.errLine.Hide()

	flat := container.NewThemeOverride(in.entry, &flatEntryTheme{app: c.sh.app})
	toolbar := container.NewBorder(nil, nil, container.NewHBox(in.chip, in.attachB), in.sendB)
	field := container.NewStack(in.box, inset(theme.SizeNamePadding, theme.SizeNamePadding,
		container.NewBorder(nil, toolbar, nil, nil, flat)))
	below := container.NewBorder(nil, nil, in.connTap, newTapTarget(in.writes, c.openWritesSettings))
	in.root = inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding,
		container.NewVBox(in.errLine, field, below))
	in.refresh()
	return in
}

// flatEntryTheme draws the entry without its own background and border: the input's box is them.
type flatEntryTheme struct{ app fyne.App }

func (t *flatEntryTheme) cur() fyne.Theme { return t.app.Settings().Theme() }

func (t *flatEntryTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if n == theme.ColorNameInputBackground {
		return color.Transparent
	}
	return t.cur().Color(n, v)
}
func (t *flatEntryTheme) Font(s fyne.TextStyle) fyne.Resource     { return t.cur().Font(s) }
func (t *flatEntryTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return t.cur().Icon(n) }
func (t *flatEntryTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameInputBorder {
		return 0
	}
	return t.cur().Size(n)
}

// chatEntry is the input's entry: Enter sends, Shift+Enter is a new line, Escape stops a running
// turn (else leaves the entry), ⌘. stops.
type chatEntry struct {
	widget.Entry
	in *chatInput
}

func newChatEntry(in *chatInput) *chatEntry {
	e := &chatEntry{in: in}
	e.MultiLine = true
	e.Wrapping = fyne.TextWrapWord
	e.SetMinRowsVisible(2)
	e.OnChanged = func(text string) {
		e.SetMinRowsVisible(min(max(strings.Count(text, "\n")+1, 2), 8))
		in.errLine.Hide()
		in.refresh()
	}
	e.ExtendBaseWidget(e)
	return e
}

// TypedKey implements fyne.Focusable.
func (e *chatEntry) TypedKey(ev *fyne.KeyEvent) {
	switch ev.Name {
	case fyne.KeyReturn, fyne.KeyEnter:
		if !e.in.shift() {
			e.in.submit()
			return
		}
	case fyne.KeyEscape:
		if e.in.c.running() {
			e.in.c.stop()
			return
		}
		if cv := fyne.CurrentApp().Driver().CanvasForObject(e); cv != nil {
			cv.Unfocus()
		}
		return
	}
	e.Entry.TypedKey(ev)
}

// TypedShortcut implements fyne.Shortcutable: ⌘. stops a running turn.
func (e *chatEntry) TypedShortcut(s fyne.Shortcut) {
	if cs, ok := s.(*desktop.CustomShortcut); ok && cs.KeyName == shortcutStop.KeyName && cs.Modifier == shortcutStop.Modifier {
		e.in.c.stop()
		return
	}
	e.Entry.TypedShortcut(s)
}

// FocusGained implements fyne.Focusable: the box's border shows the focus.
func (e *chatEntry) FocusGained() {
	e.Entry.FocusGained()
	e.in.focusChanged(true)
}

// FocusLost implements fyne.Focusable.
func (e *chatEntry) FocusLost() {
	e.Entry.FocusLost()
	e.in.focusChanged(false)
}

func (in *chatInput) focusChanged(on bool) {
	in.focused = on
	in.box.stroke = theme.ColorNameInputBorder
	if on {
		in.box.stroke = theme.ColorNamePrimary
	}
	in.box.Refresh()
}

// setText puts text into the entry (an example of the empty conversation) and focuses it.
func (in *chatInput) setText(s string) {
	in.entry.SetText(s)
	in.entry.CursorRow, in.entry.CursorColumn = 0, len([]rune(s))
	if cv := in.c.sh.win.Canvas(); !in.entry.Disabled() {
		cv.Focus(in.entry)
	}
}

// submit sends the entry's text; it is cleared at once and comes back if sending failed.
func (in *chatInput) submit() {
	text := strings.TrimSpace(in.entry.Text)
	if text == "" || in.c.running() {
		return
	}
	if in.c.send(text, in.attach && in.chip.text.text != "") {
		in.entry.SetText("")
	}
}

// sendOrStop is the button: Send, or Stop while a turn runs.
func (in *chatInput) sendOrStop() {
	if in.c.running() {
		in.c.stop()
		return
	}
	in.submit()
}

// failed shows why a message was not sent and gives its text back.
func (in *chatInput) failed(text string, err error) {
	if in.entry.Text == "" {
		in.entry.SetText(text)
	}
	in.errLine.SetText("Not sent: " + errorText(err))
	in.errLine.Show()
	in.root.Refresh()
}

// dropChip is the chip's ×: the next message attaches no selection.
func (in *chatInput) dropChip() {
	in.attach, in.dropped = false, in.chip.text.text
	in.refresh()
}

// restoreChip is "+": the chip comes back.
func (in *chatInput) restoreChip() {
	in.attach, in.dropped = true, ""
	in.refresh()
}

// attachLabel names the current view and selection as the chip shows them: "Pods ·
// default/web-7d9f", "Pods · 3 selected", or the view alone; "" without a view.
func (in *chatInput) attachLabel() string {
	sh := in.c.sh
	ui := sh.ui
	if ui.View == "" || ui.Welcome || ui.Context == "" {
		return ""
	}
	label := sh.kindTitle(ui.View)
	switch {
	case ui.Drawer != nil:
		label += " · " + refLabel(ui.Drawer.Namespace, ui.Drawer.Name)
	case len(ui.Selected) == 1:
		label += " · " + refLabel(ui.Selected[0].Namespace, ui.Selected[0].Name)
	case len(ui.Selected) > 1:
		label += " · " + strconv.Itoa(len(ui.Selected)) + " selected"
	}
	return label
}

func refLabel(ns, name string) string {
	if ns == "" {
		return name
	}
	return ns + "/" + name
}

// placeholder is the entry's hint while the user can send.
func placeholder() string {
	return "Ask about what you see… (" + shortcutText("Cmd+I", runtime.GOOS == goosMac) + " hides the chat)"
}

// refresh shows the input's state: enabled or why not, the chip, Send or Stop, the connection and
// the writes setting.
func (in *chatInput) refresh() {
	c := in.c
	reason := c.inputReason()
	if reason != "" {
		in.entry.SetPlaceHolder(reason)
		in.entry.Disable()
	} else {
		in.entry.SetPlaceHolder(placeholder())
		in.entry.Enable()
	}

	label := in.attachLabel()
	if label != in.dropped && in.dropped != "" { // another view or selection: the chip comes back
		in.attach, in.dropped = true, ""
	}
	in.chip.setText(label)
	in.chip.Hidden = label == "" || !in.attach
	in.attachB.Hidden = label == "" || in.attach

	switch {
	case c.running():
		in.sendB.SetIcon(themedIcon(theme.MediaStopIcon(), colorAgent)) // the agent's violet, not a warning's orange
		in.sendB.tip = "Stop (Escape, " + shortcutText("Cmd+.", runtime.GOOS == goosMac) + ")"
		in.sendB.Importance = widget.MediumImportance
		in.sendB.Enable()
	default:
		in.sendB.SetIcon(iconSend)
		in.sendB.tip = "Send (Enter)"
		in.sendB.Importance = widget.HighImportance
		if reason != "" || c.sending || strings.TrimSpace(in.entry.Text) == "" {
			in.sendB.Disable()
		} else {
			in.sendB.Enable()
		}
	}
	in.sendB.Refresh()

	if cn, ok := c.connection(); ok {
		text := cn.Name
		if cn.Model != "" {
			text += " · " + cn.Model
		}
		icon := theme.ComputerIcon()
		if cn.Private {
			icon = iconLock
		}
		in.conn.setIcon(icon)
		in.conn.setText(text)
		in.connTap.Show()
	} else {
		in.connTap.Hidden = !c.connsRead
		in.conn.setIcon(theme.ComputerIcon())
		in.conn.setText("No connection")
	}
	ctx := c.convContext()
	if ctx == "" {
		in.writes.Hide()
	} else {
		in.writes.Show()
		if c.sh.ui.ClusterSettings[ctx].AgentWrites {
			in.writes.setText("Writes allowed")
		} else {
			in.writes.setText("Writes ask")
		}
	}
	in.root.Refresh()
	c.refreshEditable() // what the input may do, a sent message's pencil may (F9)
}

// pickConnection is the connection pill's menu: another connection for the conversation, another
// model of its connection, or the connections' settings.
func (in *chatInput) pickConnection() {
	c := in.c
	cur, ok := c.connection()
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(in.connTap)
	go func() {
		defer c.sh.recoverTo("the agent chat")
		conns, err := c.agent.Connections()
		var models []string
		if ok && err == nil {
			models, _ = c.modelsFor(cur.ID)
		}
		c.sh.do(func() {
			if err != nil {
				c.sh.toast("error", "Could not read the agent's connections: "+err.Error())
				return
			}
			c.conns, c.connsRead = conns, true
			c.showMenu(in.connectionMenu(cur, conns, models), pos)
		})
	}()
}

// connectionMenu lists the connections (the conversation's checked) and the models of its own.
func (in *chatInput) connectionMenu(cur engine.ChatConnection, conns []engine.ChatConnection, models []string) *fyne.Menu {
	c := in.c
	var items []*fyne.MenuItem
	for _, cn := range connectionChoices(conns) {
		id := cn.ID
		label := cn.Name
		if cn.Model != "" {
			label += " · " + cn.Model
		}
		if cn.Private {
			label += " (private)"
		}
		it := fyne.NewMenuItem(label, func() { in.setConnection(id) })
		it.Checked = id == cur.ID
		items = append(items, it)
	}
	if len(models) > 0 && cur.ID != "" {
		sub := make([]*fyne.MenuItem, 0, len(models))
		for _, m := range models {
			model := m
			it := fyne.NewMenuItem(model, func() { in.setModel(cur, model) })
			it.Checked = model == cur.Model
			sub = append(sub, it)
		}
		mi := fyne.NewMenuItem("Model of "+cur.Name, nil)
		mi.ChildMenu = fyne.NewMenu("", sub...)
		items = append(items, fyne.NewMenuItemSeparator(), mi)
	}
	items = append(items, fyne.NewMenuItemSeparator(), fyne.NewMenuItem("Manage Connections…", c.openAgentSettings))
	return fyne.NewMenu("", items...)
}

// setConnection picks the conversation's connection: for a new conversation the one it is created
// with, else switched between turns.
func (in *chatInput) setConnection(id string) {
	c := in.c
	if c.convID == "" {
		c.draftConn = id
		in.refresh()
		return
	}
	conv := c.convID
	go func() {
		defer c.sh.recoverTo("the agent chat")
		err := c.agent.SetConversationConnection(conv, id)
		c.sh.do(func() {
			if err != nil {
				c.sh.toast("error", "Could not switch the connection: "+errorText(err))
				return
			}
			c.requestRead()
		})
	}()
}

// setModel changes the model of a connection: the user's own setting, as in Preferences › Agent.
func (in *chatInput) setModel(cn engine.ChatConnection, model string) {
	c := in.c
	cn.Model = model
	go func() {
		defer c.sh.recoverTo("the agent chat")
		_, err := c.agent.SaveConnection(cn, nil)
		c.sh.do(func() {
			if err != nil {
				c.sh.toast("error", "Could not change the model: "+errorText(err))
				return
			}
			c.requestLists()
		})
	}()
}

// setIcon changes the chip's icon.
func (c *chatChip) setIcon(res fyne.Resource) {
	if res == c.icon {
		return
	}
	c.icon = res
	if c.iconW != nil {
		c.iconW.SetResource(themedIcon(res, colorMuted))
	}
}
