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

// The Debug dialog (the owner; D-102; engine/debugcontainer.go): Debug on a pod, from its drawer's
// toolbar, its row menu or the palette, asks which image, prefilled with the Preferences' debug
// image in an ordinary entry the user edits, selects, copies and pastes into, and which of the
// pod's containers to target (the first preselected, None for no target), and which profile sets
// its security context (Restricted preselected where the namespace enforces Pod Security's
// restricted level, else General; engine/debugprofile.go). Debug is destructive for every origin
// (the owner), and this dialog is its confirmation, as the Rollback dialog is a rollback's
// (helmrollback.go): its rows are the confirmation's, Context › Namespace › Pod, Target and what
// the container sees of it, Profile and what it grants, Image and its digest, "This cannot be
// undone." in red, its Debug button armed after the confirmation's delay. Its answer is bound to
// the command, this window, the pod and its UID as the dialog read them, the target, the profile
// and the image, so the engine's
// confirmation of exactly that takes it instead of asking a second time; a pod created again under
// the name meanwhile is refused. One step, not two: a second dialog would only repeat the first's
// values (the lead's choice, D-102).
//
// The image field shows the image's short name (the owner's R30: nicolaka/netshoot:v0.16,
// engine.ShortImage), its digest behind it: while the text is the configured image's short name the
// configured image runs, pinned as it is, and the Digest row shows its digest; a changed text runs
// exactly what is typed, a tag without a digest marked as floating (engine.ImageFromField).

func init() { itemActionDialogs[engine.DebugCommand] = openDebugDialog }

// debugNoTarget is the Target choice for no target.
const debugNoTarget = "None"

// errPodReplacedWhileOpen refuses the dialog's answer for a pod created again under the name while
// it was open.
var errPodReplacedWhileOpen = errors.New("the pod was replaced by another of its name while the dialog was open; nothing was added")

// debugDialogParams mirror pod.debug's parameters.
type debugDialogParams struct {
	Ref     model.ObjectRef `json:"ref"`
	Image   string          `json:"image,omitempty"`
	Target  string          `json:"target,omitempty"`
	Profile string          `json:"profile,omitempty"`
	UID     string          `json:"uid,omitempty"`
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
		var (
			targets []string
			users   map[string]int64
		)
		if m, err := sh.eng.Detail(p.Ref); err == nil && m != nil {
			targets, users = m.Containers, m.DebugUsers
		}
		sh.do(func() { sh.showDebugDialog(p, targets, users) })
	}()
}

// debugDialog is a shown Debug dialog.
type debugDialog struct {
	d       *dialogFrame
	rows    *fyne.Container
	target  *selectField
	profile *selectField
	image   *fieldEntry
	confirm *textButton
	cancel  *textButton
	// configured is the image the dialog opened with (the Preferences', pinned by digest by
	// default), which the field shows by its short name.
	configured string
}

// chosenImage is the image the field stands for: the configured one while the field shows its
// short name, Cogpit's default for the default's short name, else what is typed; "" while empty.
func (dd *debugDialog) chosenImage() string {
	if strings.TrimSpace(dd.image.Text) == "" {
		return ""
	}
	return engine.ImageFromField(dd.image.Text, dd.configured, engine.DefaultDebugImage)
}

// chosenTarget is the target picked: "" for None.
func (dd *debugDialog) chosenTarget() string {
	if t := dd.target.Selected; t != debugNoTarget {
		return t
	}
	return ""
}

// chosenProfile is the profile picked.
func (dd *debugDialog) chosenProfile() engine.DebugProfile {
	return engine.DebugProfileLabeled(dd.profile.Selected)
}

// showDebugDialog builds and shows the dialog for the pod p names, targets its containers, users
// the user IDs they run as (engine.DebugUsers), from which the Restricted profile's user comes.
func (sh *shell) showDebugDialog(p debugDialogParams, targets []string, users map[string]int64) *debugDialog {
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
	labels := make([]string, 0, len(engine.DebugProfiles))
	for _, pr := range engine.DebugProfiles {
		labels = append(labels, pr.Label())
	}
	dd.profile = newSelectField(labels, nil)
	start, err := engine.ParseDebugProfile(p.Profile)
	if err != nil {
		start = engine.DebugGeneral
	}
	dd.profile.SetSelected(start.Label())
	dd.image = newFieldEntry() // an ordinary entry: select, copy, paste, edit; Escape cancels (escape.go)
	dd.configured = p.Image
	dd.image.SetText(engine.ShortImage(p.Image))
	dd.confirm = newPlateButton("Debug", nil, nil)
	dd.confirm.Importance = widget.DangerImportance
	dd.cancel = newPlateButton("Cancel", nil, nil)

	// The rows as the confirmation has them (D-85): the place, then the target, the profile and the
	// image as the fields the user sets, each followed by what the engine's confirmation would add
	// (what the container sees of the target, what the profile grants, the image's digest or that
	// it floats).
	build := func() {
		target, image := dd.chosenTarget(), dd.chosenImage()
		dd.rows.Objects = nil
		placeRows(dd.rows, p.Ref.Context, []string{p.Ref.Namespace}, []namedObject{{kind: "Pod", name: p.Ref.Name}}, valueW)
		dd.rows.Objects = append(dd.rows.Objects, rowName("Target"), dd.target)
		effects := engine.DebugEffects(target, image, dd.chosenProfile(), engine.DebugUser(users, target))
		for _, e := range effects {
			if e.Name == "Sees" {
				dd.rows.Objects = append(dd.rows.Objects, rowName(e.Name), effectValue(e, valueW))
			}
		}
		dd.rows.Objects = append(dd.rows.Objects, rowName("Profile"), dd.profile)
		for _, e := range effects {
			if e.Name == "Rights" {
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
		image := dd.chosenImage()
		if armed && image != "" && engine.CheckDebugImage(image) == nil {
			dd.confirm.Enable()
		} else {
			dd.confirm.Disable()
		}
	}
	dd.target.OnChanged = func(string) { build() }
	dd.profile.OnChanged = func(string) { build() }
	dd.image.OnChanged = func(string) { build(); enable() }
	build()
	dd.confirm.Disable()

	content := container.NewVBox(dd.rows)
	dd.d = newDialogFrame(sh, objectDialogTitle("Debug", "Pod", p.Ref.Name, "")) // no gap after the arrow (dialogframe.go)
	dd.d.setContent(content)
	dd.d.setNote("This cannot be undone.", colorStatusError)
	dd.d.setRim(colorStatusError) // adding a container cannot be undone: as a destructive confirmation
	dd.d.SetButtons([]fyne.CanvasObject{dd.cancel, dd.confirm})
	dd.cancel.OnTapped = dd.d.Hide
	dd.confirm.OnTapped = func() {
		image := dd.chosenImage()
		if image == "" {
			return
		}
		profile := dd.chosenProfile()
		params := debugDialogParams{Ref: p.Ref, Image: image, Target: dd.chosenTarget(), Profile: string(profile), UID: p.UID}
		dd.d.Hide()
		var answer *dialogConfirmation
		if p.UID != "" { // without the UID the engine's own confirmation asks
			answer = sh.confirmer.confirmedByDialogOr(engine.DebugCommand, p.Ref, p.UID,
				engine.DebugDetail(params.Target, image, profile, engine.DebugUser(users, params.Target)), errPodReplacedWhileOpen)
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
