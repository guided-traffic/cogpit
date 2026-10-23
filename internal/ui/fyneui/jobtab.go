package fyneui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A job tab (engine/jobs.go; A91, G4): a background job's progress in the dock, as the engine's
// state holds it (DockTab.Job), so that the window shows what agents read with ui_state. A header
// with the job's state, its times, its progress while it runs ("120 of 500 done, 2 failed") and its
// outcome once it ended, and a Cancel button while it runs (job.cancel: the steps not started are
// not attempted, the tab stays); then a row per step: its state's icon, what it acts on, and its
// detail ("Waiting for its controller: Reconciling: …", "Deleted", "Not attempted: cancelled"), in
// the state's colour. Closing the tab stops the job too (dock.close). The look is Cogpit's own:
// Lens has no such tab.

// jobStateStyles are a job's and a step's states: their label, colour and icon (by name, resolved
// when drawn: a theme icon asked for at package initialisation asks Fyne for an app none has
// started yet, TestStartUpLogsNoFyneError).
var jobStateStyles = map[engine.JobState]struct {
	label string
	color fyne.ThemeColorName
	icon  fyne.ThemeIconName
}{
	engine.JobPending:   {"Waiting", colorMuted, theme.IconNameRadioButton},
	engine.JobRunning:   {"Running", colorAccent, theme.IconNameViewRefresh},
	engine.JobDone:      {"Done", colorStatusOK, theme.IconNameConfirm},
	engine.JobFailed:    {jobFailedLabel, colorStatusError, theme.IconNameError},
	engine.JobCancelled: {"Stopped", colorMuted, theme.IconNameMediaStop},
}

// jobFailedLabel is a failed job's or step's state.
const jobFailedLabel = "Failed"

// jobCancelCommand stops a job and keeps its tab (commands/jobs.go).
const jobCancelCommand = "job.cancel"

// jobStyle returns a state's style; an unknown state shows as running, with its own name.
func jobStyle(st engine.JobState) (label string, c fyne.ThemeColorName, icon fyne.Resource) {
	s, ok := jobStateStyles[st]
	if !ok {
		s = jobStateStyles[engine.JobRunning]
		s.label = string(st)
	}
	return s.label, s.color, themedIcon(theme.Icon(s.icon), s.color)
}

// jobProgress is a running job's progress: "120 of 500 done, 2 failed".
func jobProgress(c engine.JobCounts) string {
	parts := []string{fmt.Sprintf("%s of %s done", formatCount(int64(c.Done)), formatCount(int64(c.Total)))}
	if c.Failed > 0 {
		parts = append(parts, formatCount(int64(c.Failed))+" failed")
	}
	if c.Cancelled > 0 {
		parts = append(parts, formatCount(int64(c.Cancelled))+" not attempted")
	}
	return strings.Join(parts, ", ")
}

// jobTab is a job tab's content.
type jobTab struct {
	sh      *shell
	id      string
	root    *fyne.Container
	icon    *widget.Icon
	state   *textLine
	times   *textLine
	outcome *textLine
	cancel  *textButton
	steps   *fyne.Container
	scroll  *container.Scroll
	rows    []*jobStepRow
	shown   engine.JobState // the header's state as drawn
	// The list follows the step under way (at, -1: none), keeping it in view as a long job moves
	// on, until the user scrolls it; auto is the offset it set last.
	follow bool
	at     int
	auto   fyne.Position
}

// jobStepRow is one step's row.
type jobStepRow struct {
	root   *fyne.Container
	icon   *widget.Icon
	text   *textLine
	detail *textLine
	state  engine.JobState // as drawn: the icon is set again only when it changes
}

func newJobTab(sh *shell, t engine.DockTab) *jobTab {
	j := &jobTab{sh: sh, id: t.ID, icon: widget.NewIcon(nil), follow: true, at: -1}
	j.state = newTextLine("", colorAccent, theme.SizeNameText, fyne.TextStyle{Bold: true})
	j.times = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	j.outcome = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	j.outcome.truncate = true
	j.cancel = newTextButton("Cancel", theme.MediaStopIcon(), func() {
		j.cancel.Disable() // until the engine says how the job ended
		sh.dispatch(jobCancelCommand, jobParams{ID: j.id}, func(_ any, err error) {
			if err != nil {
				j.cancel.Enable()
				sh.showError(err)
			}
		})
	})
	j.steps = container.NewVBox()
	j.scroll = container.NewVScroll(inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, j.steps))
	j.scroll.OnScrolled = func(p fyne.Position) { // the user scrolled: the list stays where they put it
		if p != j.auto {
			j.follow = false
		}
	}
	head := container.NewBorder(nil, nil,
		container.NewHBox(j.icon, j.state, j.times, newHGap(theme.SizeNameInnerPadding)), j.cancel, j.outcome)
	j.root = container.NewBorder(
		container.NewVBox(inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, head), newDivider(false)),
		nil, nil, nil, container.New(followLayout{j}, j.scroll))
	j.update(t)
	return j
}

// followLayout lays the steps' list out to its whole size, then keeps the step under way in view.
type followLayout struct{ j *jobTab }

func (l followLayout) MinSize(objs []fyne.CanvasObject) fyne.Size { return objs[0].MinSize() }

func (l followLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	objs[0].Move(fyne.NewPos(0, 0))
	objs[0].Resize(size)
	l.j.followRunning()
}

// followRunning scrolls the list so that the step under way shows at its bottom edge, when it is
// out of view and the user has not scrolled the list.
func (j *jobTab) followRunning() {
	view := j.scroll.Size().Height
	if !j.follow || j.at < 0 || j.at >= len(j.rows) || view <= 0 {
		return
	}
	r := j.rows[j.at].root
	pad := j.steps.Position().Y
	top := pad + r.Position().Y
	bottom := top + r.Size().Height + pad
	off := j.scroll.Offset
	if top >= off.Y && bottom <= off.Y+view {
		return
	}
	off.Y = max(0, min(bottom-view, j.scroll.Content.MinSize().Height-view))
	j.auto = off
	j.scroll.ScrollToOffset(off)
}

// jobParams are job.cancel's parameters.
type jobParams struct {
	ID string `json:"id"`
}

// update shows the job as the engine's state holds it.
func (j *jobTab) update(t engine.DockTab) {
	job := t.Job
	if job == nil {
		job = &engine.JobTab{State: engine.JobRunning}
	}
	j.updateHeader(job)
	for len(j.rows) < len(job.Steps) {
		r := &jobStepRow{icon: widget.NewIcon(nil)}
		r.text = newTextLine("", theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
		r.detail = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
		r.detail.truncate = true
		r.root = container.NewBorder(nil, nil, container.NewHBox(r.icon, r.text), nil, r.detail)
		j.rows = append(j.rows, r)
		j.steps.Add(r.root)
	}
	j.at = -1
	for i, r := range j.rows {
		if i >= len(job.Steps) {
			r.root.Hide()
			continue
		}
		st := job.Steps[i]
		if st.State == engine.JobRunning && j.at < 0 {
			j.at = i
		}
		_, c, icon := jobStyle(st.State)
		if r.state != st.State || r.icon.Resource == nil {
			r.icon.SetResource(icon)
			r.state = st.State
		}
		r.text.SetText(st.Text)
		r.detail.SetText(st.Detail)
		if st.State == engine.JobPending || st.State == engine.JobDone {
			c = colorMuted
		}
		r.detail.SetColor(c)
		r.root.Show()
	}
	j.followRunning()
}

// updateHeader shows the job's state, times, progress or outcome, and Cancel while it runs.
func (j *jobTab) updateHeader(job *engine.JobTab) {
	label, c, icon := jobStyle(job.State)
	if j.shown != job.State || j.icon.Resource == nil {
		j.icon.SetResource(icon)
		j.shown = job.State
	}
	j.state.SetText(label)
	j.state.SetColor(c)
	tf := j.sh.timeFormat()
	times := ""
	if !job.Started.IsZero() {
		times = "started " + tf.Format(job.Started)
	}
	if !job.Ended.IsZero() {
		times += ", ended " + tf.FormatClock(job.Ended)
	}
	j.times.SetText(times)
	switch {
	case job.State.Ended():
		j.outcome.SetText(job.Outcome)
	case len(job.Steps) > 0:
		counts := job.Counts
		if counts.Total == 0 {
			counts = engine.CountJobSteps(job.Steps)
		}
		j.outcome.SetText(jobProgress(counts))
	default:
		j.outcome.SetText("")
	}
	if job.State == engine.JobFailed {
		j.outcome.SetColor(colorStatusError)
	} else {
		j.outcome.SetColor(colorMuted)
	}
	if job.State.Ended() {
		j.cancel.Hide()
	} else {
		j.cancel.Show()
	}
}

// remote and close make the job tab a dockEditor; it has nothing to follow or stop.
func (j *jobTab) remote(uint64) {}
func (j *jobTab) close()        {}
