package demo

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
)

// BenchmarkNew builds the default cluster (10,000 pods); it reports the heap the cluster keeps.
func BenchmarkNew(b *testing.B) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	var c *Cluster
	for b.Loop() {
		c = New(Options{Seed: 1, NoChurn: true})
	}
	b.StopTimer()
	runtime.GC()
	runtime.ReadMemStats(&after)
	b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc)/(1<<20), "MB-heap")
	b.ReportMetric(float64(c.Stats().Objects), "objects")
}

// BenchmarkChurn measures the events per second the demo and the real cache sustain together: one
// informer per resource, every event delivered to a subscriber. The generator runs flat out in
// batches of 1,000 and waits while the informers are more than 2,000 events behind, so no watcher
// is closed for falling behind.
func BenchmarkChurn(b *testing.B) {
	c := New(Options{Seed: 1, NoChurn: true})
	ca := cache.New(c.Source(), cache.Options{})
	defer ca.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var delivered atomic.Int64
	for _, gvr := range Resources() {
		h := ca.Acquire(cache.Scope{GVR: gvr})
		defer h.Release()
		if !h.WaitForSync(ctx) {
			b.Fatalf("%s did not sync", gvr.Resource)
		}
		defer h.Subscribe(func(cache.Event) { delivered.Add(1) })()
	}

	b.ResetTimer()
	start := time.Now()
	produced := int64(0)
	for produced < int64(b.N) {
		produced += int64(c.tick(time.Now(), int(min(1_000, int64(b.N)-produced)), false))
		for produced-delivered.Load() > 2_000 {
			runtime.Gosched()
		}
	}
	for delivered.Load() < produced {
		if ctx.Err() != nil {
			b.Fatalf("delivered %d of %d", delivered.Load(), produced)
		}
		runtime.Gosched()
	}
	b.StopTimer()
	b.ReportMetric(float64(produced)/time.Since(start).Seconds(), "events/s")
	if st := c.Stats(); st.Dropped != 0 || st.Expired != 0 {
		b.Errorf("dropped %d, expired %d", st.Dropped, st.Expired)
	}
}
