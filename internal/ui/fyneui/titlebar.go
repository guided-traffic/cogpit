package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Cogpit's own title bar on macOS (ui-parity › Layout, owner requirement): no standard title bar
// above Cogpit's top bar; the top bar reaches into the title bar's area, the logo beside the window
// buttons, and the window is dragged by Cogpit's bar and zoomed or minimised by a double click on
// it, as the user's "double-click a window's title bar to" setting says. The AppKit side is
// titlebar_darwin.go and .m; other systems keep their native frames (titlebar_other.go). This file
// is what all systems share: the logic the bridge feeds, testable without a window.

// titlebarAction is what a double click on the title bar does.
type titlebarAction int

// Title bar double-click actions.
const (
	titlebarNone titlebarAction = iota
	titlebarZoom
	titlebarMinimize
)

// doubleClickAction reads macOS's "double-click a window's title bar to" setting: the global
// default AppleActionOnDoubleClick ("Maximize", "Minimize", "None"; "Fill" on systems that tile,
// for which AppKit offers no public call, so it zooms, inferred), or, when that is not set, the
// older AppleMiniaturizeOnDoubleClick. Unset, a double click zooms, as on a new Mac.
func doubleClickAction(action string, legacySet, legacyMinimize bool) titlebarAction {
	switch action {
	case "Minimize":
		return titlebarMinimize
	case "None":
		return titlebarNone
	case "Maximize", "Fill":
		return titlebarZoom
	}
	if legacySet && legacyMinimize {
		return titlebarMinimize
	}
	return titlebarZoom
}

// titlebarGeometry is the title bar as AppKit lays it out, in points from the window's top left.
type titlebarGeometry struct {
	ButtonsMinX float32 // the close button's left edge
	ButtonsMaxX float32 // the zoom button's right edge
	Height      float32 // the title bar's height
	FullScreen  bool    // in full screen the title bar and its buttons are hidden
}

// titlebarLayout turns the geometry into the top bar's room for the window buttons, in Fyne
// units: lead is the width of the spacer before the logo, height the bar's least height (the
// title bar's), both 0 in full screen. The logo starts as far right of the buttons as the buttons
// start right of the window's edge, so the buttons sit centred in their room; the spacer leaves
// out the bar's own inner padding (innerPad) and the box's gap before the logo (pad). scale is the
// canvas scale (Fyne units per point are 1/scale).
func titlebarLayout(g titlebarGeometry, scale, innerPad, pad float32) (lead, height float32) {
	if g.FullScreen || g.ButtonsMaxX <= 0 {
		return 0, 0
	}
	if scale <= 0 {
		scale = 1
	}
	room := (g.ButtonsMaxX + max(g.ButtonsMinX, 0)) / scale
	return max(room-innerPad-pad, 0), max(g.Height, 0) / scale
}

// setTitlebar makes room in the top bar for the window buttons (lead) and grows it to the title
// bar's height; zeros give the room back (full screen, other systems). It reports whether anything
// changed: AppKit reports every step of a live resize, and refreshing the window's whole content
// for each of them, the room unchanged, re-rendered every text while the window was dragged (the
// owner saw the text flicker).
func (t *topBar) setTitlebar(lead, height float32) bool {
	if t.titleSet && lead == t.titleLead && height == t.titleHeight {
		return false
	}
	t.titleSet, t.titleLead = true, lead
	if lead > 0 {
		t.lead.SetMinSize(fyne.NewSize(lead, 1))
		t.lead.Show()
	} else {
		t.lead.Hide()
	}
	t.titleHeight = height
	t.left.Refresh()
	t.sh.root.Refresh()
	return true
}

// topBarLayout stacks the bar's layers at its full size; its height is the theme's bar height or
// the title bar's, whichever is taller.
type topBarLayout struct{ t *topBar }

func (l *topBarLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var s fyne.Size
	for _, o := range objs {
		s = s.Max(o.MinSize())
	}
	h := fyne.CurrentApp().Settings().Theme().Size(sizeBarHeight)
	return fyne.NewSize(s.Width, max(s.Height, h, l.t.titleHeight))
}

func (l *topBarLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.Position{})
		o.Resize(size)
	}
}

// barDragArea lies under the top bar's contents: a mouse-down on the bar where no button is
// reaches it, and on macOS drags the window or, as the second click of a double click, zooms or
// minimises it (titlebar_darwin.go); a right-click there opens the bar's context menu
// (titlehistory.go). It draws nothing. A widget on the bar that takes clicks (a button, the bell)
// is what Fyne hands a click on it to, so a click on one never drags the window
// (TestBarClicksReachTheButtons).
type barDragArea struct {
	widget.BaseWidget
	down      func()              // the title bar's mouse-down
	secondary func(fyne.Position) // a right-click, at the pointer in window coordinates
}

var (
	_ desktop.Mouseable      = (*barDragArea)(nil)
	_ fyne.SecondaryTappable = (*barDragArea)(nil)
)

func newBarDragArea(down func()) *barDragArea {
	a := &barDragArea{down: down}
	a.ExtendBaseWidget(a)
	return a
}

// MouseDown implements desktop.Mouseable.
func (a *barDragArea) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button == desktop.MouseButtonPrimary && a.down != nil {
		a.down()
	}
}

// MouseUp implements desktop.Mouseable.
func (a *barDragArea) MouseUp(*desktop.MouseEvent) {}

// TappedSecondary implements fyne.SecondaryTappable: the bar's context menu.
func (a *barDragArea) TappedSecondary(ev *fyne.PointEvent) {
	if a.secondary != nil {
		a.secondary(ev.AbsolutePosition)
	}
}

// CreateRenderer implements fyne.Widget.
func (a *barDragArea) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

// applyTitlebar shows the geometry the bridge read in the top bar. Run it on the main goroutine.
func (sh *shell) applyTitlebar(g titlebarGeometry) {
	th := sh.app.Settings().Theme()
	scale := float32(1)
	if c := sh.win.Canvas(); c != nil {
		scale = c.Scale()
	}
	sh.top.setTitlebar(titlebarLayout(g, scale, th.Size(theme.SizeNameInnerPadding), th.Size(theme.SizeNamePadding)))
}
