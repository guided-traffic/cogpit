package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// The segmented control of the Overview (Master | Worker), drawn for the Dark Cockpit: one dark
// plate with a hairline, the chosen segment a plate one step lighter with a 2 px underline in the
// glow blue and the normal text colour, the other segment muted and flat. The segments stay the
// tipButtons they were (the engine's choice sets their Importance, the tooltips and the disabled
// state work as before); only their theme says how they look, so no solid blue fills the page.

// segmentTheme dresses one segment's button: the app's theme with the colours of a chosen or an
// unchosen segment.
type segmentTheme struct {
	app    fyne.App
	chosen bool
}

func (t *segmentTheme) cur() fyne.Theme { return t.app.Settings().Theme() }

func (t *segmentTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNamePrimary: // the chosen segment's plate
		if t.chosen {
			return t.cur().Color(colorRowSelected, v)
		}
	case theme.ColorNameForegroundOnPrimary:
		return t.cur().Color(colorRowSelectedText, v)
	case theme.ColorNameButton:
		return color.Transparent
	case theme.ColorNameForeground:
		if !t.chosen {
			return t.cur().Color(colorMuted, v)
		}
	}
	return t.cur().Color(n, v)
}
func (t *segmentTheme) Font(s fyne.TextStyle) fyne.Resource     { return t.cur().Font(s) }
func (t *segmentTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return t.cur().Icon(n) }
func (t *segmentTheme) Size(n fyne.ThemeSizeName) float32       { return t.cur().Size(n) }

// segmented is the two segments on their plate.
type segmented struct {
	root   *fyne.Container
	themes [2]*container.ThemeOverride
	st     [2]*segmentTheme
	under  *canvas.Rectangle
	lay    *segmentLayout
}

// newSegmented puts the two buttons on a plate. choose(i) says which is chosen (-1 for none); call
// refresh after it changes.
func newSegmented(sh *shell, a, b fyne.CanvasObject) *segmented {
	s := &segmented{under: canvas.NewRectangle(color.Transparent)}
	plate := canvas.NewRectangle(color.Transparent)
	plate.CornerRadius = 8
	plate.StrokeWidth = 1
	s.under.CornerRadius = 1
	for i, o := range []fyne.CanvasObject{a, b} {
		s.st[i] = &segmentTheme{app: sh.app}
		s.themes[i] = container.NewThemeOverride(o, s.st[i])
	}
	s.lay = &segmentLayout{plate: plate, under: s.under}
	s.root = container.New(s.lay, plate, s.themes[0], s.themes[1], s.under)
	return s
}

// choose marks the chosen segment (-1: none) and repaints the plate for the current theme.
func (s *segmented) choose(i int) {
	th := fyne.CurrentApp().Settings().Theme()
	v := fyne.CurrentApp().Settings().ThemeVariant()
	s.lay.chosen = i
	for j := range s.st {
		s.st[j].chosen = j == i
		s.themes[j].Refresh()
	}
	s.lay.plate.FillColor = th.Color(theme.ColorNameInputBackground, v)
	s.lay.plate.StrokeColor = th.Color(theme.ColorNameInputBorder, v)
	s.under.FillColor = th.Color(theme.ColorNamePrimary, v)
	s.under.Hidden = i < 0
	s.root.Refresh()
}

// segmentLayout lays the plate under two buttons side by side, with a small margin, and the
// underline below the chosen one.
type segmentLayout struct {
	plate  *canvas.Rectangle
	under  *canvas.Rectangle
	chosen int
}

const segmentMargin = 2

func (l *segmentLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	a, b := objs[1].MinSize(), objs[2].MinSize()
	return fyne.NewSize(a.Width+b.Width+2*segmentMargin, fyne.Max(a.Height, b.Height)+2*segmentMargin)
}

func (l *segmentLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	objs[0].Move(fyne.NewPos(0, 0))
	objs[0].Resize(size)
	a, b := objs[1].MinSize(), objs[2].MinSize()
	h := size.Height - 2*segmentMargin
	objs[1].Move(fyne.NewPos(segmentMargin, segmentMargin))
	objs[1].Resize(fyne.NewSize(a.Width, h))
	objs[2].Move(fyne.NewPos(segmentMargin+a.Width, segmentMargin))
	objs[2].Resize(fyne.NewSize(b.Width, h))
	x, w := float32(segmentMargin), a.Width
	if l.chosen == 1 {
		x, w = segmentMargin+a.Width, b.Width
	}
	const bar = 2
	objs[3].Move(fyne.NewPos(x+10, size.Height-segmentMargin-bar))
	objs[3].Resize(fyne.NewSize(fyne.Max(w-20, 0), bar))
}

// gapOf is vertical empty space of h points: the rhythm between a group and the head of the next
// (24 points above each bracket head, so that a group does not look glued to the cards above it).
func gapOf(h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(0, h))
	return r
}

// groupGap is the space above a bracket head that follows content.
const groupGap = 20
