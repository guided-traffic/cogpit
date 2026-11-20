package fyneui

import (
	"image/color"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Velero page in the window (D-new): a page under the navigator's Integrations, shown while the
// cluster serves Velero's Backups (veleroNav). Its header is the title, a muted line of what the
// cache tells of Velero and the namespace filter; its body replaces the table, as the alert board's
// does: the Status tiles, Needs attention, Failed volume backups (while there are any), Schedules and
// Outside every schedule, each under a muted section label, the tables drawn by the page itself
// (velero_tables.go), read-only. Colour only where action is needed (D-calm, calmStatusColor): red
// to act now, amber to check, everything else grey. The model is the engine's (velero.dashboard),
// the same an agent reads.

// veleroNav is the navigator's entry: a top-level page under Integrations, while the cluster serves
// Velero's Backups.
var veleroNav = func() navEntry {
	e := page(engine.VeleroDashboard)
	e.needs = engine.VeleroBackupsKey
	return e
}()

func init() {
	navIcons[engine.VeleroDashboard] = theme.DocumentSaveIcon // a save: backups
}

// The section labels and the tables' footers, as the owner's pick of the layout writes them.
const (
	veleroStatusLabel    = "STATUS"
	veleroAttentionLabel = "NEEDS ATTENTION"
	veleroVolumesLabel   = "FAILED VOLUME BACKUPS"
	veleroSchedulesLabel = "SCHEDULES"
	veleroOutsideLabel   = "OUTSIDE EVERY SCHEDULE"
	veleroInferred       = "inferred"
	veleroDotsFooter     = "Dots: last 7 runs, oldest left · ring: running · red ring: a run cron expected that was never created"
	veleroOutsideFooter  = "Stateful first · “in scope” is read from each schedule's namespaces, resources and selectors, never proof of a backup"
)

// veleroPanel is the page's body over the table's place, and its header line.
type veleroPanel struct {
	sh     *shell
	shown  bool
	headed bool // the header shows the page's line (tableView.veleroHeading)
	box    *fyne.Container
	scroll *container.Scroll
	root   *fyne.Container
	sub    *veleroSubLine
	head   *shrinkRowLayout // the header's layout while the page shows: the line gives way first
	data   *engine.VeleroDashboardModel

	// Requests of the model are coalesced, as the other pages' are (fluxdashboard.go).
	requests atomic.Int32
}

func newVeleroPanel(sh *shell) *veleroPanel {
	p := &veleroPanel{sh: sh, box: container.New(layout.NewCustomPaddedVBoxLayout(10))}
	p.sub = newVeleroSubLine()
	p.head = &shrinkRowLayout{floors: map[fyne.CanvasObject]float32{p.sub: 40}}
	p.scroll = newVScroll(inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, p.box))
	p.root = container.NewStack(newSurface(theme.ColorNameBackground), p.scroll)
	p.root.Hide()
	return p
}

// setShown shows the page's body over the table while its page is open.
func (p *veleroPanel) setShown(on bool) {
	if on == p.shown {
		return
	}
	p.shown = on
	if on {
		p.root.Show()
		return
	}
	p.root.Hide()
	p.data = nil
	p.box.Objects = nil
	p.sub.set("")
}

// request reads the model off the main goroutine and shows it; requests made while one is in
// flight become one more.
func (p *veleroPanel) request() {
	if !p.shown {
		return
	}
	for {
		n := p.requests.Load()
		if n >= 2 {
			return
		}
		if p.requests.CompareAndSwap(n, n+1) {
			if n == 1 {
				return
			}
			break
		}
	}
	p.sh.dispatch("velero.dashboard", struct{}{}, func(res any, err error) {
		again := p.requests.Swap(0) == 2
		var m engine.VeleroDashboardModel
		if err == nil && decode(res, &m) == nil && p.shown {
			p.show(&m)
		}
		// An error leaves the page as it was; the next change asks again.
		if again && p.shown {
			p.request()
		}
	})
}

// show makes the page show m; a model equal to the one shown keeps the widgets.
func (p *veleroPanel) show(m *engine.VeleroDashboardModel) {
	if p.data != nil && reflect.DeepEqual(p.data, m) && len(p.box.Objects) > 0 {
		return
	}
	p.data = m
	p.sub.set(veleroHeaderText(m.Header))
	var objs []fyne.CanvasObject
	if m.Error != "" {
		why := widget.NewLabel(m.Error) // quotes the server: wrapped
		why.Wrapping = fyne.TextWrapWord
		objs = append(objs, why)
	}
	switch {
	case !m.Served && m.Error == "":
		objs = append(objs, newTextLine("The cluster serves no Velero kinds.", colorMuted, theme.SizeNameText, fyne.TextStyle{}))
	case m.Served:
		objs = append(objs, p.sections(m)...)
	}
	p.box.Objects = objs
	p.box.Refresh()
	p.scroll.Refresh()
}

// sections are the page's four parts under their labels.
func (p *veleroPanel) sections(m *engine.VeleroDashboardModel) []fyne.CanvasObject {
	tiles := make([]fyne.CanvasObject, 0, len(m.Tiles))
	for _, t := range m.Tiles {
		tiles = append(tiles, newVeleroTile(p.sh, t))
	}
	// A section built from a kind the page could not read counts "—" and claims nothing: the
	// tiles say why (D-177). Needs attention reads every Velero kind, Schedules and Outside every
	// schedule the Schedules.
	anyUnread := len(m.Unread) > 0
	schedsUnread := slices.Contains(m.Unread, engine.VeleroSchedulesKey)
	count := func(n int, unread bool) string {
		if unread && n == 0 {
			return noCount
		}
		return strconv.Itoa(n)
	}
	objs := []fyne.CanvasObject{
		newVeleroSection(veleroStatusLabel, "", ""),
		container.New(layout.NewGridLayoutWithColumns(max(1, len(tiles))), tiles...),
		gapOf(4),
		newVeleroSection(veleroAttentionLabel, count(len(m.Attention), anyUnread), ""),
	}
	empty := func(text string) fyne.CanvasObject {
		if !m.Synced {
			text = "Loading…" // an empty section of kinds still being listed says nothing yet
		}
		return newTextLine(text, colorMuted, theme.SizeNameText, fyne.TextStyle{})
	}
	switch {
	case len(m.Attention) == 0 && anyUnread:
	case len(m.Attention) == 0:
		objs = append(objs, empty("Nothing needs attention."))
	default:
		objs = append(objs, p.attentionTable(m.Attention))
	}
	if len(m.FailedVolumes) > 0 { // the volumes behind the backups' errors; no section without them
		n := 0
		for _, g := range m.FailedVolumes {
			n += g.Volumes
		}
		objs = append(objs, gapOf(4), newVeleroSection(veleroVolumesLabel, strconv.Itoa(n), ""), p.volumesTable(m.FailedVolumes))
	}
	objs = append(objs, gapOf(4), newVeleroSection(veleroSchedulesLabel, count(len(m.Schedules), schedsUnread), ""))
	switch {
	case len(m.Schedules) == 0 && schedsUnread:
	case len(m.Schedules) == 0:
		objs = append(objs, empty("No schedules."))
	default:
		objs = append(objs, p.schedulesTable(m.Schedules))
	}
	objs = append(objs, gapOf(4), newVeleroSection(veleroOutsideLabel, count(len(m.Outside), schedsUnread), veleroInferred))
	switch {
	case m.OutsideError != "":
		objs = append(objs, newTextLine(m.OutsideError, colorMuted, theme.SizeNameText, fyne.TextStyle{}))
	case len(m.Outside) == 0 && schedsUnread:
	case len(m.Outside) == 0:
		objs = append(objs, empty("Every namespace is in scope of a schedule."))
	default:
		objs = append(objs, p.outsideTable(m.Outside))
	}
	return objs
}

// open opens a row's object's drawer beside the page, or a row about a kind its view.
func (p *veleroPanel) open(ref model.ObjectRef) {
	if ref.Name == "" {
		p.sh.dispatch("ui.openView", openViewParams{View: ref.Kind}, nil)
		return
	}
	p.sh.openDetails(ref)
}

// veleroHeaderText is the header's line: "1.17.1 · namespace velero · server 1/1 · node agents
// 6/6 · plugins aws 1.13.0, csi"; a part the model does not have is left out.
func veleroHeaderText(h engine.VeleroHeader) string {
	var parts []string
	if h.Version != "" {
		parts = append(parts, h.Version)
	}
	if h.Namespace != "" {
		parts = append(parts, "namespace "+h.Namespace)
	}
	ready := func(r *engine.VeleroReady) string { return strconv.Itoa(r.Ready) + "/" + strconv.Itoa(r.Desired) }
	if h.Server != nil {
		parts = append(parts, "server "+ready(h.Server))
	}
	if h.NodeAgents != nil {
		parts = append(parts, "node agents "+ready(h.NodeAgents))
	}
	if len(h.Plugins) > 0 {
		names := make([]string, 0, len(h.Plugins))
		for _, pl := range h.Plugins {
			names = append(names, strings.TrimSpace(pl.Name+" "+pl.Version))
		}
		parts = append(parts, "plugins "+strings.Join(names, ", "))
	}
	return strings.Join(parts, " · ")
}

// veleroHeading puts the page's line beside the title and leaves the namespace filter alone at the
// right while the Velero page shows, and gives the header its arrangement back after: the page's,
// which setPageLayout sets itself, or a view's. It runs before and after setPageLayout arranges a
// page or a view.
func (tv *tableView) veleroHeading(on bool) {
	p := tv.velero
	switch {
	case on:
		p.headed = true
		tv.heading.Layout = p.head
		tv.heading.Objects = []fyne.CanvasObject{tv.title, p.sub}
		tv.tools.Objects = []fyne.CanvasObject{tv.nsBox}
	case p.headed:
		p.headed = false
		tv.heading.Layout = layout.NewHBoxLayout()
		if !tv.pageMode {
			tv.heading.Objects = []fyne.CanvasObject{tv.title, tv.count, tv.csvButton, tv.syncing, tv.check.bar.root}
			tv.tools.Objects = []fyne.CanvasObject{tv.nsBox, tv.states.box, tv.searchBox}
		}
	default:
		return
	}
	tv.heading.Refresh()
	tv.tools.Refresh()
}

// veleroSubLine is the header's muted line after the title, on the title's baseline: as wide as its
// text, and cut with an ellipsis when the header has less room.
type veleroSubLine struct {
	widget.BaseWidget
	line *textLine
}

func newVeleroSubLine() *veleroSubLine {
	l := newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	l.truncate = true
	l.baselineOf = theme.SizeNameSubHeadingText
	s := &veleroSubLine{line: l}
	s.ExtendBaseWidget(s)
	return s
}

func (s *veleroSubLine) set(text string) {
	if text == s.line.text {
		return
	}
	s.line.SetText(text)
	s.Refresh()
}

// veleroSubLead is the room between the title and the line.
const veleroSubLead = 8

// CreateRenderer implements fyne.Widget.
func (s *veleroSubLine) CreateRenderer() fyne.WidgetRenderer { return &veleroSubRenderer{s: s} }

type veleroSubRenderer struct{ s *veleroSubLine }

func (r *veleroSubRenderer) MinSize() fyne.Size {
	if r.s.line.text == "" {
		return fyne.Size{}
	}
	th := r.s.Theme()
	w := fyne.MeasureText(r.s.line.text, th.Size(theme.SizeNameText), fyne.TextStyle{}).Width
	return fyne.NewSize(w+veleroSubLead+2, r.s.line.MinSize().Height)
}

func (r *veleroSubRenderer) Layout(size fyne.Size) {
	r.s.line.Move(fyne.NewPos(veleroSubLead, 0))
	r.s.line.Resize(fyne.NewSize(max(0, size.Width-veleroSubLead), size.Height))
}

func (r *veleroSubRenderer) Refresh()                     { r.s.line.Refresh() }
func (r *veleroSubRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.s.line} }
func (r *veleroSubRenderer) Destroy()                     {}

// veleroSection is a section's label as the layout writes it: in the muted colour, bold, small, its
// count after a middle dot in the fainter colour, a tag in a hairline box ("inferred"), and below a
// bracket over the section's width.
type veleroSection struct {
	widget.BaseWidget
	label, count, tag string
}

func newVeleroSection(label, count, tag string) *veleroSection {
	s := &veleroSection{label: label, count: count, tag: tag}
	s.ExtendBaseWidget(s)
	return s
}

// Section label metrics: the bracket's distance below the text and its ticks' height.
const (
	sectionLineGap = 5
	sectionTick    = 4
	sectionTagPad  = 5
)

// CreateRenderer implements fyne.Widget.
func (s *veleroSection) CreateRenderer() fyne.WidgetRenderer {
	r := &veleroSectionRenderer{s: s,
		label: canvas.NewText(s.label, color.Transparent), count: canvas.NewText("", color.Transparent),
		tag: canvas.NewText(s.tag, color.Transparent), tagBox: canvas.NewRectangle(color.Transparent),
		line: canvas.NewLine(color.Transparent), left: canvas.NewLine(color.Transparent), right: canvas.NewLine(color.Transparent)}
	if s.count != "" {
		r.count.Text = " · " + s.count
	}
	r.label.TextStyle = fyne.TextStyle{Bold: true}
	r.tag.TextStyle = fyne.TextStyle{Bold: true}
	r.Refresh()
	return r
}

type veleroSectionRenderer struct {
	s                 *veleroSection
	label, count, tag *canvas.Text
	tagBox            *canvas.Rectangle
	line, left, right *canvas.Line
}

func (r *veleroSectionRenderer) size() float32 { return r.s.Theme().Size(theme.SizeNameCaptionText) }

func (r *veleroSectionRenderer) MinSize() fyne.Size {
	ls := fyne.MeasureText(r.label.Text, r.size(), r.label.TextStyle)
	cs := fyne.MeasureText(r.count.Text, r.size(), fyne.TextStyle{})
	w := ls.Width + cs.Width
	if r.s.tag != "" {
		w += 2*sectionTagPad + fyne.MeasureText(r.s.tag, r.size(), r.tag.TextStyle).Width + 6
	}
	return fyne.NewSize(w, ls.Height+2+sectionLineGap+sectionTick)
}

func (r *veleroSectionRenderer) Layout(size fyne.Size) {
	ls := fyne.MeasureText(r.label.Text, r.size(), r.label.TextStyle)
	cs := fyne.MeasureText(r.count.Text, r.size(), fyne.TextStyle{})
	r.label.Move(fyne.NewPos(0, 1))
	r.label.Resize(ls)
	r.count.Move(fyne.NewPos(ls.Width, 1))
	r.count.Resize(cs)
	if r.s.tag != "" {
		ts := fyne.MeasureText(r.s.tag, r.size(), r.tag.TextStyle)
		x := ls.Width + cs.Width + 6
		r.tagBox.Move(fyne.NewPos(x, 0))
		r.tagBox.Resize(fyne.NewSize(ts.Width+2*sectionTagPad, ls.Height+2))
		r.tag.Move(fyne.NewPos(x+sectionTagPad, 1))
		r.tag.Resize(ts)
	}
	y := ls.Height + 2 + sectionLineGap
	r.line.Position1, r.line.Position2 = fyne.NewPos(0, y), fyne.NewPos(size.Width, y)
	r.left.Position1, r.left.Position2 = fyne.NewPos(0.5, y), fyne.NewPos(0.5, y+sectionTick)
	r.right.Position1, r.right.Position2 = fyne.NewPos(size.Width-0.5, y), fyne.NewPos(size.Width-0.5, y+sectionTick)
}

func (r *veleroSectionRenderer) Refresh() {
	th := r.s.Theme()
	for _, t := range []*canvas.Text{r.label, r.count, r.tag} {
		t.TextSize = r.size()
	}
	r.label.Color = themeColor(th, colorMuted)
	r.count.Color = themeColor(th, theme.ColorNamePlaceHolder)
	r.tag.Color = themeColor(th, colorMuted)
	r.tag.Hidden, r.tagBox.Hidden = r.s.tag == "", r.s.tag == ""
	r.tagBox.FillColor = color.Transparent
	r.tagBox.StrokeColor = themeColor(th, colorHairline)
	r.tagBox.StrokeWidth = 1
	r.tagBox.CornerRadius = 4
	for _, l := range []*canvas.Line{r.line, r.left, r.right} {
		l.StrokeColor, l.StrokeWidth = themeColor(th, colorBracket), 1
	}
	r.Layout(r.s.Size())
	canvas.Refresh(r.s)
}

func (r *veleroSectionRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.label, r.count, r.tagBox, r.tag, r.line, r.left, r.right}
}
func (r *veleroSectionRenderer) Destroy() {}

// newVeleroTile is a tile of Status: its title in the foreground colour, which opens its kind's
// view, its count, a thin bar of its states (grey, the idle ones darker, problems in their signal
// colour) and a line per state, the quiet ones muted, the problems in their colour with the count
// bold.
func newVeleroTile(sh *shell, t engine.VeleroTile) fyne.CanvasObject {
	kind := t.Kind
	title := newTapTarget(newTextLine(t.Title, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true}), func() {
		sh.dispatch("ui.openView", openViewParams{View: kind}, func(_ any, err error) { sh.showError(err) })
	})
	count := kindCount(t.Count, t.Synced, t.Error) // "—" for a kind it could not read (D-177)
	head := container.NewBorder(nil, nil, title, newTextLine(count, colorMuted, theme.SizeNameText, fyne.TextStyle{}))
	lines := container.New(layout.NewCustomPaddedVBoxLayout(2))
	for _, l := range t.Lines {
		lines.Add(veleroTileLine(l))
	}
	if t.Error != "" {
		e := newTextLine(t.Error, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
		e.truncate = true
		lines.Add(e)
	} else if len(t.Lines) == 0 {
		lines.Add(newTextLine("None", colorMuted, theme.SizeNameText, fyne.TextStyle{}))
	}
	plate := newSurface(theme.ColorNameInputBackground)
	plate.radius = theme.SizeNameSelectionRadius
	body := container.New(layout.NewCustomPaddedVBoxLayout(7), head, newVeleroBar(t.Lines), lines)
	return container.NewStack(plate, inset(sizeCellPadding, theme.SizeNameInnerPadding, body))
}

// veleroTileLine is one state of a tile: its count right-aligned in a column, then its label.
func veleroTileLine(l engine.VeleroCount) fyne.CanvasObject {
	tone := calmStatusColor(l.Status)
	problem := l.Status == model.StatusError || l.Status == model.StatusWarning
	n := newTextLine(formatCount(int64(l.Count)), tone, theme.SizeNameText, fyne.TextStyle{Bold: problem})
	n.align = fyne.TextAlignTrailing
	return container.New(&veleroCountLayout{}, n, newTextLine(l.Label, tone, theme.SizeNameText, fyne.TextStyle{}))
}

// veleroCountLayout puts a count in a column of at least veleroCountWidth, right-aligned, and the
// label after it.
type veleroCountLayout struct{}

const veleroCountWidth = 26

func (veleroCountLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	n, l := objs[0].MinSize(), objs[1].MinSize()
	return fyne.NewSize(max(n.Width, veleroCountWidth)+6+l.Width, max(n.Height, l.Height))
}

func (veleroCountLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	w := max(objs[0].MinSize().Width, veleroCountWidth)
	objs[0].Move(fyne.NewPos(0, 0))
	objs[0].Resize(fyne.NewSize(w, size.Height))
	objs[1].Move(fyne.NewPos(w+6, 0))
	objs[1].Resize(fyne.NewSize(max(0, size.Width-w-6), size.Height))
}

// veleroBar is a tile's thin bar: a segment per state as wide as its share, 2 points apart.
type veleroBar struct {
	widget.BaseWidget
	lines []engine.VeleroCount
}

func newVeleroBar(lines []engine.VeleroCount) *veleroBar {
	b := &veleroBar{lines: lines}
	b.ExtendBaseWidget(b)
	return b
}

// veleroBarTone is a segment's colour: a problem's signal colour, the slot colour for what is idle
// or under way, grey for the rest.
func veleroBarTone(s model.Status) fyne.ThemeColorName {
	switch s {
	case model.StatusError, model.StatusWarning:
		return calmStatusColor(s)
	case model.StatusPending:
		return colorSlotDim
	}
	return theme.ColorNameDisabled
}

// Bar metrics.
const (
	veleroBarHeight = 4
	veleroBarGap    = 2
)

// CreateRenderer implements fyne.Widget.
func (b *veleroBar) CreateRenderer() fyne.WidgetRenderer {
	r := &veleroBarRenderer{b: b}
	for range b.lines {
		seg := canvas.NewRectangle(color.Transparent)
		seg.CornerRadius = 2
		r.segs = append(r.segs, seg)
	}
	r.Refresh()
	return r
}

type veleroBarRenderer struct {
	b    *veleroBar
	segs []*canvas.Rectangle
}

func (r *veleroBarRenderer) MinSize() fyne.Size { return fyne.NewSize(0, veleroBarHeight) }

func (r *veleroBarRenderer) Layout(size fyne.Size) {
	total := 0
	for _, l := range r.b.lines {
		total += l.Count
	}
	if total == 0 {
		return
	}
	room := size.Width - float32(len(r.segs)-1)*veleroBarGap
	x := float32(0)
	for i, seg := range r.segs {
		w := max(2, room*float32(r.b.lines[i].Count)/float32(total))
		seg.Move(fyne.NewPos(x, 0))
		seg.Resize(fyne.NewSize(w, veleroBarHeight))
		x += w + veleroBarGap
	}
}

func (r *veleroBarRenderer) Refresh() {
	th := r.b.Theme()
	for i, seg := range r.segs {
		seg.FillColor = themeColor(th, veleroBarTone(r.b.lines[i].Status))
		seg.Refresh()
	}
	r.Layout(r.b.Size())
}

func (r *veleroBarRenderer) Objects() []fyne.CanvasObject {
	out := make([]fyne.CanvasObject, len(r.segs))
	for i, s := range r.segs {
		out[i] = s
	}
	return out
}
func (r *veleroBarRenderer) Destroy() {}
