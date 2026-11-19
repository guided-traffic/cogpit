package engine

// What Cogpit does for a context whose kubeconfig switches the check of the API server's
// certificate off (insecure-skip-tls-verify, H-37 in docs/security/trust-boundaries.md): client-go
// connects without checking, as kubectl does, and sends the context's credential to whoever answers
// at the server's address. Lens's documentation says nothing about such contexts.
//
//   - The window marks the context (ContextInfo.Insecure, from the kubeconfig; Cogpit's default).
//   - A source context connects only once the user allowed it in the window, asked once per
//     context before its first connect and kept like a credential plugin's approval, in the same
//     store and bound the same way: the file, context, user, server and how it is reached (the
//     kubeconfig's proxy-url and Cogpit's own proxy), so a changed server or proxy asks again (the
//     owner's A28; kubeconfig.LoginInsecure, KubeconfigSources.Policy). A context that runs a
//     credential plugin is asked in that plugin's dialog, which warns about the check. Agents, the
//     CLI and links are refused until then, as for a plugin.
//   - The connect log gets a WARN line once the connection stands, from the connection's own
//     configuration, so it is what client-go actually does.
//   - While such a connection stands, the UI state says so (Connection.TLSUnchecked, from the same
//     configuration): the status bar shows "TLS not checked" in the warning colour beside the
//     context, agents read it in ui_state and in cluster_connect's result, and `cogpit-cli get`
//     writes InsecureWarning to standard error (the owner's A29).

// TLSUncheckedLabel is the status bar's item for a connection that does not check the server's
// certificate (the owner's words, A29).
const TLSUncheckedLabel = "TLS not checked"

// tlsUnchecked reports whether b's connection does not check the server's certificate.
func tlsUnchecked(b *Backend) bool {
	return b != nil && b.Conn != nil && b.Conn.Insecure()
}

// InsecureWarning is the warning for a connection to server that does not check the server's
// certificate: the connect log's line, and the one `cogpit-cli get` writes to standard error.
func InsecureWarning(server string) string {
	return "Certificate checks are off for this context (insecure-skip-tls-verify in its kubeconfig): " +
		"Cogpit cannot tell the cluster at " + server + " from anyone else who answers there, " +
		"and sends the context's credential to whoever does"
}

// insecureLogLine is the connect log's warning for a connection that does not check the server's
// certificate; "" for any other.
func insecureLogLine(b *Backend) string {
	if !tlsUnchecked(b) {
		return ""
	}
	return InsecureWarning(b.Conn.Server())
}
