package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"

	"github.com/guided-traffic/cogpit/internal/kubeconfig"
)

// The diagnostics log's sources in the connection (D-133): a round-tripper that notes the requests
// that went wrong, and the rate limits that note the requests that waited at them. Both see only
// what the connection does anyway: they send nothing of their own.

// diagRoundTripper notes in the diagnostics log every request the API server refused, asked to
// slow down or failed, every one without an answer or broken on the way, and every one that is not a
// stream and took SlowRequest or more. It sits above the request timeout, so that a request without
// an answer reaches it as ErrNoAnswer.
type diagRoundTripper struct {
	log    *DiagLog
	prefix string
	next   http.RoundTripper
	now    func() time.Time
}

// installDiag adds the diagnostics round-tripper to cfg's transports, above the request timeout and
// the gates (installHealth comes first).
func installDiag(cfg *rest.Config, log *DiagLog, prefix string) {
	cfg.Wrap(func(next http.RoundTripper) http.RoundTripper {
		return &diagRoundTripper{log: log, prefix: prefix, next: next, now: time.Now}
	})
}

func (d *diagRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	start := d.now()
	resp, err := d.next.RoundTrip(req)
	took := d.now().Sub(start)
	if req.Context().Err() != nil && err != nil {
		return resp, err // the caller gave up: nothing about the cluster
	}
	d.observe(req, resp, err, took)
	return resp, err
}

func (d *diagRoundTripper) WrappedRoundTripper() http.RoundTripper { return d.next }
func (d *diagRoundTripper) CloseIdleConnections()                  { closeIdle(d.next) }

// observe notes one request's outcome when it is a problem.
func (d *diagRoundTripper) observe(req *http.Request, resp *http.Response, err error, took time.Duration) {
	u := stripPrefix(req.URL, d.prefix)
	verb, resource := Classify(req.Method, u)
	e := DiagEntry{What: describeRequest(verb, resource, u), Method: req.Method, URL: safeURL(req.URL), Took: took}
	switch {
	case err != nil:
		if errors.Is(err, ErrInterrupted) || errors.Is(err, ErrLoginExpired) || errors.Is(err, ErrLoginLapsed) {
			return // the gate's refusal: the engine knows the connection's state already
		}
		e.Kind, e.Text = DiagNetwork, kubeconfig.SafeError(err).Error()
		switch {
		case errors.Is(err, ErrNoAnswer):
			e.Kind = DiagTimeout
		case errors.Is(err, ErrLoginRejected):
			e.Kind, e.Status = DiagUnauthorized, http.StatusUnauthorized
		}
	case resp == nil:
		return
	case resp.StatusCode == http.StatusUnauthorized:
		e.Kind = DiagUnauthorized
	case resp.StatusCode == http.StatusForbidden:
		e.Kind = DiagRefused
	case resp.StatusCode == http.StatusTooManyRequests:
		e.Kind, e.RetryAfter = DiagTooMany, resp.Header.Get("Retry-After")
	case resp.StatusCode >= 500:
		e.Kind = DiagServerError
	case took >= SlowRequest && !isStream(req, verb, resource):
		e.Kind, e.Status = DiagSlow, resp.StatusCode
	default:
		return
	}
	if resp != nil && e.Kind != DiagSlow {
		e.Status = resp.StatusCode
		e.Text = statusMessage(resp, resource)
	}
	if e.Kind == DiagTooMany && e.Text == cacheWarmingMessage {
		e.Kind, e.Text = DiagWarming, cacheWarmingText
	}
	d.log.Add(e)
}

// cacheWarmingMessage is the API server's message on a 429 while its cache of the resource is
// being built (k8s.io/apiserver's cacher, ready.go); with ": <error>" after it, the cache is being
// built again after an error, which stays a 429 line of its own.
const cacheWarmingMessage = "storage is (re)initializing"

// cacheWarmingText is a cache warming up's line (DiagWarming).
const cacheWarmingText = cacheWarmingMessage + ": the API server was still building its cache of this resource, " +
	"as it does at the first request for a custom resource and for every resource after it starts; client-go asks again after the Retry-After"

// statusMessage reads the message of an error answer (a Status object's, else the start of the
// body) and puts the body back for the caller. A Secret's answer is not read: an admission webhook
// may quote what it refused (H-8).
func statusMessage(resp *http.Response, resource string) string {
	if strings.HasPrefix(resource, "secrets") || resp.Body == nil {
		return http.StatusText(resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(body), resp.Body), resp.Body}
	if err != nil || len(body) == 0 {
		return http.StatusText(resp.StatusCode)
	}
	var st struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &st) == nil && st.Kind == "Status" && st.Message != "" {
		return st.Message
	}
	return http.StatusText(resp.StatusCode) + ": " + string(body)
}

// describeRequest names a request kubectl's way: its verb and resource, with the object's name and
// the namespace: "list pods in team-a", "get configmaps/kube-root-ca.crt in team-a", "list pods"
// (in every namespace, or a cluster-scoped kind: the path does not tell them apart); discovery and
// other paths by their path.
func describeRequest(verb, resource string, u *url.URL) string {
	if resource == "" {
		if u == nil {
			return verb
		}
		return verb + " " + u.Path
	}
	ns, name := namespaceAndName(u)
	out := verb + " " + resource
	if name != "" {
		base, sub, _ := strings.Cut(resource, "/")
		out = verb + " " + base + "/" + name
		if sub != "" {
			out += " (" + sub + ")"
		}
	}
	if ns != "" {
		out += " in " + ns
	}
	return out
}

// namespaceAndName reads the namespace and the object's name from an API path, as Classify does.
func namespaceAndName(u *url.URL) (namespace, name string) {
	if u == nil {
		return "", ""
	}
	parts := splitPath(u.Path)
	switch {
	case len(parts) > 2 && parts[0] == "api":
		parts = parts[2:]
	case len(parts) > 3 && parts[0] == "apis":
		parts = parts[3:]
	default:
		return "", ""
	}
	if len(parts) > 0 && parts[0] == watchSegment {
		parts = parts[1:]
	}
	if len(parts) > 2 && parts[0] == "namespaces" && !namespaceSubresources[parts[2]] {
		namespace, parts = parts[1], parts[2:]
	}
	if len(parts) > 1 {
		name = parts[1]
	}
	return namespace, name
}

// safeURL is a request's URL without user information.
func safeURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	c := *u
	c.User = nil
	return c.String()
}

// BudgetStats is one of a connection's rate limits and what waited at it since the connection
// began: the requests (lists, gets, writes; watches never wait), discovery, the access reviews.
type BudgetStats struct {
	Name  string  `json:"name"`
	QPS   float32 `json:"qps"`
	Burst int     `json:"burst"`
	// Requests counts the requests that passed it; Delayed those that waited for a token, Waited
	// the time they waited together, Longest the longest wait, Waiting those that wait now.
	Requests int64         `json:"requests"`
	Delayed  int64         `json:"delayed"`
	Waited   time.Duration `json:"waited"`
	Longest  time.Duration `json:"longest"`
	Waiting  int64         `json:"waiting"`
}

// Rate limits by name (BudgetStats.Name).
const (
	BudgetRequests  = "requests"
	BudgetDiscovery = "discovery"
	BudgetReviews   = "access reviews"
)

// delayedFrom is the wait from which a request counts as delayed: less is the token bucket's own
// bookkeeping.
const delayedFrom = 10 * time.Millisecond

// measuredLimiter is a connection's token bucket that counts what waits at it and notes in the
// diagnostics log a request that waited ThrottleNote or more, under the name its context carries
// (WithRequestLabel). client-go hands Wait the request's own context.
type measuredLimiter struct {
	flowcontrol.RateLimiter
	log     *DiagLog
	name    string
	burst   int
	waiting atomic.Int64
	now     func() time.Time

	mu    sync.Mutex
	stats BudgetStats
}

func newMeasuredLimiter(name string, qps float32, burst int, log *DiagLog) *measuredLimiter {
	return &measuredLimiter{RateLimiter: flowcontrol.NewTokenBucketRateLimiter(qps, burst), log: log, name: name, burst: burst,
		now: time.Now, stats: BudgetStats{Name: name, QPS: qps, Burst: burst}}
}

// Wait implements flowcontrol.RateLimiter.
func (m *measuredLimiter) Wait(ctx context.Context) error {
	m.waiting.Add(1)
	start := m.now()
	err := m.RateLimiter.Wait(ctx)
	m.waiting.Add(-1)
	m.note(ctx, m.now().Sub(start))
	return err
}

// Accept implements flowcontrol.RateLimiter.
func (m *measuredLimiter) Accept() {
	m.waiting.Add(1)
	start := m.now()
	m.RateLimiter.Accept()
	m.waiting.Add(-1)
	m.note(context.Background(), m.now().Sub(start))
}

func (m *measuredLimiter) note(ctx context.Context, waited time.Duration) {
	m.mu.Lock()
	m.stats.Requests++
	if waited >= delayedFrom {
		m.stats.Delayed++
		m.stats.Waited += waited
		m.stats.Longest = max(m.stats.Longest, waited)
	}
	m.mu.Unlock()
	if waited < ThrottleNote {
		return
	}
	what := requestLabel(ctx)
	if what == "" {
		what = "a request"
	}
	m.log.Add(DiagEntry{Kind: DiagThrottled, What: what, Took: waited,
		Text: fmt.Sprintf("Cogpit's rate limit for %s: %s per second, burst %d", m.name, formatQPS(m.stats.QPS), m.burst)})
}

// Stats returns the limit and what waited at it.
func (m *measuredLimiter) Stats() BudgetStats {
	if m == nil {
		return BudgetStats{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stats
	st.Waiting = m.waiting.Load()
	return st
}

// formatQPS writes a rate without a needless fraction: "5", "2.5".
func formatQPS(q float32) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", q), "0"), ".")
}
