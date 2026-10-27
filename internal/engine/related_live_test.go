package engine

import (
	"context"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cache"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The related-objects indexes are kept current by their kind's events (related.go): a lookup
// never rebuilds one, and a tracking set (the drawer's) wakes only for the keys it looked up.

func indexNames(li *liveIndex, key string) []string {
	var out []string
	for id := range li.byKey[key] {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// TestLiveIndexFollowsEvents: after any sequence of adds, moves and deletes the live index holds
// what a fresh build from the same objects holds, and apply reports exactly the events that touch
// a watched key: an object entering, changing in or leaving it.
func TestLiveIndexFollowsEvents(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	r := &relatedSet{needs: []model.RelatedIndex{kinds.PodsByNode}, holds: 1, track: true,
		indexes: map[string]*liveIndex{}, pending: map[string]*pendingMoves{}}
	id := kinds.PodsByNode.ID()
	live := map[string]*unstructured.Unstructured{} // namespace/name → the pod as the cache holds it
	r.indexes[id] = buildIndex(kinds.PodsByNode, nil)
	r.indexes[id].watched["node-1"] = true
	nodeOf := func(o *unstructured.Unstructured) string { return model.NestedString(o, "spec", "nodeName") }

	for step := range 2000 {
		name := fmt.Sprintf("p-%02d", rng.IntN(40))
		old := live["apps/"+name]
		var ev cache.Event
		switch {
		case old != nil && rng.IntN(4) == 0:
			ev = cache.Event{Type: cache.Deleted, Object: old}
			delete(live, "apps/"+name)
		default:
			node := fmt.Sprintf("node-%d", rng.IntN(4)) // node-0 is unscheduled: no key
			if node == "node-0" {
				node = ""
			}
			pod := podOnNode("apps", name, node)
			typ := cache.Updated
			if old == nil {
				typ = cache.Added
			}
			ev = cache.Event{Type: typ, Object: pod}
			live["apps/"+name] = pod
		}
		touched := (old != nil && nodeOf(old) == "node-1") || (ev.Type != cache.Deleted && nodeOf(ev.Object) == "node-1")
		if got := r.apply("pods", false, ev); got != touched {
			t.Fatalf("step %d: %s of %s (node %q → %q): relevant %v, want %v", step, ev.Type, name,
				nodeOf(old), nodeOf(ev.Object), got, touched)
		}
	}
	objs := make([]*unstructured.Unstructured, 0, len(live))
	for _, o := range live {
		objs = append(objs, o)
	}
	fresh := buildIndex(kinds.PodsByNode, objs)
	for _, node := range []string{"node-1", "node-2", "node-3", ""} {
		if got, want := indexNames(r.indexes[id], node), indexNames(fresh, node); !slices.Equal(got, want) {
			t.Errorf("node %q: live %v, fresh %v", node, got, want)
		}
	}
	if len(r.indexes[id].keysOf) != len(fresh.keysOf) {
		t.Errorf("%d objects filed, want %d", len(r.indexes[id].keysOf), len(fresh.keysOf))
	}
}

// TestNodeDrawerSharesItsIndex: while a node's drawer is open, its fetches read the index the
// drawer keeps (built once, whatever the pods do), it wakes for its node's pods and not for
// another node's, and a pod moving off the node takes it out of the table.
func TestNodeDrawerSharesItsIndex(t *testing.T) {
	src := testkit.NewMemorySource()
	src.Set(testkit.NodesGVR, testkit.Object("v1", "Node", "", "node-1", nil))
	src.Set(testkit.PodsGVR, podOnNode("apps", "web-0", "node-1"))
	src.Set(testkit.PodsGVR, podOnNode("apps", "api-0", "node-2"))
	s, fc := newTestSession(t, src, 5*time.Millisecond)
	nodes := builtinProfile(t, "nodes")
	fc.profiles = append(testProfiles(), nodes)
	connectTest(t, s)
	ref := model.ObjectRef{Context: "test", Kind: nodes.Key, Name: "node-1"}
	s.UpdateUI(func(u *UIState) { u.Drawer = &ref })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	waitPods := func(want ...string) {
		t.Helper()
		if !testkit.WaitFor(ctx, func() bool {
			m, err := s.Detail(ref)
			return err == nil && slices.Equal(nodePods(m), want)
		}) {
			m, _ := s.Detail(ref)
			t.Fatalf("node's Pods %q, want %q", nodePods(m), want)
		}
	}
	waitPods("web-0")

	b := s.Backend()
	rs := s.drawerRelated(b, nodes, ref)
	if rs == nil {
		t.Fatal("the drawer's related set is not shared with its fetches")
	}
	rs.release()

	// Another node's pods do not wake the drawer: its index watches the keys its lookups asked for,
	// node-1's alone, and a tracking set wakes only for an event that touches a watched key
	// (TestLiveIndexFollowsEvents); a count of the drawer's coalesced events would depend on when
	// the last events of its opening arrive. The next pod of its own node wakes it.
	rs.mu.Lock()
	watched := slices.Sorted(maps.Keys(rs.indexes[kinds.PodsByNode.ID()].watched))
	rs.mu.Unlock()
	if !rs.track || !slices.Equal(watched, []string{"node-1"}) {
		t.Errorf("the drawer's index watches %q (tracking %v), want node-1's key alone", watched, rs.track)
	}
	var mu sync.Mutex
	wakes := 0
	cancelSub := s.Subscribe(func(ev Event) {
		if ev.Type == EventDetail {
			mu.Lock()
			wakes++
			mu.Unlock()
		}
	})
	defer cancelSub()
	woken := func() int {
		mu.Lock()
		defer mu.Unlock()
		return wakes
	}
	for i := range 30 {
		src.Set(testkit.PodsGVR, podOnNode("apps", fmt.Sprintf("api-%d", i), "node-2"))
	}
	before := woken()
	src.Set(testkit.PodsGVR, podOnNode("jobs", "batch-0", "node-1"))
	waitPods("batch-0", "web-0")
	if !testkit.WaitFor(ctx, func() bool { return woken() > before }) {
		t.Error("no detail event for a pod of the drawer's node")
	}

	// A pod that moves off the node (its key changes) leaves the table.
	src.Set(testkit.PodsGVR, podOnNode("apps", "web-0", "node-2"))
	waitPods("batch-0")
	if builds := rs.builds.Load(); builds != 1 {
		t.Errorf("the drawer's index was built %d times, want once", builds)
	}
}

// TestLiveIndexBuiltDuringChurn: indexes built while their kind changes (the events that arrive
// during a build are applied after it) end up holding what the cache holds.
func TestLiveIndexBuiltDuringChurn(t *testing.T) {
	src := testkit.NewMemorySource()
	const pods, nodeCount = 300, 6
	for i := range pods {
		src.Set(testkit.PodsGVR, podOnNode("apps", fmt.Sprintf("p-%03d", i), fmt.Sprintf("node-%d", i%nodeCount)))
	}
	s, fc := newTestSession(t, src, 5*time.Millisecond)
	fc.profiles = append(testProfiles(), builtinProfile(t, "nodes"))
	connectTest(t, s)
	b := s.Backend()

	stop := make(chan struct{})
	churned := make(chan struct{})
	go func() {
		defer close(churned)
		rng := rand.New(rand.NewPCG(3, 5))
		for {
			select {
			case <-stop:
				return
			default:
			}
			name := fmt.Sprintf("p-%03d", rng.IntN(pods))
			if rng.IntN(5) == 0 {
				src.Delete(testkit.PodsGVR, "apps", name)
			} else {
				src.Set(testkit.PodsGVR, podOnNode("apps", name, fmt.Sprintf("node-%d", rng.IntN(nodeCount))))
			}
		}
	}()
	var sets []*relatedSet
	for range 20 {
		rs := newRelatedSetTracking(b, []model.RelatedIndex{kinds.PodsByNode}, nil, true)
		rs.waitSynced(context.Background())
		rel, _ := rs.view()
		rel.Objects(kinds.PodsByNode, "node-0") // builds while the pods change
		sets = append(sets, rs)
	}
	close(stop)
	<-churned
	defer func() {
		for _, rs := range sets {
			rs.release()
		}
	}()

	podsProf := sets[0].profiles["pods"]
	truth := func(node string) []string {
		var out []string
		for _, o := range KindList(sets[0].handles["pods"], podsProf) {
			if model.NestedString(o, "spec", "nodeName") == node {
				out = append(out, o.GetName())
			}
		}
		slices.Sort(out)
		return out
	}
	names := func(objs []*unstructured.Unstructured) []string {
		out := make([]string, 0, len(objs))
		for _, o := range objs {
			out = append(out, o.GetName())
		}
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i, rs := range sets {
		for n := range nodeCount {
			node := fmt.Sprintf("node-%d", n)
			if !testkit.WaitFor(ctx, func() bool {
				rel, _ := rs.view()
				return slices.Equal(names(rel.Objects(kinds.PodsByNode, node)), truth(node))
			}) {
				rel, _ := rs.view()
				t.Fatalf("set %d, %s: %v, the cache holds %v", i, node, names(rel.Objects(kinds.PodsByNode, node)), truth(node))
			}
		}
		if rs.builds.Load() != 1 {
			t.Errorf("set %d built its index %d times", i, rs.builds.Load())
		}
	}
}
