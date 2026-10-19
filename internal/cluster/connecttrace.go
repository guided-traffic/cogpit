package cluster

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// The steps of a connect for the connection log (D-66, the owner's decision of 2026-10-16): Connect
// reports what it does through Options.Trace, one ConnectStep each, from the configuration it
// actually builds the client from: the kubeconfig file and context, the API server's URL, the
// login (a client certificate's subject and expiry, "bearer token" without any of it, a credential
// plugin's program name and whether the user allowed it), the proxy, how the server's certificate
// is checked, and the first answer (GET /version) with its time. A failed first answer adds the
// error picture (ErrorPicture): the class of the failure, the HTTP status and the API server's
// message, a presented certificate's public facts, the error chain, and, when the credential plugin
// failed, its standard error, scrubbed and capped (pluginstderr.go).
//
// A step's text never holds a credential: it names fields and methods, never a token, a password
// or key material, a URL is shown without user information, query or fragment (logURL), and a
// proxy's password is •••. The engine escapes and caps every line again.

// Connect's steps (ConnectStep.Name).
const (
	StepKubeconfig = "kubeconfig" // the file and context
	StepServer     = "server"     // the API server's URL
	StepLogin      = "login"      // how the user logs in
	StepProxy      = "proxy"      // how requests reach the server
	StepTLS        = "tls"        // how the server's certificate is checked
	StepAnswer     = "answer"     // the first answer: GET /version
	StepError      = "error"      // a line of the error picture of a failed step
	StepPlugin     = "plugin"     // a line of the credential plugin's standard error, after a failed login
)

// ConnectStep is one step of Connect for the connection log.
type ConnectStep struct {
	Name   string        // one of the Step constants
	Text   string        // what was done or found; never a credential
	Took   time.Duration // how long the step took; zero when it is not timed
	Failed bool          // the step failed: an error line
	Warn   bool          // a fact that likely fails the connect: a warning line (an out-of-date client certificate, D-75)
}

// tracer reports Connect's steps; a nil tracer reports nothing.
type tracer func(ConnectStep)

func (t tracer) step(name, text string) {
	if t != nil && text != "" {
		t(ConnectStep{Name: name, Text: text})
	}
}

func (t tracer) timed(name, text string, took time.Duration, failed bool) {
	if t != nil {
		t(ConnectStep{Name: name, Text: text, Took: took, Failed: failed})
	}
}

// failure reports the error picture of err, then the plugin's standard error, each line a failed
// step.
func (t tracer) failure(err error, stderr string) {
	if t == nil {
		return
	}
	for _, line := range ErrorPicture(err) {
		t(ConnectStep{Name: StepError, Text: line, Failed: true})
	}
	if stderr == "" {
		return
	}
	t(ConnectStep{Name: StepPlugin, Text: "The credential plugin's standard error (scrubbed, at most 4 KiB):", Failed: true})
	for _, line := range strings.Split(stderr, "\n") {
		if strings.TrimSpace(line) != "" {
			t(ConnectStep{Name: StepPlugin, Text: "  " + line, Failed: true})
		}
	}
}

// traceSetup reports the configuration Connect built: the login, the server, the certificate
// check and the proxy. policy says whether a policy allowed a credential plugin (D-26); clusterProxy
// whether Cogpit's own proxy for the cluster is set (the engine reports that one itself).
func (t tracer) traceSetup(cfg *rest.Config, raw clientcmdapi.Config, kctx *clientcmdapi.Context, policy, clusterProxy bool) {
	if t == nil || cfg == nil {
		return
	}
	t.step(StepServer, "API server: "+logURL(cfg.Host))
	now := time.Now()
	if text := describeLogin(cfg, policy, now); text != "" {
		t(ConnectStep{Name: StepLogin, Text: text, Warn: clientCertOutOfDate(cfg, now)})
	}
	t.step(StepTLS, describeTLS(cfg))
	if !clusterProxy {
		var kubeProxy string
		if kctx != nil {
			if cl := raw.Clusters[kctx.Cluster]; cl != nil {
				kubeProxy = cl.ProxyURL
			}
		}
		t.step(StepProxy, describeProxy(cfg.Host, kubeProxy))
	}
}

// describeKubeconfig is the kubeconfig step: the context, its cluster and user entries, and the
// file it comes from.
func describeKubeconfig(name string, kctx *clientcmdapi.Context, usable []string) string {
	file := kctx.LocationOfOrigin
	if file == "" {
		file = describePaths(usable)
	}
	return fmt.Sprintf("Kubeconfig: context %q (cluster %q, user %q) in %s", name, kctx.Cluster, kctx.AuthInfo, file)
}

// describeLogin says how the client logs in, never with what: a client certificate's subject and
// expiry are public facts, a token or password is named, never shown.
func describeLogin(cfg *rest.Config, policy bool, now time.Time) string {
	var text string
	switch {
	case cfg.ExecProvider != nil:
		approval := "allowed by you (D-26)"
		if !policy {
			approval = "runs without an approval, as kubectl runs it"
		}
		text = fmt.Sprintf("Login: credential plugin %s (%s)", filepath.Base(cfg.ExecProvider.Command), approval)
		if cfg.ExecProvider.InteractiveMode != "" {
			text += ", interactive mode " + string(cfg.ExecProvider.InteractiveMode)
		}
	case len(cfg.CertData) > 0 || cfg.CertFile != "":
		text = "Login: client certificate " + clientCertSummary(cfg, now)
	case cfg.BearerToken != "":
		text = "Login: bearer token from the kubeconfig (not shown)"
	case cfg.BearerTokenFile != "":
		text = "Login: bearer token from the file " + cfg.BearerTokenFile + " (not shown)"
	case cfg.Username != "":
		text = "Login: user name and password (basic authentication; the password is not shown)"
	case cfg.AuthProvider != nil:
		text = "Login: auth provider " + cfg.AuthProvider.Name + " (its refreshed tokens stay in memory, H-4)"
	default:
		text = "Login: none (anonymous requests)"
	}
	if cfg.Impersonate.UserName != "" {
		text += "; impersonates " + cfg.Impersonate.UserName
	}
	return text
}

// clientCertSummary is a client certificate's subject and validity, from the kubeconfig's
// client-certificate-data or client-certificate file (never the key).
func clientCertSummary(cfg *rest.Config, now time.Time) string {
	data := cfg.CertData
	if len(data) == 0 {
		read, err := readBounded(cfg.CertFile)
		if err != nil {
			return "from the file " + cfg.CertFile + " (cannot be read: " + kubeconfig.SafeError(err).Error() + ")"
		}
		data = read
	}
	cert := firstCertificate(data)
	if cert == nil {
		return "(not a PEM certificate)"
	}
	return certFacts(cert, now)
}

// clientCertOutOfDate reports whether the kubeconfig's client certificate is expired or not valid
// yet: the Login line then is a warning, since the API server will refuse it (D-75; the lead's
// answer of 2026-10-17 to agent G's open question: the line names the dates already).
func clientCertOutOfDate(cfg *rest.Config, now time.Time) bool {
	if cfg.ExecProvider != nil || (len(cfg.CertData) == 0 && cfg.CertFile == "") {
		return false
	}
	data := cfg.CertData
	if len(data) == 0 {
		read, err := readBounded(cfg.CertFile)
		if err != nil {
			return false
		}
		data = read
	}
	cert := firstCertificate(data)
	return cert != nil && (now.After(cert.NotAfter) || now.Before(cert.NotBefore))
}

// firstCertificate is the first PEM CERTIFICATE block of data, parsed; nil when there is none.
func firstCertificate(data []byte) *x509.Certificate {
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return nil
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil
		}
		return cert
	}
}

// certFacts is a certificate's subject common name and validity: "CN=alice, valid until
// 2027-01-02 (expired 3 days ago)".
func certFacts(cert *x509.Certificate, now time.Time) string {
	text := "CN=" + cert.Subject.CommonName + ", valid until " + cert.NotAfter.UTC().Format(time.DateOnly)
	switch {
	case now.After(cert.NotAfter):
		text += " (expired " + roundAgo(now.Sub(cert.NotAfter)) + " ago)"
	case now.Before(cert.NotBefore):
		text += " (not valid before " + cert.NotBefore.UTC().Format(time.DateTime) + " UTC)"
	}
	return text
}

// roundAgo writes a duration in its largest unit: "3 days", "5 h", "12 min".
func roundAgo(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%d h", int(d/time.Hour))
	default:
		return fmt.Sprintf("%d min", int(d/time.Minute))
	}
}

// describeTLS says how the server's certificate is checked (A29).
func describeTLS(cfg *rest.Config) string {
	if u, err := url.Parse(cfg.Host); err == nil && u.Scheme == "http" {
		return "TLS: none, the server URL is plain http"
	}
	if cfg.Insecure {
		return "TLS: the server's certificate is NOT checked (insecure-skip-tls-verify)"
	}
	against := "this computer's trusted certificate authorities"
	switch {
	case len(cfg.CAData) > 0:
		against = "the kubeconfig's certificate-authority-data"
		if ca := firstCertificate(cfg.CAData); ca != nil {
			against += " (" + certFacts(ca, time.Now()) + ")"
		}
	case cfg.CAFile != "":
		against = "the certificate authority file " + cfg.CAFile
	}
	text := "TLS: the server's certificate is checked against " + against
	if cfg.ServerName != "" {
		text += ", for the name " + cfg.ServerName + " (tls-server-name)"
	}
	return text
}

// describeProxy says how requests reach the server when Cogpit sets no proxy of its own: the
// kubeconfig's proxy-url, else the environment's proxy for the server, else directly.
func describeProxy(host, kubeProxy string) string {
	if kubeProxy != "" {
		return "Proxy: " + redactedProxy(kubeProxy) + " (the kubeconfig's proxy-url)"
	}
	if req, err := http.NewRequest(http.MethodGet, host, nil); err == nil {
		if u, err := http.ProxyFromEnvironment(req); err == nil && u != nil {
			return "Proxy: " + redactedProxy(u.String()) + " (from the environment: HTTPS_PROXY, HTTP_PROXY, NO_PROXY)"
		}
	}
	return "Proxy: none, a direct connection"
}

// redactedProxy is a proxy URL with its password as •••, the user kept, without path, query or
// fragment; a value that does not parse is not shown.
func redactedProxy(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "(a value that is not a URL; not shown)"
	}
	user := ""
	if u.User != nil {
		user = url.User(u.User.Username()).String()
		if _, has := u.User.Password(); has {
			user += ":•••"
		}
		user += "@"
	}
	return u.Scheme + "://" + user + u.Host
}

// logURL is a URL as the log shows it: without user information, query and fragment.
func logURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return kubeconfig.StripUserInfo(raw)
	}
	u.User, u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = nil, "", false, "", ""
	return u.String()
}

// urlInText finds the URLs in an error's text.
var urlInText = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"'<>]+`)

// sanitizeURLs replaces every URL in text by logURL's form of it.
func sanitizeURLs(text string) string {
	return urlInText.ReplaceAllStringFunc(text, func(raw string) string {
		trail := ""
		for strings.HasSuffix(raw, ",") || strings.HasSuffix(raw, ".") || strings.HasSuffix(raw, ")") || strings.HasSuffix(raw, ";") {
			trail = raw[len(raw)-1:] + trail
			raw = raw[:len(raw)-1]
		}
		return logURL(raw) + trail
	})
}

// ErrorPicture describes a failed request for the connection log, one line each: what kind of
// failure it is (DNS, TCP, TLS with the presented certificate's public facts, the credential
// plugin, a timeout, an HTTP status with the API server's message), the chain of error types, and
// the innermost message. No line holds a credential: texts pass kubeconfig.SafeError and lose the
// user information and query of every URL.
func ErrorPicture(err error) []string {
	if err == nil {
		return nil
	}
	out := []string{"Class: " + errorClass(err)}
	var status apierrors.APIStatus
	if errors.As(err, &status) {
		st := status.Status()
		line := fmt.Sprintf("HTTP status: %d %s", st.Code, http.StatusText(int(st.Code)))
		if st.Reason != "" {
			line += ", reason " + string(st.Reason)
		}
		if st.Message != "" {
			line += "; the API server says: " + safeText(st.Message)
		}
		out = append(out, line)
	}
	if cert := presentedCertificate(err); cert != nil {
		out = append(out, "Certificate presented: "+certificateDetails(cert))
	}
	types, inner := errorChain(err)
	if len(types) > 1 {
		out = append(out, "Error chain: "+strings.Join(types, " › "))
	}
	if inner != "" {
		out = append(out, "Innermost error: "+inner)
	}
	return out
}

// safeText is an error text as the log shows it.
func safeText(s string) string {
	return sanitizeURLs(kubeconfig.SafeError(errors.New(s)).Error())
}

// errorClass names the kind of failure in words a user can act on.
func errorClass(err error) string {
	msg := err.Error()
	var dns *net.DNSError
	var unknown x509.UnknownAuthorityError
	var host x509.HostnameError
	var invalid x509.CertificateInvalidError
	var header tls.RecordHeaderError
	var netErr net.Error
	var ended *LoginEndedError
	switch {
	case errors.As(err, &ended) && ended.TimedOut() && ended.RunsOn:
		return "timeout: " + ended.Error() + " (D-73)"
	case errors.As(err, &ended) && ended.TimedOut():
		return "timeout: " + ended.Error() + "; Cogpit ended it (D-73)"
	case errors.As(err, &ended):
		return "cancelled: " + ended.Error()
	case IsPluginFailure(err):
		return "the credential plugin failed: " + pluginFailure(msg)
	case errors.As(err, &dns):
		return dnsClass(dns)
	case errors.Is(err, syscall.ECONNREFUSED):
		return "TCP: the connection was refused" + addrOf(err) + ": nothing listens there, or a firewall refuses it"
	case errors.Is(err, syscall.ECONNRESET):
		return "TCP: the connection was reset" + addrOf(err)
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return "network: the server's address cannot be reached from this computer" + addrOf(err)
	case errors.As(err, &unknown):
		return "TLS: the server's certificate is signed by an authority this context does not trust"
	case errors.As(err, &host):
		return "TLS: the server's certificate is not issued for " + host.Host
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return "TLS: the server's certificate has expired or is not valid yet (this computer's time is " +
			time.Now().UTC().Format(time.DateTime) + " UTC)"
	case errors.As(err, &invalid):
		return "TLS: the server's certificate is not valid for an API server"
	case errors.As(err, &header), strings.Contains(msg, "server gave HTTP response to HTTPS client"):
		return "TLS: the server does not speak TLS at this address"
	case strings.Contains(msg, "TLS handshake timeout"):
		return "TLS: the handshake timed out"
	case strings.Contains(msg, "proxyconnect"):
		return "proxy: the proxy refused or failed the connection"
	case apierrors.IsUnauthorized(err) || errors.Is(err, ErrLoginRejected):
		return "unauthorized: the API server rejected the credentials"
	case apierrors.IsForbidden(err):
		return "forbidden: the API server denied access"
	case errors.As(err, &netErr) && netErr.Timeout(), errors.Is(err, ErrNoAnswer):
		return "timeout: no answer in time"
	}
	return "other"
}

// dnsClass describes a DNS failure.
func dnsClass(dns *net.DNSError) string {
	text := "DNS: the name " + dns.Name + " cannot be resolved"
	switch {
	case dns.IsNotFound:
		text += " (no such host)"
	case dns.IsTimeout:
		text += " (the lookup timed out)"
	case dns.IsTemporary:
		text += " (a temporary failure)"
	}
	if dns.Server != "" {
		text += " by " + dns.Server
	}
	return text
}

// addrOf is " by <address>" for an error that names the address it dialled.
func addrOf(err error) string {
	var op *net.OpError
	if errors.As(err, &op) && op.Addr != nil {
		return " by " + op.Addr.String()
	}
	return ""
}

// IsPluginFailure reports whether err is a credential plugin's failure as client-go reports it
// ("getting credentials: exec: …"; client-go formats the plugin's error with %v, so only its text
// tells).
func IsPluginFailure(err error) bool {
	if err == nil {
		return false
	}
	var pe *PluginError
	if errors.As(err, &pe) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "getting credentials: ") || strings.Contains(msg, "exec: executable ") ||
		strings.Contains(msg, "exec plugin")
}

// pluginFailure is the plugin's part of client-go's message.
func pluginFailure(msg string) string {
	if _, rest, ok := strings.Cut(msg, "getting credentials: "); ok {
		msg = rest
	}
	return safeText(msg)
}

// presentedCertificate is the server's own certificate (the leaf it presented), from a TLS error;
// nil otherwise. The x509 errors name the certificate they stopped at, which for an unknown
// authority is the last of the chain: the handshake's error has the leaf.
func presentedCertificate(err error) *x509.Certificate {
	var unknown x509.UnknownAuthorityError
	var host x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verify *tls.CertificateVerificationError
	switch {
	case errors.As(err, &verify) && len(verify.UnverifiedCertificates) > 0:
		return verify.UnverifiedCertificates[0]
	case errors.As(err, &host) && host.Certificate != nil:
		return host.Certificate
	case errors.As(err, &invalid) && invalid.Cert != nil:
		return invalid.Cert
	case errors.As(err, &unknown) && unknown.Cert != nil:
		return unknown.Cert
	}
	return nil
}

// certificateDetails are a certificate's public facts: subject, issuer, validity and names.
func certificateDetails(cert *x509.Certificate) string {
	text := fmt.Sprintf("subject %s, issuer %s, valid %s to %s UTC", cert.Subject.String(), cert.Issuer.String(),
		cert.NotBefore.UTC().Format(time.DateTime), cert.NotAfter.UTC().Format(time.DateTime))
	names := append([]string(nil), cert.DNSNames...)
	for _, ip := range cert.IPAddresses {
		names = append(names, ip.String())
	}
	if len(names) > 0 {
		if len(names) > 12 {
			names = append(names[:12], fmt.Sprintf("… %d more", len(names)-12))
		}
		text += ", names " + strings.Join(names, ", ")
	}
	return text
}

// errorChain is the types of err's chain, outermost first, and the innermost error's text.
func errorChain(err error) (types []string, inner string) {
	for e := err; e != nil; {
		types = append(types, fmt.Sprintf("%T", e))
		inner = e.Error()
		switch u := e.(type) {
		case interface{ Unwrap() error }:
			e = u.Unwrap()
		case interface{ Unwrap() []error }:
			if list := u.Unwrap(); len(list) > 0 {
				e = list[0]
			} else {
				e = nil
			}
		default:
			e = nil
		}
		if len(types) >= 12 {
			break
		}
	}
	return types, safeText(inner)
}
