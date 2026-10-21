package fyneui

import (
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

// The owner's findings on Dark Cockpit's first release (2026-10-21): the welcome page's grid floor
// went away at the first scroll, the tables' header lost its column dividers, and the agent's input
// showed no cursor.

// TestWelcomeScrimCoversTheColumnOnly: the scrim that keeps the grid floor from running behind text
// lies behind the page's column only, so the floor shows beside it however far the page scrolls;
// it follows the page's height without a scroll (the first build worked it out from a stale height
// until the first scroll), and there is none when the content ends above the floor.
func TestWelcomeScrimCoversTheColumnOnly(t *testing.T) {
	test.NewTempApp(t).Settings().SetTheme(newTheme(ThemeDark))
	page := canvas.NewRectangle(color.Transparent)
	page.SetMinSize(fyne.NewSize(welcomeColumn, 200)) // a short page: its last line above the floor
	box := newWelcomeStage(page)
	w := test.NewTempWindow(t, box)
	w.SetPadded(false)
	w.Resize(fyne.NewSize(1440, 900))
	st := welcomeStageOf(box)
	if st.scrim.Visible() {
		t.Fatal("a page that ends above the floor: the scrim shows; want the whole floor")
	}

	page.SetMinSize(fyne.NewSize(welcomeColumn, 2000)) // the clusters came: the page outgrows the window
	st.relayout()
	pos, size := st.scrim.Position(), st.scrim.Size()
	if !st.scrim.Visible() {
		t.Fatal("a page past the window: no scrim; want one behind the column")
	}
	if pos.X <= 0 || pos.X+size.Width >= 1440 {
		t.Errorf("scrim spans x %v to %v of 1440; want the column with a margin, the floor beside it", pos.X, pos.X+size.Width)
	}
	if left, right := pos.X, 1440-(pos.X+size.Width); left != right {
		t.Errorf("scrim off centre: %v left, %v right", left, right)
	}
	if size.Width < welcomeColumn {
		t.Errorf("scrim %v wide; want at least the column's %v", size.Width, welcomeColumn)
	}
	floorTop := float32(900) * (1 - newHorizon().floor)
	if pos.Y > floorTop-welcomeScrimEdge || pos.Y+size.Height < 900 {
		t.Errorf("scrim from y %v to %v; want from the floor's soft edge (%v) to the window's bottom", pos.Y, pos.Y+size.Height, floorTop-welcomeScrimEdge)
	}

	st.scroll.ScrollToOffset(fyne.NewPos(0, 400))
	if !st.scrim.Visible() || st.scrim.Size().Width != size.Width {
		t.Errorf("after a scroll: scrim shown %v, %v wide; want it as before", st.scrim.Visible(), st.scrim.Size().Width)
	}
}

// TestWelcomeScrimFadesOnEverySide: the scrim's image is the stage's colour, opaque in its middle
// and clear at its left, right and top edges, and at its bottom when it ends above the window's.
func TestWelcomeScrimFadesOnEverySide(t *testing.T) {
	st := &welcomeStage{drawn: fyne.NewSize(680, 300), fadeDown: true, stage: color.NRGBA{R: 13, G: 18, B: 26, A: 255}}
	img := st.drawScrim(1360, 600)
	b := img.Bounds()
	alpha := func(x, y int) uint8 { return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA).A }
	if a := alpha(b.Dx()/2, b.Dy()/2); a != 255 {
		t.Errorf("middle alpha %d; want opaque", a)
	}
	for _, p := range []struct {
		name string
		x, y int
	}{{"left", 0, b.Dy() / 2}, {"right", b.Dx() - 1, b.Dy() / 2}, {"top", b.Dx() / 2, 0}, {"bottom", b.Dx() / 2, b.Dy() - 1}} {
		if a := alpha(p.x, p.y); a > 16 {
			t.Errorf("%s edge alpha %d; want nearly clear", p.name, a)
		}
	}
	st.fadeDown = false
	img = st.drawScrim(1360, 600)
	if a := color.NRGBAModel.Convert(img.At(b.Dx()/2, b.Dy()-1)).(color.NRGBA).A; a != 255 {
		t.Errorf("reaching the window's bottom: bottom alpha %d; want opaque", a)
	}
}

// TestHeaderColumnDividersShow: every column header draws its short divider at its right edge, in
// the ink of the hairline under the header row.
func TestHeaderColumnDividersShow(t *testing.T) {
	for _, mode := range []ThemeMode{ThemeDark, ThemeLight} {
		test.NewTempApp(t).Settings().SetTheme(newTheme(mode))
		h := newHeaderCell(nil)
		h.set(0, "Namespace", 0)
		w := test.NewTempWindow(t, container.NewWithoutLayout(h))
		h.Resize(fyne.NewSize(140, 30))
		_ = w
		th := h.Theme()
		if h.sep.FillColor != themeColor(th, colorBracket) || h.sep.FillColor == color.Transparent {
			t.Errorf("%s: divider %v; want the bracket's ink %v", mode, h.sep.FillColor, themeColor(th, colorBracket))
		}
		if h.sep.Size().Width <= 0 || h.sep.Size().Height <= 0 {
			t.Errorf("%s: divider %v; want a short line", mode, h.sep.Size())
		}
	}
}

// TestChatInputShowsACursor: the agent's input, focused, shows its text cursor, as wide as Fyne's
// input border, while the entry draws neither a border nor a focus ring of its own.
func TestChatInputShowsACursor(t *testing.T) {
	sh, _, q, _, _ := chatScene(t, ThemeDark, nil)
	in := sh.chat.input
	sh.win.Canvas().Focus(in.entry)
	q.drain()
	ft := &flatEntryTheme{app: sh.app}
	border := ft.Size(theme.SizeNameInputBorder)
	if border <= 0 {
		t.Fatalf("the input border is %v wide: the cursor, as wide, cannot show", border)
	}
	for _, n := range []fyne.ThemeColorName{theme.ColorNameInputBorder, theme.ColorNamePrimary, theme.ColorNameInputBackground} {
		if c := ft.Color(n, theme.VariantDark); c != color.Transparent {
			t.Errorf("flat entry's %s is %v; want transparent (the input's box draws them)", n, c)
		}
	}
	found := false
	for _, r := range rectanglesIn(in.entry) {
		if !r.Visible() || r.Size().Width != border || r.Size().Height < 10 {
			continue
		}
		if _, _, _, a := r.FillColor.RGBA(); a > 0 {
			found = true
		}
	}
	if !found {
		t.Error("focused agent input: no visible cursor")
	}
}

// rectanglesIn returns every rectangle o draws, through containers, scrolls (whose content
// test.LaidOutObjects leaves out) and widgets' renderers.
func rectanglesIn(o fyne.CanvasObject) (out []*canvas.Rectangle) {
	switch v := o.(type) {
	case *canvas.Rectangle:
		out = append(out, v)
	case *fyne.Container:
		for _, c := range v.Objects {
			out = append(out, rectanglesIn(c)...)
		}
	case *container.Scroll:
		out = append(out, rectanglesIn(v.Content)...)
	case fyne.Widget:
		for _, c := range test.WidgetRenderer(v).Objects() {
			out = append(out, rectanglesIn(c)...)
		}
	}
	return out
}
