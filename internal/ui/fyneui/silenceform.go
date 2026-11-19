package fyneui

import (
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/alerting"
	"github.com/guided-traffic/cogpit/internal/engine"
)

// The silence form (D-166): Silence on an alert board's card or row opens it, drafted by the engine
// (alerts.silenceDraft: a card's alertname and the labels its alerts share, a row's alert's labels),
// and its Silence button creates the silence (alerts.silence) in the Alertmanager whose alerts the
// board shows. Its rows are a confirmation's (Context › Namespace › Service: the Alertmanager), then
// what the user sets: the matchers (each a row of label, operator and value, removable; Add Matcher
// adds one), how long, the author (the last one used, kept in the preferences) and the comment
// Alertmanager requires. Under them, what the matchers mute now, as the engine matches them
// (alerts.silenceMatches), the critical ones in the error colour. The form is the window's own
// confirmation of the silence: the GUI's alerts.silence asks nothing more; an agent's always asks
// (the owner). A refusal keeps the form open and says, in it, which request failed and what the
// Alertmanager or the API server answered.

// silenceDurations are the lengths the form offers; any other is typed (engine.ParseSilenceDuration).
var silenceDurations = []string{"1h", "2h", "4h", "8h", "1d", "3d", "1w"}

// maxPreviewAlerts are the alerts the form names under its count of what the matchers mute.
const maxPreviewAlerts = 6

type silenceDraftParams struct {
	Card        string `json:"card,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type silenceParams struct {
	Matchers  []engine.SilenceMatcher `json:"matchers"`
	Duration  string                  `json:"duration,omitempty"`
	Comment   string                  `json:"comment"`
	CreatedBy string                  `json:"createdBy,omitempty"`
}

type silenceMatchParams struct {
	Matchers []engine.SilenceMatcher `json:"matchers"`
}

// openSilenceForm drafts a silence of a card ("<severity>/<alertname>") or of one alert (its
// fingerprint) and shows the form.
func (sh *shell) openSilenceForm(card, fingerprint string) {
	sh.dispatch("alerts.silenceDraft", silenceDraftParams{Card: card, Fingerprint: fingerprint}, func(res any, err error) {
		if err != nil {
			sh.showError(err)
			return
		}
		var d engine.SilenceDraft
		if decode(res, &d) != nil {
			return
		}
		sh.showSilenceForm(d)
	})
}

// silenceForm is a shown silence form.
type silenceForm struct {
	sh       *shell
	d        *dialogFrame
	matchers *fyne.Container // the matcher rows
	rows     []*matcherRow
	duration *widget.SelectEntry
	until    *textLine
	author   *fieldEntry
	comment  *fieldEntry
	mutes    *fyne.Container
	problem  *paragraph
	confirm  *textButton
	valueW   float32
	asked    int // the preview asked last; an older answer is dropped
	sending  bool
}

// matcherRow is one matcher's row: its label, operator and value, and its remove button.
type matcherRow struct {
	name  *fieldEntry
	op    *selectField
	value *fieldEntry
	root  fyne.CanvasObject
}

// showSilenceForm builds and shows the form for a draft.
func (sh *shell) showSilenceForm(draft engine.SilenceDraft) *silenceForm {
	th := sh.app.Settings().Theme()
	width := max(th.Size(sizeConfirmWidth), 720)
	f := &silenceForm{sh: sh, matchers: container.NewVBox(), mutes: container.NewVBox()}
	f.valueW = width - th.Size(sizeDrawerLabel) - 2*th.Size(theme.SizeNamePadding)
	for _, m := range draft.Matchers {
		f.addMatcher(m)
	}
	if len(f.rows) == 0 {
		f.addMatcher(engine.SilenceMatcher{Name: "alertname", Op: alerting.OpEqual})
	}
	add := newTextButton("Add Matcher", theme.ContentAddIcon(), func() {
		f.addMatcher(engine.SilenceMatcher{Op: alerting.OpEqual})
		f.changed()
	})
	f.duration = widget.NewSelectEntry(silenceDurations)
	f.duration.SetText(draft.Duration)
	f.until = newTextLine("", colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	f.duration.OnChanged = func(string) { f.changed() }
	f.author = newFieldEntry()
	f.author.SetText(draft.CreatedBy)
	f.author.SetPlaceHolder("Your name")
	f.author.OnChanged = func(string) { f.check() }
	f.comment = newFieldMultiLineEntry()
	f.comment.SetMinRowsVisible(3)
	f.comment.Wrapping = fyne.TextWrapWord
	f.comment.SetPlaceHolder("Why the alerts are muted (Alertmanager shows it to everyone)")
	f.comment.OnChanged = func(string) { f.check() }
	f.problem = newParagraph("", colorStatusError, f.valueW)
	f.problem.Hide()

	rows := box(kvLayout{})
	ns, svc, _ := strings.Cut(draft.Where, "/")
	svc, _, _ = strings.Cut(svc, ":")
	placeRows(rows, draft.Context, []string{ns}, []namedObject{{kind: "Service", name: svc}}, f.valueW)
	durationRow := container.NewBorder(nil, nil, nil, container.NewCenter(f.until), sized(sizeFilterWidth, "", f.duration))
	rows.Objects = append(rows.Objects,
		rowName("Matchers"), container.NewVBox(f.matchers, container.NewHBox(add)),
		rowName("For"), durationRow,
		rowName("Author"), f.author,
		rowName("Comment"), f.comment,
		rowName("Mutes"), f.mutes,
	)
	f.confirm = newPlateButton("Silence", nil, nil)
	f.confirm.Importance = widget.HighImportance
	cancel := newPlateButton("Cancel", nil, nil)
	f.d = newDialogFrame(sh, dialogTitle("Silence Alerts"))
	// At least the form's width, so that fitting it to its content (dialogFrame.fit) never narrows it.
	f.d.setContent(container.New(textWidth{w: width - th.Size(sizeDialogPadX)}, container.NewVBox(rows, f.problem)))
	f.d.SetButtons([]fyne.CanvasObject{cancel, f.confirm})
	cancel.OnTapped = f.d.Hide
	f.confirm.OnTapped = f.send
	f.changed()
	f.d.Show()
	f.d.Resize(fyne.NewSize(width, f.d.MinSize().Height))
	return f
}

// addMatcher adds a matcher's row.
func (f *silenceForm) addMatcher(m engine.SilenceMatcher) {
	r := &matcherRow{name: newFieldEntry(), value: newFieldEntry()}
	r.name.SetText(m.Name)
	r.name.SetPlaceHolder("label")
	r.value.SetText(m.Value)
	r.value.SetPlaceHolder("value")
	op := m.Op
	if op == "" {
		op = alerting.OpEqual
	}
	r.op = newSelectField(alerting.Ops, nil)
	r.op.SetSelected(op)
	r.op.OnChanged = func(string) { f.changed() }
	r.name.OnChanged = func(string) { f.changed() }
	r.value.OnChanged = func(string) { f.changed() }
	remove := newTipButton(f.sh, theme.ContentRemoveIcon(), "Remove the matcher", nil)
	remove.OnTapped = func() {
		for i, x := range f.rows {
			if x == r {
				f.rows = append(f.rows[:i], f.rows[i+1:]...)
				f.matchers.Remove(r.root)
				break
			}
		}
		f.changed()
		f.d.fit()
	}
	left := container.NewHBox(container.New(textWidth{w: 150}, r.name), container.New(textWidth{w: 76}, r.op))
	r.root = container.NewBorder(nil, nil, left, remove, r.value)
	f.rows = append(f.rows, r)
	f.matchers.Add(r.root)
	if f.d != nil {
		f.d.fit()
	}
}

// matcherParams are the rows as alerts.silence takes them, the rows without a label left out.
func (f *silenceForm) matcherParams() []engine.SilenceMatcher {
	var out []engine.SilenceMatcher
	for _, r := range f.rows {
		if strings.TrimSpace(r.name.Text) == "" {
			continue
		}
		out = append(out, engine.SilenceMatcher{Name: strings.TrimSpace(r.name.Text), Op: r.op.Selected, Value: r.value.Text})
	}
	return out
}

// changed follows an edit of the matchers or the duration: the end it gives, the preview of what
// they mute, the Silence button.
func (f *silenceForm) changed() {
	if d, err := engine.ParseSilenceDuration(f.duration.Text); err == nil {
		f.until.SetText("until " + f.sh.timeFormat().Format(f.sh.now().Add(d)))
	} else {
		f.until.SetText("")
	}
	f.check()
	ms := f.matcherParams()
	f.asked++
	asked := f.asked
	if len(ms) == 0 {
		f.showMutes(nil, "")
		return
	}
	if _, err := engine.Matchers(ms); err != nil {
		f.showMutes(nil, err.Error())
		return
	}
	f.sh.dispatch("alerts.silenceMatches", silenceMatchParams{Matchers: ms}, func(res any, err error) {
		if asked != f.asked {
			return
		}
		if err != nil {
			f.showMutes(nil, err.Error())
			return
		}
		var m engine.SilenceMatches
		if decode(res, &m) == nil {
			f.showMutes(&m, "")
		}
	})
}

// showMutes shows what the matchers mute now: the count, the critical ones in the error colour, the
// first alerts by name and object; or why it cannot tell.
func (f *silenceForm) showMutes(m *engine.SilenceMatches, why string) {
	caption := func(text string, c fyne.ThemeColorName) *textLine {
		l := newTextLine(text, c, theme.SizeNameCaptionText, fyne.TextStyle{})
		l.truncate = true
		return l
	}
	var objs []fyne.CanvasObject
	switch {
	case why != "":
		objs = append(objs, newParagraph(why, colorStatusError, f.valueW))
	case m == nil:
		objs = append(objs, caption("No matcher yet", colorMuted))
	default:
		head := alertCount(m.Shown) + " shown now"
		c := theme.ColorNameForeground
		if m.Critical > 0 {
			head += ", " + strconv.Itoa(m.Critical) + " critical"
			c = colorStatusError
		}
		if m.Muted > 0 {
			head += " · " + strconv.Itoa(m.Muted) + " silenced already"
		}
		objs = append(objs, newTextLine(head, c, theme.SizeNameText, fyne.TextStyle{Bold: m.Critical > 0}))
		for i, a := range m.Alerts {
			if i == maxPreviewAlerts {
				objs = append(objs, caption("and "+strconv.Itoa(m.Shown+m.Muted-maxPreviewAlerts)+" more", colorMuted))
				break
			}
			text := a.Name
			for _, o := range a.Objects {
				if o.Label != paramNamespace {
					text += " · " + o.Ref.Name
					break
				}
			}
			if a.Silenced {
				text += " (silenced)"
			}
			objs = append(objs, caption(text, colorMuted))
		}
	}
	f.mutes.Objects = objs
	f.mutes.Refresh()
	if f.d != nil {
		f.d.fit()
	}
}

// check enables Silence while the form holds what Alertmanager requires.
func (f *silenceForm) check() {
	_, durErr := engine.ParseSilenceDuration(f.duration.Text)
	ok := !f.sending && durErr == nil && len(f.matcherParams()) > 0 &&
		strings.TrimSpace(f.author.Text) != "" && strings.TrimSpace(f.comment.Text) != ""
	if ok {
		f.confirm.Enable()
	} else {
		f.confirm.Disable()
	}
}

// send creates the silence; a refusal stays in the form, which says what failed.
func (f *silenceForm) send() {
	p := silenceParams{Matchers: f.matcherParams(), Duration: strings.TrimSpace(f.duration.Text),
		Comment: strings.TrimSpace(f.comment.Text), CreatedBy: strings.TrimSpace(f.author.Text)}
	f.sending = true
	f.check()
	f.problem.Hide()
	f.sh.dispatch("alerts.silence", p, func(res any, err error) {
		f.sending = false
		f.check()
		if err != nil {
			f.problem.text = err.Error()
			f.problem.Refresh()
			f.problem.Show()
			f.d.fit()
			return
		}
		var c engine.SilenceCreated
		_ = decode(res, &c)
		f.d.Hide()
		f.sh.toast("info", "Silenced "+alertCount(c.Mutes)+" until "+f.sh.timeFormat().FormatClock(c.EndsAt))
	})
}

// alertCount is "1 alert", "3 alerts".
func alertCount(n int) string {
	if n == 1 {
		return "1 alert"
	}
	return strconv.Itoa(n) + " alerts"
}

// silenceUntil says when a silence ends or ended, or when it starts: "ends in 1h", "starts in 3h",
// "ended 2h ago", for the silences page's heads (refreshed by the shell's tick).
func silenceUntil(x engine.SilenceItem) (prefix string, at time.Time, until bool) {
	switch x.State {
	case string(alerting.SilencePending):
		return "starts in ", x.StartsAt, true
	case string(alerting.SilenceActive):
		return "ends in ", x.EndsAt, true
	}
	return "ended ", x.EndsAt, false
}
