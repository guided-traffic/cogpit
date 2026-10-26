package fyneui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// Cluster Settings › General › Streamed initial lists (D-new; engine/initiallists.go). Cogpit's own:
// Lens lists and then watches, and has no such setting. On, Automatic, by default: each read streams
// its initial list, and a stream that stays silent before its end makes the connection list. Off:
// every read lists and then watches; the user switches it off, or Cogpit does after a stream stalled
// while a list answered at once, and then says when and why under the switch.

const (
	streamsTitle = "Streamed initial lists"
	streamsCheck = "Stream the initial lists (WatchList)"
	streamsHelp  = "Off: every read lists and then watches, as Lens and kubectl read, from now on. Cogpit switches it off " +
		"on its own when a stream stays silent for 10 s and a list then answers at once. On again from the next connect."
)

// clusterStreamsSetting returns the row and the function that shows the setting in it, from the UI
// state's cluster settings, so that it follows a switch Cogpit makes at once.
func (p *settingsPage) clusterStreamsSetting() (fyne.CanvasObject, func()) {
	check := widget.NewCheck(streamsCheck, func(on bool) { p.setCluster("streamInitialLists", on) })
	note := newParagraph("", colorMuted, settingsHelpWidth)
	note.size = theme.SizeNameCaptionText
	p.controls["streamsOff"] = note
	ctx := p.shown.Context
	sync := func() {
		off := p.sh.ui.ClusterSettings[ctx].StreamsOff
		check.SetChecked(off == nil)
		if off == nil {
			note.Hide()
			return
		}
		note.SetText(streamsOffNote(off, p.sh.timeFormat()))
		note.Show()
	}
	row := p.setting("streamInitialLists", streamsTitle, streamsHelp, container.NewVBox(check, note))
	p.controls["streamInitialLists"] = check // the switch itself, not the box with its note
	return row, sync
}

// streamsOffNote says when and by whom the streams were switched off, and Cogpit's reason.
func streamsOffNote(off *engine.StreamsOff, tf model.TimeFormat) string {
	if off.By == engine.StreamsOffByCogpit {
		return "Off since " + tf.Format(off.At) + ", switched off by Cogpit: " + off.Why + "."
	}
	return "Off since " + tf.Format(off.At) + ", switched off by you."
}
