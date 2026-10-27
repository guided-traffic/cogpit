package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/mcp"
)

// TestMCPControlSwitch: the window's switch stops the server (endpoint file gone, port closed, the
// token kept), starts it again with the same token and port (F6), hands the window the endpoint, the
// token and the command, prints it to the terminal and records the user's switches, but not the
// start at launch, in the audit log.
func TestMCPControlSwitch(t *testing.T) {
	var audit, terminal bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := engine.New(engine.Options{Audit: &audit, Logger: logger})
	t.Cleanup(s.Close)
	tokenFile := filepath.Join(t.TempDir(), "Cogpit", "mcp-token")
	srv, err := mcp.New(s, mcp.Options{TokenFile: tokenFile, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	c := &mcpControl{srv: srv, session: s, logger: logger, out: &terminal}

	ep, err := c.Start(false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ep.URL, "http://127.0.0.1:") || !strings.Contains(ep.Command, ep.URL) || ep.Command != ep.HTTPCommand ||
		ep.Token == "" || !strings.Contains(ep.HTTPCommand, ep.Token) || ep.PortTaken != 0 {
		t.Errorf("endpoint %s, command is the HTTP one %v, token in it %v, taken %d", ep.URL, ep.Command == ep.HTTPCommand,
			ep.Token != "" && strings.Contains(ep.HTTPCommand, ep.Token), ep.PortTaken)
	}
	if audit.Len() != 0 {
		t.Errorf("the start at launch was audited: %s", audit.String())
	}
	if !strings.Contains(terminal.String(), ep.Command) {
		t.Error("the command was not printed to the terminal")
	}
	addr := strings.TrimSuffix(strings.TrimPrefix(ep.URL, "http://"), "/mcp")

	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(tokenFile), mcp.EndpointFileName)); !os.IsNotExist(err) {
		t.Errorf("endpoint file after Stop: %v", err)
	}
	if _, err := os.Lstat(tokenFile); err != nil {
		t.Errorf("token file after Stop: %v, want it kept", err)
	}
	// Stopped, nothing of Cogpit's answers there; another process may have bound the port since.
	if resp, err := (&http.Client{Timeout: time.Second}).Post("http://"+addr+"/mcp", "application/json", strings.NewReader("{}")); err == nil {
		_ = resp.Body.Close()
		if resp.Header.Get("WWW-Authenticate") == `Bearer realm="cogpit"` {
			t.Error("the endpoint still answers after Stop")
		}
	}

	ep2, err := c.Start(true)
	if err != nil {
		t.Fatal(err)
	}
	_, oldPort, _ := net.SplitHostPort(addr)
	switch {
	case ep2.PortTaken == 0:
		if ep2.Command != ep.Command || ep2.Token != ep.Token {
			t.Error("the switch changed the registration: the token and the port are kept (F6)")
		}
	case strconv.Itoa(ep2.PortTaken) == oldPort: // another process took the freed port meanwhile
		if ep2.Token != ep.Token || ep2.URL == ep.URL {
			t.Errorf("port %d taken: URL %s (was %s), token kept %v; want another port and the token", ep2.PortTaken, ep2.URL, ep.URL, ep2.Token == ep.Token)
		}
	default:
		t.Errorf("the restart found port %d taken, want %s or none", ep2.PortTaken, oldPort)
	}

	var lines []engine.AuditEntry
	for _, l := range strings.Split(strings.TrimSpace(audit.String()), "\n") {
		var e engine.AuditEntry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("audit line %q: %v", l, err)
		}
		lines = append(lines, e)
	}
	if len(lines) != 2 {
		t.Fatalf("audit lines: %s", audit.String())
	}
	for i, enabled := range []string{`{"enabled":false}`, `{"enabled":true}`} {
		e := lines[i]
		if e.Origin != engine.OriginGUI || e.Command != auditMCPSwitch || string(e.Params) != enabled || !e.OK {
			t.Errorf("audit line %d: %+v (params %s)", i, e, e.Params)
		}
	}
	if strings.Contains(audit.String(), "Bearer") {
		t.Error("a token reached the audit log")
	}
}

// TestMCPControlRegenerate (F6): the window's Regenerate token hands it the new token and command on
// the same URL, the old token is refused, the click is one audit log line without the token; with
// the server off the window gets no endpoint, and the next start serves the newer token.
func TestMCPControlRegenerate(t *testing.T) {
	var audit, terminal bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := engine.New(engine.Options{Audit: &audit, Logger: logger})
	t.Cleanup(s.Close)
	srv, err := mcp.New(s, mcp.Options{TokenFile: filepath.Join(t.TempDir(), "Cogpit", "mcp-token"), Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	c := &mcpControl{srv: srv, session: s, logger: logger, out: &terminal}
	ep, err := c.Start(false)
	if err != nil {
		t.Fatal(err)
	}

	ep2, err := c.Regenerate()
	if err != nil {
		t.Fatal(err)
	}
	if ep2.URL != ep.URL || ep2.Token == "" || ep2.Token == ep.Token || !strings.Contains(ep2.HTTPCommand, ep2.Token) {
		t.Errorf("after the regenerate: %s (want %s), token changed %v", ep2.URL, ep.URL, ep2.Token != ep.Token)
	}
	if !strings.Contains(terminal.String(), ep2.Command) {
		t.Error("the new command was not printed to the terminal")
	}
	req, _ := http.NewRequest(http.MethodPost, ep.URL, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+ep.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("the old token: status %d, want 401", resp.StatusCode)
	}

	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	off, err := c.Regenerate()
	if err != nil || off.URL != "" || off.Token != "" {
		t.Errorf("a regenerate while off: %v, endpoint %q, token given %v", err, off.URL, off.Token != "")
	}
	ep3, err := c.Start(true)
	if err != nil {
		t.Fatal(err)
	}
	if ep3.Token == ep2.Token || ep3.URL != ep.URL {
		t.Errorf("the start after a regenerate while off: %s, token changed %v", ep3.URL, ep3.Token != ep2.Token)
	}

	var regens int
	for _, l := range strings.Split(strings.TrimSpace(audit.String()), "\n") {
		var e engine.AuditEntry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("audit line %q: %v", l, err)
		}
		if e.Command == auditMCPToken {
			regens++
			if e.Origin != engine.OriginGUI || string(e.Params) != `{"regenerated":true}` || !e.OK {
				t.Errorf("audit line %+v (params %s)", e, e.Params)
			}
		}
	}
	if regens != 2 {
		t.Errorf("%d regenerate lines in the audit log, want 2:\n%s", regens, audit.String())
	}
	for _, tok := range []string{ep.Token, ep2.Token, ep3.Token} {
		if strings.Contains(audit.String(), tok) {
			t.Error("a token reached the audit log")
		}
	}
}

// TestMCPControlOffersTheBundledCLI: an app whose bundle carries cogpit-cli hands the window the
// stdio registration of that cogpit-cli, which names no port and no token, and prints it; without
// one the HTTP registration, as before.
func TestMCPControlOffersTheBundledCLI(t *testing.T) {
	var terminal bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := engine.New(engine.Options{Audit: io.Discard, Logger: logger})
	t.Cleanup(s.Close)
	srv, err := mcp.New(s, mcp.Options{TokenFile: filepath.Join(t.TempDir(), "Cogpit", "mcp-token"), Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	cli := "/Users/a b/Applications/Cogpit.app/Contents/MacOS/cogpit-cli"
	if runtime.GOOS == "windows" {
		cli = `C:\Users\a b\AppData\Local\Programs\Cogpit\cogpit-cli.exe` // the setup's folder per user
	}
	c := &mcpControl{srv: srv, session: s, logger: logger, out: &terminal, cli: cli}

	ep, err := c.Start(false)
	if err != nil {
		t.Fatal(err)
	}
	// The command's words and quoting are register_test.go's to check, on each system.
	want := mcp.ClaudeAddStdioCommand(cli)
	if want == "" || !strings.Contains(want, cli) {
		t.Fatalf("no stdio registration of %s: %q", cli, want)
	}
	if !ep.Stdio || ep.Command != want || ep.URL == "" {
		t.Errorf("endpoint %+v, want the stdio command %s", ep, want)
	}
	if strings.Contains(ep.Command, "Bearer") || strings.Contains(ep.Command, ep.URL) {
		t.Errorf("the stdio command names this run's endpoint: %s", ep.Command)
	}
	if !strings.Contains(terminal.String(), want) || !strings.Contains(terminal.String(), "once") {
		t.Errorf("printed %q", terminal.String())
	}
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}

	c.cli = ""
	ep, err = c.Start(true)
	if err != nil {
		t.Fatal(err)
	}
	if ep.Stdio || !strings.Contains(ep.Command, "--transport http") || !strings.Contains(ep.Command, ep.URL) {
		t.Errorf("without a bundled cogpit-cli: %+v", ep)
	}
}

// TestMCPControlStoredSwitchIsOptIn (D-49, the owner's decision of 2026-10-21): the window asks the
// control for the stored switch at launch, and the server is opt-in. A fresh configuration
// directory, a new installation, reads as off and unset, and so does no directory at all; a stored
// "on" is on and set; a stored "off" is off and set; a file Cogpit cannot read is off and set, so
// the server stays off until the user clicks. The window's own click is stored where it reads from.
func TestMCPControlStoredSwitchIsOptIn(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	fresh := filepath.Join(t.TempDir(), "Cogpit")
	c := &mcpControl{logger: logger, config: fresh}
	if on, set := c.StoredMCP(); on || set {
		t.Errorf("a fresh configuration directory: on %v, set %v", on, set)
	}
	if _, err := os.Lstat(mcp.SwitchPath(fresh)); !os.IsNotExist(err) {
		t.Errorf("asking for the switch created it: %v", err)
	}
	if on, set := (&mcpControl{logger: logger}).StoredMCP(); on || set {
		t.Errorf("no configuration directory: on %v, set %v", on, set)
	}

	if err := c.StoreMCP(true); err != nil {
		t.Fatal(err)
	}
	if on, set := c.StoredMCP(); !on || !set {
		t.Errorf("a stored on: on %v, set %v", on, set)
	}
	if err := c.StoreMCP(false); err != nil {
		t.Fatal(err)
	}
	if on, set := c.StoredMCP(); on || !set {
		t.Errorf("a stored off: on %v, set %v", on, set)
	}
	if err := os.WriteFile(mcp.SwitchPath(fresh), []byte("maybe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if on, set := c.StoredMCP(); on || !set {
		t.Errorf("an unreadable switch: on %v, set %v", on, set)
	}
}
