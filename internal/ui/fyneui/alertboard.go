package fyneui

import (
	"net/url"
	"reflect"
	"strconv"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The alert board (D-77; the owner, 2026-10-17: like karma's, tidier, "sparsam bei der
// Darstellung"): the shown cluster's active alerts in the workspace, as a page (engine.AlertBoard)
// whose header is a view's (its title, the count, the namespace filter and the search, the UI
// state's) and whose body replaces the table. The engine groups them (alerts.board): a fold per
// severity, critical, warning, info, each a line with its colour and count; in it a card per
// alertname, its count, and per alert one line: the summary (a click unfolds the description
// under it), since when it fires and the runbook link at the right; under it the namespace and the
// object it is about, as links (the namespace narrows the board's namespace filter, as a list's
// Namespace column does; the object opens in its view). No label chips, no cluster or job labels,
// no silences: read only. The folding is the engine's (alerts.fold), so agents see what the user
// folded. Karma's layout is the model, not its look: no code, text or asset of it (D-12).

// maxCardAlerts bounds the alerts a card shows: the rest are counted under it, for the search to
// narrow. A card of thousands would cost thousands of widgets at every refresh.
const maxCardAlerts = 50

// alertSettingsSection is where Cluster Settings shows the Alertmanager's address: its own section
// (clustersettings_alertmanager.go, D-87).
var alertSettingsSection = engine.IntegrationSection(engine.IntegrationAlertmanager)

// alertBoard is the board's body.
type alertBoard struct {
	sh    *shell
	shown bool
	box   *fyne.Container // the state line, the groups and cards
	root  *fyne.Container // the background and the scroll over the table's place
	data  *engine.AlertBoardModel
	ages  []alertAge // the since texts, refreshed by the shell's tick (refreshAges)

	// The card head the pointer is on, kept across a rebuild of the board, so that its copy icon
	// stays shown (the owner's WF34: it showed late or not at all, a rebuilt head getting no
	// MouseIn until the pointer moved): the card's key and the hide of its icon.
	hoverKey  string
	hoverHide func()

	// Requests of the model are coalesced, as the other pages' are (fluxdashboard.go).
	requests atomic.Int32
}

type alertAge struct {
	line  *textLine
	since time.Time
}

func newAlertBoard(sh *shell) *alertBoard {
	b := &alertBoard{sh: sh, box: container.NewVBox()}
	b.root = container.NewStack(newSurface(theme.ColorNameBackground),
		container.NewVScroll(inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, b.box)))
	b.root.Hide()
	return b
}

// setShown shows the board over the table while its page is open.
func (b *alertBoard) setShown(on bool) {
	if on == b.shown {
		return
	}
	b.shown = on
	if on {
		b.root.Show()
		return
	}
	b.root.Hide()
	b.data, b.ages = nil, nil
	b.box.Objects = nil
}

// request reads the board off the main goroutine and shows it; requests made while one is in
// flight become one more.
func (b *alertBoard) request() {
	if !b.shown {
		return
	}
	for {
		n := b.requests.Load()
		if n >= 2 {
			return
		}
		if b.requests.CompareAndSwap(n, n+1) {
			if n == 1 {
				return
			}
			break
		}
	}
	b.sh.dispatch("alerts.board", struct{}{}, func(res any, err error) {
		again := b.requests.Swap(0) == 2
		var m engine.AlertBoardModel
		switch {
		case err != nil:
			m = engine.AlertBoardModel{Detail: err.Error()}
		case decode(res, &m) != nil:
			return
		}
		if b.shown {
			b.show(&m)
		}
		if again && b.shown {
			b.request()
		}
	})
}

// countText is the header's count: "24 items", "4 of 24 items"; "" while the board has no answer.
func (b *alertBoard) countText() string {
	if !b.shown || b.data == nil || b.data.AsOf.IsZero() {
		return ""
	}
	return itemCount(b.data.Matched, b.data.Total)
}

// refreshAges writes the since texts anew (the shell's tick).
func (b *alertBoard) refreshAges() {
	now := b.sh.now()
	for _, a := range b.ages {
		a.line.SetText(model.FormatAge(now.Sub(a.since)))
	}
}

// show makes the board show m. A model that shows what the board shows already (a poll's answer
// with nothing new, a UI change that is not the board's) keeps the board's widgets as they are, the
// pointer's card head and its copy icon with them (WF34).
func (b *alertBoard) show(m *engine.AlertBoardModel) {
	if sameBoard(b.data, m) && len(b.box.Objects) > 0 {
		b.data = m
		return
	}
	b.data, b.ages = m, nil
	var objs []fyne.CanvasObject
	objs = append(objs, b.stateLines(m)...)
	if !m.AsOf.IsZero() && len(m.Groups) == 0 {
		text := "No active alerts."
		if m.Total > 0 {
			text = "No alerts match the filter."
		}
		objs = append(objs, container.NewCenter(inset("", theme.SizeNameInnerPadding,
			newTextLine(text, colorMuted, theme.SizeNameText, fyne.TextStyle{}))))
	}
	if strip := alertStrip(m.Groups); strip != nil {
		objs = append(objs, strip)
	}
	for _, g := range m.Groups {
		objs = append(objs, b.groupHeader(g))
		if g.Folded {
			continue
		}
		for _, c := range g.Cards {
			objs = append(objs, b.card(g, c))
		}
	}
	b.box.Objects = objs
	b.box.Refresh()
	tv := b.sh.view
	tv.count.SetText(b.countText())
	tv.showHeadingCount()
}

// sameBoard reports whether b shows what a shows: the same model but for the time of its answer,
// whose ages the shell's tick advances (refreshAges).
func sameBoard(a, b *engine.AlertBoardModel) bool {
	if a == nil || b == nil || a.AsOf.IsZero() != b.AsOf.IsZero() {
		return false
	}
	x, y := *a, *b
	x.AsOf, y.AsOf = time.Time{}, time.Time{}
	return reflect.DeepEqual(x, y)
}

// stateLines say what the board shows while the Alertmanager does not answer, or answered more
// than Cogpit keeps: nothing while all is well.
func (b *alertBoard) stateLines(m *engine.AlertBoardModel) []fyne.CanvasObject {
	var out []fyne.CanvasObject
	wrapped := func(text string) *widget.Label {
		l := widget.NewLabel(text)
		l.Wrapping = fyne.TextWrapWord
		return l
	}
	setAddress := widget.NewButton("Set Its Address…", func() {
		b.sh.dispatch("cluster.settings.open", clusterSettingsOpenParams{Context: b.sh.ui.Context, Section: alertSettingsSection}, nil)
	})
	switch {
	case m.State == "": // the board's command refused: the integration off, no cluster
		out = append(out, wrapped(m.Detail))
	case m.AsOf.IsZero() && m.State == engine.AlertsChecking:
		out = append(out, newTextLine("Looking for the cluster's Alertmanager…", colorMuted, theme.SizeNameText, fyne.TextStyle{}))
	case m.AsOf.IsZero() && m.State == engine.AlertsNotFound:
		out = append(out, wrapped("No Alertmanager found in this cluster. Cogpit looks for the services that "+
			"kube-prometheus-stack, kube-prometheus, the Prometheus Operator and the alertmanager chart create; set its "+
			"address when it runs under another name."), container.NewHBox(setAddress))
	case m.AsOf.IsZero() && m.State == engine.AlertsForbidden:
		out = append(out, wrapped(m.Detail))
	case m.AsOf.IsZero():
		out = append(out, wrapped(m.Detail), container.NewHBox(setAddress))
	case !m.FailingSince.IsZero():
		line := newTextLine("Alertmanager does not answer since "+b.sh.timeFormat().FormatClock(m.FailingSince)+
			": the alerts of its last answer.", colorStatusWarning, theme.SizeNameText, fyne.TextStyle{})
		line.truncate = true
		out = append(out, line)
	}
	if m.Truncated {
		out = append(out, newTextLine("Alertmanager answered more alerts than Cogpit keeps: the first "+
			formatCount(int64(m.Total))+" are counted.", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}))
	}
	return out
}

// fold dispatches a fold of the board (alerts.fold).
func (b *alertBoard) fold(key string, folded, description bool) {
	b.sh.dispatch("alerts.fold", alertFoldParams{Key: key, Folded: folded, Description: description}, nil)
}

// alertFoldParams are alerts.fold's.
type alertFoldParams struct {
	Key         string `json:"key"`
	Folded      bool   `json:"folded,omitempty"`
	Description bool   `json:"description,omitempty"`
}

// chevron is a fold's icon: down while open, right while folded.
func chevron(folded bool) fyne.Resource {
	if folded {
		return theme.MenuExpandIcon()
	}
	return theme.MenuDropDownIcon()
}

// boardLink is a link of the board in the caption size, as wide as its text: a namespace, an
// object, the runbook (linkText alone gives way to any width, as a drawer value does).
func boardLink(text string, onTap func()) fyne.CanvasObject {
	l := newLinkText(text, onTap)
	l.size = theme.SizeNameCaptionText
	w := fyne.MeasureText(text, fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameCaptionText), fyne.TextStyle{}).Width
	return container.New(textWidth{w: w + 1}, l)
}

// textWidth lays out its one object at least w wide.
type textWidth struct{ w float32 }

func (t textWidth) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(t.w, objs[0].MinSize().Height)
}

func (t textWidth) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

// alertTone is a severity's token colour on the board and in the title bar's pill: the status colour
// of the group, except info, which is the Succeeded blue (the pill's, alerts.go).
func alertTone(g engine.AlertGroup) fyne.ThemeColorName {
	if g.Key == "info" {
		return colorStatusSucceeded
	}
	return statusColorName(g.Status)
}

// alertStrip is the board's overview (Dark Cockpit): a bracket "firing · 24" over one rounded token
// per alert, worst first, in its severity's colour, one run per severity with a gap between the
// runs, so that a cascade (many alerts of one severity, or one of each) shows before the list is
// read and the runs answer to the sections below. Nil without alerts. At most alertStripMax tokens.
func alertStrip(groups []engine.AlertGroup) fyne.CanvasObject {
	total, shown := 0, 0
	var runs []fyne.CanvasObject
	for _, g := range groups {
		var toks []tok
		for range g.Count {
			if shown < alertStripMax {
				toks = append(toks, tok{color: alertTone(g)})
				shown++
			}
		}
		total += g.Count
		if len(toks) > 0 {
			runs = append(runs, newTokenRow(alertStripSide, toks...))
		}
	}
	if total == 0 {
		return nil
	}
	row := box(layout.NewCustomPaddedHBoxLayout(alertStripRunGap), runs...)
	return inset("", theme.SizeNamePadding, box(layout.NewCustomPaddedVBoxLayout(0),
		newBracketLabel("firing · "+strconv.Itoa(total), colorAccent), row, gapOf(10)))
}

// The strip's tokens: 14 points, as many as fit a window's width (the list below has every alert),
// and 14 points between one severity's run and the next.
const (
	alertStripMax    = 60
	alertStripSide   = 14
	alertStripRunGap = 14
)

// groupHeader is a severity's line: its fold, a dot in its colour, its title and count.
func (b *alertBoard) groupHeader(g engine.AlertGroup) fyne.CanvasObject {
	icon := widget.NewIcon(chevron(g.Folded))
	dot := newSurface(alertTone(g))
	dot.min = fyne.NewSize(10, 10)
	dot.radius = sizeChipRadius
	title := newTextLine(g.Title, theme.ColorNameForeground, theme.SizeNameSubHeadingText, fyne.TextStyle{Bold: true})
	count := newTextLine(strconv.Itoa(g.Count), colorMuted, theme.SizeNameText, fyne.TextStyle{})
	row := container.NewHBox(icon, container.NewCenter(dot), title, count)
	return inset("", theme.SizeNamePadding, newAlertTap(b.sh, row, func() { b.fold(g.Key, !g.Folded, false) }, ""))
}

// card is an alertname's card: a bar in the severity's colour at its left, its header (fold, name,
// count, the severity label when the rules wrote another), its alerts.
func (b *alertBoard) card(g engine.AlertGroup, c engine.AlertCard) fyne.CanvasObject {
	name := newTextLine(c.Name, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	copyIcon, attach := b.cardCopy(c.Key, c.Name)
	head := []fyne.CanvasObject{widget.NewIcon(chevron(c.Folded)), name, copyIcon,
		newTextLine(strconv.Itoa(c.Count), colorMuted, theme.SizeNameText, fyne.TextStyle{})}
	if l := cardLabel(c); l != "" {
		head = append(head, newTextLine(l, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}))
	}
	headTap := newAlertTap(b.sh, container.NewHBox(head...), func() { b.fold(c.Key, !c.Folded, false) }, "")
	attach(headTap)
	rows := []fyne.CanvasObject{headTap}
	if !c.Folded {
		for i, it := range c.Alerts {
			if i == maxCardAlerts {
				rows = append(rows, inset(theme.SizeNameInnerPadding, "", newTextLine(
					"and "+formatCount(int64(len(c.Alerts)-maxCardAlerts))+" more: narrow them with the search",
					colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})))
				break
			}
			rows = append(rows, newDivider(false), b.alertRow(it))
		}
	}
	bar := newSurface(statusColorName(g.Status))
	bar.min = fyne.NewSize(3, 0)
	bg := newSurface(colorSidebar)
	bg.radius = sizeChipRadius
	body := container.NewBorder(nil, nil, bar, nil, inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, container.NewVBox(rows...)))
	return inset("", theme.SizeNamePadding, container.NewStack(bg, body))
}

// cardCopy is the copy icon of a card's alertname (the owner's WF21, 2026-10-18: "Alarm-Namen
// müssen kopierbar sein"), right after the name (WF34, 2026-10-19: it sat at the card's far right):
// shown while the pointer is on the card's head or on the icon, as the lists' copy icon (D-25: the
// same icon, its checkmark after a copy, the same toast); its room is kept, so the head does not
// move when it shows. attach hands it the head's hover. A board rebuilt under the pointer shows the
// icon of the card it is on at once (alertBoard.hoverKey).
func (b *alertBoard) cardCopy(key, name string) (icon fyne.CanvasObject, attach func(head *alertTap)) {
	var done *time.Timer // the checkmark's
	var onHead, onIcon bool
	var btn *copyButton
	hide := func() { onHead, onIcon = false, false; btn.Hide() }
	enter := func() { b.hoverKey, b.hoverHide = key, hide; btn.Show() }
	leave := func() {
		if onHead || onIcon {
			return
		}
		btn.Hide()
		if b.hoverKey == key { // also the card rebuilt from this one, whose head the pointer may have left meanwhile
			h := b.hoverHide
			b.hoverKey, b.hoverHide = "", nil
			if h != nil {
				h()
			}
		}
	}
	btn = newCopyIcon(b.sh, func() {
		b.sh.copyText(name, false)
		btn.confirm(&done)
		b.sh.copyToast("info", copiedToast)
	}, func(*desktop.MouseEvent) { onIcon = true; enter() }, func() { onIcon = false; leave() })
	btn.tip = "Copy " + name
	if key == b.hoverKey {
		onHead = true
		enter()
	}
	return container.NewStack(newHGap(theme.SizeNameInlineIcon), btn), func(head *alertTap) {
		head.in = func() { onHead = true; enter() }
		head.out = func() { onHead = false; leave() }
	}
}

// cardLabel is the severity label a card's alerts carry when it is not their group's name ("major"),
// "" when they carry none or several.
func cardLabel(c engine.AlertCard) string {
	label := ""
	for i, it := range c.Alerts {
		if i > 0 && it.SeverityLabel != label {
			return ""
		}
		label = it.SeverityLabel
	}
	return label
}

// alertRow is one alert of a card: its summary (a click unfolds the description), since when and
// the runbook at the right; its objects under it; the description when unfolded.
func (b *alertBoard) alertRow(it engine.AlertItem) fyne.CanvasObject {
	summary := newTipLine(b.sh, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
	text := it.Summary
	if text == "" {
		text = it.Name
	}
	summary.SetText(text)
	summary.truncate = true
	summary.tip = text
	lead := newHGap(theme.SizeNameInlineIcon)
	var line fyne.CanvasObject = summary
	if it.Description != "" {
		lead = widget.NewIcon(chevron(!it.Expanded))
		tip := "Show the description"
		if it.Expanded {
			tip = "Hide the description"
		}
		line = newAlertTap(b.sh, summary, func() { b.fold(it.Fingerprint, !it.Expanded, true) }, tip)
	}
	age := newTextLine(model.FormatAge(b.sh.now().Sub(it.Since)), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	b.ages = append(b.ages, alertAge{line: age, since: it.Since})
	right := []fyne.CanvasObject{age}
	if it.Runbook != "" {
		runbook := it.Runbook
		right = append(right, boardLink("Runbook", func() { b.openRunbook(runbook) }))
	}
	first := container.NewBorder(nil, nil, lead, container.NewHBox(right...), line)
	parts := []fyne.CanvasObject{first}
	if objs := b.objectsLine(it); objs != nil {
		parts = append(parts, container.NewBorder(nil, nil, newHGap(theme.SizeNameInlineIcon), nil, objs))
	}
	if it.Expanded && it.Description != "" {
		d := widget.NewLabel(it.Description)
		d.Wrapping = fyne.TextWrapWord
		d.Selectable = true
		d.SizeName = theme.SizeNameCaptionText
		parts = append(parts, container.NewBorder(nil, nil, newHGap(theme.SizeNameInlineIcon), nil, d))
	}
	return container.NewVBox(parts...)
}

// objectsLine is the namespace and the object an alert names, as links: the namespace narrows the
// board to it, the object opens in its view; an object of a kind the cluster does not serve is
// text. nil when the alert names none.
func (b *alertBoard) objectsLine(it engine.AlertItem) fyne.CanvasObject {
	var objs []fyne.CanvasObject
	caption := func(text string) *textLine {
		return newTextLine(text, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	}
	for _, o := range it.Objects {
		if len(objs) > 0 {
			objs = append(objs, caption("·"))
		}
		if o.Label == paramNamespace { // the namespace alone, as the lists' Namespace column shows it
			ns := o.Ref.Name
			objs = append(objs, boardLink(ns, func() { b.sh.dispatch("ui.setNamespace", namespacesParams{Namespaces: []string{ns}}, nil) }))
			continue
		}
		objs = append(objs, caption(o.Title))
		if o.Ref.Kind == "" {
			objs = append(objs, newTextLine(o.Ref.Name, theme.ColorNameForeground, theme.SizeNameCaptionText, fyne.TextStyle{}))
		} else {
			ref := o.Ref
			objs = append(objs, boardLink(o.Ref.Name, func() { b.sh.dispatch("ui.navigate", refParams{Ref: ref}, nil) }))
		}
		if o.Container != "" {
			objs = append(objs, caption("container "+o.Container))
		}
	}
	if len(objs) == 0 {
		return nil
	}
	return container.New(layout.NewCustomPaddedHBoxLayout(4), objs...)
}

// openRunbook opens a runbook in the browser: an http or https URL only (the engine left out any
// other, and the window checks again: the link comes from the cluster).
func (b *alertBoard) openRunbook(link string) {
	u, err := url.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return
	}
	if err := openBrowser(b.sh.app, u); err != nil {
		b.sh.toast("error", "Could not open the runbook: "+err.Error())
	}
}

// alertTap is a part of the board that does something when clicked: a fold's line, a summary.
type alertTap struct {
	widget.BaseWidget
	sh      *shell
	content fyne.CanvasObject
	onTap   func()
	tip     string
	in, out func() // the pointer came in and left (a card head's copy icon, cardCopy); nil for none
}

var (
	_ fyne.Tappable      = (*alertTap)(nil)
	_ desktop.Hoverable  = (*alertTap)(nil)
	_ desktop.Cursorable = (*alertTap)(nil)
)

func newAlertTap(sh *shell, content fyne.CanvasObject, onTap func(), tip string) *alertTap {
	t := &alertTap{sh: sh, content: content, onTap: onTap, tip: tip}
	t.ExtendBaseWidget(t)
	return t
}

// CreateRenderer implements fyne.Widget.
func (t *alertTap) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(t.content) }

// Tapped implements fyne.Tappable.
func (t *alertTap) Tapped(*fyne.PointEvent) {
	t.sh.tip.hideFor(t)
	if t.onTap != nil {
		t.onTap()
	}
}

// Cursor implements desktop.Cursorable.
func (t *alertTap) Cursor() desktop.Cursor { return desktop.PointerCursor }

// MouseIn implements desktop.Hoverable.
func (t *alertTap) MouseIn(ev *desktop.MouseEvent) {
	t.sh.tip.hover(t, t.tip, ev.AbsolutePosition)
	if t.in != nil {
		t.in()
	}
}

// MouseMoved implements desktop.Hoverable.
func (t *alertTap) MouseMoved(ev *desktop.MouseEvent) { t.sh.tip.moved(t, ev.AbsolutePosition) }

// MouseOut implements desktop.Hoverable.
func (t *alertTap) MouseOut() {
	t.sh.tip.hideFor(t)
	if t.out != nil {
		t.out()
	}
}
