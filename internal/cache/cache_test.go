package cache_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The warm period runs on the bubble's clock (testing/synctest).
func TestSharedInformerWarmPeriodAndTransform(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := testkit.NewMemorySource()
		pod := testkit.Pod("default", "web-0")
		pod.Object["metadata"].(map[string]any)["managedFields"] = []any{map[string]any{"manager": "kubectl"}}
		src.Set(testkit.PodsGVR, pod)

		c := cache.New(src, cache.Options{WarmPeriod: 200 * time.Millisecond})
		defer c.Close()
		scope := cache.Scope{GVR: testkit.PodsGVR}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		h1 := c.Acquire(scope)
		h2 := c.Acquire(scope)
		if !h1.WaitForSync(ctx) {
			t.Fatal("not synced")
		}
		got, ok := h2.Get("default", "web-0")
		if !ok {
			t.Fatal("pod not in cache")
		}
		if _, found := got.Object["metadata"].(map[string]any)["managedFields"]; found {
			t.Error("managedFields not stripped")
		}
		if src.Lists() != 1 {
			t.Errorf("two handles cost %d lists, want 1", src.Lists())
		}

		events := make(chan cache.Event, 4)
		unsub := h1.Subscribe(func(ev cache.Event) { events <- ev })
		src.Set(testkit.PodsGVR, testkit.Pod("default", "web-1"))
		select {
		case ev := <-events:
			if ev.Type != cache.Added || ev.Object.GetName() != "web-1" {
				t.Errorf("event %+v", ev)
			}
		case <-ctx.Done():
			t.Fatal("no event")
		}
		unsub()

		h1.Release()
		h1.Release() // idempotent
		h2.Release()
		h3 := c.Acquire(scope) // within the warm period: no new list
		if !h3.HasSynced() || src.Lists() != 1 {
			t.Errorf("reacquire within warm period: synced=%v lists=%d", h3.HasSynced(), src.Lists())
		}
		h3.Release()
		if !testkit.WaitFor(ctx, func() bool { return len(c.Stats()) == 0 }) {
			t.Fatal("informer still running after the warm period")
		}
		h4 := c.Acquire(scope)
		defer h4.Release()
		if !h4.WaitForSync(ctx) || src.Lists() != 2 {
			t.Errorf("after expiry: lists=%d, want 2", src.Lists())
		}
	})
}

// TestReacquireWithinWarmPeriodCostsNothing is P10 in memory: a view reopened within the warm
// period gets synced rows at once and costs no list and no watch.
func TestReacquireWithinWarmPeriodCostsNothing(t *testing.T) {
	src := testkit.NewMemorySource()
	for i := range 100 {
		src.Set(testkit.PodsGVR, testkit.Pod("default", fmt.Sprintf("web-%d", i)))
	}
	c := cache.New(src, cache.Options{WarmPeriod: time.Minute})
	defer c.Close()
	ctx := testContext(t)
	scope := cache.Scope{GVR: testkit.PodsGVR}

	h := c.Acquire(scope)
	if !h.WaitForSync(ctx) {
		t.Fatal("not synced")
	}
	lists, watches := src.Lists(), src.Watches()
	h.Release()
	for range 10 {
		h = c.Acquire(scope)
		if !h.HasSynced() || len(h.List()) != 100 {
			t.Fatalf("reacquired handle: synced=%v rows=%d", h.HasSynced(), len(h.List()))
		}
		h.Release()
	}
	if src.Lists() != lists || src.Watches() != watches {
		t.Errorf("reacquiring cost %d lists and %d watches, want 0 and 0", src.Lists()-lists, src.Watches()-watches)
	}
	st := c.Stats()
	if len(st) != 1 || st[0].Handles != 0 || st[0].WarmUntil.IsZero() || st[0].Objects != 100 {
		t.Errorf("stats %+v", st)
	}
}

func TestNamespaceScopesAreSeparateInformers(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("a", "p1"))
	src.Set(testkit.PodsGVR, testkit.Pod("b", "p2"))
	c := cache.New(src, cache.Options{})
	defer c.Close()
	ctx := testContext(t)

	all := c.Acquire(cache.Scope{GVR: testkit.PodsGVR})
	inA := c.Acquire(cache.Scope{GVR: testkit.PodsGVR, Namespace: "a"})
	defer all.Release()
	defer inA.Release()
	if !all.WaitForSync(ctx) || !inA.WaitForSync(ctx) {
		t.Fatal("not synced")
	}
	if got := names(all.List()); fmt.Sprint(got) != "[p1 p2]" {
		t.Errorf("all namespaces: %v", got)
	}
	if got := names(inA.List()); fmt.Sprint(got) != "[p1]" {
		t.Errorf("namespace a: %v", got)
	}
	if got := names(all.ListNamespace("b")); fmt.Sprint(got) != "[p2]" {
		t.Errorf("ListNamespace(b): %v", got)
	}
	if len(c.Stats()) != 2 || src.Lists() != 2 {
		t.Errorf("informers=%d lists=%d, want 2 and 2", len(c.Stats()), src.Lists())
	}
}

// flakySource fails the first failLists List calls, then delegates to a MemorySource.
type flakySource struct {
	*testkit.MemorySource
	failLists atomic.Int64
}

func (f *flakySource) ListerWatcher(scope cache.Scope) toolscache.ListerWatcher {
	return &flakyLW{f: f, inner: f.MemorySource.ListerWatcher(scope)}
}

type flakyLW struct {
	f     *flakySource
	inner toolscache.ListerWatcher
}

func (l *flakyLW) IsWatchListSemanticsUnSupported() bool { return true }

func (l *flakyLW) List(opts metav1.ListOptions) (runtime.Object, error) {
	if l.f.failLists.Add(-1) >= 0 {
		return nil, errors.New("connection refused (test)")
	}
	return toolscache.ToListerWithContext(l.inner).ListWithContext(context.Background(), opts)
}

func (l *flakyLW) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	return toolscache.ToWatcherWithContext(l.inner).WatchWithContext(context.Background(), opts)
}

// gatedSource holds every list until its gate closes.
type gatedSource struct {
	*testkit.MemorySource
	gate chan struct{}
}

func (g *gatedSource) ListerWatcher(scope cache.Scope) toolscache.ListerWatcher {
	return &gatedLW{gate: g.gate, inner: g.MemorySource.ListerWatcher(scope)}
}

type gatedLW struct {
	gate  chan struct{}
	inner toolscache.ListerWatcher
}

func (l *gatedLW) IsWatchListSemanticsUnSupported() bool { return true }

func (l *gatedLW) List(opts metav1.ListOptions) (runtime.Object, error) {
	<-l.gate
	return toolscache.ToListerWithContext(l.inner).ListWithContext(context.Background(), opts)
}

func (l *gatedLW) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	return toolscache.ToWatcherWithContext(l.inner).WatchWithContext(context.Background(), opts)
}

// client-go's retry after the failed list runs on the bubble's clock (testing/synctest).
func TestErrorStateSetThenClearedWithoutEvents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// An empty resource: recovery produces no event, so the error must clear on the successful
		// list itself.
		src := &flakySource{MemorySource: testkit.NewMemorySource()}
		src.failLists.Store(1)
		c := cache.New(src, cache.Options{})
		defer c.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		h := c.Acquire(cache.Scope{GVR: testkit.ServicesGVR})
		defer h.Release()
		if !testkit.WaitFor(ctx, func() bool { return h.Err() != nil }) {
			t.Fatal("the failed list did not set Err")
		}
		if st := c.Stats(); len(st) != 1 || st[0].Error == "" {
			t.Errorf("stats do not show the error: %+v", st)
		}
		if !h.WaitForSync(ctx) {
			t.Fatal("not synced after the list recovered")
		}
		if !testkit.WaitFor(ctx, func() bool { return h.Err() == nil }) {
			t.Fatalf("Err not cleared after recovery: %v", h.Err())
		}
	})
}

// panicSource panics where the cache calls into it.
type panicSource struct{ inLW bool }

func (p panicSource) ListerWatcher(cache.Scope) toolscache.ListerWatcher {
	if p.inLW {
		panic("source bug")
	}
	return panicLW{}
}

type panicLW struct{}

func (panicLW) IsWatchListSemanticsUnSupported() bool             { return true }
func (panicLW) List(metav1.ListOptions) (runtime.Object, error)   { panic("list bug") }
func (panicLW) Watch(metav1.ListOptions) (watch.Interface, error) { panic("watch bug") }

func TestSourcePanicsBecomeErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  cache.Source
	}{
		{"ListerWatcher panics", panicSource{inLW: true}},
		{"List panics", panicSource{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := cache.New(tt.src, cache.Options{})
			defer c.Close()
			h := c.Acquire(cache.Scope{GVR: testkit.PodsGVR})
			defer h.Release()
			if !testkit.WaitFor(testContext(t), func() bool { return h.Err() != nil }) {
				t.Fatal("no error state")
			}
			if h.HasSynced() {
				t.Error("synced although every list fails")
			}
		})
	}
}

func TestSubscribersMayPanicAndUnsubscribeInsideCallback(t *testing.T) {
	src := testkit.NewMemorySource()
	c := cache.New(src, cache.Options{})
	defer c.Close()
	ctx := testContext(t)
	h := c.Acquire(cache.Scope{GVR: testkit.PodsGVR})
	defer h.Release()
	if !h.WaitForSync(ctx) {
		t.Fatal("not synced")
	}

	h.Subscribe(func(cache.Event) { panic("subscriber bug") })
	var once, again atomic.Int64
	var cancelOnce func()
	cancelOnce = h.Subscribe(func(cache.Event) {
		once.Add(1)
		cancelOnce()                                    // cancel from inside the callback: no deadlock
		h.Subscribe(func(cache.Event) { again.Add(1) }) // subscribe from inside, too
	})
	src.Set(testkit.PodsGVR, testkit.Pod("default", "a"))
	src.Set(testkit.PodsGVR, testkit.Pod("default", "b"))
	if !testkit.WaitFor(ctx, func() bool { return again.Load() >= 1 }) {
		t.Fatal("the subscription made inside a callback saw no event")
	}
	if once.Load() != 1 {
		t.Errorf("cancelled subscriber called %d times, want 1", once.Load())
	}
	if _, ok := h.Get("default", "b"); !ok {
		t.Error("informer stopped after a subscriber panicked")
	}
}

func TestConcurrentAcquireReleaseSubscribe(t *testing.T) {
	src := testkit.NewMemorySource()
	c := cache.New(src, cache.Options{WarmPeriod: 20 * time.Millisecond})
	ctx := testContext(t)
	scopes := []cache.Scope{
		{GVR: testkit.PodsGVR},
		{GVR: testkit.PodsGVR, Namespace: "default"},
		{GVR: testkit.ServicesGVR},
	}

	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Go(func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			src.Set(testkit.PodsGVR, testkit.Pod("default", fmt.Sprintf("p-%d", i%50)))
			src.Set(testkit.ServicesGVR, testkit.Service("default", fmt.Sprintf("s-%d", i%50), "10.0.0.1"))
			time.Sleep(time.Millisecond)
		}
	})

	var workers sync.WaitGroup
	for w := range 16 {
		workers.Go(func() {
			for i := range 60 {
				h := c.Acquire(scopes[(w+i)%len(scopes)])
				unsub := h.Subscribe(func(ev cache.Event) { _ = ev.Object.GetName() })
				_ = h.List()
				_, _ = h.Get("default", "p-1")
				_ = h.Err()
				_ = c.Stats()
				if i%7 == 0 {
					sctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
					h.WaitForSync(sctx)
					cancel()
				}
				unsub()
				h.Release()
				if i%10 == 0 {
					time.Sleep(25 * time.Millisecond) // let some informers expire meanwhile
				}
			}
		})
	}
	workers.Wait()
	close(stop)
	writer.Wait()

	if !testkit.WaitFor(ctx, func() bool { return len(c.Stats()) == 0 }) {
		t.Errorf("informers still running after every handle was released: %+v", c.Stats())
	}
	c.Close()
	if h := c.Acquire(scopes[0]); h.HasSynced() || h.List() != nil {
		t.Error("Acquire after Close returned a live handle")
	}
}

func TestCloseStopsInformersAndWaits(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.PodsGVR, testkit.Pod("default", "a"))
	c := cache.New(src, cache.Options{})
	ctx := testContext(t)
	h := c.Acquire(cache.Scope{GVR: testkit.PodsGVR})
	if !h.WaitForSync(ctx) {
		t.Fatal("not synced")
	}
	c.Close()
	c.Close() // idempotent
	if len(c.Stats()) != 0 {
		t.Errorf("stats after Close: %+v", c.Stats())
	}
	if _, ok := h.Get("default", "a"); !ok {
		t.Error("a handle lost its data on Close")
	}
	h.Release() // after Close: no panic, no timer
	sctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if c.Acquire(cache.Scope{GVR: testkit.PodsGVR}).WaitForSync(sctx) {
		t.Error("handle acquired after Close synced")
	}
}

func TestDynamicSourceWithFakeClient(t *testing.T) {
	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{testkit.PodsGVR: "PodList", testkit.NamespacesGVR: "NamespaceList"},
		testkit.Pod("a", "p1"), testkit.Pod("b", "p2"),
		testkit.Object("v1", "Namespace", "", "a", nil),
	)
	c := cache.New(cache.DynamicSource(client, nil), cache.Options{})
	defer c.Close()
	ctx := testContext(t)

	inA := c.Acquire(cache.Scope{GVR: testkit.PodsGVR, Namespace: "a"})
	all := c.Acquire(cache.Scope{GVR: testkit.PodsGVR})
	nss := c.Acquire(cache.Scope{GVR: testkit.NamespacesGVR})
	defer inA.Release()
	defer all.Release()
	defer nss.Release()
	for _, h := range []*cache.Handle{inA, all, nss} {
		if !h.WaitForSync(ctx) {
			t.Fatalf("%v not synced: %v", h.Scope(), h.Err())
		}
	}
	if got := names(inA.List()); fmt.Sprint(got) != "[p1]" {
		t.Errorf("namespace a: %v", got)
	}
	if got := names(all.List()); fmt.Sprint(got) != "[p1 p2]" {
		t.Errorf("all namespaces: %v", got)
	}
	if _, ok := nss.Get("", "a"); !ok {
		t.Error("cluster-scoped namespace not in cache")
	}

	events := make(chan cache.Event, 1)
	defer all.Subscribe(func(ev cache.Event) { events <- ev })()
	if _, err := client.Resource(testkit.PodsGVR).Namespace("b").Create(ctx, testkit.Pod("b", "p3"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		if ev.Object.GetName() != "p3" {
			t.Errorf("event for %s", ev.Object.GetName())
		}
	case <-ctx.Done():
		t.Fatal("no watch event from the dynamic client")
	}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func names(objs []*unstructured.Unstructured) []string {
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.GetName())
	}
	sort.Strings(out)
	return out
}

// TestSyncWaitsForTheInitialListToReachSubscribers: client-go fills the store before it hands the
// initial list to event handlers, so a store-only sync would let a subscriber that subscribes
// "after sync" see late initial additions as changes. While a subscriber still blocks the initial
// list's delivery, the handle is not synced; once it is, a new subscriber sees only changes. The
// wait for late events is the bubble's time (testing/synctest).
func TestSyncWaitsForTheInitialListToReachSubscribers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := testkit.NewMemorySource()
		for i := range 50 {
			src.Set(testkit.PodsGVR, testkit.Pod("default", fmt.Sprintf("web-%d", i)))
		}
		// The list waits until the subscriber is in place: on a slow machine the informer could
		// otherwise deliver the initial list before Subscribe returns, to no subscriber at all.
		gate := make(chan struct{})
		c := cache.New(&gatedSource{MemorySource: src, gate: gate}, cache.Options{})
		defer c.Close()
		h := c.Acquire(cache.Scope{GVR: testkit.PodsGVR})
		defer h.Release()

		release := make(chan struct{})
		var first sync.Once
		reached := make(chan struct{})
		unsub := h.Subscribe(func(cache.Event) {
			first.Do(func() { close(reached) })
			<-release
		})
		defer unsub()
		close(gate)
		select {
		case <-reached:
		case <-time.After(5 * time.Second):
			t.Fatal("the initial list never reached the subscriber")
		}
		if h.HasSynced() {
			t.Error("synced while the initial list is still being delivered")
		}
		close(release)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if !h.WaitForSync(ctx) {
			t.Fatal("not synced")
		}
		var late atomic.Int64
		defer h.Subscribe(func(cache.Event) { late.Add(1) })()
		time.Sleep(200 * time.Millisecond)
		if n := late.Load(); n != 0 {
			t.Errorf("a subscriber added after sync saw %d initial-list events", n)
		}
		src.Set(testkit.PodsGVR, testkit.Pod("default", "web-new"))
		if !testkit.WaitFor(ctx, func() bool { return late.Load() == 1 }) {
			t.Errorf("the change after sync: %d events, want 1", late.Load())
		}
	})
}
