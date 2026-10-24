package fyneui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"fyne.io/fyne/v2/theme"

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
// word turns into the checkmark for the copy icon's time, and the toast names the key and says "as
// base64", never the value. Outlined and as high as Show and the copy icon beside it (D-104), its
// tooltip "Copy the value as base64". No command copies (TestCopyHasNoCommand): the button runs
// secret.reveal, which agents and the CLI are refused.

// Copy icons' tooltips: a masked Secret value's, a revealed one's; the b64 button's word and tooltip.
const (
	maskedCopyTip = "Copy the value without showing it"
	wholeCopyTip  = "Copy the whole value"
	b64Label      = "b64"
	b64Tip        = "Copy the value as base64"
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
// its checkmark stays.
type b64Button struct {
	btn  *tipButton
	done *time.Timer
}

func newB64Button(sh *shell) *b64Button {
	b := &b64Button{btn: newTipButton(sh, nil, b64Tip, nil)}
	b.btn.Text = b64Label
	b.btn.width = b.btn.MinSize().Width                            // the checkmark in the word's place keeps its width
	b.btn.height = newPinnedCopyIcon(sh, "", nil).MinSize().Height // as high as the copy icon and Show (D-104)
	return b
}

// set makes a tap copy with copyValue, which calls done after a copy, and returns the button.
func (b *b64Button) set(copyValue func(done func())) *tipButton {
	b.btn.OnTapped = func() { copyValue(b.confirm) }
	return b.btn
}

// confirm shows the checkmark in the OK colour in place of the word for the shell's copyConfirm,
// as the copy icon does, then the word again; a later copy restarts it.
func (b *b64Button) confirm() {
	sh := b.btn.sh
	if b.done != nil {
		b.done.Stop()
	}
	b.btn.Text, b.btn.Icon = "", themedIcon(theme.ConfirmIcon(), colorStatusOK)
	b.btn.Refresh()
	var t *time.Timer
	t = time.AfterFunc(sh.copyConfirm, func() {
		sh.do(func() {
			if b.done != t {
				return // a later copy restarted the checkmark
			}
			b.done = nil
			b.btn.Text, b.btn.Icon = b64Label, nil
			b.btn.Refresh()
		})
	})
	b.done = t
}

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
