package fyneui

import (
	"fmt"
	"math"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

// selectWindow shows a dropdown with options, selected chosen, 200 points below the top of a
// window of the given height; changed records every OnChanged.
func selectWindow(t *testing.T, options []string, selected string, height float32) (*selectField, fyne.Window, *[]string) {
	t.Helper()
	test.NewTempApp(t).Settings().SetTheme(newTheme(ThemeDark))
	var changed []string
	s := newSelectField(options, func(v string) { changed = append(changed, v) })
	if selected != "" {
		s.SetSelected(selected)
		changed = nil
	}
	spacer := newSpace("")
	w := test.NewTempWindow(t, container.NewVBox(box(widthLayout{width: 1}, spacer), container.NewPadded(container.NewHBox(s))))
	w.SetPadded(false)
	w.Resize(fyne.NewSize(500, height))
	spacer.Resize(fyne.NewSize(1, 200))
	w.Content().Refresh()
	s.Move(fyne.NewPos(40, 200))
	s.Resize(fyne.NewSize(160, s.MinSize().Height))
	return s, w, &changed
}

func samePos(a, b float32) bool { return math.Abs(float64(a-b)) < 0.51 }

// TestSelectOpensOverTheField: the list opens over the field, its current entry at the field's
// place, its text where the field's text is, marked with the check, every entry once (GW5, the
// owner: the current one under the field again was "verwirrend").
func TestSelectOpensOverTheField(t *testing.T) {
	s, w, _ := selectWindow(t, []string{"alpha", "bravo", "charlie", "delta", "echo"}, "charlie", 600)
	test.Tap(s)
	if !s.isOpen() {
		t.Fatal("the list did not open")
	}
	drv := fyne.CurrentApp().Driver()
	field := drv.AbsolutePositionForObject(s)
	p := s.pop
	if len(p.rows) != 5 {
		t.Fatalf("%d rows, want every entry once", len(p.rows))
	}
	for i, r := range p.rows {
		if r.current != (i == 2) {
			t.Errorf("row %d (%s) current %v", i, r.text, r.current)
		}
	}
	row := drv.AbsolutePositionForObject(p.rows[2])
	_, _, textX, _ := selectInsets()
	if !samePos(row.Y, field.Y) || !samePos(p.rows[2].Size().Height, s.Size().Height) {
		t.Errorf("the current row at y %v, height %v; the field at y %v, height %v", row.Y, p.rows[2].Size().Height, field.Y, s.Size().Height)
	}
	if !samePos(row.X+textX, field.X+fieldTextX()) {
		t.Errorf("the row's text at x %v, the field's at %v", row.X+textX, field.X+fieldTextX())
	}
	above := drv.AbsolutePositionForObject(p.rows[0])
	if above.Y >= field.Y || !p.popup.Visible() || w.Canvas().Overlays().Top() == nil {
		t.Errorf("the first row at %v, not above the field at %v", above.Y, field.Y)
	}
	if p.active != 2 {
		t.Errorf("the highlighted row %d, want the current one", p.active)
	}
}

// TestSelectKeys: Space opens the list, ↓ and ↑ move, Return chooses (OnChanged as Fyne's did),
// Escape closes without a choice, a letter jumps, ← and → step without the list; the field has the
// keyboard again after the list closes.
func TestSelectKeys(t *testing.T) {
	s, w, changed := selectWindow(t, []string{"alpha", "bravo", "charlie", "delta", "echo"}, "bravo", 600)
	c := w.Canvas()
	c.Focus(s)
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if !s.isOpen() || c.Focused() != s.pop {
		t.Fatalf("Space: open %v, focus %v", s.isOpen(), c.Focused())
	}
	key := func(k fyne.KeyName) { c.Focused().TypedKey(&fyne.KeyEvent{Name: k}) }
	key(fyne.KeyDown)
	key(fyne.KeyDown)
	key(fyne.KeyUp)
	if s.pop.active != 2 {
		t.Fatalf("↓↓↑ from bravo: row %d", s.pop.active)
	}
	key(fyne.KeyReturn)
	if s.isOpen() || s.Selected != "charlie" || fmt.Sprint(*changed) != "[charlie]" || c.Focused() != s {
		t.Fatalf("Return: open %v, selected %q, changed %v, focus %v", s.isOpen(), s.Selected, *changed, c.Focused())
	}
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown}) // ↓ opens, as in Fyne's
	c.Focused().TypedRune('e')
	if s.pop.active != 4 {
		t.Errorf("e: row %d, want echo", s.pop.active)
	}
	key(fyne.KeyHome)
	if s.pop.active != 0 {
		t.Errorf("Home: row %d", s.pop.active)
	}
	key(fyne.KeyEscape)
	if s.isOpen() || s.Selected != "charlie" || len(*changed) != 1 || c.Focused() != s {
		t.Fatalf("Escape: open %v, selected %q, changed %v", s.isOpen(), s.Selected, *changed)
	}
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyRight})
	if s.isOpen() || s.Selected != "delta" {
		t.Errorf("→: open %v, selected %q", s.isOpen(), s.Selected)
	}
}

// TestSelectMouse: a click on a row chooses it; a click outside the list closes it, and it opens
// again; a disabled field does not open.
func TestSelectMouse(t *testing.T) {
	s, w, changed := selectWindow(t, []string{"alpha", "bravo", "charlie"}, "alpha", 600)
	test.Tap(s)
	test.Tap(s.pop.rows[1])
	if s.isOpen() || s.Selected != "bravo" || fmt.Sprint(*changed) != "[bravo]" {
		t.Fatalf("click on bravo: open %v, selected %q, changed %v", s.isOpen(), s.Selected, *changed)
	}
	test.Tap(s)
	test.TapCanvas(w.Canvas(), fyne.NewPos(480, 20)) // outside the list
	if s.isOpen() || s.Selected != "bravo" {
		t.Fatalf("a click outside: open %v, selected %q", s.isOpen(), s.Selected)
	}
	test.Tap(s)
	if !s.isOpen() {
		t.Fatal("the list does not open again after a click outside")
	}
	s.close()
	s.Disable()
	test.Tap(s)
	if s.isOpen() {
		t.Error("a disabled field opened its list")
	}
}

// TestSelectWithoutChoice: a field showing its placeholder opens with the first entry at its place,
// no check anywhere.
func TestSelectWithoutChoice(t *testing.T) {
	s, _, _ := selectWindow(t, []string{"alpha", "bravo"}, "", 600)
	test.Tap(s)
	drv := fyne.CurrentApp().Driver()
	if !samePos(drv.AbsolutePositionForObject(s.pop.rows[0]).Y, drv.AbsolutePositionForObject(s).Y) {
		t.Error("the first row is not at the field")
	}
	for _, r := range s.pop.rows {
		if r.current {
			t.Errorf("%s checked without a choice", r.text)
		}
	}
}

// TestSelectLongList: a list taller than the window fits in it and scrolls, the current entry
// still at the field's place (a chart's hundreds of versions), and the keys scroll with the
// highlight.
func TestSelectLongList(t *testing.T) {
	var opts []string
	for i := range 200 {
		opts = append(opts, fmt.Sprintf("0.%d.0", 199-i))
	}
	s, w, _ := selectWindow(t, opts, opts[150], 500)
	test.Tap(s)
	p := s.pop
	_, margin, _, _ := selectInsets()
	if h := p.popup.Size().Height; h > w.Canvas().Size().Height-2*margin+0.5 {
		t.Fatalf("the list is %v tall in a window of %v", h, w.Canvas().Size().Height)
	}
	drv := fyne.CurrentApp().Driver()
	if p.scroll.Offset.Y <= 0 || !samePos(drv.AbsolutePositionForObject(p.rows[150]).Y, drv.AbsolutePositionForObject(s).Y) {
		t.Fatalf("offset %v; row at %v, field at %v", p.scroll.Offset.Y, drv.AbsolutePositionForObject(p.rows[150]).Y, drv.AbsolutePositionForObject(s).Y)
	}
	p.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnd})
	top, bottom := p.scroll.Offset.Y, p.scroll.Offset.Y+p.scroll.Size().Height
	if y := p.rows[199].Position().Y; y < top || y+p.rowH > bottom+0.5 {
		t.Errorf("End: the last row at %v, the view %v to %v", y, top, bottom)
	}
}

// TestSelectClosesWhenItsOptionsChange: the list closes when the field gets other entries.
func TestSelectClosesWhenItsOptionsChange(t *testing.T) {
	s, _, _ := selectWindow(t, []string{"alpha", "bravo"}, "alpha", 600)
	test.Tap(s)
	s.Options = []string{"alpha", "bravo", "charlie"}
	s.Refresh()
	if s.isOpen() {
		t.Error("the list stayed open with other entries")
	}
}
