package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Cluster Settings › General › Light strip (D-new, the owner's VW14 of 2026-10-24;
// engine/lightstrip.go): under the accent colour, how the strip under the view's header shows it
// while one of the cluster's tabs is shown: Static, or one of the animations Breathing, Scanner and
// Flow. The animations stand in a frame of their own with an ⓘ whose tooltip says they cost CPU (the
// owner's LS15: the animations framed, a small info icon whose tooltip notes the higher CPU use;
// his words in D-new). While an animation is chosen but
// stands still, because motion is reduced or the computer runs on battery and Preferences › App
// asks for the power adapter, a warning line says why (the owner's LS13: "müsste in den Settings
// wenigstens eine Warnung angezeigt werden … Das mit er weiß wieso nichts passiert"). Disabled, with
// its reason beside it, while the cluster has no accent colour: the strip is frosted glass then. A
// choice dispatches cluster.settings.setLightStrip, which only the window may run. Cogpit's own:
// Lens has no light strip.

const (
	lightStripSetting = "lightStrip"
	lightStripTitle   = "Light strip"
	lightStripHelp    = "The strip under a view's header shows the accent colour of the cluster whose tab you look at. " +
		"It stands still while Cogpit is in the background."
	lightStripNoAccent  = "Set an accent colour first"
	lightStripAnimTitle = "Animations"
	lightStripCPU       = "An animated strip repaints the window up to 60 times a second while it moves and uses " +
		"noticeably more CPU: about a fifth of a core, measured on an Apple M1 Ultra. Static costs nothing."
	lightStripReduced = "Motion is reduced, so the strip shows its colour standing still " +
		"(Preferences › App › Reduce motion, or macOS's Accessibility › Display)."
	lightStripBattery = "The computer runs on battery, so the strip shows its colour standing still " +
		"(Preferences › App)."
)

// lightStripChoices are the setting's labels by animation, in the order offered; the first is
// Static, the others the animations in their frame.
var lightStripChoices = []struct{ label, value string }{
	{"Static", engine.LightStripStatic},
	{"Breathing", engine.LightStripBreathing},
	{"Scanner", engine.LightStripScanner},
	{"Flow", engine.LightStripFlow},
}

// lightStripStands is why a chosen animation stands still for a reason the user set (motion
// reduced, on battery), "" while it may move. The background is no such reason: the help says it.
func (sh *shell) lightStripStands() string {
	switch {
	case sh.reduceMotion():
		return lightStripReduced
	case sh.batteryStopsStrip():
		return lightStripBattery
	}
	return ""
}

// lightStripSetting builds the Light strip setting and the function that shows the cluster's
// choice in it, from the UI state's cluster settings, so that it follows a change at once.
func (p *settingsPage) lightStripSetting() (fyne.CanvasObject, func()) {
	ctx := p.shown.Context
	// Two radio groups, Static alone and the animations in their frame, act as one: a choice in one
	// clears the other.
	static := widget.NewRadioGroup([]string{lightStripChoices[0].label}, nil)
	anims := make([]string, 0, len(lightStripChoices)-1)
	for _, c := range lightStripChoices[1:] {
		anims = append(anims, c.label)
	}
	anim := widget.NewRadioGroup(anims, nil)
	for _, g := range []*widget.RadioGroup{static, anim} {
		g.Horizontal, g.Required = true, true
		g.OnChanged = func(label string) {
			if p.syncing || label == "" {
				return
			}
			for _, c := range lightStripChoices {
				if c.label == label {
					p.sh.dispatch("cluster.settings.setLightStrip", map[string]any{contextParam: ctx, "animation": c.value},
						p.settled(lightStripSetting))
				}
			}
		}
	}
	why := newTextLine(lightStripNoAccent, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{})
	stands := newParagraph("", colorStatusWarning, settingsHelpWidth)
	stands.size = theme.SizeNameCaptionText
	stands.Hide()
	p.controls["lightStripStatic"] = static
	p.controls["lightStripAnim"] = anim
	p.controls["lightStripWhy"] = why
	p.controls["lightStripStands"] = stands
	sync := func() {
		cs := p.sh.ui.ClusterSettings[ctx]
		chosen := engine.LightStripOf(cs)
		static.SetSelected("")
		anim.SetSelected("")
		for _, c := range lightStripChoices {
			if c.value != chosen {
				continue
			}
			if c.value == engine.LightStripStatic {
				static.SetSelected(c.label)
			} else {
				anim.SetSelected(c.label)
			}
		}
		reason := ""
		if cs.AccentColor == "" {
			static.Disable()
			anim.Disable()
			why.Show()
		} else {
			static.Enable()
			anim.Enable()
			why.Hide()
			if chosen != engine.LightStripStatic {
				reason = p.sh.lightStripStands()
			}
		}
		if stands.text != reason {
			stands.text = reason
			stands.Refresh()
		}
		setShown(stands, reason != "")
	}
	head := container.NewHBox(newTextLine(lightStripAnimTitle, colorMuted, theme.SizeNameCaptionText, fyne.TextStyle{}),
		newTipIcon(p.sh, theme.InfoIcon(), colorMuted, lightStripCPU))
	row := container.NewHBox(container.NewCenter(static), newHGap(theme.SizeNameInnerPadding),
		newStripFrame(container.NewVBox(head, anim)), newHGap(theme.SizeNameInnerPadding), container.NewCenter(why))
	return p.setting(lightStripSetting, lightStripTitle, lightStripHelp, container.NewVBox(row, stands)), sync
}

// newStripFrame draws a thin rounded frame around the animations, the input fields' outline and
// radius, with the inner padding inside.
func newStripFrame(content fyne.CanvasObject) fyne.CanvasObject {
	frame := newSurface("")
	frame.radius, frame.stroke = theme.SizeNameInputRadius, theme.ColorNameInputBorder
	return container.NewStack(frame, inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, content))
}

// systemMotionChanged follows a change the computer made, macOS's Reduce motion or the power
// source: the strip, and the Light strip row's warning while Cluster Settings shows it.
func (sh *shell) systemMotionChanged() {
	sh.strip.follow()
	if sh.settings != nil {
		sh.settings.syncControls()
	}
}
