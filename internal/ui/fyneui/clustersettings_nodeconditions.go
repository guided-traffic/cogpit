package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// A cluster's Settings › Node Conditions (D-163; engine/nodeconditions.go): the node condition types
// that never warn on this cluster, added one at a time, and a line naming the built-in ones.

const (
	nodeConditionsSectionLabel = "Node Conditions"
	acceptedConditionsSetting  = "acceptedNodeConditions"
)

func init() { clusterSectionLabels[engine.SectionNodeConditions] = nodeConditionsSectionLabel }

// clusterNodeConditionsSection builds a cluster's Settings › Node Conditions.
func (p *settingsPage) clusterNodeConditionsSection() fyne.CanvasObject {
	builtin := newTextLine("Always accepted: "+strings.Join(kinds.BuiltinAcceptedNodeConditions(), ", ")+".",
		colorMuted, theme.SizeNameText, fyne.TextStyle{})
	builtin.truncate = true
	return container.NewVBox(
		p.setting(acceptedConditionsSetting, "Accepted conditions",
			"Condition types that never warn on this cluster, whatever their status: the Nodes list and the Overview's issues leave them out, a node's drawer still reports them. Spelled as the node reports them.",
			container.NewVBox(p.nameListEditor(nameListSpec{
				setting: acceptedConditionsSetting, entry: "acceptedConditionEntry", add: "acceptedConditionAdd", list: "acceptedConditionList",
				placeholder: "Add a condition type",
				none:        "None added.",
				current:     func(cs engine.ClusterSettings) []string { return cs.AcceptedNodeConditions },
			}), builtin)),
	)
}
