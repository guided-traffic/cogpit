package fyneui

import (
	"errors"
	"strconv"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Lens's Rollback dialog (captured 2026-10-12): "Rollback <name>", a REVISION dropdown of every
// revision, newest first, the current one selected, each "12 - <chart>-<version> - <appVersion>,
// updated: 12/10/2026, 08:30:16", then Cancel and Rollback, and no second confirmation. Rollback
// stays enabled with the current revision selected; what Lens does then is not captured. Cogpit
// rolls back to it as helm does: a new revision equal to the current one, its manifest applied
// again; the dialog's row "Writes" says so for every choice.
//
// Rollback is destructive, so the engine asks the GUI's confirmer for every origin (D-2). The
// dialog is that confirmation: it names the release, its namespace and its context in the rows
// Cogpit's confirmations have (D-85, the release as "Release → name", WF32), and when the user
// picks Rollback it hands the confirmer the release's newest
// revision as it showed it (its release Secret's UID). The engine's confirmation of exactly that
// command, from this window, on that release in that context, then takes the user's answer from
// the dialog instead of asking again; one whose release moved to another revision while the dialog
// was open is refused, as the engine refuses a confirmed rollback whose release moved. Agents'
// requests never match: they carry their own origin.

func init() { itemActionDialogs["helm.release.rollback"] = openRollbackDialog }

// rollbackCommand is the command the dialog confirms.
const rollbackCommand = "helm.release.rollback"

// dialogConfirmWindow is how long a dialog's answer waits for the engine's confirmation of it: the
// dispatch follows the click at once.
const dialogConfirmWindow = 10 * time.Second

// releaseHistory mirrors helm.release.history's result.
type releaseHistory struct {
	Ref      model.ObjectRef     `json:"ref"`
	Revision int                 `json:"revision"`
	UID      string              `json:"uid"`
	History  []helm.HistoryEntry `json:"history"`
}

// openRollbackDialog reads the release's history from the cache and shows the dialog.
func openRollbackDialog(sh *shell, ref model.ObjectRef, _ model.ItemAction) {
	sh.dispatch("helm.release.history", refParams{Ref: ref}, func(res any, err error) {
		var h releaseHistory
		if err == nil {
			err = decode(res, &h)
		}
		if err != nil {
			sh.showError(err)
			return
		}
		sh.showRollbackDialog(h)
	})
}

// rollbackDialog is a shown Rollback dialog.
type rollbackDialog struct {
	d        *dialogFrame
	revision *selectField
	confirm  *widget.Button
	cancel   *widget.Button
	choices  []int // the revision of each option
}

// showRollbackDialog builds and shows the dialog for a release's history.
func (sh *shell) showRollbackDialog(h releaseHistory) *rollbackDialog {
	th := sh.app.Settings().Theme()
	width := th.Size(sizeConfirmWidth)
	rd := &rollbackDialog{}
	options := make([]string, 0, len(h.History))
	selected := ""
	for _, e := range h.History {
		text := helm.RevisionText(e, sh.timeFormat()) // in the user's zone and format (D-31)
		options = append(options, text)
		rd.choices = append(rd.choices, e.Revision)
		if e.Revision == h.Revision {
			selected = text
		}
	}
	rd.revision = newSelectField(options, nil)
	rd.confirm = widget.NewButton("Rollback", nil)
	rd.confirm.Importance = widget.DangerImportance
	rd.cancel = widget.NewButton("Cancel", nil)
	pick := func() int {
		if i := rd.revision.SelectedIndex(); i >= 0 && i < len(rd.choices) {
			return rd.choices[i]
		}
		return 0
	}
	armed := false
	enable := func() {
		if armed && pick() > 0 {
			rd.confirm.Enable() // the current revision too, as in Lens's dialog
		} else {
			rd.confirm.Disable()
		}
	}
	rd.revision.OnChanged = func(string) { enable() }
	if selected != "" {
		rd.revision.SetSelected(selected)
	}
	rd.confirm.Disable()

	// Rows as every confirmation has them (D-85, WF32): the place, Context › Namespace › Object, the
	// release in the one form (objectnames.go), the revision to choose, and what helm writes, which
	// holds for the current revision too (a reading of helm's behaviour, not of Lens's).
	valueW := width - th.Size(sizeDrawerLabel) - 2*th.Size(theme.SizeNamePadding)
	rows := box(kvLayout{})
	placeRows(rows, h.Ref.Context, []string{h.Ref.Namespace}, []namedObject{{kind: "Release", name: h.Ref.Name}}, valueW)
	rows.Objects = append(rows.Objects, rowName("To revision"), rd.revision)
	effectRows(rows, []engine.Effect{{Name: "Writes", Value: "a new revision equal to the chosen one"}}, valueW)
	rd.d = newDialogFrame(sh, objectDialogTitle("Rollback", "Release", h.Ref.Name, "")) // no gap after the arrow (dialogframe.go)
	rd.d.setContent(rows)
	rd.d.SetButtons([]fyne.CanvasObject{rd.cancel, rd.confirm})
	rd.cancel.OnTapped = rd.d.Hide
	rd.confirm.OnTapped = func() {
		rev := pick()
		if rev == 0 {
			return
		}
		rd.d.Hide()
		answer := sh.confirmer.confirmedByDialog(rollbackCommand, h.Ref, h.UID, rollbackDetail(rev))
		sh.dispatch(rollbackCommand, helmRollbackParams{refParams: refParams{Ref: h.Ref}, Revision: rev}, func(any, error) {
			sh.confirmer.dropDialogAnswer(answer) // unused when the dispatch failed before it was asked
		})
	}
	if d := sh.confirmer.armDelay; d > 0 { // as the confirmation dialog: a stray click must not roll back
		time.AfterFunc(d, func() { sh.do(func() { armed = true; enable() }) })
	} else {
		armed = true
		enable()
	}
	rd.d.Resize(fyne.NewSize(width, rd.d.MinSize().Height))
	rd.d.Show()
	return rd
}

// dialogConfirmation is an answer the user gave in a command's own dialog, waiting for the
// engine's confirmation of the same command.
type dialogConfirmation struct {
	command string
	ref     model.ObjectRef
	uid     string
	detail  string // the confirmation's detail, which names what the dialog chose (the revision)
	until   time.Time
	moved   error // what another object under the name gives; nil: errMovedWhileOpen (a release's)
}

// rollbackDetail is the engine's confirmation detail of a rollback to revision (the engine's
// commands.RollbackDetail; TestRollbackDialogAnswerBinding keeps them equal). Were they to differ,
// the dialog's answer would match no request and the user would be asked again: safe.
func rollbackDetail(revision int) string {
	return engine.EffectsText([]engine.Effect{{Name: "To revision", Value: strconv.Itoa(revision), Tone: engine.EffectNames}})
}

// dialogAnswers holds the guiConfirmer's dialog answer; one at a time, the last one wins.
type dialogAnswers struct {
	mu sync.Mutex
	c  *dialogConfirmation
}

// errMovedWhileOpen refuses a confirmation whose release moved to another revision while its
// dialog was open.
var errMovedWhileOpen = errors.New("the release moved to another revision while the dialog was open; nothing was done")

// confirmedByDialog records that the user confirmed command on ref, at the object's uid as the
// dialog showed it, with the confirmation detail that names the choice; it returns the answer for
// dropDialogAnswer.
func (c *guiConfirmer) confirmedByDialog(command string, ref model.ObjectRef, uid, detail string) *dialogConfirmation {
	c.answers.mu.Lock()
	defer c.answers.mu.Unlock()
	c.answers.c = &dialogConfirmation{command: command, ref: ref, uid: uid, detail: detail, until: time.Now().Add(dialogConfirmWindow)}
	return c.answers.c
}

// confirmedByDialogOr is confirmedByDialog for another kind of object than a release: moved is the
// error when the object under the name is no longer the one the dialog showed (the Debug dialog's
// pod, debugdialog.go).
func (c *guiConfirmer) confirmedByDialogOr(command string, ref model.ObjectRef, uid, detail string, moved error) *dialogConfirmation {
	c.answers.mu.Lock()
	defer c.answers.mu.Unlock()
	c.answers.c = &dialogConfirmation{command: command, ref: ref, uid: uid, detail: detail, until: time.Now().Add(dialogConfirmWindow),
		moved: moved}
	return c.answers.c
}

// dropDialogAnswer forgets answer if no confirmation took it: its dispatch ended, so it must not
// confirm another one in its window.
func (c *guiConfirmer) dropDialogAnswer(answer *dialogConfirmation) {
	c.answers.mu.Lock()
	defer c.answers.mu.Unlock()
	if c.answers.c == answer {
		c.answers.c = nil
	}
}

// takeDialogAnswer answers a confirmation from a dialog the user already answered: handled is
// false when no dialog answer matches the request (another command, origin, object, context or
// choice, or too late), and the confirmer asks as usual. A matching answer is used once.
func (c *guiConfirmer) takeDialogAnswer(req engine.ConfirmRequest) (ok, handled bool, err error) {
	c.answers.mu.Lock()
	defer c.answers.mu.Unlock()
	a := c.answers.c
	if a == nil || time.Now().After(a.until) || req.Command != a.command || req.Origin != engine.OriginGUI ||
		req.Context != a.ref.Context || len(req.Objects) != 1 || !sameObject(req.Objects[0], a.ref) || req.Detail != a.detail {
		return false, false, nil
	}
	c.answers.c = nil
	if len(req.UIDs) != 1 || a.uid == "" || req.UIDs[0] != a.uid {
		if a.moved != nil {
			return false, true, a.moved
		}
		return false, true, errMovedWhileOpen
	}
	return true, true, nil
}

// sameObject compares two references by kind, namespace and name.
func sameObject(a, b model.ObjectRef) bool {
	return a.Kind == b.Kind && a.Namespace == b.Namespace && a.Name == b.Name
}
