package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/fakellm"
)

// result_read in a turn (rawresult.go): the harness's cluster with so many pods that their list is
// above a connection's limit of 1 KiB.

// listCall is the model's call for the pods, under an id of its own: the first 100, as the list's
// default has it. listAllCall asks for up to 1,000.
var (
	listCall    = fakellm.ToolCall{ID: "call_list", Name: "resources_list", Args: `{"kind":"pods"}`}
	listAllCall = fakellm.ToolCall{ID: "call_list", Name: "resources_list", Args: `{"kind":"pods","limit":1000}`}
)

// readCall is a result_read call with these arguments.
func readCall(id, args string) fakellm.ToolCall {
	return fakellm.ToolCall{ID: id, Name: toolRead, Args: args}
}

// addPods adds n pods to the harness's cluster, every fourth one pending, waits until the cache
// lists them and returns the list as the agent's resources.list returns it.
func (h *harness) addPods(t *testing.T, n int) engine.RowTable {
	t.Helper()
	for i := range n {
		pod := testkit.Pod("bulk", fmt.Sprintf("job-%03d", i))
		if i%4 == 3 {
			pod = testkit.Object("v1", "Pod", "bulk", fmt.Sprintf("job-%03d", i), map[string]any{"status": map[string]any{"phase": "Pending"}})
		}
		h.src.Set(testkit.PodsGVR, pod)
	}
	var list struct {
		Total int `json:"total"`
		engine.RowTable
	}
	h.waitFor(t, "the pods to be listed", func() bool {
		res, err := h.s.Dispatch(context.Background(), engine.Invocation{ID: "resources.list", Params: json.RawMessage(listAllCall.Args),
			Origin: engine.OriginBuiltInAgent})
		if err != nil {
			return false
		}
		data, _ := json.Marshal(res)
		list.RowTable = engine.RowTable{} // decoded anew: a table used again keeps the last one's shared cells
		return json.Unmarshal(data, &list) == nil && list.Total == n+3 && len(list.Rows) == n+3
	})
	return list.RowTable
}

// lastToolMessage is the content of the newest tool message the endpoint was sent.
func (h *harness) lastToolMessage(t *testing.T) string {
	t.Helper()
	reqs := h.llm.ChatRequests()
	sent := toolContents(t, reqs[len(reqs)-1])
	if len(sent) == 0 {
		t.Fatal("the last request carries no tool message")
	}
	return sent[len(sent)-1]
}

// send sends text in the conversation and waits for the turn's end.
func (h *harness) send(t *testing.T, id, text string) engine.ChatConversation {
	t.Helper()
	if err := h.a.Send(id, engine.ChatUserMessage{Text: text}); err != nil {
		t.Fatal(err)
	}
	return h.idle(t, id)
}

// raw returns the raw result the conversation holds under a tool call's id, and how many it holds.
func (h *harness) raw(id, call string) (*rawResult, int) {
	h.a.mu.Lock()
	defer h.a.mu.Unlock()
	c := h.a.convs[id]
	if c == nil {
		return nil, 0
	}
	return c.raws[call], len(c.raws)
}

// TestLargeResultIsReadOn: a list above the limit reaches the model as a head, valid JSON within
// the limit with exact totals; the raw result is held and the conversations file has nothing of it
// beyond the head; result_read pages to the last row, each row once, as tool items of their own,
// stamped with the time the list was read, in the session's format, however much later the page
// is asked for; a restarted Cogpit holds no raw result and says to call the tool again.
func TestLargeResultIsReadOn(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{MaxToolResultKiB: 1})
	table := h.addPods(t, 90)
	listed := time.Date(2026, 9, 10, 17, 41, 1, 0, time.UTC)
	h.clock.Store(&listed)
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{listCall}}, fakellm.Reply{Text: []string{"Many pods."}})
	id := h.start(t, "Which pods are there?")
	c := h.idle(t, id)
	if got := itemKinds(c); !slices.Equal(got, []string{"user", "tool:resources.list:done", "assistant"}) {
		t.Fatalf("items %v (%s)", got, notes(c))
	}

	frame := resultFrame("resources_list")
	sent := h.lastToolMessage(t)
	shown := strings.TrimPrefix(sent, frame)
	doc, note := splitHead(t, shown)
	var head engine.RowTable
	if err := json.Unmarshal([]byte(doc), &head); err != nil {
		t.Fatal(err)
	}
	k := len(head.Rows)
	if !strings.HasPrefix(sent, frame) || len(shown) > 1<<10 || k == 0 || k >= 93 || !reflect.DeepEqual(head.Rows, table.Rows[:k]) ||
		!slices.Equal(head.Columns, table.Columns) || !reflect.DeepEqual(head.Same, table.Same) || !strings.Contains(doc, `"total":93`) {
		t.Fatalf("the head: %d bytes, %d rows: %s", len(shown), k, shown)
	}
	levels := map[string]int{}
	for i := range table.Rows {
		levels[table.Cell(i, engine.RowLevel)]++
	}
	if len(levels) < 2 {
		t.Fatalf("the pods' levels %v: the test needs two", levels)
	}
	for level, n := range levels {
		if !strings.Contains(note, fmt.Sprintf("%s %d", level, n)) {
			t.Errorf("the note lacks the total %s %d: %q", level, n, note)
		}
	}
	next, ok := nextRead(t, note)
	if !ok || next != (readOn{Call: "call_list", Offset: k}) || !strings.Contains(note, fmt.Sprintf(`the first %d of the 93 elements of "rows". Counted over all 93: `, k)) {
		t.Errorf("the note %q", note)
	}
	if c.Items[1].Tool.Result != shown {
		t.Error("the window's result is not the head the model was sent")
	}
	raw, held := h.raw(id, "call_list")
	if raw == nil || held != 1 || raw.private || raw.tool != "resources_list" || !strings.Contains(string(raw.data), "job-089") || !raw.read.Equal(listed) {
		t.Fatalf("the raw result held: %+v, %d held", raw, held)
	}
	lastPod := table.Cell(92, engine.RowName)
	if kept := readKept(t, h.dir); strings.Contains(kept, lastPod) || !strings.Contains(kept, table.Cell(0, engine.RowName)) {
		t.Errorf("the conversations file: the head's first pod kept %v, the raw result's last pod kept %v",
			strings.Contains(kept, table.Cell(0, engine.RowName)), strings.Contains(kept, lastPod))
	}

	// Page by page, a turn each, from the offset each note names.
	rows := head.Rows
	stamp := ", as read at " + h.s.TimeFormat().Format(listed) + "."
	if strings.Contains(stamp, listed.Format(time.RFC3339)) || !strings.Contains(stamp, "41:01") {
		t.Fatalf("the session writes the time as %q: the test needs a format of its own", stamp)
	}
	for n := 0; ok; n++ {
		if n > 93 {
			t.Fatal("the pages do not end")
		}
		later := listed.Add(time.Duration(n+1) * 7 * time.Minute)
		h.clock.Store(&later)
		h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall(fmt.Sprintf("call_read_%d", n), fmt.Sprintf(`{"call":"call_list","offset":%d}`, next.Offset))}},
			fakellm.Reply{Text: []string{"More pods."}})
		c = h.send(t, id, "Go on")
		sent = h.lastToolMessage(t)
		shown = strings.TrimPrefix(sent, resultFrame(toolRead))
		doc, note = splitHead(t, shown)
		var page engine.RowTable
		if err := json.Unmarshal([]byte(doc), &page); err != nil {
			t.Fatal(err)
		}
		tool := c.Items[len(c.Items)-2].Tool
		if !strings.HasPrefix(sent, resultFrame(toolRead)) || len(shown) > 1<<10 || len(page.Rows) == 0 || !slices.Equal(page.Columns, table.Columns) ||
			!strings.Contains(note, `the 93 elements of "rows" in the result of resources_list`+stamp) {
			t.Fatalf("page %d: %d bytes, %d rows: %s", n, len(shown), len(page.Rows), shown)
		}
		if tool == nil || tool.Command != toolRead || tool.Label != readLabel || tool.State != engine.ChatToolDone || tool.Result != shown ||
			!strings.Contains(tool.Summary, "call=call_list") {
			t.Fatalf("page %d's tool item: %+v", n, tool)
		}
		rows = append(rows, page.Rows...)
		prev := next.Offset
		if next, ok = nextRead(t, note); ok && next.Offset != prev+len(page.Rows) {
			t.Fatalf("page %d from %d has %d rows, and the note leads on to %d", n, prev, len(page.Rows), next.Offset)
		}
	}
	if !reflect.DeepEqual(rows, table.Rows) {
		t.Errorf("the head and the pages returned %d rows, want the list's 93, each once and in order", len(rows))
	}
	if _, held := h.raw(id, "call_list"); held != 1 {
		t.Errorf("%d raw results held: result_read's own results are not held", held)
	}
	if kept := readKept(t, h.dir); strings.Contains(kept, string(raw.data[len(raw.data)/2:])) {
		t.Error("the conversations file holds the raw result")
	}

	// Cogpit started again: the conversation is there, its raw result is not.
	h.stopAll()
	a2 := New(Options{Session: h.s, Dir: h.dir, Keys: h.keys, NotifyEvery: time.Millisecond})
	t.Cleanup(func() { a2.Stop(id) })
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("call_late", `{"call":"call_list","offset":5}`)}}, fakellm.Reply{Text: []string{"Gone."}})
	if err := a2.Send(id, engine.ChatUserMessage{Text: "And the rest?"}); err != nil {
		t.Fatal(err)
	}
	var c2 engine.ChatConversation
	h.waitFor(t, "the restarted agent's turn", func() bool {
		c2, _ = a2.Conversation(id)
		return !c2.Running && len(c2.Items) > len(c.Items)
	})
	tool := c2.Items[len(c2.Items)-2].Tool
	if sent = h.lastToolMessage(t); !strings.HasPrefix(sent, `Error of result_read: Cogpit holds no result of call "call_list"`) ||
		!strings.Contains(sent, "call the tool again") || strings.Contains(sent, "Held now") ||
		tool == nil || tool.State != engine.ChatToolFailed || tool.Error == "" {
		t.Errorf("after a restart: %q, item %+v", sent, tool)
	}
}

// TestResultReadIsAToolCall: result_read counts against the turn's tool calls; an id that holds no
// result is answered with what to do and the ids that hold one; arguments a model writes loosely
// are taken; a limit bounds the page.
func TestResultReadIsAToolCall(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{MaxToolResultKiB: 1, MaxToolCalls: 3})
	table := h.addPods(t, 60)
	h.llm.Script(
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{listCall}},
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r1", `{"call":"call_lost"}`)}},
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r2", `{"call":"call_list","offset":"3","limit":2}`)}},
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r3", `{"call":"call_list","offset":5}`)}},
	)
	id := h.start(t, "Which pods are there?")
	c := h.idle(t, id)
	want := []string{"user", "tool:resources.list:done", "tool:result_read:failed", "tool:result_read:done", "tool:result_read:stopped", "note"}
	if got := itemKinds(c); !slices.Equal(got, want) || !strings.Contains(notes(c), "limit of 3 tool calls") {
		t.Fatalf("items %v, want %v (%s)", got, want, notes(c))
	}
	reqs := h.llm.ChatRequests()
	sent := toolContents(t, reqs[len(reqs)-1])
	if want := `Error of result_read: Cogpit holds no result of call "call_lost" (it holds only results too large to show whole, in memory, ` +
		`the newest): call the tool again, with a filter or a limit. Held now: "call_list" (resources_list).`; sent[1] != want {
		t.Errorf("an id that holds no result:\n %s\nwant\n %s", sent[1], want)
	}
	doc, note := splitHead(t, strings.TrimPrefix(sent[2], resultFrame(toolRead)))
	var page engine.RowTable
	if err := json.Unmarshal([]byte(doc), &page); err != nil {
		t.Fatal(err)
	}
	if next, _ := nextRead(t, note); !reflect.DeepEqual(page.Rows, table.Rows[3:5]) || next != (readOn{Call: "call_list", Offset: 5, Limit: 2}) ||
		!strings.HasPrefix(note, `[Cogpit: elements 3 to 4 of the 63 elements of "rows" in the result of resources_list, as read at `) {
		t.Errorf("the page of two rows from 3: %s\n%s", doc, note)
	}
	if _, held := h.raw(id, "call_list"); held != 1 {
		t.Errorf("%d raw results held, want the list's alone", held)
	}
}

// TestPrivateRawResultStaysPrivate: the raw result a turn over a private connection read is not
// handed to a turn over a connection that is not private, and not named to it; a private turn
// reads it.
func TestPrivateRawResultStaysPrivate(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{MaxToolResultKiB: 1})
	private, err := h.a.SaveConnection(engine.ChatConnection{Name: "local", Kind: engine.ChatLMStudio, BaseURL: h.llm.Base,
		Model: "test-model", Private: true, Limits: engine.ChatLimits{MaxToolResultKiB: 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.addPods(t, 60)
	conv, err := h.a.NewConversation("test", private.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := conv.ID
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{listCall}}, fakellm.Reply{Text: []string{"Many pods."}})
	h.send(t, id, "Which pods are there?")
	if raw, _ := h.raw(id, "call_list"); raw == nil || !raw.private {
		t.Fatalf("the private turn's raw result: %+v", raw)
	}

	if err := h.a.SetConversationConnection(id, h.conn.ID); err != nil {
		t.Fatal(err)
	}
	h.llm.Script(
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r1", `{"call":"call_list","offset":40}`)}},
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r2", `{"call":"call_lost"}`)}},
		fakellm.Reply{Text: []string{"Not for me."}},
	)
	c := h.send(t, id, "Go on")
	reqs := h.llm.ChatRequests()
	sent := toolContents(t, reqs[len(reqs)-1])
	refused, lost := sent[len(sent)-2], sent[len(sent)-1]
	if !strings.HasPrefix(refused, `Error of result_read: the result of call "call_list" was read over a private connection`) ||
		!strings.Contains(refused, "call the tool again") || strings.Contains(refused, "job-0") {
		t.Errorf("a turn not private asked for the private turn's raw result: %q", refused)
	}
	if !strings.Contains(lost, `Cogpit holds no result of call "call_lost"`) || strings.Contains(lost, "Held now") {
		t.Errorf("a turn not private is named the private turn's raw result: %q", lost)
	}
	if got := itemKinds(c); !slices.Equal(got[len(got)-3:], []string{"tool:result_read:failed", "tool:result_read:failed", "assistant"}) {
		t.Errorf("items %v", got)
	}

	if err := h.a.SetConversationConnection(id, private.ID); err != nil {
		t.Fatal(err)
	}
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r3", `{"call":"call_list","offset":40}`)}}, fakellm.Reply{Text: []string{"More pods."}})
	h.send(t, id, "Go on, privately")
	if sent := h.lastToolMessage(t); !strings.HasPrefix(sent, resultFrame(toolRead)) || !strings.Contains(sent, "[Cogpit: elements 40 to ") {
		t.Errorf("the private turn's page: %q", sent)
	}
}

// TestCutDropsRawResults: a message edited and sent again takes the raw results of the calls it
// cuts with it; clearing the context's conversations takes them with the conversations.
func TestCutDropsRawResults(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{MaxToolResultKiB: 1})
	h.addPods(t, 60)
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{{ID: "call_first", Name: "resources_list", Args: `{"kind":"pods"}`}}},
		fakellm.Reply{Text: []string{"Many pods."}})
	id := h.start(t, "Which pods are there?")
	h.idle(t, id)
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{listCall}}, fakellm.Reply{Text: []string{"Still many."}})
	before := h.send(t, id, "And now?")
	second := slices.IndexFunc(before.Items, func(it engine.ChatItem) bool { return it.Text == "And now?" })
	if _, held := h.raw(id, "call_list"); held != 2 || second < 0 {
		t.Fatalf("%d raw results held after two lists", held)
	}

	h.llm.Script(
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r1", `{"call":"call_list","offset":40}`)}},
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r2", `{"call":"call_first","offset":40}`)}},
		fakellm.Reply{Text: []string{"The first list only."}},
	)
	if err := h.a.Resend(id, second, engine.ChatUserMessage{Text: "The rest of both, please"}); err != nil {
		t.Fatal(err)
	}
	h.idle(t, id)
	reqs := h.llm.ChatRequests()
	sent := toolContents(t, reqs[len(reqs)-1])
	if cut, kept := sent[len(sent)-2], sent[len(sent)-1]; !strings.HasPrefix(cut, `Error of result_read: Cogpit holds no result of call "call_list"`) ||
		!strings.Contains(cut, `Held now: "call_first" (resources_list).`) || !strings.HasPrefix(kept, resultFrame(toolRead)) {
		t.Errorf("after the cut: the cut call's result %q, the kept call's %q", cut, kept)
	}
	if raw, held := h.raw(id, "call_first"); raw == nil || held != 1 {
		t.Errorf("%d raw results held after the cut, want the first turn's", held)
	}

	if err := h.a.ClearConversations("test"); err != nil {
		t.Fatal(err)
	}
	if raw, held := h.raw(id, "call_first"); raw != nil || held != 0 {
		t.Error("a raw result outlived its conversation")
	}
}

// TestClearedHeadKeepsItsNote (D-167): a head that leaves what is sent is replaced by a note that
// names result_read, and result_read still reads the raw result; when the raw result goes later,
// the note stays as it is, so that the request begins as the one before, and result_read says to
// call the tool again.
func TestClearedHeadKeepsItsNote(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{MaxToolResultKiB: 2})
	table := h.addPods(t, 90)
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{listCall}}, fakellm.Reply{Text: []string{"Many pods."}})
	id := h.start(t, "Which pods are there?")
	h.idle(t, id)
	head := h.lastToolMessage(t)

	// The window made so small that the conversation is at 80 % of it: every result goes.
	reqs := h.llm.ChatRequests()
	conn := h.conn
	conn.Limits = engine.ChatLimits{MaxToolResultKiB: 2, ContextWindow: estimateTokens(len(reqs[len(reqs)-1].Body)) * 100 / 80, ClearAt: 70, ClearTo: 1}
	if _, err := h.a.SaveConnection(conn, nil); err != nil {
		t.Fatal(err)
	}
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r1", `{"call":"call_list","offset":0,"limit":1}`)}}, fakellm.Reply{Text: []string{"The first."}})
	c := h.send(t, id, "The first pod again?")
	if !strings.Contains(notes(c), "Cogpit removed 1 old tool result") {
		t.Fatalf("the second turn's notes: %q", notes(c))
	}
	reqs = h.llm.ChatRequests()
	second := messages(t, reqs[len(reqs)-1])
	sent := toolContents(t, reqs[len(reqs)-1])
	if want := fmt.Sprintf("Result of resources_list: removed by Cogpit to keep the conversation within the model's context (it had %d bytes). "+
		`Read it with result_read {"call":"call_list","offset":0}, or call the tool again.`, len(head)); sent[0] != want {
		t.Errorf("the cleared head is sent as\n %s\nwant\n %s", sent[0], want)
	}
	if doc, _ := splitHead(t, strings.TrimPrefix(sent[1], resultFrame(toolRead))); !strings.Contains(doc, `"`+table.Cell(0, engine.RowName)+`"`) {
		t.Errorf("result_read after the clearing: %q", sent[1])
	}

	// The raw result goes, as the conversation's bound lets the oldest go.
	h.a.mu.Lock()
	h.a.convs[id].dropRaw("call_list")
	h.a.mu.Unlock()
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r2", `{"call":"call_list","offset":1,"limit":1}`)}}, fakellm.Reply{Text: []string{"Gone."}})
	h.send(t, id, "And the second?")
	reqs = h.llm.ChatRequests()
	third := messages(t, reqs[len(reqs)-1])
	if len(third) <= len(second) || !reflect.DeepEqual(second, third[:len(second)]) {
		t.Error("the request after the raw result went does not begin with the messages of the one before")
	}
	if sent := h.lastToolMessage(t); !strings.HasPrefix(sent, `Error of result_read: Cogpit holds no result of call "call_list"`) {
		t.Errorf("result_read after the raw result went: %q", sent)
	}
}

// TestRepeatedCallIDs: an endpoint that gives the calls of every answer the same ids does not mix
// two raw results up: the second one is held under the id with a number, its note names that, and
// each name reads its own result.
func TestRepeatedCallIDs(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{MaxToolResultKiB: 1})
	h.addPods(t, 60)
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "resources_list", Args: `{"kind":"pods"}`}}}, fakellm.Reply{Text: []string{"Many pods."}})
	id := h.start(t, "Which pods are there?")
	h.idle(t, id)
	_, note := splitHead(t, strings.TrimPrefix(h.lastToolMessage(t), resultFrame("resources_list")))
	if next, _ := nextRead(t, note); next.Call != "call_0" {
		t.Fatalf("the first list's note names %q: %s", next.Call, note)
	}
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "resources_list", Args: `{"kind":"pods","namespaces":["bulk"]}`}}},
		fakellm.Reply{Text: []string{"Sixty in bulk."}})
	h.send(t, id, "And in bulk?")
	_, note = splitHead(t, strings.TrimPrefix(h.lastToolMessage(t), resultFrame("resources_list")))
	if next, _ := nextRead(t, note); next.Call != "call_0#2" {
		t.Fatalf("the second list came under the first one's id, and its note names %q: %s", next.Call, note)
	}

	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("", `{"call":"call_0","limit":1}`), readCall("", `{"call":"call_0#2","limit":1}`)}},
		fakellm.Reply{Text: []string{"Both."}})
	h.send(t, id, "The first of each?")
	reqs := h.llm.ChatRequests()
	sent := toolContents(t, reqs[len(reqs)-1])
	if all, bulk := sent[len(sent)-2], sent[len(sent)-1]; !strings.Contains(all, ` of the 63 elements of "rows" in the result of resources_list`) ||
		!strings.Contains(bulk, ` of the 60 elements of "rows" in the result of resources_list`) {
		t.Errorf("the pages of the two lists:\n %s\n %s", all, bulk)
	}
}

// TestSmallResultIsNotHeld: a result within the limit goes whole, without a note, and is not held:
// result_read on its call is answered with the calls that hold one.
func TestSmallResultIsNotHeld(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{MaxToolResultKiB: 1})
	h.addPods(t, 60)
	h.llm.Script(
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{{ID: "call_small", Name: "resources_list", Args: `{"kind":"pods","namespaces":["infra"]}`}, listCall}},
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r1", `{"call":"call_small"}`)}},
		fakellm.Reply{Text: []string{"One in infra."}},
	)
	id := h.start(t, "Which pods are there?")
	c := h.idle(t, id)
	want := []string{"user", "tool:resources.list:done", "tool:resources.list:done", "tool:result_read:failed", "assistant"}
	if got := itemKinds(c); !slices.Equal(got, want) {
		t.Fatalf("items %v, want %v (%s)", got, want, notes(c))
	}
	reqs := h.llm.ChatRequests()
	sent := toolContents(t, reqs[len(reqs)-1])
	small := strings.TrimPrefix(sent[0], resultFrame("resources_list"))
	if !json.Valid([]byte(small)) || len(small) > 1<<10 || !strings.Contains(small, `"db-0"`) || strings.Contains(sent[0], "[Cogpit") {
		t.Fatalf("the small result as sent: %q", sent[0])
	}
	if raw, held := h.raw(id, "call_small"); raw != nil || held != 1 {
		t.Errorf("the small result is held: %v; %d results held, want the large one alone", raw != nil, held)
	}
	if raw, _ := h.raw(id, "call_list"); raw == nil {
		t.Error("the large result is not held")
	}
	if want := `Error of result_read: Cogpit holds no result of call "call_small" (it holds only results too large to show whole, in memory, ` +
		`the newest): call the tool again, with a filter or a limit. Held now: "call_list" (resources_list).`; sent[2] != want {
		t.Errorf("result_read on the small result's call:\n %s\nwant\n %s", sent[2], want)
	}
}

// TestResultReadKeepsTheTurnsLimits: result_read is under the turn's stops as every tool call is:
// the third identical one is not answered, and none is once the window left the conversation's
// cluster.
func TestResultReadKeepsTheTurnsLimits(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{MaxToolResultKiB: 1})
	h.addPods(t, 60)
	same := `{"call":"call_list","offset":0,"limit":1}`
	h.llm.Script(
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{listCall}},
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r1", same)}},
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r2", `{"limit":1, "offset":0, "call":"call_list"}`)}},
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r3", same)}},
	)
	id := h.start(t, "Which pods are there?")
	c := h.idle(t, id)
	want := []string{"user", "tool:resources.list:done", "tool:result_read:done", "tool:result_read:done", "tool:result_read:stopped", "note"}
	if got := itemKinds(c); !slices.Equal(got, want) || !strings.Contains(notes(c), "the same tool call a third time") {
		t.Fatalf("items %v, want %v (%s)", got, want, notes(c))
	}
	if sent := h.lastHistory(id); !strings.HasPrefix(sent, "Error of result_read: not run: ") || strings.Contains(sent, "job-0") {
		t.Errorf("the third identical result_read was answered: %q", sent)
	}

	// The window leaves the cluster while the model's answer with the call streams in.
	gate, started := make(chan struct{}), make(chan struct{})
	h.llm.Script(fakellm.Reply{Text: []string{"Reading on."}, ToolCalls: []fakellm.ToolCall{readCall("r4", `{"call":"call_list","offset":7}`)}, Gate: gate, Started: started})
	before := len(c.Items)
	if err := h.a.Send(id, engine.ChatUserMessage{Text: "Go on"}); err != nil {
		t.Fatal(err)
	}
	recv(t, started, "the answer to begin")
	if err := h.s.Disconnect(); err != nil {
		t.Fatal(err)
	}
	close(gate)
	c = h.idle(t, id)
	if got := itemKinds(c)[before:]; !slices.Equal(got, []string{"user", "assistant", "tool:result_read:stopped", "note"}) ||
		!strings.Contains(c.Items[len(c.Items)-1].Text, "no longer connected to this conversation's cluster") {
		t.Errorf("items %v (%s)", got, notes(c))
	}
	if sent := h.lastHistory(id); !strings.HasPrefix(sent, "Error of result_read: not run: ") || strings.Contains(sent, "job-0") {
		t.Errorf("result_read was answered after the window left the cluster: %q", sent)
	}
}

// lastHistory is the content of the conversation's newest history message.
func (h *harness) lastHistory(id string) string {
	h.a.mu.Lock()
	defer h.a.mu.Unlock()
	hist := h.a.convs[id].history
	return hist[len(hist)-1].Content
}

// TestRawResultStaysOutOfTheFile (the owner's decision: memory only): the conversations file has
// the members a conversation is kept with and no other, and stays a fraction of the raw result's
// size, after the turn that read it and after pages of it.
func TestRawResultStaysOutOfTheFile(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{MaxToolResultKiB: 1})
	h.addPods(t, 400)
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{listAllCall}}, fakellm.Reply{Text: []string{"Many pods."}})
	id := h.start(t, "Which pods are there?")
	h.idle(t, id)
	raw, _ := h.raw(id, "call_list")
	if raw == nil || len(raw.data) < 16<<10 {
		t.Fatalf("the raw result: %v", raw)
	}
	check := func(when string, most int) {
		t.Helper()
		kept := readKept(t, h.dir)
		if len(kept) > most {
			t.Errorf("%s the conversations file has %d bytes beside a raw result of %d: want at most %d", when, len(kept), len(raw.data), most)
		}
		var file map[string]json.RawMessage
		var convs []map[string]json.RawMessage
		if err := json.Unmarshal([]byte(kept), &file); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(file["conversations"], &convs); err != nil || len(convs) != 1 || len(file) != 2 || file["version"] == nil {
			t.Fatalf("%s the file's members: %v, %d conversations (%v)", when, keysOf(file), len(convs), err)
		}
		if got, want := keysOf(convs[0]), []string{"connectionID", "context", "history", "id", "items", "title", "updated"}; !slices.Equal(got, want) {
			t.Errorf("%s a kept conversation has the members %v, want %v", when, got, want)
		}
		var history []map[string]json.RawMessage
		_ = json.Unmarshal(convs[0]["history"], &history)
		for _, m := range history {
			for _, k := range keysOf(m) {
				if !slices.Contains([]string{"role", "content", "tool_calls", "tool_call_id"}, k) {
					t.Errorf("%s a kept message has the member %q", when, k)
				}
			}
		}
	}
	check("after the list", len(raw.data)/3)
	for n, offset := range []int{9, 200} {
		h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall(fmt.Sprintf("r%d", n), fmt.Sprintf(`{"call":"call_list","offset":%d}`, offset))}},
			fakellm.Reply{Text: []string{"More pods."}})
		h.send(t, id, "Go on")
	}
	check("after two pages", len(raw.data)*2/3)
}

// keysOf lists a JSON object's members, sorted.
func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// terminalWindow is a window whose terminal tab shows these lines.
type terminalWindow []string

func (w terminalWindow) ReadDockOutput(_ context.Context, tab engine.DockTab, q engine.DockOutputQuery) (engine.DockOutput, error) {
	lines := []string(w)
	if q.Limit > 0 && q.Limit < len(lines) {
		lines = lines[len(lines)-q.Limit:]
	}
	return engine.DockOutput{Tab: tab, DockPage: engine.DockPage{Lines: lines, First: 1, Last: int64(len(lines))}, Shown: len(w)}, nil
}

// TestTerminalOutputIsNotHeld (D-44, H-83): a terminal tab is read only while the user's switch is
// on, each read through dock.read. Its output above the limit goes as a head that names no
// result_read, nothing of it is held, and so no result_read returns a line of it, with the switch
// on or off.
func TestTerminalOutputIsNotHeld(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	lines := make([]string, 2000)
	for i := range lines {
		lines[i] = fmt.Sprintf("$ echo line %04d of the terminal's scrollback, long enough to fill a result", i)
	}
	lines[1990] = "$ export TOKEN=late-s3cr3t-in-the-scrollback"
	h.s.SetDockOutputReader(terminalWindow(lines))
	h.s.UpdateUI(func(u *engine.UIState) {
		u.Dock = append(u.Dock, engine.DockTab{ID: "terminal-1", Type: engine.DockTerminal, Title: "Terminal 1", Context: "test"})
	})
	if _, err := h.s.AllowAgentRead("terminal-1", true); err != nil {
		t.Fatal(err)
	}
	dock := fakellm.ToolCall{ID: "call_dock", Name: "dock_read", Args: `{"id":"terminal-1","limit":2000}`}
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{dock}},
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r1", `{"call":"call_dock","contains":"TOKEN"}`)}},
		fakellm.Reply{Text: []string{"A long scrollback."}})
	id := h.start(t, "What does the terminal show?")
	c := h.idle(t, id)
	if got := itemKinds(c); !slices.Equal(got, []string{"user", "tool:dock.read:done", "tool:result_read:failed", "assistant"}) {
		t.Fatalf("items %v (%s)", got, notes(c))
	}
	reqs := h.llm.ChatRequests()
	sent := toolContents(t, reqs[len(reqs)-1])
	doc, note := splitHead(t, strings.TrimPrefix(sent[0], resultFrame("dock_read")))
	if len(doc) > 32<<10 || !strings.Contains(doc, "line 0000") || strings.Contains(sent[0], "TOKEN") || strings.Contains(sent[0], toolRead) ||
		!regexp.MustCompile(`^\[Cogpit: this shows the first \d+ of the 2000 elements of "lines"\. The rest of the \d+ bytes is not held: ask the tool for less`).MatchString(note) {
		t.Errorf("the terminal's output as sent: %s", note)
	}
	if _, held := h.raw(id, "call_dock"); held != 0 {
		t.Errorf("%d raw results held of a terminal tab's output", held)
	}
	if !strings.HasPrefix(sent[1], `Error of result_read: Cogpit holds no result of call "call_dock"`) || strings.Contains(sent[1], "TOKEN") {
		t.Errorf("result_read on the terminal's output: %q", sent[1])
	}

	// The switch off: dock.read is refused, and result_read has nothing to hand out.
	if _, err := h.s.AllowAgentRead("terminal-1", false); err != nil {
		t.Fatal(err)
	}
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{{ID: "call_dock2", Name: "dock_read", Args: `{"id":"terminal-1","limit":2000}`},
		readCall("r2", `{"call":"call_dock","contains":"TOKEN"}`)}}, fakellm.Reply{Text: []string{"Not mine to read."}})
	h.send(t, id, "And now?")
	for _, r := range h.llm.ChatRequests() {
		if strings.Contains(string(r.Body), "late-s3cr3t") {
			t.Fatal("a request carried a line of the terminal that no dock.read page showed")
		}
	}
	sent = toolContents(t, h.llm.ChatRequests()[len(h.llm.ChatRequests())-1])
	if refused, read := sent[len(sent)-2], sent[len(sent)-1]; !strings.Contains(refused, "agents may not read this terminal tab") ||
		!strings.HasPrefix(read, `Error of result_read: Cogpit holds no result of call "call_dock"`) {
		t.Errorf("with the switch off: %q, %q", refused, read)
	}
}

// TestPagesOfAnInterruptedReadSaySo (D-58): a result read from the cache while the connection was
// interrupted carries Cogpit's note of that after its head, and every page of it does, whenever it
// is asked for.
func TestPagesOfAnInterruptedReadSaySo(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{MaxToolResultKiB: 1})
	h.addPods(t, 60)
	since := time.Date(2026, 9, 10, 17, 30, 0, 0, time.UTC)
	interrupt := func(state engine.ConnectionState) {
		h.s.UpdateUI(func(u *engine.UIState) { u.Connection.State, u.Connection.Since = state, since })
	}
	interrupt(engine.Interrupted)
	stale := h.s.StaleNote()
	if !strings.Contains(stale, "interrupted since 2026-09-10T17:30:00Z") {
		t.Fatalf("the session's note of the interruption: %q", stale)
	}
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{listCall}}, fakellm.Reply{Text: []string{"Many pods, as of the interruption."}})
	id := h.start(t, "Which pods are there?")
	h.idle(t, id)
	if head := h.lastToolMessage(t); !strings.HasSuffix(head, `; add "contains" to search all of it.]`+"\n"+stale) {
		t.Fatalf("the head read during the interruption ends %q", head[max(0, len(head)-200):])
	}
	if raw, _ := h.raw(id, "call_list"); raw == nil || raw.stale != stale {
		t.Fatalf("the raw result's note of the interruption: %+v", raw)
	}

	interrupt(engine.Connected)
	h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{readCall("r1", `{"call":"call_list","offset":50}`)}}, fakellm.Reply{Text: []string{"The last ones."}})
	c := h.send(t, id, "Go on")
	page := h.lastToolMessage(t)
	shown, after, _ := strings.Cut(strings.TrimPrefix(page, resultFrame(toolRead)), "]\n")
	if after != stale || !regexp.MustCompile(`\[Cogpit: elements 50 to \d+ of the 63 elements`).MatchString(shown) || h.s.StaleNote() != "" {
		t.Errorf("the page of a result read during the interruption: %q", page[max(0, len(page)-400):])
	}
	if tool := c.Items[len(c.Items)-2].Tool; tool == nil || tool.Result != shown+"]" {
		t.Errorf("the window's page: %+v", tool)
	}
}

// TestCutElementIsScrubbedWhenKept (H-82): an element shown cut keeps its text escaped as the
// result has it, so the filter of what is kept finds a Secret's value in it, quotes and all.
func TestCutElementIsScrubbedWhenKept(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	const quoted = `pw"Quote\Scrub-4711-abcdef`
	h.src.Set(testkit.SecretsGVR, testkit.Secret("shop", "quoted", nil, map[string]string{"password": quoted}))
	data, _ := json.Marshal(map[string]any{"events": []map[string]string{
		{"message": "the password is " + quoted + " and then " + strings.Repeat("a long text ", 400)}, {"message": "short"}}})
	shown, err := readPage(&rawResult{data: data, tool: "pods_list"}, readArgs{Call: "c1"}, 1<<10, "then")
	doc, note := splitHead(t, shown)
	if err != nil || !strings.Contains(note, " Element 0 is cut: ") || !strings.Contains(doc, `pw\"Quote\\Scrub-4711-abcdef`) {
		t.Fatalf("the page with the cut element: %v, %s", err, shown[:min(len(shown), 200)])
	}
	h.waitFor(t, "the Secret's value to be known to the filter", func() bool {
		out, err := h.s.ScrubSecretValues(context.Background(), "test", shown)
		return err == nil && !strings.Contains(out[0], "Scrub-4711-abcdef") && strings.Contains(out[0], "«secret shop/quoted/password»")
	})
}

// TestResultReadIsOffered: what tells the model about result_read: one rule of the system prompt,
// in both of its forms, and the tool in both tool sets, with the four parameters the notes write
// and a description that says where the call's id comes from and what to do when nothing is held.
func TestResultReadIsOffered(t *testing.T) {
	for _, prompt := range []string{systemPrompt, systemPromptAll} {
		var rules []string
		for line := range strings.Lines(prompt) {
			if strings.Contains(line, toolRead) {
				rules = append(rules, line)
			}
		}
		if len(rules) != 1 || !strings.HasPrefix(rules[0], "- A result too large to show whole comes as its first elements or lines") ||
			!strings.HasSuffix(rules[0], "read on with result_read instead of calling the tool again.\n") || len(rules[0]) > 200 {
			t.Errorf("the system prompt's rules about result_read: %q", rules)
		}
	}
	h := newHarness(t, engine.ChatLimits{})
	ts, err := h.a.toolset()
	if err != nil {
		t.Fatal(err)
	}
	for _, all := range []bool{false, true} {
		defs := ts.defsFor(all)
		i := slices.IndexFunc(defs, func(d toolDef) bool { return d.Function.Name == toolRead })
		if i < 0 || i != len(defs)-1 {
			t.Fatalf("all tools %v: result_read is tool %d of %d, want the last", all, i, len(defs))
		}
		var schema struct {
			Type       string `json:"type"`
			Properties map[string]struct {
				Type        string `json:"type"`
				Description string `json:"description"`
			} `json:"properties"`
			Required             []string `json:"required"`
			AdditionalProperties *bool    `json:"additionalProperties"`
		}
		if err := json.Unmarshal(defs[i].Function.Parameters, &schema); err != nil {
			t.Fatal(err)
		}
		types := map[string]string{}
		for name, p := range schema.Properties {
			types[name] = p.Type
			if p.Description == "" {
				t.Errorf("result_read's parameter %s has no description", name)
			}
		}
		if want := map[string]string{"call": "string", "offset": "integer", "limit": "integer", "contains": "string"}; !reflect.DeepEqual(types, want) ||
			schema.Type != "object" || !slices.Equal(schema.Required, []string{"call"}) || schema.AdditionalProperties == nil || *schema.AdditionalProperties {
			t.Errorf("result_read's parameters: %s", defs[i].Function.Parameters)
		}
		desc := defs[i].Function.Description
		for _, want := range []string{"too large to show whole", "Cogpit's note after such a result names the call's id and the next offset", "contains",
			"not the cluster", "call the tool again", "Danger level: read."} {
			if !strings.Contains(desc, want) {
				t.Errorf("result_read's description lacks %q: %s", want, desc)
			}
		}
	}
}

// TestResultReadIsAudited: every read on leaves its own line in the audit log, a served one and a
// refused one, with the agent's origin, the conversation's context and what was asked; the page
// itself is not in it.
func TestResultReadIsAudited(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{MaxToolResultKiB: 1})
	table := h.addPods(t, 90)
	h.llm.Script(
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{listCall}},
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: toolRead, Args: `{"call":"call_list","offset":40,"limit":3,"contains":"job"}`}}},
		fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: toolRead, Args: `{"call":"call_gone"}`}}},
		fakellm.Reply{Text: []string{"Done."}},
	)
	c := h.idle(t, h.start(t, "Which pods are there?"))
	want := []string{"user", "tool:resources.list:done", "tool:result_read:done", "tool:result_read:failed", "assistant"}
	if got := itemKinds(c); !slices.Equal(got, want) {
		t.Fatalf("items %v, want %v (%s)", got, want, notes(c))
	}
	var lines []engine.AuditEntry
	for _, l := range strings.Split(strings.TrimSpace(h.audit.String()), "\n") {
		var e engine.AuditEntry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("audit line %q: %v", l, err)
		}
		if e.Command == toolRead {
			lines = append(lines, e)
			if name := table.Cell(41, engine.RowName); strings.Contains(l, name) {
				t.Errorf("the audit line holds a row of the page (%s): %s", name, l)
			}
		}
	}
	if len(lines) != 2 {
		t.Fatalf("%d audit lines for two reads on: %s", len(lines), h.audit.String())
	}
	served, refused := lines[0], lines[1]
	if served.Origin != engine.OriginBuiltInAgent || served.Context != "test" || served.Phase != "done" || !served.OK ||
		string(served.Params) != `{"call":"call_list","offset":40,"limit":3,"contains":"job"}` || served.ReadsLikeUser {
		t.Errorf("the served read's line: %+v, params %s", served, served.Params)
	}
	if refused.OK || !strings.Contains(refused.Error, "holds no result") || string(refused.Params) != `{"call":"call_gone"}` ||
		refused.Origin != engine.OriginBuiltInAgent {
		t.Errorf("the refused read's line: %+v, params %s", refused, refused.Params)
	}
}
