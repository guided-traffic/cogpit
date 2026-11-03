package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/layout"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Preferences › Kubernetes › Integrations of new clusters (D-76, D-82; engine/integrations.go): what
// the question at a new cluster's first connect turns on when the user answers "Use my default
// integrations", and what the question's "Save as the default for new clusters" sets. Nothing is looked for
// before the user answers the question's pre-choice (D-82), so the default no longer applies to an
// undecided cluster by itself. Every integration is on by default (the owner, 2026-10-17). Cogpit's
// own: Lens has no integrations; its place beside the other cluster-wide defaults of the Kubernetes
// section is the lead's decision (2026-10-17). The rows are Cluster Settings › Integrations' (the
// owner's QR3: the tile, the name, the switch, what it adds), without the status line: no cluster is
// meant. A tile is tinted while its switch is on, dim while it is off.

const (
	integrationDefaultsSetting = "integrationDefaults"
	integrationDefaultsTitle   = "Integrations of new clusters"
	integrationDefaultsHelp    = "What a new cluster's question turns on when you answer Use my default integrations; nothing is looked for before you answer. A cluster's own choice is in its Settings › Integrations."
)

// newClusterIntegrationsSetting returns the row and the function that shows the preference in it.
func (p *settingsPage) newClusterIntegrationsSetting() (fyne.CanvasObject, func()) {
	rows := make([]*integrationRow, 0, len(engine.Integrations))
	objs := make([]fyne.CanvasObject, 0, len(engine.Integrations))
	for _, in := range engine.Integrations {
		name := in.Name
		var r *integrationRow
		r = newIntegrationRow(in, func(on bool) {
			r.tile.setState(defaultTileState(on))
			p.setPreference(integrationDefaultsSetting, map[string]bool{string(name): on}) // nothing while syncing
		})
		r.withoutStatus()
		p.controls["integrationDefault:"+string(name)] = r.check
		rows = append(rows, r)
		objs = append(objs, r.box)
	}
	sync := func() {
		off := p.prefs().IntegrationsOff
		for i, in := range engine.Integrations {
			on := off&in.Set == 0
			rows[i].check.SetChecked(on)
			rows[i].tile.setState(defaultTileState(on))
		}
	}
	objs = append(objs, fixedHeight(0)) // the rows' 12 pt again before the help, which is no row's
	return p.setting(integrationDefaultsSetting, integrationDefaultsTitle, integrationDefaultsHelp, box(layout.NewCustomPaddedVBoxLayout(12), objs...)), sync
}

// defaultTileState is a default's tile: tinted while it is on, dim while off.
func defaultTileState(on bool) bearingState {
	if on {
		return bearingLive
	}
	return bearingOff
}
