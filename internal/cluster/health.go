package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/connrotation"
)

// A connection's health and the requests that wait on it (D-58, standby and lost connections).
//
// Every request of a connection passes three round-trippers of Cogpit's own besides the counter:
//
//   - a gate, twice: around the connection's HTTP client (in front of client-go's credential
//     plugin, so that a request it holds never makes the plugin run) and inside every transport
//     built from the connection's configuration (exec, attach, port forwards, Helm). While the
//     engine says the connection is interrupted or the login expired (SetHealth), the informers'
//     lists and watches wait at the outer gate until it is healthy again and then go out at once
//     (no backoff of client-go's to wait out), and every other request is refused at once with
//     ErrInterrupted or ErrLoginExpired: nothing waits on a dead connection. Only the probes pass.
//   - a login guard below the credential plugin: the first 401 Unauthorized after a working
//     request reaches client-go's plugin, which then runs once to renew the login; later ones end
//     as ErrLoginRejected, which client-go does not answer with another run, until a request works
//     again or the user logs in again (ArmLogin). So a login that does not renew opens no pile of
//     browser tabs.
//   - a request timeout: a request that is not a stream (a watch, a log, exec, attach, a port
//     forward) ends after RequestTimeout without an answer. A read says so; a write whose request
//     may have reached the server ends as an OutcomeUnknownError.
//
// The probes: Heartbeat asks GET /version with the connection's credentials, through the gates;
// Reachable asks it without credentials on a client of its own, so that probing a cluster that does
// not answer never runs a credential plugin.

// DefaultRequestTimeout bounds a request that is not a stream (Options.RequestTimeout).
const DefaultRequestTimeout = 30 * time.Second

// Health is what the engine knows of a connection (SetHealth).
type Health int32

// Healths.
const (
	// HealthOK lets every request through.
	HealthOK Health = iota
	// HealthInterrupted: the cluster does not answer. Lists and watches wait, others are refused.
	HealthInterrupted
	// HealthLoginExpired: the API server rejects the login. Lists and watches wait, others are
	// refused, and no request reaches the credential plugin.
	HealthLoginExpired
	// HealthLoginLapsed: the login lapsed while the engine held its credential plugin (loginhold.go,
	// D-69 item 4). Lists and watches wait, others are refused with ErrLoginLapsed.
	HealthLoginLapsed
)

// Errors of a connection that is not healthy, and of its requests.
var (
	// ErrInterrupted refuses a request while the connection is interrupted.
	ErrInterrupted = errors.New("the connection to the cluster is interrupted; Cogpit is reconnecting")
	// ErrLoginExpired refuses a request while the login is expired.
	ErrLoginExpired = errors.New("the login to the cluster expired; log in again")
	// ErrLoginRejected is a 401 Unauthorized the login guard did not hand to the credential plugin.
	ErrLoginRejected = errors.New("the API server rejected the credentials (401 Unauthorized)")
	// ErrNoAnswer ends a read that got no answer within the request timeout.
	ErrNoAnswer = errors.New("no answer from the API server")
	// ErrOutcomeUnknown is what an OutcomeUnknownError is.
	ErrOutcomeUnknown = errors.New("outcome unknown")
)

// OutcomeUnknownError ends a write whose request may have reached the API server, but whose answer
// never came (the connection broke or the request timed out): whether it happened is unknown until
// the cache shows the object again.
type OutcomeUnknownError struct {
	Verb, Resource string
	Err            error
}

func (e *OutcomeUnknownError) Error() string {
	return fmt.Sprintf("outcome unknown: the API server did not answer the %s of %s (%v); the view shows the object's state once reconnected",
		e.Verb, e.Resource, e.Err)
}

// Is makes errors.Is(err, ErrOutcomeUnknown) hold.
func (e *OutcomeUnknownError) Is(target error) bool { return target == ErrOutcomeUnknown }

func (e *OutcomeUnknownError) Unwrap() error { return e.Err }

// probeKey marks a probe's context: the gates let it through.
type probeKey struct{}

func withProbe(ctx context.Context) context.Context { return context.WithValue(ctx, probeKey{}, true) }

func isProbe(ctx context.Context) bool { v, _ := ctx.Value(probeKey{}).(bool); return v }

// gate holds or refuses requests while the connection is not healthy. The zero value is not
// usable: newGate.
type gate struct {
	prefix string // the server URL's path, stripped before a request is classified

	mu     sync.Mutex
	health Health
	open   chan struct{} // closed while healthy; a new one while not
}

func newGate(prefix string) *gate {
	open := make(chan struct{})
	close(open)
	return &gate{prefix: prefix, open: open}
}

// set changes the health: a healthy gate lets the requests it holds go.
func (g *gate) set(h Health) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if h == g.health {
		return
	}
	switch {
	case h == HealthOK:
		close(g.open)
	case g.health == HealthOK:
		g.open = make(chan struct{})
	}
	g.health = h
}

func (g *gate) state() (Health, <-chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.health, g.open
}

// refusal is the error a request gets while the gate is h.
func refusal(h Health) error {
	switch h {
	case HealthLoginExpired:
		return ErrLoginExpired
	case HealthLoginLapsed:
		return ErrLoginLapsed
	}
	return ErrInterrupted
}

// holds reports whether the gate holds a request rather than refusing it: an informer's list or
// watch, which client-go retries anyway and which should go out the moment the connection works.
// The Metrics API's lists are polls of their own, refused like any other read.
func (g *gate) holds(req *http.Request) bool {
	verb, resource := Classify(req.Method, stripPrefix(req.URL, g.prefix))
	switch verb {
	case VerbList, VerbWatch, VerbWatchList:
		return !strings.HasSuffix(resource, ".metrics.k8s.io")
	}
	return false
}

// outer wraps the connection's HTTP client: it holds lists and watches until healthy.
func (g *gate) outer(next http.RoundTripper) http.RoundTripper {
	return &gateRoundTripper{g: g, next: next, hold: true}
}

// inner wraps every transport built from the connection's configuration: it refuses what is not a
// probe while not healthy.
func (g *gate) inner(next http.RoundTripper) http.RoundTripper {
	return &gateRoundTripper{g: g, next: next}
}

type gateRoundTripper struct {
	g    *gate
	next http.RoundTripper
	hold bool
}

func (r *gateRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if isProbe(req.Context()) {
		return r.next.RoundTrip(req)
	}
	for {
		h, open := r.g.state()
		if h == HealthOK {
			return r.next.RoundTrip(req)
		}
		if !r.hold || !r.g.holds(req) {
			closeRequestBody(req)
			return nil, refusal(h)
		}
		select {
		case <-open:
		case <-req.Context().Done():
			closeRequestBody(req)
			return nil, req.Context().Err()
		}
	}
}

// WrappedRoundTripper lets client-go (utilnet.RoundTripperWrapper) find the transport below.
func (r *gateRoundTripper) WrappedRoundTripper() http.RoundTripper { return r.next }

// CloseIdleConnections lets http.Client.CloseIdleConnections reach the transport below.
func (r *gateRoundTripper) CloseIdleConnections() { closeIdle(r.next) }

// loginGuard sits below client-go's credential plugin: it hands the plugin the first 401 after a
// request that worked, and turns the ones after it into ErrLoginRejected until a request works
// again or ArmLogin is called. While the engine holds the login (HoldLogin, loginhold.go) no 401
// reaches the plugin: each ends as ErrLoginLapsed.
type loginGuard struct {
	armed atomic.Bool
	heldGuard
	ends loginEnds // the plugin's runs Cogpit ended for this connection (loginrun.go, D-73)
}

func newLoginGuard() *loginGuard {
	g := &loginGuard{}
	g.armed.Store(true)
	return g
}

func (g *loginGuard) wrap(next http.RoundTripper) http.RoundTripper {
	return &guardRoundTripper{g: g, next: next}
}

type guardRoundTripper struct {
	g    *loginGuard
	next http.RoundTripper
}

func (r *guardRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		if r.g.held.Load() {
			drainBody(resp)
			return nil, ErrLoginLapsed // held: the plugin does not renew it (loginhold.go)
		}
		if r.g.armed.CompareAndSwap(true, false) {
			return resp, nil // client-go's plugin, if any, renews the login once
		}
		drainBody(resp)
		return nil, ErrLoginRejected
	case resp.StatusCode < http.StatusBadRequest:
		r.g.armed.Store(true)
	}
	return resp, nil
}

func (r *guardRoundTripper) WrappedRoundTripper() http.RoundTripper { return r.next }
func (r *guardRoundTripper) CloseIdleConnections()                  { closeIdle(r.next) }

// drainBody reads what is left of a response the caller never sees, at most 64 KiB, and closes it.
func drainBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

// timeoutRoundTripper bounds a request that is not a stream.
type timeoutRoundTripper struct {
	d      time.Duration
	prefix string
	next   http.RoundTripper
}

// errRequestTimeout is the cause of a request's context that the timeout ended.
var errRequestTimeout = errors.New("request timeout")

func (r *timeoutRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	verb, resource := Classify(req.Method, stripPrefix(req.URL, r.prefix))
	if isStream(req, verb, resource) {
		return r.next.RoundTrip(req)
	}
	d := r.d
	if b, ok := req.Context().Value(requestBoundKey{}).(time.Duration); ok && b > 0 {
		d = b // Connect's first request: its own bound, below the credential plugin (withRequestBound)
	}
	ctx, cancel := context.WithTimeoutCause(req.Context(), d, errRequestTimeout)
	resp, err := r.next.RoundTrip(req.WithContext(ctx))
	if err == nil {
		resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
		return resp, nil
	}
	timedOut := errors.Is(context.Cause(ctx), errRequestTimeout)
	cancel()
	if req.Context().Err() != nil {
		return nil, err // the caller gave up: nothing to add
	}
	if isWrite(verb, resource) && mayHaveBeenSent(err, timedOut) {
		if timedOut {
			err = fmt.Errorf("%w within %s", ErrNoAnswer, d)
		}
		return nil, &OutcomeUnknownError{Verb: verb, Resource: resource, Err: err}
	}
	if timedOut {
		return nil, fmt.Errorf("%w within %s", ErrNoAnswer, d)
	}
	return nil, err
}

// requestBoundKey marks a request whose bound is its context's value, not the request timeout.
type requestBoundKey struct{}

// withRequestBound bounds the request of ctx by d where the request timeout applies: below
// client-go's credential plugin, so that a login that waits for the user does not count against
// it (the plugin's run has its own bound, loginrun.go).
func withRequestBound(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, requestBoundKey{}, d)
}

func (r *timeoutRoundTripper) WrappedRoundTripper() http.RoundTripper { return r.next }
func (r *timeoutRoundTripper) CloseIdleConnections()                  { closeIdle(r.next) }

// isStream reports whether a request is a stream that stays open: a watch, a container's log
// (followed or not: an old log can be long), exec, attach, a port forward, or any upgrade.
func isStream(req *http.Request, verb, resource string) bool {
	if verb == VerbWatch || verb == VerbWatchList || req.Header.Get("Upgrade") != "" {
		return true
	}
	for _, sub := range []string{"/log", "/exec", "/attach", "/portforward"} {
		if strings.HasSuffix(resource, sub) {
			return true
		}
	}
	return false
}

// isWrite reports whether a request changes the cluster. Access and token reviews are POSTs that
// change nothing.
func isWrite(verb, resource string) bool {
	switch verb {
	case VerbCreate, VerbUpdate, VerbPatch, VerbDelete, VerbDeleteCollection:
		return !strings.Contains(resource, ".authorization.k8s.io") && !strings.Contains(resource, ".authentication.k8s.io")
	}
	return false
}

// mayHaveBeenSent reports whether a failed request may have reached the server: not when the
// connection to it could not even be opened, nor when a gate refused it.
func mayHaveBeenSent(err error, timedOut bool) bool {
	if timedOut {
		return true
	}
	if errors.Is(err, ErrInterrupted) || errors.Is(err, ErrLoginExpired) || errors.Is(err, ErrLoginRejected) || errors.Is(err, ErrLoginLapsed) {
		return false
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return false
	}
	if strings.Contains(err.Error(), "TLS handshake") { // net/http's handshake timeout has no type of its own
		return false
	}
	var dns *net.DNSError
	return !errors.As(err, &dns)
}

// cancelOnClose ends a bounded request's context when its body is closed.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func stripPrefix(u *url.URL, prefix string) *url.URL {
	if prefix == "" || u == nil || !strings.HasPrefix(u.Path, prefix+"/") {
		return u
	}
	stripped := *u
	stripped.Path = strings.TrimPrefix(u.Path, prefix)
	return &stripped
}

func closeIdle(rt http.RoundTripper) {
	if ci, ok := rt.(interface{ CloseIdleConnections() }); ok {
		ci.CloseIdleConnections()
	}
}

func closeRequestBody(req *http.Request) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
}

// health is a connection's part of the above: its gate, login guard, the dialer whose connections
// ResetConnections closes, and the client the anonymous probe uses.
type health struct {
	gate   *gate
	guard  *loginGuard
	dialer *connrotation.Dialer
	anon   discovery.DiscoveryInterface // GET /version without credentials (Reachable)
	anonHC *http.Client
}

// installHealth adds the request timeout, the login guard, the inner gate and a dialer that tracks
// its connections to cfg, before the connection's HTTP client is built from it. The counting
// transport must be installed first: the timeout and the gates sit above it.
func installHealth(cfg *rest.Config, prefix string, timeout time.Duration) *health {
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	h := &health{gate: newGate(prefix), guard: newLoginGuard()}
	base := cfg.Dial
	if base == nil {
		base = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	h.dialer = connrotation.NewDialer(base)
	cfg.Dial = h.dialer.DialContext
	cfg.Wrap(func(next http.RoundTripper) http.RoundTripper {
		return &timeoutRoundTripper{d: timeout, prefix: prefix, next: next}
	})
	cfg.Wrap(h.guard.wrap)
	cfg.Wrap(h.gate.inner)
	return h
}

// anonymousProbe builds the client Reachable uses: the connection's server, TLS trust and proxy,
// no credentials, connections of its own.
func (h *health) anonymousProbe(cfg *rest.Config, counter *RequestCounter) error {
	anon := rest.AnonymousClientConfig(cfg)
	anon.Dial = nil
	anon.Wrap(counter.Wrap)
	hc, err := rest.HTTPClientFor(anon)
	if err != nil {
		return err
	}
	d, err := discovery.NewDiscoveryClientForConfigAndClient(anon, hc)
	if err != nil {
		return err
	}
	h.anon, h.anonHC = d, hc
	return nil
}

// Probed reports whether the connection has the probes and the gate (Connect builds them; a bare
// Conn a test makes has none).
func (c *Conn) Probed() bool { return c != nil && c.health != nil && c.health.anon != nil }

// SetHealth tells the connection what the engine knows of it (D-58): healthy lets the requests the
// gate holds go out at once; interrupted or login expired holds lists and watches and refuses the
// others. A connection starts healthy.
func (c *Conn) SetHealth(h Health) {
	if c.health != nil {
		c.health.gate.set(h)
	}
}

// ArmLogin lets the next 401 Unauthorized reach client-go's credential plugin, which then runs once
// to renew the login (the user's Log in again); a plugin whose run Cogpit ended (loginrun.go) may
// run again, and a connection whose login the user cancelled (CancelLogin) asks for runs again.
func (c *Conn) ArmLogin() {
	if c.health != nil {
		c.health.guard.armed.Store(true)
	}
	if c.hold != nil {
		c.hold.p.rearm(c.hold.g)
	}
}

// ResetConnections closes every network connection of the connection's clients, those in use
// included: a request on a dead connection (after the computer slept) ends at once instead of
// after the operating system gives up. Watches and streams end with an error and are opened again.
func (c *Conn) ResetConnections() {
	if c.health != nil {
		c.health.dialer.CloseAll()
	}
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
}

// Heartbeat asks the API server for its version with the connection's credentials, bounded by
// ctx: nil when it answers, an error wrapping ErrLoginRejected when it rejects the login, and
// ErrLoginLapsed when it needed the credential plugin while the login is held (HoldLogin). It
// passes the gates, and it may make client-go's credential plugin run (a login that expired by its
// time, or a 401 the guard hands on): the call then returns only when the plugin has, or when Cogpit
// ended its run (its bound, a cancel: a *LoginEndedError, loginrun.go).
func (c *Conn) Heartbeat(ctx context.Context) error {
	if c == nil || c.disco == nil {
		return errors.New("not connected")
	}
	mark, lapses, ended := c.stderr.mark(), c.lapseMark(), c.loginEndMark()
	_, err := discovery.ToDiscoveryInterfaceWithContext(c.disco).ServerVersionWithContext(withProbe(ctx))
	if err != nil && (errors.Is(err, ErrLoginLapsed) || c.lapsedSince(lapses)) {
		c.stderr.discard() // the plugin did not run: nothing of it to keep
		return ErrLoginLapsed
	}
	if err != nil {
		if e := c.loginEndedSince(ended); e != nil {
			return endedWithStderr(err, e, c.stderr, mark, c.secrets)
		}
	}
	// A login the plugin failed to renew carries the plugin's standard error (pluginstderr.go).
	return probeError(withPluginStderr(err, c.stderr, mark, c.secrets))
}

// Reachable asks the API server for its version without credentials, on connections of its own,
// bounded by ctx: nil when the server answers at all (a 401 or 403 of a cluster that refuses
// anonymous requests included). It never runs a credential plugin.
func (c *Conn) Reachable(ctx context.Context) error {
	if c == nil || c.health == nil || c.health.anon == nil {
		return errors.New("not connected")
	}
	c.health.anonHC.CloseIdleConnections() // a connection the network lost meanwhile would only time out
	_, err := discovery.ToDiscoveryInterfaceWithContext(c.health.anon).ServerVersionWithContext(ctx)
	if err == nil || apierrors.IsUnauthorized(err) || apierrors.IsForbidden(err) {
		return nil
	}
	return err
}

// probeError marks a rejected login.
func probeError(err error) error {
	switch {
	case err == nil:
		return nil
	case apierrors.IsUnauthorized(err) && !errors.Is(err, ErrLoginRejected):
		return fmt.Errorf("%w: %v", ErrLoginRejected, err)
	}
	return err
}
