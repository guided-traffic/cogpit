// Package fakellm is a fake OpenAI-compatible Chat Completions server for the built-in agent's
// tests (internal/agent): scripted replies (text, streamed deltas, reasoning, tool calls, usage),
// error statuses, a gate that holds a stream open until the test releases it, and a record of every
// request. It never sleeps: a test waits on conditions. Test-only code, as all of testkit.
package fakellm

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ToolCall is a tool call a reply makes.
type ToolCall struct {
	ID   string // empty: "call_<n>"
	Name string
	Args string // JSON; streamed in two pieces
}

// Usage is the token count a reply reports in its last event.
type Usage struct {
	Prompt, Completion int
}

// Reply is one scripted answer to a chat request.
type Reply struct {
	// Status, when not 0 or 200, answers with this status and Body (a JSON error by default).
	Status int
	Body   string
	// Reasoning are reasoning_content deltas sent before the text.
	Reasoning []string
	// Text are content deltas, each its own event.
	Text []string
	// ToolCalls are streamed after the text, each as a delta with its name and then its arguments
	// in two pieces.
	ToolCalls []ToolCall
	// Usage, if set, is reported in a last event without choices (stream_options.include_usage).
	Usage *Usage
	// JSON answers with one application/json body instead of a stream.
	JSON bool
	// StreamError sends an error object inside the stream after the text.
	StreamError string
	// Gate, if set, holds the stream open after the first event until it is closed or the client
	// goes away (Server.Cancelled counts that).
	Gate chan struct{}
	// Started, if set, is closed once the first event is written.
	Started chan struct{}
	// Pace, if set, is waited before each text delta after the first: an answer that streams over
	// time (an answer's OTPS).
	Pace time.Duration
}

// Request is one request the server received.
type Request struct {
	Method string
	Path   string
	Auth   string // the Authorization header
	Body   []byte
}

// JSON decodes the request's body.
func (r Request) JSON() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(r.Body, &m)
	return m
}

// Server is the fake endpoint. Base is the URL a connection of kind LM Studio or "openai" takes
// (".../v1"); Root the server's root (LiteLLM, Open WebUI).
type Server struct {
	Base string
	Root string

	srv     *httptest.Server
	closing chan struct{} // closed when the test ends: gated streams give up

	mu        sync.Mutex
	key       string
	models    []string
	modelsSt  int
	replies   []Reply
	requests  []Request
	cancelled int
}

// New starts a server, closed when the test ends. Without Models it lists "test-model".
func New(t testing.TB) *Server {
	s := &Server{models: []string{"test-model"}, closing: make(chan struct{})}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	s.Root = s.srv.URL
	s.Base = s.srv.URL + "/v1"
	t.Cleanup(func() {
		close(s.closing)
		s.srv.Close()
	})
	return s
}

// NewTLS starts a server over https that presents cert (with its chain), closed when the test ends:
// a server with a private CA for the tests of a connection's CA file. Base and Root are https URLs.
func NewTLS(t testing.TB, cert tls.Certificate) *Server {
	s := &Server{models: []string{"test-model"}, closing: make(chan struct{})}
	s.srv = httptest.NewUnstartedServer(http.HandlerFunc(s.serve))
	s.srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	// A client that refuses the certificate is what these tests are about; the server's log line
	// for each refused handshake is noise.
	s.srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.srv.StartTLS()
	s.Root = s.srv.URL
	s.Base = s.srv.URL + "/v1"
	t.Cleanup(func() {
		close(s.closing)
		s.srv.Close()
	})
	return s
}

// RequireKey makes every request without "Bearer <key>" answer 401.
func (s *Server) RequireKey(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.key = key
}

// Models sets the models list and, when status is not 0, the models path's status instead.
func (s *Server) Models(status int, models ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models, s.modelsSt = models, status
}

// Script queues replies for the next chat requests, in order.
func (s *Server) Script(replies ...Reply) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replies = append(s.replies, replies...)
}

// Requests returns the requests received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// ChatRequests returns the chat requests received so far.
func (s *Server) ChatRequests() []Request {
	var out []Request
	for _, r := range s.Requests() {
		if strings.HasSuffix(r.Path, "/chat/completions") {
			out = append(out, r)
		}
	}
	return out
}

// Cancelled counts gated streams whose client went away before the gate opened.
func (s *Server) Cancelled() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

// Pending counts the scripted replies not used yet.
func (s *Server) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.replies)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.requests = append(s.requests, Request{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Body: body})
	key := s.key
	s.mu.Unlock()
	if key != "" && r.Header.Get("Authorization") != "Bearer "+key {
		http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
		return
	}
	// The paths of LM Studio and OpenAI (/v1), LiteLLM (with or without /v1) and Open WebUI (/api).
	path := r.URL.Path
	for _, prefix := range []string{"/v1", "/api"} {
		path = strings.TrimPrefix(path, prefix)
	}
	switch {
	case r.Method == http.MethodGet && path == "/models":
		s.serveModels(w)
	case r.Method == http.MethodPost && path == "/chat/completions":
		s.serveChat(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveModels(w http.ResponseWriter) {
	s.mu.Lock()
	models, st := s.models, s.modelsSt
	s.mu.Unlock()
	if st != 0 && st != http.StatusOK {
		http.Error(w, `{"error":{"message":"models refused"}}`, st)
		return
	}
	type entry struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	}
	data := make([]entry, 0, len(models))
	for _, m := range models {
		data = append(data, entry{ID: m, Object: "model"})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
}

func (s *Server) serveChat(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if len(s.replies) == 0 {
		s.mu.Unlock()
		http.Error(w, `{"error":{"message":"fakellm: no scripted reply"}}`, http.StatusInternalServerError)
		return
	}
	rep := s.replies[0]
	s.replies = s.replies[1:]
	s.mu.Unlock()

	if rep.Status != 0 && rep.Status != http.StatusOK {
		b := rep.Body
		if b == "" {
			b = fmt.Sprintf(`{"error":{"message":"fakellm status %d"}}`, rep.Status)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rep.Status)
		_, _ = io.WriteString(w, b)
		return
	}
	if rep.JSON {
		s.serveJSON(w, rep)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	first := true
	send := func(v any) bool {
		b, _ := json.Marshal(v)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		if fl != nil {
			fl.Flush()
		}
		if first {
			first = false
			if rep.Started != nil {
				close(rep.Started)
			}
			if rep.Gate != nil {
				select {
				case <-rep.Gate:
				case <-s.closing:
					return false
				case <-r.Context().Done():
					s.mu.Lock()
					s.cancelled++
					s.mu.Unlock()
					return false
				}
			}
		}
		return true
	}
	delta := func(d map[string]any, finish any) map[string]any {
		return map[string]any{"object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": d, "finish_reason": finish}}}
	}
	if !send(delta(map[string]any{"role": "assistant", "content": ""}, nil)) {
		return
	}
	for _, t := range rep.Reasoning {
		if !send(delta(map[string]any{"reasoning_content": t}, nil)) {
			return
		}
	}
	for i, t := range rep.Text {
		if i > 0 && rep.Pace > 0 {
			time.Sleep(rep.Pace)
		}
		if !send(delta(map[string]any{"content": t}, nil)) {
			return
		}
	}
	if rep.StreamError != "" {
		send(map[string]any{"error": map[string]any{"message": rep.StreamError}})
		return
	}
	for i, tc := range rep.ToolCalls {
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		half := len(tc.Args) / 2
		parts := []map[string]any{
			{"index": i, "id": id, "type": "function", "function": map[string]any{"name": tc.Name, "arguments": tc.Args[:half]}},
			{"index": i, "function": map[string]any{"arguments": tc.Args[half:]}},
		}
		for _, p := range parts {
			if !send(delta(map[string]any{"tool_calls": []any{p}}, nil)) {
				return
			}
		}
	}
	finish := "stop"
	if len(rep.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	if !send(delta(map[string]any{}, finish)) {
		return
	}
	if rep.Usage != nil {
		if !send(map[string]any{"object": "chat.completion.chunk", "choices": []any{},
			"usage": map[string]int{"prompt_tokens": rep.Usage.Prompt, "completion_tokens": rep.Usage.Completion,
				"total_tokens": rep.Usage.Prompt + rep.Usage.Completion}}) {
			return
		}
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

func (s *Server) serveJSON(w http.ResponseWriter, rep Reply) {
	msg := map[string]any{"role": "assistant", "content": strings.Join(rep.Text, "")}
	var calls []any
	for i, tc := range rep.ToolCalls {
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		calls = append(calls, map[string]any{"id": id, "type": "function", "function": map[string]any{"name": tc.Name, "arguments": tc.Args}})
	}
	if calls != nil {
		msg["tool_calls"] = calls
	}
	out := map[string]any{"object": "chat.completion", "choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": "stop"}}}
	if rep.Usage != nil {
		out["usage"] = map[string]int{"prompt_tokens": rep.Usage.Prompt, "completion_tokens": rep.Usage.Completion,
			"total_tokens": rep.Usage.Prompt + rep.Usage.Completion}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
