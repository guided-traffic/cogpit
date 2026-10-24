package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A cluster's Alertmanager address (D-77; engine/alerts_settings.go, cluster.alertmanager and
// cluster.setAlertmanager): the service the alerts come from when Cogpit does not find it by name,
// as the Prometheus address is set (clustersettings_prometheus.go): "Alertmanager service address"
// (namespace/service:port, empty for detection by name), a line naming the Alertmanager in use,
// "Use HTTPS", and its path prefix for an address only. A text field is set after the typing pause,
// the check at once; each change sets the whole setting (the window's own, no confirmation; an
// agent's waits for the user, as A16's Prometheus). The fields follow the engine's setting, read
// when they are built and after every change of the cluster's alerts (EventAlerts,
// alertmanagerChanged), so an agent's change shows too.
//
// The fields stand on Alertmanager's own section of Cluster Settings under its switch (D-87,
// integrationDetails, clustersettings_integration.go); until 2026-10-18 under its row on
// Integrations.

func init() {
	integrationDetails[engine.IntegrationAlertmanager] = func(p *settingsPage) (fyne.CanvasObject, func()) {
		return alertmanagerAddress(p, p.shown.Context), nil
	}
}

// The settings' names on the page (their controls and refusal lines).
const (
	amAddressSetting = "alertmanagerAddress"
	amPrefixSetting  = "alertmanagerPrefix"
	amFieldsControl  = "alertmanagerFields"
)

// amFields are the Alertmanager's address fields of one context's page.
type amFields struct {
	widget.BaseWidget
	p       *settingsPage
	ctx     string
	address *fieldEntry // Escape closes the card (D-116, D-118)
	prefix  *fieldEntry
	https   *widget.Check
	line    *textLine
	enabled bool
	content fyne.CanvasObject
}

// alertmanagerSetParams are cluster.setAlertmanager's parameters, the whole setting at once.
type alertmanagerSetParams struct {
	Context string `json:"context,omitempty"`
	Address string `json:"address"`
	Prefix  string `json:"prefix,omitempty"`
	HTTPS   bool   `json:"https,omitempty"`
}

// alertmanagerAddress builds the Alertmanager's address fields of a context's page and reads the
// engine's setting into them.
func alertmanagerAddress(p *settingsPage, ctx string) fyne.CanvasObject {
	f := &amFields{p: p, ctx: ctx}
	f.address = newFieldEntry()
	f.address.SetPlaceHolder("Auto detect (namespace/service:port)")
	f.address.OnChanged = func(string) {
		f.follow()
		p.typed(amAddressSetting, f.set)
	}
	f.prefix = newFieldEntry()
	f.prefix.SetPlaceHolder("/ (none)")
	f.prefix.OnChanged = func(string) { p.typed(amPrefixSetting, f.set) }
	f.https = widget.NewCheck("Use HTTPS", func(bool) { f.set() })
	f.line = newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	f.line.truncate = true
	p.controls[amAddressSetting+"Entry"], p.controls[amPrefixSetting+"Entry"] = f.address, f.prefix
	p.controls["alertmanagerHTTPS"], p.controls["alertmanagerLine"] = f.https, f.line
	f.content = container.NewVBox(
		p.setting(amAddressSetting, "Service address",
			"Empty: found by its name among the cluster's services. Kept in Cogpit's settings, never the kubeconfig.",
			container.NewVBox(f.address, f.line, f.https)),
		p.setting(amPrefixSetting, "Path prefix", "The path the service serves under: its --web.route-prefix.", f.prefix),
	)
	f.ExtendBaseWidget(f)
	p.controls[amFieldsControl] = f
	f.enable(false)
	f.request()
	return f
}

// CreateRenderer implements fyne.Widget.
func (f *amFields) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(f.content) }

// enable turns the fields on or off; the path prefix needs a service address.
func (f *amFields) enable(on bool) {
	f.enabled = on
	for _, w := range []fyne.Disableable{f.address, f.https} {
		if on {
			w.Enable()
		} else {
			w.Disable()
		}
	}
	f.follow()
}

// follow enables the path prefix while a service address is typed.
func (f *amFields) follow() {
	if f.enabled && strings.TrimSpace(f.address.Text) != "" {
		f.prefix.Enable()
	} else {
		f.prefix.Disable()
	}
}

// current reports whether the fields still belong to the page shown.
func (f *amFields) current() bool {
	cur, _ := f.p.controls[amFieldsControl].(*amFields)
	return cur == f && f.p.shown.Context == f.ctx
}

// request reads the context's setting (cluster.alertmanager) into the fields; a context that is not
// connected, or the demo cluster, says why and keeps them off.
func (f *amFields) request() {
	switch {
	case engine.IsDemoContext(f.ctx):
		f.line.SetText("The demo cluster has no Alertmanager.")
		return
	case !live(f.p.sh.ui, f.ctx):
		f.line.SetText("Connect to this cluster to set its Alertmanager.")
		return
	}
	f.p.sh.dispatch("cluster.alertmanager", clusterSettingsContext{Context: f.ctx}, func(res any, err error) {
		if !f.current() {
			return
		}
		var set engine.AlertmanagerSetting
		switch {
		case err != nil:
			f.line.SetText(err.Error())
		case decode(res, &set) == nil:
			f.enable(true)
			f.show(set)
		}
	})
}

// set sets the whole setting as the fields show it.
func (f *amFields) set() {
	if f.p.syncing || !f.enabled || !f.current() {
		return
	}
	params := alertmanagerSetParams{Context: f.ctx, Address: autoSetting, HTTPS: f.https.Checked}
	if a := strings.TrimSpace(f.address.Text); a != "" {
		params.Address, params.Prefix = a, strings.TrimSpace(f.prefix.Text)
	}
	f.p.sh.dispatchInOrder("cluster.setAlertmanager", params, func(res any, err error) {
		if !f.current() {
			return
		}
		f.p.showResult(amAddressSetting, err)
		var set engine.AlertmanagerSetting
		if err == nil && decode(res, &set) == nil {
			f.show(set)
		}
	})
}

// show shows the engine's setting; a field the user is typing in keeps what is typed.
func (f *amFields) show(set engine.AlertmanagerSetting) {
	p := f.p
	p.syncing = true
	defer func() { p.syncing = false }()
	f.https.SetChecked(set.HTTPS)
	p.setEntry(f.address, &f.address.Entry, amAddressSetting, set.Address)
	p.setEntry(f.prefix, &f.prefix.Entry, amPrefixSetting, set.Prefix)
	f.follow()
	f.line.SetText(alertmanagerInUseLine(set))
}

// alertmanagerInUseLine names the Alertmanager in use under the address: found by name or set, and
// what Cogpit knows of it (its last poll, no request of its own).
func alertmanagerInUseLine(set engine.AlertmanagerSetting) string {
	how := "Auto detect"
	if !set.Auto {
		how = "Set"
	}
	switch set.State {
	case engine.AlertsFound, engine.AlertsConfigured:
		return how + ": in use " + set.InUse + "."
	case engine.AlertsNotFound:
		return "Auto detect: no Alertmanager found among the cluster's services."
	case engine.AlertsForbidden:
		return how + ": the current user may not read it (get services/proxy)."
	case engine.AlertsUnavailable:
		if set.InUse != "" {
			return how + ": " + set.InUse + " does not answer."
		}
		return how + ": " + set.Detail
	case "":
		return how + ": " + set.Detail // the integration is off
	}
	return how + ": looking for it while the cluster is shown."
}

// live reports whether a context is connected (shown or not, D-69), from the UI state: the fields
// need no page view to know it.
func live(u engine.UIState, contextID string) bool {
	if u.Context == contextID && u.Connection.State.Live() {
		return true
	}
	for _, c := range u.Connections {
		if c.Context == contextID {
			return c.Connection.State.Live()
		}
	}
	return false
}

// alertmanagerChanged follows a change of the shown cluster's alerts: the setting in use may be new.
func (p *settingsPage) alertmanagerChanged() {
	if f, ok := p.controls[amFieldsControl].(*amFields); ok && p.visible() && f.current() {
		f.request()
	}
}
