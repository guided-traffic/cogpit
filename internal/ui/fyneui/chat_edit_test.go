package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// F9 (chat_edit.go): a sent message edited and sent again.

// twoTurns is a finished conversation of two turns, each message with what it sent.
func twoTurns(ch *fakeChat) string {
	sent := &engine.ChatSent{Requests: 1, Bytes: 9_000, Endpoint: "localhost:1234"}
	return ch.add("demo", "Which pods restart?",
		engine.ChatItem{Kind: engine.ChatItemUser, Text: "Which pods restart?", Sent: sent},
		engine.ChatItem{Kind: engine.ChatItemAssistant, Text: "None in **payments**."},
		engine.ChatItem{Kind: engine.ChatItemUser, Text: "And in shop?", Sent: sent},
		engine.ChatItem{Kind: engine.ChatItemNote, NoteLevel: engine.LevelError, Text: "the endpoint answered HTTP 503 Service Unavailable"},
	)
}

func userView(t *testing.T, sh *shell, i int) *chatUserView {
	t.Helper()
	u, ok := sh.chat.views[i].(*chatUserView)
	if !ok {
		t.Fatalf("item %d is no user's message: %T", i, sh.chat.views[i])
	}
	return u
}

// TestChatEditAndResend: a message's pencil opens it in an editor with its text and the focus;
// Enter sends the edited text again for that item, with the input's chip; the editor closes and the
// conversation shows what the agent kept: the messages after it are gone. Sent unchanged, the
// failed turn's message is retried.
func TestChatEditAndResend(t *testing.T) {
	sh, w, q, f, ch := chatScene(t, ThemeDark, twoTurns)
	conv := sh.chat.convID
	u := userView(t, sh, 2)
	if u.editB.Disabled() {
		t.Fatalf("the pencil is disabled: %q", u.editB.reason)
	}
	test.Tap(u.editB)
	if u.editor == nil || u.editor.entry.Text != "And in shop?" || sh.chat.editing != u || w.Canvas().Focused() != u.editor.entry {
		t.Fatal("the pencil did not open the message in the editor with the focus")
	}
	if u.MinSize().Height <= 0 || !u.editor.sendB.Visible() || u.editor.sendB.Disabled() {
		t.Error("the editor's Send is not there")
	}
	// Retried unchanged: Enter.
	u.editor.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	q.until(t, "the resend", func() bool { return len(ch.resendsOf()) == 1 && u.editor == nil })
	if r := ch.resendsOf()[0]; r.id != conv || r.item != 2 || r.msg.Text != "And in shop?" {
		t.Errorf("resent %+v", r)
	}
	q.until(t, "the cut conversation", func() bool { return len(sh.chat.views) == 3 && sh.chat.running() })
	ch.change(conv, func(cv *engine.ChatConversation) { cv.Running = false })
	q.until(t, "the turn's end", func() bool { return !sh.chat.running() })

	// The first message, edited; the input's chip (a selected pod) goes with it.
	row := f.tables["pods"].Rows[3].Ref
	f.update(func(u *engine.UIState) { u.Selected = []model.ObjectRef{row} })
	q.until(t, "the chip", func() bool { return sh.chat.input.chip.Visible() })
	first := userView(t, sh, 0)
	test.Tap(first.editB)
	first.editor.entry.SetText("Which pods restart in payments?")
	test.Tap(first.editor.sendB)
	q.until(t, "the second resend", func() bool { return len(ch.resendsOf()) == 2 && first.editor == nil })
	if r := ch.resendsOf()[1]; r.item != 0 || r.msg.Text != "Which pods restart in payments?" || !r.msg.AttachSelection {
		t.Errorf("resent %+v", r)
	}
	q.until(t, "the conversation of one message", func() bool { return len(sh.chat.views) == 1 })
	if got := userView(t, sh, 0).item.Text; got != "Which pods restart in payments?" {
		t.Errorf("shown %q", got)
	}
}

// TestChatEditCancel: Escape and Cancel close the editor and leave the message as it was; one
// editor at a time.
func TestChatEditCancel(t *testing.T) {
	sh, _, q, _, ch := chatScene(t, ThemeDark, twoTurns)
	u := userView(t, sh, 2)
	test.Tap(u.editB)
	u.editor.entry.SetText("something else")
	u.editor.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if u.editor != nil || u.item.Text != "And in shop?" || sh.chat.editing != nil {
		t.Error("Escape did not cancel the edit")
	}
	test.Tap(u.editB)
	first := userView(t, sh, 0)
	test.Tap(first.editB)
	if u.editor != nil || first.editor == nil || sh.chat.editing != first {
		t.Error("two editors open at once")
	}
	test.Tap(first.editor.cancelB)
	q.drain()
	if first.editor != nil || len(ch.resendsOf()) != 0 {
		t.Errorf("Cancel: editor %v, %d resends", first.editor != nil, len(ch.resendsOf()))
	}
}

// TestChatEditWhileATurnRuns (the lead's decision): while a turn runs the pencils are dimmed and say
// to stop first; an editor that was open when a turn started cannot send until it ends.
func TestChatEditWhileATurnRuns(t *testing.T) {
	sh, _, q, _, ch := chatScene(t, ThemeDark, func(ch *fakeChat) string { return demoConversation(ch, "demo") })
	conv := sh.chat.convID
	u := userView(t, sh, 0)
	if !u.editB.Disabled() || u.editB.reason != editRunning {
		t.Fatalf("a running turn: the pencil's reason %q", u.editB.reason)
	}
	test.Tap(u.editB)
	if u.editor != nil {
		t.Fatal("the pencil opened an editor while a turn runs")
	}
	ch.change(conv, func(cv *engine.ChatConversation) { cv.Running = false })
	q.until(t, "the pencil", func() bool { return !u.editB.Disabled() })
	test.Tap(u.editB)
	ch.change(conv, func(cv *engine.ChatConversation) { cv.Running = true })
	q.until(t, "the turn", sh.chat.running)
	if u.editor == nil || !u.editor.sendB.Disabled() {
		t.Fatal("an open editor can send while a turn runs")
	}
	u.editor.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	q.drain()
	if n := len(ch.resendsOf()); n != 0 || u.editor.entry.Text != u.item.Text {
		t.Errorf("Enter while a turn runs: %d resends", n)
	}
}

// TestChatEditFailureKeepsTheText: a refused resend keeps the editor open with its text and says
// why; a conversation about another cluster cannot be edited, the pencil says why.
func TestChatEditFailureKeepsTheText(t *testing.T) {
	sh, _, q, f, ch := chatScene(t, ThemeDark, twoTurns)
	ch.sendErr = errNotSent
	u := userView(t, sh, 2)
	test.Tap(u.editB)
	u.editor.entry.SetText("And in infra?")
	u.editor.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	q.until(t, "the failure", func() bool { return u.editor != nil && u.editor.errLine.Visible() })
	if u.editor.entry.Text != "And in infra?" || !strings.Contains(u.editor.errLine.text, "HTTP 503") || u.editor.sendB.Disabled() {
		t.Errorf("after a refused resend: %q, %q", u.editor.entry.Text, u.editor.errLine.text)
	}
	f.update(func(ui *engine.UIState) { ui.Context = "other" })
	q.until(t, "the other cluster", func() bool { return u.editB.Disabled() })
	if !strings.Contains(u.editB.reason, "start a new one") {
		t.Errorf("the pencil's reason %q", u.editB.reason)
	}
}

// TestRenderChatEdit: a sent message open in the editor, its text changed, under the turn before it.
func TestRenderChatEdit(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		t.Run(string(mode), func(t *testing.T) {
			renderChat(t, mode, "chat_edit", func(ch *fakeChat) string {
				id := demoConversation(ch, "demo")
				ch.mu.Lock()
				ch.convs[id].Running = false
				ch.convs[id].Items = ch.convs[id].Items[:9]
				ch.mu.Unlock()
				return id
			}, func(sh *shell, w fyne.Window, q *mainQueue) {
				u := userView(t, sh, 5)
				test.Tap(u.editB)
				u.editor.entry.SetText("Restart only checkout-api in payments,\nand tell me why ledger-writer is forbidden.")
				q.drain()
				settle(w)
				sh.chat.scroll.ScrollToBottom()
				sh.chat.updatePin()
			})
		})
	}
}
