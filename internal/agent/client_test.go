package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// TestThinkFilter: <think> blocks are removed however the stream splits them; a lone closing tag
// (a template that opened the block in the prompt) removes what came before it; a held-back
// partial tag that never completes is text.
func TestThinkFilter(t *testing.T) {
	for _, tc := range []struct {
		pieces  []string
		want    string
		thought bool
	}{
		{[]string{"Hello"}, "Hello", false},
		{[]string{"<think>plan</think>Answer"}, "Answer", true},
		{[]string{"<th", "ink>pl", "an</thi", "nk>\n\nAn", "swer"}, "Answer", true},
		{[]string{"plan only\n</think>\n\nAnswer"}, "Answer", true},
		{[]string{"pl", "an</", "think>Answer"}, "Answer", true},
		{[]string{"a < b and x <th"}, "a < b and x <th", false},
		{[]string{"<think>never closed"}, "", true},
		{[]string{"<think>a</think>B</think>C"}, "B</think>C", true}, // a second closing tag is text
		{[]string{"Use <thinking> tags"}, "Use <thinking> tags", false},
	} {
		var f thinkFilter
		for _, p := range tc.pieces {
			f.write(p)
		}
		f.flush()
		if got := f.text(); got != tc.want || f.thought != tc.thought {
			t.Errorf("%q: %q thought %v, want %q %v", tc.pieces, got, f.thought, tc.want, tc.thought)
		}
	}
}

// TestReadSSE: events of several data lines, comments, CRLF, [DONE], an event without a blank line
// at the end.
func TestReadSSE(t *testing.T) {
	in := ": keep-alive\r\ndata: {\"a\":\r\ndata: 1}\r\n\r\nevent: x\ndata: second\n\ndata: last"
	var got []string
	n := 0
	err := readSSE(strings.NewReader(in), func(d []byte) error { got = append(got, string(d)); return nil }, func(k int) { n += k })
	if err != nil || len(got) != 3 || got[0] != "{\"a\":\n1}" || got[1] != "second" || got[2] != "last" || n == 0 || n > len(in)+1 {
		t.Errorf("%q %v, %d bytes of %d", got, err, n, len(in))
	}
	err = readSSE(strings.NewReader("data: x\n\ndata: [DONE]\n\ndata: y\n\n"), func([]byte) error { return nil }, func(int) {})
	if !errors.Is(err, errStreamDone) {
		t.Errorf("[DONE]: %v", err)
	}
}

// TestAccumulatorToolCalls: deltas by index, by id without an index, a name repeated in every
// delta, ids filled in.
func TestAccumulatorToolCalls(t *testing.T) {
	a := newAccumulator(nil)
	for _, ev := range []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"b","function":{"name":"two","arguments":"{\"x\""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"one","arguments":"{}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"name":"two","arguments":":1}"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"id":"c","function":{"name":"three","arguments":"{\"y\""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":":2}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3}}`,
	} {
		if err := a.event([]byte(ev), ""); err != nil {
			t.Fatal(err)
		}
	}
	r := a.result()
	var got []string
	for _, c := range r.ToolCalls {
		got = append(got, c.ID+" "+c.Function.Name+" "+c.Function.Arguments)
	}
	want := []string{`call_0 one {}`, `b two {"x":1}`, `c three {"y":2}`}
	if strings.Join(got, "|") != strings.Join(want, "|") || r.FinishReason != "tool_calls" || r.Usage.total() != 10 {
		t.Errorf("%q, finish %q, usage %+v", got, r.FinishReason, r.Usage)
	}
	if err := a.event([]byte(`{"error":{"message":"overloaded key sk-1"}}`), "sk-1"); err == nil ||
		!strings.Contains(err.Error(), "overloaded") || strings.Contains(err.Error(), "sk-1") {
		t.Errorf("stream error: %v", err)
	}
}

// TestToolsetSize measures the tools a request carries: every command as a tool against the core
// set and the meta-tools (D-10), and keeps the core set small; GUI-only commands and chat.toggle
// are offered neither as tools nor by search_commands nor run_command.
func TestToolsetSize(t *testing.T) {
	s := engine.New(engine.Options{})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	ts, err := newToolset(s.Registry())
	if err != nil {
		t.Fatal(err)
	}
	all, n := 0, 0
	for _, c := range s.Registry().Commands() {
		if d, err := commandTool(c); err == nil {
			b, _ := json.Marshal(d)
			all += len(b)
			n++
		}
	}
	t.Logf("every command as a tool: %d tools, %d bytes (~%d tokens); the chat may run %d (%d bytes); "+
		"a request carries %d tools, %d bytes (~%d tokens)", n, all, estimateTokens(all), ts.fullN, ts.full, len(ts.defs), ts.bytes, estimateTokens(ts.bytes))
	if len(ts.defs) != len(coreCommands)+2 || ts.bytes > 40<<10 {
		t.Errorf("the core set: %d tools, %d bytes", len(ts.defs), ts.bytes)
	}
	// Sending all tools (ChatConnection.AllTools): every command the chat may run. Preferences ›
	// Agent's hint (allToolsHelp in ui/fyneui/preferences_agent.go) names some 200 tools and about
	// 70,000 tokens: fix the hint when this fails.
	if ts.fullN != len(ts.all) || ts.fullN < 180 || ts.fullN > 220 || estimateTokens(ts.full) < 60_000 || estimateTokens(ts.full) > 80_000 {
		t.Errorf("all tools: %d of %d commands, %d bytes (~%d tokens): Preferences › Agent's hint no longer holds",
			ts.fullN, len(ts.all), ts.full, estimateTokens(ts.full))
	}
	for _, c := range s.Registry().Commands() {
		if !chatExcluded(c) {
			continue
		}
		if _, err := ts.resolve(toolRun, `{"command":"`+c.ID+`"}`); err == nil {
			t.Errorf("run_command resolves %s", c.ID)
		}
		for _, e := range ts.search(searchArgs{Query: c.ID, Limit: 20}).Commands {
			if e.ID == c.ID {
				t.Errorf("search_commands offers %s", c.ID)
			}
		}
	}
	if _, err := ts.resolve(toolRun, `{"command":"statefulset_scale","params":{"replicas":1}}`); err != nil {
		t.Errorf("run_command by tool name: %v", err)
	}
	if _, err := ts.resolve("ui_state", `{not json`); err == nil {
		t.Error("invalid JSON accepted")
	}
	if all := ts.search(searchArgs{}); len(all.Commands) != ts.fullN {
		t.Errorf("an empty query lists %d of %d", len(all.Commands), ts.fullN)
	}
}

// TestCanonicalArgsAndSummary: the loop guard's key ignores key order and spacing; the summary names
// the object.
func TestCanonicalArgsAndSummary(t *testing.T) {
	if a, b := canonicalArgs(`{"b":2,"a":{"y":1,"x":[1, 2]}}`), canonicalArgs(` { "a" : {"x":[1,2],"y":1}, "b": 2 } `); a != b {
		t.Errorf("%s != %s", a, b)
	}
	if got := callSummary(json.RawMessage(`{"ref":{"kind":"pods","namespace":"shop","name":"web-0"}}`)); got != "pods shop/web-0" {
		t.Errorf("summary %q", got)
	}
	if got := callSummary(json.RawMessage(`{"kind":"pods","limit":5}`)); got != "kind=pods limit=5" {
		t.Errorf("summary %q", got)
	}
	if got := conversationTitle("  How many\npods run in the namespace shop of the staging cluster right now, please tell me? "); got !=
		"How many pods run in the namespace shop of the staging…" {
		t.Errorf("title %q", got)
	}
}
