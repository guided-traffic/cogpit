package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// navItem is an item of a settings page's list of sections, drawn with the design's selection (the
// shared look of docs/developer/design-language.md): the selected item has the selection fill, a
// two-point bar in the glow blue at its inner left edge and the selection's text colour; the item
// under the pointer has the hover plate. A solid primary button, as the list had, shouted where the
// design asks for one blue mark. Text and Importance keep the names of the button it replaced:
// Importance is HighImportance for the shown section, and the page's tests read it.
type navItem struct {
	widget.BaseWidget
	Text       string
	Importance widget.Importance // HighImportance: the section shown
	OnTapped   func()
	hover      bool
}

var (
	_ fyne.Tappable      = (*navItem)(nil)
	_ desktop.Hoverable  = (*navItem)(nil)
	_ desktop.Cursorable = (*navItem)(nil)
)

func newNavItem(text string, tapped func()) *navItem {
	n := &navItem{Text: text, OnTapped: tapped}
	n.ExtendBaseWidget(n)
	return n
}

func (n *navItem) selected() bool { return n.Importance == widget.HighImportance }

// Tapped implements fyne.Tappable.
func (n *navItem) Tapped(*fyne.PointEvent) {
	if n.OnTapped != nil {
		n.OnTapped()
	}
}

// Cursor implements desktop.Cursorable.
func (n *navItem) Cursor() desktop.Cursor { return desktop.PointerCursor }

// MouseIn implements desktop.Hoverable.
func (n *navItem) MouseIn(*desktop.MouseEvent) { n.hover = true; n.Refresh() }

// MouseMoved implements desktop.Hoverable.
func (n *navItem) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (n *navItem) MouseOut() { n.hover = false; n.Refresh() }

// CreateRenderer implements fyne.Widget.
func (n *navItem) CreateRenderer() fyne.WidgetRenderer {
	r := &navItemRenderer{n: n, plate: canvas.NewRectangle(color.Transparent), bar: canvas.NewRectangle(color.Transparent),
		txt: canvas.NewText("", color.Transparent)}
	r.Refresh()
	return r
}

type navItemRenderer struct {
	n          *navItem
	plate, bar *canvas.Rectangle
	txt        *canvas.Text
}

func (r *navItemRenderer) MinSize() fyne.Size {
	th := r.n.Theme()
	ts := fyne.MeasureText("Mg"+r.n.Text, th.Size(theme.SizeNameText), fyne.TextStyle{})
	return fyne.NewSize(ts.Width+4*th.Size(theme.SizeNameInnerPadding), ts.Height+2*th.Size(theme.SizeNameInnerPadding))
}

func (r *navItemRenderer) Layout(size fyne.Size) {
	th := r.n.Theme()
	pad := th.Size(theme.SizeNameInnerPadding)
	r.plate.Move(fyne.NewPos(0, 0))
	r.plate.Resize(size)
	r.bar.Move(fyne.NewPos(2, 7))
	r.bar.Resize(fyne.NewSize(2, max(size.Height-14, 0)))
	ts := r.txt.MinSize()
	r.txt.Move(fyne.NewPos(2*pad, (size.Height-ts.Height)/2))
	r.txt.Resize(ts)
}

func (r *navItemRenderer) Refresh() {
	th := r.n.Theme()
	r.plate.CornerRadius = th.Size(theme.SizeNameInputRadius)
	r.bar.CornerRadius = 1
	switch {
	case r.n.selected():
		r.plate.FillColor = themeColor(th, colorRowSelected)
		r.bar.FillColor = themeColor(th, theme.ColorNamePrimary)
		r.txt.Color = themeColor(th, colorRowSelectedText)
	case r.n.hover:
		r.plate.FillColor = themeColor(th, theme.ColorNameHover)
		r.bar.FillColor = color.Transparent
		r.txt.Color = themeColor(th, theme.ColorNameForeground)
	default:
		r.plate.FillColor, r.bar.FillColor = color.Transparent, color.Transparent
		r.txt.Color = themeColor(th, theme.ColorNameForeground)
	}
	r.txt.Text, r.txt.TextSize = r.n.Text, th.Size(theme.SizeNameText)
	r.txt.TextStyle = fyne.TextStyle{Bold: r.n.selected()}
	r.Layout(r.n.Size())
	r.plate.Refresh()
	r.bar.Refresh()
	r.txt.Refresh()
}

func (r *navItemRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.plate, r.bar, r.txt}
}
func (r *navItemRenderer) Destroy() {}
