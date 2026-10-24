package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/metrics"
)

// Cluster Settings › Prometheus, the integration's own section (G12; D-87, the owner's WF22 of
// 2026-10-18: until then on Cluster Settings › Metrics, where Lens has them; engine/metricssettings.go,
// cluster.prometheus and cluster.setPrometheus). Lens's Metrics page shows, captured: a "Prometheus"
// dropdown (its query formats), a checkbox that filters empty containers out of the queries (on),
// the auto-detected provider and its path, a checkbox for HTTPS (off) and a custom path prefix.
// Cogpit shows them on the page as well since 2026-10-16 night (before, a dialog the page's button
// opened), under the integration's switch (integrationDetails, clustersettings_integration.go):
//
//   - Query format: Cogpit's presets (metrics.QueryFormats: where a node's series come from; Lens's
//     provider list is not reproduced, D-12) and "Filter empty containers";
//   - Service address: namespace/service:port, empty for detection by name (the lead's decision of
//     the night run, 2026-10-16/17: Lens's empty field rather than the dialog's "Auto detect"
//     check), a line naming the Prometheus in use, and "Use HTTPS";
//   - Path prefix: the service's route prefix, for a service address only.
//
// A select or check is set at once, a text field after the typing pause; each change sets the
// whole setting with cluster.setPrometheus (the window's own, no confirmation; an agent's waits
// for the user, A16) and the engine's refusal shows under the address. The fields follow the
// engine's setting (cluster.prometheus), read when the page shows and after every metrics event,
// so an agent's change shows too. The wording and the order are Cogpit's (inferred from the
// capture). Not tried in a real window.

// The settings' names on the page (their controls and refusal lines).
const (
	promFormatSetting  = "prometheusFormat"
	promAddressSetting = "prometheusAddress"
	promPrefixSetting  = "prometheusPrefix"
)

// autoSetting is cluster.setPrometheus's word for detection (address) and the detected query format.
const autoSetting = "auto"

func init() {
	integrationDetails[engine.IntegrationPrometheus] = func(p *settingsPage) (fyne.CanvasObject, func()) {
		return p.prometheusSettings()
	}
}

// promFields are the Prometheus's controls of the shown Prometheus section.
type promFields struct {
	format  *selectField
	filter  *widget.Check
	address *fieldEntry // Escape closes the card (D-116, D-118)
	prefix  *fieldEntry
	https   *widget.Check
	line    *textLine
	enabled bool
}

// prometheusSetParams are cluster.setPrometheus's parameters, the whole setting at once.
type prometheusSetParams struct {
	Context               string `json:"context,omitempty"`
	Address               string `json:"address"`
	Prefix                string `json:"prefix,omitempty"`
	Format                string `json:"format"`
	FilterEmptyContainers bool   `json:"filterEmptyContainers"`
	HTTPS                 bool   `json:"https"`
}

// prometheusSettings builds the Prometheus's settings of the shown cluster and returns them with
// the function that shows the state in them.
func (p *settingsPage) prometheusSettings() (fyne.CanvasObject, func()) {
	f := &promFields{}
	labels := make([]string, len(metrics.QueryFormats))
	for i, o := range metrics.QueryFormats {
		labels[i] = o.Label
	}
	set := func() { p.setPrometheus(f) }
	f.format = newSelectField(labels, func(string) { set() })
	f.filter = widget.NewCheck("Filter empty containers", func(bool) { set() })
	f.https = widget.NewCheck("Use HTTPS", func(bool) { set() })
	f.address = newFieldEntry()
	f.address.SetPlaceHolder("Auto detect (namespace/service:port)")
	f.address.OnChanged = func(string) {
		f.follow()
		p.typed(promAddressSetting, set)
	}
	f.prefix = newFieldEntry()
	f.prefix.SetPlaceHolder("/ (none)")
	f.prefix.OnChanged = func(string) { p.typed(promPrefixSetting, set) }
	f.line = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	f.line.truncate = true
	f.format.SetSelected(metrics.QueryFormats[0].Label) // Lens's defaults until the engine's setting shows (f.enabled is false: nothing is set)
	f.filter.SetChecked(true)
	p.prom = f
	p.controls[promFormatSetting+"Select"], p.controls["prometheusFilter"] = f.format, f.filter
	p.controls[promAddressSetting+"Entry"], p.controls[promPrefixSetting+"Entry"] = f.address, f.prefix
	p.controls["prometheusHTTPS"], p.controls["prometheusLine"] = f.https, f.line

	sync := func() {
		v := p.clusterView()
		switch {
		case engine.IsDemoContext(p.shown.Context):
			f.enable(false)
			f.line.SetText("The demo cluster's metrics are its own.")
		case !v.Connected:
			f.enable(false)
			f.line.SetText("Connect to this cluster to set its Prometheus.")
		default:
			f.enable(true)
			p.requestPrometheus()
		}
	}
	return container.NewVBox(
		p.setting(promFormatSetting, "Query format", "How this cluster's Prometheus names a node's series.",
			container.NewVBox(f.format, f.filter)),
		p.setting(promAddressSetting, "Service address",
			"Empty: found by its name among the cluster's services. Kept in Cogpit's settings, never the kubeconfig.",
			container.NewVBox(f.address, f.line, f.https)),
		p.setting(promPrefixSetting, "Path prefix",
			"The path the service serves under: its --web.route-prefix, or /select/<tenant>/prometheus for VictoriaMetrics' vmselect.", f.prefix),
	), sync
}

// enable turns the fields on or off; the path prefix needs a service address.
func (f *promFields) enable(on bool) {
	f.enabled = on
	for _, w := range []fyne.Disableable{f.format, f.filter, f.address, f.https} {
		if on {
			w.Enable()
		} else {
			w.Disable()
		}
	}
	f.follow()
}

// follow enables the path prefix while a service address is typed: detection finds its own.
func (f *promFields) follow() {
	if f.enabled && strings.TrimSpace(f.address.Text) != "" {
		f.prefix.Enable()
	} else {
		f.prefix.Disable()
	}
}

// setPrometheus sets the whole setting as the fields show it.
func (p *settingsPage) setPrometheus(f *promFields) {
	if p.syncing || !f.enabled {
		return
	}
	params := prometheusSetParams{Context: p.shown.Context, Address: autoSetting, Format: autoSetting,
		FilterEmptyContainers: f.filter.Checked, HTTPS: f.https.Checked}
	for _, o := range metrics.QueryFormats {
		if o.Label == f.format.Selected {
			params.Format = o.Name
		}
	}
	if a := strings.TrimSpace(f.address.Text); a != "" {
		params.Address, params.Prefix = a, strings.TrimSpace(f.prefix.Text)
	}
	ctx := p.shown.Context
	p.sh.dispatchInOrder("cluster.setPrometheus", params, func(res any, err error) {
		if p.prom != f || p.shown.Context != ctx {
			return
		}
		p.showResult(promAddressSetting, err)
		var set engine.PrometheusSetting
		if err == nil && decode(res, &set) == nil {
			p.showPrometheus(f, set)
		}
	})
}

// requestPrometheus reads the shown cluster's Prometheus setting (cluster.prometheus) into the fields.
func (p *settingsPage) requestPrometheus() {
	f, ctx := p.prom, p.shown.Context
	if f == nil {
		return
	}
	p.sh.dispatch("cluster.prometheus", clusterSettingsContext{Context: ctx}, func(res any, err error) {
		if p.prom != f || p.shown.Context != ctx {
			return
		}
		var set engine.PrometheusSetting
		switch {
		case err != nil:
			f.line.SetText(err.Error())
		case decode(res, &set) == nil:
			p.showPrometheus(f, set)
		}
	})
}

// showPrometheus shows the engine's setting in the fields; a field the user is typing in keeps
// what is typed.
func (p *settingsPage) showPrometheus(f *promFields, set engine.PrometheusSetting) {
	p.syncing = true
	defer func() { p.syncing = false }()
	label := metrics.QueryFormats[0].Label
	for _, o := range metrics.QueryFormats {
		if o.Name == set.Format {
			label = o.Label
		}
	}
	if f.format.Selected != label {
		f.format.SetSelected(label)
	}
	f.filter.SetChecked(set.FilterEmptyContainers || set.Format == "") // a setting read before 2026-10-16: Lens's default
	f.https.SetChecked(set.HTTPS)
	p.setEntry(f.address, &f.address.Entry, promAddressSetting, set.Address)
	p.setEntry(f.prefix, &f.prefix.Entry, promPrefixSetting, set.Prefix)
	f.follow()
	f.line.SetText(prometheusLine(set, p.sh.ui.IntegrationOn(p.shown.Context, engine.IntegrationPrometheus)))
}

// prometheusLine is the line under the address: the Prometheus in use, or that the integration is
// off, its setting kept for when it is on (D-87).
func prometheusLine(set engine.PrometheusSetting, on bool) string {
	if on {
		return prometheusInUseLine(set)
	}
	if set.Auto {
		return "Auto detect: the Prometheus integration is off for this cluster."
	}
	return "Set: the Prometheus integration is off for this cluster."
}

// prometheusInUseLine names the Prometheus in use under the address: found by name or set, and
// what Cogpit knows of it (the status line's state, no request of its own).
func prometheusInUseLine(set engine.PrometheusSetting) string {
	how := "Auto detect"
	if !set.Auto {
		how = "Set"
	}
	switch set.InUseStatus {
	case metrics.StatusAvailable:
		return how + ": in use " + set.InUse + "."
	case metrics.StatusUnavailable:
		return how + ": " + orService(set) + " does not answer."
	case metrics.StatusForbidden:
		return how + ": the current user may not read " + orService(set) + " (get services/proxy)."
	case metrics.StatusNotServed:
		if set.Auto {
			return "Auto detect: no Prometheus found among the cluster's services."
		}
		return "Set: " + set.Address + set.Prefix + " is not found or not a Prometheus."
	}
	if set.Auto {
		return "Auto detect: Cogpit looks for it among the cluster's services when a chart needs it."
	}
	return "Set: " + set.Address + set.Prefix + "; asked when a chart needs it."
}

// orService is the Prometheus in use, else the service set.
func orService(set engine.PrometheusSetting) string {
	if set.InUse != "" {
		return set.InUse
	}
	if set.Address != "" {
		return set.Address + set.Prefix
	}
	return "the Prometheus found"
}
