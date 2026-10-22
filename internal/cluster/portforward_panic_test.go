package cluster

import (
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/streaming/pkg/httpstream"
)

// panicConn is a port-forward connection whose streams panic where the test says: the error
// stream's or the data stream's Read, or the data stream's Write. It stands for a bug in the
// streaming library meeting a hostile or broken kubelet.
type panicConn struct {
	panicOn string // "errorRead", "dataRead" or "dataWrite"
	closed  chan bool
	once    sync.Once
}

func (c *panicConn) CreateStream(h http.Header) (httpstream.Stream, error) {
	return &panicStream{conn: c, typ: h.Get(corev1.StreamType), headers: h.Clone()}, nil
}

func (c *panicConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *panicConn) CloseChan() <-chan bool             { return c.closed }
func (c *panicConn) SetIdleTimeout(time.Duration)       {}
func (c *panicConn) RemoveStreams(...httpstream.Stream) {}

type panicStream struct {
	conn    *panicConn
	typ     string
	headers http.Header
}

func (s *panicStream) Read(p []byte) (int, error) {
	if s.conn.panicOn == s.typ+"Read" {
		panic("stream bug: " + s.typ + " read")
	}
	if s.typ == corev1.StreamTypeData {
		time.Sleep(50 * time.Millisecond) // the pod sends nothing and then ends
	}
	return 0, io.EOF
}

func (s *panicStream) Write(p []byte) (int, error) {
	if s.conn.panicOn == s.typ+"Write" {
		panic("stream bug: " + s.typ + " write")
	}
	return len(p), nil
}

func (s *panicStream) Close() error         { return nil }
func (s *panicStream) Reset() error         { return nil }
func (s *panicStream) Headers() http.Header { return s.headers }
func (s *panicStream) Identifier() uint32   { return 1 }

// TestTunnelForwardSurvivesPanickingStreams: the goroutines a forwarded connection runs read and
// write the cluster's streams, so each recovers from a panic at its boundary (AGENTS.md: every
// goroutine doing cluster I/O): a panic ends that connection, never Cogpit, and Forward returns.
func TestTunnelForwardSurvivesPanickingStreams(t *testing.T) {
	for _, where := range []string{"errorRead", "dataRead", "dataWrite"} {
		t.Run(where, func(t *testing.T) {
			tun := newTunnel(&panicConn{panicOn: where, closed: make(chan bool)})
			local, remote := net.Pipe()
			defer remote.Close()
			go func() { _, _ = remote.Write([]byte("hello\n")) }()
			done := make(chan error, 1)
			go func() { done <- tun.Forward(local, 8080) }()
			select {
			case err := <-done:
				// Only the error stream's panic makes Forward fail: a data stream's panic ends its
				// copy, and Forward returns what the error stream says (nothing here).
				if where == "errorRead" && !errors.Is(err, errStreamPanicked) {
					t.Errorf("Forward returned %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Forward did not return after a stream panicked")
			}
		})
	}
}
