package fyneui

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// A list that gets shorter while it is scrolled down (the owner's finding of 2026-10-22 on
// acme-d-pinniped: the namespace filter narrowed the scrolled Pods list to two pods, "2 of 200
// items", and no row was drawn): the table's offset comes back within the rows
// (tableView.keepScrollInRows).

// scrollPods are 198 pods in one namespace and two in kundenportal, whose names sort last.
func scrollPods() []*unstructured.Unstructured {
	var pods []*unstructured.Unstructured
	for i := range 198 {
		pods = append(pods, reusePod("default", fmt.Sprintf("api-%03d", i), "ReplicaSet", "api", "app"))
	}
	return append(pods, reusePod("kundenportal", "kundenportal-adminapp", "ReplicaSet", "adminapp", "app"),
		reusePod("kundenportal", "kundenportal-applikation", "ReplicaSet", "applikation", "app"))
}

// drawnRows are the rows whose first data cell is shown inside the table's viewport, below its
// header, by the row the cell shows.
func drawnRows(sh *shell) map[int]bool {
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject
	b := sh.view.table.body
	top := at(b).Y
	bottom := top + b.Size().Height
	out := map[int]bool{}
	for _, c := range findAll[*tableCell](sh.view.table) {
		if c.col != firstDataColumn || !c.Visible() || c.row < 0 || c.row >= len(sh.view.snap.Rows) {
			continue
		}
		if y := at(c).Y; y >= top && y+c.Size().Height <= bottom {
			out[c.row] = true
		}
	}
	return out
}

// TestAFilterAfterAScrollShowsItsRows: the list scrolled to its end, the namespace filter leaves
// two rows, and both are drawn, the list at its top.
func TestAFilterAfterAScrollShowsItsRows(t *testing.T) {
	all := kindSnapshot(t, "pods", scrollPods())
	f, sh, _, q, p := reuseShell(t, ThemeDark, all)
	sh.view.scrollTo(len(all.Rows) - 1)
	p.run(sh.win)
	if sh.view.table.body.Offset.Y == 0 {
		t.Fatal("the list did not scroll: the test needs it scrolled down")
	}
	two := inNamespace(all, "kundenportal")
	showPods(t, f, sh, q, p, two)
	if got := drawnRows(sh); !got[0] || !got[1] {
		t.Errorf("rows drawn %v of %d, want 0 and 1", got, len(two.Rows))
	}
	if off := sh.view.table.body.Offset.Y; off != 0 {
		t.Errorf("offset %v, want the top", off)
	}
	lookPNG(t, sh.win, "pods_filtered_after_scroll.png") // as the frame left it, not settled

	showPods(t, f, sh, q, p, all) // the filter cleared: the list from its top
	if got := drawnRows(sh); !got[0] {
		t.Errorf("after the filter was cleared, rows drawn %v, want the first", got)
	}
}

// TestRowsGoneAtTheEndKeepTheEndShown: scrolled to its end, the list loses its last rows (a watch
// event, a status filter) and still has more than fit; it shows its new last row at the bottom.
func TestRowsGoneAtTheEndKeepTheEndShown(t *testing.T) {
	all := kindSnapshot(t, "pods", scrollPods())
	f, sh, _, q, p := reuseShell(t, ThemeDark, all)
	sh.view.scrollTo(len(all.Rows) - 1)
	p.run(sh.win)
	fewer := *all
	fewer.Rows = all.Rows[:len(all.Rows)-60]
	fewer.Revision++
	showPods(t, f, sh, q, p, &fewer)
	last := len(fewer.Rows) - 1
	if got := drawnRows(sh); !got[last] || got[0] {
		t.Errorf("rows drawn %v, want the end of the %d rows", got, len(fewer.Rows))
	}
}
