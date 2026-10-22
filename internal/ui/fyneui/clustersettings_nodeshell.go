package fyneui

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
)

// A cluster's Settings › Node Shell (G13, D-37; engine/nodeshell.go), as Lens's page holds it (its
// capture: the image, Lens's default shown as the field's placeholder, and an optional image pull
// secret in kube-system): the image of the privileged pod a node shell runs, with Cogpit's default,
// pinned by digest, as the placeholder, and the image pull secret. A line under the image says
// whether it is pinned by digest or a floating tag, as the node shell's confirmation does. Cogpit's
// own (owner, 2026-10-14): Maximum duration, how long the pod lives at most (its
// activeDeadlineSeconds; engine/nodeshell_duration.go), with a line under it saying it in words.
// Only the user sets them (cluster.settings.setNodeShell refuses agents anything but the
// defaults). Its place after Metrics follows Lens's order without the pages Cogpit lacks (Lens
// Metrics, Cost).

const (
	nodeShellSectionLabel       = "Node Shell"
	nodeShellImageSetting       = "nodeShellImage"
	nodeShellPullSecretSetting  = "nodeShellPullSecret"
	nodeShellMaxDurationSetting = "nodeShellMaxDuration"
	nodeShellNotePinned         = "Pinned by digest: every node shell runs exactly this image."
	nodeShellNoteFloating       = "A floating tag, not pinned by digest: what it runs can change without Cogpit knowing. The node shell's confirmation says so."
	nodeShellNoteDemo           = "The demo cluster is generated in memory: it has no nodes to open a shell on."
)

func init() { clusterSectionLabels[engine.SectionNodeShell] = nodeShellSectionLabel }

// nodeShellImageHelp is the image's help line, naming Cogpit's default as the engine pins it.
var nodeShellImageHelp = "The image of the privileged pod a node shell runs in kube-system; empty: Cogpit's default, " +
	engine.DefaultNodeShellImageName + " pinned by digest."

// nodeShellMaxDurationHelp is the maximum duration's help line, with Cogpit's bounds and default.
var nodeShellMaxDurationHelp = "How long a node shell's pod lives at most, even if Cogpit quits: " +
	engine.FormatNodeShellDeadline(engine.MinNodeShellDeadline) + " to " + engine.FormatNodeShellDeadline(engine.MaxNodeShellDeadline) +
	", such as 90m or 1h30m; empty: " + engine.FormatNodeShellDeadline(engine.DefaultNodeShellDeadline) + "."

// nodeShellMaxDurationNote is the line under the maximum duration: the deadline in words.
func nodeShellMaxDurationNote(set string) string {
	d := engine.DefaultNodeShellDeadline
	if minutes, err := engine.ParseNodeShellDeadline(set); err == nil && minutes != 0 {
		d = time.Duration(minutes) * time.Minute
	}
	return "Every node shell's pod ends by itself after " + engine.NodeShellDeadlineText(d) + "."
}

// clusterNodeShellSection builds a cluster's Settings › Node Shell.
func (p *settingsPage) clusterNodeShellSection() fyne.CanvasObject {
	image := widget.NewEntry()
	image.SetPlaceHolder(engine.DefaultNodeShellImage)
	image.OnChanged = func(string) {
		p.typed(nodeShellImageSetting, func() { p.setNodeShell("image", nodeShellImageSetting, strings.TrimSpace(image.Text)) })
	}
	secret := widget.NewEntry()
	secret.SetPlaceHolder("none")
	secret.OnChanged = func(string) {
		p.typed(nodeShellPullSecretSetting, func() {
			p.setNodeShell("imagePullSecret", nodeShellPullSecretSetting, strings.TrimSpace(secret.Text))
		})
	}
	maxDuration := widget.NewEntry()
	maxDuration.SetPlaceHolder(engine.FormatNodeShellDeadline(engine.DefaultNodeShellDeadline))
	maxDuration.OnChanged = func(string) {
		p.typed(nodeShellMaxDurationSetting, func() {
			p.setNodeShell("maxDuration", nodeShellMaxDurationSetting, strings.TrimSpace(maxDuration.Text))
		})
	}
	note := newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	note.truncate = true
	p.controls["nodeShellNote"] = note
	durationNote := newTextLine("", colorMuted, theme.SizeNameText, fyne.TextStyle{})
	durationNote.truncate = true
	p.controls["nodeShellMaxDurationNote"] = durationNote
	p.sync = func() {
		v := p.clusterView()
		p.setEntry(image, image, nodeShellImageSetting, v.NodeShellImage)
		p.setEntry(secret, secret, nodeShellPullSecretSetting, v.NodeShellPullSecret)
		p.setEntry(maxDuration, maxDuration, nodeShellMaxDurationSetting, v.NodeShellMaxDuration)
		durationNote.SetText(nodeShellMaxDurationNote(v.NodeShellMaxDuration))
		shown := strings.TrimSpace(image.Text)
		if shown == "" {
			shown = engine.DefaultNodeShellImage
		}
		switch {
		case engine.IsDemoContext(p.shown.Context):
			image.Disable()
			secret.Disable()
			maxDuration.Disable()
			note.SetText(nodeShellNoteDemo)
		case engine.ImagePinned(shown):
			note.SetText(nodeShellNotePinned)
		default:
			note.SetText(nodeShellNoteFloating)
		}
	}
	return container.NewVBox(
		p.setting(nodeShellImageSetting, "Image", nodeShellImageHelp, container.NewVBox(image, note)),
		p.setting(nodeShellPullSecretSetting, "Image pull secret",
			"A Secret in kube-system that pulls that image; empty: none.", secret),
		p.setting(nodeShellMaxDurationSetting, "Maximum duration", nodeShellMaxDurationHelp, container.NewVBox(maxDuration, durationNote)),
	)
}

// setNodeShell dispatches the user's node shell image, pull secret or maximum duration; a refusal
// shows under it.
func (p *settingsPage) setNodeShell(param, setting, value string) {
	if p.syncing {
		return
	}
	p.sh.dispatch("cluster.settings.setNodeShell", map[string]any{contextParam: p.shown.Context, param: value},
		p.settled(setting))
}
