package fyneui

import (
	"image/color"
	"slices"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Velero page's three tables (velero.go): simple read-only tables of the page, not the
// window's list (one per page, the views' search, sort and selection): a header row in the
// header colour, a row per entry with a hairline under it, the pointer's row lit, a click on a row
// opening its object's drawer, and a footer line. Agents read the same rows through
// velero.dashboard.

// pageColNamespace is the Namespace column's title.
const pageColNamespace = "Namespace"

// pageCol is one column of a page table: its title, its width (0: it takes what is left), and
// whether its cells are numbers, right-aligned.
type pageCol struct {
	title string
	width float32
	num   bool
}

// pageCell is one cell: its text in a colour, bold or not, or a canvas object of its own; tip,
// when set, is its tooltip (a message cut off in the cell, whole).
type pageCell struct {
	text string
	tone fyne.ThemeColorName
	bold bool
	obj  fyne.CanvasObject
	tip  string
}

func pageCellOf(text string, tone fyne.ThemeColorName) pageCell {
	return pageCell{text: text, tone: tone}
}

// pageQuiet is a cell in the muted colour, pageFg one in the foreground colour, pageDash "—" muted.
func pageQuiet(text string) pageCell { return pageCellOf(text, colorMuted) }
func pageFg(text string) pageCell    { return pageCellOf(text, theme.ColorNameForeground) }

var pageDash = pageQuiet("—")

// pageTable builds a table of rows; onTap[i] runs on a click on row i; footer is the line under
// it ("" for none); tips shows the cells' tooltips.
func pageTable(cols []pageCol, rows [][]pageCell, onTap []func(), footer string, tips *tooltip) fyne.CanvasObject {
	widths := &pageWidths{cols: cols}
	head := make([]fyne.CanvasObject, 0, len(cols))
	for i, c := range cols {
		t := newTextLine(c.title, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
		t.truncate = true
		if c.num {
			t.align = fyne.TextAlignTrailing
		}
		cell := inset(sizeCellPadding, "", t)
		if i < len(cols)-1 {
			head = append(head, container.NewBorder(nil, nil, nil, newDivider(true), cell))
		} else {
			head = append(head, cell)
		}
	}
	objs := []fyne.CanvasObject{container.NewStack(newSurface(theme.ColorNameHeaderBackground),
		container.New(&pageRowLayout{w: widths}, head...))}
	for r, row := range rows {
		cells := make([]fyne.CanvasObject, 0, len(cols))
		var tipped []*pageTipCell
		for i, c := range row {
			obj := pageCellObject(c, i < len(cols) && cols[i].num)
			if c.tip != "" && tips != nil {
				tc := newPageTipCell(obj, c.tip, tips)
				tipped = append(tipped, tc)
				obj = tc
			}
			cells = append(cells, obj)
		}
		var tap func()
		if r < len(onTap) {
			tap = onTap[r]
		}
		pr := newPageRow(container.New(&pageRowLayout{w: widths}, cells...), tap)
		for _, tc := range tipped {
			tc.row = pr
		}
		objs = append(objs, pr)
	}
	if footer != "" {
		f := newTextLine(footer, theme.ColorNamePlaceHolder, theme.SizeNameText, fyne.TextStyle{})
		f.truncate = true
		objs = append(objs, inset(sizeCellPadding, sizeRowPadding, f))
	}
	return container.New(&pageStackLayout{}, objs...)
}

func pageCellObject(c pageCell, num bool) fyne.CanvasObject {
	if c.obj != nil {
		return inset(sizeCellPadding, "", container.NewHBox(c.obj))
	}
	t := newTextLine(c.text, c.tone, theme.SizeNameText, fyne.TextStyle{Bold: c.bold})
	t.truncate = true
	if num {
		t.align = fyne.TextAlignTrailing
	}
	return inset(sizeCellPadding, "", t)
}

// pageWidths shares a table's column widths between its rows: each fixed column at its width, the
// flexible ones sharing what is left; when the fixed ones do not fit, all give way in proportion.
type pageWidths struct {
	cols []pageCol
	at   float32
	ws   []float32
}

// pageFlexMin is the least a flexible column keeps.
const pageFlexMin = 80

func (p *pageWidths) of(total float32) []float32 {
	if p.ws != nil && p.at == total {
		return p.ws
	}
	var fixed float32
	flex := 0
	for _, c := range p.cols {
		if c.width > 0 {
			fixed += c.width
		} else {
			flex++
		}
	}
	scale := float32(1)
	if avail := total - float32(flex)*pageFlexMin; fixed > avail && fixed > 0 {
		scale = max(0, avail) / fixed
	}
	left := total - fixed*scale
	ws := make([]float32, len(p.cols))
	for i, c := range p.cols {
		if c.width > 0 {
			ws[i] = c.width * scale
		} else {
			ws[i] = max(0, left/float32(flex))
		}
	}
	p.at, p.ws = total, ws
	return ws
}

// pageRowLayout lays a row's cells out in the table's columns, each as high as the row.
type pageRowLayout struct{ w *pageWidths }

func (l *pageRowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var h float32
	for _, o := range objs {
		h = max(h, o.MinSize().Height)
	}
	pad := fyne.CurrentApp().Settings().Theme().Size(sizeRowPadding)
	return fyne.NewSize(0, h+2*pad)
}

func (l *pageRowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	ws := l.w.of(size.Width)
	x := float32(0)
	for i, o := range objs {
		if i >= len(ws) {
			o.Hide()
			continue
		}
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(ws[i], size.Height))
		x += ws[i]
	}
}

// pageStackLayout stacks a table's rows at their heights over its width; it asks for no width of
// its own, the page decides it.
type pageStackLayout struct{}

func (pageStackLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var h float32
	for _, o := range objs {
		h += o.MinSize().Height
	}
	return fyne.NewSize(0, h)
}

func (pageStackLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	y := float32(0)
	for _, o := range objs {
		h := o.MinSize().Height
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h
	}
}

// pageRow is a table row: lit while the pointer is on it, a hairline under it, clickable when it
// has something to open.
type pageRow struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
	lit     *surface
	hovered bool
}

func newPageRow(content fyne.CanvasObject, onTap func()) *pageRow {
	r := &pageRow{content: content, onTap: onTap, lit: newSurface(colorRowHover)}
	r.lit.Hide()
	r.ExtendBaseWidget(r)
	return r
}

// Tapped implements fyne.Tappable.
func (r *pageRow) Tapped(*fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap()
	}
}

// Cursor implements desktop.Cursorable.
func (r *pageRow) Cursor() desktop.Cursor {
	if r.onTap == nil {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

// MouseIn implements desktop.Hoverable.
func (r *pageRow) MouseIn(*desktop.MouseEvent) { r.setLit(true) }

// MouseMoved implements desktop.Hoverable.
func (r *pageRow) MouseMoved(*desktop.MouseEvent) {}

// MouseOut implements desktop.Hoverable.
func (r *pageRow) MouseOut() { r.setLit(false) }

func (r *pageRow) setLit(on bool) {
	if on == r.hovered || r.onTap == nil {
		return
	}
	r.hovered = on
	if on {
		r.lit.Show()
	} else {
		r.lit.Hide()
	}
}

// CreateRenderer implements fyne.Widget.
func (r *pageRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(r.lit, container.NewBorder(nil, newDivider(false), nil, nil, r.content)))
}

// attentionTable is Needs attention: Problem in its colour, the object, its scope, since when, the
// detail; a row opens its object's drawer.
func (p *veleroPanel) attentionTable(rows []engine.VeleroAttention) fyne.CanvasObject {
	cols := []pageCol{{title: "Problem", width: 170}, {title: "Object", width: 300}, {title: "Scope", width: 170},
		{title: "Since", width: 64}, {title: "Detail"}}
	now := p.sh.now()
	cells := make([][]pageCell, 0, len(rows))
	taps := make([]func(), 0, len(rows))
	for _, a := range rows {
		since := pageDash
		if !a.Since.IsZero() {
			since = pageFg(model.FormatAge(now.Sub(a.Since)))
		}
		cells = append(cells, []pageCell{
			{text: a.Problem, tone: calmStatusColor(a.Status), bold: true}, pageFg(a.Object), pageQuiet(a.Scope), since, pageQuiet(a.Detail),
		})
		ref := a.Ref
		taps = append(taps, func() { p.open(ref) })
	}
	return pageTable(cols, cells, taps, "", p.sh.tip)
}

// schedulesTable is Schedules: a row per schedule with its last runs as dots.
func (p *veleroPanel) schedulesTable(rows []engine.VeleroSchedule) fyne.CanvasObject {
	cols := []pageCol{{title: "Schedule", width: 140}, {title: "When", width: 115}, {title: "Next run", width: 84},
		{title: "Last runs", width: 112}, {title: "Last completed", width: 128}, {title: "Duration", width: 86},
		{title: "Scope", width: 170}, {title: "TTL", width: 52}, {title: "Location", width: 90}, {title: "State"}}
	now := p.sh.now()
	cells := make([][]pageCell, 0, len(rows))
	taps := make([]func(), 0, len(rows))
	for _, s := range rows {
		next := pageDash
		if !s.NextRun.IsZero() {
			next = pageFg("in " + model.FormatAge(s.NextRun.Sub(now)))
		}
		last := pageDash
		if !s.LastCompleted.IsZero() {
			last = pageFg(model.FormatAge(now.Sub(s.LastCompleted)) + " ago")
		}
		problem := s.Status == model.StatusError || s.Status == model.StatusWarning
		cells = append(cells, []pageCell{
			pageFg(s.Ref.Name), pageFg(s.When), next, {obj: newVeleroDots(s.Runs)}, last, orDash(s.Duration), pageQuiet(s.Scope),
			orDash(s.TTL), orDash(s.Location), {text: s.State, tone: calmStatusColor(s.Status), bold: problem},
		})
		ref := s.Ref
		taps = append(taps, func() { p.open(ref) })
	}
	return pageTable(cols, cells, taps, veleroDotsFooter, p.sh.tip)
}

// outsideTable is Outside every schedule: the namespaces, their PVCs and workloads, the GitOps tool
// that manages them, and why they are outside; a row opens the namespace's drawer.
func (p *veleroPanel) outsideTable(rows []engine.VeleroOutside) fyne.CanvasObject {
	cols := []pageCol{{title: pageColNamespace, width: 180}, {title: "PVCs", width: 64, num: true}, {title: "Workloads", width: 94, num: true},
		{title: "Managed by", width: 136}, {title: "Reason"}}
	cells := make([][]pageCell, 0, len(rows))
	taps := make([]func(), 0, len(rows))
	for _, o := range rows {
		cells = append(cells, []pageCell{
			pageFg(o.Ref.Name), pageFg(strconv.Itoa(o.PVCs)), pageFg(strconv.Itoa(o.Workloads)), orDash(o.ManagedBy), pageQuiet(o.Reason),
		})
		ref := o.Ref
		taps = append(taps, func() { p.open(ref) })
	}
	return pageTable(cols, cells, taps, veleroOutsideFooter, p.sh.tip)
}

// volumesTable is Failed volume backups: a row per namespace and reason, its volumes, pods and
// nodes, and the first one's message, whole in its tooltip; a row opens its first volume backup's
// drawer.
func (p *veleroPanel) volumesTable(rows []engine.VeleroVolumeGroup) fyne.CanvasObject {
	cols := []pageCol{{title: pageColNamespace, width: 180}, {title: "Volumes", width: 92, num: true}, {title: "Pods", width: 64, num: true},
		{title: "Node", width: 170}, {title: "Message"}}
	cells := make([][]pageCell, 0, len(rows))
	taps := make([]func(), 0, len(rows))
	for _, g := range rows {
		cells = append(cells, []pageCell{
			pageFg(g.Namespace), pageFg(strconv.Itoa(g.Volumes)), pageFg(strconv.Itoa(g.Pods)), orDash(shortNodes(g.Nodes)),
			{text: g.Message, tone: colorMuted, tip: g.Message},
		})
		ref := g.Ref
		taps = append(taps, func() { p.open(ref) })
	}
	return pageTable(cols, cells, taps, "", p.sh.tip)
}

// shortNodes writes nodes short: each one's first name part, at most three, then how many more
// ("worker-01, worker-02 +3").
func shortNodes(nodes []string) string {
	var out []string
	for _, n := range nodes {
		short, _, _ := strings.Cut(n, ".")
		if !slices.Contains(out, short) {
			out = append(out, short)
		}
	}
	text := strings.Join(out[:min(3, len(out))], ", ")
	if more := len(out) - 3; more > 0 {
		text += " +" + strconv.Itoa(more)
	}
	return text
}

// pageTipCell is a cell with a tooltip; it lights its row as the row itself does under the pointer.
type pageTipCell struct {
	widget.BaseWidget
	content fyne.CanvasObject
	text    string
	tips    *tooltip
	row     *pageRow
}

func newPageTipCell(content fyne.CanvasObject, text string, tips *tooltip) *pageTipCell {
	c := &pageTipCell{content: content, text: text, tips: tips}
	c.ExtendBaseWidget(c)
	return c
}

// MouseIn implements desktop.Hoverable.
func (c *pageTipCell) MouseIn(ev *desktop.MouseEvent) {
	if c.row != nil {
		c.row.setLit(true)
	}
	c.tips.hover(c, c.text, ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (c *pageTipCell) MouseMoved(ev *desktop.MouseEvent) { c.tips.moved(c, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (c *pageTipCell) MouseOut() {
	if c.row != nil {
		c.row.setLit(false)
	}
	c.tips.hideFor(c)
}

// Cursor implements desktop.Cursorable: the row's.
func (c *pageTipCell) Cursor() desktop.Cursor {
	if c.row != nil {
		return c.row.Cursor()
	}
	return desktop.DefaultCursor
}

// CreateRenderer implements fyne.Widget.
func (c *pageTipCell) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.content)
}

// orDash is a foreground cell, or a muted dash for nothing.
func orDash(text string) pageCell {
	if text == "" {
		return pageDash
	}
	return pageFg(text)
}

// veleroDots is a schedule's last runs: a dot each, oldest left; completed grey, partially failed
// amber, failed red, running a muted ring, a missed run a red ring.
type veleroDots struct {
	widget.BaseWidget
	runs []engine.VeleroRun
}

func newVeleroDots(runs []engine.VeleroRun) *veleroDots {
	d := &veleroDots{runs: runs}
	d.ExtendBaseWidget(d)
	return d
}

// Dot metrics.
const (
	veleroDot    = 9
	veleroDotGap = 4
)

// The states of a schedule's runs (engine.VeleroRun).
const (
	veleroRunPartiallyFailed  = "partiallyFailed"
	veleroRunFailed           = "failed"
	veleroRunFailedValidation = "failedValidation"
	veleroRunRunning          = "running"
	veleroRunMissed           = "missed"
)

// veleroDotLook is a run's colour and whether it is a ring.
func veleroDotLook(state string) (fyne.ThemeColorName, bool) {
	switch state {
	case veleroRunPartiallyFailed:
		return calmStatusColor(model.StatusWarning), false
	case veleroRunFailed, veleroRunFailedValidation:
		return calmStatusColor(model.StatusError), false
	case veleroRunRunning:
		return colorMuted, true
	case veleroRunMissed:
		return calmStatusColor(model.StatusError), true
	}
	return theme.ColorNameDisabled, false
}

// CreateRenderer implements fyne.Widget.
func (d *veleroDots) CreateRenderer() fyne.WidgetRenderer {
	r := &veleroDotsRenderer{d: d}
	for range d.runs {
		r.dots = append(r.dots, canvas.NewCircle(color.Transparent))
	}
	r.Refresh()
	return r
}

type veleroDotsRenderer struct {
	d    *veleroDots
	dots []*canvas.Circle
}

func (r *veleroDotsRenderer) MinSize() fyne.Size {
	n := float32(len(r.dots))
	return fyne.NewSize(max(0, n*veleroDot+(n-1)*veleroDotGap), veleroDot)
}

func (r *veleroDotsRenderer) Layout(size fyne.Size) {
	y := (size.Height - veleroDot) / 2
	for i, c := range r.dots {
		c.Move(fyne.NewPos(float32(i)*(veleroDot+veleroDotGap), y))
		c.Resize(fyne.NewSize(veleroDot, veleroDot))
	}
}

func (r *veleroDotsRenderer) Refresh() {
	th := r.d.Theme()
	for i, c := range r.dots {
		tone, ring := veleroDotLook(r.d.runs[i].State)
		col := themeColor(th, tone)
		if ring {
			c.FillColor, c.StrokeColor, c.StrokeWidth = color.Transparent, col, 1.5
		} else {
			c.FillColor, c.StrokeColor, c.StrokeWidth = col, color.Transparent, 0
		}
		c.Refresh()
	}
	r.Layout(r.d.Size())
}

func (r *veleroDotsRenderer) Objects() []fyne.CanvasObject {
	out := make([]fyne.CanvasObject, len(r.dots))
	for i, c := range r.dots {
		out[i] = c
	}
	return out
}
func (r *veleroDotsRenderer) Destroy() {}
