package fyneui

import (
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// A cluster's Settings › Namespaces (G3, G13; engine/accessiblens.go): Lens's "Accessible
// namespaces" for a user who may not list the cluster's namespaces, captured as a field that adds
// one namespace at a time. Cogpit's (inferred): the field adds the name typed on Enter or with
// Add, the namespaces kept are listed under it, sorted, each with a button that removes it, and
// the engine's refusal of a name (not a namespace's, DNS-1123) shows under the field. Every change
// is cluster.settings.set with the whole list; the list follows UIState.ClusterSettings, so an
// agent's change shows at once.

// namespacesSectionLabel is the section's title (Lens's page list, captured).
const namespacesSectionLabel = "Namespaces"

// accessibleSetting is the setting's name: cluster.settings.set's parameter, its control and its
// refusal line.
const accessibleSetting = "accessibleNamespaces"

// clusterNamespacesSection builds a cluster's Settings › Namespaces.
func (p *settingsPage) clusterNamespacesSection() fyne.CanvasObject {
	ctx := p.shown.Context
	entry := newFieldEntry() // Escape closes the card, a name not added yet dropped as by the × (D-116)
	entry.SetPlaceHolder("Add a namespace, e.g. team-a")
	current := func() []string { return p.sh.ui.ClusterSettings[ctx].AccessibleNamespaces }
	set := func(next []string, done func()) {
		p.sh.dispatch("cluster.settings.set", map[string]any{contextParam: ctx, accessibleSetting: next}, func(_ any, err error) {
			p.showResult(accessibleSetting, err)
			if err == nil && done != nil {
				done()
			}
		})
	}
	addTyped := func() {
		ns := strings.TrimSpace(entry.Text)
		if ns == "" {
			return
		}
		if cur := current(); !slices.Contains(cur, ns) {
			set(append(slices.Clone(cur), ns), func() { entry.SetText("") })
			return
		}
		entry.SetText("")
		p.showResult(accessibleSetting, nil)
	}
	entry.OnSubmitted = func(string) { addTyped() }
	add := newPlateButton("Add", theme.ContentAddIcon(), addTyped)
	p.controls["accessibleEntry"] = entry
	p.controls["accessibleAdd"] = add

	list := container.NewVBox()
	p.controls["accessibleList"] = list
	none := newTextLine("None named: when Namespaces cannot be listed, Cogpit reads the namespace filter's namespaces, else the context's.",
		colorMuted, theme.SizeNameText, fyne.TextStyle{})
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
			ns := n
			remove := newTipButton(p.sh, theme.DeleteIcon(), "Remove "+ns, func() {
				set(slices.DeleteFunc(slices.Clone(current()), func(x string) bool { return x == ns }), nil)
			})
			p.controls["remove:"+ns] = remove
			name := newTextLine(ns, theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Monospace: true})
			name.truncate = true
			bg := newSurface(theme.ColorNameInputBackground)
			bg.radius, bg.stroke = theme.SizeNameInputRadius, colorHairline
			objs = append(objs, container.NewStack(bg, container.NewBorder(nil, nil, nil, remove,
				inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, name))))
		}
		list.Objects = objs
		list.Refresh()
	}
	return container.NewVBox(
		p.setting(accessibleSetting, "Accessible namespaces",
			"For a user who may not list namespaces: the filter offers these, and a kind not listable everywhere is read in each.",
			container.NewVBox(container.NewBorder(nil, nil, nil, add, entry), list)),
	)
}
