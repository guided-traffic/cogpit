package cache_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// TestRunning: a scope runs while a handle holds it and during the warm period, not before and not
// after it expired; asking starts nothing. The warm period runs on the bubble's clock
// (testing/synctest).
func TestRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := testkit.NewMemorySource()
		c := cache.New(src, cache.Options{WarmPeriod: 100 * time.Millisecond})
		defer c.Close()
		scope := cache.Scope{GVR: testkit.PodsGVR}
		if c.Running(scope) || src.Lists() != 0 {
			t.Fatal("running before any handle, or asking listed")
		}
		h := c.Acquire(scope)
		if !c.Running(scope) {
			t.Error("not running while held")
		}
		h.Release()
		if !c.Running(scope) {
			t.Error("not running during the warm period")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if !testkit.WaitFor(ctx, func() bool { return !c.Running(scope) }) {
			t.Error("still running after the warm period")
		}
	})
}
