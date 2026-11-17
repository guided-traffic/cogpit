package fyneui

import (
	"reflect"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The silences page (D-166): the shown cluster's Alertmanager's silences, a page
// (engine.AlertSilences) in the alert board's place and its look. Its header is a view's (the title,
// the count and the search; no namespace filter: silences are the Alertmanager's, not a
// namespace's). Under a line of the states (Active, Pending, Expired, each a chip with its count
// that folds its cards away, Expired folded at first) a masonry of cards, one per silence: its head,
// tinted in its state's colour, says when it ends (or starts, or ended) and lists its matchers; its
// body the comment, who made it and when, the alerts it mutes now as links into their views, and
// Expire (alerts.expireSilence, which asks first). The board's "N silenced" link leads here, the line's
// Alert Board link back.

// maxSilenceRows are the muted alerts a silence's card names; the rest are counted.
const maxSilenceRows = 8

// silenceTone is a group's colour: active the Succeeded blue (info's on the board), pending the
// Pending yellow, expired muted.
func silenceTone(key string) fyne.ThemeColorName {
	switch key {
	case engine.SilenceGroupActive:
		return colorStatusSucceeded
	case engine.SilenceGroupPending:
		return colorStatusPending
	}
	return colorMuted
}

// requestSilences reads the silences page off the main goroutine and shows it.
func (b *alertBoard) requestSilences(again func() bool) {
	b.sh.dispatch("alerts.silences", struct{}{}, func(res any, err error) {
		more := again()
		var m engine.SilencesModel
		switch {
		case err != nil:
			m = engine.SilencesModel{Detail: err.Error()}
		case decode(res, &m) != nil:
			return
		}
		if b.page == engine.AlertSilences {
			b.showSilences(&m)
		}
		if more && b.page == engine.AlertSilences {
			b.request()
		}
	})
}

// sameSilences reports whether b shows what a shows, but for when the silences were answered.
func sameSilences(a, b *engine.SilencesModel) bool {
	if a == nil || b == nil || a.AsOf.IsZero() != b.AsOf.IsZero() {
		return false
	}
	x, y := *a, *b
	x.AsOf, y.AsOf = time.Time{}, time.Time{}
	return reflect.DeepEqual(x, y)
}

// showSilences makes the page show m; a model that shows what it shows already keeps its widgets.
func (b *alertBoard) showSilences(m *engine.SilencesModel) {
	if sameSilences(b.sdata, m) && len(b.box.Objects) > 0 {
		b.sdata = m
		return
	}
	b.sdata, b.ages = m, nil
	var objs []fyne.CanvasObject
	if !m.AsOf.IsZero() && m.Error != "" {
		line := newTextLine(m.Error+": the silences of its last answer.", colorStatusWarning, theme.SizeNameText, fyne.TextStyle{})
		line.truncate = true
		objs = append(objs, line)
	}
	b.setCenter(b.silencesState(m))
	objs = append(objs, b.silenceGroupsLine(m))
	var cards []fyne.CanvasObject
	for _, g := range m.Groups {
		if g.Folded {
			continue
		}
		for _, x := range g.Silences {
			cards = append(cards, b.silenceCard(g, x))
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

// silencesState is the box the page shows in its middle instead of cards (boardbox.go): why there
// are no silences to show (no Alertmanager answers, their request failed, none yet), or that there
// are none, or none the search keeps (Clear Search). Nil while there are cards.
func (b *alertBoard) silencesState(m *engine.SilencesModel) fyne.CanvasObject {
	fg := theme.ColorNameForeground
	switch {
	case m.State == "", m.Where == "" && m.Detail != "": // refused, or no Alertmanager answers
		return boardBox(nil, m.Detail, fg)
	case m.AsOf.IsZero() && m.Error == "":
		return boardBox(nil, "Asking the Alertmanager for its silences…", colorMuted)
	case m.AsOf.IsZero():
		return boardBox(nil, m.Error, fg)
	case len(m.Groups) > 0:
		return nil
	case m.Total == 0:
		return boardBox(nil, "No silences: every alert that fires is notified.", fg)
	}
	return boardBox(nil, "No silence matches the search.", fg,
		newPlateButton("Clear Search", nil, func() { b.sh.dispatch("ui.setFilter", filterParams{}, nil) }))
}

// silenceGroupsLine is the page's first line: a chip per state with silences, its dot in the
// state's colour and its count, a click folding its cards; at its right how many alerts are
// silenced now and the link back to the board.
func (b *alertBoard) silenceGroupsLine(m *engine.SilencesModel) fyne.CanvasObject {
	chips := make([]fyne.CanvasObject, 0, len(m.Groups))
	for _, g := range m.Groups {
		tone := silenceTone(g.Key)
		dot := newSurface(tone)
		text := theme.ColorNameForeground
		tip := "Hide the " + strings.ToLower(g.Title) + " silences"
		if g.Folded {
			dot.fill, dot.stroke, text = "", tone, colorMuted
			tip = "Show the " + strings.ToLower(g.Title) + " silences"
		}
		dot.min = fyne.NewSize(10, 10)
		dot.radius = sizeChipRadius
		row := container.NewHBox(container.NewCenter(dot),
			newTextLine(g.Title, text, theme.SizeNameText, fyne.TextStyle{Bold: true}),
			newTextLine(strconv.Itoa(g.Count), colorMuted, theme.SizeNameText, fyne.TextStyle{}))
		key, folded := g.Key, g.Folded
		chips = append(chips, newAlertTap(b.sh, row, func() { b.fold(key, !folded, engine.FoldCard) }, tip))
	}
	var right []fyne.CanvasObject
	if m.Muted > 0 {
		right = append(right, container.NewCenter(newTextLine(alertCount(m.Muted)+" silenced",
			colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})))
	}
	right = append(right, container.NewCenter(boardLink("Alert Board", func() { b.sh.dispatch("alerts.open", struct{}{}, nil) })))
	line := container.NewBorder(nil, nil, box(layout.NewCustomPaddedHBoxLayout(18), chips...),
		container.New(layout.NewCustomPaddedHBoxLayout(12), right...))
	return inset("", theme.SizeNamePadding, line)
}

// silenceCard is one silence: its head (when it ends, its matchers) on its state's tint, then its
// comment, who made it, the alerts it mutes and Expire.
func (b *alertBoard) silenceCard(g engine.SilenceGroup, x engine.SilenceItem) fyne.CanvasObject {
	tone := silenceTone(g.Key)
	prefix, at, until := silenceUntil(x)
	when := newTextLine("", tone, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
	age := alertAge{line: when, since: at, prefix: strings.ToUpper(g.Title) + " · " + prefix, until: until}
	if !until {
		age.suffix = " ago"
	}
	b.ages = append(b.ages, age)
	when.SetText(age.text(b.sh.now()))
	head := make([]fyne.CanvasObject, 0, 1+len(x.Matchers))
	head = append(head, inset(theme.SizeNameInlineIcon, "", when))
	for _, m := range x.Matchers {
		head = append(head, b.matcherLine(m))
	}
	tint := newSurface(tone)
	tint.alpha = 0.16
	tint.radius = sizeChipRadius
	tint.topOnly = true
	parts := []fyne.CanvasObject{
		container.NewStack(tint, inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, box(layout.NewCustomPaddedVBoxLayout(4), head...))),
		inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, b.silenceBody(x)),
	}
	bg := newSurface(colorSidebar)
	bg.radius = sizeChipRadius
	return container.NewStack(bg, box(layout.NewCustomPaddedVBoxLayout(0), parts...))
}

// matcherLine is a matcher as Alertmanager writes it, the label muted and the value bold, selectable.
func (b *alertBoard) matcherLine(m engine.SilenceMatcher) fyne.CanvasObject {
	return newSpanText(theme.SizeNameText, b.copySelection,
		span{text: m.Name, color: colorMuted},
		span{text: m.Op, color: colorMuted},
		span{text: strconv.Quote(m.Value), color: theme.ColorNameForeground, style: fyne.TextStyle{Bold: true}})
}

// silenceBody is a card's body: the comment, the line of who and when, the alerts it mutes, Expire.
func (b *alertBoard) silenceBody(x engine.SilenceItem) fyne.CanvasObject {
	var body []fyne.CanvasObject
	if strings.TrimSpace(x.Comment) != "" {
		body = append(body, newSpanText(theme.SizeNameText, b.copySelection, span{text: x.Comment, color: theme.ColorNameForeground}))
	}
	tf := b.sh.timeFormat()
	info := "by " + x.CreatedBy + " · from " + tf.Format(x.StartsAt) + " to " + tf.Format(x.EndsAt)
	body = append(body, newSpanText(theme.SizeNameCaptionText, b.copySelection, span{text: info, color: colorMuted}))
	if x.Mutes > 0 {
		body = append(body, newDivider(false),
			newTextLine("Mutes "+alertCount(x.Mutes), colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true}))
		for i, a := range x.Alerts {
			if i == maxSilenceRows {
				body = append(body, newTextLine("and "+strconv.Itoa(x.Mutes-maxSilenceRows)+" more", colorMuted,
					theme.SizeNameCaptionText, fyne.TextStyle{}))
				break
			}
			body = append(body, b.mutedRow(a))
		}
	}
	idLine := newTextLine(x.ID, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	idLine.truncate = true
	id := newSelLine(idLine, nil, b.copySelection)
	if x.State != "expired" {
		silence := x.ID
		expire := newTextButton("Expire", nil, func() { b.sh.dispatch("alerts.expireSilence", silenceIDParams{ID: silence}, nil) })
		expire.Importance = widget.DangerImportance
		body = append(body, container.NewBorder(nil, nil, nil, expire, id))
	} else {
		body = append(body, id)
	}
	return box(layout.NewCustomPaddedVBoxLayout(4), body...)
}

// silenceIDParams are alerts.expireSilence's.
type silenceIDParams struct {
	ID string `json:"id"`
}

// mutedRow is an alert a silence mutes: its severity's dot, its alertname, the object it is about
// as a link into its view, since when it fires.
func (b *alertBoard) mutedRow(a engine.MutedAlert) fyne.CanvasObject {
	var left []fyne.CanvasObject
	if a.Severity != "" {
		dot := newSurface(alertTone(engine.AlertGroup{Key: a.Severity, Status: engine.AlertSeverityStatus(a.Severity)}))
		dot.min = fyne.NewSize(8, 8)
		dot.radius = sizeChipRadius
		left = append(left, container.NewCenter(dot))
	}
	name := newTextLine(a.Name, theme.ColorNameForeground, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
	left = append(left, name)
	for i, o := range a.Objects { // the namespace and the object it is about, as links into their views
		if i == 2 {
			break
		}
		left = append(left, b.objectCell(o, theme.SizeNameCaptionText))
	}
	return container.NewBorder(nil, nil, container.New(layout.NewCustomPaddedHBoxLayout(6), left...), b.age(a.Since))
}
