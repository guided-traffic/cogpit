package cluster

import (
	"io"
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
	"unsafe"

	"k8s.io/client-go/pkg/apis/clientauthentication"
	"k8s.io/client-go/plugin/pkg/client/auth/exec"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// A credential plugin's standard error after a failed login (D-66; the owner's answer: "only on
// failure, filtered"). client-go runs the plugin with the process's standard error and offers no
// option to change it: newAuthenticator sets the Authenticator's unexported stderr field to
// os.Stderr (k8s.io/client-go v0.37.1, plugin/pkg/client/auth/exec/exec.go:214), and
// refreshCredsLocked hands it to the command (exec.go:460, cmd.Stderr = a.stderr) while the
// plugin's standard output, the ExecCredential with the token, goes to a buffer of its own
// (exec.go:461) that Cogpit never sees.
//
// Connect therefore asks client-go for the very Authenticator the connection's transport will use
// (exec.GetAuthenticator caches one per exec section and cluster, exec.go:161–168; rest's
// TransportConfig asks for it the same way, rest/transport.go:124–136) and, holding the
// Authenticator's own lock, sets that field to a pluginStderr: it writes on to the process's
// standard error as before and keeps a copy of the newest 64 KiB in memory. Only when a request of
// Connect or of the heartbeat fails because the plugin failed is that copy taken: scrubbed
// (scrubPluginOutput), cut to 4 KiB, and written to the connection log; after a request that
// worked it is dropped. If a later client-go renames the field or its lock, nothing is set and the
// plugin's standard error goes to the process's as before (logged once): the capture's test
// (TestPluginStderrOnFailureIsScrubbed) then fails.

// maxPluginStderr bounds what the log keeps of a failed plugin's standard error.
const maxPluginStderr = 4 << 10

// pluginStderrBuffer bounds the copy held in memory until a request ends.
const pluginStderrBuffer = 64 << 10

// pluginStderr is the standard error of one Authenticator's plugin runs: written on to tee, the
// newest pluginStderrBuffer bytes kept. Safe for concurrent use; nil-safe.
type pluginStderr struct {
	tee io.Writer
	mu  sync.Mutex
	buf []byte
	off int64 // the absolute offset of buf[0]
}

func (p *pluginStderr) Write(b []byte) (int, error) {
	if p.tee != nil {
		_, _ = p.tee.Write(b)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf = append(p.buf, b...)
	if over := len(p.buf) - pluginStderrBuffer; over > 0 {
		p.buf = append(p.buf[:0:0], p.buf[over:]...)
		p.off += int64(over)
	}
	return len(b), nil
}

// mark is where the output written from now on starts.
func (p *pluginStderr) mark() int64 {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.off + int64(len(p.buf))
}

// since returns what was written after mark m and drops the copy.
func (p *pluginStderr) since(m int64) string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	start := max(m-p.off, 0)
	var out string
	if start < int64(len(p.buf)) {
		out = string(p.buf[start:])
	}
	p.off += int64(len(p.buf))
	p.buf = nil
	return out
}

// discard drops the copy: a request worked, so no run's output is kept.
func (p *pluginStderr) discard() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.off += int64(len(p.buf))
	p.buf = nil
}

// stderrSinks are the pluginStderrs set on client-go's Authenticators, which client-go keeps for the
// life of the process; patchMu serialises setting them.
var (
	stderrSinks   = map[*exec.Authenticator]*pluginStderr{}
	patchMu       sync.Mutex
	patchWarnOnce sync.Once
)

// capturePluginStderr sets the standard error of the plugin cfg runs, if any, to a pluginStderr
// (once per Authenticator) and returns it; nil without a plugin or when it cannot be set. It must
// run on the configuration the connection's HTTP client is then built from.
func capturePluginStderr(cfg *rest.Config) *pluginStderr {
	a := authenticatorFor(cfg)
	if a == nil {
		return nil
	}
	patchMu.Lock()
	defer patchMu.Unlock()
	if sink, ok := stderrSinks[a]; ok {
		return sink
	}
	sink := &pluginStderr{tee: os.Stderr}
	if !setAuthenticatorStderr(a, sink) {
		patchWarnOnce.Do(func() {
			slog.Warn("cluster: a credential plugin's standard error cannot be captured with this client-go; it goes to Cogpit's standard error")
		})
		return nil
	}
	stderrSinks[a] = sink
	return sink
}

// authenticatorFor returns the Authenticator client-go's transport built from cfg uses for its
// credential plugin (exec.GetAuthenticator keeps one per exec section and cluster); nil without a
// plugin.
func authenticatorFor(cfg *rest.Config) *exec.Authenticator {
	if cfg == nil || cfg.ExecProvider == nil {
		return nil
	}
	var cl *clientauthentication.Cluster
	if cfg.ExecProvider.ProvideClusterInfo {
		c, err := rest.ConfigToExecCluster(cfg)
		if err != nil {
			return nil
		}
		cl = c
	}
	a, err := exec.GetAuthenticator(cfg.ExecProvider, cl)
	if err != nil {
		return nil
	}
	return a
}

// setAuthenticatorStderr sets a's unexported stderr field to w under a's own lock (client-go reads
// the field under that lock, refreshCredsLocked). It reports false, setting nothing, when the
// fields are not what client-go v0.37.1 has, or a plugin run holds the lock right now.
func setAuthenticatorStderr(a *exec.Authenticator, w io.Writer) bool {
	v := reflect.ValueOf(a).Elem()
	field, lock := v.FieldByName("stderr"), v.FieldByName("mu")
	if !field.IsValid() || field.Type() != reflect.TypeFor[io.Writer]() || !lock.IsValid() || lock.Type() != reflect.TypeFor[sync.Mutex]() {
		return false
	}
	// #nosec G103 -- the address of client-go's own lock in the Authenticator, checked to be a sync.Mutex above.
	mu := (*sync.Mutex)(unsafe.Pointer(lock.UnsafeAddr()))
	if !mu.TryLock() {
		return false
	}
	defer mu.Unlock()
	// #nosec G103 -- the stderr field checked to be an io.Writer above; set to the capturing writer.
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(w))
	return true
}

// PluginError is a request that failed because the credential plugin failed, with the plugin's
// standard error of that request, scrubbed and capped (Stderr; empty when it wrote none or it was
// not captured).
type PluginError struct {
	Err    error
	Stderr string
}

func (e *PluginError) Error() string { return e.Err.Error() }
func (e *PluginError) Unwrap() error { return e.Err }

// withPluginStderr turns a failed request of the plugin into a *PluginError carrying what the
// plugin wrote to its standard error since mark, scrubbed of the known secrets; any other outcome
// drops the copy.
func withPluginStderr(err error, sink *pluginStderr, mark int64, known []string) error {
	if sink == nil {
		return err
	}
	if err == nil || !IsPluginFailure(err) {
		sink.discard()
		return err
	}
	return &PluginError{Err: err, Stderr: scrubPluginOutput(sink.since(mark), known)}
}

// knownLoginSecrets are the values of the user entry that the plugin could print: its environment
// values from the kubeconfig, its arguments' values, a token and a password, at least minSecretLen
// long; the proxy secrets come on top (kubeconfigProxySecrets).
func knownLoginSecrets(raw clientcmdapi.Config, kctx *clientcmdapi.Context) []string {
	if kctx == nil {
		return nil
	}
	ai := raw.AuthInfos[kctx.AuthInfo]
	if ai == nil {
		return kubeconfigProxySecrets(raw)
	}
	var out []string
	add := func(s string) {
		if len(s) >= minSecretLen {
			out = append(out, s)
		}
	}
	add(ai.Token)
	add(ai.Password)
	if ai.Exec != nil {
		for _, e := range ai.Exec.Env {
			add(e.Value)
		}
		for _, a := range ai.Exec.Args {
			if _, v, ok := strings.Cut(a, "="); ok {
				add(v)
			}
		}
	}
	return append(out, kubeconfigProxySecrets(raw)...)
}

// What a plugin's standard error may hold that looks like a credential: a JWT, a PEM block, an
// "Authorization: Bearer" value, a value after a key that names a secret, and a long run of
// token characters.
var (
	jwtShape    = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.?[A-Za-z0-9_-]*`)
	pemBlock    = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]+-----.*?(-----END [A-Z0-9 ]+-----|$)`)
	bearerValue = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{6,}`)
	secretPair  = regexp.MustCompile(`(?i)\b([A-Za-z0-9_-]*(?:token|password|passwd|secret|apikey|api_key|credential|assertion|cookie|session|code|key))(["']?\s*[:=]\s*["']?)([^\s"',;&]{4,})`)
	opaqueRun   = regexp.MustCompile(`[A-Za-z0-9+/_=-]{32,}`)
)

// scrubbed replaces what the scrubber removed.
const scrubbed = "•••"

// scrubPluginOutput makes a plugin's standard error fit for the connection log: URLs lose their
// user information, query and fragment; the known secrets (the user entry's values, the proxy's
// password) are replaced as cluster.scrubSecrets replaces them in errors; whatever has the shape
// of a credential (jwtShape, pemBlock, bearerValue, secretPair, a long run of token characters
// with letters and digits) is replaced; and the text is cut to maxPluginStderr bytes.
func scrubPluginOutput(text string, known []string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	text = replaceSecrets(text, known)
	text = sanitizeURLs(text) // a query string carries codes and tokens: gone with it
	text = pemBlock.ReplaceAllString(text, "-----BEGIN … (a PEM block, removed)")
	text = jwtShape.ReplaceAllString(text, scrubbed)
	text = bearerValue.ReplaceAllString(text, "$1 "+scrubbed)
	text = secretPair.ReplaceAllString(text, "$1$2"+scrubbed)
	text = opaqueRun.ReplaceAllStringFunc(text, func(run string) string {
		if strings.ContainsAny(run, "0123456789") && strings.IndexFunc(run, isLetter) >= 0 {
			return scrubbed
		}
		return run
	})
	text = strings.TrimSpace(text)
	if len(text) > maxPluginStderr {
		cut := maxPluginStderr
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + "\n… (cut at 4 KiB)"
	}
	return text
}

func isLetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }
