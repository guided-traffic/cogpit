package fyneui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The agent chat's column (chat.go) against a scripted agent (chat_fake_test.go).

// demoAnswer is an answer with what the column draws: bold text, inline code, a GitHub table, a
// code block, a numbered list and a link.
const demoAnswer = "The pod's **readiness probe fails**: `GET /healthz` on port `8080` answers **503**, because the API " +
	"cannot reach its database.\n\n" +
	"| Container | State | Restarts | Last exit |\n|---|---|---:|---|\n" +
	"| `api` | Running, not ready | 14 | `Error` (exit code 1) |\n| `envoy` | Running, ready | 0 | – |\n\n" +
	"Its log says why, on every start:\n\n```\ndial tcp 10.96.14.7:5432: connect: connection refused\n```\n\n" +
	"1. The Service `payments-db` has **no endpoints**: its selector `app=payments-db` matches no running pod.\n" +
	"2. The StatefulSet `payments-db` was scaled to `0` at 12:04.\n\n" +
	"Scaling it back to one replica should fix it. More on probes: " +
	"[container probes](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#container-probes)."

const demoPod = "checkout-api-7d9f8b6c4-x2x4v"

// demoConversation is a conversation of three turns: an answered question with its tool calls, a
// turn a looping model ended at its limit after a refused and a failed call, and a running turn
// waiting for the user's confirmation.
func demoConversation(ch *fakeChat, ctx string) string {
	sent := func(req, bytes, tokens int) *engine.ChatSent {
		return &engine.ChatSent{Requests: req, Bytes: bytes, Messages: req * 2, ToolResults: req - 1, Tokens: tokens, Endpoint: "localhost:1234", Private: true}
	}
	podRef := `{"ref":{"kind":"pods","namespace":"payments","name":"` + demoPod + `"}}`
	id := ch.add(ctx, "Why is checkout-api not ready?",
		engine.ChatItem{Kind: engine.ChatItemUser, Text: "Why is checkout-api not ready? Its pods keep restarting.",
			Attached: "Pods · payments/" + demoPod, Sent: sent(4, 48_213, 12_840)},
		engine.ChatItem{Kind: engine.ChatItemTool, Tool: &engine.ChatToolCall{CallID: "t1", Command: "object.get", Label: "Get Object",
			Summary: "Pod payments/" + demoPod, State: engine.ChatToolDone, Args: podRef,
			Result: `{"status":{"phase":"Running","containerStatuses":[{"name":"api","ready":false,"restartCount":14}]}}`}},
		engine.ChatItem{Kind: engine.ChatItemTool, Tool: &engine.ChatToolCall{CallID: "t2", Command: "pod.logs", Label: "Logs",
			Summary: demoPod + " · api", State: engine.ChatToolDone, Args: podRef}},
		engine.ChatItem{Kind: engine.ChatItemTool, Tool: &engine.ChatToolCall{CallID: "t3", Command: "dock.read", Label: "Read Tab Output",
			Summary: "Logs: " + demoPod + ", the last 200 lines", State: engine.ChatToolDone, Args: `{"id":"logs-3","lines":200}`,
			Result: "2026-10-11T14:29:58Z dial tcp 10.96.14.7:5432: connect: connection refused"}},
		engine.ChatItem{Kind: engine.ChatItemAssistant, Text: "<think>The probe fails; check the logs.</think>" + demoAnswer,
			Timing: &engine.ChatTiming{TTFT: 840 * time.Millisecond, Generating: 4200 * time.Millisecond, OutputTokens: 177}},
		engine.ChatItem{Kind: engine.ChatItemUser, Text: "Restart every deployment in payments.", Sent: sent(6, 71_904, 18_377)},
		engine.ChatItem{Kind: engine.ChatItemTool, Tool: &engine.ChatToolCall{CallID: "t4", Command: "deployment.restart", Label: "Restart",
			Summary: "Deployment payments/checkout-api", State: engine.ChatToolRefused, Args: `{"ref":{"kind":"deployments","namespace":"payments","name":"checkout-api"}}`,
			Error: "the user declined"}},
		engine.ChatItem{Kind: engine.ChatItemTool, Tool: &engine.ChatToolCall{CallID: "t5", Command: "deployment.restart", Label: "Restart",
			Summary: "Deployment payments/ledger-writer", State: engine.ChatToolFailed,
			Error: `deployments.apps "ledger-writer" is forbidden: User "dev" cannot patch resource "deployments" in the namespace "payments"`}},
		engine.ChatItem{Kind: engine.ChatItemNote, NoteLevel: engine.LevelWarning,
			Text: "The turn stopped at its third identical tool call: Restart, Deployment payments/ledger-writer."},
		engine.ChatItem{Kind: engine.ChatItemUser, Text: "Then just scale payments-db back to 1.", Sent: sent(2, 22_480, 6_102)},
		engine.ChatItem{Kind: engine.ChatItemAssistant, Text: "Scaling the StatefulSet `payments-db` to **1** replica. Cogpit asks you first.",
			Timing: &engine.ChatTiming{TTFT: 1260 * time.Millisecond, Generating: 900 * time.Millisecond, OutputTokens: 31, Estimated: true}},
		engine.ChatItem{Kind: engine.ChatItemTool, Tool: &engine.ChatToolCall{CallID: "t6", Command: "statefulset.scale", Label: "Scale",
			Summary: "StatefulSet payments/payments-db to 1 replica", State: engine.ChatToolWaiting,
			Args: `{"ref":{"kind":"statefulsets","namespace":"payments","name":"payments-db"},"replicas":1}`}},
	)
	ch.mu.Lock()
	ch.convs[id].Running = true
	ch.payloads[id+"/0"] = `{"model":"qwen3.6-27b","stream":true,"messages":[{"role":"system","content":"You are Cogpit's agent."},` +
		`{"role":"user","content":"Why is checkout-api not ready? Its pods keep restarting."}],"tools":[{"type":"function","function":{"name":"object_get"}}]}`
	ch.mu.Unlock()
	return id
}

// chatScene is a shell on the demo cluster's pods with the chat column open on conv (none: an
// empty conversation), connected to a local model.
func chatScene(t *testing.T, mode ThemeMode, script func(*fakeChat) string) (*shell, fyne.Window, *mainQueue, *fakeEngine, *fakeChat) {
	t.Helper()
	f := connectedFake(t, 300)
	ch := withChat(t, f)
	ch.conns = []engine.ChatConnection{lmStudio(), litellm()}
	conv := ""
	if script != nil {
		conv = script(ch)
	}
	f.ui.Chat = true
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the chat to load", func() bool {
		return sh.chat.connsRead && sh.chat.convID == conv && (conv == "" || len(sh.chat.views) > 0)
	})
	settle(w)
	return sh, w, q, f, ch
}

// TestChatColumnShownByUIState: the column stands at the window's right edge while UIState.Chat is
// set, beside the work area, the full height between the bars; it goes when it is unset.
func TestChatColumnShownByUIState(t *testing.T) {
	sh, w, q, f, _ := chatScene(t, ThemeDark, nil)
	if sh.chatRow.Objects[0] != sh.chatSplit {
		t.Fatal("UIState.Chat set: the chat column is not shown")
	}
	col := sh.chat.root
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(col)
	if got := pos.X + col.Size().Width; got != w.Canvas().Size().Width {
		t.Errorf("the column ends at %v, want the window's right edge %v", got, w.Canvas().Size().Width)
	}
	if col.Size().Width != defaultChatWidth {
		t.Errorf("the column is %v wide, want the default %v", col.Size().Width, defaultChatWidth)
	}
	if mid := sh.center.Size().Width; mid+col.Size().Width+handleWidth != sh.chatRow.Size().Width {
		t.Errorf("the work area (%v) and the column (%v) do not share the row (%v)", mid, col.Size().Width, sh.chatRow.Size().Width)
	}
	f.update(func(u *engine.UIState) { u.Chat = false })
	q.until(t, "the column to go", func() bool { return sh.chatRow.Objects[0] == sh.center })
}

// TestChatColumnStaysUnderSettings: a settings page is a card over the whole window (D-118); the
// chat column stays where it was, under the card's shade with the work area.
func TestChatColumnStaysUnderSettings(t *testing.T) {
	sh, w, q, f, _ := chatScene(t, ThemeDark, nil)
	f.update(func(u *engine.UIState) {
		u.Settings = &engine.SettingsPage{Page: engine.SettingsPreferences, Section: engine.SectionApp}
	})
	q.until(t, "the settings page", func() bool { return sh.settings.visible() })
	if sh.chatRow.Objects[0] != sh.chatSplit || sh.center.Objects[0] != sh.mainRow || w.Canvas().Overlays().Top() != sh.settings.overlay {
		t.Fatal("with a settings page open the chat column is gone, the work area replaced, or the card not over the window")
	}
}

// TestNoChatWithoutAgent: an engine without a chat agent (the CLI's) gives the window no column,
// no View › Agent Chat and no ⌘.
func TestNoChatWithoutAgent(t *testing.T) {
	f := connectedFake(t, 10)
	f.ui.Chat = true // cannot happen with the engine (chat.toggle is disabled); the window ignores it
	sh, _, q := newTestShell(t, f, Options{})
	q.drain()
	if sh.chatRow.Objects[0] != sh.center {
		t.Error("a column without a chat agent")
	}
	for _, m := range sh.mainMenu().Items {
		for _, it := range m.Items {
			if it.Label == "Agent Chat" || it.Label == "Stop Agent" {
				t.Errorf("menu %s has %q without a chat agent", m.Label, it.Label)
			}
		}
	}
}

// mainMenuItem finds a main menu item by its label.
func mainMenuItem(t *testing.T, sh *shell, label string) *fyne.MenuItem {
	t.Helper()
	for _, m := range sh.mainMenu().Items {
		for _, it := range m.Items {
			if it.Label == label {
				return it
			}
		}
	}
	t.Fatalf("no menu item %q", label)
	return nil
}

// TestChatToggleThroughTheEngine: View › Agent Chat carries ⌘I and dispatches chat.toggle, so the
// engine's UI state shows the column and hides it again; opened by the user, the input takes the
// focus. Against the real engine and its registry.
func TestChatToggleThroughTheEngine(t *testing.T) {
	s, _ := settingsEngine(t)
	ch := newFakeChat()
	ch.conns = []engine.ChatConnection{lmStudio()}
	ch.notify = s.NotifyChat
	s.SetChatAgent(ch)
	sh, _, q := newEngineShell(t, s, Options{})
	item := mainMenuItem(t, sh, "Agent Chat")
	sc, ok := item.Shortcut.(*desktop.CustomShortcut)
	if !ok || sc.KeyName != fyne.KeyI || sc.Modifier != fyne.KeyModifierShortcutDefault {
		t.Fatalf("Agent Chat's shortcut %#v, want ⌘I", item.Shortcut)
	}
	if stop := mainMenuItem(t, sh, "Stop Agent"); stop.Shortcut != fyne.Shortcut(shortcutStop) {
		t.Errorf("Stop Agent's shortcut %#v, want ⌘.", stop.Shortcut)
	}

	item.Action()
	q.until(t, "the engine to show the column", func() bool { return s.UIState().Chat && sh.chatRow.Objects[0] == sh.chatSplit })
	q.until(t, "the input to take the focus", func() bool { return sh.win.Canvas().Focused() == sh.chat.input.entry })
	mainMenuItem(t, sh, "Agent Chat").Action()
	q.until(t, "the engine to hide the column", func() bool { return !s.UIState().Chat && sh.chatRow.Objects[0] == sh.center })
	if sh.win.Canvas().Focused() == sh.chat.input.entry {
		t.Error("the hidden input keeps the focus")
	}
}

// TestChatSendAttachesSelection: Enter sends the text with the chip of the view and selection
// (AttachSelection); without the chip (its ×) the message attaches nothing; Shift+Enter is a new
// line, not a send.
func TestChatSendAttachesSelection(t *testing.T) {
	sh, _, q, f, ch := chatScene(t, ThemeDark, nil)
	row := f.tables["pods"].Rows[3].Ref
	f.update(func(u *engine.UIState) { u.Selected = []model.ObjectRef{row} })
	in := sh.chat.input
	want := "Pods · " + row.Namespace + "/" + row.Name
	q.until(t, "the chip", func() bool { return in.chip.text.text == want && in.chip.Visible() })

	shift := false
	in.shift = func() bool { return shift }
	test.Type(in.entry, "first line")
	shift = true
	in.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	shift = false
	test.Type(in.entry, "second line")
	if len(ch.sendsOf()) != 0 || !strings.Contains(in.entry.Text, "\n") {
		t.Fatalf("Shift+Enter: sends %v, text %q; want a new line and nothing sent", ch.sendsOf(), in.entry.Text)
	}
	in.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	q.until(t, "the message", func() bool { return len(ch.sendsOf()) == 1 && len(sh.chat.views) == 1 })
	got := ch.sendsOf()[0]
	if got.msg.Text != "first line\nsecond line" || !got.msg.AttachSelection {
		t.Fatalf("sent %+v, want both lines with the selection attached", got.msg)
	}
	if in.entry.Text != "" {
		t.Errorf("the entry keeps %q after sending", in.entry.Text)
	}
	conv := sh.chat.convID
	ch.change(conv, func(cv *engine.ChatConversation) { cv.Running = false })
	q.until(t, "the turn to end", func() bool { return !sh.chat.running() })

	test.Tap(in.chip.dropButton())
	if in.chip.Visible() || !in.attachB.Visible() {
		t.Fatal("the chip's × did not drop it")
	}
	test.Type(in.entry, "no selection")
	in.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	q.until(t, "the second message", func() bool { return len(ch.sendsOf()) == 2 })
	if got := ch.sendsOf()[1]; got.msg.AttachSelection || got.id != conv {
		t.Fatalf("sent %+v to %s, want no selection to %s", got.msg, got.id, conv)
	}
	// Another selection brings the chip back.
	other := f.tables["pods"].Rows[4].Ref
	f.update(func(u *engine.UIState) { u.Selected = []model.ObjectRef{other} })
	q.until(t, "the chip to come back", func() bool { return in.chip.Visible() && in.attach })
}

// TestChatSendFailureGivesTheTextBack: a refused message comes back into the entry with the reason.
func TestChatSendFailureGivesTheTextBack(t *testing.T) {
	sh, _, q, _, ch := chatScene(t, ThemeDark, func(ch *fakeChat) string {
		return ch.add("demo", "Earlier", engine.ChatItem{Kind: engine.ChatItemUser, Text: "earlier"})
	})
	ch.sendErr = errNotSent
	in := sh.chat.input
	test.Type(in.entry, "hello")
	in.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	q.until(t, "the failure", func() bool { return in.errLine.Visible() })
	if in.entry.Text != "hello" || !strings.Contains(in.errLine.text, "HTTP 503") {
		t.Errorf("after a failed send: text %q, line %q", in.entry.Text, in.errLine.text)
	}
}

// TestChatStop: while a turn runs, the send button is Stop, and Stop, ⌘. (the menu's and the
// entry's) and Escape in the entry each call the agent's Stop.
func TestChatStop(t *testing.T) {
	sh, _, q, _, ch := chatScene(t, ThemeDark, func(ch *fakeChat) string { return demoConversation(ch, "demo") })
	in := sh.chat.input
	conv := sh.chat.convID
	if !sh.chat.running() || in.sendB.tip == "Send (Enter)" || in.sendB.Disabled() {
		t.Fatalf("a running turn: running %v, the button %q disabled %v", sh.chat.running(), in.sendB.tip, in.sendB.Disabled())
	}
	running := func() {
		ch.change(conv, func(cv *engine.ChatConversation) { cv.Running = true })
		q.until(t, "the turn to run", sh.chat.running)
	}
	stops := func(n int, how string) {
		t.Helper()
		q.until(t, how+" to stop the turn", func() bool { return len(ch.stopsOf()) == n })
		if got := ch.stopsOf()[n-1]; got != conv {
			t.Fatalf("%s stopped %q, want %q", how, got, conv)
		}
	}
	test.Tap(in.sendB)
	stops(1, "the Stop button")
	running()
	in.entry.TypedShortcut(shortcutStop)
	stops(2, "⌘. in the entry")
	running()
	mainMenuItem(t, sh, "Stop Agent").Action()
	stops(3, "View › Stop Agent")
	running()
	in.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	stops(4, "Escape in the entry")
	q.until(t, "Send again", func() bool { return !sh.chat.running() && in.sendB.tip == "Send (Enter)" })
	mainMenuItem(t, sh, "Stop Agent").Action() // nothing runs: nothing to stop
	q.drain()
	if n := len(ch.stopsOf()); n != 4 {
		t.Errorf("⌘. without a running turn stopped it: %d stops", n)
	}
	// ⌘. is the kill switch with the column hidden too.
	running()
	sh.chat.sh.ui.Chat = false
	mainMenuItem(t, sh, "Stop Agent").Action()
	stops(5, "⌘. with the column hidden")
}

// TestChatInputDisabledWithReason: the input is disabled and says why without a cluster, while its
// cluster is not connected, and for a conversation about another cluster; that conversation stays
// readable.
func TestChatInputDisabledWithReason(t *testing.T) {
	sh, _, q, f, ch := chatScene(t, ThemeDark, func(ch *fakeChat) string { return demoConversation(ch, "demo") })
	in := sh.chat.input
	if in.entry.Disabled() {
		t.Fatalf("connected to the conversation's cluster, the input is disabled: %q", in.entry.PlaceHolder)
	}
	f.update(func(u *engine.UIState) { u.Connection = engine.Connection{State: engine.Disconnected} })
	q.until(t, "the input to be disabled", in.entry.Disabled)
	if want := "Connect demo to continue"; !strings.HasPrefix(in.entry.PlaceHolder, want) {
		t.Errorf("disconnected: %q, want it to start with %q", in.entry.PlaceHolder, want)
	}
	f.update(func(u *engine.UIState) { u.Context, u.Connection = "", engine.Connection{State: engine.Disconnected} })
	q.until(t, "no cluster", func() bool { return strings.HasPrefix(in.entry.PlaceHolder, "Connect a cluster") })
	if len(sh.chat.views) == 0 {
		t.Error("without a cluster the conversation is gone; it stays readable")
	}

	other := ch.add("staging", "About staging", engine.ChatItem{Kind: engine.ChatItemUser, Text: "How many nodes?"})
	f.update(func(u *engine.UIState) {
		u.Context, u.Connection = "demo", engine.Connection{State: engine.Connected, Demo: true}
	})
	q.drain()
	sh.chat.setConversation(other)
	q.until(t, "the other cluster's conversation", func() bool { return len(sh.chat.views) == 1 })
	if !in.entry.Disabled() || !strings.Contains(in.entry.PlaceHolder, "about staging") {
		t.Errorf("a conversation about staging while demo is connected: disabled %v, %q", in.entry.Disabled(), in.entry.PlaceHolder)
	}
}

// TestChatNoConnection: without a connection the column offers to add one (Preferences › Agent)
// and the input says why it is disabled.
func TestChatNoConnection(t *testing.T) {
	f := connectedFake(t, 10)
	withChat(t, f)
	f.ui.Chat = true
	sh, _, q := newTestShell(t, f, Options{})
	q.until(t, "the connections", func() bool { return sh.chat.connsRead })
	if !sh.chat.empty.Visible() || !sh.chat.empty.addConn.Visible() {
		t.Fatal("no connection: no offer to add one")
	}
	if !sh.chat.input.entry.Disabled() || !strings.Contains(sh.chat.input.entry.PlaceHolder, "Preferences › Agent") {
		t.Errorf("no connection: input %q", sh.chat.input.entry.PlaceHolder)
	}
	test.Tap(sh.chat.empty.addConn)
	q.until(t, "Preferences › Agent", func() bool { return len(f.callsOf("preferences.open")) == 1 })
	if p := string(f.callsOf("preferences.open")[0].Params); !strings.Contains(p, `"agent"`) {
		t.Errorf("preferences.open %s, want the agent section", p)
	}
}

// TestChatEventForAnotherConversation: EventChat of a conversation not shown is not read and
// redraws nothing; the shown one's is read once and redraws it.
func TestChatEventForAnotherConversation(t *testing.T) {
	sh, _, q, _, ch := chatScene(t, ThemeDark, func(ch *fakeChat) string {
		ch.add("demo", "Older", engine.ChatItem{Kind: engine.ChatItemUser, Text: "older"})
		return ch.add("demo", "Shown", engine.ChatItem{Kind: engine.ChatItemUser, Text: "shown"})
	})
	shown := sh.chat.convID
	other := "conv-1"
	renders, reads := sh.chat.renders, ch.readsOf(other)
	ch.change(other, func(cv *engine.ChatConversation) {
		cv.Items = append(cv.Items, engine.ChatItem{Kind: engine.ChatItemAssistant, Text: "elsewhere"})
	})
	ch.stream(shown, "here")
	q.until(t, "the shown conversation's answer", func() bool { return len(sh.chat.views) == 2 })
	if ch.readsOf(other) != reads {
		t.Errorf("the other conversation was read %d times", ch.readsOf(other)-reads)
	}
	if got := sh.chat.renders - renders; got != 1 {
		t.Errorf("%d redraws, want the shown conversation's one", got)
	}
}

// TestChatItems: each item kind draws as designed: the user's card with its chip and what it sent,
// the answer's Markdown without the model's reasoning, tool rows with their states (one opened to
// its arguments and result), the note in its level's colour.
func TestChatItems(t *testing.T) {
	sh, _, q, _, _ := chatScene(t, ThemeDark, func(ch *fakeChat) string { return demoConversation(ch, "demo") })
	views := sh.chat.views
	if len(views) != 12 {
		t.Fatalf("%d views, want 12", len(views))
	}
	u := views[0].(*chatUserView)
	if u.chip.text.text != "Pods · payments/"+demoPod || !u.chip.Visible() || !strings.Contains(u.sent.text.text, "4 requests") ||
		!strings.Contains(u.sent.text.text, "localhost:1234") || !u.sent.lock.Visible() {
		t.Errorf("the user's card: chip %q, sent %q, lock %v", u.chip.text.text, u.sent.text.text, u.sent.lock.Visible())
	}
	a := views[4].(*chatAnswerView)
	var text []string
	for _, o := range a.text.objs {
		if f, ok := o.(*mdFlow); ok {
			text = append(text, f.shownText())
		}
	}
	all := strings.Join(text, "\n")
	if strings.Contains(all, "check the logs") {
		t.Errorf("the model's reasoning is shown: %q", all)
	}
	if !strings.Contains(all, "readiness probe fails") {
		t.Errorf("the answer: %q", all)
	}
	var table *mdTableView
	for _, o := range a.text.objs {
		if tv, ok := o.(*mdTableView); ok {
			table = tv
		}
	}
	if table == nil || len(table.cells) != 3 || len(table.cells[0]) != 4 {
		t.Fatalf("the answer's table: %+v", table)
	}
	states := map[engine.ChatToolState]string{}
	for _, v := range views {
		if tv, ok := v.(*chatToolView); ok {
			states[tv.item.Tool.State] = tv.state.text
		}
	}
	for st, want := range map[engine.ChatToolState]string{engine.ChatToolDone: "Done", engine.ChatToolRefused: "Refused",
		engine.ChatToolFailed: "Failed", engine.ChatToolWaiting: "Waiting for your confirmation"} {
		if states[st] != want {
			t.Errorf("tool state %s shows %q, want %q", st, states[st], want)
		}
	}
	note := views[8].(*chatNoteView)
	if note.color != colorStatusWarning {
		t.Errorf("the warning note is %s", note.color)
	}

	tool := views[1].(*chatToolView)
	h := tool.MinSize().Height
	test.Tap(tool.header)
	q.drain()
	if !tool.body.Visible() || tool.MinSize().Height <= h {
		t.Fatal("a click did not open the tool row")
	}
	var body []string
	for _, o := range tool.body.objs {
		if c, ok := o.(*mdCode); ok {
			body = append(body, c.text)
		}
	}
	if len(body) != 2 || !strings.Contains(body[0], `"namespace": "payments"`) || !strings.Contains(body[1], "restartCount") {
		t.Errorf("the opened row: %q", body)
	}
	test.Tap(tool.header)
	if tool.body.Visible() {
		t.Error("a second click did not close it")
	}
}

// TestChatSentPayload: the line under a user's message opens what its turn sent, read-only.
func TestChatSentPayload(t *testing.T) {
	sh, _, q, _, _ := chatScene(t, ThemeDark, func(ch *fakeChat) string { return demoConversation(ch, "demo") })
	u := sh.chat.views[0].(*chatUserView)
	test.Tap(u.sent)
	q.until(t, "the request body", func() bool { return sh.chat.payload != nil })
	sh.chat.payload.Hide()
}

// TestChatHistory: the history lists the cluster's conversations; one opens, one is deleted.
func TestChatHistory(t *testing.T) {
	sh, _, q, _, ch := chatScene(t, ThemeDark, func(ch *fakeChat) string {
		ch.add("demo", "First", engine.ChatItem{Kind: engine.ChatItemUser, Text: "one"})
		return ch.add("demo", "Second", engine.ChatItem{Kind: engine.ChatItemUser, Text: "two"})
	})
	test.Tap(sh.chat.historyB)
	q.until(t, "the history", func() bool { return sh.chat.history != nil && len(sh.chat.histRows) == 2 })
	test.Tap(sh.chat.histRows["conv-1"])
	q.until(t, "the first conversation", func() bool { return sh.chat.conv != nil && sh.chat.conv.Title == "First" })
	if sh.chat.history != nil {
		t.Error("the history stays open after a conversation was picked")
	}
	sh.chat.deleteConversation("conv-1")
	q.until(t, "the deletion", func() bool {
		ch.mu.Lock()
		deleted := slices.Contains(ch.deleted, "conv-1")
		ch.mu.Unlock()
		return sh.chat.convID == "" && deleted
	})
}

// TestChatHistoryMarksUnsaved (H-96): a private conversation, which lives in memory only, says so
// in the history; a kept one does not.
func TestChatHistoryMarksUnsaved(t *testing.T) {
	sh, _, q, _, _ := chatScene(t, ThemeDark, func(ch *fakeChat) string {
		ch.add("demo", "Kept", engine.ChatItem{Kind: engine.ChatItemUser, Text: "one", Sent: &engine.ChatSent{Endpoint: "litellm.example.com"}})
		return ch.add("demo", "Private", engine.ChatItem{Kind: engine.ChatItemUser, Text: "two", Sent: &engine.ChatSent{Endpoint: "localhost:1234", Private: true}})
	})
	test.Tap(sh.chat.historyB)
	q.until(t, "the history", func() bool { return sh.chat.history != nil && len(sh.chat.histRows) == 2 })
	texts := func(id string) string {
		var out []string
		for _, l := range findAll[*textLine](sh.chat.histRows[id]) {
			out = append(out, l.text)
		}
		return strings.Join(out, "|")
	}
	if got := texts("conv-2"); !strings.Contains(got, historyNotSaved+" · ") {
		t.Errorf("the private conversation's row %q does not say it is not saved", got)
	}
	if got := texts("conv-1"); strings.Contains(got, historyNotSaved) {
		t.Errorf("the kept conversation's row %q says it is not saved", got)
	}
}

// TestChatStreamingStaysCheap streams a 2,000-token answer in 10-token deltas (about 8 KiB in 200
// steps): each step lays out the answer's last block only, never the other items; the window's
// work per step is logged.
func TestChatStreamingStaysCheap(t *testing.T) {
	sh, _, q, _, ch := chatScene(t, ThemeDark, func(ch *fakeChat) string { return demoConversation(ch, "demo") })
	conv := sh.chat.convID
	before := slices.Clone(sh.chat.views)
	words := strings.Fields(strings.Repeat(demoAnswer+" ", 8))
	for len(words) < 2000 {
		words = append(words, words...)
	}
	words = words[:2000]
	sh.chat.measure = true
	ch.stream(conv, "")
	q.until(t, "the answer to start", func() bool { return len(sh.chat.views) == len(before)+1 })
	answer := sh.chat.views[len(before)].(*chatAnswerView)
	relaid := answer.text.relaid
	steps := 0
	for i := 0; i < len(words); i += 10 {
		ch.stream(conv, strings.Join(words[i:min(i+10, len(words))], " ")+" ")
		steps++
		ch.mu.Lock()
		want := ch.convs[conv].Revision
		ch.mu.Unlock()
		q.until(t, "the delta", func() bool { return sh.chat.conv.Revision >= want })
	}
	for i, v := range before {
		if sh.chat.views[i] != v {
			t.Fatalf("item %d got a new widget while the answer streamed", i)
		}
	}
	perStep := float64(answer.text.relaid-relaid) / float64(steps)
	if perStep > 2.5 {
		t.Errorf("%.1f blocks laid out per delta, want about one (the last)", perStep)
	}
	times := slices.Clone(sh.chat.applyTimes)
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	p50, p99, worst := times[len(times)/2], times[len(times)*99/100], times[len(times)-1]
	// The blocks laid out per delta are the guarantee; the wall clock is measured, not asserted (a
	// loaded runner's time is not the code's).
	t.Logf("%d deltas: the window's work per delta p50 %v, p99 %v, max %v; %.1f blocks laid out per delta", steps, p50, p99, worst, perStep)
}

// TestChatThinking: an answer that streams without text yet (the model reasons; the agent drops its
// reasoning) says "Thinking…" and that it is alive; the line goes when the answer's text comes.
func TestChatThinking(t *testing.T) {
	sh, _, q, _, ch := chatScene(t, ThemeDark, func(ch *fakeChat) string {
		return ch.add("demo", "Thinking", engine.ChatItem{Kind: engine.ChatItemUser, Text: "Why?"})
	})
	conv := sh.chat.convID
	ch.change(conv, func(cv *engine.ChatConversation) {
		cv.Running = true
		cv.Items = append(cv.Items, engine.ChatItem{Kind: engine.ChatItemAssistant, Streaming: true})
	})
	q.until(t, "the empty answer", func() bool { return len(sh.chat.views) == 2 })
	a := sh.chat.views[1].(*chatAnswerView)
	if !a.thinking.Visible() || !strings.HasPrefix(a.thinking.text, "Thinking") {
		t.Fatalf("an empty streaming answer: thinking line %v %q", a.thinking.Visible(), a.thinking.text)
	}
	q.until(t, "the dots to move", func() bool { return a.thinking.text != "Thinking…" })
	ch.stream(conv, "Because")
	q.until(t, "the answer's text", func() bool { return !a.thinking.Visible() })
	if got := thinkingText(4, 12*time.Second); got != "Thinking.. · 12 s" {
		t.Errorf("thinkingText = %q", got)
	}
}

// TestChatMarkdown: the parser's blocks for what answers use.
func TestChatMarkdown(t *testing.T) {
	test.NewTempApp(t)
	blocks := parseChatMarkdown(demoAnswer)
	var kinds []mdKind
	for _, b := range blocks {
		kinds = append(kinds, b.kind)
	}
	want := []mdKind{mdKindPara, mdKindTable, mdKindPara, mdKindCode, mdKindList, mdKindPara}
	if !slices.Equal(kinds, want) {
		t.Fatalf("blocks %v, want %v", kinds, want)
	}
	var code, link bool
	for _, r := range blocks[0].runs {
		code = code || (r.code && r.text == "GET /healthz")
	}
	for _, r := range blocks[5].runs {
		link = link || (r.link != nil && r.link.Host == "kubernetes.io")
	}
	if !code || !link {
		t.Errorf("inline code %v, link %v", code, link)
	}
	if tb := blocks[1].table; len(tb.header) != 4 || len(tb.rows) != 2 || tb.align[2] != fyne.TextAlignTrailing {
		t.Errorf("table %+v", tb)
	}
	if l := blocks[4].list; !l.ordered || len(l.items) != 2 {
		t.Errorf("list %+v", l)
	}
	if b := parseChatMarkdown("![logo](https://example.com/x.png)"); len(b) != 1 || !strings.Contains(runsText(b[0].runs), "(image: logo)") {
		t.Errorf("an image: %+v, want its alt text, not loaded", b)
	}
}

// TestStripThinking: reasoning never reaches the answer; an open block means still thinking.
func TestStripThinking(t *testing.T) {
	for _, tt := range []struct {
		in, want string
		thinking bool
	}{
		{"<think>a</think>\n\nAnswer", "Answer", false},
		{"Start <think>plan", "Start", true},
		{"<think>only", "", true},
		{"plain", "plain", false},
		{"<thinking>x</thinking>Done", "Done", false},
	} {
		got, thinking := stripThinking(tt.in)
		if got != tt.want || thinking != tt.thinking {
			t.Errorf("stripThinking(%q) = %q, %v; want %q, %v", tt.in, got, thinking, tt.want, tt.thinking)
		}
	}
}

// TestFairShare: narrow columns keep their width, wide ones share the rest.
func TestFairShare(t *testing.T) {
	got := fairShare([]float32{50, 400, 60, 300}, 400, 30)
	if got[0] != 50 || got[2] != 60 || got[1] != 145 || got[3] != 145 {
		t.Errorf("fairShare = %v", got)
	}
	if got := fairShare([]float32{50, 60}, 400, 30); got[0] != 50 || got[1] != 60 {
		t.Errorf("columns that fit: %v", got)
	}
	// A column narrower than its longest word takes room from those with more than theirs.
	if got := keepWords([]float32{100, 100, 100}, []float32{40, 130, 40}); got[1] != 130 || got[0] != 85 || got[2] != 85 {
		t.Errorf("keepWords = %v", got)
	}
}

// TestChatWidthByDraggingItsEdge: the column's left edge drags its width, kept in the preferences.
func TestChatWidthByDraggingItsEdge(t *testing.T) {
	sh, w, _, _, _ := chatScene(t, ThemeDark, nil)
	drag := func(dx float32) { // the column's own handle: the work area beside it has others
		h := sh.chatSplit.Objects[1].(*resizeHandle)
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(h).AddXY(h.Size().Width/2, 40)
		test.MoveMouse(w.Canvas(), pos)
		test.Drag(w.Canvas(), pos, dx, 0)
		settle(w)
	}
	drag(-120)
	if got := sh.chat.root.Size().Width; got != defaultChatWidth+120 {
		t.Fatalf("dragged 120 left: %v wide, want %v", got, defaultChatWidth+120)
	}
	if got := float32(sh.app.Preferences().Float(prefChatWidth)); got != defaultChatWidth+120 {
		t.Errorf("stored %v", got)
	}
	drag(5000)
	if got := sh.chat.root.Size().Width; got != minChatWidth {
		t.Errorf("dragged shut: %v, want the minimum %v", got, minChatWidth)
	}
}

// TestChatConnectionMenu: the pill names the conversation's connection and model with a lock for a
// private one; its menu switches the connection.
func TestChatConnectionMenu(t *testing.T) {
	sh, _, q, _, ch := chatScene(t, ThemeDark, func(ch *fakeChat) string { return demoConversation(ch, "demo") })
	in := sh.chat.input
	if in.conn.text.text != "LM Studio · qwen3.6-27b" || in.conn.icon != iconLock {
		t.Errorf("the pill: %q", in.conn.text.text)
	}
	ch.mu.Lock()
	ch.models["c1"] = []string{"gemma-4-31b", "qwen3.6-27b"}
	ch.mu.Unlock()
	test.Tap(in.connTap)
	q.until(t, "the menu", func() bool { return sh.chat.menu != nil })
	var labels []string
	var team *fyne.MenuItem
	for _, it := range sh.chat.menu.Items {
		labels = append(labels, it.Label)
		if strings.HasPrefix(it.Label, "Team LiteLLM") {
			team = it
		}
	}
	if team == nil || !slices.Contains(labels, "Model of LM Studio") || !slices.Contains(labels, "Manage Connections…") {
		t.Fatalf("menu %q", labels)
	}
	team.Action()
	q.until(t, "the switch", func() bool {
		ch.mu.Lock()
		n := len(ch.switched)
		ch.mu.Unlock()
		return n == 1 && sh.chat.conv.ConnectionID == "c2"
	})
	if in.conn.text.text != "Team LiteLLM · gpt-oss-120b" {
		t.Errorf("after the switch the pill says %q", in.conn.text.text)
	}
}

// TestChatWritesState: below the input, the cluster's agent writes setting, read-only; a click opens
// Cluster Settings › Agents.
func TestChatWritesState(t *testing.T) {
	sh, _, q, f, _ := chatScene(t, ThemeDark, nil)
	in := sh.chat.input
	if in.writes.text.text != "Writes ask" {
		t.Errorf("%q, want Writes ask", in.writes.text.text)
	}
	f.update(func(u *engine.UIState) {
		u.ClusterSettings = map[string]engine.ClusterSettings{"demo": {AgentWrites: true}}
	})
	q.until(t, "Writes allowed", func() bool { return in.writes.text.text == "Writes allowed" })
	sh.chat.openWritesSettings()
	q.until(t, "Cluster Settings › Agents", func() bool { return len(f.callsOf("cluster.settings.open")) == 1 })
	if p := string(f.callsOf("cluster.settings.open")[0].Params); !strings.Contains(p, `"agents"`) || !strings.Contains(p, `"demo"`) {
		t.Errorf("cluster.settings.open %s", p)
	}
}

// --- renders ---

func renderChat(t *testing.T, mode ThemeMode, name string, script func(*fakeChat) string, after func(*shell, fyne.Window, *mainQueue)) {
	sh, w, q, _, _ := chatScene(t, mode, script)
	if after != nil {
		after(sh, w, q)
	}
	settle(w)
	savePNG(t, w, fmt.Sprintf("%s_%s.png", name, mode))
}

// TestRenderChatEmpty: the column of a new conversation, with examples.
func TestRenderChatEmpty(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		t.Run(string(mode), func(t *testing.T) { renderChat(t, mode, "chat_empty", nil, nil) })
	}
}

// TestRenderChatConversation: the first turn: the user's card with what it attached and sent, three
// tool calls, the Markdown answer with a table, inline code, a code block, a list and a link; the
// input shows Stop for the running third turn.
func TestRenderChatConversation(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		t.Run(string(mode), func(t *testing.T) {
			renderChat(t, mode, "chat_conversation", func(ch *fakeChat) string { return demoConversation(ch, "demo") },
				func(sh *shell, w fyne.Window, q *mainQueue) {
					settle(w)
					sh.chat.scroll.ScrollToTop()
					sh.chat.updatePin()
				})
		})
	}
}

// TestRenderChatHistory: the history of the cluster's conversations: the shown one (private, its
// turns ran over the local connection) bold and marked not saved, two kept ones.
func TestRenderChatHistory(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		t.Run(string(mode), func(t *testing.T) {
			renderChat(t, mode, "chat_history", func(ch *fakeChat) string {
				team := &engine.ChatSent{Requests: 1, Endpoint: "litellm.example.com"}
				ch.add("demo", "How many pods run in payments?", engine.ChatItem{Kind: engine.ChatItemUser, Text: "How many pods run in payments?", Sent: team})
				ch.add("demo", "Which images does ledger-writer run?", engine.ChatItem{Kind: engine.ChatItemUser, Text: "Which images does ledger-writer run?", Sent: team})
				return demoConversation(ch, "demo")
			}, func(sh *shell, w fyne.Window, q *mainQueue) {
				test.Tap(sh.chat.historyB)
				q.until(t, "the history", func() bool { return sh.chat.history != nil && len(sh.chat.histRows) == 3 })
			})
		})
	}
}

// TestRenderChatRunning: scrolled to the end: the pinned message, a refused call and a failed one
// opened to its error, the limit's note, a call waiting for the user's confirmation, and Stop.
func TestRenderChatRunning(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark} {
		t.Run(string(mode), func(t *testing.T) {
			renderChat(t, mode, "chat_running", func(ch *fakeChat) string { return demoConversation(ch, "demo") },
				func(sh *shell, w fyne.Window, q *mainQueue) {
					test.Tap(sh.chat.views[7].(*chatToolView).header) // the failed call, opened
					q.drain()
					settle(w)
					sh.chat.scroll.ScrollToBottom()
					sh.chat.updatePin()
				})
		})
	}
}

// TestChatTimingText (the owner's request of 2026-10-25): an answer shows its TTFT and OTPS beside
// its copy icon, ≈ where the tokens are estimated, only TTFT where the speed cannot be told.
func TestChatTimingText(t *testing.T) {
	for _, c := range []struct {
		t    *engine.ChatTiming
		want string
	}{
		{nil, ""},
		{&engine.ChatTiming{TTFT: 840 * time.Millisecond, Generating: 4200 * time.Millisecond, OutputTokens: 177}, "TTFT 840 ms · OTPS 42.1"},
		{&engine.ChatTiming{TTFT: 1260 * time.Millisecond, Generating: 900 * time.Millisecond, OutputTokens: 31, Estimated: true}, "TTFT 1.3 s · OTPS ≈34.4"},
		{&engine.ChatTiming{TTFT: 2 * time.Second, OutputTokens: 12}, "TTFT 2.0 s"},
	} {
		if got := chatTimingText(c.t); got != c.want {
			t.Errorf("%+v: %q, want %q", c.t, got, c.want)
		}
	}
}
