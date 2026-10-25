package fyneui

import (
	"image/color"
	"reflect"
	"slices"
	"sync"
	"weak"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// A text field passes the wheel on (the owner's finding of 2026-10-25: "Wenn die Maus über einem
// dieser Textfelder hovert, kann nicht mehr gescrollt werden. Wenn es sich um ein
// Multiline-Textfeld handelt, ist es in Ordnung, wenn erst bis zum Ende gescrollt wird. Ist dann
// aber das Ende des Multiline-Textfelds erreicht, wird außerhalb des Textfelds weiter gescrollt. In
// Single-Line-Textfeldern darf das Scrolling gar nicht stecken bleiben. Das gilt für alle
// Textfelder unserer App.").
//
// Fyne 2.8's Entry holds its text in a scroller of its own (widget/entry.go, whenever its wrapping
// is not off, as Cogpit's fields clip), the driver hands a wheel to the topmost Scrollable under
// the pointer, and a scroller always takes it, at its end too (internal/widget/scroller.go
// Scrolled; a single line's turns a vertical wheel into a sideways one). So every Cogpit text field
// draws a transparent wheelPass over itself, which takes the wheel only: clicks, hover, drags,
// the context menu and the cursor stay the field's, as the driver looks those up by their own
// interfaces. A single line hands a vertical wheel to the scroller around it, a sideways swipe to
// its own (a long line still moves); a multi-line field scrolls itself and hands the wheel on once
// its offset no longer moves. The scroller around it is one of Cogpit's own (newVScroll and its
// siblings register each), the innermost that holds the pointer and the field.

// scrollHosts are Cogpit's scrollers, held weakly: a dialog's goes with the dialog.
var scrollHosts struct {
	mu   sync.Mutex
	list []weak.Pointer[container.Scroll]
}

// newVScroll is container.NewVScroll, registered as a scroller a text field may hand its wheel to.
func newVScroll(content fyne.CanvasObject) *container.Scroll {
	return hostScroll(container.NewVScroll(content))
}

// newScroll is container.NewScroll, registered (newVScroll).
func newScroll(content fyne.CanvasObject) *container.Scroll {
	return hostScroll(container.NewScroll(content))
}

// newHScroll is container.NewHScroll, registered (newVScroll).
func newHScroll(content fyne.CanvasObject) *container.Scroll {
	return hostScroll(container.NewHScroll(content))
}

// hostScroll registers s and drops the scrollers gone since.
func hostScroll(s *container.Scroll) *container.Scroll {
	scrollHosts.mu.Lock()
	defer scrollHosts.mu.Unlock()
	scrollHosts.list = slices.DeleteFunc(scrollHosts.list, func(p weak.Pointer[container.Scroll]) bool { return p.Value() == nil })
	scrollHosts.list = append(scrollHosts.list, weak.Make(s))
	return s
}

// outerScroll is the innermost of Cogpit's scrollers on field's canvas whose bounds hold the point
// and the field; nil when none does.
func outerScroll(field fyne.CanvasObject, at fyne.Position) *container.Scroll {
	d := fyne.CurrentApp().Driver()
	cnv := d.CanvasForObject(field)
	if cnv == nil {
		return nil
	}
	fieldAt := d.AbsolutePositionForObject(field)
	scrollHosts.mu.Lock()
	hosts := make([]*container.Scroll, 0, len(scrollHosts.list))
	for _, p := range scrollHosts.list {
		if s := p.Value(); s != nil && s.Visible() {
			hosts = append(hosts, s)
		}
	}
	scrollHosts.mu.Unlock()
	var best *container.Scroll
	var bestArea float32
	for _, s := range hosts {
		if d.CanvasForObject(s) != cnv {
			continue
		}
		pos, size := d.AbsolutePositionForObject(s), s.Size()
		if (pos.IsZero() && !isRoot(cnv, s)) || !rectHolds(pos, size, at) || !rectHolds(pos, size, fieldAt) {
			continue // a scroller not on the canvas's visible tree reports the origin
		}
		if area := size.Width * size.Height; best == nil || area < bestArea {
			best, bestArea = s, area
		}
	}
	return best
}

// isRoot reports whether o is the canvas's content or an overlay, whose origin is the canvas's.
func isRoot(c fyne.Canvas, o fyne.CanvasObject) bool {
	return c.Content() == o || slices.Contains(c.Overlays().List(), o)
}

// rectHolds reports whether the rectangle at pos of size holds p.
func rectHolds(pos fyne.Position, size fyne.Size, p fyne.Position) bool {
	return p.X >= pos.X && p.Y >= pos.Y && p.X < pos.X+size.Width && p.Y < pos.Y+size.Height
}

// wheelPass is the transparent layer over a text field that takes its wheel (above).
type wheelPass struct {
	widget.BaseWidget
	field fyne.CanvasObject
	multi func() bool
	inner func() fyne.Scrollable // the field's own scroller, nil when it has none
	outer *container.Scroll      // the last scroller handed the wheel, kept while it still holds it
}

// CreateRenderer implements fyne.Widget: nothing drawn.
func (w *wheelPass) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

// Scrolled implements fyne.Scrollable.
func (w *wheelPass) Scrolled(ev *fyne.ScrollEvent) {
	inner := w.inner()
	sideways := abs32(ev.Scrolled.DX) > abs32(ev.Scrolled.DY)
	if inner != nil && (w.multi() || sideways) {
		before, ok := scrollOffset(inner)
		inner.Scrolled(ev)
		if after, _ := scrollOffset(inner); !ok || after != before || !w.multi() {
			return // it moved, or a single line's sideways swipe: the field's own
		}
	}
	if s := w.outerFor(ev.AbsolutePosition); s != nil {
		s.Scrolled(ev)
	}
}

// outerFor is the scroller around the field for a wheel at the point: the last one while it still
// holds the point and the field, else found again.
func (w *wheelPass) outerFor(at fyne.Position) *container.Scroll {
	d := fyne.CurrentApp().Driver()
	if s := w.outer; s != nil && s.Visible() && d.CanvasForObject(s) == d.CanvasForObject(w.field) {
		pos := d.AbsolutePositionForObject(s)
		if (!pos.IsZero() || isRoot(d.CanvasForObject(s), s)) && rectHolds(pos, s.Size(), at) && rectHolds(pos, s.Size(), d.AbsolutePositionForObject(w.field)) {
			return s
		}
	}
	w.outer = outerScroll(w.field, at)
	return w.outer
}

// scrollOffset reads a scroller's Offset (Fyne's internal Scroll exports it); false when it has none.
func scrollOffset(s fyne.Scrollable) (fyne.Position, bool) {
	v := reflect.ValueOf(s)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return fyne.Position{}, false
	}
	f := v.Elem().FieldByName("Offset")
	if !f.IsValid() || f.Type() != reflect.TypeFor[fyne.Position]() {
		return fyne.Position{}, false
	}
	return f.Interface().(fyne.Position), true
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// wheelRenderer is a text field's renderer with its wheelPass over it.
type wheelRenderer struct {
	fyne.WidgetRenderer
	pass    *wheelPass
	objects []fyne.CanvasObject
}

// passWheel wraps a text field's renderer (its widget's CreateRenderer: field.Entry.CreateRenderer)
// with the wheelPass; multi reports whether the field has several lines now.
func passWheel(field fyne.Widget, r fyne.WidgetRenderer, multi func() bool) fyne.WidgetRenderer {
	wr := &wheelRenderer{WidgetRenderer: r}
	wr.pass = &wheelPass{field: field, multi: multi, inner: func() fyne.Scrollable {
		for _, o := range r.Objects() {
			if s, ok := o.(fyne.Scrollable); ok && o.Visible() {
				return s
			}
		}
		return nil
	}}
	wr.pass.ExtendBaseWidget(wr.pass)
	return wr
}

// Layout implements fyne.WidgetRenderer: the field's, the layer over all of it.
func (r *wheelRenderer) Layout(size fyne.Size) {
	r.WidgetRenderer.Layout(size)
	r.pass.Move(fyne.NewPos(0, 0))
	r.pass.Resize(size)
}

// Objects implements fyne.WidgetRenderer: the field's, the layer last, so on top.
func (r *wheelRenderer) Objects() []fyne.CanvasObject {
	inner := r.WidgetRenderer.Objects()
	if len(r.objects) != len(inner)+1 || (len(inner) > 0 && r.objects[len(inner)-1] != inner[len(inner)-1]) || (len(inner) > 0 && r.objects[0] != inner[0]) {
		r.objects = append(slices.Clone(inner), r.pass)
	}
	return r.objects
}

// wheelEntry is Fyne's Entry passing the wheel on, for a field that needs nothing else of Cogpit's
// fields (Escape and the rest stay Fyne's).
type wheelEntry struct{ widget.Entry }

func newWheelEntry() *wheelEntry {
	e := &wheelEntry{}
	e.Wrapping = fyne.TextWrap(fyne.TextTruncateClip) // as widget.NewEntry
	e.ExtendBaseWidget(e)
	return e
}

// CreateRenderer implements fyne.Widget: the wheel passed on.
func (e *wheelEntry) CreateRenderer() fyne.WidgetRenderer {
	return passWheel(e, e.Entry.CreateRenderer(), func() bool { return e.MultiLine })
}
