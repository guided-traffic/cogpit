package cache_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A handle given back with its refusal (ReleaseWithErr) keeps the refusal as its state: its error
// stays, it never syncs, lists nothing and waits for nothing, and its scope stops at once instead
// of being listed again at every step of client-go's backoff; read namespace by namespace, it is
// given back when every namespace refused. A handle without an error stays held.
func TestReleaseWithErrKeepsTheRefusal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		mem := testkit.NewMemorySource()
		podsIn(mem, "team-a")
		src := &testkit.NamespacedOnlySource{Inner: mem, GVRs: []schema.GroupVersionResource{testkit.PodsGVR}, Denied: []string{"team-b", "team-c"}}
		c := cache.New(src, cache.Options{WarmPeriod: time.Minute})
		defer c.Close()

		ok := c.Acquire(cache.Scope{GVR: testkit.PodsGVR, Namespace: "team-a"})
		if !ok.WaitForSync(ctx) {
			t.Fatal(ok.Err())
		}
		if ok.ReleaseWithErr() || !ok.HasSynced() || len(ok.List()) != 2 {
			t.Fatalf("a handle without an error was given back: synced=%v, %d objects", ok.HasSynced(), len(ok.List()))
		}
		ok.Release()
		warm := len(c.Stats()) // team-a's, kept warm

		for _, acquire := range []func() *cache.Handle{
			func() *cache.Handle { return c.Acquire(cache.Scope{GVR: testkit.PodsGVR}) },
			func() *cache.Handle {
				return c.AcquireNamespaces(cache.Scope{GVR: testkit.PodsGVR}, []string{"team-b", "team-c"})
			},
		} {
			h := acquire()
			if !testkit.WaitFor(ctx, func() bool { return cache.IsRefused(h.Err()) && len(h.Err().Error()) > 0 }) {
				t.Fatalf("no refusal: %v", h.Err())
			}
			if h.Namespaces() != nil {
				// Read namespace by namespace: every namespace's refusal is in.
				testkit.WaitFor(ctx, func() bool {
					multi, _ := h.Err().(interface{ Unwrap() []error })
					return multi != nil && len(multi.Unwrap()) == 2
				})
			}
			refusal := h.Err().Error()
			if !h.ReleaseWithErr() {
				t.Fatal("a refused handle was not given back")
			}
			if h.ReleaseWithErr() {
				t.Error("given back twice")
			}
			if n := len(c.Stats()); n != warm || len(cache.Runners(c)) != warm {
				t.Fatalf("given back, the refused scope is still kept: %d scopes, informers %q", n, cache.Runners(c))
			}
			before := src.Calls()
			time.Sleep(5 * time.Second) // a held one lists again by then: client-go's backoff starts at 800 ms
			for k, n := range src.Calls() {
				if n != before[k] {
					t.Errorf("%s: %d more after the handle was given back", k, n-before[k])
				}
			}
			if err := h.Err(); err == nil || err.Error() != refusal || !cache.IsRefused(err) {
				t.Errorf("the kept error %v, want %q", err, refusal)
			}
			if h.HasSynced() || h.List() != nil || h.WaitForSync(ctx) {
				t.Error("a handle given back syncs or lists")
			}
			if _, found := h.Get("team-a", "web-0"); found {
				t.Error("a handle given back gets an object")
			}
			h.Release() // harmless
		}
	})
}
