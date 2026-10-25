package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/fakellm"
)

// writeFile writes a test file and returns its path.
func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestCAFileTrustsAPrivateCA (the owner's decision of 2026-10-15): an https endpoint whose
// certificate chains to a private CA fails the connection test's TLS layer with a hint at the CA
// file; with the CA's file the test passes and a turn runs; the file is kept with the connection by
// its path and read again when connecting, so a file that changed counts at once.
func TestCAFileTrustsAPrivateCA(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	ca := testkit.NewTestCA(t, "Cogpit test private CA")
	srv := fakellm.NewTLS(t, ca.Issue(t, "127.0.0.1"))
	conn := engine.ChatConnection{Name: "private CA", Kind: engine.ChatLMStudio, BaseURL: srv.Base, Model: "test-model"}

	steps := h.a.TestConnection(context.Background(), conn, nil, nil)
	if got := states(steps); got != "dns=pass tcp=pass http=fail auth=notReached models=notReached tools=notReached" ||
		!strings.Contains(steps[2].Detail, "not trusted") || !strings.Contains(steps[2].Detail, "CA file") {
		t.Fatalf("without the CA file: %s %+v", got, steps)
	}
	t.Logf("without the CA file: %s", steps[2].Detail)

	dir := t.TempDir()
	conn.CAFile = writeFile(t, dir, "ca.pem", ca.PEM())
	srv.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "get_time", Args: `{}`}}})
	steps = h.a.TestConnection(context.Background(), conn, nil, nil)
	if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass models=pass tools=pass" || !strings.Contains(steps[2].Detail, "HTTPS, TLS 1.") ||
		!strings.Contains(steps[2].Detail, "certificate verified (chain to the CA file, host name 127.0.0.1), issued by Cogpit test private CA, valid until ") ||
		steps[2].Warning != "" {
		t.Fatalf("with the CA file: %s %+v", got, steps)
	}
	t.Logf("with the CA file: %s", steps[2].Detail)

	saved, err := h.a.SaveConnection(conn, nil)
	if err != nil || saved.CAFile != conn.CAFile {
		t.Fatalf("saved %+v, %v", saved, err)
	}
	again := New(Options{Session: h.s, Dir: h.dir, Keys: h.keys})
	if !slices.ContainsFunc(again.conns, func(c engine.ChatConnection) bool { return c.CAFile == conn.CAFile }) {
		t.Error("the CA file's path was not kept with the connection")
	}
	c, err := h.a.NewConversation("test", saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	srv.Script(fakellm.Reply{Text: []string{"Hello over TLS."}})
	if err := h.a.Send(c.ID, engine.ChatUserMessage{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if got := h.idle(t, c.ID); got.Items[len(got.Items)-1].Text != "Hello over TLS." {
		t.Fatalf("the turn: %v (%s)", itemKinds(got), notes(got))
	}

	// Another CA in the same file: read when connecting, so the server is no longer trusted.
	other := testkit.NewTestCA(t, "another CA")
	writeFile(t, dir, "ca.pem", other.PEM())
	steps = h.a.TestConnection(context.Background(), saved, nil, nil)
	if got := states(steps); got != "dns=pass tcp=pass http=fail auth=notReached models=notReached tools=notReached" {
		t.Errorf("after the file changed: %s %+v", got, steps)
	}
}

// TestCAFileErrorsAreNamed: a CA file that does not exist, is not a file or holds no certificate
// fails the TLS layer and a turn with the file named and why, never its content; a CA file is
// refused with an http base URL and as a relative path.
func TestCAFileErrorsAreNamed(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	ca := testkit.NewTestCA(t, "Cogpit test private CA")
	srv := fakellm.NewTLS(t, ca.Issue(t, "127.0.0.1"))
	dir := t.TempDir()
	const content = "not a certificate SECRET-CONTENT-4711"
	for _, tc := range []struct{ path, want string }{
		{filepath.Join(dir, "missing.pem"), "does not exist"},
		{dir, "is not a file"},
		{writeFile(t, dir, "notes.txt", []byte(content)), "holds no PEM certificate"},
	} {
		conn := engine.ChatConnection{Name: "x", Kind: engine.ChatLMStudio, BaseURL: srv.Base, Model: "test-model", CAFile: tc.path}
		steps := h.a.TestConnection(context.Background(), conn, nil, nil)
		if got := states(steps); got != "dns=pass tcp=pass http=fail auth=notReached models=notReached tools=notReached" || !strings.Contains(steps[2].Detail, tc.path) ||
			!strings.Contains(steps[2].Detail, tc.want) || strings.Contains(steps[2].Detail, "SECRET-CONTENT") {
			t.Errorf("%s: %s %+v", tc.path, got, steps)
		}
	}

	conn := engine.ChatConnection{Name: "x", Kind: engine.ChatLMStudio, BaseURL: srv.Base, Model: "test-model", CAFile: filepath.Join(dir, "missing.pem")}
	saved, err := h.a.SaveConnection(conn, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := h.a.NewConversation("test", saved.ID)
	if err := h.a.Send(c.ID, engine.ChatUserMessage{Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if got := notes(h.idle(t, c.ID)); !strings.Contains(got, "missing.pem does not exist") {
		t.Errorf("the turn's note: %q", got)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("%d requests went out without the CA file", n)
	}

	plain := conn
	plain.BaseURL = h.llm.Base
	if _, err := h.a.SaveConnection(plain, nil); !errors.Is(err, ErrCAFileScheme) {
		t.Errorf("a CA file over http: %v", err)
	}
	if steps := h.a.TestConnection(context.Background(), plain, nil, nil); steps[0].State != engine.ChatTestFail {
		t.Errorf("the test of a CA file over http: %+v", steps)
	}
	relative := conn
	relative.CAFile = "certs/ca.pem"
	if _, err := h.a.SaveConnection(relative, nil); !errors.Is(err, ErrCAFilePath) {
		t.Errorf("a relative CA file: %v", err)
	}
}

// TestCAFileReplacesTheSystemRoots (N141, the owner's decision in the audit of 2026-10-16): the
// pool a CA file makes holds the file's certificates alone, never the system's roots with them, as
// kubectl's certificate-authority: Go's verifier checks the chain against the file, and macOS's
// platform verifier, which Verify asks only for a pool marked as the system's, is not asked (Go
// 1.27's crypto/x509; the analysis at loadCAPool).
func TestCAFileReplacesTheSystemRoots(t *testing.T) {
	ca := testkit.NewTestCA(t, "Cogpit test private CA")
	path := writeFile(t, t.TempDir(), "ca.pem", ca.PEM())
	got, _, err := loadCAPool(path)
	if err != nil {
		t.Fatal(err)
	}
	alone := x509.NewCertPool()
	alone.AppendCertsFromPEM(ca.PEM())
	if !got.Equal(alone) {
		t.Error("the pool is not the CA file's certificates alone")
	}
	if sys, err := x509.SystemCertPool(); err == nil {
		sys.AppendCertsFromPEM(ca.PEM())
		if got.Equal(sys) {
			t.Error("the CA file was added to the system's roots")
		}
	}
}

// TestCAFileRefusesAPublicServer (N141): with a CA file set, a server whose certificate the
// client trusts otherwise (a CA the agent's own client trusts, standing in for the system's
// roots: a public certificate) fails the TLS layer, saying that the connection trusts only its CA
// file and that the field is to be cleared for a public certificate; cleared, the same server
// passes.
func TestCAFileRefusesAPublicServer(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	public := testkit.NewTestCA(t, "a public CA")
	srv := fakellm.NewTLS(t, public.Issue(t, "127.0.0.1"))
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(public.PEM())
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	a := New(Options{Session: h.s, Keys: NewMemoryKeyStore(), HTTPClient: &http.Client{Transport: tr, Timeout: 10 * time.Second}})
	conn := engine.ChatConnection{Name: "public", Kind: engine.ChatLMStudio, BaseURL: srv.Base, Model: "test-model"}

	srv.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "get_time", Args: `{}`}}})
	if steps := a.TestConnection(context.Background(), conn, nil, nil); states(steps) != "dns=pass tcp=pass http=pass auth=pass models=pass tools=pass" {
		t.Fatalf("without a CA file: %s %+v", states(steps), steps)
	}

	private := testkit.NewTestCA(t, "Cogpit test private CA")
	conn.CAFile = writeFile(t, t.TempDir(), "ca.pem", private.PEM())
	steps := a.TestConnection(context.Background(), conn, nil, nil)
	if got := states(steps); got != "dns=pass tcp=pass http=fail auth=notReached models=notReached tools=notReached" ||
		!strings.Contains(steps[2].Detail, "this connection trusts only its CA file "+conn.CAFile) ||
		!strings.Contains(steps[2].Detail, "clear the CA file") {
		t.Fatalf("with a CA file: %s %+v", got, steps)
	}
	t.Logf("with a CA file: %s", steps[2].Detail)
}

// TestPlainHTTPToAnotherMachineIsWarned (the owner's decision of 2026-10-15, H-95): the connection
// test of an http endpoint on another machine passes its HTTP layer with the warning that the key,
// the conversation and every secret the agent reads travel unencrypted; on this computer
// (localhost, 127.0.0.1, ::1) and over https it does not warn.
func TestPlainHTTPToAnotherMachineIsWarned(t *testing.T) {
	h := newHarness(t, engine.ChatLimits{})
	fake, err := url.Parse(h.llm.Root)
	if err != nil {
		t.Fatal(err)
	}
	// Every dial, the TCP layer's and the client's, reaches the fake endpoint on this computer.
	toFake := func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, fake.Host)
	}
	old := dialTCP
	dialTCP = toFake
	t.Cleanup(func() { dialTCP = old })
	fakeResolver(t, map[string][]string{"llm.lan.example": {"192.168.1.30"}})
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = toFake
	tr.Proxy = nil // a proxy in the developer's environment is not this test's
	a := New(Options{Session: h.s, Keys: NewMemoryKeyStore(), HTTPClient: &http.Client{Transport: tr, Timeout: 10 * time.Second}})

	for base, warn := range map[string]bool{
		"http://llm.lan.example:1234/v1": true, "http://192.168.1.20:1234/v1": true, "http://[fd00::3]:1234/v1": true,
		"http://localhost:1234/v1": false, "http://127.0.0.1:1234/v1": false, "http://[::1]:1234/v1": false,
	} {
		h.llm.Script(fakellm.Reply{ToolCalls: []fakellm.ToolCall{{Name: "get_time", Args: `{}`}}})
		conn := engine.ChatConnection{Name: "lan", Kind: engine.ChatLMStudio, BaseURL: base, Model: "test-model"}
		steps := a.TestConnection(context.Background(), conn, nil, nil)
		if got := states(steps); got != "dns=pass tcp=pass http=pass auth=pass models=pass tools=pass" {
			t.Fatalf("%s: %s %+v", base, got, steps)
		}
		if warned := steps[2].Warning == engine.ChatPlainHTTPWarning; warned != warn ||
			strings.Contains(steps[2].Detail, "another machine") != warn {
			t.Errorf("%s: warning %q, detail %q; want warned %v", base, steps[2].Warning, steps[2].Detail, warn)
		}
		for i, s := range steps {
			if i != 2 && s.Warning != "" {
				t.Errorf("%s: layer %s warns %q", base, s.Layer, s.Warning)
			}
		}
	}
}
