package agent

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/enginekit"
	"github.com/guided-traffic/cogpit/internal/testkit/fakellm"
)

// secretValue is a value of a Secret in the harness's cluster: it must never reach a non-private
// endpoint through a tool result, nor the conversations file.
const secretValue = "pw-Chat-Scrub-4711-qrstuv"

// syncBuffer is a bytes.Buffer safe for the audit log's concurrent writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// fakeConfirmer records what the user is asked and answers with what the test sends.
type fakeConfirmer struct {
	asked    chan engine.ConfirmRequest
	answer   chan bool
	returned chan error // each Confirm's end: nil (answered) or ctx's error
}

func newFakeConfirmer() *fakeConfirmer {
	return &fakeConfirmer{asked: make(chan engine.ConfirmRequest, 8), answer: make(chan bool, 8), returned: make(chan error, 8)}
}

func (f *fakeConfirmer) Confirm(ctx context.Context, req engine.ConfirmRequest) (bool, error) {
	f.asked <- req
	select {
	case ok := <-f.answer:
		f.returned <- nil
		return ok, nil
	case <-ctx.Done():
		f.returned <- ctx.Err()
		return false, ctx.Err()
	}
}

// harness is a real engine session over an in-memory cluster ("test": pods, a Deployment and a
// Secret in namespace shop), Cogpit's whole registry, a fake confirmer, the agent with a memory
// key store and a temporary config directory, and the fake LLM endpoint.
type harness struct {
	s     *engine.Session
	src   *testkit.MemorySource
	a     *Agent
	llm   *fakellm.Server
	keys  *MemoryKeyStore
	dir   string
	conf  *fakeConfirmer
	audit *syncBuffer
	conn  engine.ChatConnection
	chat  chan uint64 // a signal per EventChat
	// clock, when a test set it, is the agent's time; else the session's.
	clock atomic.Pointer[time.Time]
}

// now is the agent's clock.
func (h *harness) now() time.Time {
	if t := h.clock.Load(); t != nil {
		return *t
	}
	return h.s.Now()
}

func newHarness(t *testing.T, limits engine.ChatLimits) *harness {
	t.Helper()
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-0"))
	src.Set(testkit.PodsGVR, testkit.Pod("shop", "web-1"))
	src.Set(testkit.PodsGVR, testkit.Pod("infra", "db-0"))
	src.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", "shop", "web", map[string]any{
		"spec": map[string]any{"replicas": int64(2)}, "status": map[string]any{"replicas": int64(2), "readyReplicas": int64(2)}}))
	src.Set(testkit.SecretsGVR, testkit.Secret("shop", "db", nil, map[string]string{"password": secretValue}))
	h := &harness{src: src, conf: newFakeConfirmer(), audit: &syncBuffer{}, keys: NewMemoryKeyStore(), dir: t.TempDir(), chat: make(chan uint64, 1)}
	h.s = engine.New(engine.Options{
		Connector: enginekit.MemoryConnector{Context: "test", Src: src, Profiles: kinds.Builtin(), Writer: &enginekit.FluxRecorder{}},
		Confirmer: h.conf, Audit: h.audit, SnapshotInterval: 5 * time.Millisecond})
	if err := commands.RegisterAll(h.s.Registry()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.s.Close)
	if err := h.s.Connect(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	h.s.Subscribe(func(ev engine.Event) {
		if ev.Type == engine.EventChat {
			select {
			case h.chat <- ev.Revision:
			default:
			}
		}
	})
	// The Deployments listed, as a window that showed them has them: a write's Enabled reads the
	// cache, which lists a kind on its first use.
	h.waitFor(t, "the Deployments to be listed", func() bool {
		cmd, _ := h.s.Registry().Get("deployment.restart")
		return cmd.Enabled(h.s, []byte(`{"ref":{"kind":"deployments","namespace":"shop","name":"web"}}`)) == nil
	})
	h.llm = fakellm.New(t)
	h.a = New(Options{Session: h.s, Dir: h.dir, Keys: h.keys, NotifyEvery: time.Millisecond, Now: h.now})
	t.Cleanup(func() { h.stopAll() })
	var err error
	h.conn, err = h.a.SaveConnection(engine.ChatConnection{Name: "fake", Kind: engine.ChatLMStudio, BaseURL: h.llm.Base,
		Model: "test-model", Limits: limits}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// stopAll stops every running turn (a failed test must not leave one behind).
func (h *harness) stopAll() {
	h.a.mu.Lock()
	var ids []string
	for id := range h.a.convs {
		ids = append(ids, id)
	}
	h.a.mu.Unlock()
	for _, id := range ids {
		h.a.Stop(id)
	}
}

// waitFor waits until cond holds, re-checking on every EventChat; it fails the test after 10 s.
func (h *harness) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond) // engine changes without EventChat (the confirmer's queue)
	defer tick.Stop()
	for !cond() {
		select {
		case <-h.chat:
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// recv receives from ch within 10 s.
func recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
	var zero T
	return zero
}

// start opens a conversation on "test" and sends text.
func (h *harness) start(t *testing.T, text string) string {
	t.Helper()
	c, err := h.a.NewConversation("test", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.a.Send(c.ID, engine.ChatUserMessage{Text: text}); err != nil {
		t.Fatal(err)
	}
	return c.ID
}

// idle waits until the conversation's turn ended and returns it.
func (h *harness) idle(t *testing.T, id string) engine.ChatConversation {
	t.Helper()
	var c engine.ChatConversation
	h.waitFor(t, "the turn's end", func() bool {
		c, _ = h.a.Conversation(id)
		return !c.Running
	})
	return c
}

// kinds lists the items' kinds, a tool item with its command and state.
func itemKinds(c engine.ChatConversation) []string {
	var out []string
	for _, it := range c.Items {
		s := string(it.Kind)
		if it.Tool != nil {
			s += ":" + it.Tool.Command + ":" + string(it.Tool.State)
		}
		out = append(out, s)
	}
	return out
}

// notes returns the notes' texts.
func notes(c engine.ChatConversation) string {
	var out []string
	for _, it := range c.Items {
		if it.Kind == engine.ChatItemNote {
			out = append(out, it.Text)
		}
	}
	return strings.Join(out, " | ")
}

// messages returns the messages of a chat request.
func messages(t *testing.T, r fakellm.Request) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, m := range r.JSON()["messages"].([]any) {
		out = append(out, m.(map[string]any))
	}
	return out
}
