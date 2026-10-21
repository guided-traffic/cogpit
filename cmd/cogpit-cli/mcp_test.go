package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guided-traffic/cogpit/internal/app"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/engine/commands"
	"github.com/guided-traffic/cogpit/internal/mcp"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// switchMCPOn writes the user's switch "on" into the test's Cogpit directory: the external MCP
// server is opt-in, so a bridge serves nothing but the note that it is off until the user did.
func (te *testEnv) switchMCPOn(t *testing.T) {
	t.Helper()
	if err := mcp.WriteSwitch(te.dirs.Config, true); err != nil {
		t.Fatal(err)
	}
}

// serveMCP runs `cogpit-cli mcp args...` on in-memory transports and returns a connected agent's
// session and a function that disconnects the agent and returns the exit code.
func (te *testEnv) serveMCP(t *testing.T, args ...string) (*sdk.ClientSession, func() int) {
	t.Helper()
	serverT, clientT := sdk.NewInMemoryTransports()
	te.mcpTransport = serverT
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	done := make(chan int, 1)
	go func() { done <- run(ctx, append([]string{"mcp"}, args...), te.env) }()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "agent", Version: "1"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect: %v; stderr: %s", err, te.errOut)
	}
	stopped := false
	stop := func() int {
		stopped = true
		_ = cs.Close()
		defer cancel()
		select {
		case code := <-done:
			return code
		case <-time.After(30 * time.Second):
			t.Fatal("mcp did not end when the agent disconnected")
			return -1
		}
	}
	t.Cleanup(func() {
		if !stopped {
			stop()
		}
	})
	return cs, stop
}

func callTool(t *testing.T, cs *sdk.ClientSession, name string, args any, out any) *sdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if out != nil && !res.IsError {
		data, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if res.StructuredContent == nil {
			if tc, ok := res.Content[0].(*sdk.TextContent); ok {
				data = []byte(tc.Text)
			}
		}
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("%s: %v in %s", name, err, data)
		}
	}
	return res
}

func resultText(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func resultCode(res *sdk.CallToolResult) string {
	m, _ := res.StructuredContent.(map[string]any)
	code, _ := m["error"].(string)
	return code
}

// TestMCPHeadlessServesTheRegistry: without a running Cogpit the bridge serves an engine of its
// own over the transport: every command a tool, reads and connects run, a command that needs the
// user's confirmation is refused before it touches the cluster, and every call is audited. stdout
// stays the protocol's.
func TestMCPHeadlessServesTheRegistry(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "fake-TOKEN-5678"})
	defer srv.Close()
	srv.Set(testkit.PodsGVR, testkit.Pod("team", "web-0"))
	te := newTestEnv(t)
	te.switchMCPOn(t)
	te.writeKube(t, "config", string(srv.Kubeconfig("fake", "team")))

	cs, stop := te.serveMCP(t)
	reg := engine.NewRegistry()
	if err := commands.RegisterAll(reg); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, c := range reg.Commands() {
		names[c.ToolName()] = true
	}
	n := 0
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		if !names[tool.Name] {
			t.Errorf("tool %s is no command", tool.Name)
		}
		n++
	}
	if n != len(reg.Commands()) {
		t.Errorf("%d tools, %d commands", n, len(reg.Commands()))
	}

	var conn commands.ConnectResult
	if res := callTool(t, cs, "cluster_connect", map[string]string{"context": "fake"}, &conn); res.IsError || conn.State != engine.Connected {
		t.Fatalf("cluster_connect: %s %+v", resultText(res), conn)
	}
	var list commands.ListResult
	if res := callTool(t, cs, "resources_list", map[string]string{"kind": "pods"}, &list); res.IsError || list.Total != 1 {
		t.Fatalf("resources_list: %s %+v", resultText(res), list)
	}

	before := srv.Requests()["delete pods"]
	del := callTool(t, cs, "object_delete", map[string]any{"ref": model.ObjectRef{Kind: "pods", Namespace: "team", Name: "web-0"}}, nil)
	if !del.IsError || resultCode(del) != "confirmationUnavailable" {
		t.Errorf("object_delete headless: %s (code %q)", resultText(del), resultCode(del))
	}
	if after := srv.Requests()["delete pods"]; after != before {
		t.Errorf("the refused delete reached the API server: %d requests", after-before)
	}

	if code := stop(); code != exitOK {
		t.Errorf("exit code %d when the agent disconnected; stderr: %s", code, te.errOut)
	}
	if te.out.String() != "" {
		t.Errorf("stdout carried more than the protocol: %q", te.out)
	}
	audit, err := os.ReadFile(filepath.Join(te.dirs.Config, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"object.delete"`, `"resources.list"`, `"externalAgent"`} {
		if !strings.Contains(string(audit), want) {
			t.Errorf("audit log lacks %s:\n%s", want, audit)
		}
	}
	if strings.Contains(string(audit)+te.errOut.String(), "fake-TOKEN-5678") {
		t.Error("the kubeconfig's token reached the audit log or stderr")
	}
}

// startApp starts a running Cogpit for the bridge tests: a session on the demo cluster with its MCP server
// started, its token and endpoint files in the test's Cogpit directory.
func startApp(t *testing.T, te *testEnv) *engine.Session {
	t.Helper()
	s := engine.New(engine.Options{
		Connector: &engine.ClusterConnector{IncludeDemo: true, DemoOptions: te.demo},
		Logger:    te.logger,
	})
	t.Cleanup(s.Close)
	if err := commands.RegisterAll(s.Registry()); err != nil {
		t.Fatal(err)
	}
	srv, err := mcp.New(s, mcp.Options{TokenFile: filepath.Join(te.dirs.Config, mcp.TokenFileName), Logger: te.logger})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return s
}

// TestMCPForwardsToTheRunningApp: with Cogpit running, the bridge's calls change the app's own
// state; --headless serves an engine of its own instead.
func TestMCPForwardsToTheRunningApp(t *testing.T) {
	te := newTestEnv(t)
	te.switchMCPOn(t)
	app := startApp(t, te)

	cs, stop := te.serveMCP(t)
	if res := callTool(t, cs, "cluster_connect", map[string]string{"context": "demo"}, nil); res.IsError {
		t.Fatalf("cluster_connect: %s", resultText(res))
	}
	if st := app.UIState(); st.Context != "demo" || st.Connection.State != engine.Connected {
		t.Errorf("the app did not connect: %+v", st.Connection)
	}
	if code := stop(); code != exitOK {
		t.Errorf("exit code %d; stderr: %s", code, te.errOut)
	}

	app.Close() // the app's state stays as it is from here; a headless call must not reach it
	cs, stop = te.serveMCP(t, "--headless")
	var st engine.UIState
	if res := callTool(t, cs, "ui_state", nil, &st); res.IsError || st.Context != "" {
		t.Errorf("--headless served the app's state: %s", resultText(res))
	}
	if code := stop(); code != exitOK {
		t.Errorf("--headless exit code %d; stderr: %s", code, te.errOut)
	}
}

// TestMCPStaleFilesServeHeadless: the token and endpoint files of a Cogpit that crashed name an
// address nothing listens at; the bridge serves an engine of its own.
func TestMCPStaleFilesServeHeadless(t *testing.T) {
	te := newTestEnv(t)
	te.switchMCPOn(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	writeAppFiles(t, te, "http://"+addr+"/mcp")

	cs, stop := te.serveMCP(t)
	var ctxs []engine.ContextInfo
	if res := callTool(t, cs, "cluster_contexts", nil, &ctxs); res.IsError || len(ctxs) == 0 || ctxs[len(ctxs)-1].Name != "demo" {
		t.Errorf("cluster_contexts: %s %+v", resultText(res), ctxs)
	}
	if code := stop(); code != exitOK {
		t.Errorf("exit code %d; stderr: %s", code, te.errOut)
	}
}

func writeAppFiles(t *testing.T, te *testEnv, url string) {
	t.Helper()
	if err := os.MkdirAll(te.dirs.Config, 0o700); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("cd", 32)
	if err := os.WriteFile(filepath.Join(te.dirs.Config, mcp.TokenFileName), []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(te.dirs.Config, mcp.EndpointFileName), []byte(url+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestMCPRefusesAForeignEndpoint: an endpoint file that names another machine is an error, not a
// reason to send the token there or to serve a headless engine.
func TestMCPRefusesAForeignEndpoint(t *testing.T) {
	te := newTestEnv(t)
	te.switchMCPOn(t)
	writeAppFiles(t, te, "http://192.0.2.10:4711/mcp")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	serverT, _ := sdk.NewInMemoryTransports()
	te.mcpTransport = serverT
	if code := run(ctx, []string{"mcp"}, te.env); code != exitError {
		t.Errorf("exit code %d, want %d", code, exitError)
	}
	if !strings.Contains(te.errOut.String(), "not on this machine") {
		t.Errorf("stderr: %s", te.errOut)
	}
}

// TestMCPFreshInstallationIsOff: the external MCP server is opt-in (D-49, the owner's decision of
// 2026-10-21). In a Cogpit directory without a switch file, a new installation, the bridge serves
// neither the running app's tools nor a headless engine's: the agent is told the server is off, with
// --headless too, and the bridge does not write the switch for the user. Where Cogpit's directory is
// unknown the switch cannot have been switched on, so it is the same.
func TestMCPFreshInstallationIsOff(t *testing.T) {
	te := newTestEnv(t)
	if _, err := os.Lstat(mcp.SwitchPath(te.dirs.Config)); !os.IsNotExist(err) {
		t.Fatalf("a fresh test environment has a switch file: %v", err)
	}
	for _, args := range [][]string{nil, {"--headless"}} {
		cs, stop := te.serveMCP(t, args...)
		if got := cs.InitializeResult().Instructions; got != mcp.SwitchedOffMessage {
			t.Errorf("%v: instructions %q", args, got)
		}
		tools, err := cs.ListTools(context.Background(), nil)
		if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "cogpit_mcp_off" {
			t.Errorf("%v: tools %+v, %v", args, tools, err)
		}
		if code := stop(); code != exitOK {
			t.Errorf("%v: exit code %d; stderr: %s", args, code, te.errOut)
		}
	}
	if _, err := os.Lstat(mcp.SwitchPath(te.dirs.Config)); !os.IsNotExist(err) {
		t.Errorf("the bridge wrote the switch: %v", err)
	}

	te = newTestEnv(t)
	te.dirs, te.dirsErr = app.Dirs{}, errors.New("neither $XDG_CONFIG_HOME nor $HOME are defined")
	cs, stop := te.serveMCP(t)
	if got := cs.InitializeResult().Instructions; got != mcp.SwitchedOffMessage {
		t.Errorf("no configuration directory: instructions %q", got)
	}
	if code := stop(); code != exitOK {
		t.Errorf("no configuration directory: exit code %d; stderr: %s", code, te.errOut)
	}
}

// TestMCPSwitchedOffRefuses: with the user's switch off (D-49, A15) the bridge serves neither the
// running app's tools nor a headless engine's: the agent is told the server is off, also with
// --headless; switched on again, the headless engine serves.
func TestMCPSwitchedOffRefuses(t *testing.T) {
	te := newTestEnv(t)
	if err := mcp.WriteSwitch(te.dirs.Config, false); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"--headless"}} {
		cs, stop := te.serveMCP(t, args...)
		if got := cs.InitializeResult().Instructions; got != mcp.SwitchedOffMessage {
			t.Errorf("%v: instructions %q", args, got)
		}
		tools, err := cs.ListTools(context.Background(), nil)
		if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "cogpit_mcp_off" {
			t.Errorf("%v: tools %+v, %v", args, tools, err)
		}
		if code := stop(); code != exitOK {
			t.Errorf("%v: exit code %d; stderr: %s", args, code, te.errOut)
		}
	}
	if err := mcp.WriteSwitch(te.dirs.Config, true); err != nil {
		t.Fatal(err)
	}
	cs, stop := te.serveMCP(t)
	var ctxs []engine.ContextInfo
	if res := callTool(t, cs, "cluster_contexts", nil, &ctxs); res.IsError || len(ctxs) == 0 {
		t.Errorf("switched on: cluster_contexts %s", resultText(res))
	}
	stop()
}
