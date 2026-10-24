package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Cluster Settings › General › Light strip (D-new, the owner's VW14 of 2026-10-24;
// engine/lightstrip.go): under the accent colour, how the strip under the tabs shows it while one
// of the cluster's tabs is shown: Static, Breathing, Scanner or Flow. Disabled, with its reason beside
// it, while the cluster has no accent colour: the strip is frosted glass then. A choice dispatches
// cluster.settings.setLightStrip, which only the window may run. Cogpit's own: Lens has no light
// strip.

const (
	lightStripSetting = "lightStrip"
	lightStripTitle   = "Light strip"
	lightStripHelp    = "The strip under the tabs shows the accent colour of the cluster whose tab you look at. " +
		"It stands still while Cogpit is in the background or motion is reduced."
	lightStripNoAccent = "Set an accent colour first"
)

// lightStripChoices are the setting's labels by animation, in the order offered.
var lightStripChoices = []struct{ label, value string }{
	{"Static", engine.LightStripStatic},
	{"Breathing", engine.LightStripBreathing},
	{"Scanner", engine.LightStripScanner},
	{"Flow", engine.LightStripFlow},
}

// lightStripSetting builds the Light strip setting and the function that shows the cluster's
// choice in it, from the UI state's cluster settings, so that it follows a change at once.
func (p *settingsPage) lightStripSetting() (fyne.CanvasObject, func()) {
	ctx := p.shown.Context
	labels := make([]string, len(lightStripChoices))
	for i, c := range lightStripChoices {
		labels[i] = c.label
	}
	choice := widget.NewRadioGroup(labels, nil)
	choice.Horizontal, choice.Required = true, true
	choice.OnChanged = func(label string) {
		if p.syncing {
			return
		}
		for _, c := range lightStripChoices {
			if c.label == label {
				p.sh.dispatch("cluster.settings.setLightStrip", map[string]any{contextParam: ctx, "animation": c.value},
					p.settled(lightStripSetting))
			}
		}
	}
	why := newTextLine(lightStripNoAccent, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	p.controls["lightStripChoice"] = choice
	p.controls["lightStripWhy"] = why
	sync := func() {
		cs := p.sh.ui.ClusterSettings[ctx]
		anim := engine.LightStripOf(cs)
		for _, c := range lightStripChoices {
			if c.value == anim {
				choice.SetSelected(c.label)
			}
		}
		if cs.AccentColor == "" {
			choice.Disable()
			why.Show()
		} else {
			choice.Enable()
			why.Hide()
		}
	}
	// The reason on the choice's line, beside it, so that it does not read as the help's first line.
	row := container.NewHBox(choice, newHGap(theme.SizeNameInnerPadding), container.NewCenter(why))
	return p.setting(lightStripSetting, lightStripTitle, lightStripHelp, row), sync
}
