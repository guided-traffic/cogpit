package fyneui

import (
	"image/color"
	"math"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// The Nodes list's usage bars (G12; resource-parity › Nodes, captured: "CPU, Memory, Disk (usage
// bars)"): the share as text, a bar filled to it, coloured at the thresholds; a node without a
// measurement has an empty cell, never 0%.

// barNodeUsage are the loads the bar tests give the fixture's nodes (allocatable: 7.8 cores and
// 30.2 GiB): a quiet node, one at the warning threshold, one past the error threshold, one without
// a measurement; the others in between.
var barNodeUsage = map[string]model.Usage{
	"control-plane-1": {CPUMilli: 936, MemoryBytes: 11 << 30},           // 12%, 36%
	"worker-a-01":     {CPUMilli: 6474, MemoryBytes: 25 << 30},          // 83%, 83%
	"worker-a-02":     {CPUMilli: 7410, MemoryBytes: 29 << 30},          // 95%, 96%
	"worker-a-04":     {CPUMilli: 2340, MemoryBytes: 8 << 30},           // 30%, 26%
	"worker-b-01":     {CPUMilli: 39, MemoryBytes: 3 << 30},             // 0.5%, 9.9%
	"worker-b-03":     {CPUMilli: 4290, MemoryBytes: 18 << 30},          // 55%, 60%
	"worker-c-01":     {CPUMilli: 5850, MemoryBytes: 2 << 30},           // 75%, 6.6%
	"worker-b-02":     {MemoryBytes: 30 << 30, Missing: model.UsageCPU}, // CPU not measured, 99%
	// worker-a-03: no measurement at all
}

// barNodeDisk are the fixture nodes' fullest filesystems (node-exporter through Prometheus, D-90):
// a few nodes have none (no exporter on them).
var barNodeDisk = map[string]model.NodeDisk{
	"control-plane-1": {Mountpoint: "/", UsedBytes: 21 << 30, SizeBytes: 100 << 30},      // 21%
	"worker-a-01":     {Mountpoint: "/data", UsedBytes: 172 << 30, SizeBytes: 200 << 30}, // 86%
	"worker-a-02":     {Mountpoint: "/", UsedBytes: 47 << 30, SizeBytes: 50 << 30},       // 94%
	"worker-a-03":     {Mountpoint: "/", UsedBytes: 60 << 30, SizeBytes: 200 << 30},      // 30%
	"worker-a-04":     {Mountpoint: "/", UsedBytes: 88 << 30, SizeBytes: 200 << 30},      // 44%
	"worker-b-03":     {Mountpoint: "/", UsedBytes: 131 << 30, SizeBytes: 200 << 30},     // 66%
}

// workerA01Filesystems are worker-a-01's filesystems, shaped like a real node's (D-90): the root,
// a data disk at the warning share (the fullest, the Nodes list's Disk) and a backup disk at the
// error share's edge below it.
var workerA01Filesystems = []model.NodeFilesystem{
	{Mountpoint: "/", Device: "/dev/sda1", FSType: "ext4", UsedBytes: 51 << 30, SizeBytes: 123 << 30},     // 41%
	{Mountpoint: "/data", Device: "/dev/sdb1", FSType: "xfs", UsedBytes: 172 << 30, SizeBytes: 200 << 30}, // 86%
	{Mountpoint: "/srv/backup", Device: "/dev/sdc1", FSType: "ext4", UsedBytes: 92 << 30, SizeBytes: 500 << 30},
}

// diskTipFixture is the Disk header's tooltip as the engine writes it with Prometheus found.
const diskTipFixture = "Disk: the fullest of the node's filesystems as node-exporter reports them, read through Prometheus (monitoring/prometheus-server)."

// withNodeBars fills the fixture Nodes view's CPU, Memory and Disk cells as the engine's row cache
// does, from barNodeUsage and barNodeDisk, and the Disk header's tooltip.
func withNodeBars(t testing.TB, f *fakeEngine) {
	t.Helper()
	p, _ := kinds.ByKey("nodes")
	objs := map[string]*unstructured.Unstructured{}
	for _, o := range fixtureNodeObjects() {
		objs[o.GetName()] = o
	}
	snap := *f.tables["nodes"]
	snap.Rows = slices.Clone(snap.Rows)
	snap.ColumnTips = map[string]string{model.ColumnDisk: diskTipFixture}
	cpu, mem, disk := p.ColumnIndex(model.ColumnCPU), p.ColumnIndex(model.ColumnMemory), p.ColumnIndex(model.ColumnDisk)
	for i, r := range snap.Rows {
		cells := slices.Clone(r.Cells)
		if u, ok := barNodeUsage[r.Ref.Name]; ok {
			cells[cpu], cells[mem] = p.UsageCells(objs[r.Ref.Name], u)
		}
		if d, ok := barNodeDisk[r.Ref.Name]; ok {
			cells[disk] = p.DiskCell(objs[r.Ref.Name], d)
		}
		snap.Rows[i].Cells = cells
	}
	f.tables["nodes"] = &snap
}

func TestNodesListDrawsUsageBars(t *testing.T) {
	f := kindsFake(t, "nodes")
	withNodeBars(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the nodes view", func() bool { return sh.view.snap != nil && sh.view.snap.View == "nodes" && sh.diag.total > 0 })
	settle(w)
	p, _ := kinds.ByKey("nodes")
	cpuCol := firstDataColumn + p.ColumnIndex(model.ColumnCPU)
	memCol := firstDataColumn + p.ColumnIndex(model.ColumnMemory)
	row := func(name string) int { return rowOf(t, f, "nodes", name) }
	fillOf := func(c *tableCell) color.Color { return c.bar.fill.FillColor }
	check := func(name string, col int, share float64, want fyne.ThemeColorName) {
		t.Helper()
		c := sh.view.cellAt(t, row(name), col)
		if c.bar == nil || !c.bar.shown || !c.bar.track.Visible() {
			t.Fatalf("%s: no bar shown in %q", name, c.full)
		}
		tw, fw := c.bar.track.Size().Width, c.bar.fill.Size().Width
		if math.Abs(float64(fw)-float64(tw)*math.Min(share, 1)) > 1.5 {
			t.Errorf("%s: fill %v of a track of %v, want the share %.3f", name, fw, tw, share)
		}
		if got := fillOf(c); got != themeColor(c.Theme(), want) {
			t.Errorf("%s: bar colour %v, want %s", name, got, want)
		}
		// The share's text sits right-aligned before the bar.
		if tx := c.text.Position().X + c.text.Size().Width; tx > c.bar.track.Position().X {
			t.Errorf("%s: the text ends at %v, after the bar's start %v", name, tx, c.bar.track.Position().X)
		}
	}
	check("control-plane-1", cpuCol, 936.0/7800, colorAccent)
	check("worker-a-01", cpuCol, 6474.0/7800, statusColorName(model.StatusWarning))
	check("worker-a-02", cpuCol, 7410.0/7800, statusColorName(model.StatusError))
	check("worker-b-02", memCol, float64(30<<30)/float64(31715736<<10), statusColorName(model.StatusError))

	// The bars of a column line up: every share's slot is as wide.
	a, b := sh.view.cellAt(t, row("control-plane-1"), cpuCol), sh.view.cellAt(t, row("worker-a-02"), cpuCol)
	if a.bar.track.Position().X != b.bar.track.Position().X {
		t.Errorf("bars start at %v and %v, want one x", a.bar.track.Position().X, b.bar.track.Position().X)
	}
	// No measurement, or a part not measured: an empty cell and no bar, never 0%.
	for _, c := range []*tableCell{sh.view.cellAt(t, row("worker-a-03"), cpuCol), sh.view.cellAt(t, row("worker-b-02"), cpuCol)} {
		if c.full != "" || (c.bar != nil && (c.bar.track.Visible() || c.bar.fill.Visible())) {
			t.Errorf("a cell without a measurement shows %q and a bar", c.full)
		}
	}
	// The tooltip names the amounts behind the bar.
	if got := sh.view.cellAt(t, row("worker-a-02"), cpuCol).tipText(); got != "7.410 of 7.800 cores (95%)" {
		t.Errorf("tooltip = %q", got)
	}

	// Disk: a bar of the fullest filesystem from node-exporter's numbers, its mountpoint in the
	// tooltip (D-90); a node without them is empty.
	diskCol := firstDataColumn + p.ColumnIndex(model.ColumnDisk)
	check("worker-a-01", diskCol, 172.0/200, statusColorName(model.StatusWarning))
	if got := sh.view.cellAt(t, row("worker-a-01"), diskCol).tipText(); got != "/data · 172.0 GiB of 200.0 GiB (86%)" {
		t.Errorf("disk tooltip = %q", got)
	}
	if c := sh.view.cellAt(t, row("worker-b-01"), diskCol); c.full != "" {
		t.Errorf("a node without node-exporter's numbers shows %q", c.full)
	}
	// The Disk header's tooltip says where the numbers come from; CPU's has none.
	if got := sh.view.headerTip(diskCol); got != diskTipFixture {
		t.Errorf("Disk header tooltip = %q", got)
	}
	if got := sh.view.headerTip(cpuCol); got != "" {
		t.Errorf("CPU header tooltip = %q", got)
	}
	var head *headerCell
	for _, h := range findAll[*headerCell](sh.view.table) {
		if h.col == diskCol && h.Visible() {
			head = h
		}
	}
	if head == nil {
		t.Fatal("no Disk header on screen")
	}
	hoverOver(w, q, middleOf(head).AddXY(-2, 0)) // the pointer enters the table, then moves on the header
	hoverOver(w, q, middleOf(head))
	if sh.tip.owner != head || sh.tip.text != diskTipFixture {
		t.Errorf("hovering the Disk header asks for %q (owner %T)", sh.tip.text, sh.tip.owner)
	}
	var cpuHead *headerCell
	for _, h := range sh.view.headers {
		if h.col == cpuCol && h.Visible() {
			cpuHead = h
		}
	}
	hoverOver(w, q, middleOf(cpuHead))
	if sh.tip.owner == head || sh.tip.owner == cpuHead {
		t.Errorf("the CPU header asks for a tooltip: %q", sh.tip.text)
	}
	hoverOver(w, q, middleOf(head))
	hoverOver(w, q, fyne.NewPos(20, 600)) // the sidebar
	if sh.tip.owner == head {
		t.Error("the header's tooltip stays after the pointer left")
	}
}

// A node's drawer shows a usage bar per filesystem under the chart (WF26, D-90), the root first, in
// the bars' colours at the thresholds, the numbers in each row's tooltip; or quietly why there is
// none.
func TestNodeDrawerShowsTheDisk(t *testing.T) {
	sh, ref := nodeDiskDrawer(t, ThemeDark)
	p := sh.drawer.kept[metricsKey].(*metricsPanel)
	b := p.disk
	r := test.WidgetRenderer(b).(*diskBarsRenderer)
	if !b.Visible() || len(r.rows) != 3 || r.title.Text != "Disk" {
		t.Fatalf("disk shown %v with %d rows, title %q", b.Visible(), len(r.rows), r.title.Text)
	}
	th := b.Theme()
	for i, want := range []struct {
		mount, share string
		color        fyne.ThemeColorName
	}{
		{"/", "41%", colorAccent},
		{"/data", "86%", statusColorName(model.StatusWarning)},
		{"/srv/backup", "18%", colorAccent},
	} {
		row := r.rows[i]
		if row.mount.Text != want.mount || row.share.Text != want.share || row.fill.FillColor != themeColor(th, want.color) {
			t.Errorf("row %d: %q %q in %v, want %q %q in %s", i, row.mount.Text, row.share.Text, row.fill.FillColor, want.mount, want.share, want.color)
		}
		// The bars line up and fill to the share.
		if row.track.Position().X != r.rows[0].track.Position().X {
			t.Errorf("row %d: bar at %v, the first's at %v", i, row.track.Position().X, r.rows[0].track.Position().X)
		}
		share := float32(workerA01Filesystems[i].Share())
		if got := row.fill.Size().Width / row.track.Size().Width; got < share-0.01 || got > share+0.01 {
			t.Errorf("row %d: filled %.3f of the bar, want %.3f", i, got, share)
		}
	}
	// The row under the pointer names its numbers in the tooltip.
	f, ok := b.rowAt(fyne.NewPos(10, b.rowH*1.5))
	if tip := diskTip(f); !ok || tip != "/data · /dev/sdb1 · xfs\nUsed 172.0 GiB of 200.0 GiB (86%)\nFree 28.0 GiB" {
		t.Errorf("the second row's tooltip = %q", tip)
	}
	if _, ok := b.rowAt(fyne.NewPos(10, b.rowH*3.5)); ok {
		t.Error("a row below the last")
	}

	p.apply(&engine.ObjectMetrics{Ref: ref, Status: metrics.StatusAvailable, Source: "Kubernetes Metrics Server",
		DiskNote: "the disk usage comes from node-exporter through Prometheus, and no Prometheus was found"}, nil)
	if r := test.WidgetRenderer(b).(*diskBarsRenderer); !b.Visible() || len(r.rows) != 0 ||
		!strings.HasPrefix(r.title.Text, "Disk: the disk usage comes from node-exporter") || r.title.Color != themeColor(th, colorMuted) {
		t.Errorf("disk note %q, %d rows", r.title.Text, len(r.rows))
	}
	p.apply(&engine.ObjectMetrics{Ref: ref, Status: metrics.StatusAvailable, Source: "Kubernetes Metrics Server"}, nil)
	if b.Visible() {
		t.Error("the disk shown without a disk or a note")
	}
	// A disk read without its filesystems shows the one its summary names.
	if fs := diskFilesystems(model.NodeDisk{UsedBytes: 1, SizeBytes: 2}); len(fs) != 1 || fs[0].Mountpoint != "/" {
		t.Errorf("a summary alone = %+v", fs)
	}
}

func TestRenderNodeDrawerDisk(t *testing.T) {
	for _, c := range []struct {
		mode ThemeMode
		name string
	}{{ThemeDark, "node_drawer_disk_dark.png"}, {ThemeLight, "node_drawer_disk_light.png"}} {
		t.Run(c.name, func(t *testing.T) {
			f := kindsFake(t, "nodes")
			withNodeBars(t, f)
			sh, _ := nodeDiskDrawerOn(t, f, c.mode)
			savePNG(t, sh.win, c.name)
		})
	}
}

// nodeDiskDrawer opens worker-a-01's drawer with Prometheus's history, the Metrics Server's live
// values and the node's disk.
func nodeDiskDrawer(t *testing.T, mode ThemeMode) (*shell, model.ObjectRef) {
	return nodeDiskDrawerOn(t, kindsFake(t, "nodes"), mode)
}

func nodeDiskDrawerOn(t *testing.T, f *fakeEngine, mode ThemeMode) (*shell, model.ObjectRef) {
	t.Helper()
	ref := model.ObjectRef{Context: "demo", Kind: "nodes", Name: fixtureNodeSpecs()[1].name}
	f.metrics = map[string]*engine.ObjectMetrics{ref.String(): {
		Ref: ref, Status: metrics.StatusAvailable, Source: "Prometheus (monitoring/prometheus-server) and Kubernetes Metrics Server",
		Targets: []engine.MetricsTarget{{
			Current: &model.Usage{CPUMilli: 1200, MemoryBytes: 3 << 30}, At: fixedNow,
			Samples:     metricsSeries(40, func(i int) model.Usage { return model.Usage{CPUMilli: int64(900 + 8*i), MemoryBytes: 3 << 30} }),
			Allocatable: model.Usage{CPUMilli: 7800, MemoryBytes: 30 << 30}, Capacity: model.Usage{CPUMilli: 8000, MemoryBytes: 32 << 30},
		}},
		Disk: &model.NodeDisk{Name: ref.Name, Mountpoint: "/data", UsedBytes: 172 << 30, SizeBytes: 200 << 30, Filesystems: workerA01Filesystems},
	}}
	f.ui.Selected, f.ui.Drawer = []model.ObjectRef{ref}, &ref
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the node metrics", func() bool {
		p, ok := sh.drawer.kept[metricsKey].(*metricsPanel)
		return ok && p.data != nil && sh.view.snap != nil && sh.diag.total > 0
	})
	settle(w)
	return sh, ref
}

func renderNodeBars(t *testing.T, mode ThemeMode, name string) {
	f := kindsFake(t, "nodes")
	withNodeBars(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the nodes view", func() bool {
		return len(sh.kinds) > 0 && sh.view.snap != nil && sh.view.snap.View == "nodes" && sh.diag.total > 0
	})
	settle(w)
	savePNG(t, w, name)
}

func TestRenderNodesUsageBarsDark(t *testing.T) {
	renderNodeBars(t, ThemeDark, "nodes_usage_bars_dark.png")
}

func TestRenderNodesUsageBarsLight(t *testing.T) {
	renderNodeBars(t, ThemeLight, "nodes_usage_bars_light.png")
}
