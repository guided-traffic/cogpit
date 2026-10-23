package fyneui

import (
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
	button *textButton
	entry  *widget.Entry
	add    *widget.Button
	hint   *paragraph
	row    *fyne.Container
	taken  func(key string) string // why key cannot be added beside the keys there are; "" when it can
	onAdd  func(key string)
}

func newKeyAdder(width float32, taken func(string) string, onAdd func(string)) *keyAdder {
	a := &keyAdder{taken: taken, onAdd: onAdd}
	a.button = newTextButton("Add Key", theme.ContentAddIcon(), a.open)
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
	b := newTextButton("Undo", nil, undo)
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
// that it does), a Secret's Hide or Show (toggle) and copy icon (nil for none), and the button that
// drops it.
func addedKeyLine(d *drawer, key string, exists bool, toggle *textButton, copyIcon fyne.CanvasObject, drop func()) fyne.CanvasObject {
	tag := newTextLine("new", colorStatusOK, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
	if exists {
		tag = newTextLine("new, but the cluster holds this key now", colorStatusWarning, theme.SizeNameCaptionText, fyne.TextStyle{Bold: true})
	}
	right := container.NewHBox()
	if toggle != nil {
		right.Add(toggle)
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
// owner's rule, WF14 and WF16; conventions › Dialogs), the object as every dialog names one ("Config
// Map → walk-cm", WF32, objectnames.go): then what the save changes (changes, "" before the
// review's check answered) and the keys it removes, in the error colour, and adds. Under a removal
// "This cannot be undone." in red; no sentence repeats the rows. The Edit tab's Apply names a
// removed key in the same row (engine.DataKeysEffects, WF33).
func reviewHead(kind string, ref model.ObjectRef, changes string, added, removed []string, width float32) fyne.CanvasObject {
	nameW, gap := kvLayout{}.dims()
	valueW := width - nameW - gap
	rows := box(kvLayout{})
	addRow := func(name string, value *paragraph) {
		rows.Objects = append(rows.Objects, newTextLine(name, colorMuted, theme.SizeNameText, fyne.TextStyle{}), value)
	}
	var namespaces []string
	if ref.Namespace != "" {
		namespaces = []string{ref.Namespace}
	}
	placeRows(rows, ref.Context, namespaces, []namedObject{{kind: kind, name: ref.Name}}, valueW)
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

// editCounts is a data section's edited state in its footer, beside Revert and Save (the owner's
// WF31, 2026-10-19: "Die Statuszeile ist absolut übersehbar"; it was "Edited: 1 value." in small
// muted text under the footer): a tinted pill per count, coloured as a diff marks its lines, the
// values edited in the warning colour, the keys added in the green of their "new" tag, the keys
// removed in the red of their struck name. Nothing shows while nothing is edited (D-100).
type editCounts struct {
	box                    *fyne.Container
	values, added, removed *pill
	footer                 *fyne.Container // the footer they are in (footerRow)
	shown                  [3]int          // the counts shown: values, added, removed
}

func newEditCounts() *editCounts {
	e := &editCounts{
		values:  newPill("", colorStatusWarning, true),
		added:   newPill("", colorStatusOK, true),
		removed: newPill("", colorStatusError, true),
	}
	for _, p := range []*pill{e.values, e.added, e.removed} {
		p.bold, p.size = true, theme.SizeNameText // as large as the buttons' labels beside them
		p.Hide()
	}
	e.box = container.NewHBox(e.values, e.added, e.removed)
	e.box.Hide()
	return e
}

// set shows the counts of values edited, keys added and keys removed; all zero hides them. The same
// counts again (each keystroke asks) cost nothing.
func (e *editCounts) set(values, added, removed int) {
	n := [3]int{values, added, removed}
	if n == e.shown {
		return
	}
	e.shown = n
	show := func(p *pill, n int, text string) {
		if n > 0 {
			p.Set(text, p.fg)
		}
		setVisible(p, n > 0)
	}
	show(e.values, values, valuesText(values)+" edited")
	show(e.added, added, fmt.Sprintf("%d key%s added", added, plural(added)))
	show(e.removed, removed, fmt.Sprintf("%d key%s removed", removed, plural(removed)))
	setVisible(e.box, values+added+removed > 0)
	e.box.Refresh()
	if e.footer != nil {
		e.footer.Refresh() // the pills' width moves Revert and Save's neighbours, or takes a line
	}
}

// text is what the pills shown say, joined by " · ": "1 value edited · 1 key removed".
func (e *editCounts) text() string {
	var parts []string
	for _, p := range []*pill{e.values, e.added, e.removed} {
		if p.Visible() {
			parts = append(parts, p.text)
		}
	}
	return strings.Join(parts, " · ")
}

// footerRow is a data section's footer: Add Key at its start, the edited state's pills and Revert
// and Save at its end (dataFooterLayout); width is the details' width it is told, as a paragraph
// is (setFooterWidth).
func footerRow(adder *keyAdder, counts *editCounts, revert, save *widget.Button, width float32) *fyne.Container {
	counts.footer = box(&dataFooterLayout{width: width}, adder.button, counts.box, revert, save)
	return counts.footer
}

// setFooterWidth tells a footer the details' width it lays out in.
func setFooterWidth(footer *fyne.Container, width float32) {
	if l, ok := footer.Layout.(*dataFooterLayout); ok && l.width != width {
		l.width = width
		footer.Refresh()
	}
}

// dataFooterLayout lays out a data section's footer, its objects Add Key, the edited state's pills,
// Revert and Save, a hidden one taking no room: Add Key at the start, the rest at the end, all on
// one line, centred on it. Where the details' width (told: a minimum size cannot depend on the
// size it gets) has no room for that, the pills take a line of their own above the buttons, at its
// end, so that the drawer never grows wider than the user made it.
type dataFooterLayout struct{ width float32 }

func (l *dataFooterLayout) gap() float32 {
	return fyne.CurrentApp().Settings().Theme().Size(theme.SizeNamePadding)
}

// widths are the visible objects' minimum widths at the start (Add Key), in the pills and at the
// end (Revert and Save with the gap between them), and the line's height.
func (l *dataFooterLayout) widths(objs []fyne.CanvasObject) (start, pills, end, pillsH, rowH float32) {
	gap := l.gap()
	if o := objs[0]; o.Visible() {
		start, rowH = o.MinSize().Width, o.MinSize().Height
	}
	if o := objs[1]; o.Visible() {
		pills, pillsH = o.MinSize().Width, o.MinSize().Height
	}
	for _, o := range objs[2:] {
		if !o.Visible() {
			continue
		}
		if end > 0 {
			end += gap
		}
		end += o.MinSize().Width
		rowH = max(rowH, o.MinSize().Height)
	}
	return start, pills, end, pillsH, rowH
}

// oneLine reports whether everything fits on one line of width w.
func (l *dataFooterLayout) oneLine(objs []fyne.CanvasObject, w float32) bool {
	start, pills, end, _, _ := l.widths(objs)
	return pills == 0 || w <= 0 || start+pills+end+2*l.gap() <= w
}

func (l *dataFooterLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	if len(objs) < 4 {
		return fyne.Size{}
	}
	start, pills, end, pillsH, rowH := l.widths(objs)
	gap := l.gap()
	if l.oneLine(objs, l.width) {
		w := start + end + gap
		if pills > 0 {
			w += pills + gap
		}
		return fyne.NewSize(w, max(rowH, pillsH))
	}
	return fyne.NewSize(max(start+gap+end, pills), pillsH+gap+rowH)
}

func (l *dataFooterLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) < 4 {
		return
	}
	_, pills, _, pillsH, rowH := l.widths(objs)
	gap := l.gap()
	w := l.width
	if w <= 0 || w > size.Width {
		w = size.Width
	}
	one := l.oneLine(objs, w)
	lineH, rowY := max(rowH, pillsH), float32(0)
	if !one {
		lineH, rowY = rowH, pillsH+gap
	}
	place := func(o fyne.CanvasObject, x, y, h float32) {
		ms := o.MinSize()
		o.Move(fyne.NewPos(x, y+(h-ms.Height)/2))
		o.Resize(ms)
	}
	if objs[0].Visible() {
		place(objs[0], 0, rowY, lineH)
	}
	x := size.Width
	for i := 3; i >= 2; i-- {
		if o := objs[i]; o.Visible() {
			x -= o.MinSize().Width
			place(o, x, rowY, lineH)
			x -= gap
		}
	}
	if objs[1].Visible() {
		if one {
			place(objs[1], x-pills, rowY, lineH)
		} else {
			place(objs[1], size.Width-pills, 0, pillsH)
		}
	}
}
