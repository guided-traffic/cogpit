package cache_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// WarmUntil is zero while every scope is held, the end of the last warm period once a handle is
// released, and zero again when the warm scopes are gone (D-69: a left cluster turns cold). The
// warm period runs on the bubble's clock (testing/synctest).
func TestWarmUntilFollowsTheWarmScopes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		warm := 150 * time.Millisecond
		c := cache.New(testkit.NewMemorySource(), cache.Options{WarmPeriod: warm})
		defer c.Close()
		if got := c.WarmUntil(); !got.IsZero() {
			t.Fatalf("an empty cache: %v", got)
		}
		pods := c.Acquire(cache.Scope{GVR: testkit.PodsGVR})
		svcs := c.Acquire(cache.Scope{GVR: testkit.ServicesGVR})
		defer svcs.Release()
		if got := c.WarmUntil(); !got.IsZero() {
			t.Fatalf("every scope held: %v", got)
		}
		before := time.Now()
		pods.Release()
		got := c.WarmUntil()
		if got.Before(before.Add(warm)) || got.After(time.Now().Add(warm)) {
			t.Fatalf("after the release: %v, want about %v", got, before.Add(warm))
		}
		again := c.Acquire(cache.Scope{GVR: testkit.PodsGVR}) // held again: not warm
		if got := c.WarmUntil(); !got.IsZero() {
			t.Fatalf("acquired again: %v", got)
		}
		again.Release()
		if !testkit.WaitFor(ctx, func() bool { return c.WarmUntil().IsZero() }) {
			t.Fatal("the warm scope never went")
		}
	})
}
