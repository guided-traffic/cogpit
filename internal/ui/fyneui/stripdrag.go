package fyneui

import (
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

// Moving a tab by dragging it (the owner's gate walk, 2026-10-20, GW4: "Man muss Tab beliebig
// anordnen können"), in the tab bar (tabbar.go) and in the dock's strip (dock.go) alike. The move
// is a command, ui.tab.move or dock.move, so that agents see the new order in UIState.Tabs or
// UIState.Dock as the bar does (UI state is engine state); nothing reaches the engine before the
// drop. The hotbar's tile drag (D-83, hotbardrag.go) is the model, turned sideways; the lead's
// decisions of 2026-10-20, open to revision (D-106):
//   - Once the pointer moved minTileDrag the tab is lifted: it follows the pointer along the strip,
//     held where it was grabbed, drawn raised as the active tab is, with a drop shadow, over the
//     others; it stays in the strip's line (a tab bar is one row: no card over the window as the
//     hotbar's ghost).
//   - The others slide aside, tileSlide long and eased out, to leave the gap a drop fills: the slot
//     whose left edge is nearest the carried tab's, counted over the other tabs' own widths (tabs
//     differ in width, tiles do not), so that a tab sliding away never flips the choice back.
//   - The drop on a new place sends the command, and the tab glides into its slot; the strip keeps
//     that order until the state it shows has the move. A drop far above or below the strip (more
//     than its height away), on the tab's own place, or after the tabs changed under the drag (an
//     agent moved, opened or closed one) moves nothing: the tabs slide back.
//   - Reduce motion (macOS, read at the lift): no slide and no glide; the tabs jump.
//   - A drag shorter than minTileDrag is the click that slipped: it does what the click would have,
//     shows the tab or, pressed on its ×, closes it, since Fyne taps only what was not dragged (and
//     starts a drag of the tab after 2 points, the × being no draggable of its own).
//   - The strip does not scroll by itself while a tab is carried near its edge (the hotbar's does):
//     a tab bar wider than the window scrolls with the wheel or the trackpad, and View › Move Tab
//     Left and Right (⌘⌥⇧← and ⌘⌥⇧→) move the active tab past its edge.

// liftShadow is a carried tab's drop shadow, the hotbar ghost's, smaller for a tab's height.
func liftShadow(th fyne.Theme, carried bool) canvas.Shadow {
	if !carried {
		return canvas.Shadow{}
	}
	v := fyne.CurrentApp().Settings().ThemeVariant()
	return canvas.Shadow{Color: th.Color(theme.ColorNameShadow, v), BlurRadius: 8, Offset: fyne.NewPos(0, 2), Variant: canvas.DropShadow}
}

// stripDrag carries a tab of a horizontal strip to another place.
type stripDrag struct {
	row *fyne.Container // the strip's tabs, laid out by stripLayout

	idOf     func(fyne.CanvasObject) string              // a tab widget's id
	shownIDs func() []string                             // the ids the strip shows, in the state's order
	move     func(id string, index int, failed func())   // sends the move to a 1-based place; failed undoes the shown order
	click    func(o fyne.CanvasObject, at fyne.Position) // the click that slipped, at where it was pressed in the tab
	lifting  func()                                      // a tab was lifted: tooltips go
	reduce   func() bool
	animate  func(d time.Duration, tick func(float32)) (stop func())

	active  bool // a tab is pressed and dragged, maybe not past minTileDrag yet
	lifted  bool // past minTileDrag: the tab follows the pointer
	landing bool // dropped: the tab glides into its slot
	pending bool // dropped on a new place: the strip keeps that order until the state has the move
	stale   bool // the tabs changed under the drag: the drop moves nothing
	still   bool // Reduce motion at the lift

	id     string   // the carried tab
	order  []string // the strip's ids when it was lifted
	from   int      // its place then (0-based)
	to     int      // the place a drop now gives it: from while the pointer is away from the strip
	inside bool     // the pointer is near the strip: a drop there moves

	moved   float32
	pointer fyne.Position // where the pointer is, in window coordinates
	grab    fyne.Position // where in the tab it was pressed
	x       float32       // the carried tab's left edge in the row while lifted

	dx                   map[string]float32 // per tab: how far right of its slot it is drawn while it slides or lands
	stopSlide, stopGlide func()
}

func newStripDrag(row *fyne.Container) *stripDrag {
	return &stripDrag{row: row, reduce: reduceMotion, animate: runAnimation, from: -1, to: -1, dx: map[string]float32{}}
}

// carries reports whether the tab id is lifted or landing: it is drawn raised.
func (d *stripDrag) carries(id string) bool { return (d.lifted || d.landing) && d.id == id }

// dragged follows a tab's drag: lifted past minTileDrag, under the pointer, the place a drop gives
// it.
func (d *stripDrag) dragged(o fyne.CanvasObject, ev *fyne.DragEvent) {
	if !d.active {
		d.settle() // a tab still gliding lands at once
		if d.pending {
			d.pending = false // the last drop's order gives way to the state's
			d.relayout()
		}
		at := absolutePos(o)
		d.active, d.lifted, d.stale, d.moved, d.id = true, false, false, 0, d.idOf(o)
		d.pointer = ev.AbsolutePosition.Subtract(ev.Dragged) // where the press was
		if ev.AbsolutePosition.IsZero() {                    // Fyne's test driver: test.Drag sends the press in the tab
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
		d.lift(o)
	}
	d.place()
	d.retarget()
}

// lift takes the tab out of its place: it follows the pointer, drawn over the others.
func (d *stripDrag) lift(o fyne.CanvasObject) {
	d.lifted, d.still, d.inside = true, d.reduce(), true
	d.order = d.shownIDs()
	d.from = slices.Index(d.order, d.id)
	d.to = d.from
	d.stale = d.from < 0
	if d.lifting != nil {
		d.lifting()
	}
	if i := slices.Index(d.row.Objects, o); i >= 0 { // drawn last: over the tabs it passes
		objs := slices.Delete(slices.Clone(d.row.Objects), i, i+1)
		d.row.Objects = append(objs, o)
	}
	o.Refresh() // raised
}

// place puts the carried tab under the pointer, held where it was grabbed, within the row.
func (d *stripDrag) place() {
	o := d.object(d.id)
	if o == nil {
		return
	}
	x := d.pointer.X - absolutePos(d.row).X - d.grab.X
	d.x = max(0, min(x, d.row.Size().Width-o.Size().Width))
	o.Move(fyne.NewPos(d.x, o.Position().Y))
}

// retarget picks the place a drop gives the tab now and slides the others there when it changed.
func (d *stripDrag) retarget() {
	to, inside := d.target()
	d.inside = inside
	if to != d.to {
		d.rearrange(func() { d.to = to })
	}
}

// target returns the place a drop at the pointer gives the tab: the slot whose left edge, counted
// over the other tabs' widths, is nearest the carried tab's; its own place and false away from the
// strip or once the tabs changed under the drag.
func (d *stripDrag) target() (int, bool) {
	if d.stale || len(d.order) == 0 || !d.near() {
		return d.from, false
	}
	pad := theme.Padding()
	best, bestGap := 0, float32(-1)
	var left float32
	slot := 0
	for _, id := range d.order {
		if id == d.id {
			continue
		}
		if gap := max(left-d.x, d.x-left); bestGap < 0 || gap < bestGap {
			best, bestGap = slot, gap
		}
		o := d.object(id)
		if o == nil {
			return d.from, false
		}
		left += o.MinSize().Width + pad
		slot++
	}
	if gap := max(left-d.x, d.x-left); bestGap < 0 || gap < bestGap {
		best = slot
	}
	return best, true
}

// near reports whether the pointer is near the strip: no further above or below it than its
// height.
func (d *stripDrag) near() bool {
	at, h := absolutePos(d.row).Y, d.row.Size().Height
	return d.pointer.Y >= at-h && d.pointer.Y < at+2*h
}

// rearrange changes where the tabs go (change) and slides each from where it is drawn to its new
// place; with Reduce motion they jump.
func (d *stripDrag) rearrange(change func()) {
	was := map[string]float32{}
	for _, o := range d.row.Objects {
		was[d.idOf(o)] = o.Position().X
	}
	halt(&d.stopSlide)
	change()
	d.clearSlides()
	d.relayout()
	if d.still {
		return
	}
	off := map[string]float32{}
	for _, o := range d.row.Objects {
		id := d.idOf(o)
		if id == d.id && d.lifted {
			continue
		}
		if w, ok := was[id]; ok && w != o.Position().X {
			off[id] = w - o.Position().X
		}
	}
	if len(off) == 0 {
		return
	}
	d.stopSlide = d.animate(tileSlide, func(f float32) {
		for id, v := range off {
			d.dx[id] = v * (1 - f)
		}
		d.relayout()
	})
}

// clearSlides ends every tab's slide offset but a landing tab's.
func (d *stripDrag) clearSlides() {
	for id := range d.dx {
		if !d.landing || id != d.id {
			delete(d.dx, id)
		}
	}
}

// arranged is the order the tabs are laid out in: the state's, or while a tab is carried, landing
// or its move pending, the order a drop on the chosen place gives them.
func (d *stripDrag) arranged() []string {
	ids := d.shownIDs()
	held := d.lifted || d.landing || d.pending
	if !held || d.stale || d.to == d.from || d.from < 0 || d.to < 0 || !slices.Equal(ids, d.order) || d.to >= len(ids) {
		return ids
	}
	out := slices.Delete(slices.Clone(ids), d.from, d.from+1)
	return slices.Insert(out, d.to, d.id)
}

// follow takes in the tabs the strip shows now (the bar's sync): the move shown ends the pending
// order; any other change under a carried tab makes the drag stale, so that its drop moves nothing.
func (d *stripDrag) follow() {
	now := d.shownIDs()
	if d.pending && !slices.Equal(now, d.order) {
		d.pending = false
	}
	if d.lifted && !d.stale && !slices.Equal(now, d.order) {
		d.stale, d.inside, d.to = true, false, d.from
		halt(&d.stopSlide)
		d.clearSlides()
	}
}

// ended drops the carried tab: on a new place through the strip's command, anywhere else nowhere;
// it glides into its slot. A drag that never lifted is the click that slipped.
func (d *stripDrag) ended(o fyne.CanvasObject) {
	if !d.active {
		return
	}
	d.active = false
	if !d.lifted {
		if d.click != nil {
			d.click(o, d.grab)
		}
		return
	}
	if d.inside && !d.stale && d.to != d.from {
		d.pending = true
		d.move(d.id, d.to+1, func() { d.rearrange(func() { d.pending = false }) })
	} else if d.to != d.from {
		d.rearrange(func() { d.to = d.from })
	}
	d.land()
}

// land ends a carry: the tab glides from under the pointer into its slot.
func (d *stripDrag) land() {
	d.lifted, d.landing = false, true
	halt(&d.stopGlide)
	o := d.object(d.id)
	if o == nil || d.still {
		d.settle()
		return
	}
	from := d.x
	delete(d.dx, d.id)
	d.relayout()
	slot := o.Position().X
	d.dx[d.id] = from - slot
	d.relayout()
	id := d.id
	d.stopGlide = d.animate(tileSlide, func(f float32) {
		if !d.landing || d.id != id {
			return
		}
		d.dx[id] = (from - slot) * (1 - f)
		d.relayout()
		if f >= 1 {
			d.settle()
		}
	})
}

// settle ends a landing at once: the tab is drawn in its slot, as an ordinary tab.
func (d *stripDrag) settle() {
	halt(&d.stopGlide)
	if !d.landing {
		return
	}
	d.landing = false
	delete(d.dx, d.id)
	d.relayout()
	if o := d.object(d.id); o != nil {
		o.Refresh()
	}
}

// cancel ends whatever drag there is at once: the strip went.
func (d *stripDrag) cancel() {
	halt(&d.stopSlide)
	d.active, d.lifted, d.pending, d.stale = false, false, false, false
	d.settle()
	d.order, d.from, d.to = nil, -1, -1
	clear(d.dx)
}

// pressedOn reports whether a point in a tab's coordinates lies on one of its parts (its ×).
func pressedOn(part fyne.CanvasObject, at fyne.Position) bool {
	p, size := part.Position(), part.Size()
	return part.Visible() && at.X >= p.X && at.Y >= p.Y && at.X < p.X+size.Width && at.Y < p.Y+size.Height
}

// object is the row's widget of a tab; nil when it has none.
func (d *stripDrag) object(id string) fyne.CanvasObject {
	for _, o := range d.row.Objects {
		if d.idOf(o) == id {
			return o
		}
	}
	return nil
}

// relayout places the tabs again, as they slide.
func (d *stripDrag) relayout() {
	if d.row.Layout != nil {
		d.row.Layout.Layout(d.row.Objects, d.row.Size())
	}
}

// stripLayout lays a strip's tabs out left to right with the theme's padding between, as an HBox
// does, in the order the drag arranges them (stripDrag.arranged): each tab drawn dx right of its
// slot while it slides, the carried one under the pointer.
type stripLayout struct{ d *stripDrag }

func (l *stripLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	n := 0
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		ms := o.MinSize()
		size.Width += ms.Width
		size.Height = max(size.Height, ms.Height)
		n++
	}
	if n > 1 {
		size.Width += theme.Padding() * float32(n-1)
	}
	return size
}

func (l *stripLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	d := l.d
	byID := make(map[string]fyne.CanvasObject, len(objs))
	for _, o := range objs {
		byID[d.idOf(o)] = o
	}
	pad := theme.Padding()
	var x float32
	for _, id := range d.arranged() {
		o := byID[id]
		if o == nil || !o.Visible() {
			continue
		}
		ms := o.MinSize()
		o.Resize(fyne.NewSize(ms.Width, size.Height))
		px := x + d.dx[id]
		if d.lifted && id == d.id {
			px = d.x
		}
		o.Move(fyne.NewPos(px, 0))
		x += ms.Width + pad
	}
}
