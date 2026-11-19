package agent

import (
	"slices"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit/fakellm"
)

// toolNames lists a chat request's tools by name.
func toolNames(r fakellm.Request) []string {
	var out []string
	tools, _ := r.JSON()["tools"].([]any)
	for _, tl := range tools {
		out = append(out, tl.(map[string]any)["function"].(map[string]any)["name"].(string))
	}
	return out
}

// TestAllToolsSendsEveryCommand: a connection set to send all tools (the owner's decision of
// 2026-10-15) carries every command the chat may run as a tool of its own, the core ones first,
// without the meta-tools that find and run a command, with result_read, and a system prompt that
// says so; the rules are the same: a command
// outside the core set reads through the agents' filter (no Secret value reaches the endpoint),
// and one that asks agents waits for the user. The setting is kept with the connection; off, the
// core set goes as before.
func TestAllToolsSendsEveryCommand(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	conn := h.conn
	conn.AllTools = true
	saved, err := h.a.SaveConnection(conn, nil)
	if err != nil || !saved.AllTools {
		t.Fatalf("saved %+v, %v", saved, err)
	}
	if again := New(Options{Session: h.s, Dir: h.dir, Keys: h.keys}); !again.conns[0].AllTools {
		t.Fatal("the setting was not kept with the connection")
	}
	ts, err := h.a.toolset()
	if err != nil {
		t.Fatal(err)
	}

	prom := fakellm.ToolCall{Name: "cluster_setPrometheus", Args: `{"address":"monitoring/prometheus:9090"}`}
	h.llm.Script(
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "object_get", Args: `{"ref":{"kind":"secrets","namespace":"shop","name":"db"}}`},
			{Name: "notifications_list", Args: `{}`}}},
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{prom}},
		fakellm.Reply{Text: []string{"Not set."}},
	)
	id := h.start(t, "Use monitoring/prometheus:9090")
	req := recv(t, h.conf.asked, "the confirmation")
	if req.Command != "cluster.setPrometheus" || req.Origin != engine.OriginBuiltInAgent {
		t.Errorf("confirmation %+v", req)
	}
	h.conf.answer <- false
	c := h.idle(t, id)
	want := []string{"user", "tool:object.get:done", "tool:notifications.list:done", "tool:cluster.setPrometheus:refused", "assistant"}
	if got := itemKinds(c); !slices.Equal(got, want) {
		t.Fatalf("items %v, want %v (%s)", got, want, notes(c))
	}

	reqs := h.llm.ChatRequests()
	names := toolNames(reqs[0])
	if len(names) != ts.fullN+1 || ts.fullN != len(ts.all) || slices.Contains(names, toolSearch) || slices.Contains(names, toolRun) ||
		slices.Contains(names, "chat_toggle") || slices.Contains(names, "secret_reveal") || !slices.Contains(names, "cluster_setPrometheus") ||
		names[len(names)-1] != toolRead {
		t.Errorf("%d tools (want %d, every command the chat may run, and result_read): %v", len(names), ts.fullN+1, names)
	}
	for i, id := range coreCommands {
		if c, ok := h.s.Registry().Get(id); ok && names[i] != c.ToolName() {
			t.Errorf("tool %d is %s, want the core set first (%s)", i, names[i], c.ToolName())
		}
	}
	prompt := messages(t, reqs[0])[0]["content"].(string)
	if prompt != systemPromptAll || strings.Contains(prompt, toolSearch) || !strings.Contains(prompt, promptLarge) || !strings.Contains(systemPrompt, promptLarge) {
		t.Errorf("system prompt %q", prompt)
	}
	for _, r := range h.llm.Requests() {
		if strings.Contains(string(r.Body), secretValue) {
			t.Fatal("a request carried the Secret's value")
		}
	}

	conn.AllTools = false
	if _, err := h.a.SaveConnection(conn, nil); err != nil {
		t.Fatal(err)
	}
	h.llm.Script(fakellm.Reply{Text: []string{"ok"}})
	h.idle(t, h.start(t, "again"))
	reqs = h.llm.ChatRequests()
	if names := toolNames(reqs[len(reqs)-1]); len(names) != len(coreCommands)+3 || !slices.Contains(names, toolSearch) {
		t.Errorf("all tools off: %d tools %v", len(names), names)
	}
}
