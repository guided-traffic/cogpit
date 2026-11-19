package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// The dismissable overlay (the command palette's since the context picker went, D-57) closes when
// the user clicks outside it on Cogpit, and on Escape; a click on its own panel keeps it (owner).
func TestOverlayClosesOnAClickOutside(t *testing.T) {
	sh, w, q, _, _ := paletteShell(t, ThemeDark)
	openPalette(t, sh, q)
	settle(w)

	panel := sh.palette.popup.panel
	inside := panel.Position().AddXY(4, 4) // the panel's padding, outside every widget of it
	test.TapCanvas(w.Canvas(), inside)
	if !sh.palette.visible() {
		t.Fatal("a click on the palette's own panel closed it")
	}
	test.TapCanvas(w.Canvas(), fyne.NewPos(5, w.Canvas().Size().Height-5))
	q.until(t, "closed by a click outside", func() bool { return !sh.palette.visible() && !sh.ui.Palette })

	openPalette(t, sh, q)
	w.Canvas().Unfocus()
	sh.win.Canvas().OnTypedKey()(&fyne.KeyEvent{Name: fyne.KeyEscape})
	q.until(t, "closed by Escape", func() bool { return !sh.palette.visible() })
}
