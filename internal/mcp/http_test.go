package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// start starts the fixture's server and returns its URL and the token from the token file.
func (f *fixture) start(t testing.TB) (endpoint, token string) {
	t.Helper()
	if err := f.server.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.server.TokenPath())
	if err != nil {
		t.Fatal(err)
	}
	return f.server.URL(), strings.TrimSpace(string(data))
}

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18",` +
	`"capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}`

// post sends an initialize request, changed by mutate, and returns the response status and the
// WWW-Authenticate header.
func post(t testing.TB, endpoint string, mutate func(*http.Request)) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(initializeBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	mutate(req)
	tr := &http.Transport{}
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, resp.Header.Get("WWW-Authenticate")
}

func TestHTTPGuard(t *testing.T) {
	f := newFixture(t, nil, Options{})
	endpoint, token := f.start(t)
	u, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	port := u.Port()
	auth := func(v string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", v) }
	}
	both := func(fns ...func(*http.Request)) func(*http.Request) {
		return func(r *http.Request) {
			for _, fn := range fns {
				fn(r)
			}
		}
	}
	host := func(h string) func(*http.Request) { return func(r *http.Request) { r.Host = h } }
	origin := func(o string) func(*http.Request) { return func(r *http.Request) { r.Header.Set("Origin", o) } }
	path := func(p string) func(*http.Request) { return func(r *http.Request) { r.URL.Path = p } }
	good := auth("Bearer " + token)

	cases := []struct {
		name   string
		mutate func(*http.Request)
		status int
	}{
		{"no token", func(*http.Request) {}, http.StatusUnauthorized},
		{"wrong token", auth("Bearer " + strings.Repeat("0", 64)), http.StatusUnauthorized},
		{"token prefix", auth("Bearer " + token[:32]), http.StatusUnauthorized},
		{"basic scheme", auth("Basic " + token), http.StatusUnauthorized},
		{"bare token", auth(token), http.StatusUnauthorized},
		{"foreign host", both(good, host("evil.example:"+port)), http.StatusForbidden},
		{"foreign host, no token", host("evil.example:" + port), http.StatusForbidden},
		{"loopback host, other port", both(good, host("127.0.0.1:1")), http.StatusForbidden},
		{"foreign origin", both(good, origin("http://evil.example")), http.StatusForbidden},
		{"null origin", both(good, origin("null")), http.StatusForbidden},
		{"local origin, other port", both(good, origin("http://127.0.0.1:1")), http.StatusForbidden},
		{"https origin", both(good, origin("https://127.0.0.1:"+port)), http.StatusForbidden},
		{"token", good, http.StatusOK},
		{"lowercase scheme", auth("bearer " + token), http.StatusOK},
		{"localhost host and origin", both(good, host("localhost:"+port), origin("http://localhost:"+port)), http.StatusOK},
		{"127.0.0.1 origin", both(good, origin("http://127.0.0.1:"+port)), http.StatusOK},
		{"other path, token", both(good, path("/other")), http.StatusNotFound},
		{"other path, no token", path("/other"), http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, challenge := post(t, endpoint, c.mutate)
			if status != c.status {
				t.Fatalf("status %d, want %d", status, c.status)
			}
			if status == http.StatusUnauthorized && !strings.HasPrefix(challenge, "Bearer") {
				t.Errorf("401 without a Bearer challenge: %q", challenge)
			}
		})
	}
}

// TestGuardHidesTokenFromHandlers checks that nothing behind the guard sees the Authorization
// header and that the request's context is passed on for callContext.
func TestGuardHidesTokenFromHandlers(t *testing.T) {
	var sawAuth string
	var sawCtx bool
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		_, sawCtx = r.Context().Value(requestContextKey{}).(context.Context)
	})
	g := newGuard(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4711}, "secret", discardLogger(), next)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:4711/mcp", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	g.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || sawAuth != "" || !sawCtx {
		t.Errorf("status %d, Authorization seen %q, request context passed %v", rec.Code, sawAuth, sawCtx)
	}
}

// TestHTTPEndToEnd drives the showcase through the SDK's streamable client, with the newest
// protocol and with the handshake-era versions an MCP client such as Claude Code may speak.
func TestHTTPEndToEnd(t *testing.T) {
	f := newFixture(t, nil, Options{})
	endpoint, token := f.start(t)
	for _, version := range []string{"", "2025-11-25", "2025-06-18", "2025-03-26"} {
		t.Run("protocol "+version, func(t *testing.T) {
			cs := connectHTTP(t, endpoint, token, version)
			t.Logf("negotiated protocol %s", cs.InitializeResult().ProtocolVersion)
			n := 0
			for _, err := range cs.Tools(context.Background(), nil) {
				if err != nil {
					t.Fatal(err)
				}
				n++
			}
			if n != len(f.session.Registry().Commands()) {
				t.Errorf("%d tools", n)
			}
			res := call(t, cs, "resources_find", map[string]any{"kind": "services", "field": "clusterIP", "value": showcaseIP})
			var found findResult
			decode(t, res.StructuredContent, &found)
			if res.IsError || len(found.Refs) != 1 {
				t.Fatalf("resources_find: %s", text(res))
			}
			if res := call(t, cs, "ui_navigate", map[string]any{"ref": found.Refs[0]}); res.IsError {
				t.Fatalf("ui_navigate: %s", text(res))
			}
			if ui := f.session.UIState(); ui.View != "services" || ui.Drawer == nil || *ui.Drawer != webRef {
				t.Fatalf("UI state %+v", ui)
			}
			if res := call(t, cs, "resources_find", map[string]any{"kind": 1}); !res.IsError || errorCode(res) != codeInvalidParams {
				t.Errorf("invalid params over HTTP: %s", text(res))
			}
		})
	}
	for _, e := range f.auditEntries(t) {
		if e.Origin != engine.OriginExternalAgent {
			t.Errorf("audit entry with origin %s", e.Origin)
		}
	}
}

// TestHTTPClientGoneEndsPendingConfirmation checks that a call waiting for the user's
// confirmation ends when the client abandons the HTTP request, with both protocol generations.
func TestHTTPClientGoneEndsPendingConfirmation(t *testing.T) {
	for _, version := range []string{"", "2025-06-18"} {
		t.Run("protocol "+version, func(t *testing.T) {
			confirm := newBlockingConfirmer()
			f := newFixture(t, confirm, Options{})
			endpoint, token := f.start(t)
			cs := connectHTTP(t, endpoint, token, version)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			errc := make(chan error, 1)
			go func() {
				_, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "pods_delete", Arguments: map[string]any{"ref": podRef}})
				errc <- err
			}()
			confirm.waitEntered(t)
			cancel()
			confirm.waitEnded(t)
			if err := <-errc; err == nil {
				t.Error("abandoned call returned no error")
			}
			if f.deleted.Load() != 0 {
				t.Error("the command ran after the client went away")
			}
		})
	}
}

func TestHTTPCloseEndsPendingConfirmation(t *testing.T) {
	confirm := newBlockingConfirmer()
	f := newFixture(t, confirm, Options{})
	endpoint, token := f.start(t)
	cs := connectHTTP(t, endpoint, token, "")
	go func() {
		_, _ = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "pods_delete", Arguments: map[string]any{"ref": podRef}})
	}()
	confirm.waitEntered(t)
	if err := f.server.Close(); err != nil {
		t.Fatal(err)
	}
	confirm.waitEnded(t)
	if f.deleted.Load() != 0 {
		t.Error("the command ran after the server closed")
	}
}

func TestNewRefusesNonLoopbackAddresses(t *testing.T) {
	session := engine.New(engine.Options{Logger: discardLogger()})
	defer session.Close()
	for _, addr := range []string{"0.0.0.0:0", ":0", "[::]:0", "192.168.1.10:0", "example.com:0", "127.0.0.1", "127.0.0.1:99999", "[fe80::1%lo0]:0"} {
		if _, err := New(session, Options{Addr: addr}); err == nil {
			t.Errorf("New accepted %q", addr)
		}
	}
	for _, addr := range []string{"", "127.0.0.1:0", "localhost:0", "LOCALHOST:8080", "[::1]:0", "127.0.0.2:0"} {
		if _, err := New(session, Options{Addr: addr}); err != nil {
			t.Errorf("New refused %q: %v", addr, err)
		}
	}
	if _, err := New(nil, Options{}); err == nil {
		t.Error("New accepted a nil session")
	}
}

func TestClientRegistration(t *testing.T) {
	f := newFixture(t, nil, Options{})
	if f.server.URL() != "" || f.server.ClaudeAddCommand() != "" || f.server.ClientConfigJSON() != "" {
		t.Error("endpoint details before Start")
	}
	endpoint, token := f.start(t)
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") || !strings.HasSuffix(endpoint, "/mcp") {
		t.Errorf("URL %q", endpoint)
	}
	quiet := ">/dev/null 2>&1"
	if runtime.GOOS == goosWindows {
		quiet = "*> $null" // PowerShell's
	}
	want := fmt.Sprintf(`claude mcp remove --scope user cogpit %s; `+
		`claude mcp add --scope user --transport http cogpit %s --header "Authorization: Bearer %s"`, quiet, endpoint, token)
	if got := f.server.ClaudeAddCommand(); got != want {
		t.Errorf("ClaudeAddCommand:\n got %s\nwant %s", got, want)
	}

	var cfg struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(f.server.ClientConfigJSON()), &cfg); err != nil {
		t.Fatal(err)
	}
	c, ok := cfg.MCPServers["cogpit"]
	if !ok || len(cfg.MCPServers) != 1 || c.Type != "http" || c.URL != endpoint || c.Headers["Authorization"] != "Bearer "+token {
		t.Errorf("client config %+v", cfg)
	}

	if err := f.server.Close(); err != nil {
		t.Fatal(err)
	}
	if f.server.URL() != "" || f.server.ClaudeAddCommand() != "" || f.server.ClientConfigJSON() != "" {
		t.Error("endpoint details after Close")
	}
}

// TestTokenNeverLogged runs accepted, refused and failing requests and checks that the token
// reaches neither the logs nor the audit log.
func TestTokenNeverLogged(t *testing.T) {
	f := newFixture(t, engine.ConfirmFunc(func(context.Context, engine.ConfirmRequest) (bool, error) { return false, nil }), Options{})
	endpoint, token := f.start(t)
	u, _ := url.Parse(endpoint)

	post(t, endpoint, func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") })
	post(t, endpoint, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
		r.Host = "evil.example:" + u.Port()
	})
	post(t, endpoint, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Origin", "http://evil.example")
	})
	cs := connectHTTP(t, endpoint, token, "")
	call(t, cs, "ui_state", nil)
	call(t, cs, "resources_find", map[string]any{"kind": 1})
	call(t, cs, "pods_delete", map[string]any{"ref": podRef})
	_ = cs.Close()
	if err := f.server.Close(); err != nil {
		t.Fatal(err)
	}

	logs := f.logs.String()
	for _, want := range []string{"MCP server listening", "MCP request rejected", "MCP server stopped"} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %q; the capture is not working:\n%s", want, logs)
		}
	}
	if strings.Contains(logs, token) {
		t.Error("the token appears in the logs")
	}
	if strings.Contains(f.audit.String(), token) {
		t.Error("the token appears in the audit log")
	}
}
