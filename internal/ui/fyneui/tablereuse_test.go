package fyneui

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The tests of a table cell that gets new content without a layout. A colleague of the owner saw
// one pod of a DaemonSet's thirteen with three container squares, the others with two, after the
// namespace filter had narrowed 263 rows to 13 (2026-10-19); a reconnect made it go away. Fyne's
// table gives a cell it keeps on screen new content without laying it out when the cell's size is
// unchanged, and the GL driver lays a cell out again only when one of its objects' minimum size
// changed (glPass below), which a Containers cell whose squares all fit never does: its text stays
// empty. So the cell kept the third square of the pod it showed before. The pods below are made up
// in the picture's shape; the usage bars of the Nodes list go through the same layout.

// glPass is the GL driver's pass before each frame (Canvas.EnsureMinSize in Fyne 2.8.2's
// internal/driver/common), so that these tests lay out what the running app lays out and no more:
// settle lays out every widget, which would hide a cell the table updated without a layout. The
// driver keeps a node per visible object by its place among its parent's visible children: where
// the object at a place changed, that node and every one after it are new, with a minimum size of
// zero. Walking children first, an object whose minimum size differs from its node's has its
// parent laid out when the walk reaches the parent, one pending parent at a time as in the driver.
type glPass struct {
	root    *glNode
	pending *glNode
}

type glNode struct {
	obj  fyne.CanvasObject
	min  fyne.Size
	kids []*glNode
}

// run walks the window's content as one frame does.
func (p *glPass) run(w fyne.Window) {
	content := w.Canvas().Content()
	if p.root == nil || p.root.obj != content {
		p.root = &glNode{obj: content}
	}
	p.pending = nil
	p.visit(p.root, nil)
}

func (p *glPass) visit(n, parent *glNode) {
	var kids []fyne.CanvasObject
	switch x := n.obj.(type) {
	case *fyne.Container:
		kids = x.Objects
	case fyne.Widget:
		kids = test.WidgetRenderer(x).Objects()
	}
	old, now, kept := n.kids, make([]*glNode, 0, len(kids)), true
	for _, k := range kids {
		if k == nil || !k.Visible() {
			continue
		}
		i := len(now)
		kept = kept && i < len(old) && old[i].obj == k
		c := &glNode{obj: k}
		if kept {
			c = old[i]
		}
		now = append(now, c)
		p.visit(c, n)
	}
	n.kids = now
	if p.pending == n {
		p.pending = nil
		switch x := n.obj.(type) {
		case *fyne.Container:
			if x.Layout != nil {
				x.Layout.Layout(x.Objects, x.Size())
			}
		case fyne.Widget:
			test.WidgetRenderer(x).Layout(x.Size())
		}
	}
	if m := n.obj.MinSize(); m != n.min {
		n.min = m
		if parent != nil {
			p.pending = parent
		}
	}
}

// reusePods are a three-container pod whose name sorts first, a DaemonSet's thirteen two-container
// pods in a namespace of their own, and extra pods of each kind after them to scroll through.
func reusePods(extra int) []*unstructured.Unstructured {
	out := []*unstructured.Unstructured{reusePod("monitoring", "collector-0", "StatefulSet", "collector", "collector", "config-reloader", "sidecar")}
	for i := range 13 {
		out = append(out, reusePod("telemetry", fmt.Sprintf("log-agent-%c%c4k%c", 'b'+rune(i), 'q'+rune(i%7), 'x'+rune(i%3)), "DaemonSet", "log-agent", "agent", "config-reloader"))
	}
	for i := range extra {
		out = append(out,
			reusePod("monitoring", fmt.Sprintf("metrics-store-%02d", i), "StatefulSet", "metrics-store", "store", "config-reloader", "sidecar"),
			reusePod("telemetry", fmt.Sprintf("node-agent-%02d", i), "DaemonSet", "node-agent", "agent", "config-reloader"))
	}
	return out
}

// reusePod is a made-up Running pod, its containers running and ready.
func reusePod(ns, name, ownerKind, owner string, containers ...string) *unstructured.Unstructured {
	return fixturePodObject(podSpec{ns: ns, name: name, ownerKind: ownerKind, ownerName: owner, node: "worker-a-01",
		state: podOK, containers: containers, age: 40 * 24 * time.Hour})
}

// inNamespace is snap narrowed to one namespace, as the engine's namespace filter hands it on.
func inNamespace(snap *engine.TableSnapshot, ns string) *engine.TableSnapshot {
	c := *snap
	c.Rows = nil
	for _, r := range snap.Rows {
		if r.Ref.Namespace == ns {
			c.Rows = append(c.Rows, r)
		}
	}
	c.Revision++
	return &c
}

// reuseShell shows first in the Pods view and returns the shell after its first frame.
func reuseShell(t *testing.T, mode ThemeMode, first *engine.TableSnapshot) (*fakeEngine, *shell, fyne.Window, *mainQueue, *glPass) {
	t.Helper()
	f := kindsFake(t, "pods")
	f.tables["pods"] = first
	sh, w, q := newTestShell(t, f, Options{Theme: mode})
	q.until(t, "the pods", func() bool { return sh.view.snap == first })
	settle(w)
	p := &glPass{}
	p.run(w)
	return f, sh, w, q, p
}

// showPods replaces the Pods view's snapshot, as a changed filter or a watch event does, and runs
// the next frame's pass.
func showPods(t *testing.T, f *fakeEngine, sh *shell, q *mainQueue, p *glPass, snap *engine.TableSnapshot) {
	t.Helper()
	f.setTable("pods", snap)
	q.until(t, "the new rows", func() bool { return sh.view.snap == snap })
	p.run(sh.win)
}

// drawnSquares counts a cell's squares that draw: shown and of a square's size.
func drawnSquares(c *tableCell) int {
	side, _ := markSize(c.Theme())
	n := 0
	for _, r := range c.marks {
		if r.Visible() && r.Size() == fyne.NewSize(side, side) {
			n++
		}
	}
	return n
}

// checkSquares checks every Containers cell the table draws against the row it shows: one square
// drawn per container, each at its place and in its container's colour, and none more.
func checkSquares(t *testing.T, sh *shell, how string) {
	t.Helper()
	col := columnOf(t, sh, "containers")
	cells := 0
	for _, c := range findAll[*tableCell](sh.view.table) {
		if c.col != col || !c.Visible() || c.row < 0 || c.row >= len(sh.view.snap.Rows) {
			continue
		}
		cells++
		row := sh.view.snap.Rows[c.row]
		marks := *row.Cells[sh.view.cols[col-firstDataColumn]].Marks
		if got := drawnSquares(c); got != len(marks) {
			t.Errorf("%s: %s draws %d squares for its %d containers", how, row.Ref.Name, got, len(marks))
			continue
		}
		th := c.Theme()
		side, gap := markSize(th)
		pad := th.Size(sizeCellPadding)
		for i, m := range marks {
			r := c.marks[i]
			at := fyne.NewPos(pad+float32(i)*(side+gap), (c.Size().Height-side)/2)
			if r.Position() != at || r.FillColor != markColor(th, m.Status) {
				t.Errorf("%s: %s square %d at %v in %v, want %v in %s", how, row.Ref.Name, i, r.Position(), r.FillColor, at, markColorName(m.Status))
			}
		}
	}
	if cells == 0 {
		t.Fatalf("%s: no Containers cell on screen", how)
	}
}

// The colleague's case: the namespace filter narrows the list, and the cell that showed the
// three-container pod shows a two-container pod with two squares; cleared, three again.
func TestContainersCellReusedByAFilter(t *testing.T) {
	all := kindSnapshot(t, "pods", reusePods(0))
	f, sh, _, q, p := reuseShell(t, ThemeLight, all)
	checkSquares(t, sh, "all namespaces")

	showPods(t, f, sh, q, p, inNamespace(all, "telemetry"))
	checkSquares(t, sh, "three squares, then the filter")

	showPods(t, f, sh, q, p, all)
	checkSquares(t, sh, "the filter cleared")
}

// The other direction: a cell that was laid out while it showed two squares (a scroll, a column's
// new width) keeps its third square hidden, and shows a three-container pod with two. A square the
// cell makes anew is laid out by the next frame's pass (a canvas object's minimum size is 1×1, its
// new node's zero); one it made before and hid is not.
func TestContainersCellReusedForMoreContainers(t *testing.T) {
	all := kindSnapshot(t, "pods", reusePods(0))
	f, sh, w, q, p := reuseShell(t, ThemeDark, all)
	showPods(t, f, sh, q, p, inNamespace(all, "telemetry"))
	settle(w) // the layout of a cell that shows two squares
	p.run(w)
	checkSquares(t, sh, "the filter, laid out")

	showPods(t, f, sh, q, p, all)
	checkSquares(t, sh, "two squares laid out, then three")
}

// A pod that comes or goes above the others moves every row below it to another cell, which the
// table updates without a layout as a filter does: both directions at once.
func TestContainersCellRowsMove(t *testing.T) {
	all := kindSnapshot(t, "pods", reusePods(0))
	f, sh, _, q, p := reuseShell(t, ThemeDark, all)

	more := *all
	more.Rows = slices.Insert(slices.Clone(all.Rows), 0,
		podRow(t, reusePod("telemetry", "a-log-agent-x7k2p", "DaemonSet", "log-agent", "agent", "config-reloader")))
	more.Revision++
	showPods(t, f, sh, q, p, &more)
	checkSquares(t, sh, "a two-container pod above the others")

	showPods(t, f, sh, q, p, all)
	checkSquares(t, sh, "and gone again")
}

// podRow is a pod's row through the real profile.
func podRow(t *testing.T, o *unstructured.Unstructured) model.Row {
	t.Helper()
	return kindSnapshot(t, "pods", []*unstructured.Unstructured{o}).Rows[0]
}

// The colleague's picture, made up: the Pods list narrowed to a DaemonSet's thirteen two-container
// pods by the namespace filter, right after it showed a three-container pod in the first row.
// PNGs: containers_reused_{dark,light}.png, captured after the frame's pass and no other layout.
func renderContainersReused(t *testing.T, mode ThemeMode, name string) {
	all := kindSnapshot(t, "pods", reusePods(0))
	f, sh, w, q, p := reuseShell(t, mode, all)
	f.update(func(u *engine.UIState) { u.Namespaces = []string{"telemetry"} })
	showPods(t, f, sh, q, p, inNamespace(all, "telemetry"))
	p.run(w) // the namespace button's new text, as the next frame lays it out
	writePNG(t, w, name)
}

func TestRenderContainersReusedDark(t *testing.T) {
	renderContainersReused(t, ThemeDark, "containers_reused_dark.png")
}

func TestRenderContainersReusedLight(t *testing.T) {
	renderContainersReused(t, ThemeLight, "containers_reused_light.png")
}

// A scroll shows the rows that come into view in cells from the table's pool. The driver's nodes
// follow the cells' order, which a scroll changes, so it lays the cells out again: the squares are
// right with or without the update path placing them.
func TestContainersCellReusedByAScroll(t *testing.T) {
	all := kindSnapshot(t, "pods", reusePods(40))
	_, sh, _, _, p := reuseShell(t, ThemeDark, all)
	last := len(all.Rows) - 1
	for round := range 4 {
		sh.view.scrollTo(last)
		p.run(sh.win)
		checkSquares(t, sh, fmt.Sprintf("scroll %d down", round))
		sh.view.scrollTo(0)
		p.run(sh.win)
		checkSquares(t, sh, fmt.Sprintf("scroll %d up", round))
	}
}

// The Nodes list's usage bars go through the same layout: a node that leaves the list moves every
// row below it to another cell, whose share, fill and text follow the node it shows now.
func TestUsageBarCellRowsMove(t *testing.T) {
	f := kindsFake(t, "nodes")
	withNodeBars(t, f)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	all := f.tables["nodes"]
	q.until(t, "the nodes", func() bool { return sh.view.snap == all })
	settle(w)
	p := &glPass{}
	p.run(w)

	fewer := *all
	fewer.Rows = slices.DeleteFunc(slices.Clone(all.Rows), func(r model.Row) bool { return r.Ref.Name == "control-plane-1" })
	fewer.Revision++
	for _, snap := range []*engine.TableSnapshot{&fewer, all} {
		f.setTable("nodes", snap)
		q.until(t, "the new rows", func() bool { return sh.view.snap == snap })
		p.run(w)
		checked := 0
		for _, c := range findAll[*tableCell](sh.view.table) {
			if !c.Visible() || c.row < 0 || c.row >= len(snap.Rows) || c.col < firstDataColumn || c.col >= sh.view.menuColumn() {
				continue
			}
			cell := snap.Rows[c.row].Cells[sh.view.cols[c.col-firstDataColumn]]
			name := snap.Rows[c.row].Ref.Name
			if cell.Bar == nil {
				if c.bar != nil && (c.bar.track.Visible() || c.bar.fill.Visible()) {
					t.Errorf("%d rows: %s draws a bar in a cell without one (%q)", len(snap.Rows), name, c.full)
				}
				continue
			}
			checked++
			if c.bar == nil || !c.bar.track.Visible() {
				t.Errorf("%d rows: %s draws no bar for %q", len(snap.Rows), name, cell.Text)
				continue
			}
			tw, fw := c.bar.track.Size().Width, c.bar.fill.Size().Width
			if want := tw * float32(min(cell.Bar.Share, 1)); fw < want-1.5 || fw > max(want, 2)+1.5 {
				t.Errorf("%d rows: %s fill %v of a track of %v, want the share %.3f", len(snap.Rows), name, fw, tw, cell.Bar.Share)
			}
			if end := c.text.Position().X + c.text.MinSize().Width; end > c.bar.track.Position().X {
				t.Errorf("%d rows: %s's text %q ends at %v, after the bar's start %v", len(snap.Rows), name, c.text.Text, end, c.bar.track.Position().X)
			}
		}
		if checked == 0 {
			t.Fatalf("%d rows: no bar on screen", len(snap.Rows))
		}
	}
}
