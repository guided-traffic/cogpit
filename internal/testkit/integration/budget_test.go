// Package integration_test runs the cache on a real client-go stack: cluster.Connect, the
// counting transport, the dynamic client and cache.DynamicSource, against testkit's fake API
// server over HTTPS (with the kind build tag against a kind cluster, with the envtest tag against
// envtest's kube-apiserver). It lives outside cluster and cache because the layering forbids them to
// import each other, tests included.
package integration_test

import (
	"context"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/testkit"
	"github.com/guided-traffic/cogpit/internal/testkit/scorecard"
)

// idle is how long the budget tests leave a synced view alone (P9 in miniature).
const idle = 2 * time.Second

// perResource counts the requests for one resource in a snapshot.
type perResource struct{ list, watch, watchlist, get int64 }

func countFor(st cluster.RequestStats, resource string) perResource {
	return perResource{
		list:      st.ByVerbResource["list "+resource],
		watch:     st.ByVerbResource["watch "+resource],
		watchlist: st.ByVerbResource["watchlist "+resource],
		get:       st.ByVerbResource["get "+resource],
	}
}

// TestBudgetIdleViewAndWarmReopen proves P9 and P10 at the HTTP level, for a server that streams
// initial lists (WatchList) and for one that does not:
//
//   - P9: an open view costs its initial list (or one streaming watch) and one watch, shared by
//     every handle on the same scope; staying idle costs nothing, and changes arrive through the
//     open watch.
//   - P10: reopening the view within the warm period costs no request and shows rows at once.
func TestBudgetIdleViewAndWarmReopen(t *testing.T) {
	for _, tt := range []struct {
		name      string
		watchList bool
		want      perResource // initial cost per resource
	}{
		// One request: the watch with sendInitialEvents=true streams the list and stays open.
		{name: "watchlist", watchList: true, want: perResource{watchlist: 1}},
		// The server refuses the streaming list: client-go lists, then watches from the list's
		// resourceVersion.
		{name: "list and watch", watchList: false, want: perResource{watchlist: 1, list: 1, watch: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{Token: "budget-token", DisableWatchList: !tt.watchList})
			defer srv.Close()
			for i := range 50 {
				srv.Set(testkit.PodsGVR, testkit.Pod(fmt.Sprintf("ns-%d", i%5), fmt.Sprintf("web-%d", i)))
			}
			srv.Set(testkit.ServicesGVR, testkit.Service("ns-0", "web", "10.96.0.10"))

			conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}})
			if err != nil {
				t.Fatalf("Connect: %v", err)
			}
			defer conn.Close()
			c := cache.New(cache.DynamicSource(conn.Dynamic(), conn.Metadata()), cache.Options{WarmPeriod: time.Minute})
			defer c.Close()

			podsScope := cache.Scope{GVR: testkit.PodsGVR}
			view := c.Acquire(podsScope)
			drawer := c.Acquire(podsScope) // a second consumer of the same scope
			services := c.Acquire(cache.Scope{GVR: testkit.ServicesGVR})
			for _, h := range []*cache.Handle{view, drawer, services} {
				if !h.WaitForSync(ctx) {
					t.Fatalf("%v not synced: %v", h.Scope(), h.Err())
				}
			}
			// In list-and-watch mode the watch starts just after the list has synced the store.
			if !testkit.WaitFor(ctx, func() bool { return srv.OpenWatches() == 2 }) {
				t.Fatalf("open watches: %d, want 2 (one per scope)", srv.OpenWatches())
			}
			if n := len(view.List()); n != 50 {
				t.Fatalf("rows: %d, want 50", n)
			}
			synced := conn.Counter().Snapshot()
			for _, res := range []string{"pods", "services"} {
				if got := countFor(synced, res); got != tt.want {
					t.Errorf("initial cost for %s: %+v, want %+v", res, got, tt.want)
				}
			}

			// A change arrives through the open watch, without a request.
			events := make(chan cache.Event, 1)
			unsub := view.Subscribe(func(ev cache.Event) {
				if ev.Object.GetName() == "late" {
					events <- ev
				}
			})
			srv.Set(testkit.PodsGVR, testkit.Pod("ns-0", "late"))
			select {
			case <-events:
			case <-ctx.Done():
				t.Fatal("the new pod did not arrive through the watch")
			}
			unsub()

			time.Sleep(idle)
			afterIdle := conn.Counter().Snapshot()
			scorecard.Record(t, scorecard.Measurement{Budget: "P9", Metric: fmt.Sprintf("requests while an open view stays idle for %s (%s)", idle, tt.name),
				Value: float64(afterIdle.Total - synced.Total), Unit: "requests",
				How: "the cache alone over the real client-go stack against the fake API server, counted by the client's counting transport; not 10 minutes"})
			if afterIdle.Total != synced.Total {
				t.Errorf("P9: idle for %s cost %d requests: %v", idle, afterIdle.Total-synced.Total, diff(synced, afterIdle))
			}
			if srv.OpenWatches() != 2 {
				t.Errorf("open watches after idle: %d, want 2", srv.OpenWatches())
			}

			// P10: close the view and reopen it within the warm period.
			view.Release()
			drawer.Release()
			start := time.Now()
			reopened := c.Acquire(podsScope)
			defer reopened.Release()
			if !reopened.HasSynced() || len(reopened.List()) != 51 {
				t.Errorf("P10: reopened view synced=%v rows=%d, want true and 51", reopened.HasSynced(), len(reopened.List()))
			}
			shown := time.Since(start)
			scorecard.Record(t, scorecard.Measurement{Budget: "P10", Metric: fmt.Sprintf("rows shown after reopening the view in its warm period (%s)", tt.name),
				Value: scorecard.Millis(shown), Unit: "ms", Limit: 100, How: "the cache's handle synced with its rows, no window"})
			scorecard.Record(t, scorecard.Measurement{Budget: "P10", Metric: fmt.Sprintf("requests of reopening the view in its warm period (%s)", tt.name),
				Value: float64(conn.Counter().Snapshot().Total - afterIdle.Total), Unit: "requests"})
			if after := conn.Counter().Snapshot(); after.Total != afterIdle.Total {
				t.Errorf("P10: reopening cost %d requests: %v", after.Total-afterIdle.Total, diff(afterIdle, after))
			}
			if shown > 100*time.Millisecond {
				t.Errorf("P10: rows shown after %s, budget 100ms", shown)
			}

			// The client-side counter and the server agree request by request.
			final := conn.Counter().Snapshot()
			if !maps.Equal(final.ByVerbResource, srv.Requests()) {
				t.Errorf("client counted %v, server served %v", final.ByVerbResource, srv.Requests())
			}
			t.Logf("requests: %v (Connect's discovery included); reopen within warm period took %s", final.ByVerbResource, shown)
		})
	}
}

// TestBudgetExpiryAndRelist shows the other side of the warm period: after it ends the watch is
// closed (the server's watch count drops), and the next acquire pays the initial cost again.
func TestBudgetExpiryAndRelist(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	srv.Set(testkit.PodsGVR, testkit.Pod("default", "web-0"))
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := cache.New(cache.DynamicSource(conn.Dynamic(), conn.Metadata()), cache.Options{WarmPeriod: 200 * time.Millisecond})
	defer c.Close()

	scope := cache.Scope{GVR: testkit.PodsGVR, Namespace: "default"}
	h := c.Acquire(scope)
	if !h.WaitForSync(ctx) {
		t.Fatal("not synced")
	}
	h.Release()
	if !testkit.WaitFor(ctx, func() bool { return srv.OpenWatches() == 0 }) {
		t.Fatal("the watch stayed open after the warm period")
	}
	h = c.Acquire(scope)
	defer h.Release()
	if !h.WaitForSync(ctx) {
		t.Fatal("not synced after expiry")
	}
	if got := countFor(conn.Counter().Snapshot(), "pods"); got.watchlist != 2 || got.list != 0 {
		t.Errorf("after expiry: %+v, want two streaming lists and no list", got)
	}
}

// TestRefusedResourceIsAState checks that a list the server refuses shows as the handle's error
// state and does not stop the informer from retrying.
func TestRefusedResourceIsAState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv := testkit.NewFakeAPIServer(testkit.FakeAPIOptions{})
	defer srv.Close()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{srv.WriteKubeconfig(t, "fake", "")}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := cache.New(cache.DynamicSource(conn.Dynamic(), conn.Metadata()), cache.Options{})
	defer c.Close()

	// The fake server does not serve configmaps: every list and watch answers 404.
	h := c.Acquire(cache.Scope{GVR: testkit.PodsGVR.GroupVersion().WithResource("configmaps")})
	defer h.Release()
	if !testkit.WaitFor(ctx, func() bool { return h.Err() != nil }) {
		t.Fatal("no error state for a resource the server refuses")
	}
	if h.HasSynced() {
		t.Error("synced although the server refuses the resource")
	}
	t.Logf("error state: %v", h.Err())
}

func diff(before, after cluster.RequestStats) map[string]int64 {
	out := map[string]int64{}
	for k, v := range after.ByVerbResource {
		if d := v - before.ByVerbResource[k]; d != 0 {
			out[k] = d
		}
	}
	return out
}
