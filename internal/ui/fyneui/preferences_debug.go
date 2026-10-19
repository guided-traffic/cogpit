package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Preferences › Kubernetes › Debug container image (the owner, 2026-10-19; D-102;
// engine/debugcontainer.go): the image Debug offers for a pod's ephemeral container, app-wide,
// unlike the node shell's image, which is a cluster's. Cogpit's default, nicolaka/netshoot pinned by
// digest, is the field's placeholder; a line under it says whether the image in effect is pinned or
// a floating tag, as the debug dialog and its confirmation do. Only the user sets it
// (preferences.set refuses agents anything but the default).

const debugImageSetting = "debugImage"

// debugImageHelp is the field's help line, naming Cogpit's default as the engine pins it.
var debugImageHelp = "Debug adds it to a pod as an ephemeral container; empty: " + engine.DefaultDebugImageName +
	", pinned by digest."

// debugImageSetting returns the debug container's image setting and the function that shows the
// preference in it.
func (p *settingsPage) debugImageSetting() (fyne.CanvasObject, func()) {
	image := widget.NewEntry()
	image.SetPlaceHolder(engine.DefaultDebugImage)
	image.OnChanged = func(string) {
		p.typed(debugImageSetting, func() { p.setPreference(debugImageSetting, strings.TrimSpace(image.Text)) })
	}
	note := newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	note.truncate = true
	p.controls["debugImageNote"] = note
	sync := func() {
		p.setEntry(image, image, debugImageSetting, p.prefs().DebugImage)
		note.SetText(imagePinNote(strings.TrimSpace(image.Text), engine.DefaultDebugImage))
	}
	return p.setting(debugImageSetting, "Debug container image", debugImageHelp, container.NewVBox(image, note)), sync
}

// imagePinNote is the line under an image field: whether the image in effect (the field's, else
// def) is pinned by digest.
func imagePinNote(set, def string) string {
	if set == "" {
		set = def
	}
	if engine.ImagePinned(set) {
		return debugNotePinned
	}
	return debugNoteFloating
}

const (
	debugNotePinned   = "Pinned by digest: every debug container runs exactly this image."
	debugNoteFloating = "A floating tag, not pinned by digest: what it runs can change without Cogpit knowing."
)
