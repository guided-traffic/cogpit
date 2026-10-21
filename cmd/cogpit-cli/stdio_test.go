package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guided-traffic/cogpit/internal/app"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/mcp"
)

// childEnv marks a run of this test binary as cogpit-cli itself (TestMain).
const childEnv = "COGPIT_CLI_TEST_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(childEnv) == "1" {
		main()
	}
	os.Exit(m.Run())
}

// startRealCLI runs cogpit-cli mcp as its own process, as an agent host does, with a home of its own
// (so Cogpit's real directory and ~/.kube are never read), and connects an agent to its stdio. The
// test's environment points at the same home, so Cogpit's directory under it is the child's: a test
// that switches the external MCP server on writes the switch there before it connects.
func startRealCLI(t *testing.T, home string, switchedOn bool) (*sdk.ClientSession, *exec.Cmd, *bytes.Buffer) {
	t.Helper()
	// Windows takes the configuration and cache directories from %APPDATA% and %LOCALAPPDATA%, not
	// from the home: without them the switch below would land in the developer's real one.
	appData, localAppData := filepath.Join(home, "AppData", "Roaming"), filepath.Join(home, "AppData", "Local")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", appData)
	t.Setenv("LOCALAPPDATA", localAppData)
	if switchedOn {
		dirs, err := app.UserDirs()
		if err != nil {
			t.Fatal(err)
		}
		if err := mcp.WriteSwitch(dirs.Config, true); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, exe, "mcp", "-v")
	cmd.Env = []string{
		childEnv + "=1", "HOME=" + home, "USERPROFILE=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"APPDATA=" + appData, "LOCALAPPDATA=" + localAppData,
		"KUBECONFIG=" + os.DevNull, "PATH=" + os.Getenv("PATH"),
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	cs, err := sdk.NewClient(&sdk.Implementation{Name: "agent", Version: "1"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v; stderr: %s", err, stderr.String())
	}
	return cs, cmd, stderr
}

// TestMCPOverRealStdio runs cogpit-cli mcp as its own process, as an agent host does, in a home of
// its own whose user switched the external MCP server on: the agent speaks MCP over the process's
// stdin and stdout, nothing else reaches stdout (logs go to stderr), and closing stdin ends the
// process.
func TestMCPOverRealStdio(t *testing.T) {
	cs, cmd, stderr := startRealCLI(t, t.TempDir(), true)
	var ctxs []engine.ContextInfo
	if res := callTool(t, cs, "cluster_contexts", nil, &ctxs); res.IsError || len(ctxs) != 1 || ctxs[0].Name != "demo" {
		t.Errorf("cluster_contexts in an empty home: %s %+v", resultText(res), ctxs)
	}
	var st engine.UIState
	if res := callTool(t, cs, "ui_state", nil, &st); res.IsError || st.Connection.State != engine.Disconnected {
		t.Errorf("ui_state: %s", resultText(res))
	}
	if err := cs.Close(); err != nil {
		t.Errorf("close: %v; stderr: %s", err, stderr.String())
	}
	// Closing stdin is the normal end: exit code 0, no error.
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != exitOK || strings.Contains(stderr.String(), "cogpit-cli: ") {
		t.Errorf("process state %v; stderr: %s", cmd.ProcessState, stderr.String())
	}
	// -v logs at debug level, to stderr: the bridge found no running Cogpit and served its own engine.
	if !strings.Contains(stderr.String(), "no running Cogpit serves MCP") {
		t.Errorf("stderr: %s", stderr.String())
	}
}

// TestMCPOverRealStdioFreshHomeIsOff: the same process in a home whose user never switched the
// server on (a new installation) tells the agent the server is off and serves none of Cogpit's
// tools; the external MCP server is opt-in (D-49).
func TestMCPOverRealStdioFreshHomeIsOff(t *testing.T) {
	cs, cmd, stderr := startRealCLI(t, t.TempDir(), false)
	if got := cs.InitializeResult().Instructions; got != mcp.SwitchedOffMessage {
		t.Errorf("instructions %q; stderr: %s", got, stderr.String())
	}
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "cogpit_mcp_off" {
		t.Errorf("tools %+v, %v", tools, err)
	}
	if err := cs.Close(); err != nil {
		t.Errorf("close: %v; stderr: %s", err, stderr.String())
	}
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != exitOK {
		t.Errorf("process state %v; stderr: %s", cmd.ProcessState, stderr.String())
	}
	if !strings.Contains(stderr.String(), "switched off") || strings.Contains(stderr.String(), "no running Cogpit serves MCP") {
		t.Errorf("stderr: %s", stderr.String())
	}
}
