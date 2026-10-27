package terminal

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// fakeExecutor stands in for the WebSocket/SPDY executor.
type fakeExecutor func(ctx context.Context, opts remotecommand.StreamOptions) error

func (f fakeExecutor) Stream(opts remotecommand.StreamOptions) error {
	return f(context.Background(), opts)
}

func (f fakeExecutor) StreamWithContext(ctx context.Context, opts remotecommand.StreamOptions) error {
	return f(ctx, opts)
}

// echoContainer behaves like a shell on a TTY that echoes lines, reports resizes, and exits on
// "exit".
func echoContainer(sizes chan<- remotecommand.TerminalSize, connected <-chan struct{}) fakeExecutor {
	return func(ctx context.Context, opts remotecommand.StreamOptions) error {
		if !opts.Tty || opts.Stdin == nil || opts.Stdout == nil || opts.TerminalSizeQueue == nil {
			return errors.New("fake: want a TTY with stdin, stdout and a size queue")
		}
		<-connected // the stream is still connecting until the test says otherwise
		go func() {
			for size := opts.TerminalSizeQueue.Next(); size != nil; size = opts.TerminalSizeQueue.Next() {
				sizes <- *size
			}
		}()
		in := bufio.NewScanner(opts.Stdin)
		for in.Scan() {
			line := in.Text()
			if line == "exit" {
				return nil
			}
			if _, err := io.WriteString(opts.Stdout, "you said: "+line+"\r\n"); err != nil {
				return err
			}
		}
		return ctx.Err()
	}
}

func TestExecSessionStreamsInputOutputAndSizes(t *testing.T) {
	sizes := make(chan remotecommand.TerminalSize, 8)
	connected := make(chan struct{})
	sess := startStream(context.Background(), echoContainer(sizes, connected))
	defer sess.Close()
	out := record(sess)

	// Keystrokes and a size set while the stream is still connecting must neither block nor
	// get lost: they return while it still connects.
	returned := make(chan error, 1)
	go func() {
		_, err := sess.Write([]byte("hello\n"))
		returned <- errors.Join(err, sess.Resize(80, 24))
	}()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("Write and Resize: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Write and Resize blocked while the stream connects")
	}
	close(connected)

	out.waitFor(t, "you said: hello", 5*time.Second)
	if got := <-sizes; got != (remotecommand.TerminalSize{Width: 80, Height: 24}) {
		t.Errorf("first size = %+v, want 80x24", got)
	}
	if err := sess.Resize(132, 50); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-sizes:
		if got != (remotecommand.TerminalSize{Width: 132, Height: 50}) {
			t.Errorf("size = %+v, want 132x50", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resize did not reach the container")
	}

	if _, err := sess.Write([]byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	if err := out.waitEnd(t, 5*time.Second); err != io.EOF {
		t.Errorf("Read at the end: %v, want EOF", err)
	}
	waitClosed(t, sess.Done(), 5*time.Second, "Done")
	if strings.Contains(out.String(), "[cogpit]") {
		t.Errorf("a clean exit printed a message: %q", out.String())
	}
	if err := sess.Resize(10, 10); err == nil {
		t.Error("Resize after the end succeeded")
	}
}

func TestExecSessionShowsStreamErrors(t *testing.T) {
	// The message comes from the cluster: it may carry newlines and escape sequences.
	failing := fakeExecutor(func(context.Context, remotecommand.StreamOptions) error {
		return errors.New("pods \"web-0\" is forbidden: User \"jane\" cannot create resource \"pods/exec\"\nsecond line\x1b]52;c;QUFBQQ==\a")
	})
	sess := startStream(context.Background(), failing)
	defer sess.Close()
	out := record(sess)
	if err := out.waitEnd(t, 5*time.Second); err != io.EOF {
		t.Errorf("Read at the end: %v, want EOF", err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "\r\n[cogpit] shell ended: pods \"web-0\" is forbidden") || !strings.HasSuffix(got, "\r\n") {
		t.Errorf("output = %q, want the error on its own line", got)
	}
	if body := strings.TrimSuffix(strings.TrimPrefix(got, "\r\n"), "\r\n"); strings.ContainsAny(body, "\r\n\x1b\a") {
		t.Errorf("the message keeps control characters: %q", body)
	}
	waitClosed(t, sess.Done(), 5*time.Second, "Done")
}

func TestExecSessionRecoversExecutorPanic(t *testing.T) {
	sess := startStream(context.Background(), fakeExecutor(func(context.Context, remotecommand.StreamOptions) error {
		panic("boom")
	}))
	defer sess.Close()
	out := record(sess)
	_ = out.waitEnd(t, 5*time.Second)
	if !strings.Contains(out.String(), "[cogpit] shell ended: terminal: pod shell stream: internal error: boom") {
		t.Errorf("output = %q", out.String())
	}
}

func TestExecSessionCloseEndsStream(t *testing.T) {
	var mu sync.Mutex
	var streamErr error
	blocking := fakeExecutor(func(ctx context.Context, opts remotecommand.StreamOptions) error {
		_, _ = io.WriteString(opts.Stdout, "$ ")
		<-ctx.Done()
		mu.Lock()
		streamErr = ctx.Err()
		mu.Unlock()
		return ctx.Err()
	})
	sess := startStream(context.Background(), blocking)
	out := record(sess)
	out.waitFor(t, "$ ", 5*time.Second)
	if err := sess.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if err := out.waitEnd(t, 5*time.Second); err != io.EOF {
		t.Errorf("Read after Close: %v, want EOF", err)
	}
	waitClosed(t, sess.Done(), 5*time.Second, "Done")
	mu.Lock()
	defer mu.Unlock()
	if !errors.Is(streamErr, context.Canceled) {
		t.Errorf("stream context: %v, want canceled", streamErr)
	}
	if strings.Contains(out.String(), "[cogpit]") {
		t.Errorf("Close printed a message: %q", out.String())
	}
	if _, err := sess.Write([]byte("x")); err == nil {
		t.Error("Write after Close succeeded")
	}
}

func TestStartPodExecValidates(t *testing.T) {
	client := fake.NewClientset()
	cfg := &rest.Config{Host: "https://127.0.0.1:1"}
	for name, opts := range map[string]ExecOptions{
		"no namespace": {Pod: "p"},
		"no pod":       {Namespace: "ns"},
	} {
		if _, err := StartPodExec(context.Background(), cfg, client, opts); err == nil {
			t.Errorf("%s: StartPodExec succeeded", name)
		}
	}
	if _, err := StartPodExec(context.Background(), nil, client, ExecOptions{Namespace: "ns", Pod: "p"}); err == nil {
		t.Error("StartPodExec without a config succeeded")
	}
}

func TestInputQueueBoundsAndOrder(t *testing.T) {
	q := newInputQueue()
	if _, err := q.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Write([]byte("c")); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Write(make([]byte, maxQueuedInput)); !errors.Is(err, errInputFull) {
		t.Errorf("oversized Write: %v, want errInputFull", err)
	}
	p := make([]byte, 2)
	if n, _ := q.Read(p); string(p[:n]) != "ab" {
		t.Errorf("Read = %q, want ab", p[:n])
	}
	if n, _ := q.Read(p); string(p[:n]) != "c" {
		t.Errorf("Read = %q, want c", p[:n])
	}
	done := make(chan error, 1)
	go func() {
		_, err := q.Read(p)
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	_ = q.Close()
	if err := <-done; err != io.EOF {
		t.Errorf("Read after Close: %v, want EOF", err)
	}
	if _, err := q.Write([]byte("x")); err == nil {
		t.Error("Write after Close succeeded")
	}
}
