package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/mcp"
	"github.com/guided-traffic/cogpit/internal/ui/fyneui"
)

// auditMCPSwitch is the audit log's name for the user's switch of the MCP server; the line's origin
// is "gui" and its parameters are {"enabled": true|false}.
const auditMCPSwitch = "gui.mcpServer"

// mcpControl is the window's handle on the MCP server (fyneui.MCPControl): the status bar item
// stops and starts the server through it, and its details regenerate the token. It lives in the
// app's main package because internal/ui may not import internal/mcp. Only the window holds it; no
// engine command or MCP tool reaches it, so an agent can neither switch the server on nor cut the
// other clients off with a new token.
type mcpControl struct {
	srv     *mcp.Server
	session *engine.Session
	logger  *slog.Logger
	out     io.Writer // where the registration command is printed: the terminal that started Cogpit
	// cli is the cogpit-cli in the running app's bundle (bundledCLI), "" without one. With it the
	// window offers the stdio registration, made once, instead of the HTTP one with this run's port
	// and token (D-2).
	cli string
	// config is Cogpit's configuration directory, where the switch is kept for cogpit-cli (D-49);
	// "" keeps it in the window's preferences only.
	config string
}

var (
	_ fyneui.MCPControl     = (*mcpControl)(nil)
	_ fyneui.MCPSwitchStore = (*mcpControl)(nil)
)

// StoredMCP implements fyneui.MCPSwitchStore. The server is opt-in (D-49, 2026-10-21): no switch
// file is off and unset, and a file that cannot be read counts as set and off, as cogpit-cli reads
// it, so the server stays off until the user clicks.
func (c *mcpControl) StoredMCP() (on, set bool) {
	if c.config == "" {
		return false, false
	}
	on, set, err := mcp.ReadSwitch(c.config)
	if err != nil {
		c.logger.Warn("MCP switch unreadable; the server stays off", "err", err)
	}
	return on, set
}

// StoreMCP implements fyneui.MCPSwitchStore: the switch file cogpit-cli mcp reads (D-49).
func (c *mcpControl) StoreMCP(on bool) error {
	if c.config == "" {
		return nil
	}
	return mcp.WriteSwitch(c.config, on)
}

// auditMCPToken is the audit log's name for the user's Regenerate token (F6); origin "gui", the
// parameters {"regenerated": true}, never the token.
const auditMCPToken = "gui.mcpToken" // #nosec G101 -- an audit action's name, no credential

// Start implements fyneui.MCPControl. The server starts with the token and port it kept (F6). The
// command for the window is the stdio registration of the bundled cogpit-cli when there is one,
// else the HTTP registration, which carries the token: either goes to the terminal that started
// Cogpit and to the window, never to the logger. A switch by the user is recorded in the audit
// log, the start at launch is not.
func (c *mcpControl) Start(fromUser bool) (fyneui.MCPEndpoint, error) {
	err := c.srv.Start(context.Background())
	if fromUser {
		c.session.AuditUser(auditMCPSwitch, map[string]bool{"enabled": true}, err)
	}
	if err != nil {
		return fyneui.MCPEndpoint{}, err
	}
	ep := c.endpoint()
	ep.PortTaken = c.srv.PortTaken()
	c.announce(ep)
	return ep, nil
}

// Regenerate implements fyneui.MCPControl: a new token (F6), the old one refused at once. The
// user's click is recorded in the audit log, without the token. The window gets the new endpoint
// while the server runs, and an empty one while it is off.
func (c *mcpControl) Regenerate() (fyneui.MCPEndpoint, error) {
	err := c.srv.RegenerateToken()
	c.session.AuditUser(auditMCPToken, map[string]bool{"regenerated": true}, err)
	if err != nil {
		return fyneui.MCPEndpoint{}, err
	}
	ep := c.endpoint()
	if ep.URL != "" {
		c.announce(ep)
	}
	return ep, nil
}

// endpoint is what the window shows of the running server: "" values while it is off.
func (c *mcpControl) endpoint() fyneui.MCPEndpoint {
	ep := fyneui.MCPEndpoint{URL: c.srv.URL(), Token: c.srv.Token(), HTTPCommand: c.srv.ClaudeAddCommand()}
	ep.Command = ep.HTTPCommand
	if cmd := mcp.ClaudeAddStdioCommand(c.cli); cmd != "" && ep.URL != "" { // "" for no cli
		ep.Command, ep.Stdio = cmd, true
	}
	return ep
}

// announce prints the registration command to the terminal that started Cogpit.
func (c *mcpControl) announce(ep fyneui.MCPEndpoint) {
	if c.out == nil {
		return
	}
	how := "once, and again after you regenerate the token"
	if ep.Stdio {
		how = "once: Cogpit's own cogpit-cli follows every restart and a new token"
	}
	_, _ = fmt.Fprintf(c.out, "Cogpit MCP server: %s\nRegister it with Claude Code (%s):\n\n  %s\n\n", ep.URL, how, ep.Command)
}

// Stop implements fyneui.MCPControl: the server stops listening, every agent connection and
// pending confirmation ends, and the endpoint file is removed; the token and the port stay for
// the next start (F6). The user's switch is recorded in the audit log.
func (c *mcpControl) Stop() error {
	err := c.srv.Stop()
	c.session.AuditUser(auditMCPSwitch, map[string]bool{"enabled": false}, err)
	if err != nil {
		c.logger.Warn("MCP server stopped with an error", "err", err)
	}
	return err
}
