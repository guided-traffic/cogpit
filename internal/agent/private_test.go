package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit/fakellm"
)

// A private conversation is never kept (H-96, the rule at the conversation type in
// conversation.go).

// privateConnection adds a connection the user marked private, to the harness's fake endpoint.
func (h *harness) privateConnection(t *testing.T) engine.ChatConnection {
	t.Helper()
	c, err := h.a.SaveConnection(engine.ChatConnection{Name: "local", Kind: engine.ChatLMStudio, BaseURL: h.llm.Base,
		Model: "test-model", Private: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// readKept reads the conversations file back and checks that it is owner-only.
func readKept(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, ConversationsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows sets no file modes: there the user profile's permissions protect the file.
	if fi, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Errorf("the conversations file: %v, mode %v", err, fi.Mode().Perm())
	}
	return string(data)
}

var readSecretCall = fakellm.ToolCall{Name: "object_get", Args: `{"ref":{"kind":"secrets","namespace":"shop","name":"db"}}`}

// TestPrivateConversationIsNotKept: the file holds the conversation over the connection not marked
// private, scrubbed as before, and nothing of the private one, though its turn read the Secret's
// value; the history lists both, the private one marked; a new agent (Cogpit started again) has
// only the kept one.
func TestPrivateConversationIsNotKept(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	private := h.privateConnection(t)

	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{readSecretCall}}, fakellm.Reply{Text: []string{"It has a password key."}})
	kept := h.idle(t, h.start(t, "Is "+secretValue+" the password in the db Secret?"))

	priv, err := h.a.NewConversation("test", private.ID)
	if err != nil {
		t.Fatal(err)
	}
	if priv.Private {
		t.Error("a conversation without a turn is private")
	}
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{readSecretCall}}, fakellm.Reply{Text: []string{"The password is " + secretValue + "."}})
	if err := h.a.Send(priv.ID, engine.ChatUserMessage{Text: "A private question about the db Secret"}); err != nil {
		t.Fatal(err)
	}
	c := h.idle(t, priv.ID)
	reqs := h.llm.ChatRequests()
	if !strings.Contains(string(reqs[len(reqs)-1].Body), secretValue) {
		t.Fatal("the private turn did not read the Secret's value (H-94): the test proves nothing")
	}
	if !c.Private || kept.Private {
		t.Errorf("private: the private conversation %v, the kept one %v", c.Private, kept.Private)
	}

	data := readKept(t, h.dir)
	for _, absent := range []string{priv.ID, secretValue, "A private question", "The password is"} {
		if strings.Contains(data, absent) {
			t.Errorf("the conversations file holds %q of the private conversation", absent)
		}
	}
	if !strings.Contains(data, kept.ID) || !strings.Contains(data, "the password in the db Secret?") ||
		!strings.Contains(data, "«secret shop/db/password»") {
		t.Errorf("the conversation not private is not kept scrubbed:\n%s", data)
	}

	infos := h.a.Conversations("test")
	if len(infos) != 2 || infos[0].ID != priv.ID || !infos[0].Private || infos[1].Private {
		t.Errorf("history %+v, want the private conversation listed and marked", infos)
	}

	b := New(Options{Session: h.s, Dir: h.dir, Keys: h.keys, NotifyEvery: time.Millisecond})
	if _, ok := b.Conversation(priv.ID); ok {
		t.Error("the private conversation outlived the agent")
	}
	if _, ok := b.Conversation(kept.ID); !ok {
		t.Error("the kept conversation was not restored")
	}
}

// TestPrivateTurnUnkeepsTheConversationForGood: a conversation kept while its turns ran over a
// connection not marked private stays kept when it switches to a private connection, until a turn
// runs there: from that turn's start the file no longer holds it, and switching back to a
// connection not marked private and going on does not keep it again, since the next turn sends the
// private turn's history.
func TestPrivateTurnUnkeepsTheConversationForGood(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	private := h.privateConnection(t)
	h.llm.Script(fakellm.Reply{Text: []string{"Hello."}})
	id := h.start(t, "First turn, not private")
	h.idle(t, id)
	if !strings.Contains(readKept(t, h.dir), id) {
		t.Fatal("the conversation over a connection not marked private is not kept")
	}

	if err := h.a.SetConversationConnection(id, private.ID); err != nil {
		t.Fatal(err)
	}
	if c, _ := h.a.Conversation(id); c.Private {
		t.Error("switching to a private connection alone made the conversation private")
	}
	if !strings.Contains(readKept(t, h.dir), id) {
		t.Error("a private connection without a turn yet took the conversation out of the file")
	}

	started, gate := make(chan struct{}), make(chan struct{})
	h.llm.Script(fakellm.Reply{Gate: gate, Started: started, Text: []string{"Private answer " + secretValue}})
	if err := h.a.Send(id, engine.ChatUserMessage{Text: "Second turn, private"}); err != nil {
		t.Fatal(err)
	}
	recv(t, started, "the private turn's stream")
	if data := readKept(t, h.dir); strings.Contains(data, id) || strings.Contains(data, "First turn") {
		t.Error("the file still holds the conversation while its private turn runs")
	}
	close(gate)
	h.idle(t, id)

	if err := h.a.SetConversationConnection(id, h.conn.ID); err != nil {
		t.Fatal(err)
	}
	h.llm.Script(fakellm.Reply{Text: []string{"Third."}})
	if err := h.a.Send(id, engine.ChatUserMessage{Text: "Third turn, not private"}); err != nil {
		t.Fatal(err)
	}
	c := h.idle(t, id)
	if !c.Private {
		t.Error("the conversation is no longer private after it switched back")
	}
	reqs := h.llm.ChatRequests()
	if !strings.Contains(string(reqs[len(reqs)-1].Body), "Second turn, private") {
		t.Error("the third turn did not send the private turn's history: the rule's reason does not hold")
	}
	data := readKept(t, h.dir)
	for _, absent := range []string{id, "First turn", "Second turn", "Third turn", secretValue} {
		if strings.Contains(data, absent) {
			t.Errorf("the conversations file holds %q", absent)
		}
	}
}

// keptByOldVersions is a conversations file as v0.8.0 to v0.10.0 wrote it: a conversation over a
// connection not marked private, one over a private connection that read Helm's values, one whose
// second turn ran over a private connection, and one with the private mark on its info (written by
// no version, dropped too). Every value is made up.
const keptByOldVersions = `{"version":1,"conversations":[
{"id":"kept1","title":"Pods in shop","context":"test","connectionID":"c-team","updated":"2026-10-14T21:00:00Z",
 "items":[{"kind":"user","text":"How many pods run in shop?","sent":{"requests":1,"bytes":900,"messages":2,"toolResults":0,"endpoint":"litellm.example.com"}},
  {"kind":"assistant","text":"Two pods run in shop."}],
 "history":[{"role":"user","content":"How many pods run in shop?"},{"role":"assistant","content":"Two pods run in shop."}]},
{"id":"private1","title":"Values of the release shop","context":"test","connectionID":"c-local","updated":"2026-10-14T22:00:00Z",
 "items":[{"kind":"user","text":"Show the values of the release shop","sent":{"requests":2,"bytes":2100,"messages":4,"toolResults":1,"endpoint":"localhost:1234","private":true}},
  {"kind":"tool","tool":{"callID":"call_0","command":"helm.release.values","label":"Values","args":"{}","summary":"shop","state":"done","result":"db:\n  password: fake-helm-pw-0000"}},
  {"kind":"assistant","text":"The database password is fake-helm-pw-0000."}],
 "history":[{"role":"user","content":"Show the values of the release shop"},
  {"role":"assistant","content":"","tool_calls":[{"id":"call_0","type":"function","function":{"name":"helm_release_values","arguments":"{}"}}]},
  {"role":"tool","tool_call_id":"call_0","content":"Result of helm_release_values: db:\n  password: fake-helm-pw-0000"},
  {"role":"assistant","content":"The database password is fake-helm-pw-0000."}]},
{"id":"mixed1","title":"Flux sources","context":"test","connectionID":"c-team","updated":"2026-10-14T23:00:00Z",
 "items":[{"kind":"user","text":"Which sources fail?","sent":{"requests":1,"bytes":800,"messages":2,"toolResults":0,"endpoint":"litellm.example.com"}},
  {"kind":"assistant","text":"None."},
  {"kind":"user","text":"And the token of the git source?","sent":{"requests":1,"bytes":900,"messages":4,"toolResults":0,"endpoint":"localhost:1234","private":true}},
  {"kind":"assistant","text":"It is fake-flux-token-1111."}],
 "history":[{"role":"user","content":"Which sources fail?"},{"role":"assistant","content":"None."},
  {"role":"user","content":"And the token of the git source?"},{"role":"assistant","content":"It is fake-flux-token-1111."}]},
{"id":"flagged1","title":"Flagged","context":"test","connectionID":"c-team","updated":"2026-10-14T20:00:00Z","private":true,
 "items":[{"kind":"user","text":"Flagged text","sent":{"requests":1,"bytes":100,"messages":2,"toolResults":0,"endpoint":"litellm.example.com"}}],
 "history":[{"role":"user","content":"Flagged text"}]}
]}`

// TestLoadDropsPrivateConversations: a file an earlier version wrote with private conversations
// loses them when the agent starts: they are not restored, and the file is rewritten at once
// (read back before anything else happens: the private turns' texts and tool results are gone,
// the kept conversation is there, the file owner-only). A file without one is not rewritten.
func TestLoadDropsPrivateConversations(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	dir := t.TempDir()
	path := filepath.Join(dir, ConversationsFile)
	if err := os.WriteFile(path, []byte(keptByOldVersions), 0o600); err != nil {
		t.Fatal(err)
	}

	b := New(Options{Session: h.s, Dir: dir, Keys: h.keys, NotifyEvery: time.Millisecond})
	if infos := b.Conversations("test"); len(infos) != 1 || infos[0].ID != "kept1" || infos[0].Private {
		t.Errorf("restored %+v, want kept1 only", infos)
	}
	data := readKept(t, dir)
	for _, absent := range []string{"private1", "mixed1", "flagged1", "fake-helm-pw-0000", "fake-flux-token-1111",
		"Show the values", "Which sources fail?", "Flagged text", "helm_release_values"} {
		if strings.Contains(data, absent) {
			t.Errorf("the rewritten file holds %q", absent)
		}
	}
	if !strings.Contains(data, `"id":"kept1"`) || !strings.Contains(data, "Two pods run in shop.") {
		t.Errorf("the kept conversation is not in the rewritten file:\n%s", data)
	}

	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	c := New(Options{Session: h.s, Dir: dir, Keys: h.keys, NotifyEvery: time.Millisecond})
	if _, ok := c.Conversation("kept1"); !ok {
		t.Error("the rewritten file's conversation was not restored")
	}
	if after, err := os.Stat(path); err != nil || !os.SameFile(before, after) {
		t.Errorf("a file without a private conversation was rewritten on load (%v)", err)
	}
}
