package mcp

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The token and the port kept across starts, and the user's Regenerate token (F6, the owner's
// finding).

// hostPort is the address of an endpoint URL.
func hostPort(endpoint string) string {
	return strings.TrimSuffix(strings.TrimPrefix(endpoint, "http://"), endpointPath)
}

func portOf(t testing.TB, endpoint string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(hostPort(endpoint))
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestPortKeptAcrossStarts: the first start binds a free port and keeps it in the port file,
// owner-only; the next Cogpit binds it again.
func TestPortKeptAcrossStarts(t *testing.T) {
	path := filepath.Join(privateDir(t), TokenFileName)
	first := newFixture(t, nil, Options{TokenFile: path})
	url1, _ := first.start(t)
	if got := readPort(portFile(path)); got != portOf(t, url1) {
		t.Errorf("port file holds %d, the server listens on %s", got, url1)
	}
	if runtime.GOOS != "windows" {
		if m := mode(t, portFile(path)); m != 0o600 {
			t.Errorf("port file mode %#o, want 0600", m)
		}
	}
	if err := first.server.Close(); err != nil {
		t.Fatal(err)
	}
	if readPort(portFile(path)) == 0 {
		t.Error("Close removed the port file")
	}
	next := newFixture(t, nil, Options{TokenFile: path})
	if url2, _ := next.start(t); url2 != url1 || next.server.PortTaken() != 0 {
		t.Errorf("the next start listens on %s (taken %d), want %s", url2, next.server.PortTaken(), url1)
	}
}

// TestPortTakenMovesAndKeepsTheNewOne: a kept port another program holds at start makes the server
// listen on a free one, say which port was taken (PortTaken, for the window's notice) and keep the
// new one, so that the registration the window then shows works at the next start too. The
// program on the old port gets nothing from Cogpit.
func TestPortTakenMovesAndKeepsTheNewOne(t *testing.T) {
	path := filepath.Join(privateDir(t), TokenFileName)
	first := newFixture(t, nil, Options{TokenFile: path})
	url1, _ := first.start(t)
	if err := first.server.Close(); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", hostPort(url1))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	next := newFixture(t, nil, Options{TokenFile: path})
	url2, _ := next.start(t)
	if url2 == url1 || next.server.PortTaken() != portOf(t, url1) {
		t.Fatalf("listens on %s with taken %d; want another port than %s and it named", url2, next.server.PortTaken(), url1)
	}
	if got := readPort(portFile(path)); got != portOf(t, url2) {
		t.Errorf("port file holds %d, want the new %d", got, portOf(t, url2))
	}
	if ep, err := ReadEndpoint(path); err != nil || ep.URL != url2 {
		t.Errorf("endpoint file %q, %v; want %s", ep.URL, err, url2)
	}
	if !strings.Contains(next.logs.String(), "not free") {
		t.Error("the move was not logged")
	}
}

// TestGivenPortIsNotKept: --mcp-addr with a port binds that port and keeps nothing; the kept port
// stays for the runs without the flag.
func TestGivenPortIsNotKept(t *testing.T) {
	path := filepath.Join(privateDir(t), TokenFileName)
	f := newFixture(t, nil, Options{TokenFile: path})
	url1, _ := f.start(t)
	if err := f.server.Close(); err != nil {
		t.Fatal(err)
	}
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	given := free.Addr().String()
	_ = free.Close()
	g := newFixture(t, nil, Options{TokenFile: path, Addr: given})
	if url, _ := g.start(t); hostPort(url) != given {
		t.Errorf("listens on %s, want the given %s", url, given)
	}
	if got := readPort(portFile(path)); got != portOf(t, url1) {
		t.Errorf("port file holds %d, want the kept %d", got, portOf(t, url1))
	}
}

// TestRegenerateToken: a new token in the file (0600), refused for the old one at once and
// accepted for the new one, on the same port and URL; the endpoint file stays; the registration
// command carries the new token; nothing logs either token.
func TestRegenerateToken(t *testing.T) {
	path := filepath.Join(privateDir(t), TokenFileName)
	f := newFixture(t, nil, Options{TokenFile: path})
	endpoint, old := f.start(t)
	withToken := func(tok string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
	}
	if code, _ := post(t, endpoint, withToken(old)); code != http.StatusOK {
		t.Fatalf("before: status %d", code)
	}

	if err := f.server.RegenerateToken(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fresh := strings.TrimSpace(string(data))
	if fresh == old || !validToken(fresh) || f.server.Token() != fresh {
		t.Fatal("the token file does not hold a new token the server serves")
	}
	if runtime.GOOS != "windows" {
		if m := mode(t, path); m != 0o600 {
			t.Errorf("token file mode %#o, want 0600", m)
		}
	}
	if code, _ := post(t, endpoint, withToken(old)); code != http.StatusUnauthorized {
		t.Errorf("the old token: status %d, want 401", code)
	}
	if code, _ := post(t, endpoint, withToken(fresh)); code != http.StatusOK {
		t.Errorf("the new token: status %d, want 200", code)
	}
	if f.server.URL() != endpoint {
		t.Errorf("the URL changed to %s", f.server.URL())
	}
	if ep, err := ReadEndpoint(path); err != nil || ep.URL != endpoint || ep.Token != fresh {
		t.Errorf("ReadEndpoint after the regenerate: %s, %v", ep.URL, err)
	}
	if cmd := f.server.ClaudeAddCommand(); !strings.Contains(cmd, fresh) || strings.Contains(cmd, old) {
		t.Error("the registration command does not carry the new token alone")
	}
	cs := connectHTTP(t, endpoint, fresh, "")
	defer func() { _ = cs.Close() }()
	if _, err := cs.ListTools(context.Background(), nil); err != nil {
		t.Errorf("a client with the new token: %v", err)
	}
	logs := f.logs.String()
	if !strings.Contains(logs, "MCP token regenerated") {
		t.Errorf("the regenerate was not logged:\n%s", logs)
	}
	if strings.Contains(logs, old) || strings.Contains(logs, fresh) || strings.Contains(f.audit.String(), fresh) {
		t.Error("a token reached the logs or the audit log")
	}
}

// TestRegenerateEndsPendingConfirmation: an agent's call waiting for the user's confirmation ends
// when the user regenerates the token, and its command does not run; the server keeps serving the
// new token.
func TestRegenerateEndsPendingConfirmation(t *testing.T) {
	confirm := newBlockingConfirmer()
	f := newFixture(t, confirm, Options{})
	endpoint, token := f.start(t)
	cs := connectHTTP(t, endpoint, token, "")
	t.Cleanup(func() { _ = cs.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "pods_delete", Arguments: map[string]any{"ref": podRef}})
	}()
	confirm.waitEntered(t)
	if err := f.server.RegenerateToken(); err != nil {
		t.Fatal(err)
	}
	confirm.waitEnded(t)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the agent's call did not end")
	}
	if f.deleted.Load() != 0 {
		t.Error("the command ran after the token was regenerated")
	}
	cs2 := connectHTTP(t, endpoint, f.server.Token(), "")
	t.Cleanup(func() { _ = cs2.Close() })
	if _, err := cs2.ListTools(context.Background(), nil); err != nil {
		t.Errorf("a client with the new token: %v", err)
	}
}

// TestRegenerateWhileStopped: with the server off the new token goes to the file, and the next
// start serves it on the kept port; a closed server refuses.
func TestRegenerateWhileStopped(t *testing.T) {
	path := filepath.Join(privateDir(t), TokenFileName)
	f := newFixture(t, nil, Options{TokenFile: path})
	endpoint, old := f.start(t)
	if err := f.server.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := f.server.RegenerateToken(); err != nil {
		t.Fatal(err)
	}
	if f.server.Token() != "" {
		t.Error("a stopped server hands out its token")
	}
	endpoint2, fresh := f.start(t)
	if fresh == old || endpoint2 != endpoint || f.server.Token() != fresh {
		t.Errorf("after the start: %s (want %s), token changed %v", endpoint2, endpoint, fresh != old)
	}
	if err := f.server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.server.RegenerateToken(); err == nil {
		t.Error("a closed server regenerated its token")
	}
}

// TestRegenerateBeforeAnyStart: the window may regenerate while the server stayed off since launch
// (the user's stored switch): the token file is written where Start reads it.
func TestRegenerateBeforeAnyStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Cogpit", TokenFileName)
	f := newFixture(t, nil, Options{TokenFile: path})
	if err := f.server.RegenerateToken(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, token := f.start(t); token != strings.TrimSpace(string(data)) || !validToken(token) {
		t.Error("Start did not serve the regenerated token")
	}
}
