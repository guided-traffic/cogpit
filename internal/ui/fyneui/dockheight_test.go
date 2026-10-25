package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// openDockScene is the shell with the dock open on three tabs.
func openDockScene(t *testing.T) (*shell, fyne.Window, *mainQueue, *fakeEngine) {
	t.Helper()
	f := dockTabsFake(t)
	f.ui.DockOpen = true
	sh, w, q := newTestShell(t, f, Options{Theme: ThemeDark})
	q.until(t, "the open dock", func() bool {
		return len(sh.dock.tabRow.Objects) == 3 && sh.view.snap != nil && sh.dock.open() && len(sh.body.Objects) == 1 && sh.body.Objects[0] == sh.dockSplit
	})
	settle(w)
	return sh, w, q, f
}

// dockHandle is the handle between the workspace and the dock.
func dockHandle(sh *shell) *resizeHandle { return sh.dockSplit.Objects[1].(*resizeHandle) }

// dragDockEdge drags the dock's edge by dy points, pressed on it, as the GLFW driver sends a drag.
func dragDockEdge(sh *shell, dy float32) {
	h := dockHandle(sh)
	at := absolutePos(h)
	to := at.AddXY(200, handleWidth/2+dy)
	h.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{AbsolutePosition: to, Position: to.Subtract(at)}, Dragged: fyne.NewDelta(0, dy)})
	h.DragEnd()
}

// dockShare is the dock's share of what the handle leaves of the body's height.
func dockShare(sh *shell) float32 {
	return sh.dock.panel.Size().Height / (sh.dockSplit.Size().Height - handleWidth)
}

// The open dock lies below the workspace with Cogpit's own handle between them, the side panels'
// turned (the owner's VW20): a horizontal line, the vertical resize cursor, the dock at 1-dockOffset
// of the height at first; a drag pressed on the handle sets its height, within the dock's minimum
// and the workspace's; a resized window, a collapse and a reopen keep the share.
func TestDockHeightByDraggingItsEdge(t *testing.T) {
	sh, w, q, f := openDockScene(t)
	h := dockHandle(sh)
	work, panel := sh.workArea, sh.dock.panel
	if got := dockShare(sh); !drawnNear(got, 1-dockOffset) {
		t.Fatalf("the dock takes %v of the height at first, want %v", got, 1-dockOffset)
	}
	if work.Position().Y != 0 || h.Position().Y != work.Size().Height || panel.Position().Y != work.Size().Height+handleWidth ||
		h.Size().Height != handleWidth || h.Size().Width != sh.dockSplit.Size().Width {
		t.Fatalf("workspace at %v %v, handle at %v %v, dock at %v", work.Position(), work.Size(), h.Position(), h.Size(), panel.Position())
	}
	if h.Cursor() != desktop.VResizeCursor {
		t.Error("the dock's edge shows no vertical resize cursor")
	}
	if line := test.WidgetRenderer(h).(*handleRenderer).line; line.Size().Height != 1 || line.Size().Width != h.Size().Width {
		t.Errorf("the handle's line is %v, want one point high across", line.Size())
	}

	before := panel.Size().Height
	dragDockEdge(sh, -100) // up: the dock grows
	settle(w)
	if got := panel.Size().Height; !drawnNear(got, before+100) || !drawnNear(work.Size().Height+handleWidth+got, sh.dockSplit.Size().Height) {
		t.Fatalf("dragged 100 up: the dock is %v high, want %v (workspace %v)", got, before+100, work.Size().Height)
	}
	wantWork, wantDock := sh.dockLay.mins(sh.dockSplit.Objects)
	dragDockEdge(sh, 5000) // down as far as it goes: the dock's minimum
	settle(w)
	if got := panel.Size().Height; !drawnNear(got, wantDock) {
		t.Errorf("dragged shut: the dock is %v high, want its minimum %v", got, wantDock)
	}
	dragDockEdge(sh, -5000) // up as far as it goes: the workspace keeps its minimum
	settle(w)
	if got := work.Size().Height; !drawnNear(got, wantWork) {
		t.Errorf("dragged open: the workspace is %v high, want its minimum %v", got, wantWork)
	}

	dragDockEdge(sh, 5000)
	dragDockEdge(sh, -150)
	settle(w)
	share := dockShare(sh)
	w.Resize(fyne.NewSize(1440, 1100))
	settle(w)
	if got := dockShare(sh); !drawnNear(got, share) {
		t.Errorf("a taller window: the dock's share %v, was %v", got, share)
	}
	f.update(func(u *engine.UIState) { u.DockOpen = false })
	q.until(t, "the dock collapsed", func() bool { return !sh.dock.open() && sh.body.Objects[0] != sh.dockSplit })
	f.update(func(u *engine.UIState) { u.DockOpen = true })
	q.until(t, "the dock open again", func() bool { return sh.dock.open() && sh.body.Objects[0] == sh.dockSplit })
	settle(w)
	if got := dockShare(sh); !drawnNear(got, share) {
		t.Errorf("collapsed and opened again: the dock's share %v, was %v", got, share)
	}
}
