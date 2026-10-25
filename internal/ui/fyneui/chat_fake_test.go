package fyneui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// fakeChat is a scripted engine.ChatAgent: conversations a test writes item by item (an answer it
// streams by hand, tool calls in every state, notes), connections, a connection test's steps. It
// records what the window asked of it and announces every change as the real agent does
// (Session.NotifyChat, or the fake engine's EventChat).
type fakeChat struct {
	mu       sync.Mutex
	notify   func(id string, rev uint64)
	conns    []engine.ChatConnection
	convs    map[string]*engine.ChatConversation
	seq      int
	now      time.Time
	models   map[string][]string
	modelsOf map[string][]string // by base URL: what ModelsOf lists for the form's values
	payloads map[string]string   // "id/item" → the request body

	// What the window did.
	sends    []fakeSend
	resends  []fakeResend
	stops    []string
	saves    []fakeSave
	deleted  []string
	cleared  []string
	defaults []string
	switched []string // "conversation→connection"
	reads    map[string]int
	tests    []fakeSave
	listings []fakeSave // ModelsOf's calls

	sendErr error
	// testSteps are the connection test's progress, in order; the last is the result.
	testSteps [][]engine.ChatTestStep
}

type fakeSend struct {
	id  string
	msg engine.ChatUserMessage
}

type fakeResend struct {
	id   string
	item int
	msg  engine.ChatUserMessage
}

type fakeSave struct {
	conn engine.ChatConnection
	key  *string
}

var _ engine.ChatAgent = (*fakeChat)(nil)

func newFakeChat() *fakeChat {
	return &fakeChat{convs: map[string]*engine.ChatConversation{}, reads: map[string]int{}, models: map[string][]string{},
		payloads: map[string]string{}, now: fixedNow, notify: func(string, uint64) {}}
}

// lmStudio is a typical local connection, the default.
func lmStudio() engine.ChatConnection {
	return engine.ChatConnection{ID: "c1", Name: "LM Studio", Kind: engine.ChatLMStudio, BaseURL: "http://localhost:1234/v1",
		Model: "qwen3.6-27b", Private: true, Default: true}
}

// litellm is a team proxy with a key.
func litellm() engine.ChatConnection {
	return engine.ChatConnection{ID: "c2", Name: "Team LiteLLM", Kind: engine.ChatLiteLLM, BaseURL: "https://litellm.example.com",
		Model: "gpt-oss-120b", HasKey: true, Limits: engine.ChatLimits{MaxToolCalls: 40}}
}

// withChat gives the fake engine a chat agent and the real chat.toggle's declaration (⌘I, View ›
// Agent Chat), which switches UIState.Chat as the engine's does; preferences.open and
// cluster.settings.open are recorded.
func withChat(t testing.TB, f *fakeEngine) *fakeChat {
	t.Helper()
	ch := newFakeChat()
	ch.notify = func(id string, rev uint64) { f.emit(engine.Event{Type: engine.EventChat, View: id, Revision: rev}) }
	f.mu.Lock()
	f.chat = ch
	f.mu.Unlock()
	err := engine.Register(f.reg, engine.Spec[chatToggleParams]{ID: "chat.toggle", Label: "Agent Chat", MenuPath: "View/Agent Chat",
		Shortcut: "Cmd+I", Danger: engine.DangerUI,
		Run: func(context.Context, *engine.Session, chatToggleParams) (any, error) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	f.handlers["chat.toggle"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p chatToggleParams
		if err := decode(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			if p.Open != nil {
				u.Chat = *p.Open
			} else {
				u.Chat = !u.Chat
			}
		})
		return nil, nil
	}
	for _, id := range []string{"preferences.open", "cluster.settings.open"} {
		f.handlers[id] = func(context.Context, engine.Invocation) (any, error) { return nil, nil }
	}
	return ch
}

func (c *fakeChat) emit(id string) {
	c.mu.Lock()
	var rev uint64
	if cv := c.convs[id]; cv != nil {
		rev = cv.Revision
	}
	notify := c.notify
	c.mu.Unlock()
	notify(id, rev)
}

// --- scripting ---

// add keeps a conversation of ctx with items; it is the newest. As the agent's, it is private
// (memory only, H-96) when a user item's turn ran over a private connection.
func (c *fakeChat) add(ctx, title string, items ...engine.ChatItem) string {
	c.mu.Lock()
	c.seq++
	id := fmt.Sprintf("conv-%d", c.seq)
	conn := ""
	if len(c.conns) > 0 {
		conn = c.conns[0].ID
	}
	private := slices.ContainsFunc(items, func(it engine.ChatItem) bool { return it.Sent != nil && it.Sent.Private })
	c.convs[id] = &engine.ChatConversation{ChatConversationInfo: engine.ChatConversationInfo{ID: id, Title: title, Context: ctx,
		ConnectionID: conn, Updated: c.now.Add(time.Duration(c.seq) * time.Minute), Private: private}, Revision: 1, Items: items}
	c.mu.Unlock()
	return id
}

// change edits a conversation, raises its revision and announces it.
func (c *fakeChat) change(id string, fn func(*engine.ChatConversation)) {
	c.mu.Lock()
	cv := c.convs[id]
	fn(cv)
	cv.Revision++
	c.mu.Unlock()
	c.emit(id)
}

// stream appends delta to the conversation's last answer, or starts one.
func (c *fakeChat) stream(id, delta string) {
	c.change(id, func(cv *engine.ChatConversation) {
		n := len(cv.Items)
		if n > 0 && cv.Items[n-1].Kind == engine.ChatItemAssistant && cv.Items[n-1].Streaming {
			cv.Items[n-1].Text += delta
			return
		}
		cv.Items = append(cv.Items, engine.ChatItem{Kind: engine.ChatItemAssistant, Text: delta, Streaming: true})
	})
}

func (c *fakeChat) sendsOf() []fakeSend {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.sends)
}

func (c *fakeChat) stopsOf() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.stops)
}

func (c *fakeChat) readsOf(id string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads[id]
}

func (c *fakeChat) savesOf() []fakeSave {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.saves)
}

// --- engine.ChatAgent ---

func (c *fakeChat) Connections() ([]engine.ChatConnection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.conns), nil
}

func (c *fakeChat) SaveConnection(cn engine.ChatConnection, apiKey *string) (engine.ChatConnection, error) {
	c.mu.Lock()
	c.saves = append(c.saves, fakeSave{conn: cn, key: apiKey})
	if cn.ID == "" {
		c.seq++
		cn.ID = fmt.Sprintf("c%d", 100+c.seq)
	}
	if apiKey != nil {
		cn.HasKey = *apiKey != ""
	}
	i := slices.IndexFunc(c.conns, func(x engine.ChatConnection) bool { return x.ID == cn.ID })
	if i >= 0 {
		cn.Default = c.conns[i].Default
		c.conns[i] = cn
	} else {
		cn.Default = len(c.conns) == 0
		c.conns = append(c.conns, cn)
	}
	c.mu.Unlock()
	c.notify("", 0)
	return cn, nil
}

func (c *fakeChat) DeleteConnection(id string) error {
	c.mu.Lock()
	c.conns = slices.DeleteFunc(c.conns, func(x engine.ChatConnection) bool { return x.ID == id })
	c.mu.Unlock()
	c.notify("", 0)
	return nil
}

func (c *fakeChat) SetDefaultConnection(id string) error {
	c.mu.Lock()
	c.defaults = append(c.defaults, id)
	for i := range c.conns {
		c.conns[i].Default = c.conns[i].ID == id
	}
	c.mu.Unlock()
	c.notify("", 0)
	return nil
}

func (c *fakeChat) Models(_ context.Context, id string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.models[id]), nil
}

// ModelsOf lists the models of the form's values: those of its base URL (modelsOf), else of its
// saved connection; each call is kept (listings).
func (c *fakeChat) ModelsOf(_ context.Context, cn engine.ChatConnection, apiKey *string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listings = append(c.listings, fakeSave{conn: cn, key: apiKey})
	if m, ok := c.modelsOf[cn.BaseURL]; ok {
		return slices.Clone(m), nil
	}
	return slices.Clone(c.models[cn.ID]), nil
}

func (c *fakeChat) TestConnection(_ context.Context, cn engine.ChatConnection, apiKey *string, progress func([]engine.ChatTestStep)) []engine.ChatTestStep {
	c.mu.Lock()
	c.tests = append(c.tests, fakeSave{conn: cn, key: apiKey})
	steps := slices.Clone(c.testSteps)
	c.mu.Unlock()
	if len(steps) == 0 {
		return nil
	}
	for _, s := range steps[:len(steps)-1] {
		if progress != nil {
			progress(s)
		}
	}
	return steps[len(steps)-1]
}

func (c *fakeChat) Conversations(ctx string) []engine.ChatConversationInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []engine.ChatConversationInfo
	for _, cv := range c.convs {
		if cv.Context == ctx {
			out = append(out, cv.ChatConversationInfo)
		}
	}
	slices.SortFunc(out, func(a, b engine.ChatConversationInfo) int { return b.Updated.Compare(a.Updated) })
	return out
}

func (c *fakeChat) Conversation(id string) (engine.ChatConversation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads[id]++
	cv := c.convs[id]
	if cv == nil {
		return engine.ChatConversation{}, false
	}
	out := *cv
	out.Items = slices.Clone(cv.Items)
	return out, true
}

func (c *fakeChat) NewConversation(ctx, conn string) (engine.ChatConversation, error) {
	id := c.add(ctx, "")
	c.mu.Lock()
	if conn != "" {
		c.convs[id].ConnectionID = conn
	}
	out := *c.convs[id]
	c.mu.Unlock()
	c.notify("", 0)
	return out, nil
}

func (c *fakeChat) SetConversationConnection(id, conn string) error {
	c.mu.Lock()
	c.switched = append(c.switched, id+"→"+conn)
	c.mu.Unlock()
	c.change(id, func(cv *engine.ChatConversation) { cv.ConnectionID = conn })
	return nil
}

func (c *fakeChat) DeleteConversation(id string) error {
	c.mu.Lock()
	delete(c.convs, id)
	c.deleted = append(c.deleted, id)
	c.mu.Unlock()
	c.notify("", 0)
	return nil
}

func (c *fakeChat) ClearConversations(ctx string) error {
	c.mu.Lock()
	for id, cv := range c.convs {
		if cv.Context == ctx {
			delete(c.convs, id)
		}
	}
	c.cleared = append(c.cleared, ctx)
	c.mu.Unlock()
	c.notify("", 0)
	return nil
}

func (c *fakeChat) Send(id string, msg engine.ChatUserMessage) error {
	c.mu.Lock()
	err := c.sendErr
	c.sends = append(c.sends, fakeSend{id: id, msg: msg})
	c.mu.Unlock()
	if err != nil {
		return err
	}
	c.change(id, func(cv *engine.ChatConversation) {
		attached := ""
		if msg.AttachSelection {
			attached = "attached"
		}
		if cv.Title == "" {
			cv.Title = strings.SplitN(msg.Text, "\n", 2)[0]
		}
		cv.Items = append(cv.Items, engine.ChatItem{Kind: engine.ChatItemUser, Text: msg.Text, Attached: attached})
		cv.Running = true
	})
	return nil
}

// Resend drops the user's item and everything after it and adds the message, as the agent does
// (ErrNotUserMessage's text for an item that is no user's message); sendErr fails it.
func (c *fakeChat) Resend(id string, item int, msg engine.ChatUserMessage) error {
	c.mu.Lock()
	err := c.sendErr
	c.resends = append(c.resends, fakeResend{id: id, item: item, msg: msg})
	cv := c.convs[id]
	if err == nil && (cv == nil || item < 0 || item >= len(cv.Items) || cv.Items[item].Kind != engine.ChatItemUser) {
		err = errors.New("only a message of yours can be edited and sent again")
	}
	c.mu.Unlock()
	if err != nil {
		return err
	}
	c.change(id, func(cv *engine.ChatConversation) {
		attached := ""
		if msg.AttachSelection {
			attached = "attached"
		}
		cv.Items = append(cv.Items[:item:item], engine.ChatItem{Kind: engine.ChatItemUser, Text: msg.Text, Attached: attached})
		cv.Running = true
	})
	return nil
}

func (c *fakeChat) resendsOf() []fakeResend {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.resends)
}

func (c *fakeChat) Stop(id string) {
	c.mu.Lock()
	c.stops = append(c.stops, id)
	_, ok := c.convs[id]
	c.mu.Unlock()
	if ok {
		c.change(id, func(cv *engine.ChatConversation) { cv.Running = false })
	}
}

func (c *fakeChat) SentPayload(id string, item int) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.payloads[fmt.Sprintf("%s/%d", id, item)]
	return p, ok
}

// errNotSent is a Send that fails.
var errNotSent = errors.New("the endpoint refused the request: HTTP 503")
