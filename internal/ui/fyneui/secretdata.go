package fyneui

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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

// A Secret's Data in the drawer, its values edited as a Config Map's are, in Cogpit's own window
// only (the owner's audit answer of 2026-10-15; decisions D-59). Each key stands on a line of its
// own with its value masked below; its buttons end the key's line, "Show [copy] [b64] [remove]",
// always shown, as a revealed key's "Hide [copy] [b64] [remove]" (the owner's RW1 of 2026-10-23; b64,
// the value base64-encoded, his VW7 of 2026-10-24, secretcopy.go), the value line holding the mask
// and the size only; a binary value's line holds "[b64] [remove]". Where the user may save the
// Secret's values (secret.saveData enabled: not the demo cluster, an update right on its Secrets,
// not immutable, no Helm release record), Show reads the whole value (secret.reveal with whole,
// GUI-only) into an editor below the key, the Config Map's editor row (configmapdata.go), and Hide
// in the key's line masks it again; an edited value Hide keeps, masked, until it is saved or
// reverted. Save at the end of the data reviews every edited value at once in a dialog: a masked
// diff (the keys that change and their sizes, never a value) and the server's dry run
// (secret.reviewData), then secret.saveData writes them with the version they are based on as
// precondition. A Secret that changed meanwhile in other keys than the edited ones is reviewed
// rebased onto its newer version (the review shows it); one whose edited keys changed shows the
// conflict and "Save over version N", never an overwrite unasked. Where nothing can be saved Show
// reveals the value read-only as before (secretreveal.go), with the reason under the data.
//
// The values stay in this window's widgets and memory: no command but the GUI-only reveal, review
// and save sees them, no draft keeps them (edited values the drawer moves away from are kept in
// drawer.secretUnsaved until Cogpit quits, and come back masked), a copy from an editor (⌘C, ⌘X or
// the copy icon in its key's line) goes to the clipboard as a Secret's value (concealed, cleared
// after 60 seconds), as does the copy icon in a masked key's line, which copies the value without
// showing it (secretcopy.go, the owner's decision of 2026-10-22), and no toast or status line names
// one.
//
// Keys are added and removed here too (the owner's audit decision of 2026-10-16, datakeys.go): Add
// Key opens a row for the new key's name and puts the key with an empty editor at the end of the
// data, its value typed as text and saved base64-encoded, kept as the other values are (memory
// only, Hide masks it); each key's line ends with a remove button, a removed key shows as removed
// with Undo. The review's masked diff names the keys added and removed, the removed ones in the
// error colour with the Secret, its namespace and its context, and is the confirmation of a
// removal.

func init() { drawerSections["secrets\x00Data"] = secretDataSection }

// secDataKept is what the drawer keeps across rebuilds of a Secret's Data.
const secDataKept = "secret.data"

// secretDataParams mirror secret.reviewData's and secret.saveData's parameters.
type secretDataParams struct {
	Ref             model.ObjectRef   `json:"ref"`
	ResourceVersion string            `json:"resourceVersion"`
	Data            map[string]string `json:"data,omitempty"`
	Add             map[string]string `json:"add,omitempty"`
	Remove          []string          `json:"remove,omitempty"`
	From            map[string]string `json:"from,omitempty"`
}

// secUnsaved are a Secret's edits the drawer moved away from, in the window's memory only: the
// version they are based on, each edited key's text and the value it is based on, the keys added
// with their text, the keys removed.
type secUnsaved struct {
	base              string
	values, from, add map[string]string
	remove            []string
}

// secData is the Data section of the Secret the drawer shows.
type secData struct {
	d      *drawer
	ref    model.ObjectRef
	rows   map[string]*secRow // the keys whose values are in an editor
	list   *fyne.Container
	save   *textButton
	revert *textButton
	// buttons are Add Key, the edited state's pills (WF31), Revert and Save; status the line under
	// them for what the pills don't say.
	counts  *editCounts
	buttons *fyne.Container
	status  *paragraph
	root    *fyne.Container

	width    float32 // the details' width the editors are laid out at
	base     string  // the version the edited values are based on
	latest   string  // the version the drawer model lists the keys at
	reason   string  // why no value can be saved; "" when they can
	hidden   bool    // the save command is not in this build: no footer
	revealed bool    // a value is shown read-only (secretreveal.go)
	closed   bool
	review   *secReview

	// Keys added and removed, saved with the values (datakeys.go).
	adder     *keyAdder
	listed    map[string]bool    // the keys the drawer model lists
	removed   map[string]bool    // the keys marked removed
	added     []string           // the keys added, in the order added
	addedRows map[string]*secRow // their editors

	pins map[string]*keyPin    // the copy icons of the keys whose values are in no editor, by key
	b64s map[string]*b64Button // the b64 buttons of the keys the Secret holds, by key (an added key's is its row's)
}

// keyPin is the copy icon in the line of a key whose value is in no editor, kept across the
// drawer's rebuilds so that its checkmark stays.
type keyPin struct {
	btn  *copyButton
	done *time.Timer
}

// secRow is a key whose value is in an editor.
type secRow struct {
	*cmRow
	at     string // the version the value was revealed at
	masked bool   // an edited value hidden again: the typing kept, the value masked
	added  bool   // a key the user added, not yet saved

	copyBtn  *copyButton // the copy icon in the key's line, made with the first line that shows it
	copyDone *time.Timer // its checkmark's timer
	b64      *b64Button  // an added key's b64 button (a key the Secret holds has its own in secData.b64s)
}

// secretDataSection renders a Secret's Data: the section kept for the shown Secret, rebuilt with
// every change of the drawer model.
func secretDataSection(d *drawer, ref model.ObjectRef, sec model.DetailSection) fyne.CanvasObject {
	sd, ok := d.kept[secDataKept].(*secData)
	if !ok || sd.ref != ref {
		sd = newSecData(d, ref)
		d.kept[secDataKept] = sd
	}
	sd.render(sec)
	return sd.root
}

func newSecData(d *drawer, ref model.ObjectRef) *secData {
	sd := &secData{d: d, ref: ref, rows: map[string]*secRow{}, width: d.contentWidth(), listed: map[string]bool{},
		removed: map[string]bool{}, addedRows: map[string]*secRow{}, pins: map[string]*keyPin{}, b64s: map[string]*b64Button{}}
	sd.list = box(keyedLayout{})
	sd.save = newPlateButton("Save", theme.DocumentSaveIcon(), sd.openReview)
	sd.save.Importance = widget.HighImportance
	sd.revert = newPlateButton("Revert", nil, sd.revertAll)
	sd.adder = newKeyAdder(d.contentWidth(), sd.keyTaken, sd.addKey)
	sd.counts = newEditCounts()
	sd.buttons = footerRow(sd.adder, sd.counts, sd.revert, sd.save, d.contentWidth())
	sd.status = newParagraph("", colorMuted, d.contentWidth())
	sd.root = container.NewVBox(sd.list, dataGap(), sd.adder.row, sd.buttons, sd.status)
	sd.restore()
	return sd
}

// restore brings back the edited values the drawer moved away from, masked: Show reveals them.
func (sd *secData) restore() {
	key := sd.ref.String()
	u := sd.d.secretUnsaved[key]
	if u == nil {
		return
	}
	delete(sd.d.secretUnsaved, key)
	for k, v := range u.values {
		r := sd.newRow(k, u.from[k], u.base)
		r.view.SetText(v)
		r.masked = true
	}
	for _, k := range u.remove {
		sd.removed[k] = true
		if from, ok := u.from[k]; ok && sd.rows[k] == nil {
			sd.newRow(k, from, u.base).masked = true // its value at the base, for the rebase
		}
	}
	for _, k := range mapKeys(u.add) {
		r := sd.newAddedRow(k)
		r.view.SetText(u.add[k])
		r.masked = true
	}
	sd.base = u.base
	sd.setStatus("Your unsaved changes are back, masked: Show reveals them, Save writes them, Revert drops them.", colorStatusWarning)
}

// newRow makes the editor row of a key whose value was revealed at version at.
func (sd *secData) newRow(key, value, at string) *secRow {
	r := &secRow{cmRow: newValueRow(sd.d, sd, key, sd.edited), at: at}
	r.orig, r.from, r.read = value, value, true
	r.view.SetText(value)
	r.view.OnCopy = sd.copied
	sd.rows[key] = r
	return r
}

func (sd *secData) editorWidth() float32 { return sd.width }
func (sd *secData) relayout()            { sd.list.Refresh() }

// editable reports whether Show puts a value into an editor: the user may save it.
func (sd *secData) editable() bool { return !sd.hidden && sd.reason == "" }

// render lays out the keys: an editor below a key whose value is in one, else its masked value (and,
// where nothing can be saved, a value Show revealed read-only).
func (sd *secData) render(sec model.DetailSection) {
	d := sd.d
	if w := d.contentWidth(); w != sd.width {
		sd.width = w
		sd.status.width = w
		sd.status.Refresh()
		setFooterWidth(sd.buttons, w)
	}
	if v := sectionVersion(sec); v != "" && v != sd.latest {
		sd.latest = v
		sd.dropStale()
	}
	sd.reason = sd.saveReason()
	sd.revealed = false
	listed := map[string]bool{}
	objs := make([]fyne.CanvasObject, 0, 2*(len(sec.Items)+len(sd.added)))
	for _, it := range sec.Items {
		listed[it.Name] = true
		r := sd.rows[it.Name]
		switch {
		case it.Name != "" && sd.removed[it.Name]:
			name, note := removedKeyLines(d, it.Name, func() { sd.undoRemove(it.Name) })
			objs = append(objs, name, note)
		case it.Name != "" && r == nil && revealable(it):
			objs = append(objs, sd.revealLines(it)...)
		case it.Name == "" || r == nil: // a binary value, a note
			plain := it
			plain.Actions = nil // its base64 read is the b64 button's, in its name line
			objs = append(objs, sd.nameLine(it), d.itemValue(sd.ref, sec, plain))
		case r.masked:
			objs = append(objs, sd.keyLine(r), sd.maskedLine())
		default:
			objs = append(objs, sd.keyLine(r), r.frame)
		}
	}
	delete(listed, "")
	sd.listed = listed
	for k := range sd.pins { // a key the Secret no longer holds takes its icon along
		if !listed[k] {
			delete(sd.pins, k)
		}
	}
	for k := range sd.b64s { // and its b64 button, unless its edited value still shows
		if !listed[k] && sd.rows[k] == nil {
			delete(sd.b64s, k)
		}
	}
	for _, k := range slices.Sorted(maps.Keys(sd.rows)) { // an edited key the cluster dropped: the review shows the conflict
		if r := sd.rows[k]; !listed[k] && r.dirty() && !sd.removed[k] {
			objs = append(objs, sd.keyLine(r), r.frame)
		}
	}
	for _, k := range slices.Sorted(maps.Keys(sd.removed)) { // a removed key the cluster dropped: the review shows it
		if !listed[k] {
			name, note := removedKeyLines(d, k, func() { sd.undoRemove(k) })
			objs = append(objs, name, note)
		}
	}
	for _, k := range sd.added {
		objs = append(objs, sd.addedLines(sd.addedRows[k], listed[k])...)
	}
	sd.list.Objects = objs
	sd.list.Refresh()
	sd.refreshFooter()
}

// sectionVersion is the Secret's version the drawer model lists its keys at: what its reveal
// actions name.
func sectionVersion(sec model.DetailSection) string {
	for _, it := range sec.Items {
		for _, a := range it.Actions {
			if a.Command != engine.RevealCommand {
				continue
			}
			var p engine.RevealParams
			if json.Unmarshal(a.Params, &p) == nil && p.ResourceVersion != "" {
				return p.ResourceVersion
			}
		}
	}
	return ""
}

// dropStale masks again the values revealed at an older version that the user did not edit: the
// Secret changed (a rotation), and an old value must not stay to be copied (R-6). Edited values
// stay, based on the version they were edited at.
func (sd *secData) dropStale() {
	for k, r := range sd.rows {
		if r.at != sd.latest && !r.dirty() {
			delete(sd.rows, k)
		}
	}
}

// keyLine is the line of a key whose value is in an editor: Hide, the copy icon, the b64 button and
// the remove button at its end; Show in Hide's place while Hide masks the edited value.
func (sd *secData) keyLine(r *secRow) fyne.CanvasObject {
	return sd.buttonsLine(r.key, sd.toggle(r), sd.copyIcon(r), sd.typedB64(r))
}

// toggle is Hide for a value in an editor, Show for one Hide masked.
func (sd *secData) toggle(r *secRow) *textButton {
	if r.masked {
		return revealButton("Show", func() { sd.unmask(r.key) })
	}
	return revealButton(hideLabel, func() { sd.hide(r.key) })
}

// buttonsLine is a key's name with its buttons at the end: Show or Hide, the copy icon, the b64
// button (VW7) and, where the data can be saved, the remove button (RW1).
func (sd *secData) buttonsLine(key string, toggle *textButton, copyIcon, b64 fyne.CanvasObject) fyne.CanvasObject {
	right := container.NewHBox(toggle, copyIcon, b64)
	if sd.editable() {
		right.Add(removeKeyButton(sd.d.sh, key, func() { sd.removeKey(key) }))
	}
	return container.NewBorder(nil, nil, nil, right, sd.d.keyName(key))
}

// revealLines are a key whose value is in no editor: its name line with Show (Hide once Show
// revealed the value read-only), the copy icon and the remove button, and under it the value masked
// with its size, or the value Show revealed, wrapped and selectable (secretreveal.go).
func (sd *secData) revealLines(it model.DetailItem) []fyne.CanvasObject {
	d := sd.d
	a, _ := revealAction(it)
	shown, revealed := d.revealedItem(it)
	toggle := revealButton(d.actionLabel(a), func() { d.sh.runItemAction(sd.ref, a) })
	if !revealed {
		l := newTextLine(it.Value, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{})
		l.truncate = true
		masked := newSelLine(l, nil, func(sel string) { d.sh.copyText(sel, false) })
		pin := sd.pin(it.Name, maskedCopyTip, func(done func()) { d.copyWhole(sd.ref, a, done) })
		return []fyne.CanvasObject{sd.buttonsLine(it.Name, toggle, pin, sd.readB64(it.Name, a)), masked}
	}
	sd.revealed = true
	value := newSecretText(shown.Value, nil, func(sel string) { d.sh.copyText(sel, true) })
	pin := sd.pin(it.Name, wholeCopyTip, func(done func()) {
		if n, ok := d.revealedLen[revealKey(a)]; ok && n < len(shown.Value) { // Show cut it: read it whole
			d.copyWhole(sd.ref, a, done)
			return
		}
		sd.copied(shown.Value)
		done()
	})
	return []fyne.CanvasObject{sd.buttonsLine(it.Name, toggle, pin, sd.readB64(it.Name, a)), value}
}

// readB64 is the b64 button of a key whose value is in no editor: it copies the whole value the
// reveal action a names, base64-encoded, read again (secret.reveal with base64), masked or revealed,
// a binary value's too.
func (sd *secData) readB64(key string, a model.ItemAction) fyne.CanvasObject {
	return sd.b64Of(key).set(func(done func()) { sd.d.copyBase64(sd.ref, a, done) })
}

// typedB64 is the b64 button of a key whose value is in an editor: it copies the value as typed,
// base64-encoded, as the copy icon beside it copies it as typed.
func (sd *secData) typedB64(r *secRow) fyne.CanvasObject {
	b := r.b64
	if !r.added {
		b = sd.b64Of(r.key)
	} else if b == nil {
		b = newB64Button(sd.d.sh)
		r.b64 = b
	}
	return b.set(func(done func()) { sd.d.copyTypedBase64(r.key, r.view.Text(), done) })
}

// b64Of is the b64 button of a key the Secret holds, kept across rebuilds.
func (sd *secData) b64Of(key string) *b64Button {
	b := sd.b64s[key]
	if b == nil {
		b = newB64Button(sd.d.sh)
		sd.b64s[key] = b
	}
	return b
}

// pin is the copy icon in the line of key, whose value is in no editor, with the tooltip tip;
// copyValue copies and calls done after a copy, for the icon's checkmark. The icon is kept across
// rebuilds.
func (sd *secData) pin(key, tip string, copyValue func(done func())) fyne.CanvasObject {
	p := sd.pins[key]
	if p == nil {
		p = &keyPin{}
		p.btn = newPinnedCopyIcon(sd.d.sh, tip, nil)
		sd.pins[key] = p
	}
	p.btn.tip = tip
	p.btn.tap = func() { copyValue(func() { p.btn.confirm(&p.done) }) }
	return container.NewCenter(p.btn)
}

// nameLine is a key above its value without Show (a binary value): its name, its b64 button, the
// only copy of a binary value (VW7), and, where the data can be saved, the remove button.
func (sd *secData) nameLine(it model.DetailItem) fyne.CanvasObject {
	key := it.Name
	if key == "" {
		return sd.d.keyName(key)
	}
	right := container.NewHBox()
	if a, ok := base64Action(it); ok {
		right.Add(sd.readB64(key, a))
	}
	if sd.editable() {
		right.Add(removeKeyButton(sd.d.sh, key, func() { sd.removeKey(key) }))
	}
	if len(right.Objects) == 0 {
		return sd.d.keyName(key)
	}
	return container.NewBorder(nil, nil, nil, right, sd.d.keyName(key))
}

// addedLines are an added key's lines: its name with Hide, the copy icon and the button that drops
// it, and its editor; once hidden Show in Hide's place and its value masked.
func (sd *secData) addedLines(r *secRow, exists bool) []fyne.CanvasObject {
	line := addedKeyLine(sd.d, r.key, exists, sd.toggle(r), []fyne.CanvasObject{sd.copyIcon(r), sd.typedB64(r)}, func() { sd.dropAdded(r.key) })
	if r.masked {
		return []fyne.CanvasObject{line, sd.maskedLine()}
	}
	return []fyne.CanvasObject{line, r.frame}
}

// copyIcon is the copy icon in the line of a key whose value is in an editor, right above the
// editor's top right and covering none of its text: a click copies the editor's whole value as
// ⌘C and ⌘X copy a selection of it, to the clipboard for Secret values (the owner's audit decision
// of 2026-10-16, D-59, D-25); while Hide masks the value, as typed, without showing it. A Config
// Map's editors have none. The row keeps the icon across the drawer's rebuilds, so that its
// checkmark stays.
func (sd *secData) copyIcon(r *secRow) fyne.CanvasObject {
	if r.copyBtn == nil {
		r.copyBtn = newPinnedCopyIcon(sd.d.sh, wholeCopyTip, func() { sd.copyValue(r) })
	}
	r.copyBtn.tip = wholeCopyTip
	if r.masked {
		r.copyBtn.tip = maskedCopyTip
	}
	return container.NewCenter(r.copyBtn)
}

// copyValue copies a row's whole value, as typed so far, as a Secret's value, naming the key while
// the value is masked (copyUnseen); an empty value copies nothing, so that the click does not empty
// the clipboard.
func (sd *secData) copyValue(r *secRow) {
	if r.masked {
		sd.d.copyUnseen(r.key, r.view.Text(), func() { r.copyBtn.confirm(&r.copyDone) })
		return
	}
	text := r.view.Text()
	if text == "" {
		sd.d.sh.copyToast("info", "The value is empty: nothing was copied.")
		return
	}
	sd.copied(text)
	r.copyBtn.confirm(&r.copyDone)
}

// rowOf is a key's editor row, an added key's included; nil when its value is in none.
func (sd *secData) rowOf(key string) *secRow {
	if r := sd.addedRows[key]; r != nil {
		return r
	}
	return sd.rows[key]
}

// keyTaken says why key cannot be added: the Secret, or the drawer, holds it already.
func (sd *secData) keyTaken(key string) string {
	switch {
	case sd.addedRows[key] != nil:
		return "You added this key already."
	case sd.listed[key] || sd.rows[key] != nil:
		return "The Secret holds this key already: Show its value to edit it."
	}
	return ""
}

// addKey adds a key with an empty value at the end of the data.
func (sd *secData) addKey(key string) {
	sd.beginEdit()
	r := sd.newAddedRow(key)
	sd.d.rebuild()
	if c := fyne.CurrentApp().Driver().CanvasForObject(sd.list); c != nil {
		c.Focus(r.view)
	}
}

// newAddedRow makes the editor row of an added key, typed at the version the drawer lists.
func (sd *secData) newAddedRow(key string) *secRow {
	r := &secRow{cmRow: newValueRow(sd.d, sd, key, sd.edited), at: sd.latest, added: true}
	r.read = true
	r.view.OnCopy = sd.copied
	sd.addedRows[key] = r
	sd.added = append(sd.added, key)
	return r
}

// dropAdded drops an added key and its value.
func (sd *secData) dropAdded(key string) {
	delete(sd.addedRows, key)
	sd.added = slices.DeleteFunc(sd.added, func(k string) bool { return k == key })
	sd.d.rebuild()
}

// removeKey marks a key removed; Save removes it.
func (sd *secData) removeKey(key string) {
	sd.beginEdit()
	sd.removed[key] = true
	sd.d.rebuild()
}

// undoRemove takes a key's removal back; a value kept only as the removal's base leaves the
// window's memory.
func (sd *secData) undoRemove(key string) {
	delete(sd.removed, key)
	if r := sd.rows[key]; r != nil && r.masked && !r.dirty() {
		delete(sd.rows, key)
	}
	sd.d.rebuild()
}

// beginEdit bases the edits on the version the drawer lists when this is the first of them.
func (sd *secData) beginEdit() {
	if !sd.dirty() {
		sd.base = sd.latest
	}
}

// maskedLine is an edited value hidden again: masked, Show and the copy icon in its key's line
// (keyLine), the icon copying the value as typed without showing it (the owner's decision of
// 2026-10-22, secretcopy.go).
func (sd *secData) maskedLine() fyne.CanvasObject {
	line := newTextLine(engine.MaskedValue+" (edited, not saved)", colorStatusWarning, theme.SizeNameText, fyne.TextStyle{})
	return newSelLine(line, nil, func(sel string) { sd.d.sh.copyText(sel, false) })
}

// reveal reads a key's whole value into an editor (Show on a Secret the user may save). The answer
// of an older listing, or one for a drawer that moved on, is dropped.
func (sd *secData) reveal(a model.ItemAction) {
	var p engine.RevealParams
	if err := json.Unmarshal(a.Params, &p); err != nil {
		sd.d.sh.showError(err)
		return
	}
	p.Whole = true
	sd.d.sh.dispatch(a.Command, p, func(res any, err error) {
		if err != nil {
			sd.d.sh.showError(err)
			return
		}
		var v engine.SecretValue
		if decode(res, &v) != nil || sd.closed || sd.d.shown == nil || *sd.d.shown != sd.ref || p.ResourceVersion != sd.latest {
			return // the drawer moved on, or the Secret changed meanwhile: keep nothing
		}
		if _, ok := sd.rows[p.Key]; !ok && !v.Cut {
			sd.newRow(p.Key, v.Value, p.ResourceVersion)
		}
		sd.d.rebuild()
	})
}

// hide masks a value again: an unedited one leaves the window's memory, an edited or added one
// stays, masked, until it is saved or reverted.
func (sd *secData) hide(key string) {
	r := sd.rowOf(key)
	if r == nil {
		return
	}
	if r.dirty() || r.added {
		r.masked = true
	} else {
		delete(sd.rows, key)
	}
	sd.d.rebuild()
}

// unmask shows an edited value that Hide masked.
func (sd *secData) unmask(key string) {
	if r := sd.rowOf(key); r != nil {
		r.masked = false
		sd.d.rebuild()
	}
}

// edited follows the user's typing: the editor's height, the version the edits are based on (the
// first edited value's), the footer.
func (sd *secData) edited(cr *cmRow) {
	cr.resized()
	if r := sd.rows[cr.key]; r != nil && sd.dirtyBesides(cr.key) == 0 && cr.dirty() {
		sd.base = r.at
	}
	sd.refreshFooter()
}

// dirtyBesides counts the edited values other than key's, and the keys added and removed.
func (sd *secData) dirtyBesides(key string) int {
	n := len(sd.added) + len(sd.removed)
	for k, r := range sd.rows {
		if k != key && r.dirty() && !sd.removed[k] {
			n++
		}
	}
	return n
}

// dirty reports whether a value was edited or a key added or removed.
func (sd *secData) dirty() bool { return sd.dirtyBesides("") > 0 }

// edits are the edited values by key, of the keys not removed, and froms the values they and the
// removed keys whose values were revealed are based on.
func (sd *secData) edits() (values, from map[string]string) {
	values, from = map[string]string{}, map[string]string{}
	for k, r := range sd.rows {
		switch {
		case sd.removed[k]:
			from[k] = r.from
		case r.dirty():
			values[k], from[k] = r.view.Text(), r.from
		}
	}
	return values, from
}

// adds are the added keys with their editors' text.
func (sd *secData) adds() map[string]string {
	out := map[string]string{}
	for k, r := range sd.addedRows {
		out[k] = r.view.Text()
	}
	return out
}

// removes are the keys marked removed, sorted.
func (sd *secData) removes() []string { return slices.Sorted(maps.Keys(sd.removed)) }

// saveReason says why no value can be saved: the command's reason (the demo cluster, no update
// permission, an immutable Secret, a Helm release record); "" when Save is possible.
func (sd *secData) saveReason() string {
	err := sd.d.sh.commandEnabled(engine.SecretSaveDataCommand, secretDataParams{Ref: sd.ref, ResourceVersion: "0", Data: map[string]string{}})
	sd.hidden = errors.Is(err, errUnavailable)
	if err != nil {
		return errText(err)
	}
	return ""
}

// refreshFooter makes the editors editable or read-only and sets Save, Revert, the edited state's
// pills and the status line.
func (sd *secData) refreshFooter() {
	editable := sd.editable()
	for _, r := range sd.rows {
		if ro := !editable; ro != r.view.ReadOnly() {
			r.view.SetReadOnly(ro)
		}
	}
	for _, r := range sd.addedRows {
		if ro := !editable; ro != r.view.ReadOnly() {
			r.view.SetReadOnly(ro)
		}
	}
	dirty := sd.dirty()
	editing := len(sd.rows) > 0 || dirty // Revert and Save once a value is in an editor or a key changes
	setVisible(sd.buttons, !sd.hidden && (editing || editable))
	setVisible(sd.revert, editing)
	setVisible(sd.save, editing)
	sd.adder.setEditable(editable && sd.review == nil)
	enable(sd.save, editable && dirty && sd.review == nil)
	enable(sd.revert, dirty && sd.review == nil)
	if values, _ := sd.edits(); dirty && !sd.hidden { // the edited state beside Revert and Save (WF31)
		sd.counts.set(len(values), len(sd.added), len(sd.removed))
	} else {
		sd.counts.set(0, 0, 0)
	}
	switch {
	case sd.hidden:
		sd.setStatus("", colorMuted)
	case sd.reason != "" && (sd.revealed || len(sd.rows) > 0):
		sd.setStatus("Read-only: "+sd.reason, colorMuted)
	case dirty && sd.latest != sd.base:
		sd.setStatus("Changed on the cluster since your edits began: Save rebases your changes onto it, or first shows where "+
			"they clash.", colorStatusWarning)
	case dirty:
		if !strings.HasPrefix(sd.status.text, "Your unsaved changes are back") {
			sd.setStatus("", colorMuted) // the pills say it
		}
	case strings.HasPrefix(sd.status.text, "Saved"):
	default:
		sd.setStatus("", colorMuted)
	}
}

// setVisible shows or hides o.
func setVisible(o fyne.CanvasObject, on bool) {
	if on && !o.Visible() {
		o.Show()
	} else if !on && o.Visible() {
		o.Hide()
	}
}

// setStatus shows text under the buttons; "" hides the line.
func (sd *secData) setStatus(text string, c fyne.ThemeColorName) {
	sd.status.text, sd.status.color = text, c
	setVisible(sd.status, text != "")
	sd.status.Refresh()
}

// revertAll puts the values back: an edited value revealed at the version the drawer lists goes
// back to it, one revealed at an older version is masked again.
func (sd *secData) revertAll() {
	for k, r := range sd.rows {
		switch {
		case !r.dirty() && !r.masked:
		case r.at == sd.latest && !r.masked:
			r.from = r.orig
			r.view.SetText(r.orig)
			r.resized()
		default:
			delete(sd.rows, k)
		}
	}
	sd.removed, sd.added, sd.addedRows = map[string]bool{}, nil, map[string]*secRow{}
	sd.base = ""
	sd.setStatus("", colorMuted)
	sd.d.rebuild()
}

// saved follows a save: the values written are the editors' values now, an added key's editor a
// revealed value's, a removed key gone, at the stored version.
func (sd *secData) saved(p secretDataParams, rv string) {
	for k, v := range p.Data {
		if r := sd.rowOf(k); r != nil {
			r.orig, r.from, r.at, r.added = v, v, rv, false
			sd.rows[k] = r
		}
	}
	for k, v := range p.Add {
		if r := sd.addedRows[k]; r != nil {
			r.orig, r.from, r.at, r.added = v, v, rv, false
			sd.rows[k] = r
		}
	}
	for _, k := range p.Remove {
		delete(sd.rows, k)
	}
	sd.removed, sd.added, sd.addedRows = map[string]bool{}, nil, map[string]*secRow{}
	sd.base = ""
	sd.setStatus("Saved "+editsText(len(p.Data), len(p.Add), len(p.Remove))+" at "+sd.d.sh.timeFormat().FormatTime(time.Now()), colorStatusOK)
	sd.d.rebuild()
}

// copied puts text an editor copied on the clipboard as a Secret's value: concealed for clipboard
// managers, cleared after a minute if the clipboard still holds it (D-25, H-25).
func (sd *secData) copied(text string) {
	sh := sd.d.sh
	sh.copyText(text, true)
	sh.copyToast("info", fmt.Sprintf("Copied. The clipboard is cleared in %s.", ttlText(sh.secretClipTTL)))
}

// close keeps the edited values in the window's memory when the drawer moves to another object or
// closes, and says so; a Secret's values never go to disk. The editors' undo histories, which hold
// every value typed, go: the values come back without them (D-84).
func (sd *secData) close() {
	if sd.review != nil {
		sd.review.hide()
	}
	sd.closed = true
	for _, r := range sd.rows {
		r.view.ClearHistory()
	}
	for _, r := range sd.addedRows {
		r.view.ClearHistory()
	}
	if !sd.dirty() {
		return
	}
	values, from := sd.edits()
	if sd.d.secretUnsaved == nil {
		sd.d.secretUnsaved = map[string]*secUnsaved{}
	}
	sd.d.secretUnsaved[sd.ref.String()] = &secUnsaved{base: sd.base, values: values, from: from, add: sd.adds(), remove: sd.removes()}
	sd.d.sh.toast("info", fmt.Sprintf("Your unsaved changes to Secret %s are kept in this window's memory until Cogpit quits, never "+
		"on disk: open it again to save or revert them.", sd.ref.Key()))
}

// secretDataOf returns the Data section the drawer keeps for the Secret ref; nil when it shows
// another object.
func (d *drawer) secretDataOf(ref model.ObjectRef) *secData {
	sd, ok := d.kept[secDataKept].(*secData)
	if !ok || sd.ref != ref || sd.closed {
		return nil
	}
	return sd
}

// secReview is the review of a Secret's save: the masked diff and the server's dry run, then Save.
type secReview struct {
	sd                *secData
	values, from, add map[string]string
	removes           []string
	base              string // the version the shown changes and dry run are of, which Save writes with
	uid               string // the Secret's the review is of, which a removal's confirmation binds
	last              *engine.SecretDataReview
	dlg               *dialogFrame
	changes           *fyne.Container // one line per key that changes
	banner            *textLine       // a newer version on the cluster (showOver); hidden otherwise
	keys              *fyne.Container // the head, reviewHead
	top               *fyne.Container // the head and the banner
	status            *paragraph
	save              *textButton
	width             float32
	opened            time.Time
	ready             bool   // Save may be enabled
	over              string // the newer version Save writes the values over; "" none
	closed            bool
}

// openReview reviews the edits in a dialog over the window.
func (sd *secData) openReview() {
	values, from := sd.edits()
	add, removes := sd.adds(), sd.removes()
	if len(values)+len(add)+len(removes) == 0 || sd.review != nil {
		return
	}
	width := min(float32(620), sd.d.sh.win.Canvas().Size().Width-120)
	rv := &secReview{sd: sd, values: values, from: from, add: add, removes: removes, base: sd.base, changes: container.NewVBox(),
		width: width, opened: time.Now()}
	rv.banner = newTextLine("", colorStatusWarning, theme.SizeNameText, fyne.TextStyle{Bold: true})
	rv.banner.truncate = true
	rv.banner.Hide()
	rv.keys = container.NewVBox()
	setKeysBlock(rv.keys, reviewHead("Secret", sd.ref, "", mapKeys(add), removes, width))
	rv.top = container.NewVBox(rv.keys, rv.banner, newDivider(false))
	rv.status = newParagraph("", colorMuted, width)
	rv.save = newPlateButton("Save", theme.DocumentSaveIcon(), rv.commit)
	rv.save.Importance = widget.HighImportance
	if len(removes) > 0 {
		rv.save.Importance = widget.DangerImportance
	}
	cancel := newPlateButton("Cancel", nil, func() { rv.hide() })
	list := box(sizedMin{size: fyne.NewSize(width, 120)}, container.NewStack(newSurface(theme.ColorNameInputBackground),
		inset(theme.SizeNameInnerPadding, theme.SizeNameInnerPadding, container.NewVScroll(rv.changes))))
	content := container.NewBorder(rv.top, container.NewVBox(newDivider(false), rv.status), nil, nil, list)
	rv.dlg = newDialogFrame(sd.d.sh, objectDialogTitle("Save", "Secret", sd.ref.Name, "")) // no gap after the arrow (dialogframe.go)
	rv.dlg.setContent(content)
	rv.dlg.SetButtons([]fyne.CanvasObject{cancel, rv.save})
	rv.dlg.SetOnClosed(func() { rv.closed = true; sd.review = nil; sd.refreshFooter() })
	sd.review = rv
	sd.refreshFooter()
	rv.dlg.Show()
	rv.check()
}

// setChanges shows the masked diff: each key, what changes and the sizes, never a value.
func (rv *secReview) setChanges(changes []engine.SecretKeyChange) {
	objs := make([]fyne.CanvasObject, 0, len(changes))
	for _, c := range changes {
		what, colour := "changed, "+byteText(c.Bytes)+" → "+byteText(c.NewBytes), colorMuted
		switch c.Change {
		case engine.SecretKeyAdded:
			what = "added, " + byteText(c.NewBytes)
		case engine.SecretKeyRemoved:
			what, colour = "removed, "+byteText(c.Bytes), colorStatusError
		}
		key := newTextLine(c.Key, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Monospace: true})
		if c.Change == engine.SecretKeyRemoved {
			key.SetColor(colorStatusError)
		}
		key.truncate = true
		objs = append(objs, container.NewBorder(nil, nil, nil,
			newTextLine(what+"  "+engine.MaskedValue, colour, theme.SizeNameText, fyne.TextStyle{}), key))
	}
	rv.changes.Objects = objs
	rv.changes.Refresh()
}

// check asks for the masked diff and the server's dry run, based on the version the review is
// of. A Secret that changed since in other keys than the edited ones comes back rebased onto its
// newer version, which the review is of from then on.
func (rv *secReview) check() {
	sd := rv.sd
	rv.over, rv.ready = "", false
	rv.save.SetText("Save")
	rv.save.Disable()
	setVisible(rv.banner, false)
	rv.top.Refresh()
	rv.setStatus("Comparing with the Secret in the cluster and checking with the server (dry run)…", colorMuted)
	params := secretDataParams{Ref: sd.ref, ResourceVersion: rv.base, Data: rv.values, Add: rv.add, Remove: rv.removes, From: rv.from}
	sd.d.sh.dispatch(engine.SecretReviewDataCommand, params, func(res any, err error) {
		if rv.closed {
			return
		}
		var r engine.SecretDataReview
		if err == nil {
			err = decode(res, &r)
		}
		switch {
		case err != nil:
			rv.setStatus("Could not review the change: "+errText(err), colorStatusError)
			return
		case len(r.Changes) == 0:
			rv.hide()
			sd.setStatus("No changes to save: the values are the Secret's.", colorMuted)
			return
		}
		rv.last, rv.uid = &r, r.UID
		rv.setChanges(r.Changes)
		added, removed := changedKeys(r.Changes)
		changes := editsText(len(r.Changes)-len(added)-len(removed), len(added), len(removed)) + " · no value shown"
		if r.RebasedFrom != "" && !r.Stale {
			rv.base, sd.base = r.BaseResourceVersion, r.BaseResourceVersion
			changes += " · rebased onto the cluster's change in other keys"
		}
		setKeysBlock(rv.keys, reviewHead("Secret", resolvedRef(r.Ref, sd.ref), changes, added, removed, rv.width))
		switch {
		case r.Stale:
			rv.showOver(r.BaseResourceVersion, r.CachedResourceVersion)
		case r.Accepted:
			rv.setStatus("The server accepts the change (dry run).", colorStatusOK)
			rv.arm()
		default:
			rv.setStatus("The server refuses the change: "+r.Refused, colorStatusError)
		}
		sd.refreshFooter()
	})
}

// showOver shows the changes against a newer version of the Secret: Save then writes the values
// over it, a second, explicit step.
func (rv *secReview) showOver(base, current string) {
	if current == "" || current == base {
		rv.setStatus("The Secret changed on the cluster and Cogpit has not seen the change yet; nothing was saved. "+
			"Save again in a moment.", colorStatusWarning)
		rv.arm()
		return
	}
	rv.over = current
	rv.banner.SetText("Changed on the cluster since your edits began: your changes against its current state")
	setVisible(rv.banner, true)
	rv.top.Refresh()
	rv.setStatus("Nothing was saved: a key you edited, added or removed changed there too. Overwrite writes your changes over "+
		"it; the cluster's other changes stay.", colorStatusWarning)
	rv.save.SetText("Overwrite")
	rv.arm()
}

// arm enables Save, a removal's after the confirmation's delay (armSave).
func (rv *secReview) arm() {
	rv.ready = true
	armSave(rv.sd.d.sh, rv.save, len(rv.removes) > 0, rv.opened, func() bool { return rv.ready && !rv.closed })
}

// takeOver makes the edit what the review against the newer version showed, which Save over that
// version writes: an added key the Secret holds by then is a change of its value, a removed key it
// no longer holds is dropped.
func (rv *secReview) takeOver() {
	if rv.last == nil {
		return
	}
	added, removed := changedKeys(rv.last.Changes)
	for k, v := range rv.add {
		if !slices.Contains(added, k) {
			rv.values[k] = v
			delete(rv.add, k)
		}
	}
	rv.removes = slices.DeleteFunc(rv.removes, func(k string) bool { return !slices.Contains(removed, k) })
}

// changedKeys are the keys a masked diff adds and removes.
func changedKeys(changes []engine.SecretKeyChange) (added, removed []string) {
	for _, c := range changes {
		switch c.Change {
		case engine.SecretKeyAdded:
			added = append(added, c.Key)
		case engine.SecretKeyRemoved:
			removed = append(removed, c.Key)
		}
	}
	return added, removed
}

// commit saves the reviewed values, over the newer version when the review showed one; a save
// that meets a still newer version reviews the values again and writes nothing until Save.
func (rv *secReview) commit() {
	sd := rv.sd
	if rv.over != "" {
		rv.base, sd.base, rv.over = rv.over, rv.over, ""
		rv.takeOver()
	}
	rv.ready = false
	rv.save.Disable()
	rv.setStatus("Saving…", colorMuted)
	params := secretDataParams{Ref: sd.ref, ResourceVersion: rv.base, Data: rv.values, Add: rv.add, Remove: rv.removes}
	// A removal is destructive: this review is its confirmation (datakeys.go).
	forget := confirmRemoval(sd.d.sh, engine.SecretSaveDataCommand, rv.target(), rv.uid,
		engine.SecretSaveDetail(mapKeys(params.Data), mapKeys(params.Add), params.Remove), len(params.Remove) > 0)
	sd.d.sh.dispatch(engine.SecretSaveDataCommand, params, func(res any, err error) {
		forget()
		if rv.closed {
			return
		}
		var ce *engine.DataConflictError
		switch {
		case errors.As(err, &ce):
			rv.check()
			return
		case err != nil:
			rv.setStatus("Not saved: "+errText(err), colorStatusError)
			rv.arm()
			return
		}
		var s engine.SecretDataSaved
		_ = decode(res, &s)
		rv.hide()
		sd.saved(params, s.ResourceVersion)
	})
}

func (rv *secReview) setStatus(text string, c fyne.ThemeColorName) {
	rv.status.text, rv.status.color = text, c
	rv.status.Refresh()
}

// hide closes the review; the edited values stay in the editors.
func (rv *secReview) hide() {
	if rv.closed {
		return
	}
	rv.dlg.Hide() // SetOnClosed marks it closed
}
