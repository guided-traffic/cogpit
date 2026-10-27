package cache_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

func isSecrets(s cache.Scope) bool { return s.GVR == testkit.SecretsGVR && !s.MetadataOnly }

// TestPeek (H-82): peeking reads the running scopes only, starts nothing and extends no warm
// period; its version changes with every add, change and delete and when the scope starts again,
// and stays the same while nothing happens. The warm period runs on the bubble's clock
// (testing/synctest).
func TestPeek(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := testkit.NewMemorySource()
		src.Set(testkit.SecretsGVR, testkit.Secret("a", "one", nil, map[string]string{"k": "v1"}))
		c := cache.New(src, cache.Options{WarmPeriod: 300 * time.Millisecond})
		defer c.Close()
		empty := c.PeekVersion(isSecrets)
		if got := c.Peek(isSecrets); len(got) != 0 || src.Lists() != 0 {
			t.Fatalf("peeked %d scopes, %d lists, before anything ran", len(got), src.Lists())
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		h := c.Acquire(cache.Scope{GVR: testkit.SecretsGVR})
		if !h.WaitForSync(ctx) {
			t.Fatal("not synced")
		}
		v1 := c.PeekVersion(isSecrets)
		peeked := c.Peek(isSecrets)
		if v1 == empty || len(peeked) != 1 || len(peeked[0].Objects) != 1 || peeked[0].Objects[0].GetName() != "one" {
			t.Fatalf("peeked %+v (version %q)", peeked, v1)
		}
		if c.PeekVersion(isSecrets) != v1 {
			t.Error("the version changed without an event")
		}
		lists := src.Lists()

		src.Set(testkit.SecretsGVR, testkit.Secret("a", "two", nil, map[string]string{"k": "v2"}))
		if !testkit.WaitFor(ctx, func() bool { return c.PeekVersion(isSecrets) != v1 }) {
			t.Fatal("an added Secret kept the version")
		}
		if got := c.Peek(isSecrets)[0].Objects; len(got) != 2 {
			t.Errorf("after the add: %d objects", len(got))
		}
		v2 := c.PeekVersion(isSecrets)
		src.Delete(testkit.SecretsGVR, "a", "one")
		if !testkit.WaitFor(ctx, func() bool { return c.PeekVersion(isSecrets) != v2 }) {
			t.Fatal("a deleted Secret kept the version")
		}

		// Peeking extends no warm period: the scope expires on time while it is peeked at.
		h.Release()
		start := time.Now()
		if !testkit.WaitFor(ctx, func() bool { c.Peek(isSecrets); return !c.Running(cache.Scope{GVR: testkit.SecretsGVR}) }) {
			t.Fatal("peeking kept the scope running")
		}
		if waited := time.Since(start); waited > 3*time.Second {
			t.Errorf("expired after %s", waited)
		}
		if c.PeekVersion(isSecrets) != empty || len(c.Peek(isSecrets)) != 0 {
			t.Error("an expired scope is still peeked")
		}
		if src.Lists() != lists {
			t.Errorf("peeking listed: %d lists, want %d", src.Lists(), lists)
		}

		// The scope started again is another entry: another version, whatever its counts.
		h = c.Acquire(cache.Scope{GVR: testkit.SecretsGVR})
		defer h.Release()
		if !h.WaitForSync(ctx) {
			t.Fatal("not synced again")
		}
		if c.PeekVersion(isSecrets) == v2 || c.PeekVersion(isSecrets) == empty {
			t.Error("a scope started again kept an earlier version")
		}
	})
}
