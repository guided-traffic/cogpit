package fyneui

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Keys added and removed in a Config Map's and a Secret's drawer (the owner's audit decision of
// 2026-10-16, N152 (b); decisions D-55 and D-59). Where the user may save the data, each key's
// line ends with a remove button and the footer starts with Add Key, which opens a row for the new
// key's name, checked as the API server checks it (engine.CheckDataKey) and against the keys the
// data and the drawer hold; Add puts the key at the end of the data with an empty editor. A removed
// key shows as removed, its value hidden, with Undo; Save writes every change at once. The review
// before Save names the keys removed, in the error colour with the object, its namespace and its
// context, and the keys added; a save that removes a key is destructive, so the review is its
// confirmation (helmrollback.go's dialog answer): its Save arms after the confirmation's delay and
// hands the engine the answer for exactly that command, object, UID and keys.

// keyAdder is the footer's Add Key and the row it opens: the new key's name, Add and Cancel, and a
// line that says why a name cannot be added.
type keyAdder struct {
	button *widget.Button
	entry  *widget.Entry
	add    *widget.Button
	hint   *paragraph
	row    *fyne.Container
	taken  func(key string) string // why key cannot be added beside the keys there are; "" when it can
	onAdd  func(key string)
}

func newKeyAdder(width float32, taken func(string) string, onAdd func(string)) *keyAdder {
	a := &keyAdder{taken: taken, onAdd: onAdd}
	a.button = widget.NewButtonWithIcon("Add Key", theme.ContentAddIcon(), a.open)
	a.button.Importance = widget.LowImportance
	a.entry = widget.NewEntry()
	a.entry.SetPlaceHolder("New key, e.g. config.yaml")
	a.entry.OnChanged = func(string) { a.check() }
	a.entry.OnSubmitted = func(string) { a.submit() }
	a.add = widget.NewButtonWithIcon("Add", theme.ContentAddIcon(), a.submit)
	a.add.Importance = widget.HighImportance
	cancel := widget.NewButton("Cancel", a.close)
	a.hint = newParagraph("", colorMuted, width)
	// Room around the row (the owner's WF12, 2026-10-18: "nur zu gequetscht"): a gap between the
	// name and its buttons, and one under the row before the footer (dataGap above it).
	buttons := container.NewHBox(newHGap(theme.SizeNamePadding), a.add, cancel)
	a.row = container.NewVBox(container.NewBorder(nil, nil, nil, buttons, a.entry), a.hint, newSpace(theme.SizeNameInnerPadding))
	a.row.Hide()
	a.check()
	return a
}

// open shows the row for a new key's name.
func (a *keyAdder) open() {
	a.row.Show()
	a.button.Disable()
	a.check()
	if c := fyne.CurrentApp().Driver().CanvasForObject(a.entry); c != nil {
		c.Focus(a.entry)
	}
}

// close hides the row and forgets the name typed.
func (a *keyAdder) close() {
	a.entry.SetText("")
	a.row.Hide()
	a.button.Enable()
}

// reason says why the name typed cannot be added; "" when it can.
func (a *keyAdder) reason() string {
	key := a.entry.Text
	if key == "" {
		return "A key has letters, digits, '-', '_' and '.', at most 253 characters."
	}
	if err := engine.CheckDataKey(key); err != nil {
		return strings.ToUpper(err.Error()[:1]) + err.Error()[1:] + "."
	}
	return a.taken(key)
}

// check enables Add and says why not.
func (a *keyAdder) check() {
	why := a.reason()
	enable(a.add, why == "")
	c := colorMuted
	if why != "" && a.entry.Text != "" {
		c = colorStatusError
	}
	a.hint.text, a.hint.color = why, c
	setVisible(a.hint, why != "")
	a.hint.Refresh()
}

// submit adds the key named, when it can be.
func (a *keyAdder) submit() {
	if a.reason() != "" {
		return
	}
	key := a.entry.Text
	a.close()
	a.onAdd(key)
}

// setWidth wraps the hint at the details' width.
func (a *keyAdder) setWidth(w float32) {
	a.hint.width = w
	a.hint.Refresh()
}

// setEditable shows Add Key where the data can be saved; elsewhere the row closes.
func (a *keyAdder) setEditable(on bool) {
	setVisible(a.button, on)
	if !on && a.row.Visible() {
		a.close()
	}
}

// removeKeyButton is the remove button at the end of a key's line.
func removeKeyButton(sh *shell, key string, tapped func()) *tipButton {
	return newTipButton(sh, theme.DeleteIcon(), "Remove the key "+key+" when you save", tapped)
}

// removedKeyLines are a removed key's two lines in the drawer: its name in the error colour with
// Undo, and in its value's place what Save does.
func removedKeyLines(d *drawer, key string, undo func()) (fyne.CanvasObject, fyne.CanvasObject) {
	b := widget.NewButton("Undo", undo)
	b.Importance = widget.LowImportance
	note := newTextLine("Removed: Save removes this key and its value.", colorStatusError, theme.SizeNameText, fyne.TextStyle{Italic: true})
	return container.NewBorder(nil, nil, nil, b, keyNameIn(d, key, colorStatusError)), note
}

// keyNameIn is a data key in a colour, wrapped at the details' width as keyName wraps it.
func keyNameIn(d *drawer, key string, c fyne.ThemeColorName) fyne.CanvasObject {
	th := d.sh.app.Settings().Theme()
	if w := d.contentWidth(); fyne.MeasureText(key, th.Size(theme.SizeNameText), fyne.TextStyle{}).Width > w {
		return newNameParagraph(key, c, w)
	}
	return newTextLine(key, c, theme.SizeNameText, fyne.TextStyle{})
}

// addedKeyLine is an added key's line: its name, "new" (or, when the data holds the key by now,
// that it does), Hide where the value can be masked, a Secret's copy icon (copyIcon, nil for none),
// and the button that drops it.
func addedKeyLine(d *drawer, key string, exists bool, hide func(), copyIcon fyne.CanvasObject, drop func()) fyne.CanvasObject {
	tag := newTextLine("new", colorStatusOK, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
	if exists {
		tag = newTextLine("new, but the cluster holds this key now", colorStatusWarning, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
	}
	right := container.NewHBox()
	if hide != nil {
		h := widget.NewButton("Hide", hide)
		h.Importance = widget.LowImportance
		right.Add(h)
	}
	if copyIcon != nil {
		right.Add(copyIcon)
	}
	right.Add(newTipButton(d.sh, theme.DeleteIcon(), "Drop the new key "+key, drop))
	return container.NewBorder(nil, nil, nil, right, container.NewHBox(d.keyName(key), tag))
}

// editsText counts what a drawer's edit changes: "2 values", "1 value, 1 key added, 2 keys removed".
func editsText(values, added, removed int) string {
	var parts []string
	if values > 0 || added+removed == 0 {
		parts = append(parts, valuesText(values))
	}
	if added > 0 {
		parts = append(parts, fmt.Sprintf("%d key%s added", added, plural(added)))
	}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("%d key%s removed", removed, plural(removed)))
	}
	return strings.Join(parts, ", ")
}

// reviewHead is a save review's head, rows in the place order Context › Namespace › Object (the
// owner's rule, WF14 and WF16; conventions › Dialogs): then what the save changes (changes, ""
// before the review's check answered) and the keys it removes, in the error colour, and adds.
// Under a removal "This cannot be undone." in red; no sentence repeats the rows.
func reviewHead(kind string, ref model.ObjectRef, changes string, added, removed []string, width float32) fyne.CanvasObject {
	nameW, gap := kvLayout{}.dims()
	valueW := width - nameW - gap
	rows := box(kvLayout{})
	addRow := func(name string, value *paragraph) {
		rows.Objects = append(rows.Objects, newTextLine(name, colorMuted, theme.SizeNameText, fyne.TextStyle{}), value)
	}
	addRow("Context", newParagraph(ref.Context, theme.ColorNameForeground, valueW))
	if ref.Namespace != "" {
		addRow("Namespace", newParagraph(ref.Namespace, theme.ColorNameForeground, valueW))
	}
	addRow("Object", newParagraph(kind+" "+ref.Name, theme.ColorNameForeground, valueW))
	if changes != "" {
		addRow("Changes", newParagraph(changes, theme.ColorNameForeground, valueW))
	}
	if len(removed) > 0 {
		addRow("Removes", newParagraphStyled(strings.Join(sortedKeys(removed), ", "), colorStatusError, valueW, fyne.TextStyle{Bold: true}))
	}
	if len(added) > 0 {
		addRow("Adds", newParagraphStyled(strings.Join(sortedKeys(added), ", "), theme.ColorNameForeground, valueW, fyne.TextStyle{Bold: true}))
	}
	if len(removed) == 0 {
		return rows
	}
	return container.NewVBox(rows, newParagraph("This cannot be undone.", colorStatusError, width))
}

// newParagraphStyled is a paragraph in a text style.
func newParagraphStyled(text string, c fyne.ThemeColorName, width float32, style fyne.TextStyle) *paragraph {
	p := newParagraph(text, c, width)
	p.style = style
	return p
}

func sortedKeys(keys []string) []string { return slices.Sorted(slices.Values(keys)) }

// mapKeys are a map's keys, sorted.
func mapKeys(m map[string]string) []string { return slices.Sorted(maps.Keys(m)) }

// setKeysBlock puts a review's head (reviewHead) into its place; nothing when there is none.
func setKeysBlock(place *fyne.Container, block fyne.CanvasObject) {
	place.Objects = nil
	if block != nil {
		place.Objects = []fyne.CanvasObject{block, newSpace(theme.SizeNamePadding)}
	}
	setVisible(place, block != nil)
	place.Refresh()
}

// armSave enables a review's Save: at once, or, for a save that removes keys, once the
// confirmation's delay has passed since the review opened (a click meant for the drawer must not
// confirm a removal), if ready still holds then.
func armSave(sh *shell, b *widget.Button, destructive bool, opened time.Time, ready func() bool) {
	wait := time.Duration(0)
	if destructive && sh.confirmer != nil {
		wait = time.Until(opened.Add(sh.confirmer.armDelay))
	}
	if wait <= 0 {
		b.Enable()
		return
	}
	b.Disable()
	time.AfterFunc(wait, func() {
		sh.do(func() {
			if ready() {
				b.Enable()
			}
		})
	})
}

// confirmRemoval hands the engine's confirmation of a save that removes keys the user's answer in
// the review (helmrollback.go): the command on ref at uid, with the detail that names the keys. It
// returns the function that forgets the answer once the save ended; nil does nothing.
func confirmRemoval(sh *shell, command string, ref model.ObjectRef, uid, detail string, removes bool) func() {
	if !removes || sh.confirmer == nil {
		return func() {}
	}
	answer := sh.confirmer.confirmedByDialog(command, ref, uid, detail)
	return func() { sh.confirmer.dropDialogAnswer(answer) }
}

// resolvedRef is the object a review is of as the engine resolved it (with its context), else as
// the drawer names it.
func resolvedRef(engineRef, drawerRef model.ObjectRef) model.ObjectRef {
	if engineRef.Name == "" {
		return drawerRef
	}
	return engineRef
}

// target is the Config Map the review is of, as the engine resolved it: what a removal's
// confirmation binds.
func (rv *cmReview) target() model.ObjectRef {
	if rv.last == nil {
		return rv.cd.ref
	}
	return resolvedRef(rv.last.Ref, rv.cd.ref)
}

// target is the Secret the review is of, as the engine resolved it.
func (rv *secReview) target() model.ObjectRef {
	if rv.last == nil {
		return rv.sd.ref
	}
	return resolvedRef(rv.last.Ref, rv.sd.ref)
}

// dataGap is the room between a data section's last key and its Add Key row and footer (the
// owner's WF12, 2026-10-18: "ein bisschen zu dicht am letzten key-value pair").
func dataGap() fyne.CanvasObject { return newSpace(theme.SizeNameInnerPadding) }

// footerRow is a data section's footer: Add Key at its start, Revert and Save at its end.
func footerRow(adder *keyAdder, revert, save *widget.Button) *fyne.Container {
	return container.NewHBox(adder.button, layout.NewSpacer(), revert, save)
}
