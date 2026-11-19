package engine

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// The CPU and Memory of the pods a node's drawer lists (owner), from a fake metrics provider
// through the hub, as the Pods list gets them; polled while the drawer shows the table.

// podTableProvider measures the pods in usage ("namespace/name") and counts its lists by
// namespace; it measures no single pod and no node.
type podTableProvider struct {
	mu    sync.Mutex
	usage map[string]model.Usage
	lists []string
}

func (p *podTableProvider) ID() string                         { return "metrics-server" }
func (p *podTableProvider) Label() string                      { return "Fake Metrics Server" }
func (p *podTableProvider) Capabilities() metrics.Capabilities { return metrics.Capabilities{} }

func (p *podTableProvider) ListPods(_ context.Context, ns string) ([]model.PodUsage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lists = append(p.lists, ns)
	var out []model.PodUsage
	for key, u := range p.usage {
		pns, name, _ := strings.Cut(key, "/")
		if ns == "" || ns == pns {
			out = append(out, model.PodUsage{Namespace: pns, Name: name, Timestamp: time.Now(),
				Containers: []model.ContainerUsage{{Name: "app", Usage: u}}})
		}
	}
	return out, nil
}

func (p *podTableProvider) GetPod(context.Context, string, string) (model.PodUsage, error) {
	return model.PodUsage{}, metrics.ErrNoData
}
func (p *podTableProvider) ListNodes(context.Context) ([]model.NodeUsage, error) { return nil, nil }
func (p *podTableProvider) GetNode(context.Context, string) (model.NodeUsage, error) {
	return model.NodeUsage{}, metrics.ErrNoData
}

// listed returns the namespaces the provider's pod lists asked for, in order.
func (p *podTableProvider) listed() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.lists)
}

// metricsConnector is fakeConnector with a metrics hub over a fixed provider on every backend.
type metricsConnector struct {
	*fakeConnector
	prov metrics.Provider
}

func (c *metricsConnector) Connect(ctx context.Context, name string) (*Backend, error) {
	b, err := c.fakeConnector.Connect(ctx, name)
	if err == nil {
		b.Metrics = metrics.NewHub(metrics.Fixed(c.prov), metrics.Options{
			ListInterval: 40 * time.Millisecond, ObjectInterval: 40 * time.Millisecond, TTL: 20 * time.Millisecond,
		})
	}
	return b, err
}

// The pods of the fixture: three on node-1 (web-0 and api-0 in apps, batch-0 in jobs), one on
// node-2. The provider measures web-0 and batch-0 and the pod on node-2; api-0 has no measurement.
var drawerPodUsage = map[string]model.Usage{
	"apps/web-0":   {CPUMilli: 250, MemoryBytes: 64 << 20},
	"jobs/batch-0": {CPUMilli: 5, MemoryBytes: 1536 << 20},
	"apps/far-0":   {CPUMilli: 900, MemoryBytes: 2 << 30},
}

// drawerPodsSession is a session over a node with pods, its drawer open on node-1, with the
// built-in Pods and Nodes profiles and a metrics hub over prov.
func drawerPodsSession(t *testing.T) (*Session, *podTableProvider, model.ObjectRef) {
	t.Helper()
	src := testkit.NewMemorySource()
	src.Set(testkit.NodesGVR, testkit.Object("v1", "Node", "", "node-1", nil))
	src.Set(testkit.NodesGVR, testkit.Object("v1", "Node", "", "node-2", nil))
	src.Set(testkit.PodsGVR, podOnNode("apps", "web-0", "node-1"))
	src.Set(testkit.PodsGVR, podOnNode("apps", "api-0", "node-1"))
	src.Set(testkit.PodsGVR, podOnNode("jobs", "batch-0", "node-1"))
	src.Set(testkit.PodsGVR, podOnNode("apps", "far-0", "node-2"))
	prov := &podTableProvider{usage: drawerPodUsage}
	fc := &fakeConnector{src: src, profiles: []*model.KindProfile{builtinProfile(t, "pods"), builtinProfile(t, "nodes"), testEvents}}
	s := New(Options{
		Connector:        &metricsConnector{fakeConnector: fc, prov: prov},
		ClusterSettings:  NewClusterSettingsStore(filepath.Join(t.TempDir(), "Cogpit")),
		SnapshotInterval: 5 * time.Millisecond,
	})
	t.Cleanup(s.Close)
	decideIntegrations(t, s, "test") // its metrics read (D-82)
	connectTest(t, s)
	ref := model.ObjectRef{Context: "test", Kind: "nodes", Name: "node-1"}
	// The Events view: a view that polls no metrics of its own (the Pods list would poll the
	// pods' scope the drawer shares).
	s.UpdateUI(func(u *UIState) { u.View, u.Drawer = testEvents.Key, &ref })
	return s, prov, ref
}

// podsTable returns the drawer model's Pods table, nil without one.
func podsTable(t *testing.T, s *Session, ref model.ObjectRef) *model.DetailTable {
	t.Helper()
	m, err := s.Detail(ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, sec := range m.Sections {
		if sec.Title == "Pods" && sec.Table != nil {
			return sec.Table
		}
	}
	return nil
}

// rowOfPod returns the cells and texts of the pod's row, by its name.
func rowOfPod(t *testing.T, tbl *model.DetailTable, name string) ([]model.Cell, []string) {
	t.Helper()
	for r, cells := range tbl.Cells {
		if cells[0].Text == name {
			return cells, tbl.Rows[r]
		}
	}
	t.Fatalf("no row %s in %q", name, tbl.Rows)
	return nil, nil
}

// waitPodsUsage waits for the drawer's Pods table to show web-0's CPU.
func waitPodsUsage(t *testing.T, s *Session, ref model.ObjectRef) *model.DetailTable {
	t.Helper()
	var tbl *model.DetailTable
	waitFor(t, "the Pods table's CPU and Memory", func() bool {
		tbl = podsTable(t, s, ref)
		if tbl == nil || len(tbl.Rows) != 3 {
			return false
		}
		cells, _ := rowOfPod(t, tbl, "web-0")
		ci := tbl.ColumnIndex(model.ColumnCPU)
		return ci >= 0 && cells[ci].Text != ""
	})
	return tbl
}

// A node's drawer shows its pods' CPU and Memory as the Pods list shows them: the Pods profile's
// cells (texts and sort keys) from the hub's latest measurement; a pod without one keeps empty
// cells. The poll is the pods' list in every namespace, one subscription that starts with the
// drawer's table and ends when the drawer closes, after which nothing is asked.
func TestNodeDrawerPodsShowMetrics(t *testing.T) {
	s, prov, ref := drawerPodsSession(t)
	hub := s.Backend().Metrics
	tbl := waitPodsUsage(t, s, ref)

	pods := builtinProfile(t, "pods")
	cpuCol, memCol := tbl.ColumnIndex(model.ColumnCPU), tbl.ColumnIndex(model.ColumnMemory)
	if cpuCol < 0 || memCol < 0 || tbl.Types[cpuCol] != model.ColumnMetric || tbl.Types[memCol] != model.ColumnMetric {
		t.Fatalf("columns %q, ids %q, types %q", tbl.Columns, tbl.IDs, tbl.Types)
	}
	for _, name := range []string{"web-0", "batch-0"} {
		ns := "apps"
		if name == "batch-0" {
			ns = "jobs"
		}
		wantCPU, wantMem := pods.UsageCells(nil, drawerPodUsage[ns+"/"+name])
		cells, texts := rowOfPod(t, tbl, name)
		if cells[cpuCol] != wantCPU || cells[memCol] != wantMem {
			t.Errorf("%s: cpu %+v memory %+v, want %+v %+v (the Pods list's cells)", name, cells[cpuCol], cells[memCol], wantCPU, wantMem)
		}
		if texts[cpuCol] != wantCPU.Text || texts[memCol] != wantMem.Text {
			t.Errorf("%s: texts %q, want %q and %q", name, texts, wantCPU.Text, wantMem.Text)
		}
	}
	if cells, texts := rowOfPod(t, tbl, "api-0"); cells[cpuCol] != (model.Cell{}) || cells[memCol] != (model.Cell{}) || texts[cpuCol] != "" {
		t.Errorf("a pod without a measurement: %+v, %q", cells, texts)
	}

	// The drawer polls the node (its chart) and the pods' list in every namespace (its table).
	if n := hub.Subscribed(); n != 2 {
		t.Errorf("%d scopes polled with a node's drawer open, want 2 (the node, the pods)", n)
	}
	if got := prov.listed(); len(got) == 0 || slices.ContainsFunc(got, func(ns string) bool { return ns != "" }) {
		t.Errorf("the pod lists asked for %q, want every namespace", got)
	}

	// Closed: no poll, and the provider is asked nothing more.
	s.UpdateUI(func(u *UIState) { u.Drawer = nil })
	waitFor(t, "the drawer's polls released", func() bool { return hub.Subscribed() == 0 })
	time.Sleep(60 * time.Millisecond) // a poll that started before the release may still answer
	before := len(prov.listed())
	time.Sleep(200 * time.Millisecond) // five list intervals
	if after := len(prov.listed()); after != before {
		t.Errorf("%d pod lists after the drawer closed", after-before)
	}
}

// Every poll of the pods' metrics announces the drawer (EventDetail), so the window reads the new
// numbers; another node's drawer drops the first one's poll and makes its own.
func TestNodeDrawerPodsPollWakesTheDrawer(t *testing.T) {
	s, prov, ref := drawerPodsSession(t)
	rec := record(s)
	waitPodsUsage(t, s, ref)
	before := rec.count(EventDetail, "nodes")
	waitFor(t, "a detail event after a poll", func() bool { return rec.count(EventDetail, "nodes") > before })

	prov.mu.Lock()
	prov.usage = map[string]model.Usage{"apps/web-0": {CPUMilli: 1234, MemoryBytes: 1 << 20}}
	prov.mu.Unlock()
	pods := builtinProfile(t, "pods")
	want, _ := pods.UsageCells(nil, model.Usage{CPUMilli: 1234, MemoryBytes: 1 << 20})
	waitFor(t, "the new measurement in the table", func() bool {
		tbl := podsTable(t, s, ref)
		cells, _ := rowOfPod(t, tbl, "web-0")
		return cells[tbl.ColumnIndex(model.ColumnCPU)] == want
	})

	// The drawer on another object: the first drawer's subscription goes with it.
	other := model.ObjectRef{Context: "test", Kind: "nodes", Name: "node-2"}
	s.UpdateUI(func(u *UIState) { u.Drawer = &other })
	waitFor(t, "node-2's Pods", func() bool { tbl := podsTable(t, s, other); return tbl != nil && len(tbl.Rows) == 1 })
	if n := s.Backend().Metrics.Subscribed(); n != 2 {
		t.Errorf("%d scopes polled after the drawer moved, want 2 (node-2, the pods)", n)
	}
}

// The cluster's settings hide the table's metric columns as they hide the Pods list's: Pods hides
// both, CPU or Memory one; with both hidden nothing is polled for the table. Shown again, the next
// model polls again.
func TestNodeDrawerPodsHiddenMetrics(t *testing.T) {
	s, prov, ref := drawerPodsSession(t)
	hub := s.Backend().Metrics
	waitPodsUsage(t, s, ref)
	hide := func(m MetricSet) {
		t.Helper()
		if _, err := s.ChangeClusterSettings("test", func(cs *ClusterSettings) error {
			cs.HiddenMetrics = m
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	hide(HideCPU)
	tbl := podsTable(t, s, ref)
	if tbl.ColumnIndex(model.ColumnCPU) >= 0 || slices.Contains(tbl.Columns, "CPU") || tbl.ColumnIndex(model.ColumnMemory) < 0 {
		t.Fatalf("CPU hidden: columns %q, ids %q", tbl.Columns, tbl.IDs)
	}
	for r := range tbl.Rows {
		if len(tbl.Rows[r]) != len(tbl.Columns) || len(tbl.Cells[r]) != len(tbl.Columns) || len(tbl.Types) != len(tbl.Columns) {
			t.Fatalf("row %d does not fit the columns: %q", r, tbl.Rows[r])
		}
	}
	cells, _ := rowOfPod(t, tbl, "web-0")
	if mem := cells[tbl.ColumnIndex(model.ColumnMemory)]; mem.Text == "" {
		t.Errorf("Memory empty with only CPU hidden: %+v", cells)
	}

	hide(HidePods) // the Pods place: both columns, and the node's chart stays
	waitFor(t, "the table's poll released", func() bool { return hub.Subscribed() == 1 })
	tbl = podsTable(t, s, ref)
	if tbl.ColumnIndex(model.ColumnCPU) >= 0 || tbl.ColumnIndex(model.ColumnMemory) >= 0 || len(tbl.Columns) != 6 {
		t.Fatalf("Pods hidden: columns %q", tbl.Columns)
	}
	if n := hub.Subscribed(); n != 1 {
		t.Errorf("%d scopes polled with the pods' metrics hidden, want the node's alone", n)
	}
	time.Sleep(60 * time.Millisecond)
	before := len(prov.listed())
	time.Sleep(200 * time.Millisecond)
	if after := len(prov.listed()); after != before {
		t.Errorf("%d pod lists with the table's metrics hidden", after-before)
	}

	hide(0)
	waitPodsUsage(t, s, ref)
	if n := hub.Subscribed(); n != 2 {
		t.Errorf("%d scopes polled with everything shown again, want 2", n)
	}
}

// Reads of the drawer's model racing with the drawer moving between nodes and closing leave no
// subscription behind: a model built for a drawer that stops meanwhile subscribes nothing, and
// stop releases what a build subscribed before it.
func TestNodeDrawerPodsNoLeakUnderConcurrentReads(t *testing.T) {
	s, _, ref := drawerPodsSession(t)
	hub := s.Backend().Metrics
	other := model.ObjectRef{Context: "test", Kind: "nodes", Name: "node-2"}
	waitPodsUsage(t, s, ref)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				for _, r := range []model.ObjectRef{ref, other} {
					if _, err := s.Detail(r); err != nil {
						t.Error(err)
						return
					}
				}
			}
		})
	}
	for i := range 40 {
		r := ref
		if i%2 == 1 {
			r = other
		}
		s.UpdateUI(func(u *UIState) { u.Drawer = &r })
		time.Sleep(2 * time.Millisecond)
		if i%10 == 9 {
			s.UpdateUI(func(u *UIState) { u.Drawer = nil })
		}
	}
	s.UpdateUI(func(u *UIState) { u.Drawer = nil })
	close(stop)
	wg.Wait()
	waitFor(t, "every poll released", func() bool { return hub.Subscribed() == 0 })
}

// A namespaced object's pod table polls its namespace's pods only; a node's polls the pods as the
// Pods view reads them: every namespace at once, or each namespace of the plan (D-39).
func TestPodTableScope(t *testing.T) {
	b := &Backend{Namespace: "team-a", Profiles: []*model.KindProfile{testPods}}
	ns := &drawerWatch{backend: b, profile: &model.KindProfile{Namespaced: true}, ref: model.ObjectRef{Kind: "deployments", Namespace: "shop", Name: "web"}}
	if got := ns.podTableScopes(); !slices.Equal(got, []metrics.Scope{{Kind: metrics.KindPods, Namespace: "shop"}}) {
		t.Errorf("a namespaced object's scopes %+v", got)
	}
	node := &drawerWatch{backend: b, profile: &model.KindProfile{}, ref: model.ObjectRef{Kind: "nodes", Name: "n1"}}
	if got := node.podTableScopes(); !slices.Equal(got, []metrics.Scope{{Kind: metrics.KindPods}}) {
		t.Errorf("a node's scopes %+v", got)
	}
	b.Access = denyAll{except: map[string]bool{"team-a": true, "team-b": true}}
	b.ns.setInputs([]string{"team-a", "team-b", "team-c"}, nil)
	want := []metrics.Scope{{Kind: metrics.KindPods, Namespace: "team-a"}, {Kind: metrics.KindPods, Namespace: "team-b"}}
	if got := node.podTableScopes(); !slices.Equal(got, want) {
		t.Errorf("a node's scopes for a user limited to team-a and team-b: %+v", got)
	}
}

// A table is a pod table only with a metric column and rows that link to pods: other tables of
// cells are left alone.
func TestIsPodTable(t *testing.T) {
	pod := &model.ObjectRef{Kind: "pods", Namespace: "a", Name: "p"}
	cols := []model.Column{{ID: "name", Title: "Name"}, {ID: model.ColumnCPU, Title: "CPU", Type: model.ColumnMetric}}
	if !isPodTable(model.SortableCellTable(cols, [][]model.Cell{{{Text: "p", Link: pod}, {}}})) {
		t.Error("a sortable table of pods with CPU is not a pod table")
	}
	if isPodTable(model.CellTable(cols, [][]model.Cell{{{Text: "p", Link: pod}, {}}})) {
		t.Error("a table without column ids is a pod table")
	}
	if isPodTable(model.SortableCellTable(cols, [][]model.Cell{{{Text: "s", Link: &model.ObjectRef{Kind: "secrets", Name: "s"}}, {}}})) {
		t.Error("a table of secrets is a pod table")
	}
	text := []model.Column{{ID: "name", Title: "Name"}, {ID: model.ColumnCPU, Title: "CPU", Type: model.ColumnText}}
	if isPodTable(model.SortableCellTable(text, [][]model.Cell{{{Text: "p", Link: pod}, {}}})) {
		t.Error("a CPU column of text is a metric column")
	}
	if isPodTable(nil) || isPodTable(model.SortableCellTable(cols, nil)) {
		t.Error("an empty table is a pod table")
	}
}
