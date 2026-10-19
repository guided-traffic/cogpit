package engine

import (
	"bufio"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cluster"
)

// A followed pod log whose stream outlives an interruption and breaks only once the connection is
// back still resumes, with the marker, from its last line: asked by when the stream opened, the
// watch names the interruption it lived through (CI, 2026-10-16: TestStandbyWindowStaysUsable's
// log ended there, since the watch was asked by the time the stream broke, after the reconnect).
func TestAPodLogBrokenAfterTheReconnectResumes(t *testing.T) {
	s, p := watchedSession(t, fastWatch)
	srv := newServerLogs()
	key := "web-1/app"
	t0 := time.Now().UTC().Truncate(time.Second)
	srv.add(key, t0, "before the sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rc, err := s.openResumingLog(ctx, s.Backend(), srv, cluster.LogOptions{Namespace: "default", Pod: "web-1", Container: "app", Follow: true, Timestamps: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(rc)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	next := func(what string) string {
		t.Helper()
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("the log ended before %s", what)
			}
			return l
		case <-ctx.Done():
			t.Fatalf("no %s", what)
		}
		return ""
	}
	if l := next("the first line"); !strings.HasSuffix(l, "before the sleep") {
		t.Fatalf("first line %q", l)
	}
	p.set(func(f *fakeProber) { f.down = true })
	waitState(t, s, Interrupted)
	p.set(func(f *fakeProber) { f.down = false })
	waitState(t, s, Connected)
	srv.drop(key) // the stream breaks only now, after the reconnect
	srv.add(key, t0.Add(2*time.Second), "while it slept")
	if l := next("the marker"); !strings.Contains(l, interruptedMarkerStart) {
		t.Fatalf("after the break: %q, want the interruption's marker", l)
	}
	if l := next("the line after the sleep"); !strings.HasSuffix(l, "while it slept") {
		t.Fatalf("after the marker: %q", l)
	}
}
