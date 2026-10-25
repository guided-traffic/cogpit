package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Files the agent keeps in Cogpit's configuration directory, owner-only (0600 in 0700).
const (
	// ConnectionsFile holds the LLM connections: name, kind, base URL, model, limits, the private
	// mark, whether a key was given. Never a key.
	ConnectionsFile = "llm-connections.json"
	// KeysFile holds the connections' API keys, the connection's id to its key (FileKeyStore): the
	// owner's decision of 2026-10-15, no keychain (D-10).
	KeysFile = "llm-keys.json"
	// ConversationsFile holds the last MaxConversations conversations per context, scrubbed; never
	// a private one (H-96).
	ConversationsFile = "chat-conversations.json"
)

// MaxConversations is how many conversations of a context are kept (D-10).
const MaxConversations = 20

// Errors of the chat. They carry no key and no cluster data.
var (
	ErrNoConnection = errors.New("no LLM connection: add one in the chat's settings")
	ErrUnknownConn  = errors.New("no such LLM connection")
	ErrUnknownConv  = errors.New("no such conversation")
	ErrTurnRunning  = errors.New("a turn of this conversation is still running: stop it or wait for it")
	ErrOtherContext = errors.New("this conversation belongs to another cluster than the connected one: connect its context to go on, or start a new conversation")
	ErrNotConnected = errors.New("no cluster is connected: connect the conversation's context to go on")
	ErrEmptyMessage = errors.New("the message is empty")
	// ErrNotUserMessage: Resend names an item that is not a message of the user's.
	ErrNotUserMessage   = errors.New("only a message of yours can be edited and sent again")
	ErrConnectionFields = errors.New("a connection needs a name, a kind and a base URL")
	// ErrCAFileScheme: a CA file is checked against an https server's certificate only.
	ErrCAFileScheme = errors.New("a CA file is for an https base URL: plain http checks no certificate")
	// ErrCAFilePath: the CA file is named by an absolute path.
	ErrCAFilePath = errors.New("the CA file must be an absolute path")
	ErrNoModel    = errors.New("the connection has no model: choose one in the chat's settings")
	// ErrKeyMissing: the connection was saved with an API key that is not stored (any more): a key
	// kept in the macOS keychain before 2026-10-15, which Cogpit no longer reads, or a key file
	// removed by hand.
	ErrKeyMissing = errors.New("the connection's API key is missing: enter it again in the chat's settings")
)

// Options configure the agent.
type Options struct {
	// Session is the engine the chat's tool calls are dispatched to.
	Session *engine.Session
	// Dir is Cogpit's configuration directory (app.Dirs.Config), where ConnectionsFile,
	// ConversationsFile and KeysFile are kept; empty keeps them in memory only.
	Dir string
	// Keys keeps the API keys; nil means the owner-only KeysFile in Dir (NewFileKeyStore), or
	// memory when Dir is empty.
	Keys KeyStore
	// HTTPClient sends the requests; nil means a client without redirects (newHTTPClient).
	HTTPClient *http.Client
	// Logger receives the agent's logs (never a key, a message or a tool result); nil means the
	// session's.
	Logger *slog.Logger
	// Now is the clock; nil means the session's.
	Now func() time.Time
	// NotifyEvery is the least time between two EventChat of a conversation while an answer
	// streams; zero means 75 ms. A state change notifies at once.
	NotifyEvery time.Duration
}

// Agent implements engine.ChatAgent.
type Agent struct {
	s    *engine.Session
	opts Options
	log  *slog.Logger
	keys KeyStore
	hc   *http.Client

	toolsOnce sync.Once
	tools     *toolset
	toolsErr  error

	mu        sync.Mutex
	conns     []engine.ChatConnection // as stored: HasKey says a key was given (keyLost: not stored)
	keyLost   map[string]bool         // connections saved with a key the key store does not hold
	convs     map[string]*conversation
	noOptions map[string]bool     // connections whose endpoint refused stream_options
	caClients map[string]caClient // a connection's client with its CA file, by id and path

	saveMu sync.Mutex // serialises the files' writes
}

var _ engine.ChatAgent = (*Agent)(nil)

// New returns the agent with the connections and conversations kept in opts.Dir. A file that
// cannot be read is logged and left alone (the agent starts empty and does not overwrite it until
// something changes).
func New(opts Options) *Agent {
	if opts.Logger == nil {
		opts.Logger = opts.Session.Logger()
	}
	if opts.Keys == nil {
		if opts.Dir != "" {
			opts.Keys = NewFileKeyStore(filepath.Join(opts.Dir, KeysFile), opts.Logger)
		} else {
			opts.Keys = NewMemoryKeyStore()
		}
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = newHTTPClient()
	}
	if opts.Now == nil {
		opts.Now = opts.Session.Now
	}
	if opts.NotifyEvery <= 0 {
		opts.NotifyEvery = 75 * time.Millisecond
	}
	a := &Agent{s: opts.Session, opts: opts, log: opts.Logger, keys: opts.Keys, hc: opts.HTTPClient,
		keyLost: map[string]bool{}, convs: map[string]*conversation{}, noOptions: map[string]bool{}, caClients: map[string]caClient{}}
	a.loadConnections()
	a.checkKeys()
	a.loadConversations()
	return a
}

// checkKeys marks the connections saved with a key the key store does not hold: a key kept in the
// macOS keychain before 2026-10-15, which Cogpit no longer reads (D-10: reading it would ask for
// the login password), or a key file removed by hand. They show "no API key" (Connections), and a
// turn asks for the key again (ErrKeyMissing). Called by New, before anything else can run.
func (a *Agent) checkKeys() {
	for _, c := range a.conns {
		if !c.HasKey {
			continue
		}
		switch _, err := a.keys.Get(c.ID); {
		case errors.Is(err, ErrNoKey):
			a.keyLost[c.ID] = true
		case err != nil:
			a.log.Warn("the chat's API keys could not be read", "err", err)
			return
		}
	}
	if n := len(a.keyLost); n > 0 {
		a.log.Info("connections of the chat whose API key is not stored: enter it again in the chat's settings", "connections", n)
	}
}

// toolset returns the tools, built on first use (the registry is complete by then).
func (a *Agent) toolset() (*toolset, error) {
	a.toolsOnce.Do(func() { a.tools, a.toolsErr = newToolset(a.s.Registry()) })
	return a.tools, a.toolsErr
}

// newID returns a random id.
func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// --- connections ---

// Connections implements engine.ChatAgent: the default connection first, then by name; HasKey
// only for a connection whose key is stored.
func (a *Agent) Connections() ([]engine.ChatConnection, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.connectionsLocked()
	for i := range out {
		out[i] = a.shownLocked(out[i])
	}
	return out, nil
}

// shownLocked is a stored connection as the window sees it: a key that was given but is not
// stored is no key. a.mu is held.
func (a *Agent) shownLocked(c engine.ChatConnection) engine.ChatConnection {
	c.HasKey = c.HasKey && !a.keyLost[c.ID]
	return c
}

func (a *Agent) connectionsLocked() []engine.ChatConnection {
	out := slices.Clone(a.conns)
	slices.SortStableFunc(out, func(x, y engine.ChatConnection) int {
		if x.Default != y.Default {
			if x.Default {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
	})
	return out
}

// validKind reports whether k is one of the kinds.
func validKind(k engine.ChatConnectionKind) bool {
	switch k {
	case engine.ChatLMStudio, engine.ChatLiteLLM, engine.ChatOpenWebUI, engine.ChatOpenAI:
		return true
	}
	return false
}

// normalize checks a connection's fields and tidies them.
func normalize(c engine.ChatConnection) (engine.ChatConnection, error) {
	c.Name, c.Model = strings.TrimSpace(c.Name), strings.TrimSpace(c.Model)
	// The model may be chosen later: the settings form lists the endpoint's models for a saved
	// connection (Models takes its id); a turn refuses to start without one.
	if c.Name == "" || !validKind(c.Kind) || strings.TrimSpace(c.BaseURL) == "" {
		return c, ErrConnectionFields
	}
	u, err := parseBaseURL(c.BaseURL)
	if err != nil {
		return c, err
	}
	c.BaseURL = u.String()
	if c.CAFile = strings.TrimSpace(c.CAFile); c.CAFile != "" {
		switch {
		case u.Scheme != schemeHTTPS:
			return c, ErrCAFileScheme
		case !filepath.IsAbs(c.CAFile):
			return c, ErrCAFilePath
		}
		c.CAFile = filepath.Clean(c.CAFile)
	}
	if c.Limits.RepeatStop < 0 || c.Limits.MaxToolCalls < 0 || c.Limits.MaxTokens < 0 || c.Limits.MaxDuration < 0 {
		return c, errors.New("a connection's limits cannot be negative")
	}
	return c, nil
}

// SaveConnection implements engine.ChatAgent.
func (a *Agent) SaveConnection(c engine.ChatConnection, apiKey *string) (engine.ChatConnection, error) {
	c, err := normalize(c)
	if err != nil {
		return engine.ChatConnection{}, err
	}
	wantDefault := c.Default
	a.mu.Lock()
	i := -1
	if c.ID != "" {
		i = slices.IndexFunc(a.conns, func(x engine.ChatConnection) bool { return x.ID == c.ID })
		if i < 0 {
			a.mu.Unlock()
			return engine.ChatConnection{}, ErrUnknownConn
		}
		c.HasKey = a.conns[i].HasKey
		c.Default = a.conns[i].Default
	} else {
		c.ID, c.HasKey = newID(), false
		c.Default = len(a.conns) == 0
	}
	a.mu.Unlock()

	// The key goes to the key store first: a connection never claims a key it does not have. nil
	// keeps what is stored, a key that went missing included (the chat's model menu saves with nil).
	if apiKey != nil {
		if *apiKey == "" {
			if err := a.keys.Delete(c.ID); err != nil {
				return engine.ChatConnection{}, err
			}
			c.HasKey = false
		} else {
			if err := a.keys.Set(c.ID, *apiKey); err != nil {
				return engine.ChatConnection{}, err
			}
			c.HasKey = true
		}
	}

	a.mu.Lock()
	if i >= 0 {
		// The list may have changed while the key store was written: find it again.
		if i = slices.IndexFunc(a.conns, func(x engine.ChatConnection) bool { return x.ID == c.ID }); i < 0 {
			a.mu.Unlock()
			_ = a.keys.Delete(c.ID)
			return engine.ChatConnection{}, ErrUnknownConn
		}
		a.conns[i] = c
	} else {
		a.conns = append(a.conns, c)
	}
	if wantDefault {
		for i := range a.conns {
			a.conns[i].Default = a.conns[i].ID == c.ID
		}
		c.Default = true
	}
	delete(a.noOptions, c.ID) // the endpoint may have changed
	if apiKey != nil {
		delete(a.keyLost, c.ID)
	}
	c = a.shownLocked(c)
	a.mu.Unlock()
	a.saveConnections()
	a.s.NotifyChat("", 0)
	return c, nil
}

// DeleteConnection implements engine.ChatAgent.
func (a *Agent) DeleteConnection(id string) error {
	a.mu.Lock()
	i := slices.IndexFunc(a.conns, func(x engine.ChatConnection) bool { return x.ID == id })
	if i < 0 {
		a.mu.Unlock()
		return ErrUnknownConn
	}
	wasDefault := a.conns[i].Default
	a.conns = slices.Delete(a.conns, i, i+1)
	if wasDefault && len(a.conns) > 0 {
		a.conns[0].Default = true
	}
	delete(a.keyLost, id)
	for key, cc := range a.caClients {
		if strings.HasPrefix(key, id+"\x00") {
			cc.hc.CloseIdleConnections()
			delete(a.caClients, key)
		}
	}
	a.mu.Unlock()
	err := a.keys.Delete(id)
	a.saveConnections()
	a.s.NotifyChat("", 0)
	return err
}

// SetDefaultConnection implements engine.ChatAgent.
func (a *Agent) SetDefaultConnection(id string) error {
	a.mu.Lock()
	if !slices.ContainsFunc(a.conns, func(x engine.ChatConnection) bool { return x.ID == id }) {
		a.mu.Unlock()
		return ErrUnknownConn
	}
	for i := range a.conns {
		a.conns[i].Default = a.conns[i].ID == id
	}
	a.mu.Unlock()
	a.saveConnections()
	a.s.NotifyChat("", 0)
	return nil
}

// connection returns a connection by id; "" or an id that is gone gives the default one.
func (a *Agent) connectionLocked(id string) (engine.ChatConnection, bool) {
	if i := slices.IndexFunc(a.conns, func(x engine.ChatConnection) bool { return x.ID == id }); i >= 0 && id != "" {
		return a.conns[i], true
	}
	if i := slices.IndexFunc(a.conns, func(x engine.ChatConnection) bool { return x.Default }); i >= 0 {
		return a.conns[i], true
	}
	if len(a.conns) > 0 {
		return a.conns[0], true
	}
	return engine.ChatConnection{}, false
}

// keyFor returns the stored key of a connection (as stored) that was given one: ErrKeyMissing
// when the key is not stored.
func (a *Agent) keyFor(c engine.ChatConnection) (string, error) {
	if !c.HasKey {
		return "", nil
	}
	k, err := a.keys.Get(c.ID)
	if errors.Is(err, ErrNoKey) {
		return "", ErrKeyMissing
	}
	return k, err
}

// clientFor returns a client for a connection with key: the connection's CA file read now.
func (a *Agent) clientFor(c engine.ChatConnection, key string) (*client, error) {
	ep, err := endpointFor(c.Kind, c.BaseURL)
	if err != nil {
		return nil, err
	}
	hc, err := a.httpClientFor(c)
	if err != nil {
		return nil, err
	}
	return &client{hc: hc, ep: ep, key: key, caFile: c.CAFile}, nil
}

// ModelsOf implements engine.ChatAgent: the settings form's list from the values typed, the key
// typed or else the stored one (the owner's request of 2026-10-25: a colleague's list stayed empty
// after he typed his credentials, as it was read from the saved connection only).
func (a *Agent) ModelsOf(ctx context.Context, c engine.ChatConnection, apiKey *string) ([]string, error) {
	if !validKind(c.Kind) {
		return nil, errors.New("unknown connection kind")
	}
	if _, err := normalize(c); errors.Is(err, ErrCAFileScheme) || errors.Is(err, ErrCAFilePath) {
		return nil, err
	}
	key, err := a.testKey(c, apiKey)
	if err != nil {
		return nil, err
	}
	cl, err := a.clientFor(c, key)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return cl.listModels(ctx)
}

// Models implements engine.ChatAgent.
func (a *Agent) Models(ctx context.Context, connectionID string) ([]string, error) {
	a.mu.Lock()
	i := slices.IndexFunc(a.conns, func(x engine.ChatConnection) bool { return x.ID == connectionID })
	var c engine.ChatConnection
	if i >= 0 {
		c = a.conns[i]
	}
	a.mu.Unlock()
	if i < 0 {
		return nil, ErrUnknownConn
	}
	key, err := a.keyFor(c)
	if err != nil {
		return nil, err
	}
	cl, err := a.clientFor(c, key)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	models, err := cl.listModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("models of %s: %w", c.Name, err)
	}
	return models, nil
}
