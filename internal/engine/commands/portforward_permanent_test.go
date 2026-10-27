package commands_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

var podsResource = schema.GroupResource{Resource: "pods"}

// wsaECONNREFUSED is Winsock's refused connection: on Windows a dial fails with it, never with
// syscall.ECONNREFUSED, which Go invents there.
const wsaECONNREFUSED syscall.Errno = 10061

// portFree reports whether nothing listens on the forward's local address any more.
// The forward's port comes from the ephemeral range, so once it is freed another test process may
// take it for an outgoing connection and make Listen fail (a CI flake): a refused dial says no
// listener is left just as well. Either is retried for a moment. The dial waits long enough for
// Windows, which answers a refused local connection only after retrying it for about 2 s.
func portFree(t *testing.T, addr string) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if ln, err := net.Listen("tcp4", addr); err == nil {
			_ = ln.Close()
			return true
		}
		conn, err := net.DialTimeout("tcp4", addr, 3*time.Second)
		if err != nil && (errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, wsaECONNREFUSED)) {
			return true
		}
		if conn != nil {
			_ = conn.Close()
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestPortForwardFailsAtOncePermanentErrors (D-20): a connection that cannot be made, and that
// waiting does not cure, ends a forward that was active as Failed at once, names the cause and
// frees the local port; no dial follows. Which API errors waiting does not cure is
// TestDialErrorsAreClassified's (internal/cluster); the forward treats each alike, and the one that
// waited for its pod is TestPortForwardThatWaitedFailsOnAPermanentError's.
func TestPortForwardFailsAtOncePermanentErrors(t *testing.T) {
	cause := apierrors.NewUnauthorized("token expired")
	h, fw := newForwardHarness(t, engine.Options{})
	var f engine.Forward
	h.must(t, engine.OriginGUI, "pod.portForward", map[string]any{"ref": web0, "port": 8080}, &f)
	fw.fail(cause)
	fw.cut()
	failed := waitForward(t, h.s, f.Ref, "failed", func(f engine.Forward) bool { return f.Phase == engine.ForwardFailed })
	if failed.Listening() || !strings.Contains(failed.Message, "not retried") || !strings.Contains(failed.Message, cause.Error()) {
		t.Errorf("failed forward %+v", failed)
	}
	if !portFree(t, f.Address) {
		t.Errorf("the local port %s is not freed", f.Address)
	}
	n := len(fw.dialed())
	time.Sleep(20 * forwardBackoff) // a loop that went on after the failure dials again after one backoff
	if got := len(fw.dialed()); got != n {
		t.Errorf("%d dials after the failure, was %d", got, n)
	}
}

// TestPortForwardRetriesTransientErrors: a pod that is not running yet (400), an error that passes,
// keeps the forward Reconnecting through its retries, and it connects again once it passes. Which
// API errors pass is TestDialErrorsAreClassified's (internal/cluster); the forward retries each
// alike.
func TestPortForwardRetriesTransientErrors(t *testing.T) {
	h, fw := newForwardHarness(t, engine.Options{})
	var f engine.Forward
	h.must(t, engine.OriginGUI, "pod.portForward", map[string]any{"ref": web0, "port": 8080}, &f)
	fw.fail(apierrors.NewBadRequest("pod does not have a host assigned"))
	n := len(fw.dialed())
	fw.cut()
	waitForward(t, h.s, f.Ref, "two failed retries", func(f engine.Forward) bool {
		return f.Phase == engine.ForwardReconnecting && len(fw.dialed()) >= n+2
	})
	if cur, err := h.s.ForwardOf(f.Ref); err != nil || cur.Phase == engine.ForwardFailed {
		t.Fatalf("a transient error failed the forward: %+v %v", cur, err)
	}
	fw.fail(nil)
	waitForward(t, h.s, f.Ref, "active again", func(f engine.Forward) bool { return f.Phase == engine.ForwardActive })
}

// TestPortForwardThatWaitedFailsOnAPermanentError is the case D-20 names: a forward that first
// waited for its pod, and then could not connect, used to stay Reconnecting for ever.
func TestPortForwardThatWaitedFailsOnAPermanentError(t *testing.T) {
	h, fw := newForwardHarness(t, engine.Options{})
	pending := forwardPod("web-0", false)
	_ = unstructured.SetNestedField(pending.Object, "Pending", "status", "phase")
	h.src.Set(testkit.PodsGVR, pending)
	// The cache sees the pending pod after the source has it: wait for it, or the forward starts
	// against the running pod the harness seeded (a CI flake under the race detector).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, func() bool {
		obj, err := h.s.Lookup(web0)
		phase, _, _ := unstructured.NestedString(objOrEmpty(obj, err), "status", "phase")
		return phase == "Pending"
	}) {
		t.Fatal("the cache never saw the pending pod")
	}
	var f engine.Forward
	h.must(t, engine.OriginGUI, "pod.portForward", map[string]any{"ref": web0, "port": 8080}, &f)
	if f.Phase != engine.ForwardWaiting {
		t.Fatalf("started %+v", f)
	}
	fw.fail(apierrors.NewForbidden(podsResource, "web-0", errors.New("no pods/portforward")))
	h.src.Set(testkit.PodsGVR, forwardPod("web-0", true))
	failed := waitForward(t, h.s, f.Ref, "failed", func(f engine.Forward) bool { return f.Phase == engine.ForwardFailed })
	if !strings.Contains(failed.Message, "forbidden") {
		t.Errorf("failed forward %+v", failed)
	}
	if !portFree(t, f.Address) {
		t.Errorf("the local port %s is not freed", f.Address)
	}
}

// objOrEmpty is a looked-up object's content, or nothing when the lookup failed.
func objOrEmpty(obj *unstructured.Unstructured, err error) map[string]any {
	if err != nil || obj == nil {
		return nil
	}
	return obj.Object
}
