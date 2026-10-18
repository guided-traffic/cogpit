package terminal

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// outputRecorder reads a session's output in the background, as the terminal widget does.
type outputRecorder struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	err  error
	done chan struct{}
}

func record(r io.Reader) *outputRecorder {
	o := &outputRecorder{done: make(chan struct{})}
	go func() {
		defer close(o.done)
		p := make([]byte, 4096)
		for {
			n, err := r.Read(p)
			o.mu.Lock()
			o.buf.Write(p[:n])
			if err != nil {
				o.err = err
			}
			o.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return o
}

func (o *outputRecorder) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// waitFor waits until the output contains want.
func (o *outputRecorder) waitFor(t *testing.T, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(o.String(), want) {
			return
		}
		select {
		case <-o.done:
			if strings.Contains(o.String(), want) {
				return
			}
			t.Fatalf("output ended (%v) without %q; output:\n%s", o.err, want, o.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("timed out after %v waiting for %q; output:\n%s", timeout, want, o.String())
}

// waitEnd waits until the reader has seen the end of the output and returns the final error.
func (o *outputRecorder) waitEnd(t *testing.T, timeout time.Duration) error {
	t.Helper()
	select {
	case <-o.done:
	case <-time.After(timeout):
		t.Fatalf("output did not end within %v; output:\n%s", timeout, o.String())
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}

func waitClosed(t *testing.T, ch <-chan struct{}, timeout time.Duration, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatalf("%s not closed within %v", what, timeout)
	}
}

// closeOnCleanup closes sess when the test ends and waits until its shell has exited, so that what
// the shell writes on its way out (bash's history into the test's HOME) is done before t.TempDir
// removes that HOME: the cleanups run in reverse order, this one before the directory's. A shell
// still running after 10 s is reported, not waited for.
func closeOnCleanup(t *testing.T, sess Session) {
	t.Helper()
	t.Cleanup(func() {
		_ = sess.Close()
		select {
		case <-sess.Done():
		case <-time.After(10 * time.Second):
			t.Errorf("the shell did not exit within 10s of Close")
		}
	})
}
