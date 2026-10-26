package fyneui

import (
	"fyne.io/fyne/v2"

	"github.com/guided-traffic/cogpit/internal/helm"
	"github.com/guided-traffic/cogpit/internal/model"
)

// The release drawer's ways into the Revisions tab (D-131): a
// revision's number in the History is a link that opens the tab at that revision, compared with
// the newest (the newest itself not compared), and "Open revisions" at the History's title opens it
// at the newest. The drawer's toolbar and the Releases list's row menu offer ui.helm.revisions as a
// command of the kind (its icon beside Rollback).

func init() {
	drawerSections[helm.ReleaseKind+"\x00"+helm.TitleHistory] = releaseHistorySection
	drawerTitleParts[helm.ReleaseKind+"\x00"+helm.TitleHistory] = func(d *drawer, ref model.ObjectRef) fyne.CanvasObject {
		return fitLink(newLinkText("Open revisions", func() { openRevisions(d.sh, ref, 0) }))
	}
}

// revisionsOpenParams mirror ui.helm.revisions's parameters.
type revisionsOpenParams struct {
	refParams
	Revision int `json:"revision,omitempty"`
}

// openRevisions opens the release's Revisions tab at rev (0: the newest).
func openRevisions(sh *shell, ref model.ObjectRef, rev int) {
	sh.dispatch(revisionsCommand, revisionsOpenParams{refParams: refParams{Ref: ref}, Revision: rev}, nil)
}

// releaseHistorySection draws the History as the drawer draws its table (underTable), each
// revision's number a link into the Revisions tab.
func releaseHistorySection(d *drawer, ref model.ObjectRef, sec model.DetailSection) fyne.CanvasObject {
	t := sec.Table
	if t == nil || len(t.Cells) == 0 || len(t.Columns) < 3 {
		return d.table(ref, sec.Title, &model.DetailTable{})
	}
	colType := func(i int) model.ColumnType {
		if i < len(t.Types) {
			return t.Types[i]
		}
		return model.ColumnText
	}
	return d.underTable(ref, t, colType, func(r int) func() {
		if r >= len(t.Cells) || len(t.Cells[r]) == 0 || t.Cells[r][0].Num <= 0 {
			return nil
		}
		rev := int(t.Cells[r][0].Num)
		return func() { openRevisions(d.sh, ref, rev) }
	})
}
