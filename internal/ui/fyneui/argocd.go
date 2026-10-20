package fyneui

import (
	"fmt"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/model/kinds"
)

// Argo CD in the window (G23): the navigator's group, the Sync and Refresh dialogs, the
// Dashboard's panel (argocd_dashboard.go).
//
// Lens's documentation (read 2026-10-13, not captured) lists the group's entries: Dashboard,
// Applications, Application Sets, Projects, Settings › Repositories, Settings › Clusters. Where
// Lens puts the group is not documented: Cogpit places it right after Flux CD (captured between
// Applications and Nodes), where Lens's documentation lists Argo CD too, after Flux CD (inferred).
// It shows while the cluster serves Argo CD's kinds; the argoproj.io group stays under Custom
// Resources as well, as curated sections' groups do.

// argoGroup is the navigator group's path.
const argoGroup = "argocd"

// argoNavGroup is the Argo CD group of navLayout.
var argoNavGroup = func() navEntry {
	dash := page(engine.ArgoCDDashboard)
	dash.needs = kinds.ArgoApplicationsKey
	return navGroup(argoGroup, "Argo CD", dash,
		titled(kinds.ArgoApplicationsKey, "Applications"),
		titled(kinds.ArgoApplicationSetKey, "Application Sets"),
		titled(kinds.ArgoProjectsKey, "Projects"),
		navGroup(argoGroup+"/settings", "Settings",
			titled(kinds.ArgoRepositoriesKey, "Repositories"),
			titled(kinds.ArgoClustersKey, "Clusters")))
}()

// The commands of Argo CD's dialogs.
const (
	argoSyncCommand        = "argocd.application.sync"
	argoSyncManyCommand    = "argocd.application.syncMany"
	argoRefreshCommand     = "argocd.application.refresh"
	argoRefreshManyCommand = "argocd.application.refreshMany"
)

// selectionDialogs are the GUI's own dialogs for actions on a selection, by command id: a command
// listed here opens its dialog from the selection's menu instead of running at once
// (tablecheck.go runOnSelection). Each feature registers its own in an init of its own file.
var selectionDialogs = map[string]func(sh *shell, refs []model.ObjectRef){}

func init() {
	itemActionDialogs[argoSyncCommand] = func(sh *shell, ref model.ObjectRef, _ model.ItemAction) {
		sh.openArgoSyncDialog([]model.ObjectRef{ref})
	}
	itemActionDialogs[argoRefreshCommand] = func(sh *shell, ref model.ObjectRef, _ model.ItemAction) {
		sh.openArgoRefreshDialog([]model.ObjectRef{ref})
	}
	selectionDialogs[argoSyncManyCommand] = (*shell).openArgoSyncDialog
	selectionDialogs[argoRefreshManyCommand] = (*shell).openArgoRefreshDialog
	commandIcons[argoSyncCommand] = theme.IconNameUpload // the live state moves to Git's (Cogpit's choice)
	commandIcons[argoRefreshCommand] = theme.IconNameViewRefresh
}

// argoRowNoun names the Dashboard's rows in hints: Needs attention lists Applications.
func argoRowNoun(sh *shell) string { return strings.ToLower(sh.kindTitle(kinds.ArgoApplicationsKey)) }

// argoTitle is a dialog's title naming the Applications it acts on: "Sync Application → shop"
// (the name shaped apart from the arrow, dialogframe.go), "Sync 3 Applications".
func argoTitle(verb string, refs []model.ObjectRef) *widget.RichText {
	if len(refs) == 1 {
		return objectDialogTitle(verb, "Application", refs[0].Name, "")
	}
	return dialogTitle(fmt.Sprintf("%s %d Applications", verb, len(refs)))
}

// argoDialog shows a dialog of Argo CD's actions (Lens's "Sync app(s)" and "Refresh app(s)"
// dialogs, documented, not captured: the layout and texts are Cogpit's): the Applications and
// their context as rows (D-85: Context › Namespace › Object, the Application as "Application →
// shop", WF32), the options, and the action's button. Several Applications are a list of checks
// in the Objects row, every one checked at first, as Lens preselects them; the user unchecks those
// to leave out, and run gets the checked ones (none: nothing runs).
func (sh *shell) argoDialog(action string, refs []model.ObjectRef, options fyne.CanvasObject, run func([]model.ObjectRef)) {
	th := sh.app.Settings().Theme()
	width := th.Size(sizeConfirmWidth)
	valueW := width - th.Size(sizeDrawerLabel) - 2*th.Size(theme.SizeNamePadding)
	context := ""
	if len(refs) > 0 {
		context = refs[0].Context
	}
	picker := newArgoPicker(refs)
	rows := box(kvLayout{})
	if len(refs) == 1 {
		placeRows(rows, context, []string{refs[0].Namespace}, []namedObject{{kind: "Application", name: refs[0].Name}}, valueW)
	} else {
		placeRows(rows, context, nil, nil, valueW)
		list := container.NewVScroll(picker.checks)
		list.SetMinSize(fyne.NewSize(valueW, min(picker.checks.MinSize().Height, th.Size(sizePopupMaxList))))
		rows.Objects = append(rows.Objects,
			rowName(fmt.Sprintf("Applications (%d)", len(refs))), list)
	}
	d := newDialogFrame(sh, argoTitle(action, refs)) // no gap after the arrow (dialogframe.go)
	d.setContent(container.NewVBox(rows, newSpace(theme.SizeNamePadding), options))
	confirm := widget.NewButtonWithIcon(action, theme.ConfirmIcon(), func() {
		d.Hide()
		if chosen := picker.chosen(); len(chosen) > 0 {
			run(chosen)
		}
	})
	confirm.Importance = widget.HighImportance
	d.SetButtons([]fyne.CanvasObject{widget.NewButtonWithIcon("Cancel", theme.CancelIcon(), d.Hide), confirm})
	d.Resize(fyne.NewSize(width, d.MinSize().Height))
	d.Show()
}

// argoPicker is a dialog's list of Applications, each a check.
type argoPicker struct {
	refs   []model.ObjectRef
	checks *widget.CheckGroup
}

func newArgoPicker(refs []model.ObjectRef) *argoPicker {
	keys := make([]string, len(refs))
	for i, r := range refs {
		keys[i] = r.Key()
	}
	p := &argoPicker{refs: refs, checks: widget.NewCheckGroup(keys, nil)}
	p.checks.SetSelected(keys)
	return p
}

// chosen returns the checked Applications, in the list's order; a single one is always chosen.
func (p *argoPicker) chosen() []model.ObjectRef {
	if len(p.refs) == 1 {
		return p.refs
	}
	var out []model.ObjectRef
	for _, r := range p.refs {
		if slices.Contains(p.checks.Selected, r.Key()) {
			out = append(out, r)
		}
	}
	return out
}

// openArgoSyncDialog asks whether to prune, then syncs: one Application through
// argocd.application.sync, several through argocd.application.syncMany. Prune starts off, as in
// Lens's documentation ("leave it off unless you intend that"); a sync with prune is destructive,
// so the engine's confirmation follows, naming what prune deletes (Cogpit's default: Lens shows
// the risk in its dialog).
func (sh *shell) openArgoSyncDialog(refs []model.ObjectRef) {
	if len(refs) == 0 {
		return
	}
	prune := widget.NewCheck("Prune resources not in Git", nil)
	width := sh.app.Settings().Theme().Size(sizeConfirmWidth)
	hint := newParagraph("Pruning deletes the live resources the Application manages that are no longer in Git; Cogpit asks once more before it does.", colorMuted, width)
	sh.argoDialog("Sync", refs, container.NewVBox(prune, hint), func(chosen []model.ObjectRef) {
		if len(chosen) == 1 {
			sh.dispatch(argoSyncCommand, argoSyncParams{Ref: chosen[0], Prune: prune.Checked}, nil)
			return
		}
		sh.dispatch(argoSyncManyCommand, argoSyncManyParams{Refs: chosen, Prune: prune.Checked}, nil)
	})
}

// openArgoRefreshDialog asks Normal or Hard, then refreshes.
func (sh *shell) openArgoRefreshDialog(refs []model.ObjectRef) {
	if len(refs) == 0 {
		return
	}
	const normal, hard = "Normal: compare with Git again", "Hard: also discard the cached manifests"
	mode := widget.NewRadioGroup([]string{normal, hard}, nil)
	mode.Required = true
	mode.SetSelected(normal)
	sh.argoDialog("Refresh", refs, mode, func(chosen []model.ObjectRef) {
		isHard := mode.Selected == hard
		if len(chosen) == 1 {
			sh.dispatch(argoRefreshCommand, argoRefreshParams{Ref: chosen[0], Hard: isHard}, nil)
			return
		}
		sh.dispatch(argoRefreshManyCommand, argoRefreshManyParams{Refs: chosen, Hard: isHard}, nil)
	})
}

// Parameters of Argo CD's commands, mirroring their schemas.
type (
	argoSyncParams struct {
		Ref   model.ObjectRef `json:"ref"`
		Prune bool            `json:"prune"`
	}
	argoSyncManyParams struct {
		Refs  []model.ObjectRef `json:"refs"`
		Prune bool              `json:"prune"`
	}
	argoRefreshParams struct {
		Ref  model.ObjectRef `json:"ref"`
		Hard bool            `json:"hard"`
	}
	argoRefreshManyParams struct {
		Refs []model.ObjectRef `json:"refs"`
		Hard bool              `json:"hard"`
	}
)
