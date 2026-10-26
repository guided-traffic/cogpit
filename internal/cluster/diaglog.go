package cluster

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The diagnostics log of a context (Connection Diagnostics, D-133): what went wrong with the
// requests of its connections that a view does not say on its own: a request the API server
// refused (401, 403), one it asked to slow down (429, its API Priority and Fairness), one it failed
// (5xx), one it answered while its cache of the resource was still being built (a 429 that is no
// problem unless it repeats), one without an answer or that broke on the way, one answered only after SlowRequest, one
// that waited at Cogpit's own rate limit for ThrottleNote or more, a read whose initial list the
// server did not stream, one that took SlowLoad or more to load, and a kind read namespace by
// namespace after a refusal. Kept in memory only, never on disk (the owner, 2026-10-26: gone at
// quit), and bounded (the owner: "Aber Limit einbauen, nicht immer größer werden lassen"): at most
// DiagLogLimit lines, the same problem again (a refused list retried with client-go's backoff)
// folded into its line with a count, the line least recently seen dropped first.
//
// The lines hold real values (the server's address, namespaces, the API server's messages), as
// Cogpit's errors name the request (method and URL); the engine's report replaces the endpoints
// before a report leaves Cogpit. No line holds a credential: URLs lose their user information, and
// the API server's message on a Secret is not kept (H-8).

// DiagKind is what a line of the diagnostics log reports.
type DiagKind string

// The kinds of lines. Every kind but DiagConnect, DiagNamespaced and DiagWarming is a problem
// (DiagEntry.Problem); a DiagWarming line becomes one when it repeats within WarmingRepeatWithin.
const (
	DiagRefused      DiagKind = "refused"         // 403 Forbidden
	DiagUnauthorized DiagKind = "unauthorized"    // 401 Unauthorized
	DiagTooMany      DiagKind = "tooManyRequests" // 429 Too Many Requests
	DiagWarming      DiagKind = "cacheWarming"    // 429 "storage is (re)initializing": the server's cache of the resource was still being built
	DiagServerError  DiagKind = "serverError"     // 5xx
	DiagTimeout      DiagKind = "timeout"         // no answer within the request timeout
	DiagNetwork      DiagKind = "network"         // the request did not reach the server, or its answer broke off
	DiagSlow         DiagKind = "slow"            // answered after SlowRequest or more
	DiagThrottled    DiagKind = "throttled"       // waited ThrottleNote or more at one of Cogpit's rate limits
	DiagFallback     DiagKind = "fallback"        // a read's initial list was not streamed: it was listed instead
	DiagSlowLoad     DiagKind = "slowLoad"        // a read took SlowLoad or more to load
	DiagNamespaced   DiagKind = "namespaced"      // a kind read namespace by namespace after a refusal
	DiagConnect      DiagKind = "connect"         // a connect began
)

// Problem reports whether a line of the kind is a problem the status bar counts. A connect is a
// marker, and a kind read namespace by namespace follows a refusal that counts already. A cache
// warming up is not, on its own (D-new, the owner's option a of 2026-10-26): the API server builds
// its cache of a custom resource at the first request for it, and of every resource after it
// starts, and answers a watch with 429 and Retry-After meanwhile; client-go asks again. Asked again
// and answered the same within WarmingRepeatWithin, the line is a problem (DiagLog.Add): the cache
// did not come up.
func (k DiagKind) Problem() bool { return k != DiagConnect && k != DiagNamespaced && k != DiagWarming }

// Thresholds of the diagnostics log (the lead's, D-133).
const (
	// DiagLogLimit bounds a context's diagnostics log: its lines, each a problem folded with its
	// repeats.
	DiagLogLimit = 500
	// SlowRequest is how long a request that is not a stream may take before it is a slow one.
	SlowRequest = 5 * time.Second
	// ThrottleNote is how long a request may wait at Cogpit's own rate limit before it is noted.
	ThrottleNote = time.Second
	// SlowLoad is how long a read may take to load before it is a slow one.
	SlowLoad = 5 * time.Second
	// WarmingRepeatWithin is how soon a cache warming up may be answered again before its line is a
	// problem: client-go's retries of a 429 come within it, the first request at another API server
	// or after a restart hours later does not.
	WarmingRepeatWithin = time.Minute
	// diagTextLimit bounds one line's text (an API server's message, an error).
	diagTextLimit = 1000
)

// DiagEntry is one line of the diagnostics log: one problem, with how often it happened.
type DiagEntry struct {
	Kind    DiagKind `json:"kind"`
	Problem bool     `json:"problem"`
	// What names what it happened to: a request's verb and resource, kubectl's way, with the name
	// and the namespace ("list pods in team-a", "list pods", "get configmaps/kube-root-ca.crt in team-a",
	// "discovery /apis/metrics.k8s.io/v1beta1"), a read's scope ("pods in team-a"), or the server
	// a connect goes to.
	What string `json:"what"`
	// Text says what happened: the API server's message, the error, the rate limit waited at.
	Text string `json:"text,omitempty"`
	// Method and URL are the request's, the server's address included; empty for a line that is
	// not about one request.
	Method string `json:"method,omitempty"`
	URL    string `json:"url,omitempty"`
	Status int    `json:"status,omitempty"`
	// RetryAfter is a 429's Retry-After header, as the server sent it.
	RetryAfter string `json:"retryAfter,omitempty"`
	// Took is how long the last occurrence took (a slow request, a wait, a load); Longest the
	// longest of all.
	Took    time.Duration `json:"took,omitempty"`
	Longest time.Duration `json:"longest,omitempty"`
	First   time.Time     `json:"first"`
	Last    time.Time     `json:"last"`
	Count   int           `json:"count"`

	key string
}

// DiagLog is a context's diagnostics log. It is safe for concurrent use.
type DiagLog struct {
	mu      sync.Mutex
	lines   []*DiagEntry // least recently seen first
	byKey   map[string]*DiagEntry
	since   time.Time          // the latest connect
	counts  map[DiagKind]int64 // occurrences since the latest connect
	changes uint64             // bumps on every Add: the engine's diagnostics event
	now     func() time.Time
	limit   int
}

// NewDiagLog returns an empty diagnostics log.
func NewDiagLog() *DiagLog {
	return &DiagLog{byKey: map[string]*DiagEntry{}, counts: map[DiagKind]int64{}, now: time.Now, limit: DiagLogLimit}
}

// Connecting marks the begin of a connect to server: the counts since the connect start again
// from here, and the log shows where the connect began.
func (l *DiagLog) Connecting(server string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.since = l.now()
	clear(l.counts)
	l.mu.Unlock()
	l.Add(DiagEntry{Kind: DiagConnect, What: server})
}

// Add records one occurrence of a problem: a new line, or one more of the same line (the same
// kind, what and request; a connect never folds). The line least recently seen goes when the log is
// full.
func (l *DiagLog) Add(e DiagEntry) {
	if l == nil {
		return
	}
	e.Problem = e.Kind.Problem()
	e.Text = clipText(e.Text)
	now := l.now()
	key := diagKey(e)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.changes++
	if old := l.byKey[key]; old != nil && key != "" {
		switch {
		case old.Kind == DiagWarming && !old.Problem && now.Sub(old.Last) <= WarmingRepeatWithin:
			old.Problem = true // asked again soon and still warming: the cache does not come up
			l.counts[old.Kind] += 2
		case old.Problem:
			l.counts[old.Kind]++
		}
		old.Count++
		old.Last = now
		old.Text, old.Status, old.RetryAfter, old.URL, old.Took = e.Text, e.Status, e.RetryAfter, e.URL, e.Took
		old.Longest = max(old.Longest, e.Took)
		if i := slices.Index(l.lines, old); i >= 0 {
			l.lines = append(slices.Delete(l.lines, i, i+1), old)
		}
		return
	}
	if e.Problem {
		l.counts[e.Kind]++
	}
	e.First, e.Last, e.Count, e.Longest, e.key = now, now, 1, e.Took, key
	line := &e
	l.lines = append(l.lines, line)
	if key != "" {
		l.byKey[key] = line
	}
	for len(l.lines) > l.limit {
		gone := l.lines[0]
		l.lines = slices.Delete(l.lines, 0, 1)
		if l.byKey[gone.key] == gone {
			delete(l.byKey, gone.key)
		}
	}
}

// Entries returns the lines, the one seen last first; copies the caller owns.
func (l *DiagLog) Entries() []DiagEntry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]DiagEntry, 0, len(l.lines))
	for i := len(l.lines) - 1; i >= 0; i-- {
		e := *l.lines[i]
		e.key = ""
		out = append(out, e)
	}
	return out
}

// Since returns when the latest connect began; zero before the first.
func (l *DiagLog) Since() time.Time {
	if l == nil {
		return time.Time{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.since
}

// Counts returns how often each kind of problem happened since the latest connect.
func (l *DiagLog) Counts() map[DiagKind]int64 {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[DiagKind]int64, len(l.counts))
	for k, n := range l.counts {
		out[k] = n
	}
	return out
}

// Problems counts the lines that are problems seen since the latest connect: what the status
// bar's button shows.
func (l *DiagLog) Problems() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, e := range l.lines {
		if e.Problem && !e.Last.Before(l.since) {
			n++
		}
	}
	return n
}

// Changes counts the additions so far: a reader that saw the same number saw the same log.
func (l *DiagLog) Changes() uint64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changes
}

// diagKey tells the lines apart: the kind, what, the request's method, path, selectors and status,
// and, for a line not about one request, its text (a rate limit, a fallback's cause). A connect is
// never folded.
func diagKey(e DiagEntry) string {
	if e.Kind == DiagConnect {
		return ""
	}
	parts := []string{string(e.Kind), e.What, e.Method, strconv.Itoa(e.Status)}
	if e.URL != "" {
		parts = append(parts, stableRequest(e.URL))
	} else {
		parts = append(parts, e.Text)
	}
	return strings.Join(parts, "\x00")
}

// stableRequest is a request's path and the selectors that say what it reads, without what a retry
// changes (resourceVersion, timeoutSeconds, continue); a watch and a streamed list differ by their
// verb, which What names.
func stableRequest(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	keep := url.Values{}
	for _, k := range []string{"labelSelector", "fieldSelector"} {
		if v, ok := q[k]; ok {
			keep[k] = v
		}
	}
	return u.Path + "?" + keep.Encode()
}

// clipText keeps a line's text on one line and within diagTextLimit.
func clipText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > diagTextLimit {
		s = strings.ToValidUTF8(s[:diagTextLimit], "") + "…"
	}
	return s
}

// requestLabelKey carries what a request is for (WithRequestLabel).
type requestLabelKey struct{}

// WithRequestLabel names what the requests made with ctx are for ("pods in team-a"): a request
// that waits at Cogpit's rate limit is noted under that name (the cache's reads name their scope).
func WithRequestLabel(ctx context.Context, label string) context.Context {
	return context.WithValue(ctx, requestLabelKey{}, label)
}

// requestLabel is what WithRequestLabel named, or "".
func requestLabel(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(requestLabelKey{}).(string)
	return s
}
