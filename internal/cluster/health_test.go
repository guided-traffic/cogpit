package cluster

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/guided-traffic/cogpit/internal/testkit"
)

// connectThroughProxy connects to a fake API server behind a freezable proxy.
func connectThroughProxy(t *testing.T, opts testkit.FakeAPIOptions, timeout time.Duration) (*Conn, *testkit.FakeAPIServer, *testkit.FreezableProxy) {
	t.Helper()
	srv := testkit.NewFakeAPIServer(opts)
	t.Cleanup(srv.Close)
	p := testkit.NewFreezableProxy(t, strings.TrimPrefix(srv.URL, "https://"))
	path := writeFile(t, "kubeconfig", string(p.ProxyKubeconfig(srv, "fake", "default")))
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{path}, RequestTimeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c, srv, p
}

func TestARequestWithoutAnAnswerEndsAfterTheTimeout(t *testing.T) {
	c, _, p := connectThroughProxy(t, testkit.FakeAPIOptions{}, 300*time.Millisecond)
	ctx := context.Background()
	if _, err := c.Kube().CoreV1().Namespaces().List(ctx, metav1.ListOptions{}); err != nil {
		t.Fatal(err)
	}
	p.Freeze()
	defer p.Thaw()

	start := time.Now()
	_, err := c.Kube().CoreV1().Namespaces().Get(ctx, "default", metav1.GetOptions{})
	if !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("a read on a silent connection: %v, want ErrNoAnswer", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("the read ended after %v", d)
	}
	_, err = c.Kube().CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a"}}, metav1.CreateOptions{})
	var unknown *OutcomeUnknownError
	if !errors.As(err, &unknown) || !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("a write on a silent connection: %v, want an OutcomeUnknownError", err)
	}
	if !strings.Contains(err.Error(), "the view shows the object's state once reconnected") || unknown.Verb != VerbCreate {
		t.Errorf("the write's error: %v", err)
	}
}

func TestAWatchIsNotBoundedByTheRequestTimeout(t *testing.T) {
	c, srv, _ := connectThroughProxy(t, testkit.FakeAPIOptions{}, 200*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := c.Kube().CoreV1().Namespaces().Watch(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	time.Sleep(600 * time.Millisecond) // three timeouts long, without an event: the stream stays open
	srv.Set(testkit.NamespacesGVR, testkit.Namespace("late"))
	select {
	case ev, ok := <-w.ResultChan():
		if !ok {
			t.Fatal("the watch ended")
		}
		if ev.Object == nil {
			t.Fatal("no object")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no event")
	}
}

func TestTheGateHoldsListsAndWatchesAndRefusesTheRest(t *testing.T) {
	c, _, _ := connectThroughProxy(t, testkit.FakeAPIOptions{}, 0)
	ctx := context.Background()
	c.SetHealth(HealthInterrupted)
	if _, err := c.Kube().CoreV1().Namespaces().Get(ctx, "default", metav1.GetOptions{}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("a get while interrupted: %v", err)
	}
	if _, err := c.Kube().CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "x"}}, metav1.CreateOptions{}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("a write while interrupted: %v", err)
	}
	listed := make(chan error, 1)
	go func() {
		_, err := c.Dynamic().Resource(testkit.NamespacesGVR).List(ctx, metav1.ListOptions{})
		listed <- err
	}()
	select {
	case err := <-listed:
		t.Fatalf("a list went through while interrupted: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if err := c.Heartbeat(ctx); err != nil {
		t.Fatalf("the heartbeat passes the gate: %v", err)
	}
	c.SetHealth(HealthLoginExpired)
	if _, err := c.Kube().CoreV1().Pods("default").Get(ctx, "p", metav1.GetOptions{}); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("a get while the login expired: %v", err)
	}
	c.SetHealth(HealthOK)
	select {
	case err := <-listed:
		if err != nil {
			t.Fatalf("the held list: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the held list did not go out once healthy")
	}
}

func TestAHeldListEndsWithItsContext(t *testing.T) {
	c, _, _ := connectThroughProxy(t, testkit.FakeAPIOptions{}, 0)
	c.SetHealth(HealthInterrupted)
	defer c.SetHealth(HealthOK)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := c.Dynamic().Resource(testkit.NamespacesGVR).List(ctx, metav1.ListOptions{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a held list whose context ended: %v", err)
	}
}

func TestHeartbeatAndReachable(t *testing.T) {
	c, srv, p := connectThroughProxy(t, testkit.FakeAPIOptions{Token: secretToken}, 0)
	ctx := context.Background()
	if v := c.SeenVersion(); v != "" {
		t.Errorf("a version seen before the first heartbeat: %q", v)
	}
	if err := c.Heartbeat(ctx); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	// The heartbeat keeps the version it read (the engine follows an upgrade with it); the
	// connect's stays the discovery's.
	if v, made := c.SeenVersion(), c.ServerVersion(); v != "v1.37.1-fake" || made != "v1.37.1-fake" {
		t.Errorf("seen %q, connect's %q", v, made)
	}
	// The anonymous probe gets the server's 401: it answered.
	if err := c.Reachable(ctx); err != nil {
		t.Fatalf("reachable: %v", err)
	}
	p.Freeze()
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if err := c.Heartbeat(short); err == nil || errors.Is(err, ErrLoginRejected) {
		t.Fatalf("heartbeat on a silent connection: %v", err)
	}
	short2, cancel2 := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel2()
	if err := c.Reachable(short2); err == nil {
		t.Fatal("reachable on a silent connection")
	}
	p.Thaw()
	// The probe that timed out on the frozen proxy can leave its HTTP/2 connection closing while
	// the next one starts ("client connection force closed", CI 2026-10-16); the reconnect loop
	// tries again on its backoff, so the test waits for the condition, as the loop would.
	var lastErr error
	wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
	defer wcancel()
	if !testkit.WaitFor(wctx, func() bool { lastErr = c.Reachable(ctx); return lastErr == nil }) {
		t.Fatalf("reachable after the thaw: %v", lastErr)
	}
	if got := srv.Requests()["discovery"]; got == 0 {
		t.Errorf("no probe reached the server: %v", srv.Requests())
	}
	for _, e := range []error{c.Heartbeat(ctx), c.Reachable(ctx)} {
		if e != nil && strings.Contains(e.Error(), secretToken) {
			t.Fatalf("an error carries the token: %v", e)
		}
	}
}

func TestHeartbeatSaysTheLoginIsRejected(t *testing.T) {
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "the-right-token"})
	defer srv.Close()
	path := writeFile(t, "kubeconfig", string(srv.KubeconfigWithToken("fake", "", "the-right-token")))
	c, err := Connect(context.Background(), Options{KubeconfigPaths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// The token the connection holds stops working (the cluster revoked or rotated it).
	srv.SetToken("another-token")
	for range 2 { // the guard's first 401 and the ones it turns into ErrLoginRejected
		if err := c.Heartbeat(context.Background()); !errors.Is(err, ErrLoginRejected) {
			t.Fatalf("heartbeat with a rejected login: %v", err)
		}
	}
}

// fakeTransport answers with the codes it is given, one per request.
type fakeTransport struct {
	codes []int
	n     int
}

func (f *fakeTransport) RoundTrip(*http.Request) (*http.Response, error) {
	code := f.codes[min(f.n, len(f.codes)-1)]
	f.n++
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}}, nil
}

func TestTheLoginGuardHandsOnOneUnauthorizedAfterAWorkingRequest(t *testing.T) {
	g := newLoginGuard()
	ft := &fakeTransport{codes: []int{http.StatusUnauthorized, http.StatusUnauthorized, http.StatusOK, http.StatusUnauthorized, http.StatusUnauthorized, http.StatusUnauthorized}}
	rt := g.wrap(ft)
	req, _ := http.NewRequest(http.MethodGet, "https://example.test/version", nil)
	want := []string{"401", "rejected", "200", "401", "rejected", "401"}
	for i, w := range want {
		if i == 5 {
			g.armed.Store(true) // ArmLogin: the user logs in again
		}
		resp, err := rt.RoundTrip(req)
		got := ""
		switch {
		case errors.Is(err, ErrLoginRejected):
			got = "rejected"
		case err != nil:
			t.Fatal(err)
		default:
			switch resp.StatusCode {
			case http.StatusUnauthorized:
				got = "401"
			case http.StatusOK:
				got = "200"
			default:
				got = http.StatusText(resp.StatusCode)
			}
		}
		if got != w {
			t.Fatalf("request %d: %s, want %s", i, got, w)
		}
	}
}

func TestResetConnectionsEndsAWatchOnADeadConnection(t *testing.T) {
	c, _, p := connectThroughProxy(t, testkit.FakeAPIOptions{}, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := c.Kube().CoreV1().Namespaces().Watch(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	p.Freeze()
	defer p.Thaw()
	c.ResetConnections()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-w.ResultChan():
			if !ok {
				return // ended at once, not after HTTP/2's health check
			}
		case <-deadline:
			t.Fatal("the watch on the dead connection did not end")
		}
	}
}
