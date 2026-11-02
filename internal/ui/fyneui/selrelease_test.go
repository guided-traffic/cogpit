package fyneui

import (
	"testing"

	"fyne.io/fyne/v2"
)

// A drag that selects and whose button comes up beside the text keeps its selection (the owner's
// QW6): Fyne's GLFW driver drops the focus at a release that is not over the focused widget, then
// ends the drag; the end takes the focus back, so ⌘C copies what the drag selected. A click on
// something else afterwards drops it as before.
func TestADragReleasedBesideTheTextKeepsItsSelection(t *testing.T) {
	sh, w, _, ref := podDrawerScene(t, ThemeDark)
	l := selLineWith(t, sh.drawer, ref.Name)
	at := absolutePos(l)
	press := at.AddXY(1, l.Size().Height/2)
	beyond := at.AddXY(l.Size().Width+40, l.Size().Height+30)

	l.MouseDown(mouse())
	strayDrag(l, press, at.AddXY(l.Size().Width/2, l.Size().Height/2), beyond)
	w.Canvas().Unfocus() // the driver, at the release beside the line
	l.MouseUp(mouse())
	l.DragEnd()
	if got := l.SelectedText(); got == "" || w.Canvas().Focused() != l {
		t.Fatalf("after the release beside the line: selected %q, focused %v", got, w.Canvas().Focused() == l)
	}
	sh.app.Clipboard().SetContent("")
	l.TypedShortcut(&fyne.ShortcutCopy{})
	if got := sh.app.Clipboard().Content(); got == "" || got != l.SelectedText() {
		t.Errorf("⌘C copied %q, want %q", got, l.SelectedText())
	}

	w.Canvas().Unfocus() // a click on something else
	if got := l.SelectedText(); got != "" {
		t.Errorf("the selection stayed after the focus went elsewhere: %q", got)
	}
}
