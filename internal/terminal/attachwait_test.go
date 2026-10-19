package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// TestAttachAfterAWait: an attach that waits for its container (a debug container pulling its
// image, D-102) says so first, then, once the wait is over, what it is attached to, then the
// container's output.
func TestAttachAfterAWait(t *testing.T) {
	started := make(chan struct{})
	seen := make(chan remotecommand.StreamOptions, 1)
	opts := AttachOptions{Pod: "web-0", Container: "debugger-abcde", Stdin: true}
	sess := startStreamMode(context.Background(), beforeExecutor{
		exec:   printingContainer(seen),
		before: func(ctx context.Context) error { <-started; return nil },
		after:  attachNotice(opts),
	}, streamMode{stdin: true, what: "attach", notice: "[cogpit] Waiting for debugger-abcde to start…\r\n"})
	defer sess.Close()
	out := record(sess)
	out.waitFor(t, "Waiting for debugger-abcde to start…\r\n", 5*time.Second)
	if strings.Contains(out.String(), "attached to") {
		t.Fatalf("attached before the wait was over: %q", out.String())
	}
	close(started)
	<-seen
	out.waitFor(t, "line one", 5*time.Second)
	got := out.String()
	waiting, attached, line := strings.Index(got, "Waiting"), strings.Index(got, "[cogpit] attached to web-0/debugger-abcde"), strings.Index(got, "line one")
	if waiting < 0 || attached < waiting || line < attached {
		t.Errorf("output out of order: %q", got)
	}
}

// TestStartPodAttachAfterAFailedWait: a wait that fails ends the session with its reason, the
// waiting line before it, and never connects.
func TestStartPodAttachAfterAFailedWait(t *testing.T) {
	client, err := kubernetes.NewForConfig(&rest.Config{Host: "https://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := StartPodAttachAfter(context.Background(), &rest.Config{Host: "https://127.0.0.1:1"}, client,
		AttachOptions{Namespace: "shop", Pod: "web-0", Container: "debugger-abcde", Stdin: true, TTY: true},
		AttachWait{Notice: "[cogpit] Waiting for debugger-abcde to start…", Before: func(context.Context) error {
			return errors.New("debugger-abcde does not start: ImagePullBackOff")
		}})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	out := record(sess)
	_ = out.waitEnd(t, 5*time.Second)
	want := "[cogpit] Waiting for debugger-abcde to start…\r\n\r\n[cogpit] attach ended: debugger-abcde does not start: ImagePullBackOff\r\n"
	if got := out.String(); got != want {
		t.Errorf("output %q,\nwant %q", got, want)
	}
}
