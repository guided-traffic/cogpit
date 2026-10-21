package mcp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The user's switch of the external MCP server, kept where cogpit-cli reads it (D-49, the owner's
// A15 of 2026-10-14 evening): a file beside the token file, "on" or "off", written by the window's
// status bar item only (cmd/cogpit's mcpControl), never by an engine command or an MCP tool. With
// it off, `cogpit-cli mcp` refuses to serve, headless too: it answers the agent with
// SwitchedOffMessage instead of Cogpit's tools. Since 2026-10-21 (the owner's security posture,
// D-49) the server is opt-in: no file means off, so a new installation starts a server for agents
// only after the user switched it on, and a bridge started before that is told so.

// SwitchFileName is the switch file's name in Cogpit's configuration directory.
const SwitchFileName = "mcp-switch"

// SwitchedOffMessage is what an agent behind `cogpit-cli mcp` is told while the external MCP server
// is off: either the user never switched it on (it is opt-in, off on a new installation) or the
// user switched it off again, so the text claims neither.
const SwitchedOffMessage = "Cogpit's external MCP server is switched off. It is opt-in: it runs only after the user " +
	"switched it on with the MCP item in the status bar of Cogpit's window, and while it is off no agent outside Cogpit " +
	"can use Cogpit's tools, headless included. Ask the user to switch it on there if you need Cogpit."

// SwitchPath returns the switch file's path in Cogpit's configuration directory configDir
// (<user config dir>/Cogpit).
func SwitchPath(configDir string) string { return filepath.Join(configDir, SwitchFileName) }

// ReadSwitch returns the stored switch: on, and whether the user ever set it. No file is off and
// unset: the server is opt-in, so only a stored "on" starts it, in the window or behind
// `cogpit-cli mcp`. A file that holds neither "on" nor "off", or that is a symlink, counts as off
// too: the switch fails closed.
func ReadSwitch(configDir string) (on, set bool, err error) {
	path := SwitchPath(configDir)
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, false, nil
	case err != nil:
		return false, true, fmt.Errorf("mcp: switch file: %w", err)
	case !fi.Mode().IsRegular():
		return false, true, fmt.Errorf("mcp: switch file %s is not a regular file", path)
	}
	b, err := os.ReadFile(path) //nolint:gosec // Cogpit's own file in its own directory
	if err != nil {
		return false, true, fmt.Errorf("mcp: switch file: %w", err)
	}
	switch strings.TrimSpace(string(b)) {
	case "on":
		return true, true, nil
	case "off":
		return false, true, nil
	}
	return false, true, fmt.Errorf("mcp: switch file %s holds neither on nor off", path)
}

// WriteSwitch stores the user's switch, owner-only and atomically (as the token file).
func WriteSwitch(configDir string, on bool) error {
	v := "off"
	if on {
		v = "on"
	}
	return writePrivateLine(SwitchPath(configDir), v, "switch", true)
}

// ServeSwitchedOff serves an MCP server on t that tells the agent the external server is off: its
// instructions say so, and its one tool, cogpit_mcp_off, returns the same as an error. It never
// reaches an engine. It returns when the client closes the stream or ctx ends.
func ServeSwitchedOff(ctx context.Context, t sdk.Transport, version string) error {
	srv := sdk.NewServer(&sdk.Implementation{Name: serverName, Title: "Cogpit (MCP switched off)", Version: version}, &sdk.ServerOptions{
		Instructions: SwitchedOffMessage,
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}},
	})
	tool := &sdk.Tool{
		Name:        "cogpit_mcp_off",
		Description: SwitchedOffMessage,
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}
	if err := addTool(srv, tool, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: SwitchedOffMessage}}}, nil
	}); err != nil {
		return err
	}
	return srv.Run(ctx, t)
}
