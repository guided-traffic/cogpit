package kubeconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ExecPlugin is the exec credential plugin a kubeconfig context's user runs to get a credential
// (users[].user.exec): the program Cogpit shows the user before its first run, and what the user's
// approval of it binds (docs/security/trust-boundaries.md, H-1). It describes the context's login
// whenever that needs the user's approval: the plugin, a token file outside the kubeconfig's folder
// (D-17), or, since A28 (owner), a cluster that skips the check of the server's certificate
// (insecure-skip-tls-verify, H-37), where nothing runs but the credential goes to whoever answers
// at the server's address (Kind LoginInsecure when that is all).
//
// It holds the names of the variables the plugin's environment adds, never their values: those
// enter the fingerprint only. The other fields are as the kubeconfig wrote them, untrusted text;
// show them through DisplayText.
type ExecPlugin struct {
	// File is the kubeconfig file the user entry comes from.
	File string `json:"file"`
	// Context and User are the context's name and its user entry's name in File.
	Context string `json:"context"`
	User    string `json:"user"`
	// Server is the API server the credential is sent to, without user information.
	Server string `json:"server,omitempty"`
	// APIVersion is the ExecCredential version the plugin speaks.
	APIVersion string `json:"apiVersion,omitempty"`
	// Command is the program as client-go runs it: a path with a separator, made absolute
	// against File's directory when relative, or a name looked up on PATH when it runs.
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	// EnvNames are the names of the variables the kubeconfig adds to the plugin's environment.
	EnvNames []string `json:"envNames,omitempty"`
	// InteractiveMode and ProvideClusterInfo are the exec section's own fields.
	InteractiveMode    string `json:"interactiveMode,omitempty"`
	ProvideClusterInfo bool   `json:"provideClusterInfo,omitempty"`
	// TokenFile is the token file the user entry names (users[].user.tokenFile), absolute, as the
	// kubeconfig wrote it. It needs the user's approval only when it lies outside the kubeconfig's
	// folder (Files.TokenInFolder false): the file's content is sent to Server as the login token
	// (D-17, docs/security/trust-boundaries.md H-24). A plugin without Command is such a token file
	// alone.
	TokenFile string `json:"tokenFile,omitempty"`
	// TLS is how the server is reached and trusted: what decides who can read the credential.
	TLS TLSView `json:"tls"`
	// Files holds what the cluster package found out about the files this plugin names; zero until
	// SetFiles (the kubeconfig package does no file I/O).
	Files FileFacts `json:"files"`

	env       []clientcmdapi.ExecEnvVar // values are bound by the fingerprint, never serialized
	serverRaw string                    // the server URL as written, bound by the fingerprint
	trust     serverTrust               // where the credential goes, bound by the fingerprint
}

// TLSView is what the approval dialog shows about how the credential travels (R-11). The proxy URL
// has its user information stripped. The fingerprint binds the full values.
type TLSView struct {
	ProxyURL string `json:"proxyURL,omitempty"`
	// ClusterProxy is the proxy Cogpit itself sets for this context (Cluster Settings › Proxy), which
	// replaces ProxyURL for Cogpit's connection, its password shown as "•••"; empty: none. The engine
	// sets it (SetClusterProxy): the kubeconfig does not know it (A23, D-26).
	ClusterProxy string `json:"clusterProxy,omitempty"`
	// Insecure is insecure-skip-tls-verify: the server's certificate is not checked.
	Insecure   bool   `json:"insecure,omitempty"`
	ServerName string `json:"serverName,omitempty"`
	// CAEmbedded is certificate-authority-data: the kubeconfig brings its own CA. CAFile is
	// certificate-authority, a path.
	CAEmbedded bool   `json:"caEmbedded,omitempty"`
	CAFile     string `json:"caFile,omitempty"`
}

// FileFacts are the file-content facts an approval binds beyond paths, found by the cluster
// package (R-5, D-26, D-17). A program or CA file that resolves to a file inside the kubeconfig's own
// folder (the place a writer of a synced folder can write, H-1) is bound by the SHA-256 of its
// content; one resolved elsewhere (PATH, /usr/local/bin) stays bound by its path, so a tool update
// does not ask again. All fields are empty until SetFiles.
type FileFacts struct {
	// ProgramResolved is the program's path after resolving symlinks, when it differs from Command;
	// bound, so repointing a link asks again.
	ProgramResolved string `json:"programResolved,omitempty"`
	// ProgramSHA256 is the hex SHA-256 of the program's content, when it lies inside the folder.
	// ProgramNote says why a program inside the folder has no digest (too large, not a regular
	// file, unreadable); it is bound as well.
	ProgramSHA256 string `json:"programSHA256,omitempty"`
	ProgramNote   string `json:"programNote,omitempty"`
	// CAFileResolved, CAFileSHA256 and CAFileNote are the same for certificate-authority.
	CAFileResolved string `json:"caFileResolved,omitempty"`
	CAFileSHA256   string `json:"caFileSHA256,omitempty"`
	CAFileNote     string `json:"caFileNote,omitempty"`
	// TokenFileResolved is the token file's path after resolving symlinks, when it differs.
	// TokenInFolder: it resolves to a file inside the folder, which comes from the same source as
	// the kubeconfig and needs no approval.
	TokenFileResolved string `json:"tokenFileResolved,omitempty"`
	TokenInFolder     bool   `json:"tokenInFolder,omitempty"`
}

// serverTrust is what decides which server receives the plugin's credential, beyond its URL.
type serverTrust struct {
	CAData     []byte `json:"caData,omitempty"`
	CAFile     string `json:"caFile,omitempty"`
	Insecure   bool   `json:"insecure,omitempty"`
	ServerName string `json:"serverName,omitempty"`
	ProxyURL   string `json:"proxyURL,omitempty"`
	// ClusterProxy is Cogpit's own proxy for the context as TLSView shows it, the password as
	// "•••" (A23). Omitted when empty, so that an approval given before it was bound still matches
	// for a context without one.
	ClusterProxy string `json:"clusterProxy,omitempty"`
}

// NewExecPlugin describes the exec section ec of user entry user, used by context in file to reach
// cluster (which may be nil). It returns nil when ec is nil.
func NewExecPlugin(file, context, user string, cluster *clientcmdapi.Cluster, ec *clientcmdapi.ExecConfig) *ExecPlugin {
	if ec == nil {
		return nil
	}
	return NewLogin(file, context, user, cluster, ec, "")
}

// NewLogin describes how the user entry user of context in file logs in: the exec section ec
// (which may be nil) and the token file tokenFile (which may be empty), to cluster. It returns nil
// when there is neither and the cluster checks the server's certificate. A token file alone gives
// a plugin without a Command; a cluster with insecure-skip-tls-verify alone gives one without a
// Command or token file (LoginInsecure, A28).
func NewLogin(file, context, user string, cluster *clientcmdapi.Cluster, ec *clientcmdapi.ExecConfig, tokenFile string) *ExecPlugin {
	if ec == nil && tokenFile == "" && (cluster == nil || !cluster.InsecureSkipTLSVerify) {
		return nil
	}
	p := &ExecPlugin{File: file, Context: context, User: user, TokenFile: tokenFile}
	if ec != nil {
		p.APIVersion, p.Command, p.Args = ec.APIVersion, ec.Command, append([]string(nil), ec.Args...)
		p.InteractiveMode, p.ProvideClusterInfo = string(ec.InteractiveMode), ec.ProvideClusterInfo
		p.env = append([]clientcmdapi.ExecEnvVar(nil), ec.Env...)
		for _, e := range ec.Env {
			p.EnvNames = append(p.EnvNames, e.Name)
		}
	}
	if cluster != nil {
		// The fingerprint binds the URL as written, user information included (a changed password
		// in the URL is a changed destination too); only the stripped form is ever shown.
		p.serverRaw = cluster.Server
		p.Server = StripUserInfo(cluster.Server)
		p.trust = serverTrust{
			CAData: append([]byte(nil), cluster.CertificateAuthorityData...), CAFile: cluster.CertificateAuthority,
			Insecure: cluster.InsecureSkipTLSVerify, ServerName: cluster.TLSServerName, ProxyURL: cluster.ProxyURL,
		}
		p.TLS = TLSView{
			ProxyURL: StripUserInfo(cluster.ProxyURL), Insecure: cluster.InsecureSkipTLSVerify,
			ServerName: cluster.TLSServerName, CAEmbedded: len(cluster.CertificateAuthorityData) > 0,
			CAFile: cluster.CertificateAuthority,
		}
	}
	return p
}

// ExecPluginOf returns the login of the context named context in cfg (its exec plugin, its token
// file, and whether its cluster skips the certificate check), or nil when the context is missing
// or the login needs nothing of that. A context whose user entry is missing logs in with nothing,
// which still counts when its cluster skips the check. cfg should come from client-go's loader,
// which records each entry's file (LocationOfOrigin) and resolves relative paths, so that the
// plugin is described as it will run.
func ExecPluginOf(cfg *clientcmdapi.Config, context string) *ExecPlugin {
	if cfg == nil {
		return nil
	}
	kc := cfg.Contexts[context]
	if kc == nil {
		return nil
	}
	ai := cfg.AuthInfos[kc.AuthInfo]
	if ai == nil {
		return NewLogin(kc.LocationOfOrigin, context, kc.AuthInfo, cfg.Clusters[kc.Cluster], nil, "")
	}
	return NewLogin(ai.LocationOfOrigin, context, kc.AuthInfo, cfg.Clusters[kc.Cluster], ai.Exec, ai.TokenFile)
}

// fingerprintDoc is the canonical form the fingerprint hashes; its field order is fixed.
type fingerprintDoc struct {
	Version            int         `json:"v"`
	File               string      `json:"file"`
	Context            string      `json:"context"`
	User               string      `json:"user"`
	Server             string      `json:"server"`
	Trust              serverTrust `json:"trust"`
	APIVersion         string      `json:"apiVersion"`
	Command            string      `json:"command"`
	Args               []string    `json:"args"`
	Env                [][2]string `json:"env"`
	InteractiveMode    string      `json:"interactiveMode"`
	ProvideClusterInfo bool        `json:"provideClusterInfo"`
	// Version 2 (R-5, D-17): the content of files inside the kubeconfig's folder, and the token file.
	Files     FileFacts `json:"files"`
	TokenFile string    `json:"tokenFile"` // only when it needs approval (outside the folder)
}

// Fingerprint returns "sha256:<hex>" over everything that decides what runs and where its
// credential goes: the file, context and user, the server (with any user information in its URL)
// and how it is trusted (CA, insecure, TLS server name, the kubeconfig's proxy and Cogpit's own:
// SetClusterProxy), the API version, the command, the
// arguments in order, the environment's names and values in order, the interactive mode and
// whether cluster information is passed; the token file when it lies outside the folder; and the
// SHA-256 of the program and of the CA file when they lie inside the folder (FileFacts). An approval is bound to it: any change asks the user
// again. The hash keeps the environment's values out of the approval store.
func (p *ExecPlugin) Fingerprint() string {
	if p == nil {
		return ""
	}
	doc := fingerprintDoc{
		Version: 2, File: p.File, Context: p.Context, User: p.User, Server: p.serverRaw, Trust: p.trust,
		APIVersion: p.APIVersion, Command: p.Command, Args: p.Args,
		InteractiveMode: p.InteractiveMode, ProvideClusterInfo: p.ProvideClusterInfo, Files: p.Files,
	}
	if p.AsksForTokenFile() {
		doc.TokenFile = p.TokenFile
	}
	doc.Files.TokenInFolder = false // not bound itself: a moved token file changes TokenFile above
	if doc.Args == nil {
		doc.Args = []string{}
	}
	doc.Env = make([][2]string, 0, len(p.env))
	for _, e := range p.env {
		doc.Env = append(doc.Env, [2]string{e.Name, e.Value})
	}
	data, err := json.Marshal(doc)
	if err != nil { // cannot happen for these types; an empty fingerprint never matches an approval
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// identityDoc is the canonical form Identity hashes: what a login does, without the context, user,
// server and trust that carry it.
type identityDoc struct {
	Version            int         `json:"v"`
	APIVersion         string      `json:"apiVersion"`
	Command            string      `json:"command"`
	Args               []string    `json:"args"`
	Env                [][2]string `json:"env"`
	InteractiveMode    string      `json:"interactiveMode"`
	ProvideClusterInfo bool        `json:"provideClusterInfo"`
	ProgramResolved    string      `json:"programResolved"`
	ProgramSHA256      string      `json:"programSHA256"`
	ProgramNote        string      `json:"programNote"`
	TokenFile          string      `json:"tokenFile"`
	TokenFileResolved  string      `json:"tokenFileResolved"`
	// Alone is the file and context of a LoginInsecure login: such a context is asked about on
	// its own, never with the file's other contexts (A28: once per context).
	Alone []string `json:"alone,omitempty"`
}

// Identity returns "login:<hex>" over what the login does and nothing about where it is used: the
// API version, command, arguments in order, environment names and values in order, interactive
// mode, provideClusterInfo, how the program is bound (its resolved path and, inside the folder, the
// SHA-256 of its content or the note saying why there is none: D-26), and the token file when it
// needs approval. Two contexts of one file whose logins have the same identity run the same
// program the same way (or read the same token file), so the approval dialog asks about them once
// (D-28). It is no replacement for Fingerprint: the approval stays bound per context, to the server
// and how it is trusted as well. A login that only skips the certificate check (LoginInsecure) runs
// nothing to share: its identity is its own context's, so it is asked about on its own (A28). An
// empty identity (nil or no login that needs approval) groups with nothing.
func (p *ExecPlugin) Identity() string {
	if !p.NeedsApproval() {
		return ""
	}
	doc := identityDoc{
		Version: 1, APIVersion: p.APIVersion, Command: p.Command, Args: p.Args, InteractiveMode: p.InteractiveMode,
		ProvideClusterInfo: p.ProvideClusterInfo, ProgramResolved: p.Files.ProgramResolved,
		ProgramSHA256: p.Files.ProgramSHA256, ProgramNote: p.Files.ProgramNote,
	}
	if p.AsksForTokenFile() {
		doc.TokenFile, doc.TokenFileResolved = p.TokenFile, p.Files.TokenFileResolved
	}
	if p.Kind() == LoginInsecure {
		doc.Alone = []string{p.File, p.Context}
	}
	if doc.Args == nil {
		doc.Args = []string{}
	}
	doc.Env = make([][2]string, 0, len(p.env))
	for _, e := range p.env {
		doc.Env = append(doc.Env, [2]string{e.Name, e.Value})
	}
	data, err := json.Marshal(doc)
	if err != nil { // cannot happen for these types
		return ""
	}
	sum := sha256.Sum256(data)
	return "login:" + hex.EncodeToString(sum[:])
}

// AsksForTokenFile reports whether the plugin names a token file that needs the user's approval:
// one outside the kubeconfig's folder.
func (p *ExecPlugin) AsksForTokenFile() bool {
	return p != nil && p.TokenFile != "" && !p.Files.TokenInFolder
}

// NeedsApproval reports whether the login needs the user's approval at all: it runs a program,
// reads a token file outside the kubeconfig's folder, or goes to a server whose certificate is not
// checked (insecure-skip-tls-verify, A28).
func (p *ExecPlugin) NeedsApproval() bool {
	return p != nil && (p.Command != "" || p.AsksForTokenFile() || p.TLS.Insecure)
}

// Login kinds (ExecPlugin.Kind).
const (
	LoginProgram   = "program"   // an exec credential plugin
	LoginTokenFile = "tokenFile" // a token file outside the kubeconfig's folder
	LoginBoth      = "both"
	// LoginInsecure is a login that needs the approval only because its cluster skips the check of
	// the server's certificate (A28). A program or token file that needs it too gives their kind:
	// their approval, whose dialog warns about the check, covers it.
	LoginInsecure = "insecure"
)

// Kind says what needs the user's approval: LoginProgram, LoginTokenFile, LoginBoth or
// LoginInsecure; empty when nothing does.
func (p *ExecPlugin) Kind() string {
	switch {
	case p == nil:
		return ""
	case p.Command != "" && p.AsksForTokenFile():
		return LoginBoth
	case p.Command != "":
		return LoginProgram
	case p.AsksForTokenFile():
		return LoginTokenFile
	case p.TLS.Insecure:
		return LoginInsecure
	}
	return ""
}

// CAFilePath returns the CA file the cluster names (certificate-authority), or "".
func (p *ExecPlugin) CAFilePath() string {
	if p == nil {
		return ""
	}
	return p.trust.CAFile
}

// SetClusterProxy records the proxy Cogpit itself sets for the login's context (Cluster Settings ›
// Proxy, which replaces the kubeconfig's proxy-url for Cogpit's connection) as it may be shown,
// the password replaced by "•••"; "" for none. The fingerprint binds it as shown: a changed scheme,
// user, host or port asks again, a changed password alone does not, and the approvals file never
// holds a hash over the password (A23, D-26; the lead's decision for the owner to confirm).
func (p *ExecPlugin) SetClusterProxy(shown string) {
	if p != nil {
		p.TLS.ClusterProxy, p.trust.ClusterProxy = shown, shown
	}
}

// SetFiles records what the cluster package found out about the files the plugin names.
func (p *ExecPlugin) SetFiles(f FileFacts) {
	if p != nil {
		p.Files = f
	}
}

// String names the plugin without its arguments or environment: for logs and errors.
func (p *ExecPlugin) String() string {
	if p == nil {
		return "<no exec plugin>"
	}
	if p.Kind() == LoginInsecure {
		return fmt.Sprintf("unchecked connection of context %s to %s in %s", DisplayText(p.Context, 200), DisplayText(p.Server, 200), DisplayText(p.File, 400))
	}
	return fmt.Sprintf("exec plugin %s of user %s in %s", DisplayText(p.Command, 200), DisplayText(p.User, 200), DisplayText(p.File, 400))
}

// RiskyEnv returns the names among the plugin's environment that change which program runs or
// what it loads (PATH, the dynamic loader's variables), sorted: the approval dialog points them out.
func (p *ExecPlugin) RiskyEnv() []string {
	if p == nil {
		return nil
	}
	var out []string
	for _, n := range p.EnvNames {
		u := strings.ToUpper(n)
		if u == "PATH" || strings.HasPrefix(u, "LD_") || strings.HasPrefix(u, "DYLD_") {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// DisplayText returns untrusted text from a kubeconfig made safe to show as one line: control and
// format characters (bidirectional overrides among them, which can make one path look like
// another) and invalid UTF-8 become visible escapes, and text longer than maxBytes is cut with an
// ellipsis. maxBytes ≤ 0 means no limit.
func DisplayText(s string, maxBytes int) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		var unit string // one character or its escape: the text is cut between units only
		switch {
		case r == utf8.RuneError && size <= 1:
			unit = fmt.Sprintf(`\x%02x`, s[i])
		case !unicode.IsGraphic(r) || unicode.Is(unicode.Cf, r):
			unit = fmt.Sprintf(`\u%04x`, r)
		default:
			unit = s[i : i+size]
		}
		if maxBytes > 0 && b.Len()+len(unit) > maxBytes {
			return b.String() + "…"
		}
		b.WriteString(unit)
		i += size
	}
	return b.String()
}
