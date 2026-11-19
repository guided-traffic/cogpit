//go:build !nodemo

// The live tests run over the demo cluster: a development build only, as the demo cluster is (D-67).

package agent

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/demo"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
)

// liveModels are the models the live test prefers, in order (the owner's, 2026-10-14).
var liveModels = []string{"qwen/qwen3.8-27b", "qwen/qwen3.6-27b"}

// TestLiveEndpoint runs the connection test and one chat turn against a real OpenAI-compatible
// endpoint, over the synthetic demo cluster only (never other cluster data). Local only: it runs
// with COGPIT_LLM_TEST_URL (e.g. http://host:1234/v1, an LM Studio) and, if the endpoint lists
// neither preferred model, COGPIT_LLM_TEST_MODEL; COGPIT_LLM_TEST_KIND sets the kind (default
// lmstudio) and COGPIT_LLM_TEST_KEY a key (liveAgent). Skipped without them, so it never fails CI.
func TestLiveEndpoint(t *testing.T) {
	s, a, conn, base := liveAgent(t)
	ctx := context.Background()
	steps := a.TestConnection(ctx, conn, nil, nil)
	for _, st := range steps {
		t.Logf("connection test %-5s %-10s %6s %s", st.Layer, st.State, st.Duration.Round(time.Millisecond), st.Detail)
		if st.Warning != "" {
			t.Logf("connection test %-5s warning: %s", st.Layer, st.Warning)
		}
	}
	if steps[len(steps)-1].State != engine.ChatTestPass {
		t.Fatal("the connection test did not pass")
	}
	// Plain http to another machine (the owner's LM Studio on his network) is warned about (H-95).
	if warned := steps[2].Warning == engine.ChatPlainHTTPWarning; warned != engine.ChatPlainHTTPRemote(base) {
		t.Errorf("the HTTP layer's warning %q for %s", steps[2].Warning, base)
	}

	// A namespace of the demo cluster to ask about, and its pod count as the cache holds it.
	res, err := s.Dispatch(ctx, engine.Invocation{ID: "resources.list", Origin: engine.OriginGUI, Params: json.RawMessage(`{"kind":"pods","limit":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	var first commands.ListResult
	_ = json.Unmarshal(b, &first)
	if len(first.Rows) == 0 {
		t.Fatalf("no demo pods: %s", b)
	}
	ns := first.RefAt(0).Namespace

	c, err := a.NewConversation(engine.DemoContext, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	c = liveTurn(t, s, a, c.ID, "How many pods run in namespace "+ns+"?")
	if c.Items[len(c.Items)-1].Kind != engine.ChatItemAssistant {
		t.Errorf("the turn ended without an answer")
	}
}

// TestLiveContextClearing runs the clearing of old tool results (D-167) against a real endpoint,
// local only as TestLiveEndpoint. A first turn reads the demo cluster's pods in a window no
// request fills. The window is then set so that the conversation stands at 75 % of it, by the
// endpoint's own count, and a second turn must go out without the first turn's results and still
// end with an answer. The window follows the conversation, not the model, so that a slow endpoint
// reads thousands of tokens, not hundreds of thousands. It logs how fast the endpoint read the
// first turn's results and what a clearing at the default limits would so cost: the tokens it
// leaves, read anew.
func TestLiveContextClearing(t *testing.T) {
	s, a, conn, _ := liveAgent(t)
	c, err := a.NewConversation(engine.DemoContext, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	c = liveTurn(t, s, a, c.ID, "List the pods of every namespace with resources_list, then tell me how many there are.")
	last := c.Items[len(c.Items)-1]
	if last.Kind != engine.ChatItemAssistant {
		t.Fatal("the first turn ended without an answer")
	}
	a.mu.Lock()
	conv := a.convs[c.ID]
	tokens, bytes := conv.calTokens, conv.calBytes
	results, clearables := 0, 0
	for i := len(conv.history) - 2; i >= 0 && conv.history[i].Role == roleTool; i-- { // what the last request read anew
		results += jsonSize(conv.history[i].Content)
	}
	for _, m := range conv.history {
		if clearable(m) {
			clearables++
		}
	}
	a.mu.Unlock()
	if tokens == 0 {
		t.Fatal("the endpoint reported no prompt tokens: nothing to measure the context with")
	}
	if clearables == 0 {
		t.Fatal("the first turn read no tool result large enough to clear")
	}
	t.Logf("the conversation: %d prompt tokens for %d bytes (%.2f bytes a token), %d results to clear", tokens, bytes,
		float64(bytes)/float64(tokens), clearables)
	if tm := last.Timing; tm != nil && tm.TTFT > 0 && results > 0 {
		read := float64(results) * float64(tokens) / float64(bytes)
		speed := read / tm.TTFT.Seconds()
		d := engine.DefaultChatLimits
		left := d.ContextWindow * d.ClearTo / 100
		t.Logf("the endpoint read about %.0f new tokens in %s: about %.0f tokens a second", read, tm.TTFT.Round(time.Millisecond), speed)
		t.Logf("a clearing at the defaults leaves %d tokens to read anew: about %s (a turn's limit is %s)", left,
			time.Duration(float64(left)/speed*float64(time.Second)).Round(time.Second), d.MaxDuration)
	}

	conn.Limits = engine.ChatLimits{ContextWindow: tokens * 100 / 75, ClearAt: 70, ClearTo: 35}
	if _, err := a.SaveConnection(conn, nil); err != nil {
		t.Fatal(err)
	}
	before := len(c.Items)
	c = liveTurn(t, s, a, c.ID, "And how many of them are in the first namespace of your list?")
	var cleared, full bool
	for _, it := range c.Items[before:] {
		if it.Kind == engine.ChatItemNote {
			cleared = cleared || strings.Contains(it.Text, "from what the model is sent")
			full = full || strings.Contains(it.Text, "Start a new conversation")
		}
	}
	if !cleared {
		t.Errorf("the second turn, at 75 %% of a window of %d tokens, cleared nothing", conn.Limits.ContextWindow)
	}
	if full {
		t.Errorf("the tools and the messages alone fill 70 %% of a window of %d tokens", conn.Limits.ContextWindow)
	}
	if p, ok := a.SentPayload(c.ID, len(c.Items)-1); !ok || !strings.Contains(p, "removed by Cogpit") {
		t.Error("the second turn's request carries no cleared result")
	}
	if c.Items[len(c.Items)-1].Kind != engine.ChatItemAssistant {
		t.Errorf("the turn after the clearing ended without an answer")
	}
}

// TestLiveReadsATable asks a real model, local only as TestLiveEndpoint, for what a list's table
// (D-168) says in two places: the status every pod of kube-system shares, which stands once in
// "same", and one pod's node, which stands in its row. The demo cluster's kube-system pods all run,
// and etcd-cp-2 runs on cp-2.
func TestLiveReadsATable(t *testing.T) {
	s, a, conn, _ := liveAgent(t)
	c, err := a.NewConversation(engine.DemoContext, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	c = liveTurn(t, s, a, c.ID, "List the pods of namespace kube-system with resources_list. Do they all have the same status, "+
		"and which one? On which node does etcd-cp-2 run?")
	listed := false
	for _, it := range c.Items {
		if it.Tool != nil && it.Tool.Command == "resources.list" && it.Tool.State == engine.ChatToolDone {
			listed = true
			if !strings.Contains(it.Tool.Result, `"same":{`) || !strings.Contains(it.Tool.Result, `"Status":"Running"`) {
				t.Errorf("the list's result shares no status: %.300s", it.Tool.Result)
			}
		}
	}
	last := c.Items[len(c.Items)-1]
	if !listed || last.Kind != engine.ChatItemAssistant {
		t.Fatalf("the turn listed nothing or ended without an answer")
	}
	if !strings.Contains(last.Text, "Running") || !strings.Contains(last.Text, "cp-2") {
		t.Errorf("the answer names neither the shared status nor the pod's node: %s", last.Text)
	}
}

// TestLiveOverviewFold asks a real model, local only as TestLiveEndpoint, what is wrong in the demo
// cluster: cluster_overview answers with its issues folded (D-171), a few kilobytes where every
// issue of its own was above a hundred, and the model must name the demo's fault from it: images
// that cannot be pulled.
func TestLiveOverviewFold(t *testing.T) {
	s, a, conn, _ := liveAgent(t)
	c, err := a.NewConversation(engine.DemoContext, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	c = liveTurn(t, s, a, c.ID, "Read the cluster's state with cluster_overview. What is the main problem in this cluster, in one or two sentences?")
	read := false
	for _, it := range c.Items {
		if it.Tool != nil && it.Tool.Command == "cluster.overview" && it.Tool.State == engine.ChatToolDone {
			read = true
			if !strings.Contains(it.Tool.Result, `"issueFold"`) || len(it.Tool.Result) > 24<<10 {
				t.Errorf("the overview's result is no fold or too large: %d bytes: %.200s", len(it.Tool.Result), it.Tool.Result)
			}
		}
	}
	last := c.Items[len(c.Items)-1]
	if !read || last.Kind != engine.ChatItemAssistant {
		t.Fatalf("the turn read no overview or ended without an answer")
	}
	if answer := strings.ToLower(last.Text); !strings.Contains(answer, "image") && !strings.Contains(answer, "pull") {
		t.Errorf("the answer does not name the images that cannot be pulled: %s", last.Text)
	}
}

// TestLiveReadsOn asks a real model, local only as TestLiveEndpoint, for the last pod of a list
// that does not fit one result (D-170): a limit of 2 KiB shows the first pods and the totals, and
// the name asked for stands only on a later page, which result_read reads.
func TestLiveReadsOn(t *testing.T) {
	s, a, conn, _ := liveAgent(t)
	conn.Limits = engine.ChatLimits{MaxToolResultKiB: 2}
	if _, err := a.SaveConnection(conn, nil); err != nil {
		t.Fatal(err)
	}
	res, err := s.Dispatch(context.Background(), engine.Invocation{ID: "resources.list", Origin: engine.OriginGUI,
		Params: json.RawMessage(`{"kind":"pods","namespaces":["kube-system"],"limit":1000}`)})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	var list struct{ engine.RowTable }
	if err := json.Unmarshal(b, &list); err != nil || len(list.Rows) < 30 {
		t.Fatalf("the demo's kube-system pods: %d rows, %v", len(list.Rows), err)
	}
	lastPod := list.Cell(len(list.Rows)-1, engine.RowName)
	c, err := a.NewConversation(engine.DemoContext, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	c = liveTurn(t, s, a, c.ID, "List the pods of namespace kube-system with resources_list (limit 1000). The list is sorted by name. "+
		"Which pod is the very last one in it? Read on until you have seen the end of the list.")
	reads := 0
	for _, it := range c.Items {
		if it.Tool != nil && it.Tool.Command == toolRead && it.Tool.State == engine.ChatToolDone {
			reads++
		}
	}
	last := c.Items[len(c.Items)-1]
	if last.Kind != engine.ChatItemAssistant {
		t.Fatal("the turn ended without an answer")
	}
	t.Logf("the list's last pod is %s; %d reads on", lastPod, reads)
	if reads == 0 {
		t.Errorf("the model never read on")
	}
	if !strings.Contains(last.Text, lastPod) {
		t.Errorf("the answer does not name the last pod %s: %s", lastPod, last.Text)
	}
}

// liveAgent is an agent over the demo cluster with a connection to the endpoint the environment
// names, its model chosen; it skips the test without COGPIT_LLM_TEST_URL.
func liveAgent(t *testing.T) (*engine.Session, *Agent, engine.ChatConnection, string) {
	t.Helper()
	base := os.Getenv("COGPIT_LLM_TEST_URL")
	if base == "" {
		t.Skip("set COGPIT_LLM_TEST_URL to run against a real endpoint")
	}
	kind := engine.ChatConnectionKind(os.Getenv("COGPIT_LLM_TEST_KIND"))
	if kind == "" {
		kind = engine.ChatLMStudio
	}
	var key *string
	if k := os.Getenv("COGPIT_LLM_TEST_KEY"); k != "" {
		key = &k
	}
	s := engine.New(engine.Options{Connector: &engine.ClusterConnector{IncludeDemo: true,
		DemoOptions: demo.Options{Pods: 60, EventsPerSecond: 1}}, SnapshotInterval: 20 * time.Millisecond})
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	ctx := context.Background()
	if err := s.Connect(ctx, engine.DemoContext); err != nil {
		t.Fatal(err)
	}
	a := New(Options{Session: s, Dir: t.TempDir(), Keys: NewMemoryKeyStore()})
	conn, err := a.SaveConnection(engine.ChatConnection{Name: "live", Kind: kind, BaseURL: base, Model: "pending"}, key)
	if err != nil {
		t.Fatal(err)
	}
	models, err := a.Models(ctx, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	conn.Model = os.Getenv("COGPIT_LLM_TEST_MODEL")
	for _, m := range liveModels {
		if slices.Contains(models, m) {
			conn.Model = m
			break
		}
	}
	if conn.Model == "" {
		t.Fatalf("the endpoint lists none of %v; set COGPIT_LLM_TEST_MODEL (it lists %v)", liveModels, models)
	}
	if conn, err = a.SaveConnection(conn, nil); err != nil {
		t.Fatal(err)
	}
	t.Logf("model %s of %d listed", conn.Model, len(models))
	return s, a, conn, base
}

// liveTurn sends a question, waits up to 8 minutes for the turn's end and logs what the turn
// added: its tool calls, what it sent, its notes and its answers with their time to the first
// token.
func liveTurn(t *testing.T, s *engine.Session, a *Agent, id, question string) engine.ChatConversation {
	t.Helper()
	before, _ := a.Conversation(id)
	changed := make(chan struct{}, 1)
	defer s.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventChat {
			select {
			case changed <- struct{}{}:
			default:
			}
		}
	})()
	start := time.Now()
	if err := a.Send(id, engine.ChatUserMessage{Text: question}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Stop(id) })
	deadline := time.After(8 * time.Minute)
	var c engine.ChatConversation
	for {
		got, _ := a.Conversation(id)
		if !got.Running {
			c = got
			break
		}
		select {
		case <-changed:
		case <-deadline:
			t.Fatal("the turn did not end within 8 minutes")
		}
	}
	t.Logf("question %q, %s", question, time.Since(start).Round(time.Millisecond))
	for _, it := range c.Items[len(before.Items):] {
		switch it.Kind {
		case engine.ChatItemTool:
			t.Logf("tool %s %s [%s] %d bytes %s", it.Tool.Command, it.Tool.Args, it.Tool.State, len(it.Tool.Result), firstLine(it.Tool.Error))
		case engine.ChatItemUser:
			t.Logf("sent %d requests, %d bytes, %d tokens", it.Sent.Requests, it.Sent.Bytes, it.Sent.Tokens)
		default:
			ttft := ""
			if it.Timing != nil {
				ttft = " (first token after " + it.Timing.TTFT.Round(time.Millisecond).String() + ")"
			}
			t.Logf("%s%s: %s", it.Kind, ttft, it.Text)
		}
	}
	return c
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}
