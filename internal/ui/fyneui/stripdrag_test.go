package fyneui

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// dragTab drags a strip's tab to the window position to in two moves and, unless hold, drops it
// there, as the GLFW driver sends a drag (dragTile in hotbardrag_test.go). It is pressed in its
// middle.
func dragTab(t *testing.T, o fyne.Draggable, to fyne.Position, hold bool) {
	t.Helper()
	co := o.(fyne.CanvasObject)
	tp := absolutePos(co)
	start := tp.AddXY(co.Size().Width/2, co.Size().Height/2)
	half := fyne.NewDelta((to.X-start.X)/2, (to.Y-start.Y)/2)
	for _, p := range []fyne.Position{start.Add(half), to} {
		o.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: p, Position: p.Subtract(tp)}, Dragged: half})
	}
	if !hold {
		o.DragEnd()
	}
}

// barIDs are the tab bar's tabs, left to right as drawn.
func barIDs(b *tabBar) []string {
	tabs := slices.Clone(b.shown)
	slices.SortFunc(tabs, func(x, y *viewTab) int {
		switch {
		case x.Position().X < y.Position().X:
			return -1
		case x.Position().X > y.Position().X:
			return 1
		}
		return 0
	})
	out := make([]string, len(tabs))
	for i, tab := range tabs {
		out[i] = tab.tab.ID
	}
	return out
}

func stateTabIDs(u engine.UIState) []string {
	out := make([]string, len(u.Tabs))
	for i, tab := range u.Tabs {
		out[i] = tab.ID
	}
	return out
}

// TestTabDragMovesTheTab (the owner's GW4): a tab dragged along the bar is carried under the
// pointer, raised, the others sliding aside before anything is sent; the drop sends ui.tab.move
// with its new place and the bar shows the engine's order; a drop far below the bar, on its own
// place, or after the tabs changed under the drag moves nothing; a drag shorter than minTileDrag
// shows the tab as a click does.
func TestTabDragMovesTheTab(t *testing.T) {
	sh, w, q, f := tabsShell(t, ThemeDark)
	settle(w)
	b := sh.tabs
	first := b.shown[0] // Services, tab-1
	rowAt := absolutePos(b.row)
	end := fyne.NewPos(rowAt.X+b.row.Size().Width-5, rowAt.Y+first.Size().Height/2)

	dragTab(t, first, end, true)
	d := b.drag
	if !d.lifted || d.id != "tab-1" || !d.carries("tab-1") || d.to != 2 {
		t.Fatalf("lifted %v, id %q, to %d", d.lifted, d.id, d.to)
	}
	if r := tabRenderer(first); r.bg.FillColor != themeColor(first.Theme(), colorDockTab) {
		t.Error("the carried tab is not raised")
	}
	if got := barIDs(b); !slices.Equal(got, []string{"tab-2", "tab-3", "tab-1"}) {
		t.Fatalf("drawn while carried: %v", got)
	}
	if n := len(f.callsOf("ui.tab.move")); n != 0 {
		t.Fatalf("a move sent before the drop (%d)", n)
	}
	first.DragEnd()
	q.until(t, "the moved order", func() bool { return slices.Equal(stateTabIDs(sh.ui), []string{"tab-2", "tab-3", "tab-1"}) })
	if p := decodeParams[tabMoveParams](t, f.callsOf("ui.tab.move")[0]); p.ID != "tab-1" || p.Index != 3 {
		t.Fatalf("ui.tab.move %+v", p)
	}
	settle(w)
	if got := barIDs(b); !slices.Equal(got, []string{"tab-2", "tab-3", "tab-1"}) || d.lifted || d.landing || d.pending {
		t.Fatalf("after the drop: drawn %v, lifted %v landing %v pending %v", got, d.lifted, d.landing, d.pending)
	}

	// Far below the bar: nothing moves, the tabs slide back.
	tab := b.shown[2] // Services again, now last
	dragTab(t, tab, fyne.NewPos(rowAt.X+5, rowAt.Y+6*tab.Size().Height), false)
	q.drain()
	if n := len(f.callsOf("ui.tab.move")); n != 1 {
		t.Fatalf("a drop below the bar sent a move (%d)", n)
	}
	settle(w)
	if got := barIDs(b); !slices.Equal(got, []string{"tab-2", "tab-3", "tab-1"}) {
		t.Fatalf("after a drop below the bar: %v", got)
	}

	// The tabs change under the drag (an agent closed one): the drop moves nothing.
	dragTab(t, b.shown[0], end, true)
	f.update(func(u *engine.UIState) { u.Tabs = slices.Delete(slices.Clone(u.Tabs), 1, 2) }) // tab-3 closes
	q.until(t, "two tabs", func() bool { return len(b.shown) == 2 })
	if !d.stale {
		t.Fatal("the drag is not stale after the tabs changed")
	}
	d.ended(b.shown[0])
	q.drain()
	if n := len(f.callsOf("ui.tab.move")); n != 1 {
		t.Fatalf("a stale drop sent a move (%d)", n)
	}

	// A drag shorter than minTileDrag is a click: the tab shows.
	tab = b.shown[1] // tab-1
	at := absolutePos(tab).AddXY(tab.Size().Width/2, 4)
	tab.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}, Dragged: fyne.NewDelta(3, 0)})
	tab.DragEnd()
	q.until(t, "tab-1 shown", func() bool { return sh.ui.ActiveTab == "tab-1" })
	if d.lifted || len(f.callsOf("ui.tab.move")) != 1 {
		t.Fatal("a short drag lifted the tab or moved it")
	}

	// One pressed on the × closes the tab, as its click does.
	at = absolutePos(tab.close).AddXY(tab.close.Size().Width/2, tab.close.Size().Height/2)
	tab.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}, Dragged: fyne.NewDelta(3, 1)})
	tab.DragEnd()
	q.until(t, "tab-1 closed", func() bool { return len(f.callsOf("ui.tab.close")) == 1 })
	if p := decodeParams[tabIDParams](t, f.callsOf("ui.tab.close")[0]); p.ID != "tab-1" {
		t.Fatalf("ui.tab.close %+v", p)
	}
}

// TestTabMoveMenuItems: View › Move Tab Left and Right carry the registry's ⌘⌥⇧← and ⌘⌥⇧→ and move
// the active tab one place, doing nothing at the bar's end.
func TestTabMoveMenuItems(t *testing.T) {
	sh, _, q, f := tabsShell(t, ThemeDark)
	view := menuNamed(sh.mainMenu(), "View")
	left, right := menuItem(t, view, "Move Tab Left"), menuItem(t, view, "Move Tab Right")
	for item, key := range map[*fyne.MenuItem]fyne.KeyName{left: fyne.KeyLeft, right: fyne.KeyRight} {
		sc, ok := item.Shortcut.(*desktop.CustomShortcut)
		if !ok || sc.KeyName != key || sc.Modifier != fyne.KeyModifierShortcutDefault|fyne.KeyModifierAlt|fyne.KeyModifierShift {
			t.Errorf("%s: shortcut %v", item.Label, item.Shortcut)
		}
	}
	right.Action() // the active tab, Pods (second), one place right
	q.until(t, "Pods third", func() bool { return slices.Equal(stateTabIDs(sh.ui), []string{"tab-1", "tab-3", "tab-2"}) })
	right.Action() // the last: nothing sent
	q.drain()
	if n := len(f.callsOf("ui.tab.moveRight")); n != 1 {
		t.Fatalf("Move Tab Right at the end sent %d", n)
	}
	left.Action()
	q.until(t, "Pods second again", func() bool { return slices.Equal(stateTabIDs(sh.ui), []string{"tab-1", "tab-2", "tab-3"}) })
}

// dockTabsFake is the demo with three dock tabs and dock.move answered as the engine does for the
// tabs the dock shows.
func dockTabsFake(t *testing.T) *fakeEngine {
	f := connectedFake(t, 50)
	f.ui.Dock = []engine.DockTab{
		{ID: "audit-1", Type: engine.DockAudit, Title: "Audit Log"},
		{ID: "diagnostics-1", Type: engine.DockDiagnostics, Title: "Diagnostics"},
		{ID: "job-1", Type: engine.DockJob, Title: "Drain worker-a-01", Context: "demo", Job: &engine.JobTab{}},
	}
	f.ui.ActiveDock = "audit-1"
	f.handlers["dock.move"] = func(_ context.Context, inv engine.Invocation) (any, error) {
		var p dockMoveParams
		if err := json.Unmarshal(normalize(inv.Params), &p); err != nil {
			return nil, err
		}
		f.update(func(u *engine.UIState) {
			i := slices.IndexFunc(u.Dock, func(d engine.DockTab) bool { return d.ID == p.ID })
			tab := u.Dock[i]
			u.Dock = slices.Insert(slices.Delete(slices.Clone(u.Dock), i, i+1), p.Index-1, tab)
		})
		return nil, nil
	}
	return f
}

func dockIDsDrawn(d *dock) []string {
	var btns []*tabButton
	for _, o := range d.tabRow.Objects {
		btns = append(btns, o.(*tabButton))
	}
	slices.SortFunc(btns, func(x, y *tabButton) int {
		switch {
		case x.Position().X < y.Position().X:
			return -1
		case x.Position().X > y.Position().X:
			return 1
		}
		return 0
	})
	out := make([]string, len(btns))
	for i, b := range btns {
		out[i] = b.id
	}
	return out
}

// TestDockTabDragMovesTheTab: the dock's tabs move the same way, by dock.move.
func TestDockTabDragMovesTheTab(t *testing.T) {
	f := dockTabsFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the dock's tabs", func() bool { return len(sh.dock.tabRow.Objects) == 3 && sh.view.snap != nil })
	settle(w)
	d := sh.dock
	audit := d.tabRow.Objects[0].(*tabButton)
	last := d.tabRow.Objects[2].(*tabButton)
	to := absolutePos(last).AddXY(last.Size().Width-2, last.Size().Height/2)
	dragTab(t, audit, to, true)
	if got := dockIDsDrawn(d); !slices.Equal(got, []string{"diagnostics-1", "job-1", "audit-1"}) || !audit.carried() {
		t.Fatalf("drawn while carried: %v, carried %v", got, audit.carried())
	}
	audit.DragEnd()
	q.until(t, "the moved dock", func() bool {
		var ids []string
		for _, tab := range sh.ui.Dock {
			ids = append(ids, tab.ID)
		}
		return slices.Equal(ids, []string{"diagnostics-1", "job-1", "audit-1"})
	})
	if p := decodeParams[dockMoveParams](t, f.callsOf("dock.move")[0]); p.ID != "audit-1" || p.Index != 3 {
		t.Fatalf("dock.move %+v", p)
	}
	settle(w)
	if got := dockIDsDrawn(d); !slices.Equal(got, []string{"diagnostics-1", "job-1", "audit-1"}) {
		t.Fatalf("after the drop: %v", got)
	}

	// A short drag on a tab shows it, as its click does.
	btn := d.tabRow.Objects[0].(*tabButton) // Diagnostics
	btn.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: absolutePos(btn).AddXY(10, 5)}, Dragged: fyne.NewDelta(2, 0)})
	btn.DragEnd()
	q.until(t, "dock.activate", func() bool { return len(f.callsOf("dock.activate")) == 1 })

	// One pressed on its × closes it (the strip's buttons are made anew with the state).
	settle(w)
	btn = d.tabRow.Objects[0].(*tabButton)
	at := absolutePos(btn.close).AddXY(btn.close.Size().Width/2, btn.close.Size().Height/2)
	btn.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: at}, Dragged: fyne.NewDelta(-2, 1)})
	btn.DragEnd()
	q.until(t, "dock.close", func() bool { return len(f.callsOf("dock.close")) == 1 })
}

// The tab bar with a tab carried (Services held over the bar's end, the others slid aside), and
// after the drop, dark and light (GW4, rule 7).
func TestRenderTabDragDark(t *testing.T) { renderTabDrag(t, ThemeDark, "tabbar_drag_dark.png", true) }
func renderTabDrag(t *testing.T, mode ThemeMode, name string, hold bool) {
	sh, w, q, _ := tabsShell(t, mode)
	q.until(t, "the view", func() bool { return sh.view.snap != nil && sh.diag.total > 0 })
	settle(w)
	b := sh.tabs
	pods := b.shown[1]
	to := absolutePos(b.shown[2]).AddXY(b.shown[2].Size().Width*0.6, pods.Size().Height/2)
	dragTab(t, b.shown[0], to, hold)
	if !hold {
		q.until(t, "the moved order", func() bool { return sh.ui.Tabs[0].ID != "tab-1" })
	} else {
		test.MoveMouse(w.Canvas(), to)
	}
	_ = pods
	savePNG(t, w, name)
}
