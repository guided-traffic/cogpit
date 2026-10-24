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
// map alone.

func init() { itemActionDialogs[engine.RevealCommand] = toggleReveal }

// revealKey names a revealed value by the action that revealed it: the command and its params
// (the Secret and the key).
func revealKey(a model.ItemAction) string { return a.Command + "\x00" + string(a.Params) }

// toggleReveal shows the value an item action reveals, or hides it again. On a Secret whose values
// the user may save, Show puts the whole value into an editor instead (secretdata.go, D-59).
func toggleReveal(sh *shell, ref model.ObjectRef, a model.ItemAction) {
	d := sh.drawer
	key := revealKey(a)
	if sd := d.secretDataOf(ref); sd != nil && sd.editable() {
		if _, shown := d.revealed[key]; !shown {
			sd.reveal(a)
			return
		}
	}
	if _, shown := d.revealed[key]; shown {
		delete(d.revealed, key)
		delete(d.revealedLen, key)
		d.rebuild()
		return
	}
	sh.dispatch(a.Command, a.Params, func(res any, err error) {
		if err != nil {
			sh.showError(err)
			return
		}
		var v engine.SecretValue
		if decode(res, &v) != nil || d.shown == nil || *d.shown != ref {
			return // the drawer moved on meanwhile: keep nothing
		}
		text := v.Value
		if v.Cut {
			text += "\n[the first " + byteText(len(v.Value)) + " of " + byteText(v.Size) + "]"
		}
		d.revealed[key] = text
		d.revealedLen[key] = len(v.Value)
		d.rebuild()
	})
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
	b := newTextButton("", nil, tapped)
	setRevealLook(b, showLabel)
	show := b.MinSize().Width
	setRevealLook(b, hideLabel)
	b.width = max(show, b.MinSize().Width)
	setRevealLook(b, label)
	return b
}

// setRevealLook turns b into Show or Hide in place: its word, its eye and its tone.
func setRevealLook(b *textButton, label string) {
	icon, tone := theme.VisibilityIcon(), colorStatusWarning
	if label == hideLabel {
		icon, tone = theme.VisibilityOffIcon(), colorStatusOK
	}
	if b.Text == label && b.tone == tone {
		return
	}
	b.Text, b.Icon, b.tone = label, icon, tone
	b.Refresh()
}

// hideLabel is the word on the button that masks a value Show revealed, showLabel the one that
// reveals it.
const (
	hideLabel = "Hide"
	showLabel = "Show"
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
