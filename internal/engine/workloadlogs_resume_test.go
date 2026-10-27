package engine

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// serverLogs is a log streamer that behaves like the API server for resumption: each container
// has a log of timestamped lines; a request gets the lines at or after SinceTime (which the API
// carries in whole seconds), or the last TailLines, with their timestamps when asked; a follow
// stream gets the lines added later, until the test drops it (the server ends it), the pod goes,
// or its context ends. A pod that went is refused, as the API server refuses it (NotFound).
type serverLogs struct {
	mu         sync.Mutex
	reqs       []cluster.LogOptions
	log        map[string][]stampedLine
	open       map[string][]*serverStream // oldest first: a restarted container's old and new instance
	dropAtOnce map[string]bool            // end every stream of the key after its first lines
	gone       map[string]bool            // pods deleted at the server
}

type stampedLine struct {
	at   time.Time
	text string
}

type serverStream struct {
	lines      chan stampedLine
	timestamps bool
	done       chan struct{} // closed when the stream has ended, whoever ended it
}

func newServerLogs() *serverLogs {
	return &serverLogs{log: map[string][]stampedLine{}, open: map[string][]*serverStream{}, dropAtOnce: map[string]bool{}, gone: map[string]bool{}}
}

func (l stampedLine) format(timestamps bool) string {
	if timestamps {
		return l.at.Format(time.RFC3339Nano) + " " + l.text + "\n"
	}
	return l.text + "\n"
}

func (f *serverLogs) StreamLogs(ctx context.Context, opts cluster.LogOptions) (io.ReadCloser, error) {
	key := opts.Pod + "/" + opts.Container
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, opts)
	if f.gone[opts.Pod] {
		return nil, fmt.Errorf("logs of %s/%s: %w", opts.Namespace, opts.Pod, apierrors.NewNotFound(testkit.PodsGVR.GroupResource(), opts.Pod))
	}
	var initial []stampedLine
	since := opts.SinceTime.Truncate(time.Second) // RFC 3339 on the wire
	for _, l := range f.log[key] {
		if opts.SinceTime.IsZero() || !l.at.Before(since) {
			initial = append(initial, l)
		}
	}
	if opts.TailLines > 0 && int64(len(initial)) > opts.TailLines {
		initial = initial[int64(len(initial))-opts.TailLines:]
	}
	st := &serverStream{lines: make(chan stampedLine, 64), timestamps: opts.Timestamps, done: make(chan struct{})}
	if opts.Follow && !f.dropAtOnce[key] {
		f.open[key] = append(f.open[key], st)
	} else {
		close(st.lines)
	}
	pr, pw := io.Pipe()
	go func() {
		defer func() { // no longer open, however it ended
			f.mu.Lock()
			f.open[key] = slices.DeleteFunc(f.open[key], func(o *serverStream) bool { return o == st })
			f.mu.Unlock()
			close(st.done)
		}()
		for _, l := range initial {
			if _, err := io.WriteString(pw, l.format(opts.Timestamps)); err != nil {
				return
			}
		}
		for {
			select {
			case l, ok := <-st.lines:
				if !ok {
					_ = pw.Close() // the server ended the stream
					return
				}
				if _, err := io.WriteString(pw, l.format(st.timestamps)); err != nil {
					return
				}
			case <-ctx.Done():
				_ = pw.CloseWithError(ctx.Err())
				return
			}
		}
	}()
	return pr, nil
}

// add appends a line to a container's log and sends it on its newest open stream, unless that
// stream ends first.
func (f *serverLogs) add(key string, at time.Time, text string) {
	f.mu.Lock()
	l := stampedLine{at: at, text: text}
	f.log[key] = append(f.log[key], l)
	var st *serverStream
	if open := f.open[key]; len(open) > 0 {
		st = open[len(open)-1]
	}
	f.mu.Unlock()
	if st != nil {
		select {
		case st.lines <- l:
		case <-st.done:
		}
	}
}

// drop ends a container's oldest open stream, as a server or proxy that closes it.
func (f *serverLogs) drop(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if open := f.open[key]; len(open) > 0 {
		close(open[0].lines)
		f.open[key] = open[1:]
	}
}

// deletePod deletes a pod at the server: its streams end with its containers, and a request for
// its logs is refused from now on.
func (f *serverLogs) deletePod(pod string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gone[pod] = true
	for key, open := range f.open {
		if strings.HasPrefix(key, pod+"/") {
			for _, st := range open {
				close(st.lines)
			}
			delete(f.open, key)
		}
	}
}

func (f *serverLogs) opened(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.open[key]) > 0
}

func (f *serverLogs) requestsFor(key string) []cluster.LogOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []cluster.LogOptions
	for _, r := range f.reqs {
		if r.Pod+"/"+r.Container == key {
			out = append(out, r)
		}
	}
	return out
}

// stamped is a merged stream's line: the prefix, the server's timestamp, the text.
func stamped(prefix string, at time.Time, text string) string {
	return prefix + " " + at.Format(time.RFC3339Nano) + " " + text
}

func fastResumes(t *testing.T, resumes int) {
	t.Helper()
	b, m, n := workloadLogBackoff, workloadLogMaxBackoff, workloadLogResumes
	workloadLogBackoff, workloadLogMaxBackoff, workloadLogResumes = 5*time.Millisecond, 20*time.Millisecond, resumes
	t.Cleanup(func() { workloadLogBackoff, workloadLogMaxBackoff, workloadLogResumes = b, m, n })
}

// slowResumes makes a stream the server ended wait a minute before it opens again, so that what
// the cache shows meanwhile comes first. Called before the session, as fastResumes.
func slowResumes(t *testing.T) {
	t.Helper()
	b, m := workloadLogBackoff, workloadLogMaxBackoff
	workloadLogBackoff, workloadLogMaxBackoff = time.Minute, time.Minute
	t.Cleanup(func() { workloadLogBackoff, workloadLogMaxBackoff = b, m })
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !testkit.WaitFor(ctx, cond) {
		t.Fatalf("timed out waiting for %s", what)
	}
}

// TestWorkloadLogsResumeADroppedStream: a follow stream the server ends while the container runs
// is opened again from the newest line's second (sinceTime, no tail); the lines of that second
// it has are dropped, a line written during the gap arrives, and no line arrives twice.
func TestWorkloadLogsResumeADroppedStream(t *testing.T) {
	fastResumes(t, 5)
	s, _ := logsSession(t)
	f := newServerLogs()
	const key = "db-0/db"
	t0 := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	f.add(key, t0.Add(100*time.Millisecond), "one")
	f.add(key, t0.Add(200*time.Millisecond), "two")
	f.add(key, t0.Add(200*time.Millisecond), "two again") // the same timestamp
	tab, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "db"})
	if err != nil {
		t.Fatal(err)
	}
	line := func(ms int, text string) string {
		return stamped("db-0/db", t0.Add(time.Duration(ms)*time.Millisecond), text)
	}
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	lr.wait(t, line(100, "one"), line(200, "two"), line(200, "two again"))
	waitUntil(t, "the stream to follow", func() bool { return f.opened(key) })
	f.add(key, t0.Add(500*time.Millisecond), "three")
	lr.wait(t, line(500, "three"))

	f.drop(key)
	f.add(key, t0.Add(1700*time.Millisecond), "four") // during the gap, or on the resumed stream
	lr.wait(t, line(1700, "four"))
	waitUntil(t, "the resumed stream to follow", func() bool { return f.opened(key) })
	f.add(key, t0.Add(2*time.Second), "five")
	lr.wait(t, line(2000, "five"))

	want := []string{line(100, "one"), line(200, "two"), line(200, "two again"), line(500, "three"), line(1700, "four"), line(2000, "five")}
	if got := lr.all(); !slices.Equal(got, want) {
		t.Errorf("lines %q, want %q", got, want)
	}
	reqs := f.requestsFor(key)
	if len(reqs) != 2 {
		t.Fatalf("requests %+v", reqs)
	}
	if r := reqs[1]; !r.SinceTime.Equal(t0) || r.TailLines != 0 || !r.Follow || !r.Timestamps {
		t.Errorf("the resumption %+v: want sinceTime %v, no tail", r, t0)
	}
}

// TestWorkloadLogsStopResumingWithoutNewLines: a server that ends the stream at once, again and
// again, gets a bounded number of resumptions; then the stream says it stopped following.
func TestWorkloadLogsStopResumingWithoutNewLines(t *testing.T) {
	fastResumes(t, 2)
	s, _ := logsSession(t)
	f := newServerLogs()
	const key = "db-0/db"
	only := stamped("db-0/db", time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC), "only")
	f.add(key, time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC), "only")
	f.dropAtOnce[key] = true
	tab, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "db"})
	if err != nil {
		t.Fatal(err)
	}
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	waitUntil(t, "the notice", func() bool {
		return slices.ContainsFunc(lr.all(), func(l string) bool { return strings.Contains(l, "stopped following it") })
	})
	// The first stream brought a line; then two resumptions in a row brought none.
	if n := len(f.requestsFor(key)); n != 3 {
		t.Errorf("%d requests, want the first and two resumptions", n)
	}
	if got := lr.all(); slices.Index(got, only) < 0 || len(slices.DeleteFunc(slices.Clone(got), func(l string) bool { return l != only })) != 1 {
		t.Errorf("lines %q: the line once", got)
	}
}

// TestWorkloadLogsNoResumeAfterTheContainerEnded: a stream that ends with its container is not
// opened again.
func TestWorkloadLogsNoResumeAfterTheContainerEnded(t *testing.T) {
	fastResumes(t, 5)
	s, src := logsSession(t)
	f := newServerLogs()
	const key = "db-0/db"
	f.add(key, time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC), "bye")
	tab, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "db"})
	if err != nil {
		t.Fatal(err)
	}
	wl, ok := openTabLogs(t, s, tab.ID, f).(*workloadLogs)
	if !ok {
		t.Fatal("not a workload's logs")
	}
	lr := readLines(wl)
	lr.wait(t, stamped("db-0/db", time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC), "bye"))
	src.Set(testkit.PodsGVR, owned(logPod("shop", "db-0", "db:terminated#1"), "apps/v1", "StatefulSet", "db", "uid-shop-db"))
	waitUntil(t, "the cache to show the container ended", func() bool {
		pod, err := s.Lookup(model.ObjectRef{Kind: "pods", Namespace: "shop", Name: "db-0"})
		return err == nil && model.NestedMap(model.NestedSlice(pod, "status", "containerStatuses")[0], "state", "terminated") != nil
	})
	f.drop(key)
	// A stream that resumes stays in the set through its backoff; this one ended, and a rescan
	// does not start the ended instance again.
	waitUntil(t, "the stream to end", func() bool { return len(streamsOf(wl)) == 0 })
	if targets := wl.scan(); len(targets) != 0 {
		t.Errorf("a rescan starts %+v", targets)
	}
	if n := len(f.requestsFor(key)); n != 1 {
		t.Errorf("%d requests: a stream whose container ended was opened again", n)
	}
}

// TestWorkloadLogsForgetEndedInstances: the set of started streams shrinks with the pods and
// container instances the cache no longer shows.
func TestWorkloadLogsForgetEndedInstances(t *testing.T) {
	fastResumes(t, 5)
	s, src := logsSession(t)
	f := newServerLogs()
	tab, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	wl, ok := openTabLogs(t, s, tab.ID, f).(*workloadLogs)
	if !ok {
		t.Fatal("not a workload's logs")
	}
	keys := func() []string {
		wl.mu.Lock()
		defer wl.mu.Unlock()
		var out []string
		for k := range wl.started {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	waitUntil(t, "three streams", func() bool { return len(keys()) == 3 })

	// web-1-b goes. The server has it first: it ends the pod's stream and refuses its logs while
	// the cache, a watch event behind, still shows the container running, so the stream resumes
	// and is refused. Then the cache shows the pod gone.
	waitUntil(t, "web-1-b's stream", func() bool { return f.opened("web-1-b/app") })
	f.deletePod("web-1-b")
	waitUntil(t, "a resumption refused", func() bool { return len(f.requestsFor("web-1-b/app")) >= 2 })
	src.Delete(testkit.PodsGVR, "shop", "web-1-b")
	waitUntil(t, "web-1-b forgotten", func() bool {
		return !slices.ContainsFunc(keys(), func(k string) bool { return strings.Contains(k, "web-1-b") })
	})

	// web-1-a's app restarts: its new instance is streamed, the old one forgotten once it ended.
	src.Set(testkit.PodsGVR, owned(logPod("shop", "web-1-a", "app:running#1", "proxy"), "apps/v1", "ReplicaSet", "web-1", "uid-shop-web-1"))
	waitUntil(t, "the new instance", func() bool { return len(f.requestsFor("web-1-a/app")) == 2 })
	f.drop("web-1-a/app") // the old instance's stream ends with it
	waitUntil(t, "two streams kept", func() bool {
		k := keys()
		return len(k) == 2 && strings.HasSuffix(k[0], "/app/1") && strings.HasSuffix(k[1], "/proxy/0")
	})
}

// TestWorkloadLogsFollowKeepsTimestampsWhenShown: a tab that shows timestamps gets the server's
// (every tab does since 2026-10-13: the GUI hides them), and a resumption still drops what it has.
func TestWorkloadLogsFollowKeepsTimestampsWhenShown(t *testing.T) {
	fastResumes(t, 5)
	s, _ := logsSession(t)
	f := newServerLogs()
	const key = "db-0/db"
	t0 := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	f.add(key, t0.Add(time.Millisecond), "one")
	tab, _, err := s.OpenWorkloadLogsTab(context.Background(), model.ObjectRef{Kind: "statefulsets", Namespace: "shop", Name: "db"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetLogOptions(tab.ID, func(o *LogOptions) { o.Timestamps = true }); err != nil {
		t.Fatal(err)
	}
	lr := readLines(openTabLogs(t, s, tab.ID, f))
	lr.wait(t, "db-0/db 2026-10-12T09:00:00.001Z one")
	waitUntil(t, "the stream to follow", func() bool { return f.opened(key) })
	f.drop(key)
	waitUntil(t, "the resumption", func() bool { return f.opened(key) })
	f.add(key, t0.Add(2*time.Millisecond), "two")
	lr.wait(t, "db-0/db 2026-10-12T09:00:00.002Z two")
	if got := lr.all(); len(got) != 2 {
		t.Errorf("lines %q", got)
	}
}

func TestSplitLogTimestamp(t *testing.T) {
	for _, tc := range []struct {
		line, rest string
		ok         bool
	}{
		{"2026-10-12T09:00:00.000000001Z hello world\n", "hello world\n", true},
		{"2026-10-12T09:00:00Z \n", "\n", true},
		{"2026-10-12T09:00:00Z\n", "", true}, // an empty line as some runtimes write it
		{"hello world\n", "hello world\n", false},
		{"\n", "\n", false},
		{"2026-13-40T99:00:00Z x\n", "2026-13-40T99:00:00Z x\n", false},
	} {
		_, rest, ok := splitLogTimestamp([]byte(tc.line))
		if ok != tc.ok || string(rest) != tc.rest {
			t.Errorf("%q: %q %v, want %q %v", tc.line, rest, ok, tc.rest, tc.ok)
		}
	}
}
