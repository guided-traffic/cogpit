package fyneui

import (
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Revealing a Secret's value (D-13, H-8). The drawer lists a Secret's keys with their values
// masked and a "Show" button at the end of each key's line (secretdata.go, RW1); Show runs
// secret.reveal from the GUI, the one origin the engine hands a value to, and the drawer shows the
// value in place until the user hides it, the drawer moves to another object or closes. The value
// lives in the drawer's revealed map and its widgets only: no toast, no log, no engine state. A
// masked value is copied without being shown (secretcopy.go), and that copy leaves the revealed
// map alone. Show all in the head of the data is Show on every masked key (secretshowall.go).

func init() { itemActionDialogs[engine.RevealCommand] = toggleReveal }

// revealKey names a revealed value by the action that revealed it: the command and its params
// (the Secret and the key).
func revealKey(a model.ItemAction) string { return a.Command + "\x00" + string(a.Params) }

// toggleReveal shows the value an item action reveals, or hides it again. On a Secret whose values
// the user may save, Show puts the whole value into an editor instead (secretdata.go, D-59).
func toggleReveal(sh *shell, ref model.ObjectRef, a model.ItemAction) {
	d := sh.drawer
	key := revealKey(a)
	if _, shown := d.revealed[key]; shown {
		delete(d.revealed, key)
		delete(d.revealedLen, key)
		d.rebuild()
		return
	}
	revealOne(sh, ref, a, nil)
}

// revealOne is Show on a masked value: secret.reveal from the GUI, the value kept in the drawer's
// revealed map, or, on a Secret whose values the user may save, read whole into an editor. b is
// Show all's batch of reads (secretshowall.go); nil for a single Show.
func revealOne(sh *shell, ref model.ObjectRef, a model.ItemAction, b *revealBatch) {
	d := sh.drawer
	if sd := d.secretDataOf(ref); sd != nil && sd.editable() {
		sd.reveal(a, b)
		return
	}
	key := revealKey(a)
	sh.dispatch(a.Command, a.Params, func(res any, err error) {
		kept := false
		if err == nil {
			kept = d.keepRevealed(ref, key, res)
		}
		b.answered(d, ref, err, kept)
	})
}

// keepRevealed keeps a revealed value in the drawer's revealed map, a value Show cut with a note
// that says so, and reports whether it did: nothing when the drawer moved on meanwhile.
func (d *drawer) keepRevealed(ref model.ObjectRef, key string, res any) bool {
	var v engine.SecretValue
	if decode(res, &v) != nil || d.shown == nil || *d.shown != ref {
		return false // the drawer moved on meanwhile: keep nothing
	}
	text := v.Value
	if v.Cut {
		text += "\n[the first " + byteText(len(v.Value)) + " of " + byteText(v.Size) + "]"
	}
	d.revealed[key] = text
	d.revealedLen[key] = len(v.Value)
	return true
}

// pruneRevealed drops the revealed values no item of m offers any more: the reveal action names the
// Secret's version, so a Secret changed or created again under its name shows its keys masked,
// and the old value leaves the drawer's memory (R-6 of the fourth run's review).
func (d *drawer) pruneRevealed(m *engine.DetailModel) {
	if len(d.revealed) == 0 {
		return
	}
	offered := map[string]bool{}
	for _, sec := range m.Sections {
		for _, it := range sec.Items {
			for _, a := range it.Actions {
				offered[revealKey(a)] = true
			}
		}
	}
	for k := range d.revealed {
		if !offered[k] {
			delete(d.revealed, k)
			delete(d.revealedLen, k)
		}
	}
}

// revealedItem returns an item with its revealed value in place of the masked one, and whether
// one of its actions revealed it.
func (d *drawer) revealedItem(it model.DetailItem) (model.DetailItem, bool) {
	for _, a := range it.Actions {
		if v, ok := d.revealed[revealKey(a)]; ok {
			it.Value = v
			return it, true
		}
	}
	return it, false
}

// revealButton is Show or Hide on a Secret's value. Show exposes the value, so it is marked as
// exposing (the owner's RW3 of 2026-10-23: "angemessen (als exposing) markiert … Vielleicht rot,
// weil es löscht ja nichts, aber ein warnendes Orange?"; his pick of three rendered looks, variant
// A): its word, its outline and an eye icon in the status warning's amber, the tone of "(edited,
// not saved)"; filled it would be louder than Save, with the outline alone it read as any button in
// the light theme. Hide puts the value away again: the eye struck through in the status OK's green
// (the owner's CW2 of 2026-10-23, "Hide könnte doch ein grünes Button sein, weil es die visuelle
// sicherheit wiederherstellt, vielleicht haben wir auch ein geschlossenes Auge?"; his pick of three
// rendered looks, variant C). Red stays for what removes.
//
// Show and Hide are one toggle in one place: each is as wide as the wider of the two (the owner's
// CW1 of 2026-10-23: "Hide soll genau so groß sein wie Show, damit man mit der Maus nicht erneut
// auf hide ziehen muss"; the line ends at the right, so a narrower Hide started right of Show's
// eye). They are one widget too, which setRevealLook turns from one into the other, kept by its
// owner across rebuilds (the owner's VW1 of 2026-10-23: "wenn man auf Show klickt, ist man ja dann
// direkt auf dem Hide-Button, dieser ist dann aber erstmal so als ob man nicht darüber ist"): Fyne
// sends MouseIn only when the pointer moves, so a new Hide put where Show was started without its
// hover, while the one widget keeps the hover it had as Show.
func revealButton(label string, tapped func()) *textButton {
	return revealToggle(showLabel, hideLabel, label, tapped)
}

// revealToggle is a Show and Hide toggle with the words show and hide, as wide as the wider of
// them, showing label: a key's Show and Hide, the data's Show all and Hide all.
func revealToggle(show, hide, label string, tapped func()) *textButton {
	b := newTextButton("", nil, tapped)
	setRevealLook(b, show)
	w := b.MinSize().Width
	setRevealLook(b, hide)
	b.width = max(w, b.MinSize().Width)
	setRevealLook(b, label)
	return b
}

// setRevealLook turns b into Show or Hide (Show all or Hide all) in place: its word, its eye and
// its tone.
func setRevealLook(b *textButton, label string) {
	icon, tone := theme.VisibilityIcon(), colorStatusWarning
	if label == hideLabel || label == hideAllLabel {
		icon, tone = theme.VisibilityOffIcon(), colorStatusOK
	}
	if b.Text == label && b.tone == tone {
		return
	}
	b.Text, b.Icon, b.tone = label, icon, tone
	b.Refresh()
}

// hideLabel is the word on the button that masks a value Show revealed, showLabel the one that
// reveals it; showAllLabel and hideAllLabel do so for every value of a Secret's data at once
// (secretshowall.go).
const (
	hideLabel    = "Hide"
	showLabel    = "Show"
	showAllLabel = "Show all"
	hideAllLabel = "Hide all"
)

// actionLabel is an item action's button text: Hide for a value the action revealed.
func (d *drawer) actionLabel(a model.ItemAction) string {
	if _, ok := d.revealed[revealKey(a)]; ok {
		return hideLabel
	}
	return a.Label
}

// revealable reports whether an item's value can be revealed in place.
func revealable(it model.DetailItem) bool {
	_, ok := revealAction(it)
	return ok
}

// byteText renders a size as every byte amount is (D-32): "24 B", "4.0 KiB", "1.5 MiB".
func byteText(n int) string { return model.FormatBytes(int64(n)) }
