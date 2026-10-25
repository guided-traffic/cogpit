package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A sent message edited and sent again (F9, the owner's decision of 2026-10-15, ChatGPT's form).
// Every message of the user's has a pencil under its card, at the end of the line of what its turn
// sent. It opens the message in an editor in place of the card: Enter or Send sends the edited text
// through ChatAgent.Resend, which drops the message and everything after it from the conversation,
// the model's history and the kept file (the audit log keeps everything); Escape or Cancel leaves
// the message as it was. Sending it unchanged retries a failed or stopped turn. The resent message
// attaches the view and selection the input's chip shows at that moment. No warning when editing
// and no note to the model (the owner's). While a turn of the conversation runs, or while the
// input could not send either (no cluster, another cluster's conversation), the pencil is dimmed
// and its tooltip says why (the lead's decision for the owner to confirm: Stop first); an editor
// that was open when a turn started keeps its text, its Send disabled until the turn ends. One
// editor at a time.

// chatIconTap is a small icon with a tooltip that runs onTap; disabled, it is dimmed and its
// tooltip says why.
type chatIconTap struct {
	widget.BaseWidget
	sh     *shell
	icon   fyne.Resource
	tip    string
	reason string // why it is disabled; "" enabled
	onTap  func()
}

var (
	_ fyne.Tappable      = (*chatIconTap)(nil)
	_ desktop.Hoverable  = (*chatIconTap)(nil)
	_ desktop.Cursorable = (*chatIconTap)(nil)
)

func newChatIconTap(sh *shell, icon fyne.Resource, tip string, onTap func()) *chatIconTap {
	b := &chatIconTap{sh: sh, icon: icon, tip: tip, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

// setReason disables the icon with a reason, or enables it ("").
func (b *chatIconTap) setReason(reason string) {
	if reason == b.reason {
		return
	}
	b.reason = reason
	b.Refresh()
}

// Disabled reports whether the icon is dimmed.
func (b *chatIconTap) Disabled() bool { return b.reason != "" }

// Tapped implements fyne.Tappable.
func (b *chatIconTap) Tapped(*fyne.PointEvent) {
	b.sh.tip.hideFor(b)
	if b.reason == "" && b.onTap != nil {
		b.onTap()
	}
}

// Cursor implements desktop.Cursorable.
func (b *chatIconTap) Cursor() desktop.Cursor {
	if b.reason != "" {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

// MouseIn implements desktop.Hoverable: the tooltip, or why it is disabled.
func (b *chatIconTap) MouseIn(ev *desktop.MouseEvent) {
	tip := b.tip
	if b.reason != "" {
		tip = b.reason
	}
	b.sh.tip.hover(b, tip, ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (b *chatIconTap) MouseMoved(ev *desktop.MouseEvent) { b.sh.tip.moved(b, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (b *chatIconTap) MouseOut() { b.sh.tip.hideFor(b) }

func (b *chatIconTap) iconSize() float32 { return b.Theme().Size(theme.SizeNameCaptionText) + 3 }

// MinSize implements fyne.CanvasObject.
func (b *chatIconTap) MinSize() fyne.Size {
	b.ExtendBaseWidget(b)
	s := b.iconSize()
	return fyne.NewSize(s+4, s+2)
}

// CreateRenderer implements fyne.Widget.
func (b *chatIconTap) CreateRenderer() fyne.WidgetRenderer {
	img := canvas.NewImageFromResource(nil)
	img.FillMode = canvas.ImageFillContain
	r := &chatIconTapRenderer{b: b, img: img}
	r.Refresh()
	return r
}

type chatIconTapRenderer struct {
	b   *chatIconTap
	img *canvas.Image
}

func (r *chatIconTapRenderer) Layout(size fyne.Size) {
	s := r.b.iconSize()
	r.img.Move(fyne.NewPos((size.Width-s)/2, (size.Height-s)/2))
	r.img.Resize(fyne.NewSize(s, s))
}

func (r *chatIconTapRenderer) MinSize() fyne.Size { return r.b.MinSize() }

func (r *chatIconTapRenderer) Refresh() {
	c := colorMuted
	if r.b.reason != "" {
		c = theme.ColorNameDisabled
	}
	r.img.Resource = themedIcon(r.b.icon, c)
	r.img.Refresh()
	r.Layout(r.b.Size())
}

func (r *chatIconTapRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.img} }
func (r *chatIconTapRenderer) Destroy()                     {}

// chatEditEntry is the editor's entry: Enter sends, Shift+Enter is a new line, Escape cancels, ⌘.
// stops a running turn.
type chatEditEntry struct {
	widget.Entry
	shift    func() bool
	onSubmit func()
	onCancel func()
	onStop   func()
}

// CreateRenderer implements fyne.Widget: the wheel passed on (wheelpass.go).
func (e *chatEditEntry) CreateRenderer() fyne.WidgetRenderer {
	return passWheel(e, e.Entry.CreateRenderer(), func() bool { return e.MultiLine })
}

func newChatEditEntry() *chatEditEntry {
	e := &chatEditEntry{shift: shiftHeld}
	e.MultiLine = true
	e.Wrapping = fyne.TextWrapWord
	e.ExtendBaseWidget(e)
	return e
}

// TypedKey implements fyne.Focusable.
func (e *chatEditEntry) TypedKey(ev *fyne.KeyEvent) {
	switch ev.Name {
	case fyne.KeyReturn, fyne.KeyEnter:
		if !e.shift() {
			e.onSubmit()
			return
		}
	case fyne.KeyEscape:
		e.onCancel()
		return
	}
	e.Entry.TypedKey(ev)
}

// TypedShortcut implements fyne.Shortcutable: ⌘. stops a running turn.
func (e *chatEditEntry) TypedShortcut(s fyne.Shortcut) {
	if cs, ok := s.(*desktop.CustomShortcut); ok && cs.KeyName == shortcutStop.KeyName && cs.Modifier == shortcutStop.Modifier {
		e.onStop()
		return
	}
	e.Entry.TypedShortcut(s)
}

// chatEditor is a user's message open for editing: the entry in the card, Cancel and Send below it,
// and why the last Send failed.
type chatEditor struct {
	entry   *chatEditEntry
	field   fyne.CanvasObject // the entry drawn flat: the card is its box
	cancelB *textButton
	sendB   *textButton
	errLine *textLine
	sending bool
}

// editTexts are the editor's buttons and the pencil's tooltip.
const (
	editTip     = "Edit and send again"
	editRunning = "Stop the running turn to edit and send again"
)

// startEdit opens the message in the editor (closing another one), its text selected at the end.
func (v *chatUserView) startEdit() {
	c := v.c
	if v.editor != nil || c.editReason() != "" {
		return
	}
	if c.editing != nil && c.editing != v {
		c.editing.stopEdit()
	}
	e := &chatEditor{entry: newChatEditEntry()}
	e.entry.SetText(v.item.Text)
	e.entry.CursorRow = strings.Count(v.item.Text, "\n")
	e.entry.CursorColumn = len([]rune(v.item.Text[strings.LastIndex(v.item.Text, "\n")+1:]))
	e.entry.onSubmit = func() { v.submitEdit() }
	e.entry.onCancel = func() { v.stopEdit() }
	e.entry.onStop = c.stopIfRunning
	e.entry.OnChanged = func(string) {
		e.errLine.Hide()
		v.sizeEditor()
		v.refreshEdit()
	}
	e.field = container.NewThemeOverride(e.entry, &flatEntryTheme{app: c.sh.app})
	e.cancelB = newPlateButton("Cancel", nil, v.stopEdit)
	e.sendB = newPlateButton("Send", iconSend, v.submitEdit)
	e.sendB.Importance = widget.HighImportance
	e.errLine = newTextLine("", colorStatusError, theme.SizeNameCaptionText, fyne.TextStyle{})
	e.errLine.truncate = true
	e.errLine.Hide()
	v.editor = e
	c.editing = v
	v.sizeEditor()
	v.refreshEdit()
	v.Refresh()
	c.itemsResized()
	if cv := c.sh.win.Canvas(); cv != nil {
		cv.Focus(e.entry)
	}
}

// stopEdit closes the editor; the message stays as it was.
func (v *chatUserView) stopEdit() {
	if v.editor == nil {
		return
	}
	if cv := v.c.sh.win.Canvas(); cv != nil && cv.Focused() == v.editor.entry {
		cv.Unfocus()
	}
	v.editor = nil
	if v.c.editing == v {
		v.c.editing = nil
	}
	v.Refresh()
	v.c.itemsResized()
}

// submitEdit sends the edited text again (Resend), from this message on.
func (v *chatUserView) submitEdit() {
	e := v.editor
	if e == nil || e.sending || strings.TrimSpace(e.entry.Text) == "" || v.c.editReason() != "" {
		return
	}
	e.sending = true
	v.refreshEdit()
	v.c.resend(v, e.entry.Text)
}

// editFailed shows why the resend was refused; the editor keeps the text.
func (v *chatUserView) editFailed(err error) {
	if e := v.editor; e != nil {
		e.sending = false
		e.errLine.SetText("Not sent: " + errorText(err))
		e.errLine.Show()
		v.refreshEdit()
		v.Refresh()
		v.c.itemsResized()
	}
}

// sizeEditor shows as many rows as the text takes at the card's width, 2 to 12.
func (v *chatUserView) sizeEditor() {
	e := v.editor
	if e == nil {
		return
	}
	th := v.Theme()
	w := v.width - 4*v.pad()
	lines := 0
	for _, para := range strings.Split(e.entry.Text, "\n") {
		lines += max(len(wrapLines(para, w, th.Size(theme.SizeNameText), fyne.TextStyle{})), 1)
	}
	e.entry.SetMinRowsVisible(min(max(lines, 2), 12))
}

// refreshEdit shows whether the editor can send: not while a turn runs, a send is under way or the
// text is empty.
func (v *chatUserView) refreshEdit() {
	e := v.editor
	if e == nil {
		return
	}
	if e.sending || v.c.editReason() != "" || strings.TrimSpace(e.entry.Text) == "" {
		e.sendB.Disable()
	} else {
		e.sendB.Enable()
	}
}

// setEditable dims the pencil with the reason it cannot be used ("" enabled), and keeps an open
// editor's Send in step.
func (v *chatUserView) setEditable(reason string) {
	v.editB.setReason(reason)
	v.refreshEdit()
}

// editReason is why no message can be edited and sent again now; "" when it can.
func (c *chatPanel) editReason() string {
	switch {
	case c.running():
		return editRunning
	case c.sending:
		return "A message is on its way"
	}
	return c.inputReason()
}

// refreshEditable shows on every user's message whether it can be edited now.
func (c *chatPanel) refreshEditable() {
	reason := c.editReason()
	for _, v := range c.views {
		if u, ok := v.(*chatUserView); ok {
			u.setEditable(reason)
		}
	}
}

// resend sends the edited text of the message v shows again: the agent drops that message and
// everything after it and starts a turn. The input's chip says what it attaches, as for a new
// message. A refused resend keeps the editor open with the reason.
func (c *chatPanel) resend(v *chatUserView, text string) {
	id, idx := c.convID, v.idx
	attach := c.input.attach && c.input.chip.text.text != ""
	c.sending = true
	c.input.refresh()
	go func() {
		defer c.sh.recoverTo("the agent chat")
		err := c.agent.Resend(id, idx, engine.ChatUserMessage{Text: text, AttachSelection: attach})
		c.sh.do(func() {
			c.sending = false
			if err != nil {
				v.editFailed(err)
				c.input.refresh()
				return
			}
			v.stopEdit()
			if id == c.convID {
				c.requestRead()
			}
			c.input.refresh()
			c.scroll.ScrollToBottom()
		})
	}()
}
