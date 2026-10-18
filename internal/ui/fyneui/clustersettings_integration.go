package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// An integration's own section of a cluster's Settings (the owner's WF22 of 2026-10-18, D-87): the
// page's list has, under a divider below Agents, the heading Integrations (its own section,
// clustersettings_integrations.go) and one section per integration: Metrics Server, Prometheus,
// Alertmanager, Trivy Operator, Kyverno, each named as the integration is (`metrics-server`,
// `prometheus`, …; engine.IntegrationSection). A section shows, under the integration's name, its
// switch ("On for this cluster"), its status line in the status colours (where a found Prometheus
// or Alertmanager runs as links to its Namespace and Service, which close the page and open them),
// the line of what it adds, and its details, which its own file adds through integrationDetails:
// Prometheus's query format, address, HTTPS and path prefix (clustersettings_prometheus.go, until
// 2026-10-18 on Metrics), Alertmanager's address (clustersettings_alertmanager.go, until then under
// its row on Integrations). While the cluster has no choice of integrations (D-82) the section says
// so above the switch. The details of an integration switched off stay editable: setting them sends
// nothing (the address is kept in Cogpit's settings), so an address set first is the only service
// Cogpit asks once the integration is switched on (the lead's decision, D-87).

// integrationSwitchLabel is the switch on an integration's own section: its name is the heading.
const integrationSwitchLabel = "On for this cluster"

// integrationNavLabels are the integrations' names in the page's list, short enough for it.
var integrationNavLabels = map[engine.Integration]string{
	engine.IntegrationMetricsServer: "Metrics Server",
	engine.IntegrationPrometheus:    "Prometheus",
	engine.IntegrationAlertmanager:  "Alertmanager",
	engine.IntegrationTrivy:         "Trivy Operator",
	engine.IntegrationKyverno:       "Kyverno",
}

func init() {
	for _, in := range engine.Integrations {
		label := integrationNavLabels[in.Name]
		if label == "" {
			label = in.Label
		}
		clusterSectionLabels[engine.IntegrationSection(in.Name)] = label
	}
}

// integrationDetails are what an integration's own file shows under its switch on its section, for
// the shown context, with the function that shows the state in them (nil when they follow it by
// themselves); filled in an init of that file. None: the switch and the status alone.
var integrationDetails = map[engine.Integration]func(p *settingsPage) (fyne.CanvasObject, func()){}

// clusterIntegrationSection builds an integration's own section of a cluster's Settings.
func (p *settingsPage) clusterIntegrationSection(in engine.IntegrationInfo) fyne.CanvasObject {
	ctx := p.shown.Context
	r := newIntegrationRow(in, func(on bool) {
		p.setCluster(integrationsSetting, map[string]bool{string(in.Name): on}) // nothing while syncing
	})
	r.check.Text = integrationSwitchLabel
	r.check.Refresh()
	r.placeLinks = func(text, where string) []lineSeg { return p.sh.serviceSegments(text, where, ctx) }
	p.integrationRows = []*integrationRow{r}
	p.controls["integration:"+string(in.Name)] = r.box
	note := p.integrationsNote()
	objs := []fyne.CanvasObject{note, p.setting(integrationsSetting, in.Label, "", r.box)}
	var detailsSync func()
	if details := integrationDetails[in.Name]; details != nil {
		var o fyne.CanvasObject
		o, detailsSync = details(p)
		objs = append(objs, o)
	}
	p.sync = func() {
		r.check.SetChecked(p.sh.ui.IntegrationOn(ctx, in.Name)) // the UI state's, so the switch follows at once
		if st, ok := p.integrationStatuses()[in.Name]; ok {
			r.setStatus(st)
		}
		p.syncIntegrationsNote(note)
		if detailsSync != nil {
			detailsSync()
		}
	}
	return container.NewVBox(objs...)
}
