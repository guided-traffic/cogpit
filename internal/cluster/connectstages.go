package cluster

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/rest"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// A connect's stages (D-114, the owner's AW17 of 2026-10-22, verbatim: "Zu einer Beschreibung des
// Verbindungsaufbaus gehört immer: kube-config valid und vollständig? das Auflösen der API per DNS
// (geklappt? Ergebnis?) einen TCP Verbindungsaufbau (IP + Port erreichbar?) dann erst der übliche
// Verbindungsaufbau zu einem Kube-API Server."). Connect reports them once, at its end, through
// Options.Trace (a ConnectStep named StepStages) and on the *ConnectError of a failed connect: each
// stage with its result, the one that failed with its cause in a sentence, the ones after it not
// reached.
//
//   - kubeconfig: the file, the context and its cluster and user entries load, and the client is
//     built from them (the certificate and key files read, a credential plugin allowed);
//   - login: a credential plugin's run, for a context that has one: it runs before the request is
//     sent, so before the network stages (the lead's decision, D-114);
//   - dns, tcp, tls, api: what client-go's own first request (GET /version) did, measured with
//     net/http/httptrace on that request: the name looked up and the addresses it gave, the address
//     and port dialled, the TLS handshake, and the API server's answer with the login accepted or
//     not. Cogpit opens no connection of its own for them (the lead's decision, D-114): behind a
//     proxy the dial goes to the proxy, and the stages say so.
//
// A stage's text never holds a credential: it names files, hosts, addresses and the certificate
// check, never a token, a key or a password; the proxy's password is •••.

// The stages (Stage.Name), in the order they run.
const (
	StageKubeconfig = "kubeconfig"
	StageLogin      = "login" // a credential plugin's run; only for a context that has one
	StageDNS        = "dns"
	StageTCP        = "tcp"
	StageTLS        = "tls"
	StageAPI        = "api"
)

// A stage's result (Stage.Result).
const (
	StageOK         = "ok"
	StageFailed     = "failed"
	StageNotReached = "notReached" // a stage before it failed
	StageSkipped    = "skipped"    // not needed: an address needs no lookup, plain http no TLS
	StageCancelled  = "cancelled"  // the connect was cancelled while it ran
)

// stageReused is a network stage's text when the request went over a connection already open.
const stageReused = "an open connection was used"

// StepStages is the ConnectStep that carries a connect's stages (ConnectStep.Stages).
const StepStages = "stages"

// Stage is one stage of a connect and its result.
type Stage struct {
	Name   string        // one of the Stage constants
	Result string        // StageOK, StageFailed, StageNotReached, StageSkipped or StageCancelled
	Text   string        // the result in a sentence: what was found, or why it failed; never a credential
	Took   time.Duration // how long it took; zero when it is not timed
	// Missing is the program a failed login did not find where Cogpit looks for programs
	// (MissingProgram, D-120): the credential plugin itself, or the kubectl plugin kubectl runs as
	// the credential plugin did not find; empty for any other result.
	Missing string
}

// StagesOf returns the stages a failed connect's error carries, and the cause of the failure in a
// sentence (the failed stage's text); none for an error that does not carry them.
func StagesOf(err error) (stages []Stage, cause string) {
	var ce *ConnectError
	if !errors.As(err, &ce) {
		return nil, ""
	}
	return append([]Stage(nil), ce.Stages...), ce.Cause
}

// ConfigStages are the stages of a connect whose configuration failed before Connect could run
// (no source offers the context, Cogpit's proxy for it is not a URL): the kubeconfig stage failed
// with cause, every network stage not reached.
func ConfigStages(cause string) []Stage {
	out := make([]Stage, 0, 5)
	out = append(out, Stage{Name: StageKubeconfig, Result: StageFailed, Text: cause})
	for _, name := range []string{StageDNS, StageTCP, StageTLS, StageAPI} {
		out = append(out, Stage{Name: name, Result: StageNotReached})
	}
	return out
}

// connectStages collects a connect's stages: what Connect found while it built the client, and what
// client-go's first request did (its httptrace hooks, which may run on the dialer's goroutines).
type connectStages struct {
	mu sync.Mutex

	// From the configuration.
	configured bool   // the kubeconfig loaded and the client was built from it
	config     string // the kubeconfig stage's text once it passed
	cause      string // the kubeconfig's failure in a sentence, where Problem says it less plainly
	plugin     string // the credential plugin's program; "" without one
	host, port string // the API server's host and port
	plain      bool   // the server URL is plain http
	proxy      string // the proxy the dial goes to, as the log shows it (•••); "" for a direct connection
	proxyHost  string // the proxy's host:port
	tlsCheck   string // how the server's certificate is checked, in words
	limit      time.Duration
	login      *Stage // the plugin's run, once the first request returned
	secrets    []string

	// From the first request (httptrace).
	frozen    bool
	started   bool // the request asked the transport for a connection
	reused    bool
	dnsHost   string
	dnsStart  time.Time
	dnsDone   time.Time
	dnsAddrs  []string
	dnsErr    error
	dials     []stageDial
	tlsStart  time.Time
	tlsDone   time.Time
	tlsErr    error
	tlsVer    uint16
	wrote     time.Time
	firstByte time.Time
	version   string // the API server's version, from the answer
}

type stageDial struct {
	addr       string
	start, end time.Time
	err        error
}

// configuredFrom records what the client is built from: the server, the proxy the dial goes to, the
// credential plugin and the certificate check. kubeProxy is the kubeconfig's proxy-url; the proxy
// of Cogpit's settings is cfg.Proxy already (applyProxy).
func (s *connectStages) configuredFrom(cfg *rest.Config, kubeProxy string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, err := url.Parse(cfg.Host); err == nil {
		s.host, s.port, s.plain = u.Hostname(), u.Port(), u.Scheme == "http"
		if s.port == "" {
			s.port = "443"
			if s.plain {
				s.port = "80"
			}
		}
	}
	if cfg.ExecProvider != nil {
		s.plugin = filepath.Base(cfg.ExecProvider.Command)
	}
	s.tlsCheck = tlsCheckText(cfg)
	if p := proxyOf(cfg, kubeProxy); p != nil {
		s.proxy, s.proxyHost = redactedProxy(p.String()), proxyHostPort(p)
	}
}

// proxyOf is the proxy client-go's transport sends the requests to cfg.Host through: cfg.Proxy
// (Cogpit's for the cluster, or client-go's from proxy-url), else the kubeconfig's proxy-url, else
// the environment's; nil for a direct connection.
func proxyOf(cfg *rest.Config, kubeProxy string) *url.URL {
	req, err := http.NewRequest(http.MethodGet, cfg.Host, nil)
	if err != nil {
		return nil
	}
	switch {
	case cfg.Proxy != nil:
		u, err := cfg.Proxy(req)
		if err == nil {
			return u
		}
		return nil
	case kubeProxy != "":
		u, err := url.Parse(kubeProxy)
		if err == nil && u.Host != "" {
			return u
		}
		return nil
	}
	u, err := http.ProxyFromEnvironment(req)
	if err != nil {
		return nil
	}
	return u
}

// proxyHostPort is a proxy URL's host and port, the port its scheme's when it names none.
func proxyHostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "socks5", "socks5h":
			port = "1080"
		default:
			port = "80"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}

// tlsCheckText says against what the server's certificate is checked, in a few words.
func tlsCheckText(cfg *rest.Config) string {
	switch {
	case cfg.Insecure:
		return "its certificate NOT checked (insecure-skip-tls-verify)"
	case len(cfg.CAData) > 0:
		return "its certificate checked against the kubeconfig's certificate authority"
	case cfg.CAFile != "":
		return "its certificate checked against the certificate authority file " + cfg.CAFile
	}
	return "its certificate checked against this computer's trusted authorities"
}

// passedConfig marks the kubeconfig stage passed: the client is built.
func (s *connectStages) passedConfig(text string, limit time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configured, s.config, s.limit = true, text, limit
}

// failedConfig records the kubeconfig's failure in a sentence, plainer than the error's Problem.
func (s *connectStages) failedConfig(cause string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cause = cause
}

// loginRan records the credential plugin's run of the first request: how long it took, and how it
// ended (an error of Cogpit's ending it, or nil).
func (s *connectStages) loginRan(run pluginRun, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plugin == "" {
		return
	}
	if !ok { // no run: the credential of an earlier one is still valid
		s.login = &Stage{Name: StageLogin, Result: StageOK, Text: s.plugin + ": the credential of its last run, still valid (not run again)"}
		return
	}
	end := run.ended
	if end.IsZero() {
		end = time.Now()
	}
	st := Stage{Name: StageLogin, Result: StageOK, Text: s.plugin + " answered", Took: end.Sub(run.Since)}
	switch {
	case run.ending != nil && errors.Is(run.ending.Cause, ErrLoginCancelled):
		st.Result, st.Text = StageCancelled, run.ending.endText()
	case run.ending != nil:
		st.Result, st.Text = StageFailed, run.ending.Error()
	case run.ended.IsZero():
		st.Result, st.Text = StageFailed, s.plugin+" still runs"
	}
	s.login = &st
}

// stageLoginRun records the credential plugin's run of Connect's first request (loginRunSince,
// loginRan).
func (c *Conn) stageLoginRun(run pluginRun, ok bool) {
	if c == nil || c.stages == nil || c.hold == nil {
		return
	}
	c.stages.loginRan(run, ok)
}

// connectLimit is the first request's bound: timeout, else DefaultConnectTimeout.
func connectLimit(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return DefaultConnectTimeout
	}
	return timeout
}

// proxySecrets are the password of Cogpit's proxy for the cluster, as typed and as it stands
// escaped in a URL, which no stage may quote.
func proxySecrets(proxy *url.URL) []string {
	if proxy == nil || proxy.User == nil {
		return nil
	}
	pw, has := proxy.User.Password()
	if !has || pw == "" {
		return nil
	}
	out := []string{pw}
	if _, escaped, _ := strings.Cut(proxy.User.String(), ":"); escaped != pw {
		out = append(out, escaped)
	}
	return out
}

// answered records the API server's version from the first answer.
func (s *connectStages) answered(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.version = version
}

// withTrace is ctx with the hooks that record the first request's stages.
func (s *connectStages) withTrace(ctx context.Context) context.Context {
	lock := func(f func()) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.frozen {
			f()
		}
	}
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GetConn: func(string) { lock(func() { s.started = true }) },
		GotConn: func(info httptrace.GotConnInfo) { lock(func() { s.reused = s.reused || info.Reused }) },
		DNSStart: func(info httptrace.DNSStartInfo) {
			lock(func() {
				if s.dnsStart.IsZero() {
					s.dnsHost, s.dnsStart = info.Host, time.Now()
				}
			})
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			lock(func() {
				if !s.dnsDone.IsZero() && s.dnsErr == nil {
					return // a second lookup (a retry) after one that worked
				}
				s.dnsDone, s.dnsErr, s.dnsAddrs = time.Now(), info.Err, nil
				for _, a := range info.Addrs {
					s.dnsAddrs = append(s.dnsAddrs, a.String())
				}
			})
		},
		ConnectStart: func(_, addr string) {
			lock(func() { s.dials = append(s.dials, stageDial{addr: addr, start: time.Now()}) })
		},
		ConnectDone: func(_, addr string, err error) {
			lock(func() {
				for i := range s.dials {
					if d := &s.dials[i]; d.addr == addr && d.end.IsZero() {
						d.end, d.err = time.Now(), err
						return
					}
				}
			})
		},
		TLSHandshakeStart: func() { lock(func() { s.tlsStart, s.tlsDone, s.tlsErr = time.Now(), time.Time{}, nil }) },
		TLSHandshakeDone: func(st tls.ConnectionState, err error) {
			lock(func() { s.tlsDone, s.tlsErr, s.tlsVer = time.Now(), err, st.Version })
		},
		WroteRequest:         func(httptrace.WroteRequestInfo) { lock(func() { s.wrote = time.Now() }) },
		GotFirstResponseByte: func() { lock(func() { s.firstByte = time.Now() }) },
	})
}

// finish ends the collection: it reports the stages through trace and puts them, and the cause of a
// failure, on err's *ConnectError. Hooks that run later (a dial the cancelled request left behind)
// change nothing.
func (s *connectStages) finish(ctx context.Context, trace tracer, err error) error {
	s.mu.Lock()
	s.frozen = true
	list := s.stagesLocked(err, ctx.Err() != nil)
	secrets := s.secrets
	s.mu.Unlock()
	for i := range list {
		list[i].Text = replaceSecrets(list[i].Text, secrets)
	}
	if trace != nil {
		trace(ConnectStep{Name: StepStages, Stages: list})
	}
	if err == nil {
		return nil
	}
	cause := ""
	for _, st := range list {
		if st.Result == StageFailed || st.Result == StageCancelled {
			cause = st.Text
			break
		}
	}
	var ce *ConnectError
	if errors.As(err, &ce) {
		out := *ce
		out.Stages, out.Cause = list, cause
		return &out
	}
	return &ConnectError{Problem: err.Error(), Err: err, Stages: list, Cause: cause}
}

// stagesLocked computes the stages from what was recorded; err is how the connect ended. The first
// stage that failed takes err's cause; every stage after it is not reached. s.mu is held.
func (s *connectStages) stagesLocked(err error, cancelled bool) []Stage {
	if !s.configured {
		cause := s.cause
		var ce *ConnectError
		if cause == "" && errors.As(err, &ce) {
			cause = ce.Problem
		}
		if cause == "" && err != nil {
			cause = kubeconfig.SafeError(err).Error()
		}
		st := Stage{Name: StageKubeconfig, Result: StageFailed, Text: cause}
		if cancelled {
			st.Result, st.Text = StageCancelled, "cancelled before the kubeconfig was read"
		}
		return append([]Stage{st}, ConfigStages("")[1:]...)
	}
	list := []Stage{{Name: StageKubeconfig, Result: StageOK, Text: s.config}}
	if s.plugin != "" {
		login := Stage{Name: StageLogin}
		if s.login != nil {
			login = *s.login
		}
		var ended *LoginEndedError
		if login.Result != StageCancelled && (IsPluginFailure(err) || errors.As(err, &ended)) {
			login.Result, login.Text = StageFailed, s.causeOf(err) // the run answered, its answer did not work
			login.Missing = MissingProgram(err)
		}
		list = append(list, login)
	}
	list = append(list, s.dnsStage(), s.tcpStage(err), s.tlsStage(), s.apiStage(err))
	return s.settle(list, err, cancelled)
}

// settle marks the stage err ended in: the first one that is neither passed nor skipped takes err's
// cause (cancelled when the connect was), unless one failed already; every stage after the failed
// one is not reached, whatever it recorded.
func (s *connectStages) settle(list []Stage, err error, cancelled bool) []Stage {
	failed := -1
	for i, st := range list {
		if st.Result == StageFailed || st.Result == StageCancelled {
			failed = i
			break
		}
	}
	if failed < 0 && err != nil {
		for i := range list {
			if r := list[i].Result; r != StageOK && r != StageSkipped {
				failed = i
				break
			}
		}
		if failed < 0 { // every stage passed and the connect failed after them: the answer's
			failed = len(list) - 1
		}
		list[failed].Result, list[failed].Text = StageFailed, s.causeOf(err)
		if cancelled {
			list[failed].Result, list[failed].Text = StageCancelled, "cancelled while it ran"
		}
	}
	if failed < 0 {
		return list
	}
	for i := failed + 1; i < len(list); i++ {
		list[i] = Stage{Name: list[i].Name, Result: StageNotReached}
	}
	return list
}

// dnsStage is the lookup of the name the dial went to: the API server's, or behind a proxy the
// proxy's (the proxy then resolves the API server's name).
func (s *connectStages) dnsStage() Stage {
	st := Stage{Name: StageDNS}
	switch {
	case s.reused:
		st.Result, st.Text = StageSkipped, stageReused
		return st
	case s.dnsStart.IsZero() && len(s.dials) == 0:
		return st // not reached: the request did not dial
	case s.dnsStart.IsZero(): // the dial went to an address
		st.Result = StageSkipped
		st.Text = s.host + " is an address: no lookup"
		if s.proxy != "" {
			st.Text = "the proxy " + s.proxyHost + " is an address: no lookup" + s.proxyResolves()
		}
		return st
	case s.dnsDone.IsZero():
		return st // under way when the connect ended: settle says why
	}
	st.Took = s.dnsDone.Sub(s.dnsStart)
	name := s.dnsHost
	if s.proxy != "" {
		name = "the proxy's name " + s.dnsHost
	}
	if s.dnsErr != nil {
		st.Result, st.Text = StageFailed, name+" "+lookupFailure(s.dnsErr)
		return st
	}
	st.Result, st.Text = StageOK, name+": "+joinCapped(s.dnsAddrs, 6)
	if s.proxy != "" {
		st.Text += s.proxyResolves()
	}
	return st
}

// proxyResolves says, behind a proxy, that the proxy looks up the API server's name; nothing for
// a server named by its address.
func (s *connectStages) proxyResolves() string {
	if net.ParseIP(s.host) != nil {
		return ""
	}
	return "; the proxy resolves " + s.host
}

// lookupFailure says why a lookup failed: "cannot be resolved (no such host)".
func lookupFailure(err error) string {
	var dns *net.DNSError
	if !errors.As(err, &dns) {
		return "cannot be resolved: " + safeText(err.Error())
	}
	text := "cannot be resolved"
	switch {
	case dns.IsNotFound:
		text += " (no such host)"
	case dns.IsTimeout:
		text += " (the lookup timed out)"
	case dns.IsTemporary:
		text += " (a temporary failure: " + safeText(dns.Err) + ")"
	default:
		text += ": " + safeText(dns.Err)
	}
	if dns.Server != "" {
		text += " by " + dns.Server
	}
	return text
}

// tcpStage is the dial: the address and port reached, or why not. Behind a proxy it is the dial to
// the proxy and the tunnel it opens: a tunnel the proxy refused fails it.
func (s *connectStages) tcpStage(err error) Stage {
	st := Stage{Name: StageTCP}
	if s.reused {
		st.Result, st.Text = StageSkipped, stageReused
		return st
	}
	if len(s.dials) == 0 {
		return st
	}
	target := ""
	if s.proxy != "" {
		target = "the proxy " + s.proxy + " at "
	}
	var last stageDial
	for _, d := range s.dials {
		if !d.end.IsZero() && d.err == nil {
			st.Result, st.Text, st.Took = StageOK, target+d.addr+" reached", d.end.Sub(d.start)
			if s.proxy != "" && err != nil && !s.plain && s.tlsStart.IsZero() && s.wrote.IsZero() {
				st.Result = StageFailed // reached, and it did not open the tunnel (a 407, a 403)
				st.Text = target + d.addr + " reached; it did not open the tunnel to " + net.JoinHostPort(s.host, s.port) + ": " + innermost(err)
			}
			return st
		}
		if !d.end.IsZero() {
			last = d
		}
	}
	if last.end.IsZero() {
		return st // under way when the connect ended: settle says why
	}
	st.Result, st.Took = StageFailed, last.end.Sub(s.dials[0].start)
	st.Text = target + last.addr + " " + dialFailure(last.err, s.limit)
	if n := len(s.dials); n > 1 {
		st.Text += fmt.Sprintf(" (%d addresses tried)", n)
	}
	return st
}

// dialFailure says why a dial failed: "refused the connection: …", "did not answer within 15s".
func dialFailure(err error, limit time.Duration) string {
	var netErr net.Error
	switch class := errorClass(err); {
	case strings.HasPrefix(class, "TCP: the connection was refused"):
		return "refused the connection: nothing listens there, or a firewall refuses it"
	case strings.HasPrefix(class, "TCP: the connection was reset"):
		return "reset the connection"
	case strings.HasPrefix(class, "network: "):
		return "cannot be reached from this computer (no route to it)"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		if limit > 0 {
			return "did not answer within " + limit.String()
		}
		return "did not answer in time"
	}
	return "cannot be reached: " + innermost(err)
}

// tlsStage is the handshake with the API server (through the proxy's tunnel behind one).
func (s *connectStages) tlsStage() Stage {
	st := Stage{Name: StageTLS}
	switch {
	case s.plain:
		st.Result, st.Text = StageSkipped, "none: the server URL is plain http"
		return st
	case s.reused:
		st.Result, st.Text = StageSkipped, stageReused
		return st
	case s.tlsStart.IsZero() || s.tlsDone.IsZero():
		return st
	}
	st.Took = s.tlsDone.Sub(s.tlsStart)
	if s.tlsErr != nil {
		st.Result, st.Text = StageFailed, tlsFailure(s.tlsErr)
		return st
	}
	st.Result, st.Text = StageOK, tlsVersionName(s.tlsVer)+" with "+s.host+", "+s.tlsCheck
	return st
}

// tlsFailure says why a handshake failed, as the error picture's class does, without its "TLS: ".
func tlsFailure(err error) string {
	class := errorClass(err)
	if rest, ok := strings.CutPrefix(class, "TLS: "); ok {
		return rest
	}
	return "the handshake failed: " + innermost(err)
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS10:
		return "TLS 1.0"
	}
	return "TLS"
}

// apiStage is the API server's answer to GET /version: the login accepted and the server's
// version, or what the server said.
func (s *connectStages) apiStage(err error) Stage {
	st := Stage{Name: StageAPI}
	if s.wrote.IsZero() && err != nil {
		return st // the request was not sent: an earlier stage failed
	}
	if !s.wrote.IsZero() && !s.firstByte.IsZero() {
		st.Took = s.firstByte.Sub(s.wrote)
	}
	if err == nil {
		st.Result, st.Text = StageOK, "GET /version answered: Kubernetes "+s.version+"; the login accepted"
		return st
	}
	if s.firstByte.IsZero() {
		return st // no answer: settle says why
	}
	st.Result, st.Text = StageFailed, s.causeOf(err)
	return st
}

// causeOf is the cause of a connect's failure in a sentence: the API server's answer, a timeout
// with its limit, the credential plugin's failure, or the innermost error.
func (s *connectStages) causeOf(err error) string {
	var status apierrors.APIStatus
	var ended *LoginEndedError
	var dns *net.DNSError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &ended) && ended.TimedOut() && !ended.RunsOn:
		return ended.Error() + "; Connect starts it again" // as describeConnectFailure says it (D-73)
	case errors.As(err, &ended):
		return ended.Error()
	case IsPluginFailure(err):
		if missing := MissingProgram(err); missing != "" {
			return missingProgramText(s.plugin, missing)
		}
		return "the credential plugin " + s.plugin + " failed: " + pluginFailure(err.Error())
	case apierrors.IsUnauthorized(err) || errors.Is(err, ErrLoginRejected):
		return "the API server rejected the login (401 Unauthorized): log in again or check the user entry"
	case apierrors.IsForbidden(err):
		return "the API server denied access (403 Forbidden)"
	case errors.As(err, &status):
		st := status.Status()
		text := fmt.Sprintf("the API server answered %d %s", st.Code, http.StatusText(int(st.Code)))
		if st.Message != "" {
			text += ": " + safeText(st.Message)
		}
		return text
	case errors.Is(err, ErrNoAnswer), errors.Is(err, context.DeadlineExceeded):
		if s.limit > 0 {
			return "no answer within " + s.limit.String()
		}
		return "no answer in time"
	case errors.As(err, &dns):
		return dns.Name + " " + lookupFailure(dns)
	case strings.Contains(err.Error(), "proxyconnect"):
		return "the proxy " + s.proxy + " refused or failed the connection: " + innermost(err)
	}
	return innermost(err)
}

// missingProgramText says that a credential plugin's login failed for a program it did not find
// (MissingProgram): the plugin itself, or the kubectl plugin kubectl looked for.
func missingProgramText(plugin, missing string) string {
	if missing == plugin {
		return "the credential plugin " + plugin + " was not found: no program " + plugin + " on PATH"
	}
	return "the credential plugin " + plugin + " failed: it found no " + missing + " on PATH (kubectl: unknown command)"
}

// innermost is the innermost error's text, made safe.
func innermost(err error) string {
	_, inner := errorChain(err)
	return inner
}

// joinCapped joins at most n values, naming how many more there are.
func joinCapped(values []string, n int) string {
	if len(values) == 0 {
		return "no addresses"
	}
	if len(values) <= n {
		return strings.Join(values, ", ")
	}
	return strings.Join(values[:n], ", ") + fmt.Sprintf(" and %d more", len(values)-n)
}

// configCause is a kubeconfig that client-go found invalid, said plainly: each file it names that
// does not exist or cannot be read ("the client certificate file … does not exist"), and its other
// problems as client-go words them, at most three (D-114: the error line shortened to its cause; the
// whole chain stays in the connection log).
func configCause(err error) string {
	problems := kubeconfigProblems(err)
	parts := make([]string, 0, 4)
	for i, p := range problems {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("and %d more", len(problems)-3))
			break
		}
		parts = append(parts, problemSentence(p))
	}
	return strings.Join(parts, "; ")
}

// kubeconfigProblems are the errors of client-go's aggregate of a kubeconfig's problems, nested
// aggregates flattened; err itself for any other error.
func kubeconfigProblems(err error) []error {
	var agg utilerrors.Aggregate
	if !errors.As(err, &agg) || len(agg.Errors()) == 0 {
		return []error{err}
	}
	var out []error
	for _, e := range agg.Errors() {
		out = append(out, kubeconfigProblems(e)...)
	}
	return out
}

// problemSentence is one of a kubeconfig's problems in a sentence.
func problemSentence(problem error) string {
	msg := problem.Error()
	var pe *fs.PathError
	if !errors.As(problem, &pe) {
		return strings.TrimPrefix(safeText(msg), "invalid configuration: ")
	}
	what := "the file"
	switch {
	case strings.HasPrefix(msg, "unable to read client-cert "):
		what = "the client certificate file"
	case strings.HasPrefix(msg, "unable to read client-key "):
		what = "the client key file"
	case strings.HasPrefix(msg, "unable to read certificate-authority "):
		what = "the certificate authority file"
	}
	switch {
	case errors.Is(pe, fs.ErrNotExist):
		return what + " " + pe.Path + " does not exist"
	case errors.Is(pe, fs.ErrPermission):
		return what + " " + pe.Path + " cannot be read (permission denied)"
	}
	return what + " " + pe.Path + " cannot be read: " + safeText(pe.Err.Error())
}

// configText is the kubeconfig stage's text once it passed: the context, its file and its entries,
// the server and how the user logs in.
func configText(name, file, server string, cfg *rest.Config) string {
	return fmt.Sprintf("context %q in %s: server %s, login %s", name, file, logURL(server), loginKind(cfg))
}

// loginKind names how the user logs in, never with what.
func loginKind(cfg *rest.Config) string {
	switch {
	case cfg.ExecProvider != nil:
		return "by the credential plugin " + filepath.Base(cfg.ExecProvider.Command)
	case len(cfg.CertData) > 0 || cfg.CertFile != "":
		return "by a client certificate"
	case cfg.BearerToken != "" || cfg.BearerTokenFile != "":
		return "by a bearer token"
	case cfg.Username != "":
		return "by user name and password"
	case cfg.AuthProvider != nil:
		return "by the auth provider " + cfg.AuthProvider.Name
	}
	return "none (anonymous)"
}
