package yamlview

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// releaseOver replays what Fyne 2.8.2's desktop driver does when the primary button comes up over
// the object under after a drag that began on dragged (internal/driver/glfw/window.go,
// processMouseClicked with action release): the dragged widget gets MouseUp when under is
// Mouseable; the canvas is unfocused when under is not the focused object and the focused object
// is not under the pointer (the "a tap outside unfocuses" rule, which the driver runs on the
// release as on the press; the callers release well outside the editor); then the drag ends. Fyne's
// test driver models none of this, so the test replays it in the driver's order.
func releaseOver(c fyne.Canvas, under fyne.CanvasObject, dragged fyne.Draggable, ev *desktop.MouseEvent) {
	if _, ok := under.(desktop.Mouseable); ok {
		if m, ok := dragged.(desktop.Mouseable); ok {
			m.MouseUp(ev)
		}
	}
	if f, ok := under.(fyne.Focusable); !ok || f != c.Focused() {
		c.Unfocus()
	}
	dragged.DragEnd()
}

// editorAboveFooter puts an editable view holding text above a footer button in an offscreen
// window of Fyne's test driver, as an Edit tab's editor sits above its footer.
func editorAboveFooter(t *testing.T, text string) (*View, *widget.Button, fyne.Canvas) {
	t.Helper()
	test.NewTempApp(t).Settings().SetTheme(forcedVariant{Theme: theme.DefaultTheme(), variant: theme.VariantDark})
	v := New()
	v.SetReadOnly(false)
	v.SetText(text)
	footer := widget.NewButton("Apply", nil)
	w := test.NewWindow(container.NewBorder(nil, footer, nil, nil, v))
	w.SetPadded(false)
	w.Resize(fyne.NewSize(600, 300))
	t.Cleanup(w.Close)
	return v, footer, w.Canvas()
}

// TestDragReleasedOutsideKeepsTheKeyboard is the owner's WF8: a selection made by dragging out of
// the editor and released over another widget (the tab's footer, here its button) keeps the editor
// focused and its selection, so that Backspace deletes the selection, as in every macOS text view.
// Before the fix the driver's release unfocused the editor and Backspace went nowhere.
func TestDragReleasedOutsideKeepsTheKeyboard(t *testing.T) {
	v, footer, c := editorAboveFooter(t, "alpha: 1\nbeta: 2\ngamma: 3")

	m := v.metrics()
	at := func(line, col int) fyne.Position {
		return fyne.NewPos(float32(m.textX()+float64(col)*m.cw)+1, m.pad+float32(float64(line)*m.lh+m.lh/2))
	}
	press := &desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: at(0, 2)}, Button: desktop.MouseButtonPrimary}
	v.MouseDown(press)
	if c.Focused() != v {
		t.Fatal("a press in the editor does not focus it")
	}
	v.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: at(1, 3)}, Dragged: fyne.NewDelta(0, float32(m.lh))})
	// Out of the editor, down over the footer: the selection runs to the last line.
	below := fyne.NewPos(at(2, 5).X, v.Size().Height+footer.Size().Height/2)
	v.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: below}, Dragged: fyne.NewDelta(0, below.Y-at(1, 3).Y)})
	sel := v.SelectedText()
	if sel != "pha: 1\nbeta: 2\ngamma" {
		t.Fatalf("the drag selected %q", sel)
	}

	releaseOver(c, footer, v, &desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: below}, Button: desktop.MouseButtonPrimary})

	if c.Focused() != v {
		t.Fatalf("after the release over the footer the focus is %T, want the editor", c.Focused())
	}
	if got := v.SelectedText(); got != sel {
		t.Fatalf("after the release the selection is %q, want %q", got, sel)
	}
	c.Focused().TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
	wantText(t, v, "al: 3")
}

// TestScrollBarDragReleasedOutsideKeepsTheKeyboard: the same for a drag of the editor's scroll bar.
func TestScrollBarDragReleasedOutsideKeepsTheKeyboard(t *testing.T) {
	v, footer, c := editorAboveFooter(t, syntheticYAML(20000))

	m := v.metrics()
	v.MouseDown(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(v.Size().Width-m.bar/2, 10)}, Button: desktop.MouseButtonPrimary})
	v.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(v.Size().Width-m.bar/2, v.Size().Height+20)}, Dragged: fyne.NewDelta(0, v.Size().Height)})
	if v.scrollY == 0 {
		t.Fatal("the scroll bar's drag did not scroll")
	}
	releaseOver(c, footer, v, &desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	if c.Focused() != v {
		t.Fatalf("after the release over the footer the focus is %T, want the editor", c.Focused())
	}
}
