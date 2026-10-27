package demo_test

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	toolscache "k8s.io/client-go/tools/cache"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/demo"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// informers acquires a synced informer for every resource the demo serves and counts the watch
// events each one dispatches after the sync.
type informers struct {
	handles []*cache.Handle
	events  atomic.Int64
}

func syncAll(t *testing.T, c *cache.Cache) *informers {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	inf := &informers{}
	for _, gvr := range demo.Resources() {
		h := c.Acquire(cache.Scope{GVR: gvr})
		t.Cleanup(h.Release)
		if !h.WaitForSync(ctx) {
			t.Fatalf("%s did not sync: %v", gvr.Resource, h.Err())
		}
		t.Cleanup(h.Subscribe(func(cache.Event) { inf.events.Add(1) }))
		inf.handles = append(inf.handles, h)
	}
	return inf
}

// TestChurnReachesTheInformers runs the default churn (1,000 events per second) for half a second
// against the real cache: the informers receive every event and end in the cluster's state.
func TestChurnReachesTheInformers(t *testing.T) {
	d := demo.New(demo.Options{Seed: 2, Pods: 1_000, Deployments: 50, Services: 30, Events: 100})
	c := cache.New(d.Source(), cache.Options{})
	defer c.Close()
	inf := syncAll(t, c)
	before := inf.events.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)
	time.Sleep(500 * time.Millisecond) // how long the churn runs
	d.Stop()

	st := d.Stats()
	if st.EventsSent == 0 {
		t.Fatal("the churn sent no event")
	}
	wctx, wcancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer wcancel()
	if !testkit.WaitFor(wctx, func() bool { return inf.events.Load()-before >= st.EventsSent }) {
		t.Fatalf("informers received %d of %d events", inf.events.Load()-before, st.EventsSent)
	}
	if got := inf.events.Load() - before; got != st.EventsSent {
		t.Errorf("informers received %d events, the demo sent %d", got, st.EventsSent)
	}
	if st.Dropped != 0 || st.Expired != 0 || st.Lists != int64(len(inf.handles)) {
		t.Errorf("dropped %d, expired %d, lists %d: the informers fell behind", st.Dropped, st.Expired, st.Lists)
	}

	// The informers' stores match a fresh list of the cluster.
	for _, h := range inf.handles {
		lw := toolscache.ToListerWatcherWithContext(d.Source().ListerWatcher(cache.Scope{GVR: h.Scope().GVR}))
		obj, err := lw.ListWithContext(context.Background(), metav1.ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		fresh := obj.(*unstructured.UnstructuredList)
		cached := h.List()
		if len(cached) != len(fresh.Items) {
			t.Errorf("%s: %d cached, %d in the cluster", h.Scope().GVR.Resource, len(cached), len(fresh.Items))
			continue
		}
		for _, it := range fresh.Items {
			got, ok := h.Get(it.GetNamespace(), it.GetName())
			if !ok || got.GetResourceVersion() != it.GetResourceVersion() {
				t.Errorf("%s %s/%s: cached %v, cluster resourceVersion %s", h.Scope().GVR.Resource, it.GetNamespace(), it.GetName(), ok, it.GetResourceVersion())
				break
			}
		}
	}
}

// TestChurnKeepsItsPace: the churn sends its events per second, and the rate the status bar shows
// is that pace. The generator's ticker and clock run on a testing/synctest bubble's, so the pace
// is the generator's own and not how busy the machine is.
func TestChurnKeepsItsPace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := demo.New(demo.Options{Seed: 2, Pods: 1_000, Deployments: 50, Services: 30, Events: 100})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		d.Start(ctx)
		time.Sleep(2 * time.Second)
		synctest.Wait() // the tick due now is done
		st := d.Stats()
		d.Stop()
		t.Logf("sent %d events in 2 s; rate shown %.0f/s", st.EventsSent, st.EventsPerSec)
		// 200 ticks of 10 events each; an operation may produce two events, so a tick can send
		// one more than its share, which the next tick sends less.
		if want := int64(2 * demo.DefaultEventsPerSecond); st.EventsSent < want-10 || st.EventsSent > want+10 {
			t.Errorf("sent %d events in 2 s, want %d", st.EventsSent, want)
		}
		if math.Abs(st.EventsPerSec-demo.DefaultEventsPerSecond) > 10 {
			t.Errorf("EventsPerSec %.0f, want %d", st.EventsPerSec, demo.DefaultEventsPerSecond)
		}
	})
}

func TestStartAndStop(t *testing.T) {
	d := demo.New(demo.Options{Seed: 3, Pods: 1_000, Deployments: 50, Services: 30, Events: 100})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	d.Start(ctx)
	d.Start(ctx) // already running: no second generator
	if !testkit.WaitFor(ctx, func() bool { return d.Stats().EventsSent > 100 }) {
		t.Fatal("no churn after Start")
	}
	d.Stop() // returns once the generator has finished
	stopped := d.Stats().EventsSent
	time.Sleep(100 * time.Millisecond)
	if got := d.Stats().EventsSent; got != stopped {
		t.Errorf("%d events after Stop", got-stopped)
	}
	d.Stop() // idempotent

	// Start resumes; the end of the context stops it too.
	runCtx, runCancel := context.WithCancel(ctx)
	d.Start(runCtx)
	if !testkit.WaitFor(ctx, func() bool { return d.Stats().EventsSent > stopped }) {
		t.Fatal("no churn after the second Start")
	}
	runCancel()
	if !quiet(ctx, d, 100*time.Millisecond) {
		t.Error("the churn goes on after its context ended")
	}
	d.Stop()
}

// quiet waits until the cluster sends no event for the period d.
func quiet(ctx context.Context, d *demo.Cluster, period time.Duration) bool {
	for {
		before := d.Stats().EventsSent
		select {
		case <-ctx.Done():
			return false
		case <-time.After(period):
		}
		if d.Stats().EventsSent == before {
			return true
		}
	}
}

func TestNoChurnStaysStatic(t *testing.T) {
	d := demo.New(demo.Options{Seed: 4, Pods: 500, Deployments: 30, Services: 20, Events: 50, NoChurn: true})
	d.Start(context.Background())
	defer d.Stop()
	time.Sleep(50 * time.Millisecond)
	if got := d.Stats().EventsSent; got != 0 {
		t.Errorf("%d events with NoChurn", got)
	}
}
