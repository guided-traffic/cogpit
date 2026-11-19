package cluster

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/streaming/pkg/httpstream"
	streamspdy "k8s.io/streaming/pkg/httpstream/spdy"
)

// fakeKubelet is a port-forward endpoint as the API server proxies the kubelet's: it refuses the
// WebSocket upgrade (an older server, or a proxy), so the client falls back to SPDY, and serves
// each request's error and data streams. Port 7000 answers "pod <port>: <line>"; any other port
// reports on the error stream that nothing listens there.
type fakeKubelet struct {
	websockets atomic.Int32 // WebSocket upgrades refused
	mu         sync.Mutex
	conns      []httpstream.Connection
}

func (k *fakeKubelet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		k.websockets.Add(1)
		http.Error(w, "websockets are not supported here", http.StatusBadRequest)
		return
	}
	if _, err := httpstream.Handshake(r, w, []string{portforward.PortForwardProtocolV1Name}); err != nil {
		return
	}
	streams := make(chan httpstream.Stream, 16)
	conn := streamspdy.NewResponseUpgrader().UpgradeResponse(w, r, func(s httpstream.Stream, _ <-chan struct{}) error {
		streams <- s
		return nil
	})
	if conn == nil {
		return
	}
	k.mu.Lock()
	k.conns = append(k.conns, conn)
	k.mu.Unlock()
	go k.serve(conn, streams)
}

// serve pairs each request's error and data streams by request id.
func (k *fakeKubelet) serve(conn httpstream.Connection, streams <-chan httpstream.Stream) {
	pending := map[string]httpstream.Stream{}
	for {
		select {
		case <-conn.CloseChan():
			return
		case s := <-streams:
			id := s.Headers().Get(corev1.PortForwardRequestIDHeader)
			other, ok := pending[id]
			if !ok {
				pending[id] = s
				continue
			}
			delete(pending, id)
			errS, data := other, s
			if s.Headers().Get(corev1.StreamType) == corev1.StreamTypeError {
				errS, data = s, other
			}
			go k.answer(s.Headers().Get(corev1.PortHeader), errS, data)
		}
	}
}

func (k *fakeKubelet) answer(port string, errS, data httpstream.Stream) {
	defer func() { _ = errS.Close() }()
	defer func() { _ = data.Close() }()
	if port != "7000" {
		_, _ = io.WriteString(errS, "error forwarding port "+port+": dial tcp4 127.0.0.1:"+port+": connect: connection refused\x1b[31m")
		return
	}
	line, err := bufio.NewReader(data).ReadString('\n')
	if err != nil {
		return
	}
	_, _ = io.WriteString(data, "pod "+port+": "+line)
}

// dropAll closes the server's side of every connection, once there is one: the client can see
// the upgrade's response before the handler has the connection.
func (k *fakeKubelet) dropAll(t *testing.T) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		k.mu.Lock()
		n := len(k.conns)
		for _, c := range k.conns {
			_ = c.Close()
		}
		k.mu.Unlock()
		if n > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the server never had a connection")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newFakeKubelet(t *testing.T) (*fakeKubelet, *rest.Config, *url.URL) {
	k := &fakeKubelet{}
	srv := httptest.NewServer(k)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL + "/api/v1/namespaces/shop/pods/web-0/portforward")
	if err != nil {
		t.Fatal(err)
	}
	return k, &rest.Config{Host: srv.URL}, u
}

// roundTrip connects to the listener, sends a line and reads the answer.
func roundTrip(t *testing.T, addr, line string) (string, error) {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(c, line+"\n"); err != nil {
		return "", err
	}
	return bufio.NewReader(c).ReadString('\n')
}

func TestListenLoopbackBindsIPv4LoopbackOnly(t *testing.T) {
	ln, err := ListenLoopback(0)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	a, ok := ln.Addr().(*net.TCPAddr)
	if !ok || a.IP.String() != LoopbackAddress || a.Port == 0 {
		t.Fatalf("listening on %v, want 127.0.0.1 and a free port", ln.Addr())
	}
	// The port is taken now: a second forward on it fails instead of sharing it.
	if _, err := ListenLoopback(uint16(a.Port)); err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("a second listener on port %d: %v", a.Port, err)
	}
	// Not reachable on any other address of the machine.
	addrs, _ := net.InterfaceAddrs()
	for _, ia := range addrs {
		ipn, ok := ia.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.IsLinkLocalUnicast() {
			continue
		}
		c, err := net.DialTimeout("tcp", net.JoinHostPort(ipn.IP.String(), strconv.Itoa(a.Port)), 200*time.Millisecond)
		if err == nil {
			c.Close()
			t.Errorf("the forward's port answers on %s", ipn.IP)
		}
	}
}

// The tunnel carries local connections to the pod's port over SPDY after the WebSocket upgrade
// was refused, and a port nothing listens on ends the tunnel with the pod's error, made safe.
func TestTunnelForwardsAndFallsBackToSPDY(t *testing.T) {
	k, cfg, u := newFakeKubelet(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tun, err := dialPortForward(ctx, cfg, u)
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Close()
	if k.websockets.Load() == 0 {
		t.Error("the WebSocket dialer was not tried first")
	}
	ln, err := ListenLoopback(0)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	results := make(chan error, 4)
	serve := func(port uint16) {
		c, err := ln.Accept()
		if err != nil {
			results <- err
			return
		}
		results <- tun.Forward(c, port)
	}

	for _, line := range []string{"hello", "again"} {
		go serve(7000)
		got, err := roundTrip(t, ln.Addr().String(), line)
		if err != nil || got != "pod 7000: "+line+"\n" {
			t.Fatalf("round trip %q: %q, %v", line, got, err)
		}
		if err := <-results; err != nil {
			t.Fatalf("forward: %v", err)
		}
	}

	go serve(9999)
	_, _ = roundTrip(t, ln.Addr().String(), "anyone?")
	err = <-results
	if err == nil || !strings.Contains(err.Error(), "connection refused") || strings.ContainsRune(err.Error(), 0x1b) {
		t.Fatalf("forward to a closed port: %v", err)
	}
	select {
	case <-tun.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the tunnel is still open after the pod's error")
	}
}

// Losing the connection to the pod closes Done.
func TestTunnelDoneWhenTheConnectionIsLost(t *testing.T) {
	k, cfg, u := newFakeKubelet(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tun, err := dialPortForward(ctx, cfg, u)
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Close()
	k.dropAll(t)
	select {
	case <-tun.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done not closed after the server dropped the connection")
	}
}

func TestDialPortForwardRespectsContextAndConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, cfg, u := newFakeKubelet(t)
	if _, err := dialPortForward(ctx, cfg, u); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled dial: %v", err)
	}
	var c *Conn
	if _, err := c.DialPortForward(context.Background(), "shop", "web-0"); err == nil {
		t.Error("a nil connection dialled")
	}
	if _, err := (&Conn{}).DialPortForward(context.Background(), "", "web-0"); err == nil {
		t.Error("no namespace accepted")
	}
}

// cancelledAfterFirstCheck is live at the first Err call and cancelled from then on, with a
// Done channel that never closes: the dial finishes while the context is cancelled.
type cancelledAfterFirstCheck struct {
	context.Context
	checks atomic.Int32
}

func (c *cancelledAfterFirstCheck) Done() <-chan struct{} { return nil }

func (c *cancelledAfterFirstCheck) Err() error {
	if c.checks.Add(1) == 1 {
		return nil
	}
	return context.Canceled
}

// A dial that finishes after its caller was cancelled hands back no tunnel, whatever order
// select sees the two in (CI flake on main 7e01d03).
func TestDialPortForwardCancelledWhileDialling(t *testing.T) {
	_, cfg, u := newFakeKubelet(t)
	ctx := &cancelledAfterFirstCheck{Context: context.Background()}
	tun, err := dialPortForward(ctx, cfg, u)
	if !errors.Is(err, context.Canceled) || tun != nil {
		t.Fatalf("dial finished after cancellation: tunnel %v, %v", tun, err)
	}
}

func TestForwardMessageIsSafe(t *testing.T) {
	got := forwardMessage([]byte("  boom\r\n\x1b]52;c;aGk=\x07" + strings.Repeat("x", 600)))
	if strings.ContainsAny(got, "\r\n\x1b\x07") || !strings.HasPrefix(got, "boom") || !strings.HasSuffix(got, "…") {
		t.Errorf("forwardMessage = %q", got)
	}
}
