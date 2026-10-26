package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Cluster Settings › General › Debug (D-135; engine/initiallists.go). Cogpit's own. A switch, "Use
// initial streaming (BETA)", off by default, and under it a box that says what it is (the owner,
// 2026-10-26: an opt-in beta; a static text that points at Kubernetes' WatchListClient and says it
// may save API server resources). Off: every read lists its objects once and then watches. On, from
// the next connect: every read gets its objects over the watch it keeps.

const (
	streamsTitle = "Debug"
	streamsCheck = "Use initial streaming (BETA)"
	streamsText  = "Streams each read's initial list over its watch instead of listing it first (Kubernetes' " +
		"WatchListClient, beta). This may save API server resources. Takes effect from the next connect."
)

// clusterStreamsSetting returns the row and the function that shows the setting in it, from the UI
// state's cluster settings.
func (p *settingsPage) clusterStreamsSetting() (fyne.CanvasObject, func()) {
	check := widget.NewCheck(streamsCheck, func(on bool) { p.setCluster("streamInitialLists", on) })
	desc := newParagraph(streamsText, colorMuted, settingsHelpWidth)
	desc.size = theme.SizeNameCaptionText
	p.controls["streamsText"] = desc
	bg := newSurface(theme.ColorNameInputBackground)
	bg.radius, bg.stroke = theme.SizeNameInputRadius, colorHairline
	box := container.NewStack(bg, inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, desc))
	ctx := p.shown.Context
	sync := func() { check.SetChecked(p.sh.ui.ClusterSettings[ctx].Streaming) }
	row := p.setting("streamInitialLists", streamsTitle, "", container.NewVBox(check, box))
	p.controls["streamInitialLists"] = check // the switch itself, not the box with its text
	return row, sync
}
