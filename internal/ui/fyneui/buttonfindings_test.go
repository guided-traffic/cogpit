package fyneui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The owner's button findings VW2–VW4 of the VW walk (2026-10-23, D-104's button block).

// iconName is the name of the resource a themed or coloured icon wraps.
func iconName(r fyne.Resource) string {
	if r == nil {
		return ""
	}
	return r.Name()
}

// TestFindBarStepsAreChevrons (VW2: "für next & previous match nicht der gleich icon type"): the
// find bar's previous and next match are one icon family, Cogpit's chevrons up and down; Fyne's
// arrow with a stem is gone from it.
func TestFindBarStepsAreChevrons(t *testing.T) {
	b, _ := findBarFixture(t, "a: 1\n")
	if got, want := iconName(b.prev.Icon), iconName(chevronUpIcon()); got != want || !strings.HasSuffix(got, "cogpit-chevron-up.svg") {
		t.Errorf("previous match: %q, want the chevron up %q", got, want)
	}
	if got, want := iconName(b.next.Icon), iconName(chevronDownIcon()); got != want || !strings.HasSuffix(got, "cogpit-chevron-down.svg") {
		t.Errorf("next match: %q, want the chevron down %q", got, want)
	}
	if iconName(b.prev.Icon) == iconName(theme.MoveUpIcon()) {
		t.Error("previous match is still Fyne's arrow")
	}
}

// TestSearchSwitchesOneWidth (VW3: "Die Button … sind nicht gleich breit"): the find bar's Aa, ab
// and .* are one width, the widest one's, and so are the list search's Aa and .*; their words stay
// centred.
func TestSearchSwitchesOneWidth(t *testing.T) {
	b, _ := findBarFixture(t, "a: 1\n")
	widths := func(bs ...*tipButton) []float32 {
		var out []float32
		for _, x := range bs {
			out = append(out, x.MinSize().Width)
		}
		return out
	}
	alike := func(what string, bs ...*tipButton) {
		t.Helper()
		ws := widths(bs...)
		for _, w := range ws {
			if w != ws[0] {
				t.Errorf("%s: widths %v, want one", what, ws)
				return
			}
		}
		for _, x := range bs {
			if x.Alignment != widget.ButtonAlignCenter {
				t.Errorf("%s: %q not centred", what, x.Text)
			}
		}
	}
	alike("the find bar's switches", b.toggles[:]...)
	plain := newBareTipButton(b.sh, nil, "", nil)
	plain.Text = ".*"
	if got := b.toggles[2].MinSize().Width; got <= plain.MinSize().Width {
		t.Errorf(".* is %v wide, its own width %v: want it as wide as the widest switch", got, plain.MinSize().Width)
	}

	sh, _, _, _ := nsFilterShell(t, ThemeDark)
	alike("the list search's switches", sh.view.toggles.matchCase, sh.view.toggles.regex)
}

// TestListFilterCaretAtTheRightEdge (VW4: "für den Pfeil nach unten bitte den zweiten Button aus dem
// Screenshot 2 nehmen und nach ganz rechts einrücken"): the namespace filter's and the status
// filter's caret is the find bar's chevron down, at the button's right edge, as far in as the word
// stands from its left edge; the other buttons that open a list (the Create tab's templates, the log
// tab's pickers) have the chevron too, after their word.
func TestListFilterCaretAtTheRightEdge(t *testing.T) {
	sh, w, _, _ := nsFilterShell(t, ThemeDark)
	settle(w)
	in := sh.app.Settings().Theme().Size(theme.SizeNameInnerPadding)
	for name, b := range map[string]*textButton{"namespace filter": sh.view.nsButton, "status filter": sh.view.states.button} {
		if got := iconName(b.Icon); got != iconName(chevronDownIcon()) {
			t.Errorf("%s: caret %q, want the chevron down", name, got)
			continue
		}
		r := test.WidgetRenderer(b).(*textButtonRenderer)
		r.Layout(b.Size())
		var img *canvas.Image
		var label *widget.RichText
		for _, o := range r.WidgetRenderer.Objects() {
			switch x := o.(type) {
			case *canvas.Image:
				img = x
			case *widget.RichText:
				label = x
			}
		}
		if img == nil || label == nil {
			t.Fatalf("%s: no icon or word drawn", name)
		}
		if right := img.Position().X + img.Size().Width; right != b.Size().Width-in {
			t.Errorf("%s: the caret ends at %v in a button %v wide, want %v", name, right, b.Size().Width, b.Size().Width-in)
		}
		if label.Position().X >= img.Position().X || b.Size().Width-in-img.Size().Width-label.Position().X < label.MinSize().Width/2 {
			t.Errorf("%s: the word at %v, the caret at %v: want the word at the left, the caret apart from it", name, label.Position().X, img.Position().X)
		}
	}

	_, _, _, ct := createScene(t, ThemeDark)
	if got := iconName(ct.picker.Icon); got != iconName(chevronDownIcon()) {
		t.Errorf("the Create tab's templates: caret %q, want the chevron down", got)
	}
	lsh, _, _, _ := logsScene(t, ThemeDark, true, engine.LogOptions{ResourceNames: true})
	lt := logTabOf(lsh)
	for name, b := range map[string]*textButton{"pod": lt.podPick, "container": lt.containerPick, "workload": lt.workloadPick} {
		if got := iconName(b.Icon); got != iconName(chevronDownIcon()) {
			t.Errorf("the log tab's %s picker: caret %q, want the chevron down", name, got)
		}
	}
}
