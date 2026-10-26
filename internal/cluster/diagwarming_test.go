package cluster

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// A watch the API server answered while its cache of the resource was still being built (429
// "storage is (re)initializing") is a grey line, not a problem; answered the same again within
// WarmingRepeatWithin, it is one (the cache does not come up); the first request at another API
// server hours later is not (D-new, the owner's option a).
func TestDiagLogCacheWarming(t *testing.T) {
	l, clk := newTestDiagLog()
	l.Connecting("https://api.example.test:6443")
	warming := func(rv string) DiagEntry {
		return DiagEntry{Kind: DiagWarming, What: "watchlist policyreports.wgpolicyk8s.io", Method: "GET", Status: 429, RetryAfter: "1",
			URL: "https://api.example.test:6443/apis/wgpolicyk8s.io/v1alpha2/policyreports?watch=true&resourceVersion=" + rv, Text: cacheWarmingText}
	}
	l.Add(warming("1"))
	if l.Problems() != 0 || l.Counts()[DiagWarming] != 0 || l.Entries()[0].Problem {
		t.Fatalf("one cache warming up: problems %d, counts %v, line %+v", l.Problems(), l.Counts(), l.Entries()[0])
	}

	// Hours later, at another API server: again no problem.
	clk.t = clk.t.Add(3 * time.Hour)
	l.Add(warming("2"))
	if e := l.Entries()[0]; e.Problem || e.Count != 2 || l.Problems() != 0 {
		t.Fatalf("a first request hours later: line %+v, problems %d", e, l.Problems())
	}

	// client-go's retry a second later, answered the same: the cache does not come up.
	clk.t = clk.t.Add(time.Second)
	l.Add(warming("3"))
	if e := l.Entries()[0]; !e.Problem || e.Count != 3 || l.Problems() != 1 || l.Counts()[DiagWarming] != 2 {
		t.Fatalf("a repeat within a minute: line %+v, problems %d, counts %v", e, l.Problems(), l.Counts())
	}
	clk.t = clk.t.Add(2 * time.Second)
	l.Add(warming("4"))
	if l.Counts()[DiagWarming] != 3 {
		t.Fatalf("counts %v after one more", l.Counts())
	}
}

// The round-tripper tells a cache warming up from a 429 of the server's rate limits, and from a
// cache built again after an error.
func TestDiagRoundTripperCacheWarming(t *testing.T) {
	const url = "https://api.example.test/apis/wgpolicyk8s.io/v1alpha2/policyreports?watch=true&sendInitialEvents=true"
	for _, tc := range []struct {
		name string
		body string
		want DiagKind
		text string
	}{
		{"warming", `{"kind":"Status","message":"storage is (re)initializing","reason":"TooManyRequests","code":429}`, DiagWarming,
			"storage is (re)initializing: the API server was still building its cache of this resource"},
		{"after an error", `{"kind":"Status","message":"storage is (re)initializing: watch of the storage failed","code":429}`, DiagTooMany,
			"storage is (re)initializing: watch of the storage failed"},
		{"rate limit", `{"kind":"Status","message":"too many requests, please try again later","code":429}`, DiagTooMany,
			"too many requests"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, clk := newTestDiagLog()
			rt := &diagRoundTripper{log: l, now: clk.now, next: &fakeRT{clk: clk, resp: answer(429, tc.body, "Retry-After", "1")}}
			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := rt.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			got := l.Entries()
			if len(got) != 1 || got[0].Kind != tc.want || !strings.HasPrefix(got[0].Text, tc.text) || got[0].RetryAfter != "1" ||
				got[0].What != "watchlist policyreports.wgpolicyk8s.io" {
				t.Fatalf("lines = %+v", got)
			}
			if got[0].Problem != (tc.want != DiagWarming) {
				t.Errorf("problem = %v", got[0].Problem)
			}
		})
	}
}
