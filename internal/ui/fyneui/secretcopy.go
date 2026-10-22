package fyneui

import (
	"encoding/json"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Copying a masked Secret value without showing it (the owner's decision of 2026-10-22, D-25,
// H-25): "Ich will natürlich auch Passwörter direkt in den Zwischenspeicher packen können und dann
// direkt irgendwo in ein Passwortfeld ohne es vorher anzuzeigen." A masked value in the drawer has
// the copy icon on hover as every value has, its tooltip "Copy the value without showing it". A tap
// reads the whole value with secret.reveal (the GUI-only command Show runs, with whole: never the
// first 4 KiB a reveal shows, since a password cut short and pasted unseen would be wrong) and puts
// it on the clipboard for Secret values (shell.copyText with secret: concealed and this Mac's only
// on macOS, cleared after 60 seconds if the clipboard still holds it). The value enters no widget
// and not the drawer's revealed map: the drawer stays masked, and the toast names the key, never
// the value. An answer for a drawer that moved on meanwhile is dropped. A value the user edited in
// the drawer's editor and hid again (D-59) is copied the same way from the editor's memory, with no
// reveal. A binary value has neither Show nor this icon: the engine never hands it out, and the
// clipboard holds text. No command copies: TestCopyHasNoCommand.

// maskedCopyTip is the copy icon's tooltip over a masked Secret value.
const maskedCopyTip = "Copy the value without showing it"

// maskedCopyFor returns the copy hook of a masked Secret value; fetch is what a tap runs.
func (d *drawer) maskedCopyFor(fetch func(*copyHook)) *copyHook {
	h := &copyHook{d: d, secret: true, fetch: fetch, tip: maskedCopyTip}
	d.copyHooks = append(d.copyHooks, h)
	return h
}

// maskedValue renders a Secret's masked value as the drawer always did, with the copy icon that
// copies the value a's reveal names without showing it. A selection of the masked text copies the
// mask, as before.
func (d *drawer) maskedValue(ref model.ObjectRef, it model.DetailItem, a model.ItemAction) fyne.CanvasObject {
	l := newTextLine(it.Value, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
	l.truncate = true
	h := d.maskedCopyFor(func(h *copyHook) { d.copyMasked(ref, a, h) })
	return newSelLine(l, h, func(sel string) { d.sh.copyText(sel, false) })
}

// copyMasked reads the whole value a reveal action names (secret.reveal, GUI-only) and copies it
// without showing it (maskedRead).
func (d *drawer) copyMasked(ref model.ObjectRef, a model.ItemAction, h *copyHook) {
	var p engine.RevealParams
	if err := json.Unmarshal(a.Params, &p); err != nil {
		d.sh.showError(err)
		return
	}
	p.Whole = true
	d.sh.dispatch(a.Command, p, func(res any, err error) { d.maskedRead(ref, p.Key, h, res, err) })
}

// maskedRead copies the value a masked copy read as a Secret's value: nothing, not even its error,
// when the drawer no longer shows ref; nothing when the answer is not the whole value.
func (d *drawer) maskedRead(ref model.ObjectRef, key string, h *copyHook, res any, err error) {
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
	d.copyUnseen(key, v.Value, h)
}

// copyUnseen puts a Secret's value the drawer does not show on the clipboard for Secret values,
// with the icon's checkmark while it is still on the value (h) and a toast that names the key; an
// empty value copies nothing, so that the click does not empty the clipboard.
func (d *drawer) copyUnseen(key, value string, h *copyHook) {
	if value == "" {
		d.sh.copyToast("info", "The value of "+key+" is empty: nothing was copied.")
		return
	}
	d.sh.copyText(value, true)
	if d.copyHook == h {
		d.confirmCopy()
	}
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
