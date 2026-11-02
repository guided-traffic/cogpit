package yamlview

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

// A find bar's matches show behind the text (the owner's QW1): one rectangle per visible match, the
// current one filled stronger and outlined, the others tinted; the text keeps its colours; an edit
// or a new text drops them. In both variants every colour of the text one scans keeps a contrast
// of 3 on the current match's fill.
func TestFindMarks(t *testing.T) {
	for _, variant := range []fyne.ThemeVariant{theme.VariantDark, theme.VariantLight} {
		v, w := newRenderView(t, variant, 600, 300)
		v.SetText("image: one\nname: x\nimageID: two\n")
		v.SetMarks([]Mark{{Line: 0, Start: 0, End: 5}, {Line: 2, Start: 0, End: 5}}, 1)
		w.Canvas().Capture()
		r := newRenderer(v)
		pal := r.pal
		var marks []*canvas.Rectangle
		for _, o := range r.Objects() {
			if rect, ok := o.(*canvas.Rectangle); ok && (rect.FillColor == pal.findMatch || rect.FillColor == pal.findCurrent) {
				marks = append(marks, rect)
			}
		}
		if len(marks) != 2 {
			t.Fatalf("variant %v: %d marks drawn, want 2", variant, len(marks))
		}
		if marks[0].FillColor != pal.findMatch || marks[0].StrokeWidth != 0 || marks[1].FillColor != pal.findCurrent || marks[1].StrokeWidth == 0 {
			t.Errorf("variant %v: marks %+v, %+v", variant, marks[0], marks[1])
		}
		if want := float32(5 * r.m.cw); marks[0].Size().Width < want-1 || marks[0].Size().Width > want+1 {
			t.Errorf("variant %v: a mark %v wide, want five columns (%v)", variant, marks[0].Size().Width, want)
		}
		for _, k := range []tokKind{tokPlain, tokKey, tokString, tokNumber, tokLiteral, tokAnchor} {
			if c := contrast(pal.kinds[k], pal.findCurrent); c < contrastMuted {
				t.Errorf("variant %v: kind %d has a contrast of %.2f on the current match", variant, k, c)
			}
		}

		v.SetReadOnly(false)
		v.SetCursorPosition(1, 0)
		v.ReplaceSelection("a")
		if v.marks != nil {
			t.Errorf("variant %v: an edit kept the marks", variant)
		}
		v.SetMarks([]Mark{{Line: 0, Start: 0, End: 5}}, 0)
		v.SetText("other: text\n")
		if v.marks != nil {
			t.Errorf("variant %v: a new text kept the marks", variant)
		}
	}
}
