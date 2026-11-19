package fyneui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/model"
)

// Sorting a drawer table by its column titles (owner: a node's Pods sort by Name, Namespace, CPU,
// Memory and Status). A sortable table (model.SortableCellTable) draws its titles as the lists'
// headers do (bold, muted, the arrow of the sorted column in the accent colour: tableview.go's
// headerCell), and a click on one dispatches ui.sortDrawerTable: the order is the engine's
// (UIState.DrawerSort), which comes back sorted in the drawer's model with DetailTable.Sort for the
// arrow. As in the lists, a click on the sorted column reverses it.

// drawerSortParams are ui.sortDrawerTable's parameters.
type drawerSortParams struct {
	Section string `json:"section"`
	Column  string `json:"column"`
	Desc    bool   `json:"desc"`
}

// sortHeader returns the header of a sortable table's column i, titled title.
func (d *drawer) sortHeader(section string, t *model.DetailTable, i int, title string) fyne.CanvasObject {
	id := t.IDs[i]
	sort := 0
	if s := t.Sort; s != nil && s.Column == id {
		sort = 1
		if s.Desc {
			sort = -1
		}
	}
	return newTableSortHeader(title, sort, func() {
		cur := t.Sort
		desc := cur != nil && cur.Column == id && !cur.Desc
		d.sh.dispatch("ui.sortDrawerTable", drawerSortParams{Section: section, Column: id, Desc: desc}, nil)
	})
}

// tableSortHeader is a drawer table's column title that sorts the table when clicked: the title in
// the lists' header style and, on the sorted column, the arrow up (ascending) or down. It asks for
// the room of its title and arrow; narrower, the title shortens with an ellipsis.
type tableSortHeader struct {
	widget.BaseWidget
	title string
	sort  int // 1 ascending, -1 descending, 0 not sorted
	onTap func()
}

func newTableSortHeader(title string, sort int, onTap func()) *tableSortHeader {
	h := &tableSortHeader{title: title, sort: sort, onTap: onTap}
	h.ExtendBaseWidget(h)
	return h
}

// Tapped implements fyne.Tappable.
func (h *tableSortHeader) Tapped(*fyne.PointEvent) {
	if h.onTap != nil {
		h.onTap()
	}
}

// CreateRenderer implements fyne.Widget.
func (h *tableSortHeader) CreateRenderer() fyne.WidgetRenderer {
	r := &tableSortHeaderRenderer{h: h, text: canvas.NewText("", color.Transparent), arrow: newIconImage()}
	r.Refresh()
	return r
}

type tableSortHeaderRenderer struct {
	h     *tableSortHeader
	text  *canvas.Text
	arrow *iconImage
}

// arrowRoom is the width the arrow takes after the title: half a padding and the icon; none on a
// column that is not sorted.
func (r *tableSortHeaderRenderer) arrowRoom() float32 {
	if r.h.sort == 0 {
		return 0
	}
	th := r.h.Theme()
	return th.Size(theme.SizeNamePadding)/2 + th.Size(sizeSortIcon)
}

func (r *tableSortHeaderRenderer) Layout(size fyne.Size) {
	th := r.h.Theme()
	room := r.arrowRoom()
	shown := ellipsize(r.h.title, max(size.Width-room, 0), r.text.TextSize, r.text.TextStyle)
	if shown != r.text.Text {
		r.text.Text = shown
		r.text.Refresh()
	}
	ts := r.text.MinSize()
	r.text.Move(fyne.NewPos(0, (size.Height-ts.Height)/2))
	r.text.Resize(ts)
	is := th.Size(sizeSortIcon)
	r.arrow.img.Move(fyne.NewPos(min(ts.Width, max(size.Width-room, 0))+th.Size(theme.SizeNamePadding)/2, (size.Height-is)/2))
	r.arrow.img.Resize(fyne.NewSquareSize(is))
}

func (r *tableSortHeaderRenderer) MinSize() fyne.Size {
	w := fyne.MeasureText(r.h.title, r.text.TextSize, r.text.TextStyle).Width
	h := fyne.MeasureText("Mg", r.text.TextSize, r.text.TextStyle).Height
	return fyne.NewSize(w+r.arrowRoom(), h)
}

func (r *tableSortHeaderRenderer) Refresh() {
	h := r.h
	th := h.Theme()
	r.text.Text = h.title
	r.text.TextSize = th.Size(theme.SizeNameText)
	r.text.TextStyle = fyne.TextStyle{Bold: true}
	r.text.Color = themeColor(th, colorMuted)
	var arrow fyne.Resource
	switch h.sort {
	case 1:
		arrow = theme.MenuDropUpIcon()
	case -1:
		arrow = theme.MenuDropDownIcon()
	}
	r.arrow.set(th, arrow, colorAccent)
	r.Layout(h.Size())
	r.text.Refresh()
}

func (r *tableSortHeaderRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.text, r.arrow.img}
}

func (r *tableSortHeaderRenderer) Destroy() {}
