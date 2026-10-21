package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit/fakellm"
)

// F9 (the owner's decision of 2026-10-15): a user's message edited and sent again, everything
// after it dropped from the window, the model's history and the kept file; the audit log keeps it.

// TestResendCutsTheConversation: resending the second message drops it, its write and its answer
// from the items, from what the model is sent next (nothing tells the model about them) and from
// the conversations file, at once; the audit log keeps the write; the first turn and the title stay.
func TestResendCutsTheConversation(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	h.llm.Script(
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "resources_list", Args: `{"kind":"pods","namespaces":["shop"]}`}}},
		fakellm.Reply{Text: []string{"Two pods run in shop."}},
	)
	id := h.start(t, "How many pods run in shop?")
	h.idle(t, id)
	h.llm.Script(
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "deployment_restart", Args: `{"ref":{"kind":"deployments","namespace":"shop","name":"web"}}`}}},
		fakellm.Reply{Text: []string{"Restarted web for you."}},
	)
	if err := h.a.Send(id, engine.ChatUserMessage{Text: "Restart web please"}); err != nil {
		t.Fatal(err)
	}
	recv(t, h.conf.asked, "the confirmation")
	h.conf.answer <- true
	before := h.idle(t, id)
	second := slices.IndexFunc(before.Items, func(it engine.ChatItem) bool { return it.Text == "Restart web please" })
	if second < 0 || !strings.Contains(readKept(t, h.dir), "Restarted web for you.") {
		t.Fatalf("the second turn: %v (%s)", itemKinds(before), notes(before))
	}

	// The gate holds the new turn open while the test reads the file: Windows refuses to replace a
	// file that is open, so the turn's save at its end fails when it comes during the read.
	gate := make(chan struct{})
	h.llm.Script(fakellm.Reply{Text: []string{"Which namespace?"}, Gate: gate})
	if err := h.a.Resend(id, second, engine.ChatUserMessage{Text: "Scale web to three"}); err != nil {
		t.Fatal(err)
	}
	// At once, while the new turn runs, the file holds nothing of the cut part.
	if kept := readKept(t, h.dir); strings.Contains(kept, "Restart web please") || strings.Contains(kept, "Restarted web") ||
		strings.Contains(kept, "deployment.restart") {
		t.Errorf("the file still holds the cut part:\n%s", kept)
	}
	close(gate)
	c := h.idle(t, id)
	want := []string{"user", "tool:resources.list:done", "assistant", "user", "assistant"}
	if got := itemKinds(c); !slices.Equal(got, want) {
		t.Fatalf("items %v, want %v (%s)", got, want, notes(c))
	}
	if c.Items[3].Text != "Scale web to three" || c.Items[4].Text != "Which namespace?" || c.Title != "How many pods run in shop?" {
		t.Errorf("after the resend: %q, %q, title %q", c.Items[3].Text, c.Items[4].Text, c.Title)
	}
	reqs := h.llm.ChatRequests()
	msgs := messages(t, reqs[len(reqs)-1])
	sent, _ := json.Marshal(msgs) // the messages, not the tools offered
	for _, cut := range []string{"Restart web please", "deployment_restart", "Restarted web"} {
		if strings.Contains(string(sent), cut) {
			t.Errorf("the model is still sent %q", cut)
		}
	}
	var roles []string
	for _, m := range msgs {
		roles = append(roles, m["role"].(string))
	}
	if !slices.Equal(roles, []string{"system", "user", "assistant", "tool", "assistant", "user"}) ||
		!strings.HasSuffix(msgs[len(msgs)-1]["content"].(string), "Scale web to three") {
		t.Errorf("the history sent: %v", roles)
	}
	if kept := readKept(t, h.dir); strings.Contains(kept, "Restart web please") || !strings.Contains(kept, "Scale web to three") ||
		!strings.Contains(kept, "Two pods run in shop.") {
		t.Errorf("the kept conversation:\n%s", kept)
	}
	if body, ok := h.a.SentPayload(id, 4); !ok || body != string(reqs[len(reqs)-1].Body) {
		t.Errorf("SentPayload of the new turn: %v", ok)
	}
	if a := h.audit.String(); !strings.Contains(a, `"command":"deployment.restart"`) {
		t.Errorf("the audit log lost the cut part's write:\n%s", a)
	}
	again := New(Options{Session: h.s, Dir: h.dir, Keys: h.keys})
	if got, ok := again.Conversation(id); !ok || len(got.Items) != 5 {
		t.Errorf("restored: %v, %v", ok, itemKinds(got))
	}
}

// TestResendRetriesAFailedTurn: a turn that failed is retried by sending its message again
// unchanged: the failure's note goes, the new turn answers; sending the first message again with
// other words renames the conversation.
func TestResendRetriesAFailedTurn(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	h.llm.Script(fakellm.Reply{Status: 503})
	id := h.start(t, "Why is web slow?")
	if c := h.idle(t, id); !slices.Equal(itemKinds(c), []string{"user", "note"}) {
		t.Fatalf("the failed turn: %v", itemKinds(c))
	}
	h.llm.Script(fakellm.Reply{Text: []string{"It is not."}})
	if err := h.a.Resend(id, 0, engine.ChatUserMessage{Text: "Why is web slow?"}); err != nil {
		t.Fatal(err)
	}
	c := h.idle(t, id)
	if !slices.Equal(itemKinds(c), []string{"user", "assistant"}) || c.Title != "Why is web slow?" || c.Items[0].Sent.Requests != 1 {
		t.Fatalf("the retry: %v (%s), title %q", itemKinds(c), notes(c), c.Title)
	}
	h.llm.Script(fakellm.Reply{Text: []string{"Fine."}})
	if err := h.a.Resend(id, 0, engine.ChatUserMessage{Text: "Is db healthy?"}); err != nil {
		t.Fatal(err)
	}
	if c := h.idle(t, id); c.Title != "Is db healthy?" || len(c.Items) != 2 {
		t.Errorf("the first message edited: title %q, %v", c.Title, itemKinds(c))
	}
}

// TestResendTakesTheWindowOfNow: the resent message carries the chip and the ui_state of the moment
// it is sent again, not the edited message's.
func TestResendTakesTheWindowOfNow(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	h.llm.Script(fakellm.Reply{Text: []string{"Which pod?"}})
	id := h.start(t, "Is this pod healthy?")
	if c := h.idle(t, id); c.Items[0].Attached != "" {
		t.Fatalf("attached %q before anything was shown", c.Items[0].Attached)
	}
	ref, _ := json.Marshal(map[string]any{"ref": map[string]string{"kind": "pods", "namespace": "shop", "name": "web-0"}})
	if _, err := h.s.Dispatch(context.Background(), engine.Invocation{ID: "ui.navigate", Origin: engine.OriginGUI, Params: ref}); err != nil {
		t.Fatal(err)
	}
	h.llm.Script(fakellm.Reply{Text: []string{"Yes."}})
	if err := h.a.Resend(id, 0, engine.ChatUserMessage{Text: "Is this pod healthy?", AttachSelection: true}); err != nil {
		t.Fatal(err)
	}
	c := h.idle(t, id)
	if !strings.HasSuffix(c.Items[0].Attached, "shop/web-0") {
		t.Errorf("the chip %q", c.Items[0].Attached)
	}
	reqs := h.llm.ChatRequests()
	if user := messages(t, reqs[len(reqs)-1])[1]["content"].(string); !strings.Contains(user, `"drawer":{`) || !strings.Contains(user, "web-0") {
		t.Errorf("the resent message's ui_state: %s", user)
	}
}

// TestResendRefusals: a running turn, an item that is no user's message, an empty text, another
// cluster and no cluster are refused, and the conversation and its file stay as they were.
func TestResendRefusals(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	h.llm.Script(fakellm.Reply{Text: []string{"Hello."}})
	id := h.start(t, "hi")
	before := h.idle(t, id)
	kept := readKept(t, h.dir)

	for item, want := range map[int]error{-1: ErrNotUserMessage, 1: ErrNotUserMessage, 2: ErrNotUserMessage} {
		if err := h.a.Resend(id, item, engine.ChatUserMessage{Text: "x"}); !errors.Is(err, want) {
			t.Errorf("item %d: %v", item, err)
		}
	}
	if err := h.a.Resend(id, 0, engine.ChatUserMessage{Text: "  "}); !errors.Is(err, ErrEmptyMessage) {
		t.Errorf("an empty text: %v", err)
	}
	if err := h.a.Resend("nope", 0, engine.ChatUserMessage{Text: "x"}); !errors.Is(err, ErrUnknownConv) {
		t.Errorf("an unknown conversation: %v", err)
	}
	if after, _ := h.a.Conversation(id); !slices.Equal(itemKinds(after), itemKinds(before)) || readKept(t, h.dir) != kept {
		t.Errorf("a refused resend changed the conversation: %v", itemKinds(after))
	}

	gate, started := make(chan struct{}), make(chan struct{})
	h.llm.Script(fakellm.Reply{Text: []string{"slow"}, Gate: gate, Started: started})
	if err := h.a.Send(id, engine.ChatUserMessage{Text: "and?"}); err != nil {
		t.Fatal(err)
	}
	recv(t, started, "the gated answer")
	if err := h.a.Resend(id, 0, engine.ChatUserMessage{Text: "x"}); !errors.Is(err, ErrTurnRunning) {
		t.Errorf("while a turn runs: %v", err)
	}
	close(gate)
	before = h.idle(t, id)
	kept = readKept(t, h.dir)

	other, _ := h.a.NewConversation("other", "")
	if err := h.a.Resend(other.ID, 0, engine.ChatUserMessage{Text: "x"}); !errors.Is(err, ErrOtherContext) {
		t.Errorf("another cluster's conversation: %v", err)
	}
	if err := h.s.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if err := h.a.Resend(id, 0, engine.ChatUserMessage{Text: "x"}); !errors.Is(err, ErrNotConnected) {
		t.Errorf("without a cluster: %v", err)
	}
	after, _ := h.a.Conversation(id)
	if !slices.Equal(itemKinds(after), itemKinds(before)) || after.Items[0].Text != "hi" || readKept(t, h.dir) != kept {
		t.Errorf("a refused resend changed the conversation: %v", itemKinds(after))
	}
}

// TestResendKeepsAPrivateConversationPrivate: cutting the private turn does not make the
// conversation kept again (it stays memory-only for good, H-96).
func TestResendKeepsAPrivateConversationPrivate(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	private := h.privateConnection(t)
	c, err := h.a.NewConversation("test", private.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{readSecretCall}}, fakellm.Reply{Text: []string{"Read it."}})
	if err := h.a.Send(c.ID, engine.ChatUserMessage{Text: "Read the db Secret"}); err != nil {
		t.Fatal(err)
	}
	h.idle(t, c.ID)
	if err := h.a.SetConversationConnection(c.ID, h.conn.ID); err != nil {
		t.Fatal(err)
	}
	h.llm.Script(fakellm.Reply{Text: []string{"Hello."}})
	if err := h.a.Resend(c.ID, 0, engine.ChatUserMessage{Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	got := h.idle(t, c.ID)
	if !got.Private || strings.Contains(readKept(t, h.dir), c.ID) {
		t.Errorf("private %v; the file holds it: %v", got.Private, strings.Contains(readKept(t, h.dir), c.ID))
	}
}
