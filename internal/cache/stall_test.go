package cache_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// stallMode is how a stallSource answers a namespace's streamed initial list.
type stallMode int

const (
	stallHealthy    stallMode = iota // the object, then the bookmark that ends the initial list
	stallSilent                      // the object, then nothing
	stallUnanswered                  // no answer until the request ends
	stallTrickle                     // an object every few milliseconds, never the end
)

// stallSource streams initial lists per namespace as its modes say and records each watch.
type stallSource struct {
	modes map[string]stallMode

	mu       sync.Mutex
	streamed map[string]int // streamed watches asked for, by namespace
	lists    map[string]int
	watches  []*testWatch
}

func newStallSource(modes map[string]stallMode) *stallSource {
	return &stallSource{modes: modes, streamed: map[string]int{}, lists: map[string]int{}}
}

func (s *stallSource) ListerWatcher(scope cache.Scope) toolscache.ListerWatcher {
	return &stallLW{s: s, ns: scope.Namespace}
}

func (s *stallSource) count(m map[string]int, ns string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return m[ns]
}

func (s *stallSource) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.watches {
		w.Stop()
	}
}

type stallLW struct {
	s  *stallSource
	ns string
}

func (lw *stallLW) List(opts metav1.ListOptions) (runtime.Object, error) {
	return lw.ListWithContext(context.Background(), opts)
}

func (lw *stallLW) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	return lw.WatchWithContext(context.Background(), opts)
}

func (lw *stallLW) ListWithContext(context.Context, metav1.ListOptions) (runtime.Object, error) {
	lw.s.mu.Lock()
	lw.s.lists[lw.ns]++
	lw.s.mu.Unlock()
	l := &unstructured.UnstructuredList{Object: map[string]any{"apiVersion": "v1", "kind": "List"}}
	l.SetResourceVersion("5")
	l.Items = []unstructured.Unstructured{*lw.pod("5")}
	return l, nil
}

func (lw *stallLW) pod(rv string) *unstructured.Unstructured {
	p := testkit.Pod(lw.ns, "web-0")
	p.SetResourceVersion(rv)
	return p
}

func (lw *stallLW) WatchWithContext(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	streamed := opts.SendInitialEvents != nil && *opts.SendInitialEvents
	mode := lw.s.modes[lw.ns]
	lw.s.mu.Lock()
	if streamed {
		lw.s.streamed[lw.ns]++
	}
	lw.s.mu.Unlock()
	if streamed && mode == stallUnanswered {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	w := newTestWatch()
	lw.s.mu.Lock()
	lw.s.watches = append(lw.s.watches, w)
	lw.s.mu.Unlock()
	if !streamed {
		return w, nil
	}
	go func() {
		if !w.send(watch.Event{Type: watch.Added, Object: lw.pod("5")}) {
			return
		}
		switch mode {
		case stallHealthy:
			end := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Pod"}}
			end.SetResourceVersion("5")
			end.SetAnnotations(map[string]string{metav1.InitialEventsAnnotationKey: "true"})
			w.send(watch.Event{Type: watch.Bookmark, Object: end})
		case stallTrickle:
			for rv := 6; ; rv++ {
				time.Sleep(5 * time.Millisecond)
				if !w.send(watch.Event{Type: watch.Modified, Object: lw.pod(strconv.Itoa(rv))}) {
					return
				}
			}
		}
	}()
	return w, nil
}

// testWatch is a watch whose events a test sends; Stop ends every send.
type testWatch struct {
	ch   chan watch.Event
	done chan struct{}
	once sync.Once
}

func newTestWatch() *testWatch {
	return &testWatch{ch: make(chan watch.Event), done: make(chan struct{})}
}

func (w *testWatch) ResultChan() <-chan watch.Event { return w.ch }
func (w *testWatch) Stop()                          { w.once.Do(func() { close(w.done) }) }

func (w *testWatch) send(ev watch.Event) bool {
	select {
	case w.ch <- ev:
		return true
	case <-w.done:
		return false
	}
}

const testStall = 100 * time.Millisecond

// told is what a cache told of its reads: its diagnostics and its stalls.
type told struct {
	mu     sync.Mutex
	diags  []cache.Diagnostic
	stalls []cache.Stall
}

func (t *told) fallbacks() []cache.Diagnostic {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []cache.Diagnostic
	for _, d := range t.diags {
		if d.Kind == cache.DiagStreamFallback {
			out = append(out, d)
		}
	}
	return out
}

func (t *told) stalled() []cache.Stall {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]cache.Stall(nil), t.stalls...)
}

func newStallCache(t *testing.T, src *stallSource, opts cache.Options) (*cache.Cache, *told) {
	t.Helper()
	opts.StreamStall = testStall
	c := cache.New(src, opts)
	t.Cleanup(func() {
		src.stop()
		c.Close()
	})
	got := &told{}
	c.OnDiagnostic(func(d cache.Diagnostic) {
		got.mu.Lock()
		got.diags = append(got.diags, d)
		got.mu.Unlock()
	})
	c.OnStall(func(st cache.Stall) {
		got.mu.Lock()
		got.stalls = append(got.stalls, st)
		got.mu.Unlock()
	})
	return c, got
}

func initialOf(c *cache.Cache, scope cache.Scope) string {
	for _, st := range c.Stats() {
		if st.Scope == scope {
			return st.Initial
		}
	}
	return ""
}

func syncs(t *testing.T, h *cache.Handle) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !h.WaitForSync(ctx) {
		t.Fatalf("%s did not sync", h.Scope())
	}
}

// A stream that goes silent before its end is ended after the bound, the read lists, and so does
// every read after it: a later read asks for no stream at all. The stall is told once, with the
// list's answer, and not as a fallback of the server's.
func TestStalledStreamLists(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     stallMode
		answered bool
		objects  int
	}{
		{name: "silent after an object", mode: stallSilent, answered: true, objects: 1},
		{name: "no answer", mode: stallUnanswered},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := newStallSource(map[string]stallMode{"team-a": tc.mode, "team-b": stallHealthy})
			c, got := newStallCache(t, src, cache.Options{})
			a := cache.Scope{GVR: testkit.PodsGVR, Namespace: "team-a"}
			h := c.Acquire(a)
			defer h.Release()
			start := time.Now()
			syncs(t, h)
			if took := time.Since(start); took > 2*time.Second {
				t.Errorf("synced after %v; the stall bound is %v", took, testStall)
			}
			if !c.Listing() {
				t.Error("the cache does not list after a stall")
			}
			if got := initialOf(c, a); got != cache.InitialFallback {
				t.Errorf("initial = %q, want %q", got, cache.InitialFallback)
			}
			if _, ok := h.Get("team-a", "web-0"); !ok {
				t.Error("the listed pod is not in the store")
			}
			if h.Err() != nil {
				t.Errorf("a stall shows as the read's error: %v", h.Err())
			}
			waitFor(t, func() bool { return len(got.stalled()) > 0 })
			st := got.stalled()[0]
			if st.Scope != a || st.ListErr != nil || st.Listed <= 0 || st.Listed > time.Second {
				t.Errorf("stall = %+v", st)
			}
			var stall *cache.StreamStallError
			if !errors.As(st.Err, &stall) || stall.Answered != tc.answered || stall.Objects != tc.objects || stall.Silent != testStall {
				t.Errorf("stall = %+v", st.Err)
			}

			// A read started after the stall lists without asking for a stream, even where the
			// server would stream.
			b := cache.Scope{GVR: testkit.PodsGVR, Namespace: "team-b"}
			hb := c.Acquire(b)
			defer hb.Release()
			syncs(t, hb)
			if n := src.count(src.streamed, "team-b"); n != 0 {
				t.Errorf("team-b asked for %d streams after the cache lists", n)
			}
			if got := initialOf(c, b); got != cache.InitialList {
				t.Errorf("team-b's initial = %q, want %q", got, cache.InitialList)
			}
			time.Sleep(3 * testStall)
			if n, fb := len(got.stalled()), got.fallbacks(); n != 1 || len(fb) != 0 {
				t.Errorf("%d stalls and fallbacks %+v told; want the one stall", n, fb)
			}
		})
	}
}

// A stream that still delivers when another read's stream stalls ends at once and lists too; the
// stall is told once, by the read that stalled.
func TestStallEndsTheOtherStreams(t *testing.T) {
	src := newStallSource(map[string]stallMode{"team-a": stallSilent, "team-b": stallTrickle})
	c, got := newStallCache(t, src, cache.Options{})
	b := cache.Scope{GVR: testkit.PodsGVR, Namespace: "team-b"}
	hb := c.Acquire(b)
	defer hb.Release()
	waitFor(t, func() bool { return src.count(src.streamed, "team-b") > 0 })
	time.Sleep(2 * testStall) // team-b keeps delivering: no stall of its own
	if c.Listing() || hb.HasSynced() {
		t.Fatalf("listing %v, team-b synced %v before any stall", c.Listing(), hb.HasSynced())
	}
	a := cache.Scope{GVR: testkit.PodsGVR, Namespace: "team-a"}
	ha := c.Acquire(a)
	defer ha.Release()
	syncs(t, ha)
	syncs(t, hb)
	if got := initialOf(c, b); got != cache.InitialFallback {
		t.Errorf("team-b's initial = %q, want %q", got, cache.InitialFallback)
	}
	if n := src.count(src.lists, "team-b"); n == 0 {
		t.Error("team-b did not list")
	}
	time.Sleep(3 * testStall)
	if st, fb := got.stalled(), got.fallbacks(); len(st) != 1 || st[0].Scope != a || len(fb) != 0 {
		t.Errorf("stalls %+v, fallbacks %+v; want team-a's stall only", st, fb)
	}
}

// A stream that ended its initial list may stay silent as long as nothing changes: the bound is for
// the initial list only, and the stream stays the read's watch.
func TestHealthyStreamKeepsStreaming(t *testing.T) {
	src := newStallSource(map[string]stallMode{"team-a": stallHealthy})
	c, got := newStallCache(t, src, cache.Options{})
	a := cache.Scope{GVR: testkit.PodsGVR, Namespace: "team-a"}
	h := c.Acquire(a)
	defer h.Release()
	syncs(t, h)
	time.Sleep(5 * testStall)
	if c.Listing() || len(got.fallbacks()) != 0 || len(got.stalled()) != 0 {
		t.Fatalf("listing %v, fallbacks %+v, stalls %+v after a healthy stream", c.Listing(), got.fallbacks(), got.stalled())
	}
	if got := initialOf(c, a); got != cache.InitialStream {
		t.Errorf("initial = %q, want %q", got, cache.InitialStream)
	}
	src.mu.Lock()
	w := src.watches[len(src.watches)-1]
	src.mu.Unlock()
	changed := testkit.Pod("team-a", "web-0")
	changed.SetResourceVersion("6")
	changed.SetLabels(map[string]string{"changed": "yes"})
	if !w.send(watch.Event{Type: watch.Modified, Object: changed}) {
		t.Fatal("the stream was stopped")
	}
	waitFor(t, func() bool {
		p, ok := h.Get("team-a", "web-0")
		return ok && p.GetLabels()["changed"] == "yes"
	})
	if n := src.count(src.streamed, "team-a"); n != 1 {
		t.Errorf("%d streams asked for, want one", n)
	}
	if n := src.count(src.lists, "team-a"); n != 0 {
		t.Errorf("%d lists, want none", n)
	}
}

// A cluster whose streams are off lists from the start (Options.Lists), and ListFromNowOn ends the
// streams still running: neither is a stall or a fallback.
func TestListsWithoutStreams(t *testing.T) {
	t.Run("from the start", func(t *testing.T) {
		src := newStallSource(map[string]stallMode{"team-a": stallHealthy})
		c, got := newStallCache(t, src, cache.Options{Lists: true})
		a := cache.Scope{GVR: testkit.PodsGVR, Namespace: "team-a"}
		h := c.Acquire(a)
		defer h.Release()
		syncs(t, h)
		if n := src.count(src.streamed, "team-a"); n != 0 || !c.Listing() {
			t.Errorf("%d streams asked for, listing %v", n, c.Listing())
		}
		if got := initialOf(c, a); got != cache.InitialList {
			t.Errorf("initial = %q, want %q", got, cache.InitialList)
		}
		if len(got.stalled()) != 0 || len(got.fallbacks()) != 0 {
			t.Errorf("stalls %+v, fallbacks %+v", got.stalled(), got.fallbacks())
		}
	})
	t.Run("from now on", func(t *testing.T) {
		src := newStallSource(map[string]stallMode{"team-a": stallTrickle})
		c, got := newStallCache(t, src, cache.Options{})
		a := cache.Scope{GVR: testkit.PodsGVR, Namespace: "team-a"}
		h := c.Acquire(a)
		defer h.Release()
		waitFor(t, func() bool { return src.count(src.streamed, "team-a") > 0 })
		c.ListFromNowOn()
		syncs(t, h)
		if n := src.count(src.lists, "team-a"); n == 0 {
			t.Error("team-a did not list")
		}
		time.Sleep(2 * testStall)
		if len(got.stalled()) != 0 || len(got.fallbacks()) != 0 {
			t.Errorf("stalls %+v, fallbacks %+v", got.stalled(), got.fallbacks())
		}
	})
}
