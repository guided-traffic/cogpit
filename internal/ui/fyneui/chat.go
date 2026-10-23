package fyneui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The built-in agent's chat column (D-10, M12; engine/chat.go). A column at the window's right
// edge, the full height beside the navigator's work area, the drawer and the dock, shown while
// UIState.Chat is set (chat.toggle, ⌘I); its width is dragged and kept like the sidebar's and the
// drawer's (panelwidths.go). A settings page replaces the work area only: the column stays beside
// it. A window whose engine has no chat agent (Engine.Chat nil) shows none of it.
//
// The window talks to the agent through engine.ChatAgent only, never through commands: what is
// said in the chat stays in the window (the chat is no MCP tool). Every call into the agent runs
// off the main goroutine; the widgets change on it (sh.do). On EventChat for the shown
// conversation the column reads it again (one read at a time, the next after the window applied
// the last: a fast stream cannot pile reads up), parses the answers' Markdown off the main
// goroutine, and updates only the items that changed: while an answer streams in, its last block
// is laid out again and nothing else (chat_markdown.go). EventChat for another conversation is not
// read; EventChat without a conversation reloads the connections and, when it is open, the history.
//
// Design direction: the owner's (plan/captures/chat): like Claude's panel in VS Code, not a clone,
// in Cogpit's theme tokens.

// chatPanel is the chat column.
type chatPanel struct {
	sh    *shell
	agent engine.ChatAgent // nil: this window has no chat

	root      *fyne.Container
	convTitle *textLine
	agentHead *bracketLabel // "agent · <model>", in the agent's violet
	thinking  *thinkingWave // under the header while a reply is written
	historyB  *tipButton
	newB      *tipButton
	closeB    *tipButton
	list      *vstack // the conversation's items
	content   *fyne.Container
	scroll    *container.Scroll
	pin       *chatPin
	empty     *chatEmpty
	input     *chatInput

	// What is shown (main goroutine).
	convID    string                   // the shown conversation; "" a new one, created by the first message
	conv      *engine.ChatConversation // what was read of it last
	draftConn string                   // the connection a new conversation is to use; "" the default
	conns     []engine.ChatConnection
	connsRead bool
	connsErr  error
	views     []chatView
	expanded  map[string]bool // the tool rows the user opened, by call id
	width     float32
	sending   bool                  // a message is on its way to the agent
	editing   *chatUserView         // the user's message open in the editor (F9, chat_edit.go)
	loaded    bool                  // the connections and a conversation were loaded since the column showed
	stale     bool                  // the lists changed while the column was hidden
	requested bool                  // the user's ⌘I asked to show the column: the input takes the focus
	menu      *fyne.Menu            // the last popup menu shown (tests)
	history   *widget.PopUp         // the open history
	histRows  map[string]*tapTarget // the history's rows by conversation (tests)
	payload   dialog.Dialog         // the last request body shown (tests)
	// renders counts applied reads of the shown conversation; applyTimes, while measure is set,
	// how long each took on the main goroutine (tests).
	renders    int
	measure    bool
	applyTimes []time.Duration

	// The reader (any goroutine).
	shownID atomic.Value // string: the conversation EventChat is read for
	reading atomic.Bool
	dirty   atomic.Bool
	listsQ  atomic.Bool
	parseMu sync.Mutex
	parsed  map[int]parsedAnswer // the reader's parsed answers of the shown conversation, by item
}

// parsedAnswer is an answer's text and its blocks, parsed off the main goroutine.
type parsedAnswer struct {
	conv   string
	text   string
	blocks []mdBlock
}

func newChatPanel(sh *shell) *chatPanel {
	c := &chatPanel{sh: sh, agent: sh.eng.Chat(), expanded: map[string]bool{}, parsed: map[int]parsedAnswer{}}
	c.shownID.Store("")
	c.agentHead = newBracketLabel("agent", colorAgent)
	c.thinking = newThinkingWave(sh)
	c.closeB = newTipButton(sh, theme.CancelIcon(), "Hide the chat (⌘I)", func() {
		sh.dispatch("chat.toggle", chatToggleParams{Open: new(false)}, nil)
	})
	c.convTitle = newTextLine("New conversation", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
	c.convTitle.truncate = true
	c.historyB = newTipButton(sh, theme.HistoryIcon(), "Conversations about this cluster", c.showHistory)
	c.newB = newTipButton(sh, iconNewChat, "New conversation", c.newConversation)
	row1 := inset(theme.SizeNameInnerPadding, "", container.NewBorder(nil, nil, nil, c.closeB, container.NewVBox(layout.NewSpacer(), c.agentHead, layout.NewSpacer())))
	row2 := inset(theme.SizeNameInnerPadding, "", container.NewBorder(nil, nil, nil, container.NewHBox(c.historyB, c.newB),
		container.NewVBox(layout.NewSpacer(), c.convTitle, layout.NewSpacer())))
	header := container.NewVBox(row1, row2, fixedSpace(0, 4), c.thinking.w, fixedSpace(0, 4), newDivider(false))

	c.list = newVStack(theme.SizeNameInnerPadding)
	c.list.gapX = 2
	c.content = inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, c.list)
	c.scroll = container.NewVScroll(c.content)
	c.scroll.OnScrolled = func(fyne.Position) { c.updatePin() }
	c.pin = newChatPin(c)
	c.pin.Hide()
	c.empty = newChatEmpty(c)
	body := box(&chatBodyLayout{c: c}, c.scroll, c.pin, c.empty)
	c.input = newChatInput(c)
	c.root = container.NewStack(newSurface(colorDrawer),
		container.NewBorder(header, c.input.root, nil, nil, body))
	return c
}

// available reports whether the window has a chat.
func (c *chatPanel) available() bool { return c.agent != nil }

// chatToggleParams mirrors chat.toggle's parameters (commands/chat.go).
type chatToggleParams struct {
	Open *bool `json:"open,omitempty"`
}

// toggle is the user's ⌘I and View › Agent Chat: chat.toggle through the engine; the input takes
// the focus when it opens the column.
func (c *chatPanel) toggle() {
	if !c.available() {
		return
	}
	c.requested = !c.sh.ui.Chat
	c.sh.dispatch("chat.toggle", chatToggleParams{}, nil)
}

// chatBodyLayout tells the items their width before the scroll asks their heights, then stacks the
// scroll, the pinned message at the top and the empty state.
type chatBodyLayout struct{ c *chatPanel }

func (l *chatBodyLayout) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(0, 0) }

func (l *chatBodyLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	c := l.c
	pad := c.sh.app.Settings().Theme().Size(theme.SizeNameInnerPadding)
	c.setWidth(size.Width - 2*pad)
	c.scroll.Move(fyne.NewPos(0, 0))
	c.scroll.Resize(size)
	c.empty.Move(fyne.NewPos(pad, pad))
	c.empty.Resize(fyne.NewSize(max(size.Width-2*pad, 0), max(size.Height-2*pad, 0)))
	c.pin.Move(fyne.NewPos(0, 0))
	c.pin.Resize(fyne.NewSize(size.Width, c.pin.MinSize().Height))
}

// setWidth wraps the items to the column's width.
func (c *chatPanel) setWidth(w float32) {
	if w == c.width || w <= 0 {
		return
	}
	c.width = w
	c.list.setWidth(w)
	c.pin.setWidth(w)
	c.empty.setWidth(w)
}

// --- the UI state ---

// stateChanged follows the UI state: the column shown or hidden, another cluster, its connection,
// the view and selection the input's chip names, the agent writes setting. Main goroutine.
func (c *chatPanel) stateChanged(old engine.UIState, first bool) {
	if !c.available() {
		return
	}
	st := c.sh.ui
	if first || st.Chat != old.Chat {
		c.show(st.Chat)
	}
	if !first && st.Context != old.Context && st.Chat {
		c.contextChanged()
	}
	if st.Chat {
		c.input.refresh()
		c.empty.refresh()
	}
}

// show puts the column beside the work area, or takes it away.
func (c *chatPanel) show(on bool) {
	sh := c.sh
	if on {
		sh.chatRow.Objects = []fyne.CanvasObject{sh.chatSplit}
	} else {
		sh.chatRow.Objects = []fyne.CanvasObject{sh.center}
		c.thinking.set(false) // no animation runs for a column nobody sees; the next read starts it again
		c.closePopups()
		if f := sh.win.Canvas().Focused(); f == c.input.entry {
			sh.win.Canvas().Unfocus()
		}
	}
	sh.chatRow.Refresh()
	if on {
		// Loaded before, the column may show another cluster's conversation: the cluster changed
		// while it was hidden.
		if !c.loaded || c.stale || (c.conv != nil && c.sh.ui.Context != "" && c.conv.Context != c.sh.ui.Context) {
			c.load()
		}
		c.thinking.set(c.running())
		if c.requested {
			sh.win.Canvas().Focus(c.input.entry)
		}
	}
	c.requested = false
}

// load reads the connections and shows the newest conversation of the connected cluster.
func (c *chatPanel) load() {
	c.loaded, c.stale = true, false
	c.requestLists()
	if c.convID == "" || (c.conv != nil && c.conv.Context != c.sh.ui.Context && c.sh.ui.Context != "") {
		c.pickNewest(c.sh.ui.Context)
	} else {
		c.requestRead()
	}
}

// contextChanged follows another cluster: its newest conversation, or a new one. Without a
// cluster the conversation shown stays, readable, its input disabled.
func (c *chatPanel) contextChanged() {
	ctx := c.sh.ui.Context
	if ctx == "" || (c.conv != nil && c.conv.Context == ctx) {
		return
	}
	c.pickNewest(ctx)
}

// pickNewest shows the newest kept conversation of ctx, or a new one.
func (c *chatPanel) pickNewest(ctx string) {
	if ctx == "" {
		c.setConversation("")
		return
	}
	go func() {
		defer c.sh.recoverTo("the agent chat")
		list := c.agent.Conversations(ctx)
		c.sh.do(func() {
			if c.sh.ui.Context != ctx {
				return
			}
			id := ""
			if len(list) > 0 {
				id = list[0].ID
			}
			c.setConversation(id)
		})
	}()
}

// setConversation shows a conversation ("" a new one) and reads it.
func (c *chatPanel) setConversation(id string) {
	c.convID, c.conv = id, nil
	c.shownID.Store(id)
	c.parseMu.Lock()
	clear(c.parsed)
	c.parseMu.Unlock()
	c.views = nil
	c.editing = nil
	c.list.set(nil)
	c.pin.Hide()
	c.scroll.ScrollToTop()
	c.refreshChrome()
	if id != "" {
		c.requestRead()
	}
}

// newConversation starts a new conversation on the connected cluster: the column shows an empty
// one, created by the first message (an empty one is never kept).
func (c *chatPanel) newConversation() {
	if c.conv != nil {
		c.draftConn = c.conv.ConnectionID
	}
	c.setConversation("")
	c.sh.win.Canvas().Focus(c.input.entry)
}

// --- reading ---

// event takes EventChat on an engine goroutine: it must not block or touch widgets.
func (c *chatPanel) event(ev engine.Event) {
	if !c.available() {
		return
	}
	if ev.View == "" {
		c.requestLists()
		return
	}
	if id, _ := c.shownID.Load().(string); id != "" && id == ev.View {
		c.requestRead()
	}
}

// requestRead reads the shown conversation again: at once, or after the read under way.
func (c *chatPanel) requestRead() {
	c.dirty.Store(true)
	if c.reading.CompareAndSwap(false, true) {
		go c.readLoop()
	}
}

// readLoop reads the shown conversation until no change is pending. It waits for the window to
// apply each read before the next, so reads never queue up behind a busy main goroutine.
func (c *chatPanel) readLoop() {
	defer c.sh.recoverTo("the agent chat")
	for {
		for c.dirty.Swap(false) {
			id, _ := c.shownID.Load().(string)
			if id == "" {
				continue
			}
			conv, ok := c.agent.Conversation(id)
			blocks := c.parseAnswers(id, conv)
			done := make(chan struct{})
			c.sh.do(func() {
				defer close(done)
				c.applyRead(id, conv, ok, blocks)
			})
			select {
			case <-done:
			case <-c.sh.ctx.Done():
				c.reading.Store(false)
				return
			}
		}
		c.reading.Store(false)
		if !c.dirty.Load() || !c.reading.CompareAndSwap(false, true) {
			return
		}
	}
}

// parseAnswers parses the conversation's answers off the main goroutine, each again only when its
// text changed; the result is indexed like the items, nil for the others.
func (c *chatPanel) parseAnswers(id string, conv engine.ChatConversation) [][]mdBlock {
	out := make([][]mdBlock, len(conv.Items))
	c.parseMu.Lock()
	defer c.parseMu.Unlock()
	for i, it := range conv.Items {
		if it.Kind != engine.ChatItemAssistant {
			continue
		}
		if p, ok := c.parsed[i]; ok && p.conv == id && p.text == it.Text {
			out[i] = p.blocks
			continue
		}
		answer, _ := stripThinking(it.Text)
		blocks := parseChatMarkdown(answer)
		if blocks == nil {
			blocks = []mdBlock{}
		}
		c.parsed[i] = parsedAnswer{conv: id, text: it.Text, blocks: blocks}
		out[i] = blocks
	}
	return out
}

// applyRead shows what was read of a conversation, unless the window moved on to another one or
// a newer revision is shown already.
func (c *chatPanel) applyRead(id string, conv engine.ChatConversation, ok bool, blocks [][]mdBlock) {
	if id != c.convID {
		return
	}
	if !ok { // deleted elsewhere
		c.pickNewest(c.sh.ui.Context)
		return
	}
	if c.conv != nil && conv.Revision < c.conv.Revision {
		return
	}
	start := time.Now()
	c.conv = &conv
	c.renders++
	c.renderItems(conv.Items, blocks)
	c.refreshChrome()
	if c.measure {
		c.applyTimes = append(c.applyTimes, time.Since(start))
	}
}

// renderItems updates the item widgets: an unchanged item keeps its widget, a changed one is
// updated in place, and only new items get new widgets.
func (c *chatPanel) renderItems(items []engine.ChatItem, blocks [][]mdBlock) {
	bottom := c.atBottom()
	changed := len(items) != len(c.views)
	for i, it := range items {
		var b []mdBlock
		if i < len(blocks) {
			b = blocks[i]
		}
		if i < len(c.views) && c.views[i].itemKind() == it.Kind {
			c.views[i].update(it, b)
			continue
		}
		v := newChatView(c, it.Kind, i)
		v.setWidth(c.width)
		v.update(it, b)
		if i < len(c.views) {
			c.views[i] = v
		} else {
			c.views = append(c.views, v)
		}
		changed = true
	}
	if len(c.views) > len(items) {
		if c.editing != nil && c.editing.idx >= len(items) {
			c.editing = nil // its message was cut
		}
		c.views = c.views[:len(items)]
	}
	if changed {
		objs := make([]fyne.CanvasObject, len(c.views))
		for i, v := range c.views {
			objs[i] = v
		}
		c.list.objs = objs
	}
	c.itemsResized()
	if bottom {
		c.scroll.ScrollToBottom()
		c.updatePin()
	}
}

// itemsResized lays the items out again after one of them changed its height.
func (c *chatPanel) itemsResized() {
	c.list.Refresh()
	c.scroll.Refresh()
	c.updatePin()
}

// atBottom reports whether the conversation is scrolled to its end (or nearly), so that a growing
// answer keeps it there.
func (c *chatPanel) atBottom() bool {
	h := c.content.MinSize().Height - c.scroll.Size().Height
	return h <= 0 || c.scroll.Offset.Y >= h-24
}

// refreshChrome shows the title and the input's state.
func (c *chatPanel) refreshChrome() {
	title := "New conversation"
	if c.conv != nil && c.conv.Title != "" {
		title = c.conv.Title
	}
	c.convTitle.SetText(title)
	cn, ok := c.connection()
	c.agentHead.SetText(agentHeadText(cn.Name, cn.Model, ok))
	c.thinking.set(c.running())
	c.empty.refresh()
	c.input.refresh()
}

// requestLists reads the connections again (off the main goroutine), and the history if it is open.
func (c *chatPanel) requestLists() {
	if !c.listsQ.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer c.sh.recoverTo("the agent chat")
		c.listsQ.Store(false)
		conns, err := c.agent.Connections()
		c.sh.do(func() {
			if !c.sh.ui.Chat && c.loaded {
				c.stale = true // read again when the column shows
			}
			c.conns, c.connsErr, c.connsRead = conns, err, true
			c.input.refresh()
			c.empty.refresh()
			c.sh.settings.chatConnectionsChanged() // Preferences › Agent, when open
			if c.history != nil && c.history.Visible() {
				c.showHistory()
			}
		})
	}()
}

// --- what the input asks ---

// running reports whether a turn of the shown conversation runs.
func (c *chatPanel) running() bool { return c.conv != nil && c.conv.Running }

// convContext is the context the shown conversation is bound to, the connected one for a new one.
func (c *chatPanel) convContext() string {
	if c.conv != nil && c.conv.Context != "" {
		return c.conv.Context
	}
	return c.sh.ui.Context
}

// inputReason is why the input is disabled; "" when the user can send.
func (c *chatPanel) inputReason() string {
	ui := c.sh.ui
	ctx := c.convContext()
	switch {
	case c.connsRead && c.connsErr != nil:
		return "The agent's connections could not be read: " + c.connsErr.Error()
	case c.connsRead && len(c.conns) == 0:
		return "Add an LLM connection in Preferences › Agent to chat."
	case ui.Context == "":
		return "Connect a cluster to chat about it."
	case ctx != ui.Context:
		return "This conversation is about " + c.sh.identity(ctx).name + "; start a new one for " + c.sh.identity(ui.Context).name + "."
	case ui.Connection.State == engine.Connecting:
		return "Connecting to " + c.sh.identity(ui.Context).name + "…"
	case !ui.Connection.State.Live():
		return "Connect " + c.sh.identity(ui.Context).name + " to continue this conversation."
	}
	return ""
}

// connection is the connection the shown conversation uses (a new one: the one picked, else the
// default); false when there is none.
func (c *chatPanel) connection() (engine.ChatConnection, bool) {
	id := c.draftConn
	if c.conv != nil {
		id = c.conv.ConnectionID
	}
	for _, cn := range c.conns {
		if cn.ID == id {
			return cn, true
		}
	}
	for _, cn := range c.conns {
		if cn.Default {
			return cn, true
		}
	}
	if len(c.conns) > 0 {
		return c.conns[0], true
	}
	return engine.ChatConnection{}, false
}

// send adds the user's message: to the shown conversation, or to a new one it creates first.
// Returns false when it cannot be sent now.
func (c *chatPanel) send(text string, attach bool) bool {
	if strings.TrimSpace(text) == "" || c.sending || c.running() || c.inputReason() != "" {
		return false
	}
	id, ctx, conn := c.convID, c.sh.ui.Context, c.draftConn
	c.sending = true
	c.input.refresh()
	go func() {
		defer c.sh.recoverTo("the agent chat")
		var err error
		if id == "" {
			conv, e := c.agent.NewConversation(ctx, conn)
			if err = e; err == nil {
				id = conv.ID
			}
		}
		if err == nil {
			err = c.agent.Send(id, engine.ChatUserMessage{Text: text, AttachSelection: attach})
		}
		c.sh.do(func() {
			c.sending = false
			if id != "" && id != c.convID {
				c.setConversation(id)
			} else if id != "" {
				c.requestRead()
			}
			if err != nil {
				c.input.failed(text, err)
			}
			c.input.refresh()
			c.scroll.ScrollToBottom()
		})
	}()
	return true
}

// stop is the kill switch: the Stop button, ⌘. and Escape in the input. It returns at once; Stop
// runs off the main goroutine until the turn has ended.
func (c *chatPanel) stop() {
	id := c.convID
	if id == "" || !c.available() {
		return
	}
	go func() {
		defer c.sh.recoverTo("the agent chat")
		c.agent.Stop(id)
	}()
}

// stopIfRunning is ⌘. from anywhere in the window, the column shown or not: it stops the shown
// conversation's turn.
func (c *chatPanel) stopIfRunning() {
	if c.running() {
		c.stop()
	}
}

// openAgentSettings opens Preferences › Agent (the connections).
func (c *chatPanel) openAgentSettings() {
	c.closePopups()
	c.sh.dispatch("preferences.open", preferencesOpenParams{Section: engine.SectionAgent}, nil)
}

// openWritesSettings opens Cluster Settings › Agents of the conversation's cluster, where only the
// user allows agents' changes.
func (c *chatPanel) openWritesSettings() {
	ctx := c.convContext()
	if ctx == "" {
		return
	}
	c.sh.dispatch("cluster.settings.open", clusterSettingsOpenParams{Context: ctx, Section: engine.SectionAgents}, nil)
}

// --- popups ---

func (c *chatPanel) closePopups() {
	if c.history != nil {
		c.history.Hide()
		c.history = nil
	}
}

// showMenu pops a menu up at pos (window coordinates), upwards when it would reach into the status
// bar, as the hotbar's menus do.
func (c *chatPanel) showMenu(m *fyne.Menu, pos fyne.Position) {
	c.sh.tip.hide()
	c.menu = m
	pm := widget.NewPopUpMenu(m, c.sh.win.Canvas())
	room := c.sh.win.Canvas().Size().Height - c.sh.app.Settings().Theme().Size(sizeStatusHeight)
	if ms := pm.MinSize(); pos.Y+ms.Height > room && pos.Y > ms.Height {
		pos.Y -= ms.Height
	}
	pm.ShowAtPosition(pos)
}

// historyNotSaved marks a private conversation in the history: it lives in memory only (H-96).
const historyNotSaved = "not saved"

// showHistory lists the conversations of the cluster, the kept ones and the private ones of this
// run: open one, delete one, or clear them.
func (c *chatPanel) showHistory() {
	ctx := c.sh.ui.Context
	if ctx == "" && c.conv != nil {
		ctx = c.conv.Context
	}
	if ctx == "" {
		c.sh.toast("info", "Connect a cluster to see its conversations.")
		return
	}
	go func() {
		defer c.sh.recoverTo("the agent chat")
		list := c.agent.Conversations(ctx)
		c.sh.do(func() { c.showHistoryList(ctx, list) })
	}()
}

// showHistoryList shows the history popup under the history button.
func (c *chatPanel) showHistoryList(ctx string, list []engine.ChatConversationInfo) {
	c.closePopups()
	th := c.sh.app.Settings().Theme()
	rows := []fyne.CanvasObject{heading("Conversations about " + c.sh.identity(ctx).name)}
	if len(list) == 0 {
		rows = append(rows, newTextLine("No conversations kept yet.", colorMuted, theme.SizeNameText, fyne.TextStyle{}))
	}
	c.histRows = map[string]*tapTarget{}
	for _, info := range list {
		rows = append(rows, c.historyRow(info))
	}
	if len(list) > 0 {
		clearB := newTextButton("Clear all", theme.DeleteIcon(), func() { c.confirmClear(ctx, len(list)) })
		rows = append(rows, newDivider(false), container.NewHBox(clearB))
	}
	width := max(c.width, 260)
	content := container.NewVScroll(container.NewVBox(rows...))
	content.SetMinSize(fyne.NewSize(width, min(float32(len(rows))*32+16, th.Size(sizePopupMaxList))))
	c.history = widget.NewPopUp(content, c.sh.win.Canvas())
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(c.historyB)
	at := fyne.NewPos(pos.X+c.historyB.Size().Width-width-th.Size(theme.SizeNameInnerPadding), pos.Y+c.historyB.Size().Height)
	h := c.history
	onShown(c.sh.win.Canvas(), func() { h.ShowAtPosition(at) }, h.Hide) // Escape closes it (escape.go)
}

// historyRow is one conversation of the history: its title and age (a running turn says so, and a
// private conversation that it is not saved, H-96), the shown one bold; a click opens it, ×
// deletes it.
func (c *chatPanel) historyRow(info engine.ChatConversationInfo) fyne.CanvasObject {
	title := info.Title
	if title == "" {
		title = "Untitled conversation"
	}
	t := newTextLine(title, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: info.ID == c.convID})
	t.truncate = true
	when := formatAgo(c.sh.now().Sub(info.Updated))
	if info.Private {
		when = historyNotSaved + " · " + when
	}
	if info.Running {
		when = "running · " + when
	}
	age := newTextLine(when, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	id := info.ID
	del := newTipButton(c.sh, theme.DeleteIcon(), "Delete this conversation", func() { c.deleteConversation(id) })
	open := newTapTarget(container.NewBorder(nil, nil, nil, age, t), func() {
		c.closePopups()
		if id != c.convID {
			c.setConversation(id)
		}
	})
	c.histRows[id] = open
	return container.NewBorder(nil, nil, nil, del, open)
}

// deleteConversation deletes one conversation (no question: it is one of the history's rows).
func (c *chatPanel) deleteConversation(id string) {
	go func() {
		defer c.sh.recoverTo("the agent chat")
		err := c.agent.DeleteConversation(id)
		c.sh.do(func() {
			if err != nil {
				c.sh.toast("error", "Could not delete the conversation: "+err.Error())
				return
			}
			if id == c.convID {
				c.setConversation("")
			}
			c.showHistory()
		})
	}()
}

// confirmClear asks before deleting every conversation of a cluster.
func (c *chatPanel) confirmClear(ctx string, n int) {
	c.closePopups()
	name := c.sh.identity(ctx).name
	d := dialog.NewConfirm("Delete "+pluralOf(n, "conversation")+"?",
		"Every conversation about "+name+" is deleted. This cannot be undone.", func(ok bool) {
			if !ok {
				return
			}
			go func() {
				defer c.sh.recoverTo("the agent chat")
				err := c.agent.ClearConversations(ctx)
				c.sh.do(func() {
					if err != nil {
						c.sh.toast("error", "Could not delete the conversations: "+err.Error())
						return
					}
					if c.convContext() == ctx {
						c.setConversation("")
					}
				})
			}()
		}, c.sh.win)
	d.SetConfirmText("Delete")
	c.sh.showDialog(d)
}

// showPayload shows the exact request body the turn of a user's message sent, read-only (H-13:
// "what is sent is visible"). A dialog, not a dock tab: a dock tab is UI state agents read, and
// what the chat sends stays in the window.
func (c *chatPanel) showPayload(idx int) {
	id := c.convID
	if id == "" {
		return
	}
	var endpoint string
	private := false
	if c.conv != nil && idx < len(c.conv.Items) && c.conv.Items[idx].Sent != nil {
		endpoint, private = c.conv.Items[idx].Sent.Endpoint, c.conv.Items[idx].Sent.Private
	}
	go func() {
		defer c.sh.recoverTo("the agent chat")
		body, ok := c.agent.SentPayload(id, idx)
		c.sh.do(func() {
			if !ok {
				c.sh.toast("info", "Nothing was sent for this message yet.")
				return
			}
			c.payloadDialog(body, endpoint, private)
		})
	}()
}

// payloadMaxLines is the most lines of a request body the dialog draws; Copy has all of them.
const payloadMaxLines = 2000

// payloadDialog is the request body, pretty-printed, read-only, with Copy.
func (c *chatPanel) payloadDialog(body, endpoint string, private bool) {
	text := prettyJSON(body)
	lines := strings.Split(text, "\n")
	shown := text
	note := model.FormatBytes(int64(len(body))) + ", as sent"
	if endpoint != "" {
		note += " to " + endpoint
	}
	if private {
		note += " · a private connection: what the window shows may be in it, Secret values included"
	}
	if len(lines) > payloadMaxLines {
		shown = strings.Join(lines[:payloadMaxLines], "\n")
		note += fmt.Sprintf(" · %s more lines not drawn: Copy has them", formatCount(int64(len(lines)-payloadMaxLines)))
	}
	code := newMDCode(shown)
	code.selectable(c.copySelection)
	win := c.sh.win.Canvas().Size()
	w := max(win.Width*0.6, 480)
	code.setWidth(w - 24)
	scroll := container.NewScroll(code)
	scroll.SetMinSize(fyne.NewSize(w, max(win.Height*0.6, 300)))
	info := newParagraph(note, colorMuted, w)
	copyB := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() { c.sh.app.Clipboard().SetContent(body) })
	d := dialog.NewCustom("What this turn sent", "Close", container.NewBorder(info, container.NewHBox(copyB), nil, nil, scroll), c.sh.win)
	c.payload = d
	c.sh.showDialog(d)
}

// --- the pinned message ---

// updatePin pins the user's message whose answer is being read: the last one whose top is
// scrolled out of view stays at the top of the column, its answer scrolling under it.
func (c *chatPanel) updatePin() {
	off := c.scroll.Offset.Y
	pad := c.sh.app.Settings().Theme().Size(theme.SizeNameInnerPadding)
	var pinned *chatUserView
	for _, v := range c.views {
		u, ok := v.(*chatUserView)
		if !ok {
			continue
		}
		if pad+u.Position().Y < off-1 {
			pinned = u
			continue
		}
		break
	}
	if pinned == nil {
		c.pin.Hide()
		return
	}
	c.pin.set(pinned)
	c.pin.Show()
	c.pin.Refresh()
}

// chatPin is the pinned copy of a user's message: its chip and up to two lines of its text; a
// click scrolls back to the message.
type chatPin struct {
	widget.BaseWidget
	c      *chatPanel
	target *chatUserView
	card   *surface
	chip   *chatChip
	text   *mdFlow
	width  float32
}

var _ fyne.Tappable = (*chatPin)(nil)

func newChatPin(c *chatPanel) *chatPin {
	p := &chatPin{c: c}
	p.card = newSurface(colorDrawerHeader)
	p.card.radius, p.card.stroke = theme.SizeNameInputRadius, colorHairline
	p.chip = newChatChip(iconTarget, nil)
	p.text = newMDFlow(nil, theme.ColorNameForeground, nil)
	p.text.maxLines = 2
	p.ExtendBaseWidget(p)
	return p
}

func (p *chatPin) set(u *chatUserView) {
	if p.target == u && p.text.runs != nil && runsText(p.text.runs) == u.item.Text && p.chip.text.text == u.item.Attached {
		return
	}
	p.target = u
	p.chip.setText(u.item.Attached)
	p.text.setRuns([]mdRun{{text: strings.TrimSpace(u.item.Text)}})
}

func (p *chatPin) pad() float32 { return p.Theme().Size(theme.SizeNameInnerPadding) }

func (p *chatPin) setWidth(w float32) {
	p.width = w
	p.text.setWidth(w - 2*p.pad()) // w is the items' width: the card's, inside the column's padding
}

// Tapped implements fyne.Tappable: back to the message.
func (p *chatPin) Tapped(*fyne.PointEvent) {
	if p.target != nil {
		p.c.scroll.ScrollToOffset(fyne.NewPos(0, p.target.Position().Y))
		p.c.updatePin()
	}
}

// cardHeight is the pinned card's height; the band around it adds the padding above and below.
func (p *chatPin) cardHeight() float32 {
	h := 2*p.pad() + p.text.MinSize().Height
	if p.target != nil && p.target.item.Attached != "" {
		h += p.chip.MinSize().Height + p.pad()
	}
	return h
}

// MinSize implements fyne.CanvasObject: the card on a band of the column's background, which the
// conversation scrolls under.
func (p *chatPin) MinSize() fyne.Size {
	p.ExtendBaseWidget(p)
	return fyne.NewSize(0, p.cardHeight()+2*p.pad())
}

// CreateRenderer implements fyne.Widget.
func (p *chatPin) CreateRenderer() fyne.WidgetRenderer {
	return &chatPinRenderer{p: p, band: newSurface(colorDrawer), edge: newDivider(false)}
}

type chatPinRenderer struct {
	p    *chatPin
	band *surface
	edge *surface
}

func (r *chatPinRenderer) Layout(size fyne.Size) {
	p := r.p
	pad := p.pad()
	r.band.Resize(size)
	r.edge.Move(fyne.NewPos(0, size.Height-1))
	r.edge.Resize(fyne.NewSize(size.Width, 1))
	cw := size.Width - 2*pad
	p.card.Move(fyne.NewPos(pad, pad))
	p.card.Resize(fyne.NewSize(cw, p.cardHeight()))
	y := 2 * pad
	if p.target != nil && p.target.item.Attached != "" {
		p.chip.Show()
		cs := p.chip.MinSize()
		p.chip.Move(fyne.NewPos(2*pad, y))
		p.chip.Resize(fyne.NewSize(min(cs.Width, cw-2*pad), cs.Height))
		y += cs.Height + pad
	} else {
		p.chip.Hide()
	}
	p.text.Move(fyne.NewPos(2*pad, y))
	p.text.Resize(fyne.NewSize(cw-2*pad, p.text.MinSize().Height))
}

func (r *chatPinRenderer) MinSize() fyne.Size { return r.p.MinSize() }
func (r *chatPinRenderer) Refresh() {
	r.Layout(r.p.Size())
	r.band.Refresh()
	r.edge.Refresh()
	r.p.card.Refresh()
	r.p.text.Refresh()
}
func (r *chatPinRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.band, r.edge, r.p.card, r.p.chip, r.p.text}
}
func (r *chatPinRenderer) Destroy() {}

// --- the empty column ---

// chatEmpty is what the column shows without items: how to connect an LLM when there is no
// connection, else what to ask, with examples a click puts into the input.
type chatEmpty struct {
	*vstack
	c       *chatPanel
	title   *textLine
	text    *mdFlow
	addConn *widget.Button
	samples []fyne.CanvasObject
	width   float32
}

// chatSamples are the empty conversation's examples.
var chatSamples = []string{
	"Why is the selected pod not ready?",
	"Show me the Service with ClusterIP 10.96.0.10",
	"Which deployments in this namespace have fewer ready replicas than desired?",
}

func newChatEmpty(c *chatPanel) *chatEmpty {
	e := &chatEmpty{c: c}
	e.title = newTextLine("", theme.ColorNameForeground, theme.SizeNameSubHeadingText, fyne.TextStyle{Bold: true})
	e.text = newMDFlow(nil, colorMuted, nil)
	e.addConn = widget.NewButtonWithIcon("Add a connection", theme.ContentAddIcon(), c.openAgentSettings)
	e.addConn.Importance = widget.HighImportance
	objs := make([]fyne.CanvasObject, 0, 4+len(chatSamples))
	objs = append(objs, newSpace(theme.SizeNameInnerPadding), e.title, e.text, container.NewHBox(e.addConn))
	for _, s := range chatSamples {
		sample := s
		f := newMDFlow([]mdRun{{text: "“" + s + "”"}}, colorLink, nil)
		t := newTapTarget(f, func() { c.input.setText(sample) })
		e.samples = append(e.samples, t)
		objs = append(objs, t)
	}
	e.vstack = makeVStack(theme.SizeNameInnerPadding, objs...)
	e.extend(e)
	return e
}

func (e *chatEmpty) setWidth(w float32) {
	e.width = w
	e.text.setWidth(w)
	for _, s := range e.samples {
		s.(*tapTarget).content.(*mdFlow).setWidth(w)
	}
}

// refresh shows the state: no connection, an empty conversation, or nothing (items shown).
func (e *chatEmpty) refresh() {
	c := e.c
	noConn := c.connsRead && len(c.conns) == 0 && c.connsErr == nil
	if !noConn && len(c.views) > 0 {
		e.Hide()
		return
	}
	switch {
	case noConn:
		e.title.SetText("Connect an LLM")
		e.text.setRuns([]mdRun{{text: "Cogpit's agent talks to an OpenAI-compatible endpoint: LM Studio on this computer, " +
			"LiteLLM, Open WebUI or any compatible server. It reads what you see and operates Cogpit for you; " +
			"every change it asks for waits for your confirmation."}})
		e.addConn.Show()
		for _, s := range e.samples {
			s.Hide()
		}
	case !c.loaded || c.convID != "":
		e.Hide() // a conversation still loading
		return
	default:
		e.title.SetText("Ask about what you see")
		e.text.setRuns([]mdRun{{text: "The agent reads the view, the selection and the drawer, navigates for you and " +
			"runs actions. Reads run at once; a change waits for you unless you allowed it in "},
			{text: "Cluster Settings › Agents", bold: true}, {text: ". For example:"}})
		e.addConn.Hide()
		for _, s := range e.samples {
			s.Show()
		}
	}
	e.Show()
	e.Refresh()
}

// --- helpers ---

// formatAgo is an age with "ago", as the lists write ages ("5m ago").
func formatAgo(d time.Duration) string { return model.FormatAge(d) + " ago" }

// chatTestTimeout bounds a connection's model list and test from the window.
const chatTestTimeout = 30 * time.Second

// modelsFor lists a saved connection's models, bounded in time; off the main goroutine.
func (c *chatPanel) modelsFor(id string) ([]string, error) {
	ctx, cancel := context.WithTimeout(c.sh.ctx, chatTestTimeout)
	defer cancel()
	return c.agent.Models(ctx, id)
}

// shortcutStop is ⌘. (Ctrl+. elsewhere): the chat's kill switch, from any focus.
var shortcutStop = &desktop.CustomShortcut{KeyName: fyne.KeyPeriod, Modifier: fyne.KeyModifierShortcutDefault}

// chatMenuItems are the View menu's Agent Chat (⌘I) and Stop Agent (⌘.); none without a chat.
func (sh *shell) chatMenuItems() []*fyne.MenuItem {
	if !sh.chat.available() {
		return nil
	}
	cmd, ok := sh.eng.Registry().Get("chat.toggle")
	if !ok {
		return nil
	}
	toggle := sh.withShortcut(fyne.NewMenuItem(cmd.Label, sh.chat.toggle), cmd.ID)
	toggle.Checked = sh.ui.Chat
	stop := fyne.NewMenuItem("Stop Agent", sh.chat.stopIfRunning)
	stop.Shortcut = shortcutStop
	return []*fyne.MenuItem{toggle, stop}
}

// connectionChoices sorts connections by name, the default first.
func connectionChoices(conns []engine.ChatConnection) []engine.ChatConnection {
	out := slices.Clone(conns)
	slices.SortStableFunc(out, func(a, b engine.ChatConnection) int {
		if a.Default != b.Default {
			if a.Default {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
}
