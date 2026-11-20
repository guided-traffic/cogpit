package cluster

import (
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"slices"
	"strings"
	"sync"
	"time"
)

// The connection's timings for Connection Diagnostics (D-133): how long the API server takes to
// answer GET /version (the heartbeat's request, on the connection the views use), how long it takes
// to answer a request that opens a stream (a container's log, exec, attach, a port forward: the API
// server opens it at the kubelet of the pod's node before it answers), which of those wait for an
// answer now, and the connections the HTTP client opened, with how long each took to set up and the
// protocol they speak. The diagnostics round-tripper measures them (diagtransport.go); they send
// nothing of their own. They belong to one connection: a reconnect starts them again.

// latencySamples is how many of the latest answers a median is taken over.
const latencySamples = 64

// Stream kinds (StreamStats.Kind): what a request that opens a stream is for.
const (
	StreamLogs        = "logs"
	StreamExec        = "exec"
	StreamAttach      = "attach"
	StreamPortForward = "port forwards"
)

// Latency is how long the answers to one sort of request took.
type Latency struct {
	// Count counts the requests answered or failed; Failed those without an answer or with an error
	// status (400 or more).
	Count  int64 `json:"count"`
	Failed int64 `json:"failed,omitempty"`
	// Median is over the latest answers (latencySamples), Longest the longest of all, Latest the
	// latest one's, at LatestAt.
	Median   time.Duration `json:"median,omitempty"`
	Longest  time.Duration `json:"longest,omitempty"`
	Latest   time.Duration `json:"latest,omitempty"`
	LatestAt time.Time     `json:"latestAt,omitzero"`
}

// StreamStats is how long the API server took to answer the requests that open one kind of stream.
type StreamStats struct {
	Kind string `json:"kind"`
	Latency
}

// WaitingStream is a request that opens a stream and has no answer yet.
type WaitingStream struct {
	Kind   string    `json:"kind"`
	What   string    `json:"what"`
	Method string    `json:"method"`
	URL    string    `json:"url"`
	Since  time.Time `json:"since"`
}

// ConnectionStats is what the HTTP client's connections to the API server cost: Setup counts the
// connections it opened (the first one included) and how long each took from the start of its TCP
// connect to the end of its TLS handshake (a proxy's CONNECT included); its Failed counts the TLS
// handshakes that failed. Exec, attach and port forwards open connections of their own, which are
// not counted.
type ConnectionStats struct {
	Setup Latency       `json:"setup"`
	Proto string        `json:"protocol,omitempty"` // the latest answer's: "HTTP/2.0", "HTTP/1.1"
	TCP   time.Duration `json:"tcp,omitempty"`      // the latest connection's TCP connect
	TLS   time.Duration `json:"tls,omitempty"`      // the latest connection's TLS handshake
}

// TransportStats is the connection's timings since it began.
type TransportStats struct {
	// Version is GET /version: the heartbeat's request and the connect's first.
	Version     Latency         `json:"version"`
	Streams     []StreamStats   `json:"streams,omitempty"` // logs, exec, attach, port forwards: those opened
	Waiting     []WaitingStream `json:"waiting,omitempty"` // the oldest first
	Connections ConnectionStats `json:"connections"`
}

// latency gathers one Latency.
type latency struct {
	Latency
	ring []time.Duration
	next int
}

func (l *latency) add(took time.Duration, failed bool, at time.Time) {
	l.Count++
	if failed {
		l.Failed++
	}
	l.Latest, l.LatestAt = took, at
	l.Longest = max(l.Longest, took)
	if len(l.ring) < latencySamples {
		l.ring = append(l.ring, took)
	} else {
		l.ring[l.next] = took
		l.next = (l.next + 1) % latencySamples
	}
}

func (l *latency) snapshot() Latency {
	out := l.Latency
	if len(l.ring) > 0 {
		s := slices.Clone(l.ring)
		slices.Sort(s)
		out.Median = s[len(s)/2]
	}
	return out
}

// transportStats gathers the connection's TransportStats. It is safe for concurrent use; a nil one
// gathers nothing.
type transportStats struct {
	mu      sync.Mutex
	version latency
	streams map[string]*latency
	waiting map[uint64]WaitingStream
	seq     uint64
	conns   latency
	changes uint64 // bumps when a stream starts or ends waiting, and on every answer timed
	proto   string
	tcp     time.Duration
	tls     time.Duration
}

func newTransportStats() *transportStats {
	return &transportStats{streams: map[string]*latency{}, waiting: map[uint64]WaitingStream{}}
}

// wait notes a request that opens a stream and waits for its answer; done ends the wait.
func (s *transportStats) wait(w WaitingStream) (done func()) {
	if s == nil {
		return func() {}
	}
	s.mu.Lock()
	s.seq++
	s.changes++
	id := s.seq
	s.waiting[id] = w
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.waiting, id)
		s.changes++
		s.mu.Unlock()
	}
}

// stream notes how long the answer to a request that opens a stream of kind took.
func (s *transportStats) stream(kind string, took time.Duration, failed bool, at time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changes++
	l := s.streams[kind]
	if l == nil {
		l = &latency{}
		s.streams[kind] = l
	}
	l.add(took, failed, at)
}

// versionAnswer notes how long GET /version took.
func (s *transportStats) versionAnswer(took time.Duration, failed bool, at time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changes++
	s.version.add(took, failed, at)
}

// answered notes the protocol of an answer.
func (s *transportStats) answered(proto string) {
	if s == nil || proto == "" {
		return
	}
	s.mu.Lock()
	s.proto = proto
	s.mu.Unlock()
}

// opened notes a connection set up in setup, its TCP connect and TLS handshake taking tcp and
// tlsTook; failed: its TLS handshake failed.
func (s *transportStats) opened(setup, tcp, tlsTook time.Duration, failed bool, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns.add(setup, failed, at)
	if !failed {
		s.tcp, s.tls = tcp, tlsTook
	}
}

// changed counts the changes so far: a reader that saw the same number saw the same waits and
// answers.
func (s *transportStats) changed() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changes
}

// snapshot returns the stats; streams in a fixed order, the waiting ones the oldest first.
func (s *transportStats) snapshot() TransportStats {
	if s == nil {
		return TransportStats{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := TransportStats{Version: s.version.snapshot()}
	for _, kind := range []string{StreamLogs, StreamExec, StreamAttach, StreamPortForward} {
		if l := s.streams[kind]; l != nil {
			out.Streams = append(out.Streams, StreamStats{Kind: kind, Latency: l.snapshot()})
		}
	}
	for _, w := range s.waiting {
		out.Waiting = append(out.Waiting, w)
	}
	slices.SortFunc(out.Waiting, func(a, b WaitingStream) int { return a.Since.Compare(b.Since) })
	out.Connections = ConnectionStats{Setup: s.conns.snapshot(), Proto: s.proto, TCP: s.tcp, TLS: s.tls}
	return out
}

// streamKind is the kind of stream a request opens, "" for one that opens none (a watch is a read
// of the cache's, timed by its load).
func streamKind(resource string) string {
	switch {
	case strings.HasSuffix(resource, "/log"):
		return StreamLogs
	case strings.HasSuffix(resource, "/exec"):
		return StreamExec
	case strings.HasSuffix(resource, "/attach"):
		return StreamAttach
	case strings.HasSuffix(resource, "/portforward"):
		return StreamPortForward
	}
	return ""
}

// traceConnections has the request's context note the connections the HTTP client opens for it:
// from the first TCP connect's start to the end of the TLS handshake (to the end of the TCP connect
// without TLS). A connection already open notes nothing; a TCP connect that fails is the request's
// network error (another address may still be tried).
func (s *transportStats) traceConnections(req *http.Request, now func() time.Time) *http.Request {
	if s == nil {
		return req
	}
	var (
		mu                   sync.Mutex
		start, tcpDone, tls0 time.Time
		ended                bool
	)
	end := func(failed bool) {
		at := now()
		mu.Lock()
		defer mu.Unlock()
		if ended || start.IsZero() {
			return
		}
		ended = true
		var tlsTook time.Duration
		if !tls0.IsZero() {
			tlsTook = at.Sub(tls0)
		}
		s.opened(at.Sub(start), tcpDone.Sub(start), tlsTook, failed, at)
	}
	https := req.URL.Scheme == "https"
	trace := &httptrace.ClientTrace{
		ConnectStart: func(string, string) {
			mu.Lock()
			if start.IsZero() {
				start = now()
			}
			mu.Unlock()
		},
		ConnectDone: func(_, _ string, err error) {
			mu.Lock()
			if err == nil && tcpDone.IsZero() {
				tcpDone = now()
			}
			mu.Unlock()
			if err == nil && !https {
				end(false)
			}
		},
		TLSHandshakeStart: func() {
			mu.Lock()
			if tls0.IsZero() {
				tls0 = now()
			}
			mu.Unlock()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) { end(err != nil) },
	}
	return req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
}
