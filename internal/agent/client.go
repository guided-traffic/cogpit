package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The LLM client: the subset of OpenAI's Chat Completions API the chat uses (POST
// <chat path> with tools and stream: true, GET <models path>), on plain net/http (D-10: the lead's
// choice over github.com/openai/openai-go, for the owner to confirm). Servers differ in small
// ways, and the client takes each the way it comes: tool-call deltas accumulated by index (or by
// id where a server leaves the index out), a JSON body where an SSE stream was asked for, an
// error object inside the stream, thinking as reasoning_content or reasoning deltas or as
// <think>…</think> in the content (dropped, never part of the answer). Errors carry the request's
// method and URL, the HTTP status and at most a short message of the server's, with the API key
// removed; never the key and never the request body (the connection test's record of a failed
// layer, exchange.go, shows its fixed body).

const (
	// maxEventBytes bounds one server-sent event (and a JSON answer); a model's delta is far smaller.
	maxEventBytes = 8 << 20
	// maxModelsBytes bounds the models list.
	maxModelsBytes = 4 << 20
	// maxErrorBody is how much of an error answer is read for its message.
	maxErrorBody = 4 << 10
	// maxErrorMessage bounds the server's message in an error.
	maxErrorMessage = 200
)

// DefaultBaseURL is the base URL a new connection of the kind starts with, from each server's
// documentation (verified 2026-10-14, docs/developer/decisions.md › D-10): LM Studio serves
// /v1 on port 1234, LiteLLM's proxy listens on 4000, Open WebUI's docs use port 3000.
func DefaultBaseURL(kind engine.ChatConnectionKind) string {
	switch kind {
	case engine.ChatLMStudio:
		return "http://localhost:1234/v1"
	case engine.ChatLiteLLM:
		return "http://localhost:4000"
	case engine.ChatOpenWebUI:
		return "http://localhost:3000"
	}
	return "https://api.openai.com/v1"
}

const schemeHTTPS = "https"

// endpoint is where a connection's requests go.
type endpoint struct {
	chat   string // the Chat Completions URL
	models string // the models list's URL
	host   string // host[:port], for what the chat shows was sent where
	https  bool
	// plainRemote: plain http to another machine (engine.ChatPlainHTTPRemote), which the connection
	// test warns about.
	plainRemote bool
}

// errBaseURL is the reason a base URL is refused.
var errBaseURL = errors.New("the base URL must be an http or https URL with a host, and without a user, password, query or fragment")

// endpointFor derives the URLs from the connection's kind and base URL: LM Studio, LiteLLM and other
// OpenAI-compatible servers take <base>/chat/completions and <base>/models (LiteLLM serves both
// with and without /v1, so either base works); Open WebUI takes <base>/api/chat/completions and
// <base>/api/models (a base that ends in /api is taken as the server's root plus /api).
func endpointFor(kind engine.ChatConnectionKind, base string) (endpoint, error) {
	u, err := parseBaseURL(base)
	if err != nil {
		return endpoint{}, err
	}
	root := strings.TrimRight(u.String(), "/")
	chat, models := root+"/chat/completions", root+"/models"
	if kind == engine.ChatOpenWebUI {
		root = strings.TrimSuffix(root, "/api")
		chat, models = root+"/api/chat/completions", root+"/api/models"
	}
	return endpoint{chat: chat, models: models, host: u.Host, https: u.Scheme == schemeHTTPS,
		plainRemote: engine.ChatPlainHTTPRemote(u.String())}, nil
}

// parseBaseURL checks a base URL: http or https, a host, no credentials (they would land in the
// connections file), no query or fragment.
func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errBaseURL
	}
	if (u.Scheme != "http" && u.Scheme != schemeHTTPS) || u.Host == "" || u.User != nil || u.RawQuery != "" ||
		u.Fragment != "" || u.Opaque != "" {
		return nil, errBaseURL
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

// message is one Chat Completions message.
type message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// toolCall is one call of a function tool.
type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// toolDef is a function tool offered to the model.
type toolDef struct {
	Type     string      `json:"type"`
	Function toolDefFunc `json:"function"`
}

type toolDefFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// chatRequest is the request body.
type chatRequest struct {
	Model         string         `json:"model"`
	Messages      []message      `json:"messages"`
	Tools         []toolDef      `json:"tools,omitempty"`
	Stream        bool           `json:"stream"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// usage is the token count an endpoint reports.
type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (u *usage) total() int {
	if u == nil {
		return 0
	}
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.PromptTokens + u.CompletionTokens
}

// HTTPError is an answer of the endpoint with an error status. Message is at most a short excerpt
// of the server's own message, with the API key removed; 401 and 403 carry none. Method and URL
// are the request's, which the error names first (the owner, 2026-10-26: an error that does not
// say which endpoint was called how leaves nothing to look for).
type HTTPError struct {
	Status  int
	Message string
	Method  string
	URL     string
}

func (e *HTTPError) Error() string {
	text := http.StatusText(e.Status)
	where := ""
	if e.URL != "" {
		where = e.Method + " " + e.URL + ": "
	}
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Sprintf("%sthe endpoint refused the API key (HTTP %d %s)", where, e.Status, text)
	}
	msg := fmt.Sprintf("%sthe endpoint answered HTTP %d %s", where, e.Status, text)
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// httpErrorFrom reads an error answer's message, bounded and without the key.
func httpErrorFrom(resp *http.Response, key string) *HTTPError {
	e := &HTTPError{Status: resp.StatusCode}
	if resp.Request != nil && resp.Request.URL != nil {
		e.Method, e.URL = resp.Request.Method, resp.Request.URL.String()
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return e // the message may quote a part of the key
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	e.Message = serverMessage(body, key)
	return e
}

// serverMessage is the message of an error body: OpenAI's {"error": {"message"}} or {"error":
// "…"} or {"detail": "…"}, else the text; one line, bounded, the key removed.
func serverMessage(body []byte, key string) string {
	var v struct {
		Error  json.RawMessage `json:"error"`
		Detail json.RawMessage `json:"detail"`
		Msg    string          `json:"message"`
	}
	msg := ""
	if json.Unmarshal(body, &v) == nil {
		var obj struct {
			Message string `json:"message"`
		}
		var s string
		switch {
		case json.Unmarshal(v.Error, &obj) == nil && obj.Message != "":
			msg = obj.Message
		case json.Unmarshal(v.Error, &s) == nil && s != "":
			msg = s
		case json.Unmarshal(v.Detail, &s) == nil && s != "":
			msg = s
		case v.Msg != "":
			msg = v.Msg
		}
	} else {
		msg = string(body)
		if strings.HasPrefix(strings.TrimSpace(msg), "<") {
			msg = htmlTag.ReplaceAllString(msg, " ") // a proxy's error page: its text, not its tags (2026-10-25)
		}
	}
	return cleanMessage(msg, key)
}

// cleanMessage makes a server's text one bounded line without the key.
func cleanMessage(msg, key string) string {
	if key != "" {
		msg = strings.ReplaceAll(msg, key, "•••")
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > maxErrorMessage {
		msg = truncateUTF8(msg, maxErrorMessage) + "…"
	}
	return msg
}

// client sends one connection's requests.
type client struct {
	hc     *http.Client
	ep     endpoint
	key    string
	caFile string // the connection's CA file, the only roots its TLS trusts; "" for the system's
}

// errNoStreamOptions marks a server that refused stream_options (400 naming it): the request is
// sent again without.
var errNoStreamOptions = errors.New("the endpoint does not take stream_options")

// response is one streamed answer.
type response struct {
	Text         string     // the answer, thinking removed
	ToolCalls    []toolCall // the tool calls, in index order
	FinishReason string
	Usage        *usage
	Bytes        int  // bytes of the answer read
	Thought      bool // the model thought (reasoning deltas or <think>), which was dropped
	// First and Last are when the answer's first and last parts came (content, thinking or a tool
	// call); OutBytes their bytes, for an estimate of the output tokens without usage.
	First, Last time.Time
	OutBytes    int
}

// complete posts body and reads the answer, streamed or not. onText, if not nil, receives the answer
// so far whenever its visible text grew.
func (c *client) complete(ctx context.Context, body []byte, onText func(string)) (response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ep.chat, bytes.NewReader(body))
	if err != nil {
		return response{}, fmt.Errorf("the request could not be built: %w", errBaseURL)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return response{}, transportError(ctx, err, c.key, c.caFile)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		he := httpErrorFrom(resp, c.key)
		if he.Status == http.StatusBadRequest && strings.Contains(strings.ToLower(he.Message), "stream_options") {
			return response{}, errNoStreamOptions
		}
		return response{}, he
	}
	acc := newAccumulator(onText)
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if ct == "application/json" {
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxEventBytes+1))
		if err != nil {
			return response{}, transportError(ctx, err, c.key, c.caFile)
		}
		if len(data) > maxEventBytes {
			return response{}, errors.New("the endpoint's answer is larger than 8 MiB")
		}
		acc.bytes = len(data)
		if err := acc.event(data, c.key); err != nil {
			return response{}, err
		}
		return acc.result(), nil
	}
	if err := readSSE(resp.Body, func(data []byte) error { return acc.event(data, c.key) }, func(n int) { acc.bytes += n }); err != nil {
		if errors.Is(err, errStreamDone) {
			return acc.result(), nil
		}
		var se *streamError
		if errors.As(err, &se) {
			return response{}, err
		}
		return response{}, transportError(ctx, err, c.key, c.caFile)
	}
	return acc.result(), nil
}

// transportError is a failed request in words, the context's end named as such; the key removed
// (Go's errors quote the URL, never a header, but a key in a base URL is refused anyway). A server
// certificate that could not be verified says so and where a private CA goes; with a CA file, that
// the connection trusts that file alone (caFile), so a server with a public certificate needs the
// field cleared.
func transportError(ctx context.Context, err error, key, caFile string) error {
	if ctxErr := context.Cause(ctx); ctxErr != nil {
		return ctxErr
	}
	var cv *tls.CertificateVerificationError
	if errors.As(err, &cv) {
		why := cleanMessage(cv.Err.Error(), key)
		if caFile != "" {
			return errors.New("the endpoint's certificate is not trusted (" + why + "): this connection trusts only its CA file " +
				caFile + ", not the system's certificate authorities; for a server with a public certificate, clear the CA file " +
				"in the connection's settings")
		}
		return errors.New("the endpoint's certificate is not trusted (" + why +
			"): for a server with a private CA, name its CA file in the connection's settings")
	}
	return errors.New("the endpoint could not be reached: " + cleanMessage(err.Error(), key))
}

// errStreamDone ends the stream at its [DONE] event.
var errStreamDone = errors.New("done")

// streamError is an error the server sent inside the stream.
type streamError struct{ msg string }

func (e *streamError) Error() string { return "the endpoint reported an error: " + e.msg }

// readSSE reads server-sent events and hands each event's data to fn: the data lines of an event
// joined by newlines, comments and other fields skipped. counted receives the bytes read.
func readSSE(r io.Reader, fn func([]byte) error, counted func(int)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxEventBytes)
	var data []byte
	pending := false
	dispatch := func() error {
		if !pending {
			return nil
		}
		d := data
		data, pending = data[:0], false
		if string(bytes.TrimSpace(d)) == "[DONE]" {
			return errStreamDone
		}
		return fn(d)
	}
	for sc.Scan() {
		line := sc.Bytes()
		counted(len(line) + 1)
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) == 0 {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, _ := bytes.Cut(line, []byte(":"))
		if string(field) != "data" {
			continue
		}
		value = bytes.TrimPrefix(value, []byte(" "))
		if pending {
			data = append(data, '\n')
		}
		data = append(data, value...)
		pending = true
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return dispatch()
}

// chunk is one streamed event, or a whole answer.
type chunk struct {
	Choices []struct {
		Delta        *delta  `json:"delta"`
		Message      *delta  `json:"message"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usage          `json:"usage"`
	Error json.RawMessage `json:"error"`
}

// delta is a streamed delta, or a whole message.
type delta struct {
	Content          *string         `json:"content"`
	ReasoningContent *string         `json:"reasoning_content"`
	Reasoning        json.RawMessage `json:"reasoning"`
	ToolCalls        []struct {
		Index    *int   `json:"index"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

// accumulator builds a response from the events.
type accumulator struct {
	think  thinkFilter
	calls  map[int]*toolCall
	order  []int
	last   int
	finish string
	usage  *usage
	bytes  int
	shown  string
	onText func(string)
	// firstAt and lastAt are when a part of the answer came (TTFT and OTPS, ChatTiming); out its
	// bytes.
	firstAt, lastAt time.Time
	out             int
}

func newAccumulator(onText func(string)) *accumulator {
	return &accumulator{calls: map[int]*toolCall{}, last: -1, onText: onText}
}

// event takes one event's data.
func (a *accumulator) event(data []byte, key string) error {
	var c chunk
	if err := json.Unmarshal(data, &c); err != nil {
		return &streamError{msg: "an event that is not JSON"}
	}
	if len(c.Error) > 0 && string(c.Error) != "null" {
		return &streamError{msg: serverMessage(append(append([]byte(`{"error":`), c.Error...), '}'), key)}
	}
	if c.Usage != nil {
		a.usage = c.Usage
	}
	for _, ch := range c.Choices {
		d := ch.Delta
		if d == nil {
			d = ch.Message
		}
		if d != nil {
			a.delta(d)
		}
		if ch.FinishReason != nil && *ch.FinishReason != "" {
			a.finish = *ch.FinishReason
		}
	}
	if a.onText != nil {
		if t := a.think.text(); t != a.shown {
			a.shown = t
			a.onText(t)
		}
	}
	return nil
}

func (a *accumulator) delta(d *delta) {
	n := 0
	if (d.ReasoningContent != nil && *d.ReasoningContent != "") || reasoningText(d.Reasoning) {
		a.think.thought = true
		if d.ReasoningContent != nil {
			n += len(*d.ReasoningContent)
		} else {
			n += len(d.Reasoning)
		}
	}
	if d.Content != nil {
		a.think.write(*d.Content)
		n += len(*d.Content)
	}
	for _, tc := range d.ToolCalls {
		n += len(tc.Function.Name) + len(tc.Function.Arguments)
	}
	if n > 0 {
		now := time.Now()
		if a.firstAt.IsZero() {
			a.firstAt = now
		}
		a.lastAt, a.out = now, a.out+n
	}
	for _, tc := range d.ToolCalls {
		idx := a.callIndex(tc.Index, tc.ID)
		call := a.calls[idx]
		if call == nil {
			call = &toolCall{Type: typeFunction}
			a.calls[idx] = call
			a.order = append(a.order, idx)
		}
		a.last = idx
		if tc.ID != "" && call.ID == "" {
			call.ID = tc.ID
		}
		switch name := tc.Function.Name; {
		case name == "" || name == call.Function.Name:
		case call.Function.Name == "":
			call.Function.Name = name
		default: // a name split over deltas
			call.Function.Name += name
		}
		call.Function.Arguments += tc.Function.Arguments
	}
}

// callIndex is the index a tool-call delta belongs to: its own; else the call with its id; else a
// new call when it brings an id, the last one when it does not.
func (a *accumulator) callIndex(index *int, id string) int {
	if index != nil {
		return *index
	}
	if id != "" {
		for i, c := range a.calls {
			if c.ID == id {
				return i
			}
		}
		n := 1 << 20 // beyond any index a server sends
		for _, i := range a.order {
			n = max(n, i+1)
		}
		return n
	}
	if a.last >= 0 {
		return a.last
	}
	return 0
}

// reasoningText reports whether a reasoning field holds text (a string, or an object with some).
func reasoningText(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != `""` && s != "{}"
}

func (a *accumulator) result() response {
	a.think.flush()
	r := response{Text: a.think.text(), FinishReason: a.finish, Usage: a.usage, Bytes: a.bytes, Thought: a.think.thought,
		First: a.firstAt, Last: a.lastAt, OutBytes: a.out}
	order := slices.Clone(a.order)
	sort.Ints(order)
	for n, i := range order {
		c := *a.calls[i]
		if c.Function.Name == "" {
			continue
		}
		if c.ID == "" {
			c.ID = "call_" + strconv.Itoa(n)
		}
		r.ToolCalls = append(r.ToolCalls, c)
	}
	return r
}

// models lists the endpoint's models: OpenAI's {"data": [{"id"}]} (LM Studio, LiteLLM, Open WebUI),
// a bare array, or entries with a name instead of an id.
func (c *client) listModels(ctx context.Context) ([]string, error) {
	resp, err := c.get(ctx, c.ep.models, true)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, httpErrorFrom(resp, c.key)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxModelsBytes+1))
	if err != nil {
		return nil, transportError(ctx, err, c.key, c.caFile)
	}
	if len(data) > maxModelsBytes {
		return nil, errors.New("the endpoint's models list is larger than 4 MiB")
	}
	return parseModels(data)
}

type modelEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func parseModels(data []byte) ([]string, error) {
	var wrapped struct {
		Data   []modelEntry `json:"data"`
		Models []modelEntry `json:"models"`
	}
	var entries []modelEntry
	if err := json.Unmarshal(data, &wrapped); err == nil {
		entries = append(wrapped.Data, wrapped.Models...)
	} else if err := json.Unmarshal(data, &entries); err != nil {
		return nil, errors.New("the endpoint's models list is not the JSON of an OpenAI-compatible API")
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		id := e.ID
		if id == "" {
			id = e.Name
		}
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// get sends a GET, with the key when withKey.
func (c *client) get(ctx context.Context, u string, withKey bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, errBaseURL
	}
	req.Header.Set("Accept", "application/json")
	if withKey && c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, transportError(ctx, err, c.key, c.caFile)
	}
	return resp, nil
}

// newHTTPClient is the chat's HTTP client: Go's default transport (proxies from the environment),
// no overall timeout (a turn's context bounds every request, a first answer of a local model can
// take half a minute), and no redirects, so that the key goes to the configured endpoint only.
func newHTTPClient() *http.Client {
	rt := http.DefaultTransport
	if tr, ok := rt.(*http.Transport); ok {
		rt = tr.Clone()
	}
	return &http.Client{
		Transport: rt,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errRedirect
		},
	}
}

var errRedirect = errors.New("the endpoint redirected; set the base URL to where it points")

// truncateUTF8 cuts s to at most n bytes at a rune boundary.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && !utf8Start(s[n]) {
		n--
	}
	return s[:n]
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
