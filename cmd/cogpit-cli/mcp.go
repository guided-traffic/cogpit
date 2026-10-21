package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guided-traffic/cogpit/internal/buildinfo"
	"github.com/guided-traffic/cogpit/internal/mcp"
)

const mcpAbout = `Serves Cogpit's MCP tools over stdin and stdout, for agents that speak MCP only over stdio:
register "cogpit-cli mcp" as an MCP server once, instead of the URL and token, which change when
the user regenerates the token in Cogpit's window or Cogpit finds its port taken.

While Cogpit runs with its MCP server switched on (its token and endpoint files in Cogpit's
configuration directory), every call is forwarded to it: the agent drives the window the user
sees, and mutating and destructive commands wait for the user's confirmation there. The bridge
reads the endpoint and token files before every call, so it follows a restart of Cogpit, a new
port and a regenerated token by itself and never sends the token to where Cogpit no longer
listens; a call is sent again only if it never reached Cogpit.

Without a running Cogpit (or with --headless) the bridge runs an engine of its own, without a
window: read and UI commands run (connect a context with cluster_connect first), and commands
that need the user's confirmation are refused, since there is no window to ask in. It reads the
same kubeconfig sources and runs only the credential plugins the user allowed in Cogpit's window.
Every command an agent runs through it is written to Cogpit's audit log.

The external MCP server is opt-in: it is off on a new installation until the user switches it on
with the MCP item in the status bar of Cogpit's window, and off again once the user switches it
off there. While it is off the bridge serves none of this, headless included: it tells the agent
that the server is off. Without a window, the file mcp-switch in Cogpit's configuration directory
holds the switch: "on" opts in.

stdout carries the protocol only; logs go to stderr.`

func runMCP(ctx context.Context, args []string, e *env) int {
	fs := newFlags("mcp", "[flags]", mcpAbout)
	var c common
	c.register(fs)
	headless := fs.Bool("headless", false, "serve an engine of its own even when Cogpit runs (--kubeconfig applies to it only)")
	if code, done := fs.parse(args, e); done {
		return code
	}
	if len(fs.positional) > 0 {
		return fs.usageError(e, "mcp takes no arguments")
	}
	c.apply(e)
	t := e.mcpTransport
	if t == nil {
		t = &sdk.StdioTransport{}
	}

	// The user's switch of the external MCP server (D-49, A15): the server is opt-in, so no switch
	// file is off, and off the bridge serves no engine, forwarding or headless, and tells the agent
	// why. Unreadable counts as off, and so does an unknown configuration directory: where the
	// switch cannot be read it cannot have been switched on.
	on := false
	if e.dirsErr == nil && e.dirs.Config != "" {
		var err error
		if on, _, err = mcp.ReadSwitch(e.dirs.Config); err != nil {
			e.logger.Warn("the MCP switch is unreadable; serving as switched off", "err", err)
		}
	} else {
		e.logger.Warn("Cogpit's configuration directory is unknown, so the MCP switch cannot have been switched on; serving as switched off", "err", e.dirsErr)
	}
	if !on {
		e.logger.Info("Cogpit's external MCP server is switched off (opt-in: the user switches it on in Cogpit's window); telling the agent so")
		return served(e, mcp.ServeSwitchedOff(ctx, t, buildinfo.Version()))
	}

	if !*headless {
		br, err := mcp.DialApp(ctx, mcp.BridgeOptions{
			TokenFile: filepath.Join(e.dirs.Config, mcp.TokenFileName), Version: buildinfo.Version(), Logger: e.logger,
		})
		switch {
		case err == nil:
			defer func() { _ = br.Close() }()
			return served(e, br.Serve(ctx, t))
		case !errors.Is(err, mcp.ErrAppNotRunning):
			return fail(e, err)
		}
		e.logger.Info("no running Cogpit serves MCP; serving an engine of the bridge's own", "reason", err.Error())
	}

	s, closeSession, err := e.newSession(sessionOptions{kubeconfig: c.kubeconfig, audit: true})
	if err != nil {
		return fail(e, err)
	}
	defer closeSession()
	defer s.WatchKubeconfigs(0)() // a sync folder's files that come or go, as in the app
	srv, err := mcp.New(s, mcp.Options{Version: buildinfo.Version(), Logger: e.logger})
	if err != nil {
		return fail(e, err)
	}
	defer func() { _ = srv.Close() }()
	return served(e, srv.ServeTransport(ctx, t))
}

// served turns the end of serving into an exit code: the client closing the stream, or a signal,
// is a normal end.
func served(e *env, err error) int {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, sdk.ErrConnectionClosed) {
		return exitOK
	}
	return fail(e, fmt.Errorf("mcp: %w", err))
}
