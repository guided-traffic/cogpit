//go:build !darwin

package fyneui

import "fyne.io/fyne/v2"

// The window's place on Windows and Linux (D-95, windowplace.go): Fyne gives a window no position
// there, so only the canvas's size and full screen are kept, when the window closes; a crash keeps
// the size of the start before.

// windowPlacer gives the window its kept size and keeps it when the window closes.
type windowPlacer struct{ keeper *windowPlaceKeeper }

func newWindowPlacer(k *windowPlaceKeeper) *windowPlacer { return &windowPlacer{keeper: k} }

// restore gives the window its kept size, else 1440 × 900, and full screen if it was. Call it
// before ShowAndRun.
func (p *windowPlacer) restore(_ fyne.App, w fyne.Window) {
	kept := p.keeper.kept()
	w.Resize(restoredSize(kept))
	if kept.FullScreen {
		w.SetFullScreen(true)
	}
}

// follow has nothing to follow: the size is read when the window closes.
func (*windowPlacer) follow(fyne.Window) {}

// finishWindow keeps the canvas's size and full screen once the app's run ended; the canvas still
// knows its last size.
func (p *windowPlacer) finishWindow(w fyne.Window) {
	p.keeper.changed(sizePlace(p.keeper.kept(), w.Canvas().Size(), w.FullScreen()))
	p.keeper.finish()
}
