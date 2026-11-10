package fyneui

import (
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// nameListSpec is a list of names a cluster setting keeps: its cluster.settings.set parameter (also
// its control and refusal line), the controls' names, the field's placeholder, the line shown while
// the list is empty, and the list as the UI state has it.
type nameListSpec struct {
	setting, entry, add, list string
	placeholder, none         string
	current                   func(engine.ClusterSettings) []string
}

// nameListEditor is a field that adds one name at a time on Enter or with Add, and the names kept,
// sorted, each with a button that removes it. The engine's refusal of a name shows under the field.
// Every change is cluster.settings.set with the whole list; the list follows
// UIState.ClusterSettings, so an agent's change shows at once.
func (p *settingsPage) nameListEditor(spec nameListSpec) fyne.CanvasObject {
	ctx := p.shown.Context
	entry := newFieldEntry() // Escape closes the card, a name not added yet dropped as by the × (D-116)
	entry.SetPlaceHolder(spec.placeholder)
	current := func() []string { return spec.current(p.sh.ui.ClusterSettings[ctx]) }
	set := func(next []string, done func()) {
		p.sh.dispatch("cluster.settings.set", map[string]any{contextParam: ctx, spec.setting: next}, func(_ any, err error) {
			p.showResult(spec.setting, err)
			if err == nil && done != nil {
				done()
			}
		})
	}
	addTyped := func() {
		name := strings.TrimSpace(entry.Text)
		if name == "" {
			return
		}
		if cur := current(); !slices.Contains(cur, name) {
			set(append(slices.Clone(cur), name), func() { entry.SetText("") })
			return
		}
		entry.SetText("")
		p.showResult(spec.setting, nil)
	}
	entry.OnSubmitted = func(string) { addTyped() }
	add := newPlateButton("Add", theme.ContentAddIcon(), addTyped)
	p.controls[spec.entry] = entry
	p.controls[spec.add] = add

	list := container.NewVBox()
	p.controls[spec.list] = list
	none := newTextLine(spec.none, colorMuted, theme.SizeNameText, fyne.TextStyle{})
	none.truncate = true
	p.sync = func() {
		names := current()
		for k := range p.controls {
			if strings.HasPrefix(k, "remove:") {
				delete(p.controls, k)
			}
		}
		objs := make([]fyne.CanvasObject, 0, len(names)+1)
		if len(names) == 0 {
			objs = append(objs, none)
		}
		for _, n := range names {
			kept := n
			remove := newTipButton(p.sh, theme.DeleteIcon(), "Remove "+kept, func() {
				set(slices.DeleteFunc(slices.Clone(current()), func(x string) bool { return x == kept }), nil)
			})
			p.controls["remove:"+kept] = remove
			name := newTextLine(kept, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Monospace: true})
			name.truncate = true
			bg := newSurface(theme.ColorNameInputBackground)
			bg.radius, bg.stroke = theme.SizeNameInputRadius, colorHairline
			objs = append(objs, container.NewStack(bg, container.NewBorder(nil, nil, nil, remove,
				inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, name))))
		}
		list.Objects = objs
		list.Refresh()
	}
	return container.NewVBox(container.NewBorder(nil, nil, nil, add, entry), list)
}
