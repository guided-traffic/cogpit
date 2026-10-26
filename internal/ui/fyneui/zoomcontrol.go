package fyneui

import (
	"image/color"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The status bar's zoom (D-134), right of Diagnostics (the owner, 2026-10-26: "eine Lupe mit + und
// eine Lupe mit - in der Statusleiste rechts von Diagnostics"; of three looks he chose − 120% +):
// the magnifier with − (ui.zoom.out), the zoom in percent, which a click sets back to 100%
// (ui.zoom.reset), and the magnifier with + (ui.zoom.in). The percent shows always, at one width
// for every level, so that neither magnifier moves under the pointer while it is clicked again and
// again. A magnifier at the end of the range is greyed, the reason its tooltip. VS Code shows a
// magnifier only while a window's zoom is not the default, and a click opens these controls.

// zoomControl is the status bar's zoom.
type zoomControl struct {
	out, in *statusIcon
	pct     *zoomPercent
	root    fyne.CanvasObject
}

func newZoomControl(sh *shell) *zoomControl {
	z := &zoomControl{
		out: newStatusIcon(sh, theme.ZoomOutIcon(), sh.zoomOut),
		in:  newStatusIcon(sh, theme.ZoomInIcon(), sh.zoomIn),
		pct: newZoomPercent(sh),
	}
	z.root = container.New(&rowLayout{}, z.out, z.pct, z.in)
	z.set(sh.zoom.level, engine.MaxZoomLevel)
	return z
}

// set shows the zoom level: the percent, and each magnifier greyed at its end of the range, the +
// also where the window is too small for the next level (ceiling, zoom.go).
func (z *zoomControl) set(level, ceiling int) {
	sh := z.pct.sh
	z.pct.set(level)
	z.out.set(commandTip(sh, "ui.zoom.out", level <= engine.MinZoomLevel, engine.ErrZoomMin))
	why := engine.ErrZoomMax
	if level < engine.MaxZoomLevel {
		why = engine.ZoomFitError(level + 1)
	}
	z.in.set(commandTip(sh, "ui.zoom.in", level >= min(ceiling, engine.MaxZoomLevel), why))
}

// commandTip is a status bar control's tooltip, its command's label and shortcut, and whether it is
// greyed: then the reason follows.
func commandTip(sh *shell, id string, off bool, why error) (string, bool) {
	tip := id
	if c, ok := sh.eng.Registry().Get(id); ok {
		tip = c.Label
		if sc := shortcutText(c.Shortcut, runtime.GOOS == goosMac); sc != "" {
			tip += " (" + sc + ")"
		}
	}
	if off {
		tip += ": " + why.Error()
	}
	return tip, off
}

// rowLayout lays its objects out side by side at their minimum widths, at the row's full height.
type rowLayout struct{}

func (rowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var s fyne.Size
	for _, o := range objs {
		m := o.MinSize()
		s.Width += m.Width
		s.Height = max(s.Height, m.Height)
	}
	return s
}

func (rowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	x := float32(0)
	for _, o := range objs {
		w := o.MinSize().Width
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(w, size.Height))
		x += w
	}
}

// statusIcon is an icon the status bar's zoom clicks, drawn as the MCP item's gear: muted, the
// hover's plate and the foreground under the pointer, greyed when off.
type statusIcon struct {
	widget.BaseWidget
	sh      *shell
	icon    fyne.Resource
	tapped  func()
	tip     string
	off     bool
	hovered bool
}

var (
	_ fyne.Tappable      = (*statusIcon)(nil)
	_ desktop.Hoverable  = (*statusIcon)(nil)
	_ desktop.Cursorable = (*statusIcon)(nil)
)

func newStatusIcon(sh *shell, icon fyne.Resource, tapped func()) *statusIcon {
	i := &statusIcon{sh: sh, icon: icon, tapped: tapped}
	i.ExtendBaseWidget(i)
	return i
}

// set changes the tooltip and whether the icon is off.
func (i *statusIcon) set(tip string, off bool) {
	if tip == i.tip && off == i.off {
		return
	}
	i.tip, i.off = tip, off
	i.Refresh()
	if i.sh.tip != nil && i.sh.tip.owner == i { // a shown tooltip follows
		i.sh.tip.hover(i, i.tip, i.sh.tip.pos)
	}
}

// Tapped implements fyne.Tappable.
func (i *statusIcon) Tapped(*fyne.PointEvent) {
	if !i.off {
		i.tapped()
	}
}

// Cursor implements desktop.Cursorable.
func (i *statusIcon) Cursor() desktop.Cursor {
	if i.off {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

// MouseIn implements desktop.Hoverable.
func (i *statusIcon) MouseIn(ev *desktop.MouseEvent) {
	i.hovered = true
	i.Refresh()
	i.sh.tip.hover(i, i.tip, ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (i *statusIcon) MouseMoved(ev *desktop.MouseEvent) { i.sh.tip.moved(i, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (i *statusIcon) MouseOut() {
	i.hovered = false
	i.Refresh()
	i.sh.tip.hideFor(i)
}

// CreateRenderer implements fyne.Widget.
func (i *statusIcon) CreateRenderer() fyne.WidgetRenderer {
	r := &statusIconRenderer{i: i, bg: canvas.NewRectangle(color.Transparent), img: newIconImage()}
	r.Refresh()
	return r
}

type statusIconRenderer struct {
	i   *statusIcon
	bg  *canvas.Rectangle
	img *iconImage
}

// icon is the icon's size: the MCP gear's, a little more than the status bar's text.
func (r *statusIconRenderer) icon() float32 { return r.i.Theme().Size(sizeStatusText) + 3 }

func (r *statusIconRenderer) MinSize() fyne.Size {
	is := r.icon()
	pad := r.i.Theme().Size(theme.SizeNamePadding)
	return fyne.NewSize(is+pad, is+pad/2)
}

func (r *statusIconRenderer) Layout(size fyne.Size) {
	is := r.icon()
	h := r.MinSize().Height
	r.bg.Move(fyne.NewPos(0, (size.Height-h)/2))
	r.bg.Resize(fyne.NewSize(size.Width, h))
	r.img.img.Move(fyne.NewPos((size.Width-is)/2, (size.Height-is)/2))
	r.img.img.Resize(fyne.NewSize(is, is))
}

func (r *statusIconRenderer) Refresh() {
	th := r.i.Theme()
	r.bg.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
	r.bg.FillColor = color.Transparent
	c := colorMuted
	switch {
	case r.i.off:
		c = theme.ColorNameDisabled
	case r.i.hovered:
		r.bg.FillColor = themeColor(th, theme.ColorNameHover)
		c = theme.ColorNameForeground
	}
	r.img.set(th, r.i.icon, c)
	r.Layout(r.i.Size())
	r.bg.Refresh()
}

func (r *statusIconRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.img.img}
}

func (r *statusIconRenderer) Destroy() {}

// zoomPercent is the zoom in percent between the magnifiers: a click resets it to 100%.
type zoomPercent struct {
	widget.BaseWidget
	sh      *shell
	level   int
	tip     string
	hovered bool
}

var (
	_ fyne.Tappable      = (*zoomPercent)(nil)
	_ desktop.Hoverable  = (*zoomPercent)(nil)
	_ desktop.Cursorable = (*zoomPercent)(nil)
)

func newZoomPercent(sh *shell) *zoomPercent {
	p := &zoomPercent{sh: sh}
	p.ExtendBaseWidget(p)
	return p
}

// set shows a zoom level.
func (p *zoomPercent) set(level int) {
	tip, _ := commandTip(p.sh, "ui.zoom.reset", false, nil)
	if level == 0 {
		tip = "Zoom: 100%"
	}
	if level == p.level && tip == p.tip {
		return
	}
	p.level, p.tip = level, tip
	p.Refresh()
	if p.sh.tip != nil && p.sh.tip.owner == p {
		p.sh.tip.hover(p, p.tip, p.sh.tip.pos)
	}
}

// Tapped implements fyne.Tappable.
func (p *zoomPercent) Tapped(*fyne.PointEvent) {
	if p.level != 0 {
		p.sh.dispatch("ui.zoom.reset", struct{}{}, nil)
	}
}

// Cursor implements desktop.Cursorable.
func (p *zoomPercent) Cursor() desktop.Cursor {
	if p.level == 0 {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

// MouseIn implements desktop.Hoverable.
func (p *zoomPercent) MouseIn(ev *desktop.MouseEvent) {
	p.hovered = true
	p.Refresh()
	p.sh.tip.hover(p, p.tip, ev.AbsolutePosition)
}

// MouseMoved implements desktop.Hoverable.
func (p *zoomPercent) MouseMoved(ev *desktop.MouseEvent) { p.sh.tip.moved(p, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (p *zoomPercent) MouseOut() {
	p.hovered = false
	p.Refresh()
	p.sh.tip.hideFor(p)
}

// CreateRenderer implements fyne.Widget.
func (p *zoomPercent) CreateRenderer() fyne.WidgetRenderer {
	r := &zoomPercentRenderer{p: p, bg: canvas.NewRectangle(color.Transparent), text: canvas.NewText("", nil)}
	r.Refresh()
	return r
}

type zoomPercentRenderer struct {
	p    *zoomPercent
	bg   *canvas.Rectangle
	text *canvas.Text
}

// width is the widest percent's: every level's text takes it, so the magnifiers stay put.
func (r *zoomPercentRenderer) width() float32 {
	th := r.p.Theme()
	var w float32
	for l := engine.MinZoomLevel; l <= engine.MaxZoomLevel; l++ {
		w = max(w, fyne.MeasureText(engine.ZoomPercent(l), th.Size(sizeStatusText), fyne.TextStyle{}).Width)
	}
	return w + 2*th.Size(theme.SizeNamePadding)
}

func (r *zoomPercentRenderer) MinSize() fyne.Size {
	th := r.p.Theme()
	h := fyne.MeasureText("0", th.Size(sizeStatusText), fyne.TextStyle{}).Height
	return fyne.NewSize(r.width(), h)
}

func (r *zoomPercentRenderer) Layout(size fyne.Size) {
	th := r.p.Theme()
	h := th.Size(sizeStatusText) + 3 + th.Size(theme.SizeNamePadding)/2 // the magnifiers' plate
	r.bg.Move(fyne.NewPos(0, (size.Height-h)/2))
	r.bg.Resize(fyne.NewSize(size.Width, h))
	m := r.text.MinSize()
	r.text.Move(fyne.NewPos((size.Width-m.Width)/2, (size.Height-m.Height)/2))
	r.text.Resize(m)
}

func (r *zoomPercentRenderer) Refresh() {
	th := r.p.Theme()
	r.bg.CornerRadius = th.Size(theme.SizeNameSelectionRadius)
	r.bg.FillColor = color.Transparent
	r.text.Text = engine.ZoomPercent(r.p.level)
	r.text.TextSize = th.Size(sizeStatusText)
	r.text.Color = themeColor(th, colorMuted)
	if r.p.hovered && r.p.level != 0 {
		r.bg.FillColor = themeColor(th, theme.ColorNameHover)
		r.text.Color = themeColor(th, theme.ColorNameForeground)
	}
	r.Layout(r.p.Size())
	r.bg.Refresh()
	r.text.Refresh()
}

func (r *zoomPercentRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bg, r.text}
}

func (r *zoomPercentRenderer) Destroy() {}
