package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"github.com/guided-traffic/cogpit/internal/engine"
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
	return container.NewVBox(
		p.setting(accessibleSetting, "Accessible namespaces",
			"For a user who may not list namespaces: the filter offers these, and a kind not listable everywhere is read in each.",
			p.nameListEditor(nameListSpec{
				setting: accessibleSetting, entry: "accessibleEntry", add: "accessibleAdd", list: "accessibleList",
				placeholder: "Add a namespace, e.g. team-a",
				none:        "None named: when Namespaces cannot be listed, Cogpit reads the namespace filter's namespaces, else the context's.",
				current:     func(cs engine.ClusterSettings) []string { return cs.AccessibleNamespaces },
			})),
	)
}
