package fyneui

import (
	"encoding/json"
	"fmt"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Copying a masked Secret value without showing it (the owner's decision of 2026-10-22, D-25,
// H-25): "Ich will natürlich auch Passwörter direkt in den Zwischenspeicher packen können und dann
// direkt irgendwo in ein Passwortfeld ohne es vorher anzuzeigen." A masked key has the copy icon in
// its line, always shown, between Show and the remove button (the owner's RW1 of 2026-10-23,
// secretdata.go), its tooltip "Copy the value without showing it". A tap reads the whole value with secret.reveal (the GUI-only command Show runs, with whole: never the
// first 4 KiB a reveal shows, since a password cut short and pasted unseen would be wrong) and puts
// it on the clipboard for Secret values (shell.copyText with secret: concealed and this Mac's only
// on macOS, cleared after 60 seconds if the clipboard still holds it). The value enters no widget
// and not the drawer's revealed map: the drawer stays masked, and the toast names the key, never
// the value. An answer for a drawer that moved on meanwhile is dropped. A value the user edited in
// the drawer's editor and hid again (D-59) is copied the same way from the editor's memory, with no
// reveal. A binary value has neither Show nor this icon: the engine never hands it out, and the
// clipboard holds text. No command copies: TestCopyHasNoCommand.
//
// A revealed value that Show cut (4 KiB, 40 lines) copies the whole value the same way through its
// icon, "Copy the whole value", the drawer still showing the cut text (the lead's decision of
// 2026-10-22, D-25: a password cut short on the clipboard is wrong, revealed or not, and the icon
// means the value wherever it sits); a selection and ⌘C copy exactly the selection.

// Copy icons' tooltips: a masked Secret value's, a revealed one's.
const (
	maskedCopyTip = "Copy the value without showing it"
	wholeCopyTip  = "Copy the whole value"
)

// copyWhole reads the whole value a reveal action names (secret.reveal, GUI-only) and copies it
// without showing it (wholeRead): a masked value, or a revealed one Show cut. done follows a copy.
func (d *drawer) copyWhole(ref model.ObjectRef, a model.ItemAction, done func()) {
	var p engine.RevealParams
	if err := json.Unmarshal(a.Params, &p); err != nil {
		d.sh.showError(err)
		return
	}
	p.Whole = true
	d.sh.dispatch(a.Command, p, func(res any, err error) { d.wholeRead(ref, p.Key, done, res, err) })
}

// wholeRead copies the value copyWhole read as a Secret's value: nothing, not even its error, when
// the drawer no longer shows ref; nothing when the answer is not the whole value.
func (d *drawer) wholeRead(ref model.ObjectRef, key string, done func(), res any, err error) {
	if d.shown == nil || *d.shown != ref {
		return // the drawer moved on meanwhile: copy nothing
	}
	if err != nil {
		d.sh.showError(err)
		return
	}
	var v engine.SecretValue
	if err := decode(res, &v); err != nil {
		d.sh.showError(err)
		return
	}
	if v.Cut {
		d.sh.copyToast("warning", "The value of "+key+" was not read whole: nothing was copied.")
		return
	}
	d.copyUnseen(key, v.Value, done)
}

// copyUnseen puts a Secret's value the drawer does not show on the clipboard for Secret values,
// then runs done (the icon's checkmark), with a toast that names the key; an empty value copies
// nothing, so that the click does not empty the clipboard.
func (d *drawer) copyUnseen(key, value string, done func()) {
	if value == "" {
		d.sh.copyToast("info", "The value of "+key+" is empty: nothing was copied.")
		return
	}
	d.sh.copyText(value, true)
	done()
	d.sh.copyToast("info", fmt.Sprintf("Copied the value of %s. The clipboard is cleared in %s.", key, ttlText(d.sh.secretClipTTL)))
}

// revealAction is an item's reveal action: the Show beside a masked Secret value.
func revealAction(it model.DetailItem) (model.ItemAction, bool) {
	for _, a := range it.Actions {
		if a.Command == engine.RevealCommand {
			return a, true
		}
	}
	return model.ItemAction{}, false
}
