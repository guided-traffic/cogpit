package fyneui

import (
	"fmt"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/model"
)

// table renders a section's table. A table of cells (model.CellTable: a node's pods, an endpoint
// slice's endpoints) draws its cells as the lists do: a link in the link colour that goes to its
// object, a status in its colour, and a warning column as the warning icon with the reason as its
// tooltip, under no title. A sortable one (a node's Pods) has the lists' column titles, which sort
// it (drawertable_sort.go). A table whose last column shows under its rows (LastUnder: a release's
// History and its descriptions) is laid out by underTableLayout. Other tables are plain text
// (detailTable). section is the section's title, which names the table to the engine.
func (d *drawer) table(ref model.ObjectRef, section string, t *model.DetailTable) fyne.CanvasObject {
	if len(t.Cells) == 0 || len(t.Columns) == 0 {
		return d.detailTable(t)
	}
	colType := func(i int) model.ColumnType {
		if i < len(t.Types) {
			return t.Types[i]
		}
		return model.ColumnText
	}
	if t.LastUnder && len(t.Columns) > 2 {
		return d.underTable(ref, t, colType, nil)
	}
	// The cells go into the grid at once: Container.Add lays the grid out on every call, which
	// for a node's hundreds of pods measures every cell hundreds of times.
	objs := make([]fyne.CanvasObject, 0, len(t.Columns)*(len(t.Cells)+1))
	for i, c := range t.Columns {
		if colType(i) == model.ColumnWarning {
			c = ""
		}
		if t.Sortable && i < len(t.IDs) {
			objs = append(objs, d.sortHeader(section, t, i, c))
			continue
		}
		objs = append(objs, d.line(c, colorMuted, theme.SizeNameText, fyne.TextStyle{}, true, ""))
	}
	for r, row := range t.Cells {
		for i := range t.Columns {
			cell, text := tableCellAt(t, row, r, i)
			objs = append(objs, d.tableCell(ref, colType(i), cell, text))
		}
	}
	grid := box(&detailTableLayout{cols: len(t.Columns)}, objs...)
	if t.Omitted <= 0 {
		return grid
	}
	// A long table's rest (engine.MaxDrawerTableRows, D-40): counted under its first rows.
	more := fmt.Sprintf("%d more %s not shown", t.Omitted, strings.ToLower(section))
	return container.NewVBox(grid, d.line(more, colorMuted, theme.SizeNameText, fyne.TextStyle{Italic: true}, false, ""))
}

// tableCell draws one cell of a table of cells.
func (d *drawer) tableCell(ref model.ObjectRef, typ model.ColumnType, c model.Cell, text string) fyne.CanvasObject {
	switch {
	case typ == model.ColumnWarning && c.Status != model.StatusNone:
		return newTipIcon(d.sh, theme.WarningIcon(), statusColorName(c.Status), text)
	case typ == model.ColumnWarning:
		text = ""
	case c.Link != nil && text != "" && d.sh.view.serves(c.Link.Kind):
		link := *c.Link
		l := newLinkText(text, func() { d.sh.followLink(link, ref.Context, false) })
		d.hookLink(l, text)
		return l
	}
	// A healthy word is plain text, as in the lists; a state that asks for attention keeps its
	// colour. It is not a pill here: the table's columns shrink to the drawer's width and cut a
	// text off with an ellipsis, which a pill cannot do.
	fg := theme.ColorNameForeground
	if c.Status != model.StatusNone && !healthyStatus(c.Status) {
		fg = statusColorName(c.Status)
	}
	return d.tableCellText(text, fg)
}

// tableCellAt returns row r's cell in column i of a table of cells, and its text: the row's text,
// which the engine wrote times into, else the cell's own.
func tableCellAt(t *model.DetailTable, row []model.Cell, r, i int) (model.Cell, string) {
	var cell model.Cell
	if i < len(row) {
		cell = row[i]
	}
	text := cell.Text
	if r < len(t.Rows) && i < len(t.Rows[r]) {
		text = t.Rows[r][i]
	}
	return cell, text
}

// underTable renders a LastUnder table: its other columns as a table of cells, and under each row
// the last column's text on a plate across the table, as a Config Map's value lies under its key
// (the owner, 2026-10-25: "so wie die Values in der ConfigMap, nicht als code-Block"): the input
// background, the text in the plain font wrapped onto lines within it (wrapHard), a failed state's
// in its colour; each line's copy icon copies the whole text. The last column has no title: the
// text under a row says what it is ("Upgrade complete"). rowLink, when set, makes each row's first
// cell a link that runs what it returns for the row (a release's revision opens its Revisions tab).
func (d *drawer) underTable(ref model.ObjectRef, t *model.DetailTable, colType func(int) model.ColumnType, rowLink func(row int) func()) fyne.CanvasObject {
	n := len(t.Columns) - 1
	th := d.sh.app.Settings().Theme()
	size := th.Size(theme.SizeNameText)
	width := max(d.contentWidth()-2*th.Size(theme.SizeNameInnerPadding), 0)
	cells := make([]fyne.CanvasObject, 0, n*(len(t.Cells)+1))
	for i := range n {
		cells = append(cells, d.line(t.Columns[i], colorMuted, theme.SizeNameText, fyne.TextStyle{}, true, ""))
	}
	under := make([]fyne.CanvasObject, 0, len(t.Cells))
	for r, row := range t.Cells {
		for i := range n {
			cell, text := tableCellAt(t, row, r, i)
			if i == 0 && rowLink != nil {
				if tap := rowLink(r); tap != nil {
					cells = append(cells, newLinkText(text, tap))
					continue
				}
			}
			cells = append(cells, d.tableCell(ref, colType(i), cell, text))
		}
		cell, text := tableCellAt(t, row, r, n)
		fg := theme.ColorNameForeground
		if cell.Status != model.StatusNone && !healthyStatus(cell.Status) {
			fg = statusColorName(cell.Status)
		}
		v := container.NewVBox()
		for _, l := range wrapHard(text, width, size, fyne.TextStyle{}) {
			v.Add(d.line(l, fg, theme.SizeNameText, fyne.TextStyle{}, true, text))
		}
		under = append(under, container.NewStack(newSurface(theme.ColorNameInputBackground),
			inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, v)))
	}
	l := &underTableLayout{grid: detailTableLayout{cols: n}, cells: cells, under: under}
	return box(l, append(slices.Clone(cells), under...)...)
}

// underTableLayout lays out a LastUnder table: the header and the rows' cells as
// detailTableLayout does, and each row's plate right under it across the table's width, a wider gap
// before the next row (keyedLayout's gaps: a Config Map's keys and values).
type underTableLayout struct {
	grid  detailTableLayout
	cells []fyne.CanvasObject // the header's titles, then the rows' cells, grid.cols each
	under []fyne.CanvasObject // the text under each row
}

func (l *underTableLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	_, rowH, _, rowGap := l.grid.widths(l.cells)
	inner, between := keyedLayout{}.gaps()
	h := float32(0)
	for i, rh := range rowH {
		h += rh
		if i > 0 && i-1 < len(l.under) {
			h += inner + l.under[i-1].MinSize().Height + between
		} else {
			h += rowGap
		}
	}
	return fyne.NewSize(0, h)
}

func (l *underTableLayout) Layout(_ []fyne.CanvasObject, size fyne.Size) {
	w, rowH, gap, rowGap := l.grid.widths(l.cells)
	total := gap * float32(l.grid.cols-1)
	for _, x := range w {
		total += x
	}
	if total > size.Width && total > 0 {
		capWidths(w, fyne.Max(size.Width-gap*float32(l.grid.cols-1), 0))
	}
	inner, between := keyedLayout{}.gaps()
	y := float32(0)
	for row := range rowH {
		x := float32(0)
		for col := range l.grid.cols {
			if i := row*l.grid.cols + col; i < len(l.cells) {
				o := l.cells[i]
				o.Move(fyne.NewPos(x, y))
				o.Resize(fyne.NewSize(w[col], o.MinSize().Height))
			}
			x += w[col] + gap
		}
		y += rowH[row]
		if row == 0 || row-1 >= len(l.under) {
			y += rowGap
			continue
		}
		u := l.under[row-1]
		y += inner
		u.Move(fyne.NewPos(0, y))
		u.Resize(fyne.NewSize(size.Width, u.MinSize().Height))
		y += u.MinSize().Height + between
	}
}

// tipIcon is an icon in a theme colour with a tooltip: the warning icon of a drawer table's row,
// its reason as the tooltip. It is as high as a line of text, so that it does not stretch the row.
type tipIcon struct {
	widget.BaseWidget
	sh    *shell
	res   fyne.Resource
	color fyne.ThemeColorName
	tip   string
}

func newTipIcon(sh *shell, res fyne.Resource, color fyne.ThemeColorName, tip string) *tipIcon {
	i := &tipIcon{sh: sh, res: res, color: color, tip: tip}
	i.ExtendBaseWidget(i)
	return i
}

// MouseIn implements desktop.Hoverable.
func (i *tipIcon) MouseIn(ev *desktop.MouseEvent) { i.sh.tip.hover(i, i.tip, ev.AbsolutePosition) }

// MouseMoved implements desktop.Hoverable.
func (i *tipIcon) MouseMoved(ev *desktop.MouseEvent) { i.sh.tip.moved(i, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (i *tipIcon) MouseOut() { i.sh.tip.hideFor(i) }

// CreateRenderer implements fyne.Widget.
func (i *tipIcon) CreateRenderer() fyne.WidgetRenderer {
	img := canvas.NewImageFromResource(themedIcon(i.res, i.color))
	img.FillMode = canvas.ImageFillContain
	return &tipIconRenderer{i: i, img: img}
}

type tipIconRenderer struct {
	i   *tipIcon
	img *canvas.Image
}

func (r *tipIconRenderer) side() float32 {
	return fyne.MeasureText("Mg", r.i.Theme().Size(theme.SizeNameText), fyne.TextStyle{}).Height
}

func (r *tipIconRenderer) Layout(size fyne.Size) {
	s := fyne.Min(r.side(), fyne.Min(size.Width, size.Height))
	r.img.Move(fyne.NewPos(0, (size.Height-s)/2))
	r.img.Resize(fyne.NewSquareSize(s))
}

func (r *tipIconRenderer) MinSize() fyne.Size { return fyne.NewSquareSize(r.side()) }

func (r *tipIconRenderer) Refresh() {
	r.img.Resource = themedIcon(r.i.res, r.i.color)
	r.img.Refresh()
}

func (r *tipIconRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.img} }
func (r *tipIconRenderer) Destroy()                     {}
