package fyneui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// dragTile drags the hotbar's tile at index from to the window position to in two moves and,
// unless hold, drops it there, as the GLFW driver sends a drag (dragHeader in columnorder_test.go):
// positions from the tile, the pointer's absolute position, the delta of each move. It is pressed
// in its middle.
func dragTile(t *testing.T, h *hotbar, from int, to fyne.Position, hold bool) *hotbarTile {
	t.Helper()
	tl := tilesOf(h)[from]
	tp := absolutePos(tl)
	start := tp.AddXY(tl.Size().Width/2, tl.Size().Height/2)
	half := fyne.NewDelta((to.X-start.X)/2, (to.Y-start.Y)/2)
	for _, p := range []fyne.Position{start.Add(half), to} {
		tl.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: p, Position: p.Subtract(tp)}, Dragged: half})
	}
	if !hold {
		tl.DragEnd()
	}
	return tl
}

// moveTo moves a held drag on to the window position to.
func moveTo(tl *hotbarTile, to fyne.Position) {
	d := tl.h.drag
	tl.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: to}, Dragged: fyne.NewDelta(to.X-d.pointer.X, to.Y-d.pointer.Y)})
}

// centreOf is the middle of the hotbar's tile i, in window coordinates.
func centreOf(h *hotbar, i int) fyne.Position {
	tl := tilesOf(h)[i]
	return absolutePos(tl).AddXY(tl.Size().Width/2, tl.Size().Height/2)
}

func contextsOf(h *hotbar) []string {
	out := []string{}
	for _, t := range tilesOf(h) {
		out = append(out, t.tile.Context)
	}
	return out
}

// tileYs is where each context's tile is drawn, in the bar's tiles.
func tileYs(h *hotbar) map[string]float32 {
	out := map[string]float32{}
	for _, t := range tilesOf(h) {
		out[t.tile.Context] = t.Position().Y
	}
	return out
}

// slotYs are the places of the bar's slots, top to bottom, as they are drawn before a drag.
func slotYs(h *hotbar) []float32 {
	var out []float32
	for _, t := range tilesOf(h) {
		out = append(out, t.Position().Y)
	}
	return out
}

// drawnNear reports whether two drawn values are the same but for float rounding.
func drawnNear(a, b float32) bool { return a-b < 0.01 && b-a < 0.01 }

// wantOrder checks that the tiles are drawn in the slots in this order.
func wantOrder(t *testing.T, h *hotbar, slots []float32, order ...string) {
	t.Helper()
	ys := tileYs(h)
	for i, c := range order {
		if ys[c] != slots[i] {
			t.Errorf("%s drawn at %v, want slot %d at %v (all: %v)", c, ys[c], i+1, slots[i], ys)
		}
	}
}

// The dragged tile is carried: a ghost of it under the pointer, held where it was grabbed, lifted
// (scaled, on a card); its place an empty slot; the other tiles drawn where a drop would put them,
// before anything is sent; the drop sends hotbar.moveCluster and the bar keeps that order until the
// hotbar has it; the ghost lands and goes. A drop on the "+" puts the tile last, and nothing opens.
func TestHotbarDragCarriesTheTile(t *testing.T) {
	sh, w, q, f, _ := hotbarScene(t, ThemeDark)
	settle(w)
	h := sh.hot
	before, slots := contextsOf(h), slotYs(h)

	// kind-cogpit (first) carried onto dev-hfi's place (fifth).
	to, back := centreOf(h, 4).AddXY(3, 5), centreOf(h, 2) // the fifth and the third slot, before they move
	tl := dragTile(t, h, 0, to, true)
	d := h.drag
	g := d.ghost
	if !d.lifted || g == nil || !g.ghost || len(d.float.Objects) != 1 || d.float.Objects[0] != g {
		t.Fatalf("lifted %v, ghost %v, layer %d", d.lifted, g, len(d.float.Objects))
	}
	if g.scale != tileLift || g.Size().Width != tl.Size().Width*tileLift {
		t.Errorf("the ghost at scale %v, %v wide (the tile %v)", g.scale, g.Size().Width, tl.Size().Width)
	}
	grab := fyne.NewPos(tl.Size().Width/2, tl.Size().Height/2)
	if at := absolutePos(g).AddXY(grab.X*tileLift, grab.Y*tileLift); at != to {
		t.Errorf("the ghost's grab point at %v, the pointer at %v", at, to)
	}
	gr := test.WidgetRenderer(g).(*hotbarTileRenderer)
	if !gr.card.Visible() || gr.card.Shadow.BlurRadius == 0 || gr.dot.Visible() || !gr.still.Visible() || !gr.label.Visible() {
		t.Errorf("the ghost: card %v shadow %v dot %v still %v", gr.card.Visible(), gr.card.Shadow.BlurRadius, gr.dot.Visible(), gr.still.Visible())
	}
	r := test.WidgetRenderer(tl).(*hotbarTileRenderer)
	if !tl.carried() || r.bg.FillColor != color.Transparent || r.bg.StrokeWidth == 0 || r.label.Visible() || r.dot.Visible() || r.initials.Visible() {
		t.Errorf("the empty slot: carried %v fill %v label %v dot %v", tl.carried(), r.bg.FillColor, r.label.Visible(), r.dot.Visible())
	}
	if d.to != 4 || !d.inside {
		t.Errorf("the place: %d inside %v", d.to, d.inside)
	}
	wantOrder(t, h, slots, "prod-eu-central", "staging-us-east", "legacy-onprem [.kube/old.yaml]", "dev-hfi", "kind-cogpit", "demo")
	other := tilesOf(h)[5]
	other.MouseIn(&desktop.MouseEvent{PointEvent: fyne.PointEvent{AbsolutePosition: centreOf(h, 5)}})
	if other.hovered || sh.tip.owner == other {
		t.Error("a tile lit up or asked for its tooltip under a dragged one")
	}
	g.MouseIn(&desktop.MouseEvent{})
	g.Tapped(&fyne.PointEvent{})
	q.drain()
	if n := len(f.callsOf("hotbar.moveCluster")) + len(f.callsOf("hotbar.openCluster")); n != 0 {
		t.Fatalf("%d commands sent before the drop", n)
	}

	// Back up onto staging's place (third): the tiles follow.
	moveTo(tl, back)
	if d.to != 2 {
		t.Errorf("the place after moving back: %d", d.to)
	}
	wantOrder(t, h, slots, "prod-eu-central", "staging-us-east", "kind-cogpit", "legacy-onprem [.kube/old.yaml]", "dev-hfi", "demo")

	tl.DragEnd()
	if d.lifted || d.ghost != nil || len(d.float.Objects) != 0 || tl.carried() || !r.dot.Visible() {
		t.Errorf("after the drop: lifted %v ghost %v layer %d", d.lifted, d.ghost, len(d.float.Objects))
	}
	wantOrder(t, h, slots, "prod-eu-central", "staging-us-east", "kind-cogpit") // kept until the hotbar has the move
	q.until(t, "the move", func() bool { return len(f.callsOf("hotbar.moveCluster")) == 1 })
	if p := decodeParams[hotbarMoveParams](t, f.callsOf("hotbar.moveCluster")[0]); p.Context != "kind-cogpit" || p.Position != 3 {
		t.Fatalf("hotbar.moveCluster %+v", p)
	}
	want := []string{before[1], before[2], before[0], before[3], before[4], before[5]}
	q.until(t, "the new order", func() bool { return slices.Equal(contextsOf(h), want) && !d.pending })
	settle(w)
	wantOrder(t, h, slots, want...)

	// demo (last) up onto the first place, then the first onto the "+": last again.
	dragTile(t, h, 5, centreOf(h, 0), false)
	q.until(t, "demo first", func() bool { return contextsOf(h)[0] == "demo" })
	settle(w)
	dragTile(t, h, 0, middleOf(h.add), false)
	q.until(t, "demo last", func() bool { return contextsOf(h)[5] == "demo" })
	if p := decodeParams[hotbarMoveParams](t, f.callsOf("hotbar.moveCluster")[2]); p.Position != 6 {
		t.Errorf("dropped on the +: %+v", p)
	}
	q.drain()
	if n := len(f.callsOf("hotbar.openCluster")); n != 0 {
		t.Errorf("a drag opened a cluster %d times", n)
	}
}

// The tiles slide to their new places and the ghost glides home, each tileSlide long: drawn
// between the old place and the new one mid-way; with Reduce motion nothing animates and
// everything jumps.
func TestHotbarDragSlides(t *testing.T) {
	sh, w, q, _, _ := hotbarScene(t, ThemeDark)
	settle(w)
	h := sh.hot
	slots := slotYs(h)
	type run struct {
		d    time.Duration
		tick func(float32)
	}
	var runs []run
	h.drag.animate = func(d time.Duration, tick func(float32)) func() {
		runs = append(runs, run{d, tick})
		return func() {}
	}
	h.drag.reduce = func() bool { return false }

	tl := tilesOf(h)[0] // kind-cogpit onto staging's place, in one move
	start, to := centreOf(h, 0), centreOf(h, 2)
	tl.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: to}, Dragged: fyne.NewDelta(to.X-start.X, to.Y-start.Y)})
	if len(runs) != 2 || runs[0].d != tileLiftIn || runs[1].d != tileSlide {
		t.Fatalf("animations %v", runs)
	}
	runs[0].tick(1) // the lift
	if g := h.drag.ghost; g.scale != tileLift {
		t.Errorf("the ghost at %v after the lift", g.scale)
	}
	slide := runs[1].tick
	ys := tileYs(h)
	if ys["prod-eu-central"] != slots[1] || ys["staging-us-east"] != slots[2] || ys["kind-cogpit"] != slots[0] {
		t.Errorf("before the first tick, drawn where they were: %v", ys)
	}
	slide(0.5)
	ys = tileYs(h)
	if mid := (slots[0] + slots[1]) / 2; !drawnNear(ys["prod-eu-central"], mid) || !drawnNear(ys["kind-cogpit"], (slots[0]+slots[2])/2) {
		t.Errorf("half-way: prod at %v, want %v; kind-cogpit at %v", ys["prod-eu-central"], mid, ys["kind-cogpit"])
	}
	slide(1)
	wantOrder(t, h, slots, "prod-eu-central", "staging-us-east", "kind-cogpit", "legacy-onprem [.kube/old.yaml]")

	g := h.drag.ghost
	from := g.Position()
	tl.DragEnd()
	if len(runs) != 3 || runs[2].d != tileSlide || h.drag.ghost != g {
		t.Fatalf("the glide: %d animations, ghost %v", len(runs), h.drag.ghost)
	}
	home := absolutePos(tl).Subtract(absolutePos(h.drag.float))
	runs[2].tick(0.5)
	if at := g.Position(); !drawnNear(at.X, (from.X+home.X)/2) || !drawnNear(at.Y, (from.Y+home.Y)/2) || !drawnNear(g.scale, (tileLift+1)/2) {
		t.Errorf("half-way home: %v (from %v to %v), scale %v", at, from, home, g.scale)
	}
	runs[2].tick(1)
	if h.drag.ghost != nil || len(h.drag.float.Objects) != 0 || tl.carried() {
		t.Error("the ghost stays after its glide")
	}
	q.until(t, "the move", func() bool { return contextsOf(h)[2] == "kind-cogpit" })

	// Reduce motion: no animation; the tiles and the ghost jump.
	settle(w)
	runs = nil
	h.drag.reduce = func() bool { return true }
	tl = dragTile(t, h, 0, centreOf(h, 3), true)
	if len(runs) != 0 || h.drag.ghost.scale != tileLift {
		t.Errorf("with Reduce motion: %d animations, the ghost at %v", len(runs), h.drag.ghost.scale)
	}
	wantOrder(t, h, slots, "staging-us-east", "kind-cogpit", "legacy-onprem [.kube/old.yaml]", "prod-eu-central")
	tl.DragEnd()
	if len(runs) != 0 || h.drag.ghost != nil {
		t.Errorf("the drop with Reduce motion: %d animations, ghost %v", len(runs), h.drag.ghost)
	}
}

// A drop on the tile's own place, outside the bar (in the table, below it in the status bar) or
// after the bar changed under it moves nothing: the tiles are back in their places before the drop
// and the ghost lands in the tile's own; up to half a tile right of the bar still counts as the bar.
// A failed move puts the tiles back. A drag shorter than minTileDrag opens the cluster, as the click
// it was meant to be.
func TestHotbarTileDragCancels(t *testing.T) {
	sh, w, q, f, fh := hotbarScene(t, ThemeDark)
	settle(w)
	h := sh.hot
	slots := slotYs(h)
	before := contextsOf(h)

	// Its own place.
	tl := dragTile(t, h, 1, centreOf(h, 1).AddXY(0, 10), true)
	if !h.drag.lifted || h.drag.to != 1 || !h.drag.inside {
		t.Errorf("own place: lifted %v to %d", h.drag.lifted, h.drag.to)
	}
	tl.DragEnd()

	// Outside the bar: right of it in the table. Half a tile right of it is the bar still.
	bar := absolutePos(h.scroll)
	tl = dragTile(t, h, 1, fyne.NewPos(bar.X+h.scroll.Size().Width+5, centreOf(h, 4).Y), true)
	if !h.drag.inside || h.drag.to != 4 {
		t.Errorf("just right of the bar: inside %v to %d", h.drag.inside, h.drag.to)
	}
	moveTo(tl, centreOf(h, 4).AddXY(h.root.Size().Width+200, 0))
	if h.drag.inside || h.drag.to != 1 {
		t.Errorf("outside the bar: inside %v to %d", h.drag.inside, h.drag.to)
	}
	wantOrder(t, h, slots, before...)
	tl.DragEnd()
	if h.drag.ghost != nil {
		t.Error("the ghost stays after a drop outside")
	}
	dragTile(t, h, 1, fyne.NewPos(bar.X+4, bar.Y+h.scroll.Size().Height+20), false) // the status bar

	// The bar changed under the drag (an agent moved a tile): the drop moves nothing.
	tl = dragTile(t, h, 0, centreOf(h, 4), true)
	fh.setHotbar("demo", "kind-cogpit", "prod-eu-central", "staging-us-east", "legacy-onprem [.kube/old.yaml]", "dev-hfi")
	q.until(t, "the agent's order", func() bool { return contextsOf(h)[0] == "demo" })
	if !h.drag.stale || h.drag.inside {
		t.Errorf("stale %v inside %v", h.drag.stale, h.drag.inside)
	}
	wantOrder(t, h, slots, "demo", "kind-cogpit", "prod-eu-central")
	tl.DragEnd()
	q.drain()
	if n := len(f.callsOf("hotbar.moveCluster")); n != 0 {
		t.Fatalf("%d moves sent", n)
	}

	// A move that fails: the tiles go back to the bar's order, and the reason shows.
	f.mu.Lock() // the shell's own dispatches read the handlers meanwhile (CI's race job, 2026-10-17)
	f.handlers["hotbar.moveCluster"] = func(context.Context, engine.Invocation) (any, error) {
		return nil, errors.New("the hotbar's file cannot be written")
	}
	f.mu.Unlock()
	settle(w)
	dragTile(t, h, 0, centreOf(h, 3), false)
	q.until(t, "the failed move", func() bool { return len(f.callsOf("hotbar.moveCluster")) == 1 && !h.drag.pending })
	settle(w)
	wantOrder(t, h, slots, "demo", "kind-cogpit", "prod-eu-central", "staging-us-east")

	// A twitch: the click that slipped opens the tile's cluster (demo, the first).
	tl = tilesOf(h)[0]
	tl.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(20, 20)}, Dragged: fyne.NewDelta(3, 0)})
	if tl.carried() || h.drag.ghost != nil {
		t.Error("lifted after three points")
	}
	tl.DragEnd()
	q.until(t, "the slipped click's connect", func() bool { return len(f.callsOf("hotbar.openCluster")) == 1 })
	if p := decodeParams[hotbarSlotParams](t, f.callsOf("hotbar.openCluster")[0]); p.Slot != 1 {
		t.Errorf("hotbar.openCluster %+v", p)
	}
}

// Fyne's test driver finds the tile under the pointer as the drag's target (not the bar's scroll
// or the drag's layer) and the drop moves it: test.Drag through the window's canvas.
func TestHotbarTileDragThroughTheCanvas(t *testing.T) {
	sh, w, q, f, _ := hotbarScene(t, ThemeDark)
	settle(w)
	h := sh.hot
	start := centreOf(h, 4) // dev-hfi
	test.Drag(w.Canvas(), start, 0, centreOf(h, 1).Y-start.Y)
	q.until(t, "the move", func() bool { return len(f.callsOf("hotbar.moveCluster")) == 1 })
	if p := decodeParams[hotbarMoveParams](t, f.callsOf("hotbar.moveCluster")[0]); p.Context != "dev-hfi" || p.Position != 2 {
		t.Errorf("hotbar.moveCluster %+v", p)
	}
	q.until(t, "dev-hfi second", func() bool { return contextsOf(h)[1] == "dev-hfi" })
}

// A bar whose tiles do not fit scrolls by itself while a carried tile rests near its bottom edge,
// and the place follows; in the middle it stops; the drop lands where the scrolled bar shows it.
func TestHotbarDragScrollsAtTheEdge(t *testing.T) {
	f := connectedFake(t, 10)
	var ids []string
	for i := range 20 {
		ids = append(ids, fmt.Sprintf("c%02d", i+1))
		f.contexts = append(f.contexts, engine.ContextInfo{Name: ids[i]})
	}
	withHotbar(f, ids...)
	sh, w, q := newTestShell(t, f, Options{})
	q.until(t, "twenty tiles", func() bool { return len(tilesOf(sh.hot)) == 20 && len(sh.kinds) > 0 })
	settle(w)
	h := sh.hot
	s := h.scroll
	x := centreOf(h, 0).X
	middle, bottom := absolutePos(s).Y+s.Size().Height/2, absolutePos(s).Y+s.Size().Height
	tl := dragTile(t, h, 0, fyne.NewPos(x, middle), true)
	unscrolled := h.drag.to // the place in the middle before the bar scrolled
	if h.drag.edge != 0 || h.drag.stopScroll != nil {
		t.Fatalf("scrolling in the middle: %v", h.drag.edge)
	}
	moveTo(tl, fyne.NewPos(x, bottom-4))
	first := h.drag.to
	if h.drag.edge <= 0 || h.drag.stopScroll == nil {
		t.Fatalf("no scrolling at the bottom edge: %v", h.drag.edge)
	}
	q.until(t, "the bar scrolled", func() bool { return s.Offset.Y > 3*tl.Size().Height && h.drag.to > first })
	moveTo(tl, fyne.NewPos(x, middle))
	if h.drag.edge != 0 || h.drag.stopScroll != nil {
		t.Errorf("still scrolling in the middle: %v", h.drag.edge)
	}
	to := h.drag.to
	tl.DragEnd()
	q.until(t, "the move", func() bool { return len(f.callsOf("hotbar.moveCluster")) == 1 })
	if p := decodeParams[hotbarMoveParams](t, f.callsOf("hotbar.moveCluster")[0]); p.Context != "c01" || p.Position != to+1 || to < unscrolled+3 {
		t.Errorf("hotbar.moveCluster %+v, the place %d (in the middle before scrolling %d)", p, to, unscrolled)
	}
}

func TestRenderHotbarDragDark(t *testing.T) { renderHotbarDrag(t, ThemeDark, "hotbar_drag_dark.png") }

// renderHotbarDrag: prod-eu-central (second) carried down onto dev-hfi's place (fifth): the ghost
// under the pointer on its card, its empty slot fifth, the tiles between moved up one place.
func renderHotbarDrag(t *testing.T, mode ThemeMode, name string) {
	sh, w, q, _, _ := hotbarScene(t, mode)
	q.until(t, "diagnostics", func() bool { return sh.diag.total > 0 })
	settle(w)
	tl := dragTile(t, sh.hot, 1, centreOf(sh.hot, 4).AddXY(30, 16), true)
	if sh.hot.drag.ghost == nil || sh.hot.drag.to != 4 {
		t.Fatal("no tile carried")
	}
	savePNG(t, w, name)
	tl.DragEnd()
}
