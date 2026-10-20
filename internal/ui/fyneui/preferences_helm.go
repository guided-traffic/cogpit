package fyneui

import (
	"fyne.io/fyne/v2"
)

// Lens's HELM_DRIVER on the Preferences page (G11; engine/helmdriver.go): where a new Helm install
// keeps its release's records. Lens lists secret (the default), configmap and sql; Cogpit offers
// the first two (sql keeps releases in a database outside the cluster, which Cogpit does not read
// or write). The select sits in the Kubernetes section (Cogpit's place, inferred: where Lens shows
// it is not captured) and dispatches preferences.set like every other setting.

// helmDriverChoices are the select's choices: its label and the preference's value.
var helmDriverChoices = []struct{ label, value string }{
	{"Secrets (Helm's default)", ""},
	{"ConfigMaps", "configmap"},
}

// helmDriverSetting returns the HELM_DRIVER setting and the function that shows the preference in
// it.
func (p *settingsPage) helmDriverSetting() (fyne.CanvasObject, func()) {
	labels := make([]string, len(helmDriverChoices))
	for i, c := range helmDriverChoices {
		labels[i] = c.label
	}
	sel := newSelectField(labels, func(label string) {
		for _, c := range helmDriverChoices {
			if c.label == label {
				p.setPreference("helmDriver", c.value)
			}
		}
	})
	sync := func() {
		cur := p.prefs().HelmDriver
		for _, c := range helmDriverChoices {
			if c.value == cur {
				sel.SetSelected(c.label)
			}
		}
	}
	return p.setting("helmDriver", "Helm storage driver",
		"Where new Helm installs keep their release records (HELM_DRIVER); installed releases keep theirs. SQL is not supported.", sel), sync
}
