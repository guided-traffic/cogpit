package fyneui

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Audit Log dock tab (G17; D-2: "an audit log the user can read"; engine/auditlist.go): the
// audit log's entries newest first, read-only, one line each: time, origin, command, context, the
// objects it named and its outcome. Its origin filter is the tab's engine state (audit.open with an
// origin), so agents see and set it as the user does. It reads the log through audit.list, again
// whenever the engine announces a new line (EventAudit), the newest 1,000 entries of the file's last
// 4 MiB. Lens has no such tab; its layout and wording are Cogpit's (inferred).

// auditOrigins are the filter's choices: an origin filter and its label.
var auditOrigins = []struct{ origin, label string }{
	{engine.AuditAllOrigins, "All origins"},
	{engine.AuditAgents, "Agents"},
	{string(engine.OriginExternalAgent), "External agents (MCP)"},
	{string(engine.OriginBuiltInAgent), "Built-in agent"},
	{string(engine.OriginGUI), "You, in this window"},
	{string(engine.OriginDeepLink), "Links (cogpit://)"},
}

// auditColumns are the list's columns: title and width (0: the room left).
var auditColumns = []struct {
	title string
	width float32
}{{"Time", 132}, {"Origin", 132}, {"Command", 250}, {"Context", 150}, {"Objects", 0}, {"Outcome", 240}}

// auditLogTab is the tab's content; it is the dock content's editor (dockEditor), so the dock
// hands it its tab's changes (update) and closes it.
type auditLogTab struct {
	sh  *shell
	tab engine.DockTab

	root    *fyne.Container
	origin  *selectField
	summary *textLine
	list    *widget.List
	empty   *fyne.Container

	entries []engine.AuditItem
	loaded  string // the origin the entries were read for
	seq     int    // the newest read asked for; older answers are dropped
	stale   bool   // lines were written while another dock tab was shown: read again when shown
	syncing bool
	closed  bool
}

var _ dockEditor = (*auditLogTab)(nil)

func newAuditLogTab(sh *shell, t engine.DockTab) *auditLogTab {
	a := &auditLogTab{sh: sh, tab: t}
	labels := make([]string, len(auditOrigins))
	for i, o := range auditOrigins {
		labels[i] = o.label
	}
	a.origin = newSelectField(labels, func(label string) {
		if a.syncing {
			return
		}
		for _, o := range auditOrigins {
			if o.label == label {
				sh.dispatch("audit.open", map[string]any{"origin": o.origin}, nil) // the tab's filter is engine state
			}
		}
	})
	a.summary = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	a.summary.truncate = true
	header := container.New(auditRowLayout{}, auditHeader()...)
	a.list = widget.NewList(func() int { return len(a.entries) }, a.newRow, a.updateRow)
	a.list.HideSeparators = true
	a.empty = container.NewCenter(newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{}))
	a.empty.Hide()
	toolbar := container.NewBorder(nil, nil, container.NewHBox(newTextLine("Show", colorMuted, theme.SizeNameText, fyne.TextStyle{}), a.origin), nil,
		inset(theme.SizeNameInnerPadding, "", a.summary))
	top := container.NewVBox(inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, toolbar), newDivider(false),
		inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, header), newDivider(false))
	a.root = container.NewBorder(top, nil, nil, nil, container.NewStack(a.list, a.empty))
	a.update(t)
	return a
}

// update follows the tab in the engine's state: a new filter reads the log again.
func (a *auditLogTab) update(t engine.DockTab) {
	a.tab = t
	a.syncing = true
	for _, o := range auditOrigins {
		if o.origin == t.Audit.Origin {
			a.origin.SetSelected(o.label)
		}
	}
	a.syncing = false
	if t.Audit.Origin != a.loaded || a.seq == 0 || (a.stale && a.shown()) {
		a.reload()
	}
}

// shown reports whether the tab is the dock's active one.
func (a *auditLogTab) shown() bool { return a.sh.ui.ActiveDock == a.tab.ID }

// remote implements dockEditor; the tab has no text.
func (a *auditLogTab) remote(uint64) {}

// close implements dockEditor: answers that come later are dropped.
func (a *auditLogTab) close() { a.closed = true }

// reload reads the log again through audit.list.
func (a *auditLogTab) reload() {
	a.seq++
	a.stale = false
	seq, origin := a.seq, a.tab.Audit.Origin
	a.sh.dispatch("audit.list", map[string]any{"origin": origin, "limit": engine.MaxAuditEntries}, func(res any, err error) {
		if a.closed || seq != a.seq {
			return
		}
		var l engine.AuditList
		if err == nil {
			err = decode(res, &l)
		}
		if err != nil {
			a.show(nil, "The audit log cannot be read: "+errorText(err))
			return
		}
		a.loaded = origin
		a.entries = l.Entries
		a.summary.SetText(auditSummary(l))
		msg := ""
		if len(l.Entries) == 0 {
			msg = "No entries yet: agents' commands appear here as they run."
			if origin != engine.AuditAllOrigins {
				msg = "No entries of this origin."
			}
		}
		a.show(l.Entries, msg)
	})
}

// show puts entries in the list, or a message in its place.
func (a *auditLogTab) show(entries []engine.AuditItem, msg string) {
	a.entries = entries
	a.list.Refresh()
	if msg == "" {
		a.empty.Hide()
		return
	}
	a.empty.Objects[0].(*textLine).SetText(msg)
	a.empty.Show()
}

// auditSummary says what the list holds.
func auditSummary(l engine.AuditList) string {
	s := fmt.Sprintf("%d entries, newest first", l.Matched)
	if l.Matched == 1 {
		s = "1 entry"
	}
	if l.Returned < l.Matched {
		s += fmt.Sprintf(", the newest %d shown", l.Returned)
	}
	if l.Partial {
		s += "; older entries are beyond the log's last " + model.FormatBytes(engine.MaxAuditRead)
	}
	return s
}

func auditHeader() []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, len(auditColumns))
	for i, c := range auditColumns {
		objs[i] = newTextLine(strings.ToUpper(c.title), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
	}
	return objs
}

func (a *auditLogTab) newRow() fyne.CanvasObject {
	cells := make([]fyne.CanvasObject, len(auditColumns))
	for i := range cells {
		l := newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
		l.truncate = true
		cells[i] = l
	}
	return inset(theme.SizeNameInnerPadding, "", container.New(auditRowLayout{}, cells...))
}

func (a *auditLogTab) updateRow(id widget.ListItemID, o fyne.CanvasObject) {
	if id < 0 || id >= len(a.entries) {
		return
	}
	row := o.(*fyne.Container).Objects[0].(*fyne.Container)
	e := a.entries[id]
	texts := a.rowTexts(e)
	for i, c := range row.Objects {
		l := c.(*textLine)
		l.SetText(texts[i])
		l.style = fyne.TextStyle{}
		l.SetColor(theme.ColorNameForeground)
	}
	row.Objects[2].(*textLine).style = fyne.TextStyle{Monospace: true}
	outcome := row.Objects[len(row.Objects)-1].(*textLine)
	switch {
	case e.Phase == engine.AuditPhaseRequested:
		outcome.SetColor(colorStatusPending)
	case e.OK:
		outcome.SetColor(colorStatusOK)
	default:
		outcome.SetColor(colorStatusError)
	}
	for _, c := range row.Objects {
		c.Refresh()
	}
}

// rowTexts are an entry's cells, in auditColumns' order.
func (a *auditLogTab) rowTexts(e engine.AuditItem) []string {
	objects := make([]string, 0, 3)
	for _, r := range e.Objects[:min(len(e.Objects), 3)] {
		objects = append(objects, engine.ObjectText(r))
	}
	obj := strings.Join(objects, ", ")
	if more := e.ObjectCount - len(objects); more > 0 {
		obj += fmt.Sprintf(" and %d more", more)
	}
	outcome := "ok"
	switch {
	case e.Phase == engine.AuditPhaseRequested:
		outcome = "requested"
	case !e.OK:
		outcome = e.Error
		if outcome == "" {
			outcome = "failed"
		}
	}
	if e.Phase == engine.AuditPhaseEnded { // the end of a background job the command started (engine/jobs.go)
		outcome = "job ended: " + outcome
	}
	return []string{a.timeText(e.Time), auditOriginText(e.Origin), e.Command, e.Context, obj, outcome}
}

// timeText is when an entry was written, in the user's zone and format (D-31): the time of day
// today, the date and the time of day before.
func (a *auditLogTab) timeText(t time.Time) string {
	return sameDayTime(a.sh.timeFormat(), t, a.sh.now())
}

// auditOriginText names an origin in the list (the CLI's commands are not audited).
func auditOriginText(o engine.Origin) string {
	switch o {
	case engine.OriginGUI:
		return "You (window)"
	case engine.OriginExternalAgent:
		return "External agent"
	case engine.OriginBuiltInAgent:
		return "Built-in agent"
	case engine.OriginDeepLink:
		return "Link (cogpit://)"
	}
	return string(o)
}

// auditRowLayout lays a row's cells out in auditColumns' widths, Objects taking the room left.
type auditRowLayout struct{}

func (auditRowLayout) gap() float32 {
	return fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameInnerPadding)
}

func (l auditRowLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for i, o := range objs {
		if i < len(auditColumns) {
			w += max(auditColumns[i].width, 80) + l.gap()
		}
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(w, h)
}

func (l auditRowLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	fixed := float32(0)
	for _, c := range auditColumns {
		fixed += c.width + l.gap()
	}
	rest := max(size.Width-fixed, 80)
	x := float32(0)
	for i, o := range objs {
		if i >= len(auditColumns) {
			break
		}
		w := auditColumns[i].width
		if w == 0 {
			w = rest
		}
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(w, size.Height))
		x += w + l.gap()
	}
}

// auditChanged reads the Audit Log again after the engine announced a new line (EventAudit), or,
// while another dock tab is shown, when it is shown again: the file is not read for a hidden tab.
func (d *dock) auditChanged() {
	for _, c := range d.contents {
		if a, ok := c.editor.(*auditLogTab); ok && !c.closed {
			if a.shown() {
				a.reload()
			} else {
				a.stale = true
			}
		}
	}
}

// auditMenuItem is Help › Audit Log (audit.open); nil when the build has no audit log command.
func (sh *shell) auditMenuItem() *fyne.MenuItem {
	c, ok := sh.eng.Registry().Get("audit.open")
	if !ok {
		return nil
	}
	return fyne.NewMenuItem(c.Label, func() { sh.dispatch(c.ID, struct{}{}, nil) })
}
