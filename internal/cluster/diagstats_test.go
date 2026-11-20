package cluster

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// blockingRT answers once release is closed; started tells that the request reached it.
type blockingRT struct {
	started chan struct{}
	release chan struct{}
	clk     *fakeClock
	took    time.Duration
}

func (b *blockingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	close(b.started)
	<-b.release
	b.clk.t = b.clk.t.Add(b.took)
	return answer(200, "")(req), nil
}

func TestDiagRoundTripperTimesAStreamAndShowsItWaiting(t *testing.T) {
	l, clk := newTestDiagLog()
	stats := newTransportStats()
	next := &blockingRT{started: make(chan struct{}), release: make(chan struct{}), clk: clk, took: 12 * time.Second}
	rt := &diagRoundTripper{log: l, stats: stats, now: clk.now, next: next}
	url := "https://api.example.test/api/v1/namespaces/team-a/pods/web-0/log?container=app&follow=true&tailLines=1000"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := rt.RoundTrip(req)
		if err != nil || resp.StatusCode != 200 {
			t.Errorf("RoundTrip = %v, %v", resp, err)
		}
	}()
	<-next.started
	if stats.changed() != 1 {
		t.Fatalf("changes = %d once the stream waits, want 1", stats.changed())
	}
	w := stats.snapshot().Waiting
	if len(w) != 1 || w[0].Kind != StreamLogs || w[0].What != "get pods/web-0 (log) in team-a" || w[0].URL != url || w[0].Method != http.MethodGet {
		t.Fatalf("waiting = %+v, want the log's request", w)
	}
	close(next.release)
	<-done

	if stats.changed() != 3 {
		t.Fatalf("changes = %d once answered, want 3: the wait began, ended, the answer timed", stats.changed())
	}
	got := stats.snapshot()
	if len(got.Waiting) != 0 {
		t.Fatalf("waiting = %+v after the answer, want none", got.Waiting)
	}
	if len(got.Streams) != 1 {
		t.Fatalf("streams = %+v, want the logs", got.Streams)
	}
	s := got.Streams[0]
	if s.Kind != StreamLogs || s.Count != 1 || s.Failed != 0 || s.Median != 12*time.Second || s.Longest != 12*time.Second || s.Latest != 12*time.Second {
		t.Fatalf("logs = %+v, want one answer after 12s", s)
	}
	lines := l.Entries()
	if len(lines) != 1 || lines[0].Kind != DiagSlowStream || lines[0].What != "get pods/web-0 (log) in team-a" ||
		lines[0].Status != 200 || lines[0].Took != 12*time.Second || !strings.Contains(lines[0].Text, "kubelet") {
		t.Fatalf("lines = %+v, want one slow stream line", lines)
	}
	if !lines[0].Problem {
		t.Fatal("a slow stream is a problem")
	}
}

func TestDiagRoundTripperTimesStreamsAndVersion(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		took   time.Duration
		status int
		kind   string // "" for /version
		failed bool
	}{
		{name: "fast log", url: "https://api.example.test/api/v1/namespaces/a/pods/p/log", took: 80 * time.Millisecond, status: 200, kind: StreamLogs},
		{name: "refused exec", url: "https://api.example.test/api/v1/namespaces/a/pods/p/exec?command=sh", took: 30 * time.Millisecond, status: 403, kind: StreamExec, failed: true},
		{name: "attach", url: "https://api.example.test/api/v1/namespaces/a/pods/p/attach", status: 101, kind: StreamAttach},
		{name: "port forward", url: "https://api.example.test/api/v1/namespaces/a/pods/p/portforward", status: 101, kind: StreamPortForward},
		{name: "version", url: "https://api.example.test/version", took: 40 * time.Millisecond, status: 200},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, clk := newTestDiagLog()
			stats := newTransportStats()
			rt := &diagRoundTripper{log: l, stats: stats, now: clk.now, next: &fakeRT{clk: clk, took: tc.took, resp: answer(tc.status, "")}}
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, tc.url, nil)
			if _, err := rt.RoundTrip(req); err != nil {
				t.Fatal(err)
			}
			got := stats.snapshot()
			var lat Latency
			if tc.kind == "" {
				lat = got.Version
				if len(got.Streams) != 0 {
					t.Fatalf("streams = %+v, want none", got.Streams)
				}
			} else {
				if len(got.Streams) != 1 || got.Streams[0].Kind != tc.kind {
					t.Fatalf("streams = %+v, want %s", got.Streams, tc.kind)
				}
				lat = got.Streams[0].Latency
			}
			want := int64(0)
			if tc.failed {
				want = 1
			}
			if lat.Count != 1 || lat.Failed != want || lat.Latest != tc.took {
				t.Fatalf("latency = %+v, want one answer after %s, failed %d", lat, tc.took, want)
			}
		})
	}
}

func TestDiagRoundTripperDoesNotTimeAWatch(t *testing.T) {
	l, clk := newTestDiagLog()
	stats := newTransportStats()
	rt := &diagRoundTripper{log: l, stats: stats, now: clk.now, next: &fakeRT{clk: clk, took: time.Minute, resp: answer(200, "")}}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.test/api/v1/pods?watch=true", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if got := stats.snapshot(); len(got.Streams) != 0 || got.Version.Count != 0 || len(l.Entries()) != 0 {
		t.Fatalf("a watch noted %+v, %+v", got, l.Entries())
	}
}

func TestLatencyMedianIsOverTheLatestAnswers(t *testing.T) {
	var l latency
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for range latencySamples {
		l.add(time.Hour, false, at)
	}
	for range latencySamples/2 + 1 {
		l.add(time.Millisecond, false, at)
	}
	got := l.snapshot()
	if got.Median != time.Millisecond || got.Longest != time.Hour || got.Count != int64(latencySamples+latencySamples/2+1) {
		t.Fatalf("latency = %+v, want the median of the latest %d answers", got, latencySamples)
	}
}

func TestDiagRoundTripperCountsTheClientsConnections(t *testing.T) {
	for _, h2 := range []bool{false, true} {
		name, proto := "HTTP/1.1", "HTTP/1.1"
		if h2 {
			name, proto = "HTTP/2", "HTTP/2.0"
		}
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "{}") }))
			srv.EnableHTTP2 = h2
			srv.StartTLS()
			defer srv.Close()
			l := NewDiagLog()
			stats := newTransportStats()
			base := srv.Client().Transport
			hc := &http.Client{Transport: &diagRoundTripper{log: l, stats: stats, now: time.Now, next: base}}
			get := func() {
				t.Helper()
				resp, err := hc.Get(srv.URL + "/api/v1/namespaces/a/configmaps/c")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
			get()
			get() // the open connection again
			if c := stats.snapshot().Connections; c.Setup.Count != 1 || c.Proto != proto || c.Setup.Latest <= 0 || c.TCP <= 0 || c.TLS <= 0 {
				t.Fatalf("connections = %+v, want one opened, %s, with its times", c, proto)
			}
			hc.CloseIdleConnections()
			get()
			if c := stats.snapshot().Connections; c.Setup.Count != 2 || c.Setup.Failed != 0 {
				t.Fatalf("connections = %+v, want two opened", c)
			}
		})
	}
}

func TestNilTransportStats(t *testing.T) {
	var s *transportStats
	s.wait(WaitingStream{})()
	s.stream(StreamLogs, time.Second, false, time.Now())
	s.versionAnswer(time.Second, false, time.Now())
	s.answered("HTTP/2.0")
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.test/version", nil)
	if s.traceConnections(req, time.Now) != req {
		t.Fatal("a nil stats traces nothing")
	}
	if got := s.snapshot(); got.Version.Count != 0 || got.Streams != nil {
		t.Fatalf("a nil stats holds %+v", got)
	}
	if s.changed() != 0 || (*Conn)(nil).TransportChanges() != 0 {
		t.Fatal("a nil stats changes nothing")
	}
	if got := (*Conn)(nil).TransportStats(); got.Version.Count != 0 {
		t.Fatalf("a nil conn holds %+v", got)
	}
}
