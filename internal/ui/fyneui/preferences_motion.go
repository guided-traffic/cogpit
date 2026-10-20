package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// Preferences › App › Reduce motion (the owner, 2026-10-20: "weniger Bewegung", for those who do
// not like the animations; engine.Preferences.ReduceMotion). Off by default: the navigator slides
// between All Clusters and a cluster's tree and from one cluster's tree to another's
// (navigator_slide.go), a dragged hotbar tile or tab glides into place (hotbardrag.go,
// stripdrag.go) and a reconnecting cluster's dot blinks (conndot.go); on, all of it switches at
// once and the dot is steady. macOS's own Reduce motion (System Settings › Accessibility ›
// Display, reducemotion_darwin.go) stops the same, whatever the preference says: the lead's
// decision (2026-10-20), so that one switch does not leave some of Cogpit's motion running and an
// accessibility setting the computer has does not stop only part of it.

const (
	reduceMotionTitle = "Reduce motion"
	reduceMotionCheck = "Switch at once instead of sliding"
	reduceMotionHelp  = "On: the navigator, tiles and tabs switch at once, no dot blinks. macOS's Reduce motion does the same."
)

// reduceMotion reports whether Cogpit moves nothing: the preference, or macOS's Reduce motion.
// Read on the main goroutine, where a motion starts.
func (sh *shell) reduceMotion() bool {
	if p := sh.ui.Preferences; p != nil && p.ReduceMotion {
		return true
	}
	return sh.systemReduce != nil && sh.systemReduce()
}

// reduceMotionSetting returns the row and the function that shows the preference in it.
func (p *settingsPage) reduceMotionSetting() (fyne.CanvasObject, func()) {
	check := widget.NewCheck(reduceMotionCheck, func(on bool) { p.setPreference("reduceMotion", on) })
	sync := func() { check.SetChecked(p.prefs().ReduceMotion) }
	return p.setting("reduceMotion", reduceMotionTitle, reduceMotionHelp, check), sync
}
