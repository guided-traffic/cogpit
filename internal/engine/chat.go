package engine

import (
	"context"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The built-in agent's chat (D-10, M12, architecture §7). Package agent (L4) implements ChatAgent;
// the desktop binary installs it (Session.SetChatAgent); the window's chat column drives it
// through Session.Chat and re-reads a conversation on EventChat.
//
// The chat is not a registry command and so not an MCP tool: only the window talks to it. An
// external agent that could drive the built-in one would read, through a private connection, what
// it may not read itself, and could pick the model's next tool call. The chat's own tool calls go
// through Session.Dispatch with origin OriginBuiltInAgent, so D-2's confirmations, the audit log
// and the agents' read rules apply to them as to any agent; Invocation.ReadsLikeUser loosens the
// read rules for a private connection (decided by the owner, 2026-10-14 late evening).
//
// Whether the chat column is shown is UI state (UIState.Chat, chat.toggle, ⌘I), so agents see it
// as they see the palette.

// ChatAgent is the built-in agent as the window sees it. Its methods are safe for concurrent use.
// Errors carry no credential and no API key.
type ChatAgent interface {
	// Connections returns the configured LLM connections, the default one first. A connection
	// never carries its API key: the key lives in an owner-only file of its own in Cogpit's
	// configuration directory, apart from the connections (no keychain: the owner, 2026-10-15).
	Connections() ([]ChatConnection, error)
	// SaveConnection adds a connection (empty ID) or changes one. apiKey nil leaves the stored key
	// as it is; a pointer to "" removes it; any other value replaces the stored one. Returns the
	// connection as stored (with its ID and HasKey).
	SaveConnection(c ChatConnection, apiKey *string) (ChatConnection, error)
	// DeleteConnection removes a connection and its key. Conversations that used it keep their
	// history and pick another connection on their next turn.
	DeleteConnection(id string) error
	// SetDefaultConnection makes a connection the one new conversations use.
	SetDefaultConnection(id string) error
	// Models lists the models the connection's endpoint offers (its /models), sorted.
	Models(ctx context.Context, connectionID string) ([]string, error)
	// ModelsOf lists the models of a connection that may be unsaved (the settings form's values),
	// apiKey as in TestConnection: nil means the stored key of c.ID (the owner's request of
	// 2026-10-25: the list from the credentials just typed).
	ModelsOf(ctx context.Context, c ChatConnection, apiKey *string) ([]string, error)
	// TestConnection checks a connection layer by layer (ChatTestLayers), each on its own, and
	// never sends cluster data. c may be unsaved (the settings form's values); apiKey as in
	// SaveConnection, nil meaning the stored key of c.ID. progress, if not nil, receives the steps
	// after every change; the final steps are returned.
	TestConnection(ctx context.Context, c ChatConnection, apiKey *string, progress func([]ChatTestStep)) []ChatTestStep

	// Conversations lists the conversations of a kubeconfig context, newest first: the kept ones
	// and, until Cogpit quits, those a private turn made memory-only (ChatConversationInfo.Private).
	Conversations(kubeContext string) []ChatConversationInfo
	// Conversation returns one conversation with its items.
	Conversation(id string) (ChatConversation, bool)
	// NewConversation starts an empty conversation bound to kubeContext, with the given
	// connection ("" the default one).
	NewConversation(kubeContext, connectionID string) (ChatConversation, error)
	// SetConversationConnection switches the connection (and so the model) of a conversation
	// between turns.
	SetConversationConnection(id, connectionID string) error
	// DeleteConversation removes one conversation; ClearConversations every one of a context.
	DeleteConversation(id string) error
	ClearConversations(kubeContext string) error

	// Send adds the user's message to a conversation and starts a turn, which streams the
	// answer and runs the model's tool calls; it returns once the turn has started. It fails when
	// a turn of the conversation is running or the conversation's context is not the connected
	// one.
	Send(conversationID string, msg ChatUserMessage) error
	// Resend edits the user's message at item and sends it again (F9, the owner's choice of
	// 2026-10-15, ChatGPT's form): that message and everything after it leave the conversation,
	// the history the model is sent and the kept conversations file (the audit log keeps
	// everything), msg takes its place and a turn starts as with Send, with the window's state of
	// now. The model is told nothing of what was cut. A failed or stopped turn is retried this way.
	// It fails as Send does, and when item is not a user's message; a Resend that fails leaves the
	// conversation as it was.
	Resend(conversationID string, item int, msg ChatUserMessage) error
	// Stop is the kill switch: it cancels the conversation's running turn, the model's stream and
	// a running tool call, and returns once nothing the turn started keeps running.
	Stop(conversationID string)
	// SentPayload returns the request body the turn of item index sent to the endpoint, as sent
	// (the window shows it on request: "what is sent is visible"). The API key is never in it.
	SentPayload(conversationID string, item int) (string, bool)
}

// ChatConnectionKind names the server behind a connection. All speak OpenAI's Chat Completions
// API; the kind sets the defaults (base path, models path) and how the connection test reads
// tool-call support.
type ChatConnectionKind string

// Connection kinds.
const (
	ChatLMStudio  ChatConnectionKind = "lmstudio"  // LM Studio's local server
	ChatLiteLLM   ChatConnectionKind = "litellm"   // the LiteLLM proxy
	ChatOpenWebUI ChatConnectionKind = "openwebui" // Open WebUI's API
	ChatOpenAI    ChatConnectionKind = "openai"    // any other OpenAI-compatible server
)

// ChatConnection is one configured LLM endpoint.
type ChatConnection struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"`
	Kind    ChatConnectionKind `json:"kind"`
	BaseURL string             `json:"baseURL"`
	Model   string             `json:"model"`
	// Private is the user's mark that the endpoint is a local model or a private cloud: the chat
	// then reads what the window shows (Invocation.ReadsLikeUser). Off: the external agents' read
	// rules. Only the user sets it, in the window.
	Private bool `json:"private,omitempty"`
	// AllTools sends every command the chat may run as a tool of its own with each request, for
	// large cloud models (the owner's decision of 2026-10-15): about four times the core set's
	// tokens and more, which fill a local model's context. Off (the default): the core tools and
	// two meta-tools that find and run the rest. The rules are the same either way (D-2, the
	// confirmations, the Secret filter); only the user sets it, in the window.
	AllTools bool `json:"allTools,omitempty"`
	// CAFile names a PEM file of CA certificates for an https base URL whose server has a private
	// CA (the owner's decision of 2026-10-15): for this connection they replace the system's roots,
	// as a kubeconfig's certificate-authority does for kubectl (N141, the owner's decision in the
	// audit of 2026-10-16), read whenever a request is sent. An absolute path, not secret; empty:
	// the system's roots (on macOS its trust store).
	CAFile string `json:"caFile,omitempty"`
	// HasKey reports whether an API key for the connection is stored. A connection saved with a
	// key that is not stored (one kept in the macOS keychain before 2026-10-15) reports false.
	HasKey  bool       `json:"hasKey,omitempty"`
	Default bool       `json:"default,omitempty"`
	Limits  ChatLimits `json:"limits"`
}

// ChatLimits bound one turn (D-10; the owner's numbers of 2026-10-14 late evening). Zero fields
// take DefaultChatLimits' values.
type ChatLimits struct {
	// RepeatStop stops the turn at this many identical tool calls (the same tool with the same
	// canonical arguments) in one turn: a local model looping on one request.
	RepeatStop int `json:"repeatStop,omitempty"`
	// MaxToolCalls is the most tool calls a turn runs.
	MaxToolCalls int `json:"maxToolCalls,omitempty"`
	// MaxTokens is the most tokens a turn uses: prompt and answer over all its requests, as the
	// endpoint reports them, else estimated.
	MaxTokens int `json:"maxTokens,omitempty"`
	// MaxDuration is a turn's wall-clock limit.
	MaxDuration time.Duration `json:"maxDuration,omitempty"`
}

// DefaultChatLimits are the owner's defaults (2026-10-14 late evening).
var DefaultChatLimits = ChatLimits{RepeatStop: 3, MaxToolCalls: 25, MaxTokens: 300_000, MaxDuration: 10 * time.Minute}

// WithDefaults returns l with every zero field set from DefaultChatLimits.
func (l ChatLimits) WithDefaults() ChatLimits {
	d := DefaultChatLimits
	if l.RepeatStop > 0 {
		d.RepeatStop = l.RepeatStop
	}
	if l.MaxToolCalls > 0 {
		d.MaxToolCalls = l.MaxToolCalls
	}
	if l.MaxTokens > 0 {
		d.MaxTokens = l.MaxTokens
	}
	if l.MaxDuration > 0 {
		d.MaxDuration = l.MaxDuration
	}
	return d
}

// ChatTestLayer is one step of the connection test, in this order.
type ChatTestLayer string

// The connection test's layers (owner, 2026-10-14 late evening; DNS since the owner's VW8,
// 2026-10-23; HTTP's scheme and certificate, Auth asked without and with the key, and Models since
// his request of 2026-10-25: "Solche Infos brauche ich um die Verbindung schnell, einfach und sicher
// einschätzen zu können").
const (
	ChatTestDNS    ChatTestLayer = "dns"    // the host name resolves (its addresses listed); an IP address or localhost at once
	ChatTestTCP    ChatTestLayer = "tcp"    // one of those addresses answers on the port
	ChatTestHTTP   ChatTestLayer = "http"   // HTTP or HTTPS; over HTTPS the certificate verified (chain and host name)
	ChatTestAuth   ChatTestLayer = "auth"   // whether the endpoint wants a key (/models without one), and the key accepted
	ChatTestModels ChatTestLayer = "models" // the models list read, how many, the chosen model among them
	ChatTestTools  ChatTestLayer = "tools"  // a minimal request with one harmless tool returns a tool call
)

// ChatTestLayers is the connection test's order.
var ChatTestLayers = []ChatTestLayer{ChatTestDNS, ChatTestTCP, ChatTestHTTP, ChatTestAuth, ChatTestModels, ChatTestTools}

// ChatTestState is a test step's outcome.
type ChatTestState string

// Test step states.
const (
	ChatTestPending    ChatTestState = "pending"
	ChatTestRunning    ChatTestState = "running"
	ChatTestPass       ChatTestState = "pass"
	ChatTestFail       ChatTestState = "fail"
	ChatTestNotReached ChatTestState = "notReached" // an earlier layer failed
)

// ChatTestStep is one layer's result.
type ChatTestStep struct {
	Layer  ChatTestLayer `json:"layer"`
	State  ChatTestState `json:"state"`
	Detail string        `json:"detail,omitempty"` // the reason of a failure, or what passed ("HTTP 200, TLS 1.3")
	// Warning is a caution about a layer that passed, shown in the warning colour: plain http to
	// another machine (ChatPlainHTTPWarning), an endpoint that lists no models.
	Warning  string        `json:"warning,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
	// Models are the Models layer's: the endpoint's models, for the settings form's list.
	Models []string `json:"models,omitempty"`
	// Exchange is a failed layer's last request and its answer in curl -v's form (the owner,
	// 2026-10-26: which endpoint was called how): where the connection went, the request line,
	// header fields and body, the answer's status line, header fields and the start of its body;
	// the API key never in it. Empty for a layer that sent no request.
	Exchange string `json:"exchange,omitempty"`
}

// ChatPlainHTTPWarning is what the window and the connection test say about a connection over plain
// http to another machine (the owner's decision of 2026-10-15, H-95).
const ChatPlainHTTPWarning = "Plain http to another machine: not only the API key but the whole conversation, and every " +
	"Secret or other secret the agent reads, travel unencrypted over the network."

// ChatPlainHTTPRemote reports whether a connection's base URL is plain http to another machine: the
// http scheme and a host other than localhost or a loopback address (127.0.0.0/8, ::1), which the
// owner exempted (2026-10-15). Not a URL, or https: false.
func ChatPlainHTTPRemote(baseURL string) bool {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || !strings.EqualFold(u.Scheme, "http") || u.Host == "" {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return false
	}
	return true
}

// ChatConversationInfo is a conversation as the history lists it.
type ChatConversationInfo struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Context      string    `json:"context"` // the kubeconfig context it is bound to
	ConnectionID string    `json:"connectionID"`
	Updated      time.Time `json:"updated"`
	Running      bool      `json:"running,omitempty"` // a turn is running
	// Private reports that a turn of the conversation ran over a connection the user marked
	// private: the conversation lives in memory only, is never kept and is gone when Cogpit quits
	// (H-96, the owner's decision of 2026-10-15). It stays set when the conversation switches to
	// another connection, because its history holds what that turn read.
	Private bool `json:"private,omitempty"`
}

// ChatConversation is one conversation with what the window draws. Revision grows with every
// change; EventChat carries it.
type ChatConversation struct {
	ChatConversationInfo
	Revision uint64     `json:"revision"`
	Items    []ChatItem `json:"items"`
}

// ChatItemKind is what a conversation item is.
type ChatItemKind string

// Item kinds.
const (
	ChatItemUser      ChatItemKind = "user"      // the user's message
	ChatItemAssistant ChatItemKind = "assistant" // the model's answer text (Markdown)
	ChatItemTool      ChatItemKind = "tool"      // one tool call and its result
	ChatItemNote      ChatItemKind = "note"      // Cogpit's note: a limit ended the turn, the user stopped it, an error
)

// ChatItem is one entry of a conversation.
type ChatItem struct {
	Kind ChatItemKind `json:"kind"`
	// Text is the user's message, the answer (growing while Streaming), or the note.
	Text string `json:"text,omitempty"`
	// Attached is what the user's message carried besides its text, as the chip shows it
	// ("Pods · default/web-7d9f"); empty when nothing was attached.
	Attached  string        `json:"attached,omitempty"`
	Streaming bool          `json:"streaming,omitempty"`
	Tool      *ChatToolCall `json:"tool,omitempty"`
	// Sent is set on the user's item: what the turn sent to the endpoint so far.
	Sent *ChatSent `json:"sent,omitempty"`
	// NoteLevel is a note's level (LevelInfo, LevelWarning, LevelError).
	NoteLevel string `json:"noteLevel,omitempty"`
	// Timing is an answer's speed, set when it ended (the owner's request of 2026-10-25: TTFT and
	// OTPS under every answer).
	Timing *ChatTiming `json:"timing,omitempty"`
}

// ChatTiming is how fast one answer came: the time to its first token and its output tokens over
// the time they streamed.
type ChatTiming struct {
	// TTFT is the time from the request sent to the answer's first streamed part, thinking and
	// tool calls included.
	TTFT time.Duration `json:"ttft"`
	// Generating is the time from the first streamed part to the last.
	Generating time.Duration `json:"generating"`
	// OutputTokens are the answer's tokens as the endpoint reported them (usage.completion_tokens),
	// else estimated at about four bytes a token (Estimated).
	OutputTokens int  `json:"outputTokens"`
	Estimated    bool `json:"estimated,omitempty"`
}

// OTPS is the output tokens per second while the answer streamed; 0 when it cannot be told (no
// tokens, or the answer came in one part).
func (t ChatTiming) OTPS() float64 {
	if t.OutputTokens <= 0 || t.Generating <= 0 {
		return 0
	}
	return float64(t.OutputTokens) / t.Generating.Seconds()
}

// ChatToolState is a tool call's state.
type ChatToolState string

// Tool call states.
const (
	ChatToolRunning ChatToolState = "running"
	ChatToolWaiting ChatToolState = "waiting" // for the user's confirmation (D-2)
	ChatToolDone    ChatToolState = "done"
	ChatToolFailed  ChatToolState = "failed"
	ChatToolRefused ChatToolState = "refused" // the user declined, or a rule refused it
	ChatToolStopped ChatToolState = "stopped" // the kill switch or a limit ended it
)

// ChatToolCall is one tool call of the model, dispatched as a registry command.
type ChatToolCall struct {
	CallID  string        `json:"callID"`
	Command string        `json:"command"` // the registry id ("ui.navigate")
	Label   string        `json:"label"`   // the command's label
	Args    string        `json:"args"`    // the arguments as the model sent them (JSON)
	Summary string        `json:"summary"` // one line for the collapsed row
	State   ChatToolState `json:"state"`
	// Result is the result as the model was sent it, bounded for display; Error the failure.
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ChatSent sums up what a turn sent to the endpoint (SentPayload has the body).
type ChatSent struct {
	Requests    int    `json:"requests"`
	Bytes       int    `json:"bytes"`
	Messages    int    `json:"messages"`
	ToolResults int    `json:"toolResults"`
	Tokens      int    `json:"tokens,omitempty"`
	Endpoint    string `json:"endpoint"` // the endpoint's host
	Private     bool   `json:"private,omitempty"`
}

// ChatUserMessage is what the user sends.
type ChatUserMessage struct {
	Text string `json:"text"`
	// AttachSelection adds the current view's selection and drawer object to the turn (the chip
	// in the input box); the compact ui_state goes with every turn anyway.
	AttachSelection bool `json:"attachSelection,omitempty"`
}

// EventChat: a conversation changed (Event.View is its id, Event.Revision its revision), or the
// list of a context's conversations or the connections changed (Event.View empty). Re-read with
// Session.Chat.
const EventChat EventType = "chat"

// chatSlot holds the session's chat agent.
type chatSlot struct {
	mu sync.Mutex
	a  ChatAgent
}

// SetChatAgent wires the built-in agent in. Called once by the desktop binary; a session without
// one (the CLI's, a headless bridge's) has no chat.
func (s *Session) SetChatAgent(a ChatAgent) {
	s.chat.mu.Lock()
	defer s.chat.mu.Unlock()
	s.chat.a = a
}

// Chat returns the session's chat agent; nil without one. For the window only.
func (s *Session) Chat() ChatAgent {
	s.chat.mu.Lock()
	defer s.chat.mu.Unlock()
	return s.chat.a
}

// NotifyChat emits EventChat for a conversation (id empty: the lists changed). The chat agent
// calls it after every change.
func (s *Session) NotifyChat(conversationID string, revision uint64) {
	s.emit(Event{Type: EventChat, View: conversationID, Revision: revision})
}
