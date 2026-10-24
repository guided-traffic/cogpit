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
// lmstudio) and COGPIT_LLM_TEST_KEY a key. Skipped without them, so it never fails CI.
func TestLiveEndpoint(t *testing.T) {
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
	var first struct {
		Rows []struct {
			Ref struct {
				Namespace string `json:"namespace"`
			} `json:"ref"`
		} `json:"rows"`
	}
	_ = json.Unmarshal(b, &first)
	if len(first.Rows) == 0 {
		t.Fatalf("no demo pods: %s", b)
	}
	ns := first.Rows[0].Ref.Namespace

	c, err := a.NewConversation(engine.DemoContext, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
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
	question := "How many pods run in namespace " + ns + "?"
	if err := a.Send(c.ID, engine.ChatUserMessage{Text: question}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Stop(c.ID) })
	deadline := time.After(8 * time.Minute)
	for {
		got, _ := a.Conversation(c.ID)
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
	for _, it := range c.Items {
		switch it.Kind {
		case engine.ChatItemTool:
			t.Logf("tool %s %s [%s] %s", it.Tool.Command, it.Tool.Args, it.Tool.State, firstLine(it.Tool.Error))
		case engine.ChatItemUser:
			t.Logf("sent %d requests, %d bytes, %d tokens", it.Sent.Requests, it.Sent.Bytes, it.Sent.Tokens)
		default:
			t.Logf("%s: %s", it.Kind, it.Text)
		}
	}
	if c.Items[len(c.Items)-1].Kind != engine.ChatItemAssistant {
		t.Errorf("the turn ended without an answer")
	}
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}
