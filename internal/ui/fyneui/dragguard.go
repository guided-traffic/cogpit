package fyneui

import "fyne.io/fyne/v2"

// A drag belongs to the object the mouse button went down on (the owner's VW20 of 2026-10-25:
// "Drag&Drop kann an random position gestartet werden"). Fyne's GLFW driver (window.go,
// processMouseMoved) hands a drag to the draggable under the pointer's previous position once the
// pointer is two points away from the press, wherever the press was, and the drag's first event
// carries the whole way from the press: a press on empty space and a move onto a hotbar tile, a tab
// or a panel's edge gave that object the drag, and a tile jumped by the whole way, out of the
// window. Every Draggable of Cogpit's keeps a dragGuard and ignores a drag pressed outside itself,
// from its first event to its DragEnd: nothing moves, nothing is dropped, clicked or sent. Fyne's
// own draggables (the dock's Split divider, a scroll bar, an entry's text, a slider) cannot keep
// one and still behave as the driver hands them the drag.

// dragGuard is a Draggable's view of the drag it is given: pressed on it, or outside it.
type dragGuard struct {
	started bool // the drag's first event came
	outside bool // it was pressed outside the object: ignored until its DragEnd
}

// pass reports whether a drag event goes on to o's drag: false for every event of a drag pressed
// outside o. The first event decides, once, since an object can move under its own drag (a carried
// tab, a panel's edge, a hotbar tile while its bar scrolls) and leave the press behind.
func (g *dragGuard) pass(o fyne.CanvasObject, ev *fyne.DragEvent) bool {
	if !g.started {
		g.started, g.outside = true, !pressedIn(o, ev)
	}
	return !g.outside
}

// end reports whether the drag that ends went on to the object, and forgets it. A DragEnd without
// any event before it goes on, as it did before the guard.
func (g *dragGuard) end() bool {
	passed := !g.outside
	*g = dragGuard{}
	return passed
}

// pressedIn reports whether the drag whose first event ev is was pressed on o: the press is the
// pointer less the way it came, which the driver sends whole in the first event. Fyne's test
// driver sends no pointer, and test.Drag gives the drag to the object at its press.
func pressedIn(o fyne.CanvasObject, ev *fyne.DragEvent) bool {
	if ev.AbsolutePosition.IsZero() {
		return true
	}
	return pointerOn(o, ev.AbsolutePosition.Subtract(ev.Dragged))
}
