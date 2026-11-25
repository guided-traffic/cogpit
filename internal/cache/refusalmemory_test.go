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

// A refused scope's refusal is remembered for the warm period after its last handle goes: a new
// handle on it reports the refusal at once, without a list, and is given back once by its first
// ReleaseWithErr; ForgetRefusals (cluster.refreshAccess) and the end of the warm period forget it,
// and the next handle lists again.
func TestRefusalIsRememberedForTheWarmPeriod(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		mem := testkit.NewMemorySource()
		podsIn(mem, "team-a")
		src := &testkit.NamespacedOnlySource{Inner: mem, GVRs: []schema.GroupVersionResource{testkit.PodsGVR}}
		c := cache.New(src, cache.Options{WarmPeriod: time.Minute})
		defer c.Close()
		all := cache.Scope{GVR: testkit.PodsGVR}
		refuse := func() {
			t.Helper()
			h := c.Acquire(all)
			if !testkit.WaitFor(ctx, func() bool { return cache.IsRefused(h.Err()) }) {
				t.Fatalf("no refusal: %v", h.Err())
			}
			h.Release()
		}
		lists := func() int { return src.Calls()["list pods"] }

		refuse()
		before := lists()
		h := c.Acquire(all)
		if !cache.IsRefused(h.Err()) || h.HasSynced() || len(c.Stats()) != 0 || len(cache.Runners(c)) != 0 {
			t.Fatalf("acquired again: err %v, synced %v, %d scopes, informers %q", h.Err(), h.HasSynced(), len(c.Stats()), cache.Runners(c))
		}
		if !h.ReleaseWithErr() || h.ReleaseWithErr() {
			t.Error("a remembered refusal is given back exactly once")
		}
		h.Release()
		synctest.Wait()
		if n := lists() - before; n != 0 {
			t.Errorf("a remembered refusal listed %d times", n)
		}

		c.ForgetRefusals()
		refuse()
		if n := lists() - before; n != 1 {
			t.Errorf("after ForgetRefusals the scope listed %d times, want 1", n)
		}

		time.Sleep(time.Minute + time.Second) // the warm period ends
		refuse()
		if n := lists() - before; n != 2 {
			t.Errorf("after the warm period the scope listed %d times in all, want 2", n)
		}
	})
}
