package engine

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// overviewConnector is the test connector with a metrics hub of the test's own.
type overviewConnector struct {
	*fakeConnector
	hub func() *metrics.Hub
}

func (c *overviewConnector) Connect(ctx context.Context, name string) (*Backend, error) {
	b, err := c.fakeConnector.Connect(ctx, name)
	if err == nil && c.hub != nil {
		b.Metrics = c.hub()
	}
	return b, err
}

// clusterOverviewSession is a session over the real profiles of the Overview's kinds.
func clusterOverviewSession(t *testing.T, src *testkit.MemorySource, hub func() *metrics.Hub) *Session {
	t.Helper()
	var profiles []*model.KindProfile
	for _, key := range []string{"nodes", "pods", "deployments", "statefulsets", "daemonsets", "events", "namespaces"} {
		p, ok := kinds.ByKey(key)
		if !ok {
			t.Fatalf("no profile %s", key)
		}
		profiles = append(profiles, p)
	}
	conn := &overviewConnector{fakeConnector: &fakeConnector{src: src, profiles: profiles}, hub: hub}
	s := New(Options{Connector: conn, SnapshotInterval: 5 * time.Millisecond})
	t.Cleanup(s.Close)
	connectTest(t, s)
	return s
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// ovNode is a node with 4 cores and 8 GiB (3.8 cores and 7 GiB allocatable) and room for 110 pods,
// a control-plane node when master, Ready or not since since.
func ovNode(name string, master, ready bool, since time.Time) *unstructured.Unstructured {
	labels := map[string]any{"kubernetes.io/hostname": name}
	if master {
		labels["node-role.kubernetes.io/control-plane"] = ""
	}
	status := "True"
	if !ready {
		status = "False"
	}
	return testkit.Object("v1", "Node", "", name, map[string]any{
		"metadata": map[string]any{"labels": labels},
		"status": map[string]any{
			"allocatable": map[string]any{"cpu": "3800m", "memory": "7Gi", "pods": "110"},
			"capacity":    map[string]any{"cpu": "4", "memory": "8Gi", "pods": "110"},
			"conditions": []any{map[string]any{
				"type": "Ready", "status": status, "lastTransitionTime": stamp(since),
			}},
		},
	})
}

// ovPod is a pod on node with one container requesting cpu and memory and limited to twice that.
func ovPod(ns, name, node, cpu, mem string) *unstructured.Unstructured {
	p := testkit.Pod(ns, name)
	_ = unstructured.SetNestedField(p.Object, node, "spec", "nodeName")
	res := map[string]any{
		"requests": map[string]any{"cpu": cpu, "memory": mem},
		"limits":   map[string]any{"cpu": cpu, "memory": mem},
	}
	_ = unstructured.SetNestedSlice(p.Object, []any{map[string]any{"name": "app", "image": "app:1", "resources": res}}, "spec", "containers")
	return p
}

// overviewFixture is a cluster with a master and two workers (one NotReady), a healthy pod per
// group, a crash-looping pod, a pod nobody can schedule, a finished pod, a Deployment short of a
// ready pod, and events: a recent warning about the crash-looping pod, an old one, a normal one and
// a warning about a custom resource.
func overviewFixture(now time.Time) *testkit.MemorySource {
	src := testkit.NewMemorySource()
	src.Set(testkit.NodesGVR, ovNode("master-1", true, true, now.Add(-48*time.Hour)))
	src.Set(testkit.NodesGVR, ovNode("worker-1", false, true, now.Add(-48*time.Hour)))
	src.Set(testkit.NodesGVR, ovNode("worker-2", false, false, now.Add(-20*time.Minute)))

	src.Set(testkit.PodsGVR, ovPod("kube-system", "etcd", "master-1", "100m", "100Mi"))
	src.Set(testkit.PodsGVR, ovPod("shop", "web-0", "worker-1", "250m", "256Mi"))
	crash := ovPod("shop", "web-1", "worker-1", "100m", "64Mi")
	_ = unstructured.SetNestedSlice(crash.Object, []any{map[string]any{
		"name": "app", "ready": false, "restartCount": int64(5),
		"state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
	}}, "status", "containerStatuses")
	_ = unstructured.SetNestedSlice(crash.Object, []any{map[string]any{
		"type": "Ready", "status": "False", "lastTransitionTime": stamp(now.Add(-7 * time.Minute)),
	}}, "status", "conditions")
	src.Set(testkit.PodsGVR, crash)
	pending := testkit.Pod("infra", "db-0")
	unstructured.RemoveNestedField(pending.Object, "spec", "nodeName")
	unstructured.RemoveNestedField(pending.Object, "status", "containerStatuses")
	_ = unstructured.SetNestedField(pending.Object, "Pending", "status", "phase")
	_ = unstructured.SetNestedSlice(pending.Object, []any{map[string]any{
		"type": "PodScheduled", "status": "False", "reason": "Unschedulable", "lastTransitionTime": stamp(now.Add(-3 * time.Minute)),
	}}, "status", "conditions")
	src.Set(testkit.PodsGVR, pending)
	done := ovPod("shop", "job-0", "worker-1", "2", "1Gi")
	_ = unstructured.SetNestedField(done.Object, "Succeeded", "status", "phase")
	src.Set(testkit.PodsGVR, done)

	src.Set(testkit.DeploymentsGVR, testkit.Object("apps/v1", "Deployment", "shop", "web", map[string]any{
		"metadata": map[string]any{"generation": int64(1)},
		"spec":     map[string]any{"replicas": int64(2)},
		"status": map[string]any{
			"observedGeneration": int64(1), "replicas": int64(2), "updatedReplicas": int64(2), "readyReplicas": int64(1),
			"conditions": []any{map[string]any{"type": "Available", "status": "False", "lastTransitionTime": stamp(now.Add(-6 * time.Minute))}},
		},
	}))
	src.Set(statefulSetsGVR, testkit.Object("apps/v1", "StatefulSet", "infra", "db", map[string]any{
		"spec": map[string]any{"replicas": int64(1)}, "status": map[string]any{"readyReplicas": int64(1)},
	}))

	src.Set(testkit.EventsGVR, event("shop", "e1", "Pod", "web-1", "", "BackOff", "Warning", now.Add(-5*time.Minute)))
	src.Set(testkit.EventsGVR, event("shop", "e2", "Pod", "web-0", "", "Started", "Normal", now.Add(-time.Minute)))
	src.Set(testkit.EventsGVR, event("infra", "e3", "Pod", "db-0", "", "FailedScheduling", "Warning", now.Add(-2*time.Hour)))
	src.Set(testkit.EventsGVR, event("kube-system", "e4", "Widget", "gizmo", "", "Broken", "Warning", now.Add(-time.Minute)))
	return src
}

func issueKeys(issues []ClusterIssue) []string {
	out := make([]string, 0, len(issues))
	for _, is := range issues {
		out = append(out, is.Source+" "+is.Kind+" "+is.Ref.Kind+"/"+is.Ref.Namespace+"/"+is.Name)
	}
	slices.Sort(out)
	return out
}

// TestClusterOverviewIssues: the Issues are the pods with the Pods view's warning, the nodes with the
// Nodes view's, the workloads short of ready pods with nothing under way, and the warning events of
// the last hour about their involved objects, each with its message, severity and since; read from
// the cache alone.
func TestClusterOverviewIssues(t *testing.T) {
	now := time.Now()
	src := overviewFixture(now)
	s := clusterOverviewSession(t, src, nil)
	waitTable(t, s, "pods", func(t *TableSnapshot) bool { return t.Synced }) // the view a connect opens
	lists := src.Lists()
	m, err := s.ClusterOverview(context.Background(), "", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"event Pod pods/shop/web-1", "event Widget events/kube-system/gizmo",
		"status Deployment deployments/shop/web", "status Node nodes//worker-2",
		"status Pod pods/infra/db-0", "status Pod pods/shop/web-1",
	}
	if got := issueKeys(m.Issues); !slices.Equal(got, want) || !m.Synced {
		t.Fatalf("issues (synced %v):\n%s\nwant\n%s", m.Synced, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if src.Lists()-lists != 5 {
		t.Errorf("the read listed %d kinds, want the page's 5 besides the open Pods view's (nodes, events and three workload kinds)", src.Lists()-lists)
	}
	by := map[string]ClusterIssue{}
	for _, is := range m.Issues {
		by[is.Source+" "+is.Ref.Kind+"/"+is.Name] = is
	}
	crash := by["status pods/web-1"]
	if crash.Severity != model.StatusError || !strings.Contains(crash.Message, "CrashLoopBackOff") || !strings.Contains(crash.Message, "5 restarts") ||
		crash.Since.Unix() != now.Add(-7*time.Minute).Unix() {
		t.Errorf("crash-looping pod: %+v", crash)
	}
	if p := by["status pods/db-0"]; p.Severity != model.StatusWarning || p.Message != "Unschedulable" || p.Since.Unix() != now.Add(-3*time.Minute).Unix() {
		t.Errorf("unscheduled pod: %+v", p)
	}
	if n := by["status nodes/worker-2"]; n.Severity != model.StatusError || n.Message != "NotReady" || n.Ref.Namespace != "" || n.Since.Unix() != now.Add(-20*time.Minute).Unix() {
		t.Errorf("node: %+v", n)
	}
	if d := by["status deployments/web"]; d.Severity != model.StatusWarning || d.Message != "1 of 2 pods ready" || d.Kind != "Deployment" {
		t.Errorf("deployment: %+v", d)
	}
	if e := by["event pods/web-1"]; e.Message != "BackOff: BackOff happened" || e.Since.Unix() != now.Add(-5*time.Minute).Unix() || e.Severity != model.StatusWarning {
		t.Errorf("event: %+v", e)
	}
	if e := by["event events/gizmo"]; e.Ref.Name != "e4" || e.Kind != "Widget" {
		t.Errorf("an event about a kind without a view names the event: %+v", e)
	}

	// The node groups: a control-plane master, two workers; the charts show the workers.
	if m.Nodes != NodesWorker || len(m.Groups) != 2 || m.Groups[0].Nodes != 1 || m.Groups[1].Nodes != 2 || m.Range != "1h" {
		t.Fatalf("groups %+v, nodes %s, range %s", m.Groups, m.Nodes, m.Range)
	}
	// The workers' lines: two nodes' allocatable and capacity; the two unfinished pods' requests
	// and limits (the finished one and the master's are not the workers').
	cpu, mem, pods := m.Charts[0], m.Charts[1], m.Charts[2]
	if cpu.Allocatable != 7.6 || cpu.Capacity != 8 || *cpu.Requests != 0.35 || *cpu.Limits != 0.35 {
		t.Errorf("cpu chart %+v", cpu)
	}
	if mem.Allocatable != 14<<30 || mem.Capacity != 16<<30 || *mem.Requests != 320<<20 {
		t.Errorf("memory chart %+v", mem)
	}
	if pods.Allocatable != 220 || pods.Capacity != 220 || pods.Current == nil || *pods.Current != 2 || pods.Requests != nil {
		t.Errorf("pods chart %+v", pods)
	}
	// The health header's counts: every node and pod of the cluster by the state its view shows, in
	// StatusOrder, zero counts left out.
	if len(m.NodeStates) != 2 || m.NodeStates[0] != (StatusCount{Status: model.StatusOK, Count: 2}) ||
		m.NodeStates[1] != (StatusCount{Status: model.StatusError, Count: 1}) {
		t.Errorf("node states %+v, want 2 ok then 1 error", m.NodeStates)
	}
	podTotal := 0
	for _, c := range m.PodStates {
		podTotal += c.Count
	}
	if podTotal == 0 {
		t.Errorf("pod states %+v: want the fixture's pods counted", m.PodStates)
	}
}

// TestClusterOverviewPage: the page's table lists the issues as rows of their own kinds (Name, Kind,
// Namespace, Message, Age), newest first; the namespace filter keeps a node's issue; the search,
// the sort and the cache's changes follow; ui.select takes a row of any kind there; the handles
// are held only while the page is open.
func TestClusterOverviewPage(t *testing.T) {
	now := time.Now()
	src := overviewFixture(now)
	s := clusterOverviewSession(t, src, nil)

	if title, ok := PageTitle(ClusterOverview); !ok || title != "Overview" || !IsPage(ClusterOverview) || !MixedKinds(ClusterOverview) || MixedKinds(WorkloadsOverview) {
		t.Fatal("the cluster overview is not a page of mixed kinds")
	}
	s.UpdateUI(func(u *UIState) { u.View, u.Sort = ClusterOverview, s.ViewSort(ClusterOverview) })
	if sort := s.UIState().Sort; sort != issuesSort {
		t.Fatalf("the Issues sort by %+v, want the newest first", sort)
	}
	snap := waitTable(t, s, ClusterOverview, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 6 })
	var ids []string
	for _, c := range snap.Columns {
		ids = append(ids, c.ID)
	}
	if !slices.Equal(ids, []string{"name", "kind", "namespace", "message", "age"}) || snap.Title != "Overview" || snap.View != ClusterOverview {
		t.Fatalf("columns %v, title %q, view %q", ids, snap.Title, snap.View)
	}
	first := snap.Rows[0]
	if first.Ref.Kind != "events" || first.Ref.Name != "e4" || first.Cells[0].Text != "gizmo" || first.Cells[0].Link == nil || !first.Cells[0].Link.Same(first.Ref) {
		t.Fatalf("newest row %+v", first)
	}
	if last := snap.Rows[len(snap.Rows)-1]; last.Ref.Kind != "nodes" || last.Cells[1].Text != "Node" || last.Cells[3].Status != model.StatusError {
		t.Fatalf("oldest row %+v", last)
	}

	// The namespace filter: shop's issues and the node's.
	s.UpdateUI(func(u *UIState) { u.Namespaces = []string{"shop"} })
	snap = waitTable(t, s, ClusterOverview, func(t *TableSnapshot) bool { return len(t.Rows) == 4 })
	if snap.Total != 6 || snap.IndexOf(model.ObjectRef{Context: "test", Kind: "nodes", Name: "worker-2"}) < 0 {
		t.Fatalf("filtered: %s", summary(snap))
	}
	// The search, and a sort by kind.
	s.UpdateUI(func(u *UIState) { u.Filter = "backoff" })
	waitTable(t, s, ClusterOverview, func(t *TableSnapshot) bool { return len(t.Rows) == 2 })
	s.UpdateUI(func(u *UIState) { u.Filter, u.Namespaces, u.Sort = "", nil, SortSpec{Column: "kind"} })
	snap = waitTable(t, s, ClusterOverview, func(t *TableSnapshot) bool { return len(t.Rows) == 6 && t.Sort.Column == "kind" })
	if snap.Rows[0].Cells[1].Text != "Deployment" {
		t.Fatalf("sorted by kind: first %+v", snap.Rows[0])
	}

	// The cache's changes: the node comes back, the crash-looping pod is deleted.
	src.Set(testkit.NodesGVR, ovNode("worker-2", false, true, now))
	src.Delete(testkit.PodsGVR, "shop", "web-1")
	waitTable(t, s, ClusterOverview, func(t *TableSnapshot) bool { return len(t.Rows) == 4 })

	// The handles go with the page, and come back without a list within the warm period.
	if handlesOn(s, "daemonsets") != 1 || handlesOn(s, "nodes") != 1 {
		t.Fatalf("handles while open: daemonsets %d, nodes %d", handlesOn(s, "daemonsets"), handlesOn(s, "nodes"))
	}
	s.UpdateUI(func(u *UIState) { u.View, u.Sort = "pods", s.ViewSort("pods") })
	waitFor(t, "the page's handles to go", func() bool { return handlesOn(s, "daemonsets") == 0 && handlesOn(s, "nodes") == 0 })
	if s.Table(ClusterOverview) != nil {
		t.Fatal("a closed page still has a table")
	}
	lists := src.Lists()
	s.UpdateUI(func(u *UIState) { u.View, u.Sort = ClusterOverview, s.ViewSort(ClusterOverview) })
	waitTable(t, s, ClusterOverview, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) == 4 })
	if src.Lists() != lists {
		t.Fatalf("reopening the page cost %d lists", src.Lists()-lists)
	}
}

// TestClusterOverviewSelectsAnyKind: on the Overview, ui.select's rule takes an issue's object of
// any kind (MixedKinds); the drawer opens over the page.
func TestClusterOverviewSelectsAnyKind(t *testing.T) {
	src := overviewFixture(time.Now())
	s := clusterOverviewSession(t, src, nil)
	s.UpdateUI(func(u *UIState) { u.View = ClusterOverview })
	waitTable(t, s, ClusterOverview, func(t *TableSnapshot) bool { return t.Synced && len(t.Rows) > 0 })
	node := model.ObjectRef{Context: "test", Kind: "nodes", Name: "worker-2"}
	s.UpdateUI(func(u *UIState) { u.Selected, u.Drawer = []model.ObjectRef{node}, &node })
	waitFor(t, "the node's drawer", func() bool {
		d, err := s.Detail(node)
		return err == nil && d != nil
	})
	if u := s.UIState(); u.View != ClusterOverview {
		t.Fatalf("the drawer moved the view to %s", u.View)
	}
}

// nodesProvider is a Metrics Server that measures the fixture's nodes now.
type nodesProvider struct{}

func (nodesProvider) ID() string                         { return metrics.ServerID }
func (nodesProvider) Label() string                      { return metrics.ServerLabel }
func (nodesProvider) Capabilities() metrics.Capabilities { return metrics.Capabilities{} }
func (nodesProvider) ListPods(context.Context, string) ([]model.PodUsage, error) {
	return nil, nil
}
func (nodesProvider) GetPod(context.Context, string, string) (model.PodUsage, error) {
	return model.PodUsage{}, metrics.ErrNoData
}
func (nodesProvider) ListNodes(context.Context) ([]model.NodeUsage, error) {
	now := time.Now()
	return []model.NodeUsage{
		{Name: "master-1", Timestamp: now, Usage: model.Usage{CPUMilli: 400, MemoryBytes: 1 << 30}},
		{Name: "worker-1", Timestamp: now, Usage: model.Usage{CPUMilli: 1200, MemoryBytes: 3 << 30}},
		{Name: "worker-2", Timestamp: now, Usage: model.Usage{CPUMilli: 300, MemoryBytes: 1 << 30}},
	}, nil
}
func (nodesProvider) GetNode(context.Context, string) (model.NodeUsage, error) {
	return model.NodeUsage{}, metrics.ErrNoData
}

// TestClusterOverviewCharts: while the page is open the charts' CPU and Memory come from the hub's
// cluster scope (the nodes' measurements summed per group), the Pods from the cache; the group and
// range are the UI state's; closed, the hub polls nothing for the page and a read takes the current
// values from one read of the nodes' metrics.
func TestClusterOverviewCharts(t *testing.T) {
	src := overviewFixture(time.Now())
	var hub *metrics.Hub
	s := clusterOverviewSession(t, src, func() *metrics.Hub {
		hub = metrics.NewHub(metrics.Fixed(nodesProvider{}), metrics.Options{ObjectInterval: 20 * time.Millisecond, ListInterval: time.Hour, TTL: 10 * time.Millisecond})
		return hub
	})
	s.UpdateUI(func(u *UIState) { u.View = "nodes" }) // no Pods view polling the pods' metrics
	s.UpdateUI(func(u *UIState) { u.View = ClusterOverview })
	var m *ClusterOverviewModel
	waitFor(t, "the workers' CPU from the hub", func() bool {
		var err error
		if m, err = s.ClusterOverview(context.Background(), "", nil, true); err != nil {
			t.Fatal(err)
		}
		return m.Status == metrics.StatusAvailable && m.Charts[0].Current != nil && len(m.Charts[0].Samples) >= 2
	})
	if m.Nodes != NodesWorker || *m.Charts[0].Current != 1.5 || *m.Charts[1].Current != 4<<30 || m.Source != metrics.ServerLabel || m.History {
		t.Fatalf("workers: nodes %s, cpu %v, memory %v, source %q", m.Nodes, *m.Charts[0].Current, *m.Charts[1].Current, m.Source)
	}
	if pods := m.Charts[2]; pods.Current == nil || *pods.Current != 2 || len(pods.Samples) == 0 || pods.Allocatable != 220 {
		t.Fatalf("workers' pods chart %+v", pods)
	}
	if len(m.Providers) != 1 || m.Providers[0].Status != metrics.StatusAvailable {
		t.Fatalf("providers %+v", m.Providers)
	}
	cluster := metrics.ClusterScope([]string{"master-1"})
	if n := hub.Subscribed(); n != 1 || hub.History(cluster) == nil {
		t.Fatalf("%d scopes polled with the page open, want the cluster's", n)
	}

	// The masters, over six hours: the UI state's choice.
	s.UpdateUI(func(u *UIState) { u.Overview = OverviewChoice{Nodes: NodesMaster, Range: "6h"} })
	m, _ = s.ClusterOverview(context.Background(), "", nil, false)
	if m.Nodes != NodesMaster || m.Range != "6h" || m.Window != 6*time.Hour || *m.Charts[0].Current != 0.4 || m.Charts[0].Allocatable != 3.8 ||
		*m.Charts[0].Requests != 0.1 || len(m.Charts[0].Samples) != 0 {
		t.Fatalf("masters: %+v", m.Charts[0])
	}

	// Closed: nothing polled for it; a read asks the nodes' metrics once.
	s.UpdateUI(func(u *UIState) { u.View, u.Overview = "nodes", OverviewChoice{} })
	waitFor(t, "the cluster scope to be released", func() bool { return hub.History(cluster) == nil })
	m, err := s.ClusterOverview(context.Background(), "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != metrics.StatusAvailable || m.Charts[0].Current == nil || *m.Charts[0].Current != 1.5 || len(m.Charts[0].Samples) != 0 || hub.History(cluster) != nil {
		t.Fatalf("closed page: status %s, cpu %+v", m.Status, m.Charts[0])
	}
}
