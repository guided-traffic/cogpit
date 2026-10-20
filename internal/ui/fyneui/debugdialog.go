package fyneui

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The Debug dialog (the owner, 2026-10-19; D-102; engine/debugcontainer.go): Debug on a pod, from
// its drawer's toolbar, its row menu or the palette, asks which image, prefilled with the
// Preferences' debug image in an ordinary entry the user edits, selects, copies and pastes into,
// and which of the pod's containers to target (the first preselected, None for no target). Debug
// is destructive for every origin (the owner), and this dialog is its confirmation, as the
// Rollback dialog is a rollback's (helmrollback.go): its rows are the confirmation's, Context ›
// Namespace › Pod, Target and what the container sees of it, Image and its digest, "This cannot be
// undone." in red, its Debug button armed after the confirmation's delay. Its answer is bound to
// the command, this window, the pod and its UID as the dialog read them, the target and the image,
// so the engine's confirmation of exactly that takes it instead of asking a second time; a pod
// created again under the name meanwhile is refused. One step, not two: a second dialog would only
// repeat the first's values (the lead's choice, D-102).

func init() { itemActionDialogs[engine.DebugCommand] = openDebugDialog }

// debugNoTarget is the Target choice for no target.
const debugNoTarget = "None"

// errPodReplacedWhileOpen refuses the dialog's answer for a pod created again under the name while
// it was open.
var errPodReplacedWhileOpen = errors.New("the pod was replaced by another of its name while the dialog was open; nothing was added")

// debugDialogParams mirror pod.debug's parameters.
type debugDialogParams struct {
	Ref    model.ObjectRef `json:"ref"`
	Image  string          `json:"image,omitempty"`
	Target string          `json:"target,omitempty"`
	UID    string          `json:"uid,omitempty"`
}

// openDebugDialog reads the pod's containers from the drawer's model, off the main goroutine, and
// shows the dialog with the command's Prefill (the action's params).
func openDebugDialog(sh *shell, ref model.ObjectRef, a model.ItemAction) {
	var p debugDialogParams
	if len(a.Params) > 0 {
		if err := json.Unmarshal(a.Params, &p); err != nil {
			sh.toast("error", fmt.Sprintf("%s: %v", a.Label, err))
			return
		}
	}
	if p.Ref.Kind == "" {
		p.Ref = ref
	}
	if p.Image == "" { // the Prefill failed: the Preferences' image as the window holds it
		p.Image = engine.DefaultDebugImage
		if pr := sh.ui.Preferences; pr != nil && pr.DebugImage != "" {
			p.Image = pr.DebugImage
		}
	}
	go func() {
		defer sh.recoverTo("the Debug dialog")
		var targets []string
		if m, err := sh.eng.Detail(p.Ref); err == nil && m != nil {
			targets = m.Containers
		}
		sh.do(func() { sh.showDebugDialog(p, targets) })
	}()
}

// debugDialog is a shown Debug dialog.
type debugDialog struct {
	d       *dialogFrame
	rows    *fyne.Container
	target  *selectField
	image   *widget.Entry
	confirm *widget.Button
	cancel  *widget.Button
}

// chosenTarget is the target picked: "" for None.
func (dd *debugDialog) chosenTarget() string {
	if t := dd.target.Selected; t != debugNoTarget {
		return t
	}
	return ""
}

// showDebugDialog builds and shows the dialog for the pod p names, targets its containers.
func (sh *shell) showDebugDialog(p debugDialogParams, targets []string) *debugDialog {
	th := sh.app.Settings().Theme()
	width := th.Size(sizeConfirmWidth)
	valueW := width - th.Size(sizeDrawerLabel) - 2*th.Size(theme.SizeNamePadding)
	dd := &debugDialog{rows: box(kvLayout{})}
	dd.target = newSelectField(append(slices.Clone(targets), debugNoTarget), nil)
	switch {
	case p.Target != "" && slices.Contains(targets, p.Target):
		dd.target.SetSelected(p.Target)
	case len(targets) > 0:
		dd.target.SetSelected(targets[0])
	default:
		dd.target.SetSelected(debugNoTarget)
	}
	dd.image = widget.NewEntry() // an ordinary entry: select, copy, paste, edit
	dd.image.SetText(p.Image)
	dd.confirm = widget.NewButton("Debug", nil)
	dd.confirm.Importance = widget.DangerImportance
	dd.confirm.SetIcon(theme.WarningIcon())
	dd.cancel = widget.NewButton("Cancel", nil)

	// The rows as the confirmation has them (D-85): the place, then the target and the image as the
	// fields the user sets, each followed by what the engine's confirmation would add (what the
	// container sees of the target, the image's digest or that it floats).
	build := func() {
		target, image := dd.chosenTarget(), strings.TrimSpace(dd.image.Text)
		dd.rows.Objects = nil
		placeRows(dd.rows, p.Ref.Context, []string{p.Ref.Namespace}, []namedObject{{kind: "Pod", name: p.Ref.Name}}, valueW)
		dd.rows.Objects = append(dd.rows.Objects, rowName("Target"), dd.target)
		effects := engine.DebugEffects(target, image)
		for _, e := range effects {
			if e.Name == "Sees" {
				dd.rows.Objects = append(dd.rows.Objects, rowName(e.Name), effectValue(e, valueW))
			}
		}
		dd.rows.Objects = append(dd.rows.Objects, rowName("Image"), dd.image)
		for _, e := range effects {
			if e.Name == "Digest" && image != "" {
				dd.rows.Objects = append(dd.rows.Objects, rowName(e.Name), effectValue(e, valueW))
			}
		}
		dd.rows.Refresh()
	}
	armed := false
	enable := func() {
		image := strings.TrimSpace(dd.image.Text)
		if armed && image != "" && engine.CheckDebugImage(image) == nil {
			dd.confirm.Enable()
		} else {
			dd.confirm.Disable()
		}
	}
	dd.target.OnChanged = func(string) { build() }
	dd.image.OnChanged = func(string) { build(); enable() }
	build()
	dd.confirm.Disable()

	content := container.NewVBox(dd.rows, newSpace(theme.SizeNamePadding), newParagraph("This cannot be undone.", colorStatusError, width))
	dd.d = newDialogFrame(sh, objectDialogTitle("Debug", "Pod", p.Ref.Name, "")) // no gap after the arrow (dialogframe.go)
	dd.d.setContent(content)
	dd.d.setRim(colorStatusError) // adding a container cannot be undone: as a destructive confirmation
	dd.d.SetButtons([]fyne.CanvasObject{dd.cancel, dd.confirm})
	dd.cancel.OnTapped = dd.d.Hide
	dd.confirm.OnTapped = func() {
		image := strings.TrimSpace(dd.image.Text)
		if image == "" {
			return
		}
		params := debugDialogParams{Ref: p.Ref, Image: image, Target: dd.chosenTarget(), UID: p.UID}
		dd.d.Hide()
		var answer *dialogConfirmation
		if p.UID != "" { // without the UID the engine's own confirmation asks
			answer = sh.confirmer.confirmedByDialogOr(engine.DebugCommand, p.Ref, p.UID,
				engine.DebugDetail(params.Target, image), errPodReplacedWhileOpen)
		}
		sh.dispatch(engine.DebugCommand, params, func(res any, err error) {
			if answer != nil {
				sh.confirmer.dropDialogAnswer(answer) // unused when the dispatch failed before it was asked
			}
			sh.debugDone(res, err)
		})
	}
	if d := sh.confirmer.armDelay; d > 0 { // as the confirmation: a stray click must not add a container
		time.AfterFunc(d, func() { sh.do(func() { armed = true; enable() }) })
	} else {
		armed = true
		enable()
	}
	dd.d.Resize(fyne.NewSize(width, dd.d.MinSize().Height))
	dd.d.Show() // no button has the focus: Enter must not confirm by accident
	return dd
}

// debugDone shows pod.debug's outcome: a refusal (the API server's: Pod Security admission, a
// policy engine; no permission) as an error, a container added without its Attach tab as a note.
func (sh *shell) debugDone(res any, err error) {
	if err != nil {
		sh.showError(err)
		return
	}
	var r struct {
		Note string `json:"note"`
	}
	if decode(res, &r) == nil && r.Note != "" {
		sh.toast("info", r.Note)
	}
}
