package mcp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The switch file (D-49, A15): the server is opt-in (the owner, 2026-10-21), so no file is off and
// unset; "on" and "off" round-trip owner-only; a file that holds anything else, or is a symlink,
// reads as off, set, with an error: the switch fails closed.
func TestSwitchFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Cogpit")
	if on, set, err := ReadSwitch(dir); on || set || err != nil {
		t.Fatalf("no file: on %v, set %v, %v", on, set, err)
	}
	if _, err := os.Lstat(SwitchPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("reading the switch created it: %v", err)
	}
	for _, want := range []bool{false, true, false} {
		if err := WriteSwitch(dir, want); err != nil {
			t.Fatal(err)
		}
		if on, set, err := ReadSwitch(dir); on != want || !set || err != nil {
			t.Fatalf("stored %v: on %v, set %v, %v", want, on, set, err)
		}
	}
	// Windows sets no file modes: there the user profile's permissions protect the switch.
	if fi, err := os.Stat(SwitchPath(dir)); err != nil || (runtime.GOOS != goosWindows && fi.Mode().Perm() != 0o600) {
		t.Errorf("the switch file: %v, %v", fi.Mode(), err)
	}
	if err := os.WriteFile(SwitchPath(dir), []byte("yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if on, set, err := ReadSwitch(dir); on || !set || err == nil {
		t.Errorf("garbage: on %v, set %v, %v", on, set, err)
	}
	if runtime.GOOS == goosWindows {
		return // symlinks need a privilege there
	}
	other := filepath.Join(t.TempDir(), "on")
	if err := os.WriteFile(other, []byte("on\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(SwitchPath(dir))
	if err := os.Symlink(other, SwitchPath(dir)); err != nil {
		t.Fatal(err)
	}
	if on, _, err := ReadSwitch(dir); on || err == nil {
		t.Errorf("a symlink: on %v, %v", on, err)
	}
	if err := WriteSwitch(dir, true); err == nil {
		t.Error("the switch was written through a symlink")
	}
}

// A bridge behind a new installation is told the server is off because it is opt-in, and one behind
// an installation whose user switched it off is told the same: the message claims neither case.
func TestSwitchedOffMessageHoldsForBothCases(t *testing.T) {
	for _, want := range []string{"switched off", "opt-in", "switched it on", "status bar"} {
		if !strings.Contains(SwitchedOffMessage, want) {
			t.Errorf("the message lacks %q: %s", want, SwitchedOffMessage)
		}
	}
	if strings.Contains(SwitchedOffMessage, "turned it off") {
		t.Errorf("the message says the user turned it off, which a new installation never did: %s", SwitchedOffMessage)
	}
}

// The server a switched-off bridge serves tells the agent why, in its instructions and in its one
// tool's error, and offers nothing else.
func TestServeSwitchedOff(t *testing.T) {
	serverT, clientT := sdk.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ServeSwitchedOff(ctx, serverT, "test") }()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "agent", Version: "1"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := cs.InitializeResult().Instructions; got != SwitchedOffMessage {
		t.Errorf("instructions %q", got)
	}
	tools, err := cs.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "cogpit_mcp_off" {
		t.Fatalf("tools %+v, %v", tools, err)
	}
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "cogpit_mcp_off"})
	if err != nil || !res.IsError || len(res.Content) != 1 || !strings.Contains(res.Content[0].(*sdk.TextContent).Text, "switched off") {
		t.Errorf("the tool: %+v, %v", res, err)
	}
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "cluster_contexts"}); err == nil {
		t.Error("a Cogpit tool answered while the switch is off")
	}
	_ = cs.Close()
	select {
	case _ = <-done:
	case <-ctx.Done():
		t.Fatal("the switched-off server did not end with the client")
	}
}
