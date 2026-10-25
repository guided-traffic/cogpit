package fyneui

import (
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// The widths of the side panels, the primary sidebar, the detail drawer and the agent's chat column
// (chat.go), are the user's:
// each is set by dragging the panel's edge, stored in Fyne's preferences and restored at the next
// start. Content never moves them. A panel is a fixed number of points wide, laid out by
// panelLayout, which reports a constant minimum width whatever the panel holds, so that a long
// value in the drawer (a container's CrashLoopBackOff status, which changes every few seconds in
// the demo cluster) cannot widen it, shift the table's columns or raise the window's minimum size.
//
// The widths are a window-layout preference, not engine state (AGENTS.md rule 2): no command sets
// them, no agent reads them. An agent drives what the views show (view, filter, selection, which
// object the drawer shows); how many points wide a panel is has no effect on any of it.

const (
	prefSidebarWidth = "cogpit.ui.sidebarWidth" // Fyne preferences key, in points
	prefDrawerWidth  = "cogpit.ui.drawerWidth"  // Fyne preferences key, in points
	prefChatWidth    = "cogpit.ui.chatWidth"    // Fyne preferences key, in points

	defaultSidebarWidth float32 = 216 // the theme's sizeSidebarWidth
	defaultDrawerWidth  float32 = 600

	minSidebarWidth float32 = 160
	maxSidebarWidth float32 = 480
	minDrawerWidth  float32 = 320
	maxDrawerWidth  float32 = 1000

	// The chat column: VS Code's chat panel is about as wide by default; narrower than 300 points
	// a table in an answer has no room, wider than 760 the window's views have too little.
	defaultChatWidth float32 = 400
	minChatWidth     float32 = 300
	maxChatWidth     float32 = 760

	// minTableRoom is the width the table keeps beside the drawer, and the workspace beside the
	// sidebar: dragging an edge stops there.
	minTableRoom float32 = 480

	handleWidth float32 = 5 // the grab area of a panel's edge; the line in it is one point
)

// clampWidth keeps w in [lo, hi]; a value that is not a number or not above zero (a hand-edited
// preference file) becomes def first.
func clampWidth(w, lo, hi, def float32) float32 {
	if math.IsNaN(float64(w)) || w <= 0 {
		w = def
	}
	return min(max(w, lo), max(hi, lo))
}

// loadWidths reads the stored widths; a missing or invalid value is the default.
func (sh *shell) loadWidths() {
	sh.sidebarW, sh.drawerW, sh.chatW = defaultSidebarWidth, defaultDrawerWidth, defaultChatWidth
	if sh.app == nil {
		return
	}
	p := sh.app.Preferences()
	sh.sidebarW = clampWidth(float32(p.FloatWithFallback(prefSidebarWidth, float64(defaultSidebarWidth))), minSidebarWidth, maxSidebarWidth, defaultSidebarWidth)
	sh.drawerW = clampWidth(float32(p.FloatWithFallback(prefDrawerWidth, float64(defaultDrawerWidth))), minDrawerWidth, maxDrawerWidth, defaultDrawerWidth)
	sh.chatW = clampWidth(float32(p.FloatWithFallback(prefChatWidth, float64(defaultChatWidth))), minChatWidth, maxChatWidth, defaultChatWidth)
}

// saveWidths stores the widths; called when a drag ends, not on every step of it.
func (sh *shell) saveWidths() {
	if sh.app == nil {
		return
	}
	p := sh.app.Preferences()
	p.SetFloat(prefSidebarWidth, float64(sh.sidebarW))
	p.SetFloat(prefDrawerWidth, float64(sh.drawerW))
	p.SetFloat(prefChatWidth, float64(sh.chatW))
}

// panelLayout lays out three objects: the panel (fixed width), the handle on its edge and the rest
// (what is left). With first set the objects are panel, handle, rest (the sidebar, on the left);
// otherwise rest, handle, panel (the drawer, on the right). The panel's width is want() clamped
// to [lo, hi] and to what leaves the rest at least restMin. A hidden panel (the navigator,
// ui.toggleNavigator) takes no room: the rest gets the whole width, and the handle is hidden with it.
type panelLayout struct {
	first   bool
	want    func() float32
	lo, hi  float32
	restMin float32

	total float32         // the width of the last layout, for the drag's limit
	got   float32         // the panel's width at the last layout
	onGot func(w float32) // the panel's width changed in a layout
}

func (l *panelLayout) parts(objs []fyne.CanvasObject) (panel, handle, rest fyne.CanvasObject) {
	if l.first {
		return objs[0], objs[1], objs[2]
	}
	return objs[2], objs[1], objs[0]
}

// width is the panel's width in a container total points wide.
func (l *panelLayout) width(total float32) float32 {
	return clampWidth(l.want(), l.lo, min(l.hi, total-handleWidth-l.restMin), l.lo)
}

// limit is the widest the panel may be now.
func (l *panelLayout) limit() float32 {
	if l.total <= 0 {
		return l.hi
	}
	return max(l.lo, min(l.hi, l.total-handleWidth-l.restMin))
}

// MinSize is the panel's least width, the handle and the rest's minimum: not the panel's content.
func (l *panelLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	panel, _, rest := l.parts(objs)
	r := rest.MinSize()
	if !panel.Visible() {
		return r
	}
	return fyne.NewSize(l.lo+handleWidth+r.Width, r.Height)
}

func (l *panelLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	panel, handle, rest := l.parts(objs)
	if !panel.Visible() {
		rest.Move(fyne.NewPos(0, 0))
		rest.Resize(size)
		l.total = size.Width
		return
	}
	w := l.width(size.Width)
	restW := max(size.Width-w-handleWidth, 0)
	panelX, handleX, restX := float32(0), w, w+handleWidth
	if !l.first {
		panelX, handleX, restX = restW+handleWidth, restW, 0
	}
	panel.Move(fyne.NewPos(panelX, 0))
	panel.Resize(fyne.NewSize(w, size.Height))
	handle.Move(fyne.NewPos(handleX, 0))
	handle.Resize(fyne.NewSize(handleWidth, size.Height))
	rest.Move(fyne.NewPos(restX, 0))
	rest.Resize(fyne.NewSize(restW, size.Height))
	l.total = size.Width
	if w != l.got {
		l.got = w
		if l.onGot != nil {
			l.onGot(w)
		}
	}
}

// newPanel puts panel and rest side by side with a draggable edge between them.
func newPanel(l *panelLayout, panel, rest fyne.CanvasObject, h *resizeHandle) *fyne.Container {
	if l.first {
		return box(l, panel, h, rest)
	}
	return box(l, rest, h, panel)
}

// resizeHandle is the grab area on a panel's edge: a one-point line, highlighted under the pointer,
// and a horizontal resize cursor. A drag moves the edge by the pointer's total travel; sign is +1 when
// the panel is on the left of the edge (dragging right widens it) and -1 when it is on the right.
// A vertical handle (the dock's top edge, dockLayout) is the same turned: a horizontal line, the
// vertical resize cursor, the pointer's travel up and down; sign is -1 for the dock below it.
type resizeHandle struct {
	widget.BaseWidget
	sign     float32
	vertical bool
	current  func() float32          // the panel's width (a vertical handle's: its height) now
	set      func(w float32) float32 // applies a width and returns the one that took effect
	done     func()                  // a drag ended
	hovered  bool
	dragging bool
	start    float32
	moved    float32
	guard    dragGuard // a drag pressed outside the handle is not its own (VW20, dragguard.go)
}

var (
	_ desktop.Hoverable  = (*resizeHandle)(nil)
	_ desktop.Cursorable = (*resizeHandle)(nil)
	_ fyne.Draggable     = (*resizeHandle)(nil)
)

func newResizeHandle(sign float32, current func() float32, set func(float32) float32, done func()) *resizeHandle {
	h := &resizeHandle{sign: sign, current: current, set: set, done: done}
	h.ExtendBaseWidget(h)
	return h
}

// Cursor implements desktop.Cursorable.
func (h *resizeHandle) Cursor() desktop.Cursor {
	if h.vertical {
		return desktop.VResizeCursor
	}
	return desktop.HResizeCursor
}

// MouseIn implements desktop.Hoverable.
func (h *resizeHandle) MouseIn(*desktop.MouseEvent) { h.hovered = true; h.Refresh() }

// MouseMoved implements desktop.Hoverable.
func (h *resizeHandle) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (h *resizeHandle) MouseOut() { h.hovered = false; h.Refresh() }

// Dragged implements fyne.Draggable. The edge is anchored to the pointer: the unclamped width is
// the width at the drag's start plus the pointer's total travel, and the panel shows it clamped.
// The travel past a limit is kept, so that after overshooting the edge stays at the limit until
// the pointer is back over it, and then follows it again.
func (h *resizeHandle) Dragged(e *fyne.DragEvent) {
	if !h.guard.pass(h, e) {
		return
	}
	if !h.dragging {
		h.dragging, h.start, h.moved = true, h.current(), 0
	}
	if h.vertical {
		h.moved += e.Dragged.DY
	} else {
		h.moved += e.Dragged.DX
	}
	h.set(h.start + h.sign*h.moved)
}

// DragEnd implements fyne.Draggable.
func (h *resizeHandle) DragEnd() {
	if !h.guard.end() || !h.dragging {
		return
	}
	h.dragging = false
	h.Refresh()
	if h.done != nil {
		h.done()
	}
}

// CreateRenderer implements fyne.Widget.
func (h *resizeHandle) CreateRenderer() fyne.WidgetRenderer {
	r := &handleRenderer{h: h, bg: canvas.NewRectangle(color.Transparent), line: canvas.NewRectangle(color.Transparent)}
	r.Refresh()
	return r
}

type handleRenderer struct {
	h    *resizeHandle
	bg   *canvas.Rectangle
	line *canvas.Rectangle
}

func (r *handleRenderer) Layout(size fyne.Size) {
	r.bg.Move(fyne.NewPos(0, 0))
	r.bg.Resize(size)
	if r.h.vertical {
		r.line.Move(fyne.NewPos(0, (size.Height-1)/2))
		r.line.Resize(fyne.NewSize(size.Width, 1))
		return
	}
	r.line.Move(fyne.NewPos((size.Width-1)/2, 0))
	r.line.Resize(fyne.NewSize(1, size.Height))
}

func (r *handleRenderer) MinSize() fyne.Size {
	if r.h.vertical {
		return fyne.NewSize(0, handleWidth)
	}
	return fyne.NewSize(handleWidth, 0)
}

func (r *handleRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.bg, r.line} }
func (r *handleRenderer) Destroy()                     {}

func (r *handleRenderer) Refresh() {
	th := r.h.Theme()
	r.line.FillColor = themeColor(th, theme.ColorNameSeparator)
	r.bg.FillColor = color.Transparent
	if r.h.hovered || r.h.dragging {
		r.bg.FillColor = withAlpha(themeColor(th, colorLink), 0x55)
	}
	r.Layout(r.h.Size())
	r.bg.Refresh()
	r.line.Refresh()
}

// lockedWidth is a layout for a panel's root: its minimum width is a constant, not the widest
// thing inside, and its objects get the size it is given. What does not fit is cut off by the
// scroll container inside, never passed up to the split.
type lockedWidth struct{ min float32 }

func (l lockedWidth) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var h float32
	for _, o := range objs {
		if o.Visible() {
			h = max(h, o.MinSize().Height)
		}
	}
	return fyne.NewSize(l.min, h)
}

func (lockedWidth) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

// setDrawerWidth applies the width of a drag step to the drawer and returns the one that took
// effect (within the bounds and the room the table keeps). It stores nothing: saveWidths does,
// when the drag ends.
func (sh *shell) setDrawerWidth(w float32) float32 {
	sh.drawerW = clampWidth(w, minDrawerWidth, sh.drawerLay.limit(), minDrawerWidth)
	sh.hsplit.Refresh()
	return sh.drawerW
}

// setSidebarWidth is setDrawerWidth for the primary sidebar.
func (sh *shell) setSidebarWidth(w float32) float32 {
	sh.sidebarW = clampWidth(w, minSidebarWidth, sh.sideLay.limit(), minSidebarWidth)
	sh.mainRow.Refresh()
	return sh.sidebarW
}

// setChatWidth is setDrawerWidth for the agent's chat column (chat.go).
func (sh *shell) setChatWidth(w float32) float32 {
	sh.chatW = clampWidth(w, minChatWidth, sh.chatLay.limit(), minChatWidth)
	sh.chatSplit.Layout.Layout(sh.chatSplit.Objects, sh.chatSplit.Size()) // the two sides re-lay themselves
	canvas.Refresh(sh.chatSplit)
	return sh.chatW
}

// The dock's height is the user's as well: the handle between the workspace and the dock is a
// vertical resizeHandle, as the side panels' edges are (the owner's VW20 of 2026-10-25: Fyne's
// Split divider, which held the dock before, took a drag pressed anywhere and could not keep a
// dragGuard). The height is a share of the body's: dockOffset is the workspace's at the start, a
// drag sets it, and a resized window keeps it, as the Split did. It is not stored.

const (
	minDockHeight      float32 = 100 // the dock's strip and a few lines of its tab
	minWorkspaceHeight float32 = 120 // the table's header and a few rows above the dock
)

// dockLayout lays out the workspace, the handle below it and the dock. The dock's height is its
// share of what the handle leaves (1-offset), at least minDockHeight or what its tab needs, and at
// most what leaves the workspace minWorkspaceHeight or what it needs.
type dockLayout struct {
	offset float32 // the workspace's share of the height
	total  float32 // the height of the last layout, for the drag's limit
	got    float32 // the dock's height at the last layout
}

// mins are the least heights of the workspace and the dock.
func (l *dockLayout) mins(objs []fyne.CanvasObject) (work, dock float32) {
	return max(minWorkspaceHeight, objs[0].MinSize().Height), max(minDockHeight, objs[2].MinSize().Height)
}

// MinSize is both least heights and the handle, as wide as the wider of the two.
func (l *dockLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	work, dock := l.mins(objs)
	return fyne.NewSize(max(objs[0].MinSize().Width, objs[2].MinSize().Width), work+handleWidth+dock)
}

func (l *dockLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	work, lo := l.mins(objs)
	free := size.Height - handleWidth
	h := clampWidth((1-l.offset)*free, lo, free-work, lo)
	workH := max(free-h, 0)
	objs[0].Move(fyne.NewPos(0, 0))
	objs[0].Resize(fyne.NewSize(size.Width, workH))
	objs[1].Move(fyne.NewPos(0, workH))
	objs[1].Resize(fyne.NewSize(size.Width, handleWidth))
	objs[2].Move(fyne.NewPos(0, workH+handleWidth))
	objs[2].Resize(fyne.NewSize(size.Width, h))
	l.total, l.got = size.Height, h
}

// newDockSplit puts the workspace above the dock with the handle between them.
func (sh *shell) newDockSplit() *fyne.Container {
	sh.dockLay = &dockLayout{offset: dockOffset}
	h := newResizeHandle(-1, func() float32 { return sh.dockLay.got }, sh.setDockHeight, nil)
	h.vertical = true
	return box(sh.dockLay, sh.workArea, h, sh.dock.panel)
}

// setDockHeight applies the height of a drag step to the dock and returns the one that took effect,
// within its minimum and the workspace's.
func (sh *shell) setDockHeight(h float32) float32 {
	l := sh.dockLay
	free := l.total - handleWidth
	if free <= 0 {
		return l.got
	}
	work, lo := l.mins(sh.dockSplit.Objects)
	h = clampWidth(h, lo, free-work, lo)
	l.offset = 1 - h/free
	l.Layout(sh.dockSplit.Objects, sh.dockSplit.Size()) // the two sides re-lay themselves
	canvas.Refresh(sh.dockSplit)
	return h
}
