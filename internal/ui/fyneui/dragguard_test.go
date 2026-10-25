package fyneui

import (
	"image/color"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

// strayDrag sends o the events Fyne's GLFW driver sends for a drag whose button went down at
// press, outside o, and whose pointer then came onto o and went along path (the owner's VW20): the
// first carries the whole way from the press, each next the step since the one before. The caller
// ends the drag.
func strayDrag(o fyne.Draggable, press fyne.Position, path ...fyne.Position) {
	at := absolutePos(o.(fyne.CanvasObject))
	last := press
	for _, p := range path {
		o.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: p, Position: p.Subtract(at)}, Dragged: fyne.NewDelta(p.X-last.X, p.Y-last.Y)})
		last = p
	}
}

// The guard decides on a drag's first event whether the press lay on the object, with Fyne's own
// bounds (the left and top edge on it, the right and bottom edge off), keeps that decision while
// the object moves under the drag, and forgets it at DragEnd. The test driver's events, which carry
// no pointer, and a DragEnd without any event go on.
func TestDragGuardDecidesOnTheFirstEvent(t *testing.T) {
	test.NewTempApp(t)
	r := canvas.NewRectangle(color.Black)
	w := test.NewWindow(container.NewWithoutLayout(r))
	defer w.Close()
	w.SetPadded(false)
	w.Resize(fyne.NewSize(400, 300))
	r.Move(fyne.NewPos(50, 20))
	r.Resize(fyne.NewSize(40, 30))
	ev := func(press, pointer fyne.Position) *fyne.DragEvent {
		return &fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: pointer}, Dragged: fyne.NewDelta(pointer.X-press.X, pointer.Y-press.Y)}
	}
	var g dragGuard

	// Pressed on it, carried away: the object follows, and the drag stays its own.
	if !g.pass(r, ev(fyne.NewPos(60, 25), fyne.NewPos(64, 25))) {
		t.Fatal("a drag pressed on the object did not pass")
	}
	r.Move(fyne.NewPos(250, 200))
	if !g.pass(r, ev(fyne.NewPos(64, 25), fyne.NewPos(70, 25))) || !g.end() {
		t.Fatal("a drag pressed on the object stopped passing once the object moved")
	}
	r.Move(fyne.NewPos(50, 20))

	// Pressed beside it, carried onto it: nothing passes, the end neither; the next drag decides anew.
	if g.pass(r, ev(fyne.NewPos(20, 25), fyne.NewPos(60, 25))) || g.pass(r, ev(fyne.NewPos(60, 25), fyne.NewPos(62, 26))) {
		t.Fatal("a drag pressed beside the object passed")
	}
	if g.end() {
		t.Fatal("the end of a drag pressed beside the object passed")
	}
	if !g.pass(r, ev(fyne.NewPos(89, 49), fyne.NewPos(95, 49))) || !g.end() {
		t.Fatal("the drag after an ignored one, pressed on the object's last point, did not pass")
	}

	for _, c := range []struct {
		press fyne.Position
		on    bool
	}{
		{fyne.NewPos(50, 20), true},  // the top left corner
		{fyne.NewPos(90, 30), false}, // the right edge: the next object's
		{fyne.NewPos(60, 50), false}, // the bottom edge
		{fyne.NewPos(49, 30), false},
	} {
		if got := g.pass(r, ev(c.press, c.press.AddXY(3, 0))); got != c.on {
			t.Errorf("pressed at %v: passed %v, want %v", c.press, got, c.on)
		}
		g.end()
	}

	if !g.pass(r, &fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(5, 5)}, Dragged: fyne.NewDelta(300, 0)}) || !g.end() {
		t.Error("the test driver's drag did not pass")
	}
	if !g.end() {
		t.Error("a DragEnd without an event did not pass")
	}
}

// A drag pressed beside the hotbar's tiles that the pointer carries onto one is not the tile's
// (the owner's VW20): no ghost, no empty slot, the tiles stay; its end neither moves the tile nor
// opens its cluster, a short one included, which was the click that slipped. A drag pressed on the
// tile still carries it.
func TestStrayDragLeavesTheHotbarAlone(t *testing.T) {
	sh, w, q, f, _ := hotbarScene(t, ThemeDark)
	settle(w)
	h := sh.hot
	before, slots := contextsOf(h), slotYs(h)
	tl := tilesOf(h)[0]
	mid := centreOf(h, 0)

	// Pressed far right of the bar, in the view; carried onto the first tile and down the bar.
	strayDrag(tl, mid.AddXY(400, 300), mid, centreOf(h, 3), centreOf(h, 4))
	d := h.drag
	if d.active || d.lifted || d.ghost != nil || len(d.float.Objects) != 0 || tl.carried() {
		t.Fatalf("a stray drag took the tile: active %v lifted %v ghost %v layer %d", d.active, d.lifted, d.ghost, len(d.float.Objects))
	}
	wantOrder(t, h, slots, before...)
	tl.DragEnd()

	// Pressed just above the tile, the pointer two points onto it: a short drag.
	top := absolutePos(tl).AddXY(tl.Size().Width/2, 0)
	strayDrag(tl, top.AddXY(0, -1), top.AddXY(0, 2))
	tl.DragEnd()
	q.drain()
	if n, m := len(f.callsOf("hotbar.moveCluster")), len(f.callsOf("hotbar.openCluster")); n+m != 0 {
		t.Fatalf("stray drags sent %d moves and %d opens", n, m)
	}
	settle(w)
	if got := contextsOf(h); !slices.Equal(got, before) {
		t.Fatalf("after stray drags: %v", got)
	}
	wantOrder(t, h, slots, before...)

	// Pressed on the tile: carried onto the fifth place.
	dragTile(t, h, 0, centreOf(h, 4).AddXY(3, 5), false)
	q.until(t, "the move", func() bool { return len(f.callsOf("hotbar.moveCluster")) == 1 })
}

// The same for the tab bar's tabs and the dock's: a drag pressed below the bar is not a tab's, and
// its end neither moves, selects nor closes one; a drag pressed on a tab still carries it.
func TestStrayDragLeavesTheTabsAlone(t *testing.T) {
	sh, w, q, f := tabsShell(t, ThemeDark)
	settle(w)
	b := sh.tabs
	first, third := b.shown[0], b.shown[2] // tab-2 is shown
	mid := absolutePos(first).AddXY(first.Size().Width/2, first.Size().Height/2)
	rowAt := absolutePos(b.row)
	end := fyne.NewPos(rowAt.X+b.row.Size().Width-5, mid.Y)

	strayDrag(first, mid.AddXY(0, 200), mid, end)
	d := b.drag
	if d.active || d.lifted || d.carries(first.tab.ID) {
		t.Fatalf("a stray drag took the tab: active %v lifted %v", d.active, d.lifted)
	}
	if got := barIDs(b); !slices.Equal(got, []string{"tab-1", "tab-2", "tab-3"}) {
		t.Fatalf("drawn during a stray drag: %v", got)
	}
	first.DragEnd()

	// Short ones onto a tab that is not shown and onto its ×, pressed just beside them.
	left := absolutePos(third).AddXY(0, third.Size().Height/2)
	strayDrag(third, left.AddXY(-1, 0), left.AddXY(2, 0))
	third.DragEnd()
	x := absolutePos(third.close).AddXY(third.close.Size().Width/2, third.close.Size().Height/2)
	strayDrag(third, x.AddXY(0, -third.Size().Height), x)
	third.DragEnd()
	q.drain()
	for _, id := range []string{"ui.tab.move", "ui.tab.select", "ui.tab.close", "ui.tab.pin"} {
		if n := len(f.callsOf(id)); n != 0 {
			t.Fatalf("stray drags sent %s %d times", id, n)
		}
	}
	settle(w)
	if got := barIDs(b); !slices.Equal(got, []string{"tab-1", "tab-2", "tab-3"}) {
		t.Fatalf("after stray drags: %v", got)
	}

	dragTab(t, first, end, false)
	q.until(t, "the moved order", func() bool { return slices.Equal(stateTabIDs(sh.ui), []string{"tab-2", "tab-3", "tab-1"}) })
}

func TestStrayDragLeavesTheDockAlone(t *testing.T) {
	f := dockTabsFake(t)
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the dock's tabs", func() bool { return len(sh.dock.tabRow.Objects) == 3 && sh.view.snap != nil })
	settle(w)
	d := sh.dock
	audit := d.tabRow.Objects[0].(*tabButton)
	last := d.tabRow.Objects[2].(*tabButton)
	mid := absolutePos(audit).AddXY(audit.Size().Width/2, audit.Size().Height/2)
	to := absolutePos(last).AddXY(last.Size().Width-2, last.Size().Height/2)

	strayDrag(audit, mid.AddXY(0, -150), mid, to)
	if audit.carried() || d.drag.active {
		t.Fatal("a stray drag took the dock's tab")
	}
	audit.DragEnd()
	top := absolutePos(audit).AddXY(audit.Size().Width/2, 0)
	strayDrag(audit, top.AddXY(0, -1), top.AddXY(0, 2)) // short: the click that slipped
	audit.DragEnd()
	q.drain()
	for _, id := range []string{"dock.move", "dock.activate", "dock.close"} {
		if n := len(f.callsOf(id)); n != 0 {
			t.Fatalf("stray drags sent %s %d times", id, n)
		}
	}

	dragTab(t, audit, to, false)
	q.until(t, "dock.move", func() bool { return len(f.callsOf("dock.move")) == 1 })
}

// A drag pressed in the drawer that the pointer carries over the drawer's edge does not move the
// edge, and nothing is stored at its end; one pressed on the edge, sent as the GLFW driver sends
// it, moves it as before.
func TestStrayDragLeavesThePanelEdgeAlone(t *testing.T) {
	sh, w, _, _ := openLongDrawer(t)
	prefs := sh.app.Preferences()
	h := findAll[*resizeHandle](sh.hsplit)[0]
	before, stored := sh.drawer.root.Size().Width, prefs.Float(prefDrawerWidth)
	mid := absolutePos(h).AddXY(h.Size().Width/2, 40)

	strayDrag(h, mid.AddXY(100, 0), mid, mid.AddXY(-60, 0))
	h.DragEnd()
	settle(w)
	if got := sh.drawer.root.Size().Width; got != before {
		t.Fatalf("a stray drag moved the edge: the drawer is %v wide, was %v", got, before)
	}
	if got := prefs.Float(prefDrawerWidth); got != stored || h.dragging {
		t.Fatalf("a stray drag stored %v (was %v), dragging %v", got, stored, h.dragging)
	}

	to := mid.AddXY(-100, 0)
	h.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: to, Position: to.Subtract(absolutePos(h))}, Dragged: fyne.NewDelta(-100, 0)})
	h.DragEnd()
	settle(w)
	if got := sh.drawer.root.Size().Width; got != before+100 {
		t.Fatalf("dragging the edge 100 left: the drawer is %v wide, want %v", got, before+100)
	}
	if got := float32(prefs.Float(prefDrawerWidth)); got != before+100 {
		t.Fatalf("stored %v, want %v", got, before+100)
	}
}

// A drag pressed below the table's header that the pointer carries onto a column's header moves no
// column.
func TestStrayDragLeavesTheColumnsAlone(t *testing.T) {
	f := connectedFake(t, 20)
	handleColumnOrder(f)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "kinds", func() bool { return len(sh.kinds) > 0 && sh.view.snap != nil })
	settle(w)
	tv := sh.view
	before := visibleIDs(tv)
	h := headerFor(t, sh, firstDataColumn+2)
	mid := absolutePos(h).AddXY(h.Size().Width/2, h.Size().Height/2)
	starts, _ := tv.columnStarts()
	gap := fyne.NewPos(absolutePos(tv.table).X+starts[0]+4, mid.Y)

	strayDrag(h, mid.AddXY(0, 120), mid, gap)
	if tv.order.active || tv.order.mark.Visible() || tv.order.chip.Visible() {
		t.Fatalf("a stray drag took the header: active %v line %v chip %v", tv.order.active, tv.order.mark.Visible(), tv.order.chip.Visible())
	}
	h.DragEnd()
	q.drain()
	if n := len(f.callsOf("ui.setColumnOrder")); n != 0 || !slices.Equal(visibleIDs(tv), before) {
		t.Fatalf("a stray drag sent %d orders: %v", n, visibleIDs(tv))
	}
}

// A drag pressed outside a selectable text that the pointer carries over it selects nothing and
// takes no focus.
func TestStrayDragSelectsNothing(t *testing.T) {
	sh, w, _, ref := podDrawerScene(t, ThemeDark)
	l := selLineWith(t, sh.drawer, ref.Name)
	at := absolutePos(l)
	over := at.AddXY(l.Size().Width/2, l.Size().Height/2)

	strayDrag(l, at.AddXY(-30, -40), over, over.AddXY(20, 0))
	l.DragEnd()
	if got := l.SelectedText(); got != "" || l.dragging || w.Canvas().Focused() == l {
		t.Fatalf("a stray drag selected %q, dragging %v, focused %v", got, l.dragging, w.Canvas().Focused() == l)
	}

	// Pressed on the text, it selects.
	strayDrag(l, at.AddXY(1, l.Size().Height/2), over)
	l.DragEnd()
	if l.SelectedText() == "" {
		t.Fatal("a drag pressed on the text selected nothing")
	}
}
