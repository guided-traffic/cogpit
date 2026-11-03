package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// A column dragged narrower than its title keeps the title within it (the owner, 2026-11-03: the
// headers ran over each other): the title is shortened with an ellipsis, room left for the sort
// arrow, which stays inside the column too; the header is marked cut, so its tooltip names the
// title whole. Wide enough, the title shows whole and is not cut.
func TestHeaderTitleFitsItsColumn(t *testing.T) {
	sh, _, _ := newTestShell(t, connectedFake(t, 1), Options{})
	h := newHeaderCell(nil)
	h.set(3, "Namespace", 1)
	r := test.WidgetRenderer(h)
	th := sh.app.Settings().Theme()
	for _, width := range []float32{60, 34, 12} {
		h.Resize(fyne.NewSize(width, 30))
		r.Layout(h.Size())
		if h.text.Text == "Namespace" || !h.cut {
			t.Errorf("%v wide: the title %q, cut %v", width, h.text.Text, h.cut)
		}
		if h.text.Text != "" && !strings.HasSuffix(h.text.Text, "…") {
			t.Errorf("%v wide: %q is not shortened with an ellipsis", width, h.text.Text)
		}
		if end := h.text.Position().X + h.text.MinSize().Width; h.text.Text != "" && end > width {
			t.Errorf("%v wide: the title ends at %v", width, end)
		}
		if end := h.arrow.img.Position().X + h.arrow.img.Size().Width; end > width && width >= th.Size(sizeSortIcon) {
			t.Errorf("%v wide: the sort arrow ends at %v", width, end)
		}
	}
	h.Resize(fyne.NewSize(200, 30))
	r.Layout(h.Size())
	if h.text.Text != "Namespace" || h.cut {
		t.Errorf("200 wide: the title %q, cut %v", h.text.Text, h.cut)
	}
}
