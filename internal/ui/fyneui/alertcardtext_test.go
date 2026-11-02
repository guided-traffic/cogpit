package fyneui

import (
	"image/color"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// A card's text and its description select as the drawer's texts do (the owner's QW11): a drag
// across its wrapped lines selects the text as it shows, a slot as its name, ⌘C copies it, and a
// double click selects a word.
func TestCardTextSelects(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(newTheme(ThemeDark))
	var copied string
	st := newSpanText(theme.SizeNameText, func(s string) { copied = s },
		span{text: "PGs have been inactive in pool ", color: theme.ColorNameForeground},
		span{text: "‹name›", color: colorMuted, style: fyne.TextStyle{Italic: true}},
		span{text: ". Inactive placement groups serve no requests.", color: theme.ColorNameForeground})
	w := test.NewWindow(container.NewWithoutLayout(st))
	defer w.Close()
	w.Resize(fyne.NewSize(600, 300))
	st.Resize(fyne.NewSize(160, st.MinSize().Height))
	st.Resize(fyne.NewSize(160, st.MinSize().Height))
	if len(st.rows) < 3 {
		t.Fatalf("the text wrapped into %d lines at 160 points", len(st.rows))
	}
	want := "PGs have been inactive in pool ‹name›. Inactive placement groups serve no requests."
	if st.shown != want {
		t.Fatalf("shown %q", st.shown)
	}

	at := absolutePos(st)
	h := st.r.lineHeight()
	press := at.AddXY(0, h/2)
	end := at.AddXY(st.Size().Width+30, st.Size().Height+30) // past the end, beside the text
	st.MouseDown(&desktop.MouseEvent{Button: desktop.MouseButtonPrimary})
	strayDrag(st, press, at.AddXY(40, h/2), end)
	w.Canvas().Unfocus() // the driver, at the release beside the text
	st.DragEnd()
	if got := st.SelectedText(); got != want {
		t.Fatalf("the drag selected %q", got)
	}
	st.TypedShortcut(&fyne.ShortcutCopy{})
	if copied != want {
		t.Errorf("⌘C copied %q", copied)
	}
	lit := 0
	for _, hl := range st.r.hls {
		if hl.Visible() {
			lit++
		}
	}
	if lit != len(st.rows) {
		t.Errorf("%d of %d lines show the selection", lit, len(st.rows))
	}

	st.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(st.xAt(0, 4)+2, h/2)}) // in "have"
	if got := st.SelectedText(); got != "have" {
		t.Errorf("the double click selected %q", got)
	}
	st.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if st.SelectedText() != "" {
		t.Error("Escape kept the selection")
	}
}

// A card's info line wraps in a narrow column (the owner's QW10): its units and links never
// overlap, none runs past the width, no line starts or ends with a dot, and the links sit at the
// right, on a line of their own when the last one has no room for them.
func TestCardInfoLineWraps(t *testing.T) {
	test.NewTempApp(t)
	box := func(w float32) fyne.CanvasObject {
		r := canvas.NewRectangle(color.White)
		r.SetMinSize(fyne.NewSize(w, 14))
		return r
	}
	units := []fyne.CanvasObject{box(90), box(150), box(70)}
	dots := []fyne.CanvasObject{box(5), box(5)}
	links := box(100)
	objs := append(append(append([]fyne.CanvasObject(nil), units...), dots...), links)
	l := &infoLayout{units: len(units), links: true}
	c := container.New(l, objs...)

	for _, width := range []float32{600, 330, 250, 160} {
		c.Resize(fyne.NewSize(width, 20))
		c.Resize(fyne.NewSize(width, c.MinSize().Height))
		type rect struct{ x1, y1, x2, y2 float32 }
		var shown []rect
		lineStart := map[float32]float32{}
		for _, o := range objs {
			if !o.Visible() {
				continue
			}
			p, s := o.Position(), o.Size()
			r := rect{p.X, p.Y, p.X + s.Width, p.Y + s.Height}
			if r.x2 > width+0.5 {
				t.Errorf("width %v: an object ends at %v", width, r.x2)
			}
			for _, q := range shown {
				if r.x1 < q.x2 && q.x1 < r.x2 && r.y1 < q.y2 && q.y1 < r.y2 {
					t.Errorf("width %v: %v overlaps %v", width, r, q)
				}
			}
			shown = append(shown, r)
			if x, ok := lineStart[p.Y]; !ok || p.X < x {
				lineStart[p.Y] = p.X
			}
		}
		for i, d := range dots {
			if !d.Visible() {
				continue
			}
			if d.Position().X <= lineStart[d.Position().Y] || units[i+1].Position().Y != d.Position().Y {
				t.Errorf("width %v: dot %d starts or ends a line", width, i)
			}
		}
		if links.Position().X+links.Size().Width < width-0.5 {
			t.Errorf("width %v: the links end at %v, not at the right", width, links.Position().X+links.Size().Width)
		}
		if width == 600 && c.MinSize().Height > 20 {
			t.Errorf("width 600: %v high, want one line", c.MinSize().Height)
		}
	}
}

// A card's head keeps the copy icon's room while the icon hides (the owner's QW9): the name, the
// count and the head's height stay put when the pointer comes onto the head and the icon shows,
// and the head's tooltip ("Fold the card") does not open where the icon is.
func TestCardHeadKeepsTheIconsRoom(t *testing.T) {
	s := alertEngine(t)
	sh, w, q := newEngineShell(t, s, Options{})
	b := openBoard(t, sh, w, q)
	head := firstCardHead(t, b)
	type place struct {
		at   fyne.Position
		size fyne.Size
	}
	where := func() []place {
		var out []place
		for _, l := range findAll[*textLine](head) {
			out = append(out, place{absolutePos(l), l.Size()})
		}
		return append(out, place{absolutePos(head), head.Size()})
	}
	before := where()
	if head.icon == nil || head.icon.Visible() {
		t.Fatal("the head has no hidden copy icon")
	}
	var slot *fyne.Container // the hidden icon has no place in the window; its slot has
	for _, c := range findAll[*fyne.Container](head) {
		if _, ok := c.Layout.(iconSlot); ok {
			slot = c
		}
	}
	if slot == nil {
		t.Fatal("the copy icon has no slot")
	}
	iconAt := absolutePos(slot).AddXY(slot.Size().Width/2, slot.Size().Height/2)
	head.MouseIn(&desktop.MouseEvent{PointEvent: fyne.PointEvent{AbsolutePosition: iconAt}})
	settle(w)
	if !head.icon.Visible() {
		t.Fatal("the copy icon does not show with the pointer on the head")
	}
	after := where()
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("a part of the head moved when the icon showed: %+v, then %+v", before[i], after[i])
		}
	}
	if sh.tip.owner == any(head) {
		t.Errorf("the head's tooltip %q opens over the copy icon", sh.tip.text)
	}
	head.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{AbsolutePosition: absolutePos(head).AddXY(2, 2)}})
	if sh.tip.owner != any(head) || !strings.Contains(sh.tip.text, "card") {
		t.Errorf("off the icon the head's tooltip is %q", sh.tip.text)
	}
}
