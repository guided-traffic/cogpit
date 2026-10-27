package fyneui

import (
	"net/url"
	"reflect"
	"strconv"
	"strings"
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

// The alert board (D-77, its cards D-new): the shown cluster's active alerts in the workspace, as a
// page (engine.AlertBoard) whose header is a view's (its title, the count, the namespace filter and
// the search, the UI state's) and whose body replaces the table. Under a line of the severities
// (each a chip with its count that folds its cards away), a masonry of cards, worst severity first
// (the owner, 2026-10-27: first the criticality, then a short description of the problem, then a
// list of the concrete instances, and far less empty room than a full-width list): a card per
// alertname, its head tinted in the severity's colour with the history of its last 24 hours above
// it, its text once (the alerts' summaries made one, alerts_card.go), what all its alerts share (the
// namespace, the object) as links, and a table of its alerts with the columns that tell them apart
// and since when each fires. The object opens in its view, the namespace narrows the board, the
// runbook opens in the browser. No silences: read only. The folding is the engine's (alerts.fold),
// so agents see what the user folded. Karma's layout is the model, not its look: no code, text or
// asset of it (D-12).

// maxCardAlerts bounds the rows a card shows with all shown: the rest are counted under it, for the
// search to narrow. A card of thousands would cost thousands of widgets at every refresh.
// alertCardRows are the rows it shows before Show all.
const (
	maxCardAlerts = 50
	alertCardRows = 10
)

// A card is at least alertCardMin points wide (a pod's name, a column, since when), the cards
// alertCardGap points apart.
const (
	alertCardMin = 360
	alertCardGap = 8
)

// alertSettingsSection is where Cluster Settings shows the Alertmanager's address: its own section
// (clustersettings_alertmanager.go, D-87).
var alertSettingsSection = engine.IntegrationSection(engine.IntegrationAlertmanager)

// alertBoard is the board's body.
type alertBoard struct {
	sh     *shell
	shown  bool
	box    *fyne.Container // the state lines, the severities and the cards
	cards  *fyne.Container // the cards, in the masonry
	grid   *masonry
	scroll *container.Scroll
	root   *fyne.Container // the background and the scroll over the table's place
	data   *engine.AlertBoardModel
	ages   []alertAge // the since texts, refreshed by the shell's tick (refreshAges)

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
	b := &alertBoard{sh: sh, box: container.NewVBox(), grid: &masonry{min: alertCardMin, gap: alertCardGap}}
	b.cards = box(b.grid)
	b.scroll = newVScroll(inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, b.box))
	b.root = container.New(boardRoot{b}, newSurface(theme.ColorNameBackground), b.scroll)
	b.root.Hide()
	return b
}

// boardRoot lays out the board's background and scroller over the whole place, and tells the
// masonry its width first: the scroller asks for the cards' height at it.
type boardRoot struct{ b *alertBoard }

// Layout implements fyne.Layout.
func (l boardRoot) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	pad := fyne.CurrentApp().Settings().Theme().Size(theme.SizeNameInnerPadding)
	l.b.grid.setWidth(size.Width - 2*pad)
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

// MinSize implements fyne.Layout.
func (l boardRoot) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var ms fyne.Size
	for _, o := range objs {
		ms = ms.Max(o.MinSize())
	}
	return ms
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
	b.box.Objects, b.cards.Objects = nil, nil
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
	if line := b.severities(m); line != nil {
		objs = append(objs, line)
	}
	var cards []fyne.CanvasObject
	for _, g := range m.Groups {
		if g.Folded {
			continue
		}
		for _, c := range g.Cards {
			cards = append(cards, b.card(g, c))
		}
	}
	b.cards.Objects = cards
	objs = append(objs, b.cards)
	b.box.Objects = objs
	b.box.Refresh()
	b.scroll.Refresh()
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
	setAddress := newPlateButton("Set Its Address…", nil, func() {
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

// fold dispatches a fold of the board (alerts.fold): a group's or card's (engine.FoldCard), a card's
// description or all its rows.
func (b *alertBoard) fold(key string, on bool, f engine.AlertFold) {
	b.sh.dispatch("alerts.fold", alertFoldParams{Key: key, Folded: on, Description: f == engine.FoldDescription,
		Rows: f == engine.FoldRows}, nil)
}

// alertFoldParams are alerts.fold's.
type alertFoldParams struct {
	Key         string `json:"key"`
	Folded      bool   `json:"folded,omitempty"`
	Description bool   `json:"description,omitempty"`
	Rows        bool   `json:"rows,omitempty"`
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
	if g.Key == severityInfo {
		return colorStatusSucceeded
	}
	return statusColorName(g.Status)
}

// severities is the board's first line (Dark Cockpit's overview made a line): a chip per severity
// with alerts, worst first, its dot in the severity's colour and its count; a click folds its cards
// away, the chip then hollow and muted. At its right why the cards have no history, when they have
// none. Nil without alerts.
func (b *alertBoard) severities(m *engine.AlertBoardModel) fyne.CanvasObject {
	if len(m.Groups) == 0 {
		return nil
	}
	var chips []fyne.CanvasObject
	for _, g := range m.Groups {
		tone := alertTone(g)
		dot := newSurface(tone)
		text := theme.ColorNameForeground
		tip := "Hide the " + strings.ToLower(g.Title) + " alerts"
		if g.Folded {
			dot.fill, dot.stroke, text = "", tone, colorMuted
			tip = "Show the " + strings.ToLower(g.Title) + " alerts"
		}
		dot.min = fyne.NewSize(10, 10)
		dot.radius = sizeChipRadius
		row := container.NewHBox(container.NewCenter(dot),
			newTextLine(g.Title, text, theme.SizeNameText, fyne.TextStyle{Bold: true}),
			newTextLine(strconv.Itoa(g.Count), colorMuted, theme.SizeNameText, fyne.TextStyle{}))
		chips = append(chips, newAlertTap(b.sh, row, func() { b.fold(g.Key, !g.Folded, engine.FoldCard) }, tip))
	}
	var note fyne.CanvasObject
	if m.HistoryNote != "" {
		note = container.NewCenter(newTextLine("No history: "+m.HistoryNote, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}))
	}
	line := container.NewBorder(nil, nil, box(layout.NewCustomPaddedHBoxLayout(18), chips...), note)
	return inset("", theme.SizeNamePadding, line)
}

// card is an alertname's card: its head (the history strip, the severity, the name, its copy icon
// and the count, on the severity's tint; a click folds the card), then unless folded its text once,
// what all its alerts share, its actions (the description, the runbook) and the table of its alerts.
func (b *alertBoard) card(g engine.AlertGroup, c engine.AlertCard) fyne.CanvasObject {
	tone := alertTone(g)
	sev := strings.ToUpper(g.Title)
	if c.SeverityLabel != "" {
		sev += " · " + c.SeverityLabel
	}
	name := newTextLine(c.Name, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	copyIcon, attach := b.cardCopy(c.Key, c.Name)
	count := newTextLine(strconv.Itoa(c.Count), colorMuted, theme.SizeNameText, fyne.TextStyle{})
	headRow := container.NewBorder(nil, nil, container.NewHBox(widget.NewIcon(chevron(c.Folded)), name, copyIcon), count)
	tip := "Fold the card"
	if c.Folded {
		tip = "Unfold the card"
	}
	headTap := newAlertTap(b.sh, container.NewVBox(
		inset(theme.SizeNameInlineIcon, "", newTextLine(sev, tone, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})), headRow),
		func() { b.fold(c.Key, !c.Folded, engine.FoldCard) }, tip)
	attach(headTap)
	var head []fyne.CanvasObject
	if c.History != nil {
		head = append(head, newHistoryStrip(b.sh, *c.History, tone))
	}
	head = append(head, headTap)
	tint := newSurface(tone)
	tint.alpha = 0.16
	tint.radius = sizeChipRadius
	tint.topOnly = true
	parts := []fyne.CanvasObject{container.NewStack(tint, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding,
		box(layout.NewCustomPaddedVBoxLayout(6), head...)))}
	if !c.Folded {
		parts = append(parts, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, b.cardBody(c)))
	}
	bg := newSurface(colorSidebar)
	bg.radius = sizeChipRadius
	return container.NewStack(bg, box(layout.NewCustomPaddedVBoxLayout(0), parts...))
}

// cardBody is an open card's body: its text, what its alerts share, its actions, its description
// when shown, and its table.
func (b *alertBoard) cardBody(c engine.AlertCard) fyne.CanvasObject {
	var body []fyne.CanvasObject
	if len(c.Summary) > 0 {
		body = append(body, newSpanText(theme.SizeNameText, cardSpans(c.Summary, c.Columns, theme.ColorNameForeground)...))
	}
	if line := b.infoLine(c); line != nil {
		body = append(body, line)
	}
	if c.Expanded && len(c.Description) > 0 {
		body = append(body, newSpanText(theme.SizeNameCaptionText, cardSpans(c.Description, c.Columns, theme.ColorNameForeground)...))
	}
	if t := b.table(c); t != nil {
		body = append(body, newDivider(false), t)
	}
	return box(layout.NewCustomPaddedVBoxLayout(4), body...)
}

// infoLine is a card's line under its text: at its left what all its alerts name alike, as links
// (the namespace narrows the board to it, the object opens in its view), since when its one alert
// fires when it has no table, and its history in words; at its right Details (the description) and
// its runbook. Nil when there is nothing.
func (b *alertBoard) infoLine(c engine.AlertCard) fyne.CanvasObject {
	var left, right []fyne.CanvasObject
	caption := func(text string) *textLine {
		return newTextLine(text, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	}
	dot := func() {
		if len(left) > 0 {
			left = append(left, caption("·"))
		}
	}
	for _, o := range c.Context {
		dot()
		if o.Label == paramNamespace {
			left = append(left, b.namespaceLink(o.Ref.Name, theme.SizeNameCaptionText))
			continue
		}
		left = append(left, caption(o.Title), b.objectCell(o, theme.SizeNameCaptionText))
	}
	if len(c.Columns) == 0 && len(c.Alerts) == 1 {
		dot()
		left = append(left, b.age(c.Alerts[0].Since))
	}
	if h := historySummary(c.History); h != "" {
		dot()
		left = append(left, caption(h))
	}
	if len(c.Description) > 0 {
		label := "Details"
		if c.Expanded {
			label = "Hide details"
		}
		right = append(right, boardLink(label, func() { b.fold(c.Key, !c.Expanded, engine.FoldDescription) }))
	}
	if c.Runbook != "" {
		runbook := c.Runbook
		right = append(right, boardLink("Runbook", func() { b.openRunbook(runbook) }))
	}
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	return container.NewBorder(nil, nil, container.New(layout.NewCustomPaddedHBoxLayout(4), left...),
		container.New(layout.NewCustomPaddedHBoxLayout(12), right...))
}

// age is a since text of the board, refreshed by the shell's tick.
func (b *alertBoard) age(since time.Time) *textLine {
	l := newTextLine(model.FormatAge(b.sh.now().Sub(since)), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	l.align = fyne.TextAlignTrailing
	b.ages = append(b.ages, alertAge{line: l, since: since})
	return l
}

// table is a card's alerts as rows of its columns and since when each fires, a header over them
// from two rows on; the first alertCardRows rows, all of them up to maxCardAlerts after Show all.
// An alert whose text the card's does not give back has it in a line under its row (its
// description only while the card shows its description). Nil for a card of one alert that has no
// column: its line of actions says since when it fires.
func (b *alertBoard) table(c engine.AlertCard) fyne.CanvasObject {
	if len(c.Columns) == 0 && len(c.Alerts) == 1 {
		return nil
	}
	th := fyne.CurrentApp().Settings().Theme()
	textSize, capSize := th.Size(theme.SizeNameText), th.Size(theme.SizeNameCaptionText)
	limit := min(len(c.Alerts), alertCardRows)
	if c.AllRows {
		limit = min(len(c.Alerts), maxCardAlerts)
	}
	shown := c.Alerts[:limit]
	g := &tableGrid{natural: make([]float32, len(c.Columns)+1), gap: 10}
	measure := func(i int, text string, size float32) {
		g.natural[i] = max(g.natural[i], fyne.MeasureText(text, size, fyne.TextStyle{}).Width+2)
	}
	header := len(c.Alerts) > 1 && len(c.Columns) > 0
	for i, col := range c.Columns {
		size := textSize
		if col.Link == engine.ColumnNamespace {
			size = capSize
		}
		if header {
			measure(i, col.Title, capSize)
		}
		for _, it := range shown {
			measure(i, it.Values[col.Key], size)
		}
	}
	last := len(c.Columns)
	for _, it := range shown {
		w := fyne.MeasureText(model.FormatAge(b.sh.now().Sub(it.Since)), capSize, fyne.TextStyle{}).Width + 2
		if c.Runbook == "" && it.Runbook != "" {
			w += fyne.MeasureText("Runbook", capSize, fyne.TextStyle{}).Width + 10
		}
		g.natural[last] = max(g.natural[last], w, 30)
	}
	var rows []fyne.CanvasObject
	if header {
		var cells []fyne.CanvasObject
		for _, col := range c.Columns {
			l := newTextLine(col.Title, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
			l.truncate = true
			cells = append(cells, l)
		}
		rows = append(rows, box(gridRow{g}, append(cells, layout.NewSpacer())...))
	}
	for _, it := range shown {
		cells := make([]fyne.CanvasObject, 0, len(c.Columns)+1)
		for _, col := range c.Columns {
			cells = append(cells, b.cell(col, it))
		}
		end := []fyne.CanvasObject{layout.NewSpacer()}
		if c.Runbook == "" && it.Runbook != "" {
			runbook := it.Runbook
			end = append(end, boardLink("Runbook", func() { b.openRunbook(runbook) }))
		}
		end = append(end, b.age(it.Since))
		cells = append(cells, container.New(layout.NewCustomPaddedHBoxLayout(8), end...))
		rows = append(rows, box(gridRow{g}, cells...))
		if it.Message != "" {
			rows = append(rows, b.ownText(it.Message))
		}
		if c.Expanded && it.Detail != "" {
			rows = append(rows, b.ownText(it.Detail))
		}
	}
	switch {
	case len(c.Alerts) > limit && !c.AllRows:
		rows = append(rows, container.NewHBox(boardLink("Show all "+formatCount(int64(len(c.Alerts))),
			func() { b.fold(c.Key, true, engine.FoldRows) })))
	case c.AllRows && len(c.Alerts) > alertCardRows:
		more := []fyne.CanvasObject{boardLink("Show fewer", func() { b.fold(c.Key, false, engine.FoldRows) })}
		if len(c.Alerts) > limit {
			more = append(more, newTextLine("and "+formatCount(int64(len(c.Alerts)-limit))+" more: narrow them with the search",
				colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}))
		}
		rows = append(rows, container.New(layout.NewCustomPaddedHBoxLayout(12), more...))
	}
	return box(layout.NewCustomPaddedVBoxLayout(2), rows...)
}

// cell is a row's cell of a column: the object as a link into its view (text when the cluster
// serves no kind of it), the namespace as a link that narrows the board, a label's or a number's
// value as selectable text.
func (b *alertBoard) cell(col engine.AlertColumn, it engine.AlertItem) fyne.CanvasObject {
	v := it.Values[col.Key]
	switch col.Link {
	case engine.ColumnObject:
		for _, o := range it.Objects {
			if o.Label != paramNamespace {
				return b.objectCell(o, theme.SizeNameText)
			}
		}
	case engine.ColumnNamespace:
		if v != "" {
			return b.namespaceLink(v, theme.SizeNameCaptionText)
		}
	}
	return b.valueCell(v, theme.SizeNameText)
}

// objectCell is an object's name as a link that opens it in its view; as selectable text when the
// cluster serves no kind Cogpit shows for it.
func (b *alertBoard) objectCell(o engine.AlertObject, size fyne.ThemeSizeName) fyne.CanvasObject {
	if o.Ref.Kind == "" {
		cell := b.valueCell(o.Ref.Name, size)
		w := fyne.MeasureText(o.Ref.Name, fyne.CurrentApp().Settings().Theme().Size(size), fyne.TextStyle{}).Width
		return container.New(textWidth{w: w + 1}, cell)
	}
	ref := o.Ref
	return sizedLink(o.Ref.Name, size, func() { b.sh.dispatch("ui.navigate", refParams{Ref: ref}, nil) })
}

// sizedLink is a link as wide as its text where there is room, cut off with an ellipsis where its
// place is narrower (linkText alone asks for no width).
func sizedLink(text string, size fyne.ThemeSizeName, onTap func()) fyne.CanvasObject {
	l := newLinkText(text, onTap)
	l.size = size
	w := fyne.MeasureText(text, fyne.CurrentApp().Settings().Theme().Size(size), fyne.TextStyle{}).Width
	return container.New(textWidth{w: w + 1}, l)
}

// namespaceLink is a namespace that narrows the board to it, as a list's Namespace column does.
func (b *alertBoard) namespaceLink(ns string, size fyne.ThemeSizeName) fyne.CanvasObject {
	return sizedLink(ns, size, func() { b.sh.dispatch("ui.setNamespace", namespacesParams{Namespaces: []string{ns}}, nil) })
}

// valueCell is a value worth copying: selectable, cut off with an ellipsis where its column is
// narrower.
func (b *alertBoard) valueCell(v string, size fyne.ThemeSizeName) fyne.CanvasObject {
	l := newTextLine(v, theme.ColorNameForeground, size, fyne.TextStyle{})
	l.truncate = true
	return newSelLine(l, nil, func(sel string) { b.sh.copyText(sel, false) })
}

// ownText is an alert's own text under its row, where the card's does not give it back: muted,
// selectable, one line.
func (b *alertBoard) ownText(text string) fyne.CanvasObject {
	l := newTextLine(strings.Join(strings.Fields(text), " "), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	l.truncate = true
	return inset(theme.SizeNameInlineIcon, "", newSelLine(l, nil, func(sel string) { b.sh.copyText(sel, false) }))
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
