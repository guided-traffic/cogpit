package fyneui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// Preferences › Kubernetes › Debug container image (the owner, 2026-10-19; D-102;
// engine/debugcontainer.go): the image Debug offers for a pod's ephemeral container, app-wide,
// unlike the node shell's image, which is a cluster's. The field shows an image by its short name,
// the pin behind it (the owner's R30, 2026-10-20, as the debug dialog does): Cogpit's default,
// nicolaka/netshoot:v0.16 pinned by digest, is the placeholder, a set image its short name; while
// the text is the set image's short name the setting keeps the image as it is, its digest included,
// the default's short name sets the default, and a changed text is kept as typed
// (engine.ImageFromField). A line under it says whether the image in effect is pinned or a floating
// tag, as the debug dialog and its confirmation do. Only the user sets it (preferences.set refuses
// agents anything but the default).

const debugImageSetting = "debugImage"

// debugImageHelp is the field's help line, naming Cogpit's default as the engine pins it.
var debugImageHelp = "Debug adds it to a pod as an ephemeral container; empty: " + engine.DefaultDebugImageName +
	", pinned by digest. An image shows by its name, its digest kept behind it until you change the name."

// debugImageSetting returns the debug container's image setting and the function that shows the
// preference in it.
func (p *settingsPage) debugImageSetting() (fyne.CanvasObject, func()) {
	image := newFieldEntry() // Escape closes the card (D-116, D-118)
	image.SetPlaceHolder(engine.DefaultDebugImageName)
	image.OnChanged = func(string) {
		p.typed(debugImageSetting, func() {
			p.setPreference(debugImageSetting, imageSetting(image.Text, p.prefs().DebugImage, engine.DefaultDebugImage))
		})
	}
	note := newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	note.truncate = true
	p.controls["debugImageNote"] = note
	sync := func() {
		set := p.prefs().DebugImage
		p.setEntry(image, &image.Entry, debugImageSetting, engine.ShortImage(set))
		note.SetText(imagePinNote(engine.ImageFromField(image.Text, set, engine.DefaultDebugImage), engine.DefaultDebugImage))
	}
	return p.setting(debugImageSetting, "Debug container image", debugImageHelp, container.NewVBox(image, note)), sync
}

// imageSetting is what an image field's text sets (the owner's R30): the set image unchanged while
// the text is its short name, "" (Cogpit's default, which Renovate moves) for the default or an
// empty field, else the text as typed.
func imageSetting(text, set, def string) string {
	if strings.TrimSpace(text) != "" && strings.TrimSpace(text) == engine.ShortImage(set) {
		return set
	}
	if image := engine.ImageFromField(text, set, def); image != def {
		return image
	}
	return ""
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
