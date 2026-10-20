package fyneui

import (
	"math"
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Reordering the hotbar's tiles by dragging them (R2, with the look of D-83: the owner's WF6 of
// 2026-10-17, decisions.md; ui-parity › Hotbar). The move is hotbar.moveCluster, the command the
// tile's Move Up and Move Down send, so agents see the new order in UIState.Hotbar as the bar does
// (UI state is engine state); nothing reaches the engine before the drop.
//
// The drag carries the tile: the owner asked that the cluster is taken along and the other tiles
// arrange themselves around it while the button is held. How it looks and what it does are the
// lead's decisions of 2026-10-17 evening, open to the owner's revision (D-83):
//   - Once the pointer moved minTileDrag the tile is lifted: a copy of it (the ghost) follows the
//     pointer in a layer over the whole window (shell.go), held where it was grabbed, scaled up to
//     tileLift on a card with a drop shadow; where the tile was, its empty slot (an outline) stays.
//   - The slot travels with the pointer: the place a drop would give the tile is the slot nearest
//     to the ghost's top, counted by the tiles' fixed pitch (so a tile sliding away never flips the
//     choice back), and the other tiles slide into the places they then take, tileSlide long, eased
//     out, continuously while the drag goes on.
//   - The drop on a new place sends hotbar.moveCluster, and the ghost glides into the slot and is
//     the tile there again; the bar keeps that order until the hotbar it shows has the move. A drop
//     outside the bar (above or below its tiles' area, or further right than half a tile beyond its
//     edge), on the tile's own place, or after the bar changed under the drag (an agent moved, added
//     or removed a tile) moves nothing: the tiles have slid back already, and the ghost glides back
//     into the tile's place. R3, a tile dropped outside the window opening another window with its
//     cluster, would start in ended; it waits for the owner's decision on several windows (D-52).
//   - Near the top or the bottom edge of a bar whose tiles do not fit, the bar scrolls by itself,
//     the faster the closer the pointer is to the edge.
//   - Reduce motion (Cogpit's preference or macOS's, shell.reduceMotion, read at the lift): no slide, no glide and no
//     lift; the tiles and the ghost jump to their places.
//   - A drag shorter than minTileDrag is the click that slipped: it opens the cluster, as the click
//     would have (Fyne taps only what was not dragged, and its driver counts two points as a drag).
//   - While a tile is dragged no tile lights up or shows its tooltip, and the ghost takes the
//     pointer's hover, so nothing under it does either.

const (
	// minTileDrag is how far a tile must move before it is dragged: less is a click that slipped.
	minTileDrag = minColumnDrag
	// tileLift is the ghost's size against the tile's.
	tileLift = float32(1.08)
	// tileSlide is how long a tile takes to slide to its new place, and the ghost to glide home.
	tileSlide = 150 * time.Millisecond
	// tileLiftIn is how long the ghost takes to grow to tileLift.
	tileLiftIn = 90 * time.Millisecond
	// tileScrollTick and tileScrollStep are the bar's own scrolling near an edge: at most this many
	// points a tick, at the very edge (about 870 points a second).
	tileScrollTick = 16 * time.Millisecond
	tileScrollStep = float32(14)
)

// tileDrag is a tile being dragged to another place on the hotbar: the ghost under the pointer,
// the order the tiles show, and the animations that take them there.
type tileDrag struct {
	h *hotbar

	active  bool // a tile is pressed and dragged, maybe not past minTileDrag yet
	lifted  bool // past minTileDrag: the ghost follows the pointer
	landing bool // dropped: the ghost glides into the tile's slot
	pending bool // dropped on a new place: the bar keeps that order until the hotbar it shows has the move
	acked   bool // hotbar.moveCluster answered: the next hotbar shown ends pending
	stale   bool // the bar changed under the drag: the tiles show it as it is, and the drop moves nothing

	context string   // the dragged tile's context
	order   []string // the bar's contexts when the tile was lifted
	from    int      // the tile's index then
	to      int      // the index a drop now gives it: from while the pointer is outside the bar
	inside  bool     // the pointer is over the bar: a drop there moves

	moved   float32       // how far the pointer moved since the press
	pointer fyne.Position // where it is, in window coordinates
	grab    fyne.Position // where in the tile it was pressed
	base    fyne.Size     // the tile's size when it was lifted

	float *fyne.Container // a layer over the whole window, without a layout (shell.go): the ghost
	ghost *hotbarTile

	reduce  func() bool // Reduce motion (shell.reduceMotion)
	still   bool        // Reduce motion at the lift: the tiles and the ghost jump
	animate func(d time.Duration, tick func(float32)) (stop func())

	stopSlide, stopGlide, stopLift func()

	edge       float32       // the bar's own scrolling: points a tick, up negative; 0 none
	stopScroll chan struct{} // the scrolling ticker's; nil while none runs
}

func newTileDrag(h *hotbar) *tileDrag {
	return &tileDrag{h: h, float: container.NewWithoutLayout(), reduce: h.sh.reduceMotion, animate: runAnimation, from: -1, to: -1}
}

// runAnimation runs tick over d with Fyne's animation, eased out, and returns how to stop it.
// Fyne's test driver ticks it to its end at once.
func runAnimation(d time.Duration, tick func(float32)) func() {
	a := fyne.NewAnimation(d, tick)
	a.Curve = fyne.AnimationEaseOut
	a.Start()
	return a.Stop
}

// halt stops an animation, if one runs.
func halt(stop *func()) {
	if s := *stop; s != nil {
		*stop = nil
		s()
	}
}

var _ fyne.Draggable = (*hotbarTile)(nil)

// Dragged implements fyne.Draggable: the tile is carried to another place on the bar.
func (t *hotbarTile) Dragged(ev *fyne.DragEvent) {
	if !t.ghost {
		t.h.drag.dragged(t, ev)
	}
}

// DragEnd implements fyne.Draggable.
func (t *hotbarTile) DragEnd() {
	if !t.ghost {
		t.h.drag.ended(t)
	}
}

// carried reports whether the tile is the one being carried, or landing: it is drawn as its empty
// slot.
func (t *hotbarTile) carried() bool {
	d := t.h.drag
	return !t.ghost && d != nil && (d.lifted || d.landing) && d.context == t.tile.Context
}

// dragged follows a tile's drag: lifted past minTileDrag, the ghost under the pointer, the place a
// drop would give it, the bar's own scrolling.
func (d *tileDrag) dragged(t *hotbarTile, ev *fyne.DragEvent) {
	if !d.active {
		if d.h.view == nil {
			return
		}
		d.settleGhost() // a ghost still gliding lands at once
		if d.pending {  // and the order of the last drop gives way to the bar's
			d.pending = false
			d.h.relayout()
		}
		at := absolutePos(t)
		d.active, d.lifted, d.stale, d.moved, d.context = true, false, false, 0, t.tile.Context
		d.pointer = ev.AbsolutePosition.Subtract(ev.Dragged) // where the press was
		if ev.AbsolutePosition.IsZero() {                    // Fyne's test driver: test.Drag sends the press in the tile
			d.pointer = at.Add(ev.Position)
		}
		d.grab = d.pointer.Subtract(at)
	}
	if ev.AbsolutePosition.IsZero() {
		d.pointer = d.pointer.Add(ev.Dragged)
	} else {
		d.pointer = ev.AbsolutePosition
	}
	d.moved += max(ev.Dragged.DX, -ev.Dragged.DX) + max(ev.Dragged.DY, -ev.Dragged.DY)
	if d.moved < minTileDrag {
		return
	}
	if !d.lifted {
		d.lift(t)
	}
	d.place()
	d.retarget()
	d.scrollNearEdge()
}

// lift takes the tile out of its place: the ghost appears under the pointer and grows to tileLift,
// the tile's place shows its empty slot, no tooltip shows.
func (d *tileDrag) lift(t *hotbarTile) {
	h := d.h
	d.lifted, d.still, d.inside = true, d.reduce(), true
	d.order = viewContexts(h.view)
	d.from = slices.Index(d.order, d.context)
	d.to = d.from
	d.stale = d.from < 0
	d.base = t.Size()
	h.sh.tip.hide()
	t.hovered = false
	g := newHotbarTile(h, t.tile, t.slot)
	g.ghost, g.scale = true, 1
	d.ghost = g
	d.float.Objects = []fyne.CanvasObject{g}
	d.place()
	d.float.Refresh()
	t.Refresh() // its empty slot
	if d.still {
		d.zoomGhost(tileLift)
		return
	}
	d.stopLift = d.animate(tileLiftIn, func(f float32) { d.zoomGhost(1 + (tileLift-1)*f) })
}

// zoomGhost scales the ghost about the point it was grabbed at.
func (d *tileDrag) zoomGhost(z float32) {
	if g := d.ghost; g != nil && d.lifted {
		g.scale = z
		d.place()
		g.Refresh()
	}
}

// place puts the ghost under the pointer, held where it was grabbed.
func (d *tileDrag) place() {
	g := d.ghost
	if g == nil || !d.lifted {
		return
	}
	z := g.zoom()
	g.Resize(fyne.NewSize(d.base.Width*z, d.base.Height*z))
	g.Move(d.pointer.Subtract(absolutePos(d.float)).SubtractXY(d.grab.X*z, d.grab.Y*z))
}

// retarget picks the place a drop would give the tile now and slides the tiles there when it
// changed.
func (d *tileDrag) retarget() {
	to, inside := d.target()
	d.inside = inside
	if to != d.to {
		d.rearrange(func() { d.to = to })
	}
}

// target returns the index a drop at the pointer gives the tile: the slot nearest to the ghost's
// top, by the tiles' pitch, the last for a drop below them (on the "+" or the empty bar under it);
// the tile's own index and false outside the bar or once the bar changed under the drag.
func (d *tileDrag) target() (int, bool) {
	n := len(d.order)
	if d.stale || n == 0 || !d.over(d.pointer) {
		return d.from, false
	}
	pitch := d.base.Height + theme.Padding()
	top := d.pointer.Y - d.grab.Y - absolutePos(d.h.tiles).Y
	i := int(math.Round(float64(top / pitch)))
	return max(0, min(i, n-1)), true
}

// over reports whether p is over the bar: its tiles' area, and up to half a tile to its right.
func (d *tileDrag) over(p fyne.Position) bool {
	s := d.h.scroll
	at, size := absolutePos(s), s.Size()
	margin := d.h.sh.app.Settings().Theme().Size(sizeHotbarTile) / 2
	return p.X >= at.X && p.X < at.X+size.Width+margin && p.Y >= at.Y && p.Y < at.Y+size.Height
}

// rearrange changes where the tiles go (change) and slides each from where it is drawn to its new
// place; with Reduce motion they jump.
func (d *tileDrag) rearrange(change func()) {
	h := d.h
	tiles := h.tileWidgets()
	was := make([]float32, len(tiles))
	for i, t := range tiles {
		was[i] = t.Position().Y
	}
	halt(&d.stopSlide)
	change()
	for _, t := range tiles {
		t.dy = 0
	}
	h.relayout()
	if d.still {
		return
	}
	off := make([]float32, len(tiles))
	moving := false
	for i, t := range tiles {
		off[i] = was[i] - t.Position().Y
		t.dy = off[i]
		moving = moving || off[i] != 0
	}
	if !moving {
		return
	}
	h.relayout() // where they are drawn until the first tick: where they were
	d.stopSlide = d.animate(tileSlide, func(f float32) {
		for i, t := range tiles {
			t.dy = off[i] * (1 - f)
		}
		h.relayout()
	})
}

// arranged is the order the tiles are laid out in (tileStack): the bar's, or while a tile is
// carried, landing or its move pending, the order a drop on the chosen place gives them.
func (d *tileDrag) arranged(objs []fyne.CanvasObject) []fyne.CanvasObject {
	n := len(d.order)
	held := d.lifted || d.landing || d.pending
	if !held || d.stale || d.to == d.from || d.from < 0 || d.to < 0 || d.from >= n || d.to >= n || len(objs) < n {
		return objs
	}
	for i, c := range d.order {
		if t, ok := objs[i].(*hotbarTile); !ok || t.tile.Context != c {
			return objs // the bar shows another order already: the move landed
		}
	}
	out := slices.Clone(objs)
	o := out[d.from]
	out = slices.Delete(out, d.from, d.from+1)
	return slices.Insert(out, d.to, o)
}

// follow takes in a new hotbar while a tile is carried or its move pending (setView): the move
// shown ends the pending order; any other change under a carried tile makes the drag stale, so
// that its drop moves nothing.
func (d *tileDrag) follow() {
	now := viewContexts(d.h.view)
	if d.pending && (d.acked || !slices.Equal(now, d.order)) {
		d.pending = false
	}
	if d.lifted && !d.stale && !slices.Equal(now, d.order) {
		d.stale, d.inside, d.to = true, false, d.from
		halt(&d.stopSlide)
		for _, t := range d.h.tileWidgets() {
			t.dy = 0
		}
	}
}

// ended drops the carried tile: on a new place through hotbar.moveCluster, anywhere else nowhere;
// the ghost lands in the tile's slot. A drag that never lifted is the click that slipped.
func (d *tileDrag) ended(t *hotbarTile) {
	if !d.active {
		return
	}
	h := d.h
	d.active = false
	d.scrollAt(0)
	if !d.lifted {
		ctx := d.context
		d.context = ""
		if h.view != nil {
			if slot := h.view.Position(ctx); slot > 0 {
				h.sh.tip.hideFor(t)
				h.open(slot)
			}
		}
		return
	}
	if d.inside && !d.stale && d.to != d.from {
		d.pending, d.acked = true, false
		h.sh.dispatch("hotbar.moveCluster", hotbarMoveParams{Context: d.context, Position: d.to + 1}, func(_ any, err error) {
			if !d.pending {
				return
			}
			if err != nil {
				h.sh.showError(err)
				d.rearrange(func() { d.pending = false }) // the tiles slide back to the bar's order
				return
			}
			d.acked = true
		})
	} else if d.to != d.from {
		d.rearrange(func() { d.to = d.from })
	}
	d.land()
}

// land ends a carry: the ghost glides into the tile's slot, its new place or its old one, shrinking
// back to the tile's size, and the tile is drawn there again once it arrives.
func (d *tileDrag) land() {
	d.lifted, d.landing = false, true
	halt(&d.stopLift)
	halt(&d.stopGlide)
	g, slot := d.ghost, d.slotTile()
	if g == nil || slot == nil || d.still {
		d.settleGhost()
		return
	}
	from, z := g.Position(), g.zoom()
	to := absolutePos(slot).Subtract(absolutePos(d.float)).SubtractXY(0, slot.dy)
	d.stopGlide = d.animate(tileSlide, func(f float32) {
		if d.ghost != g {
			return
		}
		g.scale = z + (1-z)*f
		g.Resize(fyne.NewSize(d.base.Width*g.scale, d.base.Height*g.scale))
		g.Move(fyne.NewPos(from.X+(to.X-from.X)*f, from.Y+(to.Y-from.Y)*f))
		g.Refresh()
		if f >= 1 {
			d.settleGhost()
		}
	})
}

// slotTile is the carried tile's widget on the bar; nil once it left the bar.
func (d *tileDrag) slotTile() *hotbarTile {
	for _, t := range d.h.tileWidgets() {
		if t.tile.Context == d.context {
			return t
		}
	}
	return nil
}

// settleGhost ends a landing: the ghost goes, and the tile is drawn in its place again.
func (d *tileDrag) settleGhost() {
	halt(&d.stopGlide)
	if !d.landing && d.ghost == nil {
		return
	}
	slot := d.slotTile()
	d.landing, d.ghost, d.context = false, nil, ""
	d.float.Objects = nil
	d.float.Refresh()
	if slot != nil {
		slot.Refresh()
	}
}

// cancel ends whatever drag there is at once: the bar went (setView nil).
func (d *tileDrag) cancel() {
	d.scrollAt(0)
	halt(&d.stopSlide)
	halt(&d.stopLift)
	d.active, d.lifted, d.pending, d.stale = false, false, false, false
	d.settleGhost()
	d.order, d.from, d.to = nil, -1, -1
	for _, t := range d.h.tileWidgets() {
		t.dy = 0
	}
}

// scrollNearEdge scrolls the bar while the pointer rests near its top or bottom edge and its tiles
// do not fit: up to tileScrollStep points a tick, more the closer the pointer is to the edge.
func (d *tileDrag) scrollNearEdge() {
	s := d.h.scroll
	if !d.lifted || !d.over(d.pointer) || s.Content.MinSize().Height <= s.Size().Height {
		d.scrollAt(0)
		return
	}
	zone := d.base.Height / 2
	top := absolutePos(s).Y
	bottom := top + s.Size().Height
	switch {
	case d.pointer.Y < top+zone:
		d.scrollAt(-tileScrollStep * edgeDepth(top+zone-d.pointer.Y, zone))
	case d.pointer.Y > bottom-zone:
		d.scrollAt(tileScrollStep * edgeDepth(d.pointer.Y-(bottom-zone), zone))
	default:
		d.scrollAt(0)
	}
}

// edgeDepth is how far into the edge's zone the pointer is, from a quarter (its inner border) to 1.
func edgeDepth(in, zone float32) float32 { return max(0.25, min(1, in/zone)) }

// scrollAt sets the bar's own scrolling, starting its ticker for a step and stopping it for 0. The
// ticker posts scrollStep to the main goroutine each tick.
func (d *tileDrag) scrollAt(step float32) {
	d.edge = step
	switch {
	case step != 0 && d.stopScroll == nil:
		stop := make(chan struct{})
		d.stopScroll = stop
		sh := d.h.sh
		go func() {
			defer sh.recoverTo("the hotbar's scrolling")
			tick := time.NewTicker(tileScrollTick)
			defer tick.Stop()
			for {
				select {
				case <-stop:
					return
				case <-sh.ctx.Done():
					return
				case <-tick.C:
					sh.do(d.scrollStep)
				}
			}
		}()
	case step == 0 && d.stopScroll != nil:
		close(d.stopScroll)
		d.stopScroll = nil
	}
}

// scrollStep scrolls the bar one tick and picks the place under the pointer again. Main goroutine.
func (d *tileDrag) scrollStep() {
	s := d.h.scroll
	if !d.lifted || d.edge == 0 {
		return
	}
	limit := max(0, s.Content.MinSize().Height-s.Size().Height)
	y := max(0, min(limit, s.Offset.Y+d.edge))
	if y == s.Offset.Y {
		return
	}
	s.ScrollToOffset(fyne.NewPos(s.Offset.X, y))
	d.retarget()
}

// tileStack lays the hotbar's tiles and its "+" out top to bottom with the theme's padding between,
// as a VBox does; while a tile is carried in the order a drop would give them (tileDrag.arranged),
// each tile drawn dy below its place while it slides there.
type tileStack struct{ h *hotbar }

func (l *tileStack) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	n := 0
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		ms := o.MinSize()
		size.Width = max(size.Width, ms.Width)
		size.Height += ms.Height
		n++
	}
	if n > 1 {
		size.Height += theme.Padding() * float32(n-1)
	}
	return size
}

func (l *tileStack) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	pad := theme.Padding()
	var y float32
	for _, o := range l.h.drag.arranged(objs) {
		if !o.Visible() {
			continue
		}
		ms := o.MinSize()
		o.Resize(fyne.NewSize(size.Width, ms.Height))
		var dy float32
		if t, ok := o.(*hotbarTile); ok {
			dy = t.dy
		}
		o.Move(fyne.NewPos(0, y+dy))
		y += ms.Height + pad
	}
}

// relayout places the tiles again, as they slide.
func (h *hotbar) relayout() { h.tiles.Layout.Layout(h.tiles.Objects, h.tiles.Size()) }

// tileWidgets returns the hotbar's tiles in the bar's order, top to bottom.
func (h *hotbar) tileWidgets() []*hotbarTile {
	out := make([]*hotbarTile, 0, len(h.tiles.Objects))
	for _, o := range h.tiles.Objects {
		if t, ok := o.(*hotbarTile); ok {
			out = append(out, t)
		}
	}
	return out
}

// viewContexts is a hotbar's contexts, top to bottom; none for nil.
func viewContexts(v *engine.HotbarState) []string {
	if v == nil {
		return nil
	}
	out := make([]string, 0, len(v.Clusters))
	for _, t := range v.Clusters {
		out = append(out, t.Context)
	}
	return out
}

// absolutePos is where o is in its window.
func absolutePos(o fyne.CanvasObject) fyne.Position {
	return fyne.CurrentApp().Driver().AbsolutePositionForObject(o)
}
