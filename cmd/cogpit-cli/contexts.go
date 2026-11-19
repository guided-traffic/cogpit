package main

import (
	"context"
	"fmt"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

const contextsAbout = `Lists the contexts Cogpit offers, as the app's navigator and the MCP tool cluster_contexts list
them: every context of ~/.kube, of the files --kubeconfig (else $KUBECONFIG) names, and of the
files and sync folders added in Cogpit, each file read on its own and named "<context> [<file>]".
LOGIN says whether a context logs in through a credential plugin or a token file outside its
folder, and whether the user allowed it in Cogpit's window: approved, required or changed.
CERTIFICATE says "not checked" for a context whose cluster sets insecure-skip-tls-verify: whoever
answers at its server's address gets its credential; Cogpit connects to it only once the user
allowed that in Cogpit's window, and the column says whether ("not checked, required").
CLIENT CERT says how long the client certificate a context's user logs in with is valid
("expires in 23 days", "expired 3 days ago"); it never keeps the app from connecting. Listing
reads the kubeconfig files and those certificates only; it runs no plugin and prints no
credential.`

func runContexts(ctx context.Context, args []string, e *env) int {
	fs := newFlags("contexts", "[flags]", contextsAbout)
	var c common
	c.register(fs)
	output := fs.String("o", outTable, "output format: table, json or yaml (json and yaml are cluster_contexts' result)")
	fs.StringVar(output, "output", outTable, "same as -o")
	if code, done := fs.parse(args, e); done {
		return code
	}
	if len(fs.positional) > 0 {
		return fs.usageError(e, "contexts takes no arguments")
	}
	if !validOutput(*output) {
		return fs.usageError(e, fmt.Sprintf("unknown output format %q: want table, json or yaml", *output))
	}
	c.apply(e)

	s, closeSession, err := e.newSession(sessionOptions{kubeconfig: c.kubeconfig})
	if err != nil {
		return fail(e, err)
	}
	defer closeSession()
	var list []engine.ContextInfo
	data, err := dispatchInto(ctx, s, "cluster.contexts", nil, &list)
	if err != nil {
		return fail(e, err)
	}
	if *output != outTable {
		if err := writeResult(e.stdout, *output, data); err != nil {
			return fail(e, err)
		}
		return exitOK
	}
	rows := make([][]string, 0, len(list))
	now := time.Now()
	for _, c := range list {
		server := c.Server
		if c.Demo {
			server = "(synthetic demo cluster)"
		}
		rows = append(rows, []string{c.Name, c.Namespace, server, login(c), certificate(c), clientCert(c, now)})
	}
	if err := writeTable(e.stdout, []string{"CONTEXT", "NAMESPACE", "SERVER", "LOGIN", "CERTIFICATE", "CLIENT CERT"}, rows); err != nil {
		return fail(e, err)
	}
	return exitOK
}

// login describes how a context logs in when the user must allow it: "plugin approved",
// "token file required", "plugin and token file changed"; empty otherwise, and for a context that
// waits only for its unchecked certificate (CERTIFICATE says so).
func login(c engine.ContextInfo) string {
	if c.Exec == "" || c.ExecKind == kubeconfig.LoginInsecure {
		return ""
	}
	what := "plugin"
	switch c.ExecKind {
	case kubeconfig.LoginTokenFile:
		what = "token file"
	case kubeconfig.LoginBoth:
		what = "plugin and token file"
	}
	return what + " " + c.Exec
}

// certificate is "not checked" for a context whose cluster skips the check of the server's
// certificate (insecure-skip-tls-verify), as the app's welcome page marks it ("certificate not checked",
// H-37), followed for a kubeconfig source's context by whether the user allowed connecting to it so
// ("not checked, required"; A28: approved, required or changed, the context's approval, a plugin's
// included); empty otherwise. After the columns it found in place, so that they keep their places
// (Cogpit's default, not the owner's decision); CLIENT CERT follows it.
func certificate(c engine.ContextInfo) string {
	switch {
	case !c.Insecure:
		return ""
	case c.Exec != "":
		return "not checked, " + c.Exec
	}
	return "not checked"
}

// clientCert says how long a context's client certificate is valid (D-75): "expires in 23 days",
// "expired 3 days ago", "not valid yet"; empty for a context without one. The last column, after
// the server's CERTIFICATE (Cogpit's default).
func clientCert(c engine.ContextInfo, now time.Time) string {
	if c.ClientCert == nil {
		return ""
	}
	return c.ClientCert.Describe(now)
}
