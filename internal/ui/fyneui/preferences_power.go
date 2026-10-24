package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// Preferences › App › Light strip on battery (the owner's LS15 and LS16, 2026-10-24: "nur bei
// Netzbetrieb ... bei Akku auf statisch zurückfällt", "Einmal für die App", beside Reduce motion;
// engine.Preferences.LightStripMainsOnly). Off by default: the light strip moves as its cluster's
// setting says on battery too. On, it shows its colour standing still while the computer runs on
// its battery and moves again once the power adapter is plugged in: an animation repaints the
// whole window at the frame cap, about a fifth of a CPU core. The power source is read where the
// strip decides its look (systemOnBattery: IOKit on macOS, GetSystemPowerStatus on Windows, the
// power supplies under /sys on Linux) and followed when it changes (watchPower, power.go).

const (
	lightStripPowerTitle = "Light strip on battery"
	lightStripPowerCheck = "Animate the light strip only on the power adapter"
	lightStripPowerHelp  = "On battery the strip shows its colour standing still: animations repaint the window and cost CPU."
)

// batteryStopsStrip reports whether the light strip stands still because the computer runs on
// battery and Preferences › App asks to animate it only on the power adapter.
func (sh *shell) batteryStopsStrip() bool {
	p := sh.ui.Preferences
	// The preference first: the power source is read only when it matters.
	return p != nil && p.LightStripMainsOnly && sh.systemBattery != nil && sh.systemBattery()
}

// lightStripPowerSetting returns the row and the function that shows the preference in it.
func (p *settingsPage) lightStripPowerSetting() (fyne.CanvasObject, func()) {
	check := widget.NewCheck(lightStripPowerCheck, func(on bool) { p.setPreference("lightStripMainsOnly", on) })
	sync := func() { check.SetChecked(p.prefs().LightStripMainsOnly) }
	return p.setting("lightStripMainsOnly", lightStripPowerTitle, lightStripPowerHelp, check), sync
}
