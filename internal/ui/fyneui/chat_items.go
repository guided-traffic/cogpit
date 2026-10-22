package fyneui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The agent chat's items (chat.go): the user's message as a rounded card with what it attached
// and, under it, what its turn sent; the answer as Markdown on the column's background; a tool call
// as a compact row that opens to its arguments and its result as the model saw it; Cogpit's notes
// in their level's colour. Each item's widget is kept while its item does not change, and updated
// in place while it streams (an answer) or changes state (a tool call).

// chatView is one item's widget.
type chatView interface {
	fyne.CanvasObject
	widthAware
	// update shows the item, which has the view's kind; blocks are an answer's parsed Markdown.
	update(it engine.ChatItem, blocks []mdBlock)
	itemKind() engine.ChatItemKind
}

// sameItem reports whether two items draw the same.
func sameItem(a, b engine.ChatItem) bool {
	if a.Kind != b.Kind || a.Text != b.Text || a.Attached != b.Attached || a.Streaming != b.Streaming || a.NoteLevel != b.NoteLevel {
		return false
	}
	if (a.Tool == nil) != (b.Tool == nil) || (a.Tool != nil && *a.Tool != *b.Tool) {
		return false
	}
	return (a.Sent == nil) == (b.Sent == nil) && (a.Sent == nil || *a.Sent == *b.Sent)
}

// Icons drawn for Cogpit: a closed padlock (a private connection), a chip's target (what the
// message attached), a speech bubble with a plus (a new conversation).
var (
	iconLock = theme.NewThemedResource(fyne.NewStaticResource("cogpit-lock.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
			`<path d="M12 2a5 5 0 0 0-5 5v3H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-8a2 2 0 0 0-2-2h-1V7a5 5 0 0 0-5-5zm-3 5a3 3 0 0 1 6 0v3H9V7zm3 7a2 2 0 0 1 1 3.7V19h-2v-1.3A2 2 0 0 1 12 14z"/></svg>`)))
	iconTarget = theme.NewThemedResource(fyne.NewStaticResource("cogpit-target.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
			`<path d="M4 5h16v2H4zm0 6h16v2H4zm0 6h10v2H4z"/></svg>`)))
	iconNewChat = theme.NewThemedResource(fyne.NewStaticResource("cogpit-new-chat.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
			`<path d="M4 3h16a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H8l-4 4V5a2 2 0 0 1 2-2zm0 2v12.2L7.2 16H20V5zm7 2h2v3h3v2h-3v3h-2v-3H8v-2h3z"/></svg>`)))
	iconSend = theme.NewThemedResource(fyne.NewStaticResource("cogpit-send.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
			`<path d="M11 20V7.8l-5.6 5.6L4 12l8-8 8 8-1.4 1.4L13 7.8V20z"/></svg>`)))
)

// chatChip is a small rounded chip: an icon and a text cut to the width, with an optional ×.
type chatChip struct {
	widget.BaseWidget
	icon   fyne.Resource
	iconW  *widget.Icon
	text   *textLine
	onDrop func()         // the ×; nil: none
	dropB  *widget.Button // the ×
	maxW   float32
}

func newChatChip(icon fyne.Resource, onDrop func()) *chatChip {
	c := &chatChip{icon: icon, onDrop: onDrop, maxW: 260, iconW: widget.NewIcon(themedIcon(icon, colorMuted))}
	c.text = newTextLine("", theme.ColorNameForeground, theme.SizeNameCaptionText, fyne.TextStyle{})
	c.text.truncate = true
	if onDrop != nil {
		c.dropB = widget.NewButtonWithIcon("", themedIcon(theme.CancelIcon(), colorMuted), onDrop)
		c.dropB.Importance = widget.LowImportance
	}
	c.ExtendBaseWidget(c)
	return c
}

func (c *chatChip) setText(s string) { c.text.SetText(s); c.Refresh() }

// dropButton is the chip's × (tests).
func (c *chatChip) dropButton() *widget.Button { return c.dropB }

// CreateRenderer implements fyne.Widget.
func (c *chatChip) CreateRenderer() fyne.WidgetRenderer {
	bg := newSurface(colorChip)
	bg.radius = sizeChipRadius
	icon := c.iconW
	objs := []fyne.CanvasObject{bg, icon, c.text}
	drop := c.dropB
	if drop != nil {
		objs = append(objs, drop)
	}
	return &chatChipRenderer{c: c, bg: bg, icon: icon, drop: drop, objs: objs}
}

type chatChipRenderer struct {
	c    *chatChip
	bg   *surface
	icon *widget.Icon
	drop *widget.Button
	objs []fyne.CanvasObject
}

func (r *chatChipRenderer) sizes() (h, iconS, pad float32) {
	th := r.c.Theme()
	iconS = th.Size(theme.SizeNameCaptionText) + 2
	pad = th.Size(theme.SizeNamePadding)
	return r.c.text.MinSize().Height + 2*pad, iconS, pad
}

func (r *chatChipRenderer) textWidth() float32 {
	th := r.c.Theme()
	return fyne.MeasureText(r.c.text.text, th.Size(theme.SizeNameCaptionText), fyne.TextStyle{}).Width + 2
}

func (r *chatChipRenderer) MinSize() fyne.Size {
	h, iconS, pad := r.sizes()
	w := pad*2 + iconS + pad + min(r.textWidth(), r.c.maxW)
	if r.drop != nil {
		w += h
	}
	return fyne.NewSize(w, h)
}

func (r *chatChipRenderer) Layout(size fyne.Size) {
	h, iconS, pad := r.sizes()
	r.bg.Resize(fyne.NewSize(size.Width, h))
	r.icon.Move(fyne.NewPos(pad*1.5, (h-iconS)/2))
	r.icon.Resize(fyne.NewSize(iconS, iconS))
	tx := pad*1.5 + iconS + pad
	tw := size.Width - tx - pad
	if r.drop != nil {
		tw -= h - pad
		r.drop.Move(fyne.NewPos(size.Width-h, 0))
		r.drop.Resize(fyne.NewSize(h, h))
	}
	r.c.text.Move(fyne.NewPos(tx, 0))
	r.c.text.Resize(fyne.NewSize(max(tw, 0), h))
}

func (r *chatChipRenderer) Refresh() {
	r.Layout(r.c.Size())
	r.bg.Refresh()
	r.c.text.Refresh()
}

func (r *chatChipRenderer) Objects() []fyne.CanvasObject { return r.objs }
func (r *chatChipRenderer) Destroy()                     {}

// --- the user's message ---

// chatUserView is the user's message: a rounded card, a chip with what it attached above the text,
// and under the card the line of what its turn sent, which opens the request body, and the pencil
// that opens the message in an editor to send it again (F9, chat_edit.go).
type chatUserView struct {
	widget.BaseWidget
	c      *chatPanel
	idx    int
	item   engine.ChatItem
	card   *surface
	chip   *chatChip
	text   *mdText
	sent   *chatSentLine
	editB  *chatIconTap
	editor *chatEditor // the open editor; nil: the message is shown
	width  float32
}

func newChatUserView(c *chatPanel, idx int) *chatUserView {
	v := &chatUserView{c: c, idx: idx}
	v.card = newSurface(colorDrawerHeader)
	v.card.radius, v.card.stroke = theme.SizeNameInputRadius, colorHairline
	v.chip = newChatChip(iconTarget, nil)
	v.text = newMDText(theme.ColorNameForeground, c.mdHooks())
	v.sent = newChatSentLine(func() { c.showPayload(v.idx) })
	v.editB = newChatIconTap(c.sh, theme.DocumentCreateIcon(), editTip, func() { v.startEdit() })
	v.editB.reason = c.editReason()
	v.ExtendBaseWidget(v)
	return v
}

func (v *chatUserView) itemKind() engine.ChatItemKind { return engine.ChatItemUser }

func (v *chatUserView) update(it engine.ChatItem, _ []mdBlock) {
	if v.item.Kind != "" && sameItem(v.item, it) {
		return
	}
	v.item = it
	v.text.setBlocks(plainBlocks(it.Text))
	v.chip.setText(it.Attached)
	v.sent.set(it.Sent)
	v.Refresh()
}

func (v *chatUserView) pad() float32 { return v.Theme().Size(theme.SizeNameInnerPadding) }

func (v *chatUserView) setWidth(w float32) {
	v.width = w
	v.text.setWidth(w - 2*v.pad())
	v.sizeEditor()
}

func (v *chatUserView) cardHeight() float32 {
	pad := v.pad()
	if v.editor != nil {
		return 2*pad + v.editor.field.MinSize().Height
	}
	h := 2*pad + v.text.MinSize().Height
	if v.item.Attached != "" {
		h += v.chip.MinSize().Height + pad
	}
	return h
}

// footerHeight is the row under the card: what was sent and the pencil, or the editor's buttons
// with why the last Send failed.
func (v *chatUserView) footerHeight() float32 {
	if e := v.editor; e != nil {
		return max(e.sendB.MinSize().Height, e.cancelB.MinSize().Height)
	}
	return max(v.sent.MinSize().Height, v.editB.MinSize().Height)
}

// MinSize implements fyne.CanvasObject.
func (v *chatUserView) MinSize() fyne.Size {
	v.ExtendBaseWidget(v)
	return fyne.NewSize(0, v.cardHeight()+v.Theme().Size(theme.SizeNamePadding)+v.footerHeight())
}

// CreateRenderer implements fyne.Widget.
func (v *chatUserView) CreateRenderer() fyne.WidgetRenderer {
	return &chatUserRenderer{v: v, objs: []fyne.CanvasObject{v.card, v.chip, v.text, v.sent, v.editB}}
}

type chatUserRenderer struct {
	v    *chatUserView
	objs []fyne.CanvasObject
}

func (r *chatUserRenderer) Layout(size fyne.Size) {
	v := r.v
	pad := v.pad()
	ch := v.cardHeight()
	v.card.Move(fyne.NewPos(0, 0))
	v.card.Resize(fyne.NewSize(size.Width, ch))
	v.card.fill, v.card.stroke = colorDrawerHeader, colorHairline
	if v.editor != nil {
		r.layoutEditor(size, ch)
		return
	}
	y := pad
	if v.item.Attached != "" {
		v.chip.Show()
		cs := v.chip.MinSize()
		v.chip.maxW = size.Width - 2*pad - cs.Height*2
		cs = v.chip.MinSize()
		v.chip.Move(fyne.NewPos(pad, y))
		v.chip.Resize(fyne.NewSize(min(cs.Width, size.Width-2*pad), cs.Height))
		y += cs.Height + pad
	} else {
		v.chip.Hide()
	}
	v.text.Show()
	v.text.Move(fyne.NewPos(pad, y))
	v.text.Resize(fyne.NewSize(size.Width-2*pad, v.text.MinSize().Height))
	fy, fh := ch+v.Theme().Size(theme.SizeNamePadding), v.footerHeight()
	es := v.editB.MinSize()
	v.editB.Show()
	v.editB.Move(fyne.NewPos(size.Width-es.Width, fy+(fh-es.Height)/2))
	v.editB.Resize(es)
	if v.item.Sent != nil {
		v.sent.Show()
		v.sent.Move(fyne.NewPos(0, fy+(fh-v.sent.MinSize().Height)/2))
		v.sent.Resize(fyne.NewSize(max(size.Width-es.Width-pad, 0), v.sent.MinSize().Height))
	} else {
		v.sent.Hide()
	}
}

// layoutEditor lays out an open editor: the entry in the card, outlined as the focused input is,
// and under it why the last Send failed, Cancel and Send.
func (r *chatUserRenderer) layoutEditor(size fyne.Size, ch float32) {
	v, e := r.v, r.v.editor
	pad := v.pad()
	v.card.fill, v.card.stroke = theme.ColorNameInputBackground, theme.ColorNamePrimary
	v.chip.Hide()
	v.text.Hide()
	v.sent.Hide()
	v.editB.Hide()
	e.field.Move(fyne.NewPos(pad, pad))
	e.field.Resize(fyne.NewSize(size.Width-2*pad, e.field.MinSize().Height))
	fy, fh := ch+v.Theme().Size(theme.SizeNamePadding), v.footerHeight()
	ss, cs := e.sendB.MinSize(), e.cancelB.MinSize()
	e.sendB.Move(fyne.NewPos(size.Width-ss.Width, fy))
	e.sendB.Resize(fyne.NewSize(ss.Width, fh))
	e.cancelB.Move(fyne.NewPos(size.Width-ss.Width-pad-cs.Width, fy))
	e.cancelB.Resize(fyne.NewSize(cs.Width, fh))
	lh := e.errLine.MinSize().Height
	e.errLine.Move(fyne.NewPos(0, fy+(fh-lh)/2))
	e.errLine.Resize(fyne.NewSize(max(size.Width-ss.Width-cs.Width-2*pad, 0), lh))
}

func (r *chatUserRenderer) MinSize() fyne.Size { return r.v.MinSize() }
func (r *chatUserRenderer) Refresh() {
	r.Layout(r.v.Size())
	r.v.card.Refresh()
	canvas.Refresh(r.v)
}

// Objects implements fyne.WidgetRenderer: the message's, and an open editor's.
func (r *chatUserRenderer) Objects() []fyne.CanvasObject {
	if e := r.v.editor; e != nil {
		return append(r.objs[:len(r.objs):len(r.objs)], e.field, e.errLine, e.cancelB, e.sendB)
	}
	return r.objs
}
func (r *chatUserRenderer) Destroy() {}

// chatSentLine is "Sent: 2 requests · 14.2 KiB · llm.example.com" with a lock for a private
// connection; a click shows the exact request body (chat.go, showPayload).
type chatSentLine struct {
	widget.BaseWidget
	lock  *widget.Icon
	text  *textLine
	onTap func()
	sent  *engine.ChatSent
}

var (
	_ fyne.Tappable      = (*chatSentLine)(nil)
	_ desktop.Cursorable = (*chatSentLine)(nil)
)

func newChatSentLine(onTap func()) *chatSentLine {
	s := &chatSentLine{onTap: onTap, lock: widget.NewIcon(themedIcon(iconLock, colorStatusOK))}
	s.text = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	s.text.truncate = true
	s.ExtendBaseWidget(s)
	return s
}

// sentText is the line's text for what a turn sent.
func sentText(s *engine.ChatSent) string {
	if s == nil {
		return ""
	}
	parts := []string{pluralOf(s.Requests, "request")}
	parts = append(parts, model.FormatBytes(int64(s.Bytes)))
	if s.Tokens > 0 {
		parts = append(parts, formatCount(int64(s.Tokens))+" tokens")
	}
	if s.Endpoint != "" {
		parts = append(parts, "to "+s.Endpoint)
	}
	return "Sent " + strings.Join(parts, " · ")
}

// pluralOf counts things: "1 request", "3 requests".
func pluralOf(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return formatCount(int64(n)) + " " + what + "s"
}

func (s *chatSentLine) set(sent *engine.ChatSent) {
	s.sent = sent
	s.text.SetText(sentText(sent))
	s.Refresh()
}

// Tapped implements fyne.Tappable.
func (s *chatSentLine) Tapped(*fyne.PointEvent) {
	if s.onTap != nil {
		s.onTap()
	}
}

// Cursor implements desktop.Cursorable.
func (s *chatSentLine) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (s *chatSentLine) iconSize() float32 { return s.Theme().Size(theme.SizeNameCaptionText) + 1 }

// MinSize implements fyne.CanvasObject.
func (s *chatSentLine) MinSize() fyne.Size {
	s.ExtendBaseWidget(s)
	return fyne.NewSize(0, max(s.text.MinSize().Height, s.iconSize()))
}

// CreateRenderer implements fyne.Widget.
func (s *chatSentLine) CreateRenderer() fyne.WidgetRenderer {
	return &chatSentRenderer{s: s}
}

type chatSentRenderer struct{ s *chatSentLine }

func (r *chatSentRenderer) Layout(size fyne.Size) {
	s := r.s
	x := s.Theme().Size(theme.SizeNamePadding)
	is := s.iconSize()
	if s.sent != nil && s.sent.Private {
		s.lock.Show()
		s.lock.Move(fyne.NewPos(x, (size.Height-is)/2))
		s.lock.Resize(fyne.NewSize(is, is))
		x += is + 2
	} else {
		s.lock.Hide()
	}
	s.text.Move(fyne.NewPos(x, 0))
	s.text.Resize(fyne.NewSize(max(size.Width-x, 0), size.Height))
}

func (r *chatSentRenderer) MinSize() fyne.Size { return r.s.MinSize() }
func (r *chatSentRenderer) Refresh() {
	r.Layout(r.s.Size())
	r.s.text.Refresh()
	canvas.Refresh(r.s)
}
func (r *chatSentRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.s.lock, r.s.text}
}
func (r *chatSentRenderer) Destroy() {}

// --- the answer ---

// chatAnswerView is the model's answer: Markdown on the column's background. While the model is
// still thinking and has written nothing of its answer (a streaming item without text: the agent
// drops a reasoning model's reasoning deltas, which a local model may send for a minute; or a
// reasoning block in the text not closed yet), a muted "Thinking…" line with moving dots and, after
// a few seconds, the time it has thought says so, so that the column does not look stuck; reasoning
// text is never shown.
type chatAnswerView struct {
	*vstack
	c        *chatPanel
	item     engine.ChatItem
	text     *mdText
	thinking *textLine
	copyB    *tipButton
	actions  *fyne.Container
	// thinkSince is when the thinking line showed; thinkGen ends its ticks when it hides.
	thinkSince time.Time
	thinkGen   int
}

// thinkTick is how often the thinking line's dots move.
const thinkTick = 500 * time.Millisecond

// setThinking shows or hides the thinking line; shown, it ticks until it hides.
func (v *chatAnswerView) setThinking(on bool) {
	if on == v.thinking.Visible() {
		return
	}
	v.thinkGen++
	if !on {
		v.thinking.Hide()
		return
	}
	v.thinkSince = time.Now()
	v.thinking.SetText("Thinking…")
	v.thinking.Show()
	v.scheduleThink(v.thinkGen, 0)
}

func (v *chatAnswerView) scheduleThink(gen, n int) {
	sh := v.c.sh
	time.AfterFunc(thinkTick, func() {
		if sh.stopped.Load() {
			return
		}
		sh.do(func() {
			if gen != v.thinkGen {
				return
			}
			v.thinking.SetText(thinkingText(n+1, time.Since(v.thinkSince)))
			v.scheduleThink(gen, n+1)
		})
	})
}

// thinkingText is the thinking line after n ticks and d of thinking: "Thinking..", "Thinking. · 12 s".
func thinkingText(n int, d time.Duration) string {
	text := "Thinking" + strings.Repeat(".", n%3+1)
	if d >= 3*time.Second {
		text += " · " + strconv.Itoa(int(d.Seconds())) + " s"
	}
	return text
}

func newChatAnswerView(c *chatPanel) *chatAnswerView {
	v := &chatAnswerView{c: c}
	v.text = newMDText(theme.ColorNameForeground, c.mdHooks())
	v.thinking = newTextLine("Thinking…", colorMuted, theme.SizeNameText, fyne.TextStyle{Italic: true})
	v.thinking.Hide()
	v.copyB = newTipButton(c.sh, themedIcon(theme.ContentCopyIcon(), colorMuted), "Copy the answer as Markdown", func() {
		answer, _ := stripThinking(v.item.Text)
		c.sh.app.Clipboard().SetContent(answer)
	})
	v.actions = container.NewHBox(v.copyB)
	v.actions.Hide()
	v.vstack = makeVStack(theme.SizeNamePadding, v.text, v.thinking, v.actions)
	v.extend(v)
	return v
}

func (v *chatAnswerView) itemKind() engine.ChatItemKind { return engine.ChatItemAssistant }

func (v *chatAnswerView) update(it engine.ChatItem, blocks []mdBlock) {
	if v.item.Kind != "" && sameItem(v.item, it) {
		return
	}
	v.item = it
	if blocks == nil {
		answer, _ := stripThinking(it.Text)
		blocks = parseChatMarkdown(answer)
	}
	_, thinking := stripThinking(it.Text)
	v.text.setBlocks(blocks)
	v.setThinking(len(blocks) == 0 && (thinking || it.Streaming))
	v.actions.Hidden = it.Streaming || len(blocks) == 0
	v.Refresh()
}

func (v *chatAnswerView) setWidth(w float32) { v.text.setWidth(max(w-answerIndent, 0)) }

// --- a tool call ---

// chatToolStates are the rows' state texts, colours and icons.
// chatToolStates are the tool rows' states. Icons by name, resolved when drawn: a theme icon
// asked for at package initialisation asks Fyne for an app none has started yet
// (TestStartUpLogsNoFyneError).
var chatToolStates = map[engine.ChatToolState]struct {
	text  string
	color fyne.ThemeColorName
	icon  fyne.ThemeIconName
}{
	engine.ChatToolRunning: {"Running", colorAccent, theme.IconNameViewRefresh},
	engine.ChatToolWaiting: {"Waiting for your confirmation", colorAgent, theme.IconNameQuestion}, // the agent asks: its violet
	engine.ChatToolDone:    {"Done", colorMuted, theme.IconNameConfirm},                           // quiet when healthy: no colour
	engine.ChatToolFailed:  {"Failed", colorStatusError, theme.IconNameError},
	engine.ChatToolRefused: {"Refused", colorStatusWarning, theme.IconNameCancel},
	engine.ChatToolStopped: {"Stopped", colorMuted, theme.IconNameMediaStop},
}

// chatToolView is one tool call: a compact row (state icon, the command's label, a one-line
// summary, the state), opened by a click to the arguments and the result as the model saw it.
type chatToolView struct {
	*vstack
	c       *chatPanel
	item    engine.ChatItem
	icon    *widget.Icon
	label   *textLine
	summary *textLine
	state   *textLine
	chevron *widget.Icon
	header  *tapTarget
	bg      *surface // the row's background: none, a plate while it is open, a violet tint while it waits for the user
	spine   *surface // 2 pt at the row's left in the state's colour; none for a finished call
	body    *vstack
	width   float32
}

func newChatToolView(c *chatPanel) *chatToolView {
	v := &chatToolView{c: c}
	v.icon = widget.NewIcon(nil)
	v.label = newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	v.summary = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	v.summary.truncate = true
	v.state = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	v.chevron = widget.NewIcon(themedIcon(iconChevronRight, colorMuted))
	row := container.NewBorder(nil, nil,
		container.NewHBox(sized(sizeCellIcon, "", v.icon), v.label),
		container.NewHBox(v.state, sized(sizeCellIcon, "", v.chevron)),
		v.summary)
	v.bg = newSurface(colorClear)
	v.bg.radius = theme.SizeNameInputRadius
	v.spine = newSurface(colorClear)
	v.spine.min = fyne.NewSize(2, 0)
	v.header = newTapTarget(container.NewStack(v.bg, container.NewBorder(nil, nil, v.spine, nil), inset(theme.SizeNamePadding, theme.SizeNamePadding, row)), v.toggle)
	v.body = newVStack(theme.SizeNamePadding)
	v.body.Hide()
	v.vstack = makeVStack(theme.SizeNamePadding, v.header, v.body)
	v.extend(v)
	return v
}

func (v *chatToolView) itemKind() engine.ChatItemKind { return engine.ChatItemTool }

func (v *chatToolView) open() bool { return v.item.Tool != nil && v.c.expanded[v.item.Tool.CallID] }

// toggle opens or closes the row's details.
func (v *chatToolView) toggle() {
	if v.item.Tool == nil {
		return
	}
	v.c.expanded[v.item.Tool.CallID] = !v.open()
	v.build()
	v.c.itemsResized()
}

func (v *chatToolView) update(it engine.ChatItem, _ []mdBlock) {
	if v.item.Kind != "" && sameItem(v.item, it) {
		return
	}
	v.item = it
	t := it.Tool
	if t == nil {
		t = &engine.ChatToolCall{State: engine.ChatToolRunning}
	}
	st, ok := chatToolStates[t.State]
	if !ok {
		st = chatToolStates[engine.ChatToolRunning]
		st.text = string(t.State)
	}
	v.icon.SetResource(themedIcon(theme.Icon(st.icon), st.color))
	label := t.Label
	if label == "" {
		label = t.Command
	}
	v.label.SetText(label)
	v.summary.SetText(strings.ReplaceAll(t.Summary, "\n", " "))
	v.state.SetText(st.text)
	v.state.SetColor(st.color)
	// Quiet rows: no fill and a 2 pt spine in the state's colour (none for a finished call); a row
	// that waits for the user is tinted violet, an open one stands on a plate.
	v.bg.fill, v.bg.stroke, v.spine.fill = colorClear, "", colorClear
	if t.State != engine.ChatToolDone {
		v.spine.fill = st.color
	}
	switch {
	case t.State == engine.ChatToolWaiting:
		v.bg.fill = colorAgentWash
	case v.open():
		v.bg.fill = theme.ColorNameHover
	}
	v.bg.Refresh()
	v.spine.Refresh()
	v.build()
}

// build fills the opened details: the arguments, pretty-printed, and the result or the error.
func (v *chatToolView) build() {
	open := v.open()
	if v.item.Tool != nil && v.item.Tool.State != engine.ChatToolWaiting { // an open row stands on a plate
		v.bg.fill = colorClear
		if open {
			v.bg.fill = theme.ColorNameHover
		}
		v.bg.Refresh()
	}
	v.chevron.SetResource(themedIcon(iconChevronRight, colorMuted))
	if open {
		v.chevron.SetResource(themedIcon(iconChevronDown, colorMuted))
	}
	if !open || v.item.Tool == nil {
		v.body.Hide()
		v.Refresh()
		return
	}
	t := v.item.Tool
	var objs []fyne.CanvasObject
	code := func(text string) *mdCode { // selectable, as the messages' text (chat_select.go)
		c := newMDCode(text)
		c.selectable(v.c.copySelection)
		return c
	}
	if args := strings.TrimSpace(t.Args); args != "" && args != "{}" {
		objs = append(objs, heading("Arguments"), code(prettyJSON(t.Args)))
	}
	switch {
	case t.Error != "":
		objs = append(objs, heading("Error"), code(t.Error))
		objs[len(objs)-1].(*mdCode).color = colorStatusError
		objs[len(objs)-1].(*mdCode).words = true
	case t.Result != "":
		objs = append(objs, heading("Result, as the model saw it"), code(prettyJSON(t.Result)))
	case t.State == engine.ChatToolRunning || t.State == engine.ChatToolWaiting:
		objs = append(objs, newTextLine("No result yet.", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Italic: true}))
	}
	v.body.objs = objs
	v.body.setWidth(v.width - v.indent())
	v.body.Show()
	v.body.Refresh()
	v.Refresh()
}

func (v *chatToolView) indent() float32 { return v.Theme().Size(theme.SizeNameInnerPadding) * 2 }

func (v *chatToolView) setWidth(w float32) {
	v.width = w
	v.body.setWidth(w - v.indent())
}

// MinSize implements fyne.CanvasObject; the details are indented under the row.
func (v *chatToolView) MinSize() fyne.Size { return v.vstack.MinSize() }

// CreateRenderer implements fyne.Widget: the vstack's, with the details indented.
func (v *chatToolView) CreateRenderer() fyne.WidgetRenderer {
	return &chatToolRenderer{vstackRenderer: vstackRenderer{v: v.vstack}, t: v}
}

type chatToolRenderer struct {
	vstackRenderer
	t *chatToolView
}

func (r *chatToolRenderer) Layout(size fyne.Size) {
	r.vstackRenderer.Layout(size)
	if r.t.body.Visible() {
		in := r.t.indent()
		p := r.t.body.Position()
		r.t.body.Move(fyne.NewPos(in, p.Y))
		r.t.body.Resize(fyne.NewSize(max(size.Width-in, 0), r.t.body.MinSize().Height))
	}
}

func (r *chatToolRenderer) Refresh() {
	r.Layout(r.t.Size())
	canvas.Refresh(r.t)
}

// chatToolView's own MinSize and CreateRenderer win over the vstack's; Fyne keys the renderer by
// the chatToolView (extend).
var _ fyne.Widget = (*chatToolView)(nil)

// prettyJSON indents JSON for reading; other text stays as it is.
func prettyJSON(s string) string {
	t := strings.TrimSpace(s)
	if t == "" || (t[0] != '{' && t[0] != '[') {
		return s
	}
	var v any
	if json.Unmarshal([]byte(t), &v) != nil {
		return s
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(b)
}

// --- Cogpit's note ---

// chatNoteLevels are a note's colour and icon by level.
func chatNoteStyle(level string) (fyne.ThemeColorName, fyne.Resource) {
	switch level {
	case engine.LevelError:
		return colorStatusError, theme.ErrorIcon()
	case engine.LevelWarning:
		return colorStatusWarning, theme.WarningIcon()
	}
	return colorMuted, theme.InfoIcon()
}

// chatNoteView is Cogpit's note in a turn (a limit ended it, the user stopped it, an error): its
// text and icon in the level's colour on a tint of it, with a bar at its left edge.
type chatNoteView struct {
	widget.BaseWidget
	item  engine.ChatItem
	bg    *canvas.Rectangle
	bar   *canvas.Rectangle
	icon  *widget.Icon
	text  *mdFlow
	color fyne.ThemeColorName
}

func newChatNoteView(c *chatPanel) *chatNoteView {
	v := &chatNoteView{bg: canvas.NewRectangle(color.Transparent), bar: canvas.NewRectangle(color.Transparent), icon: widget.NewIcon(nil)}
	v.text = newMDFlow(nil, colorMuted, nil)
	v.text.selectable(c.copySelection)
	v.ExtendBaseWidget(v)
	return v
}

func (v *chatNoteView) itemKind() engine.ChatItemKind { return engine.ChatItemNote }

func (v *chatNoteView) update(it engine.ChatItem, _ []mdBlock) {
	if v.item.Kind != "" && sameItem(v.item, it) {
		return
	}
	v.item = it
	c, icon := chatNoteStyle(it.NoteLevel)
	v.color = c
	v.icon.SetResource(themedIcon(icon, c))
	v.text.color = c
	v.text.setRuns([]mdRun{{text: it.Text}})
	v.Refresh()
}

func (v *chatNoteView) metrics() (pad, iconS float32) {
	th := v.Theme()
	return th.Size(theme.SizeNameInnerPadding), th.Size(theme.SizeNameInlineIcon)
}

func (v *chatNoteView) setWidth(w float32) {
	pad, iconS := v.metrics()
	v.text.setWidth(w - 3*pad - iconS - 3)
}

// MinSize implements fyne.CanvasObject.
func (v *chatNoteView) MinSize() fyne.Size {
	v.ExtendBaseWidget(v)
	pad, iconS := v.metrics()
	return fyne.NewSize(0, max(v.text.MinSize().Height, iconS)+2*pad)
}

// CreateRenderer implements fyne.Widget.
func (v *chatNoteView) CreateRenderer() fyne.WidgetRenderer {
	return &chatNoteRenderer{v: v}
}

type chatNoteRenderer struct{ v *chatNoteView }

func (r *chatNoteRenderer) Layout(size fyne.Size) {
	v := r.v
	pad, iconS := v.metrics()
	v.bg.Resize(size)
	v.bar.Resize(fyne.NewSize(3, size.Height))
	v.icon.Move(fyne.NewPos(3+pad, pad))
	v.icon.Resize(fyne.NewSize(iconS, iconS))
	v.text.Move(fyne.NewPos(3+2*pad+iconS, pad))
	v.text.Resize(fyne.NewSize(max(size.Width-3-3*pad-iconS, 0), v.text.MinSize().Height))
}

func (r *chatNoteRenderer) MinSize() fyne.Size { return r.v.MinSize() }

func (r *chatNoteRenderer) Refresh() {
	v := r.v
	th := v.Theme()
	c := themeColor(th, v.color)
	v.bg.FillColor = withAlpha(c, 0x12)
	v.bg.CornerRadius = th.Size(theme.SizeNameInputRadius)
	v.bar.FillColor = c
	r.Layout(v.Size())
	v.bg.Refresh()
	v.bar.Refresh()
	v.text.Refresh()
}

func (r *chatNoteRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.v.bg, r.v.bar, r.v.icon, r.v.text}
}
func (r *chatNoteRenderer) Destroy() {}

// newChatView makes the widget of an item's kind.
func newChatView(c *chatPanel, kind engine.ChatItemKind, idx int) chatView {
	switch kind {
	case engine.ChatItemUser:
		return newChatUserView(c, idx)
	case engine.ChatItemTool:
		return newChatToolView(c)
	case engine.ChatItemNote:
		return newChatNoteView(c)
	}
	return newChatAnswerView(c)
}

// openLink opens an answer's link in the browser on the user's click: an absolute http or https
// URL only, as a README's links (helmchartdrawer.go).
func (c *chatPanel) openLink(u *url.URL) {
	if u == nil || (u.Scheme != schemeWebSecure && u.Scheme != schemeWeb) || u.Host == "" {
		where := "it"
		if u != nil {
			where = shorten(strings.ToValidUTF8(u.Redacted(), "?"), 120)
		}
		c.sh.toast("warning", "Cogpit opens only http and https links from the chat, not "+where+".")
		return
	}
	if err := openBrowser(c.sh.app, u); err != nil {
		c.sh.toast("error", fmt.Sprintf("Could not open %s: %v", u.Redacted(), err))
	}
}
