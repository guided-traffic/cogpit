package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The connection test (the owner's requirement of 2026-10-14 late evening): each layer on its own,
// in order, never with cluster data. DNS (the owner's VW8, 2026-10-23): the host name resolves, its
// addresses listed; an IP address or localhost passes at once. TCP: one of those addresses answers
// on the port. HTTP: an HTTP or HTTPS answer from the API (any status passes, 401 too: the server is
// there), and over HTTPS the certificate Go verified, its chain and host name, named with its issuer
// and end. Auth: the models list asked without the key, so the step says whether the endpoint
// wants one, then with the key where one is given. Models: how many the list holds and whether the
// chosen model is among them; the list goes to the settings form. Tools: a minimal chat request
// offering one harmless tool (get_time) and asking for it returns a tool call. (HTTP's certificate,
// Auth's two questions and Models: the owner's request of 2026-10-25.) A failed layer leaves the
// later ones not reached. Each layer has its own bound. With a proxy configured for the endpoint
// (Go's default transport: HTTPS_PROXY, HTTP_PROXY, NO_PROXY), DNS and TCP check the proxy, where
// the chat's requests go; the proxy resolves the endpoint's name.

// Bounds of the test's layers. A local model can take half a minute and more to load for the first
// request, so the tool layer waits longest.
var (
	testDNSTimeout    = 5 * time.Second
	testTCPTimeout    = 5 * time.Second
	testHTTPTimeout   = 10 * time.Second
	testAuthTimeout   = 15 * time.Second
	testModelsTimeout = 5 * time.Second
	testToolsTimeout  = 120 * time.Second
)

// testToolName is the harmless tool of the Tools layer.
const testToolName = "get_time"

// TestConnection implements engine.ChatAgent.
func (a *Agent) TestConnection(ctx context.Context, c engine.ChatConnection, apiKey *string, progress func([]engine.ChatTestStep)) []engine.ChatTestStep {
	steps := make([]engine.ChatTestStep, len(engine.ChatTestLayers))
	for i, l := range engine.ChatTestLayers {
		steps[i] = engine.ChatTestStep{Layer: l, State: engine.ChatTestPending}
	}
	report := func() {
		if progress != nil {
			progress(slices.Clone(steps))
		}
	}
	report()
	fail := func(i int, detail string) []engine.ChatTestStep {
		steps[i].State, steps[i].Detail = engine.ChatTestFail, detail
		for j := i + 1; j < len(steps); j++ {
			steps[j].State = engine.ChatTestNotReached
		}
		report()
		return slices.Clone(steps)
	}

	ep, err := endpointFor(c.Kind, c.BaseURL)
	if err != nil {
		return fail(0, err.Error())
	}
	if !validKind(c.Kind) {
		return fail(0, "unknown connection kind")
	}
	key, err := a.testKey(c, apiKey)
	if err != nil {
		return fail(0, err.Error())
	}
	if _, err := normalize(c); errors.Is(err, ErrCAFileScheme) || errors.Is(err, ErrCAFilePath) {
		return fail(0, err.Error())
	}
	target, err := a.dialTargetFor(ep)
	if err != nil {
		return fail(0, err.Error())
	}
	// The CA file is read now; a file that cannot be used fails the TLS layer, after TCP.
	cl, clErr := a.clientFor(c, key)
	var addrs []netip.Addr // the DNS layer's, which the TCP layer dials
	var models []string    // the Auth layer's answer, which the Models layer reads
	var httpWarn string    // the HTTP layer's: an address of the name that does not answer as the API
	layers := []func(context.Context) (string, error){
		func(ctx context.Context) (detail string, err error) {
			addrs, detail, err = testDNS(ctx, target)
			return detail, err
		},
		func(ctx context.Context) (string, error) { return testTCP(ctx, target, addrs) },
		func(ctx context.Context) (detail string, err error) {
			if clErr != nil {
				return "", clErr
			}
			detail, httpWarn, err = a.testHTTP(ctx, cl, c.CAFile != "", target, addrs)
			return detail, err
		},
		func(ctx context.Context) (detail string, err error) {
			models, detail, err = testAuth(ctx, cl)
			return detail, err
		},
		func(context.Context) (string, error) { return testModels(models, c.Model) },
		func(ctx context.Context) (string, error) { return testTools(ctx, cl, c.Model) },
	}
	bounds := []time.Duration{testDNSTimeout, testTCPTimeout, testHTTPTimeout, testAuthTimeout, testModelsTimeout, testToolsTimeout}
	for i, run := range layers {
		steps[i].State = engine.ChatTestRunning
		report()
		start := time.Now()
		detail, err := a.testLayer(ctx, bounds[i], run)
		steps[i].Duration = time.Since(start).Round(time.Millisecond)
		if err != nil {
			return fail(i, err.Error())
		}
		steps[i].State, steps[i].Detail = engine.ChatTestPass, detail
		switch engine.ChatTestLayers[i] {
		case engine.ChatTestHTTP:
			warnings := []string{httpWarn}
			if ep.plainRemote {
				warnings = append(warnings, engine.ChatPlainHTTPWarning) // the owner's decision of 2026-10-15 (H-95)
			}
			steps[i].Warning = strings.TrimSpace(strings.Join(warnings, " "))
		case engine.ChatTestModels:
			steps[i].Models = slices.Clone(models)
			if len(models) == 0 {
				steps[i].Warning = noModelsWarning
			}
		}
		report()
	}
	return slices.Clone(steps)
}

// testLayer runs one layer within its bound, a panic turned into a failure.
func (a *Agent) testLayer(ctx context.Context, bound time.Duration, run func(context.Context) (string, error)) (detail string, err error) {
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			a.log.Error("connection test panicked", "panic", fmt.Sprint(r))
			detail, err = "", errors.New("internal error in Cogpit")
		}
	}()
	detail, err = run(ctx)
	if err != nil && errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
		err = fmt.Errorf("no answer within %s", bound)
	}
	return detail, err
}

// testKey is the key the test uses: the given one, else the stored one of c.ID.
func (a *Agent) testKey(c engine.ChatConnection, apiKey *string) (string, error) {
	if apiKey != nil {
		return *apiKey, nil
	}
	if c.ID == "" {
		return "", nil
	}
	a.mu.Lock()
	i := slices.IndexFunc(a.conns, func(x engine.ChatConnection) bool { return x.ID == c.ID })
	var stored engine.ChatConnection
	if i >= 0 {
		stored = a.conns[i]
	}
	a.mu.Unlock()
	if i < 0 {
		return "", nil
	}
	return a.keyFor(stored)
}

// dialTarget is where the chat's connections go, so where the DNS and TCP layers look: the
// endpoint's host and port, or the proxy's when one is configured for the endpoint.
type dialTarget struct {
	host, port string
	// proxied names the endpoint's host when the target is a proxy, which resolves it.
	proxied string
}

// String is how the steps name the target: "llm.example.com:443", "the proxy proxy.example:3128".
func (t dialTarget) String() string {
	hp := net.JoinHostPort(t.host, t.port)
	if t.proxied != "" {
		return "the proxy " + hp
	}
	return hp
}

// proxyPorts are the proxy schemes' default ports, as Go's transport fills them in.
var proxyPorts = map[string]string{"http": "80", "https": "443", "socks5": "1080", "socks5h": "1080"}

// dialTargetFor is the target of ep: the proxy the agent's transport picks for the models URL (Go's
// default: the environment's HTTPS_PROXY, HTTP_PROXY and NO_PROXY, never for localhost or a loopback
// address), else the endpoint itself, the scheme's default port filled in. A proxy's user and
// password never reach a step.
func (a *Agent) dialTargetFor(ep endpoint) (dialTarget, error) {
	u, err := url.Parse(ep.models)
	if err != nil {
		return dialTarget{}, errBaseURL
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == schemeHTTPS {
			port = "443"
		}
	}
	direct := dialTarget{host: u.Hostname(), port: port}
	rt := a.hc.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	tr, ok := rt.(*http.Transport)
	if !ok || tr.Proxy == nil {
		return direct, nil
	}
	pu, err := tr.Proxy(&http.Request{Method: http.MethodGet, URL: u, Host: u.Host, Header: http.Header{}})
	if err != nil {
		// Not the error's text: it quotes the proxy's URL, which may carry a password.
		return dialTarget{}, errors.New("the proxy configured for this endpoint (HTTPS_PROXY or HTTP_PROXY) is not a valid URL")
	}
	if pu == nil {
		return direct, nil
	}
	pport := pu.Port()
	if pport == "" {
		pport = proxyPorts[strings.ToLower(pu.Scheme)]
	}
	return dialTarget{host: pu.Hostname(), port: pport, proxied: direct.host}, nil
}

// lookupNetIP resolves a name with the system's resolver; tests route it to a fake one.
var lookupNetIP = net.DefaultResolver.LookupNetIP

// maxShownAddrs is how many addresses a step lists; more are counted.
const maxShownAddrs = 4

// isLocalhost reports whether host is localhost, as engine.ChatPlainHTTPRemote takes it.
func isLocalhost(host string) bool {
	return strings.EqualFold(strings.TrimSuffix(host, "."), "localhost")
}

// testDNS resolves the target's host and lists its addresses, IPv4 and IPv6 in the resolver's order
// (the order Go's dialer tries them in). An IP address and localhost (127.0.0.1, then ::1) pass at
// once without a lookup. The resolver does not say which server or file answered, so neither does
// the step.
func testDNS(ctx context.Context, t dialTarget) ([]netip.Addr, string, error) {
	suffix := ""
	if t.proxied != "" {
		suffix = "; the proxy resolves " + t.proxied
	}
	if ip, err := netip.ParseAddr(t.host); err == nil {
		return []netip.Addr{ip.Unmap()}, describeHost(t) + " is an address: nothing to resolve" + suffix, nil
	}
	if isLocalhost(t.host) {
		addrs := []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.IPv6Loopback()}
		return addrs, describeHost(t) + " is this computer (" + listAddrs(addrs) + "): nothing to resolve" + suffix, nil
	}
	found, err := lookupNetIP(ctx, "ip", t.host)
	if err != nil {
		var dnsErr *net.DNSError
		switch {
		case ctx.Err() != nil:
			return nil, "", ctx.Err()
		case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
			return nil, "", fmt.Errorf("%s: the name is not known", describeHost(t))
		case errors.As(err, &dnsErr) && dnsErr.IsTimeout:
			return nil, "", fmt.Errorf("%s: the resolver did not answer", describeHost(t))
		case errors.As(err, &dnsErr):
			return nil, "", fmt.Errorf("%s: %s", describeHost(t), dnsErr.Err)
		}
		return nil, "", fmt.Errorf("%s: %s", describeHost(t), shortNetError(err))
	}
	var addrs []netip.Addr
	for _, ip := range found {
		if ip = ip.Unmap(); !slices.Contains(addrs, ip) {
			addrs = append(addrs, ip)
		}
	}
	if len(addrs) == 0 {
		return nil, "", fmt.Errorf("%s: the name has no addresses", describeHost(t))
	}
	return addrs, describeHost(t) + ": " + listAddrs(addrs) + suffix, nil
}

// describeHost names the target's host without its port: "llm.example.com", "the proxy proxy.example".
func describeHost(t dialTarget) string {
	if t.proxied != "" {
		return "the proxy " + t.host
	}
	return t.host
}

// listAddrs lists up to maxShownAddrs addresses and counts the rest.
func listAddrs(addrs []netip.Addr) string {
	var b strings.Builder
	for i, ip := range addrs {
		if i == maxShownAddrs {
			fmt.Fprintf(&b, " and %d more", len(addrs)-i)
			break
		}
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(ip.String())
	}
	return b.String()
}

// dialTCP opens the TCP layer's connection; tests route it to a fake endpoint.
var dialTCP = (&net.Dialer{}).DialContext

// minAddrDial is the least time one address gets while time is left, Go's dialer's own minimum.
const minAddrDial = 2 * time.Second

// testTCP dials the addresses the DNS layer found, one after another in its order, on the target's
// port, until one answers; each address gets an equal share of the time left (at least
// minAddrDial), as Go's dialer gives it, without its Happy Eyeballs race of the second family. The
// name stays the HTTP layer's: its TLS handshake and Host header carry the name, not the address.
func testTCP(ctx context.Context, t dialTarget, addrs []netip.Addr) (string, error) {
	var failed []string
	for i, ip := range addrs {
		dctx, cancel := addrContext(ctx, len(addrs)-i)
		addr := net.JoinHostPort(ip.String(), t.port)
		conn, err := dialTCP(dctx, "tcp", addr)
		timedOut := dctx.Err() != nil
		cancel()
		if err == nil {
			_ = conn.Close()
			return answered(t, ip, failed), nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		reason := shortNetError(err)
		if timedOut || errors.Is(err, context.DeadlineExceeded) {
			reason = "no answer"
		}
		failed = append(failed, ip.String()+" "+reason)
	}
	if len(failed) == 1 && sameHost(t, addrs[0]) {
		return "", fmt.Errorf("%s: %s", t, strings.TrimPrefix(failed[0], addrs[0].String()+" "))
	}
	return "", fmt.Errorf("%s: %s", t, strings.Join(failed, "; "))
}

// addrContext bounds one address's dial: the time left shared by the addresses left.
func addrContext(ctx context.Context, left int) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return context.WithCancel(ctx)
	}
	remaining := time.Until(deadline)
	share := remaining / time.Duration(left)
	if share < minAddrDial {
		share = min(minAddrDial, remaining)
	}
	return context.WithTimeout(ctx, share)
}

// answered is the TCP layer's detail: the target answers, at which address when it was a name, and
// the addresses that failed before.
func answered(t dialTarget, ip netip.Addr, failed []string) string {
	detail := t.String() + " answers"
	if !sameHost(t, ip) {
		detail += " at " + ip.String()
	}
	if len(failed) > 0 {
		detail += " (" + strings.Join(failed, "; ") + ")"
	}
	return detail
}

// sameHost reports whether the target's host is the address itself (an IP address in the URL).
func sameHost(t dialTarget, ip netip.Addr) bool {
	h, err := netip.ParseAddr(t.host)
	return err == nil && h.Unmap() == ip
}

// shortNetError is the last part of a dial error ("connection refused").
func shortNetError(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}

// The HTTP layer asks the models URL without the key. It names the URL and the address the
// chat's own client reached, and when the name has several addresses it asks each of them, so that
// one broken address behind a name shows as what it is (the owner's case of 2026-10-25: three
// ingress addresses, one whose proxy had no server behind it and answered 503 every time, as Go
// takes the first address of its fixed order, while curl happened to reach the others). An answer
// of 5xx at the chat's address fails the layer: the API does not answer there; one at another
// address passes with a warning. maxAddressProbes bounds how many addresses are asked.
const maxAddressProbes = 4

// addressAnswer is one address's answer to the models URL.
type addressAnswer struct {
	ip     netip.Addr
	status int    // the HTTP status, 0 when none came
	text   string // what the answer says: an error's reason, a 5xx's page or message
}

func (a addressAnswer) ok() bool { return a.status > 0 && a.status < http.StatusInternalServerError }

// String is how the step names it: "10.18.15.12 HTTP 401", "10.18.15.11 HTTP 503 (no server …)".
func (a addressAnswer) String() string {
	if a.status == 0 {
		return a.ip.String() + " " + a.text
	}
	s := fmt.Sprintf("%s HTTP %d", a.ip, a.status)
	if !a.ok() && a.text != "" {
		s += " (" + a.text + ")"
	}
	return s
}

func (a *Agent) testHTTP(ctx context.Context, cl *client, caFile bool, t dialTarget, addrs []netip.Addr) (detail, warning string, err error) {
	var used netip.Addr // the address the chat's client reached
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
		if ap, err := netip.ParseAddrPort(info.Conn.RemoteAddr().String()); err == nil {
			used = ap.Addr().Unmap()
		}
	}}
	resp, err := cl.get(httptrace.WithClientTrace(ctx, trace), cl.ep.models, false)
	if err != nil {
		return "", "", err
	}
	first := answerOf(resp, used, cl.key)
	scheme := "HTTP, no TLS"
	if resp.TLS != nil {
		scheme = fmt.Sprintf("HTTPS, %s, %s", tlsVersion(resp.TLS.Version), certificateOf(resp.TLS, caFile, resp.Request.URL.Hostname()))
	}
	server := ""
	if v := resp.Header.Get("Server"); v != "" {
		server = ", answered by " + serverMessage([]byte(v), cl.key)
	}
	answers := []addressAnswer{first}
	if t.proxied == "" && slices.Contains(addrs, used) { // the chat's address one the DNS layer listed
		for _, ip := range addrs {
			if ip == used || len(answers) == maxAddressProbes {
				continue
			}
			answers = append(answers, a.probeAddress(ctx, cl, ip, t.port))
		}
	}
	at := ""
	if used.IsValid() {
		at = " at " + used.String()
	}
	var others, broken []string
	for _, x := range answers[1:] {
		others = append(others, x.String())
		if !x.ok() {
			broken = append(broken, x.String())
		}
	}
	if !first.ok() {
		msg := fmt.Sprintf("GET %s%s: HTTP %d %s, the API does not answer there", cl.ep.models, at, first.status, http.StatusText(first.status))
		if first.text != "" {
			msg += " (" + first.text + ")"
		}
		if len(others) > 0 {
			msg += "; the name's other addresses: " + strings.Join(others, ", ") +
				". The chat's requests go to " + used.String() + ", the first in Go's order of the name's addresses"
		}
		return "", "", errors.New(msg)
	}
	detail = fmt.Sprintf("%s; GET %s%s: HTTP %d%s", scheme, cl.ep.models, at, first.status, server)
	if len(others) > 0 {
		detail += "; the name's other addresses: " + strings.Join(others, ", ")
	}
	if cl.ep.plainRemote {
		detail += "; plain http to another machine" // TestConnection adds the warning
	} else if resp.TLS == nil && cl.key != "" {
		detail += " (the API key travels unencrypted)"
	}
	if len(broken) > 0 {
		warning = "Not every address of the name answers as the API: " + strings.Join(broken, ", ") +
			". A request that lands there fails; the chat's own go to " + used.String() + "."
	}
	return detail, warning, nil
}

// answerOf reads an answer of the models URL: its status and, for a 5xx, what its page says.
func answerOf(resp *http.Response, ip netip.Addr, key string) addressAnswer {
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxModelsBytes))
	x := addressAnswer{ip: ip, status: resp.StatusCode}
	if resp.StatusCode >= http.StatusInternalServerError {
		x.text = pageText(body, key)
	}
	return x
}

// htmlTag is a tag of an HTML page, dropped from the text a step quotes.
var htmlTag = regexp.MustCompile(`<[^>]*>`)

// pageText is what an error answer says in one line: an API's message (serverMessage), or the text
// of an HTML page without its tags, said to come from a proxy or load balancer in front of the API.
func pageText(body []byte, key string) string {
	trimmed := strings.TrimSpace(string(body))
	if !strings.HasPrefix(trimmed, "<") {
		return serverMessage(body, key)
	}
	text := strings.Join(strings.Fields(htmlTag.ReplaceAllString(trimmed, " ")), " ")
	for _, prefix := range []string{"502 Bad Gateway", "503 Service Unavailable", "504 Gateway Time-out", "504 Gateway Timeout", "500 Internal Server Error"} {
		text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
	}
	if text == "" {
		return "an HTML page, from a proxy or load balancer in front of the API"
	}
	return "an HTML page, from a proxy or load balancer in front of the API: " + strconv.Quote(serverMessage([]byte(text), key))
}

// probeAddress asks the models URL at one address of the name: the name in the TLS handshake and
// the Host header, the address dialled; no proxy (a proxied target is not probed).
func (a *Agent) probeAddress(ctx context.Context, cl *client, ip netip.Addr, port string) addressAnswer {
	rt := cl.hc.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	tr, ok := rt.(*http.Transport)
	if !ok {
		return addressAnswer{ip: ip, text: "not asked: not Cogpit's own transport"}
	}
	tr = tr.Clone()
	tr.Proxy = nil
	addr := net.JoinHostPort(ip.String(), port)
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) { return dialTCP(ctx, network, addr) }
	defer tr.CloseIdleConnections()
	probe := *cl
	probe.hc = &http.Client{Transport: tr, CheckRedirect: cl.hc.CheckRedirect}
	pctx, cancel := context.WithTimeout(ctx, minAddrDial+time.Second)
	defer cancel()
	resp, err := probe.get(pctx, cl.ep.models, false)
	if err != nil {
		return addressAnswer{ip: ip, text: shortNetError(err)}
	}
	return answerOf(resp, ip, cl.key)
}

// certificateOf says what Go verified of the endpoint's certificate: its chain to a trusted root
// (the system's, or the connection's CA file) and its host name, Go never being told to skip either
// (the owner's "Passt TLS Full Auth?"); with the certificate's issuer and the day it ends.
func certificateOf(st *tls.ConnectionState, caFile bool, host string) string {
	trust := "the system's roots"
	if caFile {
		trust = "the CA file"
	}
	if len(st.VerifiedChains) == 0 || len(st.PeerCertificates) == 0 {
		return "the certificate not verified" // Go verifies every handshake of the chat's client; kept for the case it did not
	}
	leaf := st.PeerCertificates[0]
	issuer := leaf.Issuer.CommonName
	if issuer == "" && len(leaf.Issuer.Organization) > 0 {
		issuer = leaf.Issuer.Organization[0]
	}
	if issuer == "" {
		issuer = "an unnamed issuer"
	}
	return fmt.Sprintf("certificate verified (chain to %s, host name %s), issued by %s, valid until %s",
		trust, host, issuer, leaf.NotAfter.UTC().Format(time.DateOnly))
}

func tlsVersion(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	}
	return "TLS " + tls.VersionName(v)
}

// testAuth asks for the models list without the key, so that the step can say whether the endpoint
// wants one (the owner's "will der Endpoint auth? Wenn ja, anzeigen. Wenn nein: endpoint geht auch
// ohne auth anzeigen"), then with the key where one is given; it returns the list it was given.
func testAuth(ctx context.Context, cl *client) ([]string, string, error) {
	open := *cl
	open.key = ""
	models, err := open.listModels(ctx)
	var he *HTTPError
	switch {
	case err == nil:
		if cl.key == "" {
			return models, "the endpoint answers without a key", nil
		}
		if _, err := cl.listModels(ctx); err != nil {
			return nil, "", fmt.Errorf("the endpoint answers without a key but not with this one: %w", err)
		}
		return models, "the endpoint answers without a key; the key given is accepted too", nil
	case errors.As(err, &he) && he.Status == http.StatusNotFound:
		return nil, "", fmt.Errorf("no models list at %s (HTTP 404): check the base URL and the kind", cl.ep.models)
	case errors.As(err, &he) && (he.Status == http.StatusUnauthorized || he.Status == http.StatusForbidden):
		wants := fmt.Sprintf("the endpoint wants a key (HTTP %d without one)", he.Status)
		if cl.key == "" {
			return nil, "", errors.New(wants + "; none is given")
		}
		models, err := cl.listModels(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("%s; with the key given: %w", wants, err)
		}
		return models, wants + "; the key given is accepted", nil
	}
	return nil, "", err
}

// noModelsWarning is the Models layer's warning when the list is empty.
const noModelsWarning = "The endpoint lists no models: type the model's name as the server knows it."

// testModels says how many models the list holds and whether the chosen one is among them.
func testModels(models []string, model string) (string, error) {
	detail := pluralModels(len(models)) + " found"
	switch {
	case model == "":
		detail += "; none chosen yet"
	case slices.Contains(models, model):
		detail += fmt.Sprintf("; %q among them", model)
	case len(models) > 0:
		detail += fmt.Sprintf("; %q is not among them", model)
	}
	return detail, nil
}

// pluralModels is "1 model" or "n models".
func pluralModels(n int) string {
	if n == 1 {
		return "1 model"
	}
	return fmt.Sprintf("%d models", n)
}

func testTools(ctx context.Context, cl *client, model string) (string, error) {
	if model == "" {
		return "", errors.New("no model chosen")
	}
	req := chatRequest{
		Model: model,
		Messages: []message{
			{Role: "system", Content: "You are testing a tool connection. Call the get_time tool; do not answer in text."},
			{Role: "user", Content: "What time is it? Use the get_time tool."},
		},
		Tools: []toolDef{{Type: typeFunction, Function: toolDefFunc{Name: testToolName,
			Description: "Returns the current time.", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}}},
		Stream: true,
	}
	body, _ := json.Marshal(req)
	resp, err := cl.complete(ctx, body, nil)
	if err != nil {
		return "", err
	}
	for _, tc := range resp.ToolCalls {
		if tc.Function.Name == testToolName {
			return model + " called " + testToolName, nil
		}
	}
	if len(resp.ToolCalls) > 0 {
		return "", fmt.Errorf("the model called %q instead of %s", resp.ToolCalls[0].Function.Name, testToolName)
	}
	return "", errors.New("the model answered without a tool call: tool calling may be off for this model or this server")
}
