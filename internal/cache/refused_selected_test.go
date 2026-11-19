package cache_test

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// A label-selected scope the API server refuses is not kept warm either, as an unselected one is
// not (TestRefusedScopeIsNotKeptWarm): released after the refusal it stops at once, and refused
// while only warm it stops then, instead of retrying with client-go's backoff for the whole warm
// period: Argo CD's Repositories and Clusters are label-selected Secrets, and a namespace-limited
// user's Argo CD dashboard reads both in all namespaces first. A selected scope served by its cover is not
// affected: it has no informer of its own.
func TestRefusedSelectedScopeIsNotKeptWarm(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mem := testkit.NewMemorySource()
	podsIn(mem, "team-a")
	src := &testkit.NamespacedOnlySource{Inner: mem, GVRs: []schema.GroupVersionResource{testkit.PodsGVR}}
	c := cache.New(src, cache.Options{WarmPeriod: time.Minute})
	defer c.Close()
	selected := cache.Scope{GVR: testkit.PodsGVR, LabelSelector: "app=web"}

	h := c.Acquire(selected)
	if !testkit.WaitFor(ctx, func() bool { return cache.IsRefused(h.Err()) }) {
		t.Fatalf("no refusal: %v", h.Err())
	}
	h.Release()
	if n := len(c.Stats()); n != 0 || len(cache.Runners(c)) != 0 {
		t.Fatalf("released, the refused selected scope is still kept: %d scopes, informers %q", n, cache.Runners(c))
	}
	before := src.Calls()
	time.Sleep(1200 * time.Millisecond) // longer than client-go's first backoff step (800 ms)
	if after := src.Calls(); len(after) != len(before) || after["list "+selected.String()] != before["list "+selected.String()] {
		t.Errorf("a stopped refused selected scope read again: %v, before %v", after, before)
	}

	// Released before its first answer: warm, then refused, then stopped.
	gated := &gatedAny{inner: src, gate: make(chan struct{})}
	c2 := cache.New(gated, cache.Options{WarmPeriod: time.Minute})
	defer c2.Close()
	h2 := c2.Acquire(selected)
	h2.Release()
	if len(c2.Stats()) != 1 {
		t.Fatal("not warm while its list is pending")
	}
	close(gated.gate)
	if !testkit.WaitFor(ctx, func() bool { return len(c2.Stats()) == 0 && len(cache.Runners(c2)) == 0 }) {
		t.Fatalf("refused while warm, it kept running: %+v", c2.Stats())
	}

	// A selected scope in a namespace the server allows stays warm as before.
	ok := c.Acquire(cache.Scope{GVR: testkit.PodsGVR, Namespace: "team-a", LabelSelector: "app=web"})
	if !ok.WaitForSync(ctx) {
		t.Fatal(ok.Err())
	}
	ok.Release()
	if st := c.Stats(); len(st) != 1 || st[0].WarmUntil.IsZero() {
		t.Errorf("an allowed selected scope is not warm: %+v", st)
	}
}
