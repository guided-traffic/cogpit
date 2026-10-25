package fyneui

import (
	"fmt"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/guided-traffic/cogpit/internal/engine"
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

// A view entered after another was scrolled down (the owner's finding of 2026-10-23, v0.40.1: a
// view entered showed its header and "197 items" and no row until a minimal scroll) shows its rows
// from its top (tableView.scrollToTop), however its first snapshots come.

// longPods are 400 pods, a list longer than the one entered after it.
func longPods() []*unstructured.Unstructured {
	pods := scrollPods()
	for i := range 200 {
		pods = append(pods, reusePod("tools", fmt.Sprintf("worker-%03d", i), "ReplicaSet", "worker", "app"))
	}
	return pods
}

// scrolledDown is a shell showing the 400 pods scrolled to their end.
func scrolledDown(t *testing.T) (*fakeEngine, *shell, *mainQueue, *glPass) {
	t.Helper()
	all := kindSnapshot(t, "pods", longPods())
	f, sh, _, q, p := reuseShell(t, ThemeDark, all)
	sh.view.scrollTo(len(all.Rows) - 1)
	p.run(sh.win)
	if sh.view.table.body.Offset.Y == 0 {
		t.Fatal("the list did not scroll: the test needs it scrolled down")
	}
	return f, sh, q, p
}

// enter shows a view of a cluster, its snapshots one after the other as the engine sends them
// (nil while it has none), and runs a frame's pass after each.
func enter(t *testing.T, f *fakeEngine, sh *shell, q *mainQueue, p *glPass, view, contextID string, snaps ...*engine.TableSnapshot) {
	t.Helper()
	for i, snap := range snaps {
		f.setTable(view, snap)
		if i == 0 {
			f.update(func(u *engine.UIState) { u.View, u.Context = view, contextID })
		}
		q.until(t, fmt.Sprintf("snapshot %d of %s", i, view), func() bool { return sh.view.snap == snap })
		p.run(sh.win)
	}
}

// notSynced is a view's first snapshot before its list synced: no rows.
func notSynced(full *engine.TableSnapshot) *engine.TableSnapshot {
	s := *full
	s.Rows, s.Total, s.Synced, s.Revision = nil, 0, false, full.Revision-1
	return &s
}

func wantTop(t *testing.T, sh *shell) {
	t.Helper()
	if got := drawnRows(sh); !got[0] || !got[1] {
		t.Errorf("rows drawn %v of %d, want the first ones", got, len(sh.view.snap.Rows))
	}
	if off := sh.view.table.body.Offset.Y; off != 0 {
		t.Errorf("offset %v, want the top", off)
	}
}

// TestAViewEnteredBeforeItsRowsShowsThem: the owner's picture. The view entered has no snapshot
// yet, then one without rows (its list not synced), then its 150 rows: they were drawn from the
// offset the 400 pods left, past their end, so none was.
func TestAViewEnteredBeforeItsRowsShowsThem(t *testing.T) {
	f, sh, q, p := scrolledDown(t)
	full := kindSnapshot(t, "configmaps", scrollConfigMaps())
	full.Revision = 2
	enter(t, f, sh, q, p, "configmaps", "demo", nil, notSynced(full), full)
	wantTop(t, sh)
}

// TestAViewEnteredWithItsRowsShowsTheFirst: the view's first snapshot has its rows; the first row
// was left under the header row (Fyne's ScrollTo of the keyboard's first cell).
func TestAViewEnteredWithItsRowsShowsTheFirst(t *testing.T) {
	f, sh, q, p := scrolledDown(t)
	enter(t, f, sh, q, p, "configmaps", "demo", kindSnapshot(t, "configmaps", scrollConfigMaps()))
	wantTop(t, sh)
}

// TestAnotherClustersListStartsAtTheTop: the same view of another connected cluster (D-69) is a
// view entered: its list starts at its top, not at the offset the other cluster's left.
func TestAnotherClustersListStartsAtTheTop(t *testing.T) {
	for _, n := range []int{150, 400} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			f, sh, q, p := scrolledDown(t)
			var pods []*unstructured.Unstructured
			for i := range n {
				pods = append(pods, reusePod("shop", fmt.Sprintf("web-%03d", i), "ReplicaSet", "web", "app"))
			}
			other := kindSnapshot(t, "pods", pods)
			other.Context, other.Revision = "other", 9
			enter(t, f, sh, q, p, "pods", "other", nil, notSynced(other), other)
			wantTop(t, sh)
		})
	}
}

// scrollConfigMaps are 150 Config Maps, more than the window shows.
func scrollConfigMaps() []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for i := range 150 {
		out = append(out, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1", "kind": "ConfigMap",
			"metadata": map[string]any{"name": fmt.Sprintf("settings-%03d", i), "namespace": "default",
				"uid": fmt.Sprintf("uid-cm-%03d", i), "creationTimestamp": ts(time.Hour)},
		}})
	}
	return out
}

// A tab comes back as it was left, its list where it scrolled it (the owner, 2026-10-23: "zurück
// komme in meinem floating tab, dann ist er noch exakt so wie als ich ihn verlassen habe. Gleich
// Scroll-position"; tabscroll.go).

// showTab shows a tab of a cluster's view, its snapshots one after the other as the engine sends
// them, and runs a frame's pass after each.
func showTab(t *testing.T, f *fakeEngine, sh *shell, q *mainQueue, p *glPass, tab, view, contextID string, snaps ...*engine.TableSnapshot) {
	t.Helper()
	f.update(func(u *engine.UIState) { // as the engine's syncTabs: the active tab shows the view
		u.ActiveTab, u.View, u.Context = tab, view, contextID
		for i := range u.Tabs {
			if u.Tabs[i].ID == tab {
				u.Tabs[i].Context, u.Tabs[i].View = contextID, view
			}
		}
	})
	q.until(t, tab+" active", func() bool { return sh.ui.ActiveTab == tab && sh.ui.View == view })
	p.run(sh.win)
	for i, snap := range snaps {
		f.setTable(view, snap)
		q.until(t, fmt.Sprintf("snapshot %d of %s", i, tab), func() bool { return sh.view.snap == snap })
		p.run(sh.win)
	}
}

// twoTabs is a shell with the 400 pods of demo in tab-1 and a tab of another cluster's pods.
func twoTabs(t *testing.T) (*fakeEngine, *shell, *mainQueue, *glPass, *engine.TableSnapshot, *engine.TableSnapshot) {
	t.Helper()
	all := kindSnapshot(t, "pods", longPods())
	f, sh, _, q, p := reuseShell(t, ThemeDark, all)
	var pods []*unstructured.Unstructured
	for i := range 300 {
		pods = append(pods, reusePod("shop", fmt.Sprintf("web-%03d", i), "ReplicaSet", "web", "app"))
	}
	other := kindSnapshot(t, "pods", pods)
	other.Context, other.Revision = "other", 9
	f.update(func(u *engine.UIState) {
		u.Tabs = []engine.ViewTab{{ID: "tab-1", Context: "demo", View: "pods"}, {ID: "tab-2", Context: "other", View: "pods", Preview: true}}
		u.ActiveTab = "tab-1"
	})
	q.until(t, "tab-1 active", func() bool { return sh.ui.ActiveTab == "tab-1" })
	showTab(t, f, sh, q, p, "tab-1", "pods", "demo", all)
	return f, sh, q, p, all, other
}

// TestATabComesBackWhereItWasLeft: tab-1 scrolled to the middle, another cluster's tab shown and
// scrolled, tab-1 shown again (its rows after it, as a cluster shown again sends them): each tab's
// list is where it was left; the other cluster's tab is at its own place too.
func TestATabComesBackWhereItWasLeft(t *testing.T) {
	f, sh, q, p, all, other := twoTabs(t)
	sh.view.scrollTo(200)
	p.run(sh.win)
	left := sh.view.table.body.Offset
	if left.Y == 0 {
		t.Fatal("tab-1 did not scroll")
	}

	showTab(t, f, sh, q, p, "tab-2", "pods", "other", nil, notSynced(other), other)
	wantTop(t, sh) // never scrolled
	sh.view.scrollTo(120)
	p.run(sh.win)
	otherLeft := sh.view.table.body.Offset

	showTab(t, f, sh, q, p, "tab-1", "pods", "demo", nil, notSynced(all), all)
	if got := sh.view.table.body.Offset; got != left {
		t.Errorf("tab-1 back at %v, want %v where it was left", got, left)
	}
	if got := drawnRows(sh); !got[200] {
		t.Errorf("tab-1's rows drawn %v, want row 200 among them", got)
	}

	showTab(t, f, sh, q, p, "tab-2", "pods", "other", other) // its rows at once, from the cache
	if got := sh.view.table.body.Offset; got != otherLeft {
		t.Errorf("tab-2 back at %v, want %v where it was left", got, otherLeft)
	}

	// The rows before the tab: a moment's mix of tab-2 and tab-1's rows keeps nothing wrong.
	f.setTable("pods", all)
	q.until(t, "tab-1's rows under tab-2", func() bool { return sh.view.snap == all })
	p.run(sh.win)
	showTab(t, f, sh, q, p, "tab-1", "pods", "demo", all)
	if got := sh.view.table.body.Offset; got != left {
		t.Errorf("tab-1 back after the mix at %v, want %v", got, left)
	}
	showTab(t, f, sh, q, p, "tab-2", "pods", "other", other)
	if got := sh.view.table.body.Offset; got != otherLeft {
		t.Errorf("tab-2 back after the mix at %v, want %v", got, otherLeft)
	}
}

// TestATabsNewViewStartsAtTheTop: a view the tab opens anew starts at its top, and so does the
// view it left when the tab opens that again: only showing the tab again brings its place back.
func TestATabsNewViewStartsAtTheTop(t *testing.T) {
	f, sh, q, p, all, _ := twoTabs(t)
	sh.view.scrollTo(200)
	p.run(sh.win)
	showTab(t, f, sh, q, p, "tab-1", "configmaps", "demo", kindSnapshot(t, "configmaps", scrollConfigMaps()))
	wantTop(t, sh)
	all2 := *all
	all2.Revision++
	showTab(t, f, sh, q, p, "tab-1", "pods", "demo", &all2)
	wantTop(t, sh)
}

// TestAScrollEndsTheWaitForATabsRows: the user scrolls a tab shown again before its rows are all
// in; the rows coming after leave the list where the user put it.
func TestAScrollEndsTheWaitForATabsRows(t *testing.T) {
	f, sh, q, p, all, other := twoTabs(t)
	sh.view.scrollTo(380)
	p.run(sh.win)
	showTab(t, f, sh, q, p, "tab-2", "pods", "other", other)

	part := notSynced(all)
	part.Rows = all.Rows[:100]
	showTab(t, f, sh, q, p, "tab-1", "pods", "demo", part) // 100 of the 400 rows: as far as they reach
	sh.view.scrollTo(10)
	p.run(sh.win)
	mine := sh.view.table.body.Offset
	full := *all
	full.Revision += 5
	f.setTable("pods", &full)
	q.until(t, "the rest of the rows", func() bool { return sh.view.snap == &full })
	p.run(sh.win)
	if got := sh.view.table.body.Offset; got != mine {
		t.Errorf("after the rest came: at %v, want %v where the user scrolled", got, mine)
	}
}

// TestATabsViewOpenedAgainStartsAtTheTop (the owner's VW15 of 2026-10-25, his live walk's D6 step
// 5: "der Tab hat sich weiterhin gemerkt wo wir waren auch wenn die Vorschau neu geoeffnet wurde"):
// tab-1's Pods scrolled, then Config Maps and Pods again in the same tab: Pods starts at its top,
// whether the tab was left for another tab and shown again before (it came back where it was left,
// and the place it kept then is gone once it opens another view), and whether Config Maps' rows
// came or the tab went back to Pods before they did. A place kept afterwards comes back again.
func TestATabsViewOpenedAgainStartsAtTheTop(t *testing.T) {
	for _, tc := range []struct {
		name          string
		left, cmsRows bool
	}{
		{"same tab", false, true},
		{"same tab, before Config Maps' rows", false, false},
		{"left and shown again", true, true},
		{"left and shown again, before Config Maps' rows", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, sh, q, p, all, other := twoTabs(t)
			sh.view.scrollTo(200)
			p.run(sh.win)
			left := sh.view.table.body.Offset
			if left.Y == 0 {
				t.Fatal("tab-1 did not scroll")
			}
			if tc.left {
				showTab(t, f, sh, q, p, "tab-2", "pods", "other", other)
				showTab(t, f, sh, q, p, "tab-1", "pods", "demo", all)
				if got := sh.view.table.body.Offset; got != left {
					t.Fatalf("tab-1 back at %v, want %v where it was left", got, left)
				}
			}
			if tc.cmsRows {
				showTab(t, f, sh, q, p, "tab-1", "configmaps", "demo", kindSnapshot(t, "configmaps", scrollConfigMaps()))
				wantTop(t, sh)
			} else {
				showTab(t, f, sh, q, p, "tab-1", "configmaps", "demo") // no snapshot of it yet
			}
			again := *all
			again.Revision++
			showTab(t, f, sh, q, p, "tab-1", "pods", "demo", &again)
			wantTop(t, sh)

			sh.view.scrollTo(120) // kept from here on, as before
			p.run(sh.win)
			kept := sh.view.table.body.Offset
			showTab(t, f, sh, q, p, "tab-2", "pods", "other", other)
			showTab(t, f, sh, q, p, "tab-1", "pods", "demo", &again)
			if got := sh.view.table.body.Offset; got != kept {
				t.Errorf("tab-1 shown again at %v, want %v where it was left", got, kept)
			}
		})
	}
}
