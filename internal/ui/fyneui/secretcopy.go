package fyneui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

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
// reveal. A binary value has neither Show nor this icon: the engine never hands its bytes out, and
// the clipboard holds text; it has the b64 button (below). No command copies: TestCopyHasNoCommand.
//
// A revealed value that Show cut (4 KiB, 40 lines) copies the whole value the same way through its
// icon, "Copy the whole value", the drawer still showing the cut text (the lead's decision of
// 2026-10-22, D-25: a password cut short on the clipboard is wrong, revealed or not, and the icon
// means the value wherever it sits); a selection and ⌘C copy exactly the selection.

// The b64 button (the owner's VW7 of 2026-10-24: "ich brauche neben dem Copy-Button für ein
// Secret-Value, einen weiteren kleinen Button mit b64 darin stehend. Dieser Kopiert den Secrete Value
// in den Zwischenspeicher aber als b64 encodet string."; D-25). Every key's line has it right after
// the copy icon, a binary value's line too (his pick (b) of 2026-10-24), where it is the only copy:
// base64 is the one form a binary value's bytes survive a text clipboard in. It copies, base64-encoded
// (standard, padded, one line: the string a Secret's data holds), what the copy icon beside it copies:
// a value in no editor, masked or revealed, and a binary value whole through secret.reveal with
// base64 (GUI-only; never the 4 KiB a Show reveals), a value in an editor as typed. It goes to the
// clipboard for Secret values as the copy icon's copy does (concealed, cleared after 60 seconds), the
// icon turns into the checkmark for the copy icon's time, and the toast names the key and says "as
// base64", never the value. Its tooltip "Copy the value as base64". No command copies
// (TestCopyHasNoCommand): the button runs secret.reveal, which agents and the CLI are refused.
//
// Since the owner's VW19 of 2026-10-25 ("ich wünsche mir ein Icon für b64, welches nicht breiter ist
// als der copy-button oder der papierkorb"; his pick O1 of the lead's candidates rendered in a key's
// line, "O1 it is") it is an icon, not the word: the copy icon's back sheet with a bold "64" where its
// front sheet stands (iconBase64), on a pinned copy icon (copyButton with its icon), so that it is as
// wide and high as the copy icon and outlined, hovered and checked as it is (D-104), its width the
// same with the checkmark in its place. The word's button grew after its checkmark (the owner's
// VW18: Fyne 2.8.2's button measures an icon it hides, widget/button.go `hasIcon := r.icon != nil`).

// Copy icons' tooltips: a masked Secret value's, a revealed one's, the b64 button's.
const (
	maskedCopyTip = "Copy the value without showing it"
	wholeCopyTip  = "Copy the whole value"
	b64Tip        = "Copy the value as base64"
)

// iconBase64 is the b64 button's icon (the owner's pick O1 of 2026-10-25, VW19), one colour that the
// theme recolours as Fyne's icons: the back sheet of the copy icon (Material's content_copy as Fyne
// bundles it, theme.ContentCopyIcon) and in its front sheet's place a bold "64", drawn for Cogpit.
var iconBase64 = fyne.NewStaticResource("cogpit-b64.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24">`+
		`<path d="M16 1H4c-1.1 0-2 .9-2 2v14h2V3h12V1z"/>`+
		`<path d="M12.3,10.8 Q12.3,10 11.5,10 L10.3,10 Q9.5,10 9.5,10.8 L9.5,13.848 L11.5,13.848 Q14.5,13.848 14.5,16.848 `+
		`L14.5,19.6 Q14.5,22.6 11.5,22.6 L10.3,22.6 Q7.3,22.6 7.3,19.6 L7.3,10.8 Q7.3,9.557 8.179,8.679 Q9.057,7.8 10.3,7.8 `+
		`L11.5,7.8 Q12.743,7.8 13.621,8.679 Q14.5,9.557 14.5,10.8 Q14.5,11.256 14.178,11.578 Q13.856,11.9 13.4,11.9 `+
		`Q12.944,11.9 12.622,11.578 Q12.3,11.256 12.3,10.8 Z `+
		`M18.2,8.9 L18.2,15.316 Q18.2,16.116 19,16.116 L21,16.116 L21,8.9 Q21,8.444 21.322,8.122 Q21.644,7.8 22.1,7.8 `+
		`Q22.556,7.8 22.878,8.122 Q23.2,8.444 23.2,8.9 L23.2,21.5 Q23.2,21.956 22.878,22.278 Q22.556,22.6 22.1,22.6 `+
		`Q21.644,22.6 21.322,22.278 Q21,21.956 21,21.5 L21,18.316 L19,18.316 Q16,18.316 16,15.316 L16,8.9 `+
		`Q16,8.444 16.322,8.122 Q16.644,7.8 17.1,7.8 Q17.556,7.8 17.878,8.122 Q18.2,8.444 18.2,8.9 Z `+
		`M9.5,16.048 L9.5,19.6 Q9.5,20.4 10.3,20.4 L11.5,20.4 Q12.3,20.4 12.3,19.6 L12.3,16.848 Q12.3,16.048 11.5,16.048 `+
		`L9.5,16.048 Z"/></svg>`))

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

// copyBase64 reads the whole value a reveal action names base64-encoded (secret.reveal with base64,
// GUI-only) and copies it without showing it (encodedRead): the b64 button of a value in no editor,
// a binary value's included. done follows a copy.
func (d *drawer) copyBase64(ref model.ObjectRef, a model.ItemAction, done func()) {
	var p engine.RevealParams
	if err := json.Unmarshal(a.Params, &p); err != nil {
		d.sh.showError(err)
		return
	}
	p.Base64, p.Whole = true, false
	d.sh.dispatch(a.Command, p, func(res any, err error) { d.encodedRead(ref, p.Key, done, res, err) })
}

// encodedRead copies the value copyBase64 read as a Secret's value, as wholeRead does: nothing when
// the drawer moved on, nothing when the answer is not the whole value encoded.
func (d *drawer) encodedRead(ref model.ObjectRef, key string, done func(), res any, err error) {
	d.readAndCopy(ref, key, true, done, res, err)
}

// wholeRead copies the value copyWhole read as a Secret's value: nothing, not even its error, when
// the drawer no longer shows ref; nothing when the answer is not the whole value.
func (d *drawer) wholeRead(ref model.ObjectRef, key string, done func(), res any, err error) {
	d.readAndCopy(ref, key, false, done, res, err)
}

// readAndCopy is wholeRead and encodedRead: encoded says the read asked for base64.
func (d *drawer) readAndCopy(ref model.ObjectRef, key string, encoded bool, done func(), res any, err error) {
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
	if v.Cut || encoded != v.Base64 { // an engine that ignored whole or base64
		d.sh.copyToast("warning", "The value of "+key+" was not read whole: nothing was copied.")
		return
	}
	d.copyUnseenAs(key, v.Value, encoded, done)
}

// copyUnseen puts a Secret's value the drawer does not show on the clipboard for Secret values,
// then runs done (the icon's checkmark), with a toast that names the key; an empty value copies
// nothing, so that the click does not empty the clipboard.
func (d *drawer) copyUnseen(key, value string, done func()) { d.copyUnseenAs(key, value, false, done) }

// copyUnseenAs is copyUnseen; encoded says that value is the value base64-encoded (the b64 button),
// which the toast says.
func (d *drawer) copyUnseenAs(key, value string, encoded bool, done func()) {
	if value == "" {
		d.sh.copyToast("info", "The value of "+key+" is empty: nothing was copied.")
		return
	}
	d.sh.copyText(value, true)
	done()
	as := ""
	if encoded {
		as = " as base64"
	}
	d.sh.copyToast("info", fmt.Sprintf("Copied the value of %s%s. The clipboard is cleared in %s.", key, as, ttlText(d.sh.secretClipTTL)))
}

// copyTypedBase64 copies a value in an editor as typed, base64-encoded: the b64 button beside an
// editor's copy icon.
func (d *drawer) copyTypedBase64(key, text string, done func()) {
	d.copyUnseenAs(key, base64.StdEncoding.EncodeToString([]byte(text)), true, done) // "" stays "": nothing copied
}

// b64Button is a key's b64 button, kept across the drawer's rebuilds as the copy icon is, so that
// its checkmark stays: a pinned copy icon with the b64 icon (VW19).
type b64Button struct {
	btn  *copyButton
	done *time.Timer
}

func newB64Button(sh *shell) *b64Button {
	b := &b64Button{btn: newPinnedCopyIcon(sh, b64Tip, nil)}
	b.btn.icon = iconBase64
	return b
}

// set makes a tap copy with copyValue, which calls done after a copy, and returns the button
// centred in its line, as the copy icon beside it.
func (b *b64Button) set(copyValue func(done func())) fyne.CanvasObject {
	b.btn.tap = func() { copyValue(b.confirm) }
	return container.NewCenter(b.btn)
}

// confirm shows the checkmark in the OK colour in place of the icon for the shell's copyConfirm, as
// the copy icon does, then the icon again; a later copy restarts it.
func (b *b64Button) confirm() { b.btn.confirm(&b.done) }

// revealAction is an item's reveal action: the Show beside a masked Secret value, not a binary
// value's base64 read.
func revealAction(it model.DetailItem) (model.ItemAction, bool) {
	for _, a := range it.Actions {
		if a.Command == engine.RevealCommand && !base64Read(a) {
			return a, true
		}
	}
	return model.ItemAction{}, false
}

// base64Action is a binary value's base64 read: the action its b64 button runs (a text value's b64
// button runs its Show's action with base64).
func base64Action(it model.DetailItem) (model.ItemAction, bool) {
	for _, a := range it.Actions {
		if a.Command == engine.RevealCommand && base64Read(a) {
			return a, true
		}
	}
	return model.ItemAction{}, false
}

// base64Read reports whether a reveal action reads the value base64-encoded.
func base64Read(a model.ItemAction) bool {
	var p engine.RevealParams
	return json.Unmarshal(a.Params, &p) == nil && p.Base64
}
