package cache_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// heldFiller counts the uses of a cache.Held and fills its Memory on the first one, as the
// engine's Applications do.
type heldFiller struct {
	mem   *cache.Memory
	mu    sync.Mutex
	uses  int
	ready chan struct{}
	fail  error
}

func (f *heldFiller) use() (func() error, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, nil, f.fail
	}
	f.uses++
	return func() error { <-f.ready; return nil }, func() { f.mu.Lock(); f.uses--; f.mu.Unlock() }, nil
}

func (f *heldFiller) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uses
}

// TestHeldUsesFollowTheInformer: an informer over a Held source lists only once its filler is
// ready, sees what the filler sets afterwards, holds a use while it runs, and gives every use back
// once it stops (its warm period over); a refused use is the informer's error. The warm period
// runs on the bubble's clock (testing/synctest).
func TestHeldUsesFollowTheInformer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gvr := schema.GroupVersionResource{Group: "cogpit.io", Version: "v1", Resource: "applications"}
		f := &heldFiller{mem: cache.NewMemory(), ready: make(chan struct{})}
		c := cache.New(&cache.Held{Mem: f.mem, Use: f.use}, cache.Options{WarmPeriod: 20 * time.Millisecond})
		defer c.Close()

		h := c.Acquire(cache.Scope{GVR: gvr})
		time.Sleep(50 * time.Millisecond)
		if h.HasSynced() {
			t.Fatal("synced before the filler was ready")
		}
		f.mem.Set(gvr, testkit.Object("cogpit.io/v1", "Application", "shop", "storefront", nil))
		close(f.ready)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if !h.WaitForSync(ctx) || len(h.List()) != 1 {
			t.Fatalf("synced %v with %d objects", h.HasSynced(), len(h.List()))
		}
		f.mem.Set(gvr, testkit.Object("cogpit.io/v1", "Application", "shop", "search", nil))
		waitFor(t, func() bool { return len(h.List()) == 2 })
		if f.count() == 0 {
			t.Error("a running informer holds no use")
		}
		h.Release()
		waitFor(t, func() bool { return f.count() == 0 })

		f.mu.Lock()
		f.fail = errors.New("the cluster connection is closed")
		f.mu.Unlock()
		h = c.Acquire(cache.Scope{GVR: gvr})
		defer h.Release()
		waitFor(t, func() bool { return h.Err() != nil })
	})
}
