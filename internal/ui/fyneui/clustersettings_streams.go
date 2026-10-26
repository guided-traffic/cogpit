package fyneui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Cluster Settings › General › Streaming (D-new; engine/initiallists.go). Cogpit's own. A switch,
// "Use streaming", and under it a box that describes the way chosen (the owner, 2026-10-26: what
// streaming is for and that some environments do not offer it; switched off, how Cogpit keeps the
// objects current then). On by default: each read streams its initial list, and a stream that stays
// silent before its end makes the connection list. Off: every read lists and then watches; the user
// switches it off, or Cogpit does after a stream stalled while a list answered at once, and then
// says when and why under the box.

const (
	streamsTitle = "Streaming"
	streamsCheck = "Use streaming"
)

// streamingOnText says what streaming is for, and where it may not work.
var streamingOnText = fmt.Sprintf("Each read asks the API server once: the server sends the objects one after another from "+
	"its cache, then keeps the same connection open and sends every change as it happens. The server builds no large "+
	"answer of all objects, Cogpit takes them object by object, and reading many kinds at once does not wait at Cogpit's "+
	"limit of %d requests a second. Some environments do not offer it (API servers without WatchList) or hold such "+
	"streams back (gateways, proxies): when a stream stays silent for %s, Cogpit reads that connection without "+
	"streaming, and switches it off for the cluster when a list then answers at once.",
	int(engine.RequestsPerSecond), engine.FormatSpan(engine.StreamStall))

// streamingOffText says how Cogpit keeps the objects current without streaming.
var streamingOffText = fmt.Sprintf("Each read first lists the objects in one answer, then watches from that list's "+
	"version: the connection stays open and the API server sends every change as it happens. The whole list is read "+
	"again only when the server no longer knows that version. Lists wait at Cogpit's limit of %d requests a second. "+
	"Switched on again, streaming applies from the next connect.", int(engine.RequestsPerSecond))

// clusterStreamsSetting returns the row and the function that shows the setting in it, from the UI
// state's cluster settings, so that it follows a switch Cogpit makes at once.
func (p *settingsPage) clusterStreamsSetting() (fyne.CanvasObject, func()) {
	check := widget.NewCheck(streamsCheck, func(on bool) { p.setCluster("streamInitialLists", on) })
	desc := newParagraph(streamingOnText, colorMuted, settingsHelpWidth)
	desc.size = theme.SizeNameCaptionText
	p.controls["streamsText"] = desc
	bg := newSurface(theme.ColorNameInputBackground)
	bg.radius, bg.stroke = theme.SizeNameInputRadius, colorHairline
	box := container.NewStack(bg, inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, desc))
	note := newParagraph("", colorMuted, settingsHelpWidth)
	note.size = theme.SizeNameCaptionText
	p.controls["streamsOff"] = note
	ctx := p.shown.Context
	sync := func() {
		off := p.sh.ui.ClusterSettings[ctx].StreamsOff
		check.SetChecked(off == nil)
		if off == nil {
			desc.SetText(streamingOnText)
			note.Hide()
			return
		}
		desc.SetText(streamingOffText)
		note.SetText(streamsOffNote(off, p.sh.timeFormat()))
		note.Show()
	}
	row := p.setting("streamInitialLists", streamsTitle, "", container.NewVBox(check, box, note))
	p.controls["streamInitialLists"] = check // the switch itself, not the box with its texts
	return row, sync
}

// streamsOffNote says when and by whom streaming was switched off, and Cogpit's reason.
func streamsOffNote(off *engine.StreamsOff, tf model.TimeFormat) string {
	if off.By == engine.StreamsOffByCogpit {
		return "Off since " + tf.Format(off.At) + ", switched off by Cogpit: " + off.Why + "."
	}
	return "Off since " + tf.Format(off.At) + ", switched off by you."
}
