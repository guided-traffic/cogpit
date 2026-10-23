package commands

import (
	"context"
	"errors"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// ConnectResult is cluster.connect's result.
type ConnectResult struct {
	Context string                 `json:"context"`
	State   engine.ConnectionState `json:"state"`
	Error   string                 `json:"error,omitempty"`
	// TLSUnchecked: the connection does not check the API server's certificate
	// (engine.Connection.TLSUnchecked, A29).
	TLSUnchecked bool `json:"tlsUnchecked,omitempty"`
}

// connectResult is the result of a connect from the UI state after it.
func connectResult(u engine.UIState) ConnectResult {
	return ConnectResult{Context: u.Context, State: u.Connection.State, Error: u.Connection.Error, TLSUnchecked: u.Connection.TLSUnchecked}
}

// connectAnswer is the answer of a command that connected contextID (cluster.connect,
// hotbar.openCluster) once Connect returned err. A connect that goes on in the background because
// another cluster was shown while it waited is no failure: {context, state: connecting} without an
// error (D-97; the owner's AW16 of 2026-10-22, every connecting command alike), its end in
// ui_state's connections and the connection log. A failed connect is its UI state, the error in it.
// A connect refused before it began (no connector, closed), cancelled by Disconnect
// (engine.ErrConnectCancelled, the window's Cancel Connecting: AW15) or superseded before it began
// (engine.ErrConnectSuperseded) is the error; the window raises no toast for the last two
// (engine.ConnectNotFailed).
func connectAnswer(s *engine.Session, contextID string, err error) (any, error) {
	if errors.Is(err, engine.ErrConnectInBackground) {
		return ConnectResult{Context: contextID, State: engine.Connecting}, nil
	}
	u := s.UIState()
	if err != nil && u.Connection.State != engine.ConnectError {
		return nil, err
	}
	return connectResult(u), nil
}

type connectParams struct {
	Context string `json:"context" jsonschema:"context id as cluster_contexts lists it: <context> [<file>] (a bare context name works when one file only defines it)"`
}

// ConnectLogResult is cluster.connectLog's result.
type ConnectLogResult struct {
	Context string `json:"context"`
	// Lines are this session's lines since its latest connect, as before attempts (D-66).
	Lines []engine.ConnectLogLine `json:"lines"`
	// Attempts are the context's attempts newest first, from its file (7 days, 1 MiB) and this
	// session.
	Attempts []engine.ConnectAttempt `json:"attempts"`
	// FileError says the context's file could not be read; Attempts then holds this session's.
	FileError string `json:"fileError,omitempty"`
}

type connectLogParams struct {
	Context string `json:"context" jsonschema:"context id as cluster_contexts lists it"`
	Limit   int    `json:"limit,omitempty" jsonschema:"the newest attempts to return; 0 or absent: every attempt kept (at most 7 days and 1 MiB per context)"`
}

func registerCluster(g *registrar) {
	add(g, engine.Spec[struct{}]{
		ID: "cluster.contexts", Label: "List Contexts", Danger: engine.DangerRead,
		Description: "Lists the kubeconfig contexts Cogpit can connect to (name, cluster, user, default namespace, server, " +
			"whether it is the kubeconfig's current context). Every context comes " +
			"from a kubeconfig file read on its own, never merged with another (kubeconfig_sources lists the files: ~/.kube, " +
			"the files $KUBECONFIG names, and those the user added). It is named \"<context> [<file>]\", so a name that two " +
			"files share gives two contexts, and carries source, " +
			"kubeContext, file and exec (approved, required or changed: whether their credential plugin may run, or, with " +
			"execKind insecure, whether a context whose cluster skips the certificate check may connect; the user " +
			"allows it in Cogpit's window), and insecure for such a context. clientCert is the validity of the client certificate " +
			"a context's user logs in with (client-certificate-data or a client-certificate file; none for a credential plugin): " +
			"notBefore, notAfter and state at the time of the listing (valid; expiring within 14 days or the last fifth of its " +
			"lifetime; expired; notYetValid); it never blocks a connect. Reads only the kubeconfig files and those certificates; " +
			"no credential is returned.",
		Run: func(_ context.Context, s *engine.Session, _ struct{}) (any, error) {
			list, err := s.Contexts()
			if err != nil && len(list) == 0 {
				return nil, err
			}
			if err != nil {
				s.Logger().Warn("commands: contexts listed partially", "err", err)
			}
			if list == nil {
				list = []engine.ContextInfo{}
			}
			return list, nil
		},
	})

	add(g, engine.Spec[connectLogParams]{
		ID: "cluster.connectLog", Label: "Show Connection Updates", Danger: engine.DangerRead,
		Description: "Returns a context's connection log. attempts: every connect, reconnect, login, disconnect and " +
			"interruption kept (7 days, at most 1 MiB per context, also from earlier runs), newest first, each with its kind, " +
			"start, end, duration (tookMs), outcome (ok, failed, abandoned, cancelled, interrupted for an interruption, whose " +
			"reconnect is the next attempt; empty and running:true while it runs, empty otherwise when Cogpit stopped first) and lines: a time, a level (INFO, WARN or ERROR), a text, the step it documents (kubeconfig, " +
			"server, login, tls, proxy, answer, discovery, cache, access, heartbeat, error, plugin) and its duration; a " +
			"connect's stages too (stages: kubeconfig, login for a credential plugin, dns, tcp, tls, api, each with its result " +
			"ok, failed, notReached, skipped or cancelled, its text and tookMs: the names looked up and their addresses, the " +
			"address and port dialled, or the proxy's, the TLS handshake and the API server's answer). A failed " +
			"step is followed by its error picture (class, HTTP status, the presented certificate's public facts, the error " +
			"chain) and, for a credential plugin that failed, its standard error, scrubbed and cut to 4 KiB. lines: this " +
			"session's lines since its latest connect. The contexts cluster_contexts lists with connectProblem set have an " +
			"error at the end. No line contains a credential. A read: nothing is asked of the cluster.",
		Run: func(_ context.Context, s *engine.Session, p connectLogParams) (any, error) {
			attempts, err := s.ConnectAttempts(p.Context, max(p.Limit, 0))
			res := ConnectLogResult{Context: p.Context, Lines: s.ConnectLog(p.Context), Attempts: attempts}
			if res.Attempts == nil {
				res.Attempts = []engine.ConnectAttempt{}
			}
			if err != nil {
				res.FileError = err.Error()
			}
			return res, nil
		},
	})

	add(g, engine.Spec[connectParams]{
		ID: "cluster.connect", Label: "Connect to Cluster", MenuPath: "Cluster/Connect to Cluster", Shortcut: "Cmd+P", Danger: engine.DangerUI,
		Description: "Connects to a context as cluster_contexts lists it and shows it in the window: its most recently used " +
			"tab, or else its Pods view in its preview tab; the other clusters' tabs stay. Several clusters stay connected at " +
			"once (ui_state's connections): the cluster shown before stays connected, and a context that is connected already " +
			"is only shown, from Cogpit's cache, without a request; one the window waits for already is waited for, its end the " +
			"answer. Connecting only reads from the cluster. " +
			"A context whose credential plugin, or whose connection without the certificate check (insecure-skip-tls-verify), " +
			"the user has not allowed in Cogpit's window ends in state error saying so: only the user allows it there. " +
			"Returns {context, state, error, tlsUnchecked}; state is connected or error, or connecting when another cluster " +
			"was shown while it waited (a login in a browser, say): the connect goes on in the background, its entry in " +
			"ui_state's connections, cluster_disconnect with its context cancels it, and the window keeps showing the cluster " +
			"chosen last. A failed connect leaves the window on its cluster with its error page: error is its cause in a " +
			"sentence (the whole chain is in cluster_connectLog), ui_state's connectFailure has the connect's stages. " +
			"A connect cancelled while it waits (cluster_disconnect, the window's Cancel Connecting) answers a cancelled " +
			"error, which is no failure. tlsUnchecked is set when the " +
			"connection does not check the server's certificate (ui_state's connection says the same while it stands).",
		Run: func(ctx context.Context, s *engine.Session, p connectParams) (any, error) {
			return connectAnswer(s, p.Context, s.Connect(ctx, p.Context))
		},
	})
}
