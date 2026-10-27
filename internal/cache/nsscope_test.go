package cache_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

func podsIn(src *testkit.MemorySource, namespaces ...string) {
	for _, ns := range namespaces {
		for i := range 2 {
			src.Set(testkit.PodsGVR, testkit.Pod(ns, fmt.Sprintf("web-%d", i)))
		}
	}
}

func nsNames(h *cache.Handle) []string {
	var out []string
	for _, o := range h.List() {
		out = append(out, o.GetNamespace()+"/"+o.GetName())
	}
	slices.Sort(out)
	return out
}

// A scope read in several namespaces is one informer per namespace, shared with every other
// handle on the same namespace's scope and released one by one: the merged handle lists, gets and
// tells about its namespaces' objects only, and each namespace's informer stays warm on its own.
// The warm period runs on the bubble's clock (testing/synctest).
func TestNamespacedScopesSharedAndReleased(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		src := testkit.NewMemorySource()
		podsIn(src, "team-a", "team-b", "team-c")
		c := cache.New(src, cache.Options{WarmPeriod: 300 * time.Millisecond})
		defer c.Close()

		scope := cache.Scope{GVR: testkit.PodsGVR, Namespace: "ignored"}
		merged := c.AcquireNamespaces(scope, []string{"team-b", "team-a", "team-a", ""})
		if got := merged.Namespaces(); !slices.Equal(got, []string{"team-a", "team-b"}) {
			t.Fatalf("namespaces %q", got)
		}
		if merged.Scope().Namespace != "" {
			t.Errorf("the merged scope names a namespace: %v", merged.Scope())
		}
		if !merged.WaitForSync(ctx) {
			t.Fatalf("not synced: %v", merged.Err())
		}
		if got, want := nsNames(merged), []string{"team-a/web-0", "team-a/web-1", "team-b/web-0", "team-b/web-1"}; !slices.Equal(got, want) {
			t.Errorf("list %q, want %q", got, want)
		}
		if len(merged.ListNamespace("team-a")) != 2 || len(merged.ListNamespace("team-c")) != 0 {
			t.Error("ListNamespace reads outside the namespaces")
		}
		if _, ok := merged.Get("team-b", "web-1"); !ok {
			t.Error("Get in a namespace of the set")
		}
		if _, ok := merged.Get("team-c", "web-1"); ok {
			t.Error("Get outside the set")
		}
		if got := cache.Runners(c); !slices.Equal(got, []string{"pods in team-a", "pods in team-b"}) {
			t.Errorf("informers %q, want one per namespace", got)
		}

		// Another consumer of one namespace shares its informer: no list.
		lists := src.Lists()
		one := c.Acquire(cache.Scope{GVR: testkit.PodsGVR, Namespace: "team-a"})
		defer one.Release()
		if !one.HasSynced() || src.Lists() != lists {
			t.Errorf("a shared namespace cost %d lists, synced=%v", src.Lists()-lists, one.HasSynced())
		}

		events := make(chan cache.Event, 8)
		unsub := merged.Subscribe(func(ev cache.Event) { events <- ev })
		src.Set(testkit.PodsGVR, testkit.Pod("team-c", "late"))
		src.Set(testkit.PodsGVR, testkit.Pod("team-b", "late"))
		select {
		case ev := <-events:
			if ev.Object.GetNamespace() != "team-b" || ev.Type != cache.Added {
				t.Errorf("event %s %s/%s", ev.Type, ev.Object.GetNamespace(), ev.Object.GetName())
			}
		case <-ctx.Done():
			t.Fatal("no event from team-b")
		}
		unsub()

		merged.Release()
		merged.Release() // idempotent
		if !testkit.WaitFor(ctx, func() bool { return slices.Equal(cache.Runners(c), []string{"pods in team-a"}) }) {
			t.Fatalf("after the warm period: %q, want team-a's alone (still held)", cache.Runners(c))
		}

		empty := c.AcquireNamespaces(scope, nil)
		if !empty.HasSynced() || len(empty.List()) != 0 || empty.Err() != nil {
			t.Error("a merged handle without namespaces is not an empty, synced one")
		}
		empty.Release()
	})
}

// A namespace the server refuses does not hold the others back: the merged handle syncs, lists
// what it may read and names the refused namespace in its error.
func TestMergedHandleWithARefusedNamespace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mem := testkit.NewMemorySource()
	podsIn(mem, "team-a", "team-c")
	src := &testkit.NamespacedOnlySource{Inner: mem, GVRs: []schema.GroupVersionResource{testkit.PodsGVR}, Denied: []string{"team-c"}}
	c := cache.New(src, cache.Options{})
	defer c.Close()
	h := c.AcquireNamespaces(cache.Scope{GVR: testkit.PodsGVR}, []string{"team-a", "team-c"})
	defer h.Release()
	if !h.WaitForSync(ctx) {
		t.Fatalf("not synced: %v", h.Err())
	}
	if got := nsNames(h); !slices.Equal(got, []string{"team-a/web-0", "team-a/web-1"}) {
		t.Errorf("list %q", got)
	}
	err := h.Err()
	if err == nil || !cache.IsRefused(err) || !strings.HasPrefix(err.Error(), "in team-c: ") || strings.Contains(err.Error(), "team-a") {
		t.Errorf("error %v", err)
	}
}

// A scope the server refuses (403) is not kept warm: released, it stops at once instead of
// retrying for the warm period; refused while only warm, it stops then. A scope a handle holds
// keeps retrying, and its error shows. client-go's backoff runs on the bubble's clock
// (testing/synctest).
func TestRefusedScopeIsNotKeptWarm(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		mem := testkit.NewMemorySource()
		podsIn(mem, "team-a")
		src := &testkit.NamespacedOnlySource{Inner: mem, GVRs: []schema.GroupVersionResource{testkit.PodsGVR}}
		c := cache.New(src, cache.Options{WarmPeriod: time.Minute})
		defer c.Close()
		all := cache.Scope{GVR: testkit.PodsGVR}

		h := c.Acquire(all)
		if !testkit.WaitFor(ctx, func() bool { return cache.IsRefused(h.Err()) }) {
			t.Fatalf("no refusal: %v", h.Err())
		}
		if h.HasSynced() || len(c.Stats()) != 1 {
			t.Errorf("a held refused scope: synced=%v stats=%d", h.HasSynced(), len(c.Stats()))
		}
		h.Release()
		if n := len(c.Stats()); n != 0 || len(cache.Runners(c)) != 0 {
			t.Fatalf("released, the refused scope is still kept: %d scopes, informers %q", n, cache.Runners(c))
		}
		before := src.Calls()["list pods"]
		time.Sleep(5 * time.Second) // a held one lists again by then: client-go's backoff starts at 800 ms
		if after := src.Calls()["list pods"]; after != before {
			t.Errorf("a stopped refused scope listed %d more times", after-before)
		}

		// Released before its first answer: warm, then refused, then stopped.
		gated := &gatedAny{inner: src, gate: make(chan struct{})}
		c2 := cache.New(gated, cache.Options{WarmPeriod: time.Minute})
		defer c2.Close()
		h2 := c2.Acquire(all)
		h2.Release()
		if len(c2.Stats()) != 1 {
			t.Fatal("not warm while its list is pending")
		}
		close(gated.gate)
		if !testkit.WaitFor(ctx, func() bool { return len(c2.Stats()) == 0 && len(cache.Runners(c2)) == 0 }) {
			t.Fatalf("refused while warm, it kept running: %+v", c2.Stats())
		}

		// A namespace the server allows stays warm as before.
		ok := c.Acquire(cache.Scope{GVR: testkit.PodsGVR, Namespace: "team-a"})
		if !ok.WaitForSync(ctx) {
			t.Fatal(ok.Err())
		}
		ok.Release()
		if st := c.Stats(); len(st) != 1 || st[0].WarmUntil.IsZero() {
			t.Errorf("an allowed scope is not warm: %+v", st)
		}
	})
}

// gatedAny holds every list of any source until gate closes (gatedLW, cache_test.go).
type gatedAny struct {
	inner cache.Source
	gate  chan struct{}
}

func (g *gatedAny) ListerWatcher(scope cache.Scope) toolscache.ListerWatcher {
	return &gatedLW{inner: g.inner.ListerWatcher(scope), gate: g.gate}
}

// Many goroutines acquire and release merged handles over overlapping namespaces while objects
// change: every handle ends synced with only its namespaces' objects and events, and every
// informer stops after the warm period (run with the race detector).
func TestConcurrentNamespacedHandles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	src := testkit.NewMemorySource()
	spaces := []string{"ns-0", "ns-1", "ns-2", "ns-3"}
	podsIn(src, spaces...)
	c := cache.New(src, cache.Options{WarmPeriod: 50 * time.Millisecond})
	defer c.Close()
	stop := make(chan struct{})
	var churn sync.WaitGroup
	churn.Add(1)
	go func() {
		defer churn.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(time.Millisecond):
			}
			src.Set(testkit.PodsGVR, testkit.Pod(spaces[i%len(spaces)], fmt.Sprintf("churn-%d", i%7)))
		}
	}()
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 20 {
				want := []string{spaces[(g+i)%4], spaces[(g+i+1)%4]}
				h := c.AcquireNamespaces(cache.Scope{GVR: testkit.PodsGVR}, want)
				unsub := h.Subscribe(func(ev cache.Event) {
					if !slices.Contains(want, ev.Object.GetNamespace()) {
						t.Errorf("an event of %s on a handle of %q", ev.Object.GetNamespace(), want)
					}
				})
				if !h.WaitForSync(ctx) {
					t.Errorf("not synced: %v", h.Err())
				}
				for _, o := range h.List() {
					if !slices.Contains(want, o.GetNamespace()) {
						t.Errorf("an object of %s on a handle of %q", o.GetNamespace(), want)
					}
				}
				unsub()
				h.Release()
			}
		}()
	}
	wg.Wait()
	close(stop)
	churn.Wait()
	if !testkit.WaitFor(ctx, func() bool { return len(cache.Runners(c)) == 0 }) {
		t.Errorf("informers after every release and the warm period: %q", cache.Runners(c))
	}
}

// OnRefused hears of every refused list or watch, of a scope a handle holds and of one that is
// only warm, with the refused scope; an allowed scope is not reported, and a hook that panics
// does not take the informer down.
func TestOnRefusedHearsEveryRefusal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mem := testkit.NewMemorySource()
	podsIn(mem, "team-a")
	src := &testkit.NamespacedOnlySource{Inner: mem, GVRs: []schema.GroupVersionResource{testkit.PodsGVR}}
	c := cache.New(src, cache.Options{WarmPeriod: time.Minute})
	defer c.Close()
	var mu sync.Mutex
	var heard []cache.Scope
	c.OnRefused(func(sc cache.Scope) {
		mu.Lock()
		heard = append(heard, sc)
		mu.Unlock()
		panic("a hook bug")
	})
	got := func() []cache.Scope {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(heard)
	}

	all := cache.Scope{GVR: testkit.PodsGVR}
	h := c.Acquire(all)
	if !testkit.WaitFor(ctx, func() bool { return len(got()) > 0 }) {
		t.Fatalf("a held refused scope was not reported: %v", h.Err())
	}
	if g := got(); g[0] != all {
		t.Errorf("reported %v, want %v", g, all)
	}
	// The hook hears of a refusal before the handle shows it (runner.failed: a reader that sees the
	// error may end the scope, and must know of the refusal by then): wait for the error.
	if !testkit.WaitFor(ctx, func() bool { return cache.IsRefused(h.Err()) }) {
		t.Errorf("the handle's error: %v", h.Err())
	}
	h.Release()

	ok := c.Acquire(cache.Scope{GVR: testkit.PodsGVR, Namespace: "team-a"})
	if !ok.WaitForSync(ctx) {
		t.Fatal(ok.Err())
	}
	ok.Release()
	for _, sc := range got() {
		if sc.Namespace != "" {
			t.Errorf("an allowed scope was reported: %v", sc)
		}
	}

	// Warm only, then refused: reported too.
	gated := &gatedAny{inner: src, gate: make(chan struct{})}
	c2 := cache.New(gated, cache.Options{WarmPeriod: time.Minute})
	defer c2.Close()
	warm := make(chan cache.Scope, 4)
	c2.OnRefused(func(sc cache.Scope) {
		select {
		case warm <- sc:
		default:
		}
	})
	c2.Acquire(all).Release()
	close(gated.gate)
	select {
	case sc := <-warm:
		if sc != all {
			t.Errorf("reported %v", sc)
		}
	case <-ctx.Done():
		t.Fatal("a warm refused scope was not reported")
	}
	c2.OnRefused(nil)
}
