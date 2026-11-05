package engine

import (
	"context"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// requestingPod is a pod in ns on node (none: not scheduled) in phase whose one container
// requests cpu millicores and mem MiB, limited to twice that when limited.
func requestingPod(ns, name, node, phase string, cpu, mem int64, limited bool) *unstructured.Unstructured {
	p := podOnNode(ns, name, node)
	res := map[string]any{"requests": map[string]any{"cpu": fmt.Sprintf("%dm", cpu), "memory": fmt.Sprintf("%dMi", mem)}}
	if limited {
		res["limits"] = map[string]any{"cpu": fmt.Sprintf("%dm", 2*cpu), "memory": fmt.Sprintf("%dMi", 2*mem)}
	}
	_ = unstructured.SetNestedSlice(p.Object, []any{map[string]any{"name": "app", "resources": res}}, "spec", "containers")
	_ = unstructured.SetNestedField(p.Object, phase, "status", "phase")
	return p
}

// TestNamespaceTotalsFollowThePods: a namespace's sum (PodsByNamespace, D-154) moves with every
// pod event, also while the index is built during churn: added, changed, finished, unscheduled
// and deleted pods end up counted exactly as the cache holds them.
func TestNamespaceTotalsFollowThePods(t *testing.T) {
	src := testkit.NewMemorySource()
	const pods = 200
	nss := []string{"shop", "blog", "batch"}
	for i := range pods {
		src.Set(testkit.PodsGVR, requestingPod(nss[i%3], fmt.Sprintf("p-%03d", i), "node-1", "Running", 100, 64, i%2 == 0))
	}
	s, _ := newTestSession(t, src, 5*time.Millisecond)
	connectTest(t, s)
	b := s.Backend()

	stop, churned := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(churned)
		rng := rand.New(rand.NewPCG(7, 11))
		for {
			select {
			case <-stop:
				return
			default:
			}
			i := rng.IntN(pods)
			name, ns := fmt.Sprintf("p-%03d", i), nss[i%3]
			switch rng.IntN(5) {
			case 0:
				src.Delete(testkit.PodsGVR, ns, name)
			case 1:
				src.Set(testkit.PodsGVR, requestingPod(ns, name, "node-1", "Succeeded", 100, 64, true))
			case 2:
				src.Set(testkit.PodsGVR, requestingPod(ns, name, "", "Pending", 100, 64, true))
			default:
				src.Set(testkit.PodsGVR, requestingPod(ns, name, "node-2", "Running", int64(50+rng.IntN(500)), int64(16+rng.IntN(512)), rng.IntN(2) == 0))
			}
		}
	}()
	rs := newRelatedSet(b, []model.RelatedIndex{kinds.PodsByNamespace}, nil)
	defer rs.release()
	rs.waitSynced(context.Background())
	rel, _ := rs.view()
	if _, ok := model.TotalsOf(rel, kinds.PodsByNamespace, "shop"); !ok { // built while the pods change
		t.Fatal("no totals once the pods are listed")
	}
	close(stop)
	<-churned

	podsProf := rs.profiles["pods"]
	truth := func(ns string) model.Totals {
		var sum model.Totals
		for _, o := range KindList(rs.handles["pods"], podsProf) {
			if o.GetNamespace() == ns {
				sum = sum.Add(kinds.PodsByNamespace.Sum(o))
			}
		}
		return sum
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, ns := range append(nss, "empty") {
		var got model.Totals
		if !testkit.WaitFor(ctx, func() bool {
			rel, _ := rs.view()
			var ok bool
			got, ok = model.TotalsOf(rel, kinds.PodsByNamespace, ns)
			return ok && got == truth(ns)
		}) {
			t.Fatalf("%s: %v, the cache holds %v", ns, got, truth(ns))
		}
	}
	if rs.builds.Load() != 1 {
		t.Errorf("the index was built %d times", rs.builds.Load())
	}
}

// TestNamespacesListUsageAndAllocation: the Namespaces list shows each namespace's pods' usage
// summed from the one list of every pod's metrics, and what its scheduled, unfinished pods
// request with their limits, "+" after a limit some pod runs without; a namespace without pods
// is 0; a pod that changes moves the sums at once. The drawer charts nothing for a namespace.
func TestNamespacesListUsageAndAllocation(t *testing.T) {
	mem := testkit.NewMemorySource()
	for _, ns := range []string{"apps", "jobs", "empty"} {
		mem.Set(testkit.NamespacesGVR, testkit.Object("v1", "Namespace", "", ns, map[string]any{"status": map[string]any{"phase": "Active"}}))
	}
	mem.Set(testkit.PodsGVR, requestingPod("apps", "web-0", "node-1", "Running", 250, 64, true))
	mem.Set(testkit.PodsGVR, requestingPod("apps", "far-0", "node-2", "Running", 500, 128, false))
	mem.Set(testkit.PodsGVR, requestingPod("apps", "wait-0", "", "Pending", 1000, 1024, true)) // not scheduled: not counted
	mem.Set(testkit.PodsGVR, requestingPod("jobs", "batch-0", "node-1", "Running", 100, 512, true))
	mem.Set(testkit.PodsGVR, requestingPod("jobs", "done-0", "node-1", "Succeeded", 4000, 4096, true)) // finished: not counted
	prov := &podTableProvider{usage: drawerPodUsage}
	s := New(Options{
		Connector: nsMetricsConnector{src: mem, rev: newFakeReviewer(map[string][]cluster.AccessRule{"": {rule("*", "*", "*")}}), prov: prov,
			namespace: "apps", profiles: []*model.KindProfile{builtinProfile(t, "pods"), builtinProfile(t, "namespaces")}},
		ClusterSettings:  NewClusterSettingsStore(filepath.Join(t.TempDir(), "Cogpit")),
		SnapshotInterval: 5 * time.Millisecond,
	})
	t.Cleanup(s.Close)
	decideIntegrations(t, s, "test")
	connectTest(t, s)
	s.UpdateUI(func(u *UIState) { u.View = "namespaces" })

	cell := func(snap *TableSnapshot, ns, col string) model.Cell {
		ci := slices.IndexFunc(snap.Columns, func(c model.Column) bool { return c.ID == col })
		for i := range snap.Rows {
			if r := &snap.Rows[i]; r.Ref.Name == ns {
				return cellAt(r, ci)
			}
		}
		return model.Cell{}
	}
	want := map[string][4]string{
		// CPU, Memory (drawerPodUsage: web-0 250m 64Mi, far-0 900m 2Gi; batch-0 5m 1.5Gi),
		// CPU Requests (Limits), Memory Requests (Limits)
		"apps":  {"1.150", "2.1 GiB", "0.750 (0.500+)", "192.0 MiB (128.0 MiB+)"},
		"jobs":  {"0.005", "1.5 GiB", "0.100 (0.200)", "512.0 MiB (1.0 GiB)"},
		"empty": {"0.000", "0 B", "0.000 (0.000)", "0 B (0 B)"},
	}
	var snap *TableSnapshot
	waitFor(t, "the namespaces' sums", func() bool {
		snap = s.Table("namespaces")
		if snap == nil || !snap.Synced {
			return false
		}
		for ns, w := range want {
			for i, col := range []string{model.ColumnCPU, model.ColumnMemory, "cpuRequests", "memoryRequests"} {
				if cell(snap, ns, col).Text != w[i] {
					return false
				}
			}
		}
		return true
	})
	if c := cell(snap, "apps", "memoryRequests"); c.Parts == nil || (*c.Parts)[1].Status != model.StatusWarning || !(*c.Parts)[1].Glued {
		t.Errorf("apps' memory: %+v, want an orange glued +", c)
	}
	if c := cell(snap, "apps", "cpuRequests"); c.Parts != nil || c.Num != 750 {
		t.Errorf("apps' CPU: %+v, want a plain cell sorted by 750m", c)
	}
	if n := s.Backend().Metrics.Subscribed(); n != 1 {
		t.Errorf("%d metrics scopes polled, want 1 (every pod)", n)
	}

	mem.Set(testkit.PodsGVR, requestingPod("jobs", "batch-0", "node-1", "Running", 300, 512, true))
	waitFor(t, "jobs' new request", func() bool {
		snap := s.Table("namespaces")
		return snap != nil && cell(snap, "jobs", "cpuRequests").Text == "0.300 (0.600)"
	})

	m, err := s.DetailWait(context.Background(), model.ObjectRef{Context: "test", Kind: "namespaces", Name: "apps"})
	if err != nil || m.Metrics {
		t.Errorf("a namespace's drawer: metrics %v, %v", m != nil && m.Metrics, err)
	}
}
