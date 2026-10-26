package cluster

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeClock is a clock tests move by hand.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newTestDiagLog() (*DiagLog, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)}
	l := NewDiagLog()
	l.now = clk.now
	return l, clk
}

func TestDiagLogFoldsTheSameProblem(t *testing.T) {
	l, clk := newTestDiagLog()
	l.Connecting("https://api.example.test:6443")
	refused := func(rv string) DiagEntry {
		return DiagEntry{Kind: DiagRefused, What: "list pods", Method: "GET", Status: 403,
			URL: "https://api.example.test:6443/api/v1/pods?limit=500&resourceVersion=" + rv, Text: "pods is forbidden"}
	}
	l.Add(refused("1"))
	clk.t = clk.t.Add(time.Second)
	l.Add(refused("2")) // a retry: another resource version, the same line
	clk.t = clk.t.Add(time.Second)
	l.Add(DiagEntry{Kind: DiagTimeout, What: "get namespaces/team-a", Method: "GET", URL: "https://api.example.test:6443/api/v1/namespaces/team-a"})

	got := l.Entries()
	if len(got) != 3 {
		t.Fatalf("lines = %d, want 3 (connect, refused, timeout): %+v", len(got), got)
	}
	if got[0].Kind != DiagTimeout || got[1].Kind != DiagRefused || got[2].Kind != DiagConnect {
		t.Fatalf("order = %s, %s, %s; want the line seen last first", got[0].Kind, got[1].Kind, got[2].Kind)
	}
	r := got[1]
	if r.Count != 2 || !r.Problem || r.First.Equal(r.Last) || !strings.HasSuffix(r.URL, "resourceVersion=2") {
		t.Fatalf("refused line = %+v; want count 2, first before last, the newest URL", r)
	}
	if got[2].Problem {
		t.Fatal("a connect is a marker, not a problem")
	}
	if n := l.Problems(); n != 2 {
		t.Fatalf("Problems = %d, want 2 lines", n)
	}
	if c := l.Counts(); c[DiagRefused] != 2 || c[DiagTimeout] != 1 || c[DiagConnect] != 0 {
		t.Fatalf("Counts = %v", c)
	}
}

func TestDiagLogIsBounded(t *testing.T) {
	l, clk := newTestDiagLog()
	l.limit = 3
	for i := range 5 {
		clk.t = clk.t.Add(time.Second)
		l.Add(DiagEntry{Kind: DiagRefused, What: fmt.Sprintf("list pods in ns-%d", i)})
	}
	// ns-2 seen again: it moves to the end and outlives ns-3.
	clk.t = clk.t.Add(time.Second)
	l.Add(DiagEntry{Kind: DiagRefused, What: "list pods in ns-2"})
	clk.t = clk.t.Add(time.Second)
	l.Add(DiagEntry{Kind: DiagRefused, What: "list pods in ns-5"})
	var whats []string
	for _, e := range l.Entries() {
		whats = append(whats, e.What)
	}
	if want := "list pods in ns-5|list pods in ns-2|list pods in ns-4"; strings.Join(whats, "|") != want {
		t.Fatalf("lines = %q, want %q", strings.Join(whats, "|"), want)
	}
	// A line dropped and seen again starts anew.
	l.Add(DiagEntry{Kind: DiagRefused, What: "list pods in ns-0"})
	if e := l.Entries()[0]; e.What != "list pods in ns-0" || e.Count != 1 {
		t.Fatalf("newest = %+v, want ns-0 counted once", e)
	}
}

func TestDiagLogCountsSinceTheLatestConnect(t *testing.T) {
	l, clk := newTestDiagLog()
	l.Connecting("https://a")
	l.Add(DiagEntry{Kind: DiagTimeout, What: "get version"})
	clk.t = clk.t.Add(time.Minute)
	l.Connecting("https://a")
	if n := l.Problems(); n != 0 {
		t.Fatalf("Problems after a reconnect = %d, want 0", n)
	}
	if c := l.Counts(); len(c) != 0 {
		t.Fatalf("Counts after a reconnect = %v, want none", c)
	}
	if n := len(l.Entries()); n != 3 {
		t.Fatalf("lines = %d, want the old ones kept (2 connects, 1 timeout)", n)
	}
	l.Add(DiagEntry{Kind: DiagTimeout, What: "get version"}) // the old line seen again
	if n := l.Problems(); n != 1 {
		t.Fatalf("Problems = %d, want the line seen again", n)
	}
}

func TestDiagLogClipsText(t *testing.T) {
	l, _ := newTestDiagLog()
	l.Add(DiagEntry{Kind: DiagServerError, What: "x", Text: strings.Repeat("ab\n", 1000)})
	if e := l.Entries()[0]; len(e.Text) > diagTextLimit+len("…") || strings.Contains(e.Text, "\n") {
		t.Fatalf("text not clipped to one line: %d bytes", len(e.Text))
	}
}

func TestNilDiagLog(t *testing.T) {
	var l *DiagLog
	l.Add(DiagEntry{Kind: DiagRefused})
	l.Connecting("x")
	if l.Entries() != nil || l.Problems() != 0 || l.Counts() != nil || l.Changes() != 0 {
		t.Fatal("a nil log holds nothing")
	}
}

// fakeRT answers every request with resp and err, after took on the clock.
type fakeRT struct {
	clk  *fakeClock
	took time.Duration
	resp func(*http.Request) *http.Response
	err  error
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	f.clk.t = f.clk.t.Add(f.took)
	if f.err != nil {
		return nil, f.err
	}
	return f.resp(req), nil
}

func answer(status int, body string, header ...string) func(*http.Request) *http.Response {
	return func(req *http.Request) *http.Response {
		h := http.Header{}
		for i := 0; i+1 < len(header); i += 2 {
			h.Set(header[i], header[i+1])
		}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: req}
	}
}

func TestDiagRoundTripperNotesProblems(t *testing.T) {
	forbidden := `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"pods is forbidden: User \"jdoe\" cannot list resource \"pods\" in API group \"\" at the cluster scope","reason":"Forbidden","code":403}`
	tests := []struct {
		name   string
		method string
		url    string
		took   time.Duration
		resp   func(*http.Request) *http.Response
		err    error
		want   DiagKind // "" for none
		what   string
		text   string
		status int
	}{
		{name: "refused list", url: "https://u:p@api.example.test:6443/api/v1/pods?limit=500", resp: answer(403, forbidden),
			want: DiagRefused, what: "list pods", text: `User "jdoe" cannot list`, status: 403},
		{name: "refused secret keeps no message", url: "https://api.example.test/api/v1/namespaces/a/secrets/s", method: "PUT",
			resp: answer(403, `{"kind":"Status","message":"denied: value hunter2"}`), want: DiagRefused, what: "update secrets/s in a", text: "Forbidden", status: 403},
		{name: "429", url: "https://api.example.test/apis/apps/v1/namespaces/b/deployments", resp: answer(429, `{"kind":"Status","message":"too many requests"}`, "Retry-After", "3"),
			want: DiagTooMany, what: "list deployments.apps in b", text: "too many requests", status: 429},
		{name: "503 discovery", url: "https://api.example.test/apis/metrics.k8s.io/v1beta1", resp: answer(503, "service unavailable\n"),
			want: DiagServerError, what: "discovery /apis/metrics.k8s.io/v1beta1", text: "Service Unavailable: service unavailable", status: 503},
		{name: "401", url: "https://api.example.test/api/v1/namespaces/a/pods/web-0/log", resp: answer(401, ""),
			want: DiagUnauthorized, what: "get pods/web-0 (log) in a", text: "Unauthorized", status: 401},
		{name: "timeout", url: "https://api.example.test/api/v1/nodes", err: fmt.Errorf("%w within 30s", ErrNoAnswer),
			want: DiagTimeout, what: "list nodes", text: "no answer"},
		{name: "network", url: "https://api.example.test/api/v1/nodes", err: io.ErrUnexpectedEOF, want: DiagNetwork, what: "list nodes"},
		{name: "rejected login", url: "https://api.example.test/api/v1/nodes", err: ErrLoginRejected, want: DiagUnauthorized, status: 401},
		{name: "gate refusal", url: "https://api.example.test/api/v1/nodes", err: ErrInterrupted},
		{name: "slow get", url: "https://api.example.test/api/v1/namespaces/a/configmaps/c", took: 6 * time.Second, resp: answer(200, "{}"),
			want: DiagSlow, what: "get configmaps/c in a", status: 200},
		{name: "slow watch is a stream", url: "https://api.example.test/api/v1/pods?watch=true", took: time.Minute, resp: answer(200, "")},
		{name: "fast ok", url: "https://api.example.test/api/v1/pods", resp: answer(200, "{}")},
		{name: "not found is an answer", url: "https://api.example.test/api/v1/namespaces/a/pods/x", resp: answer(404, "{}")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, clk := newTestDiagLog()
			rt := &diagRoundTripper{log: l, now: clk.now, next: &fakeRT{clk: clk, took: tc.took, resp: tc.resp, err: tc.err}}
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			req, _ := http.NewRequestWithContext(context.Background(), method, tc.url, nil)
			resp, err := rt.RoundTrip(req)
			if (err != nil) != (tc.err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if resp != nil && resp.StatusCode >= 400 {
				body, _ := io.ReadAll(resp.Body)
				if want := tc.resp(req); want != nil {
					wb, _ := io.ReadAll(want.Body)
					if string(body) != string(wb) {
						t.Fatalf("the caller's body = %q, want %q", body, wb)
					}
				}
			}
			got := l.Entries()
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("noted %+v, want nothing", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("lines = %+v, want one", got)
			}
			e := got[0]
			if e.Kind != tc.want || (tc.what != "" && e.What != tc.what) || !strings.Contains(e.Text, tc.text) || e.Status != tc.status {
				t.Fatalf("line = %+v; want kind %s, what %q, text with %q, status %d", e, tc.want, tc.what, tc.text, tc.status)
			}
			if strings.Contains(e.URL, "u:p@") || strings.Contains(e.Text, "hunter2") {
				t.Fatalf("line keeps a secret: %+v", e)
			}
			if tc.want == DiagTooMany && e.RetryAfter != "3" {
				t.Fatalf("Retry-After = %q, want 3", e.RetryAfter)
			}
		})
	}
}

func TestDiagRoundTripperIgnoresACallerThatGaveUp(t *testing.T) {
	l, clk := newTestDiagLog()
	rt := &diagRoundTripper{log: l, now: clk.now, next: &fakeRT{clk: clk, err: context.Canceled}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.test/api/v1/pods", nil)
	_, _ = rt.RoundTrip(req)
	if n := len(l.Entries()); n != 0 {
		t.Fatalf("noted %d lines for a cancelled request", n)
	}
}

// stepLimiter stands in for a token bucket: each Wait takes the next wait on the clock.
type stepLimiter struct {
	clk   *fakeClock
	waits []time.Duration
}

func (s *stepLimiter) TryAccept() bool { return true }
func (s *stepLimiter) Accept()         { _ = s.Wait(context.Background()) }
func (s *stepLimiter) Stop()           {}
func (s *stepLimiter) QPS() float32    { return 5 }
func (s *stepLimiter) Wait(context.Context) error {
	if len(s.waits) > 0 {
		s.clk.t = s.clk.t.Add(s.waits[0])
		s.waits = s.waits[1:]
	}
	return nil
}

func TestMeasuredLimiterCountsAndNotesWaits(t *testing.T) {
	l, clk := newTestDiagLog()
	m := newMeasuredLimiter(BudgetRequests, 5, 10, l)
	m.now = clk.now
	m.RateLimiter = &stepLimiter{clk: clk, waits: []time.Duration{0, 200 * time.Millisecond, 3 * time.Second, 2 * time.Second}}
	_ = m.Wait(context.Background())
	_ = m.Wait(context.Background())
	_ = m.Wait(WithRequestLabel(context.Background(), "pods in team-a"))
	m.Accept()

	st := m.Stats()
	if st.Requests != 4 || st.Delayed != 3 || st.Waited != 5200*time.Millisecond || st.Longest != 3*time.Second || st.Waiting != 0 {
		t.Fatalf("stats = %+v", st)
	}
	if st.Name != BudgetRequests || st.QPS != 5 || st.Burst != 10 {
		t.Fatalf("limit = %+v", st)
	}
	got := l.Entries()
	if len(got) != 2 {
		t.Fatalf("lines = %+v, want the two waits of a second or more", got)
	}
	if e := got[1]; e.Kind != DiagThrottled || e.What != "pods in team-a" || e.Took != 3*time.Second ||
		e.Text != "Cogpit's rate limit for requests: 5 per second, burst 10" {
		t.Fatalf("labelled wait = %+v", e)
	}
	if e := got[0]; e.What != "a request" || e.Took != 2*time.Second {
		t.Fatalf("unlabelled wait = %+v", e)
	}
}

func TestDescribeRequest(t *testing.T) {
	tests := []struct{ method, url, want string }{
		{"GET", "/api/v1/pods", "list pods"},
		{"GET", "/api/v1/namespaces/team-a/pods?watch=true", "watch pods in team-a"},
		{"GET", "/api/v1/namespaces/team-a/pods?watch=true&sendInitialEvents=true", "watchlist pods in team-a"},
		{"GET", "/api/v1/nodes", "list nodes"},
		{"GET", "/api/v1/namespaces/team-a", "get namespaces/team-a"},
		{"GET", "/apis/apps/v1/namespaces/b/deployments/web", "get deployments.apps/web in b"},
		{"POST", "/apis/authorization.k8s.io/v1/selfsubjectrulesreviews", "create selfsubjectrulesreviews.authorization.k8s.io"},
		{"GET", "/apis/metrics.k8s.io/v1beta1", "discovery /apis/metrics.k8s.io/v1beta1"},
		{"GET", "/version", "discovery /version"},
	}
	for _, tc := range tests {
		u, _ := url.Parse(tc.url)
		verb, resource := Classify(tc.method, u)
		if got := describeRequest(verb, resource, u); got != tc.want {
			t.Errorf("%s %s = %q, want %q", tc.method, tc.url, got, tc.want)
		}
	}
}
