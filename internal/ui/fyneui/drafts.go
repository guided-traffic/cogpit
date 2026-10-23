package fyneui

import (
	"fmt"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
)

// draftsBanner shows the drafts of unsaved editor text, and of a Config Map's values edited in the
// drawer (configmapdata.go), that Cogpit kept for the connected context below the drain and Helm
// banners, one row each (D-11, G9): which tab or drawer the text was typed in, when it was kept,
// and Discard (editor.discardDraft, the text is gone for good) and Restore (editor.restoreDraft,
// the tab opens with the text in the dock, or the drawer on the Config Map with the values in its
// editors). The engine announces changes
// with EventDrafts; the banner reads the drafts with editor.drafts, so that it shows what an agent
// would read. That Lens offers its drafts this way is not captured (inferred).
type draftsBanner struct {
	sh     *shell
	root   *fyne.Container // the rows; hidden while there are none
	queued atomic.Bool     // a re-read is queued; any goroutine
	drafts []engine.DraftInfo
}

// draftParams mirror editor.restoreDraft's and editor.discardDraft's parameters.
type draftParams struct {
	ID string `json:"id"`
}

func newDraftsBanner(sh *shell) *draftsBanner {
	b := &draftsBanner{sh: sh, root: container.NewVBox()}
	b.root.Hide()
	return b
}

// request re-reads the drafts. Any goroutine may call it; calls while one is queued are covered by
// it.
func (b *draftsBanner) request() {
	if !b.queued.CompareAndSwap(false, true) {
		return
	}
	b.sh.do(func() {
		b.queued.Store(false)
		b.sh.dispatch("editor.drafts", struct{}{}, b.show)
	})
}

// show renders an editor.drafts result. An error (no cluster connected, a session without drafts)
// empties the banner.
func (b *draftsBanner) show(res any, err error) {
	var list []engine.DraftInfo
	if err == nil {
		err = decode(res, &list)
	}
	if err != nil {
		list = nil
	}
	b.drafts = list
	rows := make([]fyne.CanvasObject, 0, len(list)+1)
	for _, d := range list {
		rows = append(rows, b.row(d))
	}
	if len(rows) > 0 {
		rows = append(rows, newDivider(false))
	}
	b.root.Objects = rows
	if len(rows) == 0 {
		b.root.Hide()
	} else {
		b.root.Show()
	}
	b.root.Refresh()
	b.sh.drawer.draftsChanged() // a draft restored for the Config Map the drawer shows (configmapdata.go)
}

// row is one draft: a tinted strip with an icon, two lines of text and the actions.
func (b *draftsBanner) row(d engine.DraftInfo) fyne.CanvasObject {
	title := newTextLine(draftTitle(d), theme.ColorNameForeground, theme.SizeNameText, fyne.TextStyle{Bold: true})
	title.truncate = true
	detail := newTextLine(draftDetail(d, b.sh.timeFormat()), colorMuted, theme.SizeNameText, fyne.TextStyle{})
	detail.truncate = true
	id := d.ID
	discard := newTextButton("Discard", nil, func() { b.sh.dispatch("editor.discardDraft", draftParams{ID: id}, nil) })
	restore := widget.NewButtonWithIcon("Restore", theme.HistoryIcon(), func() {
		b.sh.dispatch("editor.restoreDraft", draftParams{ID: id}, func(res any, err error) {
			var st struct {
				Tab engine.DockTab `json:"tab"`
			}
			if err == nil && decode(res, &st) == nil {
				b.sh.dock.requestFocus(st.Tab.ID) // the restored tab takes the keys; a Config Map's draft opens no tab (dockfocus.go)
			}
			b.sh.showError(err)
		})
	})
	restore.Importance = widget.HighImportance
	content := container.NewBorder(nil, nil,
		container.NewCenter(widget.NewIcon(themedIcon(theme.DocumentIcon(), colorAccent))),
		container.NewCenter(container.NewHBox(discard, restore)),
		container.NewVBox(title, detail))
	return container.NewStack(newBannerTint(colorAccent), inset(theme.SizeNameInnerPadding, theme.SizeNamePadding, content))
}

// draftTitle is a row's first line.
func draftTitle(d engine.DraftInfo) string {
	switch d.Type {
	case engine.DockCreate:
		return "Unsaved text of a Create resource tab"
	case engine.DraftConfigMapData:
		return "Unsaved values of " + d.Title + " (drawer)"
	}
	return "Unsaved changes to " + d.Title
}

// draftDetail is a row's second line: where the text belongs, when Cogpit kept it and its size (a
// drawer's values: how many); never the raw resourceVersion its changes are based on (WF13). The
// time is written in tf, the user's zone and format (D-31).
func draftDetail(d engine.DraftInfo, tf model.TimeFormat) string {
	where := "Context " + d.Context
	if d.Ref != nil && d.Ref.Namespace != "" {
		where += ", namespace " + d.Ref.Namespace
	}
	size := fmt.Sprintf("%d line%s", d.Lines, plural(d.Lines))
	if d.Type == engine.DraftConfigMapData {
		size = valuesText(len(d.Keys))
	}
	return where + ", kept " + tf.Format(d.Saved) + " · " + size
}
