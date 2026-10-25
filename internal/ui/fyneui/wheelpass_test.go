package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

// passOf is the wheelPass a text field draws over itself.
func passOf(t *testing.T, field fyne.Widget) *wheelPass {
	t.Helper()
	for _, o := range test.WidgetRenderer(field).Objects() {
		if p, ok := o.(*wheelPass); ok {
			return p
		}
	}
	t.Fatalf("%T draws no wheelPass", field)
	return nil
}

// wheelOver scrolls the wheel over a field by dx, dy as the driver does: Fyne's test driver hands
// it to the topmost Scrollable under the pointer, which is the field's wheelPass (its event carries
// no pointer, at the origin, which the page at the window's origin holds).
func wheelOver(t *testing.T, field fyne.Widget, dx, dy float32) {
	t.Helper()
	d := fyne.CurrentApp().Driver()
	test.Scroll(d.CanvasForObject(field), d.AbsolutePositionForObject(field).AddXY(5, 5), dx, dy)
}

// TestATextFieldPassesTheWheelOn (the owner's finding of 2026-10-25): over a one-line field the
// wheel scrolls the page around it, never the field (a sideways swipe still moves a long line);
// over a multi-line field it scrolls the field to its end and then the page.
func TestATextFieldPassesTheWheelOn(t *testing.T) {
	test.NewTempApp(t)
	line := newFieldEntry()
	line.SetText(strings.Repeat("a long line that does not fit the field ", 8))
	lines := newFieldMultiLineEntry()
	lines.SetText(strings.Repeat("one line of many\n", 40))
	lines.SetMinRowsVisible(4)
	page := newVScroll(container.NewVBox(line, lines, fixedHeight(2000)))
	w := test.NewWindow(page)
	defer w.Close()
	w.SetPadded(false) // the page at the window's origin, where the test driver's wheel says it is
	w.Resize(fyne.NewSize(400, 300))

	wheelOver(t, line, 0, -60)
	if page.Offset.Y != 60 {
		t.Fatalf("the wheel over one line moved the page to %v, want 60", page.Offset.Y)
	}
	if off, _ := scrollOffset(passOf(t, line).inner()); off.X != 0 || off.Y != 0 {
		t.Errorf("the line's own scroller moved to %v", off)
	}
	wheelOver(t, line, -40, 0)
	if off, _ := scrollOffset(passOf(t, line).inner()); off.X == 0 || page.Offset.Y != 60 {
		t.Errorf("a sideways swipe: the line at %v, the page at %v", off, page.Offset.Y)
	}

	page.ScrollToTop()
	inner := passOf(t, lines).inner()
	for range 100 { // down the field to its end
		before, _ := scrollOffset(inner)
		wheelOver(t, lines, 0, -40)
		if after, _ := scrollOffset(inner); after == before {
			break
		}
		if page.Offset.Y != 0 {
			t.Fatalf("the page moved at %v while the field could still scroll", page.Offset.Y)
		}
	}
	if off, _ := scrollOffset(inner); off.Y == 0 || page.Offset.Y == 0 {
		t.Errorf("at the field's end: the field at %v, the page at %v; want both moved", off.Y, page.Offset.Y)
	}
}

// TestEveryTextFieldPassesTheWheel: each of Cogpit's text field types draws the wheelPass.
func TestEveryTextFieldPassesTheWheel(t *testing.T) {
	test.NewTempApp(t)
	fields := []fyne.Widget{newFieldEntry(), newFieldSelectEntry(nil), newWheelEntry()}
	for _, f := range fields {
		w := test.NewWindow(f)
		passOf(t, f)
		w.Close()
	}
}
