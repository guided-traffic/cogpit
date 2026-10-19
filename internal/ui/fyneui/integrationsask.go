package fyneui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The question of a new cluster's integrations (D-76, D-82; engine/integrationsask.go,
// engine/integrationsconsent.go): while the UI state carries it (UIState.IntegrationsQuestion), a
// dialog over the window asks which integrations Cogpit uses for the shown cluster. It holds up
// nothing: the connect has finished and the views load behind it. It follows the question's step:
//
//   - the pre-choice (the owner's WF5 of 2026-10-17: "eine kurze Vorauswahl", since the "Suchen"
//     run with the user's privileges): why it asks, and three choices, "Check all
//     integrations" (or "Check the default integrations" while the default for new clusters
//     switches some off), "Choose integrations" and "Don't use integrations", each with a line of
//     what it does; Later. Nothing is looked for.
//   - the rows (Check all, Choose): the same rows as Cluster Settings › Integrations (a checkbox with
//     the label, the status line in the status colours, live, and what it adds), ticked as the
//     engine's IntegrationsLookFor says; a tick dispatches cluster.integrations.lookFor, and the
//     integration is looked for at once; an unticked row reads "Not looked for: tick it to look for
//     it". The checkbox "Save as the default for new clusters" (off until ticked), Back, Later and
//     Apply.
//   - Don't use: a warning over the pre-choice, "Use <name> without integrations?", that names what
//     goes and what stays (the lead's correction of the owner's wording: the cluster's resources
//     still show); Yes answers with every integration off, No (or closing it) goes back to the
//     pre-choice.
//
// Every button is an engine command (cluster.integrations.step, .lookFor, .answer, .later), so
// agents see the same state; only the window steps, ticks and answers (GUI-only, H-126), an agent
// may put the question off (Later). The dialog closes when the engine drops the question, whatever
// ended it (an answer, Later, a choice made in Cluster Settings › Integrations).

const (
	integrationsAskSave    = "Save as the default for new clusters"
	integrationsAskWhy     = "Cogpit can read tools this cluster runs to show more: metrics, alerts, image ratings and policy marks. It looks for them with your identity and never writes to them. Nothing is looked for until you choose."
	integrationsAskAll     = "Check all integrations"
	integrationsAskDefault = "Check the default integrations"
	integrationsAskChoose  = "Choose integrations"
	integrationsAskNone    = "Don't use integrations"
	// What each choice does, the line under its button.
	integrationsAskAllHint    = "Looks for all of them now; untick what you don't want before you apply."
	integrationsAskChooseHint = "Looks for nothing until you tick an integration."
	integrationsAskNoneHint   = "The cluster's resources still show; no metrics, alerts, image ratings or policy marks."
	// The rows' text after each choice.
	integrationsAskRowsAll    = "Cogpit looks for each ticked integration now, with your identity, and never writes to it. Untick what you don't want, then apply."
	integrationsAskRowsChoose = "Tick an integration to look for it: Cogpit looks with your identity and never writes to it. Apply keeps your choice."
	// integrationsAskUnticked is an unticked row's status: nothing is asked for it.
	integrationsAskUnticked = "Not looked for: tick it to look for it"
	// The warning of Don't use.
	integrationsAskNoneText = "Cogpit then shows no metrics, alerts, image ratings or policy marks for %s; its resources still show. Cluster Settings › Integrations turns them on later."
)

// integrationsQuestion is the window's dialog of the question.
type integrationsQuestion struct {
	sh   *shell
	ctx  string                  // the context the open dialog asks about; "" while none is open
	page engine.IntegrationsStep // the page it shows: the pre-choice, or the rows of Check all or Choose

	d       *dialog.CustomDialog
	choices []*widget.Button // the pre-choice's Check all, Choose and Don't use
	rows    []*integrationRow
	save    *widget.Check
	apply   *widget.Button
	back    *widget.Button
	later   *widget.Button
	errLine *textLine
	warn    *dialog.CustomDialog // Don't use's warning, over the pre-choice; nil while not shown
	yes, no *widget.Button       // the warning's
	lookFor engine.IntegrationSet
	seq     int  // the latest status request; older answers are dropped
	closing bool // the dialog is being closed because the question moved on: no Later, no No
	syncing bool // the rows' ticks are set from the UI state: no lookFor
}

func newIntegrationsQuestion(sh *shell) *integrationsQuestion { return &integrationsQuestion{sh: sh} }

// integrationsQuestionChanged reports whether the question changed between two UI states: its
// cluster, its step or its ticks.
func integrationsQuestionChanged(st, old engine.UIState) bool {
	return st.IntegrationsQuestion != old.IntegrationsQuestion || st.IntegrationsStep != old.IntegrationsStep ||
		st.IntegrationsLookFor != old.IntegrationsLookFor
}

// questionPage is the page the dialog shows for a step: the rows of Check all and Choose, else the
// pre-choice (under Don't use's warning too).
func questionPage(step engine.IntegrationsStep) engine.IntegrationsStep {
	if step == engine.IntegrationsCheckAll || step == engine.IntegrationsChoose {
		return step
	}
	return engine.IntegrationsPreChoice
}

// stateChanged follows the question: opens the dialog, turns its page, ticks its rows, shows or
// closes the warning, closes it, or asks about another cluster. Main goroutine.
func (q *integrationsQuestion) stateChanged() {
	ui := q.sh.ui
	want, page := ui.IntegrationsQuestion, questionPage(ui.IntegrationsStep)
	if want != q.ctx || (want != "" && page != q.page) {
		q.close()
		if want != "" {
			q.open(want, page)
		}
	}
	if q.ctx == "" {
		return
	}
	q.showWarning(ui.IntegrationsStep == engine.IntegrationsDontUse)
	if ui.IntegrationsLookFor != q.lookFor {
		q.lookFor = ui.IntegrationsLookFor
		q.syncTicks()
		q.request()
	}
}

// close closes the dialog and the warning without answering.
func (q *integrationsQuestion) close() {
	q.closing = true
	if q.warn != nil {
		q.warn.Hide()
	}
	if q.d != nil {
		q.d.Hide()
	}
	q.closing = false
	q.d, q.warn, q.ctx, q.page, q.rows, q.choices, q.lookFor = nil, nil, "", "", nil, nil, 0
}

// open shows the question for ctx on a page.
func (q *integrationsQuestion) open(ctx string, page engine.IntegrationsStep) {
	sh := q.sh
	q.ctx, q.page, q.lookFor = ctx, page, sh.ui.IntegrationsLookFor
	q.errLine = newTextLine("", colorStatusError, theme.SizeNameCaptionText, fyne.TextStyle{})
	q.errLine.truncate = true
	q.errLine.Hide()
	q.later = widget.NewButton("Later", q.putOff)
	var content fyne.CanvasObject
	var buttons []fyne.CanvasObject
	if page == engine.IntegrationsPreChoice {
		content = q.preChoice()
		buttons = []fyne.CanvasObject{q.later}
	} else {
		content = q.rowsPage(page)
		buttons = []fyne.CanvasObject{q.back, q.later, q.apply}
	}
	q.d = dialog.NewCustomWithoutButtons("Integrations for "+sh.identity(ctx).name, sized(sizePickerWidth, "", content), sh.win)
	q.d.SetButtons(buttons)
	d := q.d
	q.d.SetOnClosed(func() {
		if !q.closing && q.d == d { // closed otherwise than by the question's end: Later
			q.putOff()
		}
	})
	q.d.Show()
	if page != engine.IntegrationsPreChoice {
		q.request()
	}
}

// preChoice builds the pre-choice: why it asks, and the three choices with what each does.
func (q *integrationsQuestion) preChoice() fyne.CanvasObject {
	sh := q.sh
	width := sh.app.Settings().Theme().Size(sizePickerWidth)
	all, allHint := q.checkAllText()
	choice := func(label, hint string, step engine.IntegrationsStep) []fyne.CanvasObject {
		b := widget.NewButton(label, func() { q.step(step) })
		b.Alignment = widget.ButtonAlignLeading
		q.choices = append(q.choices, b)
		line := newTextLine(hint, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
		line.truncate = true
		return []fyne.CanvasObject{b, inset(theme.SizeNameInnerPadding, "", line), newSpace(theme.SizeNamePadding)}
	}
	objs := []fyne.CanvasObject{newParagraph(integrationsAskWhy, theme.ColorNameForeground, width), newSpace(theme.SizeNameInnerPadding)}
	objs = append(objs, choice(all, allHint, engine.IntegrationsCheckAll)...)
	objs = append(objs, choice(integrationsAskChoose, integrationsAskChooseHint, engine.IntegrationsChoose)...)
	objs = append(objs, choice(integrationsAskNone, integrationsAskNoneHint, engine.IntegrationsDontUse)...)
	q.choices[0].Importance = widget.HighImportance
	return container.NewVBox(append(objs, q.errLine)...)
}

// checkAllText is the first choice's label and line: all of them, or those the default for new
// clusters keeps on, named, while it switches some off.
func (q *integrationsQuestion) checkAllText() (string, string) {
	var off engine.IntegrationSet
	if p := q.sh.ui.Preferences; p != nil {
		off = p.IntegrationsOff & engine.IntAll
	}
	if off == 0 {
		return integrationsAskAll, integrationsAskAllHint
	}
	var labels []string
	for _, in := range engine.Integrations {
		if off&in.Set == 0 {
			labels = append(labels, in.Label)
		}
	}
	if len(labels) == 0 {
		return integrationsAskDefault, "Your default for new clusters has every integration off: nothing is looked for."
	}
	return integrationsAskDefault, "Looks for " + andList(labels) + " now, your default for new clusters; untick what you don't want."
}

// andList joins names as a sentence does: "a", "a and b", "a, b and c".
func andList(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// rowsPage builds the rows of Check all or Choose: what happens, a row per integration ticked as
// the engine says, the checkbox for the default, and Back and Apply.
func (q *integrationsQuestion) rowsPage(page engine.IntegrationsStep) fyne.CanvasObject {
	sh := q.sh
	text := integrationsAskRowsChoose
	if page == engine.IntegrationsCheckAll {
		text = integrationsAskRowsAll
	}
	objs := make([]fyne.CanvasObject, 0, 6+len(engine.Integrations))
	objs = append(objs, newParagraph(text, theme.ColorNameForeground, sh.app.Settings().Theme().Size(sizePickerWidth)),
		newSpace(theme.SizeNameInnerPadding))
	q.rows = nil
	for _, in := range engine.Integrations {
		name := in.Name
		r := newIntegrationRow(in, nil)
		r.check.OnChanged = func(on bool) {
			showAsked(r, r.last)
			if !q.syncing {
				q.tick(name, on)
			}
		}
		q.rows = append(q.rows, r)
		objs = append(objs, r.box)
	}
	q.syncTicks()
	q.save = widget.NewCheck(integrationsAskSave, nil)
	objs = append(objs, newSpace(theme.SizeNameInnerPadding), newDivider(false), q.save, q.errLine)
	q.apply = widget.NewButton("Apply", q.answer)
	q.apply.Importance = widget.HighImportance
	q.back = widget.NewButton("Back", func() { q.step(engine.IntegrationsPreChoice) })
	return container.NewVBox(objs...)
}

// syncTicks ticks the rows as the engine's IntegrationsLookFor says.
func (q *integrationsQuestion) syncTicks() {
	q.syncing = true
	defer func() { q.syncing = false }()
	for _, r := range q.rows {
		in, _ := engine.IntegrationOf(r.name)
		r.check.SetChecked(q.lookFor&in.Set != 0)
		showAsked(r, r.last)
	}
}

// request reads the integrations' statuses of the asked cluster (cluster.settings.get).
func (q *integrationsQuestion) request() {
	if q.ctx == "" || len(q.rows) == 0 {
		return
	}
	q.seq++
	seq, ctx := q.seq, q.ctx
	q.sh.dispatch("cluster.settings.get", clusterSettingsContext{Context: ctx}, func(res any, err error) {
		var v engine.ClusterSettingsView
		if seq != q.seq || ctx != q.ctx || err != nil || decode(res, &v) != nil {
			return
		}
		for _, st := range v.Integrations {
			for _, r := range q.rows {
				if r.name == st.Name {
					showAsked(r, st)
				}
			}
		}
	})
}

// showAsked shows a status in the question's row as its tick makes it: unticked, nothing is looked
// for; ticked while the engine still says off, it is being looked for.
func showAsked(r *integrationRow, st engine.IntegrationStatus) {
	r.setStatus(st)
	switch {
	case !r.check.Checked:
		r.status.SetText(integrationsAskUnticked)
		r.status.SetColor(colorMuted)
	case st.State == engine.IntegrationOff || st.State == "":
		text, c := integrationStatusLine(engine.IntegrationStatus{State: engine.IntegrationChecking})
		r.status.SetText(text)
		r.status.SetColor(c)
	}
}

// statusesChanged reads the statuses again when they may have changed for the asked cluster ("":
// any cluster's).
func (q *integrationsQuestion) statusesChanged(ctx string) {
	if q.ctx != "" && (ctx == "" || ctx == q.ctx) {
		q.request()
	}
}

// step dispatches a move of the question: a pre-choice, Back, or the warning's No.
func (q *integrationsQuestion) step(step engine.IntegrationsStep) {
	if q.ctx == "" {
		return
	}
	ctx := q.ctx
	q.sh.dispatchInOrder("cluster.integrations.step", map[string]any{contextParam: ctx, "step": string(step)},
		func(_ any, err error) { q.failed(ctx, err) })
}

// tick dispatches a row's tick: looked for at once, or asked nothing more.
func (q *integrationsQuestion) tick(name engine.Integration, on bool) {
	if q.ctx == "" {
		return
	}
	ctx := q.ctx
	q.sh.dispatchInOrder("cluster.integrations.lookFor", map[string]any{contextParam: ctx, "integrations": map[string]bool{string(name): on}},
		func(_ any, err error) { q.failed(ctx, err) })
}

// answer dispatches the ticks as the cluster's choice (Apply).
func (q *integrationsQuestion) answer() {
	if q.ctx == "" {
		return
	}
	switches := map[string]bool{}
	for _, r := range q.rows {
		switches[string(r.name)] = r.check.Checked
	}
	q.answerWith(switches, q.save != nil && q.save.Checked)
}

// answerWith dispatches an answer.
func (q *integrationsQuestion) answerWith(switches map[string]bool, saveAsDefault bool) {
	ctx := q.ctx
	q.sh.dispatchInOrder("cluster.integrations.answer", map[string]any{contextParam: ctx, "integrations": switches,
		"saveAsDefault": saveAsDefault}, func(_ any, err error) { q.failed(ctx, err) })
}

// putOff dispatches Later.
func (q *integrationsQuestion) putOff() {
	if q.ctx == "" {
		return
	}
	ctx := q.ctx
	q.sh.dispatchInOrder("cluster.integrations.later", map[string]any{contextParam: ctx}, func(_ any, err error) { q.failed(ctx, err) })
}

// showWarning shows Don't use's warning over the pre-choice, or closes it.
func (q *integrationsQuestion) showWarning(show bool) {
	if !show {
		if q.warn != nil {
			q.closing = true
			q.warn.Hide()
			q.closing = false
			q.warn = nil
		}
		return
	}
	if q.warn != nil {
		return
	}
	sh := q.sh
	name := sh.identity(q.ctx).name
	text := fmt.Sprintf(integrationsAskNoneText, name)
	q.no = widget.NewButton("No", func() { q.step(engine.IntegrationsPreChoice) })
	q.yes = widget.NewButton("Yes", func() {
		off := map[string]bool{}
		for _, in := range engine.Integrations {
			off[string(in.Name)] = false
		}
		q.answerWith(off, false)
	})
	q.yes.Importance = widget.WarningImportance
	q.warn = dialog.NewCustomWithoutButtons("Use "+name+" without integrations?",
		newParagraph(text, theme.ColorNameForeground, sh.app.Settings().Theme().Size(sizeConfirmWidth)), sh.win)
	q.warn.SetButtons([]fyne.CanvasObject{q.no, q.yes})
	w := q.warn
	q.warn.SetOnClosed(func() {
		if !q.closing && q.warn == w { // closed otherwise than by the question moving on: No
			q.step(engine.IntegrationsPreChoice)
		}
	})
	q.warn.Show()
}

// failed shows why a step or an answer was refused under the dialog's content; the dialog stays.
func (q *integrationsQuestion) failed(ctx string, err error) {
	if err == nil || ctx != q.ctx || q.errLine == nil {
		return
	}
	q.errLine.SetText(err.Error())
	q.errLine.Show()
	if q.d != nil && q.sh.ui.IntegrationsQuestion == ctx {
		q.d.Show() // Later by closing it: shown again while the question stands
	}
}
