// Package mcp serves the engine's command registry as MCP tools over Streamable HTTP on
// 127.0.0.1, behind a random bearer token and Host and Origin checks (D-2,
// docs/security/agents-and-mcp.md). Every tool call dispatches through engine.Session with origin
// ExternalAgent, so the engine's confirmation rules and audit log apply.
//
// Layer L4.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/guided-traffic/cogpit/internal/engine"
)

const (
	// defaultAddr binds a free port on the IPv4 loopback address.
	defaultAddr = "127.0.0.1:0"
	// endpointPath is the one path the server answers on.
	endpointPath = "/mcp"
	// serverName is the MCP server name and the name the Claude Code registration uses.
	serverName = "cogpit"
	// shutdownGrace bounds how long Close waits for open requests before cutting connections.
	shutdownGrace = 2 * time.Second
)

// Options configure the server.
type Options struct {
	// Addr is the listen address; it must be a loopback address. Empty means 127.0.0.1:0. Port 0
	// binds the port the last start kept in PortFileName, and a free port when there is none or it
	// is taken (PortTaken), which is kept then; a port given here binds that port or fails, and is
	// not kept.
	Addr string
	// TokenFile is where the bearer token is kept, owner-only (0600) in an owner-only (0700)
	// directory, across starts until the user regenerates it (F6). Empty means
	// <user config dir>/Cogpit/mcp-token. The endpoint's URL is written beside it while the server
	// listens, in EndpointFileName, for the stdio bridge, and the port in PortFileName.
	TokenFile string
	// Version is reported to clients; empty means "dev".
	Version string
	// Logger receives the server's logs; nil means slog.Default(). The token is never logged.
	Logger *slog.Logger
}

type serverState int

const (
	stateNew serverState = iota
	stateRunning
	stateClosed
)

// Server is the MCP endpoint for one engine session. Create it with New, call Start, and Close it
// on exit. Stop ends the endpoint like Close does but lets Start bring it back, with the same token
// and port (the user's switch in the status bar); a closed server cannot be started again.
// RegenerateToken replaces the token, the one way it changes (F6).
type Server struct {
	session *engine.Session
	opts    Options
	log     *slog.Logger

	// ctl serialises Start, Stop, Close and RegenerateToken as wholes, so that a Start right after
	// a Stop finds the port free again and the old run's endpoint file gone.
	ctl sync.Mutex

	mu sync.Mutex
	// lifetime ends with Stop or Close. Every tool call's context ends with it, so that a call
	// waiting for the user's confirmation does not outlive the endpoint. Each Start after a Stop
	// gets a new one. Guarded by mu.
	lifetime context.Context
	stop     context.CancelFunc

	state     serverState
	token     string
	tokenPath string
	url       string
	httpSrv   *http.Server
	guard     *guard        // the running server's guard, which RegenerateToken gives the new token
	served    chan struct{} // closed when http.Serve has returned
	// portTaken is the kept port the last Start found taken, 0 when it was free or none was kept.
	portTaken int
}

// New prepares a server for the session's registry. Nothing listens until Start. It refuses a
// listen address that is not a loopback address.
func New(session *engine.Session, opts Options) (*Server, error) {
	if session == nil {
		return nil, errors.New("mcp: a session is required")
	}
	if opts.Addr == "" {
		opts.Addr = defaultAddr
	}
	if _, err := listenAddr(opts.Addr); err != nil {
		return nil, err
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	lifetime, stop := context.WithCancel(context.Background())
	return &Server{
		session: session, opts: opts, log: log.With("component", "mcp"),
		lifetime: lifetime, stop: stop, tokenPath: opts.TokenFile,
	}, nil
}

// Start brings the endpoint up with the token kept in the token file, or a new one written there
// when there is none Cogpit can keep using (loadToken), on the port kept beside it (Options.Addr),
// writes the URL to the endpoint file beside the token, builds one MCP tool per registered command
// and starts listening. It returns once the listener is bound; ctx bounds only that start-up, and
// Close stops the server. Commands registered after Start are not tools until the next start.
func (s *Server) Start(ctx context.Context) error {
	s.ctl.Lock()
	defer s.ctl.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.state {
	case stateRunning:
		return errors.New("mcp: server already started")
	case stateClosed:
		return errors.New("mcp: server closed; create a new one")
	}

	tokenPath, ownDir, err := s.paths()
	if err != nil {
		return err
	}
	token, dropped, err := loadToken(tokenPath)
	if err != nil {
		return err
	}
	fresh := token == ""
	if fresh {
		if token, err = newToken(); err != nil {
			return err
		}
	}
	ln, tcp, taken, err := s.listen(ctx, tokenPath)
	if err != nil {
		return err
	}
	if fresh {
		if err := writeTokenFile(tokenPath, token, ownDir); err != nil {
			_ = ln.Close()
			return err
		}
		if dropped != "" {
			s.log.Warn("MCP token not kept; Cogpit made a new one, which clients need", "reason", dropped)
		}
	}
	// The endpoint file beside the token tells the stdio bridge (cogpit-cli mcp) where to forward.
	url := "http://" + net.JoinHostPort(tcp.IP.String(), strconv.Itoa(tcp.Port)) + endpointPath
	if err := writePrivateLine(endpointFile(tokenPath), url, "endpoint", ownDir); err != nil {
		_ = ln.Close()
		return err
	}
	s.keepPort(tokenPath, tcp.Port, ownDir)

	life := s.lifetime
	srv, tools := s.newMCPServer()
	sdkHandler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv }, &sdk.StreamableHTTPOptions{
		// Stateless: every POST is its own session. Clients of both protocol generations work
		// (the 2026-07-28 protocol runs only stateless over HTTP), no session table grows, and a
		// call's lifetime is the lifetime of the HTTP request that carries it, which callContext
		// relies on to end a pending confirmation when the client goes away.
		Stateless:                    true,
		PropagateRequestCancellation: true,
		Logger:                       sdkLogger(s.log),
		// The SDK's own DNS-rebinding check (DisableLocalhostProtection false) stays on behind
		// the guard.
	})
	mux := http.NewServeMux()
	mux.Handle(endpointPath, sdkHandler)

	s.guard = newGuard(tcp, token, s.log, mux)
	s.httpSrv = &http.Server{
		Handler:           s.guard,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
		// Request contexts end with Close, so streams and pending calls unwind at once.
		BaseContext: func(net.Listener) context.Context { return life },
	}
	s.served = make(chan struct{})
	go s.serve(s.httpSrv, ln, s.served)

	s.token = token
	s.tokenPath = tokenPath
	s.url = url
	s.portTaken = taken
	s.state = stateRunning
	s.log.Info("MCP server listening", "url", s.url, "tokenFile", tokenPath, "newToken", fresh, "tools", tools)
	return nil
}

// paths resolves the token file: Options.TokenFile, or the default in Cogpit's own directory,
// whose mode Cogpit may tighten (ownDir, writePrivateLine).
func (s *Server) paths() (tokenPath string, ownDir bool, err error) {
	if s.opts.TokenFile != "" {
		return s.opts.TokenFile, false, nil
	}
	p, err := defaultTokenPath()
	if err != nil {
		return "", false, err
	}
	return p, true, nil
}

// keepsPort reports whether the server keeps its port across starts: Options.Addr asks for any
// port (0), not a given one.
func (s *Server) keepsPort() bool {
	addr, err := listenAddr(s.opts.Addr)
	if err != nil {
		return false
	}
	_, port, err := net.SplitHostPort(addr)
	return err == nil && port == "0"
}

// listen binds the listen address. With any port asked for (keepsPort) it binds the port the last
// start kept, so that a registration made once keeps working, and a free port when that one is not
// free: taken is the kept port then, 0 otherwise.
func (s *Server) listen(ctx context.Context, tokenPath string) (ln net.Listener, tcp *net.TCPAddr, taken int, err error) {
	addr, err := listenAddr(s.opts.Addr)
	if err != nil {
		return nil, nil, 0, err
	}
	var lc net.ListenConfig
	if s.keepsPort() {
		if p := readPort(portFile(tokenPath)); p > 0 {
			host, _, _ := net.SplitHostPort(addr)
			ln, err = lc.Listen(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(p)))
			if err == nil {
				return checkListener(ln)
			}
			if ctx.Err() != nil {
				return nil, nil, 0, fmt.Errorf("mcp: listen on port %d: %w", p, err)
			}
			s.log.Warn("the MCP server's port is not free; listening on another", "port", p, "err", err)
			taken = p
		}
	}
	ln, err = lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("mcp: listen on %s: %w", addr, err)
	}
	ln, tcp, _, err = checkListener(ln)
	return ln, tcp, taken, err
}

// checkListener returns ln with its TCP address.
func checkListener(ln net.Listener) (net.Listener, *net.TCPAddr, int, error) {
	tcp, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return nil, nil, 0, fmt.Errorf("mcp: unexpected listener address %v", ln.Addr())
	}
	return ln, tcp, 0, nil
}

// keepPort writes the port to the port file for the next start, when the server keeps its port
// and the file does not hold it already. A port that cannot be kept is logged: the server runs,
// and the next start may listen on another port.
func (s *Server) keepPort(tokenPath string, port int, ownDir bool) {
	if !s.keepsPort() || readPort(portFile(tokenPath)) == port {
		return
	}
	if err := writePrivateLine(portFile(tokenPath), strconv.Itoa(port), "port", ownDir); err != nil {
		s.log.Warn("MCP port not kept; the next start may listen on another port", "err", err)
	}
}

// RegenerateToken replaces the bearer token with a new one and writes it to the token file: the
// user's click in the window (F6), the one way the token changes besides a token file Cogpit cannot
// keep (loadToken). A running server keeps listening on its port, refuses the old token at once and
// ends every request it admitted, a tool call waiting for the user's confirmation included (its
// command does not run); a stopped server starts with the new token. The file is written first, so
// that a client refused the old token reads the new one there (the stdio bridge does, on a 401).
// Like Stop it is the window's alone: no tool reaches it, so that no agent can cut the other
// clients off.
func (s *Server) RegenerateToken() error {
	s.ctl.Lock()
	defer s.ctl.Unlock()
	s.mu.Lock()
	closed := s.state == stateClosed
	s.mu.Unlock()
	if closed {
		return errors.New("mcp: server closed")
	}
	tokenPath, ownDir, err := s.paths()
	if err != nil {
		return err
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	if err := writeTokenFile(tokenPath, token, ownDir); err != nil {
		return err
	}
	s.mu.Lock()
	g, srv := s.guard, s.httpSrv
	if s.state == stateRunning {
		s.token = token
	}
	s.tokenPath = tokenPath
	s.mu.Unlock()
	if g != nil {
		g.rotate(token)
		// Idle connections go too; a client's next request opens a new one with the new token.
		srv.SetKeepAlivesEnabled(false)
		srv.SetKeepAlivesEnabled(true)
	}
	s.log.Info("MCP token regenerated; clients need the new one", "tokenFile", tokenPath)
	return nil
}

// PortTaken returns the kept port the last Start found taken, which then listened on another port
// and kept that one; 0 when the kept port was free or none was kept.
func (s *Server) PortTaken() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.portTaken
}

// Token returns the bearer token while the server runs, "" otherwise. It is a credential: the
// window shows it masked and copies it on request; it never goes to a log, an error or a
// notification.
func (s *Server) Token() string {
	_, token := s.endpoint()
	return token
}

// serve runs the HTTP server until Close. A panic is logged rather than ending the app.
func (s *Server) serve(srv *http.Server, ln net.Listener, done chan<- struct{}) {
	defer close(done)
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("MCP server panicked", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.log.Error("MCP server stopped", "err", err)
	}
}

// ServeTransport serves the registry's tools over one MCP transport until the client disconnects
// or ctx ends: the in-process path for tests and for the stdio bridge (architecture §7). It needs
// no Start and uses no token, because whoever holds the transport is already trusted; the
// engine's confirmation rules and audit log apply as over HTTP.
func (s *Server) ServeTransport(ctx context.Context, t sdk.Transport) error {
	s.mu.Lock()
	closed := s.state == stateClosed
	s.mu.Unlock()
	if closed {
		return errors.New("mcp: server closed")
	}
	srv, _ := s.newMCPServer()
	return srv.Run(ctx, t)
}

// URL returns the endpoint URL, "http://127.0.0.1:<port>/mcp", or "" when the server is not
// running.
func (s *Server) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != stateRunning {
		return ""
	}
	return s.url
}

// TokenPath returns the path of the token file: Options.TokenFile, or the default path once Start
// has resolved it.
func (s *Server) TokenPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokenPath
}

// ClaudeAddCommand returns a ready-to-paste shell command that registers this endpoint with
// Claude Code for every directory (user scope), replacing an earlier registration:
//
//	claude mcp remove --scope user cogpit >/dev/null 2>&1; claude mcp add --scope user --transport http cogpit http://127.0.0.1:<port>/mcp --header "Authorization: Bearer <token>"
//
// It returns "" when the server is not running. The command contains the token: print it to the
// user's terminal, never to a log. The port and the token are kept across starts (F6), so the
// registration is made once and repeated only after the user regenerates the token or a start
// found the kept port taken (PortTaken); a client that registers the stdio bridge instead
// (`cogpit-cli mcp`, architecture §7; ClaudeAddStdioCommand, which the app offers when its bundle
// carries cogpit-cli) follows both by itself, since the bridge reads the endpoint and token files.
// The syntax is POSIX shell on macOS and Linux and PowerShell on Windows (terminalShell), where the
// output of the remove goes to *> $null instead.
func (s *Server) ClaudeAddCommand() string {
	url, token := s.endpoint()
	if url == "" {
		return ""
	}
	// --header takes several values, so it comes last, after the name and the URL.
	return fmt.Sprintf(`claude mcp remove --scope user %[1]s %[4]s; `+
		`claude mcp add --scope user --transport http %[1]s %[2]s --header "Authorization: Bearer %[3]s"`,
		serverName, url, token, terminalShell().quiet)
}

// ClientConfigJSON returns an MCP client configuration for this endpoint, for
// `claude -p --strict-mcp-config --mcp-config <file>`; it leaves the user's Claude Code
// configuration untouched:
//
//	{"mcpServers": {"cogpit": {"type": "http", "url": "...", "headers": {"Authorization": "Bearer ..."}}}}
//
// It returns "" when the server is not running. Like ClaudeAddCommand it contains the token, which
// stays valid until the user regenerates it: write it to an owner-only file and never log it.
func (s *Server) ClientConfigJSON() string {
	url, token := s.endpoint()
	if url == "" {
		return ""
	}
	type entry struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	cfg := struct {
		MCPServers map[string]entry `json:"mcpServers"`
	}{MCPServers: map[string]entry{serverName: {
		Type: "http", URL: url, Headers: map[string]string{"Authorization": "Bearer " + token},
	}}}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil { // cannot happen for maps of strings
		return ""
	}
	return string(data)
}

func (s *Server) endpoint() (url, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != stateRunning {
		return "", ""
	}
	return s.url, s.token
}

// life returns the context that ends when the endpoint stops.
func (s *Server) life() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lifetime
}

// Close stops the server for good, ends pending tool calls (a call waiting for a confirmation fails
// and its command does not run) and removes the endpoint file if it still names this server. The
// token and port files stay for the next start (F6). It is safe to call more than once.
func (s *Server) Close() error { return s.halt(true) }

// Stop ends the endpoint as Close does: it stops listening, ends every connection and pending tool
// call, and removes the endpoint file. Unlike Close it leaves the server startable: Start brings the
// endpoint back with the same token and port. Stopping a server that is not running does nothing.
// The user's switch in the status bar is the one caller; no tool can reach it.
func (s *Server) Stop() error { return s.halt(false) }

func (s *Server) halt(final bool) error {
	s.ctl.Lock()
	defer s.ctl.Unlock()
	s.mu.Lock()
	if s.state == stateClosed {
		s.mu.Unlock()
		return nil
	}
	wasRunning := s.state == stateRunning
	srv, served, path, url, stop, g := s.httpSrv, s.served, s.tokenPath, s.url, s.stop, s.guard
	s.token, s.httpSrv, s.url, s.guard = "", nil, "", nil
	switch {
	case final:
		s.state = stateClosed
	case wasRunning:
		s.state = stateNew
		s.lifetime, s.stop = context.WithCancel(context.Background())
	}
	s.mu.Unlock()

	if !wasRunning && !final {
		return nil
	}
	stop()
	if !wasRunning {
		return nil
	}
	g.close()
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	var errs []error
	if err := srv.Shutdown(ctx); err != nil {
		if err := srv.Close(); err != nil {
			errs = append(errs, fmt.Errorf("mcp: close: %w", err))
		}
	}
	<-served
	// The token and the port stay for the next start; the endpoint file says the server listens,
	// and it no longer does.
	if err := removeIfHolds(endpointFile(path), url, "endpoint"); err != nil {
		errs = append(errs, err)
	}
	s.log.Info("MCP server stopped")
	return errors.Join(errs...)
}
