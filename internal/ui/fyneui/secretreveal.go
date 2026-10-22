package fyneui

import (
	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Revealing a Secret's value (D-13, H-8). The drawer lists a Secret's keys with their values
// masked and a "Show" button beside each; Show runs secret.reveal from the GUI, the one origin the
// engine hands a value to, and the drawer shows the value in place until the user hides it, the
// drawer moves to another object or closes. The value lives in the drawer's revealed map and its
// widgets only: no toast, no log, no engine state. A masked value is copied without being shown
// (secretcopy.go), and that copy leaves the revealed map alone.

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

// revealedValue renders a revealed value: selectable text in the monospace face, wrapped, so that
// a long token can be read and copied whole. Its copy icon and the copy shortcut put it on the
// clipboard through shell.copyText as a Secret value, which is cleared again later (D-25). The
// icon copies the value alone; the note that a long value was cut is part of the shown text only.
func (d *drawer) revealedValue(it model.DetailItem) fyne.CanvasObject {
	value := it.Value
	for _, a := range it.Actions {
		if n, ok := d.revealedLen[revealKey(a)]; ok && n <= len(value) {
			value = value[:n]
		}
	}
	return newSecretText(it.Value, d.copyFor(value, true), func(sel string) { d.sh.copyText(sel, true) })
}

// actionLabel is an item action's button text: Hide for a value the action revealed.
func (d *drawer) actionLabel(a model.ItemAction) string {
	if _, ok := d.revealed[revealKey(a)]; ok {
		return "Hide"
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
