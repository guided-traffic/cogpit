package fyneui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// The one form in which every dialog names a Kubernetes object (the owner's WF32, 2026-10-19:
// "ich möchte dass Config Map noch klarer vom Namen getrennt wird: "Config Map -> walk-cm" oder
// etwas ähnliches"; D-85, D-100): the kind in the muted colour with an arrow, the name in bold,
// "Config Map → walk-cm". Several objects are grouped by kind: the kind stands once, on its group's
// first line, and the names stand under each other in their own column. A dialog's title has the
// same form without the colours, its arrow ending a text of its own (objectDialogTitle,
// dialogframe.go: one text with a space after the arrow draws an em-wide gap, D-104); other plain
// text has objectText. A dialog that names objects (a confirmation, a save review, Rollback, a Helm
// install, Remove finalizers to come) builds its Object(s) row with objectsRow.

// objectArrow sets a kind apart from its name.
const objectArrow = "→"

// objectText is an object's kind and name in plain text: "Config Map → walk-cm". Not for a
// dialog's title, which is objectDialogTitle (D-104).
func objectText(kind, name string) string { return kind + " " + objectArrow + " " + name }

// namedObject is one object a dialog names: its kind as the user reads it ("Config Map", "Pod",
// "Release") and its name.
type namedObject struct{ kind, name string }

// objectNames lays out objects in two columns, kinds muted with the arrow and names in bold,
// grouped by kind in the order the kinds first appear, a kind on its group's first line only.
// width is the room for both columns; a name wider than its column wraps after - . _ / (wrapName).
func objectNames(objs []namedObject, width float32) *fyne.Container {
	var order []string
	groups := map[string][]string{}
	for _, o := range objs {
		if _, ok := groups[o.kind]; !ok {
			order = append(order, o.kind)
		}
		groups[o.kind] = append(groups[o.kind], o.name)
	}
	th := fyne.CurrentApp().Settings().Theme()
	size := th.Size(theme.SizeNameText)
	gap := th.Size(theme.SizeNameInnerPadding)
	var kindW float32
	for _, k := range order {
		kindW = max(kindW, fyne.MeasureText(kindLabel(k), size, fyne.TextStyle{}).Width)
	}
	nameW := max(width-kindW-gap, width/3) // a very long kind still leaves the names room
	out := box(objectNamesLayout{kindW: kindW, gap: gap})
	for _, k := range order {
		for i, n := range groups[k] {
			label := ""
			if i == 0 {
				label = kindLabel(k)
			}
			name := newNameParagraph(n, theme.ColorNameForeground, nameW)
			name.style = fyne.TextStyle{Bold: true}
			out.Objects = append(out.Objects, newTextLine(label, colorMuted, theme.SizeNameText, fyne.TextStyle{}), name)
		}
	}
	return out
}

// kindLabel is a kind as the first column shows it: "Config Map →"; a kind the dialog does not
// know stays empty, the name alone.
func kindLabel(kind string) string {
	if kind == "" {
		return ""
	}
	return kind + " " + objectArrow
}

// objectNamesLayout places objectNames' cells: objects alternate kind, name; the kinds in a column
// kindW wide, the names gap to their right, each row as tall as its taller cell.
type objectNamesLayout struct {
	kindW, gap float32
}

func (l objectNamesLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for i := 0; i+1 < len(objs); i += 2 {
		k, n := objs[i].MinSize(), objs[i+1].MinSize()
		w = max(w, l.kindW+l.gap+n.Width)
		h += max(k.Height, n.Height)
	}
	return fyne.NewSize(w, h)
}

func (l objectNamesLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	y := float32(0)
	for i := 0; i+1 < len(objs); i += 2 {
		k, n := objs[i].MinSize(), objs[i+1].MinSize()
		objs[i].Move(fyne.NewPos(0, y))
		objs[i].Resize(fyne.NewSize(l.kindW, k.Height))
		objs[i+1].Move(fyne.NewPos(l.kindW+l.gap, y))
		objs[i+1].Resize(fyne.NewSize(max(size.Width-l.kindW-l.gap, 0), n.Height))
		y += max(k.Height, n.Height)
	}
}

// objectListRows is how many objects a dialog's Object(s) row shows before it scrolls: Delete of a
// selection names every object, up to engine.MaxSelection of them, and the buttons must stay in
// the window.
const objectListRows = 8

// objectsRow is a dialog's Object(s) row in a kvLayout table: its name, "Object" or "Objects (12)",
// and the objects in the one form (objectNames), in a scrolling box no taller than objectListRows
// lines when there are more.
func objectsRow(objs []namedObject, width float32) (name, value fyne.CanvasObject) {
	label := "Object"
	if len(objs) > 1 {
		label = fmt.Sprintf("Objects (%d)", len(objs))
	}
	name = newTextLine(label, colorMuted, theme.SizeNameText, fyne.TextStyle{})
	list := objectNames(objs, width)
	if len(objs) <= objectListRows {
		return name, list
	}
	th := fyne.CurrentApp().Settings().Theme()
	line := fyne.MeasureText("Mg", th.Size(theme.SizeNameText), fyne.TextStyle{}).Height + th.Size(theme.SizeNameLineSpacing)
	scroll := newVScroll(list)
	scroll.SetMinSize(fyne.NewSize(width, line*objectListRows))
	// 8 pt below it: Fyne's scroll shadow at its foot would otherwise crowd the next row.
	return name, container.New(layout.NewCustomPaddedLayout(0, 8, 0, 0), scroll)
}

// placeRows adds a dialog's place to a kvLayout table in the owner's order Context › Namespace ›
// Object(s) (WF16, D-85): the context (contextValue), the namespace or namespaces where there are
// any, and the objects in the one form (objectsRow) where there are any. valueW is the values'
// column.
func placeRows(rows *fyne.Container, context string, namespaces []string, objs []namedObject, valueW float32) {
	add := func(name string, value *paragraph) {
		rows.Objects = append(rows.Objects, newTextLine(name, colorMuted, theme.SizeNameText, fyne.TextStyle{}), value)
	}
	plain := func(value string) *paragraph {
		v := newParagraph(value, theme.ColorNameForeground, valueW)
		v.hard = true
		return v
	}
	add("Context", contextValue(context, valueW))
	switch {
	case len(namespaces) == 1:
		add("Namespace", plain(namespaces[0]))
	case len(namespaces) > 1: // a selection's objects (Delete of a selection)
		add("Namespaces", plain(strings.Join(namespaces, ", ")))
	}
	if len(objs) > 0 {
		name, value := objectsRow(objs, valueW)
		rows.Objects = append(rows.Objects, name, value)
	}
}

// contextValue is a dialog's Context row value: the context's id as it is, "kind-cogpit
// [kubernetes_configs/walk-kind.yaml]", the context in the text colour and the source label that
// tells two files' contexts of one name apart (D-28, engine.SourceContextID) in the kind word's
// grey (the owner's alpha walk, AW7, 2026-10-22: "Dann lass es gerne in dem Format wie es ist, aber
// grau wäre top"), wrapped as before: on the context's line where it fits. An id without a label
// (the demo) is the context alone.
func contextValue(id string, width float32) *paragraph {
	v := newParagraph(id, theme.ColorNameForeground, width)
	v.hard = true
	if name, label := splitContextID(id); label != "" {
		v.mutedFrom = len(name) + 1 // "[" on: the label in its brackets
	}
	return v
}

// rowName is a table row's name in the muted colour: a line, or, wider than the name column
// (sizeDrawerLabel, kvLayout's), a paragraph wrapped to it, so that it never runs into its value.
func rowName(name string) fyne.CanvasObject {
	th := fyne.CurrentApp().Settings().Theme()
	w := th.Size(sizeDrawerLabel)
	if fyne.MeasureText(name, th.Size(theme.SizeNameText), fyne.TextStyle{}).Width <= w {
		return newTextLine(name, colorMuted, theme.SizeNameText, fyne.TextStyle{})
	}
	return newParagraph(name, colorMuted, w)
}

// effectRows adds a confirmation's effects to a kvLayout table, a row each: its name, its value
// (effectValue).
func effectRows(rows *fyne.Container, effects []engine.Effect, valueW float32) {
	for _, e := range effects {
		rows.Objects = append(rows.Objects, rowName(e.Name), effectValue(e, valueW))
	}
}

// effectValue is one of a confirmation's effects as a row's value (D-85, WF33: a quick view, never a
// sentence): names in bold, what is lost in bold and the error colour (as the save review's
// "Removes"), what to weigh in the warning colour. A word wider than the column (a digest) breaks
// anywhere.
func effectValue(e engine.Effect, width float32) *paragraph {
	c, style := theme.ColorNameForeground, fyne.TextStyle{}
	switch e.Tone {
	case engine.EffectNames:
		style.Bold = true
	case engine.EffectLost:
		c, style.Bold = colorStatusError, true
	case engine.EffectWarning:
		c = colorStatusWarning
	}
	v := newParagraphStyled(e.Value, c, width, style)
	v.hard = true
	return v
}
