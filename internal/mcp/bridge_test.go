package mcp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guided-traffic/cogpit/internal/engine"
)

func TestEndpointFileLifecycle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	tokenPath := filepath.Join(dir, TokenFileName)
	f := newFixture(t, nil, Options{TokenFile: tokenPath})
	url, token := f.start(t)

	ep, err := ReadEndpoint(tokenPath)
	if err != nil || ep.URL != url || ep.Token != token {
		t.Fatalf("ReadEndpoint = %+v, %v; want %s and the token", ep, err, url)
	}
	endpoint := filepath.Join(dir, EndpointFileName)
	if runtime.GOOS != "windows" {
		if m := mode(t, endpoint); m != 0o600 {
			t.Errorf("endpoint file mode %#o, want 0600", m)
		}
	}

	if err := f.server.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEndpoint(tokenPath); !errors.Is(err, ErrNoEndpoint) {
		t.Errorf("after Stop: %v, want ErrNoEndpoint", err)
	}
	if _, err := os.Lstat(endpoint); !os.IsNotExist(err) {
		t.Errorf("endpoint file still there after Stop: %v", err)
	}

	url2, token2 := f.start(t)
	if ep, err := ReadEndpoint(tokenPath); err != nil || ep.URL != url2 || ep.Token != token2 || token2 != token || url2 != url {
		t.Errorf("after a restart: %s, %v; want %s and the same token (F6)", ep.URL, err, url)
	}
	if err := f.server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(endpoint); !os.IsNotExist(err) {
		t.Errorf("endpoint file still there after Close: %v", err)
	}
}

// TestEndpointFileOfAnotherStartSurvives: a Cogpit that stops removes the endpoint file only if it
// still names its own server, as with the token.
func TestEndpointFileOfAnotherStartSurvives(t *testing.T) {
	tokenPath := filepath.Join(privateDir(t), TokenFileName)
	first := newFixture(t, nil, Options{TokenFile: tokenPath})
	first.start(t)
	second := newFixture(t, nil, Options{TokenFile: tokenPath})
	url2, _ := second.start(t)
	if err := first.server.Close(); err != nil {
		t.Fatal(err)
	}
	if ep, err := ReadEndpoint(tokenPath); err != nil || ep.URL != url2 {
		t.Errorf("the second server's endpoint: %+v, %v", ep, err)
	}
}

func TestReadEndpointTakesOnlyWhatCogpitWrites(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cases := []struct {
		url, token, err string
	}{
		{"http://127.0.0.1:4711/mcp", token, ""},
		{"http://[::1]:4711/mcp", token, ""},
		{"http://192.0.2.10:4711/mcp", token, "not on this machine"},
		{"http://localhost:4711/mcp", token, "not on this machine"},
		{"https://127.0.0.1:4711/mcp", token, "does not hold an endpoint"},
		{"http://127.0.0.1:4711/other", token, "does not hold an endpoint"},
		{"http://user:pw@127.0.0.1:4711/mcp", token, "does not hold an endpoint"},
		{"http://127.0.0.1/mcp", token, "does not hold an endpoint"},
		{"http://127.0.0.1:4711/mcp", "not-a-token", "does not hold a token"},
	}
	for _, c := range cases {
		dir := privateDir(t)
		tokenPath := filepath.Join(dir, TokenFileName)
		if err := os.WriteFile(tokenPath, []byte(c.token+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, EndpointFileName), []byte(c.url+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		ep, err := ReadEndpoint(tokenPath)
		switch {
		case c.err == "" && (err != nil || ep.URL != c.url || ep.Token != c.token):
			t.Errorf("%s: %+v, %v", c.url, ep, err)
		case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
			t.Errorf("%s %s: %v, want %q", c.url, c.token, err, c.err)
		}
		if err != nil && strings.Contains(err.Error(), token) {
			t.Errorf("the error quotes the token: %v", err)
		}
	}
	if _, err := ReadEndpoint(filepath.Join(privateDir(t), TokenFileName)); !errors.Is(err, ErrNoEndpoint) {
		t.Errorf("no files: %v, want ErrNoEndpoint", err)
	}
}

// bridgeClient serves b over in-memory transports and returns a client of it.
func bridgeClient(t *testing.T, b *Bridge) *sdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	serverT, clientT := sdk.NewInMemoryTransports()
	served := make(chan error, 1)
	go func() { served <- b.Serve(ctx, serverT) }()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "agent", Version: "1"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		cancel()
		_ = <-served
		_ = b.Close()
	})
	return cs
}

func toolNames(t *testing.T, cs *sdk.ClientSession) []string {
	t.Helper()
	var names []string
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

// TestBridgeForwardsToTheApp: the bridge serves the app's tools and forwards every call to the
// app, whose engine asks its user (here: declines) and audits the call as an external agent's.
func TestBridgeForwardsToTheApp(t *testing.T) {
	var asked atomic.Int32
	declined := engine.ConfirmFunc(func(_ context.Context, req engine.ConfirmRequest) (bool, error) {
		if req.Origin == engine.OriginExternalAgent {
			asked.Add(1)
		}
		return false, nil
	})
	tokenPath := filepath.Join(privateDir(t), TokenFileName)
	app := newFixture(t, declined, Options{TokenFile: tokenPath})
	app.start(t)
	direct := app.connect(t)

	b, err := DialApp(context.Background(), BridgeOptions{TokenFile: tokenPath, Version: "test", Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	cs := bridgeClient(t, b)
	if got, want := toolNames(t, cs), toolNames(t, direct); !slices.Equal(got, want) {
		t.Errorf("bridged tools %v, the app's %v", got, want)
	}
	if ir := cs.InitializeResult(); ir == nil || ir.ServerInfo.Name != serverName || !strings.Contains(ir.Instructions, "ui_state") {
		t.Errorf("initialize result %+v", ir)
	}

	nav := call(t, cs, "ui_navigate", map[string]any{"ref": webRef})
	if nav.IsError {
		t.Fatalf("ui_navigate: %s", text(nav))
	}
	if st := app.session.UIState(); st.Drawer == nil || st.Drawer.Name != "web" {
		t.Errorf("the app's UI state did not follow the bridged call: %+v", st.Drawer)
	}

	del := call(t, cs, "pods_delete", map[string]any{"ref": podRef})
	if !del.IsError || errorCode(del) != codeDeclined || asked.Load() != 1 || app.deleted.Load() != 0 {
		t.Errorf("a destructive call through the bridge: %s (code %q, asked %d, deleted %d)", text(del), errorCode(del), asked.Load(), app.deleted.Load())
	}
	var origins []engine.Origin
	for _, e := range app.auditEntries(t) {
		origins = append(origins, e.Origin)
	}
	if len(origins) == 0 || slices.ContainsFunc(origins, func(o engine.Origin) bool { return o != engine.OriginExternalAgent }) {
		t.Errorf("audit origins %v, want the external agent's", origins)
	}
	// Invalid arguments come back from the app as they would over HTTP.
	if bad := call(t, cs, "ui_navigate", map[string]any{"ref": "nope"}); !bad.IsError || errorCode(bad) != codeInvalidParams {
		t.Errorf("invalid arguments: %s", text(bad))
	}
}

// TestBridgeFollowsARestart: the app's next start keeps its port and token (F6) and the bridge's
// calls go on; a start that found the kept port taken listens on another, and the bridge follows
// it there without sending anything, the kept token least of all, to whatever holds the old port
// (H-98). With the server switched off, calls fail and say so.
func TestBridgeFollowsARestart(t *testing.T) {
	tokenPath := filepath.Join(privateDir(t), TokenFileName)
	app := newFixture(t, nil, Options{TokenFile: tokenPath})
	url1, _ := app.start(t)
	b, err := DialApp(context.Background(), BridgeOptions{TokenFile: tokenPath, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	cs := bridgeClient(t, b)
	if res := call(t, cs, "ui_state", nil); res.IsError {
		t.Fatalf("ui_state: %s", text(res))
	}

	if err := app.server.Stop(); err != nil {
		t.Fatal(err)
	}
	if url, _ := app.start(t); url != url1 {
		t.Errorf("the restart moved from %s to %s", url1, url)
	}
	if res := call(t, cs, "ui_state", nil); res.IsError {
		t.Fatalf("ui_state after the app restarted: %s", text(res))
	}

	// Another program takes the port while the server is off.
	if err := app.server.Stop(); err != nil {
		t.Fatal(err)
	}
	var squatted atomic.Int32
	squatter := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		squatted.Add(1)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}), ReadHeaderTimeout: time.Second}
	ln, err := net.Listen("tcp", strings.TrimSuffix(strings.TrimPrefix(url1, "http://"), endpointPath))
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = squatter.Serve(ln) }()
	t.Cleanup(func() { _ = squatter.Close() })
	url2, _ := app.start(t)
	if url2 == url1 || app.server.PortTaken() == 0 {
		t.Fatalf("the start did not move off the taken port: %s, taken %d", url2, app.server.PortTaken())
	}
	if res := call(t, cs, "ui_state", nil); res.IsError {
		t.Fatalf("ui_state after the app moved: %s", text(res))
	}
	if squatted.Load() != 0 {
		t.Errorf("the bridge sent %d requests to the program on the old port", squatted.Load())
	}

	if err := app.server.Stop(); err != nil {
		t.Fatal(err)
	}
	res := call(t, cs, "ui_state", nil)
	if !res.IsError || errorCode(res) != codeAppUnavailable || !strings.Contains(text(res), "switched its MCP server off") {
		t.Errorf("with the server off: %s (code %q)", text(res), errorCode(res))
	}
}

// TestBridgeFollowsARegenerate (F6): after the user regenerated the token, a running bridge's next
// call reaches the app with the new token, read from the token file, without the agent doing
// anything; the old token is refused.
func TestBridgeFollowsARegenerate(t *testing.T) {
	tokenPath := filepath.Join(privateDir(t), TokenFileName)
	app := newFixture(t, nil, Options{TokenFile: tokenPath})
	url, oldToken := app.start(t)
	b, err := DialApp(context.Background(), BridgeOptions{TokenFile: tokenPath, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	cs := bridgeClient(t, b)
	if res := call(t, cs, "ui_state", nil); res.IsError {
		t.Fatalf("ui_state: %s", text(res))
	}
	old := b.current()

	if err := app.server.RegenerateToken(); err != nil {
		t.Fatal(err)
	}
	if res := call(t, cs, "ui_state", nil); res.IsError {
		t.Fatalf("ui_state after the regenerate: %s", text(res))
	}
	if up := b.current(); up == old || up.ep.Token == oldToken || up.ep.URL != url {
		t.Errorf("the bridge did not take the new token at the same URL (%s, token changed %v)", up.ep.URL, up.ep.Token != oldToken)
	}
	if code, _ := post(t, url, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+oldToken) }); code != http.StatusUnauthorized {
		t.Errorf("the old token: status %d, want 401", code)
	}

	// The race the file check cannot see: the server refuses the token the bridge just read, and
	// the new one reaches the file a moment later. The refused call reads the files again (401)
	// and is sent once more.
	newer, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	app.server.mu.Lock()
	g := app.server.guard
	app.server.mu.Unlock()
	refusals := func() int { return strings.Count(app.logs.String(), "status=401") }
	before := refusals()
	g.rotate(newer)
	written := make(chan struct{})
	go func() { // once the server refused the token the bridge holds
		defer close(written)
		for deadline := time.Now().Add(10 * time.Second); refusals() == before && time.Now().Before(deadline); {
			time.Sleep(5 * time.Millisecond)
		}
		_ = writeTokenFile(tokenPath, newer, false)
	}()
	t.Cleanup(func() { <-written })
	if res := call(t, cs, "ui_state", nil); res.IsError {
		t.Fatalf("ui_state after a refused token: %s", text(res))
	}
	if b.current().ep.Token != newer {
		t.Error("the bridge did not read the newer token after the 401")
	}
}

// TestBridgeReconnectsOnceForConcurrentCalls: calls that find the old token refused at the same
// time (the user regenerated it while the app's server was off) reconnect once between them and
// all reach the restarted app.
func TestBridgeReconnectsOnceForConcurrentCalls(t *testing.T) {
	tokenPath := filepath.Join(privateDir(t), TokenFileName)
	app := newFixture(t, nil, Options{TokenFile: tokenPath})
	app.start(t)
	b, err := DialApp(context.Background(), BridgeOptions{TokenFile: tokenPath, Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	cs := bridgeClient(t, b)
	if err := app.server.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := app.server.RegenerateToken(); err != nil {
		t.Fatal(err)
	}
	app.start(t)
	old := b.current()

	const calls = 8
	errs := make(chan string, calls)
	for range calls {
		go func() {
			res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "ui_state"})
			switch {
			case err != nil:
				errs <- err.Error()
			case res.IsError:
				errs <- text(res)
			default:
				errs <- ""
			}
		}()
	}
	for range calls {
		if msg := <-errs; msg != "" {
			t.Errorf("a call during the restart: %s", msg)
		}
	}
	if b.current() == old {
		t.Error("the bridge kept the old connection")
	}
}

func TestDialAppWithoutARunningApp(t *testing.T) {
	tokenPath := filepath.Join(privateDir(t), TokenFileName)
	if _, err := DialApp(context.Background(), BridgeOptions{TokenFile: tokenPath, Logger: discardLogger()}); !errors.Is(err, ErrAppNotRunning) {
		t.Errorf("no files: %v, want ErrAppNotRunning", err)
	}

	// The files of a Cogpit that crashed: nothing listens at the address.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	writeEndpointFiles(t, tokenPath, "http://"+addr+"/mcp")
	if _, err := DialApp(context.Background(), BridgeOptions{TokenFile: tokenPath, Logger: discardLogger()}); !errors.Is(err, ErrAppNotRunning) {
		t.Errorf("stale files: %v, want ErrAppNotRunning", err)
	}
}

func writeEndpointFiles(t *testing.T, tokenPath, url string) string {
	t.Helper()
	token := strings.Repeat("ab", tokenBytes)
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endpointFile(tokenPath), []byte(url+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return token
}

// TestBridgeSendsTheTokenToTheEndpointOnly: a server at the endpoint that redirects elsewhere
// does not get the bridge to carry the token there, and one that refuses the token is an error,
// not a reason to serve a headless engine.
func TestBridgeSendsTheTokenToTheEndpointOnly(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) }))
	defer other.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/mcp", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	tokenPath := filepath.Join(privateDir(t), TokenFileName)
	writeEndpointFiles(t, tokenPath, redirect.URL+"/mcp")
	_, err := DialApp(context.Background(), BridgeOptions{TokenFile: tokenPath, Logger: discardLogger()})
	if err == nil || errors.Is(err, ErrAppNotRunning) {
		t.Errorf("a redirecting endpoint: %v", err)
	}
	if elsewhere.Load() != 0 {
		t.Error("the bridge followed the redirect")
	}

	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer refusing.Close()
	token := writeEndpointFiles(t, tokenPath, refusing.URL+"/mcp")
	_, err = DialApp(context.Background(), BridgeOptions{TokenFile: tokenPath, Logger: discardLogger()})
	if err == nil || errors.Is(err, ErrAppNotRunning) || !strings.Contains(err.Error(), "does not accept the token") || strings.Contains(err.Error(), token) {
		t.Errorf("a refused token: %v", err)
	}
}
